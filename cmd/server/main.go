// Package main provides the marionette server binary.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chunlea/marionette/pkg/server/admin"
	"github.com/chunlea/marionette/pkg/server/api"
	grpcserver "github.com/chunlea/marionette/pkg/server/grpc"
	"go.uber.org/zap"
)

func main() {
	logger, err := zap.NewProduction()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = logger.Sync() }()

	logger.Info("marionette server starting")

	// Create servers
	apiServer := api.New(api.Config{Port: 8080}, logger)
	adminServer := admin.New(admin.Config{Port: 8081}, logger)
	grpcServer, err := grpcserver.New(grpcserver.Config{Port: 9090}, logger)
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
	admin.Registry.Register("Public API", 8080, "ok", "Running")
	admin.Registry.Register("Admin API", 8081, "ok", "Running")
	admin.Registry.Register("gRPC", 9090, "ok", "Running")

	logger.Info("all servers started",
		zap.Int("api_port", 8080),
		zap.Int("admin_port", 8081),
		zap.Int("grpc_port", 9090),
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

	logger.Info("marionette server stopped")
}
