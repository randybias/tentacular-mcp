// Tests for ns_permissions_get and ns_permissions_set handlers.

package tools

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/randybias/tentacular-mcp/pkg/authz"
	"github.com/randybias/tentacular-mcp/pkg/exoskeleton"
	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

// nsPermTestClient creates a fake k8s client for permissions tests.
func nsPermTestClient() *k8s.Client {
	return newNsTestClient()
}

// seedNamespaceForPerms creates a managed namespace with ownership annotations for permissions tests.
func seedNamespaceForPerms(t *testing.T, client *k8s.Client, name, ownerSub, ownerEmail, group, mode string) {
	t.Helper()
	ctx := context.Background()
	ann := map[string]string{
		authz.AnnotationOwnerSub:   ownerSub,
		authz.AnnotationOwnerEmail: ownerEmail,
		authz.AnnotationGroup:      group,
		authz.AnnotationMode:       mode,
	}
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				k8s.ManagedByLabel: k8s.ManagedByValue,
			},
			Annotations: ann,
		},
	}
	_, err := client.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("setup seedNamespaceForPerms %q: %v", name, err)
	}
}

// --- ns_permissions_get tests ---

func TestNsPermissionsGet_Basic(t *testing.T) {
	client := nsPermTestClient()
	ctx := context.Background()

	seedNamespaceForPerms(t, client, "get-ns", "sub-alice", "alice@example.com", "dev-team", "rwxr-x---")

	result, err := handleNsPermissionsGet(ctx, client, NsPermissionsGetParams{Namespace: "get-ns"}, bearerInfo(), nsEval())
	if err != nil {
		t.Fatalf("handleNsPermissionsGet: %v", err)
	}

	if result.Namespace != "get-ns" {
		t.Errorf("Namespace = %q, want %q", result.Namespace, "get-ns")
	}
	if result.OwnerSub != "sub-alice" {
		t.Errorf("OwnerSub = %q, want sub-alice", result.OwnerSub)
	}
	if result.OwnerEmail != "alice@example.com" {
		t.Errorf("OwnerEmail = %q, want alice@example.com", result.OwnerEmail)
	}
	if result.Group != "dev-team" {
		t.Errorf("Group = %q, want dev-team", result.Group)
	}
	if result.Mode != "rwxr-x---" {
		t.Errorf("Mode = %q, want rwxr-x---", result.Mode)
	}
	if result.Preset != "group-read" {
		t.Errorf("Preset = %q, want group-read", result.Preset)
	}
}

func TestNsPermissionsGet_NotFound(t *testing.T) {
	client := nsPermTestClient()
	ctx := context.Background()

	_, err := handleNsPermissionsGet(ctx, client, NsPermissionsGetParams{Namespace: "nonexistent"}, bearerInfo(), nsEval())
	if err == nil {
		t.Error("expected error for nonexistent namespace")
	}
}

func TestNsPermissionsGet_AuthzDenied_Stranger(t *testing.T) {
	client := nsPermTestClient()
	ctx := context.Background()

	seedNamespaceForPerms(t, client, "perm-private-ns", "sub-owner", "owner@example.com", "", "rwx------")

	stranger := &exoskeleton.DeployerInfo{
		Subject:  "sub-stranger",
		Email:    "s@example.com",
		Provider: "keycloak",
	}

	_, err := handleNsPermissionsGet(ctx, client, NsPermissionsGetParams{Namespace: "perm-private-ns"}, stranger, nsEval())
	if err == nil {
		t.Error("expected permission denied for stranger on private namespace")
	}
}

func TestNsPermissionsGet_AuthzAllowed_Owner(t *testing.T) {
	client := nsPermTestClient()
	ctx := context.Background()

	seedNamespaceForPerms(t, client, "perm-owner-ns", "sub-owner", "owner@example.com", "", "rwx------")

	owner := &exoskeleton.DeployerInfo{
		Subject:  "sub-owner",
		Email:    "owner@example.com",
		Provider: "keycloak",
	}

	result, err := handleNsPermissionsGet(ctx, client, NsPermissionsGetParams{Namespace: "perm-owner-ns"}, owner, nsEval())
	if err != nil {
		t.Errorf("owner should be allowed to get ns permissions: %v", err)
	}
	if result.OwnerSub != "sub-owner" {
		t.Errorf("OwnerSub = %q, want sub-owner", result.OwnerSub)
	}
}

