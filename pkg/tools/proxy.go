package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/randybias/tentacular-mcp/pkg/proxy"
)

// ProxyStatusParams are the parameters for proxy_status.
type ProxyStatusParams struct{}

// ProxyStatusResult is the result of proxy_status.
type ProxyStatusResult struct {
	Namespace string `json:"namespace"`
	Image     string `json:"image"`
	Storage   string `json:"storage"`
	Installed bool   `json:"installed"`
	Ready     bool   `json:"ready"`
}

func registerProxyTools(srv *mcp.Server, reconciler *proxy.Reconciler) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "proxy_status",
		Description: "Check the installation and readiness status of the module proxy (esm.sh).",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Module Proxy Status",
			ReadOnlyHint:    true,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, params ProxyStatusParams) (*mcp.CallToolResult, ProxyStatusResult, error) {
		st := reconciler.GetStatus(ctx)
		result := ProxyStatusResult{
			Installed: st.Installed,
			Ready:     st.Ready,
			Namespace: reconciler.Namespace(),
			Image:     st.Image,
			Storage:   st.Storage,
		}
		return nil, result, nil
	})
}
