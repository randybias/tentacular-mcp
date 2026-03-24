package tools

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	nodev1 "k8s.io/api/node/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"github.com/randybias/tentacular-mcp/pkg/exoskeleton"
	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

func newClusterOpsTestClient() *k8s.Client {
	return &k8s.Client{
		Clientset: fake.NewClientset(),
		Config:    &rest.Config{Host: "https://test-cluster:6443"},
	}
}

// TestClusterPreflightReturnsChecks verifies preflight returns results.
func TestClusterPreflightReturnsChecks(t *testing.T) {
	client := newClusterOpsTestClient()
	ctx := context.Background()

	result, err := handleClusterPreflight(ctx, client, ClusterPreflightParams{Namespace: "my-ns"})
	if err != nil {
		t.Fatalf("handleClusterPreflight: %v", err)
	}

	if len(result.Checks) == 0 {
		t.Error("expected at least one check result")
	}
}

// TestClusterPreflightAPIReachabilityCheck verifies api-reachability check is present.
func TestClusterPreflightAPIReachabilityCheck(t *testing.T) {
	client := newClusterOpsTestClient()
	ctx := context.Background()

	result, err := handleClusterPreflight(ctx, client, ClusterPreflightParams{Namespace: "test-ns"})
	if err != nil {
		t.Fatalf("handleClusterPreflight: %v", err)
	}

	foundAPI := false
	for _, c := range result.Checks {
		if c.Name == "api-reachability" {
			foundAPI = true
			if !c.Passed {
				t.Error("expected api-reachability to pass with fake client")
			}
		}
	}
	if !foundAPI {
		t.Error("expected api-reachability check in results")
	}
}

// TestClusterPreflightNamespaceMissingFails verifies namespace-exists fails for non-existent ns.
func TestClusterPreflightNamespaceMissingFails(t *testing.T) {
	client := newClusterOpsTestClient()
	ctx := context.Background()

	result, err := handleClusterPreflight(ctx, client, ClusterPreflightParams{Namespace: "ghost-ns"})
	if err != nil {
		t.Fatalf("handleClusterPreflight: %v", err)
	}

	foundNS := false
	for _, c := range result.Checks {
		if c.Name == "namespace-exists" {
			foundNS = true
			if c.Passed {
				t.Error("expected namespace-exists to fail for non-existent namespace")
			}
			if c.Remediation == "" {
				t.Error("expected Remediation to be set for failed namespace check")
			}
		}
	}
	if !foundNS {
		t.Error("expected namespace-exists check in results")
	}
}

// TestClusterPreflightNamespaceExists verifies namespace-exists passes when ns exists.
func TestClusterPreflightNamespaceExists(t *testing.T) {
	client := newClusterOpsTestClient()
	ctx := context.Background()

	_, _ = client.Clientset.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "existing-ns"},
	}, metav1.CreateOptions{})

	result, err := handleClusterPreflight(ctx, client, ClusterPreflightParams{Namespace: "existing-ns"})
	if err != nil {
		t.Fatalf("handleClusterPreflight: %v", err)
	}

	for _, c := range result.Checks {
		if c.Name == "namespace-exists" && !c.Passed {
			t.Error("expected namespace-exists to pass for existing namespace")
		}
	}
}

// TestClusterPreflightGVisorCheckPresent verifies gvisor check is in results.
func TestClusterPreflightGVisorCheckPresent(t *testing.T) {
	client := newClusterOpsTestClient()
	ctx := context.Background()

	result, err := handleClusterPreflight(ctx, client, ClusterPreflightParams{Namespace: "test-ns"})
	if err != nil {
		t.Fatalf("handleClusterPreflight: %v", err)
	}

	found := false
	for _, c := range result.Checks {
		if c.Name == "gvisor-runtime" {
			found = true
		}
	}
	if !found {
		t.Error("expected gvisor-runtime check in preflight results")
	}
}

// TestClusterProfileReturnsProfile verifies cluster_profile returns a profile.
func TestClusterProfileReturnsProfile(t *testing.T) {
	client := newClusterOpsTestClient()
	ctx := context.Background()

	result, err := handleClusterProfile(ctx, client, nil, ClusterProfileParams{})
	if err != nil {
		t.Fatalf("handleClusterProfile: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil profile")
	}
}

