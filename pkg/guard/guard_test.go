package guard_test

import (
	"testing"

	"github.com/randybias/tentacular-mcp/pkg/guard"
)

func TestCheckNamespace_SystemNamespacesRejected(t *testing.T) {
	blocked := []string{
		"tentacular-system",
		"tentacular-support",
		"tentacular-exoskeleton",
		"kube-system",
		"kube-public",
		"kube-node-lease",
		"default",
	}
	for _, ns := range blocked {
		if err := guard.CheckNamespace(ns); err == nil {
			t.Errorf("CheckNamespace(%q) expected error, got nil", ns)
		}
	}
}

func TestCheckNamespace_UserNamespacePasses(t *testing.T) {
	allowed := []string{
		"production",
		"my-workflow-ns",
		"tentacular-user",
		"tent-wfs",
		"foo-bar",
	}
	for _, ns := range allowed {
		if err := guard.CheckNamespace(ns); err != nil {
			t.Errorf("CheckNamespace(%q) returned unexpected error: %v", ns, err)
		}
	}
}
