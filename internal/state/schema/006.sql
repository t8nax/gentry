-- The way of a task through its scenario (R124-R126). A task that is under
-- way gets the pass of its current node in Go, after this script: the stage
-- of the node is read from the snapshot of its flow.

-- A pass of a task through a node: one row per entry, closed by an exit or a skip.
CREATE TABLE task_path (
  id         INTEGER PRIMARY KEY,
  task       INTEGER NOT NULL REFERENCES tasks (id),
  node       TEXT NOT NULL,
  stage      TEXT NOT NULL,
  round      INTEGER NOT NULL,   -- pass number of the node in the task, from 1
  entered    TEXT NOT NULL,      -- UTC
  outcome    TEXT,               -- NULL while current; exit | skip
  exit_kind  TEXT,               -- artifact | result | negative
  exit_text  TEXT,
  artifact   TEXT,               -- name of the artifact of an exit of kind artifact
  next       TEXT,               -- node taken, or finish
  reason     TEXT,               -- why that transition; why the skip
  source     TEXT,               -- operator | agent: who closed the pass
  closed     TEXT                -- UTC
);
-- A task has one current pass.
CREATE UNIQUE INDEX task_path_current ON task_path (task) WHERE outcome IS NULL;
CREATE INDEX task_path_task ON task_path (task, id);

-- The steps of a pass, in the order added.
CREATE TABLE steps (
  pass          INTEGER NOT NULL REFERENCES task_path (id),
  number        INTEGER NOT NULL,  -- 1, 2, … within the pass
  title         TEXT NOT NULL,     -- one line, up to 120 characters
  state         TEXT NOT NULL,     -- planned | done | dropped
  check_text    TEXT,              -- how a done step was checked
  reason        TEXT,              -- why a step was dropped
  source        TEXT NOT NULL,     -- who added it
  added         TEXT NOT NULL,
  closed_source TEXT,              -- who marked it done or dropped it
  closed        TEXT,
  PRIMARY KEY (pass, number)
);

-- Notes of a task: added only.
CREATE TABLE notes (
  task    INTEGER NOT NULL REFERENCES tasks (id),
  number  INTEGER NOT NULL,        -- 1, 2, … within the task
  text    TEXT NOT NULL,
  pass    INTEGER NOT NULL REFERENCES task_path (id),  -- the stage it was added at
  source  TEXT NOT NULL,
  added   TEXT NOT NULL,
  PRIMARY KEY (task, number)
);

-- Artifacts of a task: a file copied to the state directory or a link.
CREATE TABLE artifacts (
  task    INTEGER NOT NULL REFERENCES tasks (id),
  name    TEXT NOT NULL,
  kind    TEXT NOT NULL,           -- file | link
  url     TEXT,                    -- the address of a link
  source  TEXT NOT NULL,
  saved   TEXT NOT NULL,
  PRIMARY KEY (task, name)
);
