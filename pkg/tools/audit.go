package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/randybias/tentacular-mcp/pkg/guard"
	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

// AuditRbacParams are the parameters for audit_rbac.
type AuditRbacParams struct {
	Namespace string `json:"namespace" jsonschema:"Namespace to audit RBAC in"`
}

// AuditFinding is a single RBAC audit finding.
type AuditFinding struct {
	Role        string `json:"role"`
	Rule        string `json:"rule"`
	Severity    string `json:"severity"`
	Reason      string `json:"reason"`
	Remediation string `json:"remediation,omitempty"`
}

// AuditRbacResult is the result of audit_rbac.
type AuditRbacResult struct {
	Findings []AuditFinding `json:"findings"`
}

// AuditNetpolParams are the parameters for audit_netpol.
type AuditNetpolParams struct {
	Namespace string `json:"namespace" jsonschema:"Namespace to audit network policies in"`
}

// NetpolInfo is a single network policy in the audit result.
type NetpolInfo struct {
	Name        string   `json:"name"`
	PodSelector string   `json:"pod_selector"`
	Types       []string `json:"types"`
}

// AuditNetpolFinding is a single netpol audit finding.
type AuditNetpolFinding struct {
	Severity    string `json:"severity"`
	Message     string `json:"message"`
	Remediation string `json:"remediation,omitempty"`
}

// AuditNetpolResult is the result of audit_netpol.
type AuditNetpolResult struct {
	Policies    []NetpolInfo         `json:"policies"`
	Findings    []AuditNetpolFinding `json:"findings"`
	DefaultDeny bool                 `json:"default_deny"`
}

// AuditPsaParams are the parameters for audit_psa.
type AuditPsaParams struct {
	Namespace string `json:"namespace" jsonschema:"Namespace to audit Pod Security Admission configuration in"`
}

// AuditPsaFinding is a single PSA audit finding.
type AuditPsaFinding struct {
	Severity    string `json:"severity"`
	Message     string `json:"message"`
	Remediation string `json:"remediation,omitempty"`
}

// AuditPsaResult is the result of audit_psa.
type AuditPsaResult struct {
	Enforce   string            `json:"enforce"`
	Audit     string            `json:"audit"`
	Warn      string            `json:"warn"`
	Findings  []AuditPsaFinding `json:"findings"`
	Compliant bool              `json:"compliant"`
}

func registerAuditTools(srv *mcp.Server, client *k8s.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "audit_rbac",
		Description: "Audit RBAC in a namespace: scan for wildcard verbs/resources, sensitive access, escalation paths (bind/escalate/impersonate verbs), and ClusterRoleBindings targeting namespace service accounts. Returns findings with remediation suggestions.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Audit RBAC Configuration",
			ReadOnlyHint:    true,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, params AuditRbacParams) (*mcp.CallToolResult, AuditRbacResult, error) {
		if err := guard.CheckNamespace(params.Namespace); err != nil {
			return nil, AuditRbacResult{}, err
		}
		result, err := handleAuditRbac(ctx, client, params)
		return nil, result, err
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "audit_netpol",
		Description: "Audit network policies in a namespace: check for default-deny policy, missing egress restrictions, overly broad allow rules, cross-namespace ingress via empty namespaceSelector, and list all policies. Returns findings with remediation suggestions.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Audit Network Policies",
			ReadOnlyHint:    true,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, params AuditNetpolParams) (*mcp.CallToolResult, AuditNetpolResult, error) {
		if err := guard.CheckNamespace(params.Namespace); err != nil {
			return nil, AuditNetpolResult{}, err
		}
		result, err := handleAuditNetpol(ctx, client, params)
		return nil, result, err
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "audit_psa",
		Description: "Audit Pod Security Admission labels on a namespace: check enforce/audit/warn levels, flag privileged or missing enforcement, detect audit/warn level mismatches, and return remediation suggestions.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Audit Pod Security Admission",
			ReadOnlyHint:    true,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, params AuditPsaParams) (*mcp.CallToolResult, AuditPsaResult, error) {
		if err := guard.CheckNamespace(params.Namespace); err != nil {
			return nil, AuditPsaResult{}, err
		}
		result, err := handleAuditPsa(ctx, client, params)
		return nil, result, err
	})
}

var sensitiveResources = map[string]bool{
	"secrets":               true,
	"serviceaccounts/token": true,
	"pods/exec":             true,
	"pods/attach":           true,
}

