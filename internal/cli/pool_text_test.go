package cli

import (
	"testing"

	"github.com/t8nax/gentry/contract"
	"github.com/t8nax/gentry/internal/agents"
	"github.com/t8nax/gentry/internal/msg"
	"github.com/t8nax/gentry/internal/project"
)

// shopKnowledge is the knowledge of the shop as the texts name it.
const shopKnowledge = "/work/shop-knowledge"

func TestProjectAddText(t *testing.T) {
	added := contract.ProjectAddOutput{Project: "shop", Prefix: "SHOP", Knowledge: shopKnowledge, KnowledgeCreated: true,
		MainWorktree: shopMain, Worktrees: []string{shopMain, shopFix}, Action: actionAdded}
	unchanged := contract.ProjectAddOutput{Project: "shop", Prefix: "SHOP", Knowledge: shopKnowledge, MainWorktree: shopMain,
		Worktrees: []string{}, Action: actionUnchanged}
	laid := added
	laid.Worktrees = []string{shopMain}
	laid.Agents = &contract.AgentsLayout{Worktrees: []contract.AgentsWorktree{{Path: shopMain, Added: []string{"reviewer"}}}}
	unpooled := lines(
		"",
		"Рабочие копии репозитория, не внесённые в пул:",
		"  /work/shop-fix",
		"",
	)
	tests := []struct {
		name       string
		out        contract.ProjectAddOutput
		x          projectAddExtra
		cli, agent string
	}{
		{"added", added, projectAddExtra{repoCreated: true}, lines(
			"Проект shop подключён.",
			"Знание проекта (репозиторий создан): /work/shop-knowledge",
			"Префикс номеров задач: SHOP",
			"Основная рабочая копия: /work/shop",
			"Также внесена в пул: /work/shop-fix",
		), ""},
		{"knowledge of a repository", laid, projectAddExtra{}, lines(
			msg.Text(msg.ProjectAdded, "shop"),
			"Знание проекта: /work/shop-knowledge",
			msg.Text(msg.ProjectPrefix, "SHOP"),
			msg.Text(msg.ProjectMainWorktree, shopMain),
			"",
			msg.Text(msg.AgentsFreeSynced, "shop"),
			msg.Text(msg.AgentsNextSession),
		), ""},
		{"unchanged", unchanged, projectAddExtra{}, "Проект shop уже подключён.\n", ""},
		{"worktrees out of the pool", unchanged, projectAddExtra{unpooled: []string{shopFix}},
			msg.Text(msg.ProjectUnchanged, "shop") + "\n" + unpooled + lines(hintLineOf(msg.HintProjectUnpooled, "gentry worktree add <путь>")),
			msg.Text(msg.ProjectUnchanged, "shop") + "\n" + unpooled + lines(hintLineOf(msg.HintProjectUnpooled, "worktree_add (path)"))},
	}
	for _, tt := range tests {
		wantText(t, tt.name, func(p *page) { projectAddText(p, tt.out, tt.x) }, tt.cli, tt.agent)
	}
}

func TestProjectListText(t *testing.T) {
	wantText(t, "no projects", func(p *page) { projectListText(p, contract.ProjectListOutput{Projects: []contract.ProjectListItem{}}) }, lines(
		"Подключённых проектов нет.",
		"",
		hintLineOf(msg.HintProjectAdd, "gentry project add"),
	), lines(
		"Подключённых проектов нет.",
		"",
		hintLineOf(msg.HintProjectAdd, "project_add"),
	))
	out := contract.ProjectListOutput{Projects: []contract.ProjectListItem{{Project: "shop", Prefix: "SHOP", Knowledge: shopKnowledge, MainWorktree: shopMain}}}
	wantText(t, "projects", func(p *page) { projectListText(p, out) }, table(
		[]string{msg.Text(msg.ColProject), msg.Text(msg.ColPrefix), msg.Text(msg.ColKnowledge), msg.Text(msg.ColMainWorktree)},
		[]string{"shop", "SHOP", shopKnowledge, shopMain},
	), "")
}

