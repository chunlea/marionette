-- Migration: Revert Android Streaming Extensions

-- Drop index first
DROP INDEX IF EXISTS idx_streams_device;

-- Remove Android-specific columns
ALTER TABLE streams DROP COLUMN IF EXISTS device_serial;
ALTER TABLE streams DROP COLUMN IF EXISTS device_name;
ALTER TABLE streams DROP COLUMN IF EXISTS android_version;
