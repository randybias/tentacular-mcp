package exoskeleton

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/nats-io/nats.go"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// NATSCreds holds the connection details returned after registering
// a tentacle with NATS.
type NATSCreds struct {
	URL           string `json:"url"`
	Token         string `json:"token,omitempty"`
	SubjectPrefix string `json:"subject_prefix"`
	Protocol      string `json:"protocol"`
	AuthMethod    string `json:"auth_method"` // "spiffe" or "token"
}

// natsAuthzEntry represents a single user entry in the NATS authorization
// configuration. Used for structured manipulation of the ConfigMap data.
type natsAuthzEntry struct {
	User           string // SPIFFE URI
	PublishAllow   []string
	SubscribeAllow []string
}

// NATSRegistrar manages per-tentacle NATS access. It supports two modes:
//   - Token mode: shared token, no ConfigMap management (Phase 1 fallback)
//   - SPIFFE mode: manages a ConfigMap containing NATS authorization entries
//     that map SPIFFE URIs to scoped publish/subscribe permissions
type NATSRegistrar struct {
	clientset kubernetes.Interface
	cfg       NATSConfig
}

// NewNATSRegistrar creates a new NATS registrar. In token mode it validates
// connectivity to NATS. In SPIFFE mode it validates the Kubernetes clientset
// is available (connectivity to NATS is via mTLS, not tested here).
func NewNATSRegistrar(ctx context.Context, cfg NATSConfig, clientset kubernetes.Interface) (*NATSRegistrar, error) {
	if cfg.SPIFFEEnabled {
		if clientset == nil {
			return nil, errors.New("nats spiffe mode requires a kubernetes clientset")
		}
		slog.Info("nats: SPIFFE mode enabled",
			"authzConfigMap", cfg.AuthzConfigMap,
			"authzNamespace", cfg.AuthzNamespace)
		return &NATSRegistrar{clientset: clientset, cfg: cfg}, nil
	}

	// Token mode: validate connectivity.
	opts := []nats.Option{nats.Name("tentacular-mcp-exoskeleton")}
	if cfg.Token != "" {
		opts = append(opts, nats.Token(cfg.Token))
	}
	nc, err := nats.Connect(cfg.URL, opts...)
	if err != nil {
		return nil, fmt.Errorf("nats connect: %w", err)
	}
	nc.Close()
	slog.Info("nats: connectivity validated (token mode)", "url", cfg.URL)
	return &NATSRegistrar{clientset: clientset, cfg: cfg}, nil
}

// Register returns NATS credentials scoped by subject prefix for the
// given identity. In SPIFFE mode, it also manages the authorization
// ConfigMap entry. This is idempotent.
func (r *NATSRegistrar) Register(ctx context.Context, id Identity) (*NATSCreds, error) {
	if r.cfg.SPIFFEEnabled {
		return r.registerSPIFFE(ctx, id)
	}
	return r.registerToken(id)
}

// registerToken returns NATS credentials using the shared token model.
func (r *NATSRegistrar) registerToken(id Identity) (*NATSCreds, error) {
	slog.Info("nats: registered tentacle (token mode)", "user", id.NATSUser, "prefix", id.NATSPrefix)
	return &NATSCreds{
		URL:           r.cfg.URL,
		Token:         r.cfg.Token,
		SubjectPrefix: id.NATSPrefix,
		Protocol:      "nats",
		AuthMethod:    "token",
	}, nil
}

// registerSPIFFE adds/updates an authorization entry in the NATS ConfigMap
// and returns credentials without a token (auth via SVID certificate).
func (r *NATSRegistrar) registerSPIFFE(ctx context.Context, id Identity) (*NATSCreds, error) {
	if err := r.upsertAuthzEntry(ctx, id); err != nil {
		return nil, fmt.Errorf("nats spiffe register: %w", err)
	}

	slog.Info("nats: registered tentacle (spiffe mode)",
		"principal", id.Principal, "prefix", id.NATSPrefix)

	return &NATSCreds{
		URL:           r.cfg.URL,
		SubjectPrefix: id.NATSPrefix,
		Protocol:      "nats",
		AuthMethod:    "spiffe",
	}, nil
}

