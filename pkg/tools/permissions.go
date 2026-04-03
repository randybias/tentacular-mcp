package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/randybias/tentacular-mcp/pkg/authz"
	"github.com/randybias/tentacular-mcp/pkg/exoskeleton"
	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

// PermissionsGetParams are the parameters for permissions_get.
type PermissionsGetParams struct {
	Namespace string `json:"namespace" jsonschema:"Namespace of the workflow"`
	Name      string `json:"name" jsonschema:"Workflow deployment name"`
}

// PermissionsGetResult is the result of permissions_get.
type PermissionsGetResult struct {
	Namespace    string `json:"namespace"`
	Name         string `json:"name"`
	OwnerSub     string `json:"owner_sub,omitempty"`
	OwnerEmail   string `json:"owner_email,omitempty"`
	OwnerName    string `json:"owner_name,omitempty"`
	Group        string `json:"group,omitempty"`
	Mode         string `json:"mode,omitempty"`
	Preset       string `json:"preset,omitempty"`
	AuthProvider string `json:"auth_provider,omitempty"`
}

// PermissionsSetParams are the parameters for permissions_set.
type PermissionsSetParams struct {
	Namespace string `json:"namespace" jsonschema:"Namespace of the workflow"`
	Name      string `json:"name" jsonschema:"Workflow deployment name"`
	Group     string `json:"group,omitempty" jsonschema:"New IdP group to assign to this workflow"`
	// Share accepts a named preset: private, group-read, group-run, group-edit, public-read.
	Share string `json:"share,omitempty" jsonschema:"New permission preset: private, group-read, group-run, group-edit, public-read"`
	// Mode accepts a raw 9-character permission string (e.g. \"rwxr-x---\"). Mutually exclusive with Share.
	Mode string `json:"mode,omitempty" jsonschema:"Raw permission mode string (e.g. rwxr-x---). Mutually exclusive with share."`
}

// PermissionsSetResult is the result of permissions_set.
type PermissionsSetResult struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Group     string `json:"group,omitempty"`
	Mode      string `json:"mode,omitempty"`
	Preset    string `json:"preset,omitempty"`
}

// NsPermissionsGetParams are the parameters for ns_permissions_get.
type NsPermissionsGetParams struct {
	Namespace string `json:"namespace" jsonschema:"Namespace to get permissions for"`
}

// NsPermissionsGetResult is the result of ns_permissions_get.
type NsPermissionsGetResult struct {
	Namespace     string `json:"namespace"`
	OwnerSub      string `json:"owner_sub,omitempty"`
	OwnerEmail    string `json:"owner_email,omitempty"`
	OwnerName     string `json:"owner_name,omitempty"`
	Group         string `json:"group,omitempty"`
	Mode          string `json:"mode,omitempty"`
	Preset        string `json:"preset,omitempty"`
	DefaultGroup  string `json:"default_group,omitempty"`
	DefaultMode   string `json:"default_mode,omitempty"`
	DefaultPreset string `json:"default_preset,omitempty"`
	AuthProvider  string `json:"auth_provider,omitempty"`
}

// NsPermissionsSetParams are the parameters for ns_permissions_set.
type NsPermissionsSetParams struct {
	Namespace    string `json:"namespace" jsonschema:"Namespace to update permissions for"`
	Group        string `json:"group,omitempty" jsonschema:"New IdP group to assign to this namespace"`
	Share        string `json:"share,omitempty" jsonschema:"New permission preset: private, group-read, group-run, group-edit, public-read"`
	Mode         string `json:"mode,omitempty" jsonschema:"Raw permission mode string (e.g. rwxr-x---). Mutually exclusive with share."`
	DefaultGroup string `json:"default_group,omitempty" jsonschema:"Default IdP group for new workflows in this namespace"`
	DefaultShare string `json:"default_share,omitempty" jsonschema:"Default permission preset for new workflows: private, group-read, group-run, group-edit, public-read"`
}

