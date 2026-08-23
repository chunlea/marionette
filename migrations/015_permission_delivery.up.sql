-- Record when a permission response actually reached a runner.
--
-- Responding to a permission request updates the row and then sends
-- ApprovePermission to the runner. The send is treated as non-fatal on the
-- grounds that the runner will get the answer later, and until now the only
-- replay path was the pending_permissions list on AttachSession, filtered by
--
--     responded_at > sessions.suspended_at
--
-- A response lost while the session was ACTIVE was recorded before the
-- suspend, so it failed that predicate and was never replayed. The agent stays
-- blocked on the gate, the task makes no progress, and roughly thirty minutes
-- later (suspend_after_seconds) the permission timeout enforcer suspends the
-- session - after which the task re-executes from the beginning and asks for
-- the same permission again. Work already paid for is thrown away.
--
-- The suspend timestamp was never the right key. What the replay wants to know
-- is whether the runner has seen this answer, and that is a fact about the
-- answer, not about the session's history. delivered_at records it: NULL means
-- undelivered and eligible for replay, set means the runner has it.
--
-- Cross-replica routing makes this newly likely - a response sent from the
-- wrong replica could not reach the runner at all - but the bug does not need
-- more than one replica: a full command queue or a stream torn down mid-flight
-- does the same thing.

ALTER TABLE permission_requests
    ADD COLUMN IF NOT EXISTS delivered_at TIMESTAMPTZ;

-- The replay predicate: answered, not yet delivered. Partial, because a
-- delivered response is never looked up this way again.
CREATE INDEX IF NOT EXISTS idx_permission_requests_undelivered
    ON permission_requests(session_id)
    WHERE responded_at IS NOT NULL AND delivered_at IS NULL;
