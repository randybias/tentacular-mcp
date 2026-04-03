// Tests for the checkAuthz dual-path routing helper in authz_helpers.go.
//
// Covers:
//   - checkAuthz: legacy path (non-enclave namespace) routes to Check
//   - checkAuthz: enclave path routes to CheckTentacle
//   - checkAuthz: nil evaluator returns Allow
//   - checkAuthz: bearer-token deployer bypasses both paths
//   - checkAuthz: enclave owner bypasses tentacle-level check
//   - checkAuthz: enclave member allowed per mode
//   - checkAuthz: non-member denied on private enclave
//   - fetchNamespaceAnnotations: returns empty map for missing namespace
//   - fetchNamespaceAnnotations: returns annotations for existing namespace

package tools

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/randybias/tentacular-mcp/pkg/authz"
	"github.com/randybias/tentacular-mcp/pkg/exoskeleton"
)

// --- helpers ---

func enclaveEval() *authz.Evaluator {
	return authz.NewEvaluator(authz.DefaultEnclaveMode)
}

// oidcDeployerAuth creates an OIDC deployer for authz_helpers tests.
// Enclave authz uses email-based member matching, not IdP groups.
func oidcDeployerAuth(sub, email string) *exoskeleton.DeployerInfo {
	return &exoskeleton.DeployerInfo{
		Subject:  sub,
		Email:    email,
		Provider: "keycloak",
	}
}

// makeLegacyNsAnn returns namespace annotations for a legacy (non-enclave) namespace.
func makeLegacyNsAnn(owner, mode string) map[string]string {
	return map[string]string{
		authz.AnnotationOwner: owner,
		authz.AnnotationMode:  mode,
	}
}

// makeEnclaveNsAnn returns namespace annotations for an enclave namespace.
// The mode is rwxrwx--- by default (members have full access, others have none).
func makeEnclaveNsAnn(name, owner string, members ...string) map[string]string {
	ann := map[string]string{
		authz.AnnotationEnclave:      name,
		authz.AnnotationEnclaveOwner: owner,
		authz.AnnotationMode:         "rwxrwx---",
	}
	if len(members) > 0 {
		ann[authz.AnnotationEnclaveMembers] = members[0]
		for _, m := range members[1:] {
			ann[authz.AnnotationEnclaveMembers] += "," + m
		}
	}
	return ann
}

// makeTentacleAnn returns resource annotations for a tentacle (Deployment).
func makeTentacleAnn(owner, mode string) map[string]string {
	return map[string]string{
		authz.AnnotationOwner: owner,
		authz.AnnotationMode:  mode,
	}
}

// --- checkAuthz: nil evaluator ---

func TestCheckAuthz_NilEval_AlwaysAllows(t *testing.T) {
	d := checkAuthz(nil, oidcDeployerAuth("sub", "a@example.com"), makeLegacyNsAnn("b@example.com", "rwx------"), nil, authz.Read)
	if !d.Allowed {
		t.Errorf("nil evaluator should allow all, got: %s", d.Reason)
	}
}

// --- checkAuthz: legacy path (non-enclave namespace) ---

func TestCheckAuthz_Legacy_OwnerAllowed(t *testing.T) {
	eval := enclaveEval()
	deployer := oidcDeployerAuth("sub-alice", "alice@example.com")
	nsAnn := makeLegacyNsAnn("alice@example.com", "rwx------")
	resAnn := makeTentacleAnn("alice@example.com", "rwx------")

	d := checkAuthz(eval, deployer, nsAnn, resAnn, authz.Read)
	if !d.Allowed {
		t.Errorf("owner should be allowed on legacy namespace: %s", d.Reason)
	}
}

func TestCheckAuthz_Legacy_StrangerDenied(t *testing.T) {
	eval := enclaveEval()
	deployer := oidcDeployerAuth("sub-bob", "bob@example.com")
	nsAnn := makeLegacyNsAnn("alice@example.com", "rwx------")
	resAnn := makeTentacleAnn("alice@example.com", "rwx------")

	d := checkAuthz(eval, deployer, nsAnn, resAnn, authz.Read)
	if d.Allowed {
		t.Error("stranger should be denied on private legacy resource")
	}
}

func TestCheckAuthz_Legacy_BearerBypass(t *testing.T) {
	eval := enclaveEval()
	deployer := bearerInfo()
	nsAnn := makeLegacyNsAnn("alice@example.com", "rwx------")
	resAnn := makeTentacleAnn("alice@example.com", "rwx------")

	d := checkAuthz(eval, deployer, nsAnn, resAnn, authz.Read)
	if !d.Allowed {
		t.Errorf("bearer-token should bypass legacy authz: %s", d.Reason)
	}
}

