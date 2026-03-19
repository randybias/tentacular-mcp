## 1. Helper Infrastructure

- [ ] 1.1 Add `boolPtr(b bool) *bool` helper function to `pkg/tools/register.go`
- [ ] 1.2 Verify the helper compiles and is accessible from all tool registration files in the package

## 2. Read-Only Tool Annotations

- [ ] 2.1 Add ToolAnnotations to `wf_list`, `wf_describe`, `wf_status` in `pkg/tools/workflow.go` (ReadOnlyHint: true, OpenWorldHint: ptr(false), Title)
- [ ] 2.2 Add ToolAnnotations to `wf_pods`, `wf_logs` in `pkg/tools/workflow.go` (ReadOnlyHint: true, OpenWorldHint: ptr(false), Title)
- [ ] 2.3 Add ToolAnnotations to `wf_events`, `wf_jobs` in `pkg/tools/discover.go` (ReadOnlyHint: true, OpenWorldHint: ptr(false), Title)
- [ ] 2.4 Add ToolAnnotations to `wf_health`, `wf_health_ns` in `pkg/tools/wf_health.go` (ReadOnlyHint: true, OpenWorldHint: ptr(false), Title)
- [ ] 2.5 Add ToolAnnotations to `ns_get`, `ns_list` in `pkg/tools/namespace.go` (ReadOnlyHint: true, OpenWorldHint: ptr(false), Title)
- [ ] 2.6 Add ToolAnnotations to `health_nodes`, `health_ns_usage`, `health_cluster_summary` in `pkg/tools/health.go` (ReadOnlyHint: true, OpenWorldHint: ptr(false), Title)
- [ ] 2.7 Add ToolAnnotations to `audit_rbac`, `audit_netpol`, `audit_psa` in `pkg/tools/audit.go` (ReadOnlyHint: true, OpenWorldHint: ptr(false), Title)
- [ ] 2.8 Add ToolAnnotations to `gvisor_check` in `pkg/tools/gvisor.go` (ReadOnlyHint: true, OpenWorldHint: ptr(false), Title)
- [ ] 2.9 Add ToolAnnotations to `exo_status`, `exo_registration`, `exo_list` in `pkg/tools/exoskeleton.go` (ReadOnlyHint: true, OpenWorldHint: ptr(false), Title)
- [ ] 2.10 Add ToolAnnotations to `cluster_profile`, `cluster_preflight` in `pkg/tools/clusterops.go` (ReadOnlyHint: true, OpenWorldHint: ptr(false), Title)
- [ ] 2.11 Add ToolAnnotations to `proxy_status` in `pkg/tools/proxy.go` (ReadOnlyHint: true, OpenWorldHint: ptr(false), Title)

## 3. Write (Non-Destructive) Tool Annotations

- [ ] 3.1 Add ToolAnnotations to `wf_apply` in `pkg/tools/deploy.go` (DestructiveHint: ptr(false), IdempotentHint: true, OpenWorldHint: ptr(false), Title)
- [ ] 3.2 Add ToolAnnotations to `wf_restart` in `pkg/tools/deploy.go` (DestructiveHint: ptr(false), IdempotentHint: false, OpenWorldHint: ptr(false), Title)
- [ ] 3.3 Add ToolAnnotations to `wf_run` in `pkg/tools/run.go` (DestructiveHint: ptr(false), IdempotentHint: false, OpenWorldHint: ptr(false), Title)
- [ ] 3.4 Add ToolAnnotations to `ns_create` in `pkg/tools/namespace.go` (DestructiveHint: ptr(false), IdempotentHint: true, OpenWorldHint: ptr(false), Title)
- [ ] 3.5 Add ToolAnnotations to `ns_update` in `pkg/tools/namespace.go` (DestructiveHint: ptr(false), IdempotentHint: true, OpenWorldHint: ptr(false), Title)
- [ ] 3.6 Add ToolAnnotations to `gvisor_annotate_ns` in `pkg/tools/gvisor.go` (DestructiveHint: ptr(false), IdempotentHint: true, OpenWorldHint: ptr(false), Title)
- [ ] 3.7 Add ToolAnnotations to `gvisor_verify` in `pkg/tools/gvisor.go` (DestructiveHint: ptr(false), IdempotentHint: false, OpenWorldHint: ptr(false), Title)
- [ ] 3.8 Add ToolAnnotations to `cred_issue_token` in `pkg/tools/credential.go` (DestructiveHint: ptr(false), IdempotentHint: false, OpenWorldHint: ptr(false), Title)
- [ ] 3.9 Add ToolAnnotations to `cred_kubeconfig` in `pkg/tools/credential.go` (DestructiveHint: ptr(false), IdempotentHint: true, OpenWorldHint: ptr(false), Title)

## 4. Destructive Tool Annotations

- [ ] 4.1 Add ToolAnnotations to `wf_remove` in `pkg/tools/deploy.go` (DestructiveHint: ptr(true), IdempotentHint: false, OpenWorldHint: ptr(false), Title)
- [ ] 4.2 Add ToolAnnotations to `ns_delete` in `pkg/tools/namespace.go` (DestructiveHint: ptr(true), IdempotentHint: false, OpenWorldHint: ptr(false), Title)
- [ ] 4.3 Add ToolAnnotations to `cred_rotate` in `pkg/tools/credential.go` (DestructiveHint: ptr(true), IdempotentHint: false, OpenWorldHint: ptr(false), Title)

## 5. Description Enrichment

- [ ] 5.1 Enrich descriptions for write tools (wf_apply, wf_restart, wf_run, ns_create, ns_update, gvisor_annotate_ns, gvisor_verify, cred_issue_token, cred_kubeconfig) with expected effects and prerequisites
- [ ] 5.2 Enrich descriptions for destructive tools (wf_remove, ns_delete, cred_rotate) with safety notes about permanent removal or invalidation
- [ ] 5.3 Review all descriptions to verify none exceed three sentences

## 6. Test Coverage

- [ ] 6.1 Add table-driven test in `pkg/tools/register_test.go` that calls `tools/list` and asserts every tool has non-nil Annotations
- [ ] 6.2 Add per-tool expected annotation map verifying ReadOnlyHint, DestructiveHint, IdempotentHint, OpenWorldHint, and Title for all 36 tools
- [ ] 6.3 Add assertion that fails if a new tool is registered without annotations (count check)
- [ ] 6.4 Run `go test ./pkg/tools/...` and verify all tests pass

## 7. Validation

- [ ] 7.1 Run `go build ./...` to verify compilation
- [ ] 7.2 Run `go vet ./...` to check for issues
- [ ] 7.3 Run golangci-lint if configured
- [ ] 7.4 Manually verify one tool from each tier via `tools/list` JSON output (if test cluster available)
