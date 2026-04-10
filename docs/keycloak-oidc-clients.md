# Keycloak OIDC Client Configuration

The Tentacular platform uses Keycloak as its identity provider. Three OIDC clients serve different platform consumers.

## Architecture

```
                  Keycloak (tentacular realm)
                    |         |          |
         tentacular-mcp   thekraken    chroma
          (PUBLIC)       (CONFIDENTIAL) (CONFIDENTIAL)
            |                |              |
     +------+------+        |              |
     |             |         |              |
  tntc CLI    Claude Code  Slack Bot    Next.js UI
  (device)    (.mcp.json)  (client_creds) (auth code)
     |             |         |              |
     +------+------+---------+--------------+
                    |
              MCP Server
           (resource server -
            validates tokens)
```

The MCP server is a **resource server**, not an OIDC client. It validates tokens issued to any of the above clients by checking the JWT signature against Keycloak's JWKS endpoint and the `azp` (authorized party) claim. The MCP server's `TENTACULAR_KEYCLOAK_TRUSTED_AZPS` env var lists which client IDs it trusts (currently: `thekraken,chroma`; `tentacular-mcp` is implicitly trusted as the primary client).

## Clients

### tentacular-mcp (PUBLIC)

| Setting | Value | Rationale |
|---------|-------|-----------|
| publicClient | **true** | Consumers (tntc CLI, Claude Code) cannot hold secrets |
| serviceAccountsEnabled | false | No machine-to-machine auth needed through this client |
| standardFlowEnabled | true | Claude Code uses authorization code + PKCE |
| directAccessGrantsEnabled | true | tntc CLI can use resource owner password grant |
| deviceAuth | true | tntc CLI primary flow (device authorization) |

**Consumers:**
- `tntc` CLI: Device authorization flow (`tntc login` opens browser, polls for token)
- Claude Code `.mcp.json`: Authorization code + PKCE via OAuth popup (no client secret in config)

**Why public:** Both consumers run on a developer's machine. A CLI binary and a browser-based OAuth popup cannot securely store a client secret. PKCE provides equivalent security for public clients.

### thekraken (CONFIDENTIAL)

| Setting | Value | Rationale |
|---------|-------|-----------|
| publicClient | false | Server-side Slack bot can hold a secret |
| serviceAccountsEnabled | **true** | Bot uses `client_credentials` for bot-level MCP calls |
| standardFlowEnabled | false | No browser-based login |
| directAccessGrantsEnabled | true | Used for per-user transitive trust authentication |
| deviceAuth | true | Per-user OIDC authentication via Slack DM |

**Consumer:** The Kraken Slack bot (Node.js, runs in Kubernetes).

**Why confidential:** The bot runs server-side in a pod with the secret injected via Kubernetes Secret. It uses `client_credentials` grant to get a service token for bot-level MCP calls (no user context), and device authorization for per-user transitive trust.

### chroma (CONFIDENTIAL)

| Setting | Value | Rationale |
|---------|-------|-----------|
| publicClient | false | Server-side Next.js can hold a secret |
| serviceAccountsEnabled | false | All requests are on behalf of a user |
| standardFlowEnabled | **true** | NextAuth uses authorization code flow |
| directAccessGrantsEnabled | false | No need for direct access |
| deviceAuth | false | Browser-only, no device flow needed |

**Consumer:** Chroma enclave UI (Next.js 15, runs in Kubernetes).

**Why confidential:** NextAuth runs server-side in the Next.js API routes. The client secret is injected via Kubernetes Secret. All authentication is authorization code flow with a server-side callback.

## Persistence Model

Keycloak uses **PostgreSQL** for persistent storage. The realm import ConfigMap (`keycloak-realm-configmap.yaml`) is a **seed file only** — Keycloak reads it on first startup via `--import-realm` and skips it if the realm already exists in the database.

**This means:**
1. The Postgres database is the authoritative source after first boot
2. Changes made in the Keycloak admin console persist in the DB
3. The ConfigMap is NOT re-imported on pod restarts (as long as the DB has the realm)
4. The ConfigMap IS imported if the database is wiped (fresh Keycloak install)

**Keep the ConfigMap in sync with the live state.** If you change a client in the admin console, update the Helm template (`charts/tentacular-platform/templates/keycloak-realm-configmap.yaml`) to match. Otherwise a database wipe will regress to the old ConfigMap values.

## Common Failure Modes

### "Invalid client or Invalid client credentials"
The client is configured as confidential but the consumer is trying a public flow (no secret). Check `publicClient` on the Keycloak client. For `tentacular-mcp`, this MUST be `true`.

### "Invalid refresh token" / token expiry
Token lifespans are set at the realm level:
- Access token: 12 hours (`accessTokenLifespan: 43200`)
- SSO session idle: 12 hours
- SSO session max: 24 hours
- Offline session idle: 30 days

If sessions expire too fast, check these realm settings in Keycloak admin.

### ConfigMap drift
If the live Keycloak state doesn't match the ConfigMap, the next database wipe will cause a regression. To check for drift:

```bash
# Compare live vs ConfigMap
# (use the admin API to query live state, compare against ConfigMap)
KEYCLOAK_SVC="https://auth.eastus-dev1.ospo-dev.miralabs.dev"
# ... get admin token ...
curl -s -H "Authorization: Bearer $TOKEN" "$KEYCLOAK_SVC/admin/realms/tentacular/clients?clientId=tentacular-mcp"
```

## Helm Values

Client configuration lives in `charts/tentacular-platform/values.yaml` under `keycloak.clients`:

```yaml
keycloak:
  clients:
    mcp:
      clientId: "tentacular-mcp"       # PUBLIC - no secret needed
    kraken:
      clientId: "thekraken"
      clientSecret: "<from-secret>"    # CONFIDENTIAL
    chroma:
      clientId: "chroma"
      clientSecret: "<from-secret>"    # CONFIDENTIAL
      hostname: "chroma.example.com"   # For redirect URI generation
```

Secrets should be provided via `--set` or an existing Kubernetes Secret, never committed to values.yaml.
