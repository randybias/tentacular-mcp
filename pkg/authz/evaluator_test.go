package authz

import (
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

func annsWith(ownerSub, group, modeStr string) map[string]string {
	return map[string]string{
		AnnotationOwnerSub: ownerSub,
		AnnotationGroup:    group,
		AnnotationMode:     modeStr,
	}
}

func TestEvaluator_NilEvaluator_AlwaysAllow(t *testing.T) {
	var e *Evaluator
	deployer := oidcDeployer("sub-a", "a@example.com")
	ann := annsWith("sub-other", "g", "rwx------")

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
	ann := annsWith("sub-other", "g", "rwx------")

	for _, action := range []Action{Read, Write, Execute} {
		d := e.Check(deployer, ann, action)
		if !d.Allowed {
			t.Errorf("disabled evaluator should allow action %v", action)
		}
	}
}

func TestEvaluator_NilDeployer_Deny(t *testing.T) {
	e := NewEvaluator(DefaultMode)
	ann := annsWith("sub-owner", "", "rwx------")

	d := e.Check(nil, ann, Read)
	if d.Allowed {
		t.Error("expected deny for nil deployer")
	}
}

func TestEvaluator_BearerToken_AlwaysAllow(t *testing.T) {
	e := NewEvaluator(DefaultMode)
	deployer := bearerDeployer()
	// Even with tight permissions, bearer bypasses.
	ann := annsWith("sub-owner", "", "rwx------")

	for _, action := range []Action{Read, Write, Execute} {
		d := e.Check(deployer, ann, action)
		if !d.Allowed {
			t.Errorf("bearer token should bypass authz for action %v", action)
		}
	}
}

func TestEvaluator_UnownedResource_Deny(t *testing.T) {
	// Unowned resources (no owner-sub annotation) are denied in strict mode.
	// Use bearer-token or admin tools to stamp ownership.
	e := NewEvaluator(DefaultMode)
	deployer := oidcDeployer("sub-a", "a@example.com")
	ann := map[string]string{} // no owner-sub

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

// --- Owner scope tests ---

func TestEvaluator_Owner_Read_OwnerReadSet_Allow(t *testing.T) {
	e := NewEvaluator(DefaultMode)
	ann := annsWith("sub-owner", "", "r--------")
	d := e.Check(oidcDeployer("sub-owner", "a@example.com"), ann, Read)
	if !d.Allowed {
		t.Errorf("owner with read bit should be allowed: %s", d.Reason)
	}
}

func TestEvaluator_Owner_Read_OwnerReadUnset_Deny(t *testing.T) {
	e := NewEvaluator(DefaultMode)
	ann := annsWith("sub-owner", "", "---------")
	d := e.Check(oidcDeployer("sub-owner", "a@example.com"), ann, Read)
	if d.Allowed {
		t.Error("owner without read bit should be denied")
	}
}

func TestEvaluator_Owner_Write_OwnerWriteSet_Allow(t *testing.T) {
	e := NewEvaluator(DefaultMode)
	ann := annsWith("sub-owner", "", "-w-------")
	d := e.Check(oidcDeployer("sub-owner", "a@example.com"), ann, Write)
	if !d.Allowed {
		t.Errorf("owner with write bit should be allowed: %s", d.Reason)
	}
}

func TestEvaluator_Owner_Write_OwnerWriteUnset_Deny(t *testing.T) {
	e := NewEvaluator(DefaultMode)
	ann := annsWith("sub-owner", "", "---------")
	d := e.Check(oidcDeployer("sub-owner", "a@example.com"), ann, Write)
	if d.Allowed {
		t.Error("owner without write bit should be denied")
	}
}

func TestEvaluator_Owner_Execute_OwnerExecuteSet_Allow(t *testing.T) {
	e := NewEvaluator(DefaultMode)
	ann := annsWith("sub-owner", "", "--x------")
	d := e.Check(oidcDeployer("sub-owner", "a@example.com"), ann, Execute)
	if !d.Allowed {
		t.Errorf("owner with execute bit should be allowed: %s", d.Reason)
	}
}

func TestEvaluator_Owner_Execute_OwnerExecuteUnset_Deny(t *testing.T) {
	e := NewEvaluator(DefaultMode)
	ann := annsWith("sub-owner", "", "---------")
	d := e.Check(oidcDeployer("sub-owner", "a@example.com"), ann, Execute)
	if d.Allowed {
		t.Error("owner without execute bit should be denied")
	}
}

// --- Group scope tests ---

func TestEvaluator_Group_Read_GroupReadSet_Allow(t *testing.T) {
	e := NewEvaluator(DefaultMode)
	ann := annsWith("sub-owner", "platform", "---r-----")
	d := e.Check(oidcDeployer("sub-other", "b@example.com", "platform"), ann, Read)
	if !d.Allowed {
		t.Errorf("group member with read bit should be allowed: %s", d.Reason)
	}
}

func TestEvaluator_Group_Read_GroupReadUnset_Deny(t *testing.T) {
	e := NewEvaluator(DefaultMode)
	ann := annsWith("sub-owner", "platform", "---------")
	d := e.Check(oidcDeployer("sub-other", "b@example.com", "platform"), ann, Read)
	if d.Allowed {
		t.Error("group member without read bit should be denied")
	}
}

func TestEvaluator_Group_Write_GroupWriteSet_Allow(t *testing.T) {
	e := NewEvaluator(DefaultMode)
	ann := annsWith("sub-owner", "platform", "----w----")
	d := e.Check(oidcDeployer("sub-other", "b@example.com", "platform"), ann, Write)
	if !d.Allowed {
		t.Errorf("group member with write bit should be allowed: %s", d.Reason)
	}
}

func TestEvaluator_Group_Execute_GroupExecuteSet_Allow(t *testing.T) {
	e := NewEvaluator(DefaultMode)
	ann := annsWith("sub-owner", "platform", "-----x---")
	d := e.Check(oidcDeployer("sub-other", "b@example.com", "platform"), ann, Execute)
	if !d.Allowed {
		t.Errorf("group member with execute bit should be allowed: %s", d.Reason)
	}
}

func TestEvaluator_Group_NotMember_FallsToOther(t *testing.T) {
	e := NewEvaluator(DefaultMode)
	// Group has read but others don't. Non-member should be denied.
	ann := annsWith("sub-owner", "platform", "---r-----")
	d := e.Check(oidcDeployer("sub-other", "b@example.com", "different-group"), ann, Read)
	if d.Allowed {
		t.Error("non-group-member should not get group permissions")
	}
}

func TestEvaluator_Group_EmptyGroupAnnotation_FallsToOther(t *testing.T) {
	e := NewEvaluator(DefaultMode)
	// Resource has group bits set but no group annotation — caller with groups can't match.
	ann := annsWith("sub-owner", "", "---r-----")
	d := e.Check(oidcDeployer("sub-other", "b@example.com", "platform"), ann, Read)
	if d.Allowed {
		t.Error("empty group annotation should fall through to other bits (which are unset)")
	}
}

// --- Other scope tests ---

func TestEvaluator_Other_Read_OtherReadSet_Allow(t *testing.T) {
	e := NewEvaluator(DefaultMode)
	ann := annsWith("sub-owner", "", "------r--")
	d := e.Check(oidcDeployer("sub-other", "b@example.com"), ann, Read)
	if !d.Allowed {
		t.Errorf("other user with read bit should be allowed: %s", d.Reason)
	}
}

func TestEvaluator_Other_Read_OtherReadUnset_Deny(t *testing.T) {
	e := NewEvaluator(DefaultMode)
	ann := annsWith("sub-owner", "", "rwxrwx---")
	d := e.Check(oidcDeployer("sub-other", "b@example.com"), ann, Read)
	if d.Allowed {
		t.Error("other user without other-read bit should be denied even if owner/group bits set")
	}
}

func TestEvaluator_Other_Write_OtherWriteSet_Allow(t *testing.T) {
	e := NewEvaluator(DefaultMode)
	ann := annsWith("sub-owner", "", "-------w-")
	d := e.Check(oidcDeployer("sub-other", "b@example.com"), ann, Write)
	if !d.Allowed {
		t.Errorf("other user with write bit should be allowed: %s", d.Reason)
	}
}

func TestEvaluator_Other_Execute_OtherExecuteSet_Allow(t *testing.T) {
	e := NewEvaluator(DefaultMode)
	ann := annsWith("sub-owner", "", "--------x")
	d := e.Check(oidcDeployer("sub-other", "b@example.com"), ann, Execute)
	if !d.Allowed {
		t.Errorf("other user with execute bit should be allowed: %s", d.Reason)
	}
}

// --- Default mode fallback ---

func TestEvaluator_NoModeAnnotation_UsesDefaultMode(t *testing.T) {
	// DefaultMode is group-read (rwxr-x---): owner full, group r+x, others none.
	e := NewEvaluator(DefaultMode)
	ann := map[string]string{
		AnnotationOwnerSub: "sub-owner",
		AnnotationGroup:    "mygroup",
		// no mode annotation
	}
	// Owner read should be allowed.
	d := e.Check(oidcDeployer("sub-owner", "o@example.com"), ann, Read)
	if !d.Allowed {
		t.Errorf("owner read under default mode should be allowed: %s", d.Reason)
	}
	// Other read should be denied (DefaultMode has no other bits).
	d = e.Check(oidcDeployer("sub-other", "x@example.com"), ann, Read)
	if d.Allowed {
		t.Error("other read under default mode should be denied")
	}
}

// --- Full truth table for private mode ---

func TestEvaluator_PrivateMode_TruthTable(t *testing.T) {
	e := NewEvaluator(DefaultMode)
	owner := oidcDeployer("sub-owner", "owner@example.com")
	groupMember := oidcDeployer("sub-other", "other@example.com", "mygroup")
	stranger := oidcDeployer("sub-stranger", "stranger@example.com")
	ann := annsWith("sub-owner", "mygroup", "rwx------") // private

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
