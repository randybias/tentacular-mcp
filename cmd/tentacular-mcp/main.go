package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/randybias/tentacular-mcp/pkg/auth"
	"github.com/randybias/tentacular-mcp/pkg/authz"
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

	// Initialize exoskeleton controller from environment configuration.
	exoCfg := exoskeleton.LoadFromEnv()
	if vErr := exoCfg.Validate(); vErr != nil {
		slog.Error("exoskeleton config validation failed", "error", vErr)
		os.Exit(1)
	}
	exoCtrl, err := exoskeleton.NewController(exoCfg, client)
	if err != nil {
		slog.Error("failed to initialize exoskeleton controller", "error", err)
		os.Exit(1)
	}
	defer exoCtrl.Close()

	// Initialize OIDC validator if auth is enabled.
	var oidcValidator *exoskeleton.OIDCValidator
	if exoCfg.AuthEnabled() {
		oidcValidator, err = exoskeleton.NewOIDCValidator(exoCfg.Auth)
		if err != nil {
			slog.Error("failed to initialize OIDC validator", "error", err)
			os.Exit(1)
		}
		slog.Info("OIDC authentication enabled", "issuer", exoCfg.Auth.IssuerURL)
	}

	// Initialize authz evaluator. TENTACULAR_AUTHZ_ENABLED=false disables all authz checks.
	eval := authz.NewEvaluator(authz.DefaultMode)
	if os.Getenv("TENTACULAR_AUTHZ_ENABLED") == "false" {
		eval.Enabled = false
		slog.Info("authz disabled via TENTACULAR_AUTHZ_ENABLED=false")
	}

	srv, err := server.New(client, reconciler, sched, exoCtrl, eval, oidcValidator, token, logger)
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

	// Start module proxy reconciliation loop as a background goroutine.
	// When the umbrella chart manages esm-sh, the reconciler is disabled
	// to avoid creating a duplicate deployment with conflicting labels.
	if os.Getenv("TENTACULAR_PROXY_RECONCILER_DISABLED") == "true" {
		slog.Info("proxy reconciler disabled (chart-managed esm-sh)")
	} else {
		go reconciler.Run(ctx)
	}

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

	slog.Info("server stopped")
}
