package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gopkg.in/yaml.v3"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/randybias/tentacular-mcp/pkg/auth"
	"github.com/randybias/tentacular-mcp/pkg/authz"
	"github.com/randybias/tentacular-mcp/pkg/exoskeleton"
	"github.com/randybias/tentacular-mcp/pkg/guard"
	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

// minimalWorkflow holds only the fields needed for MCP describe reporting.
// It is separate from the main spec package to avoid a cross-repo dependency.
type minimalWorkflow struct {
	Nodes    map[string]minimalNode `yaml:"nodes"`
	Name     string                 `yaml:"name"`
	Version  string                 `yaml:"version"`
	Triggers []minimalTrigger       `yaml:"triggers"`
}

type minimalTrigger struct {
	Type     string `yaml:"type"`
	Schedule string `yaml:"schedule,omitempty"`
}

type minimalNode struct {
	Path string `yaml:"path"`
}

// WfListParams are the parameters for wf_list.
type WfListParams struct {
	Namespace string `json:"namespace,omitempty" jsonschema:"Namespace to filter (optional, empty=all tentacular namespaces)"`
	Owner     string `json:"owner,omitempty" jsonschema:"Filter by owner annotation (optional)"`
	Tag       string `json:"tag,omitempty" jsonschema:"Filter by tag (optional)"`
}

// WfListEntry is a single workflow in the list result.
type WfListEntry struct {
	Name        string `json:"name"`
	Namespace   string `json:"namespace"`
	Version     string `json:"version"`
	Owner       string `json:"owner,omitempty"`
	Group       string `json:"group,omitempty"`
	Mode        string `json:"mode,omitempty"`
	Environment string `json:"environment,omitempty"`
	DeployedBy  string `json:"deployed_by,omitempty"`
	DeployedVia string `json:"deployed_via,omitempty"`
	Age         string `json:"age"`
	Ready       bool   `json:"ready"`
}

// WfListResult is the result of wf_list.
type WfListResult struct {
	Workflows []WfListEntry `json:"workflows"`
}

// WfDescribeParams are the parameters for wf_describe.
type WfDescribeParams struct {
	Namespace string `json:"namespace" jsonschema:"Namespace of the workflow"`
	Name      string `json:"name" jsonschema:"Workflow name"`
}

// WfDescribeResult is the result of wf_describe.
type WfDescribeResult struct {
	Annotations   map[string]string `json:"annotations,omitempty"`
	DeployedVia   string            `json:"deployed_via,omitempty"`
	Age           string            `json:"age"`
	Owner         string            `json:"owner,omitempty"`
	OwnerName     string            `json:"owner_name,omitempty"`
	Group         string            `json:"group,omitempty"`
	Mode          string            `json:"mode,omitempty"`
	Preset        string            `json:"preset,omitempty"`
	Namespace     string            `json:"namespace"`
	Environment   string            `json:"environment,omitempty"`
	DeployedBy    string            `json:"deployed_by,omitempty"`
	Name          string            `json:"name"`
	Version       string            `json:"version"`
	Image         string            `json:"image"`
	DeployedAt    string            `json:"deployed_at,omitempty"`
	Nodes         []string          `json:"nodes,omitempty"`
	Triggers      []string          `json:"triggers,omitempty"`
	Tags          []string          `json:"tags,omitempty"`
	ReadyReplicas int32             `json:"ready_replicas"`
	Replicas      int32             `json:"replicas"`
	Ready         bool              `json:"ready"`
}

func registerDiscoverTools(srv *mcp.Server, client *k8s.Client, eval *authz.Evaluator) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "wf_list",
		Description: "List all tentacular-managed workflow deployments across namespaces, with ownership and status.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "List Deployed Workflows",
			ReadOnlyHint:    true,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, params WfListParams) (*mcp.CallToolResult, WfListResult, error) {
		if params.Namespace != "" {
			if err := guard.CheckNamespace(params.Namespace); err != nil {
				return nil, WfListResult{}, err
			}
		}
		deployer := auth.DeployerFromContext(ctx)
		result, err := handleWfList(ctx, client, params, deployer, eval)
		return nil, result, err
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "wf_describe",
		Description: "Get detailed information about a single tentacular workflow deployment, including metadata annotations, replica status, nodes, and triggers.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Describe Workflow Deployment",
			ReadOnlyHint:    true,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, params WfDescribeParams) (*mcp.CallToolResult, WfDescribeResult, error) {
		if err := guard.CheckNamespace(params.Namespace); err != nil {
			return nil, WfDescribeResult{}, err
		}
		if err := guard.CheckName(params.Name); err != nil {
			return nil, WfDescribeResult{}, err
		}
		deployer := auth.DeployerFromContext(ctx)
		result, err := handleWfDescribe(ctx, client, params, deployer, eval)
		return nil, result, err
	})
}

