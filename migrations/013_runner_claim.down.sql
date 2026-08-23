-- Reverse 013.
--
-- Dropping the claim columns returns runner allocation to being safe only
-- within a single process: two replicas can then pick the same idle runner
-- again, and the loser's activation detaches the winner's session rather than
-- losing a race cleanly.

DROP INDEX IF EXISTS idx_runners_claim;

ALTER TABLE runners DROP COLUMN IF EXISTS claimed_at;
ALTER TABLE runners DROP COLUMN IF EXISTS claim_session_id;
