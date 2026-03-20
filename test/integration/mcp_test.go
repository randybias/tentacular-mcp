//go:build integration

package integration_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/randybias/tentacular-mcp/pkg/server"
)

func TestIntegration_MCPHealthEndpoint(t *testing.T) {
	client := integrationClient(t)

	srv, err := server.New(client, nil, nil, nil, nil, nil, "test-token-health", slog.Default())
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"status":"ok"`) {
		t.Errorf("unexpected health response: %s", body)
	}
}

func TestIntegration_MCPAuthRejected(t *testing.T) {
	client := integrationClient(t)

	srv, err := server.New(client, nil, nil, nil, nil, nil, "test-token-auth", slog.Default())
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// POST to /mcp without Authorization header.
	resp, err := http.Post(ts.URL+"/mcp", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("POST /mcp: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 without auth, got %d", resp.StatusCode)
	}
}

func TestIntegration_MCPAuthWithBadToken(t *testing.T) {
	client := integrationClient(t)

	srv, err := server.New(client, nil, nil, nil, nil, nil, "test-token-bad", slog.Default())
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, _ := http.NewRequest("POST", ts.URL+"/mcp", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer wrong-token")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /mcp with bad token: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 with bad token, got %d", resp.StatusCode)
	}
}

func TestIntegration_MCPAuthAccepted(t *testing.T) {
	client := integrationClient(t)
	token := "test-token-accepted"

	srv, err := server.New(client, nil, nil, nil, nil, nil, token, slog.Default())
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Send a valid MCP initialize request.
	mcpInit := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}`
	req, _ := http.NewRequest("POST", ts.URL+"/mcp", strings.NewReader(mcpInit))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /mcp with valid token: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		t.Errorf("expected non-401 with valid token, got %d", resp.StatusCode)
	}
}
