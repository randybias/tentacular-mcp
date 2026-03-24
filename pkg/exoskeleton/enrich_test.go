package exoskeleton

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// makeConfigMapManifest returns a ConfigMap manifest wrapping the given
// workflow.yaml content.
func makeConfigMapManifest(wfYAML string) map[string]any {
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata":   map[string]any{"name": "test-code"},
		"data": map[string]any{
			"workflow.yaml": wfYAML,
		},
	}
}

// makeDeploymentManifest returns a Deployment manifest with the given args.
func makeDeploymentManifest(args []string) map[string]any {
	iArgs := make([]any, len(args))
	for i, a := range args {
		iArgs[i] = a
	}
	return map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata":   map[string]any{"name": "test"},
		"spec": map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"containers": []any{
						map[string]any{
							"name":  "deno",
							"image": "denoland/deno:latest",
							"args":  iArgs,
						},
					},
				},
			},
		},
	}
}

func TestEnrichContractDeps_Postgres(t *testing.T) {
	wfYAML := `
name: test-workflow
version: "1.0"
contract:
  dependencies:
    tentacular-postgres:
      protocol: postgresql
triggers:
  - type: http
nodes:
  ingest:
    path: nodes/ingest.ts
`
	cm := makeConfigMapManifest(wfYAML)
	manifests := []map[string]any{cm}

	creds := map[string]any{
		"tentacular-postgres": &PostgresCreds{
			Host:     "postgres-postgresql.tentacular-exoskeleton.svc.cluster.local",
			Port:     "5432",
			Database: "tentacular",
			User:     "tn_test_ns_test_wf",
			Password: "secret123",
			Schema:   "tn_test_ns_test_wf",
			Protocol: "postgresql",
		},
	}

	if err := enrichContractDeps(manifests, creds); err != nil {
		t.Fatalf("enrichContractDeps: %v", err)
	}

	// Extract the updated workflow.yaml.
	data, ok := cm["data"].(map[string]any)
	if !ok {
		t.Fatal("expected data map")
	}
	enrichedYAML, ok := data["workflow.yaml"].(string)
	if !ok {
		t.Fatal("expected workflow.yaml string")
	}

	// Parse and verify enriched fields.
	var wf map[string]any
	if err := yaml.Unmarshal([]byte(enrichedYAML), &wf); err != nil {
		t.Fatalf("parse enriched yaml: %v", err)
	}

	contract := wf["contract"].(map[string]any)
	deps := contract["dependencies"].(map[string]any)
	pg := deps["tentacular-postgres"].(map[string]any)

	if pg["host"] != "postgres-postgresql.tentacular-exoskeleton.svc.cluster.local" {
		t.Errorf("expected enriched host, got %v", pg["host"])
	}
	if pg["port"] != "5432" {
		t.Errorf("expected enriched port '5432', got %v", pg["port"])
	}
	if pg["database"] != "tentacular" {
		t.Errorf("expected enriched database, got %v", pg["database"])
	}
	if pg["user"] != "tn_test_ns_test_wf" {
		t.Errorf("expected enriched user, got %v", pg["user"])
	}
	if pg["schema"] != "tn_test_ns_test_wf" {
		t.Errorf("expected enriched schema, got %v", pg["schema"])
	}
	// protocol should be preserved from original.
	if pg["protocol"] != "postgresql" {
		t.Errorf("expected protocol preserved, got %v", pg["protocol"])
	}

	// Verify non-contract fields are preserved.
	if wf["name"] != "test-workflow" {
		t.Errorf("expected name preserved, got %v", wf["name"])
	}
	if wf["version"] != "1.0" {
		t.Errorf("expected version preserved, got %v", wf["version"])
	}
	triggers, ok := wf["triggers"].([]any)
	if !ok || len(triggers) != 1 {
		t.Errorf("expected triggers preserved, got %v", wf["triggers"])
	}
	nodes, ok := wf["nodes"].(map[string]any)
	if !ok {
		t.Error("expected nodes preserved")
	}
	if _, ok := nodes["ingest"]; !ok {
		t.Error("expected ingest node preserved")
	}
}

