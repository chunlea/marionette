-- Migration: 005_session_profile (rollback)
-- Description: Remove profile_id from sessions table

DROP INDEX IF EXISTS idx_sessions_profile;
ALTER TABLE sessions DROP COLUMN IF EXISTS profile_id;
