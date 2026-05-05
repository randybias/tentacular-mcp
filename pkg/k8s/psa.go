package k8s

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	// PSA label keys on the namespace.
	PSAEnforceLabel        = "pod-security.kubernetes.io/enforce"
	PSAEnforceVersionLabel = "pod-security.kubernetes.io/enforce-version"

	// PSA policy levels.
	PSALevelPrivileged = "privileged"
	PSALevelBaseline   = "baseline"
	PSALevelRestricted = "restricted"
)

// PSAViolation describes a single PSA requirement that is not met by a manifest.
type PSAViolation struct {
	// ManifestKind is the Kubernetes Kind of the violating manifest.
	ManifestKind string
	// ManifestName is the metadata.name of the violating manifest.
	ManifestName string
	// Field is the spec path that is missing or incorrect.
	Field string
	// Reason is a human-readable explanation of the requirement.
	Reason string
}

func (v PSAViolation) String() string {
	return v.ManifestKind + "/" + v.ManifestName + ": " + v.Field + " — " + v.Reason
}

// PSAValidationError is returned when one or more PSA violations are found.
// It carries a structured list so callers can surface each issue separately.
type PSAValidationError struct {
	Namespace  string
	Level      string
	Violations []PSAViolation
}

func (e *PSAValidationError) Error() string {
	msgs := make([]string, 0, len(e.Violations)+1)
	msgs = append(msgs, fmt.Sprintf(
		"PSA validation failed for namespace %q (enforce=%s): %d violation(s)",
		e.Namespace, e.Level, len(e.Violations),
	))
	for _, v := range e.Violations {
		msgs = append(msgs, "  - "+v.String())
	}
	return strings.Join(msgs, "\n")
}

// NamespacePSALevel fetches the PSA enforce level for the given namespace.
// Returns PSALevelPrivileged when the enforce label is absent (permissive default).
func NamespacePSALevel(ctx context.Context, client *Client, namespace string) (string, error) {
	ns, err := client.Clientset.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("get namespace %q for PSA level: %w", namespace, err)
	}
	level, ok := ns.Labels[PSAEnforceLabel]
	if !ok || level == "" {
		return PSALevelPrivileged, nil
	}
	return level, nil
}

// ValidatePSA checks whether the provided manifests satisfy the given PSA
// enforce level. Only Deployment, Job, and CronJob manifests are inspected —
// other kinds are skipped.
//
// For "restricted" level the following are required per container and/or pod:
//   - pod or container securityContext.runAsNonRoot: true
//   - container securityContext.allowPrivilegeEscalation: false
//   - container securityContext.capabilities.drop contains "ALL"
//   - pod or container securityContext.seccompProfile.type set to
//     "RuntimeDefault" or "Localhost"
//
// NOTE: Image Config.User inspection is intentionally skipped — callers must
// set explicit runAsNonRoot in the spec. YAGNI: image-inspect can be added
// later if needed.
//
// Returns nil when the level is "privileged" (skip validation).
// Returns *PSAValidationError listing every failing field when violations exist.
func ValidatePSA(namespace, level string, manifests []map[string]any) error {
	if level == "" || level == PSALevelPrivileged {
		return nil
	}

	var violations []PSAViolation

	for _, m := range manifests {
		obj := &unstructured.Unstructured{Object: m}
		kind := obj.GetKind()
		name := obj.GetName()

		var podSpecPath []string
		switch kind {
		case "Deployment", "Job":
			podSpecPath = []string{"spec", "template", "spec"}
		case "CronJob":
			podSpecPath = []string{"spec", "jobTemplate", "spec", "template", "spec"}
		default:
			continue
		}

		vs := validatePodSpec(obj.Object, podSpecPath, kind, name)
		violations = append(violations, vs...)
	}

	if len(violations) > 0 {
		return &PSAValidationError{
			Namespace:  namespace,
			Level:      level,
			Violations: violations,
		}
	}
	return nil
}

