-- Give runner allocation a claim the database arbitrates.
--
-- Picking a runner for a session was two steps: list the idle runners, then ask
-- whether any live session already owns one. Between those steps the runner
-- still looks free, so two callers both take it. Inside one process an
-- in-memory reservation closed the window; across processes nothing did, and
-- multi-replica is now plausible.
--
-- The partial unique index on sessions(runner_id) WHERE status='active' is not
-- enough on its own. Activate detaches any other session holding the runner
-- before it writes, so two concurrent activations do not both end up active -
-- the later one detaches the earlier and takes the runner, and the session that
-- believed it was running finds itself suspended with a task in flight. The
-- index only catches the interleaving where both writes land together.
--
-- A claim on the runner row fixes it at the right level: one conditional UPDATE
-- decides the winner, and the loser sees "not mine" and picks another runner
-- instead of erroring.
--
-- Why not overload runners.status. Status is the runner's own report of what it
-- is doing, written by its heartbeat, and validated against a transition table.
-- A server-side claim written into it would be overwritten by the next
-- heartbeat and would corrupt the meaning of a field the agent owns.
--
-- claimed_at is what keeps a claim from becoming permanent: a process that dies
-- between claiming and activating would otherwise strand the runner forever, so
-- claims are leases and an expired one can be taken over.

ALTER TABLE runners
    ADD COLUMN IF NOT EXISTS claim_session_id TEXT;

ALTER TABLE runners
    ADD COLUMN IF NOT EXISTS claimed_at TIMESTAMPTZ;

-- Only claimed rows are interesting: the index serves "is this claim still
-- live" and operator questions about stuck claims, and stays tiny because a
-- claim is held for the length of one allocation.
CREATE INDEX IF NOT EXISTS idx_runners_claim ON runners(claim_session_id, claimed_at)
    WHERE claim_session_id IS NOT NULL;
