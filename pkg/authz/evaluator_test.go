package authz

import (
	"strings"
	"testing"

	"github.com/randybias/tentacular-mcp/pkg/exoskeleton"
)

// helpers

func oidcDeployer(sub, email string, groups ...string) *exoskeleton.DeployerInfo {
	return &exoskeleton.DeployerInfo{
		Subject:  sub,
		Email:    email,
		Provider: "keycloak",
		Groups:   groups,
	}
}

func bearerDeployer() *exoskeleton.DeployerInfo {
	return &exoskeleton.DeployerInfo{
		Provider: "bearer-token",
	}
}

// annsWith creates enclave-style annotations for use in Check/CheckEnclave tests.
// The "group" parameter is interpreted as a single member email for enclave member matching.
// To include multiple members, set members directly via AnnotationEnclaveMembers.
func annsWith(ownerEmail, memberEmail, modeStr string) map[string]string {
	ann := map[string]string{
		AnnotationEnclave:      "test-enclave", // non-empty marks it as an enclave
		AnnotationEnclaveOwner: ownerEmail,
		AnnotationMode:         modeStr,
	}
	if memberEmail != "" {
		ann[AnnotationEnclaveMembers] = memberEmail
	}
	return ann
}

func TestEvaluator_NilEvaluator_AlwaysAllow(t *testing.T) {
	var e *Evaluator
	deployer := oidcDeployer("sub-a", "a@example.com")
	ann := annsWith("other@example.com", "g", "rwx------")

	for _, action := range []Action{Read, Write, Execute} {
		d := e.Check(deployer, ann, action)
		if !d.Allowed {
			t.Errorf("nil evaluator should allow action %v, got deny: %s", action, d.Reason)
		}
	}
}

func TestEvaluator_Disabled_AlwaysAllow(t *testing.T) {
	e := &Evaluator{DefaultMode: DefaultMode, Enabled: false}
	deployer := oidcDeployer("sub-a", "a@example.com")
	ann := annsWith("other@example.com", "g", "rwx------")

	for _, action := range []Action{Read, Write, Execute} {
		d := e.Check(deployer, ann, action)
		if !d.Allowed {
			t.Errorf("disabled evaluator should allow action %v", action)
		}
	}
}

func TestEvaluator_NilDeployer_Deny(t *testing.T) {
	e := NewEvaluator(DefaultMode)
	ann := annsWith("owner@example.com", "", "rwx------")

	d := e.Check(nil, ann, Read)
	if d.Allowed {
		t.Error("expected deny for nil deployer")
	}
}

func TestEvaluator_BearerToken_AlwaysAllow(t *testing.T) {
	e := NewEvaluator(DefaultMode)
	deployer := bearerDeployer()
	// Even with tight permissions, bearer bypasses.
	ann := annsWith("owner@example.com", "", "rwx------")

	for _, action := range []Action{Read, Write, Execute} {
		d := e.Check(deployer, ann, action)
		if !d.Allowed {
			t.Errorf("bearer token should bypass authz for action %v", action)
		}
	}
}

func TestEvaluator_UnownedResource_Deny(t *testing.T) {
	// Unowned resources (no owner annotation) are denied in strict mode.
	// Use bearer-token or admin tools to stamp ownership.
	e := NewEvaluator(DefaultMode)
	deployer := oidcDeployer("sub-a", "a@example.com")
	ann := map[string]string{} // no owner

	for _, action := range []Action{Read, Write, Execute} {
		d := e.Check(deployer, ann, action)
		if d.Allowed {
			t.Errorf("unowned resource should be denied for action %v", action)
		}
	}
}

func TestEvaluator_UnownedResource_BearerToken_Allow(t *testing.T) {
	// Bearer-token callers can still access unowned resources (for admin/adoption).
	e := NewEvaluator(DefaultMode)
	deployer := &exoskeleton.DeployerInfo{Provider: "bearer-token"}
	ann := map[string]string{} // no owner-sub

	for _, action := range []Action{Read, Write, Execute} {
		d := e.Check(deployer, ann, action)
		if !d.Allowed {
			t.Errorf("bearer-token should be allowed on unowned resource for action %v", action)
		}
	}
}

