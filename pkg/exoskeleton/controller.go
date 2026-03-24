package exoskeleton

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"

	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

// PostgresRegistrarI abstracts Postgres registration for testability.
type PostgresRegistrarI interface {
	Register(ctx context.Context, id Identity) (*PostgresCreds, error)
	Unregister(ctx context.Context, id Identity) error
	Close()
}

// NATSRegistrarI abstracts NATS registration for testability.
type NATSRegistrarI interface {
	Register(ctx context.Context, id Identity) (*NATSCreds, error)
	Unregister(ctx context.Context, id Identity) error
	Close()
}

// RustFSRegistrarI abstracts RustFS registration for testability.
type RustFSRegistrarI interface {
	Register(ctx context.Context, id Identity) (*RustFSCreds, error)
	Unregister(ctx context.Context, id Identity) error
	Close()
}

// SPIRERegistrarI abstracts SPIRE identity registration for testability.
type SPIRERegistrarI interface {
	Register(ctx context.Context, id Identity, namespace string) error
	Unregister(ctx context.Context, id Identity, namespace string) error
	Close()
}

// Controller orchestrates exoskeleton registration and cleanup across
// all enabled backing services.
type Controller struct {
	cfg    *Config
	pg     PostgresRegistrarI
	nats   NATSRegistrarI
	rustfs RustFSRegistrarI
	spire  SPIRERegistrarI
}

// NewController initializes registrars for all enabled services. If the
// exoskeleton is disabled, returns a no-op controller. The k8sClient
// parameter may be nil when running without a Kubernetes cluster (e.g.,
// in tests); SPIRE registration will be skipped in that case.
func NewController(cfg *Config, k8sClient *k8s.Client) (*Controller, error) {
	c := &Controller{cfg: cfg}

	if !cfg.Enabled {
		slog.Info("exoskeleton: disabled")
		return c, nil
	}

	ctx := context.Background()

	if cfg.PostgresEnabled() {
		pg, err := NewPostgresRegistrar(ctx, cfg.Postgres)
		if err != nil {
			return nil, fmt.Errorf("exoskeleton postgres init: %w", err)
		}
		c.pg = pg
		slog.Info("exoskeleton: postgres registrar initialized")
	}

	if cfg.NATSEnabled() {
		var natsClientset kubernetes.Interface
		if k8sClient != nil {
			natsClientset = k8sClient.Clientset
		}
		natsReg, err := NewNATSRegistrar(ctx, cfg.NATS, natsClientset)
		if err != nil {
			return nil, fmt.Errorf("exoskeleton nats init: %w", err)
		}
		c.nats = natsReg
		slog.Info("exoskeleton: nats registrar initialized")
	}

	if cfg.RustFSEnabled() {
		rustfs, err := NewRustFSRegistrar(ctx, cfg.RustFS)
		if err != nil {
			return nil, fmt.Errorf("exoskeleton rustfs init: %w", err)
		}
		c.rustfs = rustfs
		slog.Info("exoskeleton: rustfs registrar initialized")
	}

	if cfg.SPIREEnabled() && k8sClient != nil && k8sClient.Dynamic != nil {
		if hasCRD := checkClusterSPIFFEIDCRD(ctx, k8sClient); hasCRD {
			c.spire = NewSPIRERegistrar(k8sClient.Dynamic, cfg.SPIRE.ClassName)
			slog.Info("exoskeleton: spire registrar initialized", "className", cfg.SPIRE.ClassName)
		} else {
			slog.Warn("exoskeleton: SPIRE enabled but ClusterSPIFFEID CRD not found on cluster, skipping")
		}
	}

	slog.Info("exoskeleton: controller ready",
		"postgres", cfg.PostgresEnabled(),
		"nats", cfg.NATSEnabled(),
		"rustfs", cfg.RustFSEnabled(),
		"spire", c.spire != nil)

	return c, nil
}

// NewControllerWithDeps creates a Controller with pre-built registrar
// implementations. This is the dependency-injection constructor used by
// tests (with mock registrars) and by callers that manage registrar
// lifecycle externally. Any registrar may be nil if the service is disabled.
func NewControllerWithDeps(cfg *Config, pg PostgresRegistrarI, nats NATSRegistrarI, rustfs RustFSRegistrarI, spire SPIRERegistrarI) *Controller {
	return &Controller{cfg: cfg, pg: pg, nats: nats, rustfs: rustfs, spire: spire}
}

