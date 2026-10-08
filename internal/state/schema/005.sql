-- The series of numbers of a project: tasks without a tracker record and,
-- from stage 8, records of the built-in tracker (R121). Numbers are never reused.
ALTER TABLE projects ADD COLUMN next_number INTEGER NOT NULL DEFAULT 1;

-- Flows tasks are pinned to: one snapshot per applied variant of a project
-- flow, shared by the tasks taken with it.
CREATE TABLE flow_snapshots (
  project     TEXT NOT NULL REFERENCES projects (id),
  commit_hash TEXT NOT NULL,  -- commit of the process repository that applied the flow
  applied     TEXT NOT NULL,  -- time of that commit, UTC
  content     TEXT NOT NULL,  -- JSON: files of the flow and copies of the library subagents it names
  PRIMARY KEY (project, commit_hash)
);

CREATE TABLE tasks (
  id          INTEGER PRIMARY KEY,
  project     TEXT NOT NULL REFERENCES projects (id),
  number      INTEGER NOT NULL,  -- SHOP-1 is number 1 of the project with prefix SHOP
  title       TEXT NOT NULL,     -- one line, up to 80 characters
  statement   TEXT NOT NULL,     -- as passed, not changed by Gentry
  source      TEXT NOT NULL,     -- who passed title and statement: operator | agent
  state       TEXT NOT NULL,     -- active | waiting | closed | cancelled
  scenario    TEXT NOT NULL,     -- scenario identifier in the snapshot
  node        TEXT NOT NULL,     -- current node of the scenario; part 10 moves it
  flow_commit TEXT NOT NULL,     -- the snapshot: flow_snapshots (project, commit_hash)
  worktree    TEXT REFERENCES worktrees (path),  -- NULL once the task releases it
  taken       TEXT NOT NULL,     -- UTC
  FOREIGN KEY (project, flow_commit) REFERENCES flow_snapshots (project, commit_hash)
);

-- A worktree holds at most one task.
CREATE UNIQUE INDEX tasks_worktree ON tasks (worktree) WHERE worktree IS NOT NULL;
CREATE INDEX tasks_number ON tasks (project, number);