// --- Tentacle owner scope tests ---
//
// CheckEnclave treats the enclave owner as a superuser (bypass all bits).
// Per-tentacle owner permission bit enforcement is done by CheckTentacle.
// These tests use CheckTentacle with a deployer who is the tentacle owner
// but NOT the enclave owner.

func enclaveAnnWith(tentacleOwnerEmail, mode string) (enclave, tentacle map[string]string) { //nolint:unparam
	enclave = map[string]string{
		AnnotationEnclave:        "test-enclave",
		AnnotationEnclaveOwner:   "admin@example.com",
		AnnotationEnclaveMembers: tentacleOwnerEmail, // must pass enclave layer check
		AnnotationMode:           "rwxrwx---",        // members have full access at enclave layer
	}
	tentacle = map[string]string{
		AnnotationOwner: tentacleOwnerEmail,
		AnnotationMode:  mode,
	}
	return enclave, tentacle
}

func TestEvaluator_Owner_Read_OwnerReadSet_Allow(t *testing.T) {
	e := NewEvaluator(DefaultMode)
	encAnn, tentAnn := enclaveAnnWith("a@example.com", "r--------")
	d := e.CheckTentacle(oidcDeployer("sub-owner", "a@example.com"), encAnn, tentAnn, Read)
	if !d.Allowed {
		t.Errorf("tentacle owner with read bit should be allowed: %s", d.Reason)
	}
}

func TestEvaluator_Owner_Read_OwnerReadUnset_Deny(t *testing.T) {
	e := NewEvaluator(DefaultMode)
	encAnn, tentAnn := enclaveAnnWith("a@example.com", "---------")
	d := e.CheckTentacle(oidcDeployer("sub-owner", "a@example.com"), encAnn, tentAnn, Read)
	if d.Allowed {
		t.Error("tentacle owner without read bit should be denied")
	}
}

func TestEvaluator_Owner_Write_OwnerWriteSet_Allow(t *testing.T) {
	e := NewEvaluator(DefaultMode)
	encAnn, tentAnn := enclaveAnnWith("a@example.com", "-w-------")
	d := e.CheckTentacle(oidcDeployer("sub-owner", "a@example.com"), encAnn, tentAnn, Write)
	if !d.Allowed {
		t.Errorf("tentacle owner with write bit should be allowed: %s", d.Reason)
	}
}

func TestEvaluator_Owner_Write_OwnerWriteUnset_Deny(t *testing.T) {
	e := NewEvaluator(DefaultMode)
	encAnn, tentAnn := enclaveAnnWith("a@example.com", "---------")
	d := e.CheckTentacle(oidcDeployer("sub-owner", "a@example.com"), encAnn, tentAnn, Write)
	if d.Allowed {
		t.Error("tentacle owner without write bit should be denied")
	}
}

func TestEvaluator_Owner_Execute_OwnerExecuteSet_Allow(t *testing.T) {
	e := NewEvaluator(DefaultMode)
	encAnn, tentAnn := enclaveAnnWith("a@example.com", "--x------")
	d := e.CheckTentacle(oidcDeployer("sub-owner", "a@example.com"), encAnn, tentAnn, Execute)
	if !d.Allowed {
		t.Errorf("tentacle owner with execute bit should be allowed: %s", d.Reason)
	}
}

func TestEvaluator_Owner_Execute_OwnerExecuteUnset_Deny(t *testing.T) {
	e := NewEvaluator(DefaultMode)
	encAnn, tentAnn := enclaveAnnWith("a@example.com", "---------")
	d := e.CheckTentacle(oidcDeployer("sub-owner", "a@example.com"), encAnn, tentAnn, Execute)
	if d.Allowed {
		t.Error("tentacle owner without execute bit should be denied")
	}
}

// --- Member scope tests (enclave model: group slot = member email list) ---

func TestEvaluator_Group_Read_GroupReadSet_Allow(t *testing.T) {
	e := NewEvaluator(DefaultMode)
	// member email "b@example.com" is in the members list
	ann := annsWith("owner@example.com", "b@example.com", "---r-----")
	d := e.Check(oidcDeployer("sub-other", "b@example.com"), ann, Read)
	if !d.Allowed {
		t.Errorf("enclave member with read bit should be allowed: %s", d.Reason)
	}
}

