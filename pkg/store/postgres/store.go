package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/store"
)

// Config holds PostgreSQL connection configuration.
type Config struct {
	// URL is the PostgreSQL connection string.
	// Format: postgres://user:password@host:port/database?sslmode=disable
	URL string

	// MaxConns is the maximum number of connections in the pool.
	// Default: 25
	MaxConns int32

	// MinConns is the minimum number of connections to keep open.
	// Default: 5
	MinConns int32

	// MaxConnLifetime is the maximum lifetime of a connection.
	// Default: 1 hour
	MaxConnLifetime time.Duration

	// MaxConnIdleTime is the maximum idle time for a connection.
	// Default: 30 minutes
	MaxConnIdleTime time.Duration

	// HealthCheckPeriod is the interval between health checks.
	// Default: 1 minute
	HealthCheckPeriod time.Duration

	// MultiTenant makes a missing tenant an error rather than a
	// single-tenant deployment.
	//
	// It is published to Postgres as app.multi_tenant on every connection, and
	// the row level security policies read it: with it on, a statement that
	// reaches the database without a tenant bound sees nothing at all. Leave it
	// off for single-tenant deployments, which never write a tenant_id.
	MultiTenant bool
}

// setDefaults applies default values to the config.
func (c *Config) setDefaults() {
	if c.MaxConns == 0 {
		c.MaxConns = 25
	}
	if c.MinConns == 0 {
		c.MinConns = 5
	}
	if c.MaxConnLifetime == 0 {
		c.MaxConnLifetime = time.Hour
	}
	if c.MaxConnIdleTime == 0 {
		c.MaxConnIdleTime = 30 * time.Minute
	}
	if c.HealthCheckPeriod == 0 {
		c.HealthCheckPeriod = time.Minute
	}
}

// Store implements store.Store using PostgreSQL.
type Store struct {
	pool *pgxpool.Pool

	// db is the querier every statement goes through. It binds the request's
	// tenant before the statement runs; see tenantDB. pool stays for the
	// operations that are not statements: Begin, Ping, Close, Stat.
	db tenantDB

	logger      *zap.Logger
	multiTenant bool
}

// querier is an interface satisfied by both *pgxpool.Pool and pgx.Tx.
// Used by CRUD operations to work with both direct pool and transaction contexts.
//
//nolint:unused // Used by CRUD operations in subsequent files
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Compile-time check: adding a method to store.Store must fail the build here,
// not at the first call site that needs it.
var _ store.Store = (*Store)(nil)

// New creates a new PostgreSQL store.
func New(ctx context.Context, cfg Config, logger *zap.Logger) (*Store, error) {
	cfg.setDefaults()

	poolCfg, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parsing database URL: %w", err)
	}

	poolCfg.MaxConns = cfg.MaxConns
	poolCfg.MinConns = cfg.MinConns
	poolCfg.MaxConnLifetime = cfg.MaxConnLifetime
	poolCfg.MaxConnIdleTime = cfg.MaxConnIdleTime
	poolCfg.HealthCheckPeriod = cfg.HealthCheckPeriod

	// app.multi_tenant is a deployment-wide constant, so it is set once per
	// connection rather than per request. The row level security policies read
	// it to decide what a missing tenant means: nothing at all in multi-tenant
	// mode, the NULL-tenant rows in single-tenant mode.
	if cfg.MultiTenant {
		poolCfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
			if _, err := conn.Exec(ctx, `SELECT set_config('app.multi_tenant', 'on', false)`); err != nil {
				return fmt.Errorf("enabling multi-tenant mode on connection: %w", err)
			}
			return nil
		}
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("creating connection pool: %w", err)
	}

	// Verify connection
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	// Row level security is the whole isolation guarantee in multi-tenant mode,
	// and superusers - and anyone with BYPASSRLS - ignore it completely. FORCE
	// ROW LEVEL SECURITY does not help: it subjects the table OWNER to the
	// policies, not a superuser. Connecting as postgres would therefore leave
	// every policy in migration 008 inert while looking perfectly healthy, so
	// this refuses to start instead.
	if err := checkRLSEnforcement(ctx, pool, cfg.MultiTenant, logger); err != nil {
		pool.Close()
		return nil, err
	}

	logger.Info("connected to PostgreSQL",
		zap.String("host", poolCfg.ConnConfig.Host),
		zap.Uint16("port", poolCfg.ConnConfig.Port),
		zap.String("database", poolCfg.ConnConfig.Database),
		zap.Int32("max_conns", cfg.MaxConns),
		zap.Bool("multi_tenant", cfg.MultiTenant),
	)

	return &Store{
		pool:        pool,
		db:          tenantDB{pool: pool},
		logger:      logger,
		multiTenant: cfg.MultiTenant,
	}, nil
}

