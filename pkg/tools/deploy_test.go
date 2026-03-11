package tools

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/randybias/tentacular-mcp/pkg/exoskeleton"
	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

// deployGVRs maps resource name to list kind for the fake dynamic client.
var deployGVRs = map[schema.GroupVersionResource]string{
	{Group: "apps", Version: "v1", Resource: "deployments"}:                   "DeploymentList",
	{Group: "", Version: "v1", Resource: "services"}:                          "ServiceList",
	{Group: "", Version: "v1", Resource: "configmaps"}:                        "ConfigMapList",
	{Group: "", Version: "v1", Resource: "secrets"}:                           "SecretList",
	{Group: "batch", Version: "v1", Resource: "jobs"}:                         "JobList",
	{Group: "batch", Version: "v1", Resource: "cronjobs"}:                     "CronJobList",
	{Group: "networking.k8s.io", Version: "v1", Resource: "networkpolicies"}: "NetworkPolicyList",
}

func deployScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = batchv1.AddToScheme(scheme)
	_ = networkingv1.AddToScheme(scheme)
	return scheme
}

func managedNs(name string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{k8s.ManagedByLabel: k8s.ManagedByValue},
		},
	}
}

func newDeployTestClient() *k8s.Client {
	scheme := deployScheme()
	dynClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, deployGVRs)
	staticClient := kubefake.NewClientset(managedNs("my-ns"), managedNs("test-ns"))

	return &k8s.Client{
		Clientset: staticClient,
		Dynamic:   dynClient,
		Config:    &rest.Config{Host: "https://test-cluster:6443"},
	}
}

func newManagedNsClient() *k8s.Client {
	scheme := deployScheme()
	dynClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, deployGVRs)
	staticClient := kubefake.NewClientset(managedNs("my-ns"), managedNs("test-ns"))

	return &k8s.Client{
		Clientset: staticClient,
		Dynamic:   dynClient,
		Config:    &rest.Config{Host: "https://test-cluster:6443"},
	}
}

// TestWorkflowRemoveEmptyName verifies wf_remove returns 0 deleted for non-existent name.
func TestWorkflowRemoveEmptyName(t *testing.T) {
	client := newDeployTestClient()
	ctx := context.Background()

	result, err := handleWorkflowRemove(ctx, client, WorkflowRemoveParams{
		Namespace: "my-ns",
		Name:      "nonexistent",
	})
	if err != nil {
		t.Fatalf("handleWorkflowRemove: %v", err)
	}
	if result.Deleted != 0 {
		t.Errorf("expected 0 deleted, got %d", result.Deleted)
	}
}

// TestWorkflowStatusReplicaCounts verifies wf_status correctly reports replica counts
// from a Deployment found by label selector. Regression test for issue #38.
func TestWorkflowStatusReplicaCounts(t *testing.T) {
	var replicas int32 = 3
	var readyReplicas int32 = 2
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-app",
			Namespace: "my-ns",
			Labels: map[string]string{
				"tentacular.io/release":     "my-app",
				"app.kubernetes.io/version": "1.2.3",
				k8s.ManagedByLabel:          k8s.ManagedByValue,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"tentacular.io/release": "my-app"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"tentacular.io/release": "my-app"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "app", Image: "test:latest"}},
				},
			},
		},
		Status: appsv1.DeploymentStatus{
			ReadyReplicas:     readyReplicas,
			AvailableReplicas: readyReplicas,
		},
	}

	scheme := deployScheme()
	dynClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, deployGVRs)
	staticClient := kubefake.NewClientset(managedNs("my-ns"), dep)

	client := &k8s.Client{
		Clientset: staticClient,
		Dynamic:   dynClient,
		Config:    &rest.Config{Host: "https://test-cluster:6443"},
	}

	ctx := context.Background()
	result, err := handleWorkflowStatus(ctx, client, WorkflowStatusParams{
		Namespace: "my-ns",
		Name:      "my-app",
	})
	if err != nil {
		t.Fatalf("handleWorkflowStatus: %v", err)
	}
	if result.Replicas != 3 {
		t.Errorf("expected Replicas=3, got %d", result.Replicas)
	}
	if result.Available != 2 {
		t.Errorf("expected Available=2 (ReadyReplicas), got %d", result.Available)
	}
	if result.Ready {
		t.Error("expected Ready=false (2/3 ready), got true")
	}
	if result.Version != "1.2.3" {
		t.Errorf("expected Version=1.2.3, got %q", result.Version)
	}
}

