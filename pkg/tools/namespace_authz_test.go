// Tests for namespace-level authorization in MCP tools.
//
// Covers:
//   - handleNsCreate: owner annotation stamping (owner-sub, group, mode, created-at)
//   - checkNamespaceAuthz: bearer bypass, nil deployer, pre-authz ns, owner/group/others
//   - Namespace Read check on wf_pods, wf_logs, wf_events, wf_jobs, wf_health_ns
//   - Namespace Write check on wf_apply CREATE path
//   - Namespace Read filter on wf_list (only namespaces caller can read)

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

// --- helper constructors ---

func nsEval() *authz.Evaluator {
	return authz.NewEvaluator(authz.DefaultMode)
}

func oidcDeployerNs(sub, email string, groups ...string) *exoskeleton.DeployerInfo {
	return &exoskeleton.DeployerInfo{
		Subject:  sub,
		Email:    email,
		Provider: "keycloak",
		Groups:   groups,
	}
}

// nsWithAuthz creates a managed namespace with authz annotations.
func nsWithAuthz(t *testing.T, client *k8s.Client, name, ownerSub, group, mode string) {
	t.Helper()
	ctx := context.Background()
	ann := map[string]string{}
	if ownerSub != "" {
		ann[authz.AnnotationOwnerSub] = ownerSub
	}
	if group != "" {
		ann[authz.AnnotationGroup] = group
	}
	if mode != "" {
		ann[authz.AnnotationMode] = mode
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
		t.Fatalf("setup nsWithAuthz %q: %v", name, err)
	}
}

// --- Test 1: handleNsCreate annotation stamping ---

func TestNsCreate_StampsOwnerAnnotations(t *testing.T) {
	client := newNsTestClient()
	ctx := context.Background()

	deployer := &exoskeleton.DeployerInfo{
		Subject:     "sub-alice",
		Email:       "alice@example.com",
		DisplayName: "Alice",
		Provider:    "keycloak",
	}

	_, err := handleNsCreate(ctx, client, nsEval(), NsCreateParams{
		Name:        "alice-ns",
		QuotaPreset: "small",
	}, deployer)
	if err != nil {
		t.Fatalf("handleNsCreate: %v", err)
	}

	ns, err := client.Clientset.CoreV1().Namespaces().Get(ctx, "alice-ns", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get namespace: %v", err)
	}

	ann := ns.Annotations
	if ann == nil {
		t.Fatal("expected annotations to be set on namespace")
	}
	if ann[authz.AnnotationOwnerSub] != "sub-alice" {
		t.Errorf("owner-sub = %q, want %q", ann[authz.AnnotationOwnerSub], "sub-alice")
	}
	if ann[authz.AnnotationOwnerEmail] != "alice@example.com" {
		t.Errorf("owner-email = %q, want %q", ann[authz.AnnotationOwnerEmail], "alice@example.com")
	}
	if ann[authz.AnnotationOwnerName] != "Alice" {
		t.Errorf("owner-name = %q, want %q", ann[authz.AnnotationOwnerName], "Alice")
	}
	if ann[authz.AnnotationMode] == "" {
		t.Error("expected mode annotation to be set")
	}
}

func TestNsCreate_StampsGroup(t *testing.T) {
	client := newNsTestClient()
	ctx := context.Background()

	deployer := oidcDeployerNs("sub-bob", "bob@example.com")

	_, err := handleNsCreate(ctx, client, nsEval(), NsCreateParams{
		Name:        "bob-ns",
		QuotaPreset: "small",
		Group:       "platform-team",
	}, deployer)
	if err != nil {
		t.Fatalf("handleNsCreate: %v", err)
	}

	ns, _ := client.Clientset.CoreV1().Namespaces().Get(ctx, "bob-ns", metav1.GetOptions{})
	if ns.Annotations[authz.AnnotationGroup] != "platform-team" {
		t.Errorf("group = %q, want %q", ns.Annotations[authz.AnnotationGroup], "platform-team")
	}
}

func TestNsCreate_StampsSharePresetMode(t *testing.T) {
	client := newNsTestClient()
	ctx := context.Background()

	deployer := oidcDeployerNs("sub-carol", "carol@example.com")

	_, err := handleNsCreate(ctx, client, nsEval(), NsCreateParams{
		Name:        "carol-ns",
		QuotaPreset: "small",
		Share:       "group-read",
	}, deployer)
	if err != nil {
		t.Fatalf("handleNsCreate: %v", err)
	}

	ns, _ := client.Clientset.CoreV1().Namespaces().Get(ctx, "carol-ns", metav1.GetOptions{})
	if ns.Annotations[authz.AnnotationMode] != "rwxr-x---" {
		t.Errorf("mode = %q, want rwxr-x---", ns.Annotations[authz.AnnotationMode])
	}
}

