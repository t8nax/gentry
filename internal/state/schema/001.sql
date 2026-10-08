-- Service values: schema_version.
CREATE TABLE meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

-- The event journal. seq is an alias of rowid without AUTOINCREMENT: writers
-- are serialized and events are never deleted, so numbers have no gaps.
CREATE TABLE events (
  seq     INTEGER PRIMARY KEY,
  time    TEXT    NOT NULL,  -- UTC, RFC 3339 with milliseconds
  project TEXT,              -- NULL for an event without a project
  task    TEXT,              -- NULL for an event without a task
  type    TEXT    NOT NULL,  -- object.action in the past tense
  data    TEXT    NOT NULL   -- a JSON object
);
