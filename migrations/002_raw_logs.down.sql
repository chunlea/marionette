-- Down migration: Remove raw_logs table and related functions

-- Drop partition management functions
DROP FUNCTION IF EXISTS drop_old_raw_log_partitions(INT);
DROP FUNCTION IF EXISTS maintain_raw_log_partitions(INT);
DROP FUNCTION IF EXISTS create_raw_log_partition(DATE);

-- Drop the partitioned table (this drops all partitions too)
DROP TABLE IF EXISTS raw_logs CASCADE;
