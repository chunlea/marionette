// Package main provides the marionette server binary.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chunlea/marionette/pkg/config"
	"github.com/chunlea/marionette/pkg/server/admin"
	"github.com/chunlea/marionette/pkg/server/api"
	grpcserver "github.com/chunlea/marionette/pkg/server/grpc"
	"github.com/chunlea/marionette/pkg/store/postgres"
	"go.uber.org/zap"
)

func main() {
	// Parse command-line flags
	configPath := flag.String("config", "configs/local.yaml", "path to config file")
	flag.Parse()

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Initialize logger based on config
	logger, err := newLogger(cfg.Logging.Level, cfg.Logging.Format)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = logger.Sync() }()

	logger.Info("marionette server starting",
		zap.String("config", *configPath),
		zap.String("log_level", cfg.Logging.Level),
	)

	// Load secrets (optional in development mode)
	secrets := config.LoadSecretsOptional()

	// Initialize database store
	var store *postgres.Store
	if secrets.DatabaseURL != "" {
		ctx := context.Background()
		var err error
		store, err = postgres.New(ctx, postgres.Config{
			URL:             secrets.DatabaseURL,
			MaxConns:        int32(cfg.Database.MaxOpenConns),
			MinConns:        int32(cfg.Database.MaxIdleConns),
			MaxConnLifetime: parseDuration(cfg.Database.ConnMaxLifetime, time.Hour),
		}, logger)
		if err != nil {
			logger.Fatal("failed to connect to database", zap.Error(err))
		}
		logger.Info("database connected")
		admin.Registry.Register("Database", 0, "ok", "Connected")
	} else {
		logger.Warn("database URL not set, database features will be unavailable")
		admin.Registry.Register("Database", 0, "warn", "Not configured")
	}

	// Create servers
	apiServer := api.New(api.Config{
		Host: cfg.Server.API.Host,
		Port: cfg.Server.API.Port,
	}, logger)

	adminServer := admin.New(admin.Config{
		Host: cfg.Server.Admin.Host,
		Port: cfg.Server.Admin.Port,
	}, logger)

	grpcServer, err := grpcserver.New(grpcserver.Config{
		Host: cfg.Server.GRPC.Host,
		Port: cfg.Server.GRPC.Port,
	}, logger)
	if err != nil {
		logger.Fatal("failed to create gRPC server", zap.Error(err))
	}

	// Start servers in goroutines
	errChan := make(chan error, 3)

	go func() {
		if err := apiServer.Start(); err != nil {
			errChan <- fmt.Errorf("api server: %w", err)
		}
	}()

	go func() {
		if err := adminServer.Start(); err != nil {
			errChan <- fmt.Errorf("admin server: %w", err)
		}
	}()

	go func() {
		if err := grpcServer.Start(); err != nil {
			errChan <- fmt.Errorf("grpc server: %w", err)
		}
	}()

	// Register service statuses
	admin.Registry.Register("Public API", cfg.Server.API.Port, "ok", "Running")
	admin.Registry.Register("Admin API", cfg.Server.Admin.Port, "ok", "Running")
	admin.Registry.Register("gRPC", cfg.Server.GRPC.Port, "ok", "Running")

	logger.Info("all servers started",
		zap.String("api_addr", fmt.Sprintf("%s:%d", cfg.Server.API.Host, cfg.Server.API.Port)),
		zap.String("admin_addr", fmt.Sprintf("%s:%d", cfg.Server.Admin.Host, cfg.Server.Admin.Port)),
		zap.String("grpc_addr", fmt.Sprintf("%s:%d", cfg.Server.GRPC.Host, cfg.Server.GRPC.Port)),
	)

	// Wait for interrupt signal or server error
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errChan:
		logger.Error("server error", zap.Error(err))
	case sig := <-sigChan:
		logger.Info("received shutdown signal", zap.String("signal", sig.String()))
	}

	// Graceful shutdown
	logger.Info("initiating graceful shutdown")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Shutdown all servers
	if err := apiServer.Shutdown(ctx); err != nil {
		logger.Error("api server shutdown error", zap.Error(err))
	}
	if err := adminServer.Shutdown(ctx); err != nil {
		logger.Error("admin server shutdown error", zap.Error(err))
	}
	if err := grpcServer.Shutdown(ctx); err != nil {
		logger.Error("grpc server shutdown error", zap.Error(err))
	}

	// Close database connection
	if store != nil {
		if err := store.Close(); err != nil {
			logger.Error("database close error", zap.Error(err))
		} else {
			logger.Info("database connection closed")
		}
	}

	logger.Info("marionette server stopped")
}

// newLogger creates a zap logger with the specified level and format.
func newLogger(level, format string) (*zap.Logger, error) {
	// Parse log level
	var zapLevel zap.AtomicLevel
	switch level {
	case "debug":
		zapLevel = zap.NewAtomicLevelAt(zap.DebugLevel)
	case "info":
		zapLevel = zap.NewAtomicLevelAt(zap.InfoLevel)
	case "warn":
		zapLevel = zap.NewAtomicLevelAt(zap.WarnLevel)
	case "error":
		zapLevel = zap.NewAtomicLevelAt(zap.ErrorLevel)
	default:
		zapLevel = zap.NewAtomicLevelAt(zap.InfoLevel)
	}

	// Build config based on format
	var cfg zap.Config
	if format == "console" {
		cfg = zap.NewDevelopmentConfig()
	} else {
		cfg = zap.NewProductionConfig()
	}
	cfg.Level = zapLevel

	return cfg.Build()
}

// parseDuration parses a duration string, returning the default if empty or invalid.
func parseDuration(s string, defaultVal time.Duration) time.Duration {
	if s == "" {
		return defaultVal
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return defaultVal
	}
	return d
}
