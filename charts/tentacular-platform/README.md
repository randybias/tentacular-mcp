# Tentacular Platform Helm Chart

Umbrella Helm chart for the complete Tentacular platform. Deploys the MCP server, PostgreSQL, NATS, esm-sh module proxy, namespace management, network policies, and configurable ingress in a single `helm install`.

## Prerequisites

- Kubernetes 1.28+
- Helm 3.x
- kubectl configured for your cluster
- Istio (if using `istio` or `alb-istio` ingress modes)
- AWS Load Balancer Controller (if using ALB ingress)

## Quick Start

```bash
# Add required Helm repositories
helm repo add nats https://nats-io.github.io/k8s/helm/charts/
helm repo add jetstack https://charts.jetstack.io
helm repo update

# Install with development values
helm dependency update charts/tentacular-platform/
helm install tentacular charts/tentacular-platform/ \
  -f charts/tentacular-platform/ci/dev-values.yaml

# Verify
kubectl get pods -n tentacular-system
kubectl get pods -n tentacular-exoskeleton
kubectl get pods -n tentacular-support
```

## Ingress Modes

The `ingress.mode` field controls how the platform is exposed externally.

| Mode | Description | When to Use |
|------|-------------|-------------|
| `none` | No external exposure; use `kubectl port-forward` | Local development, debugging |
| `nodeport` | Expose MCP via NodePort | Simple/test clusters, SSH tunnel, VPN access |
| `ingress` | Standard Kubernetes Ingress resource | Traefik, nginx-ingress, or AWS ALB Ingress Controller |
| `istio` | Istio Gateway + VirtualService | Clusters with Istio service mesh |
| `alb-istio` | AWS ALB fronting Istio ingress gateway | AWS deployments with Istio |

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
  mcp:
    hostname: mcp.example.com
  tls:
    enabled: true
    secretName: mcp-tls
```

**AWS ALB:**
```yaml
ingress:
  mode: ingress
  className: alb
  mcp:
    hostname: mcp.example.com
  annotations:
    alb.ingress.kubernetes.io/scheme: internet-facing
    alb.ingress.kubernetes.io/target-type: ip
    alb.ingress.kubernetes.io/certificate-arn: "arn:aws:acm:..."
```

**Istio:**
```yaml
ingress:
  mode: istio
  mcp:
    hostname: mcp.example.com
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
| `postgresql.auth.database` | string | `"tentacular"` | Database name |
| `postgresql.auth.username` | string | `"tentacular_admin"` | Admin username |
| `postgresql.auth.password` | string | `""` | Admin password (required) |
| `nats.enabled` | bool | `true` | Enable NATS deployment |
| `nats.config.jetstream.enabled` | bool | `true` | Enable JetStream |
| `cert-manager.enabled` | bool | `false` | Enable cert-manager (most clusters have it pre-installed) |
| `tls.clusterIssuers.create` | bool | `false` | Create Let's Encrypt ClusterIssuer |
| `tls.clusterIssuers.email` | string | `""` | Email for Let's Encrypt registration |
| `tls.clusterIssuers.production` | bool | `true` | Use production LE server (false = staging) |
| `tls.certificates.mcp.create` | bool | `false` | Create MCP TLS Certificate |
| `tls.certificates.auth.create` | bool | `false` | Create auth TLS Certificate |
| `tentacular-mcp.enabled` | bool | `true` | Enable MCP server deployment |
| `tentacular-mcp.auth.token` | string | `""` | MCP auth token (required) |
| `exoskeleton.enabled` | bool | `true` | Enable exoskeleton subsystem |
| `esm-sh.enabled` | bool | `true` | Enable esm-sh proxy |
| `ingress.mode` | string | `"none"` | Ingress mode (none/nodeport/ingress/istio/alb-istio) |
| `ingress.mcp.hostname` | string | `""` | MCP endpoint hostname |
| `ingress.auth.enabled` | bool | `false` | Enable auth endpoint routing |
| `ingress.auth.hostname` | string | `""` | Auth endpoint hostname |
| `ingress.tls.enabled` | bool | `false` | Enable TLS termination |
| `ingress.tls.secretName` | string | `""` | TLS secret name |
| `ingress.className` | string | `""` | Ingress class (traefik, nginx, alb) |
| `ingress.annotations` | object | `{}` | Freeform annotations for Ingress |
| `ingress.istio.gateway.name` | string | `"tentacular-gateway"` | Istio Gateway name |
| `ingress.nodeport.mcp` | int | `30080` | NodePort number for MCP |
| `rustfs.enabled` | bool | `false` | Enable RustFS (not yet implemented) |
| `keycloak.enabled` | bool | `false` | Enable Keycloak (not yet implemented) |
| `spire.enabled` | bool | `false` | Enable SPIRE (not yet implemented) |

## Component Toggles

Every component can be independently enabled or disabled:

| Component | Toggle | Default | Notes |
|-----------|--------|---------|-------|
| PostgreSQL | `postgresql.enabled` | `true` | Bitnami PostgreSQL subchart |
| NATS | `nats.enabled` | `true` | NATS with JetStream |
| cert-manager | `cert-manager.enabled` | `false` | Only if not pre-installed |
| MCP Server | `tentacular-mcp.enabled` | `true` | Tentacular MCP server |
| esm-sh | `esm-sh.enabled` | `true` | ES module proxy |
| Network Policies | `networkPolicies.enabled` | `true` | Default-deny with allow rules |
| RustFS | `rustfs.enabled` | `false` | Future: S3-compatible storage |
| Keycloak | `keycloak.enabled` | `false` | Future: IAM |
| SPIRE | `spire.enabled` | `false` | Future: Workload identity |

## Example Values Files

Pre-built profiles are available in `ci/`:

- `ci/test-values.yaml` - CI testing (all defaults, minimal resources)
- `ci/dev-values.yaml` - Development (NodePort, minimal resources)
- `ci/prod-values.yaml` - Production (nginx Ingress, TLS, higher resources)
- `ci/aws-values.yaml` - AWS (ALB + Istio, ACM certificates)
- `ci/tls-values.yaml` - TLS resources only (for testing cert-manager integration)

## Upgrade

```bash
helm dependency update charts/tentacular-platform/
helm upgrade tentacular charts/tentacular-platform/ \
  -f your-values.yaml
```

## Uninstall

```bash
helm uninstall tentacular

# Namespaces with finalizers may need manual cleanup
kubectl delete namespace tentacular-system tentacular-exoskeleton tentacular-support
```
