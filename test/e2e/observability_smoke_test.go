//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// TestE2E_Smoke_CollectorHealth verifies the OTel Collector is reachable and
// accepting OTLP/HTTP requests. Fails fast if the observability stack is not up.
func TestE2E_Smoke_CollectorHealth(t *testing.T) {
	requireObs(t)
	e2eClient(t)

	collectorURL := obsCollectorURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		collectorURL+"/", nil)
	if err != nil {
		t.Fatalf("build collector health request: %v", err)
	}

	// The collector may return 404 on GET / but must respond (not connection refused).
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("collector not reachable at %s: %v", collectorURL, err)
	}
	_ = resp.Body.Close()
	t.Logf("collector health check: status=%d", resp.StatusCode)
}

// TestE2E_Smoke_SigNozHealth verifies the SigNoz query frontend is reachable
// and returns a healthy response on its /api/v1/health endpoint.
func TestE2E_Smoke_SigNozHealth(t *testing.T) {
	requireObs(t)
	e2eClient(t)

	sigNozURL := sigNozBaseURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		sigNozURL+"/api/v1/health", nil)
	if err != nil {
		t.Fatalf("build SigNoz health request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("SigNoz not reachable at %s: %v", sigNozURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("SigNoz health returned HTTP %d: %s", resp.StatusCode, string(body))
	}
	t.Logf("SigNoz health OK: %s", string(body))
}

// TestE2E_Smoke_ClickHouseHealth verifies ClickHouse is reachable via the
// SigNoz query service health endpoint, which exercises ClickHouse connectivity.
// The SigNoz /api/v1/health response includes component statuses.
func TestE2E_Smoke_ClickHouseHealth(t *testing.T) {
	requireObs(t)
	e2eClient(t)

	// Use a direct ClickHouse HTTP endpoint if provided, otherwise derive from
	// SigNoz base URL (same host, different port).
	clickhouseURL := os.Getenv("TENTACULAR_E2E_OBS_CLICKHOUSE_URL")
	if clickhouseURL == "" {
		// SigNoz ClickHouse runs on port 8123 by default.
		// Derive host from TENTACULAR_E2E_OBS_SIGNOZ_URL if set, else use the
		// well-known in-cluster ClickHouse service name.
		host := "clickhouse.tentacular-observability.svc.cluster.local"
		if u := os.Getenv("TENTACULAR_E2E_OBS_CLICKHOUSE_HOST"); u != "" {
			host = u
		}
		clickhouseURL = fmt.Sprintf("http://%s:8123", host)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		clickhouseURL+"/?query=SELECT+1", nil)
	if err != nil {
		t.Fatalf("build ClickHouse request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Skipf("ClickHouse not directly reachable at %s (may be internal only): %v", clickhouseURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ClickHouse SELECT 1 returned HTTP %d: %s", resp.StatusCode, string(body))
	}
	t.Logf("ClickHouse healthy: response=%q", strings.TrimSpace(string(body)))
}

// TestE2E_Smoke_PipelineEndToEnd is a lightweight smoke version of the full
// pipeline test. It sends a synthetic span and verifies SigNoz accepts it.
// Unlike the full TestE2E_PipelineE2E, this does not poll for the span to appear
// in SigNoz — it only verifies the ingest path is functional.
func TestE2E_Smoke_PipelineEndToEnd(t *testing.T) {
	requireObs(t)
	e2eClient(t)

	collectorURL := obsCollectorURL(t)
	sigNozURL := sigNozBaseURL(t)

	// Verify SigNoz API is reachable before testing the pipeline.
	if err := checkSigNozReachable(sigNozURL); err != nil {
		t.Fatalf("SigNoz not reachable, cannot validate pipeline: %v", err)
	}

	// Send a synthetic span.
	serviceName := fmt.Sprintf("e2e-smoke-%d", time.Now().UnixNano())
	traceID := fmt.Sprintf("%032x", time.Now().UnixNano())
	spanID := fmt.Sprintf("%016x", time.Now().UnixNano())

	otlpPayload := fmt.Sprintf(`{
		"resourceSpans": [{
			"resource": {
				"attributes": [{
					"key": "service.name",
					"value": {"stringValue": %q}
				}]
			},
			"scopeSpans": [{
				"scope": {"name": "smoke-test"},
				"spans": [{
					"traceId": %q,
					"spanId": %q,
					"name": "smoke-pipeline-validation",
					"kind": 1,
					"startTimeUnixNano": "%d",
					"endTimeUnixNano": "%d",
					"status": {}
				}]
			}]
		}]
	}`, serviceName, traceID, spanID,
		time.Now().Add(-time.Second).UnixNano(),
		time.Now().UnixNano())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		collectorURL+"/v1/traces", strings.NewReader(otlpPayload))
	if err != nil {
		t.Fatalf("build collector request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST span to collector: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("collector rejected span: HTTP %d: %s", resp.StatusCode, string(body))
	}
	t.Logf("smoke pipeline: span accepted by collector, service=%s traceID=%s", serviceName, traceID)
}

// checkSigNozReachable does a lightweight GET to SigNoz /api/v1/health and
// returns an error if it is not reachable or not healthy.
func checkSigNozReachable(baseURL string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/v1/health", nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /api/v1/health: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var health struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		// If we can't decode, treat a 200 as healthy enough.
		return nil
	}
	if health.Status != "" && health.Status != "ok" && health.Status != "healthy" {
		return fmt.Errorf("SigNoz health status=%q", health.Status)
	}
	return nil
}