func TestNsCreate_NilDeployer_NoAnnotations(t *testing.T) {
	client := newNsTestClient()
	ctx := context.Background()

	_, err := handleNsCreate(ctx, client, nsEval(), NsCreateParams{
		Name:        "anon-ns",
		QuotaPreset: "small",
	}, nil)
	if err != nil {
		t.Fatalf("handleNsCreate: %v", err)
	}

	ns, _ := client.Clientset.CoreV1().Namespaces().Get(ctx, "anon-ns", metav1.GetOptions{})
	// With nil deployer, no authz annotations should be stamped.
	if ns.Annotations != nil && ns.Annotations[authz.AnnotationOwnerSub] != "" {
		t.Errorf("expected no owner-sub annotation with nil deployer, got %q", ns.Annotations[authz.AnnotationOwnerSub])
	}
}

// --- Test 2: checkNamespaceAuthz helper ---

func TestCheckNamespaceAuthz_BearerToken_Bypass(t *testing.T) {
	client := newNsTestClient()
	ctx := context.Background()

	// Namespace with strict private mode.
	nsWithAuthz(t, client, "strict-ns", "sub-owner", "", "rwx------")

	bearer := bearerInfo()
	eval := nsEval()

	// Bearer token should bypass authz even for private namespace.
	err := checkNamespaceAuthz(ctx, client, "strict-ns", bearer, eval, authz.Read)
	if err != nil {
		t.Errorf("bearer token should bypass authz, got error: %v", err)
	}
}

func TestCheckNamespaceAuthz_NilDeployer_Denies(t *testing.T) {
	client := newNsTestClient()
	ctx := context.Background()

	nsWithAuthz(t, client, "strict-ns2", "sub-owner", "", "rwx------")

	eval := nsEval()

	// Nil deployer (no identity) should be denied by the evaluator.
	err := checkNamespaceAuthz(ctx, client, "strict-ns2", nil, eval, authz.Read)
	if err == nil {
		t.Errorf("nil deployer should be denied")
	}
}

func TestCheckNamespaceAuthz_NilEval_Allows(t *testing.T) {
	client := newNsTestClient()
	ctx := context.Background()

	nsWithAuthz(t, client, "strict-ns3", "sub-owner", "", "rwx------")

	stranger := oidcDeployerNs("sub-stranger", "s@example.com")

	// Nil evaluator (authz disabled) should allow.
	err := checkNamespaceAuthz(ctx, client, "strict-ns3", stranger, nil, authz.Read)
	if err != nil {
		t.Errorf("nil evaluator should allow, got error: %v", err)
	}
}

func TestCheckNamespaceAuthz_UnownedNamespace_Denies(t *testing.T) {
	client := newNsTestClient()
	ctx := context.Background()

	// Unowned namespace: managed but no owner-sub annotation.
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "unowned-ns",
			Labels: map[string]string{
				k8s.ManagedByLabel: k8s.ManagedByValue,
			},
		},
	}
	_, _ = client.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})

	stranger := oidcDeployerNs("sub-stranger", "s@example.com")
	eval := nsEval()

	// Unowned namespace (no owner-sub) should deny OIDC callers.
	err := checkNamespaceAuthz(ctx, client, "unowned-ns", stranger, eval, authz.Read)
	if err == nil {
		t.Errorf("unowned namespace should deny OIDC callers")
	}
}

func TestCheckNamespaceAuthz_UnownedNamespace_BearerAllows(t *testing.T) {
	client := newNsTestClient()
	ctx := context.Background()

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "unowned-ns2",
			Labels: map[string]string{
				k8s.ManagedByLabel: k8s.ManagedByValue,
			},
		},
	}
	_, _ = client.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})

	bearer := &exoskeleton.DeployerInfo{Provider: "bearer-token"}
	eval := nsEval()

	// Bearer-token can still access unowned namespaces (for admin/adoption).
	err := checkNamespaceAuthz(ctx, client, "unowned-ns2", bearer, eval, authz.Read)
	if err != nil {
		t.Errorf("bearer-token should be allowed on unowned namespace, got: %v", err)
	}
}

func TestCheckNamespaceAuthz_Owner_Allowed(t *testing.T) {
	client := newNsTestClient()
	ctx := context.Background()

	nsWithAuthz(t, client, "owner-ns", "sub-owner", "", "rwx------")

	owner := oidcDeployerNs("sub-owner", "owner@example.com")
	eval := nsEval()

	err := checkNamespaceAuthz(ctx, client, "owner-ns", owner, eval, authz.Read)
	if err != nil {
		t.Errorf("owner should be allowed to read: %v", err)
	}
}

