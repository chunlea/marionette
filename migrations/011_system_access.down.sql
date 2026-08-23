-- Reverses 011: restores the 008 policies, without the system escape.
--
-- After this runs, the admin API and the background jobs see only NULL-tenant
-- rows, which in a multi-tenant deployment means nothing at all.

DROP POLICY IF EXISTS tenant_isolation ON action_logs;
CREATE POLICY tenant_isolation ON action_logs
    USING (
        CASE
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    )
    WITH CHECK (
        CASE
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
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    )
    WITH CHECK (
        CASE
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
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    )
    WITH CHECK (
        CASE
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
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    )
    WITH CHECK (
        CASE
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
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    )
    WITH CHECK (
        CASE
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
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    )
    WITH CHECK (
        CASE
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
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    )
    WITH CHECK (
        CASE
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
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    )
    WITH CHECK (
        CASE
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
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    )
    WITH CHECK (
        CASE
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
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    )
    WITH CHECK (
        CASE
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
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    )
    WITH CHECK (
        CASE
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
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    )
    WITH CHECK (
        CASE
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
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    )
    WITH CHECK (
        CASE
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
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    )
    WITH CHECK (
        CASE
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
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    )
    WITH CHECK (
        CASE
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
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    )
    WITH CHECK (
        CASE
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
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    )
    WITH CHECK (
        CASE
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
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    )
    WITH CHECK (
        CASE
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
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    )
    WITH CHECK (
        CASE
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
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    )
    WITH CHECK (
        CASE
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
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    )
    WITH CHECK (
        CASE
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
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    )
    WITH CHECK (
        CASE
            WHEN nullif(current_setting('app.tenant_id', true), '') IS NOT NULL
                THEN tenant_id = nullif(current_setting('app.tenant_id', true), '')
            WHEN coalesce(current_setting('app.multi_tenant', true), 'off') = 'on'
                THEN false
            ELSE tenant_id IS NULL
        END
    );
DROP POLICY IF EXISTS tenant_isolation ON chunks;
CREATE POLICY tenant_isolation ON chunks
    USING (
        nullif(current_setting('app.tenant_id', true), '') IS NULL
        OR tenant_id = nullif(current_setting('app.tenant_id', true), '')
    )
    WITH CHECK (
        nullif(current_setting('app.tenant_id', true), '') IS NULL
        OR tenant_id = nullif(current_setting('app.tenant_id', true), '')
    );
DROP POLICY IF EXISTS tenant_isolation ON manifests;
CREATE POLICY tenant_isolation ON manifests
    USING (
        nullif(current_setting('app.tenant_id', true), '') IS NULL
        OR tenant_id = nullif(current_setting('app.tenant_id', true), '')
    )
    WITH CHECK (
        nullif(current_setting('app.tenant_id', true), '') IS NULL
        OR tenant_id = nullif(current_setting('app.tenant_id', true), '')
    );