func TestEvaluator_Group_Read_GroupReadUnset_Deny(t *testing.T) {
	e := NewEvaluator(DefaultMode)
	ann := annsWith("owner@example.com", "b@example.com", "---------")
	d := e.Check(oidcDeployer("sub-other", "b@example.com"), ann, Read)
	if d.Allowed {
		t.Error("enclave member without read bit should be denied")
	}
}

func TestEvaluator_Group_Write_GroupWriteSet_Allow(t *testing.T) {
	e := NewEvaluator(DefaultMode)
	ann := annsWith("owner@example.com", "b@example.com", "----w----")
	d := e.Check(oidcDeployer("sub-other", "b@example.com"), ann, Write)
	if !d.Allowed {
		t.Errorf("enclave member with write bit should be allowed: %s", d.Reason)
	}
}

func TestEvaluator_Group_Execute_GroupExecuteSet_Allow(t *testing.T) {
	e := NewEvaluator(DefaultMode)
	ann := annsWith("owner@example.com", "b@example.com", "-----x---")
	d := e.Check(oidcDeployer("sub-other", "b@example.com"), ann, Execute)
	if !d.Allowed {
		t.Errorf("enclave member with execute bit should be allowed: %s", d.Reason)
	}
}

func TestEvaluator_Group_NotMember_FallsToOther(t *testing.T) {
	e := NewEvaluator(DefaultMode)
	// Member list has c@example.com but caller is b@example.com — falls to other.
	ann := annsWith("owner@example.com", "c@example.com", "---r-----")
	d := e.Check(oidcDeployer("sub-other", "b@example.com"), ann, Read)
	if d.Allowed {
		t.Error("non-member should not get member permissions")
	}
}

func TestEvaluator_Group_EmptyGroupAnnotation_FallsToOther(t *testing.T) {
	e := NewEvaluator(DefaultMode)
	// Resource has group bits set but no members annotation — caller can't match as member.
	ann := annsWith("owner@example.com", "", "---r-----")
	d := e.Check(oidcDeployer("sub-other", "b@example.com"), ann, Read)
	if d.Allowed {
		t.Error("empty members annotation should fall through to other bits (which are unset)")
	}
}

// --- Other scope tests ---

func TestEvaluator_Other_Read_OtherReadSet_Allow(t *testing.T) {
	e := NewEvaluator(DefaultMode)
	ann := annsWith("owner@example.com", "", "------r--")
	d := e.Check(oidcDeployer("sub-other", "b@example.com"), ann, Read)
	if !d.Allowed {
		t.Errorf("other user with read bit should be allowed: %s", d.Reason)
	}
}

func TestEvaluator_Other_Read_OtherReadUnset_Deny(t *testing.T) {
	e := NewEvaluator(DefaultMode)
	ann := annsWith("owner@example.com", "", "rwxrwx---")
	d := e.Check(oidcDeployer("sub-other", "b@example.com"), ann, Read)
	if d.Allowed {
		t.Error("other user without other-read bit should be denied even if owner/group bits set")
	}
}

func TestEvaluator_Other_Write_OtherWriteSet_Allow(t *testing.T) {
	e := NewEvaluator(DefaultMode)
	ann := annsWith("owner@example.com", "", "-------w-")
	d := e.Check(oidcDeployer("sub-other", "b@example.com"), ann, Write)
	if !d.Allowed {
		t.Errorf("other user with write bit should be allowed: %s", d.Reason)
	}
}

func TestEvaluator_Other_Execute_OtherExecuteSet_Allow(t *testing.T) {
	e := NewEvaluator(DefaultMode)
	ann := annsWith("owner@example.com", "", "--------x")
	d := e.Check(oidcDeployer("sub-other", "b@example.com"), ann, Execute)
	if !d.Allowed {
		t.Errorf("other user with execute bit should be allowed: %s", d.Reason)
	}
}

// --- Default mode fallback ---

func TestEvaluator_NoModeAnnotation_UsesDefaultEnclaveMode(t *testing.T) {
	// DefaultEnclaveMode is used when no mode annotation is set on an enclave.
	// DefaultEnclaveMode is rwxrwx--- (owner + members full, others none).
	e := NewEvaluator(DefaultMode)
	ann := map[string]string{
		AnnotationEnclave:      "test-enclave",
		AnnotationEnclaveOwner: "o@example.com",
		// no mode annotation — should fall back to DefaultEnclaveMode
	}
	// Enclave owner read should be allowed (superuser bypass).
	d := e.CheckEnclave(oidcDeployer("sub-owner", "o@example.com"), ann, Read)
	if !d.Allowed {
		t.Errorf("enclave owner read under default mode should be allowed: %s", d.Reason)
	}
	// Other read should be denied (DefaultEnclaveMode has no other bits).
	d = e.CheckEnclave(oidcDeployer("sub-other", "x@example.com"), ann, Read)
	if d.Allowed {
		t.Error("other read under default enclave mode should be denied")
	}
}

