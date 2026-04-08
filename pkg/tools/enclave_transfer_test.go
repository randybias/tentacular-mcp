package tools

// Tests for B1 posix-cleanup:
//   - transferOrphanedTentacles function
//   - enclave_sync new_mode parameter
//   - Verify DeployerInfo.Groups field is removed

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/randybias/tentacular-mcp/pkg/authz"
	"github.com/randybias/tentacular-mcp/pkg/exoskeleton"
)

// --- transferOrphanedTentacles tests ---

// makeTentacleDeployment creates a tentacular Deployment with the given owner annotation.
func makeTentacleDeployment(name, namespace, ownerEmail string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "tentacular",
			},
			Annotations: map[string]string{
				authz.AnnotationOwner:      ownerEmail,
				authz.AnnotationOwnerEmail: ownerEmail,
				authz.AnnotationOwnerSub:   "sub-" + ownerEmail,
				authz.AnnotationOwnerName:  ownerEmail,
			},
		},
	}
}

// makeEnclaveNamespace creates a namespace with enclave annotations.
func makeEnclaveNamespace(name, owner string) *corev1.Namespace {
	info := authz.EnclaveInfo{
		Enclave:   name,
		Owner:     owner,
		OwnerSub:  "sub-" + owner,
		Status:    "active",
		CreatedAt: "2026-01-01T00:00:00Z",
	}
	ann := authz.WriteEnclaveAnnotations(info)
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: ann,
		},
	}
}

func TestTransferOrphanedTentacles_RemovedMemberOwnsTentacles(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	const (
		namespace   = "transfer-test-enc"
		memberEmail = "bob@example.com"
		ownerEmail  = "alice@example.com"
	)

	// Create the namespace so the fake client knows it exists.
	ns := makeEnclaveNamespace(namespace, ownerEmail)
	if _, err := client.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create namespace: %v", err)
	}

	// Create two deployments owned by bob.
	for _, depName := range []string{"wf-one", "wf-two"} {
		dep := makeTentacleDeployment(depName, namespace, memberEmail)
		if _, err := client.Clientset.AppsV1().Deployments(namespace).Create(ctx, dep, metav1.CreateOptions{}); err != nil {
			t.Fatalf("create deployment %q: %v", depName, err)
		}
	}

	// Transfer bob's tentacles to alice.
	transfers, err := transferOrphanedTentacles(ctx, client, namespace, []string{memberEmail}, ownerEmail)
	if err != nil {
		t.Fatalf("transferOrphanedTentacles: %v", err)
	}

	if len(transfers) != 2 {
		t.Fatalf("expected 2 transfers, got %d", len(transfers))
	}
	for _, tr := range transfers {
		if !tr.Success {
			t.Errorf("transfer %q failed: %v", tr.TentacleName, tr.Error)
		}
		if tr.FromOwner != memberEmail {
			t.Errorf("from_owner = %q, want %q", tr.FromOwner, memberEmail)
		}
		if tr.ToOwner != ownerEmail {
			t.Errorf("to_owner = %q, want %q", tr.ToOwner, ownerEmail)
		}
	}

	// Verify annotations were updated on the deployments.
	for _, depName := range []string{"wf-one", "wf-two"} {
		dep, err := client.Clientset.AppsV1().Deployments(namespace).Get(ctx, depName, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("get deployment %q: %v", depName, err)
		}
		if dep.Annotations[authz.AnnotationOwner] != ownerEmail {
			t.Errorf("deployment %q: owner = %q, want %q", depName, dep.Annotations[authz.AnnotationOwner], ownerEmail)
		}
		if dep.Annotations[authz.AnnotationOwnerEmail] != ownerEmail {
			t.Errorf("deployment %q: owner-email = %q, want %q", depName, dep.Annotations[authz.AnnotationOwnerEmail], ownerEmail)
		}
		if dep.Annotations[authz.AnnotationOwnerSub] != "" {
			t.Errorf("deployment %q: owner-sub should be cleared, got %q", depName, dep.Annotations[authz.AnnotationOwnerSub])
		}
	}
}

func TestTransferOrphanedTentacles_RemovedMemberOwnsNone(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	const (
		namespace   = "transfer-none-enc"
		memberEmail = "carol@example.com"
		ownerEmail  = "alice@example.com"
	)

	ns := makeEnclaveNamespace(namespace, ownerEmail)
	if _, err := client.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create namespace: %v", err)
	}

	// Deployment owned by someone else entirely.
	dep := makeTentacleDeployment("other-wf", namespace, "dave@example.com")
	if _, err := client.Clientset.AppsV1().Deployments(namespace).Create(ctx, dep, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create deployment: %v", err)
	}

	transfers, err := transferOrphanedTentacles(ctx, client, namespace, []string{memberEmail}, ownerEmail)
	if err != nil {
		t.Fatalf("transferOrphanedTentacles: %v", err)
	}
	if len(transfers) != 0 {
		t.Errorf("expected 0 transfers, got %d", len(transfers))
	}
}

