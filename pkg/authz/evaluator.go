package authz

import (
	"slices"

	"github.com/randybias/tentacular-mcp/pkg/exoskeleton"
)

// Action represents the type of operation being authorized.
type Action int

const (
	// Read is the action for listing or describing a resource.
	Read Action = iota
	// Write is the action for creating, updating, or deleting a resource,
	// and for changing permissions.
	Write
	// Execute is the action for running a workflow.
	Execute
)

// Decision is the result of an authorization check.
type Decision struct {
	// Reason describes why the decision was made (for logging/debugging).
	Reason string
	// Allowed is true if the action is permitted.
	Allowed bool
}

// Allow is a shorthand Decision for permitted actions.
var Allow = Decision{Allowed: true, Reason: "allowed"}

// Deny returns a denied Decision with the given reason.
func Deny(reason string) Decision {
	return Decision{Allowed: false, Reason: reason}
}

// Evaluator holds server-level authz configuration. Create one instance at
// startup and pass it through RegisterAll to tool handlers that need it.
// A nil Evaluator disables authz (all checks return Allow).
type Evaluator struct {
	// DefaultMode is applied when a resource has an owner-sub but no mode annotation.
	DefaultMode Mode
	// Enabled is a kill switch. If false, all Check calls return Allow.
	Enabled bool
}

// NewEvaluator creates an Evaluator with the given default mode.
func NewEvaluator(defaultMode Mode) *Evaluator {
	return &Evaluator{
		DefaultMode: defaultMode,
		Enabled:     true,
	}
}

// Check evaluates whether the deployer may perform action on the resource
// described by annotations.
//
// Rules (evaluated in order):
//  1. Evaluator nil or disabled → Allow
//  2. Bearer-token deployer → Allow (full trust, no OIDC identity)
//  3. No owner-sub annotation → Deny (unowned resource, must be adopted first)
//  4. Owner match (deployer.Subject == owner-sub) → check owner bits
//  5. Group match (resource group in deployer.Groups) → check group bits
//  6. Otherwise → check other bits
func (e *Evaluator) Check(deployer *exoskeleton.DeployerInfo, annotations map[string]string, action Action) Decision {
	if e == nil || !e.Enabled {
		return Allow
	}

	// Rule 1: no deployer identity (shouldn't happen in practice, but be safe).
	if deployer == nil {
		return Deny("no deployer identity in request context")
	}

	// Rule 2: bearer-token is full-trust.
	if deployer.Provider == "bearer-token" {
		return Allow
	}

	// Rule 3: no owner-sub means unowned resource — deny access.
	// Use bearer-token or tntc admin adopt to stamp ownership.
	ownerSub := annotations[AnnotationOwnerSub]
	if ownerSub == "" {
		return Deny("resource has no owner; use bearer-token or admin tools to set ownership")
	}

	// Resolve mode, falling back to server default.
	mode := e.DefaultMode
	if raw, ok := annotations[AnnotationMode]; ok && raw != "" {
		if m, err := ParseMode(raw); err == nil {
			mode = m
		}
	}

	// Rule 4: owner match.
	if deployer.Subject == ownerSub {
		return checkBits(mode, action, true, false)
	}

	// Rule 5: group match.
	resourceGroup := annotations[AnnotationGroup]
	if resourceGroup != "" && slices.Contains(deployer.Groups, resourceGroup) {
		return checkBits(mode, action, false, true)
	}

	// Rule 6: other bits.
	return checkBits(mode, action, false, false)
}

// checkBits maps an action to the appropriate mode bits for owner, group, or other.
func checkBits(mode Mode, action Action, isOwner, isGroup bool) Decision {
	var allowed bool
	switch {
	case isOwner:
		switch action {
		case Read:
			allowed = mode.OwnerRead()
		case Write:
			allowed = mode.OwnerWrite()
		case Execute:
			allowed = mode.OwnerExecute()
		}
	case isGroup:
		switch action {
		case Read:
			allowed = mode.GroupRead()
		case Write:
			allowed = mode.GroupWrite()
		case Execute:
			allowed = mode.GroupExecute()
		}
	default:
		switch action {
		case Read:
			allowed = mode.OtherRead()
		case Write:
			allowed = mode.OtherWrite()
		case Execute:
			allowed = mode.OtherExecute()
		}
	}

	if allowed {
		return Allow
	}
	return Deny("permission denied")
}
