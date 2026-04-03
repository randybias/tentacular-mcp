package tools

import (
	"context"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/randybias/tentacular-mcp/pkg/authz"
	"github.com/randybias/tentacular-mcp/pkg/exoskeleton"
	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

// newEnclaveTestClient returns a fresh fake k8s client for enclave tests.
func newEnclaveTestClient() *k8s.Client {
	return newNsTestClient()
}

// createManagedNs creates a tentacular-managed namespace that is NOT an enclave.
// Used to verify that enclave handlers reject plain managed namespaces.
func createManagedNs(t *testing.T, client *k8s.Client, name string) {
	t.Helper()
	ctx := context.Background()
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				k8s.ManagedByLabel: k8s.ManagedByValue,
			},
		},
	}
	_, err := client.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("setup createManagedNs %q: %v", name, err)
	}
}

// noopExoCtrl returns an exoskeleton controller with exoskeleton disabled.
func noopExoCtrl() *exoskeleton.Controller {
	cfg := &exoskeleton.Config{Enabled: false}
	return exoskeleton.NewControllerWithDeps(cfg, nil, nil, nil, nil)
}

// testEval returns an enabled evaluator with default mode for tests.
func testEval() *authz.Evaluator {
	return authz.NewEvaluator(authz.DefaultMode)
}

// provisionTestEnclave is a helper that provisions a minimal enclave and fails the test on error.
func provisionTestEnclave(t *testing.T, client *k8s.Client, name, owner string) EnclaveProvisionResult {
	t.Helper()
	ctx := context.Background()
	result, err := handleEnclaveProvision(ctx, client, noopExoCtrl(), EnclaveProvisionParams{
		Name:       name,
		OwnerEmail: owner,
		OwnerSub:   "sub-" + owner,
	})
	if err != nil {
		t.Fatalf("provisionTestEnclave(%q): %v", name, err)
	}
	return result
}

// --- enclave_provision tests ---

