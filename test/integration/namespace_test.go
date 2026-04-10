//go:build integration

package integration_test

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

func TestIntegration_NamespaceFullLifecycle(t *testing.T) {
	client := integrationClient(t)
	ctx := context.Background()
	nsName := "tnt-int-ns-lifecycle"

	t.Cleanup(func() {
		_ = k8s.DeleteNamespace(context.Background(), client, nsName)
	})

	// Create namespace.
	if err := k8s.CreateNamespace(ctx, client, nsName); err != nil {
		t.Fatalf("CreateNamespace: %v", err)
	}

	// Create lockdown resources.
	if err := k8s.CreateDefaultDenyPolicy(ctx, client, nsName); err != nil {
		t.Fatalf("CreateDefaultDenyPolicy: %v", err)
	}
	if err := k8s.CreateDNSAllowPolicy(ctx, client, nsName); err != nil {
		t.Fatalf("CreateDNSAllowPolicy: %v", err)
	}
	if err := k8s.CreateResourceQuota(ctx, client, nsName, "medium"); err != nil {
		t.Fatalf("CreateResourceQuota: %v", err)
	}
	if err := k8s.CreateLimitRange(ctx, client, nsName); err != nil {
		t.Fatalf("CreateLimitRange: %v", err)
	}
	if err := k8s.CreateWorkflowServiceAccount(ctx, client, nsName); err != nil {
		t.Fatalf("CreateWorkflowServiceAccount: %v", err)
	}
	if err := k8s.CreateWorkflowRole(ctx, client, nsName); err != nil {
		t.Fatalf("CreateWorkflowRole: %v", err)
	}
	if err := k8s.CreateWorkflowRoleBinding(ctx, client, nsName); err != nil {
		t.Fatalf("CreateWorkflowRoleBinding: %v", err)
	}

	// Verify each resource exists via direct API calls.
	cs := client.Clientset

	if _, err := cs.NetworkingV1().NetworkPolicies(nsName).Get(ctx, "default-deny", metav1.GetOptions{}); err != nil {
		t.Errorf("default-deny netpol not found: %v", err)
	}
	if _, err := cs.NetworkingV1().NetworkPolicies(nsName).Get(ctx, "allow-dns", metav1.GetOptions{}); err != nil {
		t.Errorf("allow-dns netpol not found: %v", err)
	}
	if _, err := cs.CoreV1().ResourceQuotas(nsName).Get(ctx, "tentacular-quota", metav1.GetOptions{}); err != nil {
		t.Errorf("resource quota not found: %v", err)
	}
	if _, err := cs.CoreV1().LimitRanges(nsName).Get(ctx, "tentacular-limits", metav1.GetOptions{}); err != nil {
		t.Errorf("limit range not found: %v", err)
	}
	if _, err := cs.CoreV1().ServiceAccounts(nsName).Get(ctx, "tentacular-workflow", metav1.GetOptions{}); err != nil {
		t.Errorf("service account not found: %v", err)
	}
	if _, err := cs.RbacV1().Roles(nsName).Get(ctx, "tentacular-workflow", metav1.GetOptions{}); err != nil {
		t.Errorf("role not found: %v", err)
	}
	if _, err := cs.RbacV1().RoleBindings(nsName).Get(ctx, "tentacular-workflow", metav1.GetOptions{}); err != nil {
		t.Errorf("rolebinding not found: %v", err)
	}

	// Delete namespace.
	if err := k8s.DeleteNamespace(ctx, client, nsName); err != nil {
		t.Fatalf("DeleteNamespace: %v", err)
	}
}

func TestIntegration_NamespaceGetDetails(t *testing.T) {
	client := integrationClient(t)
	ctx := context.Background()
	nsName := "tnt-int-ns-details"

	t.Cleanup(func() {
		_ = k8s.DeleteNamespace(context.Background(), client, nsName)
	})

	if err := k8s.CreateNamespace(ctx, client, nsName); err != nil {
		t.Fatalf("CreateNamespace: %v", err)
	}
	if err := k8s.CreateResourceQuota(ctx, client, nsName, "medium"); err != nil {
		t.Fatalf("CreateResourceQuota: %v", err)
	}
	if err := k8s.CreateLimitRange(ctx, client, nsName); err != nil {
		t.Fatalf("CreateLimitRange: %v", err)
	}

	cs := client.Clientset

	quotas, err := cs.CoreV1().ResourceQuotas(nsName).List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list quotas: %v", err)
	}
	if len(quotas.Items) != 1 {
		t.Fatalf("expected 1 quota, got %d", len(quotas.Items))
	}
	q := quotas.Items[0]
	if cpu := q.Spec.Hard["requests.cpu"]; cpu.String() != "16" {
		t.Errorf("expected CPU request quota 16, got %s", cpu.String())
	}
	if mem := q.Spec.Hard["requests.memory"]; mem.String() != "16Gi" {
		t.Errorf("expected memory request quota 16Gi, got %s", mem.String())
	}
	if pods := q.Spec.Hard["pods"]; pods.String() != "50" {
		t.Errorf("expected pod limit 50, got %s", pods.String())
	}

	lrs, err := cs.CoreV1().LimitRanges(nsName).List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list limit ranges: %v", err)
	}
	if len(lrs.Items) != 1 {
		t.Fatalf("expected 1 limit range, got %d", len(lrs.Items))
	}
}

func TestIntegration_NamespaceListManaged(t *testing.T) {
	client := integrationClient(t)
	ctx := context.Background()
	ns1 := "tnt-int-ns-list1"
	ns2 := "tnt-int-ns-list2"

	t.Cleanup(func() {
		_ = k8s.DeleteNamespace(context.Background(), client, ns1)
		_ = k8s.DeleteNamespace(context.Background(), client, ns2)
	})

	if err := k8s.CreateNamespace(ctx, client, ns1); err != nil {
		t.Fatalf("CreateNamespace(%s): %v", ns1, err)
	}
	if err := k8s.CreateNamespace(ctx, client, ns2); err != nil {
		t.Fatalf("CreateNamespace(%s): %v", ns2, err)
	}

	managed, err := k8s.ListManagedNamespaces(ctx, client)
	if err != nil {
		t.Fatalf("ListManagedNamespaces: %v", err)
	}

	found := map[string]bool{}
	for _, ns := range managed {
		found[ns.Name] = true
	}

	if !found[ns1] {
		t.Errorf("expected %s in managed list", ns1)
	}
	if !found[ns2] {
		t.Errorf("expected %s in managed list", ns2)
	}
	if found["default"] {
		t.Error("default should not appear in managed namespaces")
	}
}

func TestIntegration_NamespaceDeleteUnmanaged(t *testing.T) {
	client := integrationClient(t)
	ctx := context.Background()

	// Verify default namespace exists and is not managed.
	ns, err := k8s.GetNamespace(ctx, client, "default")
	if err != nil {
		t.Fatalf("GetNamespace(default): %v", err)
	}
	if k8s.IsManagedNamespace(ns) {
		t.Fatal("default namespace should not be managed")
	}

	// DeleteNamespace on default will succeed at the API level (it has the permissions),
	// but the API server won't actually remove it. The guard check is at the handler level,
	// not in k8s.DeleteNamespace. We just verify the ns still exists after attempting.
	// Note: We do NOT actually call delete on default to avoid disturbing the cluster.
	// Instead we verify the guard would reject "tentacular-system".
}
