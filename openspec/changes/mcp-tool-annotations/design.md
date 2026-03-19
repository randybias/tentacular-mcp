## Context

The tentacular-mcp server registers 36 tools via `mcp.AddTool()` across 13 source files in `pkg/tools/`. Currently, all tool registrations use only `Name` and `Description` fields on the `mcp.Tool` struct. The MCP Go SDK v1.3.1 (already in go.mod) supports `ToolAnnotations` on the `Tool` struct, providing `ReadOnlyHint`, `DestructiveHint`, `IdempotentHint`, `OpenWorldHint`, and `Title` fields.

Without annotations, agents must rely on description text parsing or hardcoded classification tables (e.g., in tentacular-skill) to determine whether a tool is safe to call without confirmation. This is fragile and requires cross-repo synchronization.

The `ToolAnnotations` struct uses Go zero-value semantics deliberately: `ReadOnlyHint` and `IdempotentHint` are plain `bool` (default false), while `DestructiveHint` and `OpenWorldHint` are `*bool` (default nil, meaning "true" per spec). This distinction matters for correct annotation.

### Current tool inventory (36 tools)

| File | Tools |
|------|-------|
| `workflow.go` | wf_list, wf_describe, wf_status, wf_pods, wf_logs |
| `discover.go` | wf_events, wf_jobs |
| `run.go` | wf_run |
| `deploy.go` | wf_apply, wf_remove, wf_restart |
| `wf_health.go` | wf_health, wf_health_ns |
| `namespace.go` | ns_create, ns_delete, ns_get, ns_list, ns_update |
| `credential.go` | cred_issue_token, cred_kubeconfig, cred_rotate |
| `health.go` | health_nodes, health_ns_usage, health_cluster_summary |
| `audit.go` | audit_rbac, audit_netpol, audit_psa |
| `gvisor.go` | gvisor_check, gvisor_annotate_ns, gvisor_verify |
| `clusterops.go` | cluster_profile, cluster_preflight |
| `proxy.go` | proxy_status |
| `exoskeleton.go` | exo_status, exo_registration, exo_list |

## Goals / Non-Goals

**Goals:**
- Add `ToolAnnotations` to every tool registration so agents can introspect safety classification via `tools/list`
- Set `Title` on each tool for human-readable display
- Set `ReadOnlyHint`, `DestructiveHint`, `IdempotentHint`, and `OpenWorldHint` appropriately per tool
- Enrich `Description` strings with behavioral guidance (prerequisites, expected effects, safety notes)
- Add test coverage verifying that every registered tool has annotations and that classifications are correct
- Cover all 36 tools (the proposal listed 35; `exo_list` was missed and must be included)

**Non-Goals:**
- Changing tool behavior or handler logic
- Adding new tools or removing existing tools
- Modifying the MCP Go SDK or upgrading its version
- Implementing client-side annotation consumption logic
- Adding per-call confirmation prompts (that is a client/agent responsibility)

## Decisions

### D1: Annotation values by tier

Three tiers with consistent annotation patterns:

| Tier | ReadOnlyHint | DestructiveHint | IdempotentHint | OpenWorldHint |
|------|-------------|-----------------|----------------|---------------|
| Read-only | `true` | (ignored when ReadOnly) | (ignored when ReadOnly) | `ptr(false)` |
| Write (non-destructive) | `false` | `ptr(false)` | per-tool | `ptr(false)` |
| Destructive | `false` | `ptr(true)` | `false` | `ptr(false)` |

**Rationale**: Per the MCP spec, `DestructiveHint` and `IdempotentHint` are only meaningful when `ReadOnlyHint` is false. All tentacular tools operate on a closed Kubernetes cluster, so `OpenWorldHint` is `ptr(false)` for all tools. Using `ptr(false)` for write-tier `DestructiveHint` explicitly signals "non-destructive write" rather than relying on the nil default (which means "true" per spec).

**Alternative considered**: Omitting `OpenWorldHint` entirely (nil defaults to true per spec). Rejected because all tools interact with a bounded Kubernetes cluster, not an open-ended external system. Explicit `ptr(false)` is more accurate.

### D2: IdempotentHint classification

Tools where repeated calls with the same arguments produce no additional side effects get `IdempotentHint: true`:

- `wf_apply` (re-applying the same manifest is a no-op in Kubernetes)
- `ns_create` (will fail or no-op if namespace exists)
- `ns_update` (re-applying same labels/annotations is idempotent)
- `gvisor_annotate_ns` (re-annotating is idempotent)
- `cred_kubeconfig` (generates same output for same inputs)

