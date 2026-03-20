package tools

import (
	"context"
	"errors"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/randybias/tentacular-mcp/pkg/authz"
	"github.com/randybias/tentacular-mcp/pkg/exoskeleton"
)

// ---------- wf_describe deployer annotations tests ----------

func TestWfDescribe_DeployerAnnotations(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	dep := makeTestDeployment("deployed-wf", "dep-ns", map[string]string{
		"tentacular.io/deployed-by":  "user@example.com",
		"tentacular.io/deployed-via": "claude-code",
		"tentacular.io/deployed-at":  "2026-03-10T12:00:00Z",
		"tentacular.io/owner-email":  "platform-team@example.com",
	})
	_, err := client.Clientset.AppsV1().Deployments("dep-ns").Create(ctx, dep, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	result, err := handleWfDescribe(ctx, client, WfDescribeParams{
		Namespace: "dep-ns",
		Name:      "deployed-wf",
	}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("handleWfDescribe: %v", err)
	}

	if result.DeployedBy != "user@example.com" {
		t.Errorf("DeployedBy = %q, want 'user@example.com'", result.DeployedBy)
	}
	if result.DeployedVia != "claude-code" {
		t.Errorf("DeployedVia = %q, want 'claude-code'", result.DeployedVia)
	}
	if result.DeployedAt != "2026-03-10T12:00:00Z" {
		t.Errorf("DeployedAt = %q, want '2026-03-10T12:00:00Z'", result.DeployedAt)
	}
	if result.Owner != "platform-team@example.com" {
		t.Errorf("Owner = %q, want 'platform-team@example.com'", result.Owner)
	}

	// Verify deployer annotations appear in the annotations map.
	if result.Annotations == nil {
		t.Fatal("expected non-nil annotations map")
	}
	if result.Annotations["tentacular.io/deployed-by"] != "user@example.com" {
		t.Errorf("annotations map deployed-by = %q", result.Annotations["tentacular.io/deployed-by"])
	}
	if result.Annotations["tentacular.io/deployed-via"] != "claude-code" {
		t.Errorf("annotations map deployed-via = %q", result.Annotations["tentacular.io/deployed-via"])
	}
	if result.Annotations["tentacular.io/deployed-at"] != "2026-03-10T12:00:00Z" {
		t.Errorf("annotations map deployed-at = %q", result.Annotations["tentacular.io/deployed-at"])
	}
}

func TestWfDescribe_NoDeployerAnnotations(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	dep := makeTestDeployment("no-deployer-wf", "nd-ns", nil)
	_, _ = client.Clientset.AppsV1().Deployments("nd-ns").Create(ctx, dep, metav1.CreateOptions{})

	result, err := handleWfDescribe(ctx, client, WfDescribeParams{
		Namespace: "nd-ns",
		Name:      "no-deployer-wf",
	}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("handleWfDescribe: %v", err)
	}

	if result.DeployedBy != "" {
		t.Errorf("expected empty DeployedBy, got %q", result.DeployedBy)
	}
	if result.DeployedVia != "" {
		t.Errorf("expected empty DeployedVia, got %q", result.DeployedVia)
	}
	if result.DeployedAt != "" {
		t.Errorf("expected empty DeployedAt, got %q", result.DeployedAt)
	}
}

// ---------- wf_describe enrichment from ConfigMap ----------

func TestWfDescribe_EnrichFromConfigMap(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	dep := makeTestDeployment("enriched-wf", "enr-ns", nil)
	dep.Spec.Template.Spec.Containers = []corev1.Container{
		{Name: "deno", Image: "denoland/deno:1.40"},
	}
	_, _ = client.Clientset.AppsV1().Deployments("enr-ns").Create(ctx, dep, metav1.CreateOptions{})

	// Create ConfigMap with workflow.yaml.
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "enriched-wf-code",
			Namespace: "enr-ns",
		},
		Data: map[string]string{
			"workflow.yaml": `
name: enriched-wf
version: "2.5"
triggers:
  - type: cron
    schedule: "0 * * * *"
  - type: http
nodes:
  fetch:
    path: nodes/fetch.ts
  transform:
    path: nodes/transform.ts
  publish:
    path: nodes/publish.ts
`,
		},
	}
	_, _ = client.Clientset.CoreV1().ConfigMaps("enr-ns").Create(ctx, cm, metav1.CreateOptions{})

	result, err := handleWfDescribe(ctx, client, WfDescribeParams{
		Namespace: "enr-ns",
		Name:      "enriched-wf",
	}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("handleWfDescribe: %v", err)
	}

	// Version from ConfigMap should override label.
	if result.Version != "2.5" {
		t.Errorf("Version = %q, want '2.5'", result.Version)
	}

	// Nodes should be populated and sorted.
	if len(result.Nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d: %v", len(result.Nodes), result.Nodes)
	}
	if result.Nodes[0] != "fetch" {
		t.Errorf("first node = %q, want 'fetch'", result.Nodes[0])
	}

	// Triggers should be populated.
	if len(result.Triggers) != 2 {
		t.Fatalf("expected 2 triggers, got %d: %v", len(result.Triggers), result.Triggers)
	}

	// Image should be populated.
	if result.Image != "denoland/deno:1.40" {
		t.Errorf("Image = %q", result.Image)
	}
}

