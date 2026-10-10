package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/project"
	"github.com/t8nax/gentry/internal/state"
)

// Values of action in the output of `gentry project add --json`.
const (
	actionAdded     = "added"
	actionUnchanged = "unchanged"
)

func runProjectAdd(args []string, env Env) int {
	f := newFlags("project add")
	knowledge := f.String("knowledge")
	prefix := f.String("prefix")
	if code, done := f.parse(args, env); done {
		return code
	}
	if !knowledge.Set || knowledge.Value == "" {
		return fail(env, missingArgument("project add", "knowledge", msg.Text(msg.ErrKnowledgeFlagMissing), hintOf(msg.HintKnowledgeFlag)))
	}
	var id string
	if len(f.args) > 0 {
		id = f.args[0]
		if !project.ValidID(id) {
			return fail(env, invalidArgument("project add", "project", id, msg.Text(msg.ErrProjectIDInvalid, id), hintOf(msg.HintProjectIDInvalid)))
		}
	}
	if prefix.Set && !project.ValidPrefix(prefix.Value) {
		return fail(env, flagValueInvalid("--prefix", prefix.Value, msg.Text(msg.ErrPrefixInvalid, prefix.Value), hintOf(msg.HintPrefixInvalid)))
	}
	dir, err := os.Getwd()
	if err != nil {
		return fail(env, internal(err))
	}

	path, err := state.Path()
	if err != nil {
		return fail(env, homeUnknown())
	}
	st, err := state.Open(path)
	if err != nil {
		return fail(env, stateFailure(err))
	}
	defer st.Close()
	// The process repository first: a project is connected with a place for
	// its flow.
	r, bad := openProcess()
	if bad != nil {
		return fail(env, *bad)
	}
	defer r.Close()
	res, err := project.Add(st, project.AddRequest{Dir: dir, ID: id, Knowledge: knowledge.Value, Prefix: prefix.Value})
	if err != nil {
		return fail(env, projectFailure(err))
	}
	if err := r.EnsureFlowDir(res.Project.ID); err != nil {
		return fail(env, processFailure(err))
	}

	p := res.Project
	layout, laidPool := layoutAdded(st, r, p.ID, res.Added)
	out := contract.ProjectAddOutput{
		Project:          p.ID,
		Prefix:           p.Prefix,
		Knowledge:        p.Knowledge,
		KnowledgeCreated: res.KnowledgeCreated,
		MainWorktree:     p.MainWorktree,
		Worktrees:        append([]string{}, res.Added...),
		Action:           actionAdded,
		Agents:           layout.json(laidPool),
	}
	if res.Unchanged {
		out.Action = actionUnchanged
	}
	x := projectAddExtra{repoCreated: res.RepoCreated, unpooled: res.Unpooled}
	return emit(env, out, func(p *page, out contract.ProjectAddOutput) { projectAddText(p, out, x) })
}

// projectAddExtra is what the text of project add has apart from the
// contract: whether the repository of the knowledge was created, and the
// worktrees of the repository in no pool.
type projectAddExtra struct {
	repoCreated bool
	unpooled    []string
}

// projectAddText prints the project connected: its knowledge, prefix and
// worktrees added to the pool, the layout of their subagents and the
// worktrees of the repository left out of the pool.
func projectAddText(p *page, out contract.ProjectAddOutput, x projectAddExtra) {
	if out.Action == actionUnchanged {
		fmt.Fprintln(p, msg.Text(msg.ProjectUnchanged, out.Project))
	} else {
		fmt.Fprintln(p, msg.Text(msg.ProjectAdded, out.Project))
		if x.repoCreated {
			fmt.Fprintln(p, msg.Text(msg.ProjectKnowledgeNew, out.Knowledge))
		} else {
			fmt.Fprintln(p, msg.Text(msg.ProjectKnowledge, out.Knowledge))
		}
		fmt.Fprintln(p, msg.Text(msg.ProjectPrefix, out.Prefix))
		fmt.Fprintln(p, msg.Text(msg.ProjectMainWorktree, out.MainWorktree))
		// The main worktree is added first.
		for _, w := range out.Worktrees[1:] {
			fmt.Fprintln(p, msg.Text(msg.ProjectWorktreeAdded, w))
		}
	}
	writeLayout(p, out.Agents, false, out.Project)
	// Other worktrees of the repository are not added on their own: the
	// operator decides where Gentry may work.
	if len(x.unpooled) > 0 {
		p.WriteString("\n")
		writeList(&p.Builder, msg.Text(msg.ProjectUnpooled), x.unpooled)
		p.hints(hintOf(msg.HintProjectUnpooled))
	}
}

// runProjectList prints the connected projects. It only reads: without a
// state store there are no projects, and no store is created.
func runProjectList(args []string, env Env) int {
	f := newFlags("project list")
	if code, done := f.parse(args, env); done {
		return code
	}
	projects, bad := readProjects()
	if bad != nil {
		return fail(env, *bad)
	}
	out := contract.ProjectListOutput{Projects: []contract.ProjectListItem{}}
	for _, p := range projects {
		out.Projects = append(out.Projects, contract.ProjectListItem{
			Project: p.ID, Prefix: p.Prefix, Knowledge: p.Knowledge, MainWorktree: p.MainWorktree,
		})
	}
	return emit(env, out, projectListText)
}

