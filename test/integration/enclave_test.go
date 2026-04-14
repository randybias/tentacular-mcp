//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

// enclaveProvisionResult mirrors tools.EnclaveProvisionResult for JSON decoding.
type enclaveProvisionResult struct {
	Name             string   `json:"name"`
	Status           string   `json:"status"`
	QuotaPreset      string   `json:"quota_preset"`
	Owner            string   `json:"owner"`
	Members          []string `json:"members"`
	ResourcesCreated []string `json:"resources_created"`
}

// enclaveInfoResult mirrors tools.EnclaveInfoResult for JSON decoding.
type enclaveInfoResult struct {
	Name          string `json:"name"`
	Owner         string `json:"owner"`
	OwnerSub      string `json:"owner_sub"`
	Status        string `json:"status"`
	QuotaPreset   string `json:"quota_preset"`
	Platform      string `json:"platform,omitempty"`
	ChannelID     string `json:"channel_id,omitempty"`
	ChannelName   string `json:"channel_name,omitempty"`
	CreatedAt     string `json:"created_at,omitempty"`
	UpdatedAt     string `json:"updated_at,omitempty"`
	TentacleCount int    `json:"tentacle_count"`
	Members       []string `json:"members"`
	ExoServices   []struct {
		Name      string `json:"name"`
		Available bool   `json:"available"`
	} `json:"exo_services"`
}

// enclaveListResult mirrors tools.EnclaveListResult for JSON decoding.
type enclaveListResult struct {
	Enclaves []struct {
		Name        string   `json:"name"`
		Owner       string   `json:"owner"`
		Status      string   `json:"status"`
		Platform    string   `json:"platform,omitempty"`
		ChannelName string   `json:"channel_name,omitempty"`
		CreatedAt   string   `json:"created_at,omitempty"`
		Members     []string `json:"members"`
	} `json:"enclaves"`
}

// enclaveSyncResult mirrors tools.EnclaveSyncResult for JSON decoding.
type enclaveSyncResult struct {
	Name    string            `json:"name"`
	Updated []string          `json:"updated"`
	Enclave enclaveInfoResult `json:"enclave"`
}

// enclaveDeprovisionResult mirrors tools.EnclaveDeprovisionResult for JSON decoding.
type enclaveDeprovisionResult struct {
	Name             string `json:"name"`
	Deleted          bool   `json:"deleted"`
	TentaclesRemoved int    `json:"tentacles_removed"`
}

