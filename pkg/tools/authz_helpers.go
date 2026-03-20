package tools

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/randybias/tentacular-mcp/pkg/authz"
	"github.com/randybias/tentacular-mcp/pkg/exoskeleton"
	"github.com/randybias/tentacular-mcp/pkg/k8s"
)

// checkNamespaceAuthz evaluates whether a deployer can perform an action on a namespace.
// It reads the namespace's authz annotations and checks permission bits.
// If the namespace has no owner-sub annotation, it allows the action (pre-authz namespace).
func checkNamespaceAuthz(ctx context.Context, client *k8s.Client, namespace string, deployer *exoskeleton.DeployerInfo, eval *authz.Evaluator, action authz.Action) error {
	if eval == nil || deployer == nil {
		return nil
	}

	ns, err := client.Clientset.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// Namespace resource not found — treat as pre-authz (no owner-sub),
			// which allows all callers. The namespace clearly exists if the caller
			// is operating on resources within it.
			return nil
		}
		return fmt.Errorf("get namespace %q: %w", namespace, err)
	}

	if ns.Annotations == nil {
		ns.Annotations = map[string]string{}
	}

	decision := eval.Check(deployer, ns.Annotations, action)
	if !decision.Allowed {
		return fmt.Errorf("permission denied: %s", decision.Reason)
	}
	return nil
}
