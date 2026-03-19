package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/randybias/tentacular-mcp/pkg/guard"
	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

// CredIssueTokenParams are the parameters for cred_issue_token.
type CredIssueTokenParams struct {
	Namespace  string `json:"namespace" jsonschema:"Namespace to issue the token for"`
	TTLMinutes int    `json:"ttl_minutes" jsonschema:"Token lifetime in minutes (10-1440)"`
}

// CredIssueTokenResult is the result of cred_issue_token.
type CredIssueTokenResult struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
}

// CredKubeconfigParams are the parameters for cred_kubeconfig.
type CredKubeconfigParams struct {
	Namespace  string `json:"namespace" jsonschema:"Namespace for the kubeconfig context"`
	TTLMinutes int    `json:"ttl_minutes" jsonschema:"Token lifetime in minutes (10-1440)"`
}

// CredKubeconfigResult is the result of cred_kubeconfig.
type CredKubeconfigResult struct {
	Kubeconfig string `json:"kubeconfig"`
}

// CredRotateParams are the parameters for cred_rotate.
type CredRotateParams struct {
	Namespace string `json:"namespace" jsonschema:"Namespace whose workflow service account should be rotated"`
}

// CredRotateResult is the result of cred_rotate.
type CredRotateResult struct {
	Namespace string `json:"namespace"`
	Message   string `json:"message"`
	Rotated   bool   `json:"rotated"`
}

func registerCredentialTools(srv *mcp.Server, client *k8s.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "cred_issue_token",
		Description: "Issue a short-lived token for the tentacular-workflow service account in a namespace.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Issue Service Account Token",
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  false,
			OpenWorldHint:   boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, params CredIssueTokenParams) (*mcp.CallToolResult, CredIssueTokenResult, error) {
		if err := guard.CheckNamespace(params.Namespace); err != nil {
			return nil, CredIssueTokenResult{}, err
		}
		result, err := handleCredIssueToken(ctx, client, params)
		return nil, result, err
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "cred_kubeconfig",
		Description: "Generate a kubeconfig for the tentacular-workflow service account in a namespace.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Generate Scoped Kubeconfig",
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  false,
			OpenWorldHint:   boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, params CredKubeconfigParams) (*mcp.CallToolResult, CredKubeconfigResult, error) {
		if err := guard.CheckNamespace(params.Namespace); err != nil {
			return nil, CredKubeconfigResult{}, err
		}
		result, err := handleCredKubeconfig(ctx, client, params)
		return nil, result, err
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "cred_rotate",
		Description: "Rotate the workflow service account in a namespace by recreating it. Invalidates all existing tokens for the service account.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Rotate Service Account",
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(true),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, params CredRotateParams) (*mcp.CallToolResult, CredRotateResult, error) {
		if err := guard.CheckNamespace(params.Namespace); err != nil {
			return nil, CredRotateResult{}, err
		}
		result, err := handleCredRotate(ctx, client, params)
		return nil, result, err
	})
}

func handleCredIssueToken(ctx context.Context, client *k8s.Client, params CredIssueTokenParams) (CredIssueTokenResult, error) {
	if err := k8s.CheckManagedNamespace(ctx, client, params.Namespace); err != nil {
		return CredIssueTokenResult{}, err
	}
	if params.TTLMinutes < 10 || params.TTLMinutes > 1440 {
		return CredIssueTokenResult{}, fmt.Errorf("TTL must be between 10 and 1440 minutes, got %d", params.TTLMinutes)
	}

	token, err := k8s.IssueToken(ctx, client, params.Namespace, params.TTLMinutes)
	if err != nil {
		return CredIssueTokenResult{}, err
	}

	expiresAt := time.Now().Add(time.Duration(params.TTLMinutes) * time.Minute).UTC().Format(time.RFC3339)

	return CredIssueTokenResult{
		Token:     token,
		ExpiresAt: expiresAt,
	}, nil
}

func handleCredKubeconfig(ctx context.Context, client *k8s.Client, params CredKubeconfigParams) (CredKubeconfigResult, error) {
	if err := k8s.CheckManagedNamespace(ctx, client, params.Namespace); err != nil {
		return CredKubeconfigResult{}, err
	}
	if params.TTLMinutes < 10 || params.TTLMinutes > 1440 {
		return CredKubeconfigResult{}, fmt.Errorf("TTL must be between 10 and 1440 minutes, got %d", params.TTLMinutes)
	}

	token, err := k8s.IssueToken(ctx, client, params.Namespace, params.TTLMinutes)
	if err != nil {
		return CredKubeconfigResult{}, fmt.Errorf("issue token: %w", err)
	}

	clusterURL := client.Config.Host
	caCert := string(client.Config.CAData)

	kubeconfig, err := k8s.GenerateKubeconfig(clusterURL, caCert, token, params.Namespace)
	if err != nil {
		return CredKubeconfigResult{}, fmt.Errorf("generate kubeconfig: %w", err)
	}

	return CredKubeconfigResult{Kubeconfig: kubeconfig}, nil
}

func handleCredRotate(ctx context.Context, client *k8s.Client, params CredRotateParams) (CredRotateResult, error) {
	if err := k8s.CheckManagedNamespace(ctx, client, params.Namespace); err != nil {
		return CredRotateResult{}, err
	}
	if err := k8s.RecreateWorkflowServiceAccount(ctx, client, params.Namespace); err != nil {
		return CredRotateResult{}, fmt.Errorf("rotate service account in namespace %q: %w", params.Namespace, err)
	}

	return CredRotateResult{
		Namespace: params.Namespace,
		Rotated:   true,
		Message:   fmt.Sprintf("Service account tentacular-workflow in namespace %q has been recreated; all prior tokens are now invalid.", params.Namespace),
	}, nil
}