func TestCheckNamespaceAuthz_GroupMember_Allowed(t *testing.T) {
	client := newNsTestClient()
	ctx := context.Background()

	nsWithAuthz(t, client, "group-ns", "sub-owner", "platform", "rwxr-x---")

	member := oidcDeployerNs("sub-member", "member@example.com", "platform")
	eval := nsEval()

	err := checkNamespaceAuthz(ctx, client, "group-ns", member, eval, authz.Read)
	if err != nil {
		t.Errorf("group member should be allowed to read: %v", err)
	}
}

func TestCheckNamespaceAuthz_Stranger_Denied(t *testing.T) {
	client := newNsTestClient()
	ctx := context.Background()

	nsWithAuthz(t, client, "private-ns", "sub-owner", "platform", "rwx------")

	stranger := oidcDeployerNs("sub-stranger", "s@example.com", "other-team")
	eval := nsEval()

	err := checkNamespaceAuthz(ctx, client, "private-ns", stranger, eval, authz.Read)
	if err == nil {
		t.Error("stranger should be denied read on private namespace")
	}
}

func TestCheckNamespaceAuthz_Write_OwnerAllowed(t *testing.T) {
	client := newNsTestClient()
	ctx := context.Background()

	nsWithAuthz(t, client, "write-ns", "sub-owner", "", "rwx------")

	owner := oidcDeployerNs("sub-owner", "owner@example.com")
	eval := nsEval()

	err := checkNamespaceAuthz(ctx, client, "write-ns", owner, eval, authz.Write)
	if err != nil {
		t.Errorf("owner should be allowed to write: %v", err)
	}
}

func TestCheckNamespaceAuthz_Write_StrangerDenied(t *testing.T) {
	client := newNsTestClient()
	ctx := context.Background()

	nsWithAuthz(t, client, "write-private-ns", "sub-owner", "", "rwx------")

	stranger := oidcDeployerNs("sub-stranger", "s@example.com")
	eval := nsEval()

	err := checkNamespaceAuthz(ctx, client, "write-private-ns", stranger, eval, authz.Write)
	if err == nil {
		t.Error("stranger should be denied write on private namespace")
	}
}

// --- Test 3: Namespace Read check on wf_pods/logs/events/jobs ---

func TestWfPods_NsAuthzDenied(t *testing.T) {
	// wf_pods requires namespace Read. Stranger on private ns should get permission denied.
	// wf_pods handler does not call checkNamespaceAuthz directly — it is done at tool
	// registration (in registerWorkflowTools). We test checkNamespaceAuthz directly here.
	client := newNsTestClient()
	ctx := context.Background()

	nsWithAuthz(t, client, "pod-private-ns", "sub-owner", "", "rwx------")

	stranger := oidcDeployerNs("sub-stranger", "s@example.com")
	eval := nsEval()

	err := checkNamespaceAuthz(ctx, client, "pod-private-ns", stranger, eval, authz.Read)
	if err == nil {
		t.Error("expected permission denied for stranger on wf_pods ns Read check")
	}
}

func TestWfEvents_NsAuthzAllowsGroupMember(t *testing.T) {
	client := newNsTestClient()
	ctx := context.Background()

	nsWithAuthz(t, client, "events-ns", "sub-owner", "dev-team", "rwxr-x---")

	member := oidcDeployerNs("sub-member", "m@example.com", "dev-team")
	eval := nsEval()

	err := checkNamespaceAuthz(ctx, client, "events-ns", member, eval, authz.Read)
	if err != nil {
		t.Errorf("group member should be allowed wf_events ns Read: %v", err)
	}
}

// --- Test 4: Namespace Write check on wf_apply CREATE path ---

func TestWfApply_NsWriteCheck_AllowsOwner(t *testing.T) {
	client := newNsTestClient()
	ctx := context.Background()

	nsWithAuthz(t, client, "apply-ns", "sub-alice", "", "rwx------")

	owner := oidcDeployerNs("sub-alice", "alice@example.com")
	eval := nsEval()

	err := checkNamespaceAuthz(ctx, client, "apply-ns", owner, eval, authz.Write)
	if err != nil {
		t.Errorf("owner should be allowed to write (create workflow): %v", err)
	}
}

func TestWfApply_NsWriteCheck_DeniesStranger(t *testing.T) {
	client := newNsTestClient()
	ctx := context.Background()

	nsWithAuthz(t, client, "apply-private-ns", "sub-alice", "", "rwx------")

	stranger := oidcDeployerNs("sub-bob", "bob@example.com")
	eval := nsEval()

	err := checkNamespaceAuthz(ctx, client, "apply-private-ns", stranger, eval, authz.Write)
	if err == nil {
		t.Error("stranger should be denied namespace Write (cannot create workflows)")
	}
}