func TestEnrichContractDeps_NATS(t *testing.T) {
	wfYAML := `
name: nats-wf
contract:
  dependencies:
    tentacular-nats:
      protocol: nats
`
	cm := makeConfigMapManifest(wfYAML)
	manifests := []map[string]any{cm}

	creds := map[string]any{
		"tentacular-nats": &NATSCreds{
			URL:           "nats://nats.tentacular-exoskeleton.svc.cluster.local:4222",
			Token:         "tok123",
			SubjectPrefix: "tentacular.test-ns.test-wf.>",
			Protocol:      "nats",
		},
	}

	if err := enrichContractDeps(manifests, creds); err != nil {
		t.Fatalf("enrichContractDeps: %v", err)
	}

	data := cm["data"].(map[string]any)
	var wf map[string]any
	if err := yaml.Unmarshal([]byte(data["workflow.yaml"].(string)), &wf); err != nil {
		t.Fatalf("parse: %v", err)
	}

	contract := wf["contract"].(map[string]any)
	deps := contract["dependencies"].(map[string]any)
	nats := deps["tentacular-nats"].(map[string]any)

	if nats["host"] != "nats.tentacular-exoskeleton.svc.cluster.local" {
		t.Errorf("expected nats host, got %v", nats["host"])
	}
	if nats["port"] != "4222" {
		t.Errorf("expected nats port '4222', got %v", nats["port"])
	}
	if nats["subject"] != "tentacular.test-ns.test-wf.>" {
		t.Errorf("expected nats subject, got %v", nats["subject"])
	}
}

func TestEnrichContractDeps_RustFS(t *testing.T) {
	wfYAML := `
name: rustfs-wf
contract:
  dependencies:
    tentacular-rustfs:
      protocol: s3
`
	cm := makeConfigMapManifest(wfYAML)
	manifests := []map[string]any{cm}

	creds := map[string]any{
		"tentacular-rustfs": &RustFSCreds{
			Endpoint:  "http://rustfs-svc.tentacular-exoskeleton.svc.cluster.local:9000",
			AccessKey: "ak123",
			SecretKey: "sk456",
			Bucket:    "tentacular",
			Prefix:    "ns/test-ns/tentacles/test-wf/",
			Region:    "us-east-1",
			Protocol:  "s3",
		},
	}

	if err := enrichContractDeps(manifests, creds); err != nil {
		t.Fatalf("enrichContractDeps: %v", err)
	}

	data := cm["data"].(map[string]any)
	var wf map[string]any
	if err := yaml.Unmarshal([]byte(data["workflow.yaml"].(string)), &wf); err != nil {
		t.Fatalf("parse: %v", err)
	}

	contract := wf["contract"].(map[string]any)
	deps := contract["dependencies"].(map[string]any)
	rustfs := deps["tentacular-rustfs"].(map[string]any)

	if rustfs["host"] != "rustfs-svc.tentacular-exoskeleton.svc.cluster.local" {
		t.Errorf("expected rustfs host, got %v", rustfs["host"])
	}
	if rustfs["port"] != "9000" {
		t.Errorf("expected rustfs port '9000', got %v", rustfs["port"])
	}
	if rustfs["container"] != "tentacular" {
		t.Errorf("expected rustfs container (bucket), got %v", rustfs["container"])
	}
	if rustfs["prefix"] != "ns/test-ns/tentacles/test-wf/" {
		t.Errorf("expected rustfs prefix, got %v", rustfs["prefix"])
	}
}