// --- checkAuthz: enclave path ---

func TestCheckAuthz_Enclave_OwnerAllowed(t *testing.T) {
	eval := enclaveEval()
	deployer := oidcDeployerAuth("sub-owner", "owner@example.com")
	nsAnn := makeEnclaveNsAnn("my-enclave", "owner@example.com")
	resAnn := makeTentacleAnn("owner@example.com", "rwx------")

	d := checkAuthz(eval, deployer, nsAnn, resAnn, authz.Write)
	if !d.Allowed {
		t.Errorf("enclave owner should be allowed: %s", d.Reason)
	}
}

func TestCheckAuthz_Enclave_MemberAllowedReadWrite(t *testing.T) {
	eval := enclaveEval()
	deployer := oidcDeployerAuth("sub-member", "member@example.com")
	// Enclave mode rwxrwx--- gives members read+write+execute.
	nsAnn := makeEnclaveNsAnn("my-enclave", "owner@example.com", "member@example.com")
	// Tentacle owned by owner, mode rwxrwx--- means members can read/write.
	resAnn := makeTentacleAnn("owner@example.com", "rwxrwx---")

	d := checkAuthz(eval, deployer, nsAnn, resAnn, authz.Read)
	if !d.Allowed {
		t.Errorf("enclave member should be allowed to read: %s", d.Reason)
	}
}

func TestCheckAuthz_Enclave_NonMemberDenied(t *testing.T) {
	eval := enclaveEval()
	deployer := oidcDeployerAuth("sub-stranger", "stranger@example.com")
	// Enclave has no other bits (rwxrwx---), so strangers cannot access.
	nsAnn := makeEnclaveNsAnn("my-enclave", "owner@example.com", "member@example.com")
	resAnn := makeTentacleAnn("owner@example.com", "rwxrwx---")

	d := checkAuthz(eval, deployer, nsAnn, resAnn, authz.Read)
	if d.Allowed {
		t.Error("non-member stranger should be denied on private enclave")
	}
}

func TestCheckAuthz_Enclave_BearerBypass(t *testing.T) {
	eval := enclaveEval()
	deployer := bearerInfo()
	nsAnn := makeEnclaveNsAnn("my-enclave", "owner@example.com")
	resAnn := makeTentacleAnn("owner@example.com", "rwx------")

	d := checkAuthz(eval, deployer, nsAnn, resAnn, authz.Execute)
	if !d.Allowed {
		t.Errorf("bearer-token should bypass enclave authz: %s", d.Reason)
	}
}

func TestCheckAuthz_Enclave_ExecuteAllowedForMember(t *testing.T) {
	eval := enclaveEval()
	deployer := oidcDeployerAuth("sub-member", "member@example.com")
	// mode rwxrwx--- means group (members) have execute.
	nsAnn := makeEnclaveNsAnn("my-enclave", "owner@example.com", "member@example.com")
	resAnn := makeTentacleAnn("owner@example.com", "rwxrwx---")

	d := checkAuthz(eval, deployer, nsAnn, resAnn, authz.Execute)
	if !d.Allowed {
		t.Errorf("enclave member should be allowed to execute: %s", d.Reason)
	}
}

// TestCheckAuthz_DualPath_RoutingByNsAnnotation verifies that the path taken
// depends solely on whether the namespace has the enclave annotation.
// The test uses a public-read resource (rwxrwxrwx) so that on the legacy path
// the stranger is allowed via "other" bits. On the enclave path, CheckTentacle
// first checks enclave membership — the stranger is not a member and the
// enclave mode (rwxrwx---) has no "other" bits, so the stranger is denied.
func TestCheckAuthz_DualPath_RoutingByNsAnnotation(t *testing.T) {
	eval := enclaveEval()
	// Same deployer, same resource annotations — different result based on ns.
	deployer := oidcDeployerAuth("sub-stranger", "stranger@example.com")
	// Public resource: other bits are r-x, so legacy stranger can read.
	resAnn := makeTentacleAnn("owner@example.com", "rwxrwxr-x")

	// Legacy namespace: Check uses resource annotations directly.
	// Stranger is in "other" category; mode rwxrwxr-x → other.read = 'r' → allow.
	legacyNs := map[string]string{
		authz.AnnotationOwner: "owner@example.com",
		authz.AnnotationMode:  "rwxrwxr-x",
	}
	dLegacy := checkAuthz(eval, deployer, legacyNs, resAnn, authz.Read)
	if !dLegacy.Allowed {
		t.Errorf("legacy path: stranger should be allowed on rwxrwxr-x: %s", dLegacy.Reason)
	}

	// Enclave namespace: CheckTentacle checks enclave membership first.
	// Enclave mode is rwxrwx--- (no other bits), so stranger denied at enclave layer.
	enclaveNs := makeEnclaveNsAnn("my-enclave", "owner@example.com", "member@example.com")
	dEnclave := checkAuthz(eval, deployer, enclaveNs, resAnn, authz.Read)
	if dEnclave.Allowed {
		t.Error("enclave path: non-member stranger should be denied even though resource has other-read bits")
	}
}

