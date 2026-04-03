package exoskeleton

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Identity contains all deterministic identifiers derived from a
// (namespace, workflow) tuple. It is the canonical mapping used by all
// exoskeleton registrars.
type Identity struct {
	Namespace  string
	Workflow   string
	Principal  string // spiffe://tentacular/ns/<ns>/tentacles/<wf>
	PgRole     string // tn_<ns>_<wf> (hyphens -> underscores, max 63 chars)
	PgSchema   string // same as PgRole
	NATSUser   string // tentacle.<ns>.<wf>
	NATSPrefix string // tentacular.<ns>.<wf>.>
	S3Prefix   string // ns/<ns>/tentacles/<wf>/
	S3User     string // same as PgRole
	S3Policy   string // tn_<ns>_<wf>_policy
}

// maxPgIdentLen is the maximum length of a Postgres identifier.
const maxPgIdentLen = 63

// ErrEmptyNamespace is returned when namespace is empty.
var ErrEmptyNamespace = errors.New("namespace must not be empty")

// ErrEmptyWorkflow is returned when workflow is empty.
var ErrEmptyWorkflow = errors.New("workflow must not be empty")

// CompileIdentity deterministically computes all service-specific
// identifiers from a namespace and workflow name. Returns an error if
// namespace or workflow is empty.
func CompileIdentity(namespace, workflow string) (Identity, error) {
	if namespace == "" {
		return Identity{}, ErrEmptyNamespace
	}
	if workflow == "" {
		return Identity{}, ErrEmptyWorkflow
	}
	pgBase := sanitizePg(namespace, workflow)
	return Identity{
		Namespace:  namespace,
		Workflow:   workflow,
		Principal:  fmt.Sprintf("spiffe://tentacular/ns/%s/tentacles/%s", namespace, workflow),
		PgRole:     pgBase,
		PgSchema:   pgBase,
		NATSUser:   fmt.Sprintf("tentacle.%s.%s", namespace, workflow),
		NATSPrefix: fmt.Sprintf("tentacular.%s.%s.>", namespace, workflow),
		S3Prefix:   fmt.Sprintf("ns/%s/tentacles/%s/", namespace, workflow),
		S3User:     pgBase,
		S3Policy:   truncatePg(fmt.Sprintf("tn_%s_%s_policy", replacePg(namespace), replacePg(workflow))),
	}, nil
}

// ErrInvalidEnclaveName is returned when an enclave name fails DNS-1123 or safety checks.
var ErrInvalidEnclaveName = errors.New("invalid enclave name")

// ErrEmptyEnclave is returned when enclave is empty.
var ErrEmptyEnclave = errors.New("enclave must not be empty")

// ErrEmptyTentacle is returned when tentacle is empty.
var ErrEmptyTentacle = errors.New("tentacle must not be empty")

// ValidateEnclaveName validates that an enclave name is a valid DNS-1123 label and
// meets additional safety constraints required by Tentacular infrastructure.
//
// This function mirrors authz.ValidateEnclaveName. The implementation is duplicated
// to avoid a circular import: pkg/authz imports pkg/exoskeleton for DeployerInfo.
//
// Rules:
//   - Must match DNS-1123 label regex (lowercase alphanumeric + hyphens, 1–63 chars)
//   - Must not contain "." (NATS subject-token safety)
func ValidateEnclaveName(name string) error {
	if name == "" {
		return ErrEmptyEnclave
	}
	if !dns1123LabelRe.MatchString(name) {
		return fmt.Errorf("%w: %q must match ^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$", ErrInvalidEnclaveName, name)
	}
	if strings.Contains(name, ".") {
		return fmt.Errorf("%w: %q must not contain '.' (NATS safety)", ErrInvalidEnclaveName, name)
	}
	return nil
}

// EnclaveIdentity contains all deterministic identifiers derived from an enclave name.
// These are shared across all tentacles within the enclave.
type EnclaveIdentity struct {
	// Enclave is the enclave name (Kubernetes namespace name).
	Enclave string
	// PgDB is the Postgres database name for this enclave (tn_<enclave>, hyphens->underscores, max 63 chars).
	PgDB string
	// S3Bucket is the S3/RustFS bucket name for this enclave (tentacular-<enclave>).
	S3Bucket string
	// NATSAcct is the NATS account name for this enclave (tentacular.<enclave>).
	NATSAcct string
}

