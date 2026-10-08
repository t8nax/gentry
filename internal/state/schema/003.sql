-- Versions of a project flow: the active flow is the latest one. Rows are
-- never deleted: tasks are pinned to a version.
CREATE TABLE flow_versions (
  project TEXT    NOT NULL REFERENCES projects (id),
  version INTEGER NOT NULL,  -- 1, 2, 3 within the project
  content TEXT    NOT NULL,  -- the snapshot: a JSON object of files and library subagents
  applied TEXT    NOT NULL,  -- time of gentry flow apply, UTC
  PRIMARY KEY (project, version)
);

-- The flow draft of a project, at most one. Its files are in
-- process/<project>/flow-draft; the row says it exists and what it was made from.
CREATE TABLE flow_drafts (
  project      TEXT PRIMARY KEY REFERENCES projects (id),
  base_version INTEGER,       -- version it was made from; NULL if the project had no flow
  created      TEXT NOT NULL  -- UTC
);
