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

	"github.com/modelcontextprotocol/go-sdk/mcp"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"github.com/randybias/tentacular-mcp/pkg/authz"
	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

// deployGVRs maps resource name to list kind for the fake dynamic client.
var deployGVRs = map[schema.GroupVersionResource]string{
	{Group: "apps", Version: "v1", Resource: "deployments"}:                  "DeploymentList",
	{Group: "", Version: "v1", Resource: "services"}:                         "ServiceList",
	{Group: "", Version: "v1", Resource: "configmaps"}:                       "ConfigMapList",
	{Group: "", Version: "v1", Resource: "secrets"}:                          "SecretList",
	{Group: "batch", Version: "v1", Resource: "jobs"}:                        "JobList",
	{Group: "batch", Version: "v1", Resource: "cronjobs"}:                    "CronJobList",
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
			Manifests: []map[string]any{
				{
					"apiVersion": "v1",
					"kind":       kind,
					"metadata":   map[string]any{"name": "test-resource"},
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
		Manifests: []map[string]any{},
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
	registerDeployTools(mcpSrv, client, nil, nil, authz.NewEvaluator(authz.DefaultMode))

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
	var m map[string]any
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
	var m map[string]any
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

// TestWorkflowRemoveResultIncludesCleanupFields verifies that exo cleanup fields
// are serialized in the wf_remove result when populated.
func TestWorkflowRemoveResultIncludesCleanupFields(t *testing.T) {
	result := WorkflowRemoveResult{
		Name:              "my-wf",
		Namespace:         "my-ns",
		Deleted:           2,
		ExoCleanedUp:      true,
		ExoCleanupDetails: "postgres schema dropped, rustfs user removed",
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if v, ok := m["exo_cleaned_up"].(bool); !ok || !v {
		t.Errorf("expected exo_cleaned_up=true, got %v", m["exo_cleaned_up"])
	}
	if v, ok := m["exo_cleanup_details"]; !ok || v != "postgres schema dropped, rustfs user removed" {
		t.Errorf("expected exo_cleanup_details to match, got %v", v)
	}
}

// TestWorkflowRemoveResultOmitsCleanupFieldsWhenEmpty verifies that exo cleanup fields
// are omitted from JSON when not set (omitempty behavior).
func TestWorkflowRemoveResultOmitsCleanupFieldsWhenEmpty(t *testing.T) {
	result := WorkflowRemoveResult{
		Name:      "my-wf",
		Namespace: "my-ns",
		Deleted:   1,
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if _, ok := m["exo_cleaned_up"]; ok {
		t.Error("expected exo_cleaned_up to be omitted when false")
	}
	if _, ok := m["exo_cleanup_details"]; ok {
		t.Error("expected exo_cleanup_details to be omitted when empty")
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
	var m map[string]any
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
		Manifests: []map[string]any{},
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
		Manifests: []map[string]any{},
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
		Manifests: []map[string]any{
			{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata":   map[string]any{},
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
		Manifests: []map[string]any{
			{
				"kind":     "ConfigMap",
				"metadata": map[string]any{"name": "test"},
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

// TestEnsurePSACompliance_DeploymentDefaults verifies that a Deployment without
// security context gets PSA-restricted defaults injected.
func TestEnsurePSACompliance_DeploymentDefaults(t *testing.T) {
	dep := map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata":   map[string]any{"name": "my-app"},
		"spec": map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"containers": []any{
						map[string]any{
							"name":  "app",
							"image": "myimg:v1",
						},
					},
				},
			},
		},
	}

	manifests := []map[string]any{dep}
	ensurePSACompliance(manifests)

	// Pod-level security context.
	runAsNonRoot, found, _ := unstructured.NestedBool(dep, "spec", "template", "spec", "securityContext", "runAsNonRoot")
	if !found || !runAsNonRoot {
		t.Error("expected pod-level runAsNonRoot=true")
	}
	seccompType, found, _ := unstructured.NestedString(dep, "spec", "template", "spec", "securityContext", "seccompProfile", "type")
	if !found || seccompType != "RuntimeDefault" {
		t.Errorf("expected pod-level seccompProfile.type=RuntimeDefault, got %q", seccompType)
	}

	// Container-level security context.
	containers, _, _ := unstructured.NestedSlice(dep, "spec", "template", "spec", "containers")
	if len(containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(containers))
	}
	c := containers[0].(map[string]any)

	ape, _, _ := unstructured.NestedBool(c, "securityContext", "allowPrivilegeEscalation")
	if ape {
		t.Error("expected allowPrivilegeEscalation=false")
	}
	roFS, _, _ := unstructured.NestedBool(c, "securityContext", "readOnlyRootFilesystem")
	if !roFS {
		t.Error("expected readOnlyRootFilesystem=true")
	}
	cRunAsNonRoot, _, _ := unstructured.NestedBool(c, "securityContext", "runAsNonRoot")
	if !cRunAsNonRoot {
		t.Error("expected container runAsNonRoot=true")
	}
	drop, _, _ := unstructured.NestedSlice(c, "securityContext", "capabilities", "drop")
	if len(drop) != 1 || drop[0] != "ALL" {
		t.Errorf("expected capabilities.drop=[ALL], got %v", drop)
	}

	// Verify /tmp emptyDir volume and volumeMount were added.
	vms, _, _ := unstructured.NestedSlice(c, "volumeMounts")
	foundTmpMount := false
	for _, vm := range vms {
		vmMap := vm.(map[string]any)
		if vmMap["mountPath"] == "/tmp" {
			foundTmpMount = true
		}
	}
	if !foundTmpMount {
		t.Error("expected /tmp volumeMount on container")
	}

	volumes, _, _ := unstructured.NestedSlice(dep, "spec", "template", "spec", "volumes")
	foundTmpVol := false
	for _, v := range volumes {
		vMap := v.(map[string]any)
		if vMap["name"] == "tmp" {
			foundTmpVol = true
		}
	}
	if !foundTmpVol {
		t.Error("expected tmp emptyDir volume")
	}
}

// TestEnsurePSACompliance_PreservesExisting verifies that user-specified security
// context values are not overwritten.
func TestEnsurePSACompliance_PreservesExisting(t *testing.T) {
	dep := map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata":   map[string]any{"name": "secure-app"},
		"spec": map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"securityContext": map[string]any{
						"runAsUser":    int64(1000),
						"runAsNonRoot": true,
					},
					"containers": []any{
						map[string]any{
							"name":  "app",
							"image": "myimg:v1",
							"securityContext": map[string]any{
								"allowPrivilegeEscalation": false,
								"readOnlyRootFilesystem":   false, // User explicitly wants writable.
								"runAsNonRoot":             true,
							},
						},
					},
				},
			},
		},
	}

	ensurePSACompliance([]map[string]any{dep})

	// Pod-level: user set runAsUser, it should be preserved.
	runAsUser, found, _ := unstructured.NestedInt64(dep, "spec", "template", "spec", "securityContext", "runAsUser")
	if !found || runAsUser != 1000 {
		t.Errorf("expected runAsUser=1000 preserved, got %d", runAsUser)
	}

	// Container-level: user explicitly set readOnlyRootFilesystem=false, it should be preserved.
	containers, _, _ := unstructured.NestedSlice(dep, "spec", "template", "spec", "containers")
	c := containers[0].(map[string]any)
	roFS, _, _ := unstructured.NestedBool(c, "securityContext", "readOnlyRootFilesystem")
	if roFS {
		t.Error("expected readOnlyRootFilesystem=false to be preserved (user-specified)")
	}
}

// TestEnsurePSACompliance_SkipsNonWorkloads verifies ConfigMaps and Services are not modified.
func TestEnsurePSACompliance_SkipsNonWorkloads(t *testing.T) {
	cm := map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata":   map[string]any{"name": "my-config"},
		"data":       map[string]any{"key": "val"},
	}
	svc := map[string]any{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata":   map[string]any{"name": "my-svc"},
	}

	manifests := []map[string]any{cm, svc}
	ensurePSACompliance(manifests)

	// ConfigMap should not have spec.template.spec added.
	_, found, _ := unstructured.NestedMap(cm, "spec", "template", "spec")
	if found {
		t.Error("ConfigMap should not have spec.template.spec after PSA compliance")
	}
}

// TestEnsurePSACompliance_Job verifies that Job manifests also get PSA defaults.
func TestEnsurePSACompliance_Job(t *testing.T) {
	job := map[string]any{
		"apiVersion": "batch/v1",
		"kind":       "Job",
		"metadata":   map[string]any{"name": "my-job"},
		"spec": map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"containers": []any{
						map[string]any{
							"name":  "worker",
							"image": "busybox:latest",
						},
					},
				},
			},
		},
	}

	ensurePSACompliance([]map[string]any{job})

	runAsNonRoot, found, _ := unstructured.NestedBool(job, "spec", "template", "spec", "securityContext", "runAsNonRoot")
	if !found || !runAsNonRoot {
		t.Error("expected Job pod-level runAsNonRoot=true")
	}

	containers, _, _ := unstructured.NestedSlice(job, "spec", "template", "spec", "containers")
	c := containers[0].(map[string]any)
	ape, found, _ := unstructured.NestedBool(c, "securityContext", "allowPrivilegeEscalation")
	if !found || ape {
		t.Error("expected Job container allowPrivilegeEscalation=false")
	}
}

// TestWorkflowApplyConfigMapLargeDataIntegrity verifies that ConfigMap data keys survive
// the manifest → unstructured.Unstructured → dynamic client apply path without truncation.
// This is a regression guard: large string values (~7KB) must not be silently truncated
// during JSON marshaling or map[string]any conversion.
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
		Manifests: []map[string]any{
			{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata":   map[string]any{"name": "big-config"},
				"data": map[string]any{
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
	data, ok := rawData.(map[string]any)
	if !ok {
		t.Fatalf("ConfigMap data is not map[string]any: %T", rawData)
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
