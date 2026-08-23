-- Reverse of 007_partitions_and_session_runner.up.sql
--
-- Note: rows that landed in logs_default are moved back into daily partitions
-- where one exists; anything left has nowhere to go once the default partition
-- is dropped, so this migration fails loudly instead of discarding logs.

DROP INDEX IF EXISTS idx_sessions_active_runner;

DO $$
DECLARE
    stranded BIGINT;
BEGIN
    IF to_regclass('public.logs_default') IS NULL THEN
        RETURN;
    END IF;

    SELECT count(*) INTO stranded FROM logs_default;
    IF stranded > 0 THEN
        RAISE EXCEPTION 'logs_default still holds % row(s)', stranded
            USING HINT = 'Run SELECT maintain_log_partitions(0) (or create the matching daily '
                         'partitions) to drain logs_default before rolling this migration back.';
    END IF;

    ALTER TABLE logs DETACH PARTITION logs_default;
    DROP TABLE logs_default;
END $$;

-- Restore the original (default-partition-unaware) implementations.
CREATE OR REPLACE FUNCTION create_log_partition(partition_date DATE)
RETURNS void AS $$
DECLARE
    partition_name TEXT;
    start_date DATE;
    end_date DATE;
BEGIN
    partition_name := 'logs_' || TO_CHAR(partition_date, 'YYYYMMDD');
    start_date := partition_date;
    end_date := partition_date + INTERVAL '1 day';

    IF NOT EXISTS (
        SELECT 1 FROM pg_class c
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE c.relname = partition_name AND n.nspname = 'public'
    ) THEN
        EXECUTE format(
            'CREATE TABLE %I PARTITION OF logs FOR VALUES FROM (%L) TO (%L)',
            partition_name, start_date, end_date
        );
        RAISE NOTICE 'Created partition: %', partition_name;
    END IF;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION drop_old_log_partitions(retention_days INT DEFAULT 7)
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
        WHERE p.relname = 'logs'
    LOOP
        BEGIN
            partition_date := TO_DATE(
                SUBSTRING(partition_record.partition_name FROM 'logs_(\d{8})'),
                'YYYYMMDD'
            );

            IF partition_date < cutoff_date THEN
                EXECUTE format('DROP TABLE IF EXISTS %I', partition_record.partition_name);
                RAISE NOTICE 'Dropped partition: %', partition_record.partition_name;
            END IF;
        EXCEPTION WHEN OTHERS THEN
            CONTINUE;
        END;
    END LOOP;
END;
$$ LANGUAGE plpgsql;
