package exoskeleton

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ---------- collectExoHosts extended tests ----------

func TestCollectExoHosts_AllServices(t *testing.T) {
	creds := map[string]any{
		"tentacular-postgres": &PostgresCreds{Host: "pg.exo.svc", Port: "5432"},
		"tentacular-nats":     &NATSCreds{URL: "nats://nats.exo.svc:4222"},
		"tentacular-rustfs":   &RustFSCreds{Endpoint: "http://rustfs.exo.svc:9000"},
	}

	hosts := collectExoHosts(creds)
	if len(hosts) != 3 {
		t.Fatalf("expected 3 hosts, got %d: %v", len(hosts), hosts)
	}

	// Verify each host is present (order may vary due to map iteration).
	found := map[string]bool{}
	for _, h := range hosts {
		found[h] = true
	}
	for _, want := range []string{"pg.exo.svc:5432", "nats.exo.svc:4222", "rustfs.exo.svc:9000"} {
		if !found[want] {
			t.Errorf("missing host %q in %v", want, hosts)
		}
	}
}

func TestCollectExoHosts_Empty(t *testing.T) {
	hosts := collectExoHosts(map[string]any{})
	if len(hosts) != 0 {
		t.Errorf("expected 0 hosts, got %d", len(hosts))
	}
}

func TestCollectExoHosts_UnknownType(t *testing.T) {
	creds := map[string]any{
		"tentacular-unknown": "not-a-struct",
	}
	// Should not panic, just log a warning.
	hosts := collectExoHosts(creds)
	if len(hosts) != 0 {
		t.Errorf("expected 0 hosts for unknown type, got %d", len(hosts))
	}
}

// ---------- patchDeploymentAllowNet with command field ----------

func TestPatchDeploymentAllowNet_CommandField(t *testing.T) {
	iArgs := []any{"deno", "run", "--allow-net=existing.host:443", "main.ts"}
	dep := map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata":   map[string]any{"name": "test"},
		"spec": map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"containers": []any{
						map[string]any{
							"name":    "deno",
							"image":   "denoland/deno:latest",
							"command": iArgs,
						},
					},
				},
			},
		},
	}
	manifests := []map[string]any{dep}
	creds := map[string]any{
		"tentacular-postgres": &PostgresCreds{Host: "pg", Port: "5432"},
	}

	patchDeploymentAllowNet(manifests, creds)

	containers := getContainers(dep)
	container := containers[0].(map[string]any)
	cmdSlice, ok := toStringSlice(container["command"])
	if !ok {
		t.Fatal("expected command to be string slice")
	}
	for _, arg := range cmdSlice {
		if strings.HasPrefix(arg, "--allow-net=") {
			if !strings.Contains(arg, "pg:5432") {
				t.Errorf("expected pg:5432 in allow-net, got %s", arg)
			}
			return
		}
	}
	t.Error("expected --allow-net flag in command")
}

func TestPatchDeploymentAllowNet_EmptyAllowNet(t *testing.T) {
	dep := makeDeploymentManifest([]string{"run", "--allow-net=", "main.ts"})
	manifests := []map[string]any{dep}
	creds := map[string]any{
		"tentacular-postgres": &PostgresCreds{Host: "pg", Port: "5432"},
	}

	patchDeploymentAllowNet(manifests, creds)

	containers := getContainers(dep)
	container := containers[0].(map[string]any)
	args, _ := toStringSlice(container["args"])
	for _, arg := range args {
		if arg == "--allow-net=pg:5432" {
			return // success
		}
	}
	t.Error("expected --allow-net=pg:5432 for empty allow-net value")
}

// ---------- toStringSlice edge cases ----------

func TestToStringSlice_NonInterface(t *testing.T) {
	_, ok := toStringSlice("not-a-slice")
	if ok {
		t.Error("expected false for non-slice input")
	}
}

func TestToStringSlice_NonStringElements(t *testing.T) {
	_, ok := toStringSlice([]any{1, 2, 3})
	if ok {
		t.Error("expected false for non-string elements")
	}
}

func TestToStringSlice_Valid(t *testing.T) {
	result, ok := toStringSlice([]any{"a", "b", "c"})
	if !ok {
		t.Fatal("expected true for valid string slice")
	}
	if len(result) != 3 || result[0] != "a" {
		t.Errorf("got %v", result)
	}
}

// ---------- patchDeploymentAllowNet with no containers ----------

func TestPatchDeploymentAllowNet_NoContainers(t *testing.T) {
	dep := map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata":   map[string]any{"name": "test"},
		"spec": map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"containers": []any{},
				},
			},
		},
	}
	manifests := []map[string]any{dep}
	creds := map[string]any{
		"tentacular-postgres": &PostgresCreds{Host: "pg", Port: "5432"},
	}
	// Should not panic.
	patchDeploymentAllowNet(manifests, creds)
}

// ---------- NATSRegistrar Close test ----------

func TestNATSRegistrar_Close(t *testing.T) {
	r := &NATSRegistrar{}
	// Should not panic.
	r.Close()
}

// ---------- SPIRERegistrar Close test ----------