// rlsStatus is what the database says about whether the tenant policies can
// actually bind to this connection.
type rlsStatus struct {
	// exemptRole is true for a superuser or a role with BYPASSRLS. Such a role
	// ignores every policy, and FORCE does not change that.
	exemptRole bool
	// ownsTables is true when the connected role owns the tenant tables. An
	// owner bypasses ordinary RLS, so the policies only bind if FORCE is set.
	ownsTables bool
	// policiesInstalled is true when migration 008 has been applied.
	policiesInstalled bool
	// forced is true when the tenant tables carry FORCE ROW LEVEL SECURITY.
	forced bool
}

// bypassReason explains why isolation would not be enforced, or "" when it
// would be.
func (s rlsStatus) bypassReason() string {
	switch {
	case s.exemptRole:
		return "the database role is a superuser or has BYPASSRLS, which ignores every tenant policy"
	case !s.policiesInstalled:
		return "the tenant policies are missing: migration 008 has not been applied"
	case s.ownsTables && !s.forced:
		return "the database role owns the tenant tables and FORCE ROW LEVEL SECURITY is not set, so it bypasses every policy"
	default:
		return ""
	}
}

// readRLSStatus asks the database whether the tenant policies would bind.
func readRLSStatus(ctx context.Context, pool *pgxpool.Pool) (rlsStatus, error) {
	var s rlsStatus

	const q = `
		SELECT
			(SELECT rolsuper OR rolbypassrls FROM pg_roles WHERE rolname = current_user),
			EXISTS (SELECT 1 FROM pg_policies WHERE policyname = 'tenant_isolation'),
			COALESCE((SELECT bool_and(relforcerowsecurity) FROM pg_class
				WHERE relname IN ('sessions', 'tasks', 'runners', 'workspaces')), false),
			COALESCE((SELECT bool_or(pg_get_userbyid(relowner) = current_user) FROM pg_class
				WHERE relname IN ('sessions', 'tasks', 'runners', 'workspaces')), false)`

	if err := pool.QueryRow(ctx, q).Scan(&s.exemptRole, &s.policiesInstalled, &s.forced, &s.ownsTables); err != nil {
		return s, fmt.Errorf("checking whether row level security applies: %w", err)
	}
	return s, nil
}

// checkRLSEnforcement decides whether the deployment can rely on the database
// to keep tenants apart.
//
// In multi-tenant mode this is the whole isolation guarantee, so anything that
// would leave the policies inert is a refusal to start rather than a warning:
// a server that boots and serves every tenant's rows to every caller looks
// perfectly healthy right up until it does not.
//
// In single-tenant mode the same conditions are harmless today - there is only
// one tenant and every row has a NULL tenant_id - but they are exactly what
// makes a later switch to multi_tenant fail, so they are worth saying out loud.
func checkRLSEnforcement(ctx context.Context, pool *pgxpool.Pool, multiTenant bool, logger *zap.Logger) error {
	status, err := readRLSStatus(ctx, pool)
	if err != nil {
		if multiTenant {
			return err
		}
		logger.Warn("could not determine whether row level security applies", zap.Error(err))
		return nil
	}

	reason := status.bypassReason()
	if reason == "" {
		return nil
	}

	if multiTenant {
		return fmt.Errorf(
			"multi_tenant is on but tenant isolation would not be enforced: %s; "+
				"connect as an unprivileged role that owns no tables, against a database with migration 008 applied",
			reason)
	}

	logger.Warn("tenant row level security is not enforced on this connection",
		zap.String("reason", reason),
		zap.String("impact", "harmless while multi_tenant is off, but switching it on would fail"),
	)
	return nil
}

// Ping checks database connectivity.
func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// Close closes the connection pool.
func (s *Store) Close() error {
	s.pool.Close()
	return nil
}

// BeginTx starts a new transaction.
func (s *Store) BeginTx(ctx context.Context) (store.Tx, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}

	// Bind the tenant for the life of the transaction, so every statement
	// inside it is filtered by the same policies as a single statement is.
	if store.IsSystemAccess(ctx) {
		if _, err := tx.Exec(ctx, setSystemSQL); err != nil {
			_ = tx.Rollback(ctx)
			return nil, fmt.Errorf("granting system access: %w", err)
		}
	} else if tenantID, ok := store.TenantFromContext(ctx); ok {
		if _, err := tx.Exec(ctx, setTenantSQL, tenantID); err != nil {
			_ = tx.Rollback(ctx)
			return nil, fmt.Errorf("binding tenant %q: %w", tenantID, err)
		}
	}

	return &Tx{
		tx:     tx,
		logger: s.logger,
	}, nil
}

// Pool returns the underlying connection pool.
// This is useful for advanced operations that need direct pool access.
func (s *Store) Pool() *pgxpool.Pool {
	return s.pool
}

// Stats returns connection pool statistics.
func (s *Store) Stats() *pgxpool.Stat {
	return s.pool.Stat()
}

// ExecRaw executes raw SQL statements.
// This is useful for migrations and administrative tasks.
func (s *Store) ExecRaw(ctx context.Context, sql string) error {
	_, err := s.pool.Exec(ctx, sql)
	return err
}