func TestProjectFailureText(t *testing.T) {
	tests := []struct {
		name       string
		f          failure
		cli, agent string
	}{
		{"not a worktree", projectFailure(&project.NotRepoError{Path: "/work"}), lines(
			"Папка не является рабочей копией git: /work",
			"",
			"Выполните команду в рабочей копии кода проекта.",
		), ""},
		{"no identifier", projectFailure(&project.IDMissingError{}), lines(
			"Для нового знания нужен идентификатор проекта.",
			"",
			hintLineOf(msg.HintProjectIDMissing, "gentry project add <идентификатор> --knowledge <путь>"),
		), lines(
			"Для нового знания нужен идентификатор проекта.",
			"",
			hintLineOf(msg.HintProjectIDMissing, "project_add (project, knowledge)"),
		)},
		{"a clone", projectFailure(&project.ExistsError{Project: "shop", Knowledge: shopKnowledge, MainWorktree: shopMain, Clone: true}), lines(
			"Проект shop уже подключён, основная рабочая копия: /work/shop",
			"",
			hintLineOf(msg.HintProjectClone, "gentry worktree add --project shop"),
		), lines(
			"Проект shop уже подключён, основная рабочая копия: /work/shop",
			"",
			hintLineOf(msg.HintProjectClone, "worktree_add (project: shop)"),
		)},
	}
	for _, tt := range tests {
		wantFail(t, tt.name, tt.f, tt.cli, tt.agent)
	}
	// The cause of the damaged information of the project is a text of the
	// package project, from the catalog.
	for _, c := range []struct {
		cause, want string
	}{
		{msg.Text(msg.CauseInfoSyntax), "Сведения о проекте в знании повреждены: их не удаётся прочитать"},
		{msg.Text(msg.CauseInfoMissing, "format"), "Сведения о проекте в знании повреждены: нет поля format"},
		{msg.Text(msg.CauseInfoValue, "format", "one"), "Сведения о проекте в знании повреждены: недопустимое значение поля format: «one»"},
		{msg.Text(msg.CauseInfoUnknown, "prefix"), "Сведения о проекте в знании повреждены: неизвестное поле «prefix»"},
	} {
		f := projectFailure(&project.KnowledgeError{Path: "/work/shop-knowledge/gentry.yaml", Reason: project.ReasonBadFile, Cause: c.cause})
		wantFail(t, c.cause, f, lines(
			c.want,
			"",
			"Восстановите их из истории git репозитория знания: /work/shop-knowledge/gentry.yaml",
		), "")
	}
}

// TestProjectAddRefusalText checks the refusals of the arguments of project
// add.
func TestProjectAddRefusalText(t *testing.T) {
	tests := []struct {
		name string
		args []string
		cli  string
	}{
		{"no knowledge", []string{"shop"}, lines(
			"Не указан репозиторий знания.",
			"",
			"Укажите папку знания: --knowledge <путь>",
		)},
		{"invalid identifier", []string{"Shop", "--knowledge", "../k"}, lines(
			"Недопустимый идентификатор проекта: «Shop».",
			"",
			"Укажите 2–32 строчные латинские буквы, цифры и дефисы, первая — буква.",
		)},
		{"invalid prefix", []string{"shop", "--knowledge", "../k", "--prefix", "s"}, lines(
			"Недопустимое значение флага --prefix: «s».",
			"",
			"Укажите от 2 до 10 заглавных латинских букв.",
		)},
	}
	for _, tt := range tests {
		wantRefusal(t, tt.name, "project add", tt.args, tt.cli, "")
	}
}

