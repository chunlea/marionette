-- Let the deployment itself cross tenant boundaries.
--
-- Migration 008 made every policy answer one question: which tenant is this
-- statement for? That is right for anything reached by an API key, and wrong
-- for the two things that are not.
--
--   The admin API is the operator console. It has one credential, no tenant,
--   and its whole purpose is to see and act across the deployment: mint keys
--   for any tenant, register runners, read the audit trail. Under 008 it saw
--   nothing at all in multi-tenant mode.
--
--   The background jobs - the runner reaper, chunk GC, the log partition
--   maintainer, the redispatch sweeper - run with no request and therefore no
--   tenant. Under 008 they would quietly stop reaping, collecting and
--   dispatching the moment multi_tenant was switched on.
--
-- app.system is the escape, and it is deliberately narrow: SET LOCAL, so it
-- dies with its transaction and cannot ride a pooled connection into the next
-- request; and settable from exactly two places in the Go code, neither of
-- which an API key can reach.
--
-- This is a hole in tenant isolation. It is a hole the operator already has -
-- they hold the admin credentials and the database password - made explicit
-- and auditable rather than achieved by connecting as a superuser, which is
-- what a deployment would otherwise have to do and which turns every policy
-- off at once.

DROP POLICY IF EXISTS tenant_isolation ON action_logs;
CREATE POLICY tenant_isolation ON action_logs
    USING (
        CASE
            WHEN current_setting('app.system', true) = 'on'
                THEN true
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    )
    WITH CHECK (
        CASE
            WHEN current_setting('app.system', true) = 'on'
                THEN true
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    );
DROP POLICY IF EXISTS tenant_isolation ON agent_configs;
CREATE POLICY tenant_isolation ON agent_configs
    USING (
        CASE
            WHEN current_setting('app.system', true) = 'on'
                THEN true
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    )
    WITH CHECK (
        CASE
            WHEN current_setting('app.system', true) = 'on'
                THEN true
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    );
DROP POLICY IF EXISTS tenant_isolation ON api_keys;
CREATE POLICY tenant_isolation ON api_keys
    USING (
        CASE
            WHEN current_setting('app.system', true) = 'on'
                THEN true
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    )
    WITH CHECK (
        CASE
            WHEN current_setting('app.system', true) = 'on'
                THEN true
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    );
DROP POLICY IF EXISTS tenant_isolation ON data_keys;
CREATE POLICY tenant_isolation ON data_keys
    USING (
        CASE
            WHEN current_setting('app.system', true) = 'on'
                THEN true
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    )
    WITH CHECK (
        CASE
            WHEN current_setting('app.system', true) = 'on'
                THEN true
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    );
DROP POLICY IF EXISTS tenant_isolation ON log_archives;
CREATE POLICY tenant_isolation ON log_archives
    USING (
        CASE
            WHEN current_setting('app.system', true) = 'on'
                THEN true
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    )
    WITH CHECK (
        CASE
            WHEN current_setting('app.system', true) = 'on'
                THEN true
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    );
DROP POLICY IF EXISTS tenant_isolation ON logs;
CREATE POLICY tenant_isolation ON logs
    USING (
        CASE
            WHEN current_setting('app.system', true) = 'on'
                THEN true
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    )
    WITH CHECK (
        CASE
            WHEN current_setting('app.system', true) = 'on'
                THEN true
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    );
DROP POLICY IF EXISTS tenant_isolation ON permission_requests;
CREATE POLICY tenant_isolation ON permission_requests
    USING (
        CASE
            WHEN current_setting('app.system', true) = 'on'
                THEN true
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    )
    WITH CHECK (
        CASE
            WHEN current_setting('app.system', true) = 'on'
                THEN true
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    );
DROP POLICY IF EXISTS tenant_isolation ON profiles;
CREATE POLICY tenant_isolation ON profiles
    USING (
        CASE
            WHEN current_setting('app.system', true) = 'on'
                THEN true
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    )
    WITH CHECK (
        CASE
            WHEN current_setting('app.system', true) = 'on'
                THEN true
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    );
DROP POLICY IF EXISTS tenant_isolation ON provider_configs;
CREATE POLICY tenant_isolation ON provider_configs
    USING (
        CASE
            WHEN current_setting('app.system', true) = 'on'
                THEN true
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    )
    WITH CHECK (
        CASE
            WHEN current_setting('app.system', true) = 'on'
                THEN true
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    );
DROP POLICY IF EXISTS tenant_isolation ON runner_tokens;
CREATE POLICY tenant_isolation ON runner_tokens
    USING (
        CASE
            WHEN current_setting('app.system', true) = 'on'
                THEN true
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    )
    WITH CHECK (
        CASE
            WHEN current_setting('app.system', true) = 'on'
                THEN true
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    );
DROP POLICY IF EXISTS tenant_isolation ON runners;
CREATE POLICY tenant_isolation ON runners
    USING (
        CASE
            WHEN current_setting('app.system', true) = 'on'
                THEN true
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    )
    WITH CHECK (
        CASE
            WHEN current_setting('app.system', true) = 'on'
                THEN true
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    );
DROP POLICY IF EXISTS tenant_isolation ON scheduled_tasks;
CREATE POLICY tenant_isolation ON scheduled_tasks
    USING (
        CASE
            WHEN current_setting('app.system', true) = 'on'
                THEN true
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    )
    WITH CHECK (
        CASE
            WHEN current_setting('app.system', true) = 'on'
                THEN true
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    );
DROP POLICY IF EXISTS tenant_isolation ON sessions;
CREATE POLICY tenant_isolation ON sessions
    USING (
        CASE
            WHEN current_setting('app.system', true) = 'on'
                THEN true
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    )
    WITH CHECK (
        CASE
            WHEN current_setting('app.system', true) = 'on'
                THEN true
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    );
DROP POLICY IF EXISTS tenant_isolation ON snapshots;
CREATE POLICY tenant_isolation ON snapshots
    USING (
        CASE
            WHEN current_setting('app.system', true) = 'on'
                THEN true
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    )
    WITH CHECK (
        CASE
            WHEN current_setting('app.system', true) = 'on'
                THEN true
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    );
DROP POLICY IF EXISTS tenant_isolation ON streams;
CREATE POLICY tenant_isolation ON streams
    USING (
        CASE
            WHEN current_setting('app.system', true) = 'on'
                THEN true
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    )
    WITH CHECK (
        CASE
            WHEN current_setting('app.system', true) = 'on'
                THEN true
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    );
DROP POLICY IF EXISTS tenant_isolation ON task_runs;
CREATE POLICY tenant_isolation ON task_runs
    USING (
        CASE
            WHEN current_setting('app.system', true) = 'on'
                THEN true
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    )
    WITH CHECK (
        CASE
            WHEN current_setting('app.system', true) = 'on'
                THEN true
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    );
DROP POLICY IF EXISTS tenant_isolation ON tasks;
CREATE POLICY tenant_isolation ON tasks
    USING (
        CASE
            WHEN current_setting('app.system', true) = 'on'
                THEN true
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    )
    WITH CHECK (
        CASE
            WHEN current_setting('app.system', true) = 'on'
                THEN true
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    );
DROP POLICY IF EXISTS tenant_isolation ON tunnels;
CREATE POLICY tenant_isolation ON tunnels
    USING (
        CASE
            WHEN current_setting('app.system', true) = 'on'
                THEN true
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    )
    WITH CHECK (
        CASE
            WHEN current_setting('app.system', true) = 'on'
                THEN true
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    );
DROP POLICY IF EXISTS tenant_isolation ON webhook_events;
CREATE POLICY tenant_isolation ON webhook_events
    USING (
        CASE
            WHEN current_setting('app.system', true) = 'on'
                THEN true
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    )
    WITH CHECK (
        CASE
            WHEN current_setting('app.system', true) = 'on'
                THEN true
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    );
DROP POLICY IF EXISTS tenant_isolation ON webhooks;
CREATE POLICY tenant_isolation ON webhooks
    USING (
        CASE
            WHEN current_setting('app.system', true) = 'on'
                THEN true
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    )
    WITH CHECK (
        CASE
            WHEN current_setting('app.system', true) = 'on'
                THEN true
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    );
DROP POLICY IF EXISTS tenant_isolation ON workspaces;
CREATE POLICY tenant_isolation ON workspaces
    USING (
        CASE
            WHEN current_setting('app.system', true) = 'on'
                THEN true
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    )
    WITH CHECK (
        CASE
            WHEN current_setting('app.system', true) = 'on'
                THEN true
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    );
DROP POLICY IF EXISTS tenant_isolation ON logs_default;
CREATE POLICY tenant_isolation ON logs_default
    USING (
        CASE
            WHEN current_setting('app.system', true) = 'on'
                THEN true
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    )
    WITH CHECK (
        CASE
            WHEN current_setting('app.system', true) = 'on'
                THEN true
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    );

-- CAS tables: tenant_id is NOT NULL and every query filters on it explicitly.
DROP POLICY IF EXISTS tenant_isolation ON chunks;
CREATE POLICY tenant_isolation ON chunks
    USING (
        current_setting('app.system', true) = 'on'
        OR nullif(current_setting('app.tenant_id', true), '') IS NULL
        OR tenant_id = nullif(current_setting('app.tenant_id', true), '')
    )
    WITH CHECK (
        current_setting('app.system', true) = 'on'
        OR nullif(current_setting('app.tenant_id', true), '') IS NULL
        OR tenant_id = nullif(current_setting('app.tenant_id', true), '')
    );
DROP POLICY IF EXISTS tenant_isolation ON manifests;
CREATE POLICY tenant_isolation ON manifests
    USING (
        current_setting('app.system', true) = 'on'
        OR nullif(current_setting('app.tenant_id', true), '') IS NULL
        OR tenant_id = nullif(current_setting('app.tenant_id', true), '')
    )
    WITH CHECK (
        current_setting('app.system', true) = 'on'
        OR nullif(current_setting('app.tenant_id', true), '') IS NULL
        OR tenant_id = nullif(current_setting('app.tenant_id', true), '')
    );
