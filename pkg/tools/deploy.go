package tools

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/randybias/tentacular-mcp/pkg/auth"
	"github.com/randybias/tentacular-mcp/pkg/authz"
	"github.com/randybias/tentacular-mcp/pkg/exoskeleton"
	"github.com/randybias/tentacular-mcp/pkg/guard"
	"github.com/randybias/tentacular-mcp/pkg/k8s"
	"github.com/randybias/tentacular-mcp/pkg/proxy"
	"github.com/randybias/tentacular-mcp/pkg/scheduler"
)

// defaultProxyNamespace is the namespace where the esm.sh module proxy lives.
// Uses the canonical default from the proxy package.
var defaultProxyNamespace = proxy.DefaultNamespace

const releaseLabelKey = "tentacular.io/release"

// allowedKinds is the set of Kubernetes resource kinds wf_apply will
// accept. Cluster-scoped and sensitive kinds are not permitted.
var allowedKinds = map[string]bool{
	"Deployment":            true,
	"Service":               true,
	"PersistentVolumeClaim": true,
	"NetworkPolicy":         true,
	"ConfigMap":             true,
	"Secret":                true,
	"Job":                   true,
	"CronJob":               true,
	"Ingress":               true,
}

// knownGVRs is the set of GroupVersionResources used for garbage collection,
// removal, and status checks across workflow lifecycle operations.
var knownGVRs = []schema.GroupVersionResource{
	{Group: "apps", Version: "v1", Resource: "deployments"},
	{Group: "", Version: "v1", Resource: "services"},
	{Group: "", Version: "v1", Resource: "configmaps"},
	{Group: "", Version: "v1", Resource: "secrets"},
	{Group: "batch", Version: "v1", Resource: "jobs"},
	{Group: "batch", Version: "v1", Resource: "cronjobs"},
	{Group: "networking.k8s.io", Version: "v1", Resource: "networkpolicies"},
	{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"},
	{Group: "", Version: "v1", Resource: "persistentvolumeclaims"},
}

// WorkflowApplyParams are the parameters for wf_apply.
type WorkflowApplyParams struct {
	Namespace string           `json:"namespace" jsonschema:"Target namespace for the workflow"`
	Name      string           `json:"name" jsonschema:"Deployment name for tracking resources"`
	Group     string           `json:"group,omitempty" jsonschema:"IdP group assigned to this workflow (overrides server default)"`
	Share     string           `json:"share,omitempty" jsonschema:"Permission preset name: private, group-read, group-run, group-edit, public-read"`
	Manifests []map[string]any `json:"manifests" jsonschema:"List of Kubernetes manifest objects to apply"`
}

// WorkflowApplyResult is the result of wf_apply.
type WorkflowApplyResult struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Created   int    `json:"created"`
	Updated   int    `json:"updated"`
	Deleted   int    `json:"deleted"`
}

// WorkflowRemoveParams are the parameters for wf_remove.
type WorkflowRemoveParams struct {
	Namespace string `json:"namespace" jsonschema:"Namespace containing the workflow resources"`
	Name      string `json:"name" jsonschema:"Deployment name to remove"`
}

// WorkflowRemoveResult is the result of wf_remove.
type WorkflowRemoveResult struct {
	Name              string `json:"name"`
	Namespace         string `json:"namespace"`
	ExoCleanupDetails string `json:"exo_cleanup_details,omitempty"`
	Deleted           int    `json:"deleted"`
	ExoCleanedUp      bool   `json:"exo_cleaned_up,omitempty"`
}

// WorkflowStatusParams are the parameters for wf_status.
type WorkflowStatusParams struct {
	Namespace string `json:"namespace" jsonschema:"Namespace containing the workflow resources"`
	Name      string `json:"name" jsonschema:"Deployment name to check status for"`
	Detail    bool   `json:"detail,omitempty" jsonschema:"Include pods and events in the response"`
}

// WorkflowResourceStatus is the status of a single resource in a workflow deployment.
type WorkflowResourceStatus struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Reason string `json:"reason,omitempty"`
	Ready  bool   `json:"ready"`
}

// WorkflowPodInfo is a single pod in the status response.
type WorkflowPodInfo struct {
	Name     string `json:"name"`
	Phase    string `json:"phase"`
	NodeName string `json:"nodeName,omitempty"`
	Ready    bool   `json:"ready"`
}