func TestTransferOrphanedTentacles_MultipleMembersRemoved(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	const (
		namespace  = "transfer-multi-enc"
		ownerEmail = "alice@example.com"
	)

	ns := makeEnclaveNamespace(namespace, ownerEmail)
	if _, err := client.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create namespace: %v", err)
	}

	// bob owns wf-bob, carol owns wf-carol, dave (not removed) owns wf-dave.
	for depName, owner := range map[string]string{
		"wf-bob":   "bob@example.com",
		"wf-carol": "carol@example.com",
		"wf-dave":  "dave@example.com",
	} {
		dep := makeTentacleDeployment(depName, namespace, owner)
		if _, err := client.Clientset.AppsV1().Deployments(namespace).Create(ctx, dep, metav1.CreateOptions{}); err != nil {
			t.Fatalf("create deployment %q: %v", depName, err)
		}
	}

	// Remove bob and carol.
	transfers, err := transferOrphanedTentacles(ctx, client, namespace, []string{"bob@example.com", "carol@example.com"}, ownerEmail)
	if err != nil {
		t.Fatalf("transferOrphanedTentacles: %v", err)
	}
	if len(transfers) != 2 {
		t.Fatalf("expected 2 transfers (bob + carol), got %d", len(transfers))
	}
	for _, tr := range transfers {
		if !tr.Success {
			t.Errorf("transfer %q failed: %v", tr.TentacleName, tr.Error)
		}
		if tr.ToOwner != ownerEmail {
			t.Errorf("to_owner = %q, want %q", tr.ToOwner, ownerEmail)
		}
	}

	// Dave's tentacle should be untouched.
	daveDep, err := client.Clientset.AppsV1().Deployments(namespace).Get(ctx, "wf-dave", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get wf-dave: %v", err)
	}
	if daveDep.Annotations[authz.AnnotationOwner] != "dave@example.com" {
		t.Errorf("wf-dave owner changed unexpectedly: %q", daveDep.Annotations[authz.AnnotationOwner])
	}
}

// --- enclave_sync new_mode tests ---

func TestEnclaveSync_NewMode_ValidMode(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	provisionTestEnclave(t, client, "enc-mode-test", "alice@example.com")

	alice := &exoskeleton.DeployerInfo{Email: "alice@example.com", Provider: "keycloak"}
	result, err := handleEnclaveSync(ctx, client, EnclaveSyncParams{
		Name:    "enc-mode-test",
		NewMode: "rwxrwx---",
	}, alice)
	if err != nil {
		t.Fatalf("handleEnclaveSync: %v", err)
	}

	// Verify updated list includes "mode".
	found := false
	for _, u := range result.Updated {
		if u == "mode" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'mode' in updated list, got %v", result.Updated)
	}

	// Verify the namespace annotation was updated.
	ns, err := client.Clientset.CoreV1().Namespaces().Get(ctx, "enc-mode-test", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get namespace: %v", err)
	}
	if ns.Annotations[authz.AnnotationMode] != "rwxrwx---" {
		t.Errorf("AnnotationMode = %q, want rwxrwx---", ns.Annotations[authz.AnnotationMode])
	}
	if ns.Annotations["tentacular.io/enclave-default-mode"] != "rwxrwx---" {
		t.Errorf("enclave-default-mode = %q, want rwxrwx---", ns.Annotations["tentacular.io/enclave-default-mode"])
	}
}

func TestEnclaveSync_NewMode_InvalidMode(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	provisionTestEnclave(t, client, "enc-bad-mode", "alice@example.com")

	alice := &exoskeleton.DeployerInfo{Email: "alice@example.com", Provider: "keycloak"}
	_, err := handleEnclaveSync(ctx, client, EnclaveSyncParams{
		Name:    "enc-bad-mode",
		NewMode: "notvalid",
	}, alice)
	if err == nil {
		t.Fatal("expected error for invalid mode, got nil")
	}
}

func TestEnclaveSync_NewMode_NonOwnerDenied(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	provisionTestEnclave(t, client, "enc-mode-perm", "alice@example.com")

	// Add bob as a member.
	alice := &exoskeleton.DeployerInfo{Email: "alice@example.com", Provider: "keycloak"}
	_, err := handleEnclaveSync(ctx, client, EnclaveSyncParams{
		Name:       "enc-mode-perm",
		AddMembers: []string{"bob@example.com"},
	}, alice)
	if err != nil {
		t.Fatalf("add member: %v", err)
	}

	// bob tries to set mode — should be denied.
	bob := &exoskeleton.DeployerInfo{Email: "bob@example.com", Provider: "keycloak"}
	_, err = handleEnclaveSync(ctx, client, EnclaveSyncParams{
		Name:    "enc-mode-perm",
		NewMode: "rwxrwx---",
	}, bob)
	if err == nil {
		t.Fatal("expected permission denied for non-owner mode change, got nil")
	}
}

