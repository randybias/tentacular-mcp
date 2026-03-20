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
	if info.PresetName != "group-read" {
		t.Errorf("PresetName = %q, want 'group-read'", info.PresetName)
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
		{"rwxr-x---", "group-read"},
		{"rwx--x---", "group-run"},
		{"rwxrwx---", "group-edit"},
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
	ann := WriteOwnerAnnotations("sub-abc", "alice@example.com", "Alice", "platform", mode)

	if ann[AnnotationOwnerSub] != "sub-abc" {
		t.Errorf("owner-sub = %q", ann[AnnotationOwnerSub])
	}
	if ann[AnnotationOwnerEmail] != "alice@example.com" {
		t.Errorf("owner-email = %q", ann[AnnotationOwnerEmail])
	}
	if ann[AnnotationOwnerName] != "Alice" {
		t.Errorf("owner-name = %q", ann[AnnotationOwnerName])
	}
	if ann[AnnotationGroup] != "platform" {
		t.Errorf("group = %q", ann[AnnotationGroup])
	}
	if ann[AnnotationMode] != mode.String() {
		t.Errorf("mode = %q, want %q", ann[AnnotationMode], mode.String())
	}
}

func TestWriteOwnerAnnotations_RoundTrip(t *testing.T) {
	mode := ModeOwnerRead | ModeOwnerWrite | ModeOwnerExecute
	ann := WriteOwnerAnnotations("sub-test", "bob@example.com", "Bob", "ops-team", mode)

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
	if info.Group != "ops-team" {
		t.Errorf("round-trip Group = %q", info.Group)
	}
	if info.Mode != mode {
		t.Errorf("round-trip Mode = %v, want %v", info.Mode.String(), mode.String())
	}
}

func TestWriteOwnerAnnotations_EmptyValues(t *testing.T) {
	ann := WriteOwnerAnnotations("", "", "", "", 0)

	// All keys should be present even with empty/zero values.
	for _, key := range []string{AnnotationOwnerSub, AnnotationOwnerEmail, AnnotationOwnerName, AnnotationGroup, AnnotationMode} {
		if _, ok := ann[key]; !ok {
			t.Errorf("expected key %q to be present even with empty value", key)
		}
	}
	if ann[AnnotationMode] != "---------" {
		t.Errorf("mode for zero Mode should be '---------', got %q", ann[AnnotationMode])
	}
}

func TestWriteOwnerAnnotations_UsesNewPrefix(t *testing.T) {
	ann := WriteOwnerAnnotations("s", "e", "n", "g", DefaultMode)
	for key := range ann {
		if len(key) < 16 || key[:16] != "tentacular.io/" {
			// Check prefix properly (tentacular.io/ is 14 chars)
			if key[:14] != "tentacular.io/" {
				t.Errorf("annotation key %q does not use tentacular.io/ prefix", key)
			}
		}
	}
}