// checkClusterSPIFFEIDCRD checks if the ClusterSPIFFEID CRD is
// installed on the cluster by attempting to list the resource.
func checkClusterSPIFFEIDCRD(ctx context.Context, client *k8s.Client) bool {
	_, err := client.Clientset.Discovery().ServerResourcesForGroupVersion("spire.spiffe.io/v1alpha1")
	if err != nil {
		// Also try a direct CRD lookup.
		crdGVR := schema.GroupVersionResource{
			Group:    "apiextensions.k8s.io",
			Version:  "v1",
			Resource: "customresourcedefinitions",
		}
		_, err = client.Dynamic.Resource(crdGVR).Get(ctx, "clusterspiffeids.spire.spiffe.io", metav1.GetOptions{})
		return err == nil
	}
	return true
}

// ProcessManifests inspects the workflow manifests for tentacular-*
// dependencies. If any are found and the corresponding registrar is
// enabled, it registers the tentacle and appends an exoskeleton Secret
// manifest to the list. If a required service is not enabled, it returns
// an error.
//
// When the exoskeleton is disabled or no tentacular-* dependencies are
// declared, the manifests are returned unchanged.
func (c *Controller) ProcessManifests(ctx context.Context, namespace, name string, manifests []map[string]any) ([]map[string]any, error) {
	if !c.cfg.Enabled {
		return manifests, nil
	}

	deps := detectExoDeps(manifests)
	if len(deps) == 0 {
		return manifests, nil
	}

	slog.Info("exoskeleton: detected dependencies", "namespace", namespace, "workflow", name, "deps", deps)

	id, err := CompileIdentity(namespace, name)
	if err != nil {
		return nil, fmt.Errorf("exoskeleton identity: %w", err)
	}
	creds := make(map[string]any)

	// NOTE: Registrars are called sequentially. If an earlier registrar
	// succeeds but a later one fails (e.g., Postgres succeeds, NATS fails),
	// the successfully registered credentials become orphaned. This is
	// acceptable for Phase 1: the next deploy will re-register idempotently,
	// and Cleanup() handles tear-down of all services. A future phase may
	// add compensating rollback logic.
	for _, dep := range deps {
		switch dep {
		case "tentacular-postgres":
			if c.pg == nil {
				return nil, errors.New("workflow requires tentacular-postgres but TENTACULAR_EXOSKELETON_POSTGRES is not enabled or not configured")
			}
			pgCreds, err := c.pg.Register(ctx, id)
			if err != nil {
				return nil, fmt.Errorf("postgres registration: %w", err)
			}
			creds[dep] = pgCreds

		case "tentacular-nats":
			if c.nats == nil {
				return nil, errors.New("workflow requires tentacular-nats but TENTACULAR_EXOSKELETON_NATS is not enabled or not configured")
			}
			natsCreds, err := c.nats.Register(ctx, id)
			if err != nil {
				return nil, fmt.Errorf("nats registration: %w", err)
			}
			creds[dep] = natsCreds

		case "tentacular-rustfs":
			if c.rustfs == nil {
				return nil, errors.New("workflow requires tentacular-rustfs but TENTACULAR_EXOSKELETON_RUSTFS is not enabled or not configured")
			}
			rustfsCreds, err := c.rustfs.Register(ctx, id)
			if err != nil {
				return nil, fmt.Errorf("rustfs registration: %w", err)
			}
			creds[dep] = rustfsCreds

		default:
			slog.Warn("exoskeleton: unknown tentacular dependency, skipping", "dep", dep)
		}
	}

	if len(creds) > 0 {
		// Step 7.8 + 7.9: Enrich contract dependencies in the ConfigMap.
		if err := enrichContractDeps(manifests, creds); err != nil {
			return nil, fmt.Errorf("enrich contract deps: %w", err)
		}

		// Step 7.10: Build and append the Secret manifest.
		secret, err := BuildSecretManifest(namespace, name, creds)
		if err != nil {
			return nil, fmt.Errorf("build secret manifest: %w", err)
		}
		manifests = append(manifests, secret)
		slog.Info("exoskeleton: injected credential secret", "namespace", namespace, "workflow", name)

		// Step 7.11: Patch Deployment --allow-net flags.
		patchDeploymentAllowNet(manifests, creds)

		// Merge exo credentials into the user-provided secret so the
		// engine can resolve them via ctx.dependency().
		manifests, err = mergeExoCredsIntoUserSecret(manifests, namespace, name, creds)
		if err != nil {
			return nil, fmt.Errorf("merge exo creds: %w", err)
		}
	}

	// SPIRE identity registration: creates a ClusterSPIFFEID so matching
	// pods receive an X.509 SVID automatically. This does not produce
	// credentials -- the SPIRE agent handles SVID provisioning.
	if c.spire != nil {
		if err := c.spire.Register(ctx, id, namespace); err != nil {
			slog.Warn("exoskeleton: SPIRE registration failed (non-fatal)", "error", err)
		} else {
			patchDeploymentSpireVolume(manifests)
		}
	}

	return manifests, nil
}

