//go:build integration

package integration_test

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

func TestIntegration_HealthNodes(t *testing.T) {
	client := integrationClient(t)
	ctx := context.Background()

	nodes, err := client.Clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list nodes: %v", err)
	}
	if len(nodes.Items) == 0 {
		t.Fatal("expected at least 1 node")
	}

	for _, node := range nodes.Items {
		ready := false
		for _, cond := range node.Status.Conditions {
			if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
				ready = true
				break
			}
		}
		if !ready {
			t.Errorf("node %s is not Ready", node.Name)
		}

		cpu := node.Status.Capacity[corev1.ResourceCPU]
		mem := node.Status.Capacity[corev1.ResourceMemory]
		if cpu.IsZero() {
			t.Errorf("node %s has zero CPU capacity", node.Name)
		}
		if mem.IsZero() {
			t.Errorf("node %s has zero memory capacity", node.Name)
		}
	}
}

func TestIntegration_HealthNsUsageNoQuota(t *testing.T) {
	client := integrationClient(t)
	ctx := context.Background()
	nsName := "tnt-int-health-noq"

	t.Cleanup(func() {
		_ = k8s.DeleteNamespace(context.Background(), client, nsName)
	})

	if err := k8s.CreateNamespace(ctx, client, nsName); err != nil {
		t.Fatalf("CreateNamespace: %v", err)
	}

	quotas, err := client.Clientset.CoreV1().ResourceQuotas(nsName).List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list quotas: %v", err)
	}
	if len(quotas.Items) != 0 {
		t.Errorf("expected 0 quotas, got %d", len(quotas.Items))
	}
}

func TestIntegration_HealthNsUsageWithQuota(t *testing.T) {
	client := integrationClient(t)
	ctx := context.Background()
	nsName := "tnt-int-health-quota"

	t.Cleanup(func() {
		_ = k8s.DeleteNamespace(context.Background(), client, nsName)
	})

	if err := k8s.CreateNamespace(ctx, client, nsName); err != nil {
		t.Fatalf("CreateNamespace: %v", err)
	}
	if err := k8s.CreateResourceQuota(ctx, client, nsName, "medium"); err != nil {
		t.Fatalf("CreateResourceQuota: %v", err)
	}

	quotas, err := client.Clientset.CoreV1().ResourceQuotas(nsName).List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list quotas: %v", err)
	}
	if len(quotas.Items) != 1 {
		t.Fatalf("expected 1 quota, got %d", len(quotas.Items))
	}

	q := quotas.Items[0]
	if cpu := q.Spec.Hard[corev1.ResourceRequestsCPU]; cpu.String() != "16" {
		t.Errorf("expected CPU request 16, got %s", cpu.String())
	}
	if mem := q.Spec.Hard[corev1.ResourceRequestsMemory]; mem.String() != "16Gi" {
		t.Errorf("expected memory request 16Gi, got %s", mem.String())
	}
	if pods := q.Spec.Hard[corev1.ResourcePods]; pods.String() != "50" {
		t.Errorf("expected pod limit 50, got %s", pods.String())
	}
}

func TestIntegration_ClusterSummary(t *testing.T) {
	client := integrationClient(t)
	ctx := context.Background()

	nodes, err := client.Clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list nodes: %v", err)
	}
	if len(nodes.Items) < 1 {
		t.Fatal("expected at least 1 node")
	}

	// Count running pods across all namespaces.
	pods, err := client.Clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		FieldSelector: "status.phase=Running",
	})
	if err != nil {
		t.Fatalf("list running pods: %v", err)
	}
	if len(pods.Items) == 0 {
		t.Error("expected at least 1 running pod (kube-system components)")
	}

	t.Logf("cluster summary: %d nodes, %d running pods", len(nodes.Items), len(pods.Items))
}
