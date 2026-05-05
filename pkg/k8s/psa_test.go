package k8s_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

// hardenedDeployment returns a manifest that fully satisfies the restricted
// PSA policy at both pod and container level.
func hardenedDeployment(name string) map[string]any {
	return map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata":   map[string]any{"name": name},
		"spec": map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"securityContext": map[string]any{
						"runAsNonRoot": true,
						"seccompProfile": map[string]any{
							"type": "RuntimeDefault",
						},
					},
					"containers": []any{
						map[string]any{
							"name":  "app",
							"image": "myimg:v1",
							"securityContext": map[string]any{
								"allowPrivilegeEscalation": false,
								"runAsNonRoot":             true,
								"capabilities": map[string]any{
									"drop": []any{"ALL"},
								},
								"seccompProfile": map[string]any{
									"type": "RuntimeDefault",
								},
							},
						},
					},
				},
			},
		},
	}
}

// TestValidatePSA_PrivilegedLevelSkipsValidation verifies that privileged
// namespaces skip all validation regardless of spec content.
func TestValidatePSA_PrivilegedLevelSkipsValidation(t *testing.T) {
	// Missing all security fields — should still pass for privileged.
	bareDeployment := map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata":   map[string]any{"name": "bare"},
		"spec": map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"containers": []any{
						map[string]any{"name": "app", "image": "myimg:v1"},
					},
				},
			},
		},
	}
	for _, level := range []string{"", k8s.PSALevelPrivileged} {
		if err := k8s.ValidatePSA("ns", level, []map[string]any{bareDeployment}); err != nil {
			t.Errorf("level=%q: expected nil for privileged namespace, got: %v", level, err)
		}
	}
}

// TestValidatePSA_NonWorkloadKindsSkipped verifies that ConfigMap, Service, and
// other non-workload kinds are silently skipped during PSA validation.
func TestValidatePSA_NonWorkloadKindsSkipped(t *testing.T) {
	cm := map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata":   map[string]any{"name": "my-cm"},
		"data":       map[string]any{"key": "val"},
	}
	svc := map[string]any{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata":   map[string]any{"name": "my-svc"},
	}
	if err := k8s.ValidatePSA("ns", k8s.PSALevelRestricted, []map[string]any{cm, svc}); err != nil {
		t.Errorf("expected nil for non-workload kinds, got: %v", err)
	}
}

// TestValidatePSA_HardenedSpecPassesRestricted verifies that a fully-hardened
// Deployment passes restricted PSA validation.
func TestValidatePSA_HardenedSpecPassesRestricted(t *testing.T) {
	manifests := []map[string]any{hardenedDeployment("hardened-app")}
	if err := k8s.ValidatePSA("my-ns", k8s.PSALevelRestricted, manifests); err != nil {
		t.Errorf("expected nil for hardened spec, got: %v", err)
	}
}

// TestValidatePSA_MissingRunAsNonRoot fails when neither pod nor container has
// runAsNonRoot: true.
func TestValidatePSA_MissingRunAsNonRoot(t *testing.T) {
	m := map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata":   map[string]any{"name": "no-runasnonroot"},
		"spec": map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"securityContext": map[string]any{
						"seccompProfile": map[string]any{"type": "RuntimeDefault"},
					},
					"containers": []any{
						map[string]any{
							"name":  "app",
							"image": "myimg:v1",
							"securityContext": map[string]any{
								"allowPrivilegeEscalation": false,
								"capabilities":             map[string]any{"drop": []any{"ALL"}},
							},
						},
					},
				},
			},
		},
	}
	err := k8s.ValidatePSA("ns", k8s.PSALevelRestricted, []map[string]any{m})
	if err == nil {
		t.Fatal("expected error for missing runAsNonRoot, got nil")
	}
	if !strings.Contains(err.Error(), "runAsNonRoot") {
		t.Errorf("error should mention runAsNonRoot, got: %v", err)
	}
	var psaErr *k8s.PSAValidationError
	if !errors.As(err, &psaErr) {
		t.Fatalf("expected *PSAValidationError, got %T", err)
	}
	if !hasViolationField(psaErr, "securityContext.runAsNonRoot") {
		t.Errorf("violations should include securityContext.runAsNonRoot, got: %+v", psaErr.Violations)
	}
}