// --- Full truth table for private mode ---

func TestEvaluator_PrivateMode_TruthTable(t *testing.T) {
	e := NewEvaluator(DefaultMode)
	owner := oidcDeployer("sub-owner", "owner@example.com")
	groupMember := oidcDeployer("sub-other", "other@example.com", "mygroup")
	stranger := oidcDeployer("sub-stranger", "stranger@example.com")
	ann := annsWith("owner@example.com", "mygroup", "rwx------") // private

	tests := []struct {
		who    *exoskeleton.DeployerInfo
		desc   string
		action Action
		allow  bool
	}{
		{owner, "owner read", Read, true},
		{owner, "owner write", Write, true},
		{owner, "owner execute", Execute, true},
		{groupMember, "group read (no group bits)", Read, false},
		{groupMember, "group write (no group bits)", Write, false},
		{groupMember, "group execute (no group bits)", Execute, false},
		{stranger, "other read (no other bits)", Read, false},
		{stranger, "other write (no other bits)", Write, false},
		{stranger, "other execute (no other bits)", Execute, false},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			d := e.Check(tt.who, ann, tt.action)
			if d.Allowed != tt.allow {
				t.Errorf("Check: allowed=%v, want %v (reason: %s)", d.Allowed, tt.allow, d.Reason)
			}
		})
	}
}

// --- Email-based ownership (issue #62) ---

func TestEvaluator_OwnerMatchByEmail_NotSubject(t *testing.T) {
	// Same subject UUID but different email should NOT match as owner.
	e := NewEvaluator(DefaultMode)
	ann := annsWith("alice@example.com", "", "rwx------")
	d := e.Check(oidcDeployer("sub-alice", "bob@example.com"), ann, Read)
	if d.Allowed {
		t.Error("deployer with different email should not be owner even if subject would have matched before")
	}
}

func TestEvaluator_OwnerMatchByEmail_DifferentSubject_Allow(t *testing.T) {
	// Different subject UUID but same email SHOULD match as owner.
	// This is the key fix: user recreated in Keycloak gets a new UUID but same email.
	e := NewEvaluator(DefaultMode)
	ann := annsWith("alice@example.com", "", "rwx------")
	d := e.Check(oidcDeployer("new-uuid-after-recreate", "alice@example.com"), ann, Read)
	if !d.Allowed {
		t.Errorf("deployer with matching email should be owner regardless of subject: %s", d.Reason)
	}
}

// --- Allow and Deny constructors ---

func TestAllow_IsAllowed(t *testing.T) {
	if !Allow.Allowed {
		t.Error("Allow.Allowed should be true")
	}
}

func TestDeny_IsNotAllowed(t *testing.T) {
	d := Deny("test reason")
	if d.Allowed {
		t.Error("Deny.Allowed should be false")
	}
	if d.Reason != "test reason" {
		t.Errorf("Deny.Reason = %q, want 'test reason'", d.Reason)
	}
}

// --- NewEvaluator ---

func TestNewEvaluator_Enabled(t *testing.T) {
	e := NewEvaluator(DefaultMode)
	if !e.Enabled {
		t.Error("NewEvaluator should return enabled evaluator")
	}
	if e.DefaultMode != DefaultMode {
		t.Errorf("DefaultMode = %v, want %v", e.DefaultMode.String(), DefaultMode.String())
	}
}

// --- CheckTentacle (two-layer enclave authz, 16-scenario matrix) ---
//
// These tests cover every scenario from the design doc Section 6 permission
// matrix. CheckTentacle implements the 7-step evaluator that the task spec
// calls "CheckEnclave(deployer, nsAnnotations, resourceAnnotations, action)".

// enclaveNS builds a namespace annotation map for a minimal enclave.
func enclaveNS(owner string, members ...string) map[string]string { //nolint:unparam
	return map[string]string{
		AnnotationEnclave:        "test-enclave",
		AnnotationEnclaveOwner:   owner,
		AnnotationEnclaveMembers: FormatMembers(members),
	}
}