// isSystemNamespace returns true if the namespace should be filtered from wf_list results.
// A namespace is considered system if it matches the guard's canonical list or has the
// tentacular.io/system annotation set to "true".
func isSystemNamespace(ns string, annotations map[string]string) bool {
	if guard.IsSystemNamespace(ns) {
		return true
	}
	if annotations != nil && annotations["tentacular.io/system"] == "true" {
		return true
	}
	return false
}

func handleWfList(ctx context.Context, client *k8s.Client, params WfListParams, deployer *exoskeleton.DeployerInfo, eval *authz.Evaluator) (WfListResult, error) {
	ns := params.Namespace

	// If listing in a specific namespace, check namespace Read permission.
	if ns != "" {
		if err := checkNamespaceAuthz(ctx, client, ns, deployer, eval, authz.Read); err != nil {
			return WfListResult{}, err
		}
	}

	depList, err := client.Clientset.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/managed-by=tentacular",
	})
	if err != nil {
		return WfListResult{}, wrapListError(ns, err)
	}

	// When listing across all namespaces, build a cache of namespace annotations
	// so we can filter out system namespaces efficiently.
	nsAnnotations := map[string]map[string]string{}
	if ns == "" {
		nsList, nsErr := client.Clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
		if nsErr == nil {
			for _, n := range nsList.Items {
				nsAnnotations[n.Name] = n.Annotations
			}
		}
	}

	entries := make([]WfListEntry, 0, len(depList.Items))
	for _, dep := range depList.Items {
		// Filter out system namespaces when listing across all namespaces.
		if ns == "" && isSystemNamespace(dep.Namespace, nsAnnotations[dep.Namespace]) {
			continue
		}

		// Namespace-level authz filter: when listing across all namespaces, skip
		// namespaces where the caller lacks Read permission.
		if ns == "" {
			nsAnn := nsAnnotations[dep.Namespace]
			if nsAnn == nil {
				nsAnn = map[string]string{}
			}
			if d := eval.Check(deployer, nsAnn, authz.Read); !d.Allowed {
				continue
			}
		}

		// Authz filter: skip deployments the caller cannot read.
		if d := eval.Check(deployer, dep.Annotations, authz.Read); !d.Allowed {
			continue
		}

		entry := deploymentToListEntry(dep)

		// Apply optional client-side filters.
		if params.Owner != "" && entry.Owner != params.Owner {
			continue
		}
		if params.Tag != "" {
			ann := dep.Annotations
			if ann == nil {
				continue
			}
			if !containsTag(authz.GetAnnotation(ann, "tentacular.io/tags"), params.Tag) {
				continue
			}
		}

		entries = append(entries, entry)
	}

	return WfListResult{Workflows: entries}, nil
}

