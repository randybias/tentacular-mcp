package exoskeleton

import (
	"fmt"
	"log/slog"
	"net/url"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// enrichContractDeps updates the workflow.yaml inside the ConfigMap manifest
// with resolved host/port/database/user/subject/container/auth fields from
// the registered credentials. It preserves all other fields in the workflow
// by using a generic map[string]any for round-trip parse/serialize.
func enrichContractDeps(manifests []map[string]any, creds map[string]any) error {
	for _, m := range manifests {
		obj := &unstructured.Unstructured{Object: m}
		if obj.GetKind() != "ConfigMap" {
			continue
		}

		data, ok, _ := unstructured.NestedStringMap(obj.Object, "data")
		if !ok {
			continue
		}
		wfYAML, ok := data["workflow.yaml"]
		if !ok {
			continue
		}

		// Parse into generic map to preserve all fields.
		var workflow map[string]any
		if err := yaml.Unmarshal([]byte(wfYAML), &workflow); err != nil {
			slog.Warn("exoskeleton: failed to parse workflow.yaml in ConfigMap", "error", err)
			continue
		}

		contractRaw, ok := workflow["contract"]
		if !ok {
			continue
		}
		contract, ok := contractRaw.(map[string]any)
		if !ok {
			continue
		}
		depsRaw, ok := contract["dependencies"]
		if !ok {
			continue
		}
		deps, ok := depsRaw.(map[string]any)
		if !ok {
			continue
		}

		modified := false
		for depName, depVal := range deps {
			if !strings.HasPrefix(depName, "tentacular-") {
				continue
			}
			credVal, hasCred := creds[depName]
			if !hasCred {
				continue
			}

			depMap, ok := depVal.(map[string]any)
			if !ok {
				continue
			}

			switch c := credVal.(type) {
			case *PostgresCreds:
				depMap["host"] = c.Host
				depMap["port"] = c.Port
				depMap["database"] = c.Database
				depMap["user"] = c.User
				depMap["schema"] = c.Schema
				modified = true

			case *NATSCreds:
				host, port := parseHostPort(c.URL)
				depMap["host"] = host
				depMap["port"] = port
				depMap["subject"] = c.SubjectPrefix
				modified = true

			case *RustFSCreds:
				host, port := parseHostPort(c.Endpoint)
				depMap["host"] = host
				depMap["port"] = port
				depMap["container"] = c.Bucket
				depMap["prefix"] = c.Prefix
				modified = true
			}
		}

		if !modified {
			continue
		}

		// Re-serialize workflow.yaml and update the ConfigMap in place.
		enriched, err := yaml.Marshal(workflow)
		if err != nil {
			return fmt.Errorf("re-serialize workflow.yaml: %w", err)
		}

		// Update the data map in the manifest directly.
		dataMap, _, _ := unstructured.NestedMap(obj.Object, "data")
		if dataMap == nil {
			dataMap = make(map[string]any)
		}
		dataMap["workflow.yaml"] = string(enriched)
		if err := unstructured.SetNestedField(obj.Object, dataMap, "data"); err != nil {
			return fmt.Errorf("update ConfigMap data: %w", err)
		}

		slog.Info("exoskeleton: enriched contract dependencies in ConfigMap")
		return nil
	}
	return nil
}

// patchDeploymentAllowNet scans manifests for a Deployment and appends
// exoskeleton service hosts to any --allow-net=... flag in the first
// container's args or command. Only adds hosts for services that have creds.
func patchDeploymentAllowNet(manifests []map[string]any, creds map[string]any) {
	hosts := collectExoHosts(creds)
	if len(hosts) == 0 {
		return
	}

	for _, m := range manifests {
		obj := &unstructured.Unstructured{Object: m}
		if obj.GetKind() != "Deployment" {
			continue
		}

		// Walk into spec.template.spec.containers[0]
		containers, found, _ := unstructured.NestedSlice(obj.Object,
			"spec", "template", "spec", "containers")
		if !found || len(containers) == 0 {
			continue
		}

		container, ok := containers[0].(map[string]any)
		if !ok {
			continue
		}

		// Try patching args first, then command.
		if patchAllowNetInSlice(container, "args", hosts) {
			containers[0] = container
			_ = unstructured.SetNestedSlice(obj.Object, containers,
				"spec", "template", "spec", "containers")
			slog.Info("exoskeleton: patched Deployment --allow-net in args")
			return
		}
		if patchAllowNetInSlice(container, "command", hosts) {
			containers[0] = container
			_ = unstructured.SetNestedSlice(obj.Object, containers,
				"spec", "template", "spec", "containers")
			slog.Info("exoskeleton: patched Deployment --allow-net in command")
			return
		}
	}
}

// patchAllowNetInSlice finds a --allow-net=... entry in the named string
// slice field and appends the given hosts. Returns true if patching occurred.
func patchAllowNetInSlice(container map[string]any, field string, hosts []string) bool {
	rawSlice, ok := container[field]
	if !ok {
		return false
	}
	slice, ok := toStringSlice(rawSlice)
	if !ok {
		return false
	}

	for i, arg := range slice {
		if !strings.HasPrefix(arg, "--allow-net=") {
			continue
		}
		// Append hosts to the existing value.
		existing := strings.TrimPrefix(arg, "--allow-net=")
		if existing == "" || existing == "none" {
			slice[i] = "--allow-net=" + strings.Join(hosts, ",")
		} else {
			slice[i] = arg + "," + strings.Join(hosts, ",")
		}
		// Convert back to []any for unstructured.
		result := make([]any, len(slice))
		for j, s := range slice {
			result[j] = s
		}
		container[field] = result
		return true
	}
	return false
}

// toStringSlice converts an any (expected []any of strings)
// to a []string.
func toStringSlice(v any) ([]string, bool) {
	raw, ok := v.([]any)
	if !ok {
		return nil, false
	}
	result := make([]string, len(raw))
	for i, item := range raw {
		s, ok := item.(string)
		if !ok {
			return nil, false
		}
		result[i] = s
	}
	return result, true
}

// collectExoHosts returns host:port strings for each registered service.
// The result is sorted for deterministic output.
func collectExoHosts(creds map[string]any) []string {
	var hosts []string
	for name, c := range creds {
		switch v := c.(type) {
		case *PostgresCreds:
			hosts = append(hosts, v.Host+":"+v.Port)
		case *NATSCreds:
			h, p := parseHostPort(v.URL)
			if h != "" && p != "" {
				hosts = append(hosts, h+":"+p)
			}
		case *RustFSCreds:
			h, p := parseHostPort(v.Endpoint)
			if h != "" && p != "" {
				hosts = append(hosts, h+":"+p)
			}
		default:
			slog.Warn("exoskeleton: unknown cred type for allow-net", "dep", name)
		}
	}
	sort.Strings(hosts)
	return hosts
}

// parseHostPort extracts host and port from a URL string.
// For "nats://host:4222" returns ("host", "4222").
// For "http://host:9000" returns ("host", "9000").
// For "host:5432" returns ("host", "5432").
func parseHostPort(rawURL string) (host, port string) {
	// Try parsing as a full URL first.
	u, err := url.Parse(rawURL)
	if err == nil && u.Host != "" {
		host = u.Hostname()
		port = u.Port()
		return host, port
	}
	// Fallback: treat as host:port.
	if idx := strings.LastIndex(rawURL, ":"); idx > 0 {
		return rawURL[:idx], rawURL[idx+1:]
	}
	return rawURL, ""
}

// AnnotateDeployerParams holds the parameters for AnnotateDeployer.
type AnnotateDeployerParams struct {
	ExistingAnnotations map[string]string
	Group               string
	Mode                string
	Deployer            DeployerInfo
	IsUpdate            bool
}

// AnnotateDeployer adds provenance and ownership annotations to each Deployment manifest.
func (*Controller) AnnotateDeployer(manifests []map[string]any, p AnnotateDeployerParams) []map[string]any {
	now := time.Now().UTC().Format(time.RFC3339)
	deployedBy := p.Deployer.Email
	if deployedBy == "" {
		deployedBy = "bearer-token"
	}
	deployedVia := p.Deployer.AgentType
	if deployedVia == "" {
		deployedVia = "unknown"
	}

	// NOTE: Annotation keys are hardcoded here rather than using authz.Annotation*
	// constants to avoid a circular import (authz imports exoskeleton for DeployerInfo).
	// These keys MUST stay in sync with pkg/authz/annotations.go.
	for _, m := range manifests {
		obj := &unstructured.Unstructured{Object: m}
		if obj.GetKind() != "Deployment" {
			continue
		}
		ann := obj.GetAnnotations()
		if ann == nil {
			ann = make(map[string]string)
		}
		// Provenance annotations (always stamped on create and update).
		ann["tentacular.io/deployed-by"] = deployedBy
		ann["tentacular.io/deployed-via"] = deployedVia
		ann["tentacular.io/deployed-at"] = now
		ann["tentacular.io/auth-provider"] = p.Deployer.Provider

		if p.IsUpdate {
			// Carry forward ownership annotations from existing deployment.
			ownershipKeys := []string{
				"tentacular.io/owner-sub", "tentacular.io/owner-email",
				"tentacular.io/owner-name", "tentacular.io/group",
				"tentacular.io/mode", "tentacular.io/created-at",
			}
			for _, k := range ownershipKeys {
				if v, ok := p.ExistingAnnotations[k]; ok && v != "" {
					ann[k] = v
				}
			}
			// Allow explicit group/mode params to override on update, but only if
			// the caller is the owner or bearer-token. This prevents group members
			// with Write access from changing permissions via wf_apply --group/--share.
			isOwnerOrBearer := p.Deployer.Provider == "bearer-token" ||
				p.ExistingAnnotations["tentacular.io/owner-sub"] == "" ||
				(p.Deployer.Subject != "" && p.Deployer.Subject == p.ExistingAnnotations["tentacular.io/owner-sub"])
			if isOwnerOrBearer {
				if p.Group != "" {
					ann["tentacular.io/group"] = p.Group
				}
				if p.Mode != "" {
					ann["tentacular.io/mode"] = p.Mode
				}
			}
			// Audit trail: record who last updated and when.
			ann["tentacular.io/updated-at"] = now
			ann["tentacular.io/updated-by-sub"] = p.Deployer.Subject
			ann["tentacular.io/updated-by-email"] = p.Deployer.Email
		} else {
			// CREATE: stamp ownership and creation time.
			ann["tentacular.io/created-at"] = now
			// Only stamp owner-sub if non-empty to prevent matching empty-subject callers.
			// Bearer-token deploys intentionally leave owner-sub empty (pre-authz resource).
			if p.Deployer.Subject != "" {
				ann["tentacular.io/owner-sub"] = p.Deployer.Subject
			}
			ann["tentacular.io/owner-email"] = p.Deployer.Email
			ann["tentacular.io/owner-name"] = p.Deployer.DisplayName
			ann["tentacular.io/group"] = p.Group
			ann["tentacular.io/mode"] = p.Mode
		}

		obj.SetAnnotations(ann)
	}
	return manifests
}
