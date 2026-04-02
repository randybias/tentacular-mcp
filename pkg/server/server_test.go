package server_test

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"github.com/randybias/tentacular-mcp/pkg/authz"
	"github.com/randybias/tentacular-mcp/pkg/k8s"
	"github.com/randybias/tentacular-mcp/pkg/proxy"
	"github.com/randybias/tentacular-mcp/pkg/server"
)

const testServerToken = "server-test-token-xyz"

func newTestServer(t *testing.T, resourceMeta *server.ResourceMetadataConfig) *httptest.Server {
	t.Helper()
	cs := fake.NewClientset()
	client := k8s.NewClientFromConfig(cs, nil, &rest.Config{Host: "https://fake:6443"}, nil)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	reconciler := proxy.NewReconciler(client, proxy.Options{Namespace: "tentacular-support"}, logger)
	srv, err := server.New(client, reconciler, nil, nil, authz.NewEvaluator(authz.DefaultMode), nil, resourceMeta, testServerToken, logger)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func TestHealthEndpoint_Returns200(t *testing.T) {
	ts := newTestServer(t, nil)

	resp, err := ts.Client().Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestHealthEndpoint_JSONBody(t *testing.T) {
	ts := newTestServer(t, nil)

	resp, err := ts.Client().Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("expected status=ok, got %q", body["status"])
	}
}

func TestMCPEndpoint_RequiresAuth(t *testing.T) {
	ts := newTestServer(t, nil)

	// POST to /mcp without auth should be 401.
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", nil)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("POST /mcp: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 for unauthenticated /mcp, got %d", resp.StatusCode)
	}
}

func TestMCPEndpoint_WithValidToken(t *testing.T) {
	ts := newTestServer(t, nil)

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+testServerToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("POST /mcp: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// With valid auth, should not be 401.
	if resp.StatusCode == http.StatusUnauthorized {
		t.Errorf("expected non-401 with valid token, got 401")
	}
}

func TestResourceMetadataEndpoint_ReturnsJSON(t *testing.T) {
	meta := &server.ResourceMetadataConfig{
		Resource:               "https://mcp.example.com/mcp",
		AuthorizationServers:   []string{"https://auth.example.com/realms/test"},
		ScopesSupported:        []string{"openid", "email", "profile"},
		BearerMethodsSupported: []string{"header"},
		ResourceName:           "Test MCP Server",
	}
	ts := newTestServer(t, meta)

	resp, err := ts.Client().Get(ts.URL + "/.well-known/oauth-protected-resource")
	if err != nil {
		t.Fatalf("GET well-known: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}
	if cors := resp.Header.Get("Access-Control-Allow-Origin"); cors != "*" {
		t.Errorf("expected CORS *, got %q", cors)
	}

	var got server.ResourceMetadataConfig
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Resource != "https://mcp.example.com/mcp" {
		t.Errorf("resource mismatch: got %q", got.Resource)
	}
	if len(got.AuthorizationServers) != 1 || got.AuthorizationServers[0] != "https://auth.example.com/realms/test" {
		t.Errorf("authorization_servers mismatch: got %v", got.AuthorizationServers)
	}
	if got.ResourceName != "Test MCP Server" {
		t.Errorf("resource_name mismatch: got %q", got.ResourceName)
	}
}

func TestResourceMetadataEndpoint_DisabledWhenNil(t *testing.T) {
	ts := newTestServer(t, nil)

	resp, err := ts.Client().Get(ts.URL + "/.well-known/oauth-protected-resource")
	if err != nil {
		t.Fatalf("GET well-known: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 when resourceMeta is nil, got %d", resp.StatusCode)
	}
}

func TestResourceMetadataEndpoint_MethodNotAllowed(t *testing.T) {
	meta := &server.ResourceMetadataConfig{
		Resource:             "https://mcp.example.com/mcp",
		AuthorizationServers: []string{"https://auth.example.com/realms/test"},
	}
	ts := newTestServer(t, meta)

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/.well-known/oauth-protected-resource", nil)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("POST well-known: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for POST, got %d", resp.StatusCode)
	}
}

func TestMCPEndpoint_401_HasWWWAuthenticate(t *testing.T) {
	meta := &server.ResourceMetadataConfig{
		Resource:             "https://mcp.example.com/mcp",
		AuthorizationServers: []string{"https://auth.example.com/realms/test"},
	}
	ts := newTestServer(t, meta)

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", nil)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("POST /mcp: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}

	wwwAuth := resp.Header.Get("WWW-Authenticate")
	if wwwAuth == "" {
		t.Fatal("expected WWW-Authenticate header on 401, got empty")
	}
	expected := `Bearer resource_metadata="https://mcp.example.com/.well-known/oauth-protected-resource"`
	if wwwAuth != expected {
		t.Errorf("WWW-Authenticate mismatch\n  got:  %s\n  want: %s", wwwAuth, expected)
	}
}