func handleWfDescribe(ctx context.Context, client *k8s.Client, params WfDescribeParams, deployer *exoskeleton.DeployerInfo, eval *authz.Evaluator) (WfDescribeResult, error) {
	dep, err := client.Clientset.AppsV1().Deployments(params.Namespace).Get(ctx, params.Name, metav1.GetOptions{})
	if err != nil {
		return WfDescribeResult{}, wrapGetError(params.Name, params.Namespace, err)
	}

	// Authz check: caller must have Read permission.
	if d := eval.Check(deployer, dep.Annotations, authz.Read); !d.Allowed {
		return WfDescribeResult{}, fmt.Errorf("permission denied: %s", d.Reason)
	}

	ann := dep.Annotations
	if ann == nil {
		ann = map[string]string{}
	}

	var tags []string
	if raw := authz.GetAnnotation(ann, "tentacular.io/tags"); raw != "" {
		tags = strings.Split(raw, ",")
	}

	image := ""
	if len(dep.Spec.Template.Spec.Containers) > 0 {
		image = dep.Spec.Template.Spec.Containers[0].Image
	}

	// Collect tentacular.io/* annotations for the result.
	tentacularAnn := make(map[string]string)
	for k, v := range ann {
		if strings.HasPrefix(k, "tentacular.io/") {
			tentacularAnn[k] = v
		}
	}
	if len(tentacularAnn) == 0 {
		tentacularAnn = nil
	}

	ownerInfo := authz.ReadOwnerInfo(ann)
	age := time.Since(dep.CreationTimestamp.Time).Round(time.Second).String()

	result := WfDescribeResult{
		Name:          dep.Name,
		Namespace:     dep.Namespace,
		Version:       dep.Labels[k8s.VersionLabel],
		Owner:         ownerInfo.OwnerEmail,
		OwnerName:     ownerInfo.OwnerName,
		Group:         ownerInfo.Group,
		Mode:          ownerInfo.Mode.String(),
		Preset:        ownerInfo.PresetName,
		Tags:          tags,
		Environment:   authz.GetAnnotation(ann, "tentacular.io/environment"),
		DeployedBy:    authz.GetAnnotation(ann, "tentacular.io/deployed-by"),
		DeployedVia:   authz.GetAnnotation(ann, "tentacular.io/deployed-via"),
		DeployedAt:    authz.GetAnnotation(ann, "tentacular.io/deployed-at"),
		Ready:         dep.Status.ReadyReplicas >= 1,
		Replicas:      replicaCount(dep.Spec.Replicas),
		ReadyReplicas: dep.Status.ReadyReplicas,
		Image:         image,
		Age:           age,
		Annotations:   tentacularAnn,
	}

	// Attempt to enrich from the workflow ConfigMap (best-effort, non-fatal)
	cmName := params.Name + "-code"
	cm, err := client.Clientset.CoreV1().ConfigMaps(params.Namespace).Get(ctx, cmName, metav1.GetOptions{})
	if err == nil {
		if yamlData, ok := cm.Data["workflow.yaml"]; ok {
			var wf minimalWorkflow
			if parseErr := yaml.Unmarshal([]byte(yamlData), &wf); parseErr == nil {
				if wf.Version != "" {
					result.Version = wf.Version
				}

				nodeNames := make([]string, 0, len(wf.Nodes))
				for name := range wf.Nodes {
					nodeNames = append(nodeNames, name)
				}
				sort.Strings(nodeNames)
				if len(nodeNames) > 0 {
					result.Nodes = nodeNames
				}

				triggerDescs := make([]string, 0, len(wf.Triggers))
				for _, t := range wf.Triggers {
					desc := t.Type
					if t.Schedule != "" {
						desc += " " + t.Schedule
					}
					triggerDescs = append(triggerDescs, desc)
				}
				if len(triggerDescs) > 0 {
					result.Triggers = triggerDescs
				}
			}
		}
	}

	return result, nil
}

func deploymentToListEntry(dep appsv1.Deployment) WfListEntry {
	ann := dep.Annotations
	if ann == nil {
		ann = map[string]string{}
	}
	ownerInfo := authz.ReadOwnerInfo(ann)
	age := time.Since(dep.CreationTimestamp.Time).Round(time.Second).String()
	return WfListEntry{
		Name:        dep.Name,
		Namespace:   dep.Namespace,
		Version:     dep.Labels[k8s.VersionLabel],
		Owner:       ownerInfo.OwnerEmail,
		Group:       ownerInfo.Group,
		Mode:        ownerInfo.Mode.String(),
		Environment: authz.GetAnnotation(ann, "tentacular.io/environment"),
		DeployedBy:  authz.GetAnnotation(ann, "tentacular.io/deployed-by"),
		DeployedVia: authz.GetAnnotation(ann, "tentacular.io/deployed-via"),
		Ready:       dep.Status.ReadyReplicas >= 1,
		Age:         age,
	}
}

// containsTag checks whether a comma-separated tags string contains the given tag.
func containsTag(tagsCSV, tag string) bool {
	for _, t := range strings.Split(tagsCSV, ",") {
		if strings.TrimSpace(t) == tag {
			return true
		}
	}
	return false
}

// replicaCount returns the effective replica count for a Deployment,
// normalizing nil (omitted) to Kubernetes' default of 1.
func replicaCount(p *int32) int32 {
	if p == nil {
		return 1
	}
	return *p
}

func wrapListError(namespace string, err error) error {
	if namespace == "" {
		return fmt.Errorf("list deployments across all namespaces: %w", err)
	}
	return fmt.Errorf("list deployments in namespace %q: %w", namespace, err)
}

func wrapGetError(name, namespace string, err error) error {
	return fmt.Errorf("get deployment %q in namespace %q: %w", name, namespace, err)
}
