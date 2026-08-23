-- Reverse 015.
--
-- Without delivered_at the replay falls back to keying on suspended_at, which
-- silently drops any response that was lost while the session was active.

DROP INDEX IF EXISTS idx_permission_requests_undelivered;

ALTER TABLE permission_requests DROP COLUMN IF EXISTS delivered_at;