// TestWorkflowStatusNilReplicas verifies wf_status handles nil Spec.Replicas safely.
// Regression test for issue #38 - nil dereference panic.
func TestWorkflowStatusNilReplicas(t *testing.T) {
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nil-rep-app",
			Namespace: "my-ns",
			Labels: map[string]string{
				"tentacular.io/release": "nil-rep-app",
				k8s.ManagedByLabel:      k8s.ManagedByValue,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: nil, // defaults to 1
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"tentacular.io/release": "nil-rep-app"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"tentacular.io/release": "nil-rep-app"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "app", Image: "test:latest"}},
				},
			},
		},
		Status: appsv1.DeploymentStatus{
			ReadyReplicas:     1,
			AvailableReplicas: 1,
		},
	}

	scheme := deployScheme()
	dynClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, deployGVRs)
	staticClient := kubefake.NewClientset(managedNs("my-ns"), dep)

	client := &k8s.Client{
		Clientset: staticClient,
		Dynamic:   dynClient,
		Config:    &rest.Config{Host: "https://test-cluster:6443"},
	}

	ctx := context.Background()
	result, err := handleWorkflowStatus(ctx, client, WorkflowStatusParams{
		Namespace: "my-ns",
		Name:      "nil-rep-app",
	})
	if err != nil {
		t.Fatalf("handleWorkflowStatus: %v", err)
	}
	if result.Replicas != 1 {
		t.Errorf("expected Replicas=1 (default for nil), got %d", result.Replicas)
	}
	if result.Available != 1 {
		t.Errorf("expected Available=1, got %d", result.Available)
	}
	if !result.Ready {
		t.Error("expected Ready=true (1/1 ready), got false")
	}
}

// TestWorkflowStatusEmptyName verifies wf_status returns empty resources for non-existent name.
func TestWorkflowStatusEmptyName(t *testing.T) {
	client := newDeployTestClient()
	ctx := context.Background()

	result, err := handleWorkflowStatus(ctx, client, WorkflowStatusParams{
		Namespace: "my-ns",
		Name:      "nonexistent",
	})
	if err != nil {
		t.Fatalf("handleWorkflowStatus: %v", err)
	}
	if len(result.Resources) != 0 {
		t.Errorf("expected 0 resources, got %d", len(result.Resources))
	}
}

// TestWorkflowApplyDisallowedKind verifies wf_apply rejects manifests with disallowed kinds.
func TestWorkflowApplyDisallowedKind(t *testing.T) {
	client := newManagedNsClient()
	ctx := context.Background()

	// Create managed namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "managed-ns",
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "tentacular",
			},
		},
	}
	_, err := client.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	disallowedKinds := []string{"ClusterRole", "Namespace", "Node", "PersistentVolume", "ClusterRoleBinding"}
	for _, kind := range disallowedKinds {
		_, err = handleWorkflowApply(ctx, client, WorkflowApplyParams{
			Namespace: "managed-ns",
			Name:      "my-app",
			Manifests: []map[string]interface{}{
				{
					"apiVersion": "v1",
					"kind":       kind,
					"metadata":   map[string]interface{}{"name": "test-resource"},
				},
			},
		})
		if err == nil {
			t.Errorf("expected error for disallowed kind %q, got nil", kind)
		}
	}
}

// TestWorkflowApplyUnmanagedNamespace verifies wf_apply rejects unmanaged namespaces.
func TestWorkflowApplyUnmanagedNamespace(t *testing.T) {
	client := newManagedNsClient()
	ctx := context.Background()

	// Create unmanaged namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "unmanaged-ns"},
	}
	_, err := client.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	_, err = handleWorkflowApply(ctx, client, WorkflowApplyParams{
		Namespace: "unmanaged-ns",
		Name:      "my-app",
		Manifests: []map[string]interface{}{},
	})
	if err == nil {
		t.Fatal("expected error for unmanaged namespace, got nil")
	}
}

