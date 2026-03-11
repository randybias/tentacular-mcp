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

func TestCheckName_ValidNames(t *testing.T) {
	valid := []string{
		"my-workflow",
		"hello_world",
		"v1.2.3",
		"a",
		"A0",
		"my-app.v2",
	}
	for _, name := range valid {
		if err := guard.CheckName(name); err != nil {
			t.Errorf("CheckName(%q) returned unexpected error: %v", name, err)
		}
	}
}

func TestCheckName_InvalidNames(t *testing.T) {
	invalid := []string{
		"",
		"-starts-with-dash",
		"ends-with-dash-",
		"has spaces",
		"has,comma",
		"has=equals",
		"has!bang",
		"way-too-long-name-that-exceeds-sixty-three-characters-for-sure-and-then-some-more",
	}
	for _, name := range invalid {
		if err := guard.CheckName(name); err == nil {
			t.Errorf("CheckName(%q) expected error, got nil", name)
		}
	}
}
