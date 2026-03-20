package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/randybias/tentacular-mcp/pkg/auth"
	"github.com/randybias/tentacular-mcp/pkg/authz"
	"github.com/randybias/tentacular-mcp/pkg/exoskeleton"
	"github.com/randybias/tentacular-mcp/pkg/guard"
	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

// NsCreateParams are the parameters for ns_create.
type NsCreateParams struct {
	Name        string `json:"name" jsonschema:"Name of the namespace to create"`
	QuotaPreset string `json:"quota_preset" jsonschema:"Resource quota preset: small, medium, or large"`
	// Group is the IdP group assigned to this namespace.
	Group string `json:"group,omitempty" jsonschema:"IdP group to assign to this namespace"`
	// Share accepts a named preset: private, group-read, group-run, group-edit, public-read.
	Share string `json:"share,omitempty" jsonschema:"Permission preset for this namespace: private, group-read, group-run, group-edit, public-read"`
	// Mode accepts a raw 9-character permission string (e.g. "rwxr-x---"). Mutually exclusive with share.
	Mode string `json:"mode,omitempty" jsonschema:"Raw permission mode string (e.g. rwxr-x---). Mutually exclusive with share."`
	// DefaultGroup is the default IdP group for new tentacles (workflows) created in this namespace.
	DefaultGroup string `json:"default_group,omitempty" jsonschema:"Default group for new workflows in this namespace"`
	// DefaultShare is the default permission preset for new workflows in this namespace.
	DefaultShare string `json:"default_share,omitempty" jsonschema:"Default permission preset for new workflows in this namespace"`
}

// NsCreateResult is the result of ns_create.
type NsCreateResult struct {
	Name             string   `json:"name"`
	Status           string   `json:"status"`
	QuotaPreset      string   `json:"quota_preset"`
	ResourcesCreated []string `json:"resources_created"`
}

// NsDeleteParams are the parameters for ns_delete.
type NsDeleteParams struct {
	Name string `json:"name" jsonschema:"Name of the namespace to delete"`
}

// NsDeleteResult is the result of ns_delete.
type NsDeleteResult struct {
	Name    string `json:"name"`
	Deleted bool   `json:"deleted"`
}

// NsGetParams are the parameters for ns_get.
type NsGetParams struct {
	Name string `json:"name" jsonschema:"Name of the namespace to get"`
}

// NsGetResult is the result of ns_get.
type NsGetResult struct {
	Labels      map[string]string      `json:"labels"`
	Annotations map[string]string      `json:"annotations"`
	Quota       *k8s.QuotaSummary      `json:"quota,omitempty"`
	LimitRange  *k8s.LimitRangeSummary `json:"limitRange,omitempty"`
	Name        string                 `json:"name"`
	Status      string                 `json:"status"`
	Managed     bool                   `json:"managed"`
}

// NsUpdateParams are the parameters for ns_update.
type NsUpdateParams struct {
	Name        string            `json:"name" jsonschema:"Name of the namespace to update"`
	Labels      map[string]string `json:"labels,omitempty" jsonschema:"Labels to add or update (existing labels not listed are preserved)"`
	Annotations map[string]string `json:"annotations,omitempty" jsonschema:"Annotations to add or update (existing annotations not listed are preserved)"`
	QuotaPreset string            `json:"quota_preset,omitempty" jsonschema:"New resource quota preset: small, medium, or large"`
	// Group updates the IdP group assigned to this namespace.
	Group string `json:"group,omitempty" jsonschema:"New IdP group to assign to this namespace"`
	// Share accepts a named preset: private, group-read, group-run, group-edit, public-read.
	Share string `json:"share,omitempty" jsonschema:"New permission preset: private, group-read, group-run, group-edit, public-read"`
	// Mode accepts a raw 9-character permission string. Mutually exclusive with share.
	Mode string `json:"mode,omitempty" jsonschema:"Raw permission mode string (e.g. rwxr-x---). Mutually exclusive with share."`
}

// NsUpdateResult is the result of ns_update.
type NsUpdateResult struct {
	Name    string   `json:"name"`
	Updated []string `json:"updated"`
}

// NsListParams are the parameters for ns_list (empty).
type NsListParams struct{}