// TestClusterProfileExtensionsSet verifies the Extensions struct is populated.
func TestClusterProfileExtensionsSet(t *testing.T) {
	client := newClusterOpsTestClient()
	ctx := context.Background()

	result, err := handleClusterProfile(ctx, client, nil, ClusterProfileParams{})
	if err != nil {
		t.Fatalf("handleClusterProfile: %v", err)
	}
	// Extensions is a struct (not a map); verify it is present and zero extensions are detected
	// against an empty fake cluster.
	if result.Extensions.Istio || result.Extensions.CertManager || result.Extensions.ArgoCD {
		t.Error("expected no well-known extensions in empty fake cluster")
	}
}

// TestClusterProfileWithNamespace verifies profile includes namespace details when specified.
func TestClusterProfileWithNamespace(t *testing.T) {
	client := newClusterOpsTestClient()
	ctx := context.Background()

	_, _ = client.Clientset.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "profiled-ns",
			Labels: map[string]string{
				"pod-security.kubernetes.io/enforce": "restricted",
			},
		},
	}, metav1.CreateOptions{})

	result, err := handleClusterProfile(ctx, client, nil, ClusterProfileParams{Namespace: "profiled-ns"})
	if err != nil {
		t.Fatalf("handleClusterProfile: %v", err)
	}
	if result.Namespace != "profiled-ns" {
		t.Errorf("expected Namespace=profiled-ns, got %q", result.Namespace)
	}
	if result.PodSecurity != "restricted" {
		t.Errorf("expected PodSecurity=restricted, got %q", result.PodSecurity)
	}
}

// TestClusterProfileGVisorDetected verifies gVisor flag is set when RuntimeClass present.
func TestClusterProfileGVisorDetected(t *testing.T) {
	client := newClusterOpsTestClient()
	ctx := context.Background()

	rc := &nodev1.RuntimeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "gvisor"},
		Handler:    "runsc",
	}
	_, _ = client.Clientset.NodeV1().RuntimeClasses().Create(ctx, rc, metav1.CreateOptions{})

	result, err := handleClusterProfile(ctx, client, nil, ClusterProfileParams{})
	if err != nil {
		t.Fatalf("handleClusterProfile: %v", err)
	}
	if !result.GVisor {
		t.Error("expected GVisor=true when RuntimeClass with runsc handler exists")
	}
}

// TestClusterProfileWithExoskeleton verifies exoskeleton info is included when controller is provided.
func TestClusterProfileWithExoskeleton(t *testing.T) {
	client := newClusterOpsTestClient()
	ctx := context.Background()

	cfg := &exoskeleton.Config{
		Enabled: true,
		Postgres: exoskeleton.PostgresConfig{
			Host: "pg.example.com",
			Port: "5432",
		},
		NATS: exoskeleton.NATSConfig{
			URL: "nats://nats.example.com:4222",
		},
		RustFS: exoskeleton.RustFSConfig{
			Endpoint: "http://rustfs.example.com:9000",
		},
	}
	exoCtrl := exoskeleton.NewControllerWithDeps(cfg, nil, nil, nil, nil)

	result, err := handleClusterProfile(ctx, client, exoCtrl, ClusterProfileParams{})
	if err != nil {
		t.Fatalf("handleClusterProfile: %v", err)
	}
	if result.Exoskeleton == nil {
		t.Fatal("expected Exoskeleton to be non-nil when controller provided")
	}
	if !result.Exoskeleton.Enabled {
		t.Error("expected Exoskeleton.Enabled=true")
	}
	if len(result.Exoskeleton.Services) == 0 {
		t.Error("expected at least one service in Exoskeleton.Services")
	}

	// Verify postgres service is present
	foundPG := false
	for _, svc := range result.Exoskeleton.Services {
		if svc.Name == "postgres" {
			foundPG = true
			if svc.Host != "pg.example.com" {
				t.Errorf("postgres host = %q, want pg.example.com", svc.Host)
			}
			if svc.Port != "5432" {
				t.Errorf("postgres port = %q, want 5432", svc.Port)
			}
			// No registrar provided, so not available
			if svc.Available {
				t.Error("expected postgres Available=false with nil registrar")
			}
		}
	}
	if !foundPG {
		t.Error("expected postgres service in Exoskeleton.Services")
	}
}

// TestClusterProfileNilExoskeleton verifies backward compatibility when controller is nil.
func TestClusterProfileNilExoskeleton(t *testing.T) {
	client := newClusterOpsTestClient()
	ctx := context.Background()

	result, err := handleClusterProfile(ctx, client, nil, ClusterProfileParams{})
	if err != nil {
		t.Fatalf("handleClusterProfile: %v", err)
	}
	if result.Exoskeleton != nil {
		t.Error("expected Exoskeleton to be nil when controller is nil")
	}
}
