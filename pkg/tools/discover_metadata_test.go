package tools

// Tests for B2 metadata-enrichment:
//   - Tier 1 annotation parsing in wf_describe
//   - Tier 2 metadata ConfigMap reading
//   - Backward compatibility with pre-metadata deployments

import (
	"context"
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// makeMetadataAnnotations builds a full set of Tier 1 metadata annotations.
func makeMetadataAnnotations() map[string]string {
	nodes, _ := json.Marshal([]string{"fetch", "analyze", "report"})
	edges, _ := json.Marshal([][2]string{{"fetch", "analyze"}, {"analyze", "report"}})
	sidecars, _ := json.Marshal([]SidecarMeta{{Name: "chromium", Image: "chromium:latest", Port: 9222}})
	deps, _ := json.Marshal([]DependencyMeta{
		{Name: "tentacular-postgres", Protocol: "postgresql", Managed: true},
		{Name: "openai-api", Protocol: "https", Managed: false},
	})
	return map[string]string{
		"tentacular.io/nodes":         string(nodes),
		"tentacular.io/edges":         string(edges),
		"tentacular.io/sidecars":      string(sidecars),
		"tentacular.io/dependencies":  string(deps),
		"tentacular.io/trigger-type":  "cron",
		"tentacular.io/scaffold-name": "video-analyzer",
		"tentacular.io/metadata-ref":  "my-wf-metadata",
		"tentacular.io/version":       "1.2.0+a1b2c3d",
	}
}

// makeMetadataConfigMap builds a Tier 2 metadata ConfigMap.
func makeMetadataConfigMap(name, namespace string) *corev1.ConfigMap {
	prov, _ := json.Marshal(GitProvenance{
		Commit: "a1b2c3d",
		Branch: "main",
		Repo:   "git@github.com:user/tentacles.git",
		Dirty:  false,
	})
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string]string{
			"readme":         "# My Workflow\nThis is the readme.",
			"contract":       "## Contract\nDependencies: postgres, openai.",
			"params_schema":  `{"type":"object","properties":{"input":{"type":"string"}}}`,
			"git_provenance": string(prov),
		},
	}
}

func TestWfDescribe_Tier1Annotations(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	dep := makeTestDeployment("meta-wf", "meta-ns", makeMetadataAnnotations())
	if _, err := client.Clientset.AppsV1().Deployments("meta-ns").Create(ctx, dep, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create deployment: %v", err)
	}

	result, err := handleWfDescribe(ctx, client, WfDescribeParams{
		Namespace: "meta-ns",
		Name:      "meta-wf",
	}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("handleWfDescribe: %v", err)
	}

	// Nodes from annotation.
	if len(result.Nodes) != 3 {
		t.Errorf("Nodes: got %d, want 3: %v", len(result.Nodes), result.Nodes)
	}
	if len(result.Nodes) > 0 && result.Nodes[0] != "fetch" {
		t.Errorf("Nodes[0] = %q, want fetch", result.Nodes[0])
	}

	// Edges from annotation.
	if len(result.Edges) != 2 {
		t.Errorf("Edges: got %d, want 2", len(result.Edges))
	}
	if len(result.Edges) > 0 && (result.Edges[0][0] != "fetch" || result.Edges[0][1] != "analyze") {
		t.Errorf("Edges[0] = %v, want [fetch analyze]", result.Edges[0])
	}

	// Sidecars from annotation.
	if len(result.Sidecars) != 1 {
		t.Errorf("Sidecars: got %d, want 1", len(result.Sidecars))
	}
	if len(result.Sidecars) > 0 {
		sc := result.Sidecars[0]
		if sc.Name != "chromium" || sc.Port != 9222 {
			t.Errorf("Sidecar = %+v, want name=chromium port=9222", sc)
		}
	}

	// Dependencies from annotation.
	if len(result.Dependencies) != 2 {
		t.Errorf("Dependencies: got %d, want 2", len(result.Dependencies))
	}

	// Scalar Tier 1 fields.
	if result.TriggerType != "cron" {
		t.Errorf("TriggerType = %q, want cron", result.TriggerType)
	}
	if result.ScaffoldName != "video-analyzer" {
		t.Errorf("ScaffoldName = %q, want video-analyzer", result.ScaffoldName)
	}
	if result.MetadataRef != "my-wf-metadata" {
		t.Errorf("MetadataRef = %q, want my-wf-metadata", result.MetadataRef)
	}
	// Annotation version overrides label.
	if result.Version != "1.2.0+a1b2c3d" {
		t.Errorf("Version = %q, want 1.2.0+a1b2c3d", result.Version)
	}
}

