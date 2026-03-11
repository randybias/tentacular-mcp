package k8s

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"
)

// ModuleDep describes a single jsr or npm module dependency that should be
// pre-warmed in the in-cluster esm.sh module proxy.
type ModuleDep struct {
	// Protocol is either "jsr" or "npm".
	Protocol string
	// Host is the package name (e.g. "@db/postgres" for jsr, "lodash" for npm).
	Host string
	// Version is the package version string (e.g. "0.19.5"). May be empty.
	Version string
}

// prewarmConcurrency is the maximum number of concurrent proxy warm-up requests.
const prewarmConcurrency = 3

// prewarmTimeout is the timeout for a single module pre-warm request.
// First compilation by esm.sh can be slow (60s+), so we use a generous timeout.
const prewarmTimeout = 120 * time.Second

// PrewarmModules sends GET requests to the in-cluster esm.sh module proxy for
// each dependency, causing esm.sh to build and cache the module ahead of time.
// This prevents cold-start failures where the first import exceeds esm.sh's
// build deadline at pod startup.
//
// The function is best-effort: individual failures are logged but do not cause
// the overall operation to fail. Uses direct HTTP to the esm-sh service:
//
//	GET http://esm-sh.{proxyNamespace}.svc.cluster.local:8080/jsr/{host}@{version}
//	GET http://esm-sh.{proxyNamespace}.svc.cluster.local:8080/{host}@{version}
func PrewarmModules(ctx context.Context, client *Client, proxyNamespace string, deps []ModuleDep) error {
	if len(deps) == 0 {
		return nil
	}

	slog.Info("pre-warming module proxy", "count", len(deps), "namespace", proxyNamespace)

	sem := make(chan struct{}, prewarmConcurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var errs []error

	for _, dep := range deps {
		dep := dep // capture loop variable
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			moduleURL := buildModuleURL(proxyNamespace, dep)
			slog.Debug("pre-warming module", "url", moduleURL, "protocol", dep.Protocol, "host", dep.Host)

			reqCtx, cancel := context.WithTimeout(ctx, prewarmTimeout)
			defer cancel()

			req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, moduleURL, nil)
			if err != nil {
				slog.Warn("module pre-warm request creation failed",
					"host", dep.Host, "protocol", dep.Protocol, "error", err)
				mu.Lock()
				errs = append(errs, fmt.Errorf("%s/%s: %w", dep.Protocol, dep.Host, err))
				mu.Unlock()
				return
			}

			resp, err := client.HTTP.Do(req)
			if err != nil {
				slog.Warn("module pre-warm failed (best-effort)",
					"host", dep.Host, "protocol", dep.Protocol, "error", err)
				mu.Lock()
				errs = append(errs, fmt.Errorf("%s/%s: %w", dep.Protocol, dep.Host, err))
				mu.Unlock()
				return
			}
			_ = resp.Body.Close()

			slog.Info("module pre-warmed", "host", dep.Host, "protocol", dep.Protocol)
		}()
	}

	wg.Wait()

	if len(errs) > 0 {
		slog.Warn("some modules failed to pre-warm", "count", len(errs))
		return fmt.Errorf("%d of %d modules failed to pre-warm", len(errs), len(deps))
	}
	return nil
}

// buildModuleURL constructs the direct HTTP URL for a module dependency.
// The service name defaults to "esm-sh" (reconciler-managed) but can be
// overridden via TENTACULAR_PROXY_SERVICE_NAME for chart-managed deployments.
func buildModuleURL(proxyNamespace string, dep ModuleDep) string {
	serviceName := os.Getenv("TENTACULAR_PROXY_SERVICE_NAME")
	if serviceName == "" {
		serviceName = "esm-sh"
	}
	base := fmt.Sprintf("http://%s.%s.svc.cluster.local:8080", serviceName, proxyNamespace)
	var pkg string
	switch dep.Protocol {
	case "jsr":
		pkg = "/jsr/" + dep.Host
	default: // npm
		pkg = "/" + dep.Host
	}
	if dep.Version != "" {
		pkg += "@" + dep.Version
	}
	return base + pkg
}