// TentacleIdentity contains all deterministic identifiers for a tentacle within an enclave.
// It embeds the enclave-level identity and adds per-tentacle fields.
type TentacleIdentity struct {
	// Enclave holds the enclave-level shared identifiers.
	Enclave EnclaveIdentity
	// Tentacle is the tentacle (workflow) name.
	Tentacle string
	// PgSchema is the Postgres schema within the enclave database (tn_<enclave>_<tentacle>, max 63 chars).
	PgSchema string
	// S3Prefix is the S3/RustFS object prefix for this tentacle (tentacles/<tentacle>/).
	S3Prefix string
	// NATSUser is the NATS user name for this tentacle (tentacular.<enclave>.<tentacle>).
	NATSUser string
	// SpireSVID is the SPIFFE SVID URI for this tentacle.
	SpireSVID string
}

// CompileEnclaveIdentity deterministically computes all enclave-level service identifiers
// from an enclave name. Returns an error if the enclave name is empty or invalid.
func CompileEnclaveIdentity(enclave string) (EnclaveIdentity, error) {
	if err := ValidateEnclaveName(enclave); err != nil {
		return EnclaveIdentity{}, err
	}
	pgDB := truncatePg("tn_" + replacePg(enclave))
	return EnclaveIdentity{
		Enclave:  enclave,
		PgDB:     pgDB,
		S3Bucket: truncateS3Bucket("tentacular-" + enclave),
		NATSAcct: "tentacular." + enclave,
	}, nil
}

// CompileTentacleIdentity deterministically computes all per-tentacle service identifiers
// from an enclave and tentacle name. Returns an error if either name is empty.
func CompileTentacleIdentity(enclave, tentacle string) (TentacleIdentity, error) {
	enc, err := CompileEnclaveIdentity(enclave)
	if err != nil {
		return TentacleIdentity{}, err
	}
	if tentacle == "" {
		return TentacleIdentity{}, ErrEmptyTentacle
	}
	pgSchema := truncatePg(fmt.Sprintf("tn_%s_%s", replacePg(enclave), replacePg(tentacle)))
	return TentacleIdentity{
		Enclave:   enc,
		Tentacle:  tentacle,
		PgSchema:  pgSchema,
		S3Prefix:  fmt.Sprintf("tentacles/%s/", tentacle),
		NATSUser:  fmt.Sprintf("tentacular.%s.%s", enclave, tentacle),
		SpireSVID: fmt.Sprintf("spiffe://tentacular/enclaves/%s/tentacles/%s", enclave, tentacle),
	}, nil
}

// dns1123LabelRe mirrors the same regex in pkg/authz to avoid a circular import
// (pkg/authz imports pkg/exoskeleton for DeployerInfo).
// Pattern enforces DNS-1123 label rules: lowercase alphanumeric, hyphens allowed in
// middle, 1–63 characters total.
// NOTE: this regex MUST stay in sync with authz.dns1123LabelRe.
var dns1123LabelRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// pgSafeRe matches any character not in [a-z0-9_].
var pgSafeRe = regexp.MustCompile(`[^a-z0-9_]`)

// replacePg replaces hyphens with underscores, lowercases, and strips
// any remaining character not matching [a-z0-9_]. K8s namespace names
// are DNS-1123 (lowercase alphanumeric + hyphens), but this provides a
// broader safety net against non-standard input.
func replacePg(s string) string {
	s = strings.ToLower(strings.ReplaceAll(s, "-", "_"))
	return pgSafeRe.ReplaceAllString(s, "")
}

// sanitizePg builds the "tn_<ns>_<wf>" Postgres identifier with proper
// sanitization and length limiting.
func sanitizePg(namespace, workflow string) string {
	raw := fmt.Sprintf("tn_%s_%s", replacePg(namespace), replacePg(workflow))
	return truncatePg(raw)
}

// truncatePg ensures a Postgres identifier fits within 63 characters.
// If the raw identifier exceeds the limit, it is truncated and a short
// hash suffix is appended to maintain uniqueness.
func truncatePg(raw string) string {
	if len(raw) <= maxPgIdentLen {
		return raw
	}
	h := sha256.Sum256([]byte(raw))
	suffix := fmt.Sprintf("_%x", h[:4]) // 9 chars: _ + 8 hex
	return raw[:maxPgIdentLen-len(suffix)] + suffix
}

// maxS3BucketLen is the maximum length of an S3/RustFS bucket name.
const maxS3BucketLen = 63

// truncateS3Bucket ensures an S3 bucket name fits within 63 characters.
// S3 bucket names follow similar length constraints to DNS labels.
// If the raw name exceeds the limit, it is truncated and a short hash suffix
// is appended (using "-" as separator, since S3 names are lowercase alphanumeric+hyphens).
func truncateS3Bucket(raw string) string {
	if len(raw) <= maxS3BucketLen {
		return raw
	}
	h := sha256.Sum256([]byte(raw))
	suffix := fmt.Sprintf("-%x", h[:4]) // 9 chars: - + 8 hex
	return raw[:maxS3BucketLen-len(suffix)] + suffix
}