func TestWfDescribe_Tier2ConfigMap(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	ann := map[string]string{
		"tentacular.io/metadata-ref": "my-wf-metadata",
	}
	dep := makeTestDeployment("meta-cm-wf", "cm-ns", ann)
	if _, err := client.Clientset.AppsV1().Deployments("cm-ns").Create(ctx, dep, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create deployment: %v", err)
	}

	cm := makeMetadataConfigMap("my-wf-metadata", "cm-ns")
	if _, err := client.Clientset.CoreV1().ConfigMaps("cm-ns").Create(ctx, cm, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create configmap: %v", err)
	}

	result, err := handleWfDescribe(ctx, client, WfDescribeParams{
		Namespace: "cm-ns",
		Name:      "meta-cm-wf",
	}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("handleWfDescribe: %v", err)
	}

	if result.Readme == "" {
		t.Error("Readme should be populated from ConfigMap")
	}
	if result.ContractSummary == "" {
		t.Error("ContractSummary should be populated from ConfigMap")
	}
	if result.ParamsSchema == "" {
		t.Error("ParamsSchema should be populated from ConfigMap")
	}
	if result.GitProvenance == nil {
		t.Fatal("GitProvenance should be populated from ConfigMap")
	}
	if result.GitProvenance.Commit != "a1b2c3d" {
		t.Errorf("GitProvenance.Commit = %q, want a1b2c3d", result.GitProvenance.Commit)
	}
	if result.GitProvenance.Branch != "main" {
		t.Errorf("GitProvenance.Branch = %q, want main", result.GitProvenance.Branch)
	}
	if result.GitProvenance.Dirty {
		t.Error("GitProvenance.Dirty should be false")
	}
}

func TestWfDescribe_MissingMetadataConfigMap(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	// Deployment has a metadata-ref annotation but the ConfigMap doesn't exist.
	ann := map[string]string{
		"tentacular.io/metadata-ref": "nonexistent-metadata",
	}
	dep := makeTestDeployment("no-meta-cm-wf", "nometa-ns", ann)
	if _, err := client.Clientset.AppsV1().Deployments("nometa-ns").Create(ctx, dep, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create deployment: %v", err)
	}

	// Should not error — missing ConfigMap is non-fatal.
	result, err := handleWfDescribe(ctx, client, WfDescribeParams{
		Namespace: "nometa-ns",
		Name:      "no-meta-cm-wf",
	}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("handleWfDescribe should not error on missing metadata ConfigMap: %v", err)
	}

	// Tier 2 fields should be empty.
	if result.Readme != "" || result.ContractSummary != "" || result.ParamsSchema != "" || result.GitProvenance != nil {
		t.Error("Tier 2 fields should be empty when ConfigMap is missing")
	}
}

func TestWfDescribe_PartialMetadataConfigMap(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	dep := makeTestDeployment("partial-meta-wf", "partial-ns", map[string]string{
		"tentacular.io/metadata-ref": "partial-metadata",
	})
	if _, err := client.Clientset.AppsV1().Deployments("partial-ns").Create(ctx, dep, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create deployment: %v", err)
	}

	// ConfigMap has only readme and params_schema; no contract or git_provenance.
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "partial-metadata",
			Namespace: "partial-ns",
		},
		Data: map[string]string{
			"readme":        "# Partial",
			"params_schema": `{"type":"object"}`,
		},
	}
	if _, err := client.Clientset.CoreV1().ConfigMaps("partial-ns").Create(ctx, cm, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create configmap: %v", err)
	}

	result, err := handleWfDescribe(ctx, client, WfDescribeParams{
		Namespace: "partial-ns",
		Name:      "partial-meta-wf",
	}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("handleWfDescribe: %v", err)
	}

	if result.Readme == "" {
		t.Error("Readme should be populated")
	}
	if result.ParamsSchema == "" {
		t.Error("ParamsSchema should be populated")
	}
	// These should remain empty.
	if result.ContractSummary != "" {
		t.Errorf("ContractSummary should be empty, got %q", result.ContractSummary)
	}
	if result.GitProvenance != nil {
		t.Errorf("GitProvenance should be nil, got %+v", result.GitProvenance)
	}
}