func TestEnrichContractDeps_NoTentacularDeps(t *testing.T) {
	wfYAML := `
name: plain-wf
contract:
  dependencies:
    redis:
      protocol: redis
`
	cm := makeConfigMapManifest(wfYAML)
	manifests := []map[string]any{cm}

	creds := map[string]any{}

	if err := enrichContractDeps(manifests, creds); err != nil {
		t.Fatalf("enrichContractDeps: %v", err)
	}

	// Should be unchanged.
	data := cm["data"].(map[string]any)
	if strings.Contains(data["workflow.yaml"].(string), "host:") {
		t.Error("expected no enrichment for non-tentacular deps")
	}
}

func TestEnrichContractDeps_AllThreeServices(t *testing.T) {
	wfYAML := `
name: full-wf
version: "2.0"
contract:
  dependencies:
    tentacular-postgres:
      protocol: postgresql
    tentacular-nats:
      protocol: nats
    tentacular-rustfs:
      protocol: s3
    some-api:
      protocol: http
      host: api.example.com
`
	cm := makeConfigMapManifest(wfYAML)
	manifests := []map[string]any{cm}

	creds := map[string]any{
		"tentacular-postgres": &PostgresCreds{
			Host:     "pg.exo.svc",
			Port:     "5432",
			Database: "tentacular",
			User:     "tn_ns_wf",
			Password: "pw",
			Schema:   "tn_ns_wf",
			Protocol: "postgresql",
		},
		"tentacular-nats": &NATSCreds{
			URL:           "nats://nats.exo.svc:4222",
			Token:         "tok",
			SubjectPrefix: "tentacular.ns.wf.>",
			Protocol:      "nats",
		},
		"tentacular-rustfs": &RustFSCreds{
			Endpoint:  "http://rustfs.exo.svc:9000",
			AccessKey: "ak",
			SecretKey: "sk",
			Bucket:    "tentacular",
			Prefix:    "ns/ns/tentacles/wf/",
			Region:    "us-east-1",
			Protocol:  "s3",
		},
	}

	if err := enrichContractDeps(manifests, creds); err != nil {
		t.Fatalf("enrichContractDeps: %v", err)
	}

	data := cm["data"].(map[string]any)
	var wf map[string]any
	if err := yaml.Unmarshal([]byte(data["workflow.yaml"].(string)), &wf); err != nil {
		t.Fatalf("parse: %v", err)
	}

	contract := wf["contract"].(map[string]any)
	deps := contract["dependencies"].(map[string]any)

	// Check all three are enriched.
	pg := deps["tentacular-postgres"].(map[string]any)
	if pg["host"] != "pg.exo.svc" {
		t.Errorf("pg host: %v", pg["host"])
	}

	nats := deps["tentacular-nats"].(map[string]any)
	if nats["host"] != "nats.exo.svc" {
		t.Errorf("nats host: %v", nats["host"])
	}

	rustfs := deps["tentacular-rustfs"].(map[string]any)
	if rustfs["host"] != "rustfs.exo.svc" {
		t.Errorf("rustfs host: %v", rustfs["host"])
	}

	// Verify non-tentacular dep is unchanged.
	api := deps["some-api"].(map[string]any)
	if api["host"] != "api.example.com" {
		t.Errorf("expected some-api host preserved, got %v", api["host"])
	}
}

func TestPatchDeploymentAllowNet(t *testing.T) {
	dep := makeDeploymentManifest([]string{
		"run",
		"--allow-net=api.example.com:443",
		"main.ts",
	})
	manifests := []map[string]any{dep}

	creds := map[string]any{
		"tentacular-postgres": &PostgresCreds{
			Host: "pg.exo.svc",
			Port: "5432",
		},
		"tentacular-nats": &NATSCreds{
			URL: "nats://nats.exo.svc:4222",
		},
	}

	patchDeploymentAllowNet(manifests, creds)

	containers := getContainers(dep)
	container := containers[0].(map[string]any)
	args, _ := toStringSlice(container["args"])

	found := false
	for _, arg := range args {
		if strings.HasPrefix(arg, "--allow-net=") {
			found = true
			if !strings.Contains(arg, "api.example.com:443") {
				t.Error("expected original host preserved")
			}
			if !strings.Contains(arg, "pg.exo.svc:5432") {
				t.Error("expected postgres host appended")
			}
			if !strings.Contains(arg, "nats.exo.svc:4222") {
				t.Error("expected nats host appended")
			}
		}
	}
	if !found {
		t.Error("expected --allow-net flag in args")
	}
}

