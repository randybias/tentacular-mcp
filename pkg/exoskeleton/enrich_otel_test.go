package exoskeleton

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// makeNamedNetworkPolicyManifest returns a NetworkPolicy with the given name
// and one existing egress rule (DNS to kube-system).
func makeNamedNetworkPolicyManifest(name string) map[string]any {
	return map[string]any{
		"apiVersion": "networking.k8s.io/v1",
		"kind":       "NetworkPolicy",
		"metadata":   map[string]any{"name": name},
		"spec": map[string]any{
			"podSelector": map[string]any{
				"matchLabels": map[string]any{"app": name},
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

// ---------- patchDeploymentOTelEnv ----------

func TestPatchDeploymentOTelEnv_PatchesAllowEnvAndAllowNet(t *testing.T) {
	dep := makeDeploymentManifest([]string{
		"run",
		"--allow-env=DENO_DIR,HOME",
		"--allow-net=localhost:8080",
		"main.ts",
	})
	manifests := []map[string]any{dep}

	patchDeploymentOTelEnv(manifests)

	containers := getContainers(dep)
	container := containers[0].(map[string]any)
	args, _ := toStringSlice(container["args"])

	var allowEnv, allowNet string
	for _, arg := range args {
		if strings.HasPrefix(arg, "--allow-env=") {
			allowEnv = arg
		}
		if strings.HasPrefix(arg, "--allow-net=") {
			allowNet = arg
		}
	}

	for _, v := range otelVars {
		if !strings.Contains(allowEnv, v) {
			t.Errorf("expected --allow-env to contain %q, got: %s", v, allowEnv)
		}
	}
	if !strings.Contains(allowEnv, "DENO_DIR") {
		t.Error("expected original DENO_DIR preserved in --allow-env")
	}
	if !strings.Contains(allowNet, otelCollectorHost) {
		t.Errorf("expected --allow-net to contain %q, got: %s", otelCollectorHost, allowNet)
	}
	if !strings.Contains(allowNet, "localhost:8080") {
		t.Error("expected original localhost:8080 preserved in --allow-net")
	}
}

func TestPatchDeploymentOTelEnv_NoDeployment(t *testing.T) {
	// Only a ConfigMap — should not panic or error.
	cm := makeConfigMapManifest("kind: workflow")
	manifests := []map[string]any{cm}
	patchDeploymentOTelEnv(manifests) // must not panic
}

func TestPatchDeploymentOTelEnv_Idempotent(t *testing.T) {
	dep := makeDeploymentManifest([]string{
		"run",
		"--allow-env=DENO_DIR,HOME",
		"--allow-net=localhost:8080",
		"main.ts",
	})
	manifests := []map[string]any{dep}

	patchDeploymentOTelEnv(manifests)
	patchDeploymentOTelEnv(manifests)

	containers := getContainers(dep)
	container := containers[0].(map[string]any)
	args, _ := toStringSlice(container["args"])

	var allowEnv string
	for _, arg := range args {
		if strings.HasPrefix(arg, "--allow-env=") {
			allowEnv = arg
		}
	}

	// Each OTel var should appear exactly once.
	for _, v := range otelVars {
		count := strings.Count(allowEnv, v)
		if count != 1 {
			t.Errorf("expected %q exactly once in --allow-env, found %d times: %s", v, count, allowEnv)
		}
	}
}

func TestPatchDeploymentOTelEnv_CommandField(t *testing.T) {
	// Deployment uses command instead of args.
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
							"command": []any{"run", "--allow-env=DENO_DIR", "--allow-net=localhost:8080", "main.ts"},
						},
					},
				},
			},
		},
	}
	manifests := []map[string]any{dep}
	patchDeploymentOTelEnv(manifests)

	containers := getContainers(dep)
	container := containers[0].(map[string]any)
	cmd, _ := toStringSlice(container["command"])

	var allowEnv string
	for _, arg := range cmd {
		if strings.HasPrefix(arg, "--allow-env=") {
			allowEnv = arg
		}
	}
	if !strings.Contains(allowEnv, "OTEL_DENO") {
		t.Errorf("expected OTEL_DENO in --allow-env via command field, got: %s", allowEnv)
	}
}