func TestWfDescribe_InvalidAnnotationJSON_GracefulSkip(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	// Annotations with invalid JSON should be skipped, not error.
	dep := makeTestDeployment("bad-ann-wf", "badann-ns", map[string]string{
		"tentacular.io/nodes":        `not-valid-json`,
		"tentacular.io/edges":        `[broken`,
		"tentacular.io/sidecars":     `{not array}`,
		"tentacular.io/dependencies": `null-ish`,
	})
	if _, err := client.Clientset.AppsV1().Deployments("badann-ns").Create(ctx, dep, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create deployment: %v", err)
	}

	result, err := handleWfDescribe(ctx, client, WfDescribeParams{
		Namespace: "badann-ns",
		Name:      "bad-ann-wf",
	}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("handleWfDescribe should not error on invalid JSON annotations: %v", err)
	}

	// All fields should be nil/empty — invalid JSON is skipped.
	if len(result.Nodes) != 0 {
		t.Errorf("Nodes should be empty after JSON parse failure, got %v", result.Nodes)
	}
	if len(result.Edges) != 0 {
		t.Errorf("Edges should be empty after JSON parse failure, got %v", result.Edges)
	}
	if len(result.Sidecars) != 0 {
		t.Errorf("Sidecars should be empty after JSON parse failure, got %v", result.Sidecars)
	}
	if len(result.Dependencies) != 0 {
		t.Errorf("Dependencies should be empty after JSON parse failure, got %v", result.Dependencies)
	}
}

func TestWfDescribe_BackwardCompat_NoMetadataAnnotations(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	// Pre-metadata deployment: no Tier 1 annotations, no metadata ConfigMap.
	// The code ConfigMap still provides nodes/triggers.
	dep := makeTestDeployment("old-wf", "old-ns", map[string]string{
		"tentacular.io/owner": "alice@example.com",
	})
	if _, err := client.Clientset.AppsV1().Deployments("old-ns").Create(ctx, dep, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create deployment: %v", err)
	}

	codeCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "old-wf-code",
			Namespace: "old-ns",
		},
		Data: map[string]string{
			"workflow.yaml": `
name: old-wf
version: "0.5"
triggers:
  - type: http
nodes:
  run:
    path: nodes/run.ts
`,
		},
	}
	if _, err := client.Clientset.CoreV1().ConfigMaps("old-ns").Create(ctx, codeCM, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create code configmap: %v", err)
	}

	result, err := handleWfDescribe(ctx, client, WfDescribeParams{
		Namespace: "old-ns",
		Name:      "old-wf",
	}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("handleWfDescribe: %v", err)
	}

	// ConfigMap-derived fields still work.
	if result.Version != "0.5" {
		t.Errorf("Version = %q, want 0.5", result.Version)
	}
	if len(result.Nodes) != 1 || result.Nodes[0] != "run" {
		t.Errorf("Nodes = %v, want [run]", result.Nodes)
	}
	if len(result.Triggers) != 1 || result.Triggers[0] != "http" {
		t.Errorf("Triggers = %v, want [http]", result.Triggers)
	}

	// Tier 1/2 fields are absent.
	if len(result.Edges) != 0 || len(result.Sidecars) != 0 || len(result.Dependencies) != 0 {
		t.Error("Tier 1 structured fields should be empty for pre-metadata deployment")
	}
	if result.Readme != "" || result.ContractSummary != "" || result.GitProvenance != nil {
		t.Error("Tier 2 fields should be empty for pre-metadata deployment")
	}
}

