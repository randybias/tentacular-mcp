package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/randybias/tentacular-mcp/pkg/auth"
	"github.com/randybias/tentacular-mcp/pkg/authz"
	"github.com/randybias/tentacular-mcp/pkg/exoskeleton"
	"github.com/randybias/tentacular-mcp/pkg/k8s"
	"github.com/randybias/tentacular-mcp/pkg/proxy"
	"github.com/randybias/tentacular-mcp/pkg/scheduler"
	"github.com/randybias/tentacular-mcp/pkg/tools"
	"github.com/randybias/tentacular-mcp/pkg/version"
)

// ResourceMetadataConfig holds the RFC 9728 Protected Resource Metadata
// that the server advertises at /.well-known/oauth-protected-resource.
// When nil, the well-known endpoint is not registered.
type ResourceMetadataConfig struct {
	Resource               string   `json:"resource"`
	ResourceName           string   `json:"resource_name,omitempty"`
	AuthorizationServers   []string `json:"authorization_servers"`
	ScopesSupported        []string `json:"scopes_supported,omitempty"`
	BearerMethodsSupported []string `json:"bearer_methods_supported,omitempty"`
}

// Server wraps the MCP server with K8s client and auth.
type Server struct {
	mcpServer     *mcp.Server
	client        *k8s.Client
	reconciler    *proxy.Reconciler
	scheduler     *scheduler.Scheduler
	exoCtrl       *exoskeleton.Controller
	eval          *authz.Evaluator
	oidcValidator *exoskeleton.OIDCValidator
	resourceMeta  *ResourceMetadataConfig
	logger        *slog.Logger
	token         string
}

// New creates a new MCP server with all tools registered.
// The oidcValidator may be nil when OIDC auth is disabled.
// The resourceMeta may be nil to disable the RFC 9728 well-known endpoint.
// The eval may be nil to disable authz (all checks return Allow).
func New(client *k8s.Client, reconciler *proxy.Reconciler, sched *scheduler.Scheduler, exoCtrl *exoskeleton.Controller, eval *authz.Evaluator, oidcValidator *exoskeleton.OIDCValidator, resourceMeta *ResourceMetadataConfig, token string, logger *slog.Logger) (*Server, error) {
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
		exoCtrl:       exoCtrl,
		eval:          eval,
		oidcValidator: oidcValidator,
		resourceMeta:  resourceMeta,
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

	// Derive the resource metadata URL for the WWW-Authenticate header.
	var resourceMetadataURL string
	if s.resourceMeta != nil {
		mux.HandleFunc("/.well-known/oauth-protected-resource", s.resourceMetadataHandler)
		// Derive the well-known URL from the resource URL by replacing the path.
		resourceMetadataURL = deriveWellKnownURL(s.resourceMeta.Resource)
	}

	return auth.DualAuthMiddleware(s.token, s.oidcValidator, resourceMetadataURL, mux)
}

// resourceMetadataHandler serves the RFC 9728 Protected Resource Metadata document.
func (s *Server) resourceMetadataHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	_ = json.NewEncoder(w).Encode(s.resourceMeta) //nolint:errchkjson // metadata is a constant payload
}

func (*Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errchkjson // health handler writes a constant payload; write errors are non-actionable
}

// registerTools registers all MCP tools by delegating to the tools package.
func (s *Server) registerTools() {
	tools.RegisterAll(s.mcpServer, s.client, s.reconciler, s.scheduler, s.exoCtrl, s.eval)
}

// deriveWellKnownURL takes a resource URL like "https://mcp.example.com/mcp"
// and returns "https://mcp.example.com/.well-known/oauth-protected-resource".
func deriveWellKnownURL(resourceURL string) string {
	u, err := url.Parse(resourceURL)
	if err != nil {
		return resourceURL + "/.well-known/oauth-protected-resource"
	}
	u.Path = "/.well-known/oauth-protected-resource"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}