// ---------- patchNetworkPolicyOTelEgress ----------

func TestPatchNetworkPolicyOTelEgress_AddsRule(t *testing.T) {
	np := makeNamedNetworkPolicyManifest("test-workflow-netpol")
	manifests := []map[string]any{np}

	patchNetworkPolicyOTelEgress(manifests, "test-workflow")

	egress, found, _ := unstructured.NestedSlice(np, "spec", "egress")
	if !found {
		t.Fatal("expected spec.egress to exist")
	}
	// 1 existing DNS rule + 1 new OTel rule
	if len(egress) != 2 {
		t.Fatalf("expected 2 egress rules, got %d", len(egress))
	}

	rule := egress[1].(map[string]any)
	to := rule["to"].([]any)
	nsSelector := to[0].(map[string]any)["namespaceSelector"].(map[string]any)
	matchLabels := nsSelector["matchLabels"].(map[string]any)
	if matchLabels["kubernetes.io/metadata.name"] != "tentacular-observability" {
		t.Errorf("expected namespace tentacular-observability, got %v", matchLabels["kubernetes.io/metadata.name"])
	}

	ports := rule["ports"].([]any)
	if len(ports) != 2 {
		t.Fatalf("expected 2 ports in OTel egress rule, got %d", len(ports))
	}
	portNums := map[int64]bool{}
	for _, p := range ports {
		pm := p.(map[string]any)
		portNums[pm["port"].(int64)] = true
		if pm["protocol"] != "TCP" {
			t.Errorf("expected TCP protocol, got %v", pm["protocol"])
		}
	}
	if !portNums[4317] {
		t.Error("expected port 4317 (gRPC) in OTel egress rule")
	}
	if !portNums[4318] {
		t.Error("expected port 4318 (HTTP) in OTel egress rule")
	}
}

func TestPatchNetworkPolicyOTelEgress_NoMatchingPolicy(t *testing.T) {
	np := makeNamedNetworkPolicyManifest("other-netpol")
	manifests := []map[string]any{np}

	// Should log a warning but not panic or error.
	patchNetworkPolicyOTelEgress(manifests, "test-workflow")

	// The other NetworkPolicy should be unchanged.
	egress, _, _ := unstructured.NestedSlice(np, "spec", "egress")
	if len(egress) != 1 {
		t.Errorf("expected 1 egress rule (unchanged), got %d", len(egress))
	}
}

func TestPatchNetworkPolicyOTelEgress_Idempotent(t *testing.T) {
	np := makeNamedNetworkPolicyManifest("test-workflow-netpol")
	manifests := []map[string]any{np}

	patchNetworkPolicyOTelEgress(manifests, "test-workflow")
	patchNetworkPolicyOTelEgress(manifests, "test-workflow")

	egress, _, _ := unstructured.NestedSlice(np, "spec", "egress")
	// Should still be 2 rules (1 DNS + 1 OTel), not 3.
	if len(egress) != 2 {
		t.Fatalf("expected 2 egress rules after idempotent call, got %d", len(egress))
	}
}

func TestPatchNetworkPolicyOTelEgress_PreservesExisting(t *testing.T) {
	np := makeNamedNetworkPolicyManifest("my-wf-netpol")
	manifests := []map[string]any{np}

	patchNetworkPolicyOTelEgress(manifests, "my-wf")

	egress, _, _ := unstructured.NestedSlice(np, "spec", "egress")
	if len(egress) < 2 {
		t.Fatal("expected at least 2 egress rules")
	}
	// First rule should still be the DNS rule to kube-system.
	rule0 := egress[0].(map[string]any)
	to := rule0["to"].([]any)
	nsSelector := to[0].(map[string]any)["namespaceSelector"].(map[string]any)
	matchLabels := nsSelector["matchLabels"].(map[string]any)
	if matchLabels["kubernetes.io/metadata.name"] != "kube-system" {
		t.Errorf("expected first rule to target kube-system, got %v", matchLabels["kubernetes.io/metadata.name"])
	}
}