func TestWorktreeAddText(t *testing.T) {
	added := contract.WorktreeAddOutput{Path: shopFix, Project: "shop", Action: actionAdded}
	changed := added
	changed.Agents = &contract.AgentsLayout{Worktrees: []contract.AgentsWorktree{{Path: shopFix, Added: []string{"reviewer"}}}}
	conflict := added
	conflict.Agents = &contract.AgentsLayout{Conflicts: []contract.AgentsConflict{
		{Worktree: shopFix, Agent: "reviewer", File: ".claude/agents/reviewer.md", Reason: agents.Tracked}}}
	failed := added
	failed.Agents = &contract.AgentsLayout{Error: ptr("not a directory")}
	conflicts := table(
		[]string{msg.Text(msg.ColWorktree), msg.Text(msg.ColAgent), msg.Text(msg.ColFile), msg.Text(msg.ColReason)},
		[]string{shopFix, "reviewer", ".claude/agents/reviewer.md", "файл отслеживается git проекта"},
	)
	tests := []struct {
		name       string
		out        contract.WorktreeAddOutput
		cli, agent string
	}{
		{"added", added, "Рабочая копия внесена в пул проекта shop: /work/shop-fix\n", ""},
		{"unchanged", contract.WorktreeAddOutput{Path: shopFix, Project: "shop", Action: actionUnchanged},
			"Рабочая копия уже в пуле проекта shop: /work/shop-fix\n", ""},
		{"subagents laid out", changed, lines(
			msg.Text(msg.WorktreeAdded, "shop", shopFix),
			"",
			"Субагенты в рабочей копии изменены: добавлен reviewer.",
			msg.Text(msg.AgentsNextSession),
		), ""},
		{"a conflict", conflict, lines(
			msg.Text(msg.WorktreeAdded, "shop", shopFix),
			"",
			"Часть субагентов не разложена: файлы с их именами уже есть.",
			"",
		) + conflicts + lines("", hintLineOf(msg.HintAgentsConflictWarning, "gentry agents sync")),
			lines(
				msg.Text(msg.WorktreeAdded, "shop", shopFix),
				"",
				"Часть субагентов не разложена: файлы с их именами уже есть.",
				"",
			) + conflicts + lines("", hintLineOf(msg.HintAgentsConflictWarning, "agents_sync"))},
		{"layout failed", failed, lines(
			msg.Text(msg.WorktreeAdded, "shop", shopFix),
			"",
			"Субагентов не удалось разложить.",
			"Причина: not a directory",
			"",
			hintLineOf(msg.HintAgentsSyncFailed, "gentry agents sync"),
		), lines(
			msg.Text(msg.WorktreeAdded, "shop", shopFix),
			"",
			"Субагентов не удалось разложить.",
			"Причина: not a directory",
			"",
			hintLineOf(msg.HintAgentsSyncFailed, "agents_sync"),
		)},
	}
	for _, tt := range tests {
		wantText(t, tt.name, func(p *page) { worktreeAddText(p, tt.out) }, tt.cli, tt.agent)
	}
}

func TestWorktreeListText(t *testing.T) {
	wantText(t, "no worktrees", func(p *page) {
		worktreeListText(p, contract.WorktreeListOutput{Worktrees: []contract.WorktreeListItem{}})
	},
		"Рабочих копий нет.\n", "")
	out := contract.WorktreeListOutput{Worktrees: []contract.WorktreeListItem{
		{Path: shopMain, Project: "shop", Branch: ptr("main"), Main: true, Exists: true},
		{Path: shopFix, Project: "shop", Branch: ptr("fix"), Exists: true, Task: ptr("SHOP-1")},
		{Path: "/work/shop-2", Project: "shop", Branch: ptr("two"), Exists: true},
		{Path: "/work/shop-gone", Project: "shop", Exists: false},
	}}
	wantText(t, "worktrees", func(p *page) { worktreeListText(p, out) }, table(
		[]string{msg.Text(msg.ColProject), msg.Text(msg.ColWorktree), msg.Text(msg.ColBranch), msg.Text(msg.ColState)},
		[]string{"shop", shopMain, "main", "основная, свободна"},
		[]string{"shop", shopFix, "fix", "задача SHOP-1"},
		[]string{"shop", "/work/shop-2", "two", "свободна"},
		[]string{"shop", "/work/shop-gone", "—", "папка не найдена"},
	), "")
}

func TestWorktreeFailureText(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		cli, agent string
	}{
		{"knowledge", &project.InvalidWorktreeError{Path: shopKnowledge, Project: "shop"},
			"Папка знания проекта shop не может быть рабочей копией: /work/shop-knowledge\n", ""},
		{"not a worktree", &project.NotRepoError{Path: "/work"}, msg.Text(msg.ErrNotGitRepo, "/work") + "\n", ""},
		{"no project", &project.NotFoundError{Project: "cart"}, lines(
			"Проект «cart» не подключён.",
			"",
			hintLineOf(msg.HintProjectNotFound, "gentry project list"),
		), lines(
			"Проект «cart» не подключён.",
			"",
			hintLineOf(msg.HintProjectNotFound, "project_list"),
		)},
		{"project undetermined", &project.UndeterminedError{Dir: "/work"}, lines(
			"Не удалось определить проект для папки /work",
			"",
			"Укажите --project или выполните команду в рабочей копии проекта.",
		), ""},
	}
	for _, tt := range tests {
		wantFail(t, tt.name, worktreeFailure(tt.err), tt.cli, tt.agent)
	}
}