// escalationVerbs are verbs that enable privilege escalation in RBAC.
var escalationVerbs = map[string]bool{
	"bind":        true,
	"escalate":    true,
	"impersonate": true,
}

func handleAuditRbac(ctx context.Context, client *k8s.Client, params AuditRbacParams) (AuditRbacResult, error) {
	findings := []AuditFinding{}

	// Scan Roles
	roles, err := client.Clientset.RbacV1().Roles(params.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return AuditRbacResult{}, fmt.Errorf("list roles in namespace %q: %w", params.Namespace, err)
	}

	for _, role := range roles.Items {
		roleName := "Role/" + role.Name
		findings = auditRules(findings, roleName, role.Rules)
	}

	// Scan RoleBindings for ClusterRole bindings (which may escalate privileges)
	rbs, err := client.Clientset.RbacV1().RoleBindings(params.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return AuditRbacResult{}, fmt.Errorf("list rolebindings in namespace %q: %w", params.Namespace, err)
	}
	for _, rb := range rbs.Items {
		if rb.RoleRef.Kind == "ClusterRole" {
			findings = append(findings, AuditFinding{
				Role:        "RoleBinding/" + rb.Name,
				Rule:        "binds ClusterRole/" + rb.RoleRef.Name,
				Severity:    "low",
				Reason:      "RoleBinding references a ClusterRole; review cluster-level permissions",
				Remediation: "Replace with a namespaced Role if the required permissions are namespace-scoped.",
			})
		}
	}

	// Inspect ClusterRoleBindings targeting namespace ServiceAccounts
	crbs, err := client.Clientset.RbacV1().ClusterRoleBindings().List(ctx, metav1.ListOptions{})
	if err != nil {
		return AuditRbacResult{}, fmt.Errorf("list cluster role bindings: %w", err)
	}
	for _, crb := range crbs.Items {
		for _, subj := range crb.Subjects {
			if subj.Kind == "ServiceAccount" && subj.Namespace == params.Namespace {
				findings = append(findings, AuditFinding{
					Role:        "ClusterRoleBinding/" + crb.Name,
					Rule:        fmt.Sprintf("binds ClusterRole/%s to SA %s/%s", crb.RoleRef.Name, params.Namespace, subj.Name),
					Severity:    "medium",
					Reason:      "service account in namespace has cluster-wide permissions",
					Remediation: "Replace with a namespaced RoleBinding unless cluster-wide access is required.",
				})
			}
		}
	}

	return AuditRbacResult{Findings: findings}, nil
}

// auditRules checks a set of RBAC policy rules for security issues and appends findings.
func auditRules(findings []AuditFinding, roleName string, rules []rbacv1.PolicyRule) []AuditFinding {
	for _, rule := range rules {
		ruleDesc := ruleDescription(rule)

		// Check wildcard verbs
		for _, verb := range rule.Verbs {
			if verb == "*" {
				findings = append(findings, AuditFinding{
					Role:        roleName,
					Rule:        ruleDesc,
					Severity:    "high",
					Reason:      "wildcard verb grants all permissions",
					Remediation: "Replace '*' with explicit verbs (e.g. get, list, watch, create, update, delete).",
				})
			}
		}

		// Check escalation verbs (bind, escalate, impersonate)
		for _, verb := range rule.Verbs {
			if escalationVerbs[verb] {
				findings = append(findings, AuditFinding{
					Role:        roleName,
					Rule:        ruleDesc,
					Severity:    "high",
					Reason:      fmt.Sprintf("%q verb enables privilege escalation", verb),
					Remediation: fmt.Sprintf("Remove the %q verb unless this role explicitly needs to grant or assume other identities.", verb),
				})
			}
		}

		// Check wildcard resources and sensitive resources
		for _, res := range rule.Resources {
			if res == "*" {
				findings = append(findings, AuditFinding{
					Role:        roleName,
					Rule:        ruleDesc,
					Severity:    "high",
					Reason:      "wildcard resource grants access to all resource types",
					Remediation: "Replace '*' with the specific resources this role needs access to.",
				})
			}
			if sensitiveResources[res] {
				findings = append(findings, AuditFinding{
					Role:        roleName,
					Rule:        ruleDesc,
					Severity:    "medium",
					Reason:      fmt.Sprintf("access to sensitive resource %q", res),
					Remediation: fmt.Sprintf("Restrict %q access to only the verbs required (e.g. get instead of list/watch).", res),
				})
			}
		}
	}
	return findings
}

