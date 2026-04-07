//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

const (
	// observabilityNS is the namespace where the observability stack is deployed.
	observabilityNS = "tentacular-observability"

	// otelCollectorHost is the well-known in-cluster DNS name for the OTel Collector.
	otelCollectorHost = "otel-collector.tentacular-observability.svc.cluster.local"

	// sigNozQueryHost is the SigNoz query frontend service host.
	sigNozQueryHost = "signoz-query-service.tentacular-observability.svc.cluster.local"

	// e2eObsNS is the dedicated test namespace for observability validation.
	e2eObsNS = "tentacular-e2e-observability"

	// traceWaitTimeout is the maximum time to wait for a trace to appear in SigNoz.
	traceWaitTimeout = 30 * time.Second

	// traceWaitInterval is the polling interval when waiting for traces.
	traceWaitInterval = 3 * time.Second
)

// requireObs skips the test if TENTACULAR_E2E_OBS is not "true". All
// observability E2E tests require the tentacular-observability Helm chart to be
// deployed, so they gate on this explicit opt-in.
func requireObs(t *testing.T) {
	t.Helper()
	if os.Getenv("TENTACULAR_E2E_OBS") != "true" {
		t.Skip("TENTACULAR_E2E_OBS not set to 'true', skipping observability e2e test")
	}
}

