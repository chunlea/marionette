-- Backoff state for automatic task redispatch.
--
-- A dispatch that never reached a runner parks the task as pending with its
-- retry budget intact. Until now nothing ever looked at it again: ErrNoRunnerAttached
-- was a dead end and a human had to poke the task. Automatic redispatch needs
-- somewhere to remember how hard it has already tried.
--
-- These live on the row rather than in memory on purpose. In-memory backoff
-- resets for every task at once when the server restarts, which turns a restart
-- into a stampede against whatever made the dispatches fail in the first place.
--
-- SEPARATE FROM max_retries. retry_count is the user-facing budget for AGENT
-- failures, and unwindDispatch deliberately refunds it because a send failure is
-- not the agent's fault. These columns are the infrastructure budget, and the
-- two must not be able to exhaust each other.

ALTER TABLE tasks
    -- Earliest time a redispatch may be attempted. NULL means "no backoff
    -- pending": a task that has never failed to dispatch is eligible now.
    ADD COLUMN next_dispatch_after TIMESTAMPTZ,

    -- Consecutive failed dispatch attempts. Reset to zero the moment a dispatch
    -- reaches a runner.
    ADD COLUMN dispatch_attempts INT NOT NULL DEFAULT 0,

    -- Why automatic redispatch gave up, once it has. A task with this set is in
    -- exactly the state tasks were in before this feature: pending, waiting for
    -- a human. It is recorded so the human knows what happened.
    ADD COLUMN dispatch_parked_reason TEXT;

-- The sweeper's query: pending tasks whose backoff has expired and which have
-- not been parked. Partial, because the interesting set is a tiny slice of a
-- table that is mostly completed work.
CREATE INDEX idx_tasks_redispatch ON tasks (next_dispatch_after)
    WHERE status = 'pending' AND dispatch_parked_reason IS NULL;
