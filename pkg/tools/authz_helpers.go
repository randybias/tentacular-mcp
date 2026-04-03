package tools

import (
	"context"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/randybias/tentacular-mcp/pkg/authz"
	"github.com/randybias/tentacular-mcp/pkg/exoskeleton"
	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

// errAmbiguousNamespace is returned when namespace auto-resolution finds more
// than one enclave the caller participates in.
var errAmbiguousNamespace = errors.New("namespace required: caller belongs to multiple enclaves — specify namespace explicitly")

// errNoDeployer is the sentinel error returned when a tool handler requires
// deployer identity but the request context has none (unauthenticated).
var errNoDeployer = errors.New("authentication required: no deployer identity in request context")

// requireDeployer returns errNoDeployer when deployer is nil and authz is
// enabled. Tool handlers that use deployer identity should call this at the
// top of their handler to fail fast on unauthenticated requests.
// When the evaluator is nil or disabled, nil deployer is allowed (authz off).
func requireDeployer(deployer *exoskeleton.DeployerInfo, eval *authz.Evaluator) error {
	if deployer != nil {
		return nil
	}
	if eval == nil || !eval.Enabled {
		return nil
	}
	return errNoDeployer
}

// checkAuthz performs enclave-aware authorization via CheckTentacle.
// All namespaces are expected to be enclave namespaces; non-enclave namespaces
// fall back to CheckEnclave which will deny if the enclave annotation is absent.
//
// nsAnnotations: annotations from the namespace the resource lives in.
// resourceAnnotations: annotations from the resource itself (e.g. Deployment).
// action: the action being authorized.
func checkAuthz(eval *authz.Evaluator, deployer *exoskeleton.DeployerInfo, nsAnnotations, resourceAnnotations map[string]string, action authz.Action) authz.Decision {
	return eval.CheckTentacle(deployer, nsAnnotations, resourceAnnotations, action)
}

// checkNamespaceAuthz evaluates whether a deployer can perform an action on a namespace.
// It reads the namespace's authz annotations and checks permission bits using the
// dual-path evaluator: enclave namespaces use CheckEnclave, legacy namespaces use Check.
// If the namespace has no owner annotation, it allows the action (pre-authz namespace).
func checkNamespaceAuthz(ctx context.Context, client *k8s.Client, namespace string, deployer *exoskeleton.DeployerInfo, eval *authz.Evaluator, action authz.Action) error {
	if eval == nil {
		return nil
	}

	ns, err := client.Clientset.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Namespace resource not found — treat as pre-authz (no owner),
			// which allows all callers. The namespace clearly exists if the caller
			// is operating on resources within it.
			return nil
		}
		return fmt.Errorf("get namespace %q: %w", namespace, err)
	}

	if ns.Annotations == nil {
		ns.Annotations = map[string]string{}
	}

	// All namespaces are expected to be enclaves; CheckEnclave handles both
	// cases (enclave: two-layer check; non-enclave: deny with clear reason).
	decision := eval.CheckEnclave(deployer, ns.Annotations, action)
	if !decision.Allowed {
		return fmt.Errorf("permission denied: %s", decision.Reason)
	}
	return nil
}

// resolveNamespace returns the provided namespace as-is, or auto-resolves it to
// the single enclave the deployer participates in when namespace is empty and the
// deployer has a known email (OIDC identity). Returns an error if the deployer
// has no email, belongs to no enclaves, or belongs to more than one enclave.
//
// Auto-resolution is a convenience feature for single-enclave deployments. When
// the caller belongs to exactly one enclave, the namespace can be omitted from
// wf_* tool calls.
func resolveNamespace(ctx context.Context, client *k8s.Client, namespace string, deployer *exoskeleton.DeployerInfo) (string, error) {
	if namespace != "" {
		return namespace, nil
	}
	if deployer == nil || deployer.Email == "" {
		return "", errors.New("namespace required: no OIDC identity available for auto-resolution")
	}

	namespaces, err := k8s.ListManagedNamespaces(ctx, client)
	if err != nil {
		return "", fmt.Errorf("resolve namespace: list managed namespaces: %w", err)
	}

	var matches []string
	for _, ns := range namespaces {
		ann := ns.Annotations
		if ann == nil {
			ann = map[string]string{}
		}
		if !authz.IsEnclave(ann) {
			continue
		}
		info := authz.ReadEnclaveInfo(ann)
		if isEnclaveParticipant(info, deployer.Email) {
			matches = append(matches, ns.Name)
		}
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("namespace required: caller %q belongs to no enclaves", deployer.Email)
	case 1:
		return matches[0], nil
	default:
		return "", errAmbiguousNamespace
	}
}

// fetchNamespaceAnnotations fetches the annotation map for a namespace.
// Returns an empty (non-nil) map if the namespace is not found or has no annotations.
// Returns an error only on unexpected API failures.
func fetchNamespaceAnnotations(ctx context.Context, client *k8s.Client, namespace string) (map[string]string, error) {
	ns, err := client.Clientset.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("get namespace %q: %w", namespace, err)
	}
	if ns.Annotations == nil {
		return map[string]string{}, nil
	}
	return ns.Annotations, nil
}
