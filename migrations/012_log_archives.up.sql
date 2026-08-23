-- Teach log_archives how its object is encoded and where it stopped reading.
--
-- 001 created log_archives and nothing ever wrote a row: the archiver named in
-- roadmap 10.6 was never built. Round 3 builds it, and four facts about an
-- archive have to live in the database rather than in whatever config happened
-- to be loaded when it was written.
--
--   format tells the reader how to decode the object. The archiver writes a
--   framed container (see pkg/storage/logarchive) rather than one zstd stream,
--   so that appending to a session's archive - which happens whenever a session
--   archived while idle produces more logs - can copy the existing bytes
--   through without decrypting and re-encrypting them.
--
--   encrypted is not derivable from config. Encryption is a deployment switch,
--   and an archive written before it was turned on must still be readable
--   after. Reading the switch instead of the row is how a deployment loses the
--   logs it believes it archived.
--
--   last_log_id and last_log_sequence complete the boundary. last_log_at alone
--   is a timestamp, and log rows share timestamps: the archiver reads and
--   deletes on the exact (created_at, sequence, id) triple of the last record
--   it wrote, so that a row arriving on the same microsecond is archived by the
--   next pass rather than deleted by this one.
--
-- The partial index serves the retention coverage check: before dropping a
-- daily partition, the maintainer asks whether every row left in it is covered
-- by a live archive (see DropArchivedLogPartitions). That join is
-- session_id -> last_log_at over non-deleted archives, once per partition.

ALTER TABLE log_archives
    ADD COLUMN IF NOT EXISTS format TEXT NOT NULL DEFAULT 'ndjson+zstd/frames1';

ALTER TABLE log_archives
    ADD COLUMN IF NOT EXISTS encrypted BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE log_archives
    ADD COLUMN IF NOT EXISTS last_log_id TEXT;

ALTER TABLE log_archives
    ADD COLUMN IF NOT EXISTS last_log_sequence BIGINT;

CREATE INDEX IF NOT EXISTS idx_log_archives_session_coverage
    ON log_archives(session_id, last_log_at)
    WHERE deleted_at IS NULL;

-- Archive expiry sweeps in two phases. The existing idx_log_archives_expires
-- covers phase one (still live, past expires_at); this covers the purge phase,
-- which looks for rows already soft-deleted whose blob may still be there.
CREATE INDEX IF NOT EXISTS idx_log_archives_purge
    ON log_archives(deleted_at)
    WHERE deleted_at IS NOT NULL;