func ruleDescription(rule rbacv1.PolicyRule) string {
	groups := strings.Join(rule.APIGroups, ",")
	if groups == "" {
		groups = "core"
	}
	resources := strings.Join(rule.Resources, ",")
	verbs := strings.Join(rule.Verbs, ",")
	return fmt.Sprintf("apiGroups=[%s] resources=[%s] verbs=[%s]", groups, resources, verbs)
}

func handleAuditNetpol(ctx context.Context, client *k8s.Client, params AuditNetpolParams) (AuditNetpolResult, error) {
	policies, err := client.Clientset.NetworkingV1().NetworkPolicies(params.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return AuditNetpolResult{}, fmt.Errorf("list network policies in namespace %q: %w", params.Namespace, err)
	}

	findings := []AuditNetpolFinding{}
	defaultDeny := false
	hasEgressRestriction := false

	policyInfos := make([]NetpolInfo, 0, len(policies.Items))
	for _, p := range policies.Items {
		types := make([]string, 0, len(p.Spec.PolicyTypes))
		for _, t := range p.Spec.PolicyTypes {
			types = append(types, string(t))
		}

		// Check if this is a default-deny policy (selects all pods, blocks ingress and egress)
		if len(p.Spec.PodSelector.MatchLabels) == 0 && len(p.Spec.PodSelector.MatchExpressions) == 0 &&
			len(p.Spec.Ingress) == 0 && len(p.Spec.Egress) == 0 {
			hasIngress := false
			hasEgress := false
			for _, t := range p.Spec.PolicyTypes {
				if t == "Ingress" {
					hasIngress = true
				}
				if t == "Egress" {
					hasEgress = true
				}
			}
			if hasIngress && hasEgress {
				defaultDeny = true
			}
		}

		// Check for egress restrictions
		for _, t := range p.Spec.PolicyTypes {
			if t == "Egress" {
				hasEgressRestriction = true
			}
		}

		// Check for overly broad ingress allow rules
		for _, ingress := range p.Spec.Ingress {
			for _, from := range ingress.From {
				if isAllowAll(from.PodSelector, from.NamespaceSelector, from.IPBlock) {
					findings = append(findings, AuditNetpolFinding{
						Severity:    "high",
						Message:     fmt.Sprintf("NetworkPolicy %q has an ingress rule that allows traffic from all sources, negating default-deny", p.Name),
						Remediation: "Restrict the ingress rule to specific pod/namespace selectors or CIDR ranges.",
					})
				} else if from.NamespaceSelector != nil &&
					len(from.NamespaceSelector.MatchLabels) == 0 &&
					len(from.NamespaceSelector.MatchExpressions) == 0 &&
					from.PodSelector == nil {
					findings = append(findings, AuditNetpolFinding{
						Severity:    "medium",
						Message:     fmt.Sprintf("NetworkPolicy %q allows ingress from all namespaces via empty namespaceSelector", p.Name),
						Remediation: "Add matchLabels to the namespaceSelector to restrict which namespaces can send traffic.",
					})
				}
			}
		}

		// Check for overly broad egress allow rules
		for _, egress := range p.Spec.Egress {
			for _, to := range egress.To {
				if isAllowAll(to.PodSelector, to.NamespaceSelector, to.IPBlock) {
					findings = append(findings, AuditNetpolFinding{
						Severity:    "high",
						Message:     fmt.Sprintf("NetworkPolicy %q has an egress rule that allows traffic to all destinations, negating default-deny", p.Name),
						Remediation: "Restrict the egress rule to specific pod/namespace selectors or CIDR ranges.",
					})
				}
			}
		}

		selectorStr := "all pods"
		if len(p.Spec.PodSelector.MatchLabels) > 0 {
			parts := []string{}
			for k, v := range p.Spec.PodSelector.MatchLabels {
				parts = append(parts, k+"="+v)
			}
			selectorStr = strings.Join(parts, ",")
		}

		policyInfos = append(policyInfos, NetpolInfo{
			Name:        p.Name,
			Types:       types,
			PodSelector: selectorStr,
		})
	}

	if !defaultDeny {
		findings = append(findings, AuditNetpolFinding{
			Severity:    "high",
			Message:     "no default-deny NetworkPolicy found; traffic is unrestricted by default",
			Remediation: "Create a NetworkPolicy with empty podSelector and both Ingress+Egress policyTypes with no allow rules.",
		})
	}

	if !hasEgressRestriction {
		findings = append(findings, AuditNetpolFinding{
			Severity:    "medium",
			Message:     "no egress NetworkPolicy found; pods may be able to reach external services",
			Remediation: "Add a NetworkPolicy with policyType Egress to control outbound traffic.",
		})
	}

	return AuditNetpolResult{
		DefaultDeny: defaultDeny,
		Policies:    policyInfos,
		Findings:    findings,
	}, nil
}