// TestValidatePSA_MissingSeccompProfile fails when neither pod nor container
// has a seccompProfile set.
func TestValidatePSA_MissingSeccompProfile(t *testing.T) {
	m := map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata":   map[string]any{"name": "no-seccomp"},
		"spec": map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"securityContext": map[string]any{
						"runAsNonRoot": true,
						// seccompProfile deliberately absent
					},
					"containers": []any{
						map[string]any{
							"name":  "app",
							"image": "myimg:v1",
							"securityContext": map[string]any{
								"allowPrivilegeEscalation": false,
								"runAsNonRoot":             true,
								"capabilities":             map[string]any{"drop": []any{"ALL"}},
								// seccompProfile deliberately absent
							},
						},
					},
				},
			},
		},
	}
	err := k8s.ValidatePSA("ns", k8s.PSALevelRestricted, []map[string]any{m})
	if err == nil {
		t.Fatal("expected error for missing seccompProfile, got nil")
	}
	var psaErr *k8s.PSAValidationError
	if !errors.As(err, &psaErr) {
		t.Fatalf("expected *PSAValidationError, got %T", err)
	}
	if !hasViolationField(psaErr, "securityContext.seccompProfile.type") {
		t.Errorf("violations should include seccompProfile.type, got: %+v", psaErr.Violations)
	}
}

// TestValidatePSA_MissingCapabilitiesDrop fails when capabilities.drop does
// not include ALL.
func TestValidatePSA_MissingCapabilitiesDrop(t *testing.T) {
	m := map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata":   map[string]any{"name": "no-caps-drop"},
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
								// capabilities.drop deliberately absent
							},
						},
					},
				},
			},
		},
	}
	err := k8s.ValidatePSA("ns", k8s.PSALevelRestricted, []map[string]any{m})
	if err == nil {
		t.Fatal("expected error for missing capabilities.drop, got nil")
	}
	var psaErr *k8s.PSAValidationError
	if !errors.As(err, &psaErr) {
		t.Fatalf("expected *PSAValidationError, got %T", err)
	}
	if !hasViolationField(psaErr, "securityContext.capabilities.drop") {
		t.Errorf("violations should include capabilities.drop, got: %+v", psaErr.Violations)
	}
}

// TestValidatePSA_MissingAllowPrivilegeEscalation fails when
// allowPrivilegeEscalation is not explicitly set to false.
func TestValidatePSA_MissingAllowPrivilegeEscalation(t *testing.T) {
	m := map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata":   map[string]any{"name": "no-ape"},
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
								// allowPrivilegeEscalation deliberately absent
								"runAsNonRoot": true,
								"capabilities": map[string]any{"drop": []any{"ALL"}},
							},
						},
					},
				},
			},
		},
	}
	err := k8s.ValidatePSA("ns", k8s.PSALevelRestricted, []map[string]any{m})
	if err == nil {
		t.Fatal("expected error for missing allowPrivilegeEscalation, got nil")
	}
	var psaErr *k8s.PSAValidationError
	if !errors.As(err, &psaErr) {
		t.Fatalf("expected *PSAValidationError, got %T", err)
	}
	if !hasViolationField(psaErr, "securityContext.allowPrivilegeEscalation") {
		t.Errorf("violations should include allowPrivilegeEscalation, got: %+v", psaErr.Violations)
	}
}

// TestValidatePSA_BaselineSpecFailsRestricted verifies that a spec that satisfies
// "baseline" PSA (i.e., no privileged containers, host namespaces) but is missing
// restricted-only fields (runAsNonRoot, seccomp, etc.) fails restricted validation.
func TestValidatePSA_BaselineSpecFailsRestricted(t *testing.T) {
	// Baseline allows this (no privileged flag, no host networking), but
	// restricted requires runAsNonRoot, seccompProfile, allowPrivilegeEscalation=false,
	// and capabilities.drop=ALL.
	baselineOnly := map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata":   map[string]any{"name": "baseline-spec"},
		"spec": map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"containers": []any{
						map[string]any{
							"name":  "app",
							"image": "myimg:v1",
							// No securityContext at all — baseline would allow this
							// but restricted requires all the fields below.
						},
					},
				},
			},
		},
	}
	err := k8s.ValidatePSA("ns", k8s.PSALevelRestricted, []map[string]any{baselineOnly})
	if err == nil {
		t.Fatal("expected error: baseline spec should fail restricted validation")
	}
	var psaErr *k8s.PSAValidationError
	if !errors.As(err, &psaErr) {
		t.Fatalf("expected *PSAValidationError, got %T", err)
	}
	// Should have multiple violations — all four restricted fields missing.
	if len(psaErr.Violations) < 3 {
		t.Errorf("expected at least 3 violations for bare spec, got %d: %v", len(psaErr.Violations), psaErr.Violations)
	}
}