// resourceAnns builds a resource annotation map with owner and mode.
func resourceAnns(owner, modeStr string) map[string]string { //nolint:unparam
	return map[string]string{
		AnnotationOwner: owner,
		AnnotationMode:  modeStr,
	}
}

// nonEnclaveNS returns a namespace annotation map with no enclave annotation.
func nonEnclaveNS() map[string]string {
	return map[string]string{}
}

// TestCheckTentacle_ScenarioMatrix covers all 16 scenarios from the design doc.
func TestCheckTentacle_ScenarioMatrix(t *testing.T) {
	const (
		enclaveOwnerEmail  = "owner@example.com"
		resourceOwnerEmail = "deployer@example.com"
		memberEmail        = "member@example.com"
		nonMemberEmail     = "visitor@example.com"
	)

	enclaveOwner := oidcDeployer("sub-owner", enclaveOwnerEmail)
	resourceOwner := oidcDeployer("sub-deployer", resourceOwnerEmail)
	member := oidcDeployer("sub-member", memberEmail)
	nonMember := oidcDeployer("sub-visitor", nonMemberEmail)
	bearer := bearerDeployer()

	// ns: member-edit enclave (rwxrwx--- default — no other bits)
	ns := enclaveNS(enclaveOwnerEmail, memberEmail, resourceOwnerEmail)

	// openNS: open-run enclave (rwxrwxr-x — others can read+execute)
	openNS := enclaveNS(enclaveOwnerEmail, memberEmail, resourceOwnerEmail)
	openNS[AnnotationMode] = "rwxrwxr-x"

	tests := []struct { //nolint:govet // fieldalignment: test clarity takes priority over optimal packing
		scenario int
		name     string
		e        *Evaluator
		deployer *exoskeleton.DeployerInfo
		ns       map[string]string
		resource map[string]string
		action   Action
		want     bool
	}{
		// Scenario 1: evaluator disabled → Allow for any action
		{1, "disabled evaluator allows read", &Evaluator{Enabled: false}, nonMember, ns, resourceAnns(resourceOwnerEmail, "rwx------"), Read, true},
		// Scenario 2: bearer-token → Allow for any action
		{2, "bearer-token allows any action", NewEvaluator(DefaultMode), bearer, ns, resourceAnns(resourceOwnerEmail, "rwx------"), Write, true},
		// Scenario 3: no enclave annotation → Deny
		{3, "non-enclave namespace denied", NewEvaluator(DefaultMode), nonMember, nonEnclaveNS(), resourceAnns(resourceOwnerEmail, "rwxrwxrwx"), Read, false},
		// Scenario 4: enclave owner → Allow regardless of resource mode
		{4, "enclave owner overrides resource mode", NewEvaluator(DefaultMode), enclaveOwner, ns, resourceAnns(resourceOwnerEmail, "rwx------"), Write, true},
		// Scenario 5: resource owner, private mode (rwx------), read → Allow
		{5, "resource owner read on private", NewEvaluator(DefaultMode), resourceOwner, ns, resourceAnns(resourceOwnerEmail, "rwx------"), Read, true},
		// Scenario 6: resource owner, private mode (rwx------), write → Allow
		{6, "resource owner write on private", NewEvaluator(DefaultMode), resourceOwner, ns, resourceAnns(resourceOwnerEmail, "rwx------"), Write, true},
		// Scenario 7: resource owner, private mode (rwx------), execute → Allow
		{7, "resource owner execute on private", NewEvaluator(DefaultMode), resourceOwner, ns, resourceAnns(resourceOwnerEmail, "rwx------"), Execute, true},
		// Scenario 8: enclave member, member-edit (rwxrwx---), read → Allow
		{8, "member read on member-edit", NewEvaluator(DefaultMode), member, ns, resourceAnns(resourceOwnerEmail, "rwxrwx---"), Read, true},
		// Scenario 9: enclave member, member-edit (rwxrwx---), write → Allow
		{9, "member write on member-edit", NewEvaluator(DefaultMode), member, ns, resourceAnns(resourceOwnerEmail, "rwxrwx---"), Write, true},
		// Scenario 10: enclave member, member-edit (rwxrwx---), execute → Allow
		{10, "member execute on member-edit", NewEvaluator(DefaultMode), member, ns, resourceAnns(resourceOwnerEmail, "rwxrwx---"), Execute, true},
		// Scenario 11: enclave member, member-read (rwxr-x---), read → Allow
		{11, "member read on member-read", NewEvaluator(DefaultMode), member, ns, resourceAnns(resourceOwnerEmail, "rwxr-x---"), Read, true},
		// Scenario 12: enclave member, member-read (rwxr-x---), write → Deny
		{12, "member write on member-read denied", NewEvaluator(DefaultMode), member, ns, resourceAnns(resourceOwnerEmail, "rwxr-x---"), Write, false},
		// Scenario 13: enclave member, private (rwx------), read → Deny (no member bits)
		{13, "member read on private denied", NewEvaluator(DefaultMode), member, ns, resourceAnns(resourceOwnerEmail, "rwx------"), Read, false},
		// Scenario 14: non-member, open enclave + open-run tentacle (rwxrwxr-x), read → Allow
		// Both layers must pass: enclave has other-read (openNS), tentacle has other-read (open-run).
		{14, "non-member read on open-run", NewEvaluator(DefaultMode), nonMember, openNS, resourceAnns(resourceOwnerEmail, "rwxrwxr-x"), Read, true},
		// Scenario 15: non-member, member-edit enclave, member-edit tentacle (rwxrwx---), read → Deny
		// Enclave has no other bits — denied at the enclave layer.
		{15, "non-member read on member-edit denied", NewEvaluator(DefaultMode), nonMember, ns, resourceAnns(resourceOwnerEmail, "rwxrwx---"), Read, false},
		// Scenario 16: non-member, open enclave + open-run tentacle, write → Deny (other has no write)
		{16, "non-member write on open-run denied", NewEvaluator(DefaultMode), nonMember, openNS, resourceAnns(resourceOwnerEmail, "rwxrwxr-x"), Write, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := tt.e.CheckTentacle(tt.deployer, tt.ns, tt.resource, tt.action)
			if d.Allowed != tt.want {
				t.Errorf("scenario %d (%s): allowed=%v, want=%v (reason: %s)",
					tt.scenario, tt.name, d.Allowed, tt.want, d.Reason)
			}
		})
	}
}

