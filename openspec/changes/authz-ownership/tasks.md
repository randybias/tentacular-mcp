## 1. Core Authz Package

- [ ] 1.1 Create `pkg/authz/mode.go`: Mode type (uint16), permission constants (Read=4/Write=2/Execute=1), bit position constants (Owner/Group/Others), HasPermission method, ParseMode function
- [ ] 1.2 Create `pkg/authz/presets.go`: Named presets (Private=0700, Team=0750, Shared=0755), preset lookup by name
- [ ] 1.3 Create `pkg/authz/annotations.go`: Annotation key constants under `tentacular.io/`, ReadAnnotations helper (with tentacular.dev fallback), WriteAnnotations helper (new prefix only), migration fallback logic
- [ ] 1.4 Create `pkg/authz/evaluator.go`: CheckAccess function (caller identity + tentacle metadata + required permission -> allow/deny), scope determination (owner/group/others), bearer-token bypass (Provider=="bearer-token"), nil DeployerInfo bypass, pre-authz resource bypass (missing owner-sub), default-mode/default-group fallback
- [ ] 1.5 Create `pkg/authz/mode_test.go`: Tests for Mode parsing, HasPermission, bit arithmetic
- [ ] 1.6 Create `pkg/authz/evaluator_test.go`: Tests for CheckAccess (owner allowed, group allowed, others denied, bearer bypass, nil deployer bypass, pre-authz bypass, defaults)
- [ ] 1.7 Create `pkg/authz/annotations_test.go`: Tests for annotation read/write, migration fallback, old-key-only reads, new-key-preferred reads

## 2. Annotation Migration

- [ ] 2.1 Update `pkg/k8s/discover.go`: Use new annotation reader with fallback for environment, tags
- [ ] 2.2 Update `pkg/k8s/scheduler.go`: Use new annotation reader with fallback for cron-schedule
- [ ] 2.3 Update annotation constants throughout codebase from `tentacular.dev/*` to `tentacular.io/*`

## 3. DeployerInfo Extension

- [ ] 3.1 Add `Groups []string` field to DeployerInfo struct (exoskeleton/auth.go:22-29)
- [ ] 3.2 Add `Groups []string` to keycloakClaims struct (exoskeleton/auth.go:75-81), extract in ValidateToken
- [ ] 3.3 Make group claim name configurable (default: "groups") via server config
- [ ] 3.4 Update AnnotateDeployer in `pkg/k8s/enrich.go:269-295`: stamp owner-sub, owner-email, owner-name, group, mode, created-at on create; stamp updated-at, updated-by-sub, updated-by-email on update

## 4. Authz Tool Integration

- [ ] 4.1 Create `pkg/tools/authz_helpers.go`: Shared helper that extracts DeployerInfo via auth.DeployerFromContext, reads authz annotations from Deployment, calls CheckAccess. Separate from guard package.
- [ ] 4.2 Wire authz into deploy.go: read check for wf_status (line 256); write check for wf_apply update path in handleWorkflowApply (between Get at line 457 and Update at line 466); add DeployerFromContext to wf_remove (line 224) with write check
- [ ] 4.3 Wire authz into discover.go: wf_list filter (filter entries in handleWfList at line 147 by read permission, hide non-readable entirely); read check for wf_describe (line 129)
- [ ] 4.4 Wire authz read check into wf_health.go (wf_health, wf_health_ns)
- [ ] 4.5 Wire authz into run.go: add DeployerFromContext to wf_run (line 48) with execute check
- [ ] 4.6 Wire authz into workflow.go: write check for wf_restart (line 213)
- [ ] 4.7 Skip authz for namespace-scoped tools: wf_pods, wf_logs, wf_events, wf_jobs (rely on existing namespace guard)
- [ ] 4.8 Skip authz for new deploys (wf_apply create path, apierrors.IsNotFound) and stamp ownership via AnnotateDeployer

## 5. Permissions Tools

- [ ] 5.1 Create `pkg/tools/permissions.go`: permissions_get tool (read authz annotations, requires read permission)
- [ ] 5.2 Add permissions_set tool (update mode/group/owner annotations, owner-only for OIDC, bearer bypass, stamps updated-at/updated-by)
- [ ] 5.3 Register permissions_get and permissions_set in `pkg/tools/register.go`

## 6. WfApply Extension

- [ ] 6.1 Add group parameter to WfApplyParams
- [ ] 6.2 Add share parameter to WfApplyParams (sets mode to Team preset 0750)
- [ ] 6.3 Pass group/share through to AnnotateDeployer

## 7. Configuration

- [ ] 7.1 Add authz config fields to server config: defaultMode (default "0750"), defaultGroup, groupClaimName (default "groups")
- [ ] 7.2 Update Helm chart values.yaml with authz defaults (authz.defaultMode, authz.defaultGroup, authz.groupClaimName)
- [ ] 7.3 Wire config into evaluator construction at server startup
