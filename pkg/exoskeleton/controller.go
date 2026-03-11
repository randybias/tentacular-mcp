package exoskeleton

import (
	"context"
	"fmt"
	"log"
	"strings"

	"gopkg.in/yaml.v3"
	"k8s.io/client-go/kubernetes"
)

// PostgresRegistrarI abstracts postgres registration for testability.
type PostgresRegistrarI interface {
	Register(ctx context.Context, id Identity) (*PostgresRegistration, error)
	Unregister(ctx context.Context, id Identity) error
}

// NATSRegistrarI abstracts NATS registration for testability.
type NATSRegistrarI interface {
	Register(ctx context.Context, id Identity) (*NATSRegistration, error)
	Unregister(ctx context.Context, id Identity) error
}

// CredentialInjectorI abstracts credential injection for testability.
type CredentialInjectorI interface {
	Inject(ctx context.Context, namespace, workflow string, pgReg *PostgresRegistration, natsReg *NATSRegistration) error
	Remove(ctx context.Context, namespace, workflow string) error
}

// ExoskeletonController orchestrates the full registration lifecycle across
// all enabled backing services, coordinating identity compilation, service
// registrars, and credential injection.
type ExoskeletonController struct {
	config     *ExoskeletonConfig
	postgres   PostgresRegistrarI
	nats       NATSRegistrarI
	credential CredentialInjectorI
}

// Config returns the exoskeleton configuration.
func (c *ExoskeletonController) Config() *ExoskeletonConfig {
	return c.config
}

// K8sClientset returns the kubernetes.Interface from the credential injector,
// or nil if no credential injector is configured.
func (c *ExoskeletonController) K8sClientset() kubernetes.Interface {
	if ci, ok := c.credential.(*CredentialInjector); ok && ci != nil {
		return ci.clientset
	}
	return nil
}

// NewExoskeletonController creates a controller with the given config and components.
// Registrars and credential injector may be nil if the corresponding service is disabled.
func NewExoskeletonController(
	config *ExoskeletonConfig,
	postgres PostgresRegistrarI,
	nats NATSRegistrarI,
	credential CredentialInjectorI,
) *ExoskeletonController {
	return &ExoskeletonController{
		config:     config,
		postgres:   postgres,
		nats:       nats,
		credential: credential,
	}
}

// Register provisions backing-service credentials for a tentacle and injects
// them as a Kubernetes Secret. It compiles the identity, calls each registrar
// for the deps requested, and calls the credential injector.
func (c *ExoskeletonController) Register(ctx context.Context, namespace, workflow string, deps []string) error {
	if c.config == nil || !c.config.Enabled {
		return nil
	}

	id := CompileIdentity(namespace, workflow)

	wantPostgres := containsDep(deps, "tentacular-postgres")
	wantNATS := containsDep(deps, "tentacular-nats")

	var pgReg *PostgresRegistration
	var natsReg *NATSRegistration

	if wantPostgres {
		if !c.config.PostgresEnabled || c.postgres == nil {
			return fmt.Errorf("exoskeleton: workflow %q depends on tentacular-postgres but postgres is not enabled", workflow)
		}
		reg, err := c.postgres.Register(ctx, id)
		if err != nil {
			return fmt.Errorf("exoskeleton: postgres registration for %s/%s: %w", namespace, workflow, err)
		}
		pgReg = reg
	}

	if wantNATS {
		if !c.config.NATSEnabled || c.nats == nil {
			return fmt.Errorf("exoskeleton: workflow %q depends on tentacular-nats but nats is not enabled", workflow)
		}
		reg, err := c.nats.Register(ctx, id)
		if err != nil {
			return fmt.Errorf("exoskeleton: nats registration for %s/%s: %w", namespace, workflow, err)
		}
		natsReg = reg
	}

	// Only inject credentials if at least one service was registered
	if pgReg != nil || natsReg != nil {
		if c.credential == nil {
			return fmt.Errorf("exoskeleton: credential injector not configured")
		}
		if err := c.credential.Inject(ctx, namespace, workflow, pgReg, natsReg); err != nil {
			return fmt.Errorf("exoskeleton: credential injection for %s/%s: %w", namespace, workflow, err)
		}
	}

	return nil
}

// Unregister removes the credential Secret and unregisters from all enabled
// backing services. Partial failures are logged but do not block cleanup.
func (c *ExoskeletonController) Unregister(ctx context.Context, namespace, workflow string) error {
	if c.config == nil || !c.config.Enabled {
		return nil
	}

	id := CompileIdentity(namespace, workflow)
	var errs []string

	// Remove credentials first
	if c.credential != nil {
		if err := c.credential.Remove(ctx, namespace, workflow); err != nil {
			log.Printf("WARNING: exoskeleton: failed to remove credentials for %s/%s: %v", namespace, workflow, err)
			errs = append(errs, fmt.Sprintf("credentials: %v", err))
		}
	}

	// Unregister from Postgres
	if c.config.PostgresEnabled && c.postgres != nil {
		if err := c.postgres.Unregister(ctx, id); err != nil {
			log.Printf("WARNING: exoskeleton: failed to unregister postgres for %s/%s: %v", namespace, workflow, err)
			errs = append(errs, fmt.Sprintf("postgres: %v", err))
		}
	}

	// Unregister from NATS
	if c.config.NATSEnabled && c.nats != nil {
		if err := c.nats.Unregister(ctx, id); err != nil {
			log.Printf("WARNING: exoskeleton: failed to unregister nats for %s/%s: %v", namespace, workflow, err)
			errs = append(errs, fmt.Sprintf("nats: %v", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("exoskeleton: unregister %s/%s had partial failures: %s", namespace, workflow, strings.Join(errs, "; "))
	}

	return nil
}

// workflowYAMLDeps is the internal representation of a workflow YAML for
// dependency detection. Matches the contract.dependencies map format used
// by the workflow schema (same as extractModuleDeps in deploy.go).
type workflowYAMLDeps struct {
	Contract *contractYAMLDeps `yaml:"contract"`
}

type contractYAMLDeps struct {
	Dependencies map[string]interface{} `yaml:"dependencies"`
}

// DetectExoskeletonDeps parses workflow YAML content and returns dependency
// names that match the "tentacular-" prefix. Dependencies are parsed from
// the contract.dependencies map (keys are dependency names).
func DetectExoskeletonDeps(workflowYAML string) ([]string, error) {
	var doc workflowYAMLDeps
	if err := yaml.Unmarshal([]byte(workflowYAML), &doc); err != nil {
		return nil, fmt.Errorf("parse workflow YAML: %w", err)
	}

	if doc.Contract == nil || doc.Contract.Dependencies == nil {
		return nil, nil
	}

	var deps []string
	for name := range doc.Contract.Dependencies {
		if strings.HasPrefix(name, "tentacular-") {
			deps = append(deps, name)
		}
	}

	return deps, nil
}

// containsDep checks if the given dependency name is in the list.
func containsDep(deps []string, name string) bool {
	for _, d := range deps {
		if d == name {
			return true
		}
	}
	return false
}
