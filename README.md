# tentacular-mcp

An in-cluster MCP (Model Context Protocol) server for Kubernetes namespace lifecycle, credential management, workflow introspection, and cluster operations. Replaces direct kube-api access from developer workstations with a single authenticated HTTP endpoint backed by scoped RBAC.

## Why

Developer workstations holding cluster-wide admin kubeconfig is a security anti-pattern. tentacular-mcp proxies Kubernetes operations through a controlled ServiceAccount so CLI clients (and any MCP-capable client) interact with the cluster over one authenticated endpoint instead of raw kube-api access.

## Related Repositories

| Repository | Purpose |
|------------|---------|
| [tentacular](https://github.com/randybias/tentacular) | Go CLI + Deno workflow engine |
| [tentacular-skill](https://github.com/randybias/tentacular-skill) | Agent skill definition for AI assistants |
| [tentacular-scaffolds](https://github.com/randybias/tentacular-scaffolds) | Workflow scaffold catalog (quickstarts) |
| [tentacular-docs](https://github.com/randybias/tentacular-docs) | [Documentation site](https://randybias.github.io/tentacular-docs) |

## Documentation

Full documentation: **[randybias.github.io/tentacular-docs](https://randybias.github.io/tentacular-docs)** — see [MCP Tools Reference](https://randybias.github.io/tentacular-docs/reference/mcp-tools/), [MCP Server Setup](https://randybias.github.io/tentacular-docs/guides/mcp-server-setup/), and [Exoskeleton](https://randybias.github.io/tentacular-docs/concepts/exoskeleton/).

## Architecture

![MCP Server Architecture](docs/diagrams/mcp-server-architecture.svg)

The MCP server also manages the **module proxy** in the `tentacular-support` namespace. On startup, the proxy reconciler (`pkg/proxy/`) auto-creates an esm.sh Deployment and Service that caches jsr/npm modules for workflow pods. This eliminates direct internet egress from workflow containers.

### Request Flow

1. HTTP request hits `:8080/mcp` with `Authorization: Bearer <token>`
2. `auth.Middleware` validates the token (rejects with 401 if invalid; bypasses for `/healthz`)
3. MCP SDK `StreamableHTTPHandler` parses the message and routes to the registered tool
4. `register.go` wrapper: unmarshal params, run `guard.CheckNamespace()`, call handler, marshal result
5. Handler calls `pkg/k8s` functions using in-cluster `rest.Config`
6. Result returned as MCP `Content` with `type: "text"` containing JSON

## Prerequisites

- Go 1.25+
- A Kubernetes cluster (kind works for local development)
- `kubectl` configured with cluster access
- Docker (for building the container image)
- `openssl` (for generating the auth token)

## Quick Start

### Platform Deploy via Umbrella Chart (Recommended)

The umbrella chart (`charts/tentacular-platform/`) deploys the full platform: MCP server, PostgreSQL, NATS, cert-manager (optional), esm-sh proxy, namespace management, and network policies. It creates three namespaces (`tentacular-system`, `tentacular-exoskeleton`, `tentacular-support`), deploys all subcharts, and wires the exoskeleton Secret via `envFrom`.

**Development** (emptyDir storage, minimal resources, NodePort access):

```bash
helm dependency update charts/tentacular-platform/
helm install tentacular charts/tentacular-platform/ \
  -f charts/tentacular-platform/ci/dev-values.yaml \
  -n tentacular-system --create-namespace
```

Dev values include test credentials and disable persistent storage, so no additional `--set` flags are needed. Access MCP via NodePort 30080.

**Production** (persistent storage, TLS, nginx Ingress):

Production requires cert-manager CRDs for TLS certificate management. Install cert-manager first if not already present:

```bash
# Step 1: Install cert-manager (skip if already installed)
helm repo add jetstack https://charts.jetstack.io
helm repo update
helm install cert-manager jetstack/cert-manager \
  -n cert-manager --create-namespace --set crds.enabled=true

# Step 2: Install the platform
helm dependency update charts/tentacular-platform/
helm install tentacular charts/tentacular-platform/ \
  -f charts/tentacular-platform/ci/prod-values.yaml \
  -n tentacular-system --create-namespace \
  --set tentacular-mcp.auth.token="$(openssl rand -hex 32)" \
  --set postgresql.auth.password="$(openssl rand -hex 16)" \
  --set nats.config.merge.authorization.token="$(openssl rand -hex 16)"
```

If you don't need TLS certificates, disable them and skip the cert-manager step:

```bash
helm install tentacular charts/tentacular-platform/ \
  -f charts/tentacular-platform/ci/prod-values.yaml \
  -n tentacular-system --create-namespace \
  --set tls.clusterIssuers.create=false \
  --set tls.certificates.mcp.create=false \
  --set tentacular-mcp.auth.token="$(openssl rand -hex 32)" \
  --set postgresql.auth.password="$(openssl rand -hex 16)" \
  --set nats.config.merge.authorization.token="$(openssl rand -hex 16)"
```

The `-n tentacular-system` flag is required so the MCP pod and its Secret are co-located. See `charts/tentacular-platform/README.md` for all value profiles, ingress modes (nodeport, ingress, istio, alb-istio), network policies, and component toggles.

### MCP Server Only (Standalone)

```bash
TOKEN=$(openssl rand -hex 32)
helm install tentacular-mcp ./charts/tentacular-mcp \
  --namespace tentacular-system --create-namespace \
  --set auth.token="${TOKEN}"
```

### Manual Deploy (Kustomize)

Alternatively, deploy using Kustomize directly:

```bash
kubectl apply -k deploy/manifests/
```

This creates:
- `tentacular-system` namespace
- ServiceAccount, ClusterRole, and ClusterRoleBinding
- Auth Secret
- Deployment (single replica, distroless container, non-root)
- ClusterIP Service on port 8080

### Helm Values

Key values for customizing the deployment:

| Value | Default | Description |
|-------|---------|-------------|
| `image.repository` | `ghcr.io/randybias/tentacular-mcp` | Container image |
| `image.tag` | `latest` | Image tag |
| `image.pullPolicy` | `IfNotPresent` | Pull policy |
| `auth.token` | `""` | Bearer token (generate with `openssl rand -hex 32`) |
| `auth.existingSecret` | `""` | Use an existing Secret instead of creating one |
| `namespace.create` | `true` | Create the `tentacular-system` namespace |
| `service.type` | `ClusterIP` | Service type |
| `service.port` | `8080` | Service port |
| `resources.requests.memory` | `64Mi` | Memory request |
| `resources.limits.memory` | `256Mi` | Memory limit |

### CLI Configuration

After deploying, configure the tentacular CLI to connect to the MCP server:

```bash
# Option 1: interactive setup
tntc configure

# Option 2: manual config (~/.tentacular/config.yaml)
```

```yaml
mcp:
  endpoint: http://tentacular-mcp.tentacular-system.svc.cluster.local:8080/mcp
  token_path: ~/.tentacular/mcp-token
```

Store the token:

```bash
echo "<TOKEN>" > ~/.tentacular/mcp-token
chmod 600 ~/.tentacular/mcp-token
```

### Connect

```bash
# Port-forward to reach the server from outside the cluster
kubectl port-forward -n tentacular-system svc/tentacular-mcp 8080:8080

# Verify the health endpoint
curl http://localhost:8080/healthz

# Send an MCP initialize request (using any MCP client)
# The server listens on /mcp via Streamable HTTP transport
```

## MCP Tools

36 tools organized across 13 functional groups. All namespace-scoped tools enforce a self-protection guard that rejects operations targeting system namespaces.

### Namespace Lifecycle

| Tool | Description |
|------|-------------|
| `ns_create` | Create a managed namespace with PSA labels, default-deny NetworkPolicy, DNS-allow policy, ResourceQuota, LimitRange, and workflow SA/Role/RoleBinding. Accepts `small`, `medium`, or `large` quota presets. |
| `ns_delete` | Delete a managed namespace and all child resources. |
| `ns_get` | Get namespace details including labels, annotations, quota summary, and limit range. |
| `ns_list` | List all tentacular-managed namespaces. |
| `ns_update` | Update labels, annotations, or resource quota preset on a managed namespace. |

### Credential Management

| Tool | Description |
|------|-------------|
| `cred_issue_token` | Issue a short-lived ServiceAccount token via the TokenRequest API. TTL configurable from 10 to 1440 minutes. |
| `cred_kubeconfig` | Generate a scoped kubeconfig YAML containing a time-limited token, cluster CA, and API server URL. |
| `cred_rotate` | Rotate credentials by recreating the workflow ServiceAccount, invalidating all prior tokens. |

### Workflow Introspection

| Tool | Description |
|------|-------------|
| `wf_pods` | List pods in a namespace with phase, readiness, restart count, images, and age. |
| `wf_logs` | Tail pod logs (snapshot, not streaming). Supports container selection and line count. |
| `wf_events` | List namespace events with type, reason, message, object reference, and count. |
| `wf_jobs` | List Jobs and CronJobs in a namespace with status, schedule, and duration. |
| `wf_restart` | Rollout restart a deployment by patching the pod template with a restart timestamp. Useful after ConfigMap/Secret changes, credential rotation, or gVisor enablement. |

### Cluster Operations

| Tool | Description |
|------|-------------|
| `cluster_preflight` | Run preflight validation checks (API connectivity, namespace access, RBAC, gVisor availability). |
| `cluster_profile` | Generate a full cluster profile: K8s version, nodes, CNI, storage classes, runtime classes, extensions, and exoskeleton services. |

### gVisor Sandbox

| Tool | Description |
|------|-------------|
| `gvisor_check` | Check if a gVisor RuntimeClass is available in the cluster. |
| `gvisor_annotate_ns` | Annotate a managed namespace with the gVisor runtime class. |
| `gvisor_verify` | Run a verification pod to confirm gVisor sandbox isolation is functional. |

### Exoskeleton (Phase 1)

| Tool | Description |
|------|-------------|
| `exo_status` | Return exoskeleton feature status including which backing services (Postgres, NATS, RustFS, SPIRE) are available. |
| `exo_registration` | Return exoskeleton registration details (Secret contents) for a workflow deployment. Sensitive values are redacted. |
| `exo_list` | List all workflows with exoskeleton registrations by scanning Secrets with the exoskeleton label across all namespaces. |

### Deploy Lifecycle

| Tool | Description |
|------|-------------|
| `wf_apply` | Apply arbitrary Kubernetes manifests as a named deployment using the dynamic client. Auto-injects PSA-compliant security contexts on Deployment/Job/CronJob manifests. Tracks resources by name label for garbage collection. |
| `wf_remove` | Remove all resources associated with a deployment name. |
| `wf_status` | Check the status of all resources in a named deployment. |

### Workflow Execution

| Tool | Description |
|------|-------------|
| `wf_run` | Trigger a deployed workflow by POSTing directly to the workflow's ClusterIP service `/run` endpoint. NetworkPolicy allows ingress from tentacular-system via namespaceSelector. Returns the JSON output with execution duration. Timeout configurable (default 120s, max 600s). No ephemeral pods are created. |

### Module Proxy

| Tool | Description |
|------|-------------|
| `proxy_status` | Check installation and readiness status of the in-cluster module proxy (esm.sh). Returns installed state, readiness, namespace, image, and storage type. |

### Workflow Health

| Tool | Description |
|------|-------------|
| `wf_health` | Get G/A/R (green/amber/red) health status of a single workflow deployment. Checks pod readiness and probes the engine `/health` endpoint. With `detail=true`, includes execution telemetry from `/health?detail=1`. |
| `wf_health_ns` | Aggregate G/A/R health status for all tentacular workflow deployments in a namespace. Returns per-workflow status and a summary with green/amber/red counts. |

### Cluster Health

| Tool | Description |
|------|-------------|
| `health_nodes` | Query node readiness, capacity, allocatable resources, and conditions. |
| `health_ns_usage` | Report namespace resource utilization vs. quota (CPU, memory, pod count). |
| `health_cluster_summary` | Overall cluster resource summary: total nodes, pods, CPU and memory capacity/requested. |

### Security Audit

| Tool | Description |
|------|-------------|
| `audit_rbac` | Scan namespace RBAC for over-permissioned roles (wildcard verbs, sensitive resources, escalation paths via bind/escalate/impersonate) with remediation suggestions. |
| `audit_netpol` | Verify NetworkPolicy coverage: default-deny presence, overly broad allow rules, cross-namespace ingress detection, with remediation suggestions. |
| `audit_psa` | Validate Pod Security Admission labels: enforce/audit/warn levels, privileged detection, level mismatch detection, with remediation suggestions. |

### Workflow Discovery

| Tool | Description |
|------|-------------|
| `wf_list` | List all workflow deployments across namespaces with owner, version, tags, and replica status. Supports filtering by owner and tag. |
| `wf_describe` | Describe a single workflow deployment with detailed metadata, annotations, images, and enrichment data from the associated ConfigMap. |

## Authentication

All requests to `/mcp` require a `Authorization: Bearer <token>` header. The `/healthz` endpoint is unauthenticated.

The server loads its expected token from the `TENTACULAR_MCP_TOKEN` environment variable. In the standard deployment, this is populated from the `tentacular-mcp-token` Kubernetes Secret via `secretKeyRef`.

### Generating a Token

```bash
# Generate a 32-byte hex token
openssl rand -hex 32
```

When deployed via Helm, pass the token with `--set auth.token=<token>`.
The token is stored in the `tentacular-mcp-token` Kubernetes Secret.

### Retrieving a Deployed Token

```bash
kubectl get secret tentacular-mcp-token -n tentacular-system \
  -o jsonpath='{.data.token}' | base64 -d
```

## Deployment

### Kustomize Deploy

```bash
kubectl apply -k deploy/manifests/
```

The kustomization deploys these resources in order:
1. `tentacular-system` Namespace
2. ServiceAccount + ClusterRole + ClusterRoleBinding
3. Auth Secret
4. Deployment (single replica, distroless non-root image)
5. ClusterIP Service (port 8080)

### Verifying the Deployment

```bash
# Check the pod is running
kubectl get pods -n tentacular-system

# Check logs
kubectl logs -n tentacular-system -l app.kubernetes.io/name=tentacular-mcp

# Port-forward and test
kubectl port-forward -n tentacular-system svc/tentacular-mcp 8080:8080 &
curl http://localhost:8080/healthz
```

### Rollback

```bash
kubectl rollout undo deployment/tentacular-mcp -n tentacular-system
```

Or scale to zero:

```bash
kubectl scale deployment/tentacular-mcp -n tentacular-system --replicas=0
```

No persistent state to clean up -- all state lives in Kubernetes objects.

## Development

### Building

```bash
make build         # Build binary to bin/tentacular-mcp
make docker-build  # Build Docker image
make lint          # Run golangci-lint and go vet
make clean         # Remove build artifacts
```

### Testing

Tests are organized in 4 tiers:

| Tier | Command | Requirements |
|------|---------|-------------|
| Unit | `make test-unit` | No cluster needed |
| Integration | `make test-integration` | kind cluster (auto-provisioned) |
| E2E | `make test-e2e` | Production k0s cluster; set `TENTACULAR_E2E_KUBECONFIG` |
| All | `make test-all` | Runs all tiers sequentially |

```bash
# Unit tests only (default)
make test

# Integration tests (sets up and tears down a kind cluster)
make test-integration

# E2E tests (requires a real cluster)
TENTACULAR_E2E_KUBECONFIG=/path/to/kubeconfig make test-e2e
```

### Project Structure

```
cmd/tentacular-mcp/main.go   Entry point with graceful shutdown
pkg/auth/                     Bearer token middleware
pkg/exoskeleton/              Exoskeleton subsystem (registrars, identity, injection)
pkg/guard/                    Self-protection namespace guard
pkg/k8s/                      Kubernetes client and operations
pkg/proxy/                    Module proxy reconciler and manifests
pkg/server/                   MCP server setup and HTTP handler
pkg/tools/                    36 MCP tool handlers (one file per group)
charts/tentacular-platform/   Umbrella Helm chart (recommended deployment)
charts/tentacular-mcp/        Standalone MCP server Helm chart
deploy/manifests/             Kustomize deployment manifests
test/integration/             Integration tests (kind cluster)
test/e2e/                     E2E tests (production cluster)
```

## Configuration

| Environment Variable | Default | Description |
|---------------------|---------|-------------|
| `LISTEN_ADDR` | `:8080` | Address and port the HTTP server binds to |
| `TENTACULAR_MCP_TOKEN` | (required) | Bearer auth token for client authentication |
| `TENTACULAR_MCP_NAMESPACE` | `tentacular-system` | Namespace the MCP server is installed in |
| `TENTACULAR_PROXY_RECONCILER_DISABLED` | (unset) | Set to `"true"` to disable the built-in esm-sh proxy reconciler (used by umbrella chart) |

## Security Model

### Pod Security Admission (PSA)

All namespaces created by `ns_create` are labeled with the `restricted` PSA profile:
- `pod-security.kubernetes.io/enforce: restricted`
- `pod-security.kubernetes.io/enforce-version: latest`

`wf_apply` auto-injects PSA-compliant security contexts on Deployment/Job/CronJob manifests (runAsNonRoot, readOnlyRootFilesystem, drop ALL capabilities, seccomp RuntimeDefault, /tmp emptyDir volume). Preserves user-specified values.

### Network Policies

Every created namespace gets:
- A **default-deny** NetworkPolicy blocking all ingress and egress
- A **DNS-allow** NetworkPolicy permitting UDP/TCP egress on port 53 to kube-system/kube-dns

### RBAC Scoping

The server's ClusterRole is scoped to exactly the verbs and resources needed by the 36 tools. It is significantly narrower than `cluster-admin`. Key constraints:
- Read-only access to nodes, storage classes, runtime classes, CRDs
- Create/delete for pods (gVisor verification only)
- Namespaced CRUD for resources managed by tool handlers
- `selfsubjectaccessreviews` for preflight RBAC validation

### Self-Protection

`guard.CheckNamespace()` runs before every namespace-scoped tool. It rejects any operation targeting `tentacular-system`, `tentacular-support`, `tentacular-exoskeleton`, `kube-system`, `kube-public`, `kube-node-lease`, and `default`, preventing the server from modifying its own deployment or critical cluster namespaces.

### Container Security

The Deployment runs with:
- `runAsNonRoot: true` (UID 65534)
- `readOnlyRootFilesystem: true`
- `allowPrivilegeEscalation: false`
- All capabilities dropped
- `RuntimeDefault` seccomp profile
- Distroless base image (`gcr.io/distroless/static-debian12:nonroot`)

## CLI Integration

The tentacular CLI (`tntc`) delegates all cluster operations
to this MCP server. MCP connection details are configured
per-environment in `~/.tentacular/config.yaml` or via
`TNTC_MCP_ENDPOINT` / `TNTC_MCP_TOKEN` environment variables.
All CLI commands automatically use the MCP server -- no
per-command flags needed.

The CLI has no direct Kubernetes API access. All cluster-facing
commands (deploy, run, list, status, logs, undeploy, audit,
cluster check, cluster profile) route through MCP.

On startup, the MCP server's proxy reconciler auto-creates the esm.sh module proxy in `tentacular-support`.

### MCP Tools Used by CLI

| CLI Command | MCP Tool(s) |
|-------------|-------------|
| `cluster check` | `cluster_preflight` |
| `cluster profile` | `cluster_profile` |
| `deploy` | `wf_apply`, `ns_create` |
| `run` | `wf_run` |
| `list` | `wf_list` |
| `status` | `wf_status` |
| `logs` | `wf_logs` |
| `undeploy` | `wf_remove` |
| `audit` | `audit_rbac`, `audit_netpol`, `audit_psa` |

See the [MCP Server Setup guide](https://randybias.github.io/tentacular-docs/guides/mcp-server-setup/) for full details.

## Contributing

1. Follow the existing code patterns -- tool handlers are standalone functions that take `*k8s.Client` and return structured results
2. Add new tools in `pkg/tools/` following the one-file-per-group convention
3. Register tools through `pkg/tools/register.go` -- the wrapper handles JSON unmarshaling, guard checks, and MCP protocol concerns
4. Write unit tests alongside your code; add integration tests for K8s interactions
5. Run `make lint` before submitting changes
6. Use conventional commits for all commit messages

## License

Copyright (c) 2025-2026 Mirantis, Inc. All rights reserved. See [LICENSE](LICENSE) for details.
