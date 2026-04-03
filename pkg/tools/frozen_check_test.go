// Tests for the frozen enclave check in wf_apply, wf_run, and wf_restart handlers.
//
// Covers:
//   - fetchNamespaceAnnotations + IsEnclave + ReadEnclaveInfo: frozen status detected
//   - frozen status returns an error containing "frozen"
//   - active status passes through (not blocked)
//   - non-enclave namespace passes through (not blocked)

package tools

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/randybias/tentacular-mcp/pkg/authz"
	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

// frozenEnclaveNs creates a managed enclave namespace with "frozen" status.
func frozenEnclaveNs(t *testing.T, client *k8s.Client, name string) {
	t.Helper()
	ctx := context.Background()
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				k8s.ManagedByLabel: k8s.ManagedByValue,
			},
			Annotations: map[string]string{
				authz.AnnotationEnclave:       name,
				authz.AnnotationEnclaveOwner:  "owner@example.com",
				authz.AnnotationEnclaveStatus: "frozen",
			},
		},
	}
	_, err := client.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("setup frozenEnclaveNs %q: %v", name, err)
	}
}

// activeEnclaveNs creates a managed enclave namespace with "active" status.
func activeEnclaveNs(t *testing.T, client *k8s.Client, name string) {
	t.Helper()
	ctx := context.Background()
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				k8s.ManagedByLabel: k8s.ManagedByValue,
			},
			Annotations: map[string]string{
				authz.AnnotationEnclave:       name,
				authz.AnnotationEnclaveOwner:  "owner@example.com",
				authz.AnnotationEnclaveStatus: "active",
			},
		},
	}
	_, err := client.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("setup activeEnclaveNs %q: %v", name, err)
	}
}

// checkFrozenEnclave is the inline frozen-check logic tested in isolation.
// It mirrors the pattern used in wf_apply, wf_run, and wf_restart.
func checkFrozenEnclaveForTest(ctx context.Context, client *k8s.Client, namespace string) error {
	nsAnn, err := fetchNamespaceAnnotations(ctx, client, namespace)
	if err != nil {
		return err
	}
	if authz.IsEnclave(nsAnn) {
		if info := authz.ReadEnclaveInfo(nsAnn); info.Status == "frozen" {
			return &frozenEnclaveError{enclave: info.Enclave}
		}
	}
	return nil
}

type frozenEnclaveError struct {
	enclave string
}

func (e *frozenEnclaveError) Error() string {
	return "enclave \"" + e.enclave + "\" is frozen: new deployments and updates are blocked"
}

// --- Tests ---

// TestFrozenCheck_FrozenEnclave_ReturnsError verifies that a frozen enclave status
// is detected and returns an error containing "frozen".
func TestFrozenCheck_FrozenEnclave_ReturnsError(t *testing.T) {
	client := newNsTestClient()
	ctx := context.Background()
	frozenEnclaveNs(t, client, "frozen-enc")

	err := checkFrozenEnclaveForTest(ctx, client, "frozen-enc")
	if err == nil {
		t.Fatal("expected error for frozen enclave, got nil")
	}
	if !strings.Contains(err.Error(), "frozen") {
		t.Errorf("error should mention frozen: %v", err)
	}
	if !strings.Contains(err.Error(), "frozen-enc") {
		t.Errorf("error should mention enclave name: %v", err)
	}
}

// TestFrozenCheck_ActiveEnclave_NoError verifies that an active enclave does not block.
func TestFrozenCheck_ActiveEnclave_NoError(t *testing.T) {
	client := newNsTestClient()
	ctx := context.Background()
	activeEnclaveNs(t, client, "active-enc")

	err := checkFrozenEnclaveForTest(ctx, client, "active-enc")
	if err != nil {
		t.Errorf("active enclave should not be blocked: %v", err)
	}
}

// TestFrozenCheck_NonEnclave_NoError verifies that a non-enclave managed namespace is not blocked.
func TestFrozenCheck_NonEnclave_NoError(t *testing.T) {
	client := newNsTestClient()
	ctx := context.Background()

	// Plain managed namespace with no enclave annotations.
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "plain-ns",
			Labels: map[string]string{
				k8s.ManagedByLabel: k8s.ManagedByValue,
			},
		},
	}
	_, err := client.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	err = checkFrozenEnclaveForTest(ctx, client, "plain-ns")
	if err != nil {
		t.Errorf("non-enclave namespace should not be blocked: %v", err)
	}
}

// TestFrozenCheck_MissingNamespace_NoError verifies that a missing namespace returns no error.
// (fetchNamespaceAnnotations returns an empty map for missing namespaces.)
func TestFrozenCheck_MissingNamespace_NoError(t *testing.T) {
	client := newNsTestClient()
	ctx := context.Background()

	err := checkFrozenEnclaveForTest(ctx, client, "does-not-exist")
	if err != nil {
		t.Errorf("missing namespace should not cause frozen error: %v", err)
	}
}
