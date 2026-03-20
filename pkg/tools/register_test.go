package tools

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"github.com/randybias/tentacular-mcp/pkg/authz"
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
	RegisterAll(srv, client, reconciler, nil, nil, authz.NewEvaluator(authz.DefaultMode))
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

	result, err := handleNsCreate(ctx, client, bearerEval(), NsCreateParams{
		Name:        "test-ns",
		QuotaPreset: "small",
	}, nil)
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

	_, err := handleNsCreate(ctx, client, bearerEval(), NsCreateParams{
		Name:        "test-ns",
		QuotaPreset: "xlarge",
	}, nil)
	if err == nil {
		t.Fatal("expected error for invalid preset, got nil")
	}
}
