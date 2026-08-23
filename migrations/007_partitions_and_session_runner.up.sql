-- Log partition safety net + "one active session per runner" constraint
--
-- Two latent failures this migration closes:
--
--  1. 001_initial called maintain_log_partitions(7) exactly once and nothing
--     ever calls it again. `logs` is PARTITION BY RANGE (created_at) with no
--     DEFAULT partition, so 8 days after that migration every INSERT into logs
--     fails with "no partition of relation \"logs\" found for row". Existing
--     deployments are already in this state.
--
--  2. sessions(runner_id) is a plain index, so nothing stops two active
--     sessions from pointing at the same runner.
--
-- The DEFAULT partition alone is not enough: once rows land in it, creating the
-- matching daily partition fails ("updated partition constraint for default
-- partition would be violated by some row"), which would break partition
-- maintenance permanently. create_log_partition is therefore replaced with a
-- version that drains the default partition before attaching. The recurring
-- caller is pkg/jobs.PartitionMaintainer.

--------------------------------------------------------------------------------
-- 0. Preflight: refuse to run on data the new constraint cannot describe
--------------------------------------------------------------------------------
--
-- This runs before any DDL so a dirty database is left untouched and the
-- migration can simply be re-run once the data is fixed. It also beats letting
-- CREATE UNIQUE INDEX report one anonymous duplicate: every offending runner is
-- listed so the operator can resolve them.

DO $$
DECLARE
    offenders TEXT;
    offender_count INT;
BEGIN
    SELECT count(*), string_agg(format('runner %s -> sessions %s', runner_id, session_ids), E'\n')
    INTO offender_count, offenders
    FROM (
        SELECT runner_id, string_agg(id, ', ' ORDER BY created_at) AS session_ids
        FROM sessions
        WHERE runner_id IS NOT NULL AND status = 'active'
        GROUP BY runner_id
        HAVING count(*) > 1
    ) dupes;

    IF offender_count > 0 THEN
        RAISE EXCEPTION
            'cannot create idx_sessions_active_runner: % runner(s) have more than one active session', offender_count
            USING DETAIL = offenders,
                  HINT = 'A runner can host at most one active session. Suspend or terminate the extra sessions '
                         '(UPDATE sessions SET status = ''suspended'', runner_id = NULL WHERE id = ...), '
                         'then re-run this migration.';
    END IF;
END $$;

--------------------------------------------------------------------------------
-- 1. Default-partition-aware partition creation
--------------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION create_log_partition(partition_date DATE)
RETURNS void AS $$
DECLARE
    partition_name TEXT;
    start_date DATE;
    end_date DATE;
    stranded BIGINT := 0;
BEGIN
    partition_name := 'logs_' || TO_CHAR(partition_date, 'YYYYMMDD');
    start_date := partition_date;
    end_date := partition_date + INTERVAL '1 day';

    IF to_regclass('public.' || quote_ident(partition_name)) IS NOT NULL THEN
        RETURN;
    END IF;

    -- Rows for this day may already sit in the default partition (maintenance
    -- was not running). Count them before deciding how to create the partition.
    IF to_regclass('public.logs_default') IS NOT NULL THEN
        EXECUTE format(
            'SELECT count(*) FROM logs_default WHERE created_at >= %L AND created_at < %L',
            start_date, end_date
        ) INTO stranded;
    END IF;

    IF stranded = 0 THEN
        EXECUTE format(
            'CREATE TABLE %I PARTITION OF logs FOR VALUES FROM (%L) TO (%L)',
            partition_name, start_date, end_date
        );
    ELSE
        -- Attaching on top of those rows would fail, so build the table
        -- detached, move the rows into it, then attach.
        EXECUTE format(
            'CREATE TABLE %I (LIKE logs INCLUDING DEFAULTS INCLUDING CONSTRAINTS)',
            partition_name
        );
        EXECUTE format(
            'WITH moved AS (' ||
            '  DELETE FROM logs_default WHERE created_at >= %L AND created_at < %L RETURNING *' ||
            ') INSERT INTO %I SELECT * FROM moved',
            start_date, end_date, partition_name
        );
        EXECUTE format(
            'ALTER TABLE logs ATTACH PARTITION %I FOR VALUES FROM (%L) TO (%L)',
            partition_name, start_date, end_date
        );
        RAISE NOTICE 'Drained % rows from logs_default into %', stranded, partition_name;
    END IF;

    RAISE NOTICE 'Created partition: %', partition_name;
END;
$$ LANGUAGE plpgsql;

-- The default partition has no date in its name, so the regex below never
-- matches it, but be explicit: it must never be dropped by retention.
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
        IF partition_record.partition_name = 'logs_default' THEN
            CONTINUE;
        END IF;

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
            -- Skip partitions with unexpected naming
            CONTINUE;
        END;
    END LOOP;
END;
$$ LANGUAGE plpgsql;

--------------------------------------------------------------------------------
-- 2. Default partition + top up the daily partitions
--------------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS logs_default PARTITION OF logs DEFAULT;

SELECT maintain_log_partitions(7);

--------------------------------------------------------------------------------
-- 3. One active session per runner
--------------------------------------------------------------------------------

CREATE UNIQUE INDEX IF NOT EXISTS idx_sessions_active_runner ON sessions(runner_id)
    WHERE runner_id IS NOT NULL AND status = 'active';