// NsPermissionsSetResult is the result of ns_permissions_set.
type NsPermissionsSetResult struct {
	Namespace    string `json:"namespace"`
	Group        string `json:"group,omitempty"`
	Mode         string `json:"mode,omitempty"`
	Preset       string `json:"preset,omitempty"`
	DefaultGroup string `json:"default_group,omitempty"`
	DefaultMode  string `json:"default_mode,omitempty"`
}

func handlePermissionsGet(ctx context.Context, client *k8s.Client, params PermissionsGetParams, deployer *exoskeleton.DeployerInfo, eval *authz.Evaluator) (PermissionsGetResult, error) {
	dep, err := client.Clientset.AppsV1().Deployments(params.Namespace).Get(ctx, params.Name, metav1.GetOptions{})
	if err != nil {
		return PermissionsGetResult{}, wrapGetError(params.Name, params.Namespace, err)
	}

	nsAnn, nsAnnErr := fetchNamespaceAnnotations(ctx, client, params.Namespace)
	if nsAnnErr != nil {
		return PermissionsGetResult{}, nsAnnErr
	}
	if d := checkAuthz(eval, deployer, nsAnn, dep.Annotations, authz.Read); !d.Allowed {
		return PermissionsGetResult{}, fmt.Errorf("permission denied: %s", d.Reason)
	}

	ann := dep.Annotations
	if ann == nil {
		ann = map[string]string{}
	}
	ownerInfo := authz.ReadOwnerInfo(ann)

	return PermissionsGetResult{
		Namespace:    params.Namespace,
		Name:         params.Name,
		OwnerSub:     ownerInfo.OwnerSub,
		OwnerEmail:   ownerInfo.OwnerEmail,
		OwnerName:    ownerInfo.OwnerName,
		Group:        ownerInfo.Group,
		Mode:         ownerInfo.Mode.String(),
		Preset:       ownerInfo.PresetName,
		AuthProvider: ownerInfo.AuthProvider,
	}, nil
}

func handleNsPermissionsGet(ctx context.Context, client *k8s.Client, params NsPermissionsGetParams, deployer *exoskeleton.DeployerInfo, eval *authz.Evaluator) (NsPermissionsGetResult, error) {
	ns, err := client.Clientset.CoreV1().Namespaces().Get(ctx, params.Namespace, metav1.GetOptions{})
	if err != nil {
		return NsPermissionsGetResult{}, fmt.Errorf("get namespace %q: %w", params.Namespace, err)
	}

	if err := checkNamespaceAuthz(ctx, client, params.Namespace, deployer, eval, authz.Read); err != nil {
		return NsPermissionsGetResult{}, err
	}

	ann := ns.Annotations
	if ann == nil {
		ann = map[string]string{}
	}
	ownerInfo := authz.ReadNamespaceOwnerInfo(ann)

	result := NsPermissionsGetResult{
		Namespace:    params.Namespace,
		OwnerSub:     ownerInfo.OwnerSub,
		OwnerEmail:   ownerInfo.OwnerEmail,
		OwnerName:    ownerInfo.OwnerName,
		Group:        ownerInfo.Group,
		Mode:         ownerInfo.Mode.String(),
		Preset:       ownerInfo.PresetName,
		AuthProvider: ownerInfo.AuthProvider,
	}

	if v := ann[authz.AnnotationDefaultGroup]; v != "" {
		result.DefaultGroup = v
	}
	if v := ann[authz.AnnotationDefaultMode]; v != "" {
		result.DefaultMode = v
		if m, err := authz.ParseMode(v); err == nil {
			result.DefaultPreset = authz.PresetName(m)
		}
	}

	return result, nil
}

