package authz

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// dns1123LabelRe matches a valid DNS-1123 label (also a valid K8s namespace name).
// Pattern: starts and ends with alphanumeric, middle can have hyphens, max 63 chars.
var dns1123LabelRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// validRwxRe matches a valid 9-character rwx permission string.
var validRwxRe = regexp.MustCompile(`^[r\-][w\-][x\-][r\-][w\-][x\-][r\-][w\-][x\-]$`)

// MaxEnclaveMembers is the maximum number of members allowed in a single enclave.
// This is a package-level var (not const) so it can be overridden by server config at startup.
var MaxEnclaveMembers = 100

// Annotation key constants for enclave namespace annotations.
// All enclave annotations use the tentacular.io/enclave-* prefix.
const (
	// AnnotationEnclave is the enclave name (identifies a namespace as an enclave).
	AnnotationEnclave = "tentacular.io/enclave"

	// AnnotationEnclaveOwner is the email address of the enclave owner.
	AnnotationEnclaveOwner = "tentacular.io/enclave-owner"

	// AnnotationEnclaveOwnerSub is the OIDC subject of the enclave owner.
	AnnotationEnclaveOwnerSub = "tentacular.io/enclave-owner-sub"

	// AnnotationEnclaveMembers is the comma-separated list of member email addresses.
	AnnotationEnclaveMembers = "tentacular.io/enclave-members"

	// AnnotationEnclavePlatform is the platform binding (e.g. "slack").
	AnnotationEnclavePlatform = "tentacular.io/enclave-platform"

	// AnnotationEnclaveChannelID is the platform channel ID (e.g. Slack channel ID).
	AnnotationEnclaveChannelID = "tentacular.io/enclave-channel-id"

	// AnnotationEnclaveChannelName is the platform channel name (e.g. Slack channel name).
	AnnotationEnclaveChannelName = "tentacular.io/enclave-channel-name"

	// AnnotationEnclaveStatus is the current lifecycle status of the enclave (e.g. "active", "provisioning").
	AnnotationEnclaveStatus = "tentacular.io/enclave-status"

	// AnnotationEnclaveDefaultMode is the default permission mode for new tentacles in this enclave.
	AnnotationEnclaveDefaultMode = "tentacular.io/enclave-default-mode"

	// AnnotationEnclaveCreatedAt is the RFC3339 timestamp when the enclave was created.
	AnnotationEnclaveCreatedAt = "tentacular.io/enclave-created-at"

	// AnnotationEnclaveUpdatedAt is the RFC3339 timestamp when the enclave was last updated.
	AnnotationEnclaveUpdatedAt = "tentacular.io/enclave-updated-at"
)

// EnclaveInfo holds all enclave metadata read from namespace annotations.
// Fields are ordered for optimal memory alignment (strings before slice).
type EnclaveInfo struct {
	// Enclave is the enclave name (also the namespace name).
	Enclave string
	// Owner is the email address of the enclave owner.
	Owner string
	// OwnerSub is the OIDC subject of the enclave owner.
	OwnerSub string
	// Platform is the platform binding (e.g. "slack").
	Platform string
	// ChannelID is the platform channel ID.
	ChannelID string
	// ChannelName is the platform channel name.
	ChannelName string
	// Status is the current lifecycle status.
	Status string
	// DefaultMode is the default permission mode string for new tentacles.
	DefaultMode string
	// CreatedAt is the RFC3339 creation timestamp.
	CreatedAt string
	// UpdatedAt is the RFC3339 last-updated timestamp.
	UpdatedAt string
	// Members is the list of member email addresses (excludes the owner).
	Members []string
}

// ReadEnclaveInfo extracts enclave metadata from a namespace's annotation map.
// Returns an EnclaveInfo with all available fields populated. If the enclave
// annotation is absent, the returned EnclaveInfo will have an empty Enclave field.
func ReadEnclaveInfo(annotations map[string]string) EnclaveInfo {
	return EnclaveInfo{
		Enclave:     annotations[AnnotationEnclave],
		Owner:       annotations[AnnotationEnclaveOwner],
		OwnerSub:    annotations[AnnotationEnclaveOwnerSub],
		Members:     ParseMembers(annotations[AnnotationEnclaveMembers]),
		Platform:    annotations[AnnotationEnclavePlatform],
		ChannelID:   annotations[AnnotationEnclaveChannelID],
		ChannelName: annotations[AnnotationEnclaveChannelName],
		Status:      annotations[AnnotationEnclaveStatus],
		DefaultMode: annotations[AnnotationEnclaveDefaultMode],
		CreatedAt:   annotations[AnnotationEnclaveCreatedAt],
		UpdatedAt:   annotations[AnnotationEnclaveUpdatedAt],
	}
}

