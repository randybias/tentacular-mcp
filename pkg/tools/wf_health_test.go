package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"github.com/randybias/tentacular-mcp/pkg/guard"
	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

// newWfHealthTestClient returns a fake k8s client for wf_health tests.
func newWfHealthTestClient() *k8s.Client {
	return &k8s.Client{
		Clientset: fake.NewClientset(),
		Config:    &rest.Config{Host: "https://test-cluster:6443"},
	}
}

// createManagedNamespace seeds a tentacular-managed namespace into the fake client.
func createManagedNamespace(ctx context.Context, client *k8s.Client, name string) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				k8s.ManagedByLabel: k8s.ManagedByValue,
			},
		},
	}
	client.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{}) //nolint:errcheck
}

// createOwnedNamespace creates a tentacular-managed namespace with ownership annotations.
func createOwnedNamespace(ctx context.Context, client *k8s.Client, name, ownerSub string) {
	createOwnedNamespaceWithMode(ctx, client, name, ownerSub, "rwxr-x---")
}

// createOwnedNamespaceWithMode creates a tentacular-managed namespace with specific mode.
func createOwnedNamespaceWithMode(ctx context.Context, client *k8s.Client, name, ownerSub, mode string) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				k8s.ManagedByLabel: k8s.ManagedByValue,
			},
			Annotations: map[string]string{
				"tentacular.io/owner-sub": ownerSub,
				"tentacular.io/mode":      mode,
			},
		},
	}
	client.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{}) //nolint:errcheck
}

const testNamespace = "user-ns"

// makeManagedDeployment creates a tentacular-managed deployment for tests.
func makeManagedDeployment(name string, readyReplicas int32) *appsv1.Deployment {
	replicas := int32(1)
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
			Labels: map[string]string{
				k8s.ManagedByLabel: k8s.ManagedByValue,
				k8s.NameLabel:      name,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
		},
		Status: appsv1.DeploymentStatus{
			ReadyReplicas: readyReplicas,
		},
	}
}

// withFakeProbe temporarily replaces wfHealthProbe with a fake and restores it after the test.
func withFakeProbe(t *testing.T, fake func(name, namespace string, detail bool) (string, error)) {
	t.Helper()
	orig := wfHealthProbe
	wfHealthProbe = fake
	t.Cleanup(func() { wfHealthProbe = orig })
}

// --- wf_health tests ---

func TestWfHealth_UnmanagedNamespaceRejected(t *testing.T) {
	client := newWfHealthTestClient()
	ctx := context.Background()
	// Namespace exists but is NOT managed by tentacular.
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "unmanaged-ns"},
	}
	client.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{}) //nolint:errcheck

	_, err := handleWfHealth(ctx, client, WfHealthParams{
		Namespace: "unmanaged-ns",
		Name:      "my-wf",
	}, bearerInfo(), bearerEval())
	if err == nil {
		t.Fatal("expected error for unmanaged namespace, got nil")
	}
}

func TestWfHealth_SystemNamespaceRejected(t *testing.T) {
	client := newWfHealthTestClient()
	ctx := context.Background()

	_, err := handleWfHealth(ctx, client, WfHealthParams{
		Namespace: "tentacular-system",
		Name:      "my-wf",
	}, bearerInfo(), bearerEval())
	if err == nil {
		t.Fatal("expected error for system namespace, got nil")
	}
}

func TestWfHealth_DeploymentNotFound(t *testing.T) {
	client := newWfHealthTestClient()
	ctx := context.Background()
	createManagedNamespace(ctx, client, "user-ns")

	_, err := handleWfHealth(ctx, client, WfHealthParams{
		Namespace: "user-ns",
		Name:      "missing-wf",
	}, bearerInfo(), bearerEval())
	if err == nil {
		t.Fatal("expected error for missing deployment, got nil")
	}
}

