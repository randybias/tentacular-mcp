## MODIFIED Requirements

### Requirement: Audit RBAC for over-permissions
The system SHALL scan all Roles and RoleBindings in a given namespace and flag any rules that grant wildcard verbs (`*`), wildcard resources (`*`), access to sensitive resources (secrets, serviceaccounts/token, pods/exec, pods/attach), or escalation verbs (`bind`, `escalate`, `impersonate`). The system SHALL return a list of findings, each with the role name, the problematic rule, a severity level (high/medium/low), and a remediation suggestion. The system SHALL reject the operation if the target namespace is `tentacular-system`. The `audit_rbac` tool registration SHALL include `ToolAnnotations` with `ReadOnlyHint: true`, `OpenWorldHint: ptr(false)`, and `Title: "Audit RBAC permissions"`.

#### Scenario: Namespace with over-permissioned role
- **WHEN** the `audit_rbac` tool is called with `namespace: "dev-alice"` and a Role grants `*` verbs on secrets
- **THEN** the system returns a finding with severity `high`, the role name, the flagged rule, and a remediation suggestion

#### Scenario: Namespace with clean RBAC
- **WHEN** the `audit_rbac` tool is called and all Roles follow least-privilege principles
- **THEN** the system returns an empty findings list

#### Scenario: Also inspect ClusterRoleBindings targeting namespace
- **WHEN** the `audit_rbac` tool is called and a ClusterRoleBinding grants a ClusterRole to a ServiceAccount in the target namespace
- **THEN** the system includes any over-permissioned rules from the bound ClusterRole in the findings

#### Scenario: Detect escalation via bind verb
- **WHEN** the `audit_rbac` tool is called and a Role grants the `bind` verb on roles or clusterroles
- **THEN** the system returns a finding with severity `high` indicating privilege escalation risk and a remediation to remove the verb

#### Scenario: Detect escalation via escalate verb
- **WHEN** the `audit_rbac` tool is called and a Role grants the `escalate` verb
- **THEN** the system returns a finding with severity `high` indicating the role can modify its own permissions

#### Scenario: Detect impersonation capability
- **WHEN** the `audit_rbac` tool is called and a Role grants the `impersonate` verb on users, groups, or serviceaccounts
- **THEN** the system returns a finding with severity `high` indicating identity impersonation risk

#### Scenario: audit_rbac tool annotations are read-only
- **WHEN** the `audit_rbac` tool is retrieved from the server's tool list
- **THEN** `annotations.readOnlyHint` is `true` and `annotations.openWorldHint` is `false`

### Requirement: Audit network policy coverage
The system SHALL verify that a namespace has at least a default-deny NetworkPolicy (denying all ingress and egress) and report on all NetworkPolicies present. The system SHALL flag namespaces that allow unrestricted egress, have no NetworkPolicies at all, contain overly broad allow rules that negate default-deny, or allow cross-namespace ingress via empty `namespaceSelector`. The system SHALL return findings with remediation suggestions. The system SHALL reject the operation if the target namespace is `tentacular-system`. The `audit_netpol` tool registration SHALL include `ToolAnnotations` with `ReadOnlyHint: true`, `OpenWorldHint: ptr(false)`, and `Title: "Audit network policies"`.

#### Scenario: Namespace with default-deny policy
- **WHEN** the `audit_netpol` tool is called with `namespace: "dev-alice"` and a default-deny policy exists
- **THEN** the system returns `default_deny: true` and lists all NetworkPolicies with their policy types and pod selectors

#### Scenario: Namespace without network policies
- **WHEN** the `audit_netpol` tool is called and no NetworkPolicies exist in the namespace
- **THEN** the system returns `default_deny: false` with a finding flagging the namespace as having unrestricted network access and a remediation suggestion

#### Scenario: Namespace with partial coverage
- **WHEN** the `audit_netpol` tool is called and the namespace has ingress policies but no egress restriction
- **THEN** the system returns `default_deny: false` with a finding noting unrestricted egress

#### Scenario: Overly broad ingress allow rule
- **WHEN** the `audit_netpol` tool is called and a NetworkPolicy has an ingress rule with an empty peer (`from: [{}]`) that allows traffic from all sources
- **THEN** the system returns a finding with severity `high` indicating the rule negates default-deny

#### Scenario: Overly broad egress allow rule
- **WHEN** the `audit_netpol` tool is called and a NetworkPolicy has an egress rule with an empty peer (`to: [{}]`) that allows traffic to all destinations
- **THEN** the system returns a finding with severity `high` indicating the rule negates default-deny

#### Scenario: Cross-namespace ingress via empty namespaceSelector
- **WHEN** the `audit_netpol` tool is called and a NetworkPolicy allows ingress from all namespaces via an empty `namespaceSelector`
- **THEN** the system returns a finding with severity `medium` recommending restricting the selector

#### Scenario: audit_netpol tool annotations are read-only
- **WHEN** the `audit_netpol` tool is retrieved from the server's tool list
- **THEN** `annotations.readOnlyHint` is `true` and `annotations.openWorldHint` is `false`

### Requirement: Audit Pod Security Admission labels
The system SHALL check the Pod Security Admission labels on a namespace and report the enforce, audit, and warn levels. The system SHALL flag namespaces that do not have PSA enforce set to `restricted` or that have no PSA labels at all. The system SHALL treat `privileged` enforce level as high severity (all checks disabled) and `baseline` as medium severity. The system SHALL detect when audit or warn levels are weaker than the enforce level. The system SHALL return findings with remediation suggestions. The system SHALL reject the operation if the target namespace is `tentacular-system`. The `audit_psa` tool registration SHALL include `ToolAnnotations` with `ReadOnlyHint: true`, `OpenWorldHint: ptr(false)`, and `Title: "Audit pod security admission"`.

#### Scenario: Namespace with restricted PSA
- **WHEN** the `audit_psa` tool is called with `namespace: "dev-alice"` and PSA enforce is `restricted` with matching audit and warn levels
- **THEN** the system returns `compliant: true` with the enforce, audit, and warn levels and no findings

#### Scenario: Namespace with privileged PSA
- **WHEN** the `audit_psa` tool is called and PSA enforce is `privileged`
- **THEN** the system returns `compliant: false` with a high-severity finding indicating all pod security checks are disabled

#### Scenario: Namespace with baseline PSA
- **WHEN** the `audit_psa` tool is called and PSA enforce is `baseline`
- **THEN** the system returns `compliant: false` with a medium-severity finding recommending upgrade to `restricted`

#### Scenario: Namespace with no PSA labels
- **WHEN** the `audit_psa` tool is called and no PSA labels exist on the namespace
- **THEN** the system returns `compliant: false` with a high-severity finding noting the absence of PSA configuration

#### Scenario: Audit level weaker than enforce
- **WHEN** the `audit_psa` tool is called and the audit level is weaker than the enforce level (e.g. enforce=restricted, audit=baseline)
- **THEN** the system returns a medium-severity finding noting the audit log will not capture all violations

#### Scenario: Warn level weaker than enforce
- **WHEN** the `audit_psa` tool is called and the warn level is weaker than the enforce level
- **THEN** the system returns a medium-severity finding noting users will not see warnings for all enforced restrictions

#### Scenario: audit_psa tool annotations are read-only
- **WHEN** the `audit_psa` tool is retrieved from the server's tool list
- **THEN** `annotations.readOnlyHint` is `true` and `annotations.openWorldHint` is `false`
