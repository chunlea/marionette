-- Reverses 009: removes the redispatch backoff state.
--
-- After this runs, a task whose dispatch failed parks as pending and waits for
-- a human, which is where it was before automatic redispatch existed.

DROP INDEX IF EXISTS idx_tasks_redispatch;

ALTER TABLE tasks
    DROP COLUMN IF EXISTS next_dispatch_after,
    DROP COLUMN IF EXISTS dispatch_attempts,
    DROP COLUMN IF EXISTS dispatch_parked_reason;