// Unregister removes the tentacle's NATS access. In SPIFFE mode, removes
// the authorization entry from the ConfigMap. In token mode, this is a no-op.
func (r *NATSRegistrar) Unregister(ctx context.Context, id Identity) error {
	if r.cfg.SPIFFEEnabled {
		return r.unregisterSPIFFE(ctx, id)
	}
	slog.Info("nats: unregistered tentacle (no-op in token mode)", "user", id.NATSUser)
	return nil
}

// unregisterSPIFFE removes the authorization entry from the ConfigMap.
func (r *NATSRegistrar) unregisterSPIFFE(ctx context.Context, id Identity) error {
	cm, err := r.clientset.CoreV1().ConfigMaps(r.cfg.AuthzNamespace).Get(
		ctx, r.cfg.AuthzConfigMap, metav1.GetOptions{})
	if err != nil {
		if k8serr.IsNotFound(err) {
			slog.Warn("nats: authz ConfigMap not found during unregister, nothing to remove",
				"configmap", r.cfg.AuthzConfigMap, "namespace", r.cfg.AuthzNamespace)
			return nil
		}
		return fmt.Errorf("get authz configmap: %w", err)
	}

	entries := parseAuthzConfig(cm.Data["authorization.conf"])
	newEntries := make([]natsAuthzEntry, 0, len(entries))
	for _, e := range entries {
		if e.User != id.Principal {
			newEntries = append(newEntries, e)
		}
	}

	if len(newEntries) == len(entries) {
		slog.Info("nats: tentacle not found in authz config, nothing to remove",
			"principal", id.Principal)
		return nil
	}

	cm.Data["authorization.conf"] = renderAuthzConfig(newEntries)
	if _, err := r.clientset.CoreV1().ConfigMaps(r.cfg.AuthzNamespace).Update(
		ctx, cm, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update authz configmap: %w", err)
	}

	slog.Info("nats: removed tentacle from authz config (spiffe mode)",
		"principal", id.Principal)
	return nil
}

// upsertAuthzEntry adds or updates an authorization entry in the ConfigMap.
// Uses optimistic concurrency via resourceVersion.
func (r *NATSRegistrar) upsertAuthzEntry(ctx context.Context, id Identity) error {
	cmClient := r.clientset.CoreV1().ConfigMaps(r.cfg.AuthzNamespace)

	cm, err := cmClient.Get(ctx, r.cfg.AuthzConfigMap, metav1.GetOptions{})
	if err != nil {
		if !k8serr.IsNotFound(err) {
			return fmt.Errorf("get authz configmap: %w", err)
		}
		// Create the ConfigMap.
		entry := buildAuthzEntry(id)
		cm = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      r.cfg.AuthzConfigMap,
				Namespace: r.cfg.AuthzNamespace,
				Labels: map[string]string{
					"app.kubernetes.io/managed-by": "tentacular",
					"tentacular.io/exoskeleton":    "true",
				},
			},
			Data: map[string]string{
				"authorization.conf": renderAuthzConfig([]natsAuthzEntry{entry}),
			},
		}
		if _, err := cmClient.Create(ctx, cm, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("create authz configmap: %w", err)
		}
		return nil
	}

	// Update existing ConfigMap.
	entries := parseAuthzConfig(cm.Data["authorization.conf"])
	entry := buildAuthzEntry(id)

	// Replace existing entry or append.
	found := false
	for i, e := range entries {
		if e.User == id.Principal {
			entries[i] = entry
			found = true
			break
		}
	}
	if !found {
		entries = append(entries, entry)
	}

	if cm.Data == nil {
		cm.Data = make(map[string]string)
	}
	cm.Data["authorization.conf"] = renderAuthzConfig(entries)

	// Update with resourceVersion for optimistic concurrency.
	if _, err := cmClient.Update(ctx, cm, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update authz configmap: %w", err)
	}
	return nil
}

// buildAuthzEntry creates an natsAuthzEntry for the given identity.
func buildAuthzEntry(id Identity) natsAuthzEntry {
	return natsAuthzEntry{
		User:           id.Principal,
		PublishAllow:   []string{id.NATSPrefix},
		SubscribeAllow: []string{id.NATSPrefix},
	}
}