// Additional targeted tests for CheckEnclave (enclave-level only check).

func TestCheckEnclave_Disabled_Allow(t *testing.T) {
	e := &Evaluator{Enabled: false}
	d := e.CheckEnclave(nonEnclaveDeployer(), enclaveNS("owner@example.com"), Read)
	if !d.Allowed {
		t.Error("disabled evaluator should allow")
	}
}

func TestCheckEnclave_BearerToken_Allow(t *testing.T) {
	e := NewEvaluator(DefaultMode)
	d := e.CheckEnclave(bearerDeployer(), enclaveNS("owner@example.com"), Write)
	if !d.Allowed {
		t.Error("bearer token should always allow")
	}
}

func TestCheckEnclave_NotEnclave_Deny(t *testing.T) {
	e := NewEvaluator(DefaultMode)
	d := e.CheckEnclave(oidcDeployer("sub-x", "x@example.com"), nonEnclaveNS(), Read)
	if d.Allowed {
		t.Error("non-enclave namespace should deny")
	}
}

func TestCheckEnclave_EnclaveOwner_Allow(t *testing.T) {
	e := NewEvaluator(DefaultMode)
	ns := enclaveNS("owner@example.com")
	// Set enclave mode to private (owner full, others none) — owner should still pass.
	ns[AnnotationMode] = "rwx------"
	d := e.CheckEnclave(oidcDeployer("sub-owner", "owner@example.com"), ns, Write)
	if !d.Allowed {
		t.Error("enclave owner should be allowed regardless of mode")
	}
}

func TestCheckEnclave_Member_MemberEditMode_Allow(t *testing.T) {
	e := NewEvaluator(DefaultMode)
	ns := enclaveNS("owner@example.com", "member@example.com")
	ns[AnnotationMode] = "rwxrwx---"
	d := e.CheckEnclave(oidcDeployer("sub-m", "member@example.com"), ns, Write)
	if !d.Allowed {
		t.Errorf("member should be allowed with member-edit mode: %s", d.Reason)
	}
}