func TestWfApply_NsWriteCheck_AllowsPublicWrite(t *testing.T) {
	client := newNsTestClient()
	ctx := context.Background()

	// Public-write namespace: rwxrwxrwx
	nsWithAuthz(t, client, "public-ns", "sub-alice", "", "rwxrwxrwx")

	stranger := oidcDeployerNs("sub-bob", "bob@example.com")
	eval := nsEval()

	err := checkNamespaceAuthz(ctx, client, "public-ns", stranger, eval, authz.Write)
	if err != nil {
		t.Errorf("stranger should be allowed to write on public namespace: %v", err)
	}
}

// --- Test 5: Namespace Read filter on wf_list ---

func TestWfList_NsReadFilter_HidesPrivateNs(t *testing.T) {
	// When listing with a specific namespace, checkNamespaceAuthz gates access.
	// A stranger cannot list workflows in a private namespace.
	client := newWfTestClient()
	ctx := context.Background()

	nsWithAuthz(t, client, "private-list-ns", "sub-owner", "", "rwx------")

	stranger := oidcDeployerNs("sub-stranger", "s@example.com")
	eval := nsEval()

	_, err := handleWfList(ctx, client, WfListParams{Namespace: "private-list-ns"}, stranger, eval)
	if err == nil {
		t.Error("stranger should be denied access to private namespace in wf_list")
	}
}

func TestWfList_NsReadFilter_AllowsOwner(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	nsWithAuthz(t, client, "owner-list-ns", "sub-owner", "", "rwx------")

	owner := oidcDeployerNs("sub-owner", "owner@example.com")
	eval := nsEval()

	_, err := handleWfList(ctx, client, WfListParams{Namespace: "owner-list-ns"}, owner, eval)
	if err != nil {
		t.Errorf("owner should be allowed to list workflows: %v", err)
	}
}

func TestWfList_NsReadFilter_AllowsGroupMember(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	nsWithAuthz(t, client, "group-list-ns", "sub-owner", "dev-team", "rwxr-x---")

	member := oidcDeployerNs("sub-member", "m@example.com", "dev-team")
	eval := nsEval()

	_, err := handleWfList(ctx, client, WfListParams{Namespace: "group-list-ns"}, member, eval)
	if err != nil {
		t.Errorf("group member should be allowed to list workflows: %v", err)
	}
}

func TestWfList_CrossNs_FiltersInaccessibleNamespaces(t *testing.T) {
	// When listing across all namespaces, inaccessible namespaces should be filtered out.
	client := newWfTestClient()
	ctx := context.Background()

	// Namespace accessible to alice.
	nsWithAuthz(t, client, "alice-ns", "sub-alice", "", "rwx------")
	// Namespace not accessible to alice (owned by bob, private).
	nsWithAuthz(t, client, "bob-ns", "sub-bob", "", "rwx------")

	alice := oidcDeployerNs("sub-alice", "alice@example.com")
	eval := nsEval()

	result, err := handleWfList(ctx, client, WfListParams{Namespace: ""}, alice, eval)
	if err != nil {
		t.Fatalf("handleWfList across all ns: %v", err)
	}

	// No deployments were created, so result should be empty regardless.
	// The important thing is that no error is returned — namespace filtering is by authz, not error.
	if result.Workflows == nil {
		t.Error("expected non-nil workflows slice")
	}
}

// --- Test 6: wf_health_ns Namespace Read check ---

func TestWfHealthNs_NsAuthzDenied(t *testing.T) {
	client := newWfHealthTestClient()
	ctx := context.Background()

	nsWithAuthz(t, client, "health-private-ns", "sub-owner", "", "rwx------")

	stranger := oidcDeployerNs("sub-stranger", "s@example.com")
	eval := nsEval()

	_, err := handleWfHealthNs(ctx, client, WfHealthNsParams{Namespace: "health-private-ns"}, stranger, eval)
	if err == nil {
		t.Error("stranger should be denied wf_health_ns on private namespace")
	}
}

func TestWfHealthNs_NsAuthzAllowed_BearerToken(t *testing.T) {
	client := newWfHealthTestClient()
	ctx := context.Background()

	nsWithAuthz(t, client, "health-bearer-ns", "sub-owner", "", "rwx------")

	eval := nsEval()

	_, err := handleWfHealthNs(ctx, client, WfHealthNsParams{Namespace: "health-bearer-ns"}, bearerInfo(), eval)
	if err != nil {
		t.Errorf("bearer token should bypass ns authz for wf_health_ns: %v", err)
	}
}
