## ADDED Requirements

### Requirement: Every registered tool SHALL have ToolAnnotations
The system SHALL set a non-nil `ToolAnnotations` struct on every `mcp.Tool` passed to `mcp.AddTool()`. No tool registration SHALL omit the `Annotations` field.

#### Scenario: All tools have annotations after registration
- **WHEN** `RegisterAll()` is called and the server's tool list is retrieved via `tools/list`
- **THEN** every tool in the response has a non-nil `annotations` field

#### Scenario: New tool added without annotations fails CI
- **WHEN** a developer adds a new tool via `mcp.AddTool()` without setting `Annotations`
- **THEN** the annotation coverage test in `register_test.go` fails

### Requirement: Every tool SHALL have a Title annotation
The system SHALL set a non-empty `Title` field on every tool's `ToolAnnotations`. Titles SHALL use sentence case and action-oriented phrasing (e.g., "List managed namespaces", "Delete a managed namespace").

#### Scenario: All tools have titles
- **WHEN** the server's tool list is retrieved via `tools/list`
- **THEN** every tool has a non-empty `annotations.title` field

### Requirement: Read-only tools SHALL be annotated with ReadOnlyHint true
The system SHALL set `ReadOnlyHint: true` on all tools that do not modify cluster state. Read-only tools are: wf_list, wf_describe, wf_status, wf_health, wf_health_ns, wf_pods, wf_logs, wf_events, wf_jobs, ns_get, ns_list, health_nodes, health_ns_usage, health_cluster_summary, audit_rbac, audit_netpol, audit_psa, gvisor_check, exo_status, exo_registration, exo_list, proxy_status, cluster_profile, cluster_preflight.

#### Scenario: Read-only tool has ReadOnlyHint true
- **WHEN** the tool `wf_list` is retrieved from the server's tool list
- **THEN** `annotations.readOnlyHint` is `true`

#### Scenario: Read-only tool does not set DestructiveHint
- **WHEN** the tool `ns_list` is retrieved from the server's tool list
- **THEN** `annotations.readOnlyHint` is `true` and `annotations.destructiveHint` is nil (not meaningful for read-only tools)

### Requirement: Write tools SHALL be annotated with DestructiveHint false
The system SHALL set `DestructiveHint: ptr(false)` on tools that modify cluster state but are not destructive. Write tools are: wf_apply, wf_restart, wf_run, ns_create, ns_update, gvisor_annotate_ns, gvisor_verify, cred_issue_token, cred_kubeconfig.

#### Scenario: Write tool has DestructiveHint false
- **WHEN** the tool `wf_apply` is retrieved from the server's tool list
- **THEN** `annotations.readOnlyHint` is `false` and `annotations.destructiveHint` is `false`

#### Scenario: Write tool ReadOnlyHint is false
- **WHEN** the tool `ns_create` is retrieved from the server's tool list
- **THEN** `annotations.readOnlyHint` is `false`

### Requirement: Destructive tools SHALL be annotated with DestructiveHint true
The system SHALL set `DestructiveHint: ptr(true)` on tools that permanently remove or irreversibly modify resources. Destructive tools are: wf_remove, ns_delete, cred_rotate.

#### Scenario: Destructive tool has DestructiveHint true
- **WHEN** the tool `wf_remove` is retrieved from the server's tool list
- **THEN** `annotations.readOnlyHint` is `false` and `annotations.destructiveHint` is `true`

#### Scenario: ns_delete is destructive
- **WHEN** the tool `ns_delete` is retrieved from the server's tool list
- **THEN** `annotations.destructiveHint` is `true`

#### Scenario: cred_rotate is destructive
- **WHEN** the tool `cred_rotate` is retrieved from the server's tool list
- **THEN** `annotations.destructiveHint` is `true`

### Requirement: Idempotent tools SHALL be annotated with IdempotentHint true
The system SHALL set `IdempotentHint: true` on write tools where repeated calls with the same arguments produce no additional side effects. Idempotent write tools are: wf_apply, ns_create, ns_update, gvisor_annotate_ns, cred_kubeconfig.

#### Scenario: Idempotent write tool
- **WHEN** the tool `wf_apply` is retrieved from the server's tool list
- **THEN** `annotations.idempotentHint` is `true`

#### Scenario: Non-idempotent write tool
- **WHEN** the tool `wf_run` is retrieved from the server's tool list
- **THEN** `annotations.idempotentHint` is `false`

### Requirement: All tools SHALL have OpenWorldHint false
The system SHALL set `OpenWorldHint: ptr(false)` on all tools because all tools interact with a bounded Kubernetes cluster, not an open-ended external system.

#### Scenario: Tool has closed-world annotation
- **WHEN** any tool is retrieved from the server's tool list
- **THEN** `annotations.openWorldHint` is `false`

### Requirement: Bool pointer helper for ToolAnnotations
The system SHALL provide a helper function `boolPtr(b bool) *bool` in `pkg/tools/` for constructing `*bool` values used by `DestructiveHint` and `OpenWorldHint` fields.

#### Scenario: Helper returns pointer to true
- **WHEN** `boolPtr(true)` is called
- **THEN** the returned `*bool` points to a value of `true`

#### Scenario: Helper returns pointer to false
- **WHEN** `boolPtr(false)` is called
- **THEN** the returned `*bool` points to a value of `false`
