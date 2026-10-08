-- Connected projects. The prefix is personal: the knowledge does not hold it.
CREATE TABLE projects (
  id        TEXT PRIMARY KEY,      -- identifier shared by the team: shop
  prefix    TEXT NOT NULL UNIQUE,  -- prefix of task numbers: SHOP
  knowledge TEXT NOT NULL UNIQUE,  -- absolute path of the knowledge repository
  added     TEXT NOT NULL          -- connection time, UTC
);

-- The pool of worktrees. The branch is not stored: it is read from git.
CREATE TABLE worktrees (
  path    TEXT PRIMARY KEY,                  -- absolute path of the worktree root
  project TEXT NOT NULL REFERENCES projects (id),
  main    INTEGER NOT NULL DEFAULT 0,        -- 1 for the main worktree of the project
  added   TEXT NOT NULL                      -- time it was added, UTC
);

-- A project has one main worktree.
CREATE UNIQUE INDEX worktrees_main ON worktrees (project) WHERE main = 1;
