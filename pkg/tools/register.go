package tools

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/randybias/tentacular-mcp/pkg/exoskeleton"
	"github.com/randybias/tentacular-mcp/pkg/k8s"
	"github.com/randybias/tentacular-mcp/pkg/proxy"
	"github.com/randybias/tentacular-mcp/pkg/scheduler"
)

// boolPtr returns a pointer to the given bool value.
// Used for ToolAnnotations fields that require *bool (DestructiveHint, OpenWorldHint).
func boolPtr(b bool) *bool { return &b }

// RegisterAll registers all MCP tools with the given server.
func RegisterAll(srv *mcp.Server, client *k8s.Client, reconciler *proxy.Reconciler, sched *scheduler.Scheduler, exoCtrl *exoskeleton.Controller) {
	registerNamespaceTools(srv, client)
	registerCredentialTools(srv, client)
	registerWorkflowTools(srv, client)
	registerRunTools(srv, client)
	registerDiscoverTools(srv, client)
	registerClusterOpsTools(srv, client)
	registerGVisorTools(srv, client)
	registerDeployTools(srv, client, sched, exoCtrl)
	registerHealthTools(srv, client)
	registerWfHealthTools(srv, client)
	registerAuditTools(srv, client)
	registerProxyTools(srv, reconciler)
	registerExoskeletonTools(srv, client, exoCtrl)
}
