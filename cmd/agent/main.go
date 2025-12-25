// Package main provides the marionette-agent binary.
package main

import (
	"fmt"
	"os"

	"go.uber.org/zap"
)

func main() {
	logger, err := zap.NewProduction()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = logger.Sync() }()

	logger.Info("marionette agent starting")

	// TODO: Initialize configuration
	// TODO: Connect to server via gRPC
	// TODO: Register runner
	// TODO: Start control channel
	// TODO: Handle tasks

	logger.Info("marionette agent stopped")
}
