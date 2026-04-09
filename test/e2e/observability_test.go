//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

const (
	// observabilityNS is the namespace where the observability stack is deployed.
	observabilityNS = "tentacular-observability"

	// otelCollectorHost is the well-known in-cluster DNS name for the OTel Collector.
	otelCollectorHost = "otel-collector.tentacular-observability.svc.cluster.local"

	// sigNozQueryHost is the SigNoz query frontend service host.
	sigNozQueryHost = "signoz-query-service.tentacular-observability.svc.cluster.local"

	// e2eObsNS is the namespace where pre-deployed otel validation tentacles live.
	// These tentacles were deployed via tntc deploy with the enrichment pipeline.
	e2eObsNS = "tentacular-e2e-test"

	// traceWaitTimeout is the maximum time to wait for a trace to appear in ClickHouse.
	traceWaitTimeout = 30 * time.Second

	// traceWaitInterval is the polling interval when waiting for traces.
	traceWaitInterval = 3 * time.Second

	// clickhouseDefaultHost is the in-cluster ClickHouse HTTP endpoint.
	clickhouseDefaultHost = "signoz-clickhouse.tentacular-observability.svc.cluster.local"
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

// clickhouseBaseURL returns the ClickHouse HTTP endpoint URL, preferring
// TENTACULAR_E2E_OBS_CLICKHOUSE_URL if set.
func clickhouseBaseURL(t *testing.T) string {
	t.Helper()
	if u := os.Getenv("TENTACULAR_E2E_OBS_CLICKHOUSE_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	host := clickhouseDefaultHost
	if h := os.Getenv("TENTACULAR_E2E_OBS_CLICKHOUSE_HOST"); h != "" {
		host = h
	}
	return fmt.Sprintf("http://%s:8123", host)
}

// queryClickHouse executes a SQL query against ClickHouse via HTTP and returns
// the result rows as a slice of maps. Uses JSONEachRow format for easy parsing.
func queryClickHouse(t *testing.T, sql string) []map[string]any {
	t.Helper()
	chURL := clickhouseBaseURL(t)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	reqURL := fmt.Sprintf("%s/?default_format=JSONEachRow&query=%s", chURL, url.QueryEscape(sql))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		t.Fatalf("build ClickHouse request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("ClickHouse query failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ClickHouse returned HTTP %d: %s\nQuery: %s", resp.StatusCode, string(body), sql)
	}

	var rows []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		if line == "" {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("parse ClickHouse row: %v\nLine: %s", err, line)
		}
		rows = append(rows, row)
	}
	return rows
}

// waitForSpans polls ClickHouse until spans matching the given service and span
// name appear, or the timeout expires. Returns the matching rows.
func waitForSpans(t *testing.T, serviceName, spanName string, timeout time.Duration) []map[string]any {
	t.Helper()

	sql := fmt.Sprintf(
		`SELECT name, statusCode, durationNano,
			attributes_string['gen_ai.system'] AS gen_ai_system,
			attributes_string['gen_ai.request.model'] AS gen_ai_model,
			attributes_number['gen_ai.usage.input_tokens'] AS input_tokens,
			attributes_number['gen_ai.usage.output_tokens'] AS output_tokens,
			has(attributes_string, 'gen_ai.input.messages') AS has_input_messages,
			has(attributes_string, 'gen_ai.output.messages') AS has_output_messages,
			resources_string['tentacular.enclave'] AS enclave,
			resources_string['k8s.namespace.name'] AS namespace
		FROM signoz_traces.distributed_signoz_index_v3
		WHERE serviceName = '%s' AND name = '%s'
			AND timestamp > toDateTime64(now() - INTERVAL 2 MINUTE, 9)
		ORDER BY timestamp DESC
		LIMIT 10`,
		serviceName, spanName,
	)

	deadline := time.Now().Add(timeout)
	for {
		rows := queryClickHouse(t, sql)
		if len(rows) > 0 {
			t.Logf("found %d %s spans for service=%s", len(rows), spanName, serviceName)
			return rows
		}
		if time.Now().After(deadline) {
			t.Fatalf("no %s spans found for service %q within %s", spanName, serviceName, timeout)
		}
		t.Logf("waiting for %s spans (service=%s)...", spanName, serviceName)
		time.Sleep(traceWaitInterval)
	}
}

// scaleDeployment sets the replica count for a Deployment.
func scaleDeployment(ctx context.Context, t *testing.T, client *k8s.Client, namespace, name string, replicas int32) {
	t.Helper()
	scale, err := client.Clientset.AppsV1().Deployments(namespace).GetScale(ctx, name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get scale for %s/%s: %v", namespace, name, err)
	}
	scale.Spec.Replicas = replicas
	_, err = client.Clientset.AppsV1().Deployments(namespace).UpdateScale(ctx, name, scale, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("scale %s/%s to %d: %v", namespace, name, replicas, err)
	}
}

// toFloat64 extracts a float64 from a JSON-decoded value (ClickHouse numbers
// may arrive as float64 or string in JSONEachRow format).
func toFloat64(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case json.Number:
		f, _ := n.Float64()
		return f
	case string:
		var f float64
		_, _ = fmt.Sscanf(n, "%f", &f)
		return f
	default:
		return 0
	}
}