func TestNsPermissionsGet_WithDefaults(t *testing.T) {
	client := nsPermTestClient()
	ctx := context.Background()

	// Namespace with default-group and default-mode annotations.
	ctx2 := context.Background()
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "defaults-ns",
			Labels: map[string]string{
				k8s.ManagedByLabel: k8s.ManagedByValue,
			},
			Annotations: map[string]string{
				authz.AnnotationOwnerSub:     "sub-x",
				authz.AnnotationMode:         "rwx------",
				authz.AnnotationDefaultGroup: "ci-team",
				authz.AnnotationDefaultMode:  "rwxr-x---",
			},
		},
	}
	_, err := client.Clientset.CoreV1().Namespaces().Create(ctx2, ns, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	result, err := handleNsPermissionsGet(ctx, client, NsPermissionsGetParams{Namespace: "defaults-ns"}, bearerInfo(), nsEval())
	if err != nil {
		t.Fatalf("handleNsPermissionsGet: %v", err)
	}

	if result.DefaultGroup != "ci-team" {
		t.Errorf("DefaultGroup = %q, want ci-team", result.DefaultGroup)
	}
	if result.DefaultMode != "rwxr-x---" {
		t.Errorf("DefaultMode = %q, want rwxr-x---", result.DefaultMode)
	}
	if result.DefaultPreset != "group-read" {
		t.Errorf("DefaultPreset = %q, want group-read", result.DefaultPreset)
	}
}

func TestNsPermissionsGet_BearerToken_Bypasses(t *testing.T) {
	client := nsPermTestClient()
	ctx := context.Background()

	seedNamespaceForPerms(t, client, "bearer-perm-ns", "sub-owner", "owner@example.com", "", "rwx------")

	_, err := handleNsPermissionsGet(ctx, client, NsPermissionsGetParams{Namespace: "bearer-perm-ns"}, bearerInfo(), nsEval())
	if err != nil {
		t.Errorf("bearer token should bypass authz for ns_permissions_get: %v", err)
	}
}

// --- ns_permissions_set tests ---

func TestNsPermissionsSet_OwnerCanSetGroup(t *testing.T) {
	client := nsPermTestClient()
	ctx := context.Background()

	seedNamespaceForPerms(t, client, "set-group-ns", "sub-owner", "owner@example.com", "", "rwxr-x---")

	owner := &exoskeleton.DeployerInfo{
		Subject:  "sub-owner",
		Email:    "owner@example.com",
		Provider: "keycloak",
	}

	result, err := handleNsPermissionsSet(ctx, client, NsPermissionsSetParams{
		Namespace: "set-group-ns",
		Group:     "new-team",
	}, owner, nsEval())
	if err != nil {
		t.Fatalf("handleNsPermissionsSet: %v", err)
	}
	if result.Group != "new-team" {
		t.Errorf("Group = %q, want new-team", result.Group)
	}
}

func TestNsPermissionsSet_OwnerCanSetModePreset(t *testing.T) {
	client := nsPermTestClient()
	ctx := context.Background()

	seedNamespaceForPerms(t, client, "set-mode-ns", "sub-owner", "owner@example.com", "", "rwx------")

	owner := &exoskeleton.DeployerInfo{
		Subject:  "sub-owner",
		Email:    "owner@example.com",
		Provider: "keycloak",
	}

	result, err := handleNsPermissionsSet(ctx, client, NsPermissionsSetParams{
		Namespace: "set-mode-ns",
		Share:     "group-read",
	}, owner, nsEval())
	if err != nil {
		t.Fatalf("handleNsPermissionsSet: %v", err)
	}
	if result.Mode != "rwxr-x---" {
		t.Errorf("Mode = %q, want rwxr-x---", result.Mode)
	}
	if result.Preset != "group-read" {
		t.Errorf("Preset = %q, want group-read", result.Preset)
	}
}

func TestNsPermissionsSet_NonOwner_Denied(t *testing.T) {
	client := nsPermTestClient()
	ctx := context.Background()

	seedNamespaceForPerms(t, client, "owned-ns", "sub-owner", "owner@example.com", "", "rwxr-x---")

	nonOwner := &exoskeleton.DeployerInfo{
		Subject:  "sub-other",
		Email:    "other@example.com",
		Provider: "keycloak",
	}

	_, err := handleNsPermissionsSet(ctx, client, NsPermissionsSetParams{
		Namespace: "owned-ns",
		Share:     "private",
	}, nonOwner, nsEval())
	if err == nil {
		t.Error("expected permission denied for non-owner changing namespace permissions")
	}
}