func TestWfDescribe_AnnotationVersionOverridesConfigMap(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	// Deployment has annotation version AND a code ConfigMap with a different version.
	// Annotation should win.
	dep := makeTestDeployment("ver-ann-wf", "verans-ns", map[string]string{
		"tentacular.io/version": "2.0.0+abc1234",
	})
	if _, err := client.Clientset.AppsV1().Deployments("verans-ns").Create(ctx, dep, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create deployment: %v", err)
	}

	codeCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ver-ann-wf-code",
			Namespace: "verans-ns",
		},
		Data: map[string]string{
			"workflow.yaml": `name: ver-ann-wf
version: "1.0"
`,
		},
	}
	if _, err := client.Clientset.CoreV1().ConfigMaps("verans-ns").Create(ctx, codeCM, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create code configmap: %v", err)
	}

	result, err := handleWfDescribe(ctx, client, WfDescribeParams{
		Namespace: "verans-ns",
		Name:      "ver-ann-wf",
	}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("handleWfDescribe: %v", err)
	}

	if result.Version != "2.0.0+abc1234" {
		t.Errorf("Version = %q; annotation version should override ConfigMap version", result.Version)
	}
}

func TestWfDescribe_PromptsFromConfigMap(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	dep := makeTestDeployment("prompt-wf", "prompt-ns", map[string]string{
		"tentacular.io/metadata-ref":   "prompt-wf-metadata",
		"tentacular.io/prompt-count":   "2",
		"tentacular.io/template-count": "1",
	})
	if _, err := client.Clientset.AppsV1().Deployments("prompt-ns").Create(ctx, dep, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create deployment: %v", err)
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "prompt-wf-metadata",
			Namespace: "prompt-ns",
		},
		Data: map[string]string{
			"prompts": `prompts:
  - node: analyze
    name: classify
    description: "Classify input data"
    model: claude-sonnet-4-20250514
    system_prompt: "You are a classifier."
    user_prompt_template: "Classify: {{input}}"
    tools:
      - name: web_search
        description: "Search the web"
  - node: report
    name: summarize
    description: "Summarize results"
    model: claude-sonnet-4-20250514
templates:
  - node: report
    name: report_template
    description: "HTML report template"
    format: html
    template: "<h1>{{title}}</h1>"
`,
		},
	}
	if _, err := client.Clientset.CoreV1().ConfigMaps("prompt-ns").Create(ctx, cm, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create configmap: %v", err)
	}

	result, err := handleWfDescribe(ctx, client, WfDescribeParams{
		Namespace: "prompt-ns",
		Name:      "prompt-wf",
	}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("handleWfDescribe: %v", err)
	}

	// Prompts
	if len(result.Prompts) != 2 {
		t.Fatalf("Prompts: got %d, want 2", len(result.Prompts))
	}
	p0 := result.Prompts[0]
	if p0.Node != "analyze" || p0.Name != "classify" {
		t.Errorf("Prompts[0] = node=%q name=%q, want analyze/classify", p0.Node, p0.Name)
	}
	if p0.Model != "claude-sonnet-4-20250514" {
		t.Errorf("Prompts[0].Model = %q, want claude-sonnet-4-20250514", p0.Model)
	}
	if p0.SystemPrompt != "You are a classifier." {
		t.Errorf("Prompts[0].SystemPrompt = %q", p0.SystemPrompt)
	}
	if p0.UserPromptTemplate != "Classify: {{input}}" {
		t.Errorf("Prompts[0].UserPromptTemplate = %q", p0.UserPromptTemplate)
	}
	if len(p0.Tools) != 1 || p0.Tools[0].Name != "web_search" {
		t.Errorf("Prompts[0].Tools = %+v, want [{web_search ...}]", p0.Tools)
	}

	// Templates
	if len(result.Templates) != 1 {
		t.Fatalf("Templates: got %d, want 1", len(result.Templates))
	}
	tpl := result.Templates[0]
	if tpl.Node != "report" || tpl.Name != "report_template" {
		t.Errorf("Templates[0] = node=%q name=%q, want report/report_template", tpl.Node, tpl.Name)
	}
	if tpl.Format != "html" {
		t.Errorf("Templates[0].Format = %q, want html", tpl.Format)
	}
	if tpl.Template != "<h1>{{title}}</h1>" {
		t.Errorf("Templates[0].Template = %q", tpl.Template)
	}

	// Annotation counts should be in the annotations map.
	if result.Annotations["tentacular.io/prompt-count"] != "2" {
		t.Errorf("prompt-count annotation = %q, want 2", result.Annotations["tentacular.io/prompt-count"])
	}
	if result.Annotations["tentacular.io/template-count"] != "1" {
		t.Errorf("template-count annotation = %q, want 1", result.Annotations["tentacular.io/template-count"])
	}
}

