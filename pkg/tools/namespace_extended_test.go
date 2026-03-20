package tools

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

// ---------- handleNsGet tests ----------

func TestHandleNsGet_Basic(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-ns",
			Labels: map[string]string{
				k8s.ManagedByLabel: k8s.ManagedByValue,
			},
			Annotations: map[string]string{
				"tentacular.dev/owner": "platform-team",
			},
		},
		Status: corev1.NamespaceStatus{Phase: corev1.NamespaceActive},
	}
	_, err := client.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	result, err := handleNsGet(ctx, client, bearerEval(), NsGetParams{Name: "test-ns"}, nil)
	if err != nil {
		t.Fatalf("handleNsGet: %v", err)
	}

	if result.Name != "test-ns" {
		t.Errorf("Name = %q", result.Name)
	}
	if result.Status != "Active" {
		t.Errorf("Status = %q, want Active", result.Status)
	}
	if !result.Managed {
		t.Error("expected Managed=true for tentacular-managed namespace")
	}
	if result.Labels[k8s.ManagedByLabel] != k8s.ManagedByValue {
		t.Errorf("missing managed-by label")
	}
	if result.Annotations["tentacular.dev/owner"] != "platform-team" {
		t.Errorf("missing owner annotation")
	}
}

func TestHandleNsGet_NotFound(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	_, err := handleNsGet(ctx, client, bearerEval(), NsGetParams{Name: "nonexistent"}, nil)
	if err == nil {
		t.Error("expected error for nonexistent namespace")
	}
}

func TestHandleNsGet_NoLabelsOrAnnotations(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "bare-ns"},
	}
	_, _ = client.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})

	result, err := handleNsGet(ctx, client, bearerEval(), NsGetParams{Name: "bare-ns"}, nil)
	if err != nil {
		t.Fatalf("handleNsGet: %v", err)
	}

	// Labels and Annotations should be non-nil empty maps.
	if result.Labels == nil {
		t.Error("expected non-nil labels map")
	}
	if result.Annotations == nil {
		t.Error("expected non-nil annotations map")
	}
	if result.Managed {
		t.Error("expected Managed=false for unmanaged namespace")
	}
}
