// Tests for the requireDeployer guard and nil-deployer behavior across handlers.
//
// Covers:
//   - requireDeployer: nil deployer with authz enabled/disabled, non-nil deployer
//   - Nil-deployer rejection on deploy handlers (wf_apply, wf_remove, wf_status)
//   - Nil-deployer rejection on discover handlers (wf_describe)

package tools

import (
	"context"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/randybias/tentacular-mcp/pkg/authz"
	"github.com/randybias/tentacular-mcp/pkg/exoskeleton"
)

// --- requireDeployer unit tests ---

func TestRequireDeployer_NilDeployer_AuthzEnabled(t *testing.T) {
	eval := authz.NewEvaluator(authz.DefaultMode)
	err := requireDeployer(nil, eval)
	if err == nil {
		t.Error("expected error for nil deployer with authz enabled")
	}
	if !errors.Is(err, errNoDeployer) {
		t.Errorf("expected errNoDeployer, got: %v", err)
	}
}

func TestRequireDeployer_NilDeployer_AuthzDisabled(t *testing.T) {
	eval := &authz.Evaluator{Enabled: false}
	err := requireDeployer(nil, eval)
	if err != nil {
		t.Errorf("expected nil error with authz disabled, got: %v", err)
	}
}

func TestRequireDeployer_NilDeployer_NilEval(t *testing.T) {
	err := requireDeployer(nil, nil)
	if err != nil {
		t.Errorf("expected nil error with nil eval, got: %v", err)
	}
}

func TestRequireDeployer_NonNilDeployer_AuthzEnabled(t *testing.T) {
	eval := authz.NewEvaluator(authz.DefaultMode)
	deployer := &exoskeleton.DeployerInfo{Subject: "test", Provider: "keycloak"}
	err := requireDeployer(deployer, eval)
	if err != nil {
		t.Errorf("expected nil error for non-nil deployer, got: %v", err)
	}
}

// --- Nil-deployer rejection on deploy/discover handlers ---

func TestWfDescribe_NilDeployer_AuthzEnabled_Denied(t *testing.T) {
	client := newWfTestClient()
	ctx := context.Background()

	nsWithAuthz(t, client, "desc-nil-ns", "owner@example.com", "", "rwx------")
	dep := makeTestDeployment("desc-nil-wf", "desc-nil-ns", map[string]string{
		authz.AnnotationOwner:    "owner@example.com",
		authz.AnnotationOwnerSub: "sub-owner",
		authz.AnnotationMode:     "rwx------",
	})
	_, _ = client.Clientset.AppsV1().Deployments("desc-nil-ns").Create(ctx, dep, metav1.CreateOptions{})

	eval := authz.NewEvaluator(authz.DefaultMode)
	_, err := handleWfDescribe(ctx, client, WfDescribeParams{Namespace: "desc-nil-ns", Name: "desc-nil-wf"}, nil, eval)
	if err == nil {
		t.Error("expected error for nil deployer on wf_describe with authz enabled")
	}
}
