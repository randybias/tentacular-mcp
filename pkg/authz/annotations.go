package authz

// Annotation key constants for authz-related Deployment annotations.
// All authz annotations use the tentacular.io/* domain.
const (
	// AnnotationOwnerSub is the OIDC subject of the deployer (used for owner identity check).
	AnnotationOwnerSub = "tentacular.io/owner-sub"

	// AnnotationOwnerEmail is the email of the deployer (display only).
	AnnotationOwnerEmail = "tentacular.io/owner-email"

	// AnnotationOwnerName is the display name of the deployer (display only).
	AnnotationOwnerName = "tentacular.io/owner-name"

	// AnnotationGroup is the single IdP group assigned to this resource.
	// Group-bit evaluation: slices.Contains(caller.Groups, annotation[AnnotationGroup]).
	AnnotationGroup = "tentacular.io/group"

	// AnnotationMode is the permission string (e.g. "rwxr-x---").
	AnnotationMode = "tentacular.io/mode"

	// AnnotationAuthProvider is the auth provider used at deploy time (e.g. "keycloak", "bearer-token").
	AnnotationAuthProvider = "tentacular.io/auth-provider"

	// AnnotationDefaultMode is the default permission mode for new tentacles (workflows) in a namespace.
	AnnotationDefaultMode = "tentacular.io/default-mode"

	// AnnotationDefaultGroup is the default IdP group for new tentacles in a namespace.
	AnnotationDefaultGroup = "tentacular.io/default-group"
)

// OwnerInfo holds the ownership and permission fields read from Deployment annotations.
type OwnerInfo struct {
	OwnerSub     string
	OwnerEmail   string
	OwnerName    string
	Group        string
	AuthProvider string
	// PresetName is the preset matching the mode, or "" if none matches.
	PresetName string
	Mode       Mode
}

// ReadOwnerInfo extracts authz ownership fields from a Deployment's annotation map.
// Missing or unparseable mode defaults to DefaultMode.
func ReadOwnerInfo(annotations map[string]string) OwnerInfo {
	info := OwnerInfo{
		OwnerSub:     annotations[AnnotationOwnerSub],
		OwnerEmail:   annotations[AnnotationOwnerEmail],
		OwnerName:    annotations[AnnotationOwnerName],
		Group:        annotations[AnnotationGroup],
		AuthProvider: annotations[AnnotationAuthProvider],
	}

	if raw, ok := annotations[AnnotationMode]; ok && raw != "" {
		if m, err := ParseMode(raw); err == nil {
			info.Mode = m
		} else {
			info.Mode = DefaultMode
		}
	} else {
		info.Mode = DefaultMode
	}

	info.PresetName = PresetName(info.Mode)
	return info
}

// GetAnnotation reads an annotation value by its tentacular.io/* key, falling
// back to the equivalent tentacular.dev/* key if the new key is absent or empty.
// This provides a graceful read-time migration for deployments annotated before
// the tentacular.io/* prefix was introduced. Write paths must NEVER use this
// fallback — writes always use the tentacular.io/* key only.
func GetAnnotation(annotations map[string]string, newKey string) string {
	if v := annotations[newKey]; v != "" {
		return v
	}
	// Build the fallback key by replacing "tentacular.io/" with "tentacular.dev/".
	if len(newKey) > 14 && newKey[:14] == "tentacular.io/" {
		oldKey := "tentacular.dev/" + newKey[14:]
		return annotations[oldKey]
	}
	return ""
}

// WriteOwnerAnnotations returns a map of annotation key→value for the authz
// fields to be stamped onto a Deployment at deploy time.
func WriteOwnerAnnotations(ownerSub, ownerEmail, ownerName, group string, mode Mode) map[string]string {
	return map[string]string{
		AnnotationOwnerSub:   ownerSub,
		AnnotationOwnerEmail: ownerEmail,
		AnnotationOwnerName:  ownerName,
		AnnotationGroup:      group,
		AnnotationMode:       mode.String(),
	}
}

// WriteNamespaceAnnotations returns a map of annotation key→value for the authz
// fields to be stamped onto a Namespace at creation time.
func WriteNamespaceAnnotations(ownerSub, ownerEmail, ownerName, group string, mode Mode, defaultGroup string, defaultMode Mode) map[string]string {
	annotations := map[string]string{
		AnnotationOwnerSub:   ownerSub,
		AnnotationOwnerEmail: ownerEmail,
		AnnotationOwnerName:  ownerName,
		AnnotationGroup:      group,
		AnnotationMode:       mode.String(),
	}
	if defaultGroup != "" {
		annotations[AnnotationDefaultGroup] = defaultGroup
	}
	if defaultMode != 0 {
		annotations[AnnotationDefaultMode] = defaultMode.String()
	}
	return annotations
}

// ReadNamespaceOwnerInfo extracts authz ownership fields from a Namespace's annotation map.
// Missing or unparseable mode defaults to DefaultMode.
func ReadNamespaceOwnerInfo(annotations map[string]string) OwnerInfo {
	info := OwnerInfo{
		OwnerSub:     annotations[AnnotationOwnerSub],
		OwnerEmail:   annotations[AnnotationOwnerEmail],
		OwnerName:    annotations[AnnotationOwnerName],
		Group:        annotations[AnnotationGroup],
		AuthProvider: annotations[AnnotationAuthProvider],
	}

	if raw, ok := annotations[AnnotationMode]; ok && raw != "" {
		if m, err := ParseMode(raw); err == nil {
			info.Mode = m
		} else {
			info.Mode = DefaultMode
		}
	} else {
		info.Mode = DefaultMode
	}

	info.PresetName = PresetName(info.Mode)
	return info
}
