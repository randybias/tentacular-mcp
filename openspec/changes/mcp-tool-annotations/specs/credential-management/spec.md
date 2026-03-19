## MODIFIED Requirements

### Requirement: Issue short-lived ServiceAccount token
The system SHALL issue a time-bound token for the `tentacular-workflow` ServiceAccount in a given namespace using the Kubernetes TokenRequest API. The TTL SHALL be specified in minutes and MUST be between 10 and 1440 (24 hours). The system SHALL reject the operation if the target namespace is `tentacular-system`. The `cred_issue_token` tool registration SHALL include `ToolAnnotations` with `ReadOnlyHint: false`, `DestructiveHint: ptr(false)`, `IdempotentHint: false`, `OpenWorldHint: ptr(false)`, and `Title: "Issue service account token"`.

#### Scenario: Issue token with valid TTL
- **WHEN** the `cred_issue_token` tool is called with `namespace: "dev-alice"` and `ttl_minutes: 60`
- **THEN** the system returns a JWT token string valid for 60 minutes scoped to the workflow ServiceAccount in `dev-alice`

#### Scenario: Reject TTL out of range
- **WHEN** the `cred_issue_token` tool is called with `ttl_minutes: 5`
- **THEN** the system returns an error indicating the TTL must be between 10 and 1440 minutes

#### Scenario: Reject protected namespace
- **WHEN** the `cred_issue_token` tool is called with `namespace: "tentacular-system"`
- **THEN** the system returns an error indicating operations on `tentacular-system` are not allowed

#### Scenario: cred_issue_token tool annotations are non-destructive non-idempotent write
- **WHEN** the `cred_issue_token` tool is retrieved from the server's tool list
- **THEN** `annotations.readOnlyHint` is `false`, `annotations.destructiveHint` is `false`, and `annotations.idempotentHint` is `false`

### Requirement: Generate scoped kubeconfig
The system SHALL generate a complete kubeconfig YAML string containing the cluster CA, API server URL, issued token, and target namespace. The kubeconfig SHALL use context name `tentacular` and user name `tentacular-workflow`. The system SHALL call the token issuance internally with the specified TTL. The `cred_kubeconfig` tool registration SHALL include `ToolAnnotations` with `ReadOnlyHint: false`, `DestructiveHint: ptr(false)`, `IdempotentHint: true`, `OpenWorldHint: ptr(false)`, and `Title: "Generate scoped kubeconfig"`.

#### Scenario: Generate kubeconfig
- **WHEN** the `cred_kubeconfig` tool is called with `namespace: "dev-alice"` and `ttl_minutes: 120`
- **THEN** the system returns a valid kubeconfig YAML with the cluster endpoint, CA data, a token valid for 120 minutes, and the namespace set to `dev-alice`

#### Scenario: cred_kubeconfig tool annotations are non-destructive idempotent write
- **WHEN** the `cred_kubeconfig` tool is retrieved from the server's tool list
- **THEN** `annotations.readOnlyHint` is `false`, `annotations.destructiveHint` is `false`, and `annotations.idempotentHint` is `true`

### Requirement: Rotate credentials
The system SHALL rotate credentials for a namespace by deleting and recreating the `tentacular-workflow` ServiceAccount, which invalidates all previously issued tokens. The system SHALL reject the operation if the target namespace is `tentacular-system`. The `cred_rotate` tool registration SHALL include `ToolAnnotations` with `ReadOnlyHint: false`, `DestructiveHint: ptr(true)`, `IdempotentHint: false`, `OpenWorldHint: ptr(false)`, and `Title: "Rotate namespace credentials"`. The description SHALL include a safety note about token invalidation.

#### Scenario: Successful credential rotation
- **WHEN** the `cred_rotate` tool is called with `namespace: "dev-alice"`
- **THEN** the system deletes the existing workflow ServiceAccount and creates a new one, and all previously issued tokens for that ServiceAccount become invalid

#### Scenario: ServiceAccount does not exist
- **WHEN** the `cred_rotate` tool is called for a namespace where the ServiceAccount was never created
- **THEN** the system creates the ServiceAccount (idempotent behavior)

#### Scenario: cred_rotate tool annotations are destructive
- **WHEN** the `cred_rotate` tool is retrieved from the server's tool list
- **THEN** `annotations.readOnlyHint` is `false` and `annotations.destructiveHint` is `true`
