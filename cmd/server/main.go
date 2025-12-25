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
	defer logger.Sync()

	logger.Info("marionette server starting")

	// TODO: Initialize configuration
	// TODO: Initialize store
	// TODO: Initialize gRPC server
	// TODO: Initialize HTTP servers (public + admin)
	// TODO: Start servers

	logger.Info("marionette server stopped")
}
