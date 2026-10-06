// Package task takes tasks of projects and shows them. A task holds a
// worktree of the pool of its project and is pinned to a snapshot of the flow
// it was taken with.
package task

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/flow"
	"github.com/t8nax/gentry/internal/git"
	"github.com/t8nax/gentry/internal/paths"
	"github.com/t8nax/gentry/internal/process"
	"github.com/t8nax/gentry/internal/state"
)

// EventTaken is the event of a task taken.
const EventTaken = "task.taken"

// TitleMax is the limit of a title in characters.
const TitleMax = 80

// Reasons a title is refused, as the contract names them.
const (
	TitleTooLong   = "too_long"
	TitleMultiline = "multiline"
)

// CheckTitle returns why a title is refused, or "" if it is fine.
func CheckTitle(title string) string {
	switch {
	case strings.ContainsAny(title, "\r\n"):
		return TitleMultiline
	case utf8.RuneCountInString(title) > TitleMax:
		return TitleTooLong
	}
	return ""
}

// NotPooledError means the directory is not a worktree of a pool.
type NotPooledError struct{ Path string }

func (e *NotPooledError) Error() string { return e.Path + ": not in a pool" }

// BusyError means the worktree holds a task already.
type BusyError struct{ Path, Task string }

func (e *BusyError) Error() string { return e.Path + ": holds " + e.Task }

// DirtyError means the worktree has uncommitted changes.
type DirtyError struct {
	Path  string
	Files []string // relative to the worktree root, with forward slashes
}

func (e *DirtyError) Error() string { return e.Path + ": uncommitted changes" }

// NotFoundError means no task has the identifier.
type NotFoundError struct{ Task string }

func (e *NotFoundError) Error() string { return e.Task + ": no such task" }

// UndeterminedError means no task is named and the directory holds none.
type UndeterminedError struct{ Dir string }

func (e *UndeterminedError) Error() string { return e.Dir + ": no task" }

// InvalidKeyError means a task is named by something that is not an
// identifier of a task.
type InvalidKeyError struct{ Value string }

func (e *InvalidKeyError) Error() string { return e.Value + ": not a task identifier" }

// Worktree returns the worktree of the pool that dir is: the root of the git
// worktree of dir, or dir itself if it is no git worktree.
func Worktree(worktrees []state.Worktree, dir string) (state.Worktree, error) {
	d, err := paths.Canonical(dir)
	if err != nil {
		return state.Worktree{}, err
	}
	root := d
	if fi, err := os.Stat(d); err == nil && fi.IsDir() {
		r, err := git.Root(d)
		switch {
		case err == nil:
			root = r
		case !errors.Is(err, git.ErrNotRepo):
			return state.Worktree{}, err
		}
	}
	for _, w := range worktrees {
		if paths.Same(w.Path, root) {
			return w, nil
		}
	}
	return state.Worktree{}, &NotPooledError{Path: root}
}

// CheckClean refuses a worktree with uncommitted changes. Files git ignores
// do not count.
func CheckClean(path string) error {
	files, err := git.Changed(path)
	if err != nil {
		return err
	}
	if len(files) > 0 {
		return &DirtyError{Path: path, Files: files}
	}
	return nil
}

// TakeRequest is a task to take. The flow is read before: its reading and
// the synchronization of the process stay out of the transaction.
type TakeRequest struct {
	Project   string
	Worktree  string // a worktree of the pool of Project
	Scenario  flow.Scenario
	Title     string
	Statement string
	Source    string          // state.SourceOperator or state.SourceAgent
	Snapshot  flow.Snapshot   // the active flow
	Flow      process.Applied // the commit that applied the flow
	Library   process.Applied // the commit that applied the library; zero if none
}

// Take records the task in one transaction with its event: the worktree is
// checked to be free, the flow is kept as a snapshot unless it is already,
// and the task gets the next number of the series of the project.
func Take(st *state.Store, req TakeRequest) (state.Task, error) {
	content, err := req.Snapshot.Encode()
	if err != nil {
		return state.Task{}, err
	}
	var task state.Task
	err = st.Write(func(tx *state.Tx) error {
		if held, ok, err := tx.TaskIn(req.Worktree); err != nil {
			return err
		} else if ok {
			return &BusyError{Path: req.Worktree, Task: held.Key()}
		}
		applied, err := keepSnapshot(tx, req, string(content))
		if err != nil {
			return err
		}
		n, err := tx.TakeNumber(req.Project)
		if err != nil {
			return err
		}
		task, err = tx.AddTask(state.Task{
			Project: req.Project, Number: n, Title: req.Title, Statement: req.Statement, Source: req.Source,
			State: state.TaskActive, Scenario: req.Scenario.ID, Node: req.Scenario.Start,
			FlowCommit: applied.Commit, Worktree: req.Worktree,
		})
		if err != nil {
			return err
		}
		task.FlowApplied = applied.Time
		projects, err := tx.Projects()
		if err != nil {
			return err
		}
		for _, p := range projects {
			if p.ID == req.Project {
				task.Prefix = p.Prefix
			}
		}
		_, err = tx.AddEvent(EventTaken, req.Project, task.Key(), contract.TaskTakenData{
			Title: task.Title, Scenario: task.Scenario, Node: task.Node, Worktree: task.Worktree,
			FlowCommit: task.FlowCommit, Source: contract.TaskTakenDataSource(task.Source),
		})
		return err
	})
	if err != nil {
		return state.Task{}, err
	}
	return task, nil
}

