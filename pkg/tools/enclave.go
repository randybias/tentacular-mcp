package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/randybias/tentacular-mcp/pkg/auth"
	"github.com/randybias/tentacular-mcp/pkg/authz"
	"github.com/randybias/tentacular-mcp/pkg/exoskeleton"
	"github.com/randybias/tentacular-mcp/pkg/guard"
	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

// annotationEnclaveQuotaPreset stores the quota preset on the enclave namespace annotation.
const annotationEnclaveQuotaPreset = "tentacular.io/enclave-quota-preset"

// enclaveRequiredServices lists the exoskeleton services always provisioned for enclaves.
var enclaveRequiredServices = []string{"postgres", "rustfs"}

// EnclaveProvisionParams are the parameters for enclave_provision.
type EnclaveProvisionParams struct {
	Name        string   `json:"name" jsonschema:"Name of the enclave (becomes the namespace name)"`
	OwnerEmail  string   `json:"owner_email" jsonschema:"Email address of the enclave owner"`
	OwnerSub    string   `json:"owner_sub" jsonschema:"OIDC subject of the enclave owner"`
	Platform    string   `json:"platform,omitempty" jsonschema:"Platform binding (e.g. slack)"`
	ChannelID   string   `json:"channel_id,omitempty" jsonschema:"Platform channel ID"`
	ChannelName string   `json:"channel_name,omitempty" jsonschema:"Platform channel name"`
	QuotaPreset string   `json:"quota_preset,omitempty" jsonschema:"Resource quota preset: small, medium, or large (default: medium)"`
	Members     []string `json:"members,omitempty" jsonschema:"Initial member email addresses (excludes owner)"`
}

// EnclaveProvisionResult is the result of enclave_provision.
type EnclaveProvisionResult struct {
	Name             string   `json:"name"`
	Status           string   `json:"status"`
	QuotaPreset      string   `json:"quota_preset"`
	Owner            string   `json:"owner"`
	Members          []string `json:"members"`
	ResourcesCreated []string `json:"resources_created"`
}

// EnclaveInfoParams are the parameters for enclave_info.
type EnclaveInfoParams struct {
	Name string `json:"name" jsonschema:"Name of the enclave"`
}

// EnclaveInfoResult is the result of enclave_info.
type EnclaveInfoResult struct {
	CreatedAt     string              `json:"created_at,omitempty"`
	Owner         string              `json:"owner"`
	OwnerSub      string              `json:"owner_sub"`
	Platform      string              `json:"platform,omitempty"`
	ChannelID     string              `json:"channel_id,omitempty"`
	ChannelName   string              `json:"channel_name,omitempty"`
	Status        string              `json:"status"`
	QuotaPreset   string              `json:"quota_preset,omitempty"`
	Name          string              `json:"name"`
	UpdatedAt     string              `json:"updated_at,omitempty"`
	Members       []string            `json:"members"`
	ExoServices   []EnclaveExoService `json:"exo_services"`
	TentacleCount int                 `json:"tentacle_count"`
}

// EnclaveExoService describes a single exoskeleton service for an enclave.
type EnclaveExoService struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
}

// EnclaveListParams are the parameters for enclave_list.
type EnclaveListParams struct {
	CallerEmail string `json:"caller_email,omitempty" jsonschema:"Filter to enclaves where caller is owner or member"`
}

// EnclaveListItem is a single enclave in the list result.
type EnclaveListItem struct {
	Name        string   `json:"name"`
	Owner       string   `json:"owner"`
	Status      string   `json:"status"`
	Platform    string   `json:"platform,omitempty"`
	ChannelName string   `json:"channel_name,omitempty"`
	CreatedAt   string   `json:"created_at,omitempty"`
	Members     []string `json:"members"`
}

// EnclaveListResult is the result of enclave_list.
type EnclaveListResult struct {
	Enclaves []EnclaveListItem `json:"enclaves"`
}

// EnclaveSyncParams are the parameters for enclave_sync.
type EnclaveSyncParams struct {
	Name           string   `json:"name" jsonschema:"Name of the enclave to update"`
	NewOwner       string   `json:"new_owner,omitempty" jsonschema:"Transfer ownership to this email (must be a current member)"`
	NewChannelName string   `json:"new_channel_name,omitempty" jsonschema:"Update the platform channel name"`
	NewStatus      string   `json:"new_status,omitempty" jsonschema:"Update enclave lifecycle status: active or frozen"`
	AddMembers     []string `json:"add_members,omitempty" jsonschema:"Email addresses of members to add"`
	RemoveMembers  []string `json:"remove_members,omitempty" jsonschema:"Email addresses of members to remove"`
}

