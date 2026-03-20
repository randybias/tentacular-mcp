package tools

// Tests for wf_list and wf_describe handlers - Phase 3 of "Enrich Workflow Metadata for MCP Reporting"
//
// Tests against the implementation in discover.go.
// Uses the existing fake K8s clientset pattern from workflow_test.go.

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// makeTestDeployment creates a fake tentacular Deployment for tests.
func makeTestDeployment(name, namespace string, annotations map[string]string) *appsv1.Deployment {
	labels := map[string]string{
		"app.kubernetes.io/name":       name,
		"app.kubernetes.io/managed-by": "tentacular",
		"app.kubernetes.io/version":    "1.0",
	}
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec:       corev1.PodSpec{},
			},
		},
	}
	return dep
}

func int32Ptr(i int32) *int32 { return &i }

// --- wf_list tests ---

func TestWfListBasic(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	dep := makeTestDeployment("my-workflow", "test-ns", nil)
	_, err := client.Clientset.AppsV1().Deployments("test-ns").Create(ctx, dep, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	result, err := handleWfList(ctx, client, WfListParams{Namespace: "test-ns"}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("handleWfList: %v", err)
	}
	if len(result.Workflows) != 1 {
		t.Errorf("expected 1 workflow, got %d", len(result.Workflows))
	}
	if result.Workflows[0].Name != "my-workflow" {
		t.Errorf("expected name 'my-workflow', got %q", result.Workflows[0].Name)
	}
	if result.Workflows[0].Namespace != "test-ns" {
		t.Errorf("expected namespace 'test-ns', got %q", result.Workflows[0].Namespace)
	}
}

func TestWfListEmpty(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	result, err := handleWfList(ctx, client, WfListParams{Namespace: "empty-ns"}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("handleWfList on empty namespace: %v", err)
	}
	if result.Workflows == nil {
		t.Error("expected non-nil workflows slice for empty namespace")
	}
	if len(result.Workflows) != 0 {
		t.Errorf("expected 0 workflows, got %d", len(result.Workflows))
	}
}

func TestWfListFiltersByManagedByLabel(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	// Create a non-tentacular deployment (no managed-by label)
	otherDep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "other-service",
			Namespace: "mixed-ns",
			Labels: map[string]string{
				"app": "other",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "other"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "other"}},
				Spec:       corev1.PodSpec{},
			},
		},
	}
	_, _ = client.Clientset.AppsV1().Deployments("mixed-ns").Create(ctx, otherDep, metav1.CreateOptions{})

	// handleWfList uses label selector "app.kubernetes.io/managed-by=tentacular"
	// The fake client supports label selector filtering
	result, err := handleWfList(ctx, client, WfListParams{Namespace: "mixed-ns"}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("handleWfList: %v", err)
	}
	for _, wf := range result.Workflows {
		if wf.Name == "other-service" {
			t.Error("expected non-tentacular deployment to be filtered out")
		}
	}
}

func TestWfListReturnsOwnerFromAnnotations(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	dep := makeTestDeployment("annotated-wf", "annot-ns", map[string]string{
		"tentacular.io/owner-email": "platform-team@example.com",
		"tentacular.io/group":       "infra",
	})
	_, _ = client.Clientset.AppsV1().Deployments("annot-ns").Create(ctx, dep, metav1.CreateOptions{})

	result, err := handleWfList(ctx, client, WfListParams{Namespace: "annot-ns"}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("handleWfList: %v", err)
	}
	if len(result.Workflows) == 0 {
		t.Fatal("expected at least one workflow")
	}
	wf := result.Workflows[0]
	if wf.Owner != "platform-team@example.com" {
		t.Errorf("expected owner='platform-team@example.com', got %q", wf.Owner)
	}
	if wf.Group != "infra" {
		t.Errorf("expected group='infra', got %q", wf.Group)
	}
}

func TestWfListReturnsVersionFromLabel(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	dep := makeTestDeployment("version-wf", "ver-ns", nil)
	_, _ = client.Clientset.AppsV1().Deployments("ver-ns").Create(ctx, dep, metav1.CreateOptions{})

	result, err := handleWfList(ctx, client, WfListParams{Namespace: "ver-ns"}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("handleWfList: %v", err)
	}
	if len(result.Workflows) == 0 {
		t.Fatal("expected at least one workflow")
	}
	if result.Workflows[0].Version != "1.0" {
		t.Errorf("expected version='1.0', got %q", result.Workflows[0].Version)
	}
}

func TestWfListEnvironmentFromAnnotations(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	dep := makeTestDeployment("env-wf", "env-ns", map[string]string{
		"tentacular.io/environment": "production",
	})
	_, _ = client.Clientset.AppsV1().Deployments("env-ns").Create(ctx, dep, metav1.CreateOptions{})

	result, err := handleWfList(ctx, client, WfListParams{Namespace: "env-ns"}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("handleWfList: %v", err)
	}
	if len(result.Workflows) == 0 {
		t.Fatal("expected at least one workflow")
	}
	if result.Workflows[0].Environment != "production" {
		t.Errorf("expected environment='production', got %q", result.Workflows[0].Environment)
	}
}

