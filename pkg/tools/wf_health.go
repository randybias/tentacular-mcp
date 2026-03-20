package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/randybias/tentacular-mcp/pkg/auth"
	"github.com/randybias/tentacular-mcp/pkg/authz"
	"github.com/randybias/tentacular-mcp/pkg/exoskeleton"
	"github.com/randybias/tentacular-mcp/pkg/guard"
	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

const (
	wfHealthPort    = 8080
	wfHealthTimeout = 5 * time.Second
	wfHealthDefault = 10
)

// wfHealthProbe is the function used to probe a workflow health endpoint.
// It is a package-level variable so tests can replace it with a fake.
var wfHealthProbe = func(name, namespace string, detail bool) (string, error) {
	return probeURL(wfHealthURL(name, namespace, detail))
}

// WfHealthParams are the parameters for wf_health.
type WfHealthParams struct {
	Namespace string `json:"namespace" jsonschema:"Workflow namespace"`
	Name      string `json:"name" jsonschema:"Deployment name"`
	Detail    bool   `json:"detail,omitempty" jsonschema:"Include execution telemetry from health endpoint (default false)"`
}

// WfHealthResult is the result of wf_health.
type WfHealthResult struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Status    string `json:"status"`
	Reason    string `json:"reason,omitempty"`
	Detail    string `json:"detail,omitempty"`
	PodReady  bool   `json:"pod_ready"`
}

// WfHealthNsParams are the parameters for wf_health_ns.
type WfHealthNsParams struct {
	Namespace string `json:"namespace" jsonschema:"Namespace to scan"`
	Limit     int    `json:"limit,omitempty" jsonschema:"Max workflows to check (default 20)"`
}

// WfHealthNsSummary is the G/A/R count summary for wf_health_ns.
type WfHealthNsSummary struct {
	Green int `json:"green"`
	Amber int `json:"amber"`
	Red   int `json:"red"`
}

// WfHealthNsEntry is a single workflow entry in wf_health_ns results.
type WfHealthNsEntry struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

// WfHealthNsResult is the result of wf_health_ns.
type WfHealthNsResult struct {
	Namespace string            `json:"namespace"`
	Workflows []WfHealthNsEntry `json:"workflows"`
	Summary   WfHealthNsSummary `json:"summary"`
	Total     int               `json:"total"`
	Truncated bool              `json:"truncated"`
}

func registerWfHealthTools(srv *mcp.Server, client *k8s.Client, eval *authz.Evaluator) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "wf_health",
		Description: "Get G/A/R health status of a single workflow runtime deployment, with optional execution telemetry.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Workflow Health Status",
			ReadOnlyHint:    true,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, params WfHealthParams) (*mcp.CallToolResult, WfHealthResult, error) {
		if err := guard.CheckNamespace(params.Namespace); err != nil {
			return nil, WfHealthResult{}, err
		}
		deployer := auth.DeployerFromContext(ctx)
		result, err := handleWfHealth(ctx, client, params, deployer, eval)
		return nil, result, err
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "wf_health_ns",
		Description: "Aggregate G/A/R health status for all tentacular workflow deployments in a namespace.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Namespace Workflow Health",
			ReadOnlyHint:    true,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, params WfHealthNsParams) (*mcp.CallToolResult, WfHealthNsResult, error) {
		if err := guard.CheckNamespace(params.Namespace); err != nil {
			return nil, WfHealthNsResult{}, err
		}
		deployer := auth.DeployerFromContext(ctx)
		result, err := handleWfHealthNs(ctx, client, params, deployer, eval)
		return nil, result, err
	})
}

func handleWfHealth(ctx context.Context, client *k8s.Client, params WfHealthParams, deployer *exoskeleton.DeployerInfo, eval *authz.Evaluator) (WfHealthResult, error) {
	if err := k8s.CheckManagedNamespace(ctx, client, params.Namespace); err != nil {
		return WfHealthResult{}, err
	}

	dep, err := client.Clientset.AppsV1().Deployments(params.Namespace).Get(ctx, params.Name, metav1.GetOptions{})
	if err != nil {
		return WfHealthResult{}, fmt.Errorf("get deployment %q in namespace %q: %w", params.Name, params.Namespace, err)
	}

	if d := eval.Check(deployer, dep.Annotations, authz.Read); !d.Allowed {
		return WfHealthResult{}, fmt.Errorf("permission denied: %s", d.Reason)
	}

	podReady := dep.Status.ReadyReplicas >= 1

	if !podReady {
		return WfHealthResult{
			Name:      params.Name,
			Namespace: params.Namespace,
			Status:    "red",
			Reason:    fmt.Sprintf("0/%d replicas ready", replicaCount(dep.Spec.Replicas)),
			PodReady:  false,
		}, nil
	}

	// Pod is ready: probe the health endpoint.
	detail, probeErr := wfHealthProbe(params.Name, params.Namespace, params.Detail)
	if probeErr != nil {
		// Health endpoint unreachable = RED.
		return WfHealthResult{
			Name:      params.Name,
			Namespace: params.Namespace,
			Status:    "red",
			Reason:    fmt.Sprintf("health endpoint unreachable: %v", probeErr),
			PodReady:  true,
		}, nil
	}

	// Parse probe response for AMBER conditions.
	status, reason := classifyFromDetail(detail)

	result := WfHealthResult{
		Name:      params.Name,
		Namespace: params.Namespace,
		Status:    status,
		Reason:    reason,
		PodReady:  true,
	}
	if params.Detail {
		result.Detail = detail
	}
	return result, nil
}

