package tools

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

func newNsTestClient() *k8s.Client {
	return &k8s.Client{
		Clientset: fake.NewClientset(),
		Config:    &rest.Config{Host: "https://test-cluster:6443"},
	}
}

func TestNsCreateOrchestration(t *testing.T) {
	client := newNsTestClient()
	ctx := context.Background()

	result, err := handleNsCreate(ctx, client, bearerEval(), NsCreateParams{
		Name:        "dev-alice",
		QuotaPreset: "small",
	}, nil)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	if result.Name != "dev-alice" {
		t.Errorf("name: got %q, want %q", result.Name, "dev-alice")
	}
	if result.QuotaPreset != "small" {
		t.Errorf("quota_preset: got %q, want %q", result.QuotaPreset, "small")
	}

	// Expect 8 resources created
	expectedCount := 8
	if len(result.ResourcesCreated) != expectedCount {
		t.Errorf("resources_created: got %d, want %d: %v", len(result.ResourcesCreated), expectedCount, result.ResourcesCreated)
	}
}

func TestNsDeleteManagedCheck(t *testing.T) {
	client := newNsTestClient()
	ctx := context.Background()

	// Create a managed namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "dev-bob",
			Labels: map[string]string{
				k8s.ManagedByLabel: k8s.ManagedByValue,
			},
		},
	}
	_, err := client.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("setup: create ns: %v", err)
	}

	result, err := handleNsDelete(ctx, client, bearerEval(), NsDeleteParams{Name: "dev-bob"}, nil)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if !result.Deleted {
		t.Error("expected deleted=true")
	}
}

func TestNsDeleteUnmanagedRejects(t *testing.T) {
	client := newNsTestClient()
	ctx := context.Background()

	// Create an unmanaged namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "unmanaged-ns"},
	}
	_, err := client.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("setup: create ns: %v", err)
	}

	_, err = handleNsDelete(ctx, client, bearerEval(), NsDeleteParams{Name: "unmanaged-ns"}, nil)
	if err == nil {
		t.Fatal("expected error for unmanaged namespace, got nil")
	}
}

func TestNsList(t *testing.T) {
	client := newNsTestClient()
	ctx := context.Background()

	for _, name := range []string{"managed-1", "managed-2"} {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
				Labels: map[string]string{
					k8s.ManagedByLabel: k8s.ManagedByValue,
				},
			},
		}
		_, err := client.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("setup: create ns %q: %v", name, err)
		}
	}

	// Create one unmanaged
	unmanaged := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "not-managed"},
	}
	_, err := client.Clientset.CoreV1().Namespaces().Create(ctx, unmanaged, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("setup: create unmanaged ns: %v", err)
	}

	result, err := handleNsList(ctx, client, bearerEval(), nil)
	if err != nil {
		t.Fatalf("handleNsList: %v", err)
	}

	if len(result.Namespaces) != 2 {
		t.Errorf("expected 2 managed namespaces, got %d", len(result.Namespaces))
	}
}

// --- ns_update tests ---

func createManagedNsWithQuota(t *testing.T, client *k8s.Client, name string) {
	t.Helper()
	ctx := context.Background()
	_, err := handleNsCreate(ctx, client, bearerEval(), NsCreateParams{Name: name, QuotaPreset: "small"}, nil)
	if err != nil {
		t.Fatalf("setup: create managed ns %q: %v", name, err)
	}
}

func TestNsUpdateLabels(t *testing.T) {
	client := newNsTestClient()
	ctx := context.Background()
	createManagedNsWithQuota(t, client, "upd-labels")

	result, err := handleNsUpdate(ctx, client, bearerEval(), NsUpdateParams{
		Name:   "upd-labels",
		Labels: map[string]string{"env": "staging"},
	}, nil)
	if err != nil {
		t.Fatalf("handleNsUpdate: %v", err)
	}
	if len(result.Updated) != 1 || result.Updated[0] != "labels" {
		t.Errorf("expected updated=[labels], got %v", result.Updated)
	}

	// Verify label was applied.
	ns, _ := k8s.GetNamespace(ctx, client, "upd-labels")
	if ns.Labels["env"] != "staging" {
		t.Errorf("expected label env=staging, got %q", ns.Labels["env"])
	}
	// Managed-by label must still be present.
	if ns.Labels[k8s.ManagedByLabel] != k8s.ManagedByValue {
		t.Error("managed-by label was lost after update")
	}
}