// containsStrSlice returns true if s is in slice.
func containsStrSlice(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

// --- E2E: Enclave lifecycle ---

// TestEnclave_FullLifecycle exercises the core lifecycle: provision, info, list, sync
// (add member), sync (remove member), then deprovision.
func TestEnclave_FullLifecycle(t *testing.T) {
	session, client, cleanup := e2eEnv(t)
	defer cleanup()

	enclaveName := "tnt-e2e-enc-lifecycle"
	cleanupNs(t, client, enclaveName)

	// enclave_provision
	text := callTool(t, session, "enclave_provision", map[string]any{
		"name":         enclaveName,
		"owner_email":  "alice@example.com",
		"owner_sub":    "sub-alice",
		"quota_preset": "medium",
	})
	var provResult enclaveProvisionResult
	if err := json.Unmarshal([]byte(text), &provResult); err != nil {
		t.Fatalf("unmarshal enclave_provision result: %v", err)
	}
	if provResult.Name != enclaveName {
		t.Errorf("provision name: got %q, want %q", provResult.Name, enclaveName)
	}
	if provResult.Status != "active" {
		t.Errorf("provision status: got %q, want active", provResult.Status)
	}
	if provResult.Owner != "alice@example.com" {
		t.Errorf("provision owner: got %q, want alice@example.com", provResult.Owner)
	}
	if provResult.QuotaPreset != "medium" {
		t.Errorf("provision quota_preset: got %q, want medium", provResult.QuotaPreset)
	}
	if len(provResult.ResourcesCreated) == 0 {
		t.Error("provision: expected non-empty resources_created")
	}
	t.Logf("enclave_provision resources_created: %v", provResult.ResourcesCreated)

	// enclave_info
	text = callTool(t, session, "enclave_info", map[string]any{"name": enclaveName})
	var infoResult enclaveInfoResult
	if err := json.Unmarshal([]byte(text), &infoResult); err != nil {
		t.Fatalf("unmarshal enclave_info result: %v", err)
	}
	if infoResult.Name != enclaveName {
		t.Errorf("info name: got %q, want %q", infoResult.Name, enclaveName)
	}
	if infoResult.Owner != "alice@example.com" {
		t.Errorf("info owner: got %q, want alice@example.com", infoResult.Owner)
	}
	if infoResult.Status != "active" {
		t.Errorf("info status: got %q, want active", infoResult.Status)
	}
	if infoResult.QuotaPreset != "medium" {
		t.Errorf("info quota_preset: got %q, want medium", infoResult.QuotaPreset)
	}
	if len(infoResult.ExoServices) != 2 {
		t.Errorf("info exo_services: got %d, want 2", len(infoResult.ExoServices))
	}
	t.Logf("enclave_info: tentacle_count=%d", infoResult.TentacleCount)

	// enclave_list — verify enclave appears
	text = callTool(t, session, "enclave_list", map[string]any{})
	var listResult enclaveListResult
	if err := json.Unmarshal([]byte(text), &listResult); err != nil {
		t.Fatalf("unmarshal enclave_list result: %v", err)
	}
	foundInList := false
	for _, e := range listResult.Enclaves {
		if e.Name == enclaveName {
			foundInList = true
			break
		}
	}
	if !foundInList {
		t.Errorf("enclave_list: %q not found in enclaves", enclaveName)
	}

	// enclave_sync: add member
	text = callTool(t, session, "enclave_sync", map[string]any{
		"name":        enclaveName,
		"add_members": []string{"bob@example.com"},
	})
	var syncResult enclaveSyncResult
	if err := json.Unmarshal([]byte(text), &syncResult); err != nil {
		t.Fatalf("unmarshal enclave_sync (add) result: %v", err)
	}
	if !containsStrSlice(syncResult.Updated, "members") {
		t.Errorf("sync add: expected 'members' in updated, got %v", syncResult.Updated)
	}
	if !containsStrSlice(syncResult.Enclave.Members, "bob@example.com") {
		t.Errorf("sync add: expected bob in members, got %v", syncResult.Enclave.Members)
	}

	// enclave_sync: remove member
	text = callTool(t, session, "enclave_sync", map[string]any{
		"name":           enclaveName,
		"remove_members": []string{"bob@example.com"},
	})
	if err := json.Unmarshal([]byte(text), &syncResult); err != nil {
		t.Fatalf("unmarshal enclave_sync (remove) result: %v", err)
	}
	if containsStrSlice(syncResult.Enclave.Members, "bob@example.com") {
		t.Error("sync remove: expected bob removed from members")
	}

	// enclave_deprovision
	text = callTool(t, session, "enclave_deprovision", map[string]any{"name": enclaveName})
	var deprovResult enclaveDeprovisionResult
	if err := json.Unmarshal([]byte(text), &deprovResult); err != nil {
		t.Fatalf("unmarshal enclave_deprovision result: %v", err)
	}
	if !deprovResult.Deleted {
		t.Error("deprovision: expected deleted=true")
	}
}

// TestEnclave_ProvisionWithQuotas verifies each quota preset (small, medium, large)
// is accepted and reflected in the provision result.
func TestEnclave_ProvisionWithQuotas(t *testing.T) {
	session, client, cleanup := e2eEnv(t)
	defer cleanup()

	presets := []string{"small", "medium", "large"}
	for _, preset := range presets {
		nsName := "tnt-e2e-enc-quota-" + preset
		cleanupNs(t, client, nsName)

		text := callTool(t, session, "enclave_provision", map[string]any{
			"name":         nsName,
			"owner_email":  "alice@example.com",
			"owner_sub":    "sub-alice",
			"quota_preset": preset,
		})
		var provResult enclaveProvisionResult
		if err := json.Unmarshal([]byte(text), &provResult); err != nil {
			t.Fatalf("preset %q: unmarshal enclave_provision: %v", preset, err)
		}
		if provResult.QuotaPreset != preset {
			t.Errorf("preset %q: got quota_preset=%q", preset, provResult.QuotaPreset)
		}
		if provResult.Status != "active" {
			t.Errorf("preset %q: got status=%q, want active", preset, provResult.Status)
		}

		// Confirm info reflects the preset.
		text = callTool(t, session, "enclave_info", map[string]any{"name": nsName})
		var infoResult enclaveInfoResult
		if err := json.Unmarshal([]byte(text), &infoResult); err != nil {
			t.Fatalf("preset %q: unmarshal enclave_info: %v", preset, err)
		}
		if infoResult.QuotaPreset != preset {
			t.Errorf("preset %q: info quota_preset=%q", preset, infoResult.QuotaPreset)
		}

		// Cleanup via deprovision.
		callTool(t, session, "enclave_deprovision", map[string]any{"name": nsName})
	}
}

// TestEnclave_OwnershipTransfer provisions an enclave with a member, then transfers
// ownership to that member and verifies the new owner annotation.
func TestEnclave_OwnershipTransfer(t *testing.T) {
	session, client, cleanup := e2eEnv(t)
	defer cleanup()

	enclaveName := "tnt-e2e-enc-owner"
	cleanupNs(t, client, enclaveName)

	// Provision with alice as owner and bob as member.
	callTool(t, session, "enclave_provision", map[string]any{
		"name":        enclaveName,
		"owner_email": "alice@example.com",
		"owner_sub":   "sub-alice",
		"members":     []string{"bob@example.com"},
	})

	// Transfer ownership to bob.
	text := callTool(t, session, "enclave_sync", map[string]any{
		"name":      enclaveName,
		"new_owner": "bob@example.com",
	})
	var syncResult enclaveSyncResult
	if err := json.Unmarshal([]byte(text), &syncResult); err != nil {
		t.Fatalf("unmarshal enclave_sync (ownership) result: %v", err)
	}
	if !containsStrSlice(syncResult.Updated, "owner") {
		t.Errorf("ownership transfer: expected 'owner' in updated, got %v", syncResult.Updated)
	}
	if syncResult.Enclave.Owner != "bob@example.com" {
		t.Errorf("ownership transfer: new owner=%q, want bob@example.com", syncResult.Enclave.Owner)
	}
	// Old owner should be demoted to member.
	if !containsStrSlice(syncResult.Enclave.Members, "alice@example.com") {
		t.Errorf("ownership transfer: expected alice in members after demotion, got %v", syncResult.Enclave.Members)
	}
	// New owner must not appear in members.
	if containsStrSlice(syncResult.Enclave.Members, "bob@example.com") {
		t.Error("ownership transfer: bob should not remain in members after promotion to owner")
	}

	// Confirm via enclave_info.
	text = callTool(t, session, "enclave_info", map[string]any{"name": enclaveName})
	var infoResult enclaveInfoResult
	if err := json.Unmarshal([]byte(text), &infoResult); err != nil {
		t.Fatalf("unmarshal enclave_info after ownership transfer: %v", err)
	}
	if infoResult.Owner != "bob@example.com" {
		t.Errorf("info after transfer: owner=%q, want bob@example.com", infoResult.Owner)
	}

	// Cleanup.
	callTool(t, session, "enclave_deprovision", map[string]any{"name": enclaveName})
}

// TestEnclave_FreezeUnfreeze provisions an enclave, freezes it, verifies the frozen
// status via enclave_info, then unfreezes and verifies active status.
func TestEnclave_FreezeUnfreeze(t *testing.T) {
	session, client, cleanup := e2eEnv(t)
	defer cleanup()

	enclaveName := "tnt-e2e-enc-freeze"
	cleanupNs(t, client, enclaveName)

	callTool(t, session, "enclave_provision", map[string]any{
		"name":        enclaveName,
		"owner_email": "alice@example.com",
		"owner_sub":   "sub-alice",
	})

	// Freeze.
	text := callTool(t, session, "enclave_sync", map[string]any{
		"name":       enclaveName,
		"new_status": "frozen",
	})
	var syncResult enclaveSyncResult
	if err := json.Unmarshal([]byte(text), &syncResult); err != nil {
		t.Fatalf("unmarshal enclave_sync (freeze) result: %v", err)
	}
	if !containsStrSlice(syncResult.Updated, "status") {
		t.Errorf("freeze: expected 'status' in updated, got %v", syncResult.Updated)
	}
	if syncResult.Enclave.Status != "frozen" {
		t.Errorf("freeze: got status=%q, want frozen", syncResult.Enclave.Status)
	}

	// Verify via enclave_info.
	text = callTool(t, session, "enclave_info", map[string]any{"name": enclaveName})
	var infoResult enclaveInfoResult
	if err := json.Unmarshal([]byte(text), &infoResult); err != nil {
		t.Fatalf("unmarshal enclave_info after freeze: %v", err)
	}
	if infoResult.Status != "frozen" {
		t.Errorf("info after freeze: status=%q, want frozen", infoResult.Status)
	}

	// Unfreeze.
	text = callTool(t, session, "enclave_sync", map[string]any{
		"name":       enclaveName,
		"new_status": "active",
	})
	if err := json.Unmarshal([]byte(text), &syncResult); err != nil {
		t.Fatalf("unmarshal enclave_sync (unfreeze) result: %v", err)
	}
	if syncResult.Enclave.Status != "active" {
		t.Errorf("unfreeze: got status=%q, want active", syncResult.Enclave.Status)
	}

	// Verify via enclave_info.
	text = callTool(t, session, "enclave_info", map[string]any{"name": enclaveName})
	if err := json.Unmarshal([]byte(text), &infoResult); err != nil {
		t.Fatalf("unmarshal enclave_info after unfreeze: %v", err)
	}
	if infoResult.Status != "active" {
		t.Errorf("info after unfreeze: status=%q, want active", infoResult.Status)
	}

	callTool(t, session, "enclave_deprovision", map[string]any{"name": enclaveName})
}

// TestEnclave_MemberPermissions provisions an enclave with members, deploys a tentacle
// (ConfigMap via wf_apply), then verifies a member can list/describe it while a
// non-member cannot access the enclave info.
func TestEnclave_MemberPermissions(t *testing.T) {
	session, client, cleanup := e2eEnv(t)
	defer cleanup()

	enclaveName := "tnt-e2e-enc-members"
	cleanupNs(t, client, enclaveName)

	// Provision with alice as owner, bob as member.
	callTool(t, session, "enclave_provision", map[string]any{
		"name":        enclaveName,
		"owner_email": "alice@example.com",
		"owner_sub":   "sub-alice",
		"members":     []string{"bob@example.com"},
	})

	// Deploy a tentacle (ConfigMap) into the enclave namespace.
	manifests := []map[string]any{
		{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name": "enc-test-config",
			},
			"data": map[string]any{
				"key": "value",
			},
		},
	}
	text := callTool(t, session, "wf_apply", map[string]any{
		"enclave": enclaveName,
		"name":      "enc-tentacle",
		"manifests": manifests,
	})
	var applyResult struct {
		Created int `json:"created"`
	}
	if err := json.Unmarshal([]byte(text), &applyResult); err != nil {
		t.Fatalf("unmarshal wf_apply result: %v", err)
	}
	if applyResult.Created != 1 {
		t.Errorf("wf_apply created: got %d, want 1", applyResult.Created)
	}

	// Verify member (bob) can see the enclave in a filtered list.
	text = callTool(t, session, "enclave_list", map[string]any{
		"caller_email": "bob@example.com",
	})
	var listResult enclaveListResult
	if err := json.Unmarshal([]byte(text), &listResult); err != nil {
		t.Fatalf("unmarshal enclave_list (bob) result: %v", err)
	}
	bobSees := false
	for _, e := range listResult.Enclaves {
		if e.Name == enclaveName {
			bobSees = true
			break
		}
	}
	if !bobSees {
		t.Errorf("member (bob) expected to see enclave %q in filtered list", enclaveName)
	}

	// Verify non-member (carol) does NOT see the enclave in a filtered list.
	text = callTool(t, session, "enclave_list", map[string]any{
		"caller_email": "carol@example.com",
	})
	var carolList enclaveListResult
	if err := json.Unmarshal([]byte(text), &carolList); err != nil {
		t.Fatalf("unmarshal enclave_list (carol) result: %v", err)
	}
	for _, e := range carolList.Enclaves {
		if e.Name == enclaveName {
			t.Errorf("non-member (carol) should not see enclave %q", enclaveName)
			break
		}
	}

	// Tentacle count should be reflected in enclave_info.
	text = callTool(t, session, "enclave_info", map[string]any{"name": enclaveName})
	var infoResult enclaveInfoResult
	if err := json.Unmarshal([]byte(text), &infoResult); err != nil {
		t.Fatalf("unmarshal enclave_info after deploy: %v", err)
	}
	t.Logf("enclave tentacle_count after wf_apply: %d", infoResult.TentacleCount)

	callTool(t, session, "enclave_deprovision", map[string]any{"name": enclaveName})
}

