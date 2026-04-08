package authz

import (
	"testing"
)

// --- ReadOwnerInfo ---

func TestReadOwnerInfo_FullAnnotations(t *testing.T) {
	ann := map[string]string{
		AnnotationOwnerSub:     "sub-abc123",
		AnnotationOwnerEmail:   "alice@example.com",
		AnnotationOwnerName:    "Alice",
		AnnotationGroup:        "platform-team",
		AnnotationMode:         "rwxr-x---",
		AnnotationAuthProvider: "keycloak",
	}
	info := ReadOwnerInfo(ann)

	if info.OwnerSub != "sub-abc123" {
		t.Errorf("OwnerSub = %q, want %q", info.OwnerSub, "sub-abc123")
	}
	if info.OwnerEmail != "alice@example.com" {
		t.Errorf("OwnerEmail = %q, want %q", info.OwnerEmail, "alice@example.com")
	}
	if info.OwnerName != "Alice" {
		t.Errorf("OwnerName = %q, want %q", info.OwnerName, "Alice")
	}
	if info.Group != "platform-team" {
		t.Errorf("Group = %q, want %q", info.Group, "platform-team")
	}
	if info.AuthProvider != "keycloak" {
		t.Errorf("AuthProvider = %q, want %q", info.AuthProvider, "keycloak")
	}

	want, _ := ParseMode("rwxr-x---")
	if info.Mode != want {
		t.Errorf("Mode = %v, want %v", info.Mode.String(), want.String())
	}
	// rwxr-x--- maps to member-read (takes precedence over group-read alias).
	if info.PresetName != "member-read" {
		t.Errorf("PresetName = %q, want 'member-read'", info.PresetName)
	}
}

func TestReadOwnerInfo_MissingMode_DefaultsToDefaultMode(t *testing.T) {
	ann := map[string]string{
		AnnotationOwnerSub: "sub-xyz",
	}
	info := ReadOwnerInfo(ann)

	if info.Mode != DefaultMode {
		t.Errorf("Mode = %v, want DefaultMode (%v)", info.Mode.String(), DefaultMode.String())
	}
}

func TestReadOwnerInfo_EmptyModeAnnotation_DefaultsToDefaultMode(t *testing.T) {
	ann := map[string]string{
		AnnotationOwnerSub: "sub-xyz",
		AnnotationMode:     "",
	}
	info := ReadOwnerInfo(ann)

	if info.Mode != DefaultMode {
		t.Errorf("Mode = %v, want DefaultMode for empty mode annotation", info.Mode.String())
	}
}

func TestReadOwnerInfo_InvalidModeAnnotation_DefaultsToDefaultMode(t *testing.T) {
	ann := map[string]string{
		AnnotationOwnerSub: "sub-xyz",
		AnnotationMode:     "not-valid-mode",
	}
	info := ReadOwnerInfo(ann)

	if info.Mode != DefaultMode {
		t.Errorf("Mode = %v, want DefaultMode for invalid mode annotation", info.Mode.String())
	}
}

func TestReadOwnerInfo_EmptyAnnotations(t *testing.T) {
	info := ReadOwnerInfo(map[string]string{})

	if info.OwnerSub != "" {
		t.Errorf("OwnerSub = %q, want empty", info.OwnerSub)
	}
	if info.OwnerEmail != "" {
		t.Errorf("OwnerEmail = %q, want empty", info.OwnerEmail)
	}
	if info.Group != "" {
		t.Errorf("Group = %q, want empty", info.Group)
	}
	if info.Mode != DefaultMode {
		t.Errorf("Mode = %v, want DefaultMode for empty annotations", info.Mode.String())
	}
}

func TestReadOwnerInfo_NilAnnotations(t *testing.T) {
	// Should not panic on nil map.
	info := ReadOwnerInfo(nil)

	if info.OwnerSub != "" {
		t.Errorf("OwnerSub = %q, want empty", info.OwnerSub)
	}
	if info.Mode != DefaultMode {
		t.Errorf("Mode = %v, want DefaultMode for nil annotations", info.Mode.String())
	}
}

func TestReadOwnerInfo_PresetNameSet(t *testing.T) {
	tests := []struct {
		modeStr    string
		wantPreset string
	}{
		{"rwx------", "private"},
		{"rwxr-x---", "member-read"},
		{"rwx--x---", "group-run"},
		{"rwxrwx---", "member-edit"},
		{"rwxr--r--", "public-read"},
		{"---------", ""},
	}
	for _, tt := range tests {
		t.Run(tt.modeStr, func(t *testing.T) {
			ann := map[string]string{
				AnnotationOwnerSub: "sub-x",
				AnnotationMode:     tt.modeStr,
			}
			info := ReadOwnerInfo(ann)
			if info.PresetName != tt.wantPreset {
				t.Errorf("PresetName = %q, want %q for mode %q", info.PresetName, tt.wantPreset, tt.modeStr)
			}
		})
	}
}

// --- WriteOwnerAnnotations ---

func TestWriteOwnerAnnotations_AllFields(t *testing.T) {
	mode := ModeOwnerRead | ModeOwnerWrite | ModeOwnerExecute | ModeGroupRead | ModeGroupExecute
	ann := WriteOwnerAnnotations("sub-abc", "alice@example.com", "Alice", mode)

	if ann[AnnotationOwnerSub] != "sub-abc" {
		t.Errorf("owner-sub = %q", ann[AnnotationOwnerSub])
	}
	if ann[AnnotationOwnerEmail] != "alice@example.com" {
		t.Errorf("owner-email = %q", ann[AnnotationOwnerEmail])
	}
	if ann[AnnotationOwnerName] != "Alice" {
		t.Errorf("owner-name = %q", ann[AnnotationOwnerName])
	}
	// AnnotationGroup is deprecated; WriteOwnerAnnotations no longer writes it.
	if _, ok := ann[AnnotationGroup]; ok {
		t.Errorf("group annotation should not be written by WriteOwnerAnnotations")
	}
	if ann[AnnotationMode] != mode.String() {
		t.Errorf("mode = %q, want %q", ann[AnnotationMode], mode.String())
	}
}

