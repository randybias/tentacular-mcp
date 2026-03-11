package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"gopkg.in/yaml.v3"

	"github.com/randybias/tentacular-mcp/pkg/guard"
	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

// minimalWorkflow holds only the fields needed for MCP describe reporting.
// It is separate from the main spec package to avoid a cross-repo dependency.
type minimalWorkflow struct {
	Name     string                    `yaml:"name"`
	Version  string                    `yaml:"version"`
	Triggers []minimalTrigger          `yaml:"triggers"`
	Nodes    map[string]minimalNode    `yaml:"nodes"`
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
	Team        string `json:"team,omitempty"`
	Environment string `json:"environment,omitempty"`
	Ready       bool   `json:"ready"`
	Age         string `json:"age"`
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
	Name          string            `json:"name"`
	Namespace     string            `json:"namespace"`
	Version       string            `json:"version"`
	Owner         string            `json:"owner,omitempty"`
	Team          string            `json:"team,omitempty"`
	Tags          []string          `json:"tags,omitempty"`
	Environment   string            `json:"environment,omitempty"`
	Ready         bool              `json:"ready"`
	Replicas      int32             `json:"replicas"`
	ReadyReplicas int32             `json:"ready_replicas"`
	Image         string            `json:"image"`
	Age           string            `json:"age"`
	Nodes         []string          `json:"nodes,omitempty"`
	Triggers      []string          `json:"triggers,omitempty"`
	Annotations   map[string]string `json:"annotations,omitempty"`
}

func registerDiscoverTools(srv *mcp.Server, client *k8s.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "wf_list",
		Description: "List all tentacular-managed workflow deployments across namespaces, with ownership and status.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, params WfListParams) (*mcp.CallToolResult, WfListResult, error) {
		if params.Namespace != "" {
			if err := guard.CheckNamespace(params.Namespace); err != nil {
				return nil, WfListResult{}, err
			}
		}
		result, err := handleWfList(ctx, client, params)
		return nil, result, err
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "wf_describe",
		Description: "Get detailed information about a single tentacular workflow deployment, including metadata annotations, replica status, nodes, and triggers.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, params WfDescribeParams) (*mcp.CallToolResult, WfDescribeResult, error) {
		if err := guard.CheckNamespace(params.Namespace); err != nil {
			return nil, WfDescribeResult{}, err
		}
		result, err := handleWfDescribe(ctx, client, params)
		return nil, result, err
	})
}

// systemNamespacesForList is the set of namespaces that wf_list should never show.
// These are platform infrastructure namespaces, not user workflow namespaces.
var systemNamespacesForList = map[string]bool{
	"tentacular-system":       true,
	"tentacular-support":      true,
	"tentacular-exoskeleton":  true,
	"kube-system":             true,
	"kube-public":             true,
	"kube-node-lease":         true,
	"default":                 true,
}

// isSystemNamespace returns true if the namespace should be filtered from wf_list results.
// A namespace is considered system if it matches a hardcoded name or has the
// tentacular.io/system annotation set to "true".
func isSystemNamespace(ns string, annotations map[string]string) bool {
	if systemNamespacesForList[ns] {
		return true
	}
	if annotations != nil && annotations["tentacular.io/system"] == "true" {
		return true
	}
	return false
}

func handleWfList(ctx context.Context, client *k8s.Client, params WfListParams) (WfListResult, error) {
	ns := params.Namespace
	depList, err := client.Clientset.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/managed-by=tentacular",
	})
	if err != nil {
		return WfListResult{}, wrapListError("deployments", ns, err)
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
		// Filter out system namespaces when listing across all namespaces
		if ns == "" && isSystemNamespace(dep.Namespace, nsAnnotations[dep.Namespace]) {
			continue
		}

		entry := deploymentToListEntry(dep)

		// Apply optional client-side filters
		if params.Owner != "" && entry.Owner != params.Owner {
			continue
		}
		if params.Tag != "" {
			ann := dep.Annotations
			if ann == nil {
				continue
			}
			if !containsTag(ann["tentacular.dev/tags"], params.Tag) {
				continue
			}
		}

		entries = append(entries, entry)
	}

	return WfListResult{Workflows: entries}, nil
}

func handleWfDescribe(ctx context.Context, client *k8s.Client, params WfDescribeParams) (WfDescribeResult, error) {
	dep, err := client.Clientset.AppsV1().Deployments(params.Namespace).Get(ctx, params.Name, metav1.GetOptions{})
	if err != nil {
		return WfDescribeResult{}, wrapGetError("deployment", params.Name, params.Namespace, err)
	}

	ann := dep.Annotations
	if ann == nil {
		ann = map[string]string{}
	}

	var tags []string
	if raw := ann["tentacular.dev/tags"]; raw != "" {
		tags = strings.Split(raw, ",")
	}

	image := ""
	if len(dep.Spec.Template.Spec.Containers) > 0 {
		image = dep.Spec.Template.Spec.Containers[0].Image
	}

	// Collect only tentacular.dev/* annotations for the result
	tentacularAnn := make(map[string]string)
	for k, v := range ann {
		if strings.HasPrefix(k, "tentacular.dev/") {
			tentacularAnn[k] = v
		}
	}
	if len(tentacularAnn) == 0 {
		tentacularAnn = nil
	}

	age := time.Since(dep.CreationTimestamp.Time).Round(time.Second).String()

	result := WfDescribeResult{
		Name:          dep.Name,
		Namespace:     dep.Namespace,
		Version:       dep.Labels[k8s.VersionLabel],
		Owner:         ann["tentacular.dev/owner"],
		Team:          ann["tentacular.dev/team"],
		Tags:          tags,
		Environment:   ann["tentacular.dev/environment"],
		Ready:         dep.Status.ReadyReplicas >= 1,
		Replicas:      derefInt32(dep.Spec.Replicas),
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
	age := time.Since(dep.CreationTimestamp.Time).Round(time.Second).String()
	return WfListEntry{
		Name:        dep.Name,
		Namespace:   dep.Namespace,
		Version:     dep.Labels[k8s.VersionLabel],
		Owner:       ann["tentacular.dev/owner"],
		Team:        ann["tentacular.dev/team"],
		Environment: ann["tentacular.dev/environment"],
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

func derefInt32(p *int32) int32 {
	if p == nil {
		return 0
	}
	return *p
}

func wrapListError(resource, namespace string, err error) error {
	if namespace == "" {
		return fmt.Errorf("list %s across all namespaces: %w", resource, err)
	}
	return fmt.Errorf("list %s in namespace %q: %w", resource, namespace, err)
}

func wrapGetError(resource, name, namespace string, err error) error {
	return fmt.Errorf("get %s %q in namespace %q: %w", resource, name, namespace, err)
}
