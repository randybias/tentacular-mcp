package tools

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/randybias/tentacular-mcp/pkg/exoskeleton"
	"github.com/randybias/tentacular-mcp/pkg/guard"
	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

// ExoStatusParams are the parameters for exo_status (none required).
type ExoStatusParams struct{}

// ExoStatusServiceInfo describes a single exoskeleton service's status.
type ExoStatusServiceInfo struct {
	Name    string `json:"name"`
	Detail  string `json:"detail,omitempty"`
	Enabled bool   `json:"enabled"`
}

// ExoStatusResult is the result of exo_status.
type ExoStatusResult struct {
	AuthIssuer        string                 `json:"auth_issuer,omitempty"`
	Services          []ExoStatusServiceInfo `json:"services"`
	Enabled           bool                   `json:"enabled"`
	CleanupOnUndeploy bool                   `json:"cleanup_on_undeploy"`
	PostgresAvailable bool                   `json:"postgres_available"`
	NATSAvailable     bool                   `json:"nats_available"`
	RustFSAvailable   bool                   `json:"rustfs_available"`
	SPIREAvailable    bool                   `json:"spire_available"`
	NATSSpiffeEnabled bool                   `json:"nats_spiffe_enabled"`
	AuthEnabled       bool                   `json:"auth_enabled"`
}

// ExoRegistrationParams are the parameters for exo_registration.
type ExoRegistrationParams struct {
	Namespace string `json:"namespace" jsonschema:"Namespace of the workflow"`
	Name      string `json:"name" jsonschema:"Workflow deployment name"`
}

// ExoRegistrationResult is the result of exo_registration.
type ExoRegistrationResult struct {
	Data      map[string]string `json:"data,omitempty"`
	Namespace string            `json:"namespace"`
	Name      string            `json:"name"`
	Found     bool              `json:"found"`
}

// ExoListParams are the parameters for exo_list (none required).
type ExoListParams struct{}

// ExoListEntry is a single workflow registration in the list.
type ExoListEntry struct {
	Namespace string   `json:"namespace"`
	Workflow  string   `json:"workflow"`
	Created   string   `json:"created"`
	Services  []string `json:"services,omitempty"`
}

// ExoListResult is the result of exo_list.
type ExoListResult struct {
	Registrations []ExoListEntry `json:"registrations"`
}

// buildServiceInfoList returns a slice of ExoStatusServiceInfo for each
// backing service, derived from the controller's ServiceInfo to avoid
// duplicating the service inventory.
func buildServiceInfoList(ctrl *exoskeleton.Controller) []ExoStatusServiceInfo {
	info := ctrl.ServiceInfo()
	if info == nil {
		return []ExoStatusServiceInfo{}
	}
	result := make([]ExoStatusServiceInfo, len(info.Services))
	for i, svc := range info.Services {
		result[i] = ExoStatusServiceInfo{
			Name:    svc.Name,
			Enabled: svc.Available,
		}
	}
	return result
}

// handleExoList scans for Secrets labeled with the exoskeleton label
// across all namespaces and returns registration entries.
func handleExoList(ctx context.Context, client *k8s.Client) (ExoListResult, error) {
	labelSelector := exoskeleton.ExoskeletonLabel + "=true"

	secretList, err := client.Clientset.CoreV1().Secrets("").List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return ExoListResult{}, fmt.Errorf("list exoskeleton secrets: %w", err)
	}

	entries := make([]ExoListEntry, 0, len(secretList.Items))
	for _, s := range secretList.Items {
		if guard.IsSystemNamespace(s.Namespace) {
			continue
		}
		workflow := s.Labels[exoskeleton.ReleaseLabel]
		created := ""
		if !s.CreationTimestamp.IsZero() {
			created = s.CreationTimestamp.Format("2006-01-02T15:04:05Z")
		}
		services := detectRegisteredServices(s.Data)
		entries = append(entries, ExoListEntry{
			Namespace: s.Namespace,
			Workflow:  workflow,
			Created:   created,
			Services:  services,
		})
	}

	return ExoListResult{
		Registrations: entries,
	}, nil
}

// detectRegisteredServices inspects Secret data key prefixes to determine
// which services have credentials registered.
func detectRegisteredServices(data map[string][]byte) []string {
	seen := map[string]bool{}
	for key := range data {
		switch {
		case strings.HasPrefix(key, "tentacular-postgres."):
			seen["postgres"] = true
		case strings.HasPrefix(key, "tentacular-nats."):
			seen["nats"] = true
		case strings.HasPrefix(key, "tentacular-rustfs."):
			seen["rustfs"] = true
		}
	}
	var services []string
	// Return in deterministic order.
	for _, svc := range []string{"postgres", "nats", "rustfs"} {
		if seen[svc] {
			services = append(services, svc)
		}
	}
	return services
}

// isSecretKey returns true for keys that contain sensitive values.
func isSecretKey(key string) bool {
	sensitiveSuffixes := []string{".password", ".secret_key", ".access_key", ".token"}
	for _, suffix := range sensitiveSuffixes {
		if strings.HasSuffix(key, suffix) {
			return true
		}
	}
	return false
}