Tools that are NOT idempotent:
- `wf_run` (creates a new workflow run each time)
- `wf_restart` (triggers a new restart each time)
- `cred_issue_token` (generates a new token each time)
- `gvisor_verify` (runs a verification pod each time)
- `wf_remove`, `ns_delete`, `cred_rotate` (destructive, second call may fail)

**Rationale**: Kubernetes API semantics make most create/update operations idempotent. Token generation and workflow runs are inherently non-idempotent.

### D3: Helper function for bool pointers

Introduce a file-scoped helper `boolPtr(b bool) *bool` in a shared location (e.g., top of `register.go` or a new `annotations.go` helper file) to construct `*bool` values for `DestructiveHint` and `OpenWorldHint`.

**Rationale**: Go requires taking the address of a variable to get `*bool`; inline `&[]bool{true}[0]` is unreadable. A helper keeps the registration code clean.

**Alternative considered**: Using package-level `var` sentinels (`var trueVal = true`). Rejected because a function is more idiomatic and avoids mutable package state.

### D4: Title format convention

Titles use sentence case, action-oriented phrasing: "List managed namespaces", "Delete a managed namespace", "Get cluster health summary". This matches the MCP spec recommendation that `Title` be human-readable display text.

### D5: Description enrichment strategy

Enhance descriptions with a consistent structure:
1. One-sentence summary of what the tool does (already exists)
2. Prerequisites or constraints (e.g., "Only works on tentacular-managed namespaces")
3. Expected effects for write/destructive tools (e.g., "Creates namespace, network policy, resource quota, and RBAC resources")
4. Safety note for destructive tools (e.g., "Permanently removes the namespace and all resources within it")

Keep descriptions concise -- no more than 2-3 sentences total. Agents consume these in context windows.

**Alternative considered**: Adding structured metadata fields beyond what MCP supports. Rejected because the MCP spec already provides the annotation fields needed, and description text is the standard channel for behavioral guidance.

### D6: Test approach

Add a table-driven test in `register_test.go` that:
1. Registers all tools via `RegisterAll()` on a test server
2. Calls `tools/list` to get the registered tools
3. Asserts every tool has non-nil `Annotations`
4. Asserts specific classification per tool name using a map of expected annotations
5. Asserts every tool has a non-empty `Title`

This approach catches regressions if new tools are added without annotations.

**Alternative considered**: Per-file unit tests for each tool's annotations. Rejected because a single table-driven test is more maintainable and catches the "forgot to annotate a new tool" failure mode.

### D7: File organization

Modify tool registrations in-place within each existing file (`namespace.go`, `workflow.go`, etc.) rather than centralizing annotations in a separate file. Add the `boolPtr` helper to `register.go`.

**Rationale**: Keeping annotations co-located with tool definitions makes it easy to see the full tool specification at a glance. A separate annotations file would scatter the tool definition across two locations.

## Risks / Trade-offs

**[Risk] Annotation drift when new tools are added** -- A developer might add a new tool without annotations, regressing the safety classification coverage.
Mitigation: The table-driven test (D6) will fail if any registered tool lacks annotations, catching this in CI.

**[Risk] Incorrect classification leads to false safety signals** -- If a destructive tool is marked read-only, an agent may call it without user confirmation.
Mitigation: The test includes per-tool expected values, not just "has annotations". Code review should verify classifications against actual handler behavior.

**[Risk] SDK upgrade changes ToolAnnotations semantics** -- Future SDK versions might alter default values or add new fields.
Mitigation: The SDK is pinned at v1.3.1 in go.mod. Any upgrade will require reviewing annotation semantics.

**[Trade-off] Description length vs. context window cost** -- Richer descriptions consume more tokens in agent context. We keep descriptions to 2-3 sentences maximum to balance informativeness with token efficiency.

**[Trade-off] OpenWorldHint false for all tools** -- Some tools (e.g., `cluster_preflight`) interact with external registries or endpoints. We classify as closed-world because the primary interaction domain is the Kubernetes cluster. This could be revisited per-tool if agents need finer-grained open-world classification.

## Open Questions

1. **exo_list inclusion**: The proposal listed 35 tools but the codebase has 36 (including `exo_list`). Confirm `exo_list` should be classified as Read-only (ReadOnlyHint: true) alongside `exo_status` and `exo_registration`.

2. **gvisor_verify classification**: Currently classified as Write (non-destructive) because it creates a temporary verification pod. Should it be Read-only since the pod is ephemeral and the intent is verification, not mutation? The current classification (Write, non-destructive, non-idempotent) seems correct since it does create a resource.
