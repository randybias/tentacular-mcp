package guard

import "fmt"

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
