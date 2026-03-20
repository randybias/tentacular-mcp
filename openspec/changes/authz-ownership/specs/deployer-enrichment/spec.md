## ADDED Requirements

### Requirement: DeployerInfo struct includes Groups
The DeployerInfo struct (exoskeleton/auth.go:22-29) SHALL include a `Groups []string` field populated from OIDC group claims.

#### Scenario: OIDC token with groups claim
- **WHEN** an OIDC token contains a groups claim (configurable claim name, default "groups")
- **THEN** DeployerInfo.Groups SHALL be populated with the group names

#### Scenario: Bearer token has empty groups
- **WHEN** a bearer token is used for authentication (Provider == "bearer-token")
- **THEN** DeployerInfo.Groups SHALL be nil/empty

### Requirement: keycloakClaims extracts groups
The keycloakClaims struct (exoskeleton/auth.go:75-81) SHALL be extended to extract groups from the JWT. The claim name SHALL be configurable to support different IdP configurations (Keycloak uses "groups", Google uses "groups", others may vary).

#### Scenario: Groups extracted from JWT
- **WHEN** ValidateToken parses a JWT containing a "groups" claim with ["platform-team", "devs"]
- **THEN** the resulting DeployerInfo.Groups SHALL be ["platform-team", "devs"]

#### Scenario: Missing groups claim results in empty groups
- **WHEN** ValidateToken parses a JWT without a groups claim
- **THEN** DeployerInfo.Groups SHALL be nil/empty (not an error)

### Requirement: AnnotateDeployer stamps authz annotations on create
The AnnotateDeployer function (enrich.go:269-295) SHALL stamp authz annotations on new deployments. This extends the existing annotation stamping (deployed-by, deployed-via, deployed-at).

#### Scenario: New deployment gets full authz annotations
- **WHEN** a new tentacle is deployed via wf_apply with OIDC authentication
- **THEN** the deployment SHALL have `tentacular.io/owner-sub` set from DeployerInfo.Subject
- **THEN** the deployment SHALL have `tentacular.io/owner-email` set from DeployerInfo.Email
- **THEN** the deployment SHALL have `tentacular.io/owner-name` set from DeployerInfo.DisplayName
- **THEN** the deployment SHALL have `tentacular.io/group` set to the requested group or server default-group
- **THEN** the deployment SHALL have `tentacular.io/mode` set to the requested mode or server default-mode
- **THEN** the deployment SHALL have `tentacular.io/created-at` set to the current timestamp

### Requirement: AnnotateDeployer stamps updated-by on update
On update path (existing deployment), AnnotateDeployer SHALL stamp updated-by fields without changing ownership or permissions.

#### Scenario: Update deployment stamps updated-by
- **WHEN** an existing tentacle is updated via wf_apply with OIDC authentication
- **THEN** the deployment SHALL have `tentacular.io/updated-at` set to the current timestamp
- **THEN** the deployment SHALL have `tentacular.io/updated-by-sub` set from DeployerInfo.Subject
- **THEN** the deployment SHALL have `tentacular.io/updated-by-email` set from DeployerInfo.Email
- **THEN** the deployment SHALL NOT change owner-sub, owner-email, owner-name, group, or mode

### Requirement: Deploy with group parameter overrides default group
When the wf_apply request includes a group parameter, the system SHALL use that group instead of the server default.

#### Scenario: Explicit group at deploy time
- **WHEN** a tentacle is deployed with group parameter set to "platform-team"
- **THEN** the `tentacular.io/group` annotation SHALL be set to "platform-team" instead of the default

### Requirement: Deploy with share flag sets Team preset mode
When the wf_apply request includes share=true, the system SHALL set mode to the Team preset (0750).

#### Scenario: Share flag sets team-readable mode
- **WHEN** a tentacle is deployed with share=true
- **THEN** the `tentacular.io/mode` annotation SHALL be set to "0750"
