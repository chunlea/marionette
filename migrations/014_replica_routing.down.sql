-- Reverse 014.
--
-- Dropping the registry returns command routing to being correct only within a
-- single process: a replica that does not hold the runner's stream reports
-- "runner not connected" again, and the deployment is single-replica only.

DROP INDEX IF EXISTS idx_runners_connected_replica;

ALTER TABLE runners DROP COLUMN IF EXISTS connected_at;
ALTER TABLE runners DROP COLUMN IF EXISTS connected_replica_id;

DROP INDEX IF EXISTS idx_server_replicas_heartbeat;
DROP TABLE IF EXISTS server_replicas;
