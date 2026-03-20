## Context

The MCP server (`tentacular-mcp`) is the enforcement point for all Kubernetes operations in Tentacular. Currently, authentication is binary: either you have a valid bearer token or OIDC token, or you don't. There is no per-resource authorization. All tool handlers (deploy, discover, run, workflow lifecycle, health) execute without checking whether the caller has permission on the specific tentacle.

The existing annotation scheme uses `tentacular.dev/*` prefix with `owner` and `team` annotations that carry no enforcement semantics.

## Goals / Non-Goals

**Goals:**
- Implement POSIX-like owner/group/mode model where every tentacle has an owner identity, a group, and numeric permission bits
- Enforce permissions in every tool handler that reads or modifies a tentacle
- Preserve bearer-token bypass (bearer tokens skip authz entirely for backward compatibility and agent use)
- Migrate annotation namespace from `tentacular.dev/*` to `tentacular.io/*`
- Stamp authz annotations at deploy time from deployer identity
- Provide permissions_get and permissions_set tools for inspecting and modifying permissions

**Non-Goals:**
- IdP integration (reading group membership from OIDC claims is in scope, but configuring Keycloak/Google is not)
- RBAC-style policy engine (we use POSIX mode bits, not role-based policies)
- Namespace-level permissions (authz is per-tentacle, not per-namespace)
- Audit logging of permission checks (future work)

## Decisions

### 1. POSIX mode bits as the permission model

**Rationale:** POSIX permissions are universally understood, simple to implement, and sufficient for the owner/group/others access pattern. Three permission types map to tentacle operations: read (list, status, health), write (deploy, update, remove, annotate), execute (run, restart).

**Alternative considered:** Kubernetes RBAC-style policies. Rejected as over-engineered for the current use case and would require a policy engine.

### 2. Bearer-token bypass

**Rationale:** Bearer tokens have no identity claims. Since we cannot determine owner/group membership, bearer-token requests bypass authz entirely. This preserves backward compatibility and supports agent/automation use cases.

**Alternative considered:** Require OIDC for all authz-enabled clusters. Rejected because it would break existing deployments.

### 3. Annotations as source of truth (not a database)

**Rationale:** Storing owner/group/mode as Kubernetes annotations on the Deployment keeps authz data co-located with the resource. No additional database or CRD required. Annotations are readable with standard kubectl.

**Alternative considered:** CRD or ConfigMap-based permission store. Rejected as unnecessary indirection.

### 4. Authz evaluation in tool handlers, not middleware

**Rationale:** Different tool operations require different permission checks (read vs write vs execute). Tool handlers know the required permission. A shared helper (`authz_helpers.go`) provides the common check pattern while each handler specifies the required permission.

### 5. Annotation migration at read time

**Rationale:** When reading annotations (discover, scheduler), check both old (`tentacular.dev/*`) and new (`tentacular.io/*`) prefixes, preferring new. At write time (deploy, annotate), always write new prefix. This provides graceful migration without a one-time batch job.

## Risks / Trade-offs

- **[Annotation size limits]** Kubernetes annotations have a combined size limit (~256KB). The 14 new annotations add roughly 1-2KB. -> Low risk, well within limits.
- **[Group membership from OIDC claims]** Different IdPs structure group claims differently (`groups`, `roles`, `team`). -> Mitigation: Make the group claim name configurable via Helm.
- **[Bearer bypass as a security gap]** Anyone with a bearer token has full access. -> Mitigation: Document clearly; bearer tokens are intended for trusted agents. For multi-user environments, OIDC should be required.
- **[Migration period confusion]** During migration, some tentacles will have old annotations, some new. -> Mitigation: Read logic checks both; discover/list shows normalized output.