func TestSPIRERegistrar_Close(t *testing.T) {
	r := &SPIRERegistrar{}
	// Should not panic.
	r.Close()
}

// ---------- parseHostPort edge cases ----------

func TestParseHostPort_HostOnly(t *testing.T) {
	host, port := parseHostPort("pg.example.com")
	if host != "pg.example.com" {
		t.Errorf("host = %q", host)
	}
	if port != "" {
		t.Errorf("port = %q, want empty", port)
	}
}

// ---------- AnnotateDeployer with empty AgentType ----------

func TestAnnotateDeployer_EmptyAgentType(t *testing.T) {
	dep := makeDeploymentManifest([]string{"run"})
	manifests := []map[string]any{dep}

	cfg := &Config{Enabled: true}
	ctrl := &Controller{cfg: cfg}

	deployer := DeployerInfo{
		Email: "user@example.com",
	}

	result := ctrl.AnnotateDeployer(manifests, AnnotateDeployerParams{Deployer: deployer})
	obj := &unstructured.Unstructured{Object: result[0]}
	ann := obj.GetAnnotations()
	if ann["tentacular.io/deployed-via"] != "unknown" {
		t.Errorf("expected deployed-via='unknown' for empty AgentType, got %q", ann["tentacular.io/deployed-via"])
	}
}

// ---------- enrichContractDeps edge: no contract ----------

func TestEnrichContractDeps_NoContract(t *testing.T) {
	wfYAML := `
name: no-contract-wf
version: "1.0"
triggers:
  - type: http
`
	cm := makeConfigMapManifest(wfYAML)
	manifests := []map[string]any{cm}
	creds := map[string]any{
		"tentacular-postgres": &PostgresCreds{Host: "pg", Port: "5432"},
	}

	// Should not error, just pass through.
	if err := enrichContractDeps(manifests, creds); err != nil {
		t.Fatalf("enrichContractDeps: %v", err)
	}
}

// ---------- enrichContractDeps edge: no data in ConfigMap ----------

func TestEnrichContractDeps_NoData(t *testing.T) {
	cm := map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata":   map[string]any{"name": "test"},
	}
	manifests := []map[string]any{cm}
	creds := map[string]any{}

	if err := enrichContractDeps(manifests, creds); err != nil {
		t.Fatalf("enrichContractDeps: %v", err)
	}
}

// ---------- enrichContractDeps edge: no dependencies field ----------

func TestEnrichContractDeps_NoDependencies(t *testing.T) {
	wfYAML := `
name: test-wf
contract:
  timeout: 30s
`
	cm := makeConfigMapManifest(wfYAML)
	manifests := []map[string]any{cm}
	creds := map[string]any{
		"tentacular-postgres": &PostgresCreds{Host: "pg", Port: "5432"},
	}

	if err := enrichContractDeps(manifests, creds); err != nil {
		t.Fatalf("enrichContractDeps: %v", err)
	}
}

// ---------- enrichContractDeps edge: non-ConfigMap manifests ----------

func TestEnrichContractDeps_MixedManifests(t *testing.T) {
	dep := map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata":   map[string]any{"name": "test"},
	}
	svc := map[string]any{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata":   map[string]any{"name": "test"},
	}
	manifests := []map[string]any{dep, svc}
	creds := map[string]any{}

	if err := enrichContractDeps(manifests, creds); err != nil {
		t.Fatalf("enrichContractDeps: %v", err)
	}
}

// ---------- patchDeploymentAllowNet with non-container map ----------

func TestPatchDeploymentAllowNet_InvalidContainerType(t *testing.T) {
	dep := map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata":   map[string]any{"name": "test"},
		"spec": map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"containers": []any{
						"not-a-map", // invalid container type
					},
				},
			},
		},
	}
	manifests := []map[string]any{dep}
	creds := map[string]any{
		"tentacular-postgres": &PostgresCreds{Host: "pg", Port: "5432"},
	}
	// Should not panic.
	patchDeploymentAllowNet(manifests, creds)
}

// ---------- patchAllowNetInSlice with no args field ----------

func TestPatchAllowNetInSlice_MissingField(t *testing.T) {
	container := map[string]any{
		"name":  "deno",
		"image": "denoland/deno:latest",
	}
	result := patchAllowNetInSlice(container, "args", []string{"pg:5432"})
	if result {
		t.Error("expected false when field missing")
	}
}

// ---------- patchDeploymentAllowNet with empty creds ----------

func TestPatchDeploymentAllowNet_EmptyCreds(t *testing.T) {
	dep := makeDeploymentManifest([]string{"run", "--allow-net=api:443", "main.ts"})
	manifests := []map[string]any{dep}

	// Empty creds should not modify anything.
	patchDeploymentAllowNet(manifests, map[string]any{})

	containers := getContainers(dep)
	container := containers[0].(map[string]any)
	args, _ := toStringSlice(container["args"])
	for _, arg := range args {
		if arg == "--allow-net=api:443" {
			return
		}
	}
	t.Error("expected --allow-net unchanged with empty creds")
}
