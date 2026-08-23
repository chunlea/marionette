-- Reverses 010. After this runs, a synced workspace cannot be located again and
-- a resumed session starts from an empty workspace.

ALTER TABLE sessions
    DROP COLUMN IF EXISTS workspace_manifest_id;