func TestWfDescribe_PromptsKeyAbsent(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	dep := makeTestDeployment("noprompt-wf", "noprompt-ns", map[string]string{
		"tentacular.io/metadata-ref": "noprompt-wf-metadata",
	})
	if _, err := client.Clientset.AppsV1().Deployments("noprompt-ns").Create(ctx, dep, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create deployment: %v", err)
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "noprompt-wf-metadata",
			Namespace: "noprompt-ns",
		},
		Data: map[string]string{
			"readme": "# No prompts here",
		},
	}
	if _, err := client.Clientset.CoreV1().ConfigMaps("noprompt-ns").Create(ctx, cm, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create configmap: %v", err)
	}

	result, err := handleWfDescribe(ctx, client, WfDescribeParams{
		Namespace: "noprompt-ns",
		Name:      "noprompt-wf",
	}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("handleWfDescribe: %v", err)
	}

	if len(result.Prompts) != 0 {
		t.Errorf("Prompts should be empty, got %d", len(result.Prompts))
	}
	if len(result.Templates) != 0 {
		t.Errorf("Templates should be empty, got %d", len(result.Templates))
	}
}

func TestWfDescribe_PromptsInvalidYAML(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	dep := makeTestDeployment("badprompt-wf", "badprompt-ns", map[string]string{
		"tentacular.io/metadata-ref": "badprompt-wf-metadata",
	})
	if _, err := client.Clientset.AppsV1().Deployments("badprompt-ns").Create(ctx, dep, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create deployment: %v", err)
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "badprompt-wf-metadata",
			Namespace: "badprompt-ns",
		},
		Data: map[string]string{
			"prompts": `{{{not valid yaml at all`,
		},
	}
	if _, err := client.Clientset.CoreV1().ConfigMaps("badprompt-ns").Create(ctx, cm, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create configmap: %v", err)
	}

	result, err := handleWfDescribe(ctx, client, WfDescribeParams{
		Namespace: "badprompt-ns",
		Name:      "badprompt-wf",
	}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("handleWfDescribe should not error on invalid YAML: %v", err)
	}

	if len(result.Prompts) != 0 {
		t.Errorf("Prompts should be empty after parse failure, got %d", len(result.Prompts))
	}
	if len(result.Templates) != 0 {
		t.Errorf("Templates should be empty after parse failure, got %d", len(result.Templates))
	}
}

