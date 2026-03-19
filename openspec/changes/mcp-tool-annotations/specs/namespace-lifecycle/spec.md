## MODIFIED Requirements

### Requirement: Create managed namespace
The system SHALL create a Kubernetes namespace with the `app.kubernetes.io/managed-by: tentacular` label and Pod Security Admission labels set to `restricted` profile at `latest` version. The system SHALL also create a default-deny NetworkPolicy, a DNS-allow NetworkPolicy, a ResourceQuota (from a named preset), a LimitRange with default container resource requests/limits, a workflow ServiceAccount, a workflow Role, and a workflow RoleBinding in the new namespace. The workflow Role SHALL grant `create,update,delete,patch,get,list,watch` on apps/deployments, core/services+configmaps+secrets, batch/cronjobs+jobs, networking.k8s.io/networkpolicies+ingresses; `get,list,watch` on core/pods+pods/log+events; and `get,list,patch,update` on core/serviceaccounts (for imagePullSecrets management). The system SHALL reject the operation if the target namespace is `tentacular-system`. The `ns_create` tool registration SHALL include `ToolAnnotations` with `ReadOnlyHint: false`, `DestructiveHint: ptr(false)`, `IdempotentHint: true`, `OpenWorldHint: ptr(false)`, and `Title: "Create managed namespace"`. The description SHALL mention the resources created (namespace, network policies, resource quotas, RBAC).

#### Scenario: Successful namespace creation with small quota
- **WHEN** the `ns_create` tool is called with `name: "dev-alice"` and `quota_preset: "small"`
- **THEN** the system creates namespace `dev-alice` with managed-by label, PSA restricted labels, default-deny and DNS-allow NetworkPolicies, a ResourceQuota with CPU=2, Mem=2Gi, Pods=10, a LimitRange, and the workflow ServiceAccount/Role/RoleBinding

#### Scenario: Reject creation of protected namespace
- **WHEN** the `ns_create` tool is called with `name: "tentacular-system"`
- **THEN** the system returns an error indicating operations on `tentacular-system` are not allowed

#### Scenario: Namespace already exists
- **WHEN** the `ns_create` tool is called with a name that already exists
- **THEN** the system returns an error indicating the namespace already exists

#### Scenario: ns_create tool annotations are non-destructive idempotent write
- **WHEN** the `ns_create` tool is retrieved from the server's tool list
- **THEN** `annotations.readOnlyHint` is `false`, `annotations.destructiveHint` is `false`, and `annotations.idempotentHint` is `true`

### Requirement: Delete managed namespace
The system SHALL delete a namespace only if it carries the `app.kubernetes.io/managed-by: tentacular` label. The system SHALL reject deletion of `tentacular-system`. Deleting a namespace removes all resources within it (Kubernetes garbage collection). The `ns_delete` tool registration SHALL include `ToolAnnotations` with `ReadOnlyHint: false`, `DestructiveHint: ptr(true)`, `IdempotentHint: false`, `OpenWorldHint: ptr(false)`, and `Title: "Delete managed namespace"`. The description SHALL include a safety note about permanent removal.

#### Scenario: Successful deletion of managed namespace
- **WHEN** the `ns_delete` tool is called with `name: "dev-alice"` and the namespace has the managed-by label
- **THEN** the system deletes the namespace

#### Scenario: Reject deletion of unmanaged namespace
- **WHEN** the `ns_delete` tool is called with a namespace that lacks the managed-by label
- **THEN** the system returns an error indicating the namespace is not managed by tentacular

#### Scenario: Reject deletion of protected namespace
- **WHEN** the `ns_delete` tool is called with `name: "tentacular-system"`
- **THEN** the system returns an error indicating operations on `tentacular-system` are not allowed

#### Scenario: ns_delete tool annotations are destructive
- **WHEN** the `ns_delete` tool is retrieved from the server's tool list
- **THEN** `annotations.readOnlyHint` is `false` and `annotations.destructiveHint` is `true`

### Requirement: Get namespace details
The system SHALL retrieve a single namespace by name and return its metadata, labels, annotations, status, resource quota usage, and limit range configuration. The system SHALL reject the operation if the target is `tentacular-system`. The `ns_get` tool registration SHALL include `ToolAnnotations` with `ReadOnlyHint: true`, `OpenWorldHint: ptr(false)`, and `Title: "Get namespace details"`.

#### Scenario: Get existing namespace
- **WHEN** the `ns_get` tool is called with `name: "dev-alice"`
- **THEN** the system returns the namespace metadata, labels, annotations, status, quota summary, and limit range summary

#### Scenario: ns_get tool annotations are read-only
- **WHEN** the `ns_get` tool is retrieved from the server's tool list
- **THEN** `annotations.readOnlyHint` is `true` and `annotations.openWorldHint` is `false`

### Requirement: List managed namespaces
The system SHALL list all namespaces that carry the `app.kubernetes.io/managed-by: tentacular` label. The `ns_list` tool registration SHALL include `ToolAnnotations` with `ReadOnlyHint: true`, `OpenWorldHint: ptr(false)`, and `Title: "List managed namespaces"`.

#### Scenario: List namespaces
- **WHEN** the `ns_list` tool is called
- **THEN** the system returns all namespaces with the managed-by label

#### Scenario: ns_list tool annotations are read-only
- **WHEN** the `ns_list` tool is retrieved from the server's tool list
- **THEN** `annotations.readOnlyHint` is `true` and `annotations.openWorldHint` is `false`

### Requirement: Update managed namespace
The system SHALL update labels, annotations, or resource quota preset on a managed namespace. The `ns_update` tool registration SHALL include `ToolAnnotations` with `ReadOnlyHint: false`, `DestructiveHint: ptr(false)`, `IdempotentHint: true`, `OpenWorldHint: ptr(false)`, and `Title: "Update managed namespace"`.

#### Scenario: Update namespace labels
- **WHEN** the `ns_update` tool is called with new labels
- **THEN** the system merges the labels onto the namespace

#### Scenario: ns_update tool annotations are non-destructive idempotent write
- **WHEN** the `ns_update` tool is retrieved from the server's tool list
- **THEN** `annotations.readOnlyHint` is `false`, `annotations.destructiveHint` is `false`, and `annotations.idempotentHint` is `true`
