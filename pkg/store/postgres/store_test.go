package postgres_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.uber.org/zap"

	pgstore "github.com/chunlea/marionette/pkg/store/postgres"
)

var testStore *pgstore.Store

func TestMain(m *testing.M) {
	ctx := context.Background()

	// Start PostgreSQL container
	postgresContainer, err := postgres.Run(ctx,
		"postgres:15-alpine",
		postgres.WithDatabase("marionette_test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		panic("failed to start postgres container: " + err.Error())
	}

	// Get connection string
	connStr, err := postgresContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		panic("failed to get connection string: " + err.Error())
	}

	// Create logger
	logger, _ := zap.NewDevelopment()

	// Create store
	testStore, err = pgstore.New(ctx, pgstore.Config{URL: connStr}, logger)
	if err != nil {
		panic("failed to create store: " + err.Error())
	}

	// Run migrations
	if err := runMigrations(ctx, testStore); err != nil {
		panic("failed to run migrations: " + err.Error())
	}

	// Run tests
	code := m.Run()

	// Cleanup
	_ = testStore.Close()
	_ = postgresContainer.Terminate(ctx)

	os.Exit(code)
}

func runMigrations(ctx context.Context, s *pgstore.Store) error {
	// Read and execute all migration files in order
	// Note: All migrations have been consolidated into 001_initial.up.sql
	migrationFiles := []string{
		"../../../migrations/001_initial.up.sql",
		"../../../migrations/002_add_streams.up.sql",
	}

	for _, file := range migrationFiles {
		migration, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		if err := s.ExecRaw(ctx, string(migration)); err != nil {
			return err
		}
	}
	return nil
}

// =============================================================================
// Helper Functions
// =============================================================================

func strPtr(s string) *string {
	return &s
}

func intPtr(i int) *int {
	return &i
}

func int64Ptr(i int64) *int64 {
	return &i
}