func TestWfListMultipleWorkflows(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	names := []string{"wf-alpha", "wf-beta", "wf-gamma"}
	for _, name := range names {
		dep := makeTestDeployment(name, "multi-ns", nil)
		_, _ = client.Clientset.AppsV1().Deployments("multi-ns").Create(ctx, dep, metav1.CreateOptions{})
	}

	result, err := handleWfList(ctx, client, WfListParams{Namespace: "multi-ns"}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("handleWfList: %v", err)
	}
	if len(result.Workflows) != 3 {
		t.Errorf("expected 3 workflows, got %d", len(result.Workflows))
	}
}

func TestWfListNoAnnotationsNoOwner(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	dep := makeTestDeployment("bare-wf", "bare-ns", nil)
	_, _ = client.Clientset.AppsV1().Deployments("bare-ns").Create(ctx, dep, metav1.CreateOptions{})

	result, err := handleWfList(ctx, client, WfListParams{Namespace: "bare-ns"}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("handleWfList: %v", err)
	}
	if len(result.Workflows) == 0 {
		t.Fatal("expected at least one workflow")
	}
	wf := result.Workflows[0]
	// Without annotations, owner/group/environment should be empty strings
	if wf.Owner != "" {
		t.Errorf("expected empty owner for workflow without annotations, got %q", wf.Owner)
	}
	if wf.Group != "" {
		t.Errorf("expected empty group for workflow without annotations, got %q", wf.Group)
	}
}

// --- wf_describe tests ---

func TestWfDescribeBasic(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	dep := makeTestDeployment("described-wf", "desc-ns", map[string]string{
		"tentacular.io/owner-email": "ops-team",
	})
	_, _ = client.Clientset.AppsV1().Deployments("desc-ns").Create(ctx, dep, metav1.CreateOptions{})

	result, err := handleWfDescribe(ctx, client, WfDescribeParams{
		Namespace: "desc-ns",
		Name:      "described-wf",
	}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("handleWfDescribe: %v", err)
	}
	if result.Name != "described-wf" {
		t.Errorf("expected name='described-wf', got %q", result.Name)
	}
	if result.Namespace != "desc-ns" {
		t.Errorf("expected namespace='desc-ns', got %q", result.Namespace)
	}
	if result.Owner != "ops-team" { //nolint:gocritic // owner-email maps to Owner field
		t.Errorf("expected owner='ops-team', got %q", result.Owner)
	}
}

func TestWfDescribeVersionFromLabel(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	dep := makeTestDeployment("ver-describe-wf", "vd-ns", nil)
	_, _ = client.Clientset.AppsV1().Deployments("vd-ns").Create(ctx, dep, metav1.CreateOptions{})

	result, err := handleWfDescribe(ctx, client, WfDescribeParams{
		Namespace: "vd-ns",
		Name:      "ver-describe-wf",
	}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("handleWfDescribe: %v", err)
	}
	if result.Version != "1.0" {
		t.Errorf("expected version='1.0', got %q", result.Version)
	}
}

func TestWfDescribeTagsSplit(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	dep := makeTestDeployment("tags-describe-wf", "td-ns", map[string]string{
		"tentacular.io/tags": "etl,daily,reporting",
	})
	_, _ = client.Clientset.AppsV1().Deployments("td-ns").Create(ctx, dep, metav1.CreateOptions{})

	result, err := handleWfDescribe(ctx, client, WfDescribeParams{
		Namespace: "td-ns",
		Name:      "tags-describe-wf",
	}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("handleWfDescribe: %v", err)
	}
	if len(result.Tags) != 3 {
		t.Errorf("expected 3 tags, got %d: %v", len(result.Tags), result.Tags)
	}
	if result.Tags[0] != "etl" {
		t.Errorf("expected first tag='etl', got %q", result.Tags[0])
	}
	if result.Tags[2] != "reporting" {
		t.Errorf("expected third tag='reporting', got %q", result.Tags[2])
	}
}

func TestWfDescribeNoTagsAnnotation(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	dep := makeTestDeployment("no-tags-wf", "nt-ns", nil)
	_, _ = client.Clientset.AppsV1().Deployments("nt-ns").Create(ctx, dep, metav1.CreateOptions{})

	result, err := handleWfDescribe(ctx, client, WfDescribeParams{
		Namespace: "nt-ns",
		Name:      "no-tags-wf",
	}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("handleWfDescribe: %v", err)
	}
	// No tags annotation = nil or empty slice
	if len(result.Tags) != 0 {
		t.Errorf("expected 0 tags for workflow without tags annotation, got %d: %v", len(result.Tags), result.Tags)
	}
}

