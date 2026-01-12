-- Migration: Android Streaming Extensions
-- Extends the base streams table with Android-specific columns

-- Add Android-specific columns to streams table
ALTER TABLE streams ADD COLUMN IF NOT EXISTS device_serial TEXT;
ALTER TABLE streams ADD COLUMN IF NOT EXISTS device_name TEXT;
ALTER TABLE streams ADD COLUMN IF NOT EXISTS android_version TEXT;

-- Index for finding streams by device
CREATE INDEX IF NOT EXISTS idx_streams_device ON streams(device_serial)
    WHERE type = 'android';

-- Comment on new columns
COMMENT ON COLUMN streams.device_serial IS 'Android device serial number (e.g., emulator-5554 or USB serial)';
COMMENT ON COLUMN streams.device_name IS 'Android device model name';
COMMENT ON COLUMN streams.android_version IS 'Android SDK version';
