package tools

import (
	"context"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// PlatformConfigParams are the parameters for platform_config (none required).
type PlatformConfigParams struct{}

// PlatformConfigResult is the result of platform_config.
type PlatformConfigResult struct {
	ChromaURL string `json:"chroma_url"`
	DocsURL   string `json:"docs_url"`
}

func registerPlatformTools(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "platform_config",
		Description: "Returns platform service URLs for integration (Chroma UI, documentation site).",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Get Platform Configuration",
			ReadOnlyHint:    true,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(false),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, params PlatformConfigParams) (*mcp.CallToolResult, PlatformConfigResult, error) {
		result := PlatformConfigResult{
			ChromaURL: os.Getenv("TENTACULAR_CHROMA_URL"),
			DocsURL:   os.Getenv("TENTACULAR_DOCS_URL"),
		}
		return nil, result, nil
	})
}
