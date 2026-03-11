# Exoskeleton Platform: Umbrella Helm Chart and MCP Integration

## Overview

Build a unified Helm umbrella chart (`charts/tentacular-platform/`) that deploys the complete Tentacular platform in a single `helm install`, then implement the exoskeleton control plane in the MCP server to provide automatic per-tentacle scoped access to PostgreSQL and NATS.

The umbrella chart embeds official subchart dependencies (bitnami/postgresql, nats/nats, jetstack/cert-manager) alongside the existing tentacular-mcp chart, esm-sh module proxy, namespace provisioning, flexible ingress (AWS ALB, Istio, Traefik/nginx, or NodePort), and TLS. Every component has an enable/disable toggle. Future components (RustFS, Keycloak, SPIRE) have disabled placeholder sections for follow-up work. All container images and subchart dependencies MUST support multi-arch (linux/amd64 + linux/arm64).

After the chart is complete, the MCP server gains an exoskeleton subsystem: feature-flag parsing, deterministic identity compilation, per-service registrars (Postgres, NATS), credential injection into tentacle pods, and orchestration wired into the existing wf_apply/wf_remove deploy path.

Reference architecture documents (informational, not prescriptive):
- `tentacular-platform-plan-v3.md` in the tentacular repo — contract v2, identity model, NATS subject schema
- `exoskeleton-architecture.md` in the tentacular repo — MCP subsystems, registration lifecycle, runtime injection
- `exoskeleton-deployment.md` in the tentacular repo — service-by-service deployment, eastus-dev cluster notes

## Context

- Existing Helm chart: `charts/tentacular-mcp/` (deployment, RBAC, service, secret)
- MCP server entry point: `cmd/tentacular-mcp/main.go`
- MCP tools registration: `pkg/tools/register.go` (22 tools across 12 files)
- Deploy handler: `pkg/tools/deploy.go` (wf_apply, wf_remove, wf_status)
- K8s client: `pkg/k8s/client.go` (typed + dynamic + rest config)
- Auth middleware: `pkg/auth/auth.go` (bearer token, file-mounted)
- Namespace guard: `pkg/guard/guard.go` (blocks system namespaces)
- Test helpers: `test/testutil/fakeclient.go` (fake K8s clientset)
- Tentacular CLI repo: mounted at a sibling path (e.g., `../tentacular/`)
- Reference cluster: single-node k0s ARM64 with exoskeleton services deployed individually
- All images must be multi-arch (linux/amd64 + linux/arm64) — the reference cluster is ARM64 but production may be AMD64
- Docker builds use `docker buildx` with `--platform linux/amd64,linux/arm64`
- Existing tentacular-mcp Dockerfile: multi-stage, golang builder + distroless runtime, CGO_ENABLED=0
- Existing tentacular engine Dockerfile: multi-stage, Deno + distroless runtime
- CI: `.github/workflows/docker-build.yml` builds and pushes multi-arch images to GHCR
- No exoskeleton code exists in this repo yet

## Validation Commands

- `helm dependency update charts/tentacular-platform/ 2>/dev/null; helm lint charts/tentacular-platform/`
- `helm dependency update charts/tentacular-platform/ 2>/dev/null; helm template test charts/tentacular-platform/ -f charts/tentacular-platform/ci/test-values.yaml > /dev/null`
- `go test ./pkg/... -v -count=1 -race`
- `go vet ./...`
- `golangci-lint run`
- `go build -o /dev/null ./cmd/tentacular-mcp/`

## Development Approach

- **Testing approach**: Regular (code first, then tests)
- Complete each task fully before moving to the next
- CRITICAL: every code task MUST include new/updated tests
- CRITICAL: all tests must pass before starting next task
- Pull latest from remote before modifying existing files to avoid conflicts with concurrent work
- Helm chart tasks (1-6) create new files and are lower conflict risk
- Task 7 audits and improves the Docker build pipeline for reliable multi-arch support
- Code tasks (8-15) modify existing files and must check for upstream changes first
- ALL subchart dependencies and container images must publish multi-arch manifests (linux/amd64 + linux/arm64)

## Implementation Steps

### Task 1: Create umbrella Helm chart scaffolding with namespace management

Create the umbrella chart directory structure with Chart.yaml, global values, helper templates, and namespace templates with security defaults (PSA labels, optional default-deny NetworkPolicies). This establishes the chart skeleton that subsequent tasks build on.

**Files:**
- Create: `charts/tentacular-platform/Chart.yaml`
- Create: `charts/tentacular-platform/values.yaml`
- Create: `charts/tentacular-platform/templates/_helpers.tpl`
- Create: `charts/tentacular-platform/templates/namespaces.yaml`
- Create: `charts/tentacular-platform/templates/networkpolicies.yaml`
- Create: `charts/tentacular-platform/ci/test-values.yaml`
- Create: `charts/tentacular-platform/.helmignore`