// WriteEnclaveAnnotations returns a map of annotation key→value for the enclave
// fields to be stamped onto a namespace. All fields are written; absent values
// produce empty strings (which callers may choose to omit via a merge operation).
func WriteEnclaveAnnotations(info EnclaveInfo) map[string]string {
	return map[string]string{
		AnnotationEnclave:            info.Enclave,
		AnnotationEnclaveOwner:       info.Owner,
		AnnotationEnclaveOwnerSub:    info.OwnerSub,
		AnnotationEnclaveMembers:     FormatMembers(info.Members),
		AnnotationEnclavePlatform:    info.Platform,
		AnnotationEnclaveChannelID:   info.ChannelID,
		AnnotationEnclaveChannelName: info.ChannelName,
		AnnotationEnclaveStatus:      info.Status,
		AnnotationEnclaveDefaultMode: info.DefaultMode,
		AnnotationEnclaveCreatedAt:   info.CreatedAt,
		AnnotationEnclaveUpdatedAt:   info.UpdatedAt,
	}
}

// ParseMembers parses a comma-separated list of member emails into a string slice.
// Whitespace around each entry is trimmed. Empty entries are ignored.
// Always returns a non-nil slice.
func ParseMembers(csv string) []string {
	result := []string{}
	if csv == "" {
		return result
	}
	for _, part := range strings.Split(csv, ",") {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// FormatMembers serializes a slice of member emails to comma-separated format.
// Returns an empty string for nil or empty slices.
func FormatMembers(members []string) string {
	return strings.Join(members, ",")
}

// ValidateMembers validates the members slice. Returns an error if the number
// of members exceeds MaxEnclaveMembers.
func ValidateMembers(members []string) error {
	if len(members) > MaxEnclaveMembers {
		return fmt.Errorf("enclave member count %d exceeds maximum of %d", len(members), MaxEnclaveMembers)
	}
	return nil
}

// ValidateEnclaveName validates an enclave name against DNS-1123 label rules and
// additional safety constraints required by Tentacular's infrastructure components.
//
// Rules enforced:
//   - Must match DNS-1123 label: ^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$
//   - Must not contain "." (NATS subject-token safety)
//   - Must not equal ".." (SPIFFE path safety; already excluded by DNS-1123 but listed
//     explicitly for documentation clarity)
func ValidateEnclaveName(name string) error {
	if !dns1123LabelRe.MatchString(name) {
		return fmt.Errorf("enclave name %q is not a valid DNS-1123 label: must match ^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$", name)
	}
	// DNS-1123 already excludes "." but we guard explicitly for NATS/SPIFFE safety.
	if strings.Contains(name, ".") {
		return fmt.Errorf("enclave name %q must not contain '.' (NATS safety)", name)
	}
	return nil
}

// ValidateEnclaveInfo validates an EnclaveInfo struct for correctness before
// writing to namespace annotations. Returns a non-nil error describing the
// first validation failure found.
//
// Validation rules:
//   - Enclave: must pass ValidateEnclaveName
//   - Owner: must be non-empty and contain "@"
//   - Each member: must be non-empty and contain "@"
//   - Platform: must be one of "slack", "" (empty allowed for backwards compatibility)
//   - Status: must be one of "active", "provisioning", "frozen", "" (empty allowed)
//   - DefaultMode: if non-empty, must be a valid 9-character rwx string
//   - Member count: must not exceed MaxEnclaveMembers
func ValidateEnclaveInfo(info EnclaveInfo) error {
	if err := ValidateEnclaveName(info.Enclave); err != nil {
		return err
	}
	if info.Owner == "" {
		return errors.New("enclave owner must not be empty")
	}
	if !strings.Contains(info.Owner, "@") {
		return fmt.Errorf("enclave owner %q must contain '@'", info.Owner)
	}
	for i, m := range info.Members {
		if m == "" {
			return fmt.Errorf("enclave member at index %d must not be empty", i)
		}
		if !strings.Contains(m, "@") {
			return fmt.Errorf("enclave member %q at index %d must contain '@'", m, i)
		}
	}
	switch info.Platform {
	case "slack", "":
		// valid
	default:
		return fmt.Errorf("enclave platform %q is not in allowed set: slack, \"\"", info.Platform)
	}
	switch info.Status {
	case "active", "provisioning", "frozen", "":
		// valid
	default:
		return fmt.Errorf("enclave status %q is not in allowed set: active, provisioning, frozen, \"\"", info.Status)
	}
	if info.DefaultMode != "" && !validRwxRe.MatchString(info.DefaultMode) {
		return fmt.Errorf("enclave default-mode %q must be a valid 9-character rwx string (e.g. \"rwxr-x---\")", info.DefaultMode)
	}
	return ValidateMembers(info.Members)
}

// IsEnclave reports whether the annotations indicate the namespace is an enclave.
// Returns true only if the tentacular.io/enclave annotation is present and non-empty.
func IsEnclave(annotations map[string]string) bool {
	return annotations[AnnotationEnclave] != ""
}