// toBool extracts a boolean from a JSON-decoded value. ClickHouse has()
// returns 0 or 1 as a number.
func toBool(v any) bool {
	return toFloat64(v) != 0
}

// ---------- MCP-based workflow runner ----------

// bearerTransport injects a Bearer token into every HTTP request.
type obsBearerTransport struct {
	token string
	base  http.RoundTripper
}

func (t *obsBearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(req)
}

var (
	mcpSessionOnce sync.Once
	mcpSessionVal  *mcp.ClientSession
	mcpSessionErr  error
)

// obsMCPSession returns a shared MCP client session connected to the external
// MCP server. Returns nil if TENTACULAR_E2E_MCP_URL is not set.
func obsMCPSession(t *testing.T) *mcp.ClientSession {
	t.Helper()
	mcpURL := os.Getenv("TENTACULAR_E2E_MCP_URL")
	if mcpURL == "" {
		return nil
	}
	mcpSessionOnce.Do(func() {
		token := os.Getenv("TENTACULAR_E2E_MCP_TOKEN")
		if token == "" {
			mcpSessionErr = fmt.Errorf("TENTACULAR_E2E_MCP_URL set but TENTACULAR_E2E_MCP_TOKEN is empty")
			return
		}
		transport := &mcp.StreamableClientTransport{
			Endpoint: strings.TrimRight(mcpURL, "/") + "/mcp",
			HTTPClient: &http.Client{
				Transport: &obsBearerTransport{token: token, base: http.DefaultTransport},
			},
			MaxRetries: -1,
		}
		client := mcp.NewClient(&mcp.Implementation{
			Name:    "e2e-obs-test",
			Version: "1.0.0",
		}, nil)
		mcpSessionVal, mcpSessionErr = client.Connect(context.Background(), transport, nil)
	})
	if mcpSessionErr != nil {
		t.Fatalf("MCP session: %v", mcpSessionErr)
	}
	return mcpSessionVal
}

// runWorkflow triggers a deployed workflow. If TENTACULAR_E2E_MCP_URL is set,
// it uses the MCP server's wf_run tool (works from outside the cluster).
// Otherwise it calls k8s.RunWorkflow() directly (requires ClusterIP access).
func runWorkflow(ctx context.Context, t *testing.T, client *k8s.Client, namespace, name string) json.RawMessage {
	t.Helper()

	if sess := obsMCPSession(t); sess != nil {
		result, err := sess.CallTool(ctx, &mcp.CallToolParams{
			Name: "wf_run",
			Arguments: map[string]any{
				"namespace": namespace,
				"name":      name,
			},
		})
		if err != nil {
			t.Fatalf("MCP wf_run %s/%s: %v", namespace, name, err)
		}
		if result.IsError {
			msg := "wf_run returned error"
			if len(result.Content) > 0 {
				if tc, ok := result.Content[0].(*mcp.TextContent); ok {
					msg = tc.Text
				}
			}
			t.Fatalf("MCP wf_run %s/%s: %s", namespace, name, msg)
		}
		if len(result.Content) > 0 {
			if tc, ok := result.Content[0].(*mcp.TextContent); ok {
				return json.RawMessage(tc.Text)
			}
		}
		return json.RawMessage(`{}`)
	}

	out, err := k8s.RunWorkflow(ctx, client, namespace, name, nil)
	if err != nil {
		t.Fatalf("RunWorkflow %s/%s: %v", namespace, name, err)
	}
	return out
}

// runWorkflowAllowError is like runWorkflow but does not fail on error
// (used for otel-error which deliberately fails).
func runWorkflowAllowError(ctx context.Context, t *testing.T, client *k8s.Client, namespace, name string) (json.RawMessage, error) {
	t.Helper()

	if sess := obsMCPSession(t); sess != nil {
		result, err := sess.CallTool(ctx, &mcp.CallToolParams{
			Name: "wf_run",
			Arguments: map[string]any{
				"namespace": namespace,
				"name":      name,
			},
		})
		if err != nil {
			return nil, err
		}
		if result.IsError {
			msg := "wf_run error"
			if len(result.Content) > 0 {
				if tc, ok := result.Content[0].(*mcp.TextContent); ok {
					msg = tc.Text
				}
			}
			return nil, fmt.Errorf("%s", msg)
		}
		if len(result.Content) > 0 {
			if tc, ok := result.Content[0].(*mcp.TextContent); ok {
				return json.RawMessage(tc.Text), nil
			}
		}
		return json.RawMessage(`{}`), nil
	}

	return k8s.RunWorkflow(ctx, client, namespace, name, nil)
}