// projectListText prints the connected projects as a table, or a line that
// there are none with the hint to connect one.
func projectListText(p *page, out contract.ProjectListOutput) {
	if len(out.Projects) == 0 {
		fmt.Fprintln(p, msg.Text(msg.ProjectsNone))
		p.hints(hintOf(msg.HintProjectAdd))
		return
	}
	rows := [][]string{{msg.Text(msg.ColProject), msg.Text(msg.ColPrefix), msg.Text(msg.ColKnowledge), msg.Text(msg.ColMainWorktree)}}
	for _, pr := range out.Projects {
		rows = append(rows, []string{pr.Project, pr.Prefix, pr.Knowledge, orNone(pr.MainWorktree)})
	}
	writeTable(&p.Builder, rows)
}

func readProjects() ([]state.Project, *failure) {
	path, err := state.Path()
	if err != nil {
		f := homeUnknown()
		return nil, &f
	}
	st, err := state.OpenRead(path)
	if errors.Is(err, state.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		f := stateFailure(err)
		return nil, &f
	}
	defer st.Close()
	projects, err := st.Projects()
	if err != nil {
		f := stateFailure(err)
		return nil, &f
	}
	return projects, nil
}

// projectFailure turns an error of connecting a project into a failure.
func projectFailure(err error) failure {
	var (
		notRepo  *project.NotRepoError
		ke       *project.KnowledgeError
		newer    *project.NewerFormatError
		mismatch *project.MismatchError
		noID     *project.IDMissingError
		noPrefix *project.PrefixUnderivableError
		exists   *project.ExistsError
		taken    *project.PrefixTakenError
		wtTaken  *project.WorktreeTakenError
		pathErr  *fs.PathError
	)
	if f, ok := gitFailure(err); ok {
		return f
	}
	switch {
	case errors.As(err, &notRepo):
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeNotGitRepo,
			message: msg.Text(msg.ErrNotGitRepo, notRepo.Path),
			hints:   []hint{hintOf(msg.HintNotGitRepo)},
			details: map[string]any{"path": notRepo.Path},
		}
	case errors.As(err, &ke):
		return knowledgeFailure(ke)
	case errors.As(err, &newer):
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeKnowledgeNewer,
			message: msg.Text(msg.ErrKnowledgeNewer, newer.Format, newer.Supported),
			hints:   []hint{hintOf(msg.HintKnowledgeNewer)},
			details: map[string]any{"path": newer.Path, "format": newer.Format, "supported": newer.Supported},
		}
	case errors.As(err, &mismatch):
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeKnowledgeMismatch,
			message: msg.Text(msg.ErrKnowledgeMismatch, mismatch.Value, mismatch.KnowledgeValue),
			hints:   []hint{hintOf(msg.HintKnowledgeMismatch, mismatch.KnowledgeValue)},
			details: map[string]any{"value": mismatch.Value, "knowledge_value": mismatch.KnowledgeValue},
		}
	case errors.As(err, &noID):
		return missingArgument("project add", "project", msg.Text(msg.ErrProjectIDMissing), hintOf(msg.HintProjectIDMissing))
	case errors.As(err, &noPrefix):
		return missingArgument("project add", "prefix", msg.Text(msg.ErrPrefixUnderivable, noPrefix.ID), hintOf(msg.HintPrefixUnderivable))
	case errors.As(err, &exists):
		f := failure{
			exit:    contract.ExitError,
			code:    contract.CodeProjectExists,
			message: msg.Text(msg.ErrProjectExists, exists.Project, exists.Knowledge),
			details: map[string]any{"project": exists.Project, "knowledge": exists.Knowledge, "main_worktree": exists.MainWorktree},
		}
		if exists.Clone {
			f.message = msg.Text(msg.ErrProjectClone, exists.Project, exists.MainWorktree)
			f.hints = []hint{hintOf(msg.HintProjectClone).set("project", exists.Project)}
		}
		return f
	case errors.As(err, &taken):
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodePrefixTaken,
			message: msg.Text(msg.ErrPrefixTaken, taken.Prefix, taken.Project),
			hints:   []hint{hintOf(msg.HintPrefixTaken)},
			details: map[string]any{"prefix": taken.Prefix, "project": taken.Project},
		}
	case errors.As(err, &wtTaken):
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeWorktreeTaken,
			message: msg.Text(msg.ErrWorktreeTaken, wtTaken.Project, wtTaken.Path),
			details: map[string]any{"path": wtTaken.Path, "project": wtTaken.Project},
		}
	case errors.As(err, &pathErr):
		return ioError(pathErr.Path, pathErr.Err)
	}
	return stateFailure(err)
}

func knowledgeFailure(e *project.KnowledgeError) failure {
	f := failure{
		exit:    contract.ExitError,
		code:    contract.CodeKnowledgeInvalid,
		details: map[string]any{"path": e.Path, "reason": e.Reason},
	}
	switch e.Reason {
	case project.ReasonNotEmpty:
		f.message, f.hints = msg.Text(msg.ErrKnowledgeNotEmpty, e.Path), []hint{hintOf(msg.HintKnowledgeDir)}
	case project.ReasonForeignRepo:
		f.message, f.hints = msg.Text(msg.ErrKnowledgeForeign, e.Path), []hint{hintOf(msg.HintKnowledgeDir)}
	case project.ReasonNested:
		f.message, f.hints = msg.Text(msg.ErrKnowledgeNested, e.Path, e.Worktree), []hint{hintOf(msg.HintKnowledgeNested)}
		f.details["worktree"] = e.Worktree
	default:
		f.message, f.hints = msg.Text(msg.ErrKnowledgeBadFile, e.Cause), []hint{hintOf(msg.HintKnowledgeBadFile, e.Path)}
	}
	return f
}