// parseAuthzConfig parses the NATS authorization configuration text into
// structured entries. The format is a simplified NATS config with user blocks.
func parseAuthzConfig(conf string) []natsAuthzEntry {
	if conf == "" {
		return nil
	}

	var entries []natsAuthzEntry
	lines := strings.Split(conf, "\n")

	var current *natsAuthzEntry
	var inPublish, inSubscribe bool

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "user = ") {
			if current != nil {
				entries = append(entries, *current)
			}
			user := strings.Trim(strings.TrimPrefix(trimmed, "user = "), "\"")
			current = &natsAuthzEntry{User: user}
			inPublish = false
			inSubscribe = false
			continue
		}

		if current == nil {
			continue
		}

		if strings.HasPrefix(trimmed, "publish") && strings.Contains(trimmed, "{") {
			inPublish = true
			inSubscribe = false
			continue
		}
		if strings.HasPrefix(trimmed, "subscribe") && strings.Contains(trimmed, "{") {
			inSubscribe = true
			inPublish = false
			continue
		}

		if strings.HasPrefix(trimmed, "allow = [") {
			// Extract subjects from allow = ["subj1", "subj2"]
			subjects := parseAllowList(trimmed)
			if inPublish {
				current.PublishAllow = subjects
			} else if inSubscribe {
				current.SubscribeAllow = subjects
			}
			continue
		}

		// Close of a permissions or publish/subscribe block.
		if trimmed == "}" {
			if inPublish || inSubscribe {
				inPublish = false
				inSubscribe = false
			}
		}
	}

	if current != nil {
		entries = append(entries, *current)
	}

	return entries
}

// parseAllowList extracts subject strings from a line like:
// allow = ["tentacular.tent-dev.hn-digest.>"]
func parseAllowList(line string) []string {
	start := strings.Index(line, "[")
	end := strings.LastIndex(line, "]")
	if start < 0 || end < 0 || end <= start {
		return nil
	}

	inner := line[start+1 : end]
	parts := strings.Split(inner, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		p = strings.Trim(p, "\"")
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// renderAuthzConfig generates the NATS authorization configuration text
// from structured entries. Entries are sorted by user for deterministic output.
func renderAuthzConfig(entries []natsAuthzEntry) string {
	// Sort entries by user for deterministic output.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].User < entries[j].User
	})

	var b strings.Builder
	b.WriteString("authorization {\n")
	b.WriteString("  users = [\n")

	for i, e := range entries {
		if i > 0 {
			b.WriteString(",\n")
		}
		b.WriteString("    {\n")
		fmt.Fprintf(&b, "      user = %q\n", e.User)
		b.WriteString("      permissions = {\n")

		b.WriteString("        publish = {\n")
		fmt.Fprintf(&b, "          allow = [%s]\n", quoteSubjects(e.PublishAllow))
		b.WriteString("        }\n")

		b.WriteString("        subscribe = {\n")
		fmt.Fprintf(&b, "          allow = [%s]\n", quoteSubjects(e.SubscribeAllow))
		b.WriteString("        }\n")

		b.WriteString("      }\n")
		b.WriteString("    }")
	}

	b.WriteString("\n  ]\n")
	b.WriteString("}\n")

	return b.String()
}

// quoteSubjects formats a slice of subject strings for NATS config.
func quoteSubjects(subjects []string) string {
	quoted := make([]string, len(subjects))
	for i, s := range subjects {
		quoted[i] = fmt.Sprintf("%q", s)
	}
	return strings.Join(quoted, ", ")
}

// Close is a no-op since we don't hold a persistent connection.
func (*NATSRegistrar) Close() {}

// EnsureEnclave provisions the enclave-level NATS account if it does not
// already exist. Phase 0 stub: no-op placeholder. Full implementation in Phase 1.
func (*NATSRegistrar) EnsureEnclave(_ context.Context, id EnclaveIdentity) error {
	slog.Info("exoskeleton: nats EnsureEnclave (stub)", "enclave", id.Enclave, "account", id.NATSAcct)
	return nil
}

// CleanupEnclave removes the enclave-level NATS account.
// Phase 0 stub: no-op placeholder. Full implementation in Phase 1.
func (*NATSRegistrar) CleanupEnclave(_ context.Context, id EnclaveIdentity) error {
	slog.Info("exoskeleton: nats CleanupEnclave (stub)", "enclave", id.Enclave, "account", id.NATSAcct)
	return nil
}