- [x] Create `charts/tentacular-platform/Chart.yaml`: apiVersion v2, name tentacular-platform, type application, version 0.1.0, appVersion "0.2.0". Add metadata (description, home, sources, maintainers, keywords). Add empty `dependencies: []` placeholder — subcharts are added in subsequent tasks.
- [x] Create `charts/tentacular-platform/.helmignore` with standard patterns (*.tgz, .git, etc.).
- [x] Create `charts/tentacular-platform/values.yaml` with sections: `global` (domain, imagePullSecrets), `namespaces` (system/exoskeleton/support each with create bool and name), `networkPolicies` (enabled: true — on by default, can be switched off), `postgresql` (enabled: true), `nats` (enabled: true), `cert-manager` (enabled: false — many clusters have it pre-installed), `tentacular-mcp` (enabled: true), `esm-sh` (enabled: true), `ingress` section with `mode` field supporting values: `none` (default — no ingress, use port-forward or NodePort), `nodeport` (expose MCP via NodePort for simple/test deployments), `ingress` (standard Kubernetes Ingress for Traefik, nginx, or AWS ALB), `istio` (Istio Gateway + VirtualService), `alb-istio` (AWS ALB fronting Istio). Plus disabled placeholders for future components: `rustfs` (enabled: false), `keycloak` (enabled: false), `spire` (enabled: false).
- [x] Create `charts/tentacular-platform/templates/_helpers.tpl` with standard Helm helpers: chart name, fullname, common labels, selector labels. Add namespace name helpers that resolve `namespaces.system.name`, `namespaces.exoskeleton.name`, `namespaces.support.name` with defaults (tentacular-system, tentacular-exoskeleton, tentacular-support).
- [x] Create `charts/tentacular-platform/templates/namespaces.yaml` rendering conditional Namespace resources for system, exoskeleton, and support. Each conditional on its `namespaces.<type>.create` toggle. Apply PSA labels: `pod-security.kubernetes.io/enforce: restricted`, `pod-security.kubernetes.io/warn: restricted`. Add `tentacular.io/system: "true"` annotation to all three.
- [x] Create `charts/tentacular-platform/templates/networkpolicies.yaml`: all policies conditional on `networkPolicies.enabled` (defaults to true). Contents: default-deny ingress+egress NetworkPolicy for each namespace (also conditional on namespace creation). Allow-dns egress rule (UDP 53 to kube-dns). Allow ingress from tentacular-system to tentacular-exoskeleton (for MCP server to reach backing services). Allow ingress from any `tent-*` namespace to tentacular-exoskeleton (for tentacle pods to reach their scoped services). When `networkPolicies.enabled` is false, no NetworkPolicy resources are rendered at all.
- [x] Create `charts/tentacular-platform/ci/test-values.yaml` with minimal overrides for CI testing: all core components enabled, a dummy auth token for tentacular-mcp, small resource requests, ingress mode `none`, networkPolicies enabled.
- [x] Verify: `helm lint charts/tentacular-platform/` passes with no errors.

### Task 2: Add PostgreSQL as embedded subchart dependency

Add bitnami/postgresql as a Helm subchart dependency with defaults for the tentacular database, admin role, and bootstrap SQL that grants the MCP server's admin role permission to create per-tentacle roles and schemas.

**Files:**
- Modify: `charts/tentacular-platform/Chart.yaml`
- Modify: `charts/tentacular-platform/values.yaml`
- Modify: `charts/tentacular-platform/ci/test-values.yaml`
- Create: `charts/tentacular-platform/templates/postgres-init-configmap.yaml`

- [x] Add bitnami/postgresql dependency to Chart.yaml: `name: postgresql`, repository `oci://registry-1.docker.io/bitnamicharts`, version constraint `~16.x` (must have multi-arch images: linux/amd64 + linux/arm64), condition `postgresql.enabled`. Verify the chosen version publishes multi-arch manifests before pinning.
- [x] Add postgresql configuration to values.yaml under the `postgresql:` key. Set: `auth.database: tentacular`, `auth.username: tentacular_admin`, `auth.password: ""` (user must provide or use existingSecret), `auth.existingSecret: ""`, `primary.persistence.size: 5Gi`, `primary.persistence.storageClass: ""` (use cluster default). Note in a YAML comment that `namespaceOverride` is set automatically by the chart.
- [x] Create `charts/tentacular-platform/templates/postgres-init-configmap.yaml`: ConfigMap in the exoskeleton namespace containing an init SQL script that runs `ALTER ROLE tentacular_admin CREATEROLE;` (idempotent — ALTER is safe to re-run). Conditional on `postgresql.enabled`. Reference this ConfigMap from postgresql values via `primary.initdb.scriptsConfigMap`.
- [x] Update ci/test-values.yaml: set postgresql.auth.password to a test value so template rendering succeeds.
- [x] Run `helm dependency update charts/tentacular-platform/` to fetch the subchart tarball.
- [x] Verify: `helm template test charts/tentacular-platform/ -f charts/tentacular-platform/ci/test-values.yaml` renders a PostgreSQL StatefulSet.
- [x] Verify: helm lint passes.

### Task 3: Add NATS as embedded subchart dependency

Add nats/nats as a Helm subchart dependency with JetStream enabled, token auth, and persistent storage defaults.

**Files:**
- Modify: `charts/tentacular-platform/Chart.yaml`
- Modify: `charts/tentacular-platform/values.yaml`
- Modify: `charts/tentacular-platform/ci/test-values.yaml`

