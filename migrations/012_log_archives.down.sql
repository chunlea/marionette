-- Reverse 012.
--
-- Dropping these columns destroys no archive object, but it does destroy the
-- only record of how to decode one: afterwards an encrypted archive is
-- indistinguishable from a plaintext one, and the append boundary is gone, so
-- the next archiver pass would re-archive rows the object already holds.
-- Re-applying 012 restores the columns at their defaults, which is correct only
-- for a deployment that never enabled encryption and has no archives yet.

DROP INDEX IF EXISTS idx_log_archives_purge;
DROP INDEX IF EXISTS idx_log_archives_session_coverage;

ALTER TABLE log_archives DROP COLUMN IF EXISTS last_log_sequence;
ALTER TABLE log_archives DROP COLUMN IF EXISTS last_log_id;
ALTER TABLE log_archives DROP COLUMN IF EXISTS encrypted;
ALTER TABLE log_archives DROP COLUMN IF EXISTS format;
