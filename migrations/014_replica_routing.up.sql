-- Record which server process is holding each runner's control stream.
--
-- A command to a runner is written to the bidirectional gRPC stream the runner
-- opened, and that stream terminates in exactly one process. The connection map
-- that resolves runner -> stream is in that process's memory, so a second
-- replica asked to send the same command finds nothing and reports "runner not
-- connected". With the shipped production overlay at three replicas, roughly
-- two out of three ExecuteTask commands could not reach their runner.
--
-- Every other cross-process decision in this schema is already arbitrated here:
-- the dispatch CAS on tasks.status, the runner claim in 013, the cron tick
-- precondition, the webhook SKIP LOCKED lease. This is one more of the same
-- kind - the database says who holds the connection, and a replica that is not
-- the holder forwards the command to the one that is.
--
-- WHY A TABLE FOR REPLICAS RATHER THAN A COLUMN ON runners
--
-- The sender needs two things: which process, and where to reach it. Putting
-- the address on every runner row would repeat it per connection and leave no
-- place for a liveness heartbeat. A replica row carries the address once and
-- gives the heartbeat somewhere to live, which is what turns a routing pointer
-- into a lease that expires instead of a claim that strands.
--
-- The id is generated fresh at every process start and never persisted to
-- disk. A restarted process holds no streams; inheriting its predecessor's
-- pointers would route commands into a connection map that is empty.
--
-- WHY NO tenant_id AND NO ROW LEVEL SECURITY HERE
--
-- A replica is deployment infrastructure, not tenant data: it serves every
-- tenant's runners and belongs to none of them, exactly like the background
-- jobs that migration 011 had to grant system access to. There is nothing in
-- the row to isolate - an id, a host:port and two timestamps - and giving it a
-- policy would only mean the routing lookup could not read it.
--
-- runners.connected_replica_id is on a table that DOES have RLS, so the lookup
-- runs with system access (store.WithSystemAccess), for the same reason the
-- reaper does: it acts for the deployment and has no request to take a tenant
-- from.

CREATE TABLE IF NOT EXISTS server_replicas (
    id                TEXT PRIMARY KEY,
    -- host:port a peer dials to hand this process a command. Defaults to the
    -- gRPC listener; a deployment where that guess is wrong overrides it.
    advertise_addr    TEXT NOT NULL,
    version           TEXT,
    started_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_heartbeat_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- The reaper's predicate, and the operator question "which replicas are up".
CREATE INDEX IF NOT EXISTS idx_server_replicas_heartbeat
    ON server_replicas(last_heartbeat_at);

-- ON DELETE SET NULL is load-bearing: reaping one dead replica clears every
-- routing pointer it owned, atomically, with no second sweep to get wrong or
-- to race against a runner reconnecting elsewhere.
--
-- Referential integrity actions bypass row level security even under FORCE ROW
-- LEVEL SECURITY, so this clears rows belonging to every tenant - which is the
-- required behaviour, since a dead replica held connections for all of them.
ALTER TABLE runners
    ADD COLUMN IF NOT EXISTS connected_replica_id TEXT
        REFERENCES server_replicas(id) ON DELETE SET NULL;

ALTER TABLE runners
    ADD COLUMN IF NOT EXISTS connected_at TIMESTAMPTZ;

-- Only connected rows are interesting: the index answers "what is this replica
-- holding" for the reaper and for operators, and stays small because a pointer
-- exists only for the life of a stream.
CREATE INDEX IF NOT EXISTS idx_runners_connected_replica
    ON runners(connected_replica_id) WHERE connected_replica_id IS NOT NULL;
