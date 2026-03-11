package server

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/randybias/tentacular-mcp/pkg/auth"
	"github.com/randybias/tentacular-mcp/pkg/exoskeleton"
	"github.com/randybias/tentacular-mcp/pkg/k8s"
	"github.com/randybias/tentacular-mcp/pkg/proxy"
	"github.com/randybias/tentacular-mcp/pkg/scheduler"
	"github.com/randybias/tentacular-mcp/pkg/tools"
	"github.com/randybias/tentacular-mcp/pkg/version"
)

// Server wraps the MCP server with K8s client and auth.
type Server struct {
	mcpServer     *mcp.Server
	client        *k8s.Client
	reconciler    *proxy.Reconciler
	scheduler     *scheduler.Scheduler
	exoController *exoskeleton.ExoskeletonController
	token         string
	logger        *slog.Logger
}

// New creates a new MCP server with all tools registered.
// exoCtrl may be nil when the exoskeleton subsystem is disabled.
func New(client *k8s.Client, reconciler *proxy.Reconciler, sched *scheduler.Scheduler, token string, logger *slog.Logger, exoCtrl *exoskeleton.ExoskeletonController) (*Server, error) {
	mcpServer := mcp.NewServer(
		&mcp.Implementation{
			Name:    "tentacular-mcp",
			Version: version.Version,
		},
		&mcp.ServerOptions{
			Instructions: "In-cluster MCP server for Kubernetes namespace lifecycle, credential management, workflow introspection, cluster operations, and module proxy management.",
			Logger:       logger,
		},
	)

	s := &Server{
		mcpServer:     mcpServer,
		client:        client,
		reconciler:    reconciler,
		scheduler:     sched,
		exoController: exoCtrl,
		token:         token,
		logger:        logger,
	}

	s.registerTools()

	return s, nil
}

// Handler returns the HTTP handler with auth middleware and health endpoint.
func (s *Server) Handler() http.Handler {
	mcpHandler := mcp.NewStreamableHTTPHandler(
		func(r *http.Request) *mcp.Server {
			return s.mcpServer
		},
		nil,
	)

	mux := http.NewServeMux()
	mux.Handle("/mcp", mcpHandler)
	mux.HandleFunc("/healthz", s.healthHandler)

	return auth.Middleware(s.token, mux)
}

func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// registerTools registers all MCP tools by delegating to the tools package.
func (s *Server) registerTools() {
	tools.RegisterAll(s.mcpServer, s.client, s.reconciler, s.scheduler, s.exoController)
}