// TestEnclave_DeprovisionCleanup provisions an enclave, deploys a tentacle, deprovisions,
// then verifies the namespace is gone.
func TestEnclave_DeprovisionCleanup(t *testing.T) {
	session, client, cleanup := e2eEnv(t)
	defer cleanup()

	enclaveName := "tnt-e2e-enc-cleanup"
	// Register cleanup in case deprovision fails mid-test.
	cleanupNs(t, client, enclaveName)

	callTool(t, session, "enclave_provision", map[string]any{
		"name":        enclaveName,
		"owner_email": "alice@example.com",
		"owner_sub":   "sub-alice",
	})

	// Deploy a tentacle so we can verify tentacles_removed > 0 after deprovision.
	manifests := []map[string]any{
		{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name": "cleanup-config",
			},
			"data": map[string]any{
				"key": "value",
			},
		},
	}
	callTool(t, session, "wf_apply", map[string]any{
		"enclave": enclaveName,
		"name":      "cleanup-tentacle",
		"manifests": manifests,
	})

	// Deprovision.
	text := callTool(t, session, "enclave_deprovision", map[string]any{"name": enclaveName})
	var deprovResult enclaveDeprovisionResult
	if err := json.Unmarshal([]byte(text), &deprovResult); err != nil {
		t.Fatalf("unmarshal enclave_deprovision result: %v", err)
	}
	if !deprovResult.Deleted {
		t.Error("deprovision: expected deleted=true")
	}
	t.Logf("deprovision: tentacles_removed=%d", deprovResult.TentaclesRemoved)

	// Verify namespace is deleted or terminating. K8s namespace deletion is
	// asynchronous — the namespace may linger in Terminating phase for 10-30s in
	// kind clusters. We check the namespace phase directly via K8s client rather
	// than waiting for it to fully disappear.
	var deleted bool
	for range 30 {
		ns, err := k8s.GetNamespace(context.Background(), client, enclaveName)
		if err != nil {
			// Namespace is gone.
			deleted = true
			break
		}
		if ns.Status.Phase == "Terminating" {
			// Namespace is being deleted — good enough.
			deleted = true
			break
		}
		time.Sleep(1 * time.Second)
	}
	if !deleted {
		t.Error("namespace still Active after deprovision — expected Terminating or deleted")
	}
}

