-- Decisions of the operator (R127): the words of the operator given in
-- conversation, with the question they answer. Tasks under way get none.

-- Decisions of the operator: words given in conversation, added only.
CREATE TABLE operator_decisions (
  task         INTEGER NOT NULL REFERENCES tasks (id),
  number       INTEGER NOT NULL,   -- 1, 2, … within the task
  question     TEXT,               -- the question answered; NULL for words given without one
  options      TEXT,               -- JSON array of {label, description, recommended}; NULL without options
  answer       TEXT NOT NULL,      -- the words of the operator as given
  pass         INTEGER NOT NULL REFERENCES task_path (id),  -- the stage it was recorded at
  allow_return TEXT,               -- one more return from the node of pass to this node
  source       TEXT NOT NULL,      -- operator | agent
  recorded     TEXT NOT NULL,      -- UTC
  PRIMARY KEY (task, number)
);