// TestCheckAuthz_Enclave_PublicReadMode verifies a public-read enclave
// (mode rwxrwxr--) allows non-members to read but not write.
// This test uses a different enclave name to satisfy unparam.
func TestCheckAuthz_Enclave_PublicReadMode(t *testing.T) {
	eval := enclaveEval()
	deployer := oidcDeployerAuth("sub-pub", "pub@example.com")

	// Public-read enclave: other bits = r-- (read only).
	nsAnn := makeEnclaveNsAnn("public-enclave", "admin@example.com")
	nsAnn[authz.AnnotationMode] = "rwxrwxr--"
	resAnn := makeTentacleAnn("admin@example.com", "rwxrwxr--")

	dRead := checkAuthz(eval, deployer, nsAnn, resAnn, authz.Read)
	if !dRead.Allowed {
		t.Errorf("non-member should be allowed to read public-read enclave: %s", dRead.Reason)
	}

	dWrite := checkAuthz(eval, deployer, nsAnn, resAnn, authz.Write)
	if dWrite.Allowed {
		t.Error("non-member should NOT be allowed to write on public-read enclave")
	}
}

// TestCheckAuthz_Legacy_PublicWriteMode verifies a public-write legacy namespace
// (mode rwxrwxrwx) allows non-owners to write.
// This test uses a different mode to satisfy unparam.
func TestCheckAuthz_Legacy_PublicWriteMode(t *testing.T) {
	eval := enclaveEval()
	deployer := oidcDeployerAuth("sub-pub2", "pub2@example.com")

	nsAnn := makeLegacyNsAnn("admin@example.com", "rwxrwxrwx")
	resAnn := makeTentacleAnn("admin@example.com", "rwxrwxrwx")

	d := checkAuthz(eval, deployer, nsAnn, resAnn, authz.Write)
	if !d.Allowed {
		t.Errorf("non-owner should be allowed to write on public-write resource: %s", d.Reason)
	}
}

// --- fetchNamespaceAnnotations ---

func TestFetchNamespaceAnnotations_NotFound_ReturnsEmpty(t *testing.T) {
	client := newNsTestClient() // uses fake client seeded with no namespaces of interest
	ctx := context.Background()

	ann, err := fetchNamespaceAnnotations(ctx, client, "does-not-exist")
	if err != nil {
		t.Fatalf("expected no error for missing namespace, got: %v", err)
	}
	if ann == nil {
		t.Error("expected non-nil empty map")
	}
	if len(ann) != 0 {
		t.Errorf("expected empty map, got %d entries", len(ann))
	}
}

func TestFetchNamespaceAnnotations_NoAnnotations_ReturnsEmpty(t *testing.T) {
	client := newNsTestClient()
	ctx := context.Background()

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "bare-ns"},
	}
	_, _ = client.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})

	ann, err := fetchNamespaceAnnotations(ctx, client, "bare-ns")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ann == nil {
		t.Error("expected non-nil map")
	}
}

func TestFetchNamespaceAnnotations_ReturnsAnnotations(t *testing.T) {
	client := newNsTestClient()
	ctx := context.Background()

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "annotated-ns",
			Annotations: map[string]string{
				authz.AnnotationEnclave:      "my-enclave",
				authz.AnnotationEnclaveOwner: "owner@example.com",
			},
		},
	}
	_, _ = client.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})

	ann, err := fetchNamespaceAnnotations(ctx, client, "annotated-ns")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ann[authz.AnnotationEnclave] != "my-enclave" {
		t.Errorf("expected enclave annotation, got %q", ann[authz.AnnotationEnclave])
	}
	if ann[authz.AnnotationEnclaveOwner] != "owner@example.com" {
		t.Errorf("expected enclave-owner annotation, got %q", ann[authz.AnnotationEnclaveOwner])
	}
}

func TestFetchNamespaceAnnotations_IsEnclaveDetectable(t *testing.T) {
	client := newNsTestClient()
	ctx := context.Background()

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "enclave-ns",
			Annotations: map[string]string{
				authz.AnnotationEnclave: "my-enclave",
			},
		},
	}
	_, _ = client.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})

	ann, err := fetchNamespaceAnnotations(ctx, client, "enclave-ns")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !authz.IsEnclave(ann) {
		t.Error("expected IsEnclave to return true for annotated namespace")
	}
}