// ---------- Pipeline tests ----------

// TestE2E_CollectorReachable verifies the OTel Collector HTTP endpoint accepts
// OTLP trace requests. This is the lowest-level gate for all pipeline tests.
func TestE2E_CollectorReachable(t *testing.T) {
	requireObs(t)
	e2eClient(t) // validate kubeconfig

	collectorURL := obsCollectorURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

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

// ---------- Workflow-level tests ----------

// TestE2E_WorkflowTrace runs the otel-echo validation tentacle and verifies
// the resulting trace appears in ClickHouse with invoke_workflow and
// execute_node spans plus correct resource attributes.
func TestE2E_WorkflowTrace(t *testing.T) {
	requireObs(t)
	client := e2eClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	out := runWorkflow(ctx, t, client, e2eObsNS, "otel-echo")
	t.Logf("otel-echo output: %s", string(out))

	invSpans := waitForSpans(t, "otel-echo", "invoke_workflow", traceWaitTimeout)
	if len(invSpans) == 0 {
		t.Fatal("no invoke_workflow spans found")
	}

	enclave, _ := invSpans[0]["enclave"].(string)
	ns, _ := invSpans[0]["namespace"].(string)
	if enclave != e2eObsNS {
		t.Errorf("expected enclave=%q, got %q", e2eObsNS, enclave)
	}
	if ns != e2eObsNS {
		t.Errorf("expected namespace=%q, got %q", e2eObsNS, ns)
	}

	execSpans := waitForSpans(t, "otel-echo", "execute_node", traceWaitTimeout)
	if len(execSpans) == 0 {
		t.Fatal("no execute_node spans found")
	}
	t.Logf("WorkflowTrace: %d invoke_workflow, %d execute_node spans", len(invSpans), len(execSpans))
}

// TestE2E_LLMTelemetry runs the otel-llm-echo validation tentacle (real
// Anthropic API call) and verifies GenAI span attributes in ClickHouse.
// Content capture must be off by default.
func TestE2E_LLMTelemetry(t *testing.T) {
	requireObs(t)
	client := e2eClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	out := runWorkflow(ctx, t, client, e2eObsNS, "otel-llm-echo")
	t.Logf("otel-llm-echo output length: %d bytes", len(out))

	sql := `SELECT name,
			attributes_string['gen_ai.system'] AS gen_ai_system,
			attributes_string['gen_ai.request.model'] AS gen_ai_model,
			attributes_number['gen_ai.usage.input_tokens'] AS input_tokens,
			attributes_number['gen_ai.usage.output_tokens'] AS output_tokens,
			has(attributes_string, 'gen_ai.input.messages') AS has_input_messages,
			has(attributes_string, 'gen_ai.output.messages') AS has_output_messages
		FROM signoz_traces.distributed_signoz_index_v3
		WHERE serviceName = 'otel-llm-echo'
			AND attributes_string['gen_ai.system'] <> ''
			AND timestamp > toDateTime64(now() - INTERVAL 2 MINUTE, 9)
		ORDER BY timestamp DESC
		LIMIT 5`

	var rows []map[string]any
	deadline := time.Now().Add(traceWaitTimeout)
	for {
		rows = queryClickHouse(t, sql)
		if len(rows) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no GenAI spans found for otel-llm-echo within timeout")
		}
		t.Log("waiting for GenAI spans...")
		time.Sleep(traceWaitInterval)
	}

	row := rows[0]
	t.Logf("GenAI span: system=%v model=%v input=%v output=%v",
		row["gen_ai_system"], row["gen_ai_model"], row["input_tokens"], row["output_tokens"])

	if s, _ := row["gen_ai_system"].(string); s != "anthropic" {
		t.Errorf("expected gen_ai.system=anthropic, got %q", s)
	}
	if m, _ := row["gen_ai_model"].(string); m == "" {
		t.Error("gen_ai.request.model is empty")
	}

	inputTok := toFloat64(row["input_tokens"])
	outputTok := toFloat64(row["output_tokens"])
	if inputTok <= 0 {
		t.Errorf("expected input_tokens > 0, got %v", inputTok)
	}
	if outputTok <= 0 {
		t.Errorf("expected output_tokens > 0, got %v", outputTok)
	}

	if toBool(row["has_input_messages"]) {
		t.Error("gen_ai.input.messages should not be present (content leakage)")
	}
	if toBool(row["has_output_messages"]) {
		t.Error("gen_ai.output.messages should not be present (content leakage)")
	}
}