// NsListItem is a single namespace in the list result.
type NsListItem struct {
	Name        string `json:"name"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
	QuotaPreset string `json:"quota_preset,omitempty"`
}

// NsListResult is the result of ns_list.
type NsListResult struct {
	Namespaces []NsListItem `json:"namespaces"`
}

func registerNamespaceTools(srv *mcp.Server, client *k8s.Client, eval *authz.Evaluator) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "ns_create",
		Description: "Create a new managed namespace with network policies, resource quotas, and workflow RBAC.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Create Managed Namespace",
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  false,
			OpenWorldHint:   boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, params NsCreateParams) (*mcp.CallToolResult, NsCreateResult, error) {
		if err := guard.CheckNamespace(params.Name); err != nil {
			return nil, NsCreateResult{}, err
		}
		if params.Mode != "" && params.Share != "" {
			return nil, NsCreateResult{}, errors.New("mode and share are mutually exclusive; provide one or the other")
		}
		deployer := auth.DeployerFromContext(ctx)
		result, err := handleNsCreate(ctx, client, eval, params, deployer)
		return nil, result, err
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "ns_delete",
		Description: "Delete a managed namespace. Only namespaces with the tentacular managed-by label can be deleted.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Delete Managed Namespace",
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(true),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, params NsDeleteParams) (*mcp.CallToolResult, NsDeleteResult, error) {
		if err := guard.CheckNamespace(params.Name); err != nil {
			return nil, NsDeleteResult{}, err
		}
		deployer := auth.DeployerFromContext(ctx)
		result, err := handleNsDelete(ctx, client, eval, params, deployer)
		return nil, result, err
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "ns_get",
		Description: "Get details for a namespace including labels, status, quota summary, and limit range summary.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Get Namespace Details",
			ReadOnlyHint:    true,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, params NsGetParams) (*mcp.CallToolResult, NsGetResult, error) {
		if err := guard.CheckNamespace(params.Name); err != nil {
			return nil, NsGetResult{}, err
		}
		deployer := auth.DeployerFromContext(ctx)
		result, err := handleNsGet(ctx, client, eval, params, deployer)
		return nil, result, err
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "ns_list",
		Description: "List all namespaces managed by tentacular.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "List Managed Namespaces",
			ReadOnlyHint:    true,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, params NsListParams) (*mcp.CallToolResult, NsListResult, error) {
		deployer := auth.DeployerFromContext(ctx)
		result, err := handleNsList(ctx, client, eval, deployer)
		return nil, result, err
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "ns_update",
		Description: "Update labels, annotations, resource quota preset, or permissions on a managed namespace.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Update Namespace Metadata",
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, params NsUpdateParams) (*mcp.CallToolResult, NsUpdateResult, error) {
		if err := guard.CheckNamespace(params.Name); err != nil {
			return nil, NsUpdateResult{}, err
		}
		if params.Mode != "" && params.Share != "" {
			return nil, NsUpdateResult{}, errors.New("mode and share are mutually exclusive; provide one or the other")
		}
		deployer := auth.DeployerFromContext(ctx)
		result, err := handleNsUpdate(ctx, client, eval, params, deployer)
		return nil, result, err
	})
}

func handleNsCreate(ctx context.Context, client *k8s.Client, eval *authz.Evaluator, params NsCreateParams, deployer *exoskeleton.DeployerInfo) (NsCreateResult, error) {
	created := []string{}

	if err := k8s.CreateNamespace(ctx, client, params.Name); err != nil {
		return NsCreateResult{}, err
	}
	created = append(created, "namespace/"+params.Name)

	// Stamp authz annotations on the namespace.
	if deployer != nil {
		// Resolve namespace mode from share preset or raw mode string.
		nsMode := authz.DefaultMode
		switch {
		case params.Mode != "":
			if m, err := authz.ParseMode(params.Mode); err == nil {
				nsMode = m
			}
		case params.Share != "":
			if m, ok := authz.PresetFromName(params.Share); ok {
				nsMode = m
			}
		case eval != nil:
			nsMode = eval.DefaultMode
		}

		// Resolve default mode for new workflows in this namespace.
		var defaultMode authz.Mode
		if params.DefaultShare != "" {
			if m, ok := authz.PresetFromName(params.DefaultShare); ok {
				defaultMode = m
			}
		}

		ownerAnnotations := authz.WriteNamespaceAnnotations(
			deployer.Subject,
			deployer.Email,
			deployer.DisplayName,
			params.Group,
			nsMode,
			params.DefaultGroup,
			defaultMode,
		)

		ns, getErr := client.Clientset.CoreV1().Namespaces().Get(ctx, params.Name, metav1.GetOptions{})
		if getErr == nil {
			if ns.Annotations == nil {
				ns.Annotations = map[string]string{}
			}
			for k, v := range ownerAnnotations {
				ns.Annotations[k] = v
			}
			_, patchErr := client.Clientset.CoreV1().Namespaces().Update(ctx, ns, metav1.UpdateOptions{})
			if patchErr != nil {
				return NsCreateResult{}, fmt.Errorf("namespace created but failed to annotate with authz: %w", patchErr)
			}
		}

		slog.Info("ns_create authz stamped",
			"namespace", params.Name,
			"owner", deployer.Subject,
			"group", params.Group,
			"mode", nsMode.String(),
		)
	}

	if err := k8s.CreateDefaultDenyPolicy(ctx, client, params.Name); err != nil {
		return NsCreateResult{}, fmt.Errorf("namespace created but failed to create default-deny network policy: %w", err)
	}
	created = append(created, "networkpolicy/default-deny")

	if err := k8s.CreateDNSAllowPolicy(ctx, client, params.Name); err != nil {
		return NsCreateResult{}, fmt.Errorf("namespace created but failed to create allow-dns network policy: %w", err)
	}
	created = append(created, "networkpolicy/allow-dns")

	if err := k8s.CreateResourceQuota(ctx, client, params.Name, params.QuotaPreset); err != nil {
		return NsCreateResult{}, fmt.Errorf("namespace created but failed to create resource quota: %w", err)
	}
	created = append(created, "resourcequota/tentacular-quota")

	if err := k8s.CreateLimitRange(ctx, client, params.Name); err != nil {
		return NsCreateResult{}, fmt.Errorf("namespace created but failed to create limit range: %w", err)
	}
	created = append(created, "limitrange/tentacular-limits")

	if err := k8s.CreateWorkflowServiceAccount(ctx, client, params.Name); err != nil {
		return NsCreateResult{}, fmt.Errorf("namespace created but failed to create workflow service account: %w", err)
	}
	created = append(created, "serviceaccount/tentacular-workflow")

	if err := k8s.CreateWorkflowRole(ctx, client, params.Name); err != nil {
		return NsCreateResult{}, fmt.Errorf("namespace created but failed to create workflow role: %w", err)
	}
	created = append(created, "role/tentacular-workflow")

	if err := k8s.CreateWorkflowRoleBinding(ctx, client, params.Name); err != nil {
		return NsCreateResult{}, fmt.Errorf("namespace created but failed to create workflow role binding: %w", err)
	}
	created = append(created, "rolebinding/tentacular-workflow")

	return NsCreateResult{
		Name:             params.Name,
		Status:           "Active",
		QuotaPreset:      params.QuotaPreset,
		ResourcesCreated: created,
	}, nil
}

func handleNsDelete(ctx context.Context, client *k8s.Client, eval *authz.Evaluator, params NsDeleteParams, deployer *exoskeleton.DeployerInfo) (NsDeleteResult, error) {
	ns, err := k8s.GetNamespace(ctx, client, params.Name)
	if err != nil {
		return NsDeleteResult{}, err
	}

	if !k8s.IsManagedNamespace(ns) {
		return NsDeleteResult{}, fmt.Errorf("namespace %q is not managed by tentacular and cannot be deleted", params.Name)
	}

	// Authz check: caller must have Write permission on the namespace.
	if deployer != nil {
		ann := ns.Annotations
		if ann == nil {
			ann = map[string]string{}
		}
		if d := eval.Check(deployer, ann, authz.Write); !d.Allowed {
			slog.Info("ns_delete authz denied", "namespace", params.Name, "subject", deployer.Subject, "reason", d.Reason)
			return NsDeleteResult{}, fmt.Errorf("permission denied: %s", d.Reason)
		}
		slog.Info("ns_delete authz allowed", "namespace", params.Name, "subject", deployer.Subject)
	}

	if err := k8s.DeleteNamespace(ctx, client, params.Name); err != nil {
		return NsDeleteResult{}, err
	}

	return NsDeleteResult{Name: params.Name, Deleted: true}, nil
}

func handleNsGet(ctx context.Context, client *k8s.Client, eval *authz.Evaluator, params NsGetParams, deployer *exoskeleton.DeployerInfo) (NsGetResult, error) {
	ns, err := k8s.GetNamespace(ctx, client, params.Name)
	if err != nil {
		return NsGetResult{}, err
	}

	// Authz check: caller must have Read permission on the namespace.
	if deployer != nil {
		ann := ns.Annotations
		if ann == nil {
			ann = map[string]string{}
		}
		if d := eval.Check(deployer, ann, authz.Read); !d.Allowed {
			slog.Info("ns_get authz denied", "namespace", params.Name, "subject", deployer.Subject, "reason", d.Reason)
			return NsGetResult{}, fmt.Errorf("permission denied: %s", d.Reason)
		}
	}

	labels := ns.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	annotations := ns.Annotations
	if annotations == nil {
		annotations = map[string]string{}
	}
	result := NsGetResult{
		Name:        ns.Name,
		Labels:      labels,
		Annotations: annotations,
		Status:      string(ns.Status.Phase),
		Managed:     k8s.IsManagedNamespace(ns),
	}

	// Fetch quota summary
	quotas, err := client.Clientset.CoreV1().ResourceQuotas(params.Name).List(ctx, metav1.ListOptions{})
	if err == nil && len(quotas.Items) > 0 {
		q := quotas.Items[0]
		qs := &k8s.QuotaSummary{}
		if v, ok := q.Spec.Hard[corev1.ResourceLimitsCPU]; ok {
			qs.CPULimit = v.String()
		}
		if v, ok := q.Spec.Hard[corev1.ResourceLimitsMemory]; ok {
			qs.MemoryLimit = v.String()
		}
		if v, ok := q.Spec.Hard[corev1.ResourcePods]; ok {
			qs.MaxPods = int(v.Value())
		}
		result.Quota = qs
	}

	// Fetch limit range summary
	lrs, err := client.Clientset.CoreV1().LimitRanges(params.Name).List(ctx, metav1.ListOptions{})
	if err == nil && len(lrs.Items) > 0 {
		lr := lrs.Items[0]
		for _, item := range lr.Spec.Limits {
			if item.Type == corev1.LimitTypeContainer {
				lrs := &k8s.LimitRangeSummary{}
				if v, ok := item.DefaultRequest[corev1.ResourceCPU]; ok {
					lrs.DefaultCPURequest = v.String()
				}
				if v, ok := item.DefaultRequest[corev1.ResourceMemory]; ok {
					lrs.DefaultMemoryRequest = v.String()
				}
				if v, ok := item.Default[corev1.ResourceCPU]; ok {
					lrs.DefaultCPULimit = v.String()
				}
				if v, ok := item.Default[corev1.ResourceMemory]; ok {
					lrs.DefaultMemoryLimit = v.String()
				}
				result.LimitRange = lrs
				break
			}
		}
	}

	return result, nil
}

func handleNsUpdate(ctx context.Context, client *k8s.Client, eval *authz.Evaluator, params NsUpdateParams, deployer *exoskeleton.DeployerInfo) (NsUpdateResult, error) {
	if err := k8s.CheckManagedNamespace(ctx, client, params.Name); err != nil {
		return NsUpdateResult{}, err
	}

	if len(params.Labels) == 0 && len(params.Annotations) == 0 && params.QuotaPreset == "" && params.Group == "" && params.Share == "" && params.Mode == "" {
		return NsUpdateResult{}, errors.New("at least one of labels, annotations, quota_preset, group, share, or mode must be provided")
	}

	// Authz check: caller must have Write permission on the namespace.
	if deployer != nil {
		if err := checkNamespaceAuthz(ctx, client, params.Name, deployer, eval, authz.Write); err != nil {
			slog.Info("ns_update authz denied", "namespace", params.Name, "subject", deployer.Subject)
			return NsUpdateResult{}, err
		}
	}

	updated := []string{}

	// Patch labels and/or annotations on the namespace via merge patch.
	if len(params.Labels) > 0 || len(params.Annotations) > 0 {
		// Prevent overwriting the managed-by label.
		if v, ok := params.Labels[k8s.ManagedByLabel]; ok && v != k8s.ManagedByValue {
			return NsUpdateResult{}, fmt.Errorf("cannot change the %s label", k8s.ManagedByLabel)
		}

		// Prevent overwriting authz annotations via the generic annotations field.
		// These are only modifiable through ns_permissions_set (owner-only).
		for k := range params.Annotations {
			if strings.HasPrefix(k, "tentacular.io/owner-") ||
				k == "tentacular.io/mode" ||
				k == "tentacular.io/group" ||
				k == "tentacular.io/created-at" ||
				k == "tentacular.io/default-mode" ||
				k == "tentacular.io/default-group" {
				return NsUpdateResult{}, fmt.Errorf("annotation %q is protected; use ns_permissions_set to change ownership/permissions", k)
			}
		}

		patchMeta := map[string]any{}
		if len(params.Labels) > 0 {
			patchMeta["labels"] = params.Labels
		}
		if len(params.Annotations) > 0 {
			patchMeta["annotations"] = params.Annotations
		}
		patchBody, err := json.Marshal(map[string]any{"metadata": patchMeta})
		if err != nil {
			return NsUpdateResult{}, fmt.Errorf("marshal patch: %w", err)
		}

		_, err = client.Clientset.CoreV1().Namespaces().Patch(
			ctx, params.Name, types.MergePatchType, patchBody, metav1.PatchOptions{},
		)
		if err != nil {
			return NsUpdateResult{}, fmt.Errorf("patch namespace %q metadata: %w", params.Name, err)
		}

		if len(params.Labels) > 0 {
			updated = append(updated, "labels")
		}
		if len(params.Annotations) > 0 {
			updated = append(updated, "annotations")
		}
	}

	// Update group and/or mode (permissions) if requested.
	if params.Group != "" || params.Share != "" || params.Mode != "" {
		// owner-only: only the namespace owner (or bearer-token) may change group/mode.
		if eval != nil && eval.Enabled && deployer != nil && deployer.Provider != "bearer-token" {
			ns, getErr := client.Clientset.CoreV1().Namespaces().Get(ctx, params.Name, metav1.GetOptions{})
			if getErr == nil {
				ownerSub := ns.Annotations[authz.AnnotationOwnerSub]
				if ownerSub != "" && deployer.Subject != ownerSub {
					return NsUpdateResult{}, errors.New("permission denied: only the namespace owner may change group or mode")
				}
			}
		}

		permPatch := map[string]any{}
		if params.Group != "" {
			permPatch[authz.AnnotationGroup] = params.Group
		}

		if params.Mode != "" {
			m, err := authz.ParseMode(params.Mode)
			if err != nil {
				return NsUpdateResult{}, fmt.Errorf("invalid mode %q: %w", params.Mode, err)
			}
			permPatch[authz.AnnotationMode] = m.String()
		} else if params.Share != "" {
			m, ok := authz.PresetFromName(params.Share)
			if !ok {
				return NsUpdateResult{}, fmt.Errorf("unknown share preset %q; valid presets: %s", params.Share, authz.ValidPresetNames())
			}
			permPatch[authz.AnnotationMode] = m.String()
		}

		patchBody, err := json.Marshal(map[string]any{
			"metadata": map[string]any{"annotations": permPatch},
		})
		if err != nil {
			return NsUpdateResult{}, fmt.Errorf("marshal permissions patch: %w", err)
		}

		_, err = client.Clientset.CoreV1().Namespaces().Patch(
			ctx, params.Name, types.MergePatchType, patchBody, metav1.PatchOptions{},
		)
		if err != nil {
			return NsUpdateResult{}, fmt.Errorf("patch namespace %q permissions: %w", params.Name, err)
		}

		updated = append(updated, "permissions")
		subject := ""
		if deployer != nil {
			subject = deployer.Subject
		}
		slog.Info("ns_update permissions changed", "namespace", params.Name, "subject", subject)
	}

	// Update resource quota if requested.
	if params.QuotaPreset != "" {
		if err := k8s.UpdateResourceQuota(ctx, client, params.Name, params.QuotaPreset); err != nil {
			return NsUpdateResult{}, err
		}
		updated = append(updated, "quota")
	}

	return NsUpdateResult{
		Name:    params.Name,
		Updated: updated,
	}, nil
}

func handleNsList(ctx context.Context, client *k8s.Client, eval *authz.Evaluator, deployer *exoskeleton.DeployerInfo) (NsListResult, error) {
	namespaces, err := k8s.ListManagedNamespaces(ctx, client)
	if err != nil {
		return NsListResult{}, err
	}

	items := make([]NsListItem, 0, len(namespaces))
	for _, ns := range namespaces {
		// Authz filter: skip namespaces the caller cannot read.
		ann := ns.Annotations
		if ann == nil {
			ann = map[string]string{}
		}
		if deployer != nil {
			if d := eval.Check(deployer, ann, authz.Read); !d.Allowed {
				continue
			}
		}

		item := NsListItem{
			Name:      ns.Name,
			Status:    string(ns.Status.Phase),
			CreatedAt: ns.CreationTimestamp.Format(time.RFC3339),
		}
		items = append(items, item)
	}

	return NsListResult{Namespaces: items}, nil
}