func TestPatchDeploymentAllowNet_NoFlag(t *testing.T) {
	dep := makeDeploymentManifest([]string{
		"run",
		"--allow-net",
		"main.ts",
	})
	manifests := []map[string]any{dep}

	creds := map[string]any{
		"tentacular-postgres": &PostgresCreds{
			Host: "pg.exo.svc",
			Port: "5432",
		},
	}

	// --allow-net without = means broad access; should NOT be patched.
	patchDeploymentAllowNet(manifests, creds)

	containers := getContainers(dep)
	container := containers[0].(map[string]any)
	args, _ := toStringSlice(container["args"])

	for _, arg := range args {
		if strings.Contains(arg, "pg.exo.svc") {
			t.Error("should not patch --allow-net (broad access)")
		}
	}
}

func TestPatchDeploymentAllowNet_NoDeployment(t *testing.T) {
	cm := makeConfigMapManifest("name: test")
	manifests := []map[string]any{cm}

	creds := map[string]any{
		"tentacular-postgres": &PostgresCreds{Host: "pg", Port: "5432"},
	}

	// Should not panic.
	patchDeploymentAllowNet(manifests, creds)
}

func TestAnnotateDeployer(t *testing.T) {
	dep := makeDeploymentManifest([]string{"run", "main.ts"})
	cm := makeConfigMapManifest("name: test")
	manifests := []map[string]any{cm, dep}

	cfg := &Config{Enabled: true}
	ctrl := &Controller{cfg: cfg}

	deployer := DeployerInfo{
		Email:     "user@example.com",
		Provider:  "google",
		AgentType: "cli",
		SessionID: "sess-123",
	}

	result := ctrl.AnnotateDeployer(manifests, AnnotateDeployerParams{Deployer: deployer})
	if len(result) != 2 {
		t.Fatalf("expected 2 manifests, got %d", len(result))
	}

	// ConfigMap should not have deployer annotations.
	cmObj := &unstructured.Unstructured{Object: result[0]}
	cmAnn := cmObj.GetAnnotations()
	if len(cmAnn) != 0 {
		t.Error("ConfigMap should not get deployer annotations")
	}

	// Deployment should have deployer annotations.
	depObj := &unstructured.Unstructured{Object: result[1]}
	ann := depObj.GetAnnotations()
	if ann["tentacular.io/deployed-by"] != "user@example.com" {
		t.Errorf("expected deployed-by=user@example.com, got %v", ann["tentacular.io/deployed-by"])
	}
	if ann["tentacular.io/deployed-via"] != "cli" {
		t.Errorf("expected deployed-via=cli, got %v", ann["tentacular.io/deployed-via"])
	}
	if ann["tentacular.io/deployed-at"] == "" {
		t.Error("expected deployed-at timestamp")
	}
}

func TestAnnotateDeployer_NilEmail(t *testing.T) {
	dep := makeDeploymentManifest([]string{"run"})
	manifests := []map[string]any{dep}

	cfg := &Config{Enabled: true}
	ctrl := &Controller{cfg: cfg}

	deployer := DeployerInfo{
		Provider:  "bearer-token",
		AgentType: "mcp-direct",
	}

	result := ctrl.AnnotateDeployer(manifests, AnnotateDeployerParams{Deployer: deployer})
	depObj := &unstructured.Unstructured{Object: result[0]}
	ann := depObj.GetAnnotations()
	if ann["tentacular.io/deployed-by"] != "bearer-token" {
		t.Errorf("expected deployed-by=bearer-token for empty email, got %v", ann["tentacular.io/deployed-by"])
	}
}

