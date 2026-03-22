package exoskeleton

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Label and naming constants used across the exoskeleton subsystem.
const (
	// ExoskeletonLabel is the label key marking a Secret as exoskeleton-managed.
	ExoskeletonLabel = "tentacular.io/exoskeleton"

	// ReleaseLabel is the label key holding the workflow name.
	ReleaseLabel = "tentacular.io/release"

	// ExoskeletonSecretPrefix is the naming prefix for exoskeleton credential Secrets.
	ExoskeletonSecretPrefix = "tentacular-exoskeleton-"
)

// BuildSecretManifest constructs a Kubernetes Secret manifest containing
// exoskeleton credentials for a workflow deployment. Each enabled service
// gets a JSON-encoded entry keyed by service name.
//
// The returned manifest is an unstructured map suitable for inclusion in
// the wf_apply manifest list.
func BuildSecretManifest(namespace, workflow string, creds map[string]any) (map[string]any, error) {
	secretName := ExoskeletonSecretPrefix + workflow

	stringData := make(map[string]any)
	for svcName, svcCreds := range creds {
		// Marshal per-service creds to a flat key-value map.
		switch c := svcCreds.(type) {
		case *PostgresCreds:
			stringData[svcName+".host"] = c.Host
			stringData[svcName+".port"] = c.Port
			stringData[svcName+".database"] = c.Database
			stringData[svcName+".user"] = c.User
			stringData[svcName+".password"] = c.Password
			stringData[svcName+".schema"] = c.Schema
			stringData[svcName+".protocol"] = c.Protocol
		case *NATSCreds:
			stringData[svcName+".url"] = c.URL
			if c.Token != "" {
				stringData[svcName+".token"] = c.Token
			}
			stringData[svcName+".subject_prefix"] = c.SubjectPrefix
			stringData[svcName+".protocol"] = c.Protocol
			stringData[svcName+".auth_method"] = c.AuthMethod
		case *RustFSCreds:
			stringData[svcName+".endpoint"] = c.Endpoint
			stringData[svcName+".access_key"] = c.AccessKey
			stringData[svcName+".secret_key"] = c.SecretKey
			stringData[svcName+".bucket"] = c.Bucket
			stringData[svcName+".prefix"] = c.Prefix
			stringData[svcName+".region"] = c.Region
			stringData[svcName+".protocol"] = c.Protocol
		default:
			// Fallback: JSON-encode the entire value under a single key.
			b, err := json.Marshal(svcCreds)
			if err == nil {
				stringData[svcName] = string(b)
			}
		}
	}

	// Always include identity fields.
	id, err := CompileIdentity(namespace, workflow)
	if err != nil {
		return nil, fmt.Errorf("compile identity for secret: %w", err)
	}
	stringData["tentacular-identity.principal"] = id.Principal
	stringData["tentacular-identity.namespace"] = namespace
	stringData["tentacular-identity.workflow"] = workflow

	return map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]any{
			"name":      secretName,
			"namespace": namespace,
			"labels": map[string]any{
				ReleaseLabel:     workflow,
				ExoskeletonLabel: "true",
			},
		},
		"type":       "Opaque",
		"stringData": stringData,
	}, nil
}

// mergeExoCredsIntoUserSecret finds the user-provided Secret named
// "<workflow>-secrets" in the manifests list and merges the exoskeleton
// credential keys into its stringData. If no such Secret exists, one is
// created with just the exo keys. This ensures the engine can resolve
// credentials via ctx.dependency() without requiring a separate volume mount.
func mergeExoCredsIntoUserSecret(manifests []map[string]any, namespace, workflow string, creds map[string]any) ([]map[string]any, error) {
	userSecretName := workflow + "-secrets"

	// Build the flat key-value credential map (same format as BuildSecretManifest).
	exoData := buildExoStringData(namespace, workflow, creds)

	// Search for the existing user Secret in the manifests.
	for _, m := range manifests {
		obj := &unstructured.Unstructured{Object: m}
		if obj.GetKind() != "Secret" {
			continue
		}
		if obj.GetName() != userSecretName {
			continue
		}

		// Found the user Secret -- merge exo keys into its stringData.
		sd, _, _ := unstructured.NestedMap(obj.Object, "stringData")
		if sd == nil {
			sd = make(map[string]any)
		}
		for k, v := range exoData {
			sd[k] = v
		}
		if err := unstructured.SetNestedField(obj.Object, sd, "stringData"); err != nil {
			return nil, fmt.Errorf("set stringData on user secret: %w", err)
		}

		slog.Info("exoskeleton: merged credentials into user secret",
			"secret", userSecretName, "namespace", namespace, "keys", len(exoData))
		return manifests, nil
	}

	// No user Secret found -- create one with the exo keys.
	userSecret := map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]any{
			"name":      userSecretName,
			"namespace": namespace,
		},
		"type":       "Opaque",
		"stringData": exoData,
	}
	manifests = append(manifests, userSecret)

	slog.Info("exoskeleton: created user secret with exo credentials",
		"secret", userSecretName, "namespace", namespace, "keys", len(exoData))
	return manifests, nil
}

// buildExoStringData returns the credential map for the user secret.
// Each service becomes a single key with JSON-encoded value, matching
// the engine's loadSecretsFromDir which parses JSON content into nested
// objects: secrets["tentacular-postgres"]["password"].
func buildExoStringData(namespace, workflow string, creds map[string]any) map[string]any {
	sd := make(map[string]any)
	for svcName, svcCreds := range creds {
		var obj map[string]string
		switch c := svcCreds.(type) {
		case *PostgresCreds:
			obj = map[string]string{
				"host": c.Host, "port": c.Port, "database": c.Database,
				"user": c.User, "password": c.Password, "schema": c.Schema,
				"protocol": c.Protocol,
			}
		case *NATSCreds:
			obj = map[string]string{
				"url": c.URL, "subject_prefix": c.SubjectPrefix,
				"protocol": c.Protocol, "auth_method": c.AuthMethod,
			}
			if c.Token != "" {
				obj["token"] = c.Token
			}
		case *RustFSCreds:
			obj = map[string]string{
				"endpoint": c.Endpoint, "access_key": c.AccessKey,
				"secret_key": c.SecretKey, "bucket": c.Bucket,
				"prefix": c.Prefix, "region": c.Region, "protocol": c.Protocol,
			}
		default:
			b, err := json.Marshal(svcCreds)
			if err == nil {
				sd[svcName] = string(b)
			}
			continue
		}
		b, err := json.Marshal(obj)
		if err == nil {
			sd[svcName] = string(b)
		}
	}

	// Include identity fields as a JSON object.
	id, err := CompileIdentity(namespace, workflow)
	if err == nil {
		idObj := map[string]string{
			"principal": id.Principal,
			"namespace": namespace,
			"workflow":  workflow,
		}
		b, err := json.Marshal(idObj)
		if err == nil {
			sd["tentacular-identity"] = string(b)
		}
	}

	return sd
}
