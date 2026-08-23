-- Remember which CAS snapshot a session's workspace was last synced to.
--
-- A runner that suspends a session syncs its workspace into content-addressed
-- storage and reports the manifest id it produced. That runner then goes away.
-- Without somewhere to keep the id, a synced workspace could never be restored:
-- the only thing that knew where the snapshot lived has been destroyed, which
-- made workspace sync a write-only feature.
--
-- Distinct from suspend_snapshot_id, which is a PROVIDER snapshot (a paused VM
-- or container image) and belongs to a different layer with a different
-- lifetime. Overloading one column for both would make "restore" ambiguous.

ALTER TABLE sessions
    ADD COLUMN workspace_manifest_id TEXT;