func TestNsPermissionsSet_BearerToken_Bypasses(t *testing.T) {
	client := nsPermTestClient()
	ctx := context.Background()

	seedNamespaceForPerms(t, client, "bearer-set-ns", "sub-owner", "owner@example.com", "", "rwxr-x---")

	result, err := handleNsPermissionsSet(ctx, client, NsPermissionsSetParams{
		Namespace: "bearer-set-ns",
		Share:     "private",
	}, bearerInfo(), nsEval())
	if err != nil {
		t.Errorf("bearer token should bypass owner check for ns_permissions_set: %v", err)
	}
	if result.Mode != "rwx------" {
		t.Errorf("Mode = %q, want rwx------", result.Mode)
	}
}

func TestNsPermissionsSet_PreAuthzNs_AnyCallerCanSet(t *testing.T) {
	// A pre-authz namespace (no owner-sub) can be updated by any caller.
	client := nsPermTestClient()
	ctx := context.Background()

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pre-authz-set-ns",
			Labels: map[string]string{
				k8s.ManagedByLabel: k8s.ManagedByValue,
			},
		},
	}
	_, _ = client.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})

	stranger := &exoskeleton.DeployerInfo{
		Subject:  "sub-stranger",
		Email:    "s@example.com",
		Provider: "keycloak",
	}

	result, err := handleNsPermissionsSet(ctx, client, NsPermissionsSetParams{
		Namespace: "pre-authz-set-ns",
		Share:     "group-read",
	}, stranger, nsEval())
	if err != nil {
		t.Errorf("pre-authz ns allows any caller to set permissions: %v", err)
	}
	if result.Mode != "rwxr-x---" {
		t.Errorf("Mode = %q, want rwxr-x---", result.Mode)
	}
}

func TestNsPermissionsSet_InvalidPreset_Rejected(t *testing.T) {
	client := nsPermTestClient()
	ctx := context.Background()

	seedNamespaceForPerms(t, client, "invalid-preset-ns", "sub-owner", "owner@example.com", "", "rwx------")

	owner := &exoskeleton.DeployerInfo{
		Subject:  "sub-owner",
		Email:    "owner@example.com",
		Provider: "keycloak",
	}

	_, err := handleNsPermissionsSet(ctx, client, NsPermissionsSetParams{
		Namespace: "invalid-preset-ns",
		Share:     "not-a-valid-preset",
	}, owner, nsEval())
	if err == nil {
		t.Error("expected error for invalid preset")
	}
}

func TestNsPermissionsSet_InvalidMode_Rejected(t *testing.T) {
	client := nsPermTestClient()
	ctx := context.Background()

	seedNamespaceForPerms(t, client, "invalid-mode-ns", "sub-owner", "owner@example.com", "", "rwx------")

	owner := &exoskeleton.DeployerInfo{
		Subject:  "sub-owner",
		Email:    "owner@example.com",
		Provider: "keycloak",
	}

	_, err := handleNsPermissionsSet(ctx, client, NsPermissionsSetParams{
		Namespace: "invalid-mode-ns",
		Mode:      "not-a-mode",
	}, owner, nsEval())
	if err == nil {
		t.Error("expected error for invalid mode string")
	}
}

func TestNsPermissionsSet_SetsDefaultShare(t *testing.T) {
	client := nsPermTestClient()
	ctx := context.Background()

	seedNamespaceForPerms(t, client, "default-share-ns", "sub-owner", "owner@example.com", "", "rwx------")

	owner := &exoskeleton.DeployerInfo{
		Subject:  "sub-owner",
		Email:    "owner@example.com",
		Provider: "keycloak",
	}

	result, err := handleNsPermissionsSet(ctx, client, NsPermissionsSetParams{
		Namespace:    "default-share-ns",
		DefaultShare: "group-read",
	}, owner, nsEval())
	if err != nil {
		t.Fatalf("handleNsPermissionsSet with default_share: %v", err)
	}

	// DefaultMode should be set on the result.
	if result.DefaultMode != "rwxr-x---" {
		t.Errorf("DefaultMode = %q, want rwxr-x---", result.DefaultMode)
	}
}
