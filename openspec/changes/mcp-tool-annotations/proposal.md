## Why

The MCP specification supports ToolAnnotations (ReadOnlyHint, DestructiveHint, IdempotentHint, OpenWorldHint) for safety classification, but the tentacular-mcp server registers all 35 tools without any annotations. Agents cannot auto-classify tool safety from introspection and must rely on hardcoded knowledge in skill docs. Adding annotations enables agents to discover tool risk levels dynamically, reducing misuse and enabling the skill restructuring in tentacular-skill to reference introspectable safety data rather than maintaining a static classification table.

## What Changes

- Add MCP ToolAnnotations to all 35 tool registrations in `pkg/tools/`, classified into three tiers:
  - **Read** (ReadOnlyHint: true, 23 tools): wf_list, wf_describe, wf_status, wf_health, wf_health_ns, wf_pods, wf_logs, wf_events, wf_jobs, ns_get, ns_list, health_nodes, health_ns_usage, health_cluster_summary, audit_rbac, audit_netpol, audit_psa, gvisor_check, exo_status, exo_registration, proxy_status, cluster_profile, cluster_preflight
  - **Write** (DestructiveHint: false explicitly, 9 tools): wf_apply, wf_restart, wf_run, ns_create, ns_update, gvisor_annotate_ns, gvisor_verify, cred_issue_token, cred_kubeconfig
  - **Destructive** (DestructiveHint: true, 3 tools): wf_remove, ns_delete, cred_rotate
- Enrich tool descriptions with behavioral guidance (expected effects, confirmation patterns)
- Add/update tests in register_test.go to verify annotations are set correctly on every tool

## Capabilities

### New Capabilities
- `tool-annotations`: MCP ToolAnnotation support for all registered tools, including ReadOnlyHint, DestructiveHint, IdempotentHint, and OpenWorldHint classification
- `tool-description-enrichment`: Enhanced tool descriptions with behavioral guidance for agent consumption

### Modified Capabilities
- `workflow-introspection`: Tool registrations for wf_* tools gain ToolAnnotations (ReadOnlyHint for read ops, DestructiveHint for wf_remove)
- `namespace-lifecycle`: Tool registrations for ns_* tools gain ToolAnnotations (DestructiveHint: true for ns_delete)
- `credential-management`: Tool registrations for cred_* tools gain ToolAnnotations (DestructiveHint: true for cred_rotate)
- `security-audit`: Tool registrations for audit_* tools gain ToolAnnotations (ReadOnlyHint: true)
- `gvisor-sandbox`: Tool registrations for gvisor_* tools gain ToolAnnotations
- `cluster-health`: Tool registrations for health_* tools gain ToolAnnotations (ReadOnlyHint: true)
- `cluster-ops`: Tool registrations for cluster_profile and cluster_preflight gain ToolAnnotations (ReadOnlyHint: true)
- `module-proxy`: Tool registration for proxy_status gains ToolAnnotations (ReadOnlyHint: true)

## Impact

- **pkg/tools/*.go** -- all tool registration files updated to include ToolAnnotations structs
- **pkg/tools/register_test.go** -- new or updated test cases verifying annotation correctness for all 35 tools
- **MCP protocol surface** -- tools.list responses will now include annotations; this is additive and non-breaking for existing clients
- **Cross-repo dependency** -- tentacular-skill's references/mcp-tools.md (from skill-restructure-accuracy change) will reference these annotations as the source of truth for tool safety classification