// TestValidatePSA_ErrorListsEachFieldSeparately verifies that the structured
// error enumerates each missing field as a separate violation entry.
func TestValidatePSA_ErrorListsEachFieldSeparately(t *testing.T) {
	bare := map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata":   map[string]any{"name": "bare"},
		"spec": map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"containers": []any{
						map[string]any{"name": "app", "image": "myimg:v1"},
					},
				},
			},
		},
	}
	err := k8s.ValidatePSA("ns", k8s.PSALevelRestricted, []map[string]any{bare})
	if err == nil {
		t.Fatal("expected error")
	}
	var psaErr *k8s.PSAValidationError
	if !errors.As(err, &psaErr) {
		t.Fatalf("expected *PSAValidationError")
	}
	// Verify specific fields are reported separately.
	fields := make(map[string]bool, len(psaErr.Violations))
	for _, v := range psaErr.Violations {
		fields[v.Field] = true
	}
	wantFields := []string{
		"securityContext.runAsNonRoot",
		"securityContext.allowPrivilegeEscalation",
		"securityContext.capabilities.drop",
		"securityContext.seccompProfile.type",
	}
	for _, f := range wantFields {
		if !fields[f] {
			t.Errorf("expected violation for field %q, not found in: %v", f, psaErr.Violations)
		}
	}
}

// TestValidatePSA_JobManifest verifies that Job manifests are also validated.
func TestValidatePSA_JobManifest(t *testing.T) {
	job := map[string]any{
		"apiVersion": "batch/v1",
		"kind":       "Job",
		"metadata":   map[string]any{"name": "bare-job"},
		"spec": map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"containers": []any{
						map[string]any{"name": "worker", "image": "busybox:latest"},
					},
				},
			},
		},
	}
	err := k8s.ValidatePSA("ns", k8s.PSALevelRestricted, []map[string]any{job})
	if err == nil {
		t.Fatal("expected error for bare Job, got nil")
	}
	if !strings.Contains(err.Error(), "Job") {
		t.Errorf("error should mention Job kind, got: %v", err)
	}
}

// TestValidatePSA_SeccompAtPodLevelSatisfiesContainerRequirement verifies that
// a seccompProfile set at the pod level satisfies the requirement for all containers
// (no need to duplicate it on each container).
func TestValidatePSA_SeccompAtPodLevelSatisfiesContainerRequirement(t *testing.T) {
	m := map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata":   map[string]any{"name": "pod-level-seccomp"},
		"spec": map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"securityContext": map[string]any{
						"runAsNonRoot":   true,
						"seccompProfile": map[string]any{"type": "RuntimeDefault"}, // pod-level
					},
					"containers": []any{
						map[string]any{
							"name":  "app",
							"image": "myimg:v1",
							"securityContext": map[string]any{
								"allowPrivilegeEscalation": false,
								"runAsNonRoot":             true,
								"capabilities":             map[string]any{"drop": []any{"ALL"}},
								// seccompProfile absent at container level — pod level should satisfy
							},
						},
					},
				},
			},
		},
	}
	if err := k8s.ValidatePSA("ns", k8s.PSALevelRestricted, []map[string]any{m}); err != nil {
		t.Errorf("expected nil when seccompProfile is at pod level, got: %v", err)
	}
}

// TestValidatePSA_RunAsNonRootAtPodLevelSatisfiesContainerRequirement verifies
// that runAsNonRoot at pod level satisfies the requirement for all containers.
func TestValidatePSA_RunAsNonRootAtPodLevelSatisfiesContainerRequirement(t *testing.T) {
	m := map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata":   map[string]any{"name": "pod-level-nonroot"},
		"spec": map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"securityContext": map[string]any{
						"runAsNonRoot":   true, // pod-level
						"seccompProfile": map[string]any{"type": "RuntimeDefault"},
					},
					"containers": []any{
						map[string]any{
							"name":  "app",
							"image": "myimg:v1",
							"securityContext": map[string]any{
								"allowPrivilegeEscalation": false,
								// runAsNonRoot absent at container level
								"capabilities": map[string]any{"drop": []any{"ALL"}},
							},
						},
					},
				},
			},
		},
	}
	if err := k8s.ValidatePSA("ns", k8s.PSALevelRestricted, []map[string]any{m}); err != nil {
		t.Errorf("expected nil when runAsNonRoot is at pod level, got: %v", err)
	}
}

// TestPSAValidationError_Format verifies that PSAValidationError.Error() includes
// the namespace, level, and each violation.
func TestPSAValidationError_Format(t *testing.T) {
	err := &k8s.PSAValidationError{
		Namespace: "my-ns",
		Level:     "restricted",
		Violations: []k8s.PSAViolation{
			{ManifestKind: "Deployment", ManifestName: "my-app", Field: "securityContext.runAsNonRoot", Reason: "must be true"},
		},
	}
	msg := err.Error()
	for _, want := range []string{"my-ns", "restricted", "securityContext.runAsNonRoot"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message missing %q: %v", want, msg)
		}
	}
}

// hasViolationField is a helper that checks whether any violation in a
// PSAValidationError matches the given field path.
func hasViolationField(err *k8s.PSAValidationError, field string) bool {
	for _, v := range err.Violations {
		if v.Field == field {
			return true
		}
	}
	return false
}