func TestWfHealth_ZeroReplicas_Red(t *testing.T) {
	client := newWfHealthTestClient()
	ctx := context.Background()
	createManagedNamespace(ctx, client, "user-ns")

	dep := makeManagedDeployment("my-wf", 0)
	_, _ = client.Clientset.AppsV1().Deployments("user-ns").Create(ctx, dep, metav1.CreateOptions{})

	result, err := handleWfHealth(ctx, client, WfHealthParams{
		Namespace: "user-ns",
		Name:      "my-wf",
	}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "red" {
		t.Errorf("expected status=red, got %q", result.Status)
	}
	if result.PodReady {
		t.Error("expected pod_ready=false")
	}
	if result.Reason == "" {
		t.Error("expected non-empty reason for red status")
	}
}

func TestWfHealth_HealthEndpointUnreachable_Red(t *testing.T) {
	client := newWfHealthTestClient()
	ctx := context.Background()
	createManagedNamespace(ctx, client, "user-ns")

	dep := makeManagedDeployment("my-wf", 1)
	_, _ = client.Clientset.AppsV1().Deployments("user-ns").Create(ctx, dep, metav1.CreateOptions{})

	withFakeProbe(t, func(name, namespace string, detail bool) (string, error) {
		return "", errors.New("connection refused")
	})

	result, err := handleWfHealth(ctx, client, WfHealthParams{
		Namespace: "user-ns",
		Name:      "my-wf",
	}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "red" {
		t.Errorf("expected status=red, got %q", result.Status)
	}
	if !result.PodReady {
		t.Error("expected pod_ready=true (pod up, endpoint unreachable)")
	}
}

func TestWfHealth_HealthyEndpoint_Green(t *testing.T) {
	client := newWfHealthTestClient()
	ctx := context.Background()
	createManagedNamespace(ctx, client, "user-ns")

	dep := makeManagedDeployment("my-wf", 1)
	_, _ = client.Clientset.AppsV1().Deployments("user-ns").Create(ctx, dep, metav1.CreateOptions{})

	withFakeProbe(t, func(name, namespace string, detail bool) (string, error) {
		return `{"healthy":true}`, nil
	})

	result, err := handleWfHealth(ctx, client, WfHealthParams{
		Namespace: "user-ns",
		Name:      "my-wf",
	}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "green" {
		t.Errorf("expected status=green, got %q", result.Status)
	}
	if !result.PodReady {
		t.Error("expected pod_ready=true")
	}
}

func TestWfHealth_LastExecutionFailed_Amber(t *testing.T) {
	client := newWfHealthTestClient()
	ctx := context.Background()
	createManagedNamespace(ctx, client, "user-ns")

	dep := makeManagedDeployment("my-wf", 1)
	_, _ = client.Clientset.AppsV1().Deployments("user-ns").Create(ctx, dep, metav1.CreateOptions{})

	withFakeProbe(t, func(name, namespace string, detail bool) (string, error) {
		return `{"lastRunFailed":true}`, nil
	})

	result, err := handleWfHealth(ctx, client, WfHealthParams{
		Namespace: "user-ns",
		Name:      "my-wf",
	}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "amber" {
		t.Errorf("expected status=amber, got %q", result.Status)
	}
}

func TestWfHealth_InFlight_Amber(t *testing.T) {
	client := newWfHealthTestClient()
	ctx := context.Background()
	createManagedNamespace(ctx, client, "user-ns")

	dep := makeManagedDeployment("my-wf", 1)
	_, _ = client.Clientset.AppsV1().Deployments("user-ns").Create(ctx, dep, metav1.CreateOptions{})

	withFakeProbe(t, func(name, namespace string, detail bool) (string, error) {
		return `{"inFlight":1}`, nil
	})

	result, err := handleWfHealth(ctx, client, WfHealthParams{
		Namespace: "user-ns",
		Name:      "my-wf",
	}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "amber" {
		t.Errorf("expected status=amber, got %q", result.Status)
	}
}

func TestWfHealth_DetailNotIncludedByDefault(t *testing.T) {
	client := newWfHealthTestClient()
	ctx := context.Background()
	createManagedNamespace(ctx, client, "user-ns")

	dep := makeManagedDeployment("my-wf", 1)
	_, _ = client.Clientset.AppsV1().Deployments("user-ns").Create(ctx, dep, metav1.CreateOptions{})

	withFakeProbe(t, func(name, namespace string, detail bool) (string, error) {
		return `{"healthy":true}`, nil
	})

	result, err := handleWfHealth(ctx, client, WfHealthParams{
		Namespace: "user-ns",
		Name:      "my-wf",
		Detail:    false,
	}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Detail != "" {
		t.Errorf("expected empty detail when Detail=false, got %q", result.Detail)
	}
}

func TestWfHealth_DetailIncludedWhenRequested(t *testing.T) {
	client := newWfHealthTestClient()
	ctx := context.Background()
	createManagedNamespace(ctx, client, "user-ns")

	dep := makeManagedDeployment("my-wf", 1)
	_, _ = client.Clientset.AppsV1().Deployments("user-ns").Create(ctx, dep, metav1.CreateOptions{})

	withFakeProbe(t, func(name, namespace string, detail bool) (string, error) {
		return `{"healthy":true}`, nil
	})

	result, err := handleWfHealth(ctx, client, WfHealthParams{
		Namespace: "user-ns",
		Name:      "my-wf",
		Detail:    true,
	}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Detail == "" {
		t.Error("expected non-empty detail when Detail=true")
	}
}

// --- wf_health_ns tests ---

func TestWfHealthNs_SystemNamespaceGuardRejected(t *testing.T) {
	// The guard is enforced in the tool registration closure, not in the handler.
	// Verify guard.CheckNamespace rejects system namespaces.
	for _, ns := range []string{"kube-system", "tentacular-system", "default"} {
		if err := guard.CheckNamespace(ns); err == nil {
			t.Errorf("expected guard to reject namespace %q, but it did not", ns)
		}
	}
}

func TestWfHealthNs_EmptyNamespace(t *testing.T) {
	client := newWfHealthTestClient()
	ctx := context.Background()
	createManagedNamespace(ctx, client, "empty-ns")

	result, err := handleWfHealthNs(ctx, client, WfHealthNsParams{Namespace: "empty-ns"}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Total != 0 {
		t.Errorf("expected total=0, got %d", result.Total)
	}
	if result.Truncated {
		t.Error("expected truncated=false for empty namespace")
	}
	if len(result.Workflows) != 0 {
		t.Errorf("expected 0 workflows, got %d", len(result.Workflows))
	}
}

func TestWfHealthNs_AllGreen(t *testing.T) {
	client := newWfHealthTestClient()
	ctx := context.Background()
	createManagedNamespace(ctx, client, "user-ns")

	for _, name := range []string{"wf-a", "wf-b"} {
		dep := makeManagedDeployment(name, 1)
		_, _ = client.Clientset.AppsV1().Deployments("user-ns").Create(ctx, dep, metav1.CreateOptions{})
	}

	withFakeProbe(t, func(name, namespace string, detail bool) (string, error) {
		return `{"healthy":true}`, nil
	})

	result, err := handleWfHealthNs(ctx, client, WfHealthNsParams{Namespace: "user-ns"}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Total != 2 {
		t.Errorf("expected total=2, got %d", result.Total)
	}
	if result.Summary.Green != 2 {
		t.Errorf("expected 2 green, got %d", result.Summary.Green)
	}
	if result.Summary.Amber != 0 || result.Summary.Red != 0 {
		t.Errorf("expected amber=0, red=0, got amber=%d, red=%d", result.Summary.Amber, result.Summary.Red)
	}
}

func TestWfHealthNs_MixedStatuses(t *testing.T) {
	client := newWfHealthTestClient()
	ctx := context.Background()
	createManagedNamespace(ctx, client, "user-ns")

	// wf-red: 0 ready replicas
	depRed := makeManagedDeployment("wf-red", 0)
	_, _ = client.Clientset.AppsV1().Deployments("user-ns").Create(ctx, depRed, metav1.CreateOptions{})
	// wf-amber: ready but lastRunFailed=true
	depAmber := makeManagedDeployment("wf-amber", 1)
	_, _ = client.Clientset.AppsV1().Deployments("user-ns").Create(ctx, depAmber, metav1.CreateOptions{})
	// wf-green: ready and healthy
	depGreen := makeManagedDeployment("wf-green", 1)
	_, _ = client.Clientset.AppsV1().Deployments("user-ns").Create(ctx, depGreen, metav1.CreateOptions{})

	withFakeProbe(t, func(name, namespace string, detail bool) (string, error) {
		if name == "wf-amber" {
			return `{"lastRunFailed":true}`, nil
		}
		return `{"healthy":true}`, nil
	})

	result, err := handleWfHealthNs(ctx, client, WfHealthNsParams{Namespace: "user-ns"}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Summary.Green != 1 {
		t.Errorf("expected green=1, got %d", result.Summary.Green)
	}
	if result.Summary.Amber != 1 {
		t.Errorf("expected amber=1, got %d", result.Summary.Amber)
	}
	if result.Summary.Red != 1 {
		t.Errorf("expected red=1, got %d", result.Summary.Red)
	}
}

func TestWfHealthNs_TruncatesAtLimit(t *testing.T) {
	client := newWfHealthTestClient()
	ctx := context.Background()
	createManagedNamespace(ctx, client, "user-ns")

	// Create 5 deployments but limit to 3
	for i := range 5 {
		dep := makeManagedDeployment(fmt.Sprintf("wf-%d", i), 1)
		_, _ = client.Clientset.AppsV1().Deployments("user-ns").Create(ctx, dep, metav1.CreateOptions{})
	}

	withFakeProbe(t, func(name, namespace string, detail bool) (string, error) {
		return `{"healthy":true}`, nil
	})

	result, err := handleWfHealthNs(ctx, client, WfHealthNsParams{Namespace: "user-ns", Limit: 3}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Total != 5 {
		t.Errorf("expected total=5, got %d", result.Total)
	}
	if !result.Truncated {
		t.Error("expected truncated=true")
	}
	if len(result.Workflows) != 3 {
		t.Errorf("expected 3 workflows in result, got %d", len(result.Workflows))
	}
}

func TestWfHealthNs_DefaultLimit(t *testing.T) {
	client := newWfHealthTestClient()
	ctx := context.Background()
	createManagedNamespace(ctx, client, "user-ns")

	// Create 5 deployments, no limit specified - should default to 20
	for i := range 5 {
		dep := makeManagedDeployment(fmt.Sprintf("wf-%d", i), 1)
		_, _ = client.Clientset.AppsV1().Deployments("user-ns").Create(ctx, dep, metav1.CreateOptions{})
	}

	withFakeProbe(t, func(name, namespace string, detail bool) (string, error) {
		return `{"healthy":true}`, nil
	})

	result, err := handleWfHealthNs(ctx, client, WfHealthNsParams{Namespace: "user-ns"}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Total != 5 {
		t.Errorf("expected total=5, got %d", result.Total)
	}
	if result.Truncated {
		t.Error("expected truncated=false when total <= default limit")
	}
	if len(result.Workflows) != 5 {
		t.Errorf("expected 5 workflows, got %d", len(result.Workflows))
	}
}

func TestWfHealthNs_NotTruncatedWhenExactlyAtLimit(t *testing.T) {
	client := newWfHealthTestClient()
	ctx := context.Background()
	createManagedNamespace(ctx, client, "user-ns")

	for i := range 3 {
		dep := makeManagedDeployment(fmt.Sprintf("wf-%d", i), 1)
		_, _ = client.Clientset.AppsV1().Deployments("user-ns").Create(ctx, dep, metav1.CreateOptions{})
	}

	withFakeProbe(t, func(name, namespace string, detail bool) (string, error) {
		return `{"healthy":true}`, nil
	})

	result, err := handleWfHealthNs(ctx, client, WfHealthNsParams{Namespace: "user-ns", Limit: 3}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Truncated {
		t.Error("expected truncated=false when total == limit")
	}
}

func TestWfHealthNs_OnlyListsManagedDeployments(t *testing.T) {
	client := newWfHealthTestClient()
	ctx := context.Background()
	createManagedNamespace(ctx, client, "user-ns")

	// Create one managed and one unmanaged deployment
	managed := makeManagedDeployment("managed-wf", 1)
	_, _ = client.Clientset.AppsV1().Deployments("user-ns").Create(ctx, managed, metav1.CreateOptions{})

	unmanaged := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "unmanaged-wf",
			Namespace: "user-ns",
			// No managed-by label
		},
		Spec: appsv1.DeploymentSpec{Replicas: int32Ptr(1)},
	}
	_, _ = client.Clientset.AppsV1().Deployments("user-ns").Create(ctx, unmanaged, metav1.CreateOptions{})

	withFakeProbe(t, func(name, namespace string, detail bool) (string, error) {
		return `{"healthy":true}`, nil
	})

	result, err := handleWfHealthNs(ctx, client, WfHealthNsParams{Namespace: "user-ns"}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Total != 1 {
		t.Errorf("expected total=1 (only managed deployment), got %d", result.Total)
	}
}

// --- classifyFromDetail tests ---

func TestClassifyFromDetail_Green(t *testing.T) {
	status, reason := classifyFromDetail(`{"healthy":true}`)
	if status != "green" {
		t.Errorf("expected green, got %q", status)
	}
	if reason != "" {
		t.Errorf("expected empty reason, got %q", reason)
	}
}

func TestClassifyFromDetail_AmberLastRunFailed(t *testing.T) {
	status, reason := classifyFromDetail(`{"lastRunFailed":true}`)
	if status != "amber" {
		t.Errorf("expected amber, got %q", status)
	}
	if reason == "" {
		t.Error("expected non-empty reason")
	}
}

func TestClassifyFromDetail_GreenLastRunNotFailed(t *testing.T) {
	status, _ := classifyFromDetail(`{"lastRunFailed":false}`)
	if status != "green" {
		t.Errorf("expected green for lastRunFailed=false, got %q", status)
	}
}

func TestClassifyFromDetail_AmberInFlight(t *testing.T) {
	status, reason := classifyFromDetail(`{"inFlight":1}`)
	if status != "amber" {
		t.Errorf("expected amber, got %q", status)
	}
	if reason == "" {
		t.Error("expected non-empty reason")
	}
}

func TestClassifyFromDetail_GreenNoRuns(t *testing.T) {
	// Pod ready, no executions yet - endpoint reachable with empty/healthy body
	status, _ := classifyFromDetail(`{"healthy":true,"execution_count":0}`)
	if status != "green" {
		t.Errorf("expected green for no executions, got %q", status)
	}
}

func TestClassifyFromDetail_EmptyBody(t *testing.T) {
	// An empty response body should not panic and should default to green.
	status, reason := classifyFromDetail("")
	if status != "green" {
		t.Errorf("expected green for empty body, got %q", status)
	}
	if reason != "" {
		t.Errorf("expected empty reason, got %q", reason)
	}
}

func TestClassifyFromDetail_InFlightZeroIsGreen(t *testing.T) {
	status, _ := classifyFromDetail(`{"inFlight":0}`)
	if status != "green" {
		t.Errorf("expected green for inFlight=0, got %q", status)
	}
}

func TestClassifyFromDetail_BothAmberSignals(t *testing.T) {
	// When both signals are present, lastRunFailed takes priority (checked first).
	status, reason := classifyFromDetail(`{"lastRunFailed":true,"inFlight":1}`)
	if status != "amber" {
		t.Errorf("expected amber, got %q", status)
	}
	if reason == "" {
		t.Error("expected non-empty reason")
	}
}

// --- wfHealthURL tests ---

func TestWfHealthURL_NoDetail(t *testing.T) {
	url := wfHealthURL("my-wf", "user-ns", false)
	expected := "http://my-wf.user-ns.svc.cluster.local:8080/health"
	if url != expected {
		t.Errorf("wfHealthURL(detail=false): got %q, want %q", url, expected)
	}
}

func TestWfHealthURL_WithDetail(t *testing.T) {
	url := wfHealthURL("my-wf", "user-ns", true)
	expected := "http://my-wf.user-ns.svc.cluster.local:8080/health?detail=1"
	if url != expected {
		t.Errorf("wfHealthURL(detail=true): got %q, want %q", url, expected)
	}
}

// --- probeURL tests ---

func TestProbeURL_NonTwoxxStatusReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error")) //nolint:errcheck
	}))
	defer srv.Close()

	_, err := probeURL(srv.URL)
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
}

func TestProbeURL_SuccessReturnsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"healthy":true}`)) //nolint:errcheck
	}))
	defer srv.Close()

	body, err := probeURL(srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if body != `{"healthy":true}` {
		t.Errorf("unexpected body: %q", body)
	}
}

func TestProbeURL_ConnectionRefusedReturnsError(t *testing.T) {
	// Use a port that is not listening.
	_, err := probeURL("http://127.0.0.1:19999")
	if err == nil {
		t.Fatal("expected error for unreachable server, got nil")
	}
}

// --- additional handleWfHealth tests ---

func TestWfHealth_ResultFieldsPopulated(t *testing.T) {
	client := newWfHealthTestClient()
	ctx := context.Background()
	createManagedNamespace(ctx, client, "user-ns")

	dep := makeManagedDeployment("my-wf", 1)
	_, _ = client.Clientset.AppsV1().Deployments("user-ns").Create(ctx, dep, metav1.CreateOptions{})

	withFakeProbe(t, func(name, namespace string, detail bool) (string, error) {
		return `{"healthy":true}`, nil
	})

	result, err := handleWfHealth(ctx, client, WfHealthParams{
		Namespace: "user-ns",
		Name:      "my-wf",
	}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Name != "my-wf" {
		t.Errorf("name: got %q, want %q", result.Name, "my-wf")
	}
	if result.Namespace != "user-ns" {
		t.Errorf("namespace: got %q, want %q", result.Namespace, "user-ns")
	}
}

func TestWfHealth_DetailFlagPassedToProbe(t *testing.T) {
	client := newWfHealthTestClient()
	ctx := context.Background()
	createManagedNamespace(ctx, client, "user-ns")

	dep := makeManagedDeployment("my-wf", 1)
	_, _ = client.Clientset.AppsV1().Deployments("user-ns").Create(ctx, dep, metav1.CreateOptions{})

	var probeDetailFlag bool
	withFakeProbe(t, func(name, namespace string, detail bool) (string, error) {
		probeDetailFlag = detail
		return `{"healthy":true}`, nil
	})

	_, err := handleWfHealth(ctx, client, WfHealthParams{
		Namespace: "user-ns",
		Name:      "my-wf",
		Detail:    true,
	}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !probeDetailFlag {
		t.Error("expected probe to receive detail=true")
	}
}

// --- additional handleWfHealthNs tests ---

func TestWfHealthNs_ProbeUnreachableCountsAsRed(t *testing.T) {
	client := newWfHealthTestClient()
	ctx := context.Background()
	createManagedNamespace(ctx, client, "user-ns")

	dep := makeManagedDeployment("wf-up", 1)
	_, _ = client.Clientset.AppsV1().Deployments("user-ns").Create(ctx, dep, metav1.CreateOptions{})

	withFakeProbe(t, func(name, namespace string, detail bool) (string, error) {
		return "", errors.New("connection refused")
	})

	result, err := handleWfHealthNs(ctx, client, WfHealthNsParams{Namespace: "user-ns"}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Summary.Red != 1 {
		t.Errorf("expected red=1 for unreachable probe, got %d", result.Summary.Red)
	}
	if result.Workflows[0].Reason == "" {
		t.Error("expected non-empty reason for unreachable probe")
	}
}

func TestWfHealthNs_SummaryTotalsMatchWorkflowCount(t *testing.T) {
	client := newWfHealthTestClient()
	ctx := context.Background()
	createManagedNamespace(ctx, client, "user-ns")

	// 2 green (ready, healthy), 1 red (no replicas)
	for _, name := range []string{"wf-a", "wf-b"} {
		dep := makeManagedDeployment(name, 1)
		_, _ = client.Clientset.AppsV1().Deployments("user-ns").Create(ctx, dep, metav1.CreateOptions{})
	}
	depRed := makeManagedDeployment("wf-c", 0)
	_, _ = client.Clientset.AppsV1().Deployments("user-ns").Create(ctx, depRed, metav1.CreateOptions{})

	withFakeProbe(t, func(name, namespace string, detail bool) (string, error) {
		return `{"healthy":true}`, nil
	})

	result, err := handleWfHealthNs(ctx, client, WfHealthNsParams{Namespace: "user-ns"}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	total := result.Summary.Green + result.Summary.Amber + result.Summary.Red
	if total != len(result.Workflows) {
		t.Errorf("summary totals (%d) don't match workflow count (%d)", total, len(result.Workflows))
	}
	if total != result.Total {
		t.Errorf("summary totals (%d) don't match result.Total (%d)", total, result.Total)
	}
}
