package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.uber.org/zap"

	pgstore "github.com/chunlea/marionette/pkg/store/postgres"
)

var testStore *pgstore.Store

// dockerHealth reports whether a Docker daemon is reachable.
//
// testcontainers panics rather than returning an error when it cannot find a
// Docker host at all ("rootless Docker not found"), so the probe has to
// recover: that panic is exactly the case this function exists to detect.
func dockerHealth(ctx context.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("no docker host: %v", r)
		}
	}()

	provider, err := testcontainers.NewDockerProvider()
	if err != nil {
		return err
	}
	defer func() { _ = provider.Close() }()

	return provider.Health(ctx)
}

const noDockerBanner = `
################################################################################
# pkg/store/postgres CANNOT RUN — no Docker daemon reachable.
#
# These are the only tests that exercise real SQL: migrations, constraints,
# pagination, error mapping. Nothing else covers them.
#
# Reason: %v
#
# To run them:  make test-store    (on the host)
#               make test-linux    (in the Linux container; mounts the socket)
#
# To skip instead of failing, set MARIONETTE_TEST_SKIP_WITHOUT_DOCKER=1.
################################################################################
`

// Failing is the default rather than skipping, because "go test" prints a
// package's output only when it fails: a skip here would be invisible in normal
// output and the suite would report success for tests that never ran. That is
// exactly how this package went unexercised by make test-linux.

func TestMain(m *testing.M) {
	ctx := context.Background()

	// Probe first: testcontainers panics deep in its internals when there is no
	// Docker host, which is unreadable and gives the reader nothing to act on.
	if err := dockerHealth(ctx); err != nil {
		fmt.Fprintf(os.Stderr, noDockerBanner, err)

		if os.Getenv("MARIONETTE_TEST_SKIP_WITHOUT_DOCKER") != "" {
			fmt.Fprintln(os.Stderr, "MARIONETTE_TEST_SKIP_WITHOUT_DOCKER is set: skipping.")
			os.Exit(0)
		}
		os.Exit(1)
	}

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
	// Glob instead of a hardcoded list: a new migration must never be silently
	// skipped here, or the tests would validate a stale schema.
	migrationFiles, err := filepath.Glob("../../../migrations/*.up.sql")
	if err != nil {
		return err
	}
	if len(migrationFiles) == 0 {
		return errors.New("no migrations found in ../../../migrations")
	}

	// Numeric prefixes are zero-padded, so lexical order is migration order.
	sort.Strings(migrationFiles)

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