func TestWfDescribe_NodeDescriptionsPresent(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	dep := makeTestDeployment("nodedesc-wf", "nodedesc-ns", map[string]string{
		"tentacular.io/metadata-ref": "nodedesc-wf-metadata",
	})
	if _, err := client.Clientset.AppsV1().Deployments("nodedesc-ns").Create(ctx, dep, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create deployment: %v", err)
	}

	nodeDescs, _ := json.Marshal([]NodeMeta{
		{Name: "fetch-data", Description: "Fetches data from external API"},
		{Name: "analyze", Description: "Runs LLM analysis on fetched data"},
	})
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nodedesc-wf-metadata",
			Namespace: "nodedesc-ns",
		},
		Data: map[string]string{
			"node_descriptions": string(nodeDescs),
			"prompts": `prompts:
  - node: analyze
    name: data-analysis
    model: claude-3-haiku
`,
		},
	}
	if _, err := client.Clientset.CoreV1().ConfigMaps("nodedesc-ns").Create(ctx, cm, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create configmap: %v", err)
	}

	result, err := handleWfDescribe(ctx, client, WfDescribeParams{
		Namespace: "nodedesc-ns",
		Name:      "nodedesc-wf",
	}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("handleWfDescribe: %v", err)
	}

	// NodeDescriptions come from the node_descriptions JSON key.
	if len(result.NodeDescriptions) != 2 {
		t.Fatalf("NodeDescriptions: got %d, want 2", len(result.NodeDescriptions))
	}
	if result.NodeDescriptions[0].Name != "fetch-data" {
		t.Errorf("NodeDescriptions[0].Name = %q, want fetch-data", result.NodeDescriptions[0].Name)
	}
	if result.NodeDescriptions[0].Description != "Fetches data from external API" {
		t.Errorf("NodeDescriptions[0].Description = %q", result.NodeDescriptions[0].Description)
	}
	if result.NodeDescriptions[1].Name != "analyze" {
		t.Errorf("NodeDescriptions[1].Name = %q, want analyze", result.NodeDescriptions[1].Name)
	}
	if result.NodeDescriptions[1].Description != "Runs LLM analysis on fetched data" {
		t.Errorf("NodeDescriptions[1].Description = %q", result.NodeDescriptions[1].Description)
	}

	// Prompts should still parse correctly from the prompts key.
	if len(result.Prompts) != 1 {
		t.Fatalf("Prompts: got %d, want 1", len(result.Prompts))
	}
	if result.Prompts[0].Node != "analyze" || result.Prompts[0].Name != "data-analysis" {
		t.Errorf("Prompts[0] = node=%q name=%q, want analyze/data-analysis", result.Prompts[0].Node, result.Prompts[0].Name)
	}
}

func TestWfDescribe_NodeDescriptionsAbsent(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	dep := makeTestDeployment("nonodedesc-wf", "nonodedesc-ns", map[string]string{
		"tentacular.io/metadata-ref": "nonodedesc-wf-metadata",
	})
	if _, err := client.Clientset.AppsV1().Deployments("nonodedesc-ns").Create(ctx, dep, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create deployment: %v", err)
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nonodedesc-wf-metadata",
			Namespace: "nonodedesc-ns",
		},
		Data: map[string]string{
			"prompts": `prompts:
  - node: analyze
    name: classify
    model: claude-sonnet-4-20250514
`,
		},
	}
	if _, err := client.Clientset.CoreV1().ConfigMaps("nonodedesc-ns").Create(ctx, cm, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create configmap: %v", err)
	}

	result, err := handleWfDescribe(ctx, client, WfDescribeParams{
		Namespace: "nonodedesc-ns",
		Name:      "nonodedesc-wf",
	}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("handleWfDescribe: %v", err)
	}

	// NodeDescriptions should be nil/empty when no nodes section exists.
	if len(result.NodeDescriptions) != 0 {
		t.Errorf("NodeDescriptions should be empty, got %d: %+v", len(result.NodeDescriptions), result.NodeDescriptions)
	}

	// Prompts should still work.
	if len(result.Prompts) != 1 {
		t.Fatalf("Prompts: got %d, want 1", len(result.Prompts))
	}
}

func TestWfDescribe_FallbackMetadataConfigMapName(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	// No metadata-ref annotation, but has a metadata annotation (nodes) — should
	// fall back to "<name>-metadata" naming convention. Deployments with zero
	// metadata annotations are treated as pre-metadata and skip the lookup entirely.
	dep := makeTestDeployment("fallback-meta-wf", "fb-ns", map[string]string{
		"tentacular.io/nodes": `["node-a"]`,
	})
	if _, err := client.Clientset.AppsV1().Deployments("fb-ns").Create(ctx, dep, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create deployment: %v", err)
	}

	// ConfigMap uses fallback name convention.
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "fallback-meta-wf-metadata",
			Namespace: "fb-ns",
		},
		Data: map[string]string{
			"readme": "# Fallback",
		},
	}
	if _, err := client.Clientset.CoreV1().ConfigMaps("fb-ns").Create(ctx, cm, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create configmap: %v", err)
	}

	result, err := handleWfDescribe(ctx, client, WfDescribeParams{
		Namespace: "fb-ns",
		Name:      "fallback-meta-wf",
	}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("handleWfDescribe: %v", err)
	}

	if result.Readme == "" {
		t.Error("Readme should be populated via fallback ConfigMap name convention")
	}
}