// ---------- containsTag tests ----------

func TestContainsTag(t *testing.T) {
	tests := []struct {
		csv  string
		tag  string
		want bool
	}{
		{"etl,daily,reporting", "etl", true},
		{"etl,daily,reporting", "daily", true},
		{"etl,daily,reporting", "reporting", true},
		{"etl,daily,reporting", "missing", false},
		{"single", "single", true},
		{"single", "other", false},
		{"", "", true},                    // empty tag matches empty split
		{" etl , daily ", "etl", true},    // containsTag trims spaces
		{" etl , daily ", " etl ", false}, // exact match after trim won't match with outer spaces
	}

	for _, tt := range tests {
		got := containsTag(tt.csv, tt.tag)
		if got != tt.want {
			t.Errorf("containsTag(%q, %q) = %v, want %v", tt.csv, tt.tag, got, tt.want)
		}
	}
}

// ---------- wf_list filter tests ----------

func TestWfList_FilterByOwner(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	dep1 := makeTestDeployment("owner-wf", "filter-ns", map[string]string{
		"tentacular.io/owner-email": "team-a@example.com",
	})
	dep2 := makeTestDeployment("other-wf", "filter-ns", map[string]string{
		"tentacular.io/owner-email": "team-b@example.com",
	})
	_, _ = client.Clientset.AppsV1().Deployments("filter-ns").Create(ctx, dep1, metav1.CreateOptions{})
	_, _ = client.Clientset.AppsV1().Deployments("filter-ns").Create(ctx, dep2, metav1.CreateOptions{})

	result, err := handleWfList(ctx, client, WfListParams{
		Namespace: "filter-ns",
		Owner:     "team-a@example.com",
	}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("handleWfList: %v", err)
	}
	if len(result.Workflows) != 1 {
		t.Fatalf("expected 1 workflow, got %d", len(result.Workflows))
	}
	if result.Workflows[0].Name != "owner-wf" {
		t.Errorf("expected 'owner-wf', got %q", result.Workflows[0].Name)
	}
}

func TestWfList_FilterByTag(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	dep1 := makeTestDeployment("tagged-wf", "tag-ns", map[string]string{
		"tentacular.io/tags": "etl,daily",
	})
	dep2 := makeTestDeployment("untagged-wf", "tag-ns", nil)
	_, _ = client.Clientset.AppsV1().Deployments("tag-ns").Create(ctx, dep1, metav1.CreateOptions{})
	_, _ = client.Clientset.AppsV1().Deployments("tag-ns").Create(ctx, dep2, metav1.CreateOptions{})

	result, err := handleWfList(ctx, client, WfListParams{
		Namespace: "tag-ns",
		Tag:       "etl",
	}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("handleWfList: %v", err)
	}
	if len(result.Workflows) != 1 {
		t.Fatalf("expected 1 workflow, got %d", len(result.Workflows))
	}
	if result.Workflows[0].Name != "tagged-wf" {
		t.Errorf("expected 'tagged-wf', got %q", result.Workflows[0].Name)
	}
}

// ---------- wf_list deployer annotations ----------

func TestWfList_DeployerAnnotations(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	dep := makeTestDeployment("deployer-list-wf", "dl-ns", map[string]string{
		"tentacular.io/deployed-by":  "deployer@example.com",
		"tentacular.io/deployed-via": "mcp-client",
	})
	_, _ = client.Clientset.AppsV1().Deployments("dl-ns").Create(ctx, dep, metav1.CreateOptions{})

	result, err := handleWfList(ctx, client, WfListParams{Namespace: "dl-ns"}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("handleWfList: %v", err)
	}
	if len(result.Workflows) != 1 {
		t.Fatal("expected 1 workflow")
	}
	wf := result.Workflows[0]
	if wf.DeployedBy != "deployer@example.com" {
		t.Errorf("DeployedBy = %q", wf.DeployedBy)
	}
	if wf.DeployedVia != "mcp-client" {
		t.Errorf("DeployedVia = %q", wf.DeployedVia)
	}
}

// ---------- wrapListError / wrapGetError tests ----------

func TestWrapListError(t *testing.T) {
	err := wrapListError("", errors.New("test"))
	if err.Error() != "list deployments across all namespaces: test" {
		t.Errorf("got: %v", err)
	}

	err = wrapListError("my-ns", errors.New("test"))
	if err.Error() != `list deployments in namespace "my-ns": test` {
		t.Errorf("got: %v", err)
	}
}

func TestWrapGetError(t *testing.T) {
	err := wrapGetError("my-wf", "my-ns", errors.New("not found"))
	if err.Error() != `get deployment "my-wf" in namespace "my-ns": not found` {
		t.Errorf("got: %v", err)
	}
}

// ---------- deploymentToListEntry test ----------