// EnclaveSyncResult is the result of enclave_sync.
type EnclaveSyncResult struct {
	Name    string            `json:"name"`
	Updated []string          `json:"updated"`
	Enclave EnclaveInfoResult `json:"enclave"`
}

// EnclaveDeprovisionParams are the parameters for enclave_deprovision.
type EnclaveDeprovisionParams struct {
	Name string `json:"name" jsonschema:"Name of the enclave to deprovision"`
}

// EnclaveDeprovisionResult is the result of enclave_deprovision.
type EnclaveDeprovisionResult struct {
	Name             string `json:"name"`
	Deleted          bool   `json:"deleted"`
	TentaclesRemoved int    `json:"tentacles_removed"`
}

func registerEnclaveTools(srv *mcp.Server, client *k8s.Client, exoCtrl *exoskeleton.Controller, eval *authz.Evaluator) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "enclave_provision",
		Description: "Provision a new enclave: creates a namespace with enclave annotations, sets up RBAC, resource quotas, and provisions exoskeleton services (Postgres + RustFS).",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Provision Enclave",
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  false,
			OpenWorldHint:   boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, params EnclaveProvisionParams) (*mcp.CallToolResult, EnclaveProvisionResult, error) {
		if err := guard.CheckNamespace(params.Name); err != nil {
			return nil, EnclaveProvisionResult{}, err
		}
		deployer := auth.DeployerFromContext(ctx)
		if err := requireDeployer(deployer, eval); err != nil {
			return nil, EnclaveProvisionResult{}, err
		}
		result, err := handleEnclaveProvision(ctx, client, exoCtrl, params)
		return nil, result, err
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "enclave_info",
		Description: "Get full metadata for an enclave including owner, members, platform binding, lifecycle status, tentacle count, and exoskeleton service status.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Get Enclave Info",
			ReadOnlyHint:    true,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, params EnclaveInfoParams) (*mcp.CallToolResult, EnclaveInfoResult, error) {
		if err := guard.CheckNamespace(params.Name); err != nil {
			return nil, EnclaveInfoResult{}, err
		}
		deployer := auth.DeployerFromContext(ctx)
		if err := requireDeployer(deployer, eval); err != nil {
			return nil, EnclaveInfoResult{}, err
		}
		result, err := handleEnclaveInfo(ctx, client, exoCtrl, params)
		return nil, result, err
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "enclave_list",
		Description: "List all enclaves. Optionally filter to enclaves where a given caller is the owner or a member.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "List Enclaves",
			ReadOnlyHint:    true,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, params EnclaveListParams) (*mcp.CallToolResult, EnclaveListResult, error) {
		deployer := auth.DeployerFromContext(ctx)
		if err := requireDeployer(deployer, eval); err != nil {
			return nil, EnclaveListResult{}, err
		}
		result, err := handleEnclaveList(ctx, client, params)
		return nil, result, err
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "enclave_sync",
		Description: "Update an enclave: add/remove members, transfer ownership, update channel name, or change lifecycle status (freeze/unfreeze).",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Sync Enclave",
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, params EnclaveSyncParams) (*mcp.CallToolResult, EnclaveSyncResult, error) {
		if err := guard.CheckNamespace(params.Name); err != nil {
			return nil, EnclaveSyncResult{}, err
		}
		deployer := auth.DeployerFromContext(ctx)
		if err := requireDeployer(deployer, eval); err != nil {
			return nil, EnclaveSyncResult{}, err
		}
		result, err := handleEnclaveSync(ctx, client, params)
		return nil, result, err
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "enclave_deprovision",
		Description: "Deprovision an enclave: removes all tentacle deployments, cleans up exoskeleton services, and deletes the namespace. Caller must be the enclave owner.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Deprovision Enclave",
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(true),
			IdempotentHint:  false,
			OpenWorldHint:   boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, params EnclaveDeprovisionParams) (*mcp.CallToolResult, EnclaveDeprovisionResult, error) {
		if err := guard.CheckNamespace(params.Name); err != nil {
			return nil, EnclaveDeprovisionResult{}, err
		}
		deployer := auth.DeployerFromContext(ctx)
		if err := requireDeployer(deployer, eval); err != nil {
			return nil, EnclaveDeprovisionResult{}, err
		}
		result, err := handleEnclaveDeprovision(ctx, client, exoCtrl, params, deployer)
		return nil, result, err
	})
}

