## ADDED Requirements

### Requirement: CheckAccess evaluates permission for a caller against a tentacle
The system SHALL provide a CheckAccess function that takes caller identity (sub, email, groups), tentacle authz metadata (owner-sub, group, mode), and required permission (read/write/execute), and returns allow/deny.

#### Scenario: Owner has owner-scope permissions
- **WHEN** the caller's sub matches the tentacle's owner-sub
- **THEN** the owner permission bits SHALL be evaluated

#### Scenario: Group member has group-scope permissions
- **WHEN** the caller's sub does not match owner-sub but the caller's groups include the tentacle's group
- **THEN** the group permission bits SHALL be evaluated

#### Scenario: Others get others-scope permissions
- **WHEN** the caller's sub does not match owner-sub and the caller's groups do not include the tentacle's group
- **THEN** the others permission bits SHALL be evaluated

#### Scenario: Permission denied returns error
- **WHEN** the evaluated permission bits do not include the required permission
- **THEN** CheckAccess SHALL return a permission-denied error with the caller's identity, the tentacle name, and the required permission

### Requirement: Bearer-token requests bypass authz
The system SHALL skip authz evaluation entirely for requests authenticated via bearer token. Bearer-token is identified by DeployerInfo.Provider == "bearer-token" (set in DualAuthMiddleware bearer path at auth.go:137).

#### Scenario: Bearer token always allowed
- **WHEN** a request is authenticated via bearer token (DeployerInfo.Provider == "bearer-token")
- **THEN** the system SHALL allow the operation without checking permissions

### Requirement: Nil DeployerInfo bypasses authz
The system SHALL allow operations when DeployerFromContext returns nil (no auth info attached to context).

#### Scenario: No deployer info allows operation
- **WHEN** DeployerFromContext returns nil
- **THEN** the system SHALL allow the operation

### Requirement: Pre-authz resources bypass authz
Existing deployments without a `tentacular.io/owner-sub` annotation are pre-authz resources and SHALL be allowed for all operations. This provides backwards compatibility for tentacles deployed before the authz feature.

#### Scenario: Missing owner-sub allows all operations
- **WHEN** a Deployment has no `tentacular.io/owner-sub` annotation
- **THEN** the system SHALL allow the operation regardless of caller identity

### Requirement: Missing authz annotations use defaults
The system SHALL apply default-mode and default-group from server configuration when a tentacle has mode or group annotation missing but DOES have owner-sub (post-authz resource).

#### Scenario: No mode annotation uses default
- **WHEN** a tentacle has `tentacular.io/owner-sub` but no `tentacular.io/mode` annotation
- **THEN** the system SHALL use the server-configured default mode for evaluation

#### Scenario: No group annotation uses default
- **WHEN** a tentacle has `tentacular.io/owner-sub` but no `tentacular.io/group` annotation
- **THEN** the system SHALL use the server-configured default group for evaluation

### Requirement: Default mode is configurable via server config
The default mode SHALL be configurable via environment variable or Helm value (authz.defaultMode). The default SHALL be 0750 (Team preset).

#### Scenario: Default mode from env var
- **WHEN** the AUTHZ_DEFAULT_MODE environment variable is set to "0700"
- **THEN** the evaluator SHALL use 0700 as the default mode for tentacles without a mode annotation
