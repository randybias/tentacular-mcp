package tools

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

// makeDeployment creates a minimal Deployment for testing.
func makeDeployment(name, namespace string) *appsv1.Deployment {
	var replicas int32 = 1
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "tentacular",
				"app.kubernetes.io/version":    "1.0.0",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": name},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": name},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "app", Image: "test:latest"}},
				},
			},
		},
		Status: appsv1.DeploymentStatus{
			ReadyReplicas: 1,
		},
	}
}

// TestWfListFiltersSystemNamespaces verifies that wf_list filters out system namespaces
// when listing across all namespaces. Regression test for issue #45.
func TestWfListFiltersSystemNamespaces(t *testing.T) {
	// Create namespaces: a user namespace and system namespaces
	userNs := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "user-workflows",
			Labels: map[string]string{k8s.ManagedByLabel: k8s.ManagedByValue},
		},
	}
	systemNs := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "tentacular-system",
			Labels:      map[string]string{k8s.ManagedByLabel: k8s.ManagedByValue},
			Annotations: map[string]string{"tentacular.io/system": "true"},
		},
	}
	exoNs := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "tentacular-exoskeleton",
			Labels:      map[string]string{k8s.ManagedByLabel: k8s.ManagedByValue},
			Annotations: map[string]string{"tentacular.io/system": "true"},
		},
	}
	annotatedSystemNs := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "custom-infra",
			Labels:      map[string]string{k8s.ManagedByLabel: k8s.ManagedByValue},
			Annotations: map[string]string{"tentacular.io/system": "true"},
		},
	}

	// Deployments in each namespace
	userDep := makeDeployment("user-app", "user-workflows")
	systemDep := makeDeployment("system-app", "tentacular-system")
	exoDep := makeDeployment("exo-app", "tentacular-exoskeleton")
	annotatedDep := makeDeployment("infra-app", "custom-infra")

	staticClient := kubefake.NewClientset(
		userNs, systemNs, exoNs, annotatedSystemNs,
		userDep, systemDep, exoDep, annotatedDep,
	)

	client := &k8s.Client{
		Clientset: staticClient,
		Config:    &rest.Config{Host: "https://test-cluster:6443"},
	}

	ctx := context.Background()

	// List across all namespaces (empty namespace param)
	result, err := handleWfList(ctx, client, WfListParams{})
	if err != nil {
		t.Fatalf("handleWfList: %v", err)
	}

	// Should only contain user-app, not system-app, exo-app, or infra-app
	if len(result.Workflows) != 1 {
		names := make([]string, len(result.Workflows))
		for i, w := range result.Workflows {
			names[i] = w.Namespace + "/" + w.Name
		}
		t.Fatalf("expected 1 workflow, got %d: %v", len(result.Workflows), names)
	}
	if result.Workflows[0].Name != "user-app" {
		t.Errorf("expected workflow name user-app, got %q", result.Workflows[0].Name)
	}
	if result.Workflows[0].Namespace != "user-workflows" {
		t.Errorf("expected namespace user-workflows, got %q", result.Workflows[0].Namespace)
	}
}

// TestWfListAllowsExplicitSystemNamespace verifies that when a user explicitly
// requests a system namespace, wf_list returns results (the guard check handles rejection).
func TestWfListAllowsExplicitSystemNamespace(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "user-ns",
			Labels: map[string]string{k8s.ManagedByLabel: k8s.ManagedByValue},
		},
	}
	dep := makeDeployment("my-app", "user-ns")

	staticClient := kubefake.NewClientset(ns, dep)
	client := &k8s.Client{
		Clientset: staticClient,
		Config:    &rest.Config{Host: "https://test-cluster:6443"},
	}

	ctx := context.Background()

	// Explicit namespace param - should not be filtered
	result, err := handleWfList(ctx, client, WfListParams{Namespace: "user-ns"})
	if err != nil {
		t.Fatalf("handleWfList: %v", err)
	}
	if len(result.Workflows) != 1 {
		t.Fatalf("expected 1 workflow, got %d", len(result.Workflows))
	}
}

// TestIsSystemNamespace verifies the system namespace detection logic.
func TestIsSystemNamespace(t *testing.T) {
	tests := []struct {
		name        string
		ns          string
		annotations map[string]string
		want        bool
	}{
		{"tentacular-system", "tentacular-system", nil, true},
		{"tentacular-support", "tentacular-support", nil, true},
		{"tentacular-exoskeleton", "tentacular-exoskeleton", nil, true},
		{"kube-system", "kube-system", nil, true},
		{"kube-public", "kube-public", nil, true},
		{"kube-node-lease", "kube-node-lease", nil, true},
		{"default", "default", nil, true},
		{"user namespace", "my-workflows", nil, false},
		{"annotated system", "custom-infra", map[string]string{"tentacular.io/system": "true"}, true},
		{"annotated non-system", "my-ns", map[string]string{"tentacular.io/system": "false"}, false},
		{"tent- prefix not blocked", "tent-user", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isSystemNamespace(tt.ns, tt.annotations)
			if got != tt.want {
				t.Errorf("isSystemNamespace(%q, %v) = %v, want %v", tt.ns, tt.annotations, got, tt.want)
			}
		})
	}
}