- [x] Add nats/nats dependency to Chart.yaml: `name: nats`, repository `https://nats-io.github.io/k8s/helm/charts/`, version constraint for a stable 1.x release of the chart, condition `nats.enabled`.
- [x] Add nats configuration to values.yaml under the `nats:` key. Set: `config.jetstream.enabled: true`, `config.jetstream.fileStore.pvc.size: 5Gi`, single replica for dev (replicaCount or similar). Add auth section with token-based auth (config.merge or auth block depending on chart version). Add a YAML comment noting that JWT-based auth is planned for production.
- [x] Update ci/test-values.yaml with NATS test token.
- [x] Run `helm dependency update charts/tentacular-platform/`.
- [x] Verify: `helm template test charts/tentacular-platform/ -f charts/tentacular-platform/ci/test-values.yaml` renders a NATS StatefulSet.
- [x] Verify: helm lint passes.

### Task 4: Add cert-manager as optional embedded subchart dependency

Add jetstack/cert-manager as an optional subchart (disabled by default). Add ClusterIssuer templates for Let's Encrypt and Certificate templates for platform endpoints. Support clusters that already have cert-manager by allowing ClusterIssuer/Certificate creation independently of the subchart.

**Files:**
- Modify: `charts/tentacular-platform/Chart.yaml`
- Modify: `charts/tentacular-platform/values.yaml`
- Modify: `charts/tentacular-platform/ci/test-values.yaml`
- Create: `charts/tentacular-platform/templates/clusterissuer.yaml`
- Create: `charts/tentacular-platform/templates/certificates.yaml`

