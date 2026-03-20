## Why

Tentacular currently has no authorization model beyond bearer-token authentication. Any authenticated user can deploy, modify, or delete any tentacle. As the platform grows to multi-user and team environments, we need POSIX-like owner/group/mode authorization so that tentacle access can be scoped to individuals and teams. The MCP server is the natural enforcement point since all cluster operations route through it.

## What Changes

- New `pkg/authz/` package implementing POSIX-like permission evaluation (mode.go, presets.go, annotations.go, evaluator.go)
- **BREAKING**: Annotation namespace migration from `tentacular.dev/*` to `tentacular.io/*` (environment, tags, cron-schedule)
- **BREAKING**: `tentacular.dev/owner` and `tentacular.dev/team` annotations dropped, replaced by authz annotations (owner-sub, owner-email, owner-name, group)
- New authz annotations on Deployments: owner-sub, owner-email, owner-name, group, mode, auth-provider, idp-provider, default-mode, default-group, created-at, updated-at, updated-by-sub, updated-by-email
- Extend DeployerInfo with Groups field
- Wire authz evaluation into all tool handlers (deploy, discover, run, workflow, wf_health)
- New authz_helpers.go providing shared permission-check helpers for tool handlers
- New permissions tools: permissions_get, permissions_set
- Extend AnnotateDeployer in enrich.go to stamp authz annotations at deploy time
- Annotation migration logic in discover.go, scheduler.go

## Capabilities

### New Capabilities
- `authz-model`: Core permission model (Mode type, permission bits, owner/group/mode evaluation, presets)
- `authz-annotations`: Annotation constants, reader/writer, migration from tentacular.dev to tentacular.io
- `authz-evaluator`: Permission evaluation engine (CheckAccess, identity matching, bearer-token bypass)
- `authz-tool-integration`: Wiring authz checks into all existing MCP tool handlers
- `permissions-tools`: New permissions_get and permissions_set MCP tools
- `deployer-enrichment`: Extended deployer annotation stamping with owner/group/mode at deploy time

### Modified Capabilities
<!-- No existing OpenSpec capabilities to modify -->

## Impact

- **All MCP tool handlers** gain authz checks (deploy, discover, run, workflow lifecycle, health)
- **Kubernetes annotations** on Deployments change namespace and gain new authz fields
- **DeployerInfo struct** gains Groups field, affecting authentication flow
- **Scheduler** must handle annotation migration for cron-schedule
- **Discover** must handle annotation migration for environment/tags
- **Breaking**: Clients using old `tentacular.dev/*` annotations will see them ignored
- **New Helm values**: authz.defaultMode, authz.defaultGroup, authz.provider
