// Package main provides the marionette server binary.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/chunlea/marionette/pkg/audit"
	"github.com/chunlea/marionette/pkg/auth"
	"github.com/chunlea/marionette/pkg/config"
	"github.com/chunlea/marionette/pkg/cryptoutil"
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
	"github.com/chunlea/marionette/pkg/storage"
	"github.com/chunlea/marionette/pkg/storage/cas"
	"github.com/chunlea/marionette/pkg/store"
	"github.com/chunlea/marionette/pkg/store/postgres"
	"github.com/chunlea/marionette/pkg/streaming"
	"github.com/chunlea/marionette/pkg/streaming/browser"
	"github.com/chunlea/marionette/pkg/tunnel"
	"github.com/chunlea/marionette/pkg/webhook"
	"github.com/prometheus/client_golang/prometheus"
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
			MultiTenant:     cfg.MultiTenant,
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

	// The metrics registry is built before the core managers because they
	// register collectors of their own; the middleware and the scrape endpoint
	// are attached further down, once the servers exist.
	var metricsRegistry *metrics.Registry
	var metricsServer *metrics.Server
	if cfg.Observability.Metrics.Enabled {
		metricsRegistry = metrics.NewRegistry(cfg.Observability.Metrics.Namespace)
	}
	var metricsRegisterer prometheus.Registerer
	if metricsRegistry != nil {
		metricsRegisterer = metricsRegistry.PrometheusRegistry()
	}

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
	var logArchive logArchiving
	var replicaRegistry *core.ReplicaRegistry
	var peerForwarder *grpcserver.PeerForwarder

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

		chunkGC, chunkTenants := initChunkGC(cfg, secrets, dbStore, logger)
		logArchive = initLogArchiving(cfg, secrets, dbStore, logger)

		// The replica registry is built before the core managers because they
		// need its id: the routing pointers it publishes and the live fan-out's
		// notices have to name this process the same way, or a replica would
		// filter out its own notices under one id and publish them under
		// another.
		routingMetrics := grpcserver.NewRoutingMetrics(metricsRegisterer, cfg.Observability.Metrics.Namespace)
		replicaRegistry, err = core.NewReplicaRegistry(dbStore, core.ReplicaRegistryConfig{
			AdvertiseAddr:       replicaAdvertiseAddr(cfg, logger),
			ObserveReplicaCount: routingMetrics.SetLiveReplicas,
		}, logger.Named("replica-registry"))
		if err != nil {
			logger.Fatal("failed to build the replica registry", zap.Error(err))
		}

		app, err = core.Wire(core.WireDeps{
			Store:              dbStore,
			ChunkGC:            chunkGC,
			ChunkTenants:       chunkTenants,
			ConnManager:        connManager,
			CmdSender:          connManager,
			RunnerTokenService: runnerTokenSvc,
			ProviderRegistry:   providerRegistry,
			RunnerServerURL:    runnerServerURL(cfg, logger),
			AuditLog:           auditLog,
			Logger:             logger,
			ReplicaID:          replicaRegistry.ID(),
			MetricsRegisterer:  metricsRegisterer,
			MetricsNamespace:   cfg.Observability.Metrics.Namespace,
			WorkspaceConfig:    cfg.Storage.Workspace,
			WebhookConfig:      webhookConfig(),
			Jobs: core.JobsConfig{
				DisableChunkGC:  !cfg.Storage.GC.Enabled,
				ChunkGCInterval: cfg.Storage.GC.Interval,
				// Zero unless the archiver is actually running. The drop is
				// archive-gated as well, so this is the second of two locks on
				// deleting the only copy of the logs.
				LogRetentionDays: logArchive.RetentionDays,
			},
		})
		if err != nil {
			logger.Fatal("failed to wire core services", zap.Error(err))
		}

		// Create adapters and add to API options
		sessionAdapter := api.NewSessionAdapter(app.Sessions, app.Workspaces,
			api.WithSessionLogReader(logArchive.Reader))
		taskAdapter := api.NewTaskAdapter(app.Tasks, dbStore,
			api.WithTaskLogArchive(logArchive.Reader))
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
				tunnelStore: dbStore,
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

		// Cross-replica command routing.
		//
		// A runner's control stream lives in one process, so without this a
		// second replica cannot reach it and every ExecuteTask, DetachSession
		// and permission response between them fails. The registry (built
		// above) publishes which process holds which stream; the forwarder is
		// the one hop that uses it.
		//
		// A single-process deployment pays for the heartbeat and nothing else:
		// SendCommand answers from the local map every time and never reaches
		// the locator.
		peerCredential := grpcserver.DerivePeerCredential(secrets.MasterKey)
		if peerCredential == "" {
			logger.Warn("cross-replica command routing is disabled: " +
				"MARIONETTE_MASTER_KEY is not set, so peer replicas cannot be authenticated. " +
				"Single-replica deployments are unaffected")
		}
		peerForwarder = grpcserver.NewPeerForwarder(
			peerCredential, replicaRegistry.ID(), logger.Named("peer-forwarder"))

		connManager.SetRouter(replicaLocator{registry: replicaRegistry}, peerForwarder, routingMetrics)

		grpcCfg.ConnectionBinder = replicaRegistry
		grpcCfg.PeerCredential = peerCredential
		grpcCfg.RoutingMetrics = routingMetrics

		// Wire gRPC server options
		grpcOpts = append(grpcOpts,
			grpcserver.WithConnManager(connManager),
			grpcserver.WithBrowserStream(browserStreamHandler),
		)

		logger.Info("core services initialized and wired to API")
	}

	// Attach the metrics middleware (if enabled)
	if metricsRegistry != nil {
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
		Host:        cfg.Server.API.Host,
		Port:        cfg.Server.API.Port,
		MultiTenant: cfg.MultiTenant,
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

	// Create admin server options.
	//
	// The services themselves are assembled by buildAdminServices, which
	// returns one value per admin.WithX option so a test can assert that the
	// production binary attaches all of them. Wiring by append is how
	// provider-configs came to answer 501 for the life of the project.
	adminOpts := buildAdminServices(adminDeps{
		store:       storeOrNil(dbStore),
		app:         app,
		apiKeys:     apiKeySvc,
		crypto:      initCredentialCrypto(secrets, dbStore, logger),
		health:      healthChecker,
		streamMgr:   streamMgr,
		connManager: connManager,
		logger:      logger,
	}).options()

	// Add metrics middleware to admin server (if enabled)
	if metricsRegistry != nil {
		adminOpts = append(adminOpts, admin.WithMiddleware(metrics.HTTPMiddleware(metricsRegistry)))
	}

	if app != nil {
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

		// The archiver runs on the App context, which carries system access:
		// it archives every tenant's sessions and has no request to take a
		// tenant from.
		if logArchive.Archiver != nil {
			if err := logArchive.Archiver.Start(app.Context()); err != nil {
				logger.Error("failed to start log archiver", zap.Error(err))
				logArchive.Archiver = nil
			}
		}
	}
	// The dashboard is served by the admin server but calls a relative
	// /api/v1, which lives on the public API port. Forwarding that prefix
	// gives the browser one origin: no CORS, no build-time URL baking, and
	// WebSocket upgrades relay through. The direction matters - the admin API
	// mints API keys and registers runners, so it must not be reachable
	// through the public port, whereas the public API authenticates every
	// request itself.
	apiProxy, err := api.NewUpstreamProxy("/api/v1", localAPIAddr(cfg.Server.API), logger)
	if err != nil {
		logger.Fatal("failed to create the API proxy", zap.Error(err))
	}
	// Mounted ahead of the other admin middleware so proxied traffic is not
	// counted as admin traffic, and so a relayed WebSocket does not sit in the
	// admin latency histogram for its whole lifetime.
	adminOpts = append([]admin.Option{admin.WithMiddleware(apiProxy)}, adminOpts...)

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

	// Announce this replica once the gRPC listener is up, so a peer that reads
	// the row and dials immediately finds something listening.
	if replicaRegistry != nil {
		if err := replicaRegistry.Start(context.Background()); err != nil {
			logger.Error("failed to start the replica registry", zap.Error(err))
		}
	}

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

	if logArchive.Archiver != nil {
		if err := logArchive.Archiver.Stop(ctx); err != nil {
			logger.Error("log archiver stop error", zap.Error(err))
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

	// Withdraw this replica from the routing table before the listener goes,
	// so peers stop forwarding to a process that can no longer deliver.
	// Without it they would keep trying until the heartbeat expires, which
	// turns every rolling restart into a window of failed commands.
	if replicaRegistry != nil {
		replicaRegistry.Stop(ctx)
	}
	if peerForwarder != nil {
		peerForwarder.Close()
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
	store store.Store
	// tunnelStore persists tunnels. It is a narrower interface than
	// store.Store because DeleteExpiredTunnels lives only on the Postgres
	// store. Nil leaves tunnels memory-only, which is what production ran
	// with until this was wired.
	tunnelStore tunnelStore
	connManager *grpcserver.ConnectionManager
	apiKeySvc   *auth.APIKeyService
	baseURL     string
	logger      *zap.Logger
}

// newTunnelManager builds the tunnel manager the way production does.
//
// Separated from wireTunnels so a test can assert the property that was wrong
// for the life of the project: the manager production builds must persist.
// Without a store it keeps tunnels in memory only - Create never writes the
// tunnels table, every read-through path dead-ends, and a restart loses every
// open tunnel while the URLs already handed out keep being answered with
// "tunnel not found".
func newTunnelManager(deps wireTunnelsDeps) *tunnel.TunnelManager {
	opts := []tunnel.ManagerOption{
		tunnel.WithLogger(deps.logger),
		tunnel.WithBaseURL(deps.baseURL),
	}

	if deps.tunnelStore != nil {
		opts = append(opts, tunnel.WithStore(newTunnelStoreAdapter(deps.tunnelStore)))
		deps.logger.Info("tunnel persistence enabled")
	} else {
		deps.logger.Warn("no database: tunnels are memory-only and will not survive a restart")
	}

	return tunnel.NewTunnelManager(opts...)
}

// wireTunnels builds the tunnel manager, router and HTTP handlers and appends
// the tunnel API options. It returns the router so the gRPC message router can
// forward runner tunnel data to it.
func wireTunnels(deps wireTunnelsDeps, apiOpts *[]api.Option) *grpcserver.TunnelRouter {
	tunnelMgr := newTunnelManager(deps)

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

// localAPIAddr is the address the admin server dials to reach the public API.
//
// Both servers live in one process, so this is a loopback call. The configured
// host is a bind address, not a dial target: the usual 0.0.0.0 (or an empty
// value, or the IPv6 wildcard) cannot be dialled, so those become the
// loopback. A concrete bind address is used as-is, because an API bound to one
// interface may not be listening on the loopback at all.
func localAPIAddr(cfg config.EndpointConfig) string {
	host := cfg.Host
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}
	return fmt.Sprintf("http://%s", net.JoinHostPort(host, strconv.Itoa(cfg.Port)))
}

// runnerServerURL is the gRPC address a spawned runner dials back on.
//
// The operator-set isolation server_url wins: it is the address that already
// has to be reachable from inside a restricted runner, so it is the one that
// has been thought about. Otherwise it is derived from the gRPC listener,
// which is right for a runner on the same host and a guess anywhere else -
// hence the warning, because a wrong value here produces instances that come
// up, fail to connect, and bill until the reaper notices.
func runnerServerURL(cfg *config.Config, logger *zap.Logger) string {
	if cfg.Providers.Docker != nil && cfg.Providers.Docker.Isolation.ServerURL != "" {
		return cfg.Providers.Docker.Isolation.ServerURL
	}

	host := cfg.Server.GRPC.Host
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}
	addr := net.JoinHostPort(host, strconv.Itoa(cfg.Server.GRPC.Port))
	logger.Warn("no runner server URL configured, derived one from the gRPC listener",
		zap.String("server_url", addr),
		zap.String("configure", "providers.docker.isolation.server_url"),
	)
	return addr
}

// EnvGRPCAdvertiseAddr overrides the host:port peer replicas dial to hand this
// process a command.
//
// The default derives from the gRPC listener, which is right whenever the
// listener address is reachable from the other replicas. In Kubernetes that
// means the pod IP: set this from the downward API (status.podIP) plus the
// gRPC port. A wrong value is worse than none, because peers will route here
// and time out rather than failing fast.
const EnvGRPCAdvertiseAddr = "MARIONETTE_GRPC_ADVERTISE_ADDR"

// replicaAdvertiseAddr resolves where peers reach this process.
//
// A loopback default is correct for a single-process deployment and correct
// for the two-process tests; it is wrong for a real multi-replica deployment,
// which is why picking it is logged.
func replicaAdvertiseAddr(cfg *config.Config, logger *zap.Logger) string {
	if addr := os.Getenv(EnvGRPCAdvertiseAddr); addr != "" {
		return addr
	}

	host := cfg.Server.GRPC.Host
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}
	addr := net.JoinHostPort(host, strconv.Itoa(cfg.Server.GRPC.Port))
	logger.Info("derived the replica advertise address from the gRPC listener",
		zap.String("advertise_addr", addr),
		zap.String("configure", EnvGRPCAdvertiseAddr),
	)
	return addr
}

// replicaLocator adapts core.ReplicaRegistry to the locator the gRPC
// connection manager wants.
//
// The two Peer types are the same three fields; they are separate because core
// cannot import the gRPC package (that dependency runs the other way), and
// this is the seam where they meet.
type replicaLocator struct {
	registry *core.ReplicaRegistry
}

func (l replicaLocator) Locate(runnerID string) (grpcserver.RunnerPeer, bool) {
	peer, ok := l.registry.Locate(runnerID)
	if !ok {
		return grpcserver.RunnerPeer{}, false
	}
	return grpcserver.RunnerPeer{ReplicaID: peer.ReplicaID, Addr: peer.Addr}, true
}

// initCredentialCrypto builds the envelope crypto the admin agent-config
// routes encrypt API keys with.
//
// It returns nil rather than failing startup when there is no key: the rest of
// the server runs fine without agent configs, and buildAdminServices leaves
// those routes unwired rather than storing a credential in plaintext.
//
// It is a separate Service instance from the ones chunk GC and log archiving
// build. They share the KEK and the DEK table, so the only cost is a second
// data-key cache; concurrent creation of the same DEK is already handled in
// pkg/cryptoutil.
func initCredentialCrypto(
	secrets *config.Secrets,
	dbStore *postgres.Store,
	logger *zap.Logger,
) secretEncryptor {
	if dbStore == nil || secrets == nil || secrets.EncryptionKey == "" {
		return nil
	}

	svc, err := cryptoutil.NewService(secrets.EncryptionKey, postgres.NewDEKStore(dbStore), id.DataKey)
	if err != nil {
		logger.Error("could not build the credential crypto service; agent config routes stay disabled",
			zap.Error(err))
		return nil
	}
	return svc
}

// initChunkGC builds the content-addressed storage garbage collector.
//
// It returns nils when GC is switched off or cannot be built, which leaves the
// job unbuilt rather than failing startup: a server that cannot collect chunks
// is degraded, not broken, and refusing to boot over it would take the whole
// deployment down for a background sweep.
func initChunkGC(
	cfg *config.Config,
	secrets *config.Secrets,
	dbStore *postgres.Store,
	logger *zap.Logger,
) (cas.GarbageCollector, jobs.TenantLister) {
	if !cfg.Storage.GC.Enabled {
		return nil, nil
	}

	if secrets.EncryptionKey == "" {
		logger.Error("chunk gc is enabled but MARIONETTE_ENCRYPTION_KEY is not set; gc disabled")
		return nil, nil
	}

	// Chunks are encrypted per tenant, so the collector needs the same crypto
	// service the writers use in order to read what it is deleting.
	cryptoSvc, err := cryptoutil.NewService(secrets.EncryptionKey, postgres.NewDEKStore(dbStore), id.DataKey)
	if err != nil {
		logger.Error("chunk gc is enabled but the crypto service could not be built; gc disabled",
			zap.Error(err))
		return nil, nil
	}

	blobs, err := chunkBlobProvider(cfg)
	if err != nil {
		logger.Error("chunk gc is enabled but the blob store could not be built; gc disabled",
			zap.Error(err))
		return nil, nil
	}

	chunkStore := cas.NewBlobChunkStore(blobs, cas.NewTenantEncryptor(cryptoSvc))
	logger.Info("chunk gc enabled",
		zap.String("storage_provider", cfg.Storage.Provider),
		zap.Duration("interval", cfg.Storage.GC.Interval),
	)
	return cas.NewGC(dbStore, chunkStore, cas.GCConfig{}), dbStore
}

// chunkBlobProvider resolves the object store chunks live in.
func chunkBlobProvider(cfg *config.Config) (storage.StorageProvider, error) {
	switch cfg.Storage.Provider {
	case "local", "":
		if cfg.Storage.Local == nil || cfg.Storage.Local.Path == "" {
			return nil, fmt.Errorf("storage.local.path is required for the local provider")
		}
		return cas.NewLocalProvider(cfg.Storage.Local.Path)
	default:
		// S3 needs a client this binary does not construct yet.
		return nil, fmt.Errorf("storage provider %q is not wired for chunk gc", cfg.Storage.Provider)
	}
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
