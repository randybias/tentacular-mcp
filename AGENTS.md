# Tentacular MCP Server — Agent Instructions

In-cluster Go HTTP server that exposes MCP (Model Context Protocol) tools for AI agents and the `tntc` CLI. Part of the Tentacular platform — a security-first, agent-centric, DAG-based workflow builder and runner for Kubernetes.

## Related Repositories

| Repository | Purpose |
|------------|---------|
| [tentacular](https://github.com/randybias/tentacular) | Go CLI (`tntc`) + Deno workflow engine |
| [tentacular-mcp](https://github.com/randybias/tentacular-mcp) | In-cluster MCP server (this repo) |
| [tentacular-skill](https://github.com/randybias/tentacular-skill) | Agent skill definition (Markdown) |
| [tentacular-scaffolds](https://github.com/randybias/tentacular-scaffolds) | Scaffold quickstart library (TypeScript/Deno) |

## System Architecture

```
Developer / AI Agent
        |
    tntc CLI (Go)            <-- tentacular
        |
   JSON-RPC 2.0 / HTTP
        |
    MCP Server (Go)          <-- this repo (Helm-installed in-cluster)
        |
    Kubernetes API
        |
    Workflow Pods             <-- Deno engine from tentacular/engine/
        (gVisor sandbox)         Scaffolds from tentacular-scaffolds
```

The CLI has zero direct Kubernetes API access. All cluster operations route through this MCP server via authenticated HTTP. The MCP server runs inside the cluster with scoped RBAC.

## Project Structure

- `cmd/tentacular-mcp/main.go` — server entry point
- `pkg/tools/` — MCP tool handlers (one file per tool group)
- `pkg/auth/` — authentication and token management
- `pkg/guard/` — namespace and resource guards
- `pkg/k8s/` — Kubernetes client operations
- `pkg/proxy/` — module proxy management
- `pkg/server/` — HTTP server and JSON-RPC routing
- `charts/tentacular-mcp/` — Helm chart for deployment
- `deploy/manifests/` — Kustomize manifests (alternative to Helm)
- `test/integration/` — integration tests (kind cluster)
- `test/e2e/` — end-to-end tests (real cluster)

## Go Module Path

All Go code uses `github.com/randybias` as the module path prefix: `github.com/randybias/tentacular-mcp`.

## Key Commands

```bash
# Build
make build-binary

# Unit tests (no cluster required)
make test-unit

# Integration tests (provisions a kind cluster automatically)
make test-integration

# E2E tests (requires TENTACULAR_E2E_KUBECONFIG)
TENTACULAR_E2E_KUBECONFIG=path/to/kubeconfig make test-e2e

# All test tiers
make test-all

# Lint
make lint

# Multi-arch Docker build and push
make build TAG=v0.5.0

# Local single-arch Docker build (no push)
make build-local

# Deploy to current cluster
make deploy
```

## Test Tiers

| Tier | Command | Requirements | Scope |
|------|---------|--------------|-------|
| Unit | `make test-unit` | None | Go package tests, no cluster |
| Integration | `make test-integration` | Docker (for kind) | Auto-provisions kind cluster, tears down after |
| E2E | `make test-e2e` | Real cluster + `TENTACULAR_E2E_KUBECONFIG` | Tests against production-like cluster |

## Cross-Repo Changes

When adding or modifying MCP tools, changes typically span repos:

- **New MCP tool:** handler in `pkg/tools/` -> register in `pkg/tools/register.go` -> client method in `tentacular/pkg/mcp/tools.go` -> CLI command in `tentacular/pkg/cli/` -> skill docs in `tentacular-skill/`
- **Security model changes:** may touch `pkg/k8s/`, `pkg/guard/`, `tentacular/pkg/builder/`, and `tentacular-skill/`

## Conventions

- Container images must always be built as multi-arch (linux/amd64,linux/arm64) using `docker buildx`. Never build single-platform images.
- Secrets are never environment variables — always volume mounts or files.
- The MCP server runs with scoped RBAC — only the permissions it needs, no cluster-admin.

## Commit Messages

All repos use [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/):

```
feat: add workflow health endpoint
fix: handle nil pod status in wf_pods
test: add unit tests for discover handler
docs: update CLI reference for catalog commands
chore: bump Go to 1.25
```

GoReleaser uses these prefixes to auto-generate changelogs (grouping by `feat`/`fix`, excluding `docs`/`test`/`chore`).

## Versioning

All four repos use **lockstep versioning** — they are tagged with the same version number for every release, even if a repo has no changes. Tags use semantic versioning: `vMAJOR.MINOR.PATCH`.

## Temporary Files

Use `scratch/` for all temporary files, experiments, and throwaway work. This directory is gitignored. Never place temp files in the project root or alongside source code.

## License

Copyright (c) 2025-2026 Mirantis, Inc. All rights reserved.
