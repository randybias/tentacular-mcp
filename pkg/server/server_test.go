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

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	cs := fake.NewClientset()
	client := k8s.NewClientFromConfig(cs, nil, &rest.Config{Host: "https://fake:6443"}, nil)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	reconciler := proxy.NewReconciler(client, proxy.Options{Namespace: "tentacular-support"}, logger)
	srv, err := server.New(client, reconciler, nil, nil, authz.NewEvaluator(authz.DefaultMode), nil, testServerToken, logger)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func TestHealthEndpoint_Returns200(t *testing.T) {
	ts := newTestServer(t)

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
	ts := newTestServer(t)

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
	ts := newTestServer(t)

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
	ts := newTestServer(t)

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
