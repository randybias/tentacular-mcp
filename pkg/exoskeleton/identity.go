package exoskeleton

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"
)

// Identity holds all service-specific identifiers for a tentacle,
// deterministically derived from the namespace and workflow name.
type Identity struct {
	Namespace          string
	Workflow           string
	PostgresRole       string
	PostgresSchema     string
	NATSSubjectPrefix  string
	NATSPrincipal      string
	RustFSPrefix       string
	CanonicalPrincipal string
}

const (
	postgresPrefix    = "tn_"
	natsPrefix        = "tentacular."
	rustfsPrefix      = "ns/"
	maxPostgresIDLen  = 63
)

// nonAlphanumericOrUnderscore matches any character that is not a lowercase letter, digit, or underscore.
var nonAlphanumericOrUnderscore = regexp.MustCompile(`[^a-z0-9_]`)

// CompileIdentity produces a deterministic Identity for the given namespace
// and workflow. All identifiers are derived from the same inputs.
func CompileIdentity(namespace, workflow string) Identity {
	ns := strings.ToLower(namespace)
	wf := strings.ToLower(workflow)

	pgNS := sanitizePostgres(ns)
	pgWF := sanitizePostgres(wf)
	pgRole := truncatePostgresID(postgresPrefix + pgNS + "_" + pgWF)
	pgSchema := truncatePostgresID(postgresPrefix + pgNS + "_" + pgWF)

	natsNS := sanitizeNATS(ns)
	natsWF := sanitizeNATS(wf)
	natsSubject := natsPrefix + natsNS + "." + natsWF
	natsPrincipal := natsNS + "." + natsWF

	rustfs := rustfsPrefix + ns + "/tentacles/" + wf + "/"

	canonical := ns + "/" + wf

	return Identity{
		Namespace:          namespace,
		Workflow:           workflow,
		PostgresRole:       pgRole,
		PostgresSchema:     pgSchema,
		NATSSubjectPrefix:  natsSubject,
		NATSPrincipal:      natsPrincipal,
		RustFSPrefix:       rustfs,
		CanonicalPrincipal: canonical,
	}
}

// sanitizePostgres replaces hyphens and non-alphanumeric characters with
// underscores for Postgres identifier safety.
func sanitizePostgres(s string) string {
	// Replace hyphens with underscores first, then replace remaining
	// non-alphanumeric (excluding underscore) characters.
	s = strings.ReplaceAll(s, "-", "_")
	return nonAlphanumericOrUnderscore.ReplaceAllString(s, "_")
}

// natsUnsafe matches characters that are not safe in NATS subject tokens.
// NATS subjects use dots as hierarchy separators and > / * as wildcards,
// so those characters (plus spaces) must be replaced.
var natsUnsafe = regexp.MustCompile(`[^a-z0-9_]`)

// sanitizeNATS replaces hyphens with underscores and strips all other
// non-alphanumeric characters (dots, wildcards, spaces) to produce a
// safe NATS subject token.
func sanitizeNATS(s string) string {
	s = strings.ReplaceAll(s, "-", "_")
	return natsUnsafe.ReplaceAllString(s, "_")
}

// truncatePostgresID ensures a Postgres identifier does not exceed
// maxPostgresIDLen (63) characters. If it does, it truncates and appends
// a short hash suffix to maintain uniqueness.
func truncatePostgresID(id string) string {
	if len(id) <= maxPostgresIDLen {
		return id
	}
	h := sha256.Sum256([]byte(id))
	suffix := fmt.Sprintf("_%x", h[:4])
	return id[:maxPostgresIDLen-len(suffix)] + suffix
}