// isAllowAll returns true when a NetworkPolicy peer matches all traffic.
// This happens when all selector/IPBlock fields are nil (the peer is `{}`),
// which Kubernetes interprets as "allow from/to everywhere".
func isAllowAll(podSel, nsSel *metav1.LabelSelector, ipBlock *networkingv1.IPBlock) bool {
	return podSel == nil && nsSel == nil && ipBlock == nil
}

// psaLevelOrder maps PSA levels to a numeric rank for comparison.
// Higher rank = more restrictive.
var psaLevelOrder = map[string]int{
	"privileged": 0,
	"baseline":   1,
	"restricted": 2,
}

func handleAuditPsa(ctx context.Context, client *k8s.Client, params AuditPsaParams) (AuditPsaResult, error) {
	ns, err := k8s.GetNamespace(ctx, client, params.Namespace)
	if err != nil {
		return AuditPsaResult{}, err
	}

	labels := ns.Labels
	enforce := labels["pod-security.kubernetes.io/enforce"]
	audit := labels["pod-security.kubernetes.io/audit"]
	warn := labels["pod-security.kubernetes.io/warn"]

	findings := []AuditPsaFinding{}
	compliant := true

	if enforce == "" {
		findings = append(findings, AuditPsaFinding{
			Severity:    "high",
			Message:     "pod-security.kubernetes.io/enforce label is missing; no PSA enforcement active",
			Remediation: "Add the label pod-security.kubernetes.io/enforce=restricted to the namespace.",
		})
		compliant = false
	} else if enforce == "privileged" {
		findings = append(findings, AuditPsaFinding{
			Severity:    "high",
			Message:     "PSA enforce level is \"privileged\"; all pod security checks are disabled",
			Remediation: "Set pod-security.kubernetes.io/enforce to \"restricted\" (or \"baseline\" as a first step).",
		})
		compliant = false
	} else if enforce != "restricted" {
		findings = append(findings, AuditPsaFinding{
			Severity:    "medium",
			Message:     fmt.Sprintf("PSA enforce level is %q; recommended level is \"restricted\"", enforce),
			Remediation: "Set pod-security.kubernetes.io/enforce to \"restricted\" for full pod security enforcement.",
		})
		compliant = false
	}

	if audit == "" {
		findings = append(findings, AuditPsaFinding{
			Severity:    "low",
			Message:     "pod-security.kubernetes.io/audit label is missing",
			Remediation: "Add pod-security.kubernetes.io/audit=restricted to log PSA violations in the audit log.",
		})
	}

	if warn == "" {
		findings = append(findings, AuditPsaFinding{
			Severity:    "low",
			Message:     "pod-security.kubernetes.io/warn label is missing",
			Remediation: "Add pod-security.kubernetes.io/warn=restricted so users see warnings when deploying non-compliant pods.",
		})
	}

	// Check audit/warn level mismatch: if they are weaker than enforce,
	// users won't get dry-run feedback for the enforced policy.
	if enforce != "" {
		enforceRank := psaLevelOrder[enforce]
		if audit != "" && psaLevelOrder[audit] < enforceRank {
			findings = append(findings, AuditPsaFinding{
				Severity:    "medium",
				Message:     fmt.Sprintf("audit level %q is weaker than enforce level %q; audit log will not capture all violations", audit, enforce),
				Remediation: fmt.Sprintf("Set pod-security.kubernetes.io/audit to %q or stricter to match enforcement.", enforce),
			})
		}
		if warn != "" && psaLevelOrder[warn] < enforceRank {
			findings = append(findings, AuditPsaFinding{
				Severity:    "medium",
				Message:     fmt.Sprintf("warn level %q is weaker than enforce level %q; users will not see warnings for all enforced restrictions", warn, enforce),
				Remediation: fmt.Sprintf("Set pod-security.kubernetes.io/warn to %q or stricter to match enforcement.", enforce),
			})
		}
	}

	return AuditPsaResult{
		Compliant: compliant,
		Enforce:   enforce,
		Audit:     audit,
		Warn:      warn,
		Findings:  findings,
	}, nil
}