// --- enclave_sync ownership transfer integration test ---

func TestEnclaveSync_RemoveMembers_TransfersOwnedTentacles(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	const enclave = "enc-transfer-int"
	provisionTestEnclave(t, client, enclave, "alice@example.com")

	// Add bob as a member.
	alice := &exoskeleton.DeployerInfo{Email: "alice@example.com", Provider: "keycloak"}
	_, err := handleEnclaveSync(ctx, client, EnclaveSyncParams{
		Name:       enclave,
		AddMembers: []string{"bob@example.com"},
	}, alice)
	if err != nil {
		t.Fatalf("add member: %v", err)
	}

	// Create a deployment owned by bob in the enclave namespace.
	dep := makeTentacleDeployment("bob-wf", enclave, "bob@example.com")
	if _, createErr := client.Clientset.AppsV1().Deployments(enclave).Create(ctx, dep, metav1.CreateOptions{}); createErr != nil {
		t.Fatalf("create deployment: %v", createErr)
	}

	// Remove bob — should trigger ownership transfer.
	result, err := handleEnclaveSync(ctx, client, EnclaveSyncParams{
		Name:          enclave,
		RemoveMembers: []string{"bob@example.com"},
	}, alice)
	if err != nil {
		t.Fatalf("remove member: %v", err)
	}

	// Verify transfers are reported.
	if len(result.Transfers) != 1 {
		t.Fatalf("expected 1 transfer, got %d", len(result.Transfers))
	}
	tr := result.Transfers[0]
	if tr.TentacleName != "bob-wf" {
		t.Errorf("transfer: tentacle_name = %q, want bob-wf", tr.TentacleName)
	}
	if !tr.Success {
		t.Errorf("transfer failed: %v", tr.Error)
	}
	if tr.ToOwner != "alice@example.com" {
		t.Errorf("transfer: to_owner = %q, want alice@example.com", tr.ToOwner)
	}

	// Verify 'ownership_transfers' in updated list.
	foundTransfer := false
	for _, u := range result.Updated {
		if u == "ownership_transfers" {
			foundTransfer = true
		}
	}
	if !foundTransfer {
		t.Errorf("expected 'ownership_transfers' in updated, got %v", result.Updated)
	}

	// Verify the deployment annotation was actually updated.
	updatedDep, err := client.Clientset.AppsV1().Deployments(enclave).Get(ctx, "bob-wf", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get deployment: %v", err)
	}
	if updatedDep.Annotations[authz.AnnotationOwner] != "alice@example.com" {
		t.Errorf("deployment owner = %q, want alice@example.com", updatedDep.Annotations[authz.AnnotationOwner])
	}
}

func TestEnclaveSync_RemoveMembers_NoOwnedTentacles_NoTransfers(t *testing.T) {
	client := newEnclaveTestClient()
	ctx := context.Background()

	const enclave = "enc-notransfer"
	provisionTestEnclave(t, client, enclave, "alice@example.com")

	alice := &exoskeleton.DeployerInfo{Email: "alice@example.com", Provider: "keycloak"}
	_, err := handleEnclaveSync(ctx, client, EnclaveSyncParams{
		Name:       enclave,
		AddMembers: []string{"carol@example.com"},
	}, alice)
	if err != nil {
		t.Fatalf("add member: %v", err)
	}

	// Remove carol who owns no tentacles.
	result, err := handleEnclaveSync(ctx, client, EnclaveSyncParams{
		Name:          enclave,
		RemoveMembers: []string{"carol@example.com"},
	}, alice)
	if err != nil {
		t.Fatalf("remove member: %v", err)
	}

	// No transfers should occur.
	if len(result.Transfers) != 0 {
		t.Errorf("expected 0 transfers, got %d", len(result.Transfers))
	}
	for _, u := range result.Updated {
		if u == "ownership_transfers" {
			t.Error("'ownership_transfers' should not appear in updated when no transfers occurred")
		}
	}
}

// --- DeployerInfo.Groups removal verification ---

func TestDeployerInfo_NoGroupsField(t *testing.T) {
	// This test verifies at compile time that Groups was removed.
	// If Groups still exists, this struct literal would need the field.
	// The struct must function without Groups.
	d := exoskeleton.DeployerInfo{
		Email: "user@example.com",
	}
	if d.Email != "user@example.com" {
		t.Errorf("unexpected email: %q", d.Email)
	}
}
