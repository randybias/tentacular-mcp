package tools

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"github.com/randybias/tentacular-mcp/pkg/guard"
	"github.com/randybias/tentacular-mcp/pkg/k8s"
	"github.com/randybias/tentacular-mcp/pkg/proxy"
)

func newTestClient() *k8s.Client {
	return &k8s.Client{
		Clientset: fake.NewClientset(),
		Config:    &rest.Config{Host: "https://test-cluster:6443"},
	}
}

func newTestServer() *mcp.Server {
	return mcp.NewServer(
		&mcp.Implementation{Name: "test", Version: "0.0.1"},
		&mcp.ServerOptions{Logger: slog.New(slog.NewTextHandler(os.Stderr, nil))},
	)
}

// TestRegisterAll verifies RegisterAll does not panic.
func TestRegisterAll(t *testing.T) {
	srv := newTestServer()
	client := newTestClient()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	reconciler := proxy.NewReconciler(client, proxy.Options{Namespace: "tentacular-system"}, logger)
	RegisterAll(srv, client, reconciler, nil, nil)
}

// TestAllExpectedToolsRegistered verifies that every expected MCP tool is registered
// by RegisterAll. This catches regressions where a tool definition exists in code
// but is not wired into the registration function. Regression test for issue #31.
func TestAllExpectedToolsRegistered(t *testing.T) {
	mcpSrv := newTestServer()
	client := newTestClient()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	reconciler := proxy.NewReconciler(client, proxy.Options{Namespace: "tentacular-system"}, logger)
	RegisterAll(mcpSrv, client, reconciler, nil, nil)

	handler := mcp.NewStreamableHTTPHandler(
		func(r *http.Request) *mcp.Server { return mcpSrv },
		&mcp.StreamableHTTPOptions{JSONResponse: true},
	)
	ts := httptest.NewServer(handler)
	defer ts.Close()

	acceptHdr := "application/json, text/event-stream"

	// Initialize the MCP session
	initBody := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}`
	initReq, _ := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader(initBody))
	initReq.Header.Set("Content-Type", "application/json")
	initReq.Header.Set("Accept", acceptHdr)
	initResp, err := http.DefaultClient.Do(initReq)
	if err != nil {
		t.Fatalf("POST initialize: %v", err)
	}
	sessionID := initResp.Header.Get("Mcp-Session-Id")
	_ = initResp.Body.Close()

	// Send initialized notification
	notifyBody := `{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`
	notifyReq, _ := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader(notifyBody))
	notifyReq.Header.Set("Content-Type", "application/json")
	notifyReq.Header.Set("Accept", acceptHdr)
	if sessionID != "" {
		notifyReq.Header.Set("Mcp-Session-Id", sessionID)
	}
	notifyResp, err := http.DefaultClient.Do(notifyReq)
	if err != nil {
		t.Fatalf("POST notifications/initialized: %v", err)
	}
	_ = notifyResp.Body.Close()

	// Call tools/list
	listBody := `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`
	listReq, _ := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader(listBody))
	listReq.Header.Set("Content-Type", "application/json")
	listReq.Header.Set("Accept", acceptHdr)
	if sessionID != "" {
		listReq.Header.Set("Mcp-Session-Id", sessionID)
	}
	listResp, err := http.DefaultClient.Do(listReq)
	if err != nil {
		t.Fatalf("POST tools/list: %v", err)
	}
	defer func() { _ = listResp.Body.Close() }()

	var result struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&result); err != nil {
		t.Fatalf("decode tools/list response: %v", err)
	}

	registered := make(map[string]bool, len(result.Result.Tools))
	for _, tool := range result.Result.Tools {
		registered[tool.Name] = true
	}

	// Complete list of all expected MCP tools
	expectedTools := []string{
		// namespace tools
		"ns_create", "ns_delete", "ns_get", "ns_list", "ns_update",
		// credential tools
		"cred_issue_token", "cred_kubeconfig", "cred_rotate",
		// workflow tools (including wf_restart - issue #31)
		"wf_pods", "wf_logs", "wf_events", "wf_jobs", "wf_restart",
		// run tools
		"wf_run",
		// discover tools
		"wf_list", "wf_describe",
		// cluster ops tools
		"cluster_preflight", "cluster_profile",
		// gvisor tools
		"gvisor_check", "gvisor_annotate_ns", "gvisor_verify",
		// deploy tools
		"wf_apply", "wf_remove", "wf_status",
		// health tools
		"health_nodes", "health_ns_usage", "health_cluster_summary",
		// wf health tools
		"wf_health", "wf_health_ns",
		// audit tools
		"audit_rbac", "audit_netpol", "audit_psa",
		// proxy tools
		"proxy_status",
		// exoskeleton tools
		"exo_status", "exo_registration", "exo_list",
	}

	for _, name := range expectedTools {
		if !registered[name] {
			t.Errorf("expected tool %q to be registered, but it was not found", name)
		}
	}

	// Log total registered count for informational purposes
	t.Logf("Total registered tools: %d, expected: %d", len(registered), len(expectedTools))
}

// TestGuardCheckNamespace verifies the guard rejects tentacular-system.
func TestGuardCheckNamespace(t *testing.T) {
	err := guard.CheckNamespace("tentacular-system")
	if err == nil {
		t.Fatal("expected error for tentacular-system, got nil")
	}

	err = guard.CheckNamespace("user-namespace")
	if err != nil {
		t.Fatalf("expected nil for user-namespace, got %v", err)
	}
}

// TestNsCreateSuccessWithFakeClient tests ns_create with a fake clientset.
func TestNsCreateSuccessWithFakeClient(t *testing.T) {
	client := newTestClient()
	ctx := context.Background()

	result, err := handleNsCreate(ctx, client, NsCreateParams{
		Name:        "test-ns",
		QuotaPreset: "small",
	})
	if err != nil {
		t.Fatalf("handleNsCreate failed: %v", err)
	}
	if result.Name != "test-ns" {
		t.Errorf("expected name=test-ns, got %s", result.Name)
	}
	if len(result.ResourcesCreated) == 0 {
		t.Error("expected resources_created to be non-empty")
	}
}

// TestNsCreateInvalidPreset tests ns_create with an invalid quota preset.
func TestNsCreateInvalidPreset(t *testing.T) {
	client := newTestClient()
	ctx := context.Background()

	_, err := handleNsCreate(ctx, client, NsCreateParams{
		Name:        "test-ns",
		QuotaPreset: "xlarge",
	})
	if err == nil {
		t.Fatal("expected error for invalid preset, got nil")
	}
}
