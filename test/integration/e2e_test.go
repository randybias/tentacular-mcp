//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/randybias/tentacular-mcp/pkg/k8s"
	"github.com/randybias/tentacular-mcp/pkg/server"
)

// bearerTransport injects a Bearer token into every HTTP request.
type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func (t *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(req)
}

// e2eEnv sets up an MCP server backed by a real K8s client and returns
// a connected MCP client session plus the underlying K8s client for cleanup.
// Callers must defer cleanup().
func e2eEnv(t *testing.T) (session *mcp.ClientSession, k8sClient *k8s.Client, cleanup func()) {
	t.Helper()

	k8sClient = integrationClient(t)
	token := "e2e-test-token"

	srv, err := server.New(k8sClient, nil, nil, nil, nil, nil, token, slog.Default())
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	ts := httptest.NewServer(srv.Handler())

	httpClient := &http.Client{
		Transport: &bearerTransport{
			token: token,
			base:  http.DefaultTransport,
		},
	}

	transport := &mcp.StreamableClientTransport{
		Endpoint:   ts.URL + "/mcp",
		HTTPClient: httpClient,
		MaxRetries: -1, // disable retries
	}

	mcpClient := mcp.NewClient(&mcp.Implementation{
		Name:    "e2e-test",
		Version: "1.0.0",
	}, nil)

	ctx := context.Background()
	sess, err := mcpClient.Connect(ctx, transport, nil)
	if err != nil {
		ts.Close()
		t.Fatalf("MCP client connect: %v", err)
	}

	return sess, k8sClient, func() {
		sess.Close()
		ts.Close()
	}
}

// cleanupNs registers a t.Cleanup that deletes a namespace via direct K8s API.
// This is safe even after the MCP session is closed.
func cleanupNs(t *testing.T, client *k8s.Client, names ...string) {
	t.Helper()
	t.Cleanup(func() {
		for _, name := range names {
			_ = k8s.DeleteNamespace(context.Background(), client, name)
		}
	})
}

// callTool is a helper that calls an MCP tool and returns the JSON text result.
func callTool(t *testing.T, session *mcp.ClientSession, name string, args any) string {
	t.Helper()
	ctx := context.Background()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	if result.IsError {
		msg := "tool returned error"
		if len(result.Content) > 0 {
			if tc, ok := result.Content[0].(*mcp.TextContent); ok {
				msg = tc.Text
			}
		}
		t.Fatalf("CallTool(%s): %s", name, msg)
	}
	if len(result.Content) == 0 {
		t.Fatalf("CallTool(%s): empty content", name)
	}
	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("CallTool(%s): content is %T, not TextContent", name, result.Content[0])
	}
	return tc.Text
}

// callToolExpectError calls a tool expecting it to return an error (isError=true).
func callToolExpectError(t *testing.T, session *mcp.ClientSession, name string, args any) string {
	t.Helper()
	ctx := context.Background()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		// Transport-level error is also acceptable.
		return err.Error()
	}
	if !result.IsError {
		t.Fatalf("CallTool(%s): expected error, got success", name)
	}
	if len(result.Content) > 0 {
		if tc, ok := result.Content[0].(*mcp.TextContent); ok {
			return tc.Text
		}
	}
	return "unknown error"
}

// --- E2E: Namespace lifecycle ---

