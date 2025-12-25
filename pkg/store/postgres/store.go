package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
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
	pool   *pgxpool.Pool
	logger *zap.Logger
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

// NOTE: Compile-time interface check is deferred until all CRUD methods are implemented.
// var _ store.Store = (*Store)(nil)

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

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("creating connection pool: %w", err)
	}

	// Verify connection
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	logger.Info("connected to PostgreSQL",
		zap.String("host", poolCfg.ConnConfig.Host),
		zap.Uint16("port", poolCfg.ConnConfig.Port),
		zap.String("database", poolCfg.ConnConfig.Database),
		zap.Int32("max_conns", cfg.MaxConns),
	)

	return &Store{
		pool:   pool,
		logger: logger,
	}, nil
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
// NOTE: Returns *Tx temporarily; will return store.Tx when all CRUD methods are implemented.
func (s *Store) BeginTx(ctx context.Context) (*Tx, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
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
