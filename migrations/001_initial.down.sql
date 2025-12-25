-- Marionette Database Schema
-- Migration: 001_initial (DOWN)
-- Description: Drop all tables and functions

-- Drop functions first
DROP FUNCTION IF EXISTS drop_old_log_partitions(INT);
DROP FUNCTION IF EXISTS maintain_log_partitions(INT);
DROP FUNCTION IF EXISTS create_log_partition(DATE);

-- Drop tables in reverse dependency order
DROP TABLE IF EXISTS tunnels;
DROP TABLE IF EXISTS action_logs;
DROP TABLE IF EXISTS snapshots;
DROP TABLE IF EXISTS log_archives;
DROP TABLE IF EXISTS logs;
DROP TABLE IF EXISTS data_keys;
DROP TABLE IF EXISTS manifests;
DROP TABLE IF EXISTS chunks;
DROP TABLE IF EXISTS permission_requests;
DROP TABLE IF EXISTS scheduled_tasks;
DROP TABLE IF EXISTS task_runs;
DROP TABLE IF EXISTS tasks;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS workspaces;
DROP TABLE IF EXISTS runner_tokens;
DROP TABLE IF EXISTS api_keys;
DROP TABLE IF EXISTS runners;
DROP TABLE IF EXISTS profiles;
DROP TABLE IF EXISTS agent_configs;
DROP TABLE IF EXISTS provider_configs;
