package tools

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/randybias/tentacular-mcp/pkg/auth"
	"github.com/randybias/tentacular-mcp/pkg/authz"
	"github.com/randybias/tentacular-mcp/pkg/exoskeleton"
	"github.com/randybias/tentacular-mcp/pkg/guard"
	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

// validQuotaPresets is the set of recognized quota preset names for enclave provisioning.
var validQuotaPresets = map[string]bool{
	"small":  true,
	"medium": true,
	"large":  true,
}

// annotationEnclaveQuotaPreset stores the quota preset on the enclave namespace annotation.
const annotationEnclaveQuotaPreset = "tentacular.io/enclave-quota-preset"

// enclaveRequiredServices lists the exoskeleton services always provisioned for enclaves.
var enclaveRequiredServices = []string{"postgres", "rustfs"}

// MaxEnclavesPerOwner is the maximum number of enclaves a single owner can provision.
// Package-level var so it can be overridden by server config at startup.
var MaxEnclavesPerOwner = 10

// EnclaveProvisionParams are the parameters for enclave_provision.
type EnclaveProvisionParams struct {
	Name        string   `json:"name" jsonschema:"Name of the enclave (becomes the namespace name)"`
	OwnerEmail  string   `json:"owner_email" jsonschema:"Email address of the enclave owner"`
	OwnerSub    string   `json:"owner_sub" jsonschema:"OIDC subject of the enclave owner"`
	Platform    string   `json:"platform,omitempty" jsonschema:"Platform binding (e.g. slack)"`
	ChannelID   string   `json:"channel_id,omitempty" jsonschema:"Platform channel ID"`
	ChannelName string   `json:"channel_name,omitempty" jsonschema:"Platform channel name"`
	QuotaPreset string   `json:"quota_preset,omitempty" jsonschema:"Resource quota preset: small, medium, or large (default: medium)"`
	DefaultMode string   `json:"default_mode,omitempty" jsonschema:"Default permission mode for new tentacles (9-char rwx string, e.g. rwxrwx---)"`
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
		// C2: For OIDC callers, force owner identity to the caller's own credentials.
		// Bearer-token callers (platform operators) may specify arbitrary owner.
		if deployer != nil && deployer.Provider != "bearer-token" {
			if deployer.Email == "" {
				return nil, EnclaveProvisionResult{}, errors.New("permission denied: OIDC caller has no email claim; cannot provision enclave")
			}
			params.OwnerEmail = deployer.Email
			params.OwnerSub = deployer.Subject
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
		result, err := handleEnclaveInfo(ctx, client, exoCtrl, eval, params, deployer)
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
		// H3: For OIDC callers, force the caller_email filter to their own identity.
		// This prevents OIDC users from listing all enclaves by omitting the filter.
		if deployer != nil && deployer.Provider != "bearer-token" {
			params.CallerEmail = deployer.Email
		}
		result, err := handleEnclaveList(ctx, client, eval, params, deployer)
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
		result, err := handleEnclaveSync(ctx, client, params, deployer)
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

	// M3: Validate quota preset before any resource creation.
	if !validQuotaPresets[preset] {
		return EnclaveProvisionResult{}, fmt.Errorf("invalid quota_preset %q; valid values: small, medium, large", preset)
	}

	// Validate members before creating anything.
	allMembers := params.Members
	if allMembers == nil {
		allMembers = []string{}
	}
	if err := authz.ValidateMembers(allMembers); err != nil {
		return EnclaveProvisionResult{}, err
	}

	// Rate limit: count existing enclaves owned by this email.
	existingCount, countErr := countEnclavesOwnedBy(ctx, client, strings.ToLower(params.OwnerEmail))
	if countErr != nil {
		return EnclaveProvisionResult{}, fmt.Errorf("checking enclave count: %w", countErr)
	}
	if existingCount >= MaxEnclavesPerOwner {
		return EnclaveProvisionResult{}, fmt.Errorf("owner %q already has %d enclaves (limit: %d)", params.OwnerEmail, existingCount, MaxEnclavesPerOwner)
	}

	// Create the namespace using the standard k8s helper (adds managed-by + PSA labels).
	if err := k8s.CreateNamespace(ctx, client, params.Name); err != nil {
		return EnclaveProvisionResult{}, err
	}
	created := []string{"namespace/" + params.Name}

	// Deferred cleanup — if any step after namespace creation fails, delete the namespace
	// and clean up any partially-provisioned exoskeleton services.
	nsCleanupNeeded := true
	exoProvisioned := false
	defer func() {
		if nsCleanupNeeded {
			if exoProvisioned && exoCtrl != nil {
				if cleanErr := exoCtrl.CleanupEnclave(ctx, params.Name); cleanErr != nil {
					slog.Warn("enclave_provision: exo cleanup during rollback failed", "enclave", params.Name, "error", cleanErr)
				}
			}
			_ = k8s.DeleteNamespace(ctx, client, params.Name)
		}
	}()

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
		DefaultMode: params.DefaultMode,
		Status:      "active",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := authz.ValidateEnclaveInfo(info); err != nil {
		return EnclaveProvisionResult{}, fmt.Errorf("invalid enclave info: %w", err)
	}

	enclaveAnnotations := authz.WriteEnclaveAnnotations(info)

	ns, err := client.Clientset.CoreV1().Namespaces().Get(ctx, params.Name, metav1.GetOptions{})
	if err != nil {
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
	// If this fails, the deferred cleanup will delete the namespace.
	if exoCtrl != nil {
		if err := exoCtrl.EnsureEnclaveServices(ctx, params.Name, enclaveRequiredServices); err != nil {
			exoProvisioned = true // mark for cleanup in deferred rollback
			return EnclaveProvisionResult{}, fmt.Errorf("enclave provisioned but exoskeleton services failed (namespace deleted): %w", err)
		}
		exoProvisioned = true
		created = append(created, "exoskeleton/postgres", "exoskeleton/rustfs")
	}

	// All steps succeeded — disable the deferred cleanup.
	nsCleanupNeeded = false

	return EnclaveProvisionResult{
		Name:             params.Name,
		Status:           "active",
		QuotaPreset:      preset,
		Owner:            params.OwnerEmail,
		Members:          allMembers,
		ResourcesCreated: created,
	}, nil
}

func handleEnclaveInfo(ctx context.Context, client *k8s.Client, exoCtrl *exoskeleton.Controller, eval *authz.Evaluator, params EnclaveInfoParams, deployer *exoskeleton.DeployerInfo) (EnclaveInfoResult, error) {
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

	// Only allow viewing enclave details if the caller is owner/member OR has
	// read access via mode bits (e.g., "other" read on open-read/open-run enclaves).
	if deployer != nil && deployer.Provider != "bearer-token" {
		if !isEnclaveParticipant(info, deployer.Email) {
			if d := eval.CheckEnclave(deployer, ann, authz.Read); !d.Allowed {
				return EnclaveInfoResult{}, fmt.Errorf("permission denied: caller is not the enclave owner or a member of %q", params.Name)
			}
		}
	}

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

func handleEnclaveList(ctx context.Context, client *k8s.Client, eval *authz.Evaluator, params EnclaveListParams, deployer *exoskeleton.DeployerInfo) (EnclaveListResult, error) {
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

		// For OIDC callers, include enclaves where the caller is owner/member
		// OR where the enclave's mode grants "other" read access (open-read/open-run presets).
		if params.CallerEmail != "" {
			if !isEnclaveParticipant(info, params.CallerEmail) {
				// Check if the enclave mode grants read to "other" via the evaluator.
				if d := eval.CheckEnclave(deployer, ann, authz.Read); !d.Allowed {
					continue
				}
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
// Email comparison is case-insensitive. info.Members are already lowercased by
// ParseMembers, but info.Owner and the email parameter need normalization.
func isEnclaveParticipant(info authz.EnclaveInfo, email string) bool {
	emailLower := strings.ToLower(email)
	if strings.ToLower(info.Owner) == emailLower {
		return true
	}
	for _, m := range info.Members {
		if m == emailLower {
			return true
		}
	}
	return false
}

// maxSyncRetries is the maximum number of retries for optimistic concurrency conflicts in enclave_sync.
const maxSyncRetries = 3

func handleEnclaveSync(ctx context.Context, client *k8s.Client, params EnclaveSyncParams, deployer *exoskeleton.DeployerInfo) (EnclaveSyncResult, error) {
	for attempt := range maxSyncRetries {
		result, conflict, err := attemptEnclaveSync(ctx, client, params, deployer)
		if err != nil && conflict {
			slog.Info("enclave_sync conflict, retrying", "enclave", params.Name, "attempt", attempt+1)
			continue
		}
		return result, err
	}
	return EnclaveSyncResult{}, fmt.Errorf("enclave_sync: conflict persisted after %d retries for %q", maxSyncRetries, params.Name)
}

// attemptEnclaveSync performs a single read-modify-write cycle for enclave_sync.
// Returns (result, isConflict, error). When isConflict is true the caller should retry.
func attemptEnclaveSync(ctx context.Context, client *k8s.Client, params EnclaveSyncParams, deployer *exoskeleton.DeployerInfo) (EnclaveSyncResult, bool, error) {
	ns, err := client.Clientset.CoreV1().Namespaces().Get(ctx, params.Name, metav1.GetOptions{})
	if err != nil {
		return EnclaveSyncResult{}, false, fmt.Errorf("get namespace %q: %w", params.Name, err)
	}

	ann := ns.Annotations
	if ann == nil {
		ann = map[string]string{}
	}

	if !authz.IsEnclave(ann) {
		return EnclaveSyncResult{}, false, fmt.Errorf("namespace %q is not an enclave", params.Name)
	}

	info := authz.ReadEnclaveInfo(ann)

	// Ownership transfer, member management, and freeze/unfreeze are owner-only.
	// Bearer-token callers bypass this check (platform operators).
	isOwnerOp := params.NewOwner != "" || len(params.AddMembers) > 0 || len(params.RemoveMembers) > 0 || params.NewStatus != ""
	if isOwnerOp && deployer != nil && deployer.Provider != "bearer-token" {
		if deployer.Email == "" {
			return EnclaveSyncResult{}, false, errors.New("permission denied: OIDC caller has no email claim")
		}
		if deployer.Email != info.Owner {
			return EnclaveSyncResult{}, false, fmt.Errorf("permission denied: only the enclave owner (%s) may modify enclave %q", info.Owner, params.Name)
		}
	}

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
			return EnclaveSyncResult{}, false, validateErr
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
			return EnclaveSyncResult{}, false, fmt.Errorf("new_owner %q is already the enclave owner", params.NewOwner)
		}
		// New owner must be a current member.
		if !isEnclaveParticipant(info, params.NewOwner) {
			return EnclaveSyncResult{}, false, fmt.Errorf("new_owner %q must be a current member before ownership transfer", params.NewOwner)
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
		// M6: We don't know the new owner's OIDC subject at sync time.
		// Clear OwnerSub; it will be re-populated on the new owner's next
		// OIDC-authenticated action that stamps deployer identity.
		info.OwnerSub = ""
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
			return EnclaveSyncResult{}, false, fmt.Errorf("invalid status %q; valid values: active, frozen", params.NewStatus)
		}
	}

	if len(updated) == 0 {
		return EnclaveSyncResult{}, false, errors.New("no updates specified; provide at least one of add_members, remove_members, new_owner, new_channel_name, or new_status")
	}

	// Write updated annotations.
	info.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	newAnnotations := authz.WriteEnclaveAnnotations(info)

	for k, v := range newAnnotations {
		ns.Annotations[k] = v
	}

	// Use Update with the namespace's ResourceVersion for optimistic concurrency.
	// If another write happened between our GET and this UPDATE, the API server
	// returns a Conflict error and we retry from scratch.
	if _, err := client.Clientset.CoreV1().Namespaces().Update(ctx, ns, metav1.UpdateOptions{}); err != nil {
		if k8serrors.IsConflict(err) {
			return EnclaveSyncResult{}, true, err
		}
		return EnclaveSyncResult{}, false, fmt.Errorf("update namespace %q enclave annotations: %w", params.Name, err)
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
	}, false, nil
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

	// H1: Caller must be the enclave owner (when deployer info is available).
	// Bearer-token callers bypass the check (platform operators).
	// OIDC callers with no email are denied — fail closed, never fail open.
	if deployer != nil && deployer.Provider != "bearer-token" {
		if deployer.Email == "" {
			return EnclaveDeprovisionResult{}, errors.New("permission denied: OIDC caller has no email claim; cannot verify enclave ownership")
		}
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

	// Clean up enclave-level exoskeleton resources (database, bucket, NATS account).
	if exoCtrl != nil {
		if cleanErr := exoCtrl.CleanupEnclave(ctx, params.Name); cleanErr != nil {
			slog.Warn("enclave_deprovision: enclave-level exoskeleton cleanup failed", "enclave", params.Name, "error", cleanErr)
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

// countEnclavesOwnedBy counts the number of enclave namespaces owned by the given email.
func countEnclavesOwnedBy(ctx context.Context, client *k8s.Client, ownerEmail string) (int, error) {
	namespaces, err := k8s.ListManagedNamespaces(ctx, client)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, ns := range namespaces {
		ann := ns.Annotations
		if ann == nil {
			continue
		}
		if !authz.IsEnclave(ann) {
			continue
		}
		info := authz.ReadEnclaveInfo(ann)
		if strings.ToLower(info.Owner) == ownerEmail {
			count++
		}
	}
	return count, nil
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
