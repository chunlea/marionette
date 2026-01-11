// Package main provides the marionette server binary.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chunlea/marionette/pkg/audit"
	"github.com/chunlea/marionette/pkg/auth"
	"github.com/chunlea/marionette/pkg/config"
	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/provider"
	"github.com/chunlea/marionette/pkg/provider/docker"
	"github.com/chunlea/marionette/pkg/server/admin"
	"github.com/chunlea/marionette/pkg/server/api"
	"github.com/chunlea/marionette/pkg/server/core"
	grpcserver "github.com/chunlea/marionette/pkg/server/grpc"
	"github.com/chunlea/marionette/pkg/store"
	"github.com/chunlea/marionette/pkg/store/postgres"
	"github.com/chunlea/marionette/pkg/tunnel"
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
	var dbStore *postgres.Store
	if secrets.DatabaseURL != "" {
		ctx := context.Background()
		var err error
		dbStore, err = postgres.New(ctx, postgres.Config{
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

	// Initialize provider registry
	providerRegistry := initProviderRegistry(dbStore, cfg, logger)

	// Create core managers (only if database is available)
	var sessionMgr *core.SessionManager
	var taskMgr *core.TaskManager
	var permMgr *core.PermissionManager
	var permEnforcer *core.PermissionTimeoutEnforcer
	var connManager *grpcserver.ConnectionManager
	var apiOpts []api.Option
	var apiKeySvc *auth.APIKeyService
	var grpcOpts []grpcserver.ServerOption

	if dbStore != nil {
		// Create API key service (needed for both public and admin APIs)
		apiKeySvc = auth.NewAPIKeyService(dbStore, id.APIKey)

		// Create connection manager first (needed by PermissionManager)
		connManager = grpcserver.NewConnectionManager(logger)

		// Create workspace manager
		workspaceMgr := core.NewWorkspaceManager(dbStore, cfg.Storage.Workspace, logger)

		// Create audit logger
		auditStoreAdapter := audit.NewStoreAdapter(dbStore)
		auditLog := audit.NewLogger(auditStoreAdapter)

		// Create core managers
		sessionMgr = core.NewSessionManager(dbStore, connManager, connManager, logger)
		sessionMgr.SetWorkspaceManager(workspaceMgr)
		taskMgr = core.NewTaskManager(dbStore, connManager, sessionMgr, auditLog, logger)
		sessionMgr.SetProviderRegistry(providerRegistry)
		sessionMgr.SetTaskManager(taskMgr)

		// Create permission manager with connection manager as command sender
		permMgr = core.NewPermissionManager(dbStore, connManager, sessionMgr, auditLog, logger)

		// Create permission timeout enforcer
		permEnforcer = core.NewPermissionTimeoutEnforcer(dbStore, sessionMgr, logger)

		// Create adapters and add to API options
		sessionAdapter := api.NewSessionAdapter(sessionMgr, workspaceMgr)
		taskAdapter := api.NewTaskAdapter(taskMgr, dbStore)
		permAdapter := api.NewPermissionAdapter(permMgr)
		workspaceAdapter := api.NewWorkspaceAdapter(workspaceMgr)

		apiOpts = append(apiOpts,
			api.WithSessionService(sessionAdapter),
			api.WithTaskService(taskAdapter),
			api.WithPermissionService(permAdapter),
			api.WithWorkspaceService(workspaceAdapter),
			api.WithAPIKeyService(apiKeySvc),
		)

		// Create tunnel manager for HTTP tunnel proxy
		baseURL := fmt.Sprintf("http://%s:%d", cfg.Server.API.Host, cfg.Server.API.Port)
		if cfg.Server.API.Host == "" || cfg.Server.API.Host == "0.0.0.0" {
			baseURL = fmt.Sprintf("http://localhost:%d", cfg.Server.API.Port)
		}
		tunnelMgr := tunnel.NewTunnelManager(
			tunnel.WithLogger(logger),
			tunnel.WithBaseURL(baseURL),
		)

		// Create tunnel router
		tunnelRouter := grpcserver.NewTunnelRouter(
			grpcserver.WithTRLogger(logger),
			grpcserver.WithTRConnectionManager(connManager),
			grpcserver.WithTRTunnelManager(tunnelMgr),
		)

		// Create tunnel proxy adapter and handler
		tunnelProxyAdapter := api.NewTunnelProxyAdapter(
			api.WithTPALogger(logger),
			api.WithTPATunnelManager(tunnelMgr),
			api.WithTPATunnelRouter(tunnelRouter),
		)

		tunnelProxyHandler := api.NewTunnelProxyHandler(
			api.WithTPLogger(logger),
			api.WithTPService(tunnelProxyAdapter),
			api.WithTPAPIKeyAuth(func(r *http.Request) (bool, error) {
				// Extract and validate API key
				key := r.Header.Get("X-API-Key")
				if key == "" {
					key = r.Header.Get("Authorization")
					if len(key) > 7 && key[:7] == "Bearer " {
						key = key[7:]
					}
				}
				if key == "" {
					return false, nil
				}
				_, err := apiKeySvc.Validate(r.Context(), key)
				return err == nil, nil
			}),
		)

		// Create tunnel adapter for tunnel API endpoints
		tunnelAdapter := api.NewTunnelAdapter(
			api.WithTALogger(logger),
			api.WithTATunnelManager(tunnelMgr),
			api.WithTATunnelRouter(tunnelRouter),
			api.WithTAStore(dbStore),
		)

		apiOpts = append(apiOpts,
			api.WithTunnelProxy(tunnelProxyHandler),
			api.WithTunnelService(tunnelAdapter),
		)
		logger.Info("tunnel proxy handler wired to API",
			zap.String("base_url", baseURL),
		)

		// Wire gRPC server options
		grpcOpts = append(grpcOpts,
			grpcserver.WithConnManager(connManager),
			grpcserver.WithPermissionManager(permMgr),
			grpcserver.WithTaskManager(taskMgr),
			grpcserver.WithSessionManager(sessionMgr),
			grpcserver.WithTunnelRouter(tunnelRouter),
		)

		logger.Info("core services initialized and wired to API")
	}

	// Create servers
	apiServer := api.New(api.Config{
		Host: cfg.Server.API.Host,
		Port: cfg.Server.API.Port,
	}, logger, apiOpts...)

	// Create admin server options
	var adminOpts []admin.Option
	if apiKeySvc != nil {
		// Create adapter for admin API using existing API key service
		apiKeyAdapter := admin.NewAPIKeyAdapter(apiKeySvc)
		adminOpts = append(adminOpts, admin.WithAPIKeyService(apiKeyAdapter))
		logger.Info("API key service wired to Admin API")
	}
	if sessionMgr != nil {
		adminOpts = append(adminOpts, admin.WithSessionActivator(sessionMgr))
		logger.Info("Session activator wired to Admin API")
	}
	if dbStore != nil {
		// Create action log service adapter for admin API
		actionLogAdapter := admin.NewActionLogStoreAdapter(dbStore)
		adminOpts = append(adminOpts, admin.WithActionLogService(actionLogAdapter))
		logger.Info("Action log service wired to Admin API")
	}

	adminServer := admin.New(admin.Config{
		Host: cfg.Server.Admin.Host,
		Port: cfg.Server.Admin.Port,
	}, logger, adminOpts...)

	grpcServer, err := grpcserver.New(grpcserver.Config{
		Host:  cfg.Server.GRPC.Host,
		Port:  cfg.Server.GRPC.Port,
		TLS:   &cfg.TLS,
		Store: dbStore,
	}, logger, grpcOpts...)
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

	// Start permission timeout enforcer if available
	if permEnforcer != nil {
		permEnforcer.Start(context.Background())
		logger.Info("permission timeout enforcer started")
	}

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

	// Stop permission timeout enforcer first
	if permEnforcer != nil {
		permEnforcer.Stop()
	}

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

	// Close provider registry
	if providerRegistry != nil {
		if err := providerRegistry.Close(); err != nil {
			logger.Error("provider registry close error", zap.Error(err))
		} else {
			logger.Info("provider registry closed")
		}
	}

	// Close database connection
	if dbStore != nil {
		if err := dbStore.Close(); err != nil {
			logger.Error("database close error", zap.Error(err))
		} else {
			logger.Info("database connection closed")
		}
	}

	logger.Info("marionette server stopped")
}

// initProviderRegistry creates and configures the provider registry.
func initProviderRegistry(s store.Store, cfg *config.Config, logger *zap.Logger) *provider.Registry {
	// Create registry with store for database-backed provider configs
	registry := provider.NewRegistry(s)

	// Register Docker provider factory
	registry.RegisterFactory("docker", func(cfg *store.ProviderConfig) (provider.Provider, error) {
		return docker.New(cfg)
	})

	// Load default Docker provider from YAML config if specified
	if cfg.Providers.Default == "docker" && cfg.Providers.Docker != nil {
		dockerCfg := cfg.Providers.Docker
		providerCfg := &store.ProviderConfig{
			Name:     "docker-default",
			Provider: "docker",
			Config:   dockerConfigToJSON(dockerCfg),
		}

		p, err := docker.New(providerCfg)
		if err != nil {
			logger.Error("failed to create default Docker provider", zap.Error(err))
		} else {
			if err := registry.Register("docker-default", p); err != nil {
				logger.Error("failed to register default Docker provider", zap.Error(err))
			} else {
				registry.SetDefault("docker-default")
				logger.Info("default Docker provider registered",
					zap.String("image", dockerCfg.Image),
					zap.String("network", dockerCfg.Network),
				)
			}
		}
	}

	return registry
}

// dockerConfigToJSON converts DockerProviderConfig to JSON for the provider.
func dockerConfigToJSON(cfg *config.DockerProviderConfig) json.RawMessage {
	data := map[string]interface{}{
		"host":    cfg.Host,
		"image":   cfg.Image,
		"network": cfg.Network,
	}
	if cfg.Resources.Memory != "" || cfg.Resources.CPUs != "" {
		data["resources"] = map[string]string{
			"memory": cfg.Resources.Memory,
			"cpus":   cfg.Resources.CPUs,
		}
	}
	b, _ := json.Marshal(data)
	return b
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