func TestDeploymentToListEntry(t *testing.T) {
	dep := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-wf",
			Namespace: "test-ns",
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "tentacular",
				"app.kubernetes.io/version":    "3.0",
			},
			Annotations: map[string]string{
				"tentacular.io/owner-email": "data-team@example.com",
				"tentacular.io/group":       "analytics",
				"tentacular.io/environment": "staging",
			},
		},
		Status: appsv1.DeploymentStatus{
			ReadyReplicas: 1,
		},
	}

	entry := deploymentToListEntry(dep)
	if entry.Name != "test-wf" {
		t.Errorf("Name = %q", entry.Name)
	}
	if entry.Version != "3.0" {
		t.Errorf("Version = %q", entry.Version)
	}
	if entry.Owner != "data-team@example.com" {
		t.Errorf("Owner = %q", entry.Owner)
	}
	if entry.Group != "analytics" {
		t.Errorf("Group = %q", entry.Group)
	}
	if entry.Environment != "staging" {
		t.Errorf("Environment = %q", entry.Environment)
	}
	if !entry.Ready {
		t.Error("expected Ready=true with ReadyReplicas=1")
	}
}

// ---------- authz integration tests for wf_list and wf_describe ----------

func TestWfList_AuthzDeny_SkipsResource(t *testing.T) {
	// An OIDC caller who is not the owner (mode rwx------) should get 0 results.
	// Namespace is owned by "sub-owner" with public-read (rwxr--r--) so stranger can
	// read the namespace but not the private workflow inside it.
	client := newWfTestClient()
	ctx := context.Background()

	createOwnedNamespaceWithMode(ctx, client, "authz-ns", "sub-owner", "rwxr--r--")
	dep := makeTestDeployment("private-wf", "authz-ns", map[string]string{
		"tentacular.io/owner-sub": "sub-owner",
		"tentacular.io/mode":      "rwx------",
	})
	_, _ = client.Clientset.AppsV1().Deployments("authz-ns").Create(ctx, dep, metav1.CreateOptions{})

	stranger := &exoskeleton.DeployerInfo{Subject: "sub-stranger", Email: "s@example.com", Provider: "keycloak"}
	eval := authz.NewEvaluator(authz.DefaultMode)

	result, err := handleWfList(ctx, client, WfListParams{Namespace: "authz-ns"}, stranger, eval)
	if err != nil {
		t.Fatalf("handleWfList: %v", err)
	}
	if len(result.Workflows) != 0 {
		t.Errorf("expected 0 workflows for unauthorized caller, got %d", len(result.Workflows))
	}
}

func TestWfList_AuthzAllow_Owner(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	createOwnedNamespace(ctx, client, "authz-ns2", "sub-owner")
	dep := makeTestDeployment("my-wf", "authz-ns2", map[string]string{
		"tentacular.io/owner-sub": "sub-owner",
		"tentacular.io/mode":      "rwx------",
	})
	_, _ = client.Clientset.AppsV1().Deployments("authz-ns2").Create(ctx, dep, metav1.CreateOptions{})

	owner := &exoskeleton.DeployerInfo{Subject: "sub-owner", Email: "o@example.com", Provider: "keycloak"}
	eval := authz.NewEvaluator(authz.DefaultMode)

	result, err := handleWfList(ctx, client, WfListParams{Namespace: "authz-ns2"}, owner, eval)
	if err != nil {
		t.Fatalf("handleWfList: %v", err)
	}
	if len(result.Workflows) != 1 {
		t.Errorf("expected 1 workflow for owner, got %d", len(result.Workflows))
	}
}

func TestWfDescribe_AuthzDeny(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	dep := makeTestDeployment("locked-wf", "authz-ns3", map[string]string{
		"tentacular.io/owner-sub": "sub-owner",
		"tentacular.io/mode":      "rwx------",
	})
	_, _ = client.Clientset.AppsV1().Deployments("authz-ns3").Create(ctx, dep, metav1.CreateOptions{})

	stranger := &exoskeleton.DeployerInfo{Subject: "sub-stranger", Email: "s@example.com", Provider: "keycloak"}
	eval := authz.NewEvaluator(authz.DefaultMode)

	_, err := handleWfDescribe(ctx, client, WfDescribeParams{
		Namespace: "authz-ns3",
		Name:      "locked-wf",
	}, stranger, eval)
	if err == nil {
		t.Error("expected permission denied error for unauthorized caller")
	}
}

func TestWfDescribe_BearerToken_Bypasses_Authz(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	dep := makeTestDeployment("locked-wf2", "authz-ns4", map[string]string{
		"tentacular.io/owner-sub": "sub-owner",
		"tentacular.io/mode":      "rwx------",
	})
	_, _ = client.Clientset.AppsV1().Deployments("authz-ns4").Create(ctx, dep, metav1.CreateOptions{})

	eval := authz.NewEvaluator(authz.DefaultMode)

	_, err := handleWfDescribe(ctx, client, WfDescribeParams{
		Namespace: "authz-ns4",
		Name:      "locked-wf2",
	}, bearerInfo(), eval)
	if err != nil {
		t.Errorf("bearer token should bypass authz, got error: %v", err)
	}
}