func TestParseHostPort(t *testing.T) {
	tests := []struct {
		input    string
		wantHost string
		wantPort string
	}{
		{"nats://nats.exo.svc:4222", "nats.exo.svc", "4222"},
		{"http://rustfs.exo.svc:9000", "rustfs.exo.svc", "9000"},
		{"https://secure.host:443", "secure.host", "443"},
		{"pg.exo.svc:5432", "pg.exo.svc", "5432"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			host, port := parseHostPort(tt.input)
			if host != tt.wantHost {
				t.Errorf("host: got %q, want %q", host, tt.wantHost)
			}
			if port != tt.wantPort {
				t.Errorf("port: got %q, want %q", port, tt.wantPort)
			}
		})
	}
}

func TestNonExoskeletonWorkflowPassthrough(t *testing.T) {
	// A workflow with no tentacular-* deps should pass through enrichment
	// without any changes.
	wfYAML := `
name: plain-workflow
version: "1.0"
contract:
  dependencies:
    redis:
      protocol: redis
      host: redis.default.svc
triggers:
  - type: http
nodes:
  process:
    path: nodes/process.ts
`
	cm := makeConfigMapManifest(wfYAML)
	dep := makeDeploymentManifest([]string{"run", "--allow-net=redis.default.svc:6379", "main.ts"})
	manifests := []map[string]any{cm, dep}

	// No creds - nothing to enrich.
	creds := map[string]any{}
	if err := enrichContractDeps(manifests, creds); err != nil {
		t.Fatalf("enrichContractDeps: %v", err)
	}
	patchDeploymentAllowNet(manifests, creds)

	// Verify workflow.yaml unchanged (no tentacular deps to enrich).
	data := cm["data"].(map[string]any)
	enrichedYAML := data["workflow.yaml"].(string)
	// The original YAML should still be there (not re-serialized since
	// no tentacular deps were modified).
	if !strings.Contains(enrichedYAML, "redis") {
		t.Error("expected redis dep preserved")
	}

	// Verify --allow-net unchanged.
	containers := getContainers(dep)
	container := containers[0].(map[string]any)
	args, _ := toStringSlice(container["args"])
	for _, arg := range args {
		if arg == "--allow-net=redis.default.svc:6379" {
			return // success
		}
	}
	t.Error("expected --allow-net to remain unchanged")
}

// makeNetworkPolicyManifest returns a minimal NetworkPolicy manifest matching
// the CLI's output format, with one existing DNS egress rule.
func makeNetworkPolicyManifest() map[string]any {
	return map[string]any{
		"apiVersion": "networking.k8s.io/v1",
		"kind":       "NetworkPolicy",
		"metadata": map[string]any{
			"name":      "test-workflow-netpol",
			"namespace": "tent-dev",
		},
		"spec": map[string]any{
			"podSelector": map[string]any{
				"matchLabels": map[string]any{
					"app": "test-workflow",
				},
			},
			"policyTypes": []any{"Ingress", "Egress"},
			"egress": []any{
				map[string]any{
					"to": []any{
						map[string]any{
							"namespaceSelector": map[string]any{
								"matchLabels": map[string]any{
									"kubernetes.io/metadata.name": "kube-system",
								},
							},
						},
					},
					"ports": []any{
						map[string]any{
							"protocol": "UDP",
							"port":     int64(53),
						},
					},
				},
			},
		},
	}
}

