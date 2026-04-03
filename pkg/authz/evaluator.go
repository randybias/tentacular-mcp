package authz

import (
	"fmt"
	"strings"

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
	// DefaultMode is applied when a resource has an owner but no mode annotation.
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
// described by annotations. This method is retained for backward compatibility
// with handler code that pre-dates the enclave model; it delegates to
// CheckEnclave. Callers that have namespace annotations available should
// prefer CheckTentacle directly.
//
// Deprecated: use CheckEnclave or CheckTentacle for new code.
func (e *Evaluator) Check(deployer *exoskeleton.DeployerInfo, annotations map[string]string, action Action) Decision {
	return e.CheckEnclave(deployer, annotations, action)
}

// CheckEnclave evaluates whether the deployer may perform action on the enclave
// described by enclaveAnnotations. This is the enclave-aware 7-step evaluator.
//
// Steps (evaluated in order):
//  1. Evaluator nil or disabled → Allow
//  2. Bearer-token deployer → Allow (platform operator)
//  3. No enclave annotation → Deny (not an enclave namespace)
//  4. Enclave owner match → Allow (superuser within the enclave)
//  5. Resource owner match (AnnotationOwner) → check owner bits
//  6. Enclave member match (AnnotationEnclaveMembers) → check member/group bits
//  7. Otherwise → check other bits
//
// The mode is resolved from the enclave annotations (AnnotationMode). If
// absent, DefaultEnclaveMode is used.
func (e *Evaluator) CheckEnclave(deployer *exoskeleton.DeployerInfo, enclaveAnn map[string]string, action Action) Decision {
	if e == nil || !e.Enabled {
		return Allow
	}
	if deployer == nil {
		return Deny("no deployer identity in request context")
	}
	if deployer.Provider == "bearer-token" {
		return Allow
	}

	// Step 3: must be an enclave namespace.
	if enclaveAnn[AnnotationEnclave] == "" {
		return Deny("namespace is not an enclave")
	}

	// Resolve mode for this enclave.
	mode := DefaultEnclaveMode
	if raw, ok := enclaveAnn[AnnotationMode]; ok && raw != "" {
		if m, err := ParseMode(raw); err == nil {
			mode = m
		}
	}

	// Step 4: enclave owner is superuser — bypass all permission checks.
	// Normalize to lowercase for case-insensitive comparison.
	deployerEmail := strings.ToLower(deployer.Email)
	enclaveOwner := strings.ToLower(enclaveAnn[AnnotationEnclaveOwner])
	if enclaveOwner != "" && deployerEmail == enclaveOwner {
		return Allow
	}

	// Step 5: resource owner check (e.g., for tentacle-level operations using
	// enclave annotations directly, the resource owner is the enclave owner —
	// already handled above. For standalone enclave-level operations, the
	// resource owner IS the enclave owner, so this step is a no-op here.
	// The AnnotationOwner is checked via CheckTentacle for tentacle-level ops.)

	// Step 6: enclave member match.
	members := ParseMembers(enclaveAnn[AnnotationEnclaveMembers])
	for _, m := range members {
		if m == deployerEmail {
			return checkBits(mode, action, false, true)
		}
	}

	// Step 7: other bits.
	return checkBits(mode, action, false, false)
}

// CheckTentacle performs a two-layer authorization check for tentacle-level
// operations within an enclave. Both the enclave layer and the tentacle layer
// must pass. The enclave owner bypasses the tentacle layer (superuser).
//
// enclaveAnn: annotations from the enclave namespace.
// tentacleAnn: annotations from the tentacle resource (Deployment).
// action: the action being authorized.
//
// Two-layer check:
//  1. Enclave layer: CheckEnclave(deployer, enclaveAnn, action)
//  2. Tentacle layer: 7-step check using tentacleAnn with enclave members
//     Exception: enclave owner bypasses tentacle layer entirely.
func (e *Evaluator) CheckTentacle(deployer *exoskeleton.DeployerInfo, enclaveAnn, tentacleAnn map[string]string, action Action) Decision {
	if e == nil || !e.Enabled {
		return Allow
	}
	if deployer == nil {
		return Deny("no deployer identity in request context")
	}
	if deployer.Provider == "bearer-token" {
		return Allow
	}

	// Normalize deployer email to lowercase for case-insensitive comparisons.
	deployerEmail := strings.ToLower(deployer.Email)

	// Enclave owner bypass: superuser within the enclave skips all checks.
	enclaveOwner := strings.ToLower(enclaveAnn[AnnotationEnclaveOwner])
	if enclaveOwner != "" && deployerEmail == enclaveOwner {
		return Allow
	}

	// Layer 1: enclave access check.
	if d := e.CheckEnclave(deployer, enclaveAnn, action); !d.Allowed {
		return d
	}

	// Layer 2: tentacle access check (uses enclave members as the group).
	// Resolve tentacle mode.
	mode := DefaultEnclaveMode
	if raw, ok := tentacleAnn[AnnotationMode]; ok && raw != "" {
		if m, err := ParseMode(raw); err == nil {
			mode = m
		}
	}

	// Tentacle owner check (email-based, case-insensitive).
	tentacleOwner := strings.ToLower(tentacleAnn[AnnotationOwner])
	if tentacleOwner != "" && deployerEmail == tentacleOwner {
		return checkBits(mode, action, true, false)
	}

	// Enclave member check at tentacle level.
	members := ParseMembers(enclaveAnn[AnnotationEnclaveMembers])
	for _, m := range members {
		if m == deployerEmail {
			return checkBits(mode, action, false, true)
		}
	}

	// Other.
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

	var principal string
	switch {
	case isOwner:
		principal = "owner"
	case isGroup:
		principal = "member"
	default:
		principal = "other"
	}
	var actionStr string
	switch action {
	case Read:
		actionStr = "read"
	case Write:
		actionStr = "write"
	case Execute:
		actionStr = "execute"
	default:
		actionStr = "unknown"
	}
	return Deny(fmt.Sprintf("permission denied: %s lacks %s access", principal, actionStr))
}