func handleWfHealthNs(ctx context.Context, client *k8s.Client, params WfHealthNsParams, deployer *exoskeleton.DeployerInfo, eval *authz.Evaluator) (WfHealthNsResult, error) {
	if err := k8s.CheckManagedNamespace(ctx, client, params.Namespace); err != nil {
		return WfHealthNsResult{}, err
	}

	if err := checkNamespaceAuthz(ctx, client, params.Namespace, deployer, eval, authz.Read); err != nil {
		return WfHealthNsResult{}, err
	}

	limit := params.Limit
	if limit <= 0 {
		limit = wfHealthDefault
	}

	depList, err := client.Clientset.AppsV1().Deployments(params.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: k8s.ManagedByLabel + "=" + k8s.ManagedByValue,
	})
	if err != nil {
		return WfHealthNsResult{}, fmt.Errorf("list deployments in namespace %q: %w", params.Namespace, err)
	}

	// Filter to only deployments the caller can read before applying the limit.
	visible := depList.Items[:0]
	for _, dep := range depList.Items {
		if d := eval.Check(deployer, dep.Annotations, authz.Read); d.Allowed {
			visible = append(visible, dep)
		}
	}

	total := len(visible)
	truncated := total > limit

	items := visible
	if truncated {
		items = items[:limit]
	}

	workflows := make([]WfHealthNsEntry, 0, len(items))
	summary := WfHealthNsSummary{}

	for _, dep := range items {
		name := dep.Name
		podReady := dep.Status.ReadyReplicas >= 1

		var status, reason string

		if !podReady {
			status = "red"
			reason = fmt.Sprintf("0/%d replicas ready", replicaCount(dep.Spec.Replicas))
		} else {
			detail, probeErr := wfHealthProbe(name, params.Namespace, false)
			if probeErr != nil {
				status = "red"
				reason = fmt.Sprintf("health endpoint unreachable: %v", probeErr)
			} else {
				status, reason = classifyFromDetail(detail)
			}
		}

		switch status {
		case "green":
			summary.Green++
		case "amber":
			summary.Amber++
		case "red":
			summary.Red++
		}

		workflows = append(workflows, WfHealthNsEntry{
			Name:   name,
			Status: status,
			Reason: reason,
		})
	}

	return WfHealthNsResult{
		Namespace: params.Namespace,
		Summary:   summary,
		Workflows: workflows,
		Truncated: truncated,
		Total:     total,
	}, nil
}

// wfHealthURL constructs the health endpoint URL for a workflow deployment.
func wfHealthURL(name, namespace string, detail bool) string {
	url := fmt.Sprintf("http://%s.%s.svc.cluster.local:%d/health", name, namespace, wfHealthPort)
	if detail {
		url += "?detail=1"
	}
	return url
}

// probeURL performs an HTTP GET to the given URL and returns the response body.
// Returns an error if the request fails or the status is non-2xx.
// Body reads are capped at 1MiB for defense-in-depth.
func probeURL(url string) (string, error) {
	const maxBody = 1 << 20 // 1MiB

	httpClient := &http.Client{Timeout: wfHealthTimeout}
	resp, err := httpClient.Get(url) //nolint:noctx
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("health endpoint returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return "", fmt.Errorf("read health response: %w", err)
	}

	return string(body), nil
}

// wfHealthResponse is the parsed structure of a workflow health endpoint response.
// Fields are optional; unknown fields are silently ignored.
//
// The engine adds lastRunFailed and inFlight to its /health?detail=1 response
// to support G/A/R classification.
type wfHealthResponse struct {
	LastRunFailed bool `json:"lastRunFailed"`
	InFlight      int  `json:"inFlight"`
}

// classifyFromDetail applies G/A/R classification to a health probe response body.
// GREEN is the default when the endpoint is reachable and no AMBER signals are present.
// If the body cannot be parsed as JSON the endpoint is treated as green (reachable = not red).
//
// AMBER conditions:
//   - lastRunFailed == true: last execution failed (resets on next successful run)
//   - inFlight > 0: execution currently in flight
func classifyFromDetail(detail string) (status, reason string) {
	var resp wfHealthResponse
	if err := json.Unmarshal([]byte(detail), &resp); err != nil {
		// Unparseable body but endpoint was reachable: treat as green.
		return "green", ""
	}
	if resp.LastRunFailed {
		return "amber", "last execution failed"
	}
	if resp.InFlight > 0 {
		return "amber", "execution in flight"
	}
	return "green", ""
}