func TestAgentsSyncText(t *testing.T) {
	missing := true
	worktrees := func(added ...string) []contract.AgentsWorktree {
		return []contract.AgentsWorktree{
			{Path: shopMain, Agents: []string{"reviewer"}, Added: added},
			{Path: shopFix, Agents: []string{"reviewer"}, Task: ptr("SHOP-1")},
			{Path: "/work/shop-gone", Missing: &missing},
		}
	}
	laid := map[string]bool{shopMain: true, shopFix: true, "/work/shop-gone": true}
	header := []string{msg.Text(msg.ColWorktree), msg.Text(msg.ColTask), msg.Text(msg.ColAgents), msg.Text(msg.ColChanges)}
	wantText(t, "in place", func(p *page) {
		agentsSyncText(p, contract.AgentsSyncOutput{Project: "shop", Worktrees: worktrees()}, laid)
	}, lines("Субагенты в рабочих копиях проекта shop соответствуют флоу.", "")+table(header,
		[]string{shopMain, "—", "reviewer", "—"},
		[]string{shopFix, "SHOP-1", "reviewer", "—"},
		[]string{"/work/shop-gone", "—", "—", "папки нет, копия пропущена"},
	), "")
	wantText(t, "laid out", func(p *page) {
		agentsSyncText(p, contract.AgentsSyncOutput{Project: "shop", Changed: true, Worktrees: worktrees("reviewer")}, laid)
	}, lines("Субагенты разложены в рабочие копии проекта shop.", msg.Text(msg.AgentsNextSession), "")+table(header,
		[]string{shopMain, "—", "reviewer", msg.Text(msg.AgentAdded, "reviewer")},
		[]string{shopFix, "SHOP-1", "reviewer", "—"},
		[]string{"/work/shop-gone", "—", "—", msg.Text(msg.AgentsWorktreeMissing)},
	), "")
}

// TestLayoutText checks the block of a layout of subagents inside another
// command: of one worktree, of the free worktrees of a project or of all.
func TestLayoutText(t *testing.T) {
	one := &contract.AgentsLayout{Worktrees: []contract.AgentsWorktree{{Path: shopFix, Updated: []string{"reviewer"}}}}
	removed := &contract.AgentsLayout{Worktrees: []contract.AgentsWorktree{{Path: shopFix, Removed: []string{"reviewer"}}}}
	free := &contract.AgentsLayout{Worktrees: []contract.AgentsWorktree{{Path: shopMain}, {Path: shopFix}}}
	tests := []struct {
		name string
		text func(p *page)
		cli  string
	}{
		{"one worktree", func(p *page) { writeLayout(p, one, true, "") }, lines(
			"",
			msg.Text(msg.AgentsWorktreeChanged, "обновлён reviewer"),
			"Изменения вступят в силу со следующей сессии агента.",
		)},
		{"one worktree, a subagent removed", func(p *page) { writeLayout(p, removed, true, "") }, lines(
			"",
			msg.Text(msg.AgentsWorktreeChanged, "удалён reviewer"),
			msg.Text(msg.AgentsNextSession),
		)},
		{"free worktrees of a project", func(p *page) { writeLayout(p, free, false, "shop") }, lines(
			"",
			"Субагенты разложены в свободные рабочие копии проекта shop.",
			msg.Text(msg.AgentsNextSession),
		)},
		{"all free worktrees", func(p *page) { writeLayout(p, free, false, "") }, lines(
			"",
			"Субагенты разложены в свободные рабочие копии.",
			msg.Text(msg.AgentsNextSession),
		)},
		{"free worktrees after a synchronization", func(p *page) {
			writeFreeLayout(p, free, map[string]string{shopMain: "shop", shopFix: "shop"})
		}, lines(
			"",
			msg.Text(msg.AgentsFreeSynced, "shop"),
			msg.Text(msg.AgentsNextSession),
		)},
	}
	for _, tt := range tests {
		wantText(t, tt.name, tt.text, tt.cli, "")
	}
}

func TestAgentsConflictText(t *testing.T) {
	f := agentsConflict([]agents.Conflict{{Worktree: shopFix, Agent: "reviewer", File: ".claude/agents/reviewer.md", Reason: agents.Foreign}})
	conflicts := table(
		[]string{msg.Text(msg.ColWorktree), msg.Text(msg.ColAgent), msg.Text(msg.ColFile), msg.Text(msg.ColReason)},
		[]string{shopFix, "reviewer", ".claude/agents/reviewer.md", "файл разложен не Gentry"},
	)
	wantFail(t, "conflict", f, lines("Субагентов нельзя разложить: файлы с их именами уже есть.", "")+conflicts+
		lines("", msg.Text(msg.HintAgentsConflict)), "")
}