func TestCheckEnclave_NonMember_MemberEditMode_Deny(t *testing.T) {
	e := NewEvaluator(DefaultMode)
	ns := enclaveNS("owner@example.com", "member@example.com")
	ns[AnnotationMode] = "rwxrwx---"
	d := e.CheckEnclave(oidcDeployer("sub-v", "visitor@example.com"), ns, Read)
	if d.Allowed {
		t.Error("non-member should be denied when other bits are not set")
	}
}

func TestCheckEnclave_NonMember_OpenReadMode_Allow(t *testing.T) {
	e := NewEvaluator(DefaultMode)
	ns := enclaveNS("owner@example.com", "member@example.com")
	ns[AnnotationMode] = "rwxrwxr--" // open-read
	d := e.CheckEnclave(oidcDeployer("sub-v", "visitor@example.com"), ns, Read)
	if !d.Allowed {
		t.Errorf("non-member should be allowed read in open-read mode: %s", d.Reason)
	}
}

// --- checkBits descriptive deny messages (2.12) ---
// Tests use CheckTentacle to exercise per-principal deny messages.
// The deployer is an enclave member (not owner) so the enclave layer passes,
// and the tentacle layer produces the deny with the expected principal class.

func TestCheckBits_DenyMessage_Owner(t *testing.T) {
	e := NewEvaluator(DefaultMode)
	// deployer is the tentacle owner AND enclave member, with mode "---------" on tentacle
	encAnn, tentAnn := enclaveAnnWith("deployer@example.com", "---------")
	d := e.CheckTentacle(oidcDeployer("sub-deployer", "deployer@example.com"), encAnn, tentAnn, Write)
	if d.Allowed {
		t.Fatal("expected deny")
	}
	if !strings.Contains(d.Reason, "owner") {
		t.Errorf("expected 'owner' in reason, got %q", d.Reason)
	}
	if !strings.Contains(d.Reason, "write") {
		t.Errorf("expected 'write' in reason, got %q", d.Reason)
	}
}

func TestCheckBits_DenyMessage_Member(t *testing.T) {
	e := NewEvaluator(DefaultMode)
	// deployer is enclave member (not tentacle owner), tentacle has "---------"
	encAnn := map[string]string{
		AnnotationEnclave:        "test-enclave",
		AnnotationEnclaveOwner:   "admin@example.com",
		AnnotationEnclaveMembers: "bob@example.com",
		AnnotationMode:           "rwxrwx---",
	}
	tentAnn := map[string]string{
		AnnotationOwner: "other-deployer@example.com",
		AnnotationMode:  "---------",
	}
	d := e.CheckTentacle(oidcDeployer("sub-bob", "bob@example.com"), encAnn, tentAnn, Read)
	if d.Allowed {
		t.Fatal("expected deny")
	}
	if !strings.Contains(d.Reason, "member") {
		t.Errorf("expected 'member' in reason, got %q", d.Reason)
	}
	if !strings.Contains(d.Reason, "read") {
		t.Errorf("expected 'read' in reason, got %q", d.Reason)
	}
}

func TestCheckBits_DenyMessage_Other(t *testing.T) {
	e := NewEvaluator(DefaultMode)
	// deployer is non-member, enclave has open-read, tentacle has "---------"
	encAnn := map[string]string{
		AnnotationEnclave:      "test-enclave",
		AnnotationEnclaveOwner: "admin@example.com",
		AnnotationMode:         "rwxrwxr-x", // other can read/execute at enclave level
	}
	tentAnn := map[string]string{
		AnnotationOwner: "deployer@example.com",
		AnnotationMode:  "---------",
	}
	d := e.CheckTentacle(oidcDeployer("sub-stranger", "stranger@example.com"), encAnn, tentAnn, Execute)
	if d.Allowed {
		t.Fatal("expected deny")
	}
	if !strings.Contains(d.Reason, "other") {
		t.Errorf("expected 'other' in reason, got %q", d.Reason)
	}
	if !strings.Contains(d.Reason, "execute") {
		t.Errorf("expected 'execute' in reason, got %q", d.Reason)
	}
}

// nonEnclaveDeployer returns an OIDC deployer with no enclave membership.
func nonEnclaveDeployer() *exoskeleton.DeployerInfo {
	return oidcDeployer("sub-none", "nobody@example.com")
}