func handleNsPermissionsSet(ctx context.Context, client *k8s.Client, params NsPermissionsSetParams, deployer *exoskeleton.DeployerInfo, eval *authz.Evaluator) (NsPermissionsSetResult, error) {
	ns, err := client.Clientset.CoreV1().Namespaces().Get(ctx, params.Namespace, metav1.GetOptions{})
	if err != nil {
		return NsPermissionsSetResult{}, fmt.Errorf("get namespace %q: %w", params.Namespace, err)
	}

	ann := ns.Annotations
	if ann == nil {
		ann = map[string]string{}
	}
	ownerInfo := authz.ReadNamespaceOwnerInfo(ann)

	// ns_permissions_set is owner-only: only the original owner (or bearer-token) may
	// change permissions.
	if eval != nil && eval.Enabled && deployer != nil && deployer.Provider != "bearer-token" {
		owner := ann[authz.AnnotationOwner]
		if owner != "" && deployer.Email != owner {
			return NsPermissionsSetResult{}, errors.New("permission denied: only the owner may change namespace permissions")
		}
	}

	// Determine new group value.
	newGroup := ownerInfo.Group
	if params.Group != "" {
		newGroup = params.Group
	}

	// Determine new mode value: Mode (raw string) takes precedence over Share (preset name).
	newMode := ownerInfo.Mode
	switch {
	case params.Mode != "":
		m, parseErr := authz.ParseMode(params.Mode)
		if parseErr != nil {
			return NsPermissionsSetResult{}, fmt.Errorf("invalid mode %q: %w", params.Mode, parseErr)
		}
		newMode = m
	case params.Share != "":
		m, ok := authz.PresetFromName(params.Share)
		if !ok {
			return NsPermissionsSetResult{}, fmt.Errorf("unknown share preset %q; valid presets: %s", params.Share, authz.ValidPresetNames())
		}
		newMode = m
	}

	// Determine new default mode for workflows in this namespace.
	var newDefaultMode authz.Mode
	if params.DefaultShare != "" {
		m, ok := authz.PresetFromName(params.DefaultShare)
		if !ok {
			return NsPermissionsSetResult{}, fmt.Errorf("unknown default_share preset %q; valid presets: %s", params.DefaultShare, authz.ValidPresetNames())
		}
		newDefaultMode = m
	} else {
		// Preserve existing default mode, or use evaluator default if none.
		if raw, ok := ann[authz.AnnotationDefaultMode]; ok && raw != "" {
			if m, parseErr := authz.ParseMode(raw); parseErr == nil {
				newDefaultMode = m
			}
		}
	}

	// Build patch annotations.
	patchAnnotations := map[string]any{
		"metadata": map[string]any{
			"annotations": map[string]any{
				authz.AnnotationGroup:        newGroup,
				authz.AnnotationMode:         newMode.String(),
				authz.AnnotationDefaultGroup: params.DefaultGroup,
			},
		},
	}

	// Only include default mode if explicitly set or if it was already present.
	if params.DefaultShare != "" || newDefaultMode != 0 {
		patchAnnotations["metadata"].(map[string]any)["annotations"].(map[string]any)[authz.AnnotationDefaultMode] = newDefaultMode.String()
	}

	patchBody, err := json.Marshal(patchAnnotations)
	if err != nil {
		return NsPermissionsSetResult{}, fmt.Errorf("marshal namespace permissions patch: %w", err)
	}

	_, err = client.Clientset.CoreV1().Namespaces().Patch(
		ctx, params.Namespace, types.MergePatchType, patchBody, metav1.PatchOptions{},
	)
	if err != nil {
		return NsPermissionsSetResult{}, fmt.Errorf("patch namespace %q permissions: %w", params.Namespace, err)
	}

	result := NsPermissionsSetResult{
		Namespace: params.Namespace,
		Group:     newGroup,
		Mode:      newMode.String(),
		Preset:    authz.PresetName(newMode),
	}

	if newDefaultMode != 0 {
		result.DefaultMode = newDefaultMode.String()
	}
	if params.DefaultGroup != "" {
		result.DefaultGroup = params.DefaultGroup
	}

	return result, nil
}
