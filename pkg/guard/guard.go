package guard

import (
	"fmt"
	"regexp"
)

// systemNamespaces is the set of namespaces that tentacular must never touch.
// These are either Kubernetes control-plane namespaces or the tentacular
// server's own namespace.
var systemNamespaces = map[string]bool{
	"tentacular-system":      true,
	"tentacular-support":     true,
	"tentacular-exoskeleton": true,
	"kube-system":            true,
	"kube-public":            true,
	"kube-node-lease":        true,
	"default":                true,
}

// CheckNamespace returns an error if the given namespace is a protected
// system namespace. All tool handlers must call this before performing
// operations.
func CheckNamespace(namespace string) error {
	if systemNamespaces[namespace] {
		return fmt.Errorf("operations on namespace %q are not permitted", namespace)
	}
	return nil
}

// validLabelValue matches strings that are valid Kubernetes label values:
// alphanumeric, '-', '_', '.', max 63 chars, must start and end with alphanumeric.
var validLabelValue = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9._-]{0,61}[a-zA-Z0-9])?$`)

// CheckName returns an error if the given name is not a valid Kubernetes
// label value. Names are used in label selectors and must be safe.
func CheckName(name string) error {
	if name == "" {
		return fmt.Errorf("name must not be empty")
	}
	if !validLabelValue.MatchString(name) {
		return fmt.Errorf("name %q is not a valid Kubernetes label value", name)
	}
	return nil
}