func TestNsUpdateAnnotations(t *testing.T) {
	client := newNsTestClient()
	ctx := context.Background()
	createManagedNsWithQuota(t, client, "upd-annot")

	result, err := handleNsUpdate(ctx, client, bearerEval(), NsUpdateParams{
		Name:        "upd-annot",
		Annotations: map[string]string{"team": "platform"},
	}, nil)
	if err != nil {
		t.Fatalf("handleNsUpdate: %v", err)
	}
	if len(result.Updated) != 1 || result.Updated[0] != "annotations" {
		t.Errorf("expected updated=[annotations], got %v", result.Updated)
	}

	ns, _ := k8s.GetNamespace(ctx, client, "upd-annot")
	if ns.Annotations["team"] != "platform" {
		t.Errorf("expected annotation team=platform, got %q", ns.Annotations["team"])
	}
}

func TestNsUpdateQuota(t *testing.T) {
	client := newNsTestClient()
	ctx := context.Background()
	createManagedNsWithQuota(t, client, "upd-quota")

	result, err := handleNsUpdate(ctx, client, bearerEval(), NsUpdateParams{
		Name:        "upd-quota",
		QuotaPreset: "large",
	}, nil)
	if err != nil {
		t.Fatalf("handleNsUpdate: %v", err)
	}
	if len(result.Updated) != 1 || result.Updated[0] != "quota" {
		t.Errorf("expected updated=[quota], got %v", result.Updated)
	}

	// Verify quota was updated to large preset (CPU=8).
	quotas, _ := client.Clientset.CoreV1().ResourceQuotas("upd-quota").List(ctx, metav1.ListOptions{})
	if len(quotas.Items) == 0 {
		t.Fatal("no resource quota found")
	}
	cpu := quotas.Items[0].Spec.Hard[corev1.ResourceLimitsCPU]
	if cpu.String() != "8" {
		t.Errorf("expected CPU limit=8, got %q", cpu.String())
	}
}

func TestNsUpdateAllFields(t *testing.T) {
	client := newNsTestClient()
	ctx := context.Background()
	createManagedNsWithQuota(t, client, "upd-all")

	result, err := handleNsUpdate(ctx, client, bearerEval(), NsUpdateParams{
		Name:        "upd-all",
		Labels:      map[string]string{"env": "prod"},
		Annotations: map[string]string{"owner": "alice"},
		QuotaPreset: "medium",
	}, nil)
	if err != nil {
		t.Fatalf("handleNsUpdate: %v", err)
	}
	if len(result.Updated) != 3 {
		t.Errorf("expected 3 updated items, got %v", result.Updated)
	}
}

func TestNsUpdateRejectsUnmanaged(t *testing.T) {
	client := newNsTestClient()
	ctx := context.Background()

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "foreign-ns"},
	}
	_, _ = client.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})

	_, err := handleNsUpdate(ctx, client, bearerEval(), NsUpdateParams{
		Name:   "foreign-ns",
		Labels: map[string]string{"env": "test"},
	}, nil)
	if err == nil {
		t.Fatal("expected error for unmanaged namespace, got nil")
	}
}

func TestNsUpdateRejectsEmptyParams(t *testing.T) {
	client := newNsTestClient()
	ctx := context.Background()
	createManagedNsWithQuota(t, client, "upd-empty")

	_, err := handleNsUpdate(ctx, client, bearerEval(), NsUpdateParams{Name: "upd-empty"}, nil)
	if err == nil {
		t.Fatal("expected error when no update fields provided, got nil")
	}
}

func TestNsUpdateRejectsManagedByLabelChange(t *testing.T) {
	client := newNsTestClient()
	ctx := context.Background()
	createManagedNsWithQuota(t, client, "upd-protect")

	_, err := handleNsUpdate(ctx, client, bearerEval(), NsUpdateParams{
		Name:   "upd-protect",
		Labels: map[string]string{k8s.ManagedByLabel: "someone-else"},
	}, nil)
	if err == nil {
		t.Fatal("expected error when trying to change managed-by label, got nil")
	}
}
