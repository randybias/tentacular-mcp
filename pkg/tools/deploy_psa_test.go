package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

// restrictedNs returns a namespace with PSA enforce=restricted labels and the
// tentacular managed-by label.
func restrictedNs(name string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				k8s.ManagedByLabel:                           k8s.ManagedByValue,
				"pod-security.kubernetes.io/enforce":         "restricted",
				"pod-security.kubernetes.io/enforce-version": "latest",
			},
		},
	}
}

// privilegedNs returns a namespace with no PSA enforce label (privileged default).
func privilegedNs(name string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				k8s.ManagedByLabel: k8s.ManagedByValue,
			},
		},
	}
}

// newPSATestClient returns a test client seeded with the given namespaces.
// Discovery is pre-populated so resolveGVR can find apps/v1 and core v1 kinds.
func newPSATestClient(namespaces ...*corev1.Namespace) *k8s.Client {
	scheme := deployScheme()
	dynClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, deployGVRs)
	objs := make([]runtime.Object, 0, len(namespaces))
	for _, ns := range namespaces {
		objs = append(objs, ns)
	}
	staticClient := kubefake.NewClientset(objs...)
	// Inject discovery resource lists so resolveGVR can resolve all used kinds.
	staticClient.Resources = []*metav1.APIResourceList{
		{
			GroupVersion: "apps/v1",
			APIResources: []metav1.APIResource{
				{Name: "deployments", Kind: "Deployment", Namespaced: true},
			},
		},
		{
			GroupVersion: "v1",
			APIResources: []metav1.APIResource{
				{Name: "configmaps", Kind: "ConfigMap", Namespaced: true},
				{Name: "services", Kind: "Service", Namespaced: true},
				{Name: "secrets", Kind: "Secret", Namespaced: true},
			},
		},
	}
	return &k8s.Client{
		Clientset: staticClient,
		Dynamic:   dynClient,
		Config:    &rest.Config{Host: "https://test-cluster:6443"},
	}
}

// hardenedDeployManifest returns a wf_apply-style manifest map for a Deployment
// that fully satisfies restricted PSA after ensurePSACompliance has run.
func hardenedDeployManifest(name string) map[string]any {
	return map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata":   map[string]any{"name": name},
		"spec": map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"securityContext": map[string]any{
						"runAsNonRoot":   true,
						"seccompProfile": map[string]any{"type": "RuntimeDefault"},
					},
					"containers": []any{
						map[string]any{
							"name":  "app",
							"image": "myimg:v1",
							"securityContext": map[string]any{
								"allowPrivilegeEscalation": false,
								"runAsNonRoot":             true,
								"capabilities":             map[string]any{"drop": []any{"ALL"}},
								"seccompProfile":           map[string]any{"type": "RuntimeDefault"},
							},
						},
					},
				},
			},
		},
	}
}

// bareDeployManifest returns a wf_apply-style Deployment with no security context.
// After ensurePSACompliance it will have runAsNonRoot, seccomp, etc. injected —
// but NOT allowPrivilegeEscalation or capabilities.drop which require explicit values
// that ensurePSACompliance injects. This is used to test that the fully-injected
// spec passes PSA validation (since ensurePSACompliance runs before ValidatePSA).
func bareDeployManifest(name string) map[string]any {
	return map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata":   map[string]any{"name": name},
		"spec": map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"containers": []any{
						map[string]any{
							"name":  "app",
							"image": "myimg:v1",
						},
					},
				},
			},
		},
	}
}

// incompleteDeployManifest returns a Deployment with partial security context —
// runAsNonRoot set but missing allowPrivilegeEscalation and capabilities.drop.
// Even after ensurePSACompliance (which only sets-if-absent) this spec needs
// the user-absent fields filled in. ensurePSACompliance will fill them, so to
// test a truly failing spec we need one where ensurePSACompliance can't fix it.
//
// We craft a spec where allowPrivilegeEscalation=true (explicitly) which
// ensurePSACompliance will NOT overwrite (setIfAbsent leaves it).
func nonCompliantDeployManifest(name string) map[string]any {
	return map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata":   map[string]any{"name": name},
		"spec": map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"securityContext": map[string]any{
						"runAsNonRoot":   true,
						"seccompProfile": map[string]any{"type": "RuntimeDefault"},
					},
					"containers": []any{
						map[string]any{
							"name":  "app",
							"image": "myimg:v1",
							"securityContext": map[string]any{
								// allowPrivilegeEscalation explicitly set to true —
								// ensurePSACompliance won't overwrite this.
								"allowPrivilegeEscalation": true,
								"runAsNonRoot":             true,
								"capabilities":             map[string]any{"drop": []any{"ALL"}},
								"seccompProfile":           map[string]any{"type": "RuntimeDefault"},
							},
						},
					},
				},
			},
		},
	}
}