// obsCollectorURL returns the OTel Collector HTTP endpoint URL, preferring
// TENTACULAR_E2E_OBS_COLLECTOR_URL if set (for out-of-cluster test runs).
func obsCollectorURL(t *testing.T) string {
	t.Helper()
	if u := os.Getenv("TENTACULAR_E2E_OBS_COLLECTOR_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	return fmt.Sprintf("http://%s:4318", otelCollectorHost)
}

// sigNozBaseURL returns the SigNoz query frontend base URL, preferring
// TENTACULAR_E2E_OBS_SIGNOZ_URL if set.
func sigNozBaseURL(t *testing.T) string {
	t.Helper()
	if u := os.Getenv("TENTACULAR_E2E_OBS_SIGNOZ_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	return fmt.Sprintf("http://%s:8080", sigNozQueryHost)
}

// TestE2E_CollectorReachable verifies the OTel Collector HTTP endpoint accepts
// OTLP trace requests. This is the lowest-level gate for all pipeline tests.
func TestE2E_CollectorReachable(t *testing.T) {
	requireObs(t)
	e2eClient(t) // validate kubeconfig

	collectorURL := obsCollectorURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Minimal valid OTLP/JSON payload — empty resource spans.
	payload := []byte(`{"resourceSpans":[]}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		collectorURL+"/v1/traces", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("build collector request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST to collector /v1/traces: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("collector returned HTTP %d: %s", resp.StatusCode, string(body))
	}
	t.Logf("collector reachable, status=%d", resp.StatusCode)
}

// TestE2E_PipelineE2E sends a synthetic OTLP span to the collector, then polls
// SigNoz until the span is queryable (within 30 seconds).
func TestE2E_PipelineE2E(t *testing.T) {
	requireObs(t)
	e2eClient(t)

	collectorURL := obsCollectorURL(t)
	sigNozURL := sigNozBaseURL(t)

	// Use a unique service name so we can query it back precisely.
	serviceName := fmt.Sprintf("e2e-pipeline-test-%d", time.Now().UnixNano())
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
				"scope": {"name": "e2e-test"},
				"spans": [{
					"traceId": %q,
					"spanId": %q,
					"name": "e2e-pipeline-validation",
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

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
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
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("collector returned HTTP %d", resp.StatusCode)
	}
	t.Logf("span sent: traceID=%s service=%s", traceID, serviceName)

	// Poll SigNoz until the span is visible.
	deadline := time.Now().Add(traceWaitTimeout)
	for {
		found, queryErr := querySigNozForService(sigNozURL, serviceName)
		if queryErr != nil {
			t.Logf("SigNoz query error (will retry): %v", queryErr)
		}
		if found {
			t.Logf("span found in SigNoz for service=%s", serviceName)
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("span for service %q not found in SigNoz within %s", serviceName, traceWaitTimeout)
		}
		time.Sleep(traceWaitInterval)
	}
}

// TestE2E_WorkflowTrace deploys the otel-echo validation tentacle, runs it,
// and verifies the resulting trace appears in SigNoz with invoke_workflow and
// execute_node spans.
func TestE2E_WorkflowTrace(t *testing.T) {
	requireObs(t)
	client := e2eClient(t)
	ctx := context.Background()

	t.Cleanup(func() {
		_ = k8s.DeleteNamespace(context.Background(), client, e2eObsNS)
	})

	t.Skip("TestE2E_WorkflowTrace: requires engine OTel env var injection (builder dev) and wf_apply/wf_run helpers — skipping until Phase 1 engine changes land")

	if err := k8s.CreateNamespace(ctx, client, e2eObsNS); err != nil {
		t.Fatalf("CreateNamespace: %v", err)
	}

	// TODO(phase1): deploy otel-echo via wf_apply, run via wf_run,
	// query SigNoz for invoke_workflow + execute_node spans with
	// tentacular.workflow.name attribute set correctly.
}

// TestE2E_LLMTelemetry deploys the otel-llm-echo validation tentacle, runs it
// (makes a real Anthropic API call), and verifies GenAI span attributes in
// SigNoz. Content capture must be off by default.
func TestE2E_LLMTelemetry(t *testing.T) {
	requireObs(t)
	client := e2eClient(t)
	ctx := context.Background()

	t.Cleanup(func() {
		_ = k8s.DeleteNamespace(context.Background(), client, e2eObsNS)
	})

	t.Skip("TestE2E_LLMTelemetry: requires GenAI fetch wrapper (engine dev) and wf_apply/wf_run helpers — skipping until Phase 1 engine changes land")

	if err := k8s.CreateNamespace(ctx, client, e2eObsNS); err != nil {
		t.Fatalf("CreateNamespace: %v", err)
	}

	// TODO(phase1): deploy otel-llm-echo via wf_apply, run via wf_run,
	// assert gen_ai.usage.input_tokens > 0, gen_ai.system = "anthropic",
	// gen_ai.request.model present, gen_ai.input.messages absent.
}

// TestE2E_GracefulDegradation verifies that workflow execution succeeds even
// when the OTel Collector service is unavailable (telemetry export is non-fatal).
func TestE2E_GracefulDegradation(t *testing.T) {
	requireObs(t)
	client := e2eClient(t)
	ctx := context.Background()

	t.Cleanup(func() {
		_ = k8s.DeleteNamespace(context.Background(), client, e2eObsNS)
	})

	t.Skip("TestE2E_GracefulDegradation: requires wf_apply/wf_run and collector Service manipulation — skipping until Phase 1 engine changes land")

	if err := k8s.CreateNamespace(ctx, client, e2eObsNS); err != nil {
		t.Fatalf("CreateNamespace: %v", err)
	}

	// TODO(phase1): delete otel-collector Service, deploy and run a tentacle,
	// assert workflow succeeds, restore Service, verify telemetry resumes.
}

// TestE2E_ErrorSpan deploys the otel-error validation tentacle, runs it, and
// verifies an ERROR-status span with an exception event appears in SigNoz.
func TestE2E_ErrorSpan(t *testing.T) {
	requireObs(t)
	client := e2eClient(t)
	ctx := context.Background()

	t.Cleanup(func() {
		_ = k8s.DeleteNamespace(context.Background(), client, e2eObsNS)
	})

	t.Skip("TestE2E_ErrorSpan: requires wf_apply/wf_run MCP integration — skipping until Phase 1 engine changes land")

	if err := k8s.CreateNamespace(ctx, client, e2eObsNS); err != nil {
		t.Fatalf("CreateNamespace: %v", err)
	}

	// TODO(phase1): deploy otel-error via wf_apply, run via wf_run,
	// assert execute_node span has status ERROR and exception event recorded.
}

// querySigNozForService queries the SigNoz services list API and returns true
// if the given service name appears in the response.
func querySigNozForService(baseURL, serviceName string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		baseURL+"/api/v1/services", nil)
	if err != nil {
		return false, fmt.Errorf("build SigNoz request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("GET SigNoz services: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("SigNoz returned HTTP %d", resp.StatusCode)
	}

	var result struct {
		Data []struct {
			ServiceName string `json:"serviceName"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, fmt.Errorf("decode SigNoz response: %w", err)
	}

	for _, svc := range result.Data {
		if svc.ServiceName == serviceName {
			return true, nil
		}
	}
	return false, nil
}