// validatePodSpec checks the pod-level and container-level security contexts for
// a workload manifest. The podSpecPath is the dot-path to the PodSpec within the
// object (e.g., ["spec","template","spec"] for Deployments).
func validatePodSpec(obj map[string]any, podSpecPath []string, kind, name string) []PSAViolation {
	var violations []PSAViolation

	scPath := append(append([]string{}, podSpecPath...), "securityContext")

	// ---- Pod-level checks (restricted) ----

	// runAsNonRoot: must be true at pod level OR on every container.
	// We check pod-level here; container-level is checked per-container below.
	podRunAsNonRoot, podRunAsNonRootFound, _ := unstructured.NestedBool(obj, append(append([]string{}, scPath...), "runAsNonRoot")...)

	// seccompProfile: must be set at pod OR container level.
	podSeccompType, podSeccompFound, _ := unstructured.NestedString(obj, append(append([]string{}, scPath...), "seccompProfile", "type")...)
	podSeccompOK := podSeccompFound && isValidSeccompType(podSeccompType)

	// ---- Container-level checks ----
	for _, field := range []string{"containers", "initContainers"} {
		containersRaw, found, _ := unstructured.NestedSlice(obj, append(append([]string{}, podSpecPath...), field)...)
		if !found {
			continue
		}

		for i, cRaw := range containersRaw {
			c, ok := cRaw.(map[string]any)
			if !ok {
				continue
			}

			containerName := fmt.Sprintf("%s[%d]", field, i)
			if n, nok, _ := unstructured.NestedString(c, "name"); nok && n != "" {
				containerName = n
			}

			qualifier := fmt.Sprintf("%s/%s container %q", kind, name, containerName)

			// runAsNonRoot: required at pod OR container level.
			cRunAsNonRoot, cRunAsNonRootFound, _ := unstructured.NestedBool(c, "securityContext", "runAsNonRoot")
			podSatisfied := podRunAsNonRootFound && podRunAsNonRoot
			containerSatisfied := cRunAsNonRootFound && cRunAsNonRoot
			if !podSatisfied && !containerSatisfied {
				violations = append(violations, PSAViolation{
					ManifestKind: kind,
					ManifestName: name,
					Field:        "securityContext.runAsNonRoot",
					Reason:       qualifier + ": runAsNonRoot must be true at pod or container level (restricted PSA)",
				})
			}

			// allowPrivilegeEscalation: must be false at container level.
			ape, apeFound, _ := unstructured.NestedBool(c, "securityContext", "allowPrivilegeEscalation")
			if !apeFound || ape {
				violations = append(violations, PSAViolation{
					ManifestKind: kind,
					ManifestName: name,
					Field:        "securityContext.allowPrivilegeEscalation",
					Reason:       qualifier + ": allowPrivilegeEscalation must be false (restricted PSA)",
				})
			}

			// capabilities.drop: must include ALL.
			drop, dropFound, _ := unstructured.NestedStringSlice(c, "securityContext", "capabilities", "drop")
			if !dropFound || !containsALL(drop) {
				violations = append(violations, PSAViolation{
					ManifestKind: kind,
					ManifestName: name,
					Field:        "securityContext.capabilities.drop",
					Reason:       qualifier + ": capabilities.drop must contain \"ALL\" (restricted PSA)",
				})
			}

			// seccompProfile: must be set at pod OR container level.
			cSeccompType, cSeccompFound, _ := unstructured.NestedString(c, "securityContext", "seccompProfile", "type")
			cSeccompOK := cSeccompFound && isValidSeccompType(cSeccompType)
			if !podSeccompOK && !cSeccompOK {
				violations = append(violations, PSAViolation{
					ManifestKind: kind,
					ManifestName: name,
					Field:        "securityContext.seccompProfile.type",
					Reason:       qualifier + ": seccompProfile.type must be RuntimeDefault or Localhost at pod or container level (restricted PSA)",
				})
			}
		}
	}

	return violations
}

// isValidSeccompType returns true for the two PSA-allowed seccomp types.
func isValidSeccompType(t string) bool {
	return t == "RuntimeDefault" || t == "Localhost"
}

// containsALL returns true when "ALL" appears in the capabilities drop list.
func containsALL(drop []string) bool {
	for _, d := range drop {
		if strings.EqualFold(d, "ALL") {
			return true
		}
	}
	return false
}
