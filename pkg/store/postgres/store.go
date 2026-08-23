package postgres

import (
	"context"
	"errors"
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
	if cfg.MultiTenant {
		if err := verifyRLSApplies(ctx, pool); err != nil {
			pool.Close()
			return nil, err
		}
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

// verifyRLSApplies fails when the connected role is exempt from row level
// security, which would make tenant isolation silently unenforced.
func verifyRLSApplies(ctx context.Context, pool *pgxpool.Pool) error {
	var exempt bool
	const q = `SELECT rolsuper OR rolbypassrls FROM pg_roles WHERE rolname = current_user`
	if err := pool.QueryRow(ctx, q).Scan(&exempt); err != nil {
		return fmt.Errorf("checking whether row level security applies: %w", err)
	}
	if exempt {
		return errors.New(
			"multi_tenant is on but the database role is a superuser or has BYPASSRLS, " +
				"which ignores the tenant policies entirely: connect as an unprivileged role " +
				"that owns no tables")
	}
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
	if tenantID, ok := store.TenantFromContext(ctx); ok {
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