func TestWfDescribeMissingDeployment(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	_, err := handleWfDescribe(ctx, client, WfDescribeParams{
		Namespace: "absent-ns",
		Name:      "nonexistent-wf",
	}, bearerInfo(), bearerEval())
	if err == nil {
		t.Error("expected error when workflow Deployment not found")
	}
}

func TestWfDescribeAnnotationsMapOnlyTentacular(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	dep := makeTestDeployment("ann-wf", "ann-ns", map[string]string{
		"tentacular.io/owner-email":          "platform-team",
		"tentacular.io/group":                "infra",
		"kubectl.kubernetes.io/last-applied": "{}",
		"deployment.kubernetes.io/revision":  "1",
	})
	_, _ = client.Clientset.AppsV1().Deployments("ann-ns").Create(ctx, dep, metav1.CreateOptions{})

	result, err := handleWfDescribe(ctx, client, WfDescribeParams{
		Namespace: "ann-ns",
		Name:      "ann-wf",
	}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("handleWfDescribe: %v", err)
	}
	// Annotations map should only include tentacular.io/* keys
	for k := range result.Annotations {
		if !hasPrefix(k, "tentacular.io/") {
			t.Errorf("expected only tentacular.io/* annotations, got key %q", k)
		}
	}
	// Should include the authz annotations
	if result.Annotations["tentacular.io/owner-email"] != "platform-team" {
		t.Errorf("expected tentacular.io/owner-email='platform-team', got %q", result.Annotations["tentacular.io/owner-email"])
	}
}

// hasPrefix is a simple helper for prefix matching in annotations
func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func TestWfDescribeNoAnnotationsNilAnnotationsMap(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	dep := makeTestDeployment("no-ann-wf", "na-ns", nil)
	_, _ = client.Clientset.AppsV1().Deployments("na-ns").Create(ctx, dep, metav1.CreateOptions{})

	result, err := handleWfDescribe(ctx, client, WfDescribeParams{
		Namespace: "na-ns",
		Name:      "no-ann-wf",
	}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("handleWfDescribe: %v", err)
	}
	// No tentacular.io/* annotations means Annotations map should be nil
	if result.Annotations != nil {
		t.Errorf("expected nil annotations map when no tentacular.io/* annotations present, got %v", result.Annotations)
	}
}

func TestWfDescribeReplicasFromSpec(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	dep := makeTestDeployment("replicas-wf", "rep-ns", nil)
	// makeTestDeployment sets Replicas to 1 via int32Ptr(1)
	_, _ = client.Clientset.AppsV1().Deployments("rep-ns").Create(ctx, dep, metav1.CreateOptions{})

	result, err := handleWfDescribe(ctx, client, WfDescribeParams{
		Namespace: "rep-ns",
		Name:      "replicas-wf",
	}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("handleWfDescribe: %v", err)
	}
	if result.Replicas != 1 {
		t.Errorf("expected replicas=1, got %d", result.Replicas)
	}
}

func TestWfDescribeReadyFalseWhenNoReadyReplicas(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	dep := makeTestDeployment("not-ready-wf", "nr-ns", nil)
	// Status.ReadyReplicas defaults to 0 in fake client
	_, _ = client.Clientset.AppsV1().Deployments("nr-ns").Create(ctx, dep, metav1.CreateOptions{})

	result, err := handleWfDescribe(ctx, client, WfDescribeParams{
		Namespace: "nr-ns",
		Name:      "not-ready-wf",
	}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("handleWfDescribe: %v", err)
	}
	// ReadyReplicas is 0 by default (fake client doesn't simulate controller)
	if result.Ready {
		t.Error("expected Ready=false when ReadyReplicas=0")
	}
}

func TestWfDescribeAllMetadataFields(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	dep := makeTestDeployment("full-meta-wf", "full-ns", map[string]string{
		"tentacular.io/owner-email": "data-team@example.com",
		"tentacular.io/group":       "analytics",
		"tentacular.io/tags":        "etl,daily",
		"tentacular.io/environment": "production",
	})
	_, _ = client.Clientset.AppsV1().Deployments("full-ns").Create(ctx, dep, metav1.CreateOptions{})

	result, err := handleWfDescribe(ctx, client, WfDescribeParams{
		Namespace: "full-ns",
		Name:      "full-meta-wf",
	}, bearerInfo(), bearerEval())
	if err != nil {
		t.Fatalf("handleWfDescribe: %v", err)
	}
	if result.Owner != "data-team@example.com" {
		t.Errorf("expected owner='data-team@example.com', got %q", result.Owner)
	}
	if result.Group != "analytics" {
		t.Errorf("expected group='analytics', got %q", result.Group)
	}
	if result.Environment != "production" {
		t.Errorf("expected environment='production', got %q", result.Environment)
	}
	if len(result.Tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(result.Tags))
	}
}