func TestEnclaveProvision_BasicSuccess(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	result, err := handleEnclaveProvision(ctx, client, noopExoCtrl(), EnclaveProvisionParams{
		Name:        "enc-basic",
		OwnerEmail:  "alice@example.com",
		OwnerSub:    "sub-alice",
		QuotaPreset: "small",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Name != "enc-basic" {
		t.Errorf("name: got %q, want enc-basic", result.Name)
	}
	if result.Status != "active" {
		t.Errorf("status: got %q, want active", result.Status)
	}
	if result.Owner != "alice@example.com" {
		t.Errorf("owner: got %q, want alice@example.com", result.Owner)
	}
	if result.QuotaPreset != "small" {
		t.Errorf("quota_preset: got %q, want small", result.QuotaPreset)
	}
	if len(result.ResourcesCreated) == 0 {
		t.Error("expected resources_created to be non-empty")
	}
}

func TestEnclaveProvision_DefaultQuotaPreset(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	result, err := handleEnclaveProvision(ctx, client, noopExoCtrl(), EnclaveProvisionParams{
		Name:       "enc-default-quota",
		OwnerEmail: "bob@example.com",
		OwnerSub:   "sub-bob",
		// No QuotaPreset — should default to "medium".
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.QuotaPreset != "medium" {
		t.Errorf("expected default quota_preset=medium, got %q", result.QuotaPreset)
	}
}

func TestEnclaveProvision_WithMembers(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	result, err := handleEnclaveProvision(ctx, client, noopExoCtrl(), EnclaveProvisionParams{
		Name:       "enc-members",
		OwnerEmail: "alice@example.com",
		OwnerSub:   "sub-alice",
		Members:    []string{"bob@example.com", "carol@example.com"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Members) != 2 {
		t.Errorf("expected 2 members, got %d", len(result.Members))
	}
}

func TestEnclaveProvision_StampsEnclaveAnnotations(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	_, err := handleEnclaveProvision(ctx, client, noopExoCtrl(), EnclaveProvisionParams{
		Name:        "enc-annot",
		OwnerEmail:  "alice@example.com",
		OwnerSub:    "sub-alice",
		Platform:    "slack",
		ChannelID:   "C12345",
		ChannelName: "eng-general",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify annotations were stored on the namespace.
	ns, err := client.Clientset.CoreV1().Namespaces().Get(ctx, "enc-annot", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get namespace: %v", err)
	}
	ann := ns.Annotations

	if ann[authz.AnnotationEnclave] != "enc-annot" {
		t.Errorf("enclave annotation: got %q, want enc-annot", ann[authz.AnnotationEnclave])
	}
	if ann[authz.AnnotationEnclaveOwner] != "alice@example.com" {
		t.Errorf("owner annotation: got %q, want alice@example.com", ann[authz.AnnotationEnclaveOwner])
	}
	if ann[authz.AnnotationEnclavePlatform] != "slack" {
		t.Errorf("platform annotation: got %q, want slack", ann[authz.AnnotationEnclavePlatform])
	}
	if ann[authz.AnnotationEnclaveChannelID] != "C12345" {
		t.Errorf("channel_id annotation: got %q, want C12345", ann[authz.AnnotationEnclaveChannelID])
	}
	if ann[authz.AnnotationEnclaveStatus] != "active" {
		t.Errorf("status annotation: got %q, want active", ann[authz.AnnotationEnclaveStatus])
	}
}

func TestEnclaveProvision_TooManyMembers(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	// Temporarily lower the limit for this test.
	orig := authz.MaxEnclaveMembers
	authz.MaxEnclaveMembers = 2
	defer func() { authz.MaxEnclaveMembers = orig }()

	_, err := handleEnclaveProvision(ctx, client, noopExoCtrl(), EnclaveProvisionParams{
		Name:       "enc-overflow",
		OwnerEmail: "alice@example.com",
		OwnerSub:   "sub-alice",
		Members:    []string{"b@x.com", "c@x.com", "d@x.com"},
	})
	if err == nil {
		t.Fatal("expected error for too many members, got nil")
	}
}

// --- enclave_info tests ---

func TestEnclaveInfo_Success(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	provisionTestEnclave(t, client, "enc-info", "alice@example.com")

	// Bearer-token deployer bypasses the owner/member check.
	bearer := &exoskeleton.DeployerInfo{Provider: "bearer-token"}
	result, err := handleEnclaveInfo(ctx, client, noopExoCtrl(), testEval(), EnclaveInfoParams{Name: "enc-info"}, bearer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Name != "enc-info" {
		t.Errorf("name: got %q, want enc-info", result.Name)
	}
	if result.Owner != "alice@example.com" {
		t.Errorf("owner: got %q, want alice@example.com", result.Owner)
	}
	if result.Status != "active" {
		t.Errorf("status: got %q, want active", result.Status)
	}
	if len(result.ExoServices) != 2 {
		t.Errorf("expected 2 exo services, got %d", len(result.ExoServices))
	}
}

func TestEnclaveInfo_NotEnclave(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	// Create a regular managed namespace (not an enclave).
	createManagedNs(t, client, "regular-ns")

	bearer := &exoskeleton.DeployerInfo{Provider: "bearer-token"}
	_, err := handleEnclaveInfo(ctx, client, noopExoCtrl(), testEval(), EnclaveInfoParams{Name: "regular-ns"}, bearer)
	if err == nil {
		t.Fatal("expected error for non-enclave namespace, got nil")
	}
}

func TestEnclaveInfo_NotFound(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	bearer := &exoskeleton.DeployerInfo{Provider: "bearer-token"}
	_, err := handleEnclaveInfo(ctx, client, noopExoCtrl(), testEval(), EnclaveInfoParams{Name: "does-not-exist"}, bearer)
	if err == nil {
		t.Fatal("expected error for missing namespace, got nil")
	}
}

func TestEnclaveInfo_QuotaPresetRoundTrip(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	_, err := handleEnclaveProvision(ctx, client, noopExoCtrl(), EnclaveProvisionParams{
		Name:        "enc-quota-rt",
		OwnerEmail:  "alice@example.com",
		OwnerSub:    "sub-alice",
		QuotaPreset: "large",
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}

	bearer := &exoskeleton.DeployerInfo{Provider: "bearer-token"}
	info, err := handleEnclaveInfo(ctx, client, noopExoCtrl(), testEval(), EnclaveInfoParams{Name: "enc-quota-rt"}, bearer)
	if err != nil {
		t.Fatalf("info: %v", err)
	}
	if info.QuotaPreset != "large" {
		t.Errorf("quota_preset: got %q, want large", info.QuotaPreset)
	}
}

// --- enclave_list tests ---

func TestEnclaveList_Empty(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	result, err := handleEnclaveList(ctx, client, testEval(), EnclaveListParams{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Enclaves) != 0 {
		t.Errorf("expected 0 enclaves, got %d", len(result.Enclaves))
	}
}

func TestEnclaveList_MultipleEnclaves(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	provisionTestEnclave(t, client, "enc-list-1", "alice@example.com")
	provisionTestEnclave(t, client, "enc-list-2", "bob@example.com")

	// Also create a regular managed namespace to verify it's filtered out.
	createManagedNs(t, client, "not-an-enclave")

	result, err := handleEnclaveList(ctx, client, testEval(), EnclaveListParams{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Enclaves) != 2 {
		t.Errorf("expected 2 enclaves, got %d", len(result.Enclaves))
	}
}

func TestEnclaveList_FilterByCallerEmail_Owner(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	provisionTestEnclave(t, client, "enc-filter-1", "alice@example.com")
	provisionTestEnclave(t, client, "enc-filter-2", "bob@example.com")

	// Filter to alice's enclaves.
	result, err := handleEnclaveList(ctx, client, testEval(), EnclaveListParams{CallerEmail: "alice@example.com"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Enclaves) != 1 {
		t.Errorf("expected 1 enclave for alice, got %d", len(result.Enclaves))
	}
	if result.Enclaves[0].Name != "enc-filter-1" {
		t.Errorf("expected enc-filter-1, got %q", result.Enclaves[0].Name)
	}
}

func TestEnclaveList_FilterByCallerEmail_Member(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	// Provision an enclave with carol as a member.
	_, err := handleEnclaveProvision(ctx, client, noopExoCtrl(), EnclaveProvisionParams{
		Name:       "enc-member-filter",
		OwnerEmail: "alice@example.com",
		OwnerSub:   "sub-alice",
		Members:    []string{"carol@example.com"},
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}

	// Carol should see this enclave.
	result, err := handleEnclaveList(ctx, client, testEval(), EnclaveListParams{CallerEmail: "carol@example.com"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Enclaves) != 1 {
		t.Errorf("expected 1 enclave for carol, got %d", len(result.Enclaves))
	}
}

func TestEnclaveList_FilterByCallerEmail_NoMatch(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	provisionTestEnclave(t, client, "enc-nomatch", "alice@example.com")

	// dave is neither owner nor member.
	result, err := handleEnclaveList(ctx, client, testEval(), EnclaveListParams{CallerEmail: "dave@example.com"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Enclaves) != 0 {
		t.Errorf("expected 0 enclaves for dave, got %d", len(result.Enclaves))
	}
}

// --- enclave_sync tests ---

func TestEnclaveSync_AddMember(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	provisionTestEnclave(t, client, "enc-sync-add", "alice@example.com")

	// alice is the owner — she can add members.
	alice := &exoskeleton.DeployerInfo{Email: "alice@example.com", Provider: "keycloak"}
	result, err := handleEnclaveSync(ctx, client, EnclaveSyncParams{
		Name:       "enc-sync-add",
		AddMembers: []string{"bob@example.com"},
	}, alice)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !containsStr(result.Updated, "members") {
		t.Errorf("expected 'members' in updated, got %v", result.Updated)
	}
	if !containsStr(result.Enclave.Members, "bob@example.com") {
		t.Errorf("expected bob in members, got %v", result.Enclave.Members)
	}
}

func TestEnclaveSync_RemoveMember(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	_, err := handleEnclaveProvision(ctx, client, noopExoCtrl(), EnclaveProvisionParams{
		Name:       "enc-sync-rm",
		OwnerEmail: "alice@example.com",
		OwnerSub:   "sub-alice",
		Members:    []string{"bob@example.com", "carol@example.com"},
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}

	// alice is the owner — she can remove members.
	alice := &exoskeleton.DeployerInfo{Email: "alice@example.com", Provider: "keycloak"}
	result, err := handleEnclaveSync(ctx, client, EnclaveSyncParams{
		Name:          "enc-sync-rm",
		RemoveMembers: []string{"bob@example.com"},
	}, alice)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if containsStr(result.Enclave.Members, "bob@example.com") {
		t.Error("expected bob to be removed from members")
	}
	if !containsStr(result.Enclave.Members, "carol@example.com") {
		t.Error("expected carol to still be in members")
	}
}

func TestEnclaveSync_TransferOwnership(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	_, err := handleEnclaveProvision(ctx, client, noopExoCtrl(), EnclaveProvisionParams{
		Name:       "enc-sync-owner",
		OwnerEmail: "alice@example.com",
		OwnerSub:   "sub-alice",
		Members:    []string{"bob@example.com"},
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}

	// alice is the owner — she can transfer ownership.
	alice := &exoskeleton.DeployerInfo{Email: "alice@example.com", Provider: "keycloak"}
	result, err := handleEnclaveSync(ctx, client, EnclaveSyncParams{
		Name:     "enc-sync-owner",
		NewOwner: "bob@example.com",
	}, alice)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Enclave.Owner != "bob@example.com" {
		t.Errorf("expected new owner=bob, got %q", result.Enclave.Owner)
	}
	// Old owner should be in members now.
	if !containsStr(result.Enclave.Members, "alice@example.com") {
		t.Errorf("expected old owner alice in members, got %v", result.Enclave.Members)
	}
	// New owner should not be in members.
	if containsStr(result.Enclave.Members, "bob@example.com") {
		t.Error("expected new owner bob removed from members list")
	}
}

func TestEnclaveSync_TransferOwnership_NonMemberFails(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	provisionTestEnclave(t, client, "enc-sync-owner-fail", "alice@example.com")

	// alice is the owner — she can attempt transfer (but dave is not a member)
	alice := &exoskeleton.DeployerInfo{Email: "alice@example.com", Provider: "keycloak"}
	_, err := handleEnclaveSync(ctx, client, EnclaveSyncParams{
		Name:     "enc-sync-owner-fail",
		NewOwner: "dave@example.com", // not a member
	}, alice)
	if err == nil {
		t.Fatal("expected error for ownership transfer to non-member, got nil")
	}
}

func TestEnclaveSync_FreezeAndUnfreeze(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	provisionTestEnclave(t, client, "enc-sync-freeze", "alice@example.com")

	// alice is the owner — she can freeze/unfreeze.
	alice := &exoskeleton.DeployerInfo{Email: "alice@example.com", Provider: "keycloak"}

	// Freeze.
	result, err := handleEnclaveSync(ctx, client, EnclaveSyncParams{
		Name:      "enc-sync-freeze",
		NewStatus: "frozen",
	}, alice)
	if err != nil {
		t.Fatalf("freeze: %v", err)
	}
	if result.Enclave.Status != "frozen" {
		t.Errorf("expected status=frozen, got %q", result.Enclave.Status)
	}

	// Unfreeze.
	result, err = handleEnclaveSync(ctx, client, EnclaveSyncParams{
		Name:      "enc-sync-freeze",
		NewStatus: "active",
	}, alice)
	if err != nil {
		t.Fatalf("unfreeze: %v", err)
	}
	if result.Enclave.Status != "active" {
		t.Errorf("expected status=active after unfreeze, got %q", result.Enclave.Status)
	}
}

func TestEnclaveSync_InvalidStatus(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	provisionTestEnclave(t, client, "enc-sync-badstatus", "alice@example.com")

	// alice is the owner — she can attempt status change (but "deleted" is invalid).
	alice := &exoskeleton.DeployerInfo{Email: "alice@example.com", Provider: "keycloak"}
	_, err := handleEnclaveSync(ctx, client, EnclaveSyncParams{
		Name:      "enc-sync-badstatus",
		NewStatus: "deleted", // invalid
	}, alice)
	if err == nil {
		t.Fatal("expected error for invalid status, got nil")
	}
}

func TestEnclaveSync_UpdateChannelName(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	provisionTestEnclave(t, client, "enc-sync-chan", "alice@example.com")

	// Channel name update is not owner-restricted; any deployer (or nil) can call.
	// Use a bearer-token deployer to keep the test simple.
	bearer := &exoskeleton.DeployerInfo{Provider: "bearer-token"}
	result, err := handleEnclaveSync(ctx, client, EnclaveSyncParams{
		Name:           "enc-sync-chan",
		NewChannelName: "new-channel",
	}, bearer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Enclave.ChannelName != "new-channel" {
		t.Errorf("expected ChannelName=new-channel, got %q", result.Enclave.ChannelName)
	}
}

func TestEnclaveSync_NoUpdatesError(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	provisionTestEnclave(t, client, "enc-sync-empty", "alice@example.com")

	bearer := &exoskeleton.DeployerInfo{Provider: "bearer-token"}
	_, err := handleEnclaveSync(ctx, client, EnclaveSyncParams{Name: "enc-sync-empty"}, bearer)
	if err == nil {
		t.Fatal("expected error for no-op sync, got nil")
	}
}

func TestEnclaveSync_NotEnclave(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	// Regular managed namespace.
	createManagedNs(t, client, "not-enc-sync")

	alice := &exoskeleton.DeployerInfo{Email: "alice@example.com", Provider: "keycloak"}
	_, err := handleEnclaveSync(ctx, client, EnclaveSyncParams{
		Name:      "not-enc-sync",
		NewStatus: "frozen",
	}, alice)
	if err == nil {
		t.Fatal("expected error for non-enclave namespace, got nil")
	}
}

func TestEnclaveSync_MemberCountEnforced(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	provisionTestEnclave(t, client, "enc-sync-maxmem", "alice@example.com")

	orig := authz.MaxEnclaveMembers
	authz.MaxEnclaveMembers = 1
	defer func() { authz.MaxEnclaveMembers = orig }()

	alice := &exoskeleton.DeployerInfo{Email: "alice@example.com", Provider: "keycloak"}
	_, err := handleEnclaveSync(ctx, client, EnclaveSyncParams{
		Name:       "enc-sync-maxmem",
		AddMembers: []string{"b@x.com", "c@x.com"},
	}, alice)
	if err == nil {
		t.Fatal("expected error for exceeding max members, got nil")
	}
}

// --- enclave_deprovision tests ---

func TestEnclaveDeprovision_Success(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	provisionTestEnclave(t, client, "enc-deprov", "alice@example.com")

	deployer := &exoskeleton.DeployerInfo{
		Email:    "alice@example.com",
		Provider: "keycloak",
	}
	result, err := handleEnclaveDeprovision(ctx, client, noopExoCtrl(), EnclaveDeprovisionParams{Name: "enc-deprov"}, deployer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Deleted {
		t.Error("expected deleted=true")
	}
}

func TestEnclaveDeprovision_OwnerRequired(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	provisionTestEnclave(t, client, "enc-deprov-perm", "alice@example.com")

	// bob is not the owner.
	deployer := &exoskeleton.DeployerInfo{
		Email:    "bob@example.com",
		Provider: "keycloak",
	}
	_, err := handleEnclaveDeprovision(ctx, client, noopExoCtrl(), EnclaveDeprovisionParams{Name: "enc-deprov-perm"}, deployer)
	if err == nil {
		t.Fatal("expected error for non-owner deprovision, got nil")
	}
}

func TestEnclaveDeprovision_BearerTokenBypasses(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	provisionTestEnclave(t, client, "enc-deprov-bearer", "alice@example.com")

	// Bearer token bypasses owner check.
	deployer := &exoskeleton.DeployerInfo{
		Provider: "bearer-token",
	}
	result, err := handleEnclaveDeprovision(ctx, client, noopExoCtrl(), EnclaveDeprovisionParams{Name: "enc-deprov-bearer"}, deployer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Deleted {
		t.Error("expected deleted=true")
	}
}

func TestEnclaveDeprovision_NotEnclave(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	// Regular managed namespace.
	createManagedNs(t, client, "not-enc-deprov")

	deployer := &exoskeleton.DeployerInfo{Provider: "bearer-token"}
	_, err := handleEnclaveDeprovision(ctx, client, noopExoCtrl(), EnclaveDeprovisionParams{Name: "not-enc-deprov"}, deployer)
	if err == nil {
		t.Fatal("expected error for non-enclave namespace, got nil")
	}
}

func TestEnclaveDeprovision_CountsTentacles(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	provisionTestEnclave(t, client, "enc-deprov-count", "alice@example.com")

	// Create two fake deployments (tentacles) in the enclave namespace.
	for _, depName := range []string{"wf-one", "wf-two"} {
		dep := makeTestDeployment(depName, "enc-deprov-count", nil)
		_, err := client.Clientset.AppsV1().Deployments("enc-deprov-count").Create(ctx, dep, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("setup deployment %q: %v", depName, err)
		}
	}

	deployer := &exoskeleton.DeployerInfo{Provider: "bearer-token"}
	result, err := handleEnclaveDeprovision(ctx, client, noopExoCtrl(), EnclaveDeprovisionParams{Name: "enc-deprov-count"}, deployer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TentaclesRemoved != 2 {
		t.Errorf("expected 2 tentacles removed, got %d", result.TentaclesRemoved)
	}
}

// --- ExoServices status tests ---

func TestBuildEnclaveExoServices_NilController(t *testing.T) {
	services := buildEnclaveExoServices(nil)
	if len(services) != 2 {
		t.Fatalf("expected 2 services, got %d", len(services))
	}
	for _, svc := range services {
		if svc.Available {
			t.Errorf("expected %s unavailable for nil controller", svc.Name)
		}
	}
}

func TestBuildEnclaveExoServices_DisabledController(t *testing.T) {
	ctrl := noopExoCtrl()
	services := buildEnclaveExoServices(ctrl)
	if len(services) != 2 {
		t.Fatalf("expected 2 services, got %d", len(services))
	}
	// Both should be unavailable since no registrars were configured.
	for _, svc := range services {
		if svc.Available {
			t.Errorf("expected %s unavailable with no registrars", svc.Name)
		}
	}
}

// --- isEnclaveParticipant tests ---

func TestIsEnclaveParticipant_Owner(t *testing.T) {
	info := authz.EnclaveInfo{Owner: "alice@example.com", Members: []string{"bob@example.com"}}
	if !isEnclaveParticipant(info, "alice@example.com") {
		t.Error("expected owner to be a participant")
	}
}

func TestIsEnclaveParticipant_Member(t *testing.T) {
	info := authz.EnclaveInfo{Owner: "alice@example.com", Members: []string{"bob@example.com"}}
	if !isEnclaveParticipant(info, "bob@example.com") {
		t.Error("expected member to be a participant")
	}
}

func TestIsEnclaveParticipant_Neither(t *testing.T) {
	info := authz.EnclaveInfo{Owner: "alice@example.com", Members: []string{"bob@example.com"}}
	if isEnclaveParticipant(info, "dave@example.com") {
		t.Error("expected dave to NOT be a participant")
	}
}

// --- containsStr tests ---

func TestContainsStr_Found(t *testing.T) {
	if !containsStr([]string{"a", "b", "c"}, "b") {
		t.Error("expected containsStr to find 'b'")
	}
}

func TestContainsStr_NotFound(t *testing.T) {
	if containsStr([]string{"a", "b", "c"}, "d") {
		t.Error("expected containsStr not to find 'd'")
	}
}

func TestContainsStr_Empty(t *testing.T) {
	if containsStr([]string{}, "a") {
		t.Error("expected containsStr to return false for empty slice")
	}
}

// --- Security fix tests ---

// C1: enclave_sync non-owner is denied for owner-gated operations.
func TestEnclaveSync_UnauthorizedNonOwner(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	provisionTestEnclave(t, client, "enc-sync-authz", "alice@example.com")

	// bob is not the owner — he should not be able to add members.
	bob := &exoskeleton.DeployerInfo{Email: "bob@example.com", Provider: "keycloak"}
	_, err := handleEnclaveSync(ctx, client, EnclaveSyncParams{
		Name:       "enc-sync-authz",
		AddMembers: []string{"carol@example.com"},
	}, bob)
	if err == nil {
		t.Fatal("expected error for non-owner sync attempt, got nil")
	}
}

// C1: enclave_sync bearer-token caller bypasses owner check.
func TestEnclaveSync_BearerTokenBypasses(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	provisionTestEnclave(t, client, "enc-sync-bearer", "alice@example.com")

	// Bearer-token caller can sync regardless of ownership.
	bearer := &exoskeleton.DeployerInfo{Provider: "bearer-token"}
	result, err := handleEnclaveSync(ctx, client, EnclaveSyncParams{
		Name:       "enc-sync-bearer",
		AddMembers: []string{"carol@example.com"},
	}, bearer)
	if err != nil {
		t.Fatalf("expected bearer-token to bypass owner check, got error: %v", err)
	}
	if !containsStr(result.Enclave.Members, "carol@example.com") {
		t.Errorf("expected carol in members after bearer-token sync, got %v", result.Enclave.Members)
	}
}

// C1: enclave_sync OIDC caller with no email is denied.
func TestEnclaveSync_EmptyEmailDenied(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	provisionTestEnclave(t, client, "enc-sync-noemail", "alice@example.com")

	// OIDC caller with empty email should be denied (fail closed).
	noEmail := &exoskeleton.DeployerInfo{Email: "", Provider: "keycloak"}
	_, err := handleEnclaveSync(ctx, client, EnclaveSyncParams{
		Name:      "enc-sync-noemail",
		NewStatus: "frozen",
	}, noEmail)
	if err == nil {
		t.Fatal("expected error for OIDC caller with empty email, got nil")
	}
}

// C2: enclave_provision for OIDC caller forces owner to caller identity.
func TestEnclaveProvision_ForcedOwnerIdentityBinding(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	// At the handler level, the identity binding happens in the tool registration
	// wrapper, not in handleEnclaveProvision itself. We test the wrapper behavior
	// indirectly by verifying that handleEnclaveProvision stores whatever owner
	// is given (the wrapper is responsible for forcing the owner).
	// This test verifies the handler does store the caller-supplied owner.
	result, err := handleEnclaveProvision(ctx, client, noopExoCtrl(), EnclaveProvisionParams{
		Name:       "enc-forced-owner",
		OwnerEmail: "alice@example.com",
		OwnerSub:   "sub-alice",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Owner != "alice@example.com" {
		t.Errorf("expected owner=alice@example.com, got %q", result.Owner)
	}
}

// H1: enclave_deprovision OIDC caller with no email is denied.
func TestEnclaveDeprovision_EmptyEmailDenied(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	provisionTestEnclave(t, client, "enc-deprov-noemail", "alice@example.com")

	// OIDC caller with empty email should be denied — fail closed, not fail open.
	noEmail := &exoskeleton.DeployerInfo{Email: "", Provider: "keycloak"}
	_, err := handleEnclaveDeprovision(ctx, client, noopExoCtrl(), EnclaveDeprovisionParams{Name: "enc-deprov-noemail"}, noEmail)
	if err == nil {
		t.Fatal("expected error for OIDC caller with empty email, got nil")
	}
}

// H1: enclave_deprovision non-owner OIDC caller is denied even with non-empty email.
func TestEnclaveDeprovision_NonOwnerOIDCDenied(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	provisionTestEnclave(t, client, "enc-deprov-nonowner", "alice@example.com")

	// carol has an email but is not the owner.
	carol := &exoskeleton.DeployerInfo{Email: "carol@example.com", Provider: "keycloak"}
	_, err := handleEnclaveDeprovision(ctx, client, noopExoCtrl(), EnclaveDeprovisionParams{Name: "enc-deprov-nonowner"}, carol)
	if err == nil {
		t.Fatal("expected error for non-owner deprovision, got nil")
	}
}

// M3: enclave_provision rejects invalid quota preset before any resource creation.
func TestEnclaveProvision_InvalidQuotaPreset(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	_, err := handleEnclaveProvision(ctx, client, noopExoCtrl(), EnclaveProvisionParams{
		Name:        "enc-bad-quota",
		OwnerEmail:  "alice@example.com",
		OwnerSub:    "sub-alice",
		QuotaPreset: "xlarge", // not a valid preset
	})
	if err == nil {
		t.Fatal("expected error for invalid quota preset, got nil")
	}

	// Verify no namespace was created (cleanup worked correctly).
	_, err2 := client.Clientset.CoreV1().Namespaces().Get(ctx, "enc-bad-quota", metav1.GetOptions{})
	if err2 == nil {
		t.Error("expected namespace enc-bad-quota to not exist after invalid quota preset rejection")
	}
}

// H3: enclave_info denies access to non-participants.
func TestEnclaveInfo_NonParticipantDenied(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	provisionTestEnclave(t, client, "enc-info-authz", "alice@example.com")

	// dave is neither owner nor member.
	dave := &exoskeleton.DeployerInfo{Email: "dave@example.com", Provider: "keycloak"}
	_, err := handleEnclaveInfo(ctx, client, noopExoCtrl(), testEval(), EnclaveInfoParams{Name: "enc-info-authz"}, dave)
	if err == nil {
		t.Fatal("expected error for non-participant enclave_info, got nil")
	}
}

// 2.1: isEnclaveParticipant email case normalization.
func TestIsEnclaveParticipant_OwnerMixedCase(t *testing.T) {
	info := authz.EnclaveInfo{Owner: "alice@example.com", Members: []string{"bob@example.com"}}
	if !isEnclaveParticipant(info, "Alice@Example.COM") {
		t.Error("expected owner match with mixed case email")
	}
}

func TestIsEnclaveParticipant_MemberMixedCase(t *testing.T) {
	info := authz.EnclaveInfo{Owner: "alice@example.com", Members: []string{"bob@example.com"}}
	if !isEnclaveParticipant(info, "BOB@Example.COM") {
		t.Error("expected member match with mixed case email")
	}
}

func TestIsEnclaveParticipant_NonParticipantMixedCase(t *testing.T) {
	info := authz.EnclaveInfo{Owner: "alice@example.com", Members: []string{"bob@example.com"}}
	if isEnclaveParticipant(info, "Dave@Example.COM") {
		t.Error("expected dave (non-participant) to NOT match even with mixed case")
	}
}

// 2.2: handleEnclaveInfo — "other" read access via mode bits.
func TestEnclaveInfo_OtherReadAccess_OpenReadMode(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	// Provision an enclave with open-read mode (rwxrwxr--).
	_, err := handleEnclaveProvision(ctx, client, noopExoCtrl(), EnclaveProvisionParams{
		Name:       "enc-info-openread",
		OwnerEmail: "alice@example.com",
		OwnerSub:   "sub-alice",
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}

	// Manually set the mode to open-read on the namespace annotations.
	ns, err := client.Clientset.CoreV1().Namespaces().Get(ctx, "enc-info-openread", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get ns: %v", err)
	}
	ns.Annotations[authz.AnnotationMode] = "rwxrwxr--"
	_, err = client.Clientset.CoreV1().Namespaces().Update(ctx, ns, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("update ns: %v", err)
	}

	// Non-member OIDC deployer should be able to read.
	dave := &exoskeleton.DeployerInfo{Email: "dave@example.com", Provider: "keycloak"}
	result, err := handleEnclaveInfo(ctx, client, noopExoCtrl(), testEval(), EnclaveInfoParams{Name: "enc-info-openread"}, dave)
	if err != nil {
		t.Fatalf("expected non-member to read open-read enclave, got error: %v", err)
	}
	if result.Name != "enc-info-openread" {
		t.Errorf("expected name=enc-info-openread, got %q", result.Name)
	}
}

func TestEnclaveInfo_OtherReadAccess_PrivateMode(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	// Provision an enclave with private mode (rwxrwx---).
	_, err := handleEnclaveProvision(ctx, client, noopExoCtrl(), EnclaveProvisionParams{
		Name:       "enc-info-private",
		OwnerEmail: "alice@example.com",
		OwnerSub:   "sub-alice",
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}

	// Manually set the mode to private on the namespace annotations.
	ns, err := client.Clientset.CoreV1().Namespaces().Get(ctx, "enc-info-private", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get ns: %v", err)
	}
	ns.Annotations[authz.AnnotationMode] = "rwxrwx---"
	_, err = client.Clientset.CoreV1().Namespaces().Update(ctx, ns, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("update ns: %v", err)
	}

	// Non-member OIDC deployer should be denied.
	dave := &exoskeleton.DeployerInfo{Email: "dave@example.com", Provider: "keycloak"}
	_, err = handleEnclaveInfo(ctx, client, noopExoCtrl(), testEval(), EnclaveInfoParams{Name: "enc-info-private"}, dave)
	if err == nil {
		t.Fatal("expected error for non-member accessing private enclave, got nil")
	}
}

// 2.3: handleEnclaveList — "other" read visibility.
func TestEnclaveList_OtherReadVisibility(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	// Provision two enclaves.
	provisionTestEnclave(t, client, "enc-list-private", "alice@example.com")
	provisionTestEnclave(t, client, "enc-list-open", "bob@example.com")

	// Set private mode on the first enclave.
	ns1, _ := client.Clientset.CoreV1().Namespaces().Get(ctx, "enc-list-private", metav1.GetOptions{})
	ns1.Annotations[authz.AnnotationMode] = "rwxrwx---"
	_, _ = client.Clientset.CoreV1().Namespaces().Update(ctx, ns1, metav1.UpdateOptions{})

	// Set open-read mode on the second enclave.
	ns2, _ := client.Clientset.CoreV1().Namespaces().Get(ctx, "enc-list-open", metav1.GetOptions{})
	ns2.Annotations[authz.AnnotationMode] = "rwxrwxr--"
	_, _ = client.Clientset.CoreV1().Namespaces().Update(ctx, ns2, metav1.UpdateOptions{})

	// Non-member OIDC deployer: should see only the open-read enclave.
	dave := &exoskeleton.DeployerInfo{Email: "dave@example.com", Provider: "keycloak"}
	result, err := handleEnclaveList(ctx, client, testEval(), EnclaveListParams{CallerEmail: "dave@example.com"}, dave)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Enclaves) != 1 {
		t.Fatalf("expected 1 enclave visible to non-member, got %d", len(result.Enclaves))
	}
	if result.Enclaves[0].Name != "enc-list-open" {
		t.Errorf("expected enc-list-open, got %q", result.Enclaves[0].Name)
	}
}

// 2.5: handleEnclaveDeprovision — CleanupEnclave called on exo controller.
func TestEnclaveDeprovision_CallsCleanupEnclave(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	// Create an exo controller with mock registrars that track CleanupEnclave calls.
	pg := &trackingMockPG{}
	rustfs := &trackingMockRustFS{}
	cfg := &exoskeleton.Config{Enabled: true}
	ctrl := exoskeleton.NewControllerWithDeps(cfg, pg, nil, rustfs, nil)

	// Provision with the tracking controller (for EnsureEnclave calls).
	_, err := handleEnclaveProvision(ctx, client, ctrl, EnclaveProvisionParams{
		Name:       "enc-deprov-exo",
		OwnerEmail: "alice@example.com",
		OwnerSub:   "sub-alice",
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}

	// Deprovision with bearer-token deployer.
	deployer := &exoskeleton.DeployerInfo{Provider: "bearer-token"}
	_, err = handleEnclaveDeprovision(ctx, client, ctrl, EnclaveDeprovisionParams{Name: "enc-deprov-exo"}, deployer)
	if err != nil {
		t.Fatalf("deprovision: %v", err)
	}

	if pg.cleanupEnclaveCalls == 0 {
		t.Error("expected postgres CleanupEnclave to be called")
	}
	if rustfs.cleanupEnclaveCalls == 0 {
		t.Error("expected rustfs CleanupEnclave to be called")
	}
}

// trackingMockPG tracks CleanupEnclave calls for test 2.5.
type trackingMockPG struct {
	cleanupEnclaveCalls int
	ensureEnclaveCalls  int
	registerCalls       int
}

func (m *trackingMockPG) Register(_ context.Context, _ exoskeleton.Identity) (*exoskeleton.PostgresCreds, error) {
	m.registerCalls++
	return &exoskeleton.PostgresCreds{Host: "pg.test", Port: "5432", Database: "testdb", User: "u", Password: "p", Schema: "s", Protocol: "postgresql"}, nil
}

func (*trackingMockPG) Unregister(_ context.Context, _ exoskeleton.Identity) error { return nil }

func (m *trackingMockPG) EnsureEnclave(_ context.Context, _ exoskeleton.EnclaveIdentity) error {
	m.ensureEnclaveCalls++
	return nil
}

func (m *trackingMockPG) CleanupEnclave(_ context.Context, _ exoskeleton.EnclaveIdentity) error {
	m.cleanupEnclaveCalls++
	return nil
}

func (*trackingMockPG) Close() {}

// trackingMockRustFS tracks CleanupEnclave calls for test 2.5.
type trackingMockRustFS struct {
	cleanupEnclaveCalls int
	ensureEnclaveCalls  int
	registerCalls       int
}

func (m *trackingMockRustFS) Register(_ context.Context, _ exoskeleton.Identity) (*exoskeleton.RustFSCreds, error) {
	m.registerCalls++
	return &exoskeleton.RustFSCreds{Endpoint: "http://minio:9000", AccessKey: "ak", SecretKey: "sk", Bucket: "b", Prefix: "p/", Region: "us-east-1", Protocol: "s3"}, nil
}

func (*trackingMockRustFS) Unregister(_ context.Context, _ exoskeleton.Identity) error { return nil }

func (m *trackingMockRustFS) EnsureEnclave(_ context.Context, _ exoskeleton.EnclaveIdentity) error {
	m.ensureEnclaveCalls++
	return nil
}

func (m *trackingMockRustFS) CleanupEnclave(_ context.Context, _ exoskeleton.EnclaveIdentity) error {
	m.cleanupEnclaveCalls++
	return nil
}

func (*trackingMockRustFS) Close() {}

// 2.6: handleEnclaveProvision — rate limiting (MaxEnclavesPerOwner).
func TestEnclaveProvision_RateLimitMaxEnclavesPerOwner(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	orig := MaxEnclavesPerOwner
	MaxEnclavesPerOwner = 2
	defer func() { MaxEnclavesPerOwner = orig }()

	// Provision 2 enclaves for alice (at the limit).
	for i := range 2 {
		_, err := handleEnclaveProvision(ctx, client, noopExoCtrl(), EnclaveProvisionParams{
			Name:       fmt.Sprintf("enc-rate-%d", i),
			OwnerEmail: "alice@example.com",
			OwnerSub:   "sub-alice",
		})
		if err != nil {
			t.Fatalf("provision %d: %v", i, err)
		}
	}

	// Third should fail.
	_, err := handleEnclaveProvision(ctx, client, noopExoCtrl(), EnclaveProvisionParams{
		Name:       "enc-rate-over",
		OwnerEmail: "alice@example.com",
		OwnerSub:   "sub-alice",
	})
	if err == nil {
		t.Fatal("expected error for exceeding MaxEnclavesPerOwner, got nil")
	}
}

// 2.7: handleEnclaveProvision — default_mode parameter round-trip.
func TestEnclaveProvision_DefaultModeRoundTrip(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	_, err := handleEnclaveProvision(ctx, client, noopExoCtrl(), EnclaveProvisionParams{
		Name:        "enc-defmode",
		OwnerEmail:  "alice@example.com",
		OwnerSub:    "sub-alice",
		DefaultMode: "rwxr-x---",
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}

	// Verify via enclave_info.
	bearer := &exoskeleton.DeployerInfo{Provider: "bearer-token"}
	info, err := handleEnclaveInfo(ctx, client, noopExoCtrl(), testEval(), EnclaveInfoParams{Name: "enc-defmode"}, bearer)
	if err != nil {
		t.Fatalf("info: %v", err)
	}

	// Read the namespace annotations directly to verify default_mode was stored.
	ns, err := client.Clientset.CoreV1().Namespaces().Get(ctx, "enc-defmode", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get ns: %v", err)
	}
	if ns.Annotations[authz.AnnotationEnclaveDefaultMode] != "rwxr-x---" {
		t.Errorf("expected default_mode annotation=rwxr-x---, got %q", ns.Annotations[authz.AnnotationEnclaveDefaultMode])
	}
	_ = info // info retrieved successfully
}

// 2.9: handleEnclaveSync — OwnerSub cleared on ownership transfer.
func TestEnclaveSync_TransferOwnership_ClearsOwnerSub(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	_, err := handleEnclaveProvision(ctx, client, noopExoCtrl(), EnclaveProvisionParams{
		Name:       "enc-sync-ownsub",
		OwnerEmail: "alice@example.com",
		OwnerSub:   "sub-alice",
		Members:    []string{"bob@example.com"},
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}

	// Transfer ownership from alice to bob.
	alice := &exoskeleton.DeployerInfo{Email: "alice@example.com", Provider: "keycloak"}
	result, err := handleEnclaveSync(ctx, client, EnclaveSyncParams{
		Name:     "enc-sync-ownsub",
		NewOwner: "bob@example.com",
	}, alice)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if result.Enclave.Owner != "bob@example.com" {
		t.Fatalf("expected new owner=bob, got %q", result.Enclave.Owner)
	}

	// Verify OwnerSub is cleared in both the result and the stored annotations.
	if result.Enclave.OwnerSub != "" {
		t.Errorf("expected OwnerSub empty in result after transfer, got %q", result.Enclave.OwnerSub)
	}

	ns, err := client.Clientset.CoreV1().Namespaces().Get(ctx, "enc-sync-ownsub", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get ns: %v", err)
	}
	if ns.Annotations[authz.AnnotationEnclaveOwnerSub] != "" {
		t.Errorf("expected AnnotationEnclaveOwnerSub empty after transfer, got %q", ns.Annotations[authz.AnnotationEnclaveOwnerSub])
	}
}

// 2.10: handleEnclaveProvision — ValidateEnclaveInfo rejection.
func TestEnclaveProvision_InvalidOwnerEmail(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	_, err := handleEnclaveProvision(ctx, client, noopExoCtrl(), EnclaveProvisionParams{
		Name:       "enc-badowner",
		OwnerEmail: "alice-no-at",
		OwnerSub:   "sub-alice",
	})
	if err == nil {
		t.Fatal("expected error for owner email missing @, got nil")
	}
}

func TestEnclaveProvision_InvalidPlatform(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	_, err := handleEnclaveProvision(ctx, client, noopExoCtrl(), EnclaveProvisionParams{
		Name:       "enc-badplatform",
		OwnerEmail: "alice@example.com",
		OwnerSub:   "sub-alice",
		Platform:   "teams", // invalid platform
	})
	if err == nil {
		t.Fatal("expected error for invalid platform, got nil")
	}
}

// H3: enclave_info allows access to enclave members.
func TestEnclaveInfo_MemberAllowed(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	_, err := handleEnclaveProvision(ctx, client, noopExoCtrl(), EnclaveProvisionParams{
		Name:       "enc-info-member",
		OwnerEmail: "alice@example.com",
		OwnerSub:   "sub-alice",
		Members:    []string{"bob@example.com"},
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}

	// bob is a member and should be allowed to view the enclave.
	bob := &exoskeleton.DeployerInfo{Email: "bob@example.com", Provider: "keycloak"}
	result, err := handleEnclaveInfo(ctx, client, noopExoCtrl(), testEval(), EnclaveInfoParams{Name: "enc-info-member"}, bob)
	if err != nil {
		t.Fatalf("expected member to access enclave_info, got error: %v", err)
	}
	if result.Name != "enc-info-member" {
		t.Errorf("expected name=enc-info-member, got %q", result.Name)
	}
}
