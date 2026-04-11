package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gopkg.in/yaml.v3"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
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

// SidecarMeta is a minimal representation of a sidecar container captured at build time.
type SidecarMeta struct {
	Name  string `json:"name"`
	Image string `json:"image"`
	Port  int    `json:"port"`
}

// DependencyMeta is a minimal representation of a contract dependency.
type DependencyMeta struct {
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Managed  bool   `json:"managed"`
}

// PromptTool is a tool reference within a prompt entry.
type PromptTool struct {
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// PromptEntry describes a single prompt definition within a tentacle.
type PromptEntry struct {
	Node               string       `json:"node" yaml:"node"`
	Name               string       `json:"name" yaml:"name"`
	Description        string       `json:"description,omitempty" yaml:"description,omitempty"`
	Model              string       `json:"model,omitempty" yaml:"model,omitempty"`
	SystemPrompt       string       `json:"system_prompt,omitempty" yaml:"system_prompt,omitempty"`
	UserPromptTemplate string       `json:"user_prompt_template,omitempty" yaml:"user_prompt_template,omitempty"`
	Tools              []PromptTool `json:"tools,omitempty" yaml:"tools,omitempty"`
}

// NodeMeta describes a single node with an optional human-readable description.
type NodeMeta struct {
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// TemplateEntry describes a single template definition within a tentacle.
type TemplateEntry struct {
	Node        string `json:"node" yaml:"node"`
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	Format      string `json:"format,omitempty" yaml:"format,omitempty"`
	Template    string `json:"template,omitempty" yaml:"template,omitempty"`
}

// GitProvenance holds git state captured at build time for audit and reproducibility.
type GitProvenance struct {
	Commit string `json:"commit"`
	Branch string `json:"branch"`
	Repo   string `json:"repo,omitempty"`
	Dirty  bool   `json:"dirty"`
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
	Description string `json:"description,omitempty"`
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
	Namespace string `json:"namespace,omitempty" jsonschema:"Namespace of the workflow (auto-resolved if caller belongs to exactly one enclave)"`
	Name      string `json:"name" jsonschema:"Workflow name"`
}

// WfDescribeResult is the result of wf_describe.
type WfDescribeResult struct {
	Annotations   map[string]string `json:"annotations,omitempty"`
	GitProvenance *GitProvenance    `json:"git_provenance,omitempty"`
	DeployedBy    string            `json:"deployed_by,omitempty"`
	Image         string            `json:"image"`
	OwnerName     string            `json:"owner_name,omitempty"`
	Group         string            `json:"group,omitempty"`
	Mode          string            `json:"mode,omitempty"`
	Preset        string            `json:"preset,omitempty"`
	Namespace     string            `json:"namespace"`
	Environment   string            `json:"environment,omitempty"`
	TriggerType   string            `json:"trigger_type,omitempty"`
	Name          string            `json:"name"`
	Version       string            `json:"version"`
	Owner         string            `json:"owner,omitempty"`
	DeployedAt    string            `json:"deployed_at,omitempty"`
	EnclaveOwner  string            `json:"enclave_owner,omitempty"`
	Age           string            `json:"age"`
	DeployedVia   string            `json:"deployed_via,omitempty"`
	ParamsSchema  string            `json:"params_schema,omitempty"`
	// ContractSummary maps to the "contract" key in the metadata ConfigMap.
	// The field name is more descriptive for API consumers.
	ContractSummary  string           `json:"contract_summary,omitempty"`
	Readme           string           `json:"readme,omitempty"`
	MetadataRef      string           `json:"metadata_ref,omitempty"`
	ScaffoldName     string           `json:"scaffold_name,omitempty"`
	Nodes            []string         `json:"nodes,omitempty"`
	NodeDescriptions []NodeMeta       `json:"node_descriptions,omitempty"`
	Sidecars         []SidecarMeta    `json:"sidecars,omitempty"`
	Dependencies     []DependencyMeta `json:"dependencies,omitempty"`
	Prompts          []PromptEntry    `json:"prompts,omitempty"`
	Templates        []TemplateEntry  `json:"templates,omitempty"`
	Edges            [][2]string      `json:"edges,omitempty"`
	EnclaveMembers   []string         `json:"enclave_members,omitempty"`
	Tags             []string         `json:"tags,omitempty"`
	Triggers         []string         `json:"triggers,omitempty"`
	Replicas         int32            `json:"replicas"`
	ReadyReplicas    int32            `json:"ready_replicas"`
	Ready            bool             `json:"ready"`
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
		if err := requireDeployer(deployer, eval); err != nil {
			return nil, WfListResult{}, err
		}
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
		if err := guard.CheckName(params.Name); err != nil {
			return nil, WfDescribeResult{}, err
		}
		deployer := auth.DeployerFromContext(ctx)
		if err := requireDeployer(deployer, eval); err != nil {
			return nil, WfDescribeResult{}, err
		}
		ns, nsErr := resolveNamespace(ctx, client, params.Namespace, deployer)
		if nsErr != nil {
			return nil, WfDescribeResult{}, nsErr
		}
		if err := guard.CheckNamespace(ns); err != nil {
			return nil, WfDescribeResult{}, err
		}
		params.Namespace = ns
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

	// Build a cache of namespace annotations for authz and system namespace filtering.
	// When listing a specific namespace, pre-populate the cache and check Read permission.
	nsAnnotations := map[string]map[string]string{}
	if ns != "" {
		// Single-namespace listing: pre-fetch annotations and check Read permission.
		nsAnn, nsAnnErr := fetchNamespaceAnnotations(ctx, client, ns)
		if nsAnnErr != nil {
			return WfListResult{}, nsAnnErr
		}
		nsAnnotations[ns] = nsAnn
		// Check namespace-level Read permission (enclave model).
		nsDecision := eval.CheckEnclave(deployer, nsAnn, authz.Read)
		if !nsDecision.Allowed {
			return WfListResult{}, fmt.Errorf("permission denied: %s", nsDecision.Reason)
		}
	} else {
		// Cross-namespace listing: build full namespace annotation cache.
		nsList, nsErr := client.Clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
		if nsErr == nil {
			for _, n := range nsList.Items {
				nsAnnotations[n.Name] = n.Annotations
			}
		}
	}

	depList, err := client.Clientset.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/managed-by=tentacular",
	})
	if err != nil {
		return WfListResult{}, wrapListError(ns, err)
	}

	entries := make([]WfListEntry, 0, len(depList.Items))
	for _, dep := range depList.Items {
		// Filter out system namespaces when listing across all namespaces.
		if ns == "" && isSystemNamespace(dep.Namespace, nsAnnotations[dep.Namespace]) {
			continue
		}

		// Namespace-level authz filter: when listing across all namespaces, skip
		// namespaces where the caller lacks Read permission (enclave model).
		if ns == "" {
			nsAnn := nsAnnotations[dep.Namespace]
			if nsAnn == nil {
				nsAnn = map[string]string{}
			}
			if !eval.CheckEnclave(deployer, nsAnn, authz.Read).Allowed {
				continue
			}
		}

		// Authz filter: skip deployments the caller cannot read.
		// Use the cached namespace annotations for dual-path routing.
		depNsAnn := nsAnnotations[dep.Namespace]
		if depNsAnn == nil {
			depNsAnn = map[string]string{}
		}
		if d := checkAuthz(eval, deployer, depNsAnn, dep.Annotations, authz.Read); !d.Allowed {
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

	// Fetch namespace annotations for dual-path authz routing.
	nsAnn, nsAnnErr := fetchNamespaceAnnotations(ctx, client, params.Namespace)
	if nsAnnErr != nil {
		return WfDescribeResult{}, nsAnnErr
	}

	// Authz check: caller must have Read permission (enclave or legacy path).
	if d := checkAuthz(eval, deployer, nsAnn, dep.Annotations, authz.Read); !d.Allowed {
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
	enclaveInfo := authz.ReadEnclaveInfo(nsAnn)

	result := WfDescribeResult{
		Name:           dep.Name,
		Namespace:      dep.Namespace,
		Version:        dep.Labels[k8s.VersionLabel],
		Owner:          ownerInfo.OwnerEmail,
		OwnerName:      ownerInfo.OwnerName,
		Group:          ownerInfo.Group,
		Mode:           ownerInfo.Mode.String(),
		Preset:         ownerInfo.PresetName,
		Tags:           tags,
		Environment:    authz.GetAnnotation(ann, "tentacular.io/environment"),
		DeployedBy:     authz.GetAnnotation(ann, "tentacular.io/deployed-by"),
		DeployedVia:    authz.GetAnnotation(ann, "tentacular.io/deployed-via"),
		DeployedAt:     authz.GetAnnotation(ann, "tentacular.io/deployed-at"),
		Ready:          dep.Status.ReadyReplicas >= 1,
		Replicas:       replicaCount(dep.Spec.Replicas),
		ReadyReplicas:  dep.Status.ReadyReplicas,
		Image:          image,
		Age:            age,
		Annotations:    tentacularAnn,
		EnclaveOwner:   enclaveInfo.Owner,
		EnclaveMembers: enclaveInfo.Members,
	}

	// --- Tier 1: metadata annotations written by the builder ---
	// All parsing is best-effort; invalid JSON is logged and skipped.
	if raw := ann["tentacular.io/nodes"]; raw != "" {
		var nodeNames []string
		if parseErr := json.Unmarshal([]byte(raw), &nodeNames); parseErr == nil {
			result.Nodes = nodeNames
		} else {
			slog.Warn("wf_describe: invalid JSON in tentacular.io/nodes annotation", "deployment", params.Name, "error", parseErr)
		}
	}

	if raw := ann["tentacular.io/edges"]; raw != "" {
		var edges [][2]string
		if parseErr := json.Unmarshal([]byte(raw), &edges); parseErr == nil {
			result.Edges = edges
		} else {
			slog.Warn("wf_describe: invalid JSON in tentacular.io/edges annotation", "deployment", params.Name, "error", parseErr)
		}
	}

	if raw := ann["tentacular.io/sidecars"]; raw != "" {
		var sidecars []SidecarMeta
		if parseErr := json.Unmarshal([]byte(raw), &sidecars); parseErr == nil {
			result.Sidecars = sidecars
		} else {
			slog.Warn("wf_describe: invalid JSON in tentacular.io/sidecars annotation", "deployment", params.Name, "error", parseErr)
		}
	}

	if raw := ann["tentacular.io/dependencies"]; raw != "" {
		var deps []DependencyMeta
		if parseErr := json.Unmarshal([]byte(raw), &deps); parseErr == nil {
			result.Dependencies = deps
		} else {
			slog.Warn("wf_describe: invalid JSON in tentacular.io/dependencies annotation", "deployment", params.Name, "error", parseErr)
		}
	}

	if raw := ann["tentacular.io/trigger-type"]; raw != "" {
		result.TriggerType = raw
	}

	if raw := ann["tentacular.io/scaffold-name"]; raw != "" {
		result.ScaffoldName = raw
	}

	if raw := ann["tentacular.io/metadata-ref"]; raw != "" {
		result.MetadataRef = raw
	}

	// Track whether the annotation provided a version — used below to decide whether
	// the ConfigMap version should override the label-derived version.
	annotationVersion := ann["tentacular.io/version"]
	if annotationVersion != "" {
		// Annotation version (semver+git from builder) takes highest priority.
		result.Version = annotationVersion
	}

	// --- Existing code ConfigMap enrichment (fallback for pre-metadata deployments) ---
	// Only parse the code ConfigMap if nodes/triggers were not populated from annotations.
	cmName := params.Name + "-code"
	cm, err := client.Clientset.CoreV1().ConfigMaps(params.Namespace).Get(ctx, cmName, metav1.GetOptions{})
	if err == nil {
		if yamlData, ok := cm.Data["workflow.yaml"]; ok {
			var wf minimalWorkflow
			if parseErr := yaml.Unmarshal([]byte(yamlData), &wf); parseErr == nil {
				// ConfigMap version overrides label-derived version,
				// but not annotation-derived version (annotation is highest priority).
				if annotationVersion == "" && wf.Version != "" {
					result.Version = wf.Version
				}

				// Nodes: use annotation-derived value if present; fall back to YAML parse.
				if len(result.Nodes) == 0 {
					nodeNames := make([]string, 0, len(wf.Nodes))
					for name := range wf.Nodes {
						nodeNames = append(nodeNames, name)
					}
					sort.Strings(nodeNames)
					if len(nodeNames) > 0 {
						result.Nodes = nodeNames
					}
				}

				// Triggers: fall back to YAML parse if TriggerType annotation is absent.
				if len(result.Triggers) == 0 {
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
	}

	// --- Tier 2: metadata ConfigMap written by the builder ---
	// Missing ConfigMap is non-fatal (old deployments won't have one).
	// Only attempt the lookup if the deployment has an explicit metadata-ref annotation
	// or at least one other metadata annotation (e.g. tentacular.io/nodes), which
	// indicates it was built with the metadata-aware builder. This avoids a
	// unnecessary API call for every pre-metadata deployment.
	metadataRef := result.MetadataRef
	hasMetadataAnnotations := metadataRef != "" ||
		ann["tentacular.io/nodes"] != "" ||
		ann["tentacular.io/edges"] != "" ||
		ann["tentacular.io/sidecars"] != "" ||
		ann["tentacular.io/dependencies"] != "" ||
		ann["tentacular.io/trigger-type"] != "" ||
		ann["tentacular.io/scaffold-name"] != ""
	if metadataRef == "" {
		metadataRef = params.Name + "-metadata"
	}
	var metaCM *corev1.ConfigMap
	var metaErr error
	if hasMetadataAnnotations {
		metaCM, metaErr = client.Clientset.CoreV1().ConfigMaps(params.Namespace).Get(
			ctx, metadataRef, metav1.GetOptions{},
		)
	}
	if metaErr == nil && metaCM != nil && metaCM.Data != nil {
		if v, ok := metaCM.Data["readme"]; ok {
			result.Readme = v
		}
		// ConfigMap key "contract" → API field "contract_summary"
		if v, ok := metaCM.Data["contract"]; ok {
			result.ContractSummary = v
		}
		if v, ok := metaCM.Data["params_schema"]; ok {
			result.ParamsSchema = v
		}
		if v, ok := metaCM.Data["git_provenance"]; ok {
			var prov GitProvenance
			if jsonErr := json.Unmarshal([]byte(v), &prov); jsonErr == nil {
				result.GitProvenance = &prov
			} else {
				slog.Warn("wf_describe: invalid JSON in git_provenance ConfigMap key", "deployment", params.Name, "error", jsonErr)
			}
		}
		if v, ok := metaCM.Data["prompts"]; ok {
			var promptsDoc struct {
				Prompts   []PromptEntry   `yaml:"prompts"`
				Templates []TemplateEntry `yaml:"templates"`
			}
			if yamlErr := yaml.Unmarshal([]byte(v), &promptsDoc); yamlErr == nil {
				result.Prompts = promptsDoc.Prompts
				result.Templates = promptsDoc.Templates
			} else {
				slog.Warn("wf_describe: invalid YAML in prompts ConfigMap key",
					"deployment", params.Name, "error", yamlErr)
			}
		}
		// node_descriptions: JSON array of [{name, description}] written by the builder
		// (tentacular/pkg/builder/metadata.go). Falls back to empty when the key is absent
		// (pre-description deploys).
		if v, ok := metaCM.Data["node_descriptions"]; ok {
			var descs []NodeMeta
			if jsonErr := json.Unmarshal([]byte(v), &descs); jsonErr == nil {
				result.NodeDescriptions = descs
			} else {
				slog.Warn("wf_describe: invalid JSON in node_descriptions ConfigMap key",
					"deployment", params.Name, "error", jsonErr)
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
		Description: authz.GetAnnotation(ann, "tentacular.io/description"),
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