// keepSnapshot stores the snapshot of the flow unless it is stored already,
// and returns the commit it is stored by. The commit that applied the flow
// names it; if the library changed a subagent of the flow after that commit,
// the commit that applied the library does, so that a snapshot never changes.
func keepSnapshot(tx *state.Tx, req TakeRequest, content string) (process.Applied, error) {
	key := req.Flow
	s, ok, err := tx.Snapshot(req.Project, key.Commit)
	if err != nil {
		return process.Applied{}, err
	}
	if ok && s.Content != content && req.Library.Commit != "" {
		key = req.Library
		if s, ok, err = tx.Snapshot(req.Project, key.Commit); err != nil {
			return process.Applied{}, err
		}
	}
	if ok {
		return process.Applied{Commit: s.Commit, Time: s.Applied}, nil
	}
	err = tx.AddSnapshot(state.FlowSnapshot{Project: req.Project, Commit: key.Commit, Applied: key.Time, Content: content})
	return key, err
}

// keyPattern is an identifier of a task: the prefix of the project and the
// number, in any letter case.
var keyPattern = regexp.MustCompile(`^([A-Za-z]{2,10})-([1-9][0-9]{0,8})$`)

// Find returns the task named by key, such as SHOP-1 or shop-1, among tasks;
// of several tasks with the number, possible from stage 8, the last taken.
func Find(tasks []state.Task, key string) (state.Task, error) {
	m := keyPattern.FindStringSubmatch(key)
	if m == nil {
		return state.Task{}, &InvalidKeyError{Value: key}
	}
	prefix := strings.ToUpper(m[1])
	n, _ := strconv.Atoi(m[2])
	name := fmt.Sprintf("%s-%d", prefix, n)
	var found *state.Task
	for i := range tasks {
		if tasks[i].Prefix == prefix && tasks[i].Number == n {
			found = &tasks[i]
		}
	}
	if found == nil {
		return state.Task{}, &NotFoundError{Task: name}
	}
	return *found, nil
}

// In returns the task that holds the worktree at path among tasks.
func In(tasks []state.Task, path string) (state.Task, bool) {
	for _, t := range tasks {
		if t.Worktree != "" && paths.Same(t.Worktree, path) {
			return t, true
		}
	}
	return state.Task{}, false
}

// Current returns the task of the worktree that dir is.
func Current(tasks []state.Task, worktrees []state.Worktree, dir string) (state.Task, error) {
	if d, err := paths.Canonical(dir); err == nil {
		dir = d
	}
	w, err := Worktree(worktrees, dir)
	var np *NotPooledError
	if errors.As(err, &np) {
		return state.Task{}, &UndeterminedError{Dir: dir}
	}
	if err != nil {
		return state.Task{}, err
	}
	t, ok := In(tasks, w.Path)
	if !ok {
		return state.Task{}, &UndeterminedError{Dir: dir}
	}
	return t, nil
}

// View is a task with the names of its scenario and stage in its flow.
type View struct {
	state.Task
	ScenarioTitle string
	Stage         string // the stage of the current node
	StageTitle    string
}

// Views returns the tasks with the names from their snapshots; each snapshot
// is read once.
func Views(st *state.Store, tasks []state.Task) ([]View, error) {
	flows := map[[2]string]*flow.Flow{}
	views := make([]View, len(tasks))
	for i, t := range tasks {
		k := [2]string{t.Project, t.FlowCommit}
		fl, ok := flows[k]
		if !ok {
			var err error
			if fl, err = snapshotFlow(st, t.Project, t.FlowCommit); err != nil {
				return nil, err
			}
			flows[k] = fl
		}
		views[i] = view(t, fl)
	}
	return views, nil
}

// snapshotFlow reads the flow of a snapshot; nil if it cannot be read, as a
// later Gentry may check more than the one that took the task.
func snapshotFlow(st *state.Store, project, commit string) (*flow.Flow, error) {
	s, ok, err := st.Snapshot(project, commit)
	if err != nil || !ok {
		return nil, err
	}
	snap, err := flow.DecodeSnapshot([]byte(s.Content))
	if err != nil {
		return nil, nil
	}
	res, err := flow.ReadSnapshot(snap)
	if err != nil {
		return nil, nil
	}
	return res.Flow, nil
}

// view names the scenario and the stage of t in its flow fl; without the
// flow the stage is named by the node.
func view(t state.Task, fl *flow.Flow) View {
	v := View{Task: t, Stage: t.Node}
	if fl == nil {
		return v
	}
	s, ok := fl.Scenario(t.Scenario)
	if !ok {
		return v
	}
	v.ScenarioTitle = s.Title
	for _, n := range s.Nodes {
		if n.ID == t.Node {
			v.Stage = n.Stage
		}
	}
	if st, ok := fl.Stage(v.Stage); ok {
		v.StageTitle = st.Title
	}
	return v
}

// ViewOf returns the view of a task just taken with flow fl.
func ViewOf(t state.Task, fl *flow.Flow) View { return view(t, fl) }
