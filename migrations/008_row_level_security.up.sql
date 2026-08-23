-- Row level security: make tenant isolation a property of the database.
--
-- Until now tenant_id was a column nobody enforced. Every query that forgot a
-- WHERE clause returned every tenant's rows, and the only thing standing
-- between two customers was the diligence of 133 hand-written queries.
--
-- The application still filters, but Postgres is now the backstop: a query that
-- forgets is answered with the caller's rows, not everyone's.
--
-- HOW THE POLICY DECIDES
--
--   app.tenant_id  is set per transaction by the store (SET LOCAL) whenever a
--                  tenant is present in the request context.
--   app.multi_tenant  is set once per connection from the multi_tenant config
--                  flag. It selects what a MISSING tenant means.
--
--   tenant set                  -> rows of that tenant, and only those.
--   no tenant, multi_tenant off -> rows with tenant_id IS NULL. This is exactly
--                                  what a single-tenant deployment sees today:
--                                  it never writes a tenant_id, so every row it
--                                  owns is NULL and every row stays visible.
--   no tenant, multi_tenant on  -> nothing. A code path that reaches SQL
--                                  without a tenant in a multi-tenant
--                                  deployment is a bug, and it fails closed
--                                  rather than leaking across customers.
--
-- FORCE ROW LEVEL SECURITY is required, not optional: the application connects
-- as the table owner, and owners bypass ordinary RLS. Without FORCE these
-- policies would be decorative in exactly the deployment they protect.
--
-- current_setting(..., true) returns NULL when the GUC was never set and ''
-- once a SET LOCAL has gone out of scope, so both spellings of "unset" are
-- normalised with nullif before they are compared.

--------------------------------------------------------------------------------
-- Tables whose tenant_id is nullable (single-tenant writes NULL)
--------------------------------------------------------------------------------
ALTER TABLE action_logs ENABLE ROW LEVEL SECURITY;
ALTER TABLE action_logs FORCE ROW LEVEL SECURITY;
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
ALTER TABLE agent_configs ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_configs FORCE ROW LEVEL SECURITY;
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
ALTER TABLE api_keys ENABLE ROW LEVEL SECURITY;
ALTER TABLE api_keys FORCE ROW LEVEL SECURITY;
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
ALTER TABLE data_keys ENABLE ROW LEVEL SECURITY;
ALTER TABLE data_keys FORCE ROW LEVEL SECURITY;
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
ALTER TABLE log_archives ENABLE ROW LEVEL SECURITY;
ALTER TABLE log_archives FORCE ROW LEVEL SECURITY;
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
ALTER TABLE logs ENABLE ROW LEVEL SECURITY;
ALTER TABLE logs FORCE ROW LEVEL SECURITY;
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
ALTER TABLE permission_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE permission_requests FORCE ROW LEVEL SECURITY;
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
ALTER TABLE profiles ENABLE ROW LEVEL SECURITY;
ALTER TABLE profiles FORCE ROW LEVEL SECURITY;
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
ALTER TABLE provider_configs ENABLE ROW LEVEL SECURITY;
ALTER TABLE provider_configs FORCE ROW LEVEL SECURITY;
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
ALTER TABLE runner_tokens ENABLE ROW LEVEL SECURITY;
ALTER TABLE runner_tokens FORCE ROW LEVEL SECURITY;
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
ALTER TABLE runners ENABLE ROW LEVEL SECURITY;
ALTER TABLE runners FORCE ROW LEVEL SECURITY;
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
ALTER TABLE scheduled_tasks ENABLE ROW LEVEL SECURITY;
ALTER TABLE scheduled_tasks FORCE ROW LEVEL SECURITY;
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
ALTER TABLE sessions ENABLE ROW LEVEL SECURITY;
ALTER TABLE sessions FORCE ROW LEVEL SECURITY;
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
ALTER TABLE snapshots ENABLE ROW LEVEL SECURITY;
ALTER TABLE snapshots FORCE ROW LEVEL SECURITY;
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
ALTER TABLE streams ENABLE ROW LEVEL SECURITY;
ALTER TABLE streams FORCE ROW LEVEL SECURITY;
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
ALTER TABLE task_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE task_runs FORCE ROW LEVEL SECURITY;
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
ALTER TABLE tasks ENABLE ROW LEVEL SECURITY;
ALTER TABLE tasks FORCE ROW LEVEL SECURITY;
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
ALTER TABLE tunnels ENABLE ROW LEVEL SECURITY;
ALTER TABLE tunnels FORCE ROW LEVEL SECURITY;
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
ALTER TABLE webhook_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE webhook_events FORCE ROW LEVEL SECURITY;
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
ALTER TABLE webhooks ENABLE ROW LEVEL SECURITY;
ALTER TABLE webhooks FORCE ROW LEVEL SECURITY;
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
ALTER TABLE workspaces ENABLE ROW LEVEL SECURITY;
ALTER TABLE workspaces FORCE ROW LEVEL SECURITY;
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

--------------------------------------------------------------------------------
-- CAS tables: tenant_id is NOT NULL and every query filters by it explicitly
--------------------------------------------------------------------------------
--
-- chunks carries tenant_id in its primary key and manifests requires it, so
-- neither can ever hold a NULL-tenant row. The CAS API takes the tenant as an
-- argument rather than from the request context, so these policies constrain a
-- session that HAS a tenant and otherwise defer to that explicit predicate.
-- Once the CAS queries read the tenant from context, the ELSE arm here can
-- tighten to false the way the tables above do.
ALTER TABLE chunks ENABLE ROW LEVEL SECURITY;
ALTER TABLE chunks FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chunks
    USING (
        nullif(current_setting('app.tenant_id', true), '') IS NULL
        OR tenant_id = nullif(current_setting('app.tenant_id', true), '')
    )
    WITH CHECK (
        nullif(current_setting('app.tenant_id', true), '') IS NULL
        OR tenant_id = nullif(current_setting('app.tenant_id', true), '')
    );
ALTER TABLE manifests ENABLE ROW LEVEL SECURITY;
ALTER TABLE manifests FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON manifests
    USING (
        nullif(current_setting('app.tenant_id', true), '') IS NULL
        OR tenant_id = nullif(current_setting('app.tenant_id', true), '')
    )
    WITH CHECK (
        nullif(current_setting('app.tenant_id', true), '') IS NULL
        OR tenant_id = nullif(current_setting('app.tenant_id', true), '')
    );

--------------------------------------------------------------------------------
-- Partitioned logs
--------------------------------------------------------------------------------
--
-- A policy on the partitioned parent covers every partition for queries that go
-- through the parent, which is the only way the store reaches logs. The default
-- partition gets its own copy so that a direct query against it - a human at a
-- psql prompt, a future maintenance job - is covered too. Daily partitions are
-- created at runtime by create_log_partition; they are reachable only through
-- the parent, and inherit its policy there.

ALTER TABLE logs_default ENABLE ROW LEVEL SECURITY;
ALTER TABLE logs_default FORCE ROW LEVEL SECURITY;
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