- [x] Add jetstack/cert-manager dependency to Chart.yaml: `name: cert-manager`, repository `https://charts.jetstack.io`, stable version with multi-arch images (linux/amd64 + linux/arm64), condition `cert-manager.enabled`.
- [x] Add cert-manager section to values.yaml: `enabled: false` (off by default — most clusters have it pre-installed). When enabled: `crds.enabled: true` to install CRDs. Add a separate `tls` section at the top level: `tls.clusterIssuers.create: false`, `tls.clusterIssuers.email: ""`, `tls.clusterIssuers.production: true` (use production LE or staging), `tls.certificates.mcp.create: false`, `tls.certificates.mcp.secretName: mcp-tls`, `tls.certificates.auth.create: false`, `tls.certificates.auth.secretName: auth-tls`.
- [x] Create `charts/tentacular-platform/templates/clusterissuer.yaml`: Let's Encrypt ClusterIssuer (production and/or staging based on toggle). Uses HTTP-01 solver. Conditional on `tls.clusterIssuers.create`. Works regardless of whether cert-manager was installed by this chart or pre-exists.
- [x] Create `charts/tentacular-platform/templates/certificates.yaml`: Certificate resources for MCP endpoint (`mcp.<global.domain>`) and auth endpoint (`auth.<global.domain>`). Conditional on their respective create toggles. Reference the ClusterIssuer by name.
- [x] Update ci/test-values.yaml: keep cert-manager disabled, keep tls resources disabled (they need CRDs present to template).
- [x] Run `helm dependency update charts/tentacular-platform/`.
- [x] Verify: helm lint passes. Verify: `helm template` succeeds with cert-manager disabled. Create a separate ci/tls-values.yaml if needed to test with TLS enabled (skipping CRD-dependent resources if CRDs aren't installed).

### Task 5: Integrate tentacular-mcp subchart and esm-sh module proxy

Add the existing tentacular-mcp chart as a subchart dependency and create templates for the esm-sh module proxy Deployment and Service in the support namespace. Create an exoskeleton Secret template that wires backing-service connection details to the MCP server.

**Files:**
- Modify: `charts/tentacular-platform/Chart.yaml`
- Modify: `charts/tentacular-platform/values.yaml`
- Modify: `charts/tentacular-platform/ci/test-values.yaml`
- Create: `charts/tentacular-platform/templates/exoskeleton-secret.yaml`
- Create: `charts/tentacular-platform/templates/esm-sh-deployment.yaml`
- Create: `charts/tentacular-platform/templates/esm-sh-service.yaml`
- Create: `charts/tentacular-platform/templates/esm-sh-networkpolicy.yaml`

- [x] Add tentacular-mcp as subchart dependency in Chart.yaml: `name: tentacular-mcp`, `repository: "file://../tentacular-mcp"` (local reference for development; YAML comment noting OCI registry path for releases), condition `tentacular-mcp.enabled`.
- [x] Add tentacular-mcp section to values.yaml: `enabled: true`. Wire through auth.token, image settings, service type. Add `exoskeleton.enabled`, `exoskeleton.existingSecret` (name of the Secret containing backing-service connection details) pointing to the exoskeleton-secret template. Add environment variable overrides for all TENTACULAR_EXOSKELETON_* flags.
- [x] Create `charts/tentacular-platform/templates/exoskeleton-secret.yaml`: generates a Secret in the system namespace with keys matching the MCP server's expected env vars: postgres-host, postgres-port, postgres-database, postgres-user, postgres-password, postgres-sslmode, nats-url, nats-token. Values derived from the postgresql and nats subchart values (service names, credentials). Conditional on any exoskeleton service being enabled. Add label `tentacular.io/exoskeleton-config: "true"`.
- [x] Wire the exoskeleton Secret into the tentacular-mcp subchart values: set `extraEnvFrom` or `extraVolumes`/`extraVolumeMounts` (check what the existing tentacular-mcp chart supports; if it doesn't support extra env/volume injection, note that the chart needs a small enhancement first and add a TODO).
- [x] Create `charts/tentacular-platform/templates/esm-sh-deployment.yaml`: Deployment in support namespace for esm-sh proxy. Image: `ghcr.io/esm-dev/esm.sh:v136` (configurable). Security context: runAsNonRoot, readOnlyRootFilesystem where possible, drop ALL capabilities. Resource defaults. Conditional on `esm-sh.enabled`.
- [x] Create `charts/tentacular-platform/templates/esm-sh-service.yaml`: ClusterIP Service on port 8080 for the proxy.
- [x] Create `charts/tentacular-platform/templates/esm-sh-networkpolicy.yaml`: conditional on both `esm-sh.enabled` AND `networkPolicies.enabled`. Allow inbound from any namespace with `tentacular.io/managed-by` label (workflow namespaces), allow outbound to internet (for module resolution), deny all else.
- [x] Add esm-sh section to values.yaml: `enabled: true`, `image.repository`, `image.tag`, `replicas: 1`, `resources` (requests/limits), `persistence.enabled: false`, `persistence.size: 1Gi`.
- [x] Update ci/test-values.yaml with appropriate overrides.
- [x] Verify: `helm template` renders MCP deployment, esm-sh deployment, and exoskeleton Secret correctly.
- [x] Verify: helm lint passes.

### Task 6: Add multi-backend ingress support and chart documentation

Implement configurable ingress with support for multiple backends: standard Kubernetes Ingress (Traefik, nginx, AWS ALB), Istio Gateway + VirtualService, AWS ALB fronting Istio, and NodePort for simple/test deployments. Create example values files and chart README.

**Files:**
- Modify: `charts/tentacular-platform/values.yaml`
- Modify: `charts/tentacular-platform/ci/test-values.yaml`
- Create: `charts/tentacular-platform/templates/ingress.yaml`
- Create: `charts/tentacular-platform/templates/istio.yaml`
- Create: `charts/tentacular-platform/templates/nodeport.yaml`
- Create: `charts/tentacular-platform/README.md`
- Create: `charts/tentacular-platform/ci/dev-values.yaml`
- Create: `charts/tentacular-platform/ci/prod-values.yaml`
- Create: `charts/tentacular-platform/ci/aws-values.yaml`

- [x] Add full ingress section to values.yaml with `ingress.mode` controlling the backend. Supported modes: `none` (default — no external exposure, use kubectl port-forward), `nodeport` (MCP Service becomes NodePort, configurable nodePort number), `ingress` (standard Kubernetes Ingress resource — works with Traefik, nginx-ingress, AWS ALB Ingress Controller), `istio` (Istio Gateway + VirtualService, requires Istio CRDs), `alb-istio` (AWS ALB annotations on Istio Gateway for end-to-end AWS integration). Common fields: `ingress.mcp.hostname`, `ingress.auth.enabled`, `ingress.auth.hostname`, `ingress.tls.enabled`, `ingress.tls.secretName`. Mode-specific fields: `ingress.className` (for ingress mode — traefik, nginx, alb), `ingress.annotations` (freeform, for ALB-specific annotations like `alb.ingress.kubernetes.io/scheme: internet-facing`), `ingress.istio.gateway.name`, `ingress.istio.gateway.servers` (port/TLS config), `ingress.nodeport.mcp` (port number for MCP NodePort).
- [x] Create `charts/tentacular-platform/templates/ingress.yaml`: standard Kubernetes Ingress resource, rendered only when `ingress.mode == "ingress"`. Support configurable `ingressClassName` for Traefik, nginx, or ALB. Apply user-provided annotations (enables AWS ALB-specific annotations like target-type, scheme, certificate-arn). Wire TLS when enabled. Route to MCP service and optionally auth service.
- [x] Create `charts/tentacular-platform/templates/istio.yaml`: Istio Gateway + VirtualService resources, rendered only when `ingress.mode` is `istio` or `alb-istio`. Gateway configures server ports, hosts, and TLS. VirtualService routes traffic to MCP service (and optionally auth service). When mode is `alb-istio`, add AWS ALB annotations to the Gateway's associated Service (or add a separate Service annotation template) so the ALB fronts the Istio ingress gateway.
- [x] Create `charts/tentacular-platform/templates/nodeport.yaml`: when `ingress.mode == "nodeport"`, create a Service override or patch that sets the tentacular-mcp Service type to NodePort with the configured port. Keep it simple — just expose the MCP server, no TLS termination (suitable for dev/test behind SSH tunnel or VPN).
- [x] Create `charts/tentacular-platform/ci/dev-values.yaml`: single-node dev profile — all core components enabled (postgresql, nats, tentacular-mcp, esm-sh), cert-manager disabled, `ingress.mode: nodeport`, networkPolicies enabled, minimal resource requests, test auth token.
- [x] Create `charts/tentacular-platform/ci/prod-values.yaml`: production profile — all core components enabled, cert-manager enabled, `ingress.mode: ingress`, `ingress.className: nginx`, TLS enabled, networkPolicies enabled, higher resource limits, placeholder auth token and domain.
- [x] Create `charts/tentacular-platform/ci/aws-values.yaml`: AWS profile — all core components enabled, cert-manager disabled (use ACM), `ingress.mode: alb-istio` (or `ingress` with ALB className), ALB-specific annotations (scheme, certificate-arn, target-type), networkPolicies enabled, placeholder domain.
- [x] Create `charts/tentacular-platform/README.md`: brief description, prerequisites (Kubernetes 1.28+, Helm 3.x, kubectl; plus Istio if using istio/alb-istio modes; AWS LB Controller if using ALB), quickstart installation commands, configuration reference table listing all top-level values with types and defaults, ingress mode comparison table (none/nodeport/ingress/istio/alb-istio with when-to-use guidance), component toggle reference (including networkPolicies.enabled), examples for each ingress mode, upgrade and uninstall instructions. Note that cert-manager is optional if already installed.
- [x] Verify: `helm lint charts/tentacular-platform/` passes.
- [x] Verify: `helm template` succeeds with default values (mode: none), dev-values (nodeport), prod-values (ingress), and aws-values (alb-istio). For istio and alb-istio modes, template rendering should succeed even without Istio CRDs installed (use `{{- if .Capabilities.APIVersions.Has "networking.istio.io/v1" }}` guard or similar, or accept that these templates require the CRDs and document it).
- [x] Run full project test suite (`go test ./pkg/... -v -count=1 -race`) to confirm no regressions from any incidental changes.

### Task 7: Audit and improve Docker multi-arch build pipeline

Review the Docker image build pipeline for both tentacular-mcp and tentacular (engine) to ensure reliable multi-arch (linux/amd64 + linux/arm64) image builds. Fix any gaps, add build verification, and ensure CI produces correct multi-arch manifests on every push.

**Files:**
- Review and modify: `Dockerfile`
- Review and modify: `Makefile`
- Review and modify: `.github/workflows/docker-build.yml`
- Review and modify: `.github/workflows/ci.yml`
- Review (sibling repo): `../tentacular/Dockerfile`, `../tentacular/Makefile`, `../tentacular/.github/workflows/build-engine.yml`

- [x] Pull latest from remote before starting.
- [x] Audit `Dockerfile` in this repo (tentacular-mcp): verify the builder stage uses a multi-arch base image (e.g., `golang:1.25-bookworm` which is multi-arch). Verify `CGO_ENABLED=0` is set (required for static cross-compilation). Verify the runtime base image (`gcr.io/distroless/static-debian12:nonroot`) publishes multi-arch manifests. Verify no architecture-specific instructions (no hardcoded GOARCH, no platform-specific binary paths).
- [x] Audit `Makefile` build targets: verify `make build` uses `docker buildx build --platform linux/amd64,linux/arm64 --push`. Verify `make build-local` builds for the host platform only (for fast local iteration). If `make dev-release` exists, verify it also builds multi-arch. If any target builds single-arch only, fix it or clearly document it as local-only.
- [x] Audit `.github/workflows/docker-build.yml`: verify it uses `docker/build-push-action` with `platforms: linux/amd64,linux/arm64`. Verify buildx is set up with `docker/setup-buildx-action`. Verify QEMU is set up for cross-platform emulation with `docker/setup-qemu-action`. Verify the workflow pushes to GHCR with proper tags (`:latest`, `:v*`, `:sha-*`). If any of these are missing, add them.
- [x] Audit the sibling tentacular repo's engine image build: review `../tentacular/Dockerfile` for multi-arch support (Deno base image must be multi-arch). Review `../tentacular/.github/workflows/build-engine.yml` for `--platform linux/amd64,linux/arm64`. Review `../tentacular/Makefile` build target. Document any issues found as GitHub issues or fix them if the sibling repo is writable.
- [x] Add a build verification step: after multi-arch build, use `docker buildx imagetools inspect <image>` to verify the manifest list contains both amd64 and arm64 entries. Add this check to the Makefile as `make verify-multiarch` and consider adding it to CI.
- [x] Verify the GoReleaser config (`.goreleaser.yaml`) cross-compiles the CLI binary for linux/darwin x amd64/arm64. This is for the CLI binary distribution, separate from Docker images.
- [x] If any fixes were made, run all validation commands — all tests pass, lint clean, builds successfully.

### Task 8: Implement ExoskeletonConfig and IdentityCompiler

Add the exoskeleton configuration system (feature flags, per-service connection details parsed from environment) and the deterministic identity compiler that maps (namespace, workflow) to Postgres role/schema, NATS subject prefix, and other service-specific identifiers.

**Files:**
- Create: `pkg/exoskeleton/config.go`
- Create: `pkg/exoskeleton/config_test.go`
- Create: `pkg/exoskeleton/identity.go`
- Create: `pkg/exoskeleton/identity_test.go`
- Modify: `cmd/tentacular-mcp/main.go`

- [x] Pull latest from remote (`git pull --rebase origin main`) before starting.
- [x] Create `pkg/exoskeleton/config.go`: define `ExoskeletonConfig` struct with fields: `Enabled bool`, `PostgresEnabled bool`, `NATSEnabled bool`, `RustFSEnabled bool` (for future use), `CleanupOnUndeploy bool`. Nested structs: `PostgresConfig` (Host, Port, Database, User, Password, SSLMode), `NATSConfig` (URL, Token), `RustFSConfig` (Endpoint, AccessKey, SecretKey, Bucket, Region — for future use). Add `LoadFromEnv() (*ExoskeletonConfig, error)` that reads `TENTACULAR_EXOSKELETON_ENABLED` and per-service env vars (`TENTACULAR_EXOSKELETON_POSTGRES_ENABLED`, `TENTACULAR_POSTGRES_ADMIN_HOST`, etc.). Add `Validate() error` that checks: if a service is enabled, its required connection fields must be non-empty.
- [x] Create `pkg/exoskeleton/config_test.go`: test `LoadFromEnv` with full config (all services enabled), partial config (only postgres), disabled exoskeleton (returns config with Enabled=false), validation failures (postgres enabled but host missing). Use `t.Setenv` for env manipulation.
- [x] Create `pkg/exoskeleton/identity.go`: define `Identity` struct with `Namespace`, `Workflow`, `PostgresRole`, `PostgresSchema`, `NATSSubjectPrefix`, `NATSPrincipal`, `RustFSPrefix`, `CanonicalPrincipal` fields. Add `CompileIdentity(namespace, workflow string) Identity`. Rules: lowercase all, replace hyphens and non-alphanumeric chars with underscores for Postgres (prefix `tn_`), use dots for NATS (prefix `tentacular.`), use path separators for RustFS (prefix `ns/<ns>/tentacles/<wf>/`). If `tn_<ns>_<wf>` exceeds 63 chars, truncate and append a short hash suffix. NATS principal: `<ns>.<wf>`.
- [x] Create `pkg/exoskeleton/identity_test.go`: test standard identity compilation, long names (triggers truncation), special characters, determinism (same input always produces same output), edge cases (single-char names, names at exactly 63 chars). Test that NATS and Postgres identifiers are derived from the same inputs consistently.
- [x] Wire into `cmd/tentacular-mcp/main.go`: call `exoskeleton.LoadFromEnv()` at startup, log enabled/disabled status and which services are active. Pass config to server setup (store in a struct or pass to server constructor). When `Enabled=false`, log "exoskeleton disabled" and skip all setup.
- [x] Run all validation commands — all tests pass, lint clean, builds successfully.

### Task 9: Implement PostgresRegistrar

Create the Postgres registrar that provisions per-tentacle roles and schemas using the admin connection, handles idempotent re-registration, and performs destructive cleanup on unregistration.

**Files:**
- Create: `pkg/exoskeleton/postgres.go`
- Create: `pkg/exoskeleton/postgres_test.go`

- [ ] Pull latest from remote before starting.
- [ ] Define a `DBExecutor` interface in `pkg/exoskeleton/postgres.go` with methods: `ExecContext(ctx, query, args...)`, `QueryRowContext(ctx, query, args...)`. This allows unit testing with a mock instead of a real database. Add a constructor `NewPostgresRegistrar(config PostgresConfig)` that creates the registrar and a `Connect(ctx) error` method that dials the admin connection.
- [ ] Implement `Register(ctx context.Context, id Identity) (*PostgresRegistration, error)`: generate a strong random password (32 bytes, hex-encoded), execute `CREATE ROLE IF NOT EXISTS <role> WITH LOGIN PASSWORD '<pw>'`, execute `CREATE SCHEMA IF NOT EXISTS <schema> AUTHORIZATION <role>`, execute `GRANT USAGE, CREATE ON SCHEMA <schema> TO <role>`. Return `PostgresRegistration` struct with Role, Schema, Password, Host, Port, Database.
- [ ] Implement `ReRegister(ctx context.Context, id Identity) (*PostgresRegistration, error)`: verify role exists (query pg_roles), verify schema exists (query information_schema.schemata), verify grants are correct. If role doesn't exist, create it (handles drift). Do NOT drop or recreate schema. Optionally rotate password if a rotation flag is set. Return updated registration.
- [ ] Implement `Unregister(ctx context.Context, id Identity) error`: execute `DROP SCHEMA IF EXISTS <schema> CASCADE`, execute `DROP ROLE IF EXISTS <role>`. Log what was dropped. Handle "role cannot be dropped because some objects depend on it" errors by logging a warning.
- [ ] Create `pkg/exoskeleton/postgres_test.go`: implement a mock DBExecutor that records executed SQL statements. Test: Register generates correct SQL sequence and returns valid registration, ReRegister does not issue DROP statements, Unregister issues DROP CASCADE, password generation produces unique values, error handling for connection failures and permission errors.
- [ ] Run all validation commands — all tests pass, lint clean.

### Task 10: Implement NATSRegistrar

Create the NATS registrar that provisions per-tentacle credentials with scoped publish/subscribe permissions on the tentacle's subject prefix.

**Files:**
- Create: `pkg/exoskeleton/nats.go`
- Create: `pkg/exoskeleton/nats_test.go`

- [ ] Pull latest from remote before starting.
- [ ] Define a `NATSAdmin` interface in `pkg/exoskeleton/nats.go` for NATS management operations (user creation, permission setting, user deletion) to allow mocking in tests. The v1 implementation uses token-based auth — the registrar generates a scoped token or username+password pair for the tentacle. Add constructor `NewNATSRegistrar(config NATSConfig)`.
- [ ] Implement `Register(ctx context.Context, id Identity) (*NATSRegistration, error)`: derive subject scope from identity (publish: `tentacular.<ns>.<wf>.>`, subscribe: `tentacular.<ns>.<wf>.>`). For v1 with simple token auth, generate a unique token for the tentacle and store the permission mapping. Return `NATSRegistration` struct with URL, Token/Credentials, SubjectPrefix, Principal.
- [ ] Implement `ReRegister(ctx context.Context, id Identity) (*NATSRegistration, error)`: verify existing credentials are valid, optionally reissue. Preserve any JetStream durable state.
- [ ] Implement `Unregister(ctx context.Context, id Identity) error`: revoke/invalidate the tentacle's credentials. Log what was cleaned up.
- [ ] Create `pkg/exoskeleton/nats_test.go`: mock NATSAdmin interface. Test: Register creates correct subject scope matching IdentityCompiler output, ReRegister preserves state, Unregister revokes credentials, error handling for unreachable NATS.
- [ ] Run all validation commands — all tests pass, lint clean.

### Task 11: Implement CredentialInjector and ExoskeletonController

Create the credential injector that materializes Kubernetes Secrets for tentacle pods with the `<dep>.<field>` key convention, and the top-level controller that orchestrates the full registration lifecycle across all enabled services.

**Files:**
- Create: `pkg/exoskeleton/credential.go`
- Create: `pkg/exoskeleton/credential_test.go`
- Create: `pkg/exoskeleton/controller.go`
- Create: `pkg/exoskeleton/controller_test.go`

- [ ] Pull latest from remote before starting.
- [ ] Create `pkg/exoskeleton/credential.go` with `CredentialInjector` struct (holds K8s client interface). Method `Inject(ctx, namespace, workflow string, pgReg *PostgresRegistration, natsReg *NATSRegistration) error`: build a K8s Secret named `tentacular-exoskeleton-<workflow>` in the tentacle's namespace. Keys follow `<dep>.<field>` convention: `tentacular-postgres.host`, `.port`, `.database`, `.user`, `.password`, `.schema`, `.protocol` (value: "postgresql"); `tentacular-nats.url`, `.token`, `.protocol` (value: "nats"). Apply labels: `tentacular.io/release: <workflow>`, `tentacular.io/exoskeleton: "true"`. Use Create if not exists, Update if exists.
- [ ] Add `Remove(ctx, namespace, workflow string) error`: delete the `tentacular-exoskeleton-<workflow>` Secret. Return nil if not found (idempotent).
- [ ] Create `pkg/exoskeleton/credential_test.go`: use fake K8s clientset. Test Secret creation with correct keys and labels, test update (existing Secret gets replaced), test Remove (deletes Secret), test Remove when Secret doesn't exist (no error), test with only Postgres registration (NATS keys omitted), test with only NATS registration.
- [ ] Create `pkg/exoskeleton/controller.go` with `ExoskeletonController` struct (holds config, registrars, credential injector). Method `Register(ctx, namespace, workflow string, deps []string) error`: compile identity via `CompileIdentity`, check which `tentacular-*` deps are in the list, call each enabled registrar, call credential injector. Method `Unregister(ctx, namespace, workflow string) error`: call credential injector Remove, then unregister from each enabled service. Add helper `DetectExoskeletonDeps(workflowYAML string) ([]string, error)` that parses workflow.yaml content and returns dependency names matching the `tentacular-*` prefix.
- [ ] Create `pkg/exoskeleton/controller_test.go`: use mock registrars and fake K8s client. Test full Register flow (both services called, credentials injected), test Register with only Postgres dep (NATS registrar not called), test Register with disabled service that workflow depends on (returns clear error), test Unregister flow (credentials removed, both services unregistered), test Unregister with partial failure (logs warning, continues cleanup), test DetectExoskeletonDeps parsing with various workflow YAML inputs.
- [ ] Run all validation commands — all tests pass, lint clean.

### Task 12: Wire exoskeleton into wf_apply and wf_remove deploy path

Integrate the ExoskeletonController into the existing deployment tools so that tentacle registration happens automatically during deploy (wf_apply) and unregistration during undeploy (wf_remove). Maintain full backward compatibility when exoskeleton is disabled.

**Files:**
- Modify: `cmd/tentacular-mcp/main.go`
- Modify: `pkg/server/server.go`
- Modify: `pkg/tools/deploy.go`
- Modify: `pkg/tools/register.go`
- Create or modify: test files for deploy handlers

- [ ] Pull latest from remote before starting. Review current state of `pkg/tools/deploy.go`, `pkg/tools/register.go`, `pkg/server/server.go`, and `cmd/tentacular-mcp/main.go` for any upstream changes.
- [ ] Modify `cmd/tentacular-mcp/main.go`: construct the ExoskeletonController from the loaded config (create registrars, credential injector). Pass it to the server/tool registration. When exoskeleton is disabled, pass a nil controller.
- [ ] Modify `pkg/server/server.go` or `pkg/tools/register.go`: accept the ExoskeletonController and pass it to the deploy tool handlers.
- [ ] Modify `pkg/tools/deploy.go` wf_apply handler: after successful manifest application, if the ExoskeletonController is non-nil, extract the workflow ConfigMap from the applied manifests, parse it for tentacular-* dependencies using `DetectExoskeletonDeps`. If deps found, call `controller.Register(ctx, namespace, workflow, deps)`. If registration fails, return the error to the caller with an actionable message (do not silently continue). If no tentacular-* deps, skip registration entirely.
- [ ] Modify `pkg/tools/deploy.go` wf_remove handler: before removing manifests, if ExoskeletonController is non-nil and cleanup is enabled, call `controller.Unregister(ctx, namespace, workflow)`. Log the outcome. Continue with manifest removal even if unregistration partially fails (log warnings, don't block undeploy).
- [ ] Ensure backward compatibility: when ExoskeletonController is nil (disabled), wf_apply and wf_remove execute identically to current code — no new code paths, no new function calls, zero overhead.
- [ ] Add tests: test wf_apply with exoskeleton controller (mock) and tentacular-* deps (Register called), test wf_apply with controller but no tentacular-* deps (Register not called), test wf_apply with nil controller (no exoskeleton calls), test wf_remove with cleanup enabled (Unregister called), test wf_remove with cleanup disabled (Unregister not called).
- [ ] Run all validation commands — all tests pass, lint clean, binary builds.

### Task 13: Add exoskeleton MCP tools

Create new MCP tools for querying exoskeleton status and tentacle registration details, providing observability into the exoskeleton subsystem.

**Files:**
- Create: `pkg/tools/exoskeleton.go`
- Create: `pkg/tools/exoskeleton_test.go`
- Modify: `pkg/tools/register.go`

- [ ] Pull latest from remote before starting.
- [ ] Create `pkg/tools/exoskeleton.go` with three tools:
  - `exo_status`: returns which exoskeleton services are enabled, their feature flag state, and basic connection health (can the MCP server reach Postgres? NATS?). Requires no namespace parameter — this is cluster-level information.
  - `exo_registration`: given namespace + workflow parameters, returns the tentacle's exoskeleton registration details: Postgres role/schema, NATS subject prefix, whether a credential Secret exists, Secret creation timestamp. Returns "not registered" if no exoskeleton Secret found for the tentacle.
  - `exo_list`: list all tentacles that have exoskeleton registrations by scanning Secrets with the `tentacular.io/exoskeleton: "true"` label across all namespaces. Return namespace, workflow, registration timestamp for each.
- [ ] Register all three tools in `pkg/tools/register.go`.
- [ ] Create `pkg/tools/exoskeleton_test.go`: test `exo_status` with enabled and disabled configs, test `exo_registration` with existing and non-existing registrations using fake K8s client, test `exo_list` with multiple registrations across namespaces.
- [ ] Run all validation commands — all tests pass, lint clean.

### Task 14: Fix critical bugs from GitHub issues

Address the highest-priority bugs in tentacular-mcp that affect daily operations. Each fix includes a regression test.

**Files:**
- Modify: relevant files per bug (see checklist below)
- Modify: `pkg/guard/guard.go`

- [ ] Pull latest from remote before starting. Check issue status on GitHub — some may have been fixed by concurrent work.
- [ ] Fix tentacular-mcp#38 (wf_status returns 0/0 replicas): investigate the wf_status handler in `pkg/tools/deploy.go`, find where replica count and readiness state are read from the Deployment object, fix the field mapping so it correctly reports `.status.readyReplicas` and `.spec.replicas`. Add regression test that creates a fake Deployment with known replica counts and verifies wf_status returns them correctly.
- [ ] Fix tentacular-mcp#45 (wf_list shows system namespaces): modify the wf_list handler in `pkg/tools/discover.go` to filter out namespaces with the `tentacular.io/system: "true"` annotation OR matching the hardcoded system namespace names (tentacular-system, tentacular-support, tentacular-exoskeleton). Add regression test.
- [ ] Fix tentacular-mcp#31 (wf_restart not available in deployed server): verify that `wf_restart` is registered in `pkg/tools/register.go`. If the registration call is missing, add it. This may have been a stale image issue — confirm the tool is registered in code. Add a test that verifies all expected tools are registered.
- [ ] Add `tentacular-exoskeleton` to the namespace blocklist in `pkg/guard/guard.go` so MCP tools cannot accidentally deploy workflows into the exoskeleton namespace. Add test for the new entry.
- [ ] Run all validation commands — all tests pass, lint clean.

### Task 15: Verify acceptance criteria and run full test suite

Final verification that all changes work together, existing tests pass, and the chart plus code are ready for deployment.

- [ ] Pull latest from remote before starting.
- [ ] Run full Go test suite: `go test ./pkg/... -v -count=1 -race` — all tests must pass.
- [ ] Run Go vet: `go vet ./...` — no issues.
- [ ] Run linter: `golangci-lint run` — no issues.
- [ ] Run helm lint: `helm lint charts/tentacular-platform/` — passes.
- [ ] Run helm template with test values: `helm template test charts/tentacular-platform/ -f charts/tentacular-platform/ci/test-values.yaml` — renders without errors.
- [ ] Run helm template with dev values: `helm template test charts/tentacular-platform/ -f charts/tentacular-platform/ci/dev-values.yaml` — renders without errors.
- [ ] Verify binary builds: `go build -o /dev/null ./cmd/tentacular-mcp/` — succeeds.
- [ ] Verify: ExoskeletonConfig correctly parses all TENTACULAR_EXOSKELETON_* env vars (config_test.go passes).
- [ ] Verify: IdentityCompiler produces deterministic normalized identifiers for Postgres and NATS (identity_test.go passes).
- [ ] Verify: PostgresRegistrar handles register/reregister/unregister with correct SQL (postgres_test.go passes).
- [ ] Verify: NATSRegistrar handles register/reregister/unregister with correct permissions (nats_test.go passes).
- [ ] Verify: CredentialInjector produces correct Secret structure with `<dep>.<field>` keys (credential_test.go passes).
- [ ] Verify: ExoskeletonController orchestrates full lifecycle (controller_test.go passes).
- [ ] Verify: wf_apply with exoskeleton disabled is unchanged from baseline behavior (deploy tests pass).
- [ ] Verify: wf_apply with exoskeleton enabled triggers registration for tentacular-* deps (deploy tests pass).
- [ ] Verify: wf_remove with cleanup triggers unregistration (deploy tests pass).
- [ ] Verify: exo_status, exo_registration, exo_list tools return correct data (exoskeleton tool tests pass).
