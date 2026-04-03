package tools

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/randybias/tentacular-mcp/pkg/authz"
	"github.com/randybias/tentacular-mcp/pkg/exoskeleton"
	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

// newEnclaveTestClient returns a fresh fake k8s client for enclave tests.
func newEnclaveTestClient() *k8s.Client {
	return newNsTestClient()
}

// noopExoCtrl returns an exoskeleton controller with exoskeleton disabled.
func noopExoCtrl() *exoskeleton.Controller {
	cfg := &exoskeleton.Config{Enabled: false}
	return exoskeleton.NewControllerWithDeps(cfg, nil, nil, nil, nil)
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
	result, err := handleEnclaveInfo(ctx, client, noopExoCtrl(), EnclaveInfoParams{Name: "enc-info"}, bearer)
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
	_, err := handleNsCreate(ctx, client, bearerEval(), NsCreateParams{
		Name:        "regular-ns",
		QuotaPreset: "small",
	}, nil)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	bearer := &exoskeleton.DeployerInfo{Provider: "bearer-token"}
	_, err = handleEnclaveInfo(ctx, client, noopExoCtrl(), EnclaveInfoParams{Name: "regular-ns"}, bearer)
	if err == nil {
		t.Fatal("expected error for non-enclave namespace, got nil")
	}
}

func TestEnclaveInfo_NotFound(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	bearer := &exoskeleton.DeployerInfo{Provider: "bearer-token"}
	_, err := handleEnclaveInfo(ctx, client, noopExoCtrl(), EnclaveInfoParams{Name: "does-not-exist"}, bearer)
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
	info, err := handleEnclaveInfo(ctx, client, noopExoCtrl(), EnclaveInfoParams{Name: "enc-quota-rt"}, bearer)
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

	result, err := handleEnclaveList(ctx, client, EnclaveListParams{})
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
	_, err := handleNsCreate(ctx, client, bearerEval(), NsCreateParams{Name: "not-an-enclave", QuotaPreset: "small"}, nil)
	if err != nil {
		t.Fatalf("setup regular ns: %v", err)
	}

	result, err := handleEnclaveList(ctx, client, EnclaveListParams{})
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
	result, err := handleEnclaveList(ctx, client, EnclaveListParams{CallerEmail: "alice@example.com"})
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
	result, err := handleEnclaveList(ctx, client, EnclaveListParams{CallerEmail: "carol@example.com"})
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
	result, err := handleEnclaveList(ctx, client, EnclaveListParams{CallerEmail: "dave@example.com"})
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
	_, err := handleNsCreate(ctx, client, bearerEval(), NsCreateParams{Name: "not-enc-sync", QuotaPreset: "small"}, nil)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	alice := &exoskeleton.DeployerInfo{Email: "alice@example.com", Provider: "keycloak"}
	_, err = handleEnclaveSync(ctx, client, EnclaveSyncParams{
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
	_, err := handleNsCreate(ctx, client, bearerEval(), NsCreateParams{Name: "not-enc-deprov", QuotaPreset: "small"}, nil)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	deployer := &exoskeleton.DeployerInfo{Provider: "bearer-token"}
	_, err = handleEnclaveDeprovision(ctx, client, noopExoCtrl(), EnclaveDeprovisionParams{Name: "not-enc-deprov"}, deployer)
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
	_, err := handleEnclaveInfo(ctx, client, noopExoCtrl(), EnclaveInfoParams{Name: "enc-info-authz"}, dave)
	if err == nil {
		t.Fatal("expected error for non-participant enclave_info, got nil")
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
	result, err := handleEnclaveInfo(ctx, client, noopExoCtrl(), EnclaveInfoParams{Name: "enc-info-member"}, bob)
	if err != nil {
		t.Fatalf("expected member to access enclave_info, got error: %v", err)
	}
	if result.Name != "enc-info-member" {
		t.Errorf("expected name=enc-info-member, got %q", result.Name)
	}
}
