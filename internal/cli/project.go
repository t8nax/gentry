package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/git"
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
	asJSON := f.Bool("json")
	if code, done := f.parse(args, env); done {
		return code
	}
	if !knowledge.Set || knowledge.Value == "" {
		return fail(env, missingArgument("project add", "knowledge", msg.Text(msg.ErrKnowledgeFlagMissing), ""))
	}
	var id string
	if len(f.args) > 0 {
		id = f.args[0]
		if !project.ValidID(id) {
			return fail(env, invalidArgument("project add", "project", id, msg.Text(msg.ErrProjectIDInvalid, id), ""))
		}
	}
	if prefix.Set && !project.ValidPrefix(prefix.Value) {
		return fail(env, flagValueInvalid("--prefix", prefix.Value, msg.Text(msg.ErrPrefixInvalid, prefix.Value)))
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
	res, err := project.Add(st, project.AddRequest{Dir: dir, ID: id, Knowledge: knowledge.Value, Prefix: prefix.Value})
	if err != nil {
		return fail(env, projectFailure(err))
	}

	p := res.Project
	if *asJSON {
		out := contract.ProjectAddOutput{
			Project:          p.ID,
			Prefix:           p.Prefix,
			Knowledge:        p.Knowledge,
			KnowledgeCreated: res.KnowledgeCreated,
			MainWorktree:     p.MainWorktree,
			Worktrees:        append([]string{}, res.Added...),
			Action:           actionAdded,
		}
		if res.Unchanged {
			out.Action = actionUnchanged
		}
		if err := writeJSON(env, out); err != nil {
			return fail(env, internal(err))
		}
		return contract.ExitOK
	}
	if res.Unchanged {
		fmt.Fprintln(env.Stdout, msg.Text(msg.ProjectUnchanged, p.ID))
		return contract.ExitOK
	}
	var b strings.Builder
	fmt.Fprintln(&b, msg.Text(msg.ProjectAdded, p.ID))
	if res.RepoCreated {
		fmt.Fprintln(&b, msg.Text(msg.ProjectKnowledgeNew, p.Knowledge))
	} else {
		fmt.Fprintln(&b, msg.Text(msg.ProjectKnowledge, p.Knowledge))
	}
	fmt.Fprintln(&b, msg.Text(msg.ProjectPrefix, p.Prefix))
	fmt.Fprintln(&b, msg.Text(msg.ProjectMainWorktree, p.MainWorktree))
	for _, w := range res.Added[1:] {
		fmt.Fprintln(&b, msg.Text(msg.ProjectWorktreeAdded, w))
	}
	fmt.Fprint(env.Stdout, b.String())
	return contract.ExitOK
}

