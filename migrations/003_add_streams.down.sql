-- Remove streams table

DROP INDEX IF EXISTS idx_streams_expires;
DROP INDEX IF EXISTS idx_streams_session_active;
DROP INDEX IF EXISTS idx_streams_type;
DROP INDEX IF EXISTS idx_streams_state;
DROP INDEX IF EXISTS idx_streams_tenant;
DROP INDEX IF EXISTS idx_streams_runner;
DROP INDEX IF EXISTS idx_streams_session;
DROP TABLE IF EXISTS streams;