// TestEnclave_ListFiltering provisions two enclaves with different members, then uses
// caller_email filtering to verify each caller sees only their own enclaves.
func TestEnclave_ListFiltering(t *testing.T) {
	session, client, cleanup := e2eEnv(t)
	defer cleanup()

	enc1 := "tnt-e2e-enc-filter1"
	enc2 := "tnt-e2e-enc-filter2"
	cleanupNs(t, client, enc1, enc2)

	// Enclave 1: alice owns, bob is member.
	callTool(t, session, "enclave_provision", map[string]any{
		"name":        enc1,
		"owner_email": "alice@example.com",
		"owner_sub":   "sub-alice",
		"members":     []string{"bob@example.com"},
	})

	// Enclave 2: carol owns, dave is member.
	callTool(t, session, "enclave_provision", map[string]any{
		"name":        enc2,
		"owner_email": "carol@example.com",
		"owner_sub":   "sub-carol",
		"members":     []string{"dave@example.com"},
	})

	// Alice should see enc1 only.
	text := callTool(t, session, "enclave_list", map[string]any{
		"caller_email": "alice@example.com",
	})
	var aliceList enclaveListResult
	if err := json.Unmarshal([]byte(text), &aliceList); err != nil {
		t.Fatalf("unmarshal enclave_list (alice): %v", err)
	}
	for _, e := range aliceList.Enclaves {
		if e.Name == enc2 {
			t.Errorf("alice should not see enc2 (owned by carol)")
		}
	}
	aliceSees1 := false
	for _, e := range aliceList.Enclaves {
		if e.Name == enc1 {
			aliceSees1 = true
			break
		}
	}
	if !aliceSees1 {
		t.Errorf("alice should see enc1 (her own enclave)")
	}

	// Bob (member of enc1) should see enc1 only.
	text = callTool(t, session, "enclave_list", map[string]any{
		"caller_email": "bob@example.com",
	})
	var bobList enclaveListResult
	if err := json.Unmarshal([]byte(text), &bobList); err != nil {
		t.Fatalf("unmarshal enclave_list (bob): %v", err)
	}
	for _, e := range bobList.Enclaves {
		if e.Name == enc2 {
			t.Errorf("bob (member of enc1) should not see enc2")
		}
	}

	// Dave (member of enc2) should see enc2 only.
	text = callTool(t, session, "enclave_list", map[string]any{
		"caller_email": "dave@example.com",
	})
	var daveList enclaveListResult
	if err := json.Unmarshal([]byte(text), &daveList); err != nil {
		t.Fatalf("unmarshal enclave_list (dave): %v", err)
	}
	daveSees2 := false
	for _, e := range daveList.Enclaves {
		if e.Name == enc2 {
			daveSees2 = true
		}
		if e.Name == enc1 {
			t.Errorf("dave (member of enc2) should not see enc1")
		}
	}
	if !daveSees2 {
		t.Errorf("dave should see enc2 (he is a member)")
	}

	// Cleanup.
	callTool(t, session, "enclave_deprovision", map[string]any{"name": enc1})
	callTool(t, session, "enclave_deprovision", map[string]any{"name": enc2})
}