func TestPatchNetworkPolicyEgress_Postgres(t *testing.T) {
	np := makeNetworkPolicyManifest()
	manifests := []map[string]any{np}

	creds := map[string]any{
		"tentacular-postgres": &PostgresCreds{
			Host: "postgres-postgresql.tentacular-exoskeleton.svc.cluster.local",
			Port: "5432",
		},
	}

	patchNetworkPolicyExoEgress(manifests, creds)

	egress, found, _ := unstructured.NestedSlice(np, "spec", "egress")
	if !found {
		t.Fatal("expected spec.egress to exist")
	}
	// 1 existing DNS rule + 1 new postgres rule
	if len(egress) != 2 {
		t.Fatalf("expected 2 egress rules, got %d", len(egress))
	}

	rule := egress[1].(map[string]any)
	to := rule["to"].([]any)
	nsSelector := to[0].(map[string]any)["namespaceSelector"].(map[string]any)
	matchLabels := nsSelector["matchLabels"].(map[string]any)
	if matchLabels["kubernetes.io/metadata.name"] != "tentacular-exoskeleton" {
		t.Errorf("expected namespace tentacular-exoskeleton, got %v", matchLabels["kubernetes.io/metadata.name"])
	}

	ports := rule["ports"].([]any)
	portEntry := ports[0].(map[string]any)
	if portEntry["port"] != int64(5432) {
		t.Errorf("expected port 5432, got %v", portEntry["port"])
	}
	if portEntry["protocol"] != "TCP" {
		t.Errorf("expected protocol TCP, got %v", portEntry["protocol"])
	}
}

func TestPatchNetworkPolicyEgress_AllServices(t *testing.T) {
	np := makeNetworkPolicyManifest()
	manifests := []map[string]any{np}

	// Use distinct namespaces to verify namespace extraction per credential type.
	creds := map[string]any{
		"tentacular-postgres": &PostgresCreds{
			Host: "pg.ns-postgres.svc.cluster.local",
			Port: "5432",
		},
		"tentacular-nats": &NATSCreds{
			URL: "nats://nats.ns-nats.svc.cluster.local:4222",
		},
		"tentacular-rustfs": &RustFSCreds{
			Endpoint: "http://rustfs-svc.ns-rustfs.svc.cluster.local:9000",
		},
	}

	patchNetworkPolicyExoEgress(manifests, creds)

	egress, found, _ := unstructured.NestedSlice(np, "spec", "egress")
	if !found {
		t.Fatal("expected spec.egress to exist")
	}
	// 1 existing DNS rule + 3 new service rules
	if len(egress) != 4 {
		t.Fatalf("expected 4 egress rules, got %d", len(egress))
	}

	// Rules are sorted by dep name: nats, postgres, rustfs
	type want struct {
		port int64
		ns   string
	}
	wantRules := []want{
		{port: 4222, ns: "ns-nats"},
		{port: 5432, ns: "ns-postgres"},
		{port: 9000, ns: "ns-rustfs"},
	}
	for i, w := range wantRules {
		rule := egress[i+1].(map[string]any)
		ports := rule["ports"].([]any)
		portEntry := ports[0].(map[string]any)
		if portEntry["port"] != w.port {
			t.Errorf("rule %d: expected port %d, got %v", i+1, w.port, portEntry["port"])
		}
		to := rule["to"].([]any)
		nsSelector := to[0].(map[string]any)["namespaceSelector"].(map[string]any)
		matchLabels := nsSelector["matchLabels"].(map[string]any)
		if matchLabels["kubernetes.io/metadata.name"] != w.ns {
			t.Errorf("rule %d: expected namespace %s, got %v", i+1, w.ns, matchLabels["kubernetes.io/metadata.name"])
		}
	}
}

func TestPatchNetworkPolicyEgress_NoNetworkPolicy(t *testing.T) {
	cm := makeConfigMapManifest("name: test")
	dep := makeDeploymentManifest([]string{"run", "main.ts"})
	manifests := []map[string]any{cm, dep}

	creds := map[string]any{
		"tentacular-postgres": &PostgresCreds{
			Host: "pg.exo.svc.cluster.local",
			Port: "5432",
		},
	}

	// Should not panic when no NetworkPolicy is present.
	patchNetworkPolicyExoEgress(manifests, creds)
}

