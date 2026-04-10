//go:build integration

package integration_test

import (
	"context"
	"testing"

	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

func TestIntegration_ClusterProfile(t *testing.T) {
	client := integrationClient(t)
	ctx := context.Background()

	profile, err := k8s.ProfileCluster(ctx, client, "")
	if err != nil {
		t.Fatalf("ProfileCluster: %v", err)
	}

	if profile.K8sVersion == "" {
		t.Error("expected non-empty K8sVersion")
	}
	t.Logf("K8sVersion: %s", profile.K8sVersion)

	if len(profile.Nodes) == 0 {
		t.Error("expected at least 1 node")
	}

	if profile.Distribution == "" {
		t.Error("expected non-empty Distribution")
	}
	t.Logf("Distribution: %s", profile.Distribution)

	if profile.CNI.Name == "" {
		t.Error("expected non-empty CNI name")
	}
	t.Logf("CNI: %s", profile.CNI.Name)
}

func TestIntegration_ClusterProfileWithNamespace(t *testing.T) {
	client := integrationClient(t)
	ctx := context.Background()
	nsName := "tnt-int-ops-profile"

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

	profile, err := k8s.ProfileCluster(ctx, client, nsName)
	if err != nil {
		t.Fatalf("ProfileCluster with namespace: %v", err)
	}

	if profile.Namespace != nsName {
		t.Errorf("expected namespace %q, got %q", nsName, profile.Namespace)
	}
	if profile.Quota == nil {
		t.Error("expected Quota to be populated")
	} else {
		if profile.Quota.CPURequest != "16" {
			t.Errorf("expected quota CPU request 16, got %s", profile.Quota.CPURequest)
		}
		if profile.Quota.MemoryRequest != "16Gi" {
			t.Errorf("expected quota memory request 16Gi, got %s", profile.Quota.MemoryRequest)
		}
		if profile.Quota.MaxPods != 50 {
			t.Errorf("expected quota pod limit 50, got %d", profile.Quota.MaxPods)
		}
	}
	if profile.LimitRange == nil {
		t.Error("expected LimitRange to be populated")
	} else {
		if profile.LimitRange.DefaultCPURequest != "50m" {
			t.Errorf("expected default CPU request 50m, got %s", profile.LimitRange.DefaultCPURequest)
		}
	}
	if profile.PodSecurity != "restricted" {
		t.Errorf("expected PodSecurity=restricted, got %q", profile.PodSecurity)
	}
}
