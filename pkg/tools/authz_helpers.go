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

// checkAuthz performs dual-path authorization: CheckTentacle for enclave namespaces,
// Check for legacy namespaces. Returns the Decision.
//
// nsAnnotations: annotations from the namespace the resource lives in.
// resourceAnnotations: annotations from the resource itself (e.g. Deployment).
// action: the action being authorized.
//
// When nsAnnotations indicates an enclave namespace (tentacular.io/enclave is set),
// the enclave-aware two-layer CheckTentacle is used. Otherwise the legacy Check is used.
func checkAuthz(eval *authz.Evaluator, deployer *exoskeleton.DeployerInfo, nsAnnotations, resourceAnnotations map[string]string, action authz.Action) authz.Decision {
	if authz.IsEnclave(nsAnnotations) {
		return eval.CheckTentacle(deployer, nsAnnotations, resourceAnnotations, action)
	}
	return eval.Check(deployer, resourceAnnotations, action)
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

	var decision authz.Decision
	if authz.IsEnclave(ns.Annotations) {
		decision = eval.CheckEnclave(deployer, ns.Annotations, action)
	} else {
		decision = eval.Check(deployer, ns.Annotations, action)
	}
	if !decision.Allowed {
		return fmt.Errorf("permission denied: %s", decision.Reason)
	}
	return nil
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
