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
	"github.com/chunlea/marionette/pkg/jobs"
	"github.com/chunlea/marionette/pkg/observability/health"
	"github.com/chunlea/marionette/pkg/observability/metrics"
	"github.com/chunlea/marionette/pkg/provider"
	"github.com/chunlea/marionette/pkg/provider/docker"
	"github.com/chunlea/marionette/pkg/provider/e2b"
	"github.com/chunlea/marionette/pkg/provider/kubernetes"
	"github.com/chunlea/marionette/pkg/provider/pool"
	"github.com/chunlea/marionette/pkg/server/admin"
	"github.com/chunlea/marionette/pkg/server/api"
	"github.com/chunlea/marionette/pkg/server/core"
	grpcserver "github.com/chunlea/marionette/pkg/server/grpc"
	"github.com/chunlea/marionette/pkg/store"
	"github.com/chunlea/marionette/pkg/store/postgres"
	"github.com/chunlea/marionette/pkg/streaming"
	"github.com/chunlea/marionette/pkg/streaming/browser"
	"github.com/chunlea/marionette/pkg/tunnel"
	"github.com/chunlea/marionette/pkg/webhook"
	"go.uber.org/zap"
)

func main() {
	// Parse command-line flags
	configPath := flag.String("config", "configs/local.yaml", "path to config file")
	devInsecureAdmin := flag.Bool("dev-insecure-admin", false,
		"serve the admin API without authentication (development only)")
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
		zap.Bool("metrics_enabled", cfg.Observability.Metrics.Enabled),
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

	// Create core managers (only if database is available).
	// core.Wire is the single production wiring point: every manager and every
	// background job is built there, so the binary cannot drift away from what
	// the tests exercise.
	var app *core.App
	var connManager *grpcserver.ConnectionManager
	var apiOpts []api.Option
	var apiKeySvc *auth.APIKeyService
	var grpcOpts []grpcserver.ServerOption
	var grpcCfg grpcserver.Config
	var webhookDeliveryJob *jobs.WebhookDeliveryJob

	if dbStore != nil {
		// Create API key service (needed for both public and admin APIs)
		apiKeySvc = auth.NewAPIKeyService(dbStore, id.APIKey)

		// Create connection manager first: it is both the connectivity oracle
		// and the command sender for every core manager.
		connManager = grpcserver.NewConnectionManager(logger)

		// Create audit logger
		auditStoreAdapter := audit.NewStoreAdapter(dbStore)
		auditLog := audit.NewLogger(auditStoreAdapter)

		runnerTokenSvc := auth.NewRunnerTokenService(dbStore, id.RunnerToken)

		app, err = core.Wire(core.WireDeps{
			Store:              dbStore,
			ConnManager:        connManager,
			CmdSender:          connManager,
			RunnerTokenService: runnerTokenSvc,
			ProviderRegistry:   providerRegistry,
			AuditLog:           auditLog,
			Logger:             logger,
			WorkspaceConfig:    cfg.Storage.Workspace,
			WebhookConfig:      webhookConfig(),
		})
		if err != nil {
			logger.Fatal("failed to wire core services", zap.Error(err))
		}

		// Create adapters and add to API options
		sessionAdapter := api.NewSessionAdapter(app.Sessions, app.Workspaces)
		taskAdapter := api.NewTaskAdapter(app.Tasks, dbStore)
		permAdapter := api.NewPermissionAdapter(app.Permissions)
		workspaceAdapter := api.NewWorkspaceAdapter(app.Workspaces)
		scheduledTaskAdapter := api.NewScheduledTaskAdapter(app.ScheduledTasks)

		apiOpts = append(apiOpts,
			api.WithSessionService(sessionAdapter),
			api.WithTaskService(taskAdapter),
			api.WithPermissionService(permAdapter),
			api.WithWorkspaceService(workspaceAdapter),
			api.WithAPIKeyService(apiKeySvc),
			api.WithScheduledTaskService(scheduledTaskAdapter),
			// These three routes answered 501 for the life of the project
			// because their services were never wired.
			api.WithRunnerService(newRunnerServiceAdapter(dbStore)),
			api.WithLogStreamService(newLogStreamAdapter(dbStore, app.LogSubscribers, logger)),
			api.WithEventStreamService(newEventStreamAdapter(app.Events, logger)),
		)

		baseURL := fmt.Sprintf("http://%s:%d", cfg.Server.API.Host, cfg.Server.API.Port)
		if cfg.Server.API.Host == "" || cfg.Server.API.Host == "0.0.0.0" {
			baseURL = fmt.Sprintf("http://localhost:%d", cfg.Server.API.Port)
		}

		// The tunnel subsystem is gated by tunnels.enabled (default true).
		// When off, no tunnel routes are mounted and the message router has no
		// tunnel router to forward runner data to.
		var tunnelRouter *grpcserver.TunnelRouter
		if cfg.Tunnels.Enabled {
			tunnelRouter = wireTunnels(wireTunnelsDeps{
				store:       dbStore,
				connManager: connManager,
				apiKeySvc:   apiKeySvc,
				baseURL:     baseURL,
				logger:      logger,
			}, &apiOpts)
		} else {
			logger.Info("tunnel subsystem disabled (tunnels.enabled=false)")
		}

		// Create browser stream provider
		browserStreamProvider := browser.NewBrowserStreamProvider(browser.BrowserStreamProviderConfig{
			BaseURL: baseURL,
			Logger:  logger,
		})

		// Create browser stream handler for gRPC
		browserStreamHandler := grpcserver.NewBrowserStreamHandler(browserStreamProvider, logger)

		// Create browser stream adapter for API
		browserStreamAdapter := api.NewBrowserStreamAdapter(browserStreamProvider)

		apiOpts = append(apiOpts,
			api.WithBrowserStreamService(browserStreamAdapter),
		)
		logger.Info("browser stream service wired to API")

		// The message router lives in the gRPC package but is built here, from
		// the managers core.Wire produced, so the gRPC server never constructs
		// a half-wired manager of its own.
		routerOpts := []grpcserver.MessageRouterOption{
			grpcserver.WithMRStore(dbStore),
			grpcserver.WithMRPermissionManager(app.Permissions),
			grpcserver.WithMRTaskManager(app.Tasks),
			grpcserver.WithMRSessionManager(app.Sessions),
		}
		if tunnelRouter != nil {
			routerOpts = append(routerOpts, grpcserver.WithMRTunnelRouter(tunnelRouter))
		}
		messageRouter := grpcserver.NewMessageRouter(logger, app.Runners, routerOpts...)

		grpcCfg.RunnerManager = app.Runners
		grpcCfg.RunnerRegistry = app.RunnerRegistry
		grpcCfg.RunnerTokenService = runnerTokenSvc
		grpcCfg.MessageRouter = messageRouter
		grpcCfg.LogSubscribers = app.LogSubscribers

		// Wire gRPC server options
		grpcOpts = append(grpcOpts,
			grpcserver.WithConnManager(connManager),
			grpcserver.WithBrowserStream(browserStreamHandler),
		)

		logger.Info("core services initialized and wired to API")
	}

	// Create metrics registry and middleware (if enabled)
	var metricsRegistry *metrics.Registry
	var metricsServer *metrics.Server
	if cfg.Observability.Metrics.Enabled {
		metricsRegistry = metrics.NewRegistry(cfg.Observability.Metrics.Namespace)

		// Add metrics middleware to API and Admin servers
		apiOpts = append(apiOpts, api.WithMiddleware(metrics.HTTPMiddleware(metricsRegistry)))

		// Add gRPC metrics interceptors
		grpcOpts = append(grpcOpts,
			grpcserver.WithUnaryInterceptor(metrics.UnaryServerInterceptor(metricsRegistry)),
			grpcserver.WithStreamInterceptor(metrics.StreamServerInterceptor(metricsRegistry)),
		)

		logger.Info("metrics middleware configured",
			zap.String("namespace", cfg.Observability.Metrics.Namespace),
		)
	}

	// Create stream manager (for desktop streaming).
	//
	// Streaming is frozen (decision D1): the SFU has no media source, no
	// renegotiation and never reads RTCP, so it cannot deliver a frame. It
	// stays compiled but registers nothing unless an operator opts in with
	// streaming.enabled.
	var streamMgr *core.StreamManager
	if dbStore != nil && cfg.Streaming.Enabled {
		var err error
		streamMgr, err = core.NewStreamManager(core.DefaultStreamManagerConfig(), dbStore, logger)
		if err != nil {
			logger.Error("failed to create stream manager", zap.Error(err))
		} else {
			// Register SFU provider for desktop streaming
			signalingBaseURL := fmt.Sprintf("ws://%s:%d/admin/api/v1/signaling",
				cfg.Server.Admin.Host, cfg.Server.Admin.Port)
			if cfg.Server.Admin.Host == "" || cfg.Server.Admin.Host == "0.0.0.0" {
				signalingBaseURL = fmt.Sprintf("ws://localhost:%d/admin/api/v1/signaling",
					cfg.Server.Admin.Port)
			}

			sfuProvider := streaming.NewSFUProvider(streaming.SFUProviderConfig{
				SignalingBaseURL: signalingBaseURL,
			})
			if err := streamMgr.RegisterProvider(sfuProvider); err != nil {
				logger.Error("failed to register SFU provider", zap.Error(err))
			} else {
				logger.Info("SFU provider registered",
					zap.String("signaling_url", signalingBaseURL),
				)
			}

			logger.Info("stream manager created")
		}
	} else if dbStore != nil {
		logger.Info("streaming subsystem disabled (streaming.enabled=false)")
	}

	// Create servers
	apiServer := api.New(api.Config{
		Host: cfg.Server.API.Host,
		Port: cfg.Server.API.Port,
	}, logger, apiOpts...)

	// Create health checker
	healthChecker := health.NewChecker()

	// Register database health check if database is available
	if dbStore != nil {
		healthChecker.Register("database", health.DatabaseCheck(dbStore))
	}

	// Register connection manager health check if available
	if connManager != nil {
		healthChecker.Register("grpc_connections", health.ConnectionManagerCheck(connManager))
	}

	// Create admin server options
	var adminOpts []admin.Option
	adminOpts = append(adminOpts, admin.WithHealthService(healthChecker))

	// Add metrics middleware to admin server (if enabled)
	if metricsRegistry != nil {
		adminOpts = append(adminOpts, admin.WithMiddleware(metrics.HTTPMiddleware(metricsRegistry)))
	}

	if apiKeySvc != nil {
		// Create adapter for admin API using existing API key service
		apiKeyAdapter := admin.NewAPIKeyAdapter(apiKeySvc)
		adminOpts = append(adminOpts, admin.WithAPIKeyService(apiKeyAdapter))
		logger.Info("API key service wired to Admin API")
	}
	if app != nil {
		adminOpts = append(adminOpts, admin.WithSessionActivator(app.Sessions))
		logger.Info("Session activator wired to Admin API")
	}
	if dbStore != nil {
		// Create action log service adapter for admin API
		actionLogAdapter := admin.NewActionLogStoreAdapter(dbStore)
		adminOpts = append(adminOpts, admin.WithActionLogService(actionLogAdapter))
		logger.Info("Action log service wired to Admin API")

		// Create runner token service for admin API
		runnerTokenAdapter := admin.NewRunnerTokenAdapter(auth.NewRunnerTokenService(dbStore, id.RunnerToken))
		adminOpts = append(adminOpts, admin.WithRunnerTokenAdminService(runnerTokenAdapter))
		logger.Info("Runner token service wired to Admin API")

		// Create profile service for admin API
		profileAdapter := admin.NewProfileAdapter(dbStore)
		adminOpts = append(adminOpts, admin.WithProfileService(profileAdapter))
		logger.Info("Profile service wired to Admin API")
	}
	if app != nil {
		// The webhook manager and its integration are built by core.Wire so the
		// managers get them at construction time instead of through setters.
		webhookAdapter := admin.NewWebhookAdapter(app.Webhooks)
		adminOpts = append(adminOpts, admin.WithWebhookService(webhookAdapter))
		logger.Info("Webhook service wired to Admin API")

		// Start webhook delivery job
		webhookDeliveryJob = jobs.NewWebhookDeliveryJob(app.Webhooks, jobs.WebhookDeliveryJobConfig{
			Interval:  5 * time.Second,
			BatchSize: 100,
			Logger:    logger.Named("webhook-delivery"),
		})
		if err := webhookDeliveryJob.Start(app.Context()); err != nil {
			logger.Error("failed to start webhook delivery job", zap.Error(err))
			webhookDeliveryJob = nil
		} else {
			logger.Info("Webhook delivery job started")
		}
	}
	if streamMgr != nil {
		// Create streams handler for admin API
		// Pass connManager to enable sending StartDesktopStream commands to agents
		streamsHandler := admin.NewStreamsHandler(streamMgr, connManager, logger)
		adminOpts = append(adminOpts, admin.WithStreamsHandler(streamsHandler))
		logger.Info("Streams handler wired to Admin API")

		// Create signaling handler for WebRTC
		sfuHandler := streamMgr.GetSignalingHandler()
		if sfuHandler != nil {
			signalingHandler := admin.NewSignalingHandler(
				sfuHandler,
				admin.DefaultSignalingConfig(),
				logger,
			)
			adminOpts = append(adminOpts, admin.WithSignalingHandler(signalingHandler))
			logger.Info("Signaling handler wired to Admin API")
		}
	}

	// The admin API mints API keys, registers runners and reads every session.
	// It fails closed: without credentials the server refuses to start unless
	// --dev-insecure-admin says otherwise.
	adminServer, err := admin.New(admin.Config{
		Host:          cfg.Server.Admin.Host,
		Port:          cfg.Server.Admin.Port,
		Username:      secrets.UIUsername,
		Password:      secrets.UIPassword,
		AllowInsecure: *devInsecureAdmin,
	}, logger, adminOpts...)
	if err != nil {
		logger.Fatal("failed to create admin server", zap.Error(err))
	}

	grpcCfg.Host = cfg.Server.GRPC.Host
	grpcCfg.Port = cfg.Server.GRPC.Port
	grpcCfg.TLS = &cfg.TLS
	grpcCfg.Store = dbStore

	grpcServer, err := grpcserver.New(grpcCfg, logger, grpcOpts...)
	if err != nil {
		logger.Fatal("failed to create gRPC server", zap.Error(err))
	}

	// Create metrics server (if enabled)
	if metricsRegistry != nil {
		metricsServer = metrics.NewServer(metricsRegistry, metrics.ServerConfig{
			Host: cfg.Observability.Metrics.Host,
			Port: cfg.Observability.Metrics.Port,
			Path: cfg.Observability.Metrics.Path,
		}, logger)
	}

	// Start servers in goroutines
	errChan := make(chan error, 4) // 4 servers now (api, admin, grpc, metrics)

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

	// Start metrics server if enabled
	if metricsServer != nil {
		go func() {
			if err := metricsServer.Start(); err != nil {
				errChan <- fmt.Errorf("metrics server: %w", err)
			}
		}()
	}

	// Register service statuses
	admin.Registry.Register("Public API", cfg.Server.API.Port, "ok", "Running")
	admin.Registry.Register("Admin API", cfg.Server.Admin.Port, "ok", "Running")
	admin.Registry.Register("gRPC", cfg.Server.GRPC.Port, "ok", "Running")
	if metricsServer != nil {
		admin.Registry.Register("Metrics", cfg.Observability.Metrics.Port, "ok", "Running")
	}

	logFields := []zap.Field{
		zap.String("api_addr", fmt.Sprintf("%s:%d", cfg.Server.API.Host, cfg.Server.API.Port)),
		zap.String("admin_addr", fmt.Sprintf("%s:%d", cfg.Server.Admin.Host, cfg.Server.Admin.Port)),
		zap.String("grpc_addr", fmt.Sprintf("%s:%d", cfg.Server.GRPC.Host, cfg.Server.GRPC.Port)),
	}
	if metricsServer != nil {
		logFields = append(logFields, zap.String("metrics_addr", metricsServer.Addr()))
	}
	logger.Info("all servers started", logFields...)

	// Start the core background jobs (stale detector, task/permission timeout
	// enforcers, scheduled task executor, scheduled session activator).
	if app != nil {
		if err := app.Start(context.Background()); err != nil {
			logger.Error("failed to start core background jobs", zap.Error(err))
		} else {
			logger.Info("core background jobs started")
		}
	}

	// Start stream manager if available
	if streamMgr != nil {
		if err := streamMgr.Start(context.Background()); err != nil {
			logger.Error("failed to start stream manager", zap.Error(err))
		} else {
			logger.Info("stream manager started")
		}
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

	// Drain the core background jobs first so nothing keeps writing while the
	// servers and the database go away.
	if app != nil {
		if err := app.Stop(ctx); err != nil {
			logger.Error("core background jobs stop error", zap.Error(err))
		} else {
			logger.Info("core background jobs stopped")
		}
	}

	if webhookDeliveryJob != nil {
		if err := webhookDeliveryJob.Stop(ctx); err != nil {
			logger.Error("webhook delivery job stop error", zap.Error(err))
		}
	}

	// Stop stream manager
	if streamMgr != nil {
		if err := streamMgr.Stop(ctx); err != nil {
			logger.Error("stream manager stop error", zap.Error(err))
		} else {
			logger.Info("stream manager stopped")
		}
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
	if metricsServer != nil {
		if err := metricsServer.Shutdown(ctx); err != nil {
			logger.Error("metrics server shutdown error", zap.Error(err))
		}
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

// wireTunnelsDeps are the pieces the tunnel subsystem needs.
type wireTunnelsDeps struct {
	store       store.Store
	connManager *grpcserver.ConnectionManager
	apiKeySvc   *auth.APIKeyService
	baseURL     string
	logger      *zap.Logger
}

// wireTunnels builds the tunnel manager, router and HTTP handlers and appends
// the tunnel API options. It returns the router so the gRPC message router can
// forward runner tunnel data to it.
func wireTunnels(deps wireTunnelsDeps, apiOpts *[]api.Option) *grpcserver.TunnelRouter {
	tunnelMgr := tunnel.NewTunnelManager(
		tunnel.WithLogger(deps.logger),
		tunnel.WithBaseURL(deps.baseURL),
	)

	tunnelRouter := grpcserver.NewTunnelRouter(
		grpcserver.WithTRLogger(deps.logger),
		grpcserver.WithTRConnectionManager(deps.connManager),
		grpcserver.WithTRTunnelManager(tunnelMgr),
	)

	tunnelProxyAdapter := api.NewTunnelProxyAdapter(
		api.WithTPALogger(deps.logger),
		api.WithTPATunnelManager(tunnelMgr),
		api.WithTPATunnelRouter(tunnelRouter),
	)

	tunnelProxyHandler := api.NewTunnelProxyHandler(
		api.WithTPLogger(deps.logger),
		api.WithTPService(tunnelProxyAdapter),
		api.WithTPAPIKeyAuth(func(r *http.Request) (bool, error) {
			// Check X-Marionette-API-Key first (brand-prefixed header), then
			// fall back to X-API-Key for backwards compatibility.
			key := r.Header.Get("X-Marionette-API-Key")
			if key == "" {
				key = r.Header.Get("X-API-Key")
			}
			if key == "" {
				return false, nil
			}
			_, err := deps.apiKeySvc.Validate(r.Context(), key)
			return err == nil, nil
		}),
	)

	tunnelAdapter := api.NewTunnelAdapter(
		api.WithTALogger(deps.logger),
		api.WithTATunnelManager(tunnelMgr),
		api.WithTATunnelRouter(tunnelRouter),
		api.WithTAStore(deps.store),
	)

	*apiOpts = append(*apiOpts,
		api.WithTunnelProxy(tunnelProxyHandler),
		api.WithTunnelService(tunnelAdapter),
	)
	deps.logger.Info("tunnel proxy handler wired to API",
		zap.String("base_url", deps.baseURL),
	)

	return tunnelRouter
}

// webhookConfig returns the webhook delivery configuration.
// These values are not user-configurable yet; keeping them in one function
// makes the eventual move into config.yaml a single edit.
func webhookConfig() webhook.Config {
	return webhook.Config{
		DefaultMaxRetries:        3,
		DefaultRetryDelaySeconds: 60,
		DefaultTimeoutSeconds:    30,
		MaxPayloadSize:           10 * 1024 * 1024, // 10MB
		UserAgent:                "Marionette-Webhook/1.0",
		WorkerCount:              4,
		BatchSize:                100,
	}
}

// initProviderRegistry creates and configures the provider registry.
func initProviderRegistry(s store.Store, cfg *config.Config, logger *zap.Logger) *provider.Registry {
	// Create registry with store for database-backed provider configs
	registry := provider.NewRegistry(s)

	// Register Docker provider factory
	registry.RegisterFactory("docker", func(cfg *store.ProviderConfig) (provider.Provider, error) {
		return docker.New(cfg)
	})

	// Register Kubernetes provider factory
	registry.RegisterFactory("kubernetes", func(cfg *store.ProviderConfig) (provider.Provider, error) {
		return kubernetes.New(cfg)
	})

	// Register Pool provider factory
	registry.RegisterFactory("pool", pool.NewProviderFactory(s, logger))

	// Register E2B provider factory
	registry.RegisterFactory("e2b", e2b.NewProviderFactory())

	// Load default provider from YAML config based on configured default
	switch cfg.Providers.Default {
	case "docker":
		if cfg.Providers.Docker != nil {
			registerDefaultDockerProvider(registry, cfg.Providers.Docker, logger)
		}
	case "kubernetes":
		if cfg.Providers.Kubernetes != nil {
			registerDefaultKubernetesProvider(registry, cfg.Providers.Kubernetes, logger)
		}
	}

	return registry
}

// registerDefaultDockerProvider registers the default Docker provider from config.
func registerDefaultDockerProvider(registry *provider.Registry, dockerCfg *config.DockerProviderConfig, logger *zap.Logger) {
	providerCfg := &store.ProviderConfig{
		Name:     "docker-default",
		Provider: "docker",
		Config:   dockerConfigToJSON(dockerCfg),
	}

	p, err := docker.New(providerCfg)
	if err != nil {
		logger.Error("failed to create default Docker provider", zap.Error(err))
		return
	}

	if err := registry.Register("docker-default", p); err != nil {
		logger.Error("failed to register default Docker provider", zap.Error(err))
		return
	}

	registry.SetDefault("docker-default")
	logger.Info("default Docker provider registered",
		zap.String("image", dockerCfg.Image),
		zap.String("network", dockerCfg.Network),
	)
}

// registerDefaultKubernetesProvider registers the default Kubernetes provider from config.
func registerDefaultKubernetesProvider(registry *provider.Registry, k8sCfg *config.KubernetesProviderConfig, logger *zap.Logger) {
	providerCfg := &store.ProviderConfig{
		Name:     "kubernetes-default",
		Provider: "kubernetes",
		Config:   kubernetesConfigToJSON(k8sCfg),
	}

	p, err := kubernetes.New(providerCfg)
	if err != nil {
		logger.Error("failed to create default Kubernetes provider", zap.Error(err))
		return
	}

	if err := registry.Register("kubernetes-default", p); err != nil {
		logger.Error("failed to register default Kubernetes provider", zap.Error(err))
		return
	}

	registry.SetDefault("kubernetes-default")
	logger.Info("default Kubernetes provider registered",
		zap.String("namespace", k8sCfg.Namespace),
		zap.String("image", k8sCfg.Image),
	)
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

// kubernetesConfigToJSON converts KubernetesProviderConfig to JSON for the provider.
func kubernetesConfigToJSON(cfg *config.KubernetesProviderConfig) json.RawMessage {
	data := map[string]interface{}{
		"kubeconfig":      cfg.Kubeconfig,
		"context":         cfg.Context,
		"namespace":       cfg.Namespace,
		"image":           cfg.Image,
		"service_account": cfg.ServiceAccount,
	}

	// Resources
	if cfg.Resources.Memory != "" || cfg.Resources.CPUs != "" {
		resources := map[string]string{}
		if cfg.Resources.Memory != "" {
			resources["memory"] = cfg.Resources.Memory
		}
		if cfg.Resources.MemoryRequest != "" {
			resources["memory_request"] = cfg.Resources.MemoryRequest
		}
		if cfg.Resources.CPUs != "" {
			resources["cpus"] = cfg.Resources.CPUs
		}
		if cfg.Resources.CPURequest != "" {
			resources["cpu_request"] = cfg.Resources.CPURequest
		}
		data["resources"] = resources
	}

	// Storage
	if cfg.Storage.Size != "" {
		storage := map[string]string{
			"size": cfg.Storage.Size,
		}
		if cfg.Storage.StorageClass != "" {
			storage["storage_class"] = cfg.Storage.StorageClass
		}
		if cfg.Storage.AccessMode != "" {
			storage["access_mode"] = cfg.Storage.AccessMode
		}
		data["storage"] = storage
	}

	// Node selector
	if len(cfg.NodeSelector) > 0 {
		data["node_selector"] = cfg.NodeSelector
	}

	// Tolerations
	if len(cfg.Tolerations) > 0 {
		tolerations := make([]map[string]string, len(cfg.Tolerations))
		for i, t := range cfg.Tolerations {
			tolerations[i] = map[string]string{
				"key":      t.Key,
				"operator": t.Operator,
				"value":    t.Value,
				"effect":   t.Effect,
			}
		}
		data["tolerations"] = tolerations
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
