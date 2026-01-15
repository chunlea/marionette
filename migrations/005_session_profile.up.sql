-- Migration: 005_session_profile
-- Description: Add profile_id to sessions table for profile integration

-- Add profile_id column to sessions table
ALTER TABLE sessions ADD COLUMN profile_id TEXT REFERENCES profiles(id) ON DELETE SET NULL;

-- Create index for profile lookups
CREATE INDEX idx_sessions_profile ON sessions(profile_id) WHERE profile_id IS NOT NULL;