// WorkflowEventInfo is a single event in the status response.
type WorkflowEventInfo struct {
	Type    string `json:"type"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
	Count   int32  `json:"count"`
}

// WorkflowStatusResult is the result of wf_status.
type WorkflowStatusResult struct {
	Name      string                   `json:"name"`
	Namespace string                   `json:"namespace"`
	Version   string                   `json:"version,omitempty"`
	Resources []WorkflowResourceStatus `json:"resources"`
	Pods      []WorkflowPodInfo        `json:"pods,omitempty"`
	Events    []WorkflowEventInfo      `json:"events,omitempty"`
	Replicas  int32                    `json:"replicas"`
	Available int32                    `json:"available"`
	Ready     bool                     `json:"ready"`
}

func registerDeployTools(srv *mcp.Server, client *k8s.Client, sched *scheduler.Scheduler, exoCtrl *exoskeleton.Controller, eval *authz.Evaluator) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "wf_apply",
		Description: "Apply a set of Kubernetes manifests as a named deployment in a namespace. Uses release labels for tracking and garbage collection. Includes garbage collection of stale resources.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Apply Workflow Manifests",
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, params WorkflowApplyParams) (*mcp.CallToolResult, WorkflowApplyResult, error) {
		if err := guard.CheckNamespace(params.Namespace); err != nil {
			return nil, WorkflowApplyResult{}, err
		}
		if err := guard.CheckName(params.Name); err != nil {
			return nil, WorkflowApplyResult{}, err
		}

		// Resolve Share preset to a mode string before touching manifests.
		var mode string
		if params.Share != "" {
			m, ok := authz.PresetFromName(params.Share)
			if !ok {
				return nil, WorkflowApplyResult{}, fmt.Errorf("unknown share preset %q; valid presets: %s", params.Share, authz.ValidPresetNames())
			}
			mode = m.String()
		} else if eval != nil {
			mode = eval.DefaultMode.String()
		} else {
			mode = authz.DefaultMode.String()
		}

		// Extract deployer identity from request context (set by auth middleware).
		deployer := auth.DeployerFromContext(ctx)

		// Authz check for UPDATE path: fetch existing Deployment annotations.
		// CREATE path checks namespace Write permission (creating a tentacle requires namespace Write).
		// isUpdate is used downstream to prevent ownership annotation overwrite.
		isUpdate := false
		var existingAnnotations map[string]string
		existing, getErr := client.Clientset.AppsV1().Deployments(params.Namespace).Get(ctx, params.Name, metav1.GetOptions{})
		if getErr == nil {
			// Deployment exists — this is an update.
			isUpdate = true
			existingAnnotations = existing.Annotations
			if d := eval.Check(deployer, existing.Annotations, authz.Write); !d.Allowed {
				return nil, WorkflowApplyResult{}, fmt.Errorf("permission denied: %s", d.Reason)
			}
		} else if apierrors.IsNotFound(getErr) {
			// CREATE path: check namespace Write permission.
			if err := checkNamespaceAuthz(ctx, client, params.Namespace, deployer, eval, authz.Write); err != nil {
				return nil, WorkflowApplyResult{}, err
			}
		} else {
			// Other error (e.g., permission denied on Deployment get) — fail fast.
			return nil, WorkflowApplyResult{}, fmt.Errorf("check deployment %q: %w", params.Name, getErr)
		}
		if exoCtrl != nil {
			processed, exoErr := exoCtrl.ProcessManifests(ctx, params.Namespace, params.Name, params.Manifests)
			if exoErr != nil {
				return nil, WorkflowApplyResult{}, fmt.Errorf("exoskeleton: %w", exoErr)
			}
			params.Manifests = processed

			// Annotate Deployment manifests with deployer provenance and ownership.
			// IsUpdate=true prevents overwriting ownership on existing deployments.
			// ExistingAnnotations carries forward ownership keys on update.
			annotateDeployer := exoskeleton.DeployerInfo{Provider: "bearer-token"}
			if deployer != nil {
				annotateDeployer = *deployer
			}
			params.Manifests = exoCtrl.AnnotateDeployer(params.Manifests, exoskeleton.AnnotateDeployerParams{
				Deployer:            annotateDeployer,
				Group:               params.Group,
				Mode:                mode,
				IsUpdate:            isUpdate,
				ExistingAnnotations: existingAnnotations,
			})
		}
		result, err := handleWorkflowApply(ctx, client, params)
		if err == nil {
			if sched != nil {
				syncCronSchedule(ctx, client, sched, params.Namespace, params.Name)
			}
			// Pre-warm module proxy in background (best-effort, non-blocking).
			// Parse jsr/npm dependencies from the workflow ConfigMap manifest and
			// trigger esm.sh to build and cache each module before pod startup.
			if deps := extractModuleDeps(params.Manifests); len(deps) > 0 {
				proxyNS := os.Getenv("TENTACULAR_PROXY_NAMESPACE")
				if proxyNS == "" {
					proxyNS = defaultProxyNamespace
				}
				go func() { //nolint:gosec,contextcheck // G118: goroutine intentionally outlives the request to pre-warm modules in the background
					bgCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
					defer cancel()
					if prewarmErr := k8s.PrewarmModules(bgCtx, client, proxyNS, deps); prewarmErr != nil {
						slog.Warn("module pre-warm completed with errors", "error", prewarmErr)
					}
				}()
			}
		}
		return nil, result, err
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "wf_remove",
		Description: "Remove all resources belonging to a named deployment in a namespace. When exoskeleton cleanup is enabled, also drops backing-service data.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Remove Workflow Deployment",
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(true),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, params WorkflowRemoveParams) (*mcp.CallToolResult, WorkflowRemoveResult, error) {
		if err := guard.CheckNamespace(params.Namespace); err != nil {
			return nil, WorkflowRemoveResult{}, err
		}
		if err := guard.CheckName(params.Name); err != nil {
			return nil, WorkflowRemoveResult{}, err
		}
		deployer := auth.DeployerFromContext(ctx)
		dep, getErr := client.Clientset.AppsV1().Deployments(params.Namespace).Get(ctx, params.Name, metav1.GetOptions{})
		if getErr == nil {
			if d := eval.Check(deployer, dep.Annotations, authz.Write); !d.Allowed {
				return nil, WorkflowRemoveResult{}, fmt.Errorf("permission denied: %s", d.Reason)
			}
		} else if !apierrors.IsNotFound(getErr) {
			return nil, WorkflowRemoveResult{}, fmt.Errorf("check deployment %q: %w", params.Name, getErr)
		}
		if sched != nil {
			sched.Deregister(params.Namespace, params.Name)
		}
		result, err := handleWorkflowRemove(ctx, client, params)
		// Exoskeleton: cleanup registrations after removing K8s resources.
		if err == nil && exoCtrl != nil {
			report, cleanupErr := exoCtrl.CleanupWithReport(ctx, params.Namespace, params.Name)
			if cleanupErr != nil {
				slog.Warn("exoskeleton cleanup failed", "namespace", params.Namespace, "name", params.Name, "error", cleanupErr)
			}
			if report != nil && report.Performed {
				result.ExoCleanedUp = true
				result.ExoCleanupDetails = report.Summary()
			}
		}
		return nil, result, err
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "wf_status",
		Description: "Get status of all resources belonging to a named deployment in a namespace.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Get Workflow Status",
			ReadOnlyHint:    true,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, params WorkflowStatusParams) (*mcp.CallToolResult, WorkflowStatusResult, error) {
		if err := guard.CheckNamespace(params.Namespace); err != nil {
			return nil, WorkflowStatusResult{}, err
		}
		if err := guard.CheckName(params.Name); err != nil {
			return nil, WorkflowStatusResult{}, err
		}
		deployer := auth.DeployerFromContext(ctx)
		dep, getErr := client.Clientset.AppsV1().Deployments(params.Namespace).Get(ctx, params.Name, metav1.GetOptions{})
		if getErr == nil {
			if d := eval.Check(deployer, dep.Annotations, authz.Read); !d.Allowed {
				return nil, WorkflowStatusResult{}, fmt.Errorf("permission denied: %s", d.Reason)
			}
		} else if !apierrors.IsNotFound(getErr) {
			return nil, WorkflowStatusResult{}, fmt.Errorf("check deployment %q: %w", params.Name, getErr)
		}
		result, err := handleWorkflowStatus(ctx, client, params)
		return nil, result, err
	})
}

// resolveGVR derives the GroupVersionResource from apiVersion and kind using the discovery client.
func resolveGVR(_ context.Context, client *k8s.Client, apiVersion, kind string) (schema.GroupVersionResource, error) {
	_, resourceLists, err := client.Clientset.Discovery().ServerGroupsAndResources()
	if err != nil && resourceLists == nil {
		return schema.GroupVersionResource{}, fmt.Errorf("discovery failed: %w", err)
	}

	for _, rl := range resourceLists {
		if rl.GroupVersion != apiVersion {
			continue
		}
		for _, r := range rl.APIResources {
			if r.Kind == kind {
				gv, err := schema.ParseGroupVersion(apiVersion)
				if err != nil {
					return schema.GroupVersionResource{}, fmt.Errorf("parse group version %q: %w", apiVersion, err)
				}
				return schema.GroupVersionResource{
					Group:    gv.Group,
					Version:  gv.Version,
					Resource: r.Name,
				}, nil
			}
		}
	}

	return schema.GroupVersionResource{}, fmt.Errorf("no resource found for apiVersion=%q kind=%q", apiVersion, kind)
}

// resourceKey returns a unique identifier for a resource.
func resourceKey(gvr schema.GroupVersionResource, name string) string {
	return fmt.Sprintf("%s/%s/%s", gvr.Group, gvr.Resource, name)
}

// ensurePSACompliance ensures Deployment, Job, and CronJob manifests have
// PSA-compliant security contexts. Required fields are added only when absent
// so user-specified values are preserved. This prevents rejection by PSA
// restricted enforcement on managed namespaces.
func ensurePSACompliance(manifests []map[string]any) {
	for _, m := range manifests {
		kind, _, _ := unstructured.NestedString(m, "kind")

		var podSpecPath []string
		switch kind {
		case "Deployment", "Job":
			podSpecPath = []string{"spec", "template", "spec"}
		case "CronJob":
			podSpecPath = []string{"spec", "jobTemplate", "spec", "template", "spec"}
		default:
			continue
		}

		ensurePodSpecPSA(m, podSpecPath)
	}
}

// ensurePodSpecPSA sets PSA-restricted security context defaults on a PodSpec
// at the given path. It only sets fields that are not already present.
func ensurePodSpecPSA(obj map[string]any, podSpecPath []string) {
	// Pod-level security context.
	scPath := append(append([]string{}, podSpecPath...), "securityContext")
	setIfAbsent(obj, true, append(append([]string{}, scPath...), "runAsNonRoot")...)
	setIfAbsent(obj, "RuntimeDefault", append(append([]string{}, scPath...), "seccompProfile", "type")...)

	// Container-level security contexts (containers and initContainers).
	needsTmpVolume := false
	for _, field := range []string{"containers", "initContainers"} {
		containersRaw, found, _ := unstructured.NestedSlice(obj, append(append([]string{}, podSpecPath...), field)...)
		if !found {
			continue
		}

		for i, cRaw := range containersRaw {
			c, ok := cRaw.(map[string]any)
			if !ok {
				continue
			}
			prefix := []string{"securityContext"}
			setIfAbsent(c, false, append(append([]string{}, prefix...), "allowPrivilegeEscalation")...)
			setIfAbsent(c, true, append(append([]string{}, prefix...), "readOnlyRootFilesystem")...)
			setIfAbsent(c, true, append(append([]string{}, prefix...), "runAsNonRoot")...)
			setIfAbsent(c, []any{"ALL"}, append(append([]string{}, prefix...), "capabilities", "drop")...)
			setIfAbsent(c, "RuntimeDefault", append(append([]string{}, prefix...), "seccompProfile", "type")...)

			// Check if this container already has a /tmp volumeMount.
			vms, _, _ := unstructured.NestedSlice(c, "volumeMounts")
			containerHasTmp := false
			for _, vm := range vms {
				vmMap, ok := vm.(map[string]any)
				if ok && vmMap["mountPath"] == "/tmp" {
					containerHasTmp = true
					needsTmpVolume = true
				}
			}

			// Add /tmp volumeMount if readOnlyRootFilesystem is set and no /tmp mount exists.
			roFS, _, _ := unstructured.NestedBool(c, "securityContext", "readOnlyRootFilesystem")
			if roFS && !containerHasTmp {
				vms = append(vms, map[string]any{
					"name":      "tmp",
					"mountPath": "/tmp",
				})
				_ = unstructured.SetNestedSlice(c, vms, "volumeMounts")
				needsTmpVolume = true
			}

			containersRaw[i] = c
		}
		_ = unstructured.SetNestedSlice(obj, containersRaw, append(append([]string{}, podSpecPath...), field)...)
	}

	// Add tmp emptyDir volume if any container got a /tmp mount.
	if needsTmpVolume {
		volumes, _, _ := unstructured.NestedSlice(obj, append(append([]string{}, podSpecPath...), "volumes")...)
		hasTmpVol := false
		for _, v := range volumes {
			vMap, ok := v.(map[string]any)
			if ok && vMap["name"] == "tmp" {
				hasTmpVol = true
				break
			}
		}
		if !hasTmpVol {
			volumes = append(volumes, map[string]any{
				"name":     "tmp",
				"emptyDir": map[string]any{},
			})
			_ = unstructured.SetNestedSlice(obj, volumes, append(append([]string{}, podSpecPath...), "volumes")...)
		}
	}
}

// setIfAbsent sets a nested field only when it does not already exist.
func setIfAbsent(obj map[string]any, value any, fields ...string) {
	_, found, _ := unstructured.NestedFieldNoCopy(obj, fields...)
	if !found {
		_ = unstructured.SetNestedField(obj, value, fields...)
	}
}

// handleWorkflowApply applies a set of Kubernetes manifests as a named deployment.
//
// ConfigMap data integrity note: large string values in ConfigMap data are NOT
// truncated server-side. The manifest map[string]any is wrapped directly
// in unstructured.Unstructured and passed to the dynamic client without any JSON
// round-trip or size limit in this function. If ConfigMap data appears truncated,
// the cause is client-side (e.g. the LLM generating incomplete manifests), not
// the MCP server. See TestWorkflowApplyConfigMapLargeDataIntegrity for verification.
func handleWorkflowApply(ctx context.Context, client *k8s.Client, params WorkflowApplyParams) (WorkflowApplyResult, error) {
	if err := k8s.CheckManagedNamespace(ctx, client, params.Namespace); err != nil {
		return WorkflowApplyResult{}, err
	}

	// Ensure all workload manifests have PSA-compliant security contexts.
	ensurePSACompliance(params.Manifests)

	created, updated, deleted := 0, 0, 0
	appliedKeys := make(map[string]bool)

	for _, manifest := range params.Manifests {
		obj := &unstructured.Unstructured{Object: manifest}

		apiVersion := obj.GetAPIVersion()
		kind := obj.GetKind()
		if apiVersion == "" || kind == "" {
			return WorkflowApplyResult{}, errors.New("manifest missing apiVersion or kind")
		}

		if !allowedKinds[kind] {
			return WorkflowApplyResult{}, fmt.Errorf("kind %q is not permitted in workflow manifests; allowed kinds: Deployment, Service, PersistentVolumeClaim, NetworkPolicy, ConfigMap, Secret, Job, CronJob, Ingress", kind)
		}

		gvr, err := resolveGVR(ctx, client, apiVersion, kind)
		if err != nil {
			return WorkflowApplyResult{}, fmt.Errorf("resolve GVR for %s/%s: %w", apiVersion, kind, err)
		}

		// Set namespace and release label
		obj.SetNamespace(params.Namespace)
		labels := obj.GetLabels()
		if labels == nil {
			labels = map[string]string{}
		}
		labels[releaseLabelKey] = params.Name
		obj.SetLabels(labels)

		name := obj.GetName()
		if name == "" {
			return WorkflowApplyResult{}, fmt.Errorf("manifest of kind %s is missing a name", kind)
		}

		key := resourceKey(gvr, name)
		appliedKeys[key] = true

		// Try to get existing resource
		existing, err := client.Dynamic.Resource(gvr).Namespace(params.Namespace).Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			// Create
			_, createErr := client.Dynamic.Resource(gvr).Namespace(params.Namespace).Create(ctx, obj, metav1.CreateOptions{})
			if createErr != nil {
				return WorkflowApplyResult{}, fmt.Errorf("create %s/%s: %w", kind, name, createErr)
			}
			created++
		} else if err != nil {
			return WorkflowApplyResult{}, fmt.Errorf("get %s/%s: %w", kind, name, err)
		} else {
			// Update: preserve resource version
			obj.SetResourceVersion(existing.GetResourceVersion())
			_, updateErr := client.Dynamic.Resource(gvr).Namespace(params.Namespace).Update(ctx, obj, metav1.UpdateOptions{})
			if updateErr != nil {
				return WorkflowApplyResult{}, fmt.Errorf("update %s/%s: %w", kind, name, updateErr)
			}
			updated++
		}
	}

	// Garbage collect: delete previously-labeled resources not in new manifest set
	labelSelector := fmt.Sprintf("%s=%s", releaseLabelKey, params.Name)
	for _, gvr := range knownGVRs {
		list, err := client.Dynamic.Resource(gvr).Namespace(params.Namespace).List(ctx, metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		if err != nil {
			continue // skip GVRs that don't exist or are not accessible
		}
		for _, item := range list.Items {
			key := resourceKey(gvr, item.GetName())
			if !appliedKeys[key] {
				err := client.Dynamic.Resource(gvr).Namespace(params.Namespace).Delete(ctx, item.GetName(), metav1.DeleteOptions{})
				if err != nil {
					continue // best-effort GC
				}
				deleted++
			}
		}
	}

	return WorkflowApplyResult{
		Name:      params.Name,
		Namespace: params.Namespace,
		Created:   created,
		Updated:   updated,
		Deleted:   deleted,
	}, nil
}

func handleWorkflowRemove(ctx context.Context, client *k8s.Client, params WorkflowRemoveParams) (WorkflowRemoveResult, error) {
	if err := k8s.CheckManagedNamespace(ctx, client, params.Namespace); err != nil {
		return WorkflowRemoveResult{}, err
	}

	labelSelector := fmt.Sprintf("%s=%s", releaseLabelKey, params.Name)
	deleted := 0

	for _, gvr := range knownGVRs {
		list, err := client.Dynamic.Resource(gvr).Namespace(params.Namespace).List(ctx, metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		if err != nil {
			continue
		}
		for _, item := range list.Items {
			err := client.Dynamic.Resource(gvr).Namespace(params.Namespace).Delete(ctx, item.GetName(), metav1.DeleteOptions{})
			if err != nil {
				continue
			}
			deleted++
		}
	}

	return WorkflowRemoveResult{
		Name:      params.Name,
		Namespace: params.Namespace,
		Deleted:   deleted,
	}, nil
}

func handleWorkflowStatus(ctx context.Context, client *k8s.Client, params WorkflowStatusParams) (WorkflowStatusResult, error) {
	if err := k8s.CheckManagedNamespace(ctx, client, params.Namespace); err != nil {
		return WorkflowStatusResult{}, err
	}

	labelSelector := fmt.Sprintf("%s=%s", releaseLabelKey, params.Name)
	resources := []WorkflowResourceStatus{}

	for _, gvr := range knownGVRs {
		list, err := client.Dynamic.Resource(gvr).Namespace(params.Namespace).List(ctx, metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		if err != nil {
			continue
		}
		for _, item := range list.Items {
			ready, reason := resourceReadiness(item, gvr.Resource)
			resources = append(resources, WorkflowResourceStatus{
				Kind:   item.GetKind(),
				Name:   item.GetName(),
				Ready:  ready,
				Reason: reason,
			})
		}
	}

	result := WorkflowStatusResult{
		Name:      params.Name,
		Namespace: params.Namespace,
		Resources: resources,
	}

	// Get deployment replicas and version from the typed API for accurate status
	deploy, err := client.Clientset.AppsV1().Deployments(params.Namespace).Get(ctx, params.Name, metav1.GetOptions{})
	if err == nil {
		replicas := replicaCount(deploy.Spec.Replicas)
		result.Replicas = replicas
		result.Available = deploy.Status.AvailableReplicas
		if deploy.Spec.Replicas != nil && replicas == 0 {
			// Deliberately scaled to zero — the desired state is met.
			result.Ready = true
		} else {
			result.Ready = deploy.Status.AvailableReplicas >= replicas
		}
		if v, ok := deploy.Labels["app.kubernetes.io/version"]; ok {
			result.Version = v
		}
	}

	// Optionally include pods and events
	if params.Detail {
		podList, err := client.Clientset.CoreV1().Pods(params.Namespace).List(ctx, metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		if err == nil {
			for _, pod := range podList.Items {
				ready := false
				for _, cond := range pod.Status.Conditions {
					if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
						ready = true
						break
					}
				}
				result.Pods = append(result.Pods, WorkflowPodInfo{
					Name:     pod.Name,
					Phase:    string(pod.Status.Phase),
					Ready:    ready,
					NodeName: pod.Spec.NodeName,
				})
			}
		}

		eventList, err := client.Clientset.CoreV1().Events(params.Namespace).List(ctx, metav1.ListOptions{
			FieldSelector: "involvedObject.name=" + params.Name,
		})
		if err == nil {
			for _, e := range eventList.Items {
				result.Events = append(result.Events, WorkflowEventInfo{
					Type:    e.Type,
					Reason:  e.Reason,
					Message: e.Message,
					Count:   e.Count,
				})
			}
		}
	}

	return result, nil
}

// resourceReadiness determines readiness from an unstructured resource.
func resourceReadiness(obj unstructured.Unstructured, resource string) (bool, string) {
	switch resource {
	case "deployments":
		readyReplicas, _, _ := unstructured.NestedInt64(obj.Object, "status", "readyReplicas")
		replicas, found, _ := unstructured.NestedInt64(obj.Object, "spec", "replicas")
		if !found {
			replicas = 1 // Kubernetes defaults omitted replicas to 1
		}
		if found && replicas == 0 {
			// Deliberately scaled to zero — the desired state is met.
			return true, ""
		}
		if readyReplicas >= replicas {
			return true, ""
		}
		return false, fmt.Sprintf("%d/%d replicas ready", readyReplicas, replicas)
	case "jobs":
		conditions, _, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
		for _, c := range conditions {
			cond, ok := c.(map[string]any)
			if !ok {
				continue
			}
			if cond["type"] == "Complete" && cond["status"] == "True" {
				return true, ""
			}
			if cond["type"] == "Failed" && cond["status"] == "True" {
				return false, "job failed"
			}
		}
		return false, "job in progress"
	default:
		// Services, ConfigMaps, Secrets, NetworkPolicies, CronJobs: presence = ready
		return true, ""
	}
}

// workflowYAML is the minimal structure used to parse contract.dependencies from
// the workflow.yaml stored in a ConfigMap.
type workflowYAML struct {
	Contract *contractYAML `yaml:"contract"`
}

type contractYAML struct {
	Dependencies map[string]dependencyYAML `yaml:"dependencies"`
}

type dependencyYAML struct {
	Protocol string `yaml:"protocol"`
	Host     string `yaml:"host"`
	Version  string `yaml:"version"`
}

// extractModuleDeps inspects the manifests for a ConfigMap containing
// workflow.yaml and parses jsr/npm entries from contract.dependencies.
// Returns the unique set of module dependencies to pre-warm.
func extractModuleDeps(manifests []map[string]any) []k8s.ModuleDep {
	for _, m := range manifests {
		obj := &unstructured.Unstructured{Object: m}
		if obj.GetKind() != "ConfigMap" {
			continue
		}
		data, ok, _ := unstructured.NestedStringMap(obj.Object, "data")
		if !ok {
			continue
		}
		wfYAML, ok := data["workflow.yaml"]
		if !ok {
			continue
		}
		var wf workflowYAML
		if err := yaml.Unmarshal([]byte(wfYAML), &wf); err != nil || wf.Contract == nil {
			continue
		}
		seen := make(map[string]bool)
		var deps []k8s.ModuleDep
		for _, dep := range wf.Contract.Dependencies {
			if dep.Protocol != "jsr" && dep.Protocol != "npm" {
				continue
			}
			key := dep.Protocol + ":" + dep.Host + "@" + dep.Version
			if seen[key] {
				continue
			}
			seen[key] = true
			deps = append(deps, k8s.ModuleDep{
				Protocol: dep.Protocol,
				Host:     dep.Host,
				Version:  dep.Version,
			})
		}
		return deps
	}
	return nil
}

// syncCronSchedule checks the deployed Deployment for a cron schedule annotation
// and registers or deregisters it with the scheduler.
func syncCronSchedule(ctx context.Context, client *k8s.Client, sched *scheduler.Scheduler, namespace, name string) {
	deploy, err := client.Clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return // deployment may not exist yet (e.g., only ConfigMap applied so far)
	}

	schedule := authz.GetAnnotation(deploy.Annotations, scheduler.CronAnnotation)
	if schedule == "" {
		sched.Deregister(namespace, name)
		return
	}

	if err := sched.Register(namespace, name, schedule); err != nil { //nolint:contextcheck // Register is a cron-scheduler method that does not accept context
		slog.Warn("failed to register cron schedule", "workflow", namespace+"/"+name, "error", err)
	}
}
