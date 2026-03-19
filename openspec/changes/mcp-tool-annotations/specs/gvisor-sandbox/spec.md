## MODIFIED Requirements

### Requirement: Check gVisor availability
The system SHALL check whether a gVisor-compatible RuntimeClass (handler `gvisor` or `runsc`) exists in the cluster and return its name, handler, and availability status. The `gvisor_check` tool registration SHALL include `ToolAnnotations` with `ReadOnlyHint: true`, `OpenWorldHint: ptr(false)`, and `Title: "Check gVisor availability"`.

#### Scenario: gVisor RuntimeClass exists
- **WHEN** the `gvisor_check` tool is called and a RuntimeClass with handler `runsc` exists
- **THEN** the system returns `available: true` with the RuntimeClass name and handler

#### Scenario: gVisor not installed
- **WHEN** the `gvisor_check` tool is called and no gVisor RuntimeClass exists
- **THEN** the system returns `available: false` with installation guidance

#### Scenario: gvisor_check tool annotations are read-only
- **WHEN** the `gvisor_check` tool is retrieved from the server's tool list
- **THEN** `annotations.readOnlyHint` is `true` and `annotations.openWorldHint` is `false`

### Requirement: Annotate namespace with gVisor runtime class
The system SHALL annotate a managed namespace to indicate gVisor sandboxing is desired by adding the annotation `tentacular.io/runtime-class: gvisor`. This annotation signals to workflow deployments that pods SHOULD use the gVisor RuntimeClass. The system SHALL verify the namespace is managed by tentacular and reject the operation if the target namespace is `tentacular-system`. The `gvisor_annotate_ns` tool registration SHALL include `ToolAnnotations` with `ReadOnlyHint: false`, `DestructiveHint: ptr(false)`, `IdempotentHint: true`, `OpenWorldHint: ptr(false)`, and `Title: "Enable gVisor for namespace"`.

#### Scenario: Annotate managed namespace with gVisor
- **WHEN** the `gvisor_annotate_ns` tool is called with `namespace: "dev-alice"` and the namespace is managed
- **THEN** the system adds the `tentacular.io/runtime-class: gvisor` annotation to the namespace

#### Scenario: Reject for unmanaged namespace
- **WHEN** the `gvisor_annotate_ns` tool is called for a namespace without the managed-by label
- **THEN** the system returns an error indicating the namespace is not managed by tentacular

#### Scenario: gVisor not available in cluster
- **WHEN** the `gvisor_annotate_ns` tool is called and no gVisor RuntimeClass exists
- **THEN** the system returns an error indicating gVisor is not available in the cluster

#### Scenario: gvisor_annotate_ns tool annotations are non-destructive idempotent write
- **WHEN** the `gvisor_annotate_ns` tool is retrieved from the server's tool list
- **THEN** `annotations.readOnlyHint` is `false`, `annotations.destructiveHint` is `false`, and `annotations.idempotentHint` is `true`

### Requirement: Verify gVisor sandbox
The system SHALL launch a short-lived verification pod in the target namespace using the gVisor RuntimeClass, run `dmesg | head -1` inside it, confirm the output contains "gVisor" or "runsc", and clean up the pod. The system SHALL reject the operation if the target namespace is `tentacular-system`. The `gvisor_verify` tool registration SHALL include `ToolAnnotations` with `ReadOnlyHint: false`, `DestructiveHint: ptr(false)`, `IdempotentHint: false`, `OpenWorldHint: ptr(false)`, and `Title: "Verify gVisor sandbox"`.

#### Scenario: gVisor verification succeeds
- **WHEN** the `gvisor_verify` tool is called with `namespace: "dev-alice"` and gVisor is functional
- **THEN** the system returns `verified: true` with the dmesg output confirming gVisor

#### Scenario: gVisor verification fails
- **WHEN** the `gvisor_verify` tool is called and the verification pod output does not contain gVisor markers
- **THEN** the system returns `verified: false` with the actual dmesg output for debugging

#### Scenario: Verification pod times out
- **WHEN** the `gvisor_verify` tool is called and the verification pod does not reach Running state within 60 seconds
- **THEN** the system returns an error indicating the verification timed out and cleans up the pod

#### Scenario: gvisor_verify tool annotations are non-destructive non-idempotent write
- **WHEN** the `gvisor_verify` tool is retrieved from the server's tool list
- **THEN** `annotations.readOnlyHint` is `false`, `annotations.destructiveHint` is `false`, and `annotations.idempotentHint` is `false`
