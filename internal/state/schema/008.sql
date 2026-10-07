-- Closing and cancelling of tasks (R128–R130). A cancelled task taken anew
-- is a new row with the same number and the next attempt; the path, steps,
-- notes, artifacts and decisions of the operator of each attempt stay with
-- its row. Tasks under way are attempt 1.
ALTER TABLE tasks ADD COLUMN attempt INTEGER NOT NULL DEFAULT 1;  -- 1, 2, … within the number
ALTER TABLE tasks ADD COLUMN ended TEXT;          -- UTC, the closing or the cancelling; NULL while in work
ALTER TABLE tasks ADD COLUMN ended_source TEXT;   -- who closed or cancelled it: operator | agent
ALTER TABLE tasks ADD COLUMN cancel_reason TEXT;  -- NULL if none was given

DROP INDEX tasks_number;
CREATE UNIQUE INDEX tasks_number ON tasks (project, number, attempt);