func TestPatchNetworkPolicyEgress_EmptyCreds(t *testing.T) {
	np := makeNetworkPolicyManifest()
	manifests := []map[string]any{np}

	// Use an unrecognized credential type so the loop body executes but
	// the type-switch produces no endpoints. This exercises the full code
	// path (not just the len(creds)==0 early return).
	creds := map[string]any{
		"tentacular-unknown": "not-a-known-creds-struct",
	}

	patchNetworkPolicyExoEgress(manifests, creds)

	egress, found, _ := unstructured.NestedSlice(np, "spec", "egress")
	if !found {
		t.Fatal("expected spec.egress to exist")
	}
	// Only the original DNS rule should remain — no rules added for unknown type.
	if len(egress) != 1 {
		t.Fatalf("expected 1 egress rule (unchanged), got %d", len(egress))
	}
}

func TestPatchNetworkPolicyEgress_NoExistingEgress(t *testing.T) {
	// NetworkPolicy with no spec.egress key — tests the nil initialization branch.
	np := map[string]any{
		"apiVersion": "networking.k8s.io/v1",
		"kind":       "NetworkPolicy",
		"metadata": map[string]any{
			"name":      "test-workflow-netpol",
			"namespace": "tent-dev",
		},
		"spec": map[string]any{
			"podSelector": map[string]any{
				"matchLabels": map[string]any{"app": "test-workflow"},
			},
			"policyTypes": []any{"Ingress", "Egress"},
		},
	}
	manifests := []map[string]any{np}

	creds := map[string]any{
		"tentacular-postgres": &PostgresCreds{
			Host: "pg.tentacular-exoskeleton.svc.cluster.local",
			Port: "5432",
		},
	}

	patchNetworkPolicyExoEgress(manifests, creds)

	egress, found, _ := unstructured.NestedSlice(np, "spec", "egress")
	if !found {
		t.Fatal("expected spec.egress to be created")
	}
	if len(egress) != 1 {
		t.Fatalf("expected 1 egress rule, got %d", len(egress))
	}

	rule := egress[0].(map[string]any)
	ports := rule["ports"].([]any)
	portEntry := ports[0].(map[string]any)
	if portEntry["port"] != int64(5432) {
		t.Errorf("expected port 5432, got %v", portEntry["port"])
	}
}

func TestPatchNetworkPolicyEgress_PreservesExistingRules(t *testing.T) {
	np := makeNetworkPolicyManifest()
	manifests := []map[string]any{np}

	creds := map[string]any{
		"tentacular-nats": &NATSCreds{
			URL: "nats://nats.tentacular-exoskeleton.svc.cluster.local:4222",
		},
	}

	patchNetworkPolicyExoEgress(manifests, creds)

	egress, found, _ := unstructured.NestedSlice(np, "spec", "egress")
	if !found {
		t.Fatal("expected spec.egress to exist")
	}
	if len(egress) != 2 {
		t.Fatalf("expected 2 egress rules, got %d", len(egress))
	}

	// First rule should be the original DNS rule (preserved).
	dnsRule := egress[0].(map[string]any)
	dnsPorts := dnsRule["ports"].([]any)
	dnsPort := dnsPorts[0].(map[string]any)
	if dnsPort["protocol"] != "UDP" {
		t.Errorf("expected original DNS rule preserved with protocol UDP, got %v", dnsPort["protocol"])
	}
	if dnsPort["port"] != int64(53) {
		t.Errorf("expected original DNS rule preserved with port 53, got %v", dnsPort["port"])
	}

	// Second rule should be the new NATS rule.
	natsRule := egress[1].(map[string]any)
	natsPorts := natsRule["ports"].([]any)
	natsPort := natsPorts[0].(map[string]any)
	if natsPort["port"] != int64(4222) {
		t.Errorf("expected NATS port 4222, got %v", natsPort["port"])
	}
}

// getContainers is a test helper to extract containers from a deployment.
func getContainers(dep map[string]any) []any {
	spec, ok := dep["spec"].(map[string]any)
	if !ok {
		return nil
	}
	tmpl, ok := spec["template"].(map[string]any)
	if !ok {
		return nil
	}
	podSpec, ok := tmpl["spec"].(map[string]any)
	if !ok {
		return nil
	}
	containers, _ := podSpec["containers"].([]any)
	return containers
}