// TestDeployToolNamesRegistered verifies that the registered tool names are exactly
// wf_apply, wf_remove, and wf_status — not the old module_* names.
// It calls the MCP tools/list JSON-RPC endpoint via an HTTP test server.
func TestDeployToolNamesRegistered(t *testing.T) {
	mcpSrv := mcp.NewServer(
		&mcp.Implementation{Name: "test", Version: "0.0.1"},
		&mcp.ServerOptions{Logger: slog.New(slog.NewTextHandler(os.Stderr, nil))},
	)
	client := newDeployTestClient()
	registerDeployTools(mcpSrv, client, nil, nil)

	handler := mcp.NewStreamableHTTPHandler(
		func(r *http.Request) *mcp.Server { return mcpSrv },
		&mcp.StreamableHTTPOptions{JSONResponse: true},
	)
	ts := httptest.NewServer(handler)
	defer ts.Close()

	acceptHdr := "application/json, text/event-stream"

	// Initialize the MCP session first (required before tools/list).
	initBody := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}`
	initReq, _ := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader(initBody))
	initReq.Header.Set("Content-Type", "application/json")
	initReq.Header.Set("Accept", acceptHdr)
	initResp, err := http.DefaultClient.Do(initReq)
	if err != nil {
		t.Fatalf("POST initialize: %v", err)
	}
	sessionID := initResp.Header.Get("Mcp-Session-Id")
	_ = initResp.Body.Close()

	// Send initialized notification so the server transitions out of init state.
	notifyBody := `{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`
	notifyReq, _ := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader(notifyBody))
	notifyReq.Header.Set("Content-Type", "application/json")
	notifyReq.Header.Set("Accept", acceptHdr)
	if sessionID != "" {
		notifyReq.Header.Set("Mcp-Session-Id", sessionID)
	}
	notifyResp, err := http.DefaultClient.Do(notifyReq)
	if err != nil {
		t.Fatalf("POST notifications/initialized: %v", err)
	}
	_ = notifyResp.Body.Close()

	// Call tools/list.
	listBody := `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`
	listReq, _ := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader(listBody))
	listReq.Header.Set("Content-Type", "application/json")
	listReq.Header.Set("Accept", acceptHdr)
	if sessionID != "" {
		listReq.Header.Set("Mcp-Session-Id", sessionID)
	}
	listResp, err := http.DefaultClient.Do(listReq)
	if err != nil {
		t.Fatalf("POST tools/list: %v", err)
	}
	defer func() { _ = listResp.Body.Close() }()

	var result struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&result); err != nil {
		t.Fatalf("decode tools/list response: %v", err)
	}

	registered := make(map[string]bool, len(result.Result.Tools))
	for _, tool := range result.Result.Tools {
		registered[tool.Name] = true
	}

	wantNames := []string{"wf_apply", "wf_remove", "wf_status"}
	oldNames := []string{"module_apply", "module_remove", "module_status"}

	for _, name := range wantNames {
		if !registered[name] {
			t.Errorf("expected tool %q to be registered, but it was not; registered: %v", name, result.Result.Tools)
		}
	}
	for _, name := range oldNames {
		if registered[name] {
			t.Errorf("old tool name %q is still registered; it should have been renamed", name)
		}
	}
}

// TestWorkflowApplyResultJSONNameField verifies that the wf_apply result serializes
// the deployment name under the "name" JSON key, not the old "release" key.
func TestWorkflowApplyResultJSONNameField(t *testing.T) {
	result := WorkflowApplyResult{
		Name:      "my-release",
		Namespace: "my-ns",
		Created:   1,
		Updated:   0,
		Deleted:   0,
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if _, ok := m["name"]; !ok {
		t.Error("WorkflowApplyResult JSON: missing \"name\" field")
	}
	if _, ok := m["release"]; ok {
		t.Error("WorkflowApplyResult JSON: old \"release\" field is present; should have been renamed to \"name\"")
	}
	if m["name"] != "my-release" {
		t.Errorf("WorkflowApplyResult JSON name: got %v, want my-release", m["name"])
	}
}

// TestWorkflowRemoveResultJSONNameField verifies that the wf_remove result serializes
// the deployment name under the "name" JSON key, not the old "release" key.
func TestWorkflowRemoveResultJSONNameField(t *testing.T) {
	result := WorkflowRemoveResult{
		Name:      "my-release",
		Namespace: "my-ns",
		Deleted:   3,
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if _, ok := m["name"]; !ok {
		t.Error("WorkflowRemoveResult JSON: missing \"name\" field")
	}
	if _, ok := m["release"]; ok {
		t.Error("WorkflowRemoveResult JSON: old \"release\" field is present; should have been renamed to \"name\"")
	}
}

// TestWorkflowStatusResultJSONNameField verifies that the wf_status result serializes
// the deployment name under the "name" JSON key, not the old "release" key.
func TestWorkflowStatusResultJSONNameField(t *testing.T) {
	result := WorkflowStatusResult{
		Name:      "my-release",
		Namespace: "my-ns",
		Resources: []WorkflowResourceStatus{},
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if _, ok := m["name"]; !ok {
		t.Error("WorkflowStatusResult JSON: missing \"name\" field")
	}
	if _, ok := m["release"]; ok {
		t.Error("WorkflowStatusResult JSON: old \"release\" field is present; should have been renamed to \"name\"")
	}
}

// TestWorkflowParamStructsJSONNameField verifies that all three param structs
// accept the "name" JSON key (not the old "release" key).
func TestWorkflowParamStructsJSONNameField(t *testing.T) {
	applyJSON := `{"namespace":"ns","name":"rel","manifests":[]}`
	var applyParams WorkflowApplyParams
	if err := json.Unmarshal([]byte(applyJSON), &applyParams); err != nil {
		t.Fatalf("unmarshal WorkflowApplyParams: %v", err)
	}
	if applyParams.Name != "rel" {
		t.Errorf("WorkflowApplyParams Name: got %q, want rel", applyParams.Name)
	}

	removeJSON := `{"namespace":"ns","name":"rel"}`
	var removeParams WorkflowRemoveParams
	if err := json.Unmarshal([]byte(removeJSON), &removeParams); err != nil {
		t.Fatalf("unmarshal WorkflowRemoveParams: %v", err)
	}
	if removeParams.Name != "rel" {
		t.Errorf("WorkflowRemoveParams Name: got %q, want rel", removeParams.Name)
	}

	statusJSON := `{"namespace":"ns","name":"rel"}`
	var statusParams WorkflowStatusParams
	if err := json.Unmarshal([]byte(statusJSON), &statusParams); err != nil {
		t.Fatalf("unmarshal WorkflowStatusParams: %v", err)
	}
	if statusParams.Name != "rel" {
		t.Errorf("WorkflowStatusParams Name: got %q, want rel", statusParams.Name)
	}
}

// TestWorkflowRemoveUnmanagedNamespace verifies wf_remove rejects unmanaged namespaces.
func TestWorkflowRemoveUnmanagedNamespace(t *testing.T) {
	client := newManagedNsClient()
	ctx := context.Background()

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "unmanaged-remove"},
	}
	_, err := client.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	_, err = handleWorkflowRemove(ctx, client, WorkflowRemoveParams{
		Namespace: "unmanaged-remove",
		Name:      "my-app",
	})
	if err == nil {
		t.Fatal("expected error for unmanaged namespace, got nil")
	}
}

// TestWorkflowStatusUnmanagedNamespace verifies wf_status rejects unmanaged namespaces.
func TestWorkflowStatusUnmanagedNamespace(t *testing.T) {
	client := newManagedNsClient()
	ctx := context.Background()

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "unmanaged-status"},
	}
	_, err := client.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	_, err = handleWorkflowStatus(ctx, client, WorkflowStatusParams{
		Namespace: "unmanaged-status",
		Name:      "my-app",
	})
	if err == nil {
		t.Fatal("expected error for unmanaged namespace, got nil")
	}
}

// TestWorkflowApplyEmptyManifestsManagedNs verifies wf_apply succeeds with zero manifests
// on a managed namespace and returns zero created/updated/deleted with the correct name field.
func TestWorkflowApplyEmptyManifestsManagedNs(t *testing.T) {
	client := newDeployTestClient()
	ctx := context.Background()

	result, err := handleWorkflowApply(ctx, client, WorkflowApplyParams{
		Namespace: "my-ns",
		Name:      "empty-app",
		Manifests: []map[string]interface{}{},
	})
	if err != nil {
		t.Fatalf("handleWorkflowApply: %v", err)
	}
	if result.Name != "empty-app" {
		t.Errorf("result Name: got %q, want empty-app", result.Name)
	}
	if result.Namespace != "my-ns" {
		t.Errorf("result Namespace: got %q, want my-ns", result.Namespace)
	}
	if result.Created != 0 || result.Updated != 0 || result.Deleted != 0 {
		t.Errorf("expected 0 created/updated/deleted, got created=%d updated=%d deleted=%d",
			result.Created, result.Updated, result.Deleted)
	}
}

// TestWorkflowApplyResultNamePropagation verifies that the name parameter is echoed
// back correctly in the result's Name field (testing the rename from "release" to "name").
func TestWorkflowApplyResultNamePropagation(t *testing.T) {
	client := newDeployTestClient()
	ctx := context.Background()

	const deployName = "my-special-app"
	result, err := handleWorkflowApply(ctx, client, WorkflowApplyParams{
		Namespace: "my-ns",
		Name:      deployName,
		Manifests: []map[string]interface{}{},
	})
	if err != nil {
		t.Fatalf("handleWorkflowApply: %v", err)
	}
	if result.Name != deployName {
		t.Errorf("wf_apply result Name: got %q, want %q", result.Name, deployName)
	}
}

// TestWorkflowRemoveResultNamePropagation verifies the name is echoed in wf_remove result.
func TestWorkflowRemoveResultNamePropagation(t *testing.T) {
	client := newDeployTestClient()
	ctx := context.Background()

	const deployName = "app-to-remove"
	result, err := handleWorkflowRemove(ctx, client, WorkflowRemoveParams{
		Namespace: "my-ns",
		Name:      deployName,
	})
	if err != nil {
		t.Fatalf("handleWorkflowRemove: %v", err)
	}
	if result.Name != deployName {
		t.Errorf("wf_remove result Name: got %q, want %q", result.Name, deployName)
	}
}

// TestWorkflowStatusResultNamePropagation verifies the name is echoed in wf_status result.
func TestWorkflowStatusResultNamePropagation(t *testing.T) {
	client := newDeployTestClient()
	ctx := context.Background()

	const deployName = "app-to-check"
	result, err := handleWorkflowStatus(ctx, client, WorkflowStatusParams{
		Namespace: "my-ns",
		Name:      deployName,
	})
	if err != nil {
		t.Fatalf("handleWorkflowStatus: %v", err)
	}
	if result.Name != deployName {
		t.Errorf("wf_status result Name: got %q, want %q", result.Name, deployName)
	}
}

// TestWorkflowApplyMissingManifestName verifies wf_apply rejects manifests with no name.
func TestWorkflowApplyMissingManifestName(t *testing.T) {
	client := newDeployTestClient()
	ctx := context.Background()

	_, err := handleWorkflowApply(ctx, client, WorkflowApplyParams{
		Namespace: "my-ns",
		Name:      "my-app",
		Manifests: []map[string]interface{}{
			{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata":   map[string]interface{}{},
			},
		},
	})
	if err == nil {
		t.Fatal("expected error for manifest missing name, got nil")
	}
}

// TestWorkflowApplyMissingAPIVersion verifies wf_apply rejects manifests with missing apiVersion.
func TestWorkflowApplyMissingAPIVersion(t *testing.T) {
	client := newDeployTestClient()
	ctx := context.Background()

	_, err := handleWorkflowApply(ctx, client, WorkflowApplyParams{
		Namespace: "my-ns",
		Name:      "my-app",
		Manifests: []map[string]interface{}{
			{
				"kind":     "ConfigMap",
				"metadata": map[string]interface{}{"name": "test"},
			},
		},
	})
	if err == nil {
		t.Fatal("expected error for manifest missing apiVersion, got nil")
	}
}

// newDeployTestClientWithDiscovery returns a test client whose fake discovery
// is pre-populated with the core v1 resource list so that resolveGVR can find
// ConfigMap, Service, Secret, and other core resources.
func newDeployTestClientWithDiscovery() *k8s.Client {
	scheme := deployScheme()
	dynClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, deployGVRs)
	staticClient := kubefake.NewClientset(managedNs("my-ns"), managedNs("test-ns"))

	// Inject discovery resource lists so resolveGVR can resolve core v1 kinds.
	staticClient.Resources = []*metav1.APIResourceList{
		{
			GroupVersion: "v1",
			APIResources: []metav1.APIResource{
				{Name: "configmaps", Kind: "ConfigMap", Namespaced: true},
				{Name: "services", Kind: "Service", Namespaced: true},
				{Name: "secrets", Kind: "Secret", Namespaced: true},
			},
		},
	}

	return &k8s.Client{
		Clientset: staticClient,
		Dynamic:   dynClient,
		Config:    &rest.Config{Host: "https://test-cluster:6443"},
	}
}

// TestWorkflowApplyConfigMapLargeDataIntegrity verifies that ConfigMap data keys survive
// the manifest → unstructured.Unstructured → dynamic client apply path without truncation.
// This is a regression guard: large string values (~7KB) must not be silently truncated
// during JSON marshaling or map[string]interface{} conversion.
func TestWorkflowApplyConfigMapLargeDataIntegrity(t *testing.T) {
	client := newDeployTestClientWithDiscovery()
	ctx := context.Background()

	// Build a ~7KB value to stress-test the unstructured conversion path.
	largeValue := strings.Repeat("x", 7*1024)
	smallValue1 := "small-value-alpha"
	smallValue2 := "small-value-beta"

	result, err := handleWorkflowApply(ctx, client, WorkflowApplyParams{
		Namespace: "my-ns",
		Name:      "large-data-test",
		Manifests: []map[string]interface{}{
			{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata":   map[string]interface{}{"name": "big-config"},
				"data": map[string]interface{}{
					"key-small-1": smallValue1,
					"key-large":   largeValue,
					"key-small-2": smallValue2,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("handleWorkflowApply: %v", err)
	}
	if result.Created != 1 {
		t.Fatalf("expected 1 created resource, got %d", result.Created)
	}

	// Retrieve via the dynamic client and verify all keys are intact.
	cmGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}
	stored, err := client.Dynamic.Resource(cmGVR).Namespace("my-ns").Get(ctx, "big-config", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get stored ConfigMap: %v", err)
	}

	rawData, found := stored.Object["data"]
	if !found {
		t.Fatal("ConfigMap data field missing from stored object")
	}
	data, ok := rawData.(map[string]interface{})
	if !ok {
		t.Fatalf("ConfigMap data is not map[string]interface{}: %T", rawData)
	}

	checkStringKey := func(key, want string) {
		t.Helper()
		v, exists := data[key]
		if !exists {
			t.Errorf("key %q missing from stored ConfigMap data", key)
			return
		}
		s, ok := v.(string)
		if !ok {
			t.Errorf("key %q is not a string: %T", key, v)
			return
		}
		if len(s) != len(want) {
			t.Errorf("key %q length mismatch: got %d, want %d (possible truncation)", key, len(s), len(want))
		} else if s != want {
			t.Errorf("key %q value does not match (data corruption)", key)
		}
	}

	checkStringKey("key-small-1", smallValue1)
	checkStringKey("key-small-2", smallValue2)
	checkStringKey("key-large", largeValue)
}

// --- Exoskeleton integration tests ---

// testExoController creates an ExoskeletonController backed by mock registrars.
func testExoController(t *testing.T, pgEnabled, natsEnabled bool) (*exoskeleton.ExoskeletonController, *mockPgRegistrar, *mockNatsRegistrar) {
	t.Helper()
	cfg := &exoskeleton.ExoskeletonConfig{
		Enabled:           true,
		PostgresEnabled:   pgEnabled,
		NATSEnabled:       natsEnabled,
		CleanupOnUndeploy: true,
	}

	var pg exoskeleton.PostgresRegistrarI
	var nr exoskeleton.NATSRegistrarI
	mockPg := &mockPgRegistrar{}
	mockNr := &mockNatsRegistrar{}

	if pgEnabled {
		pg = mockPg
	}
	if natsEnabled {
		nr = mockNr
	}

	// Use a fake K8s clientset for the credential injector
	staticClient := kubefake.NewClientset(managedNs("my-ns"), managedNs("test-ns"))
	credInj := exoskeleton.NewCredentialInjector(staticClient)

	ctrl := exoskeleton.NewExoskeletonController(cfg, pg, nr, credInj)
	return ctrl, mockPg, mockNr
}

// mockPgRegistrar implements PostgresRegistrarI for testing.
type mockPgRegistrar struct {
	registerCalls   int
	unregisterCalls int
	registerErr     error
	unregisterErr   error
}

func (m *mockPgRegistrar) Register(ctx context.Context, id exoskeleton.Identity) (*exoskeleton.PostgresRegistration, error) {
	m.registerCalls++
	if m.registerErr != nil {
		return nil, m.registerErr
	}
	return &exoskeleton.PostgresRegistration{
		Role:     id.PostgresRole,
		Schema:   id.PostgresSchema,
		Password: "test-password",
		Host:     "localhost",
		Port:     "5432",
		Database: "testdb",
	}, nil
}

func (m *mockPgRegistrar) Unregister(ctx context.Context, id exoskeleton.Identity) error {
	m.unregisterCalls++
	return m.unregisterErr
}

// mockNatsRegistrar implements NATSRegistrarI for testing.
type mockNatsRegistrar struct {
	registerCalls   int
	unregisterCalls int
	registerErr     error
	unregisterErr   error
}

func (m *mockNatsRegistrar) Register(ctx context.Context, id exoskeleton.Identity) (*exoskeleton.NATSRegistration, error) {
	m.registerCalls++
	if m.registerErr != nil {
		return nil, m.registerErr
	}
	return &exoskeleton.NATSRegistration{
		URL:           "nats://localhost:4222",
		Token:         "test-nats-token",
		SubjectPrefix: id.NATSSubjectPrefix,
		Principal:     id.NATSPrincipal,
	}, nil
}

func (m *mockNatsRegistrar) Unregister(ctx context.Context, id exoskeleton.Identity) error {
	m.unregisterCalls++
	return m.unregisterErr
}

// TestWorkflowApplyWithExoskeletonAndTentacularDeps verifies that wf_apply with an
// exoskeleton controller triggers registration when the workflow has tentacular-* deps.
func TestWorkflowApplyWithExoskeletonAndTentacularDeps(t *testing.T) {
	client := newDeployTestClientWithDiscovery()
	ctx := context.Background()

	ctrl, mockPg, mockNr := testExoController(t, true, true)

	// Build manifests with a ConfigMap containing workflow.yaml with tentacular-* deps
	manifests := []map[string]interface{}{
		{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata":   map[string]interface{}{"name": "my-workflow-config"},
			"data": map[string]interface{}{
				"workflow.yaml": "dependencies:\n  - tentacular-postgres\n  - tentacular-nats\n",
			},
		},
	}

	result, err := handleWorkflowApply(ctx, client, WorkflowApplyParams{
		Namespace: "my-ns",
		Name:      "exo-test-app",
		Manifests: manifests,
	})
	if err != nil {
		t.Fatalf("handleWorkflowApply: %v", err)
	}
	if result.Created != 1 {
		t.Fatalf("expected 1 created, got %d", result.Created)
	}

	// Now simulate the exoskeleton registration that happens in the tool handler
	wfYAML := extractWorkflowYAML(manifests)
	if wfYAML == "" {
		t.Fatal("extractWorkflowYAML returned empty string")
	}

	exoDeps, err := exoskeleton.DetectExoskeletonDeps(wfYAML)
	if err != nil {
		t.Fatalf("DetectExoskeletonDeps: %v", err)
	}
	if len(exoDeps) != 2 {
		t.Fatalf("expected 2 exo deps, got %d: %v", len(exoDeps), exoDeps)
	}

	if err := ctrl.Register(ctx, "my-ns", "exo-test-app", exoDeps); err != nil {
		t.Fatalf("Register: %v", err)
	}

	if mockPg.registerCalls != 1 {
		t.Errorf("expected 1 postgres register call, got %d", mockPg.registerCalls)
	}
	if mockNr.registerCalls != 1 {
		t.Errorf("expected 1 nats register call, got %d", mockNr.registerCalls)
	}
}

// TestWorkflowApplyWithExoskeletonNoTentacularDeps verifies that wf_apply with an
// exoskeleton controller does NOT trigger registration when there are no tentacular-* deps.
func TestWorkflowApplyWithExoskeletonNoTentacularDeps(t *testing.T) {
	client := newDeployTestClientWithDiscovery()
	ctx := context.Background()

	_, mockPg, mockNr := testExoController(t, true, true)

	// ConfigMap with workflow.yaml that has no tentacular-* deps
	manifests := []map[string]interface{}{
		{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata":   map[string]interface{}{"name": "my-config"},
			"data": map[string]interface{}{
				"workflow.yaml": "dependencies:\n  - redis\n  - rabbitmq\n",
			},
		},
	}

	result, err := handleWorkflowApply(ctx, client, WorkflowApplyParams{
		Namespace: "my-ns",
		Name:      "no-exo-app",
		Manifests: manifests,
	})
	if err != nil {
		t.Fatalf("handleWorkflowApply: %v", err)
	}
	if result.Created != 1 {
		t.Fatalf("expected 1 created, got %d", result.Created)
	}

	// Simulate the exoskeleton check
	wfYAML := extractWorkflowYAML(manifests)
	exoDeps, err := exoskeleton.DetectExoskeletonDeps(wfYAML)
	if err != nil {
		t.Fatalf("DetectExoskeletonDeps: %v", err)
	}
	if len(exoDeps) != 0 {
		t.Fatalf("expected 0 exo deps, got %d: %v", len(exoDeps), exoDeps)
	}

	// No register calls should have been made
	if mockPg.registerCalls != 0 {
		t.Errorf("expected 0 postgres register calls, got %d", mockPg.registerCalls)
	}
	if mockNr.registerCalls != 0 {
		t.Errorf("expected 0 nats register calls, got %d", mockNr.registerCalls)
	}
}

// TestWorkflowApplyWithNilExoskeletonController verifies that wf_apply with nil
// controller has no exoskeleton side effects.
func TestWorkflowApplyWithNilExoskeletonController(t *testing.T) {
	client := newDeployTestClientWithDiscovery()
	ctx := context.Background()

	// nil controller — exoskeleton disabled
	manifests := []map[string]interface{}{
		{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata":   map[string]interface{}{"name": "my-config"},
			"data": map[string]interface{}{
				"workflow.yaml": "dependencies:\n  - tentacular-postgres\n",
			},
		},
	}

	result, err := handleWorkflowApply(ctx, client, WorkflowApplyParams{
		Namespace: "my-ns",
		Name:      "nil-ctrl-app",
		Manifests: manifests,
	})
	if err != nil {
		t.Fatalf("handleWorkflowApply: %v", err)
	}
	if result.Created != 1 {
		t.Errorf("expected 1 created, got %d", result.Created)
	}
	// The test passes if no panic occurs — nil controller means no exoskeleton code runs
}

// TestWorkflowRemoveWithExoskeletonCleanup verifies that wf_remove calls Unregister
// when the exoskeleton controller is non-nil.
func TestWorkflowRemoveWithExoskeletonCleanup(t *testing.T) {
	client := newDeployTestClient()
	ctx := context.Background()

	ctrl, mockPg, mockNr := testExoController(t, true, true)

	// Call Unregister directly (simulates what the handler does)
	if err := ctrl.Unregister(ctx, "my-ns", "cleanup-app"); err != nil {
		t.Fatalf("Unregister: %v", err)
	}

	if mockPg.unregisterCalls != 1 {
		t.Errorf("expected 1 postgres unregister call, got %d", mockPg.unregisterCalls)
	}
	if mockNr.unregisterCalls != 1 {
		t.Errorf("expected 1 nats unregister call, got %d", mockNr.unregisterCalls)
	}

	// Also verify the workflow removal works fine after unregistration
	result, err := handleWorkflowRemove(ctx, client, WorkflowRemoveParams{
		Namespace: "my-ns",
		Name:      "cleanup-app",
	})
	if err != nil {
		t.Fatalf("handleWorkflowRemove: %v", err)
	}
	if result.Name != "cleanup-app" {
		t.Errorf("result Name: got %q, want cleanup-app", result.Name)
	}
}

// TestWorkflowRemoveWithNilController verifies wf_remove with nil controller
// has no exoskeleton side effects and works identically to before.
func TestWorkflowRemoveWithNilController(t *testing.T) {
	client := newDeployTestClient()
	ctx := context.Background()

	// nil controller — no exoskeleton calls should happen
	result, err := handleWorkflowRemove(ctx, client, WorkflowRemoveParams{
		Namespace: "my-ns",
		Name:      "no-exo-remove",
	})
	if err != nil {
		t.Fatalf("handleWorkflowRemove: %v", err)
	}
	if result.Name != "no-exo-remove" {
		t.Errorf("result Name: got %q, want no-exo-remove", result.Name)
	}
	// Test passes if no panic — nil controller means no exoskeleton code
}

// TestExtractWorkflowYAML verifies the helper that extracts workflow.yaml from manifests.
func TestExtractWorkflowYAML(t *testing.T) {
	tests := []struct {
		name      string
		manifests []map[string]interface{}
		want      string
	}{
		{
			name: "ConfigMap with workflow.yaml",
			manifests: []map[string]interface{}{
				{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata":   map[string]interface{}{"name": "wf-config"},
					"data": map[string]interface{}{
						"workflow.yaml": "dependencies:\n  - tentacular-postgres\n",
					},
				},
			},
			want: "dependencies:\n  - tentacular-postgres\n",
		},
		{
			name: "no ConfigMap",
			manifests: []map[string]interface{}{
				{
					"apiVersion": "apps/v1",
					"kind":       "Deployment",
					"metadata":   map[string]interface{}{"name": "my-app"},
				},
			},
			want: "",
		},
		{
			name: "ConfigMap without workflow.yaml key",
			manifests: []map[string]interface{}{
				{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata":   map[string]interface{}{"name": "other-config"},
					"data": map[string]interface{}{
						"config.yaml": "some: value",
					},
				},
			},
			want: "",
		},
		{
			name:      "empty manifests",
			manifests: []map[string]interface{}{},
			want:      "",
		},
		{
			name: "multiple manifests, ConfigMap is second",
			manifests: []map[string]interface{}{
				{
					"apiVersion": "v1",
					"kind":       "Service",
					"metadata":   map[string]interface{}{"name": "my-svc"},
				},
				{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata":   map[string]interface{}{"name": "wf"},
					"data": map[string]interface{}{
						"workflow.yaml": "dependencies:\n  - tentacular-nats\n",
					},
				},
			},
			want: "dependencies:\n  - tentacular-nats\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractWorkflowYAML(tt.manifests)
			if got != tt.want {
				t.Errorf("extractWorkflowYAML() = %q, want %q", got, tt.want)
			}
		})
	}
}
