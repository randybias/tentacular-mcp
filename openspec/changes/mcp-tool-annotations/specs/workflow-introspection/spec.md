## MODIFIED Requirements

### Requirement: List pods in namespace
The system SHALL list all pods in a given namespace, returning pod name, phase, ready condition, restart count, container images, and age. The ClusterRole SHALL include the `watch` verb on pods and events to support future event streaming for `wf_pods` and `wf_events`. The system SHALL reject the operation if the target namespace is `tentacular-system`. The `wf_pods` tool registration SHALL include `ToolAnnotations` with `ReadOnlyHint: true`, `OpenWorldHint: ptr(false)`, and `Title: "List pods in namespace"`.

#### Scenario: List pods with running workloads
- **WHEN** the `wf_pods` tool is called with `namespace: "dev-alice"` and pods exist
- **THEN** the system returns a list of pods with name, phase, ready status, restart count, images, and creation timestamp

#### Scenario: No pods in namespace
- **WHEN** the `wf_pods` tool is called with `namespace: "dev-alice"` and no pods exist
- **THEN** the system returns an empty list

#### Scenario: wf_pods tool annotations are read-only
- **WHEN** the `wf_pods` tool is retrieved from the server's tool list
- **THEN** `annotations.readOnlyHint` is `true` and `annotations.openWorldHint` is `false`

### Requirement: Tail pod logs
The system SHALL retrieve the most recent log lines from a specified container in a pod. The caller MAY specify `tail_lines` (default 100) and an optional `container` name. If the pod has a single container, the container parameter MAY be omitted. The system SHALL reject the operation if the target namespace is `tentacular-system`. The `wf_logs` tool registration SHALL include `ToolAnnotations` with `ReadOnlyHint: true`, `OpenWorldHint: ptr(false)`, and `Title: "Tail pod logs"`.

#### Scenario: Tail logs from single-container pod
- **WHEN** the `wf_logs` tool is called with `namespace: "dev-alice"`, `pod: "web-abc123"`, and `tail_lines: 50`
- **THEN** the system returns the last 50 log lines from the pod's only container

#### Scenario: Tail logs from specific container in multi-container pod
- **WHEN** the `wf_logs` tool is called with `namespace: "dev-alice"`, `pod: "web-abc123"`, `container: "sidecar"`, and `tail_lines: 20`
- **THEN** the system returns the last 20 log lines from the `sidecar` container

#### Scenario: Pod not found
- **WHEN** the `wf_logs` tool is called with a pod name that does not exist
- **THEN** the system returns an error indicating the pod was not found

#### Scenario: wf_logs tool annotations are read-only
- **WHEN** the `wf_logs` tool is retrieved from the server's tool list
- **THEN** `annotations.readOnlyHint` is `true` and `annotations.openWorldHint` is `false`

### Requirement: List namespace events
The system SHALL list recent events in a namespace, returning type (Normal/Warning), reason, message, involved object, count, and last timestamp. Results SHALL be sorted by last timestamp descending and limited to the most recent 100 events by default. The system SHALL reject the operation if the target namespace is `tentacular-system`. The `wf_events` tool registration SHALL include `ToolAnnotations` with `ReadOnlyHint: true`, `OpenWorldHint: ptr(false)`, and `Title: "List namespace events"`.

#### Scenario: List events with warnings present
- **WHEN** the `wf_events` tool is called with `namespace: "dev-alice"`
- **THEN** the system returns events sorted by last timestamp descending, each with type, reason, message, involved object reference, count, and timestamp

#### Scenario: No events in namespace
- **WHEN** the `wf_events` tool is called and no events exist in the namespace
- **THEN** the system returns an empty list

#### Scenario: wf_events tool annotations are read-only
- **WHEN** the `wf_events` tool is retrieved from the server's tool list
- **THEN** `annotations.readOnlyHint` is `true` and `annotations.openWorldHint` is `false`

### Requirement: List jobs and cronjobs
The system SHALL list all Jobs and CronJobs in a namespace. For Jobs, return name, status (active/succeeded/failed), start time, completion time, and duration. For CronJobs, return name, schedule, last schedule time, active count, and suspend status. The system SHALL reject the operation if the target namespace is `tentacular-system`. The `wf_jobs` tool registration SHALL include `ToolAnnotations` with `ReadOnlyHint: true`, `OpenWorldHint: ptr(false)`, and `Title: "List jobs and cronjobs"`.

#### Scenario: List jobs and cronjobs
- **WHEN** the `wf_jobs` tool is called with `namespace: "dev-alice"`
- **THEN** the system returns separate lists of Jobs and CronJobs with their respective status fields

#### Scenario: No jobs or cronjobs
- **WHEN** the `wf_jobs` tool is called and no Jobs or CronJobs exist
- **THEN** the system returns empty lists for both Jobs and CronJobs

#### Scenario: wf_jobs tool annotations are read-only
- **WHEN** the `wf_jobs` tool is retrieved from the server's tool list
- **THEN** `annotations.readOnlyHint` is `true` and `annotations.openWorldHint` is `false`

### Requirement: Rollout restart a deployment
The system SHALL perform a rollout restart of a Deployment in a managed namespace by patching the pod template with a `tentacular.io/restartedAt` annotation containing the current UTC timestamp. This is the same mechanism as `kubectl rollout restart`. The system SHALL verify the namespace is managed by tentacular and that the deployment exists before patching. The system SHALL reject the operation if the target namespace is `tentacular-system` or if the namespace is not managed by tentacular. The `wf_restart` tool registration SHALL include `ToolAnnotations` with `ReadOnlyHint: false`, `DestructiveHint: ptr(false)`, `IdempotentHint: false`, `OpenWorldHint: ptr(false)`, and `Title: "Restart a deployment"`.

#### Scenario: Successful rollout restart
- **WHEN** the `wf_restart` tool is called with `namespace: "dev-alice"` and `deployment: "web-app"` and the namespace is managed
- **THEN** the system patches the deployment's pod template with a `tentacular.io/restartedAt` annotation and returns `restarted: true`

#### Scenario: Reject restart in unmanaged namespace
- **WHEN** the `wf_restart` tool is called with a namespace that is not managed by tentacular
- **THEN** the system returns an error indicating the namespace is not managed

#### Scenario: Deployment not found
- **WHEN** the `wf_restart` tool is called with a deployment name that does not exist in the namespace
- **THEN** the system returns an error indicating the deployment was not found

#### Scenario: Reject restart in protected namespace
- **WHEN** the `wf_restart` tool is called with `namespace: "tentacular-system"`
- **THEN** the system returns an error indicating operations on `tentacular-system` are not allowed

#### Scenario: wf_restart tool annotations are non-destructive write
- **WHEN** the `wf_restart` tool is retrieved from the server's tool list
- **THEN** `annotations.readOnlyHint` is `false`, `annotations.destructiveHint` is `false`, and `annotations.idempotentHint` is `false`