// TestHandleWorkflowApply_HardenedSpecPassesRestrictedNs verifies that a hardened
// Deployment spec is accepted by wf_apply when the namespace is PSA-restricted.
func TestHandleWorkflowApply_HardenedSpecPassesRestrictedNs(t *testing.T) {
	client := newPSATestClient(restrictedNs("secure-ns"))
	ctx := context.Background()

	result, err := handleWorkflowApply(ctx, client, WorkflowApplyParams{
		Namespace: "secure-ns",
		Name:      "my-hardened-app",
		Manifests: []map[string]any{hardenedDeployManifest("my-hardened-app")},
	})
	if err != nil {
		t.Fatalf("expected hardened spec to pass restricted PSA, got error: %v", err)
	}
	if result.Created != 1 {
		t.Errorf("expected 1 created resource, got %d", result.Created)
	}
}

// TestHandleWorkflowApply_BareSpecPassesAfterPSACompliance verifies that a bare
// Deployment (no security context) is accepted on a restricted namespace because
// ensurePSACompliance injects all required fields before ValidatePSA runs.
func TestHandleWorkflowApply_BareSpecPassesAfterPSACompliance(t *testing.T) {
	client := newPSATestClient(restrictedNs("secure-ns2"))
	ctx := context.Background()

	result, err := handleWorkflowApply(ctx, client, WorkflowApplyParams{
		Namespace: "secure-ns2",
		Name:      "bare-app",
		Manifests: []map[string]any{bareDeployManifest("bare-app")},
	})
	if err != nil {
		t.Fatalf("ensurePSACompliance should have injected required fields before validation: %v", err)
	}
	if result.Created != 1 {
		t.Errorf("expected 1 created, got %d", result.Created)
	}
}

// TestHandleWorkflowApply_NonCompliantSpecFailsRestrictedNs verifies that a spec
// with allowPrivilegeEscalation=true (which ensurePSACompliance won't fix) is
// rejected by wf_apply with a structured PSA error.
func TestHandleWorkflowApply_NonCompliantSpecFailsRestrictedNs(t *testing.T) {
	client := newPSATestClient(restrictedNs("secure-ns3"))
	ctx := context.Background()

	_, err := handleWorkflowApply(ctx, client, WorkflowApplyParams{
		Namespace: "secure-ns3",
		Name:      "bad-app",
		Manifests: []map[string]any{nonCompliantDeployManifest("bad-app")},
	})
	if err == nil {
		t.Fatal("expected PSA validation error for non-compliant spec, got nil")
	}
	if !strings.Contains(err.Error(), "PSA validation failed") {
		t.Errorf("error should indicate PSA validation failure, got: %v", err)
	}
	if !strings.Contains(err.Error(), "allowPrivilegeEscalation") {
		t.Errorf("error should mention allowPrivilegeEscalation, got: %v", err)
	}
}

// TestHandleWorkflowApply_NonCompliantSpecPassesPrivilegedNs verifies that a
// non-compliant spec (allowPrivilegeEscalation=true) is accepted in a privileged
// namespace where PSA validation is skipped.
func TestHandleWorkflowApply_NonCompliantSpecPassesPrivilegedNs(t *testing.T) {
	client := newPSATestClient(privilegedNs("priv-ns"))
	ctx := context.Background()

	result, err := handleWorkflowApply(ctx, client, WorkflowApplyParams{
		Namespace: "priv-ns",
		Name:      "any-app",
		Manifests: []map[string]any{nonCompliantDeployManifest("any-app")},
	})
	if err != nil {
		t.Fatalf("privileged namespace should skip PSA validation, got error: %v", err)
	}
	if result.Created != 1 {
		t.Errorf("expected 1 created, got %d", result.Created)
	}
}

// TestHandleWorkflowApply_PSAErrorStructured verifies that the PSA error returned
// by wf_apply on a restricted namespace is a *k8s.PSAValidationError with
// individual violation entries for each missing field.
func TestHandleWorkflowApply_PSAErrorStructured(t *testing.T) {
	client := newPSATestClient(restrictedNs("structured-err-ns"))
	ctx := context.Background()

	_, err := handleWorkflowApply(ctx, client, WorkflowApplyParams{
		Namespace: "structured-err-ns",
		Name:      "bad-app",
		Manifests: []map[string]any{nonCompliantDeployManifest("bad-app")},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var psaErr *k8s.PSAValidationError
	if !errors.As(err, &psaErr) {
		t.Fatalf("expected *k8s.PSAValidationError, got %T: %v", err, err)
	}
	if psaErr.Namespace != "structured-err-ns" {
		t.Errorf("PSAValidationError.Namespace: got %q, want %q", psaErr.Namespace, "structured-err-ns")
	}
	if psaErr.Level != "restricted" {
		t.Errorf("PSAValidationError.Level: got %q, want %q", psaErr.Level, "restricted")
	}
	if len(psaErr.Violations) == 0 {
		t.Error("expected at least one violation in PSAValidationError")
	}
}