// CleanupReport describes what the exoskeleton cleanup performed.
type CleanupReport struct {
	Postgres  string
	NATS      string
	RustFS    string
	SPIRE     string
	Performed bool
}

// Summary returns a human-readable description of cleanup actions.
func (r *CleanupReport) Summary() string {
	var parts []string
	if r.Postgres != "" {
		parts = append(parts, "postgres "+r.Postgres)
	}
	if r.NATS != "" {
		parts = append(parts, "nats "+r.NATS)
	}
	if r.RustFS != "" {
		parts = append(parts, "rustfs "+r.RustFS)
	}
	if r.SPIRE != "" {
		parts = append(parts, "spire "+r.SPIRE)
	}
	if len(parts) == 0 {
		return "no services cleaned up"
	}
	return strings.Join(parts, ", ")
}

// Cleanup unregisters the tentacle from all enabled services. Called
// from wf_remove when CleanupOnUndeploy is true.
func (c *Controller) Cleanup(ctx context.Context, namespace, name string) error {
	_, err := c.CleanupWithReport(ctx, namespace, name)
	return err
}

// CleanupWithReport unregisters the tentacle from all enabled services and
// returns a report of what was cleaned up. Called from wf_remove.
func (c *Controller) CleanupWithReport(ctx context.Context, namespace, name string) (*CleanupReport, error) {
	report := &CleanupReport{}

	if !c.cfg.Enabled || !c.cfg.CleanupOnUndeploy {
		return report, nil
	}

	id, err := CompileIdentity(namespace, name)
	if err != nil {
		return report, fmt.Errorf("exoskeleton identity: %w", err)
	}
	var errs []string

	report.Performed = true

	if c.pg != nil {
		if err := c.pg.Unregister(ctx, id); err != nil {
			errs = append(errs, fmt.Sprintf("postgres: %v", err))
		} else {
			report.Postgres = "schema dropped"
		}
	}
	if c.nats != nil {
		if err := c.nats.Unregister(ctx, id); err != nil {
			errs = append(errs, fmt.Sprintf("nats: %v", err))
		} else {
			if c.cfg.NATS.SPIFFEEnabled {
				report.NATS = "authz entry removed"
			} else {
				report.NATS = "no-op"
			}
		}
	}
	if c.rustfs != nil {
		if err := c.rustfs.Unregister(ctx, id); err != nil {
			errs = append(errs, fmt.Sprintf("rustfs: %v", err))
		} else {
			report.RustFS = "user removed"
		}
	}
	if c.spire != nil {
		if err := c.spire.Unregister(ctx, id, namespace); err != nil {
			errs = append(errs, fmt.Sprintf("spire: %v", err))
		} else {
			report.SPIRE = "identity removed"
		}
	}

	if len(errs) > 0 {
		return report, fmt.Errorf("exoskeleton cleanup errors: %s", strings.Join(errs, "; "))
	}

	slog.Info("exoskeleton: cleanup complete", "namespace", namespace, "workflow", name)
	return report, nil
}

// Close releases all registrar resources.
func (c *Controller) Close() error {
	if c.pg != nil {
		c.pg.Close()
	}
	if c.nats != nil {
		c.nats.Close()
	}
	if c.rustfs != nil {
		c.rustfs.Close()
	}
	if c.spire != nil {
		c.spire.Close()
	}
	return nil
}

// Enabled returns true if the exoskeleton is enabled.
func (c *Controller) Enabled() bool {
	return c.cfg.Enabled
}

// PostgresAvailable returns true if the Postgres registrar is initialized.
func (c *Controller) PostgresAvailable() bool {
	return c.pg != nil
}

