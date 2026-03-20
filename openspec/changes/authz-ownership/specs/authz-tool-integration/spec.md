## ADDED Requirements

### Requirement: Workflow-scoped read tools check read permission
The tools wf_status, wf_describe, wf_health, and wf_health_ns SHALL check read permission before returning data. These tools operate on a specific workflow Deployment and can read its authz annotations.

#### Scenario: Unauthorized read returns permission error
- **WHEN** an OIDC-authenticated user calls wf_status on a tentacle they have no read access to
- **THEN** the tool SHALL return a permission-denied error

#### Scenario: Authorized read succeeds
- **WHEN** an OIDC-authenticated user who is the owner calls wf_status on their tentacle with mode 0700
- **THEN** the tool SHALL return the status normally

### Requirement: wf_list filters results by read permission
The wf_list tool SHALL filter its result set, hiding tentacles the caller has no read access to. It SHALL NOT deny the entire request.

#### Scenario: List shows only readable tentacles
- **WHEN** an OIDC-authenticated user calls wf_list in a namespace with 5 tentacles, 3 of which they can read
- **THEN** the tool SHALL return only the 3 readable tentacles (hidden entirely, not redacted)

### Requirement: Namespace-scoped tools skip per-workflow authz
The tools wf_pods, wf_logs, wf_events, and wf_jobs operate at namespace scope and SHALL NOT perform per-workflow authz checks. Access is governed by the existing namespace guard.

#### Scenario: Namespace-scoped tool allowed without workflow authz
- **WHEN** an authenticated user calls wf_logs for a tentacle in a namespace they have access to
- **THEN** the tool SHALL return logs without checking workflow-level permissions

### Requirement: All write-path tools check write permission
The tools wf_apply (update path), wf_remove, and wf_restart SHALL check write permission. The authz check SHALL occur after fetching the existing Deployment (to read annotations) and before performing the mutation.

#### Scenario: Unauthorized update returns permission error
- **WHEN** an OIDC-authenticated user calls wf_apply to update a tentacle they have no write access to
- **THEN** the tool SHALL return a permission-denied error

#### Scenario: wf_remove checks write permission
- **WHEN** an OIDC-authenticated user calls wf_remove on a tentacle they do not own and mode does not grant group/others write
- **THEN** the tool SHALL return a permission-denied error

#### Scenario: wf_restart checks write permission
- **WHEN** an OIDC-authenticated user calls wf_restart on a tentacle they have no write access to
- **THEN** the tool SHALL return a permission-denied error

### Requirement: Execute-path tools check execute permission
The tools wf_run SHALL check execute permission.

#### Scenario: Unauthorized run returns permission error
- **WHEN** an OIDC-authenticated user calls wf_run on a tentacle they have no execute access to
- **THEN** the tool SHALL return a permission-denied error

### Requirement: New deploys (wf_apply create path) skip authz check
When deploying a new tentacle (apierrors.IsNotFound on existing Deployment), the system SHALL allow the deploy and stamp the deployer as owner. The create vs update path is determined in handleWorkflowApply.

#### Scenario: First deploy sets ownership
- **WHEN** an OIDC-authenticated user calls wf_apply for a tentacle that does not yet exist
- **THEN** the system SHALL allow the deploy and set owner-sub, owner-email, owner-name from the deployer identity

### Requirement: Pre-authz resources are allowed by default
Existing deployments without a `tentacular.io/owner-sub` annotation SHALL be treated as pre-authz and all operations SHALL be allowed (backwards compatibility).

#### Scenario: Legacy tentacle without authz annotations
- **WHEN** an OIDC-authenticated user operates on a tentacle that has no `tentacular.io/owner-sub` annotation
- **THEN** the system SHALL allow the operation

### Requirement: Authz checks run after guard checks, before business logic
The authz helper SHALL be called in each handler after existing guard checks (CheckNamespace, CheckName) and before the business logic execution.

#### Scenario: Handler ordering
- **WHEN** a tool handler processes a request
- **THEN** the handler SHALL run guard checks first, then authz check, then business logic

### Requirement: Shared authz helper simplifies handler integration
A shared helper function SHALL accept the handler context, Deployment object, and required permission, and return nil (allowed) or a permission-denied error. The helper is separate from the guard package (different concern: guard=safety, authz=permissions).

#### Scenario: Helper extracts deployer and evaluates
- **WHEN** a tool handler calls the authz helper with a Deployment
- **THEN** the helper SHALL extract DeployerInfo from the request context, read authz annotations from the Deployment, and call CheckAccess
