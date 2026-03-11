package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	// Register the "postgres" database/sql driver so that
	// sql.Open("postgres", ...) works at runtime.
	_ "github.com/lib/pq"

	"github.com/randybias/tentacular-mcp/pkg/auth"
	"github.com/randybias/tentacular-mcp/pkg/exoskeleton"
	"github.com/randybias/tentacular-mcp/pkg/k8s"
	"github.com/randybias/tentacular-mcp/pkg/proxy"
	"github.com/randybias/tentacular-mcp/pkg/scheduler"
	"github.com/randybias/tentacular-mcp/pkg/server"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	tokenPath := os.Getenv("TOKEN_PATH")
	if tokenPath == "" {
		tokenPath = "/etc/tentacular-mcp/token"
	}

	token, err := auth.LoadToken(tokenPath)
	if err != nil {
		slog.Error("failed to load auth token", "error", err)
		os.Exit(1)
	}

	client, err := k8s.NewInClusterClient()
	if err != nil {
		slog.Error("failed to create kubernetes client", "error", err)
		os.Exit(1)
	}

	proxyOpts := proxy.Options{
		Namespace:   os.Getenv("PROXY_NAMESPACE"),
		Image:       os.Getenv("PROXY_IMAGE"),
		StorageSize: os.Getenv("PROXY_STORAGE_SIZE"),
	}
	if proxyOpts.Namespace == "" {
		proxyOpts.Namespace = "tentacular-support"
	}

	reconciler := proxy.NewReconciler(client, proxyOpts, logger)

	sched := scheduler.New(client, logger)

	// Load exoskeleton configuration from environment
	exoCfg, err := exoskeleton.LoadFromEnv()
	if err != nil {
		slog.Error("failed to load exoskeleton config", "error", err)
		os.Exit(1)
	}

	var exoCtrl *exoskeleton.ExoskeletonController
	var pgRegistrar *exoskeleton.PostgresRegistrar
	if exoCfg.Enabled {
		if err := exoCfg.Validate(); err != nil {
			slog.Error("exoskeleton config validation failed", "error", err)
			os.Exit(1)
		}
		slog.Info("exoskeleton enabled",
			"postgres", exoCfg.PostgresEnabled,
			"nats", exoCfg.NATSEnabled,
			"rustfs", exoCfg.RustFSEnabled,
			"cleanupOnUndeploy", exoCfg.CleanupOnUndeploy,
		)

		var pgReg exoskeleton.PostgresRegistrarI
		var natsReg exoskeleton.NATSRegistrarI

		if exoCfg.PostgresEnabled {
			pgRegistrar = exoskeleton.NewPostgresRegistrar(exoCfg.Postgres)
			if err := pgRegistrar.Connect(); err != nil {
				slog.Error("failed to connect postgres registrar", "error", err)
				os.Exit(1)
			}
			pgReg = pgRegistrar
		}

		if exoCfg.NATSEnabled {
			nr := exoskeleton.NewNATSRegistrar(exoCfg.NATS)
			nr.SetAdmin(exoskeleton.NewInMemoryNATSAdmin())
			natsReg = nr
		}

		credInj := exoskeleton.NewCredentialInjector(client.Clientset)
		exoCtrl = exoskeleton.NewExoskeletonController(exoCfg, pgReg, natsReg, credInj)
	} else {
		slog.Info("exoskeleton disabled")
	}

	srv, err := server.New(client, reconciler, sched, token, logger, exoCtrl)
	if err != nil {
		slog.Error("failed to create MCP server", "error", err)
		os.Exit(1)
	}

	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Start module proxy reconciliation loop as a background goroutine
	go reconciler.Run(ctx)

	// Start cron scheduler and scan for existing workflow schedules
	sched.Start()
	go func() {
		if err := sched.ScanWorkflows(context.Background()); err != nil {
			slog.Warn("initial cron schedule scan failed", "error", err)
		}
	}()

	go func() {
		slog.Info("starting tentacular-mcp server", "addr", addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down server")
	sched.Stop()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		slog.Error("server shutdown failed", "error", err)
		os.Exit(1)
	}

	if pgRegistrar != nil {
		if err := pgRegistrar.Close(); err != nil {
			slog.Warn("postgres registrar close failed", "error", err)
		}
	}

	slog.Info("server stopped")
}