// TestE2E_GracefulDegradation verifies that workflow execution succeeds even
// when the OTel Collector is unavailable (telemetry export is non-fatal).
func TestE2E_GracefulDegradation(t *testing.T) {
	requireObs(t)
	client := e2eClient(t)

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		scaleDeployment(ctx, t, client, observabilityNS, "tentacular-observability-collector", 1)
		t.Log("collector restored to 1 replica")
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	scaleDeployment(ctx, t, client, observabilityNS, "tentacular-observability-collector", 0)
	t.Log("collector scaled to 0")

	// Wait for collector pod to terminate.
	time.Sleep(5 * time.Second)

	runCtx, runCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer runCancel()
	out := runWorkflow(runCtx, t, client, e2eObsNS, "otel-echo")
	t.Logf("workflow succeeded without collector: %d bytes output", len(out))
}

// TestE2E_ErrorSpan runs the otel-error validation tentacle and verifies an
// ERROR-status span appears in ClickHouse.
func TestE2E_ErrorSpan(t *testing.T) {
	requireObs(t)
	client := e2eClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// otel-error may return an HTTP error or succeed with error in output.
	_, err := runWorkflowAllowError(ctx, t, client, e2eObsNS, "otel-error")
	if err != nil {
		t.Logf("otel-error returned error (expected): %v", err)
	}

	sql := `SELECT name, statusCode, statusMessage
		FROM signoz_traces.distributed_signoz_index_v3
		WHERE serviceName = 'otel-error'
			AND statusCode = 2
			AND timestamp > toDateTime64(now() - INTERVAL 2 MINUTE, 9)
		ORDER BY timestamp DESC
		LIMIT 5`

	var rows []map[string]any
	deadline := time.Now().Add(traceWaitTimeout)
	for {
		rows = queryClickHouse(t, sql)
		if len(rows) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no ERROR spans found for otel-error within timeout")
		}
		t.Log("waiting for error spans...")
		time.Sleep(traceWaitInterval)
	}

	t.Logf("found %d ERROR spans for otel-error", len(rows))
	statusCode := toFloat64(rows[0]["statusCode"])
	if statusCode != 2 {
		t.Errorf("expected statusCode=2 (Error), got %v", statusCode)
	}
}

// ---------- Enrichment validation ----------

// TestE2E_EnrichmentApplied verifies that the OTel enrichment pipeline applied
// the correct NetworkPolicy egress rules and Deployment environment variables
// to a deployed tentacle.
func TestE2E_EnrichmentApplied(t *testing.T) {
	requireObs(t)
	client := e2eClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Verify NetworkPolicy has OTel egress rules.
	np, err := client.Clientset.NetworkingV1().NetworkPolicies(e2eObsNS).Get(
		ctx, "otel-echo-netpol", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get NetworkPolicy otel-echo-netpol: %v", err)
	}

	foundOTelEgress := false
	for _, rule := range np.Spec.Egress {
		for _, port := range rule.Ports {
			if port.Port != nil && port.Port.IntValue() == 4317 {
				foundOTelEgress = true
				for _, to := range rule.To {
					if to.NamespaceSelector != nil {
						nsLabels := to.NamespaceSelector.MatchLabels
						if nsLabels["kubernetes.io/metadata.name"] == observabilityNS {
							t.Log("NetworkPolicy has OTel egress to tentacular-observability on port 4317")
						} else {
							t.Errorf("OTel egress targets wrong namespace: %v", nsLabels)
						}
					}
				}
				break
			}
		}
		if foundOTelEgress {
			break
		}
	}
	if !foundOTelEgress {
		t.Error("NetworkPolicy missing OTel egress rule (port 4317)")
	}

	// Verify Deployment has OTel environment variables.
	deploy, err := client.Clientset.AppsV1().Deployments(e2eObsNS).Get(
		ctx, "otel-echo", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get Deployment otel-echo: %v", err)
	}

	envMap := map[string]string{}
	for _, c := range deploy.Spec.Template.Spec.Containers {
		for _, env := range c.Env {
			envMap[env.Name] = env.Value
		}
	}

	requiredEnvs := []string{
		"OTEL_DENO",
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_PROTOCOL",
		"OTEL_SERVICE_NAME",
		"OTEL_RESOURCE_ATTRIBUTES",
	}
	for _, key := range requiredEnvs {
		if v, ok := envMap[key]; !ok {
			t.Errorf("Deployment missing env var %s", key)
		} else {
			t.Logf("env %s=%s", key, v)
		}
	}
}

// ---------- Helpers ----------

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