func TestWriteOwnerAnnotations_RoundTrip(t *testing.T) {
	mode := ModeOwnerRead | ModeOwnerWrite | ModeOwnerExecute
	ann := WriteOwnerAnnotations("sub-test", "bob@example.com", "Bob", mode)

	// Read back the annotations and verify they round-trip correctly.
	info := ReadOwnerInfo(ann)
	if info.OwnerSub != "sub-test" {
		t.Errorf("round-trip OwnerSub = %q", info.OwnerSub)
	}
	if info.OwnerEmail != "bob@example.com" {
		t.Errorf("round-trip OwnerEmail = %q", info.OwnerEmail)
	}
	if info.OwnerName != "Bob" {
		t.Errorf("round-trip OwnerName = %q", info.OwnerName)
	}
	// Group is no longer written; should be empty after round-trip through write+read.
	if info.Group != "" {
		t.Errorf("round-trip Group should be empty (no longer written), got %q", info.Group)
	}
	if info.Mode != mode {
		t.Errorf("round-trip Mode = %v, want %v", info.Mode.String(), mode.String())
	}
}

func TestWriteOwnerAnnotations_EmptyValues(t *testing.T) {
	ann := WriteOwnerAnnotations("", "", "", 0)

	// All active keys should be present even with empty/zero values.
	for _, key := range []string{AnnotationOwnerSub, AnnotationOwnerEmail, AnnotationOwnerName, AnnotationMode} {
		if _, ok := ann[key]; !ok {
			t.Errorf("expected key %q to be present even with empty value", key)
		}
	}
	// AnnotationGroup should NOT be present — no longer written.
	if _, ok := ann[AnnotationGroup]; ok {
		t.Errorf("AnnotationGroup should not be written by WriteOwnerAnnotations")
	}
	if ann[AnnotationMode] != "---------" {
		t.Errorf("mode for zero Mode should be '---------', got %q", ann[AnnotationMode])
	}
}

func TestWriteOwnerAnnotations_UsesNewPrefix(t *testing.T) {
	ann := WriteOwnerAnnotations("s", "e", "n", DefaultMode)
	for key := range ann {
		if len(key) < 16 || key[:16] != "tentacular.io/" {
			// Check prefix properly (tentacular.io/ is 14 chars)
			if key[:14] != "tentacular.io/" {
				t.Errorf("annotation key %q does not use tentacular.io/ prefix", key)
			}
		}
	}
}

// --- WriteNamespaceAnnotations ---

// TestWriteNamespaceAnnotations_OmitsGroup verifies that WriteNamespaceAnnotations
// does not write the tentacular.io/group annotation (posix-cleanup B1 requirement).
func TestWriteNamespaceAnnotations_OmitsGroup(t *testing.T) {
	mode := ModeOwnerRead | ModeOwnerWrite | ModeOwnerExecute
	ann := WriteNamespaceAnnotations("sub-abc", "alice@example.com", "Alice", mode, DefaultMode)

	if _, ok := ann[AnnotationGroup]; ok {
		t.Errorf("WriteNamespaceAnnotations must not write %q (group annotation removed in posix-cleanup)", AnnotationGroup)
	}
}

func TestWriteNamespaceAnnotations_AllActiveFields(t *testing.T) {
	mode := ModeOwnerRead | ModeOwnerWrite | ModeOwnerExecute
	defaultMode := ModeOwnerRead | ModeOwnerWrite | ModeOwnerExecute | ModeGroupRead | ModeGroupExecute
	ann := WriteNamespaceAnnotations("sub-ns", "bob@example.com", "Bob", mode, defaultMode)

	if ann[AnnotationOwnerSub] != "sub-ns" {
		t.Errorf("owner-sub = %q, want sub-ns", ann[AnnotationOwnerSub])
	}
	if ann[AnnotationOwnerEmail] != "bob@example.com" {
		t.Errorf("owner-email = %q, want bob@example.com", ann[AnnotationOwnerEmail])
	}
	if ann[AnnotationOwnerName] != "Bob" {
		t.Errorf("owner-name = %q, want Bob", ann[AnnotationOwnerName])
	}
	if ann[AnnotationMode] != mode.String() {
		t.Errorf("mode = %q, want %q", ann[AnnotationMode], mode.String())
	}
	if ann[AnnotationDefaultMode] != defaultMode.String() {
		t.Errorf("default-mode = %q, want %q", ann[AnnotationDefaultMode], defaultMode.String())
	}
	// group must NOT be present
	if _, ok := ann[AnnotationGroup]; ok {
		t.Error("group annotation must not be written by WriteNamespaceAnnotations")
	}
}

func TestWriteNamespaceAnnotations_ZeroDefaultModeOmitted(t *testing.T) {
	ann := WriteNamespaceAnnotations("sub", "e@e.com", "E", DefaultMode, 0)
	if _, ok := ann[AnnotationDefaultMode]; ok {
		t.Error("expected AnnotationDefaultMode to be omitted when defaultMode is zero")
	}
}
