package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/randybias/tentacular-mcp/pkg/exoskeleton"
	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

// ExoStatusParams are the parameters for exo_status (empty, cluster-scoped).
type ExoStatusParams struct{}

// ExoStatusServiceInfo describes a single exoskeleton service's status.
type ExoStatusServiceInfo struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Healthy bool   `json:"healthy"`
	Detail  string `json:"detail,omitempty"`
}

// ExoStatusResult is the result of exo_status.
type ExoStatusResult struct {
	Enabled  bool                   `json:"enabled"`
	Services []ExoStatusServiceInfo `json:"services"`
}

// ExoRegistrationParams are the parameters for exo_registration.
type ExoRegistrationParams struct {
	Namespace string `json:"namespace" jsonschema:"Namespace of the tentacle"`
	Workflow  string `json:"workflow" jsonschema:"Workflow name of the tentacle"`
}

// ExoRegistrationResult is the result of exo_registration.
type ExoRegistrationResult struct {
	Registered       bool   `json:"registered"`
	Namespace        string `json:"namespace"`
	Workflow         string `json:"workflow"`
	PostgresRole     string `json:"postgres_role,omitempty"`
	PostgresSchema   string `json:"postgres_schema,omitempty"`
	NATSSubjectPrefix string `json:"nats_subject_prefix,omitempty"`
	SecretName       string `json:"secret_name,omitempty"`
	SecretCreated    string `json:"secret_created,omitempty"`
}

// ExoListParams are the parameters for exo_list (empty, cluster-scoped).
type ExoListParams struct{}

// ExoListEntry is a single tentacle registration in the list.
type ExoListEntry struct {
	Namespace string `json:"namespace"`
	Workflow  string `json:"workflow"`
	Created   string `json:"created"`
}

// ExoListResult is the result of exo_list.
type ExoListResult struct {
	Registrations []ExoListEntry `json:"registrations"`
}

func registerExoskeletonTools(srv *mcp.Server, client *k8s.Client, exoCtrl *exoskeleton.ExoskeletonController) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "exo_status",
		Description: "Show exoskeleton subsystem status: which services are enabled and their connection health.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, params ExoStatusParams) (*mcp.CallToolResult, ExoStatusResult, error) {
		result, err := handleExoStatus(ctx, client, exoCtrl)
		return nil, result, err
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "exo_registration",
		Description: "Show exoskeleton registration details for a specific tentacle (Postgres role/schema, NATS subject prefix, credential Secret).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, params ExoRegistrationParams) (*mcp.CallToolResult, ExoRegistrationResult, error) {
		result, err := handleExoRegistration(ctx, client, exoCtrl, params)
		return nil, result, err
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "exo_list",
		Description: "List all tentacles with exoskeleton registrations by scanning Secrets with the exoskeleton label across all namespaces.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, params ExoListParams) (*mcp.CallToolResult, ExoListResult, error) {
		result, err := handleExoList(ctx, client, exoCtrl)
		return nil, result, err
	})
}

func handleExoStatus(_ context.Context, _ *k8s.Client, exoCtrl *exoskeleton.ExoskeletonController) (ExoStatusResult, error) {
	if exoCtrl == nil {
		return ExoStatusResult{
			Enabled:  false,
			Services: []ExoStatusServiceInfo{},
		}, nil
	}

	cfg := exoCtrl.Config()
	if cfg == nil || !cfg.Enabled {
		return ExoStatusResult{
			Enabled:  false,
			Services: []ExoStatusServiceInfo{},
		}, nil
	}

	services := []ExoStatusServiceInfo{
		{
			Name:    "postgres",
			Enabled: cfg.PostgresEnabled,
			Healthy: cfg.PostgresEnabled && cfg.Postgres.Host != "",
			Detail:  postgresDetail(cfg),
		},
		{
			Name:    "nats",
			Enabled: cfg.NATSEnabled,
			Healthy: cfg.NATSEnabled && cfg.NATS.URL != "",
			Detail:  natsDetail(cfg),
		},
		{
			Name:    "rustfs",
			Enabled: cfg.RustFSEnabled,
			Healthy: cfg.RustFSEnabled && cfg.RustFS.Endpoint != "",
			Detail:  rustfsDetail(cfg),
		},
	}

	return ExoStatusResult{
		Enabled:  true,
		Services: services,
	}, nil
}

func postgresDetail(cfg *exoskeleton.ExoskeletonConfig) string {
	if !cfg.PostgresEnabled {
		return "disabled"
	}
	if cfg.Postgres.Host == "" {
		return "enabled but host not configured"
	}
	return fmt.Sprintf("host=%s port=%s db=%s", cfg.Postgres.Host, cfg.Postgres.Port, cfg.Postgres.Database)
}

func natsDetail(cfg *exoskeleton.ExoskeletonConfig) string {
	if !cfg.NATSEnabled {
		return "disabled"
	}
	if cfg.NATS.URL == "" {
		return "enabled but URL not configured"
	}
	return fmt.Sprintf("url=%s", cfg.NATS.URL)
}

func rustfsDetail(cfg *exoskeleton.ExoskeletonConfig) string {
	if !cfg.RustFSEnabled {
		return "disabled"
	}
	if cfg.RustFS.Endpoint == "" {
		return "enabled but endpoint not configured"
	}
	return fmt.Sprintf("endpoint=%s bucket=%s", cfg.RustFS.Endpoint, cfg.RustFS.Bucket)
}

func handleExoRegistration(ctx context.Context, client *k8s.Client, exoCtrl *exoskeleton.ExoskeletonController, params ExoRegistrationParams) (ExoRegistrationResult, error) {
	if params.Namespace == "" || params.Workflow == "" {
		return ExoRegistrationResult{}, fmt.Errorf("namespace and workflow are required")
	}

	result := ExoRegistrationResult{
		Namespace: params.Namespace,
		Workflow:  params.Workflow,
	}

	// Compile identity to show the derived identifiers
	id := exoskeleton.CompileIdentity(params.Namespace, params.Workflow)
	result.PostgresRole = id.PostgresRole
	result.PostgresSchema = id.PostgresSchema
	result.NATSSubjectPrefix = id.NATSSubjectPrefix

	// Check if credential Secret exists
	secretName := exoskeleton.ExoskeletonSecretPrefix + params.Workflow
	result.SecretName = secretName

	secret, err := client.Clientset.CoreV1().Secrets(params.Namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		// Secret not found — not registered
		result.Registered = false
		return result, nil
	}

	result.Registered = true
	if !secret.CreationTimestamp.IsZero() {
		result.SecretCreated = secret.CreationTimestamp.Format("2006-01-02T15:04:05Z")
	}

	return result, nil
}

func handleExoList(ctx context.Context, client *k8s.Client, _ *exoskeleton.ExoskeletonController) (ExoListResult, error) {
	labelSelector := fmt.Sprintf("%s=true", exoskeleton.ExoskeletonLabel)

	secretList, err := client.Clientset.CoreV1().Secrets("").List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return ExoListResult{}, fmt.Errorf("list exoskeleton secrets: %w", err)
	}

	entries := make([]ExoListEntry, 0, len(secretList.Items))
	for _, s := range secretList.Items {
		workflow := s.Labels[exoskeleton.ReleaseLabel]
		created := ""
		if !s.CreationTimestamp.IsZero() {
			created = s.CreationTimestamp.Format("2006-01-02T15:04:05Z")
		}
		entries = append(entries, ExoListEntry{
			Namespace: s.Namespace,
			Workflow:  workflow,
			Created:   created,
		})
	}

	return ExoListResult{
		Registrations: entries,
	}, nil
}
