# Tentacular Platform Helm Chart

Umbrella Helm chart for the complete Tentacular platform. Deploys the MCP server, PostgreSQL, NATS, RustFS, Keycloak, esm-sh module proxy, namespace management, network policies, and configurable ingress in a single `helm install`.

## Exoskeleton Subsystem (Phase 1)

The platform includes the exoskeleton subsystem for automated backing-service lifecycle management:

- **Identity compiler** -- deterministic namespace/credential identity from workflow name
- **Registrars** -- PostgreSQL (role/schema), NATS (account/JetStream), RustFS (bucket/policy), SPIRE (ClusterSPIFFEID)
- **Credential injection** -- auto-generated Kubernetes Secrets with connection strings
- **SSO/OIDC auth** -- Keycloak integration with deployer provenance (realm and client auto-created on first boot)
- **MCP tools** -- `exo_status` (health), `exo_registration` (credential lookup), `exo_list` (enumerate registrations)

When `exoskeleton.enabled: true`, the umbrella chart generates a Secret (`tentacular-exoskeleton-config`) containing all `TENTACULAR_*` environment variables and loads them into the MCP server via `envFrom`.

## Prerequisites

- Kubernetes 1.28+
- Helm 3.x
- kubectl configured for your cluster
- cert-manager installed (for TLS via Let's Encrypt)
- nginx ingress controller (recommended; for AWS, use NLB as the controller's Service type)
- Istio (optional, experimental `istio` ingress mode)

## Quick Start

### Development

Dev values include test credentials, disable persistent storage (emptyDir), and expose MCP via NodePort 30080. No additional `--set` flags are needed.

```bash
helm dependency update charts/tentacular-platform/
helm install tentacular charts/tentacular-platform/ \
  -f charts/tentacular-platform/ci/dev-values.yaml \
  -n tentacular-system --create-namespace

# Verify
kubectl get pods -n tentacular-system
kubectl get pods -n tentacular-exoskeleton
kubectl get pods -n tentacular-support
```

### Production (cloud-agnostic)

Production uses persistent storage, TLS via cert-manager, nginx Ingress, Keycloak for OIDC, and full exoskeleton backing services. All credentials are generated at install time.

**Step 1: Install cert-manager** (skip if already installed):

```bash
helm repo add jetstack https://charts.jetstack.io
helm repo update
helm install cert-manager jetstack/cert-manager \
  -n cert-manager --create-namespace --set crds.enabled=true
```

**Step 2: Install nginx ingress controller** (skip if already installed):

```bash
helm repo add ingress-nginx https://kubernetes.github.io/ingress-nginx
helm install ingress-nginx ingress-nginx/ingress-nginx \
  -n ingress-nginx --create-namespace
```

**Step 3: Install the platform:**

```bash
helm dependency update charts/tentacular-platform/
KC_DB_PASS="$(openssl rand -hex 16)"
helm install tentacular charts/tentacular-platform/ \
  -f charts/tentacular-platform/ci/prod-values.yaml \
  -n tentacular-system --create-namespace \
  --set postgresql.auth.password="$(openssl rand -hex 16)" \
  --set nats.config.merge.authorization.token="$(openssl rand -hex 16)" \
  --set tentacular-mcp.auth.token="$(openssl rand -hex 32)" \
  --set keycloak.admin.password="$(openssl rand -hex 16)" \
  --set keycloakx.database.password="$KC_DB_PASS" \
  --set keycloakx.database.hostname="tentacular-postgresql.tentacular-exoskeleton.svc.cluster.local" \
  --set exoskeletonAuth.clientSecret="$(openssl rand -hex 32)" \
  --set rustfs.secret.rustfs.access_key="$(openssl rand -hex 16)" \
  --set rustfs.secret.rustfs.secret_key="$(openssl rand -hex 32)"
```

### AWS (K8s on EC2 with NLB)

For AWS deployments, layer the `aws-values.yaml` overlay on top of `prod-values.yaml`. This sets the domain, TLS, and Keycloak hostnames for your environment.

**Step 1: Install cert-manager** (skip if already installed):

```bash
helm repo add jetstack https://charts.jetstack.io
helm install cert-manager jetstack/cert-manager \
  -n cert-manager --create-namespace --set crds.enabled=true
```

**Step 2: Install nginx ingress controller with NLB:**

```bash
helm repo add ingress-nginx https://kubernetes.github.io/ingress-nginx
helm install ingress-nginx ingress-nginx/ingress-nginx \
  -n ingress-nginx --create-namespace \
  --set controller.service.annotations."service\.beta\.kubernetes\.io/aws-load-balancer-type"=nlb \
  --set controller.service.annotations."service\.beta\.kubernetes\.io/aws-load-balancer-scheme"=internet-facing
```

**Step 3: Create DNS records** pointing to the NLB:
```
tentacular-mcp.<your-domain>      → <NLB hostname>
tentacular-keycloak.<your-domain> → <NLB hostname>
```

Get the NLB hostname with:
```bash
kubectl get svc -n ingress-nginx ingress-nginx-controller \
  -o jsonpath='{.status.loadBalancer.ingress[0].hostname}'
```

**Step 4: Install the platform:**

```bash
helm dependency update charts/tentacular-platform/
KC_DB_PASS="$(openssl rand -hex 16)"
helm install tentacular charts/tentacular-platform/ \
  -f charts/tentacular-platform/ci/prod-values.yaml \
  -f charts/tentacular-platform/ci/aws-values.yaml \
  -n tentacular-system --create-namespace \
  --set postgresql.auth.password="$(openssl rand -hex 16)" \
  --set nats.config.merge.authorization.token="$(openssl rand -hex 16)" \
  --set tentacular-mcp.auth.token="$(openssl rand -hex 32)" \
  --set keycloak.admin.password="$(openssl rand -hex 16)" \
  --set keycloakx.database.password="$KC_DB_PASS" \
  --set keycloakx.database.hostname="tentacular-postgresql.tentacular-exoskeleton.svc.cluster.local" \
  --set keycloakx.proxy.mode=xforwarded \
  --set-json 'keycloakx.command=["/opt/keycloak/bin/kc.sh","start","--hostname-strict=false","--import-realm"]' \
  --set exoskeletonAuth.clientSecret="$(openssl rand -hex 32)" \
  --set rustfs.secret.rustfs.access_key="$(openssl rand -hex 16)" \
  --set rustfs.secret.rustfs.secret_key="$(openssl rand -hex 32)"
```

**Step 5: Verify:**

```bash
# Check all pods
kubectl get pods -n tentacular-system
kubectl get pods -n tentacular-exoskeleton
kubectl get pods -n tentacular-support

# Check TLS certificates
kubectl get certificate -A

# Test MCP health
curl https://tentacular-mcp.<your-domain>/healthz

# Test Keycloak
curl https://tentacular-keycloak.<your-domain>/auth/realms/tentacular/.well-known/openid-configuration

# Test with tntc CLI
tntc cluster check -e <your-env>
```

> **Note:** Keycloak takes ~60-90 seconds on first boot (Quarkus build phase + realm import).
> The MCP server will restart a few times while waiting for Keycloak OIDC discovery to become available.
> Both stabilize automatically.

## Ingress Modes

The `ingress.mode` field controls how the platform is exposed externally.

| Mode | Description | When to Use |
|------|-------------|-------------|
| `none` | No external exposure; use `kubectl port-forward` | Local development, debugging |
| `nodeport` | Expose MCP via NodePort | Simple/test clusters, SSH tunnel, VPN access |
| `ingress` | Standard Kubernetes Ingress resource (cloud-agnostic) | nginx, Traefik, or any K8s ingress controller |
| `istio` | **(Experimental)** Istio Gateway + VirtualService + DestinationRule | Clusters with Istio service mesh (includes `Mcp-Session-Id` consistent hash) |

### MCP Session Affinity

For multi-replica MCP deployments, the MCP Streamable HTTP transport uses the `Mcp-Session-Id` header for session routing. Configure session affinity via ingress annotations:

**nginx:**
```yaml
ingress:
  annotations:
    nginx.ingress.kubernetes.io/upstream-hash-by: "$http_mcp_session_id"
```

**Istio** (automatic via DestinationRule when `ingress.mode: istio`).

### Examples

**NodePort (dev/test):**
```yaml
ingress:
  mode: nodeport
  nodeport:
    mcp: 30080
```

**Standard Ingress (nginx):**
```yaml
ingress:
  mode: ingress
  className: nginx
  controllerNamespace: ingress-nginx
  mcp:
    hostname: tentacular-mcp.example.com
  tls:
    enabled: true
    secretName: tentacular-mcp-tls
  annotations:
    nginx.ingress.kubernetes.io/upstream-hash-by: "$http_mcp_session_id"
```

**Istio (experimental):**
```yaml
ingress:
  mode: istio
  mcp:
    hostname: tentacular-mcp.example.com
  tls:
    enabled: true
    secretName: tentacular-tls
```

## Configuration Reference

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `global.domain` | string | `""` | Base domain for platform endpoints |
| `global.imagePullSecrets` | list | `[]` | Image pull secrets for private registries |
| `namespaces.system.create` | bool | `true` | Create the system namespace |
| `namespaces.system.name` | string | `"tentacular-system"` | System namespace name |
| `namespaces.exoskeleton.create` | bool | `true` | Create the exoskeleton namespace |
| `namespaces.exoskeleton.name` | string | `"tentacular-exoskeleton"` | Exoskeleton namespace name |
| `namespaces.support.create` | bool | `true` | Create the support namespace |
| `namespaces.support.name` | string | `"tentacular-support"` | Support namespace name |
| `networkPolicies.enabled` | bool | `true` | Enable default-deny network policies |
| `postgresql.enabled` | bool | `true` | Enable PostgreSQL deployment |
| `postgresql.auth.password` | string | `""` | Admin password (required) |
| `postgresql.tls.enabled` | bool | `false` | Enable PostgreSQL TLS (cert-manager) |
| `nats.enabled` | bool | `true` | Enable NATS deployment |
| `nats.config.jetstream.enabled` | bool | `true` | Enable JetStream |
| `cert-manager.enabled` | bool | `false` | Enable cert-manager (most clusters have it pre-installed) |
| `tls.clusterIssuers.create` | bool | `false` | Create Let's Encrypt ClusterIssuer |
| `tls.clusterIssuers.email` | string | `""` | Email for Let's Encrypt registration |
| `tentacular-mcp.enabled` | bool | `true` | Enable MCP server deployment |
| `tentacular-mcp.auth.token` | string | `""` | MCP auth token (required) |
| `exoskeleton.enabled` | bool | `true` | Enable exoskeleton subsystem |
| `exoskeletonAuth.enabled` | bool | `false` | Enable OIDC authentication (Keycloak) |
| `exoskeletonAuth.clientID` | string | `"tentacular-mcp"` | OIDC client ID |
| `exoskeletonAuth.clientSecret` | string | `""` | OIDC client secret (required when enabled) |
| `keycloak.enabled` | bool | `false` | Enable Keycloak deployment |
| `keycloak.realm` | string | `"tentacular"` | Keycloak realm name |
| `keycloak.admin.user` | string | `"admin"` | Keycloak admin username |
| `keycloak.admin.password` | string | `""` | Keycloak admin password (required when enabled) |
| `keycloak.hostname` | string | `""` | Keycloak hostname (e.g., tentacular-keycloak.example.com) |
| `rustfs.enabled` | bool | `false` | Enable RustFS deployment |
| `rustfs.secret.rustfs.access_key` | string | `""` | RustFS admin access key (required when enabled) |
| `rustfs.secret.rustfs.secret_key` | string | `""` | RustFS admin secret key (required when enabled) |
| `esm-sh.enabled` | bool | `true` | Enable esm-sh proxy |
| `ingress.mode` | string | `"none"` | Ingress mode (none/nodeport/ingress/istio) |
| `ingress.mcp.hostname` | string | `""` | MCP endpoint hostname |
| `ingress.controllerNamespace` | string | `""` | Ingress controller namespace (for NetworkPolicy) |
| `ingress.tls.enabled` | bool | `false` | Enable TLS termination |
| `ingress.className` | string | `""` | Ingress class (nginx, traefik, etc.) |
| `ingress.annotations` | object | `{}` | Freeform annotations for Ingress |

## Component Toggles

Every component can be independently enabled or disabled:

| Component | Toggle | Default | Notes |
|-----------|--------|---------|-------|
| PostgreSQL | `postgresql.enabled` | `true` | Bitnami PostgreSQL with optional TLS |
| NATS | `nats.enabled` | `true` | NATS with JetStream |
| Keycloak | `keycloak.enabled` | `false` | Quarkus Keycloak (codecentric/keycloakx) with auto realm import |
| cert-manager | `cert-manager.enabled` | `false` | Only if not pre-installed |
| MCP Server | `tentacular-mcp.enabled` | `true` | Tentacular MCP server |
| esm-sh | `esm-sh.enabled` | `true` | ES module proxy |
| Network Policies | `networkPolicies.enabled` | `true` | Default-deny with allow rules |
| RustFS | `rustfs.enabled` | `false` | S3-compatible object storage (per-tentacle prefix scoping) |
| SPIRE | `spire.enabled` | `false` | Future: Workload identity |

## Storage

PostgreSQL, NATS, and RustFS require persistent storage. In production clusters with a
StorageClass provisioner this works out of the box (uses cluster default). For
**dev/test clusters without a provisioner** (e.g., kind, bare minikube), the CI
value files disable PVCs and fall back to `emptyDir`:

```yaml
postgresql:
  primary:
    persistence:
      enabled: false   # emptyDir — data lost on pod restart

nats:
  config:
    jetstream:
      fileStore:
        pvc:
          enabled: false  # emptyDir — data lost on pod restart
```

> **Note:** This is acceptable for development only. Production deployments
> must use persistent storage.

## PostgreSQL TLS

Production deployments enable PostgreSQL TLS using cert-manager self-signed certificates.
Do not use `tls.autoGenerated: true` -- the Bitnami init image (`bitnami/os-shell`) is
behind Broadcom's paywall since August 2025. Instead, the chart creates a cert-manager
`Issuer` + `Certificate` that populates the TLS Secret.

```yaml
postgresql:
  tls:
    enabled: true
    autoGenerated: false
    certificatesSecret: tentacular-postgresql-tls
    certFilename: tls.crt
    certKeyFilename: tls.key
```

## RustFS TLS

RustFS serves HTTPS internally using cert-manager-issued certificates (server-only mTLS).
The CA certificate is automatically mounted into the MCP pod so the registrar trusts
the self-signed cert chain. No manual certificate management is needed.

```yaml
rustfs:
  mtls:
    enabled: true
    serverOnly: true
```

When `rustfs.mtls.enabled` is true, the RustFS subchart creates a cert-manager CA and
server certificate. The platform chart mounts the CA secret (`tentacular-rustfs-root-ca-secret`)
into the MCP pod at `/etc/rustfs-ca/` and configures the exoskeleton registrar to load
the CA cert for HTTPS connections. The volume mount is `optional: true`, so the MCP pod
still starts when RustFS is disabled.

## Keycloak User Management

When `keycloak.enabled: true`, the chart deploys Keycloak and auto-creates the `tentacular`
realm with the `tentacular-mcp` OIDC client. Users can then authenticate via `tntc login`.

### Adding Local Users

Use the Keycloak admin API to create users. First, get an admin token:

```bash
KC_URL="https://tentacular-keycloak.<your-domain>"
KC_ADMIN_PASS="$(kubectl get secret tentacular-keycloak-admin \
  -n tentacular-exoskeleton -o jsonpath='{.data.KEYCLOAK_ADMIN_PASSWORD}' | base64 -d)"

TOKEN=$(curl -sk "$KC_URL/auth/realms/master/protocol/openid-connect/token" \
  -d "client_id=admin-cli" -d "username=admin" -d "password=$KC_ADMIN_PASS" \
  -d "grant_type=password" | python3 -c "import sys,json; print(json.load(sys.stdin)['access_token'])")
```

Then create a user:

```bash
curl -sk -X POST "$KC_URL/auth/admin/realms/tentacular/users" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "username": "jdoe",
    "email": "jdoe@example.com",
    "firstName": "Jane",
    "lastName": "Doe",
    "enabled": true,
    "emailVerified": true,
    "credentials": [{
      "type": "password",
      "value": "change-me-on-first-login",
      "temporary": true
    }]
  }'
```

Or use the Keycloak admin console at `https://tentacular-keycloak.<your-domain>/auth/admin/`.

### Authenticating with tntc

Configure OIDC in `~/.tentacular/config.yaml`:

```yaml
environments:
  my-env:
    mcp_endpoint: https://tentacular-mcp.<your-domain>/mcp
    namespace: tentacular-dev
    oidc_issuer: https://tentacular-keycloak.<your-domain>/auth/realms/tentacular
    oidc_client_id: tentacular-mcp
    oidc_client_secret: <from exoskeletonAuth.clientSecret>
```

Then authenticate:

```bash
tntc login -e my-env     # Opens browser for Keycloak login
tntc whoami -e my-env    # Verify identity
tntc cluster check -e my-env  # Test MCP connectivity with OIDC token
```

### Federating External Identity Providers

Keycloak supports identity brokering -- users can sign in with Google, GitHub, or any
OIDC/SAML provider alongside local accounts. The MCP server automatically detects the
upstream provider from the Keycloak `identity_provider` token claim.

**Google:**

1. Create OAuth credentials in [Google Cloud Console](https://console.cloud.google.com/apis/credentials):
   - Application type: Web application
   - Authorized redirect URI: `https://tentacular-keycloak.<your-domain>/auth/realms/tentacular/broker/google/endpoint`
2. In Keycloak admin console, go to **Identity Providers > Add provider > Google**
3. Enter your Google Client ID and Client Secret
4. Users can now click "Sign in with Google" on the Keycloak login page

**GitHub:**

1. Create an OAuth App in [GitHub Developer Settings](https://github.com/settings/developers):
   - Authorization callback URL: `https://tentacular-keycloak.<your-domain>/auth/realms/tentacular/broker/github/endpoint`
2. In Keycloak admin console, go to **Identity Providers > Add provider > GitHub**
3. Enter your GitHub Client ID and Client Secret

**Any OIDC provider:**

1. In Keycloak admin console, go to **Identity Providers > Add provider > OpenID Connect v1.0**
2. Set the Discovery endpoint (or configure manually)
3. Set Client ID and Client Secret from the external provider

All federated users appear in the tentacular realm and can use `tntc login` -- the device
authorization flow redirects through Keycloak, which presents all configured sign-in options.

## Example Values Files

Pre-built profiles are available in `ci/`:

- `ci/test-values.yaml` - CI testing (all defaults, minimal resources)
- `ci/dev-values.yaml` - Development (NodePort, minimal resources, emptyDir storage)
- `ci/prod-values.yaml` - Production (nginx Ingress, TLS, Keycloak, full exoskeleton)
- `ci/aws-values.yaml` - AWS overlay (layered on prod-values, sets domain and hostnames)
- `ci/tls-values.yaml` - TLS resources only (for testing cert-manager integration)

## Upgrade

```bash
helm dependency update charts/tentacular-platform/
helm upgrade tentacular charts/tentacular-platform/ \
  -f your-values.yaml
```

> **Warning:** Do not use `helm upgrade --reuse-values -f ci/prod-values.yaml` --
> the `CHANGE-ME` placeholders in value files will overwrite your generated secrets.
> Either omit `-f` when using `--reuse-values`, or pass all secrets via `--set`.

## Uninstall

```bash
helm uninstall tentacular -n tentacular-system

# Namespaces with finalizers may need manual cleanup
kubectl delete namespace tentacular-system tentacular-exoskeleton tentacular-support
```
