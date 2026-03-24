package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/randybias/tentacular-mcp/pkg/exoskeleton"
	"github.com/randybias/tentacular-mcp/pkg/guard"
	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

// ClusterPreflightParams are the parameters for cluster_preflight.
type ClusterPreflightParams struct {
	Namespace string `json:"namespace" jsonschema:"Namespace to run preflight checks against"`
}

// ClusterPreflightResult is the result of cluster_preflight.
type ClusterPreflightResult struct {
	Checks  []k8s.CheckResult `json:"checks"`
	AllPass bool              `json:"allPass"`
}

// ClusterProfileParams are the parameters for cluster_profile.
type ClusterProfileParams struct {
	Namespace string `json:"namespace,omitempty" jsonschema:"Optional namespace to include quota and limit range details"`
}

func registerClusterOpsTools(srv *mcp.Server, client *k8s.Client, exoCtrl *exoskeleton.Controller) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "cluster_preflight",
		Description: "Run preflight checks for a namespace: API reachability, namespace existence, RBAC, and gVisor availability.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Run Cluster Preflight Checks",
			ReadOnlyHint:    true,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, params ClusterPreflightParams) (*mcp.CallToolResult, ClusterPreflightResult, error) {
		if err := guard.CheckNamespace(params.Namespace); err != nil {
			return nil, ClusterPreflightResult{}, err
		}
		result, err := handleClusterPreflight(ctx, client, params)
		return nil, result, err
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "cluster_profile",
		Description: "Profile the cluster: K8s version, distribution, nodes, runtime classes, CNI, storage, and extensions.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Profile Cluster Capabilities",
			ReadOnlyHint:    true,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, params ClusterProfileParams) (*mcp.CallToolResult, *k8s.ClusterProfile, error) {
		if params.Namespace != "" {
			if err := guard.CheckNamespace(params.Namespace); err != nil {
				return nil, nil, err
			}
		}
		result, err := handleClusterProfile(ctx, client, exoCtrl, params)
		return nil, result, err
	})
}

func handleClusterPreflight(ctx context.Context, client *k8s.Client, params ClusterPreflightParams) (ClusterPreflightResult, error) {
	checks, err := k8s.RunPreflightChecks(ctx, client, params.Namespace)
	if err != nil {
		return ClusterPreflightResult{}, err
	}
	allPass := true
	for _, c := range checks {
		if !c.Passed {
			allPass = false
			break
		}
	}
	return ClusterPreflightResult{Checks: checks, AllPass: allPass}, nil
}

func handleClusterProfile(ctx context.Context, client *k8s.Client, exoCtrl *exoskeleton.Controller, params ClusterProfileParams) (*k8s.ClusterProfile, error) {
	profile, err := k8s.ProfileCluster(ctx, client, params.Namespace)
	if err != nil {
		return nil, err
	}
	if exoCtrl != nil {
		profile.Exoskeleton = exoCtrl.ServiceInfo()
	}
	return profile, nil
}