// NATSAvailable returns true if the NATS registrar is initialized.
func (c *Controller) NATSAvailable() bool {
	return c.nats != nil
}

// RustFSAvailable returns true if the RustFS registrar is initialized.
func (c *Controller) RustFSAvailable() bool {
	return c.rustfs != nil
}

// SPIREAvailable returns true if the SPIRE registrar is initialized.
func (c *Controller) SPIREAvailable() bool {
	return c.spire != nil
}

// CleanupOnUndeploy returns the cleanup setting.
func (c *Controller) CleanupOnUndeploy() bool {
	return c.cfg.CleanupOnUndeploy
}

// NATSSpiffeEnabled returns true if NATS is configured to use SPIFFE mTLS.
func (c *Controller) NATSSpiffeEnabled() bool {
	return c.cfg.NATS.SPIFFEEnabled
}

// AuthEnabled returns true if OIDC authentication is enabled.
func (c *Controller) AuthEnabled() bool {
	return c.cfg.AuthEnabled()
}

// AuthIssuer returns the configured OIDC issuer URL, or empty if not set.
func (c *Controller) AuthIssuer() string {
	return c.cfg.Auth.IssuerURL
}

// ServiceInfo returns an ExoskeletonInfo describing the exoskeleton subsystem
// and its backing services. Returns nil if the exoskeleton is disabled.
func (c *Controller) ServiceInfo() *k8s.ExoskeletonInfo {
	if !c.cfg.Enabled {
		return nil
	}

	var services []k8s.ExoskeletonServiceInfo

	// Postgres
	services = append(services, k8s.ExoskeletonServiceInfo{
		Name:      "postgres",
		Host:      c.cfg.Postgres.Host,
		Port:      c.cfg.Postgres.Port,
		Protocol:  "postgresql",
		Available: c.PostgresAvailable(),
	})

	// NATS — parse host and port from the URL
	natsHost, natsPort := parseHostPort(c.cfg.NATS.URL)
	services = append(services, k8s.ExoskeletonServiceInfo{
		Name:          "nats",
		Host:          natsHost,
		Port:          natsPort,
		Protocol:      "nats",
		Available:     c.NATSAvailable(),
		SPIFFEEnabled: c.cfg.NATS.SPIFFEEnabled,
	})

	// RustFS — parse host and port from the endpoint URL
	rustfsHost, rustfsPort := parseHostPort(c.cfg.RustFS.Endpoint)
	services = append(services, k8s.ExoskeletonServiceInfo{
		Name:      "rustfs",
		Host:      rustfsHost,
		Port:      rustfsPort,
		Protocol:  "s3",
		Available: c.RustFSAvailable(),
	})

	// SPIRE
	services = append(services, k8s.ExoskeletonServiceInfo{
		Name:      "spire",
		Protocol:  "spiffe",
		Available: c.SPIREAvailable(),
	})

	var issuer string
	if c.AuthEnabled() {
		issuer = c.AuthIssuer()
	}

	return &k8s.ExoskeletonInfo{
		Enabled:  true,
		Services: services,
		Auth: k8s.ExoskeletonAuthInfo{
			Enabled: c.AuthEnabled(),
			Issuer:  issuer,
		},
	}
}

// contractDeps is a minimal YAML structure to extract tentacular-*
// dependencies from a workflow ConfigMap.
type contractDeps struct {
	Contract *struct {
		Dependencies map[string]any `yaml:"dependencies"`
	} `yaml:"contract"`
}

// detectExoDeps scans manifests for a ConfigMap containing workflow.yaml
// and returns any dependency names with the "tentacular-" prefix.
func detectExoDeps(manifests []map[string]any) []string {
	for _, m := range manifests {
		obj := &unstructured.Unstructured{Object: m}
		if obj.GetKind() != "ConfigMap" {
			continue
		}
		data, ok, _ := unstructured.NestedStringMap(obj.Object, "data")
		if !ok {
			continue
		}
		wfYAML, ok := data["workflow.yaml"]
		if !ok {
			continue
		}
		var cd contractDeps
		if err := yaml.Unmarshal([]byte(wfYAML), &cd); err != nil || cd.Contract == nil {
			continue
		}
		var deps []string
		for name := range cd.Contract.Dependencies {
			if strings.HasPrefix(name, "tentacular-") {
				deps = append(deps, name)
			}
		}
		sort.Strings(deps)
		return deps
	}
	return nil
}
