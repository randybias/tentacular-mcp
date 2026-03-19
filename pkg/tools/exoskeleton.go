package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

func registerExoskeletonTools(srv *mcp.Server, client *k8s.Client, ctrl *exoskeleton.Controller) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "exo_status",
		Description: "Return exoskeleton feature status including which backing services are available.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Exoskeleton Service Status",
			ReadOnlyHint:    true,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, params ExoStatusParams) (*mcp.CallToolResult, ExoStatusResult, error) {
		if ctrl == nil {
			return nil, ExoStatusResult{Enabled: false, Services: []ExoStatusServiceInfo{}}, nil
		}
		services := buildServiceInfoList(ctrl)
		return nil, ExoStatusResult{
			Enabled:           ctrl.Enabled(),
			CleanupOnUndeploy: ctrl.CleanupOnUndeploy(),
			PostgresAvailable: ctrl.PostgresAvailable(),
			NATSAvailable:     ctrl.NATSAvailable(),
			RustFSAvailable:   ctrl.RustFSAvailable(),
			SPIREAvailable:    ctrl.SPIREAvailable(),
			NATSSpiffeEnabled: ctrl.NATSSpiffeEnabled(),
			AuthEnabled:       ctrl.AuthEnabled(),
			AuthIssuer:        ctrl.AuthIssuer(),
			Services:          services,
		}, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "exo_registration",
		Description: "Return exoskeleton registration details (Secret contents) for a workflow deployment.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Workflow Exo Registration",
			ReadOnlyHint:    true,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, params ExoRegistrationParams) (*mcp.CallToolResult, ExoRegistrationResult, error) {
		if err := guard.CheckNamespace(params.Namespace); err != nil {
			return nil, ExoRegistrationResult{}, err
		}
		if err := guard.CheckName(params.Name); err != nil {
			return nil, ExoRegistrationResult{}, err
		}

		secretName := exoskeleton.ExoskeletonSecretPrefix + params.Name
		secret, err := client.Clientset.CoreV1().Secrets(params.Namespace).Get(ctx, secretName, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return nil, ExoRegistrationResult{
					Found:     false,
					Namespace: params.Namespace,
					Name:      params.Name,
				}, nil
			}
			return nil, ExoRegistrationResult{}, fmt.Errorf("get exoskeleton secret: %w", err)
		}

		data := make(map[string]string)
		for k, v := range secret.Data {
			// Redact password and secret key values.
			if isSecretKey(k) {
				data[k] = "***REDACTED***"
			} else {
				data[k] = string(v)
			}
		}

		return nil, ExoRegistrationResult{
			Found:     true,
			Namespace: params.Namespace,
			Name:      params.Name,
			Data:      data,
		}, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "exo_list",
		Description: "List all workflows with exoskeleton registrations by scanning Secrets with the exoskeleton label across all namespaces.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "List Exo Registrations",
			ReadOnlyHint:    true,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, params ExoListParams) (*mcp.CallToolResult, ExoListResult, error) {
		result, err := handleExoList(ctx, client)
		return nil, result, err
	})
}

// buildServiceInfoList returns a slice of ExoStatusServiceInfo for each
// backing service, using the controller's status accessors.
func buildServiceInfoList(ctrl *exoskeleton.Controller) []ExoStatusServiceInfo {
	if !ctrl.Enabled() {
		return []ExoStatusServiceInfo{}
	}
	return []ExoStatusServiceInfo{
		{
			Name:    "postgres",
			Enabled: ctrl.PostgresAvailable(),
		},
		{
			Name:    "nats",
			Enabled: ctrl.NATSAvailable(),
		},
		{
			Name:    "rustfs",
			Enabled: ctrl.RustFSAvailable(),
		},
		{
			Name:    "spire",
			Enabled: ctrl.SPIREAvailable(),
		},
	}
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