// runProjectList prints the connected projects. It only reads: without a
// state store there are no projects, and no store is created.
func runProjectList(args []string, env Env) int {
	f := newFlags("project list")
	asJSON := f.Bool("json")
	if code, done := f.parse(args, env); done {
		return code
	}
	projects, bad := readProjects()
	if bad != nil {
		return fail(env, *bad)
	}
	if *asJSON {
		out := contract.ProjectListOutput{Projects: []contract.ProjectListItem{}}
		for _, p := range projects {
			out.Projects = append(out.Projects, contract.ProjectListItem{
				Project: p.ID, Prefix: p.Prefix, Knowledge: p.Knowledge, MainWorktree: p.MainWorktree,
			})
		}
		if err := writeJSON(env, out); err != nil {
			return fail(env, internal(err))
		}
		return contract.ExitOK
	}
	if len(projects) == 0 {
		fmt.Fprintln(env.Stdout, msg.Text(msg.ProjectsNone))
		return contract.ExitOK
	}
	var b strings.Builder
	for _, p := range projects {
		fmt.Fprintln(&b, msg.Text(msg.ProjectLine, p.ID, p.Prefix, p.Knowledge, p.MainWorktree))
	}
	fmt.Fprint(env.Stdout, b.String())
	return contract.ExitOK
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
		notRepo   *project.NotRepoError
		ke        *project.KnowledgeError
		newer     *project.NewerFormatError
		mismatch  *project.MismatchError
		noID      *project.IDMissingError
		noPrefix  *project.PrefixUnderivableError
		exists    *project.ExistsError
		taken     *project.PrefixTakenError
		wtTaken   *project.WorktreeTakenError
		gitFailed *git.CommandError
		pathErr   *fs.PathError
	)
	switch {
	case errors.Is(err, git.ErrNotFound):
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeGitNotFound,
			message: msg.Text(msg.ErrToolNotFound, "git"),
			hint:    msg.Text(msg.HintGitNotFound),
		}
	case errors.As(err, &gitFailed):
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeGitFailed,
			message: msg.Text(msg.ErrGitFailed, gitFailed.Command, gitFailed.Output),
			details: map[string]any{"command": gitFailed.Command, "output": gitFailed.Output},
		}
	case errors.As(err, &notRepo):
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeNotGitRepo,
			message: msg.Text(msg.ErrNotGitRepo, notRepo.Path),
			hint:    msg.Text(msg.HintNotGitRepo),
			details: map[string]any{"path": notRepo.Path},
		}
	case errors.As(err, &ke):
		return knowledgeFailure(ke)
	case errors.As(err, &newer):
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeKnowledgeNewer,
			message: msg.Text(msg.ErrKnowledgeNewer, newer.Path, newer.Format, newer.Supported),
			hint:    msg.Text(msg.HintKnowledgeNewer),
			details: map[string]any{"path": newer.Path, "format": newer.Format, "supported": newer.Supported},
		}
	case errors.As(err, &mismatch):
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeKnowledgeMismatch,
			message: msg.Text(msg.ErrKnowledgeMismatch, mismatch.Value, mismatch.KnowledgeValue),
			hint:    msg.Text(msg.HintKnowledgeMismatch),
			details: map[string]any{"value": mismatch.Value, "knowledge_value": mismatch.KnowledgeValue},
		}
	case errors.As(err, &noID):
		return missingArgument("project add", "project", msg.Text(msg.ErrProjectIDMissing), "")
	case errors.As(err, &noPrefix):
		return missingArgument("project add", "prefix", msg.Text(msg.ErrPrefixUnderivable, noPrefix.ID), msg.Text(msg.HintPrefixUnderivable))
	case errors.As(err, &exists):
		f := failure{
			exit:    contract.ExitError,
			code:    contract.CodeProjectExists,
			message: msg.Text(msg.ErrProjectExists, exists.Project, exists.Knowledge),
			details: map[string]any{"project": exists.Project, "knowledge": exists.Knowledge, "main_worktree": exists.MainWorktree},
		}
		if exists.Clone {
			f.message = msg.Text(msg.ErrProjectClone, exists.Project, exists.MainWorktree)
		}
		return f
	case errors.As(err, &taken):
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodePrefixTaken,
			message: msg.Text(msg.ErrPrefixTaken, taken.Prefix, taken.Project),
			hint:    msg.Text(msg.HintPrefixTaken),
			details: map[string]any{"prefix": taken.Prefix, "project": taken.Project},
		}
	case errors.As(err, &wtTaken):
		return failure{
			exit:    contract.ExitError,
			code:    contract.CodeWorktreeTaken,
			message: msg.Text(msg.ErrWorktreeTaken, wtTaken.Path, wtTaken.Project),
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
		f.message, f.hint = msg.Text(msg.ErrKnowledgeNotEmpty, e.Path), msg.Text(msg.HintKnowledgeDir)
	case project.ReasonForeignRepo:
		f.message, f.hint = msg.Text(msg.ErrKnowledgeForeign, e.Path), msg.Text(msg.HintKnowledgeDir)
	case project.ReasonNested:
		f.message, f.hint = msg.Text(msg.ErrKnowledgeNested, e.Path, e.Worktree), msg.Text(msg.HintKnowledgeNested)
		f.details["worktree"] = e.Worktree
	default:
		f.message, f.hint = msg.Text(msg.ErrKnowledgeBadFile, e.Path, e.Cause), msg.Text(msg.HintKnowledgeBadFile)
	}
	return f
}
