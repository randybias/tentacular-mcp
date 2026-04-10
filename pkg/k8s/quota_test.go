package k8s_test

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

func TestCreateResourceQuota_SmallPreset(t *testing.T) {
	cs, client := newFakeK8sClient()
	ctx := context.Background()

	if err := k8s.CreateResourceQuota(ctx, client, "test-ns", "small"); err != nil {
		t.Fatalf("CreateResourceQuota: %v", err)
	}

	quota, err := cs.CoreV1().ResourceQuotas("test-ns").Get(ctx, "tentacular-quota", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get ResourceQuota: %v", err)
	}

	cpuReq := quota.Spec.Hard[corev1.ResourceRequestsCPU]
	if cpuReq.String() != "4" {
		t.Errorf("small CPU request quota: expected '4', got %q", cpuReq.String())
	}
	memReq := quota.Spec.Hard[corev1.ResourceRequestsMemory]
	if memReq.String() != "4Gi" {
		t.Errorf("small memory request quota: expected '4Gi', got %q", memReq.String())
	}
	pods := quota.Spec.Hard[corev1.ResourcePods]
	if pods.Value() != 20 {
		t.Errorf("small pod limit: expected 20, got %d", pods.Value())
	}
}

func TestCreateResourceQuota_MediumPreset(t *testing.T) {
	cs, client := newFakeK8sClient()
	ctx := context.Background()

	_ = k8s.CreateResourceQuota(ctx, client, "test-ns", "medium")
	quota, err := cs.CoreV1().ResourceQuotas("test-ns").Get(ctx, "tentacular-quota", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get ResourceQuota: %v", err)
	}

	cpuReq := quota.Spec.Hard[corev1.ResourceRequestsCPU]
	if cpuReq.String() != "16" {
		t.Errorf("medium CPU request quota: expected '16', got %q", cpuReq.String())
	}
	memReq := quota.Spec.Hard[corev1.ResourceRequestsMemory]
	if memReq.String() != "16Gi" {
		t.Errorf("medium memory request quota: expected '16Gi', got %q", memReq.String())
	}
}

func TestCreateResourceQuota_LargePreset(t *testing.T) {
	cs, client := newFakeK8sClient()
	ctx := context.Background()

	_ = k8s.CreateResourceQuota(ctx, client, "test-ns", "large")
	quota, err := cs.CoreV1().ResourceQuotas("test-ns").Get(ctx, "tentacular-quota", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get ResourceQuota: %v", err)
	}

	cpuReq := quota.Spec.Hard[corev1.ResourceRequestsCPU]
	if cpuReq.String() != "32" {
		t.Errorf("large CPU request quota: expected '32', got %q", cpuReq.String())
	}
	pods := quota.Spec.Hard[corev1.ResourcePods]
	if pods.Value() != 100 {
		t.Errorf("large pod limit: expected 100, got %d", pods.Value())
	}
}

func TestCreateResourceQuota_UnknownPreset(t *testing.T) {
	_, client := newFakeK8sClient()
	err := k8s.CreateResourceQuota(context.Background(), client, "test-ns", "xlarge")
	if err == nil {
		t.Error("expected error for unknown preset, got nil")
	}
}

func TestCreateResourceQuota_AlreadyExists(t *testing.T) {
	_, client := newFakeK8sClient()
	ctx := context.Background()

	_ = k8s.CreateResourceQuota(ctx, client, "test-ns", "small")
	err := k8s.CreateResourceQuota(ctx, client, "test-ns", "small")
	if err == nil {
		t.Error("expected error for duplicate ResourceQuota, got nil")
	}
}

func TestCreateLimitRange_DefaultsSet(t *testing.T) {
	cs, client := newFakeK8sClient()
	ctx := context.Background()

	if err := k8s.CreateLimitRange(ctx, client, "test-ns"); err != nil {
		t.Fatalf("CreateLimitRange: %v", err)
	}

	lr, err := cs.CoreV1().LimitRanges("test-ns").Get(ctx, "tentacular-limits", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get LimitRange: %v", err)
	}

	if len(lr.Spec.Limits) == 0 {
		t.Fatal("expected at least one LimitRange item")
	}

	item := lr.Spec.Limits[0]
	if item.Type != corev1.LimitTypeContainer {
		t.Errorf("expected LimitTypeContainer, got %v", item.Type)
	}

	cpuReq := item.DefaultRequest[corev1.ResourceCPU]
	if cpuReq.String() != "50m" {
		t.Errorf("default CPU request: expected '50m', got %q", cpuReq.String())
	}
	memReq := item.DefaultRequest[corev1.ResourceMemory]
	if memReq.String() != "64Mi" {
		t.Errorf("default memory request: expected '64Mi', got %q", memReq.String())
	}
	cpuLim := item.Default[corev1.ResourceCPU]
	if cpuLim.String() != "500m" {
		t.Errorf("default CPU limit: expected '500m', got %q", cpuLim.String())
	}
	memLim := item.Default[corev1.ResourceMemory]
	if memLim.String() != "256Mi" {
		t.Errorf("default memory limit: expected '256Mi', got %q", memLim.String())
	}
}

func TestCreateLimitRange_AlreadyExists(t *testing.T) {
	_, client := newFakeK8sClient()
	ctx := context.Background()

	_ = k8s.CreateLimitRange(ctx, client, "test-ns")
	err := k8s.CreateLimitRange(ctx, client, "test-ns")
	if err == nil {
		t.Error("expected error for duplicate LimitRange, got nil")
	}
}