func TestE2E_NsCreateGetListDelete(t *testing.T) {
	session, client, cleanup := e2eEnv(t)
	defer cleanup()

	nsName := "tnt-e2e-ns-crud"
	cleanupNs(t, client, nsName)

	// ns_create
	text := callTool(t, session, "ns_create", map[string]any{
		"name":         nsName,
		"quota_preset": "medium",
	})
	var createResult struct {
		Name             string   `json:"name"`
		Status           string   `json:"status"`
		QuotaPreset      string   `json:"quota_preset"`
		ResourcesCreated []string `json:"resources_created"`
	}
	if err := json.Unmarshal([]byte(text), &createResult); err != nil {
		t.Fatalf("unmarshal ns_create result: %v", err)
	}
	if createResult.Name != nsName {
		t.Errorf("ns_create name: got %q, want %q", createResult.Name, nsName)
	}
	if createResult.Status != "Active" {
		t.Errorf("ns_create status: got %q, want %q", createResult.Status, "Active")
	}
	if createResult.QuotaPreset != "medium" {
		t.Errorf("ns_create quota_preset: got %q, want %q", createResult.QuotaPreset, "medium")
	}
	if len(createResult.ResourcesCreated) == 0 {
		t.Error("ns_create: expected non-empty resources_created")
	}
	t.Logf("ns_create resources_created: %v", createResult.ResourcesCreated)

	// ns_get
	text = callTool(t, session, "ns_get", map[string]any{"name": nsName})
	var getResult struct {
		Name    string `json:"name"`
		Managed bool   `json:"managed"`
		Quota   *struct {
			CPULimit string `json:"cpuLimit"`
			MemLimit string `json:"memLimit"`
			PodLimit int    `json:"podLimit"`
		} `json:"quota"`
		LimitRange *struct {
			DefaultCPURequest string `json:"defaultCPURequest"`
		} `json:"limitRange"`
	}
	if err := json.Unmarshal([]byte(text), &getResult); err != nil {
		t.Fatalf("unmarshal ns_get result: %v", err)
	}
	if !getResult.Managed {
		t.Error("ns_get: expected managed=true")
	}
	if getResult.Quota == nil {
		t.Error("ns_get: expected quota to be populated")
	} else if getResult.Quota.CPULimit != "4" {
		t.Errorf("ns_get quota CPU: got %q, want 4", getResult.Quota.CPULimit)
	}

	// ns_list
	text = callTool(t, session, "ns_list", map[string]any{})
	var listResult struct {
		Namespaces []struct {
			Name string `json:"name"`
		} `json:"namespaces"`
	}
	if err := json.Unmarshal([]byte(text), &listResult); err != nil {
		t.Fatalf("unmarshal ns_list result: %v", err)
	}
	found := false
	for _, ns := range listResult.Namespaces {
		if ns.Name == nsName {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ns_list: %s not found in managed namespaces", nsName)
	}

	// ns_delete
	text = callTool(t, session, "ns_delete", map[string]any{"name": nsName})
	var deleteResult struct {
		Name    string `json:"name"`
		Deleted bool   `json:"deleted"`
	}
	if err := json.Unmarshal([]byte(text), &deleteResult); err != nil {
		t.Fatalf("unmarshal ns_delete result: %v", err)
	}
	if !deleteResult.Deleted {
		t.Error("ns_delete: expected deleted=true")
	}
}

// --- E2E: Guard rejects tentacular-system ---

func TestE2E_NsCreateRejectsProtectedNamespace(t *testing.T) {
	session, _, cleanup := e2eEnv(t)
	defer cleanup()

	errText := callToolExpectError(t, session, "ns_create", map[string]any{
		"name":         "tentacular-system",
		"quota_preset": "small",
	})
	t.Logf("guard rejection: %s", errText)
}

// --- E2E: Credentials ---

// --- E2E: Workflow introspection ---

func TestE2E_WorkflowPodsEventsJobs(t *testing.T) {
	session, client, cleanup := e2eEnv(t)
	defer cleanup()

	nsName := "tnt-e2e-wf"
	cleanupNs(t, client, nsName)

	callTool(t, session, "ns_create", map[string]any{
		"name":         nsName,
		"quota_preset": "small",
	})

	// wf_pods - empty namespace, should return empty list.
	text := callTool(t, session, "wf_pods", map[string]any{"namespace": nsName})
	var podsResult struct {
		Pods []struct {
			Name string `json:"name"`
		} `json:"pods"`
	}
	if err := json.Unmarshal([]byte(text), &podsResult); err != nil {
		t.Fatalf("unmarshal wf_pods result: %v", err)
	}
	t.Logf("wf_pods: %d pods", len(podsResult.Pods))

	// wf_events
	text = callTool(t, session, "wf_events", map[string]any{
		"namespace": nsName,
		"limit":     10,
	})
	var eventsResult struct {
		Events []struct {
			Reason string `json:"reason"`
		} `json:"events"`
	}
	if err := json.Unmarshal([]byte(text), &eventsResult); err != nil {
		t.Fatalf("unmarshal wf_events result: %v", err)
	}
	t.Logf("wf_events: %d events", len(eventsResult.Events))

	// wf_jobs - empty namespace.
	text = callTool(t, session, "wf_jobs", map[string]any{"namespace": nsName})
	var jobsResult struct {
		Jobs     []any `json:"jobs"`
		CronJobs []any `json:"cronjobs"`
	}
	if err := json.Unmarshal([]byte(text), &jobsResult); err != nil {
		t.Fatalf("unmarshal wf_jobs result: %v", err)
	}
	t.Logf("wf_jobs: %d jobs, %d cronjobs", len(jobsResult.Jobs), len(jobsResult.CronJobs))
}

// --- E2E: Cluster operations ---

func TestE2E_ClusterPreflightAndProfile(t *testing.T) {
	session, client, cleanup := e2eEnv(t)
	defer cleanup()

	nsName := "tnt-e2e-ops"
	cleanupNs(t, client, nsName)

	callTool(t, session, "ns_create", map[string]any{
		"name":         nsName,
		"quota_preset": "medium",
	})

	// cluster_preflight
	text := callTool(t, session, "cluster_preflight", map[string]any{"namespace": nsName})
	var preflightResult struct {
		Checks []struct {
			Name    string `json:"name"`
			Passed  bool   `json:"passed"`
			Message string `json:"message"`
		} `json:"checks"`
	}
	if err := json.Unmarshal([]byte(text), &preflightResult); err != nil {
		t.Fatalf("unmarshal cluster_preflight result: %v", err)
	}
	for _, c := range preflightResult.Checks {
		if !c.Passed {
			t.Errorf("preflight %q failed: %s", c.Name, c.Message)
		}
	}

	// cluster_profile (with namespace)
	text = callTool(t, session, "cluster_profile", map[string]any{"namespace": nsName})
	var profileResult struct {
		K8sVersion   string `json:"k8sVersion"`
		Distribution string `json:"distribution"`
		Nodes        []struct {
			Name  string `json:"name"`
			Ready bool   `json:"ready"`
		} `json:"nodes"`
		CNI struct {
			Name string `json:"name"`
		} `json:"cni"`
		Namespace string `json:"namespace"`
		Quota     *struct {
			CPULimit string `json:"cpuLimit"`
		} `json:"quota"`
	}
	if err := json.Unmarshal([]byte(text), &profileResult); err != nil {
		t.Fatalf("unmarshal cluster_profile result: %v", err)
	}
	if profileResult.K8sVersion == "" {
		t.Error("cluster_profile: empty K8sVersion")
	}
	if len(profileResult.Nodes) == 0 {
		t.Error("cluster_profile: no nodes")
	}
	if profileResult.CNI.Name == "" {
		t.Error("cluster_profile: empty CNI name")
	}
	if profileResult.Namespace != nsName {
		t.Errorf("cluster_profile namespace: got %q, want %q", profileResult.Namespace, nsName)
	}
	if profileResult.Quota == nil || profileResult.Quota.CPULimit != "4" {
		t.Error("cluster_profile: expected quota with CPU=4")
	}
	t.Logf("cluster_profile: K8s=%s dist=%s CNI=%s nodes=%d",
		profileResult.K8sVersion, profileResult.Distribution,
		profileResult.CNI.Name, len(profileResult.Nodes))
}

// --- E2E: gVisor ---

// --- E2E: Health ---

func TestE2E_HealthNodesUsageSummary(t *testing.T) {
	session, client, cleanup := e2eEnv(t)
	defer cleanup()

	// health_nodes
	text := callTool(t, session, "health_nodes", map[string]any{})
	var nodesResult struct {
		Nodes []struct {
			Name        string `json:"name"`
			Ready       bool   `json:"ready"`
			CPUCapacity string `json:"cpu_capacity"`
			MemCapacity string `json:"mem_capacity"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(text), &nodesResult); err != nil {
		t.Fatalf("unmarshal health_nodes result: %v", err)
	}
	if len(nodesResult.Nodes) == 0 {
		t.Error("health_nodes: no nodes")
	}
	for _, n := range nodesResult.Nodes {
		if !n.Ready {
			t.Errorf("health_nodes: node %s not ready", n.Name)
		}
	}

	// health_ns_usage on a namespace with quota
	nsName := "tnt-e2e-health"
	cleanupNs(t, client, nsName)

	callTool(t, session, "ns_create", map[string]any{
		"name":         nsName,
		"quota_preset": "medium",
	})

	text = callTool(t, session, "health_ns_usage", map[string]any{"namespace": nsName})
	var usageResult struct {
		Namespace string  `json:"namespace"`
		CPULimit  string  `json:"cpu_limit"`
		MemLimit  string  `json:"mem_limit"`
		PodLimit  int     `json:"pod_limit"`
		CPUPct    float64 `json:"cpu_pct"`
	}
	if err := json.Unmarshal([]byte(text), &usageResult); err != nil {
		t.Fatalf("unmarshal health_ns_usage result: %v", err)
	}
	if usageResult.Namespace != nsName {
		t.Errorf("health_ns_usage namespace: got %q, want %q", usageResult.Namespace, nsName)
	}
	if usageResult.CPULimit == "" {
		t.Error("health_ns_usage: empty cpu_limit")
	}
	t.Logf("health_ns_usage: CPU=%s Mem=%s Pods=%d", usageResult.CPULimit, usageResult.MemLimit, usageResult.PodLimit)

	// health_cluster_summary
	text = callTool(t, session, "health_cluster_summary", map[string]any{})
	var summaryResult struct {
		TotalNodes  int    `json:"total_nodes"`
		ReadyNodes  int    `json:"ready_nodes"`
		TotalPods   int    `json:"total_pods"`
		CPUCapacity string `json:"cpu_capacity"`
		MemCapacity string `json:"mem_capacity"`
	}
	if err := json.Unmarshal([]byte(text), &summaryResult); err != nil {
		t.Fatalf("unmarshal health_cluster_summary result: %v", err)
	}
	if summaryResult.TotalNodes < 1 {
		t.Error("health_cluster_summary: expected at least 1 node")
	}
	if summaryResult.TotalPods < 1 {
		t.Error("health_cluster_summary: expected at least 1 pod")
	}
	t.Logf("health_cluster_summary: nodes=%d pods=%d", summaryResult.TotalNodes, summaryResult.TotalPods)
}

// --- E2E: Audit ---

func TestE2E_AuditRbacNetpolPsa(t *testing.T) {
	session, client, cleanup := e2eEnv(t)
	defer cleanup()

	nsName := "tnt-e2e-audit"
	cleanupNs(t, client, nsName)

	callTool(t, session, "ns_create", map[string]any{
		"name":         nsName,
		"quota_preset": "small",
	})

	// audit_rbac
	text := callTool(t, session, "audit_rbac", map[string]any{"namespace": nsName})
	var rbacResult struct {
		Findings []struct {
			Role     string `json:"role"`
			Severity string `json:"severity"`
			Reason   string `json:"reason"`
		} `json:"findings"`
	}
	if err := json.Unmarshal([]byte(text), &rbacResult); err != nil {
		t.Fatalf("unmarshal audit_rbac result: %v", err)
	}
	t.Logf("audit_rbac: %d findings", len(rbacResult.Findings))

	// audit_netpol
	text = callTool(t, session, "audit_netpol", map[string]any{"namespace": nsName})
	var netpolResult struct {
		DefaultDeny bool `json:"default_deny"`
		Policies    []struct {
			Name string `json:"name"`
		} `json:"policies"`
		Findings []struct {
			Severity string `json:"severity"`
			Message  string `json:"message"`
		} `json:"findings"`
	}
	if err := json.Unmarshal([]byte(text), &netpolResult); err != nil {
		t.Fatalf("unmarshal audit_netpol result: %v", err)
	}
	if !netpolResult.DefaultDeny {
		t.Error("audit_netpol: expected default_deny=true")
	}
	if len(netpolResult.Policies) < 2 {
		t.Errorf("audit_netpol: expected at least 2 policies, got %d", len(netpolResult.Policies))
	}

	// audit_psa
	text = callTool(t, session, "audit_psa", map[string]any{"namespace": nsName})
	var psaResult struct {
		Compliant bool   `json:"compliant"`
		Enforce   string `json:"enforce"`
	}
	if err := json.Unmarshal([]byte(text), &psaResult); err != nil {
		t.Fatalf("unmarshal audit_psa result: %v", err)
	}
	if psaResult.Enforce != "restricted" {
		t.Errorf("audit_psa enforce: got %q, want restricted", psaResult.Enforce)
	}
	t.Logf("audit_psa: compliant=%v enforce=%s", psaResult.Compliant, psaResult.Enforce)
}

// --- E2E: Workflow deploy lifecycle ---

func TestE2E_WorkflowApplyStatusRemove(t *testing.T) {
	session, client, cleanup := e2eEnv(t)
	defer cleanup()

	nsName := "tnt-e2e-deploy"
	cleanupNs(t, client, nsName)

	callTool(t, session, "ns_create", map[string]any{
		"name":         nsName,
		"quota_preset": "medium",
	})

	// wf_apply with a ConfigMap (simplest resource to create).
	manifests := []map[string]any{
		{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name": "test-config",
			},
			"data": map[string]any{
				"key": "value",
			},
		},
	}

	text := callTool(t, session, "wf_apply", map[string]any{
		"namespace": nsName,
		"name":      "test-app",
		"manifests": manifests,
	})
	var applyResult struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
		Created   int    `json:"created"`
		Updated   int    `json:"updated"`
		Deleted   int    `json:"deleted"`
	}
	if err := json.Unmarshal([]byte(text), &applyResult); err != nil {
		t.Fatalf("unmarshal wf_apply result: %v", err)
	}
	if applyResult.Created != 1 {
		t.Errorf("wf_apply created: got %d, want 1", applyResult.Created)
	}
	if applyResult.Name != "test-app" {
		t.Errorf("wf_apply name: got %q, want test-app", applyResult.Name)
	}

	// wf_status
	text = callTool(t, session, "wf_status", map[string]any{
		"namespace": nsName,
		"name":      "test-app",
	})
	var statusResult struct {
		Name      string `json:"name"`
		Resources []struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"resources"`
	}
	if err := json.Unmarshal([]byte(text), &statusResult); err != nil {
		t.Fatalf("unmarshal wf_status result: %v", err)
	}
	if len(statusResult.Resources) != 1 {
		t.Errorf("wf_status resources: got %d, want 1", len(statusResult.Resources))
	}

	// wf_apply again (update, should be idempotent).
	text = callTool(t, session, "wf_apply", map[string]any{
		"namespace": nsName,
		"name":      "test-app",
		"manifests": manifests,
	})
	if err := json.Unmarshal([]byte(text), &applyResult); err != nil {
		t.Fatalf("unmarshal wf_apply update result: %v", err)
	}
	// Second apply should update, not create.
	t.Logf("wf_apply update: created=%d updated=%d", applyResult.Created, applyResult.Updated)

	// wf_remove
	text = callTool(t, session, "wf_remove", map[string]any{
		"namespace": nsName,
		"name":      "test-app",
	})
	var removeResult struct {
		Name    string `json:"name"`
		Deleted int    `json:"deleted"`
	}
	if err := json.Unmarshal([]byte(text), &removeResult); err != nil {
		t.Fatalf("unmarshal wf_remove result: %v", err)
	}
	if removeResult.Deleted != 1 {
		t.Errorf("wf_remove deleted: got %d, want 1", removeResult.Deleted)
	}
}