func handleEnclaveProvision(ctx context.Context, client *k8s.Client, exoCtrl *exoskeleton.Controller, params EnclaveProvisionParams) (EnclaveProvisionResult, error) {
	// Default quota preset.
	preset := params.QuotaPreset
	if preset == "" {
		preset = "medium"
	}

	// Validate members before creating anything.
	allMembers := params.Members
	if allMembers == nil {
		allMembers = []string{}
	}
	if err := authz.ValidateMembers(allMembers); err != nil {
		return EnclaveProvisionResult{}, err
	}

	// Create the namespace using the standard k8s helper (adds managed-by + PSA labels).
	if err := k8s.CreateNamespace(ctx, client, params.Name); err != nil {
		return EnclaveProvisionResult{}, err
	}
	created := []string{"namespace/" + params.Name}

	// Stamp enclave annotations.
	now := time.Now().UTC().Format(time.RFC3339)
	info := authz.EnclaveInfo{
		Enclave:     params.Name,
		Owner:       params.OwnerEmail,
		OwnerSub:    params.OwnerSub,
		Members:     allMembers,
		Platform:    params.Platform,
		ChannelID:   params.ChannelID,
		ChannelName: params.ChannelName,
		Status:      "active",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	enclaveAnnotations := authz.WriteEnclaveAnnotations(info)

	ns, err := client.Clientset.CoreV1().Namespaces().Get(ctx, params.Name, metav1.GetOptions{})
	if err != nil {
		_ = k8s.DeleteNamespace(ctx, client, params.Name)
		return EnclaveProvisionResult{}, fmt.Errorf("namespace created but failed to fetch for annotation: %w", err)
	}
	if ns.Annotations == nil {
		ns.Annotations = map[string]string{}
	}
	for k, v := range enclaveAnnotations {
		ns.Annotations[k] = v
	}
	// Store quota preset in a dedicated annotation for later retrieval by enclave_info.
	ns.Annotations[annotationEnclaveQuotaPreset] = preset
	if _, err := client.Clientset.CoreV1().Namespaces().Update(ctx, ns, metav1.UpdateOptions{}); err != nil {
		_ = k8s.DeleteNamespace(ctx, client, params.Name)
		return EnclaveProvisionResult{}, fmt.Errorf("namespace created but failed to annotate with enclave metadata: %w", err)
	}
	created = append(created, "annotation/enclave-metadata")

	slog.Info("enclave_provision annotated", "enclave", params.Name, "owner", params.OwnerEmail)

	// Standard network policies.
	if err := k8s.CreateDefaultDenyPolicy(ctx, client, params.Name); err != nil {
		return EnclaveProvisionResult{}, fmt.Errorf("namespace created but failed to create default-deny network policy: %w", err)
	}
	created = append(created, "networkpolicy/default-deny")

	if err := k8s.CreateDNSAllowPolicy(ctx, client, params.Name); err != nil {
		return EnclaveProvisionResult{}, fmt.Errorf("namespace created but failed to create allow-dns network policy: %w", err)
	}
	created = append(created, "networkpolicy/allow-dns")

	// Resource quota using the selected preset.
	if err := k8s.CreateResourceQuota(ctx, client, params.Name, preset); err != nil {
		return EnclaveProvisionResult{}, fmt.Errorf("namespace created but failed to create resource quota: %w", err)
	}
	created = append(created, "resourcequota/tentacular-quota")

	if err := k8s.CreateLimitRange(ctx, client, params.Name); err != nil {
		return EnclaveProvisionResult{}, fmt.Errorf("namespace created but failed to create limit range: %w", err)
	}
	created = append(created, "limitrange/tentacular-limits")

	// Workflow RBAC.
	if err := k8s.CreateWorkflowServiceAccount(ctx, client, params.Name); err != nil {
		return EnclaveProvisionResult{}, fmt.Errorf("namespace created but failed to create workflow service account: %w", err)
	}
	created = append(created, "serviceaccount/tentacular-workflow")

	if err := k8s.CreateWorkflowRole(ctx, client, params.Name); err != nil {
		return EnclaveProvisionResult{}, fmt.Errorf("namespace created but failed to create workflow role: %w", err)
	}
	created = append(created, "role/tentacular-workflow")

	if err := k8s.CreateWorkflowRoleBinding(ctx, client, params.Name); err != nil {
		return EnclaveProvisionResult{}, fmt.Errorf("namespace created but failed to create workflow role binding: %w", err)
	}
	created = append(created, "rolebinding/tentacular-workflow")

	// Provision exoskeleton services (Postgres + RustFS) — always for enclaves.
	// If this fails, clean up the namespace and return an error (spec: cleanup on partial failure).
	if exoCtrl != nil {
		if err := exoCtrl.EnsureEnclaveServices(ctx, params.Name, enclaveRequiredServices); err != nil {
			_ = k8s.DeleteNamespace(ctx, client, params.Name)
			return EnclaveProvisionResult{}, fmt.Errorf("enclave provisioned but exoskeleton services failed (namespace deleted): %w", err)
		}
		created = append(created, "exoskeleton/postgres", "exoskeleton/rustfs")
	}

	return EnclaveProvisionResult{
		Name:             params.Name,
		Status:           "active",
		QuotaPreset:      preset,
		Owner:            params.OwnerEmail,
		Members:          allMembers,
		ResourcesCreated: created,
	}, nil
}

func handleEnclaveInfo(ctx context.Context, client *k8s.Client, exoCtrl *exoskeleton.Controller, params EnclaveInfoParams) (EnclaveInfoResult, error) {
	ns, err := client.Clientset.CoreV1().Namespaces().Get(ctx, params.Name, metav1.GetOptions{})
	if err != nil {
		return EnclaveInfoResult{}, fmt.Errorf("get namespace %q: %w", params.Name, err)
	}

	ann := ns.Annotations
	if ann == nil {
		ann = map[string]string{}
	}

	if !authz.IsEnclave(ann) {
		return EnclaveInfoResult{}, fmt.Errorf("namespace %q is not an enclave", params.Name)
	}

	info := authz.ReadEnclaveInfo(ann)

	// Count tentacle deployments.
	deps, err := client.Clientset.AppsV1().Deployments(params.Name).List(ctx, metav1.ListOptions{})
	tentacleCount := 0
	if err == nil {
		tentacleCount = len(deps.Items)
	}

	// Get quota preset from annotations (stored at creation).
	quotaPreset := ann[annotationEnclaveQuotaPreset]

	// Build exo services status.
	exoServices := buildEnclaveExoServices(exoCtrl)

	return EnclaveInfoResult{
		Name:          info.Enclave,
		Owner:         info.Owner,
		OwnerSub:      info.OwnerSub,
		Members:       info.Members,
		Platform:      info.Platform,
		ChannelID:     info.ChannelID,
		ChannelName:   info.ChannelName,
		Status:        info.Status,
		QuotaPreset:   quotaPreset,
		CreatedAt:     info.CreatedAt,
		UpdatedAt:     info.UpdatedAt,
		TentacleCount: tentacleCount,
		ExoServices:   exoServices,
	}, nil
}

func handleEnclaveList(ctx context.Context, client *k8s.Client, params EnclaveListParams) (EnclaveListResult, error) {
	namespaces, err := k8s.ListManagedNamespaces(ctx, client)
	if err != nil {
		return EnclaveListResult{}, err
	}

	items := make([]EnclaveListItem, 0)
	for _, ns := range namespaces {
		ann := ns.Annotations
		if ann == nil {
			ann = map[string]string{}
		}

		// Filter to enclave namespaces only.
		if !authz.IsEnclave(ann) {
			continue
		}

		info := authz.ReadEnclaveInfo(ann)

		// If caller_email filter is set, only include enclaves where caller is owner or member.
		if params.CallerEmail != "" {
			if !isEnclaveParticipant(info, params.CallerEmail) {
				continue
			}
		}

		items = append(items, EnclaveListItem{
			Name:        info.Enclave,
			Owner:       info.Owner,
			Status:      info.Status,
			Members:     info.Members,
			Platform:    info.Platform,
			ChannelName: info.ChannelName,
			CreatedAt:   info.CreatedAt,
		})
	}

	return EnclaveListResult{Enclaves: items}, nil
}

// isEnclaveParticipant returns true if email is the owner or a member of info.
func isEnclaveParticipant(info authz.EnclaveInfo, email string) bool {
	if info.Owner == email {
		return true
	}
	for _, m := range info.Members {
		if m == email {
			return true
		}
	}
	return false
}

func handleEnclaveSync(ctx context.Context, client *k8s.Client, params EnclaveSyncParams) (EnclaveSyncResult, error) {
	ns, err := client.Clientset.CoreV1().Namespaces().Get(ctx, params.Name, metav1.GetOptions{})
	if err != nil {
		return EnclaveSyncResult{}, fmt.Errorf("get namespace %q: %w", params.Name, err)
	}

	ann := ns.Annotations
	if ann == nil {
		ann = map[string]string{}
	}

	if !authz.IsEnclave(ann) {
		return EnclaveSyncResult{}, fmt.Errorf("namespace %q is not an enclave", params.Name)
	}

	info := authz.ReadEnclaveInfo(ann)
	updated := []string{}

	// Handle member additions.
	if len(params.AddMembers) > 0 {
		existing := make(map[string]bool)
		for _, m := range info.Members {
			existing[m] = true
		}
		for _, m := range params.AddMembers {
			if !existing[m] && m != info.Owner {
				info.Members = append(info.Members, m)
				existing[m] = true
			}
		}
		if validateErr := authz.ValidateMembers(info.Members); validateErr != nil {
			return EnclaveSyncResult{}, validateErr
		}
		updated = append(updated, "members")
	}

	// Handle member removals.
	if len(params.RemoveMembers) > 0 {
		removeSet := make(map[string]bool)
		for _, m := range params.RemoveMembers {
			removeSet[m] = true
		}
		filtered := make([]string, 0, len(info.Members))
		for _, m := range info.Members {
			if !removeSet[m] {
				filtered = append(filtered, m)
			}
		}
		info.Members = filtered
		if !containsStr(updated, "members") {
			updated = append(updated, "members")
		}
	}

	// Handle ownership transfer.
	if params.NewOwner != "" {
		if params.NewOwner == info.Owner {
			return EnclaveSyncResult{}, fmt.Errorf("new_owner %q is already the enclave owner", params.NewOwner)
		}
		// New owner must be a current member.
		if !isEnclaveParticipant(info, params.NewOwner) {
			return EnclaveSyncResult{}, fmt.Errorf("new_owner %q must be a current member before ownership transfer", params.NewOwner)
		}
		// Move old owner to members, remove new owner from members.
		newMembers := make([]string, 0, len(info.Members))
		for _, m := range info.Members {
			if m != params.NewOwner {
				newMembers = append(newMembers, m)
			}
		}
		newMembers = append(newMembers, info.Owner)
		info.Owner = params.NewOwner
		info.Members = newMembers
		updated = append(updated, "owner")
	}

	// Handle channel name update.
	if params.NewChannelName != "" {
		info.ChannelName = params.NewChannelName
		updated = append(updated, "channel_name")
	}

	// Handle status change (freeze/unfreeze).
	if params.NewStatus != "" {
		switch params.NewStatus {
		case "active", "frozen":
			info.Status = params.NewStatus
			updated = append(updated, "status")
		default:
			return EnclaveSyncResult{}, fmt.Errorf("invalid status %q; valid values: active, frozen", params.NewStatus)
		}
	}

	if len(updated) == 0 {
		return EnclaveSyncResult{}, errors.New("no updates specified; provide at least one of add_members, remove_members, new_owner, new_channel_name, or new_status")
	}

	// Write updated annotations.
	info.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	newAnnotations := authz.WriteEnclaveAnnotations(info)

	patchAnnotations := make(map[string]any, len(newAnnotations))
	for k, v := range newAnnotations {
		patchAnnotations[k] = v
	}

	patchBody, err := json.Marshal(map[string]any{
		"metadata": map[string]any{"annotations": patchAnnotations},
	})
	if err != nil {
		return EnclaveSyncResult{}, fmt.Errorf("marshal enclave sync patch: %w", err)
	}

	if _, err := client.Clientset.CoreV1().Namespaces().Patch(
		ctx, params.Name, types.MergePatchType, patchBody, metav1.PatchOptions{},
	); err != nil {
		return EnclaveSyncResult{}, fmt.Errorf("patch namespace %q enclave annotations: %w", params.Name, err)
	}

	slog.Info("enclave_sync applied", "enclave", params.Name, "updated", updated)

	// Build the updated info result.
	infoResult := EnclaveInfoResult{
		Name:        info.Enclave,
		Owner:       info.Owner,
		OwnerSub:    info.OwnerSub,
		Members:     info.Members,
		Platform:    info.Platform,
		ChannelID:   info.ChannelID,
		ChannelName: info.ChannelName,
		Status:      info.Status,
		CreatedAt:   info.CreatedAt,
		UpdatedAt:   info.UpdatedAt,
	}

	return EnclaveSyncResult{
		Name:    params.Name,
		Updated: updated,
		Enclave: infoResult,
	}, nil
}

func handleEnclaveDeprovision(ctx context.Context, client *k8s.Client, exoCtrl *exoskeleton.Controller, params EnclaveDeprovisionParams, deployer *exoskeleton.DeployerInfo) (EnclaveDeprovisionResult, error) {
	ns, err := client.Clientset.CoreV1().Namespaces().Get(ctx, params.Name, metav1.GetOptions{})
	if err != nil {
		return EnclaveDeprovisionResult{}, fmt.Errorf("get namespace %q: %w", params.Name, err)
	}

	ann := ns.Annotations
	if ann == nil {
		ann = map[string]string{}
	}

	if !authz.IsEnclave(ann) {
		return EnclaveDeprovisionResult{}, fmt.Errorf("namespace %q is not an enclave", params.Name)
	}

	info := authz.ReadEnclaveInfo(ann)

	// Caller must be the enclave owner (when deployer info is available).
	if deployer != nil && deployer.Provider != "bearer-token" && deployer.Email != "" {
		if deployer.Email != info.Owner {
			return EnclaveDeprovisionResult{}, fmt.Errorf("permission denied: only the enclave owner (%s) may deprovision enclave %q", info.Owner, params.Name)
		}
	}

	// Remove all tentacle deployments.
	deps, err := client.Clientset.AppsV1().Deployments(params.Name).List(ctx, metav1.ListOptions{})
	tentaclesRemoved := 0
	if err == nil {
		for _, dep := range deps.Items {
			if delErr := client.Clientset.AppsV1().Deployments(params.Name).Delete(ctx, dep.Name, metav1.DeleteOptions{}); delErr != nil {
				slog.Warn("enclave_deprovision: failed to delete deployment", "enclave", params.Name, "deployment", dep.Name, "error", delErr)
			} else {
				tentaclesRemoved++
			}
		}
	}

	// Clean up exoskeleton services for each tentacle (best-effort).
	if exoCtrl != nil && err == nil {
		for _, dep := range deps.Items {
			if cleanErr := exoCtrl.Cleanup(ctx, params.Name, dep.Name); cleanErr != nil {
				slog.Warn("enclave_deprovision: exoskeleton cleanup failed for tentacle", "enclave", params.Name, "tentacle", dep.Name, "error", cleanErr)
			}
		}
	}

	// Delete the namespace (cascades to all remaining resources).
	if err := k8s.DeleteNamespace(ctx, client, params.Name); err != nil {
		return EnclaveDeprovisionResult{}, err
	}

	slog.Info("enclave_deprovision complete", "enclave", params.Name, "tentacles_removed", tentaclesRemoved)

	return EnclaveDeprovisionResult{
		Name:             params.Name,
		Deleted:          true,
		TentaclesRemoved: tentaclesRemoved,
	}, nil
}

// buildEnclaveExoServices returns the list of exoskeleton services provisioned for enclaves.
func buildEnclaveExoServices(exoCtrl *exoskeleton.Controller) []EnclaveExoService {
	if exoCtrl == nil {
		return []EnclaveExoService{
			{Name: "postgres", Available: false},
			{Name: "rustfs", Available: false},
		}
	}
	return []EnclaveExoService{
		{Name: "postgres", Available: exoCtrl.PostgresAvailable()},
		{Name: "rustfs", Available: exoCtrl.RustFSAvailable()},
	}
}

// containsStr returns true if s is in the slice.
func containsStr(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
