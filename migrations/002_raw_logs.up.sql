-- Migration: Add raw_logs table for binary log content preservation
-- This table stores raw log output from agent execution with exact byte preservation.

-- raw_logs: Binary content preservation (replaces logs for new data)
-- Note: Foreign keys omitted for partitioned table compatibility
-- Referential integrity enforced at application layer
CREATE TABLE raw_logs (
    id TEXT NOT NULL,  -- rlog_xxx
    session_id TEXT NOT NULL,
    conversation_id TEXT,  -- Phase 4: nullable for now
    task_id TEXT NOT NULL,
    run_id TEXT NOT NULL,
    runner_id TEXT NOT NULL,
    stream TEXT NOT NULL DEFAULT 'stdout',  -- stdout, stderr, json
    content BYTEA NOT NULL,  -- Raw bytes (not TEXT)
    sequence BIGINT NOT NULL,
    tenant_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);

-- Indexes for common query patterns
CREATE UNIQUE INDEX idx_raw_logs_run_seq_unique ON raw_logs(run_id, sequence, created_at);
CREATE INDEX idx_raw_logs_task_seq ON raw_logs(task_id, sequence);
CREATE INDEX idx_raw_logs_session ON raw_logs(session_id, created_at);
CREATE INDEX idx_raw_logs_tenant ON raw_logs(tenant_id, created_at);
CREATE INDEX idx_raw_logs_conversation ON raw_logs(conversation_id, created_at)
    WHERE conversation_id IS NOT NULL;

-- Partition management functions (reuse from logs table pattern)

-- Function to create a partition for a specific date
CREATE OR REPLACE FUNCTION create_raw_log_partition(partition_date DATE)
RETURNS void AS $$
DECLARE
    partition_name TEXT;
    start_date DATE;
    end_date DATE;
BEGIN
    partition_name := 'raw_logs_' || TO_CHAR(partition_date, 'YYYYMMDD');
    start_date := partition_date;
    end_date := partition_date + INTERVAL '1 day';

    -- Check if partition already exists
    IF NOT EXISTS (
        SELECT 1 FROM pg_class c
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE c.relname = partition_name AND n.nspname = 'public'
    ) THEN
        EXECUTE format(
            'CREATE TABLE %I PARTITION OF raw_logs FOR VALUES FROM (%L) TO (%L)',
            partition_name, start_date, end_date
        );
        RAISE NOTICE 'Created partition: %', partition_name;
    END IF;
END;
$$ LANGUAGE plpgsql;

-- Function to ensure partitions exist for upcoming days
CREATE OR REPLACE FUNCTION maintain_raw_log_partitions(days_ahead INT DEFAULT 7)
RETURNS void AS $$
DECLARE
    target_date DATE;
BEGIN
    FOR i IN 0..days_ahead LOOP
        target_date := CURRENT_DATE + (i || ' days')::INTERVAL;
        PERFORM create_raw_log_partition(target_date);
    END LOOP;
END;
$$ LANGUAGE plpgsql;

-- Function to drop old partitions (beyond retention period)
CREATE OR REPLACE FUNCTION drop_old_raw_log_partitions(retention_days INT DEFAULT 7)
RETURNS void AS $$
DECLARE
    partition_record RECORD;
    cutoff_date DATE;
    partition_date DATE;
BEGIN
    cutoff_date := CURRENT_DATE - (retention_days || ' days')::INTERVAL;

    FOR partition_record IN
        SELECT c.relname as partition_name
        FROM pg_class c
        JOIN pg_inherits i ON c.oid = i.inhrelid
        JOIN pg_class p ON i.inhparent = p.oid
        WHERE p.relname = 'raw_logs'
    LOOP
        -- Extract date from partition name (raw_logs_YYYYMMDD)
        BEGIN
            partition_date := TO_DATE(
                SUBSTRING(partition_record.partition_name FROM 'raw_logs_(\d{8})'),
                'YYYYMMDD'
            );

            IF partition_date < cutoff_date THEN
                EXECUTE format('DROP TABLE IF EXISTS %I', partition_record.partition_name);
                RAISE NOTICE 'Dropped partition: %', partition_record.partition_name;
            END IF;
        EXCEPTION WHEN OTHERS THEN
            -- Skip partitions with unexpected naming
            CONTINUE;
        END;
    END LOOP;
END;
$$ LANGUAGE plpgsql;

-- Initialize partitions for current week + 7 days ahead
SELECT maintain_raw_log_partitions(7);
