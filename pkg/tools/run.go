package tools

import (
	"context"
	"encoding/json"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/randybias/tentacular-mcp/pkg/guard"
	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

// WfRunParams are the parameters for wf_run.
type WfRunParams struct {
	Namespace string          `json:"namespace" jsonschema:"Namespace of the workflow"`
	Name      string          `json:"name" jsonschema:"Workflow deployment name"`
	Input     json.RawMessage `json:"input,omitempty" jsonschema:"Optional JSON input payload"`
	TimeoutS  int             `json:"timeout_seconds,omitempty" jsonschema:"Timeout in seconds (default 120, max 600)"`
}

// WfRunResult is the result of wf_run.
type WfRunResult struct {
	Output     map[string]any `json:"output"`
	Name       string         `json:"name"`
	Namespace  string         `json:"namespace"`
	DurationMs int64          `json:"duration_ms"`
}

func registerRunTools(srv *mcp.Server, client *k8s.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "wf_run",
		Description: "Trigger a deployed workflow by POSTing directly to its /run endpoint via HTTP. The MCP server connects to the workflow's ClusterIP service; NetworkPolicy allows ingress from tentacular-system via namespaceSelector. Returns the JSON output from the workflow.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Trigger Workflow Execution",
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  false,
			OpenWorldHint:   boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, params WfRunParams) (*mcp.CallToolResult, WfRunResult, error) {
		if err := guard.CheckNamespace(params.Namespace); err != nil {
			return nil, WfRunResult{}, err
		}
		if err := guard.CheckName(params.Name); err != nil {
			return nil, WfRunResult{}, err
		}
		result, err := handleWfRun(ctx, client, params)
		return nil, result, err
	})
}

func handleWfRun(ctx context.Context, client *k8s.Client, params WfRunParams) (WfRunResult, error) {
	if err := k8s.CheckManagedNamespace(ctx, client, params.Namespace); err != nil {
		return WfRunResult{}, err
	}

	timeout := 120 * time.Second
	if params.TimeoutS > 0 && params.TimeoutS <= 600 {
		timeout = time.Duration(params.TimeoutS) * time.Second
	} else if params.TimeoutS > 600 {
		timeout = 600 * time.Second
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	raw, err := k8s.RunWorkflow(runCtx, client, params.Namespace, params.Name, params.Input)
	if err != nil {
		return WfRunResult{}, err
	}

	// Unmarshal raw JSON into map[string]any so the MCP schema generates
	// {"type": "object"} which Claude Code's Zod validator accepts.
	var output map[string]any
	if len(raw) > 0 {
		if jsonErr := json.Unmarshal(raw, &output); jsonErr != nil {
			output = map[string]any{"raw": string(raw)}
		}
	}

	return WfRunResult{
		Name:       params.Name,
		Namespace:  params.Namespace,
		Output:     output,
		DurationMs: time.Since(start).Milliseconds(),
	}, nil
}
