package authz

import (
	"testing"
)

// --- PresetFromName ---

func TestPresetFromName_AllPresets(t *testing.T) {
	tests := []struct {
		name      string
		wantMode  Mode
		wantFound bool
	}{
		{
			name:      "private",
			wantMode:  ModeOwnerRead | ModeOwnerWrite | ModeOwnerExecute,
			wantFound: true,
		},
		{
			name:      "group-read",
			wantMode:  ModeOwnerRead | ModeOwnerWrite | ModeOwnerExecute | ModeGroupRead | ModeGroupExecute,
			wantFound: true,
		},
		{
			name:      "group-run",
			wantMode:  ModeOwnerRead | ModeOwnerWrite | ModeOwnerExecute | ModeGroupExecute,
			wantFound: true,
		},
		{
			name:      "group-edit",
			wantMode:  ModeOwnerRead | ModeOwnerWrite | ModeOwnerExecute | ModeGroupRead | ModeGroupWrite | ModeGroupExecute,
			wantFound: true,
		},
		{
			name:      "public-read",
			wantMode:  ModeOwnerRead | ModeOwnerWrite | ModeOwnerExecute | ModeGroupRead | ModeOtherRead,
			wantFound: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := PresetFromName(tt.name)
			if ok != tt.wantFound {
				t.Errorf("PresetFromName(%q) found=%v, want %v", tt.name, ok, tt.wantFound)
			}
			if got != tt.wantMode {
				t.Errorf("PresetFromName(%q) mode=%v, want %v", tt.name, got.String(), tt.wantMode.String())
			}
		})
	}
}

func TestPresetFromName_Unknown(t *testing.T) {
	tests := []string{"", "unknown", "PRIVATE", "owner-only", "755", "team-shared", "team-edit"}
	for _, name := range tests {
		t.Run(name, func(t *testing.T) {
			got, ok := PresetFromName(name)
			if ok {
				t.Errorf("PresetFromName(%q) found=true, want false", name)
			}
			if got != 0 {
				t.Errorf("PresetFromName(%q) mode=%v, want 0", name, got)
			}
		})
	}
}

// --- PresetName reverse lookup ---

func TestPresetName_AllPresets(t *testing.T) {
	groupReadMode := ModeOwnerRead | ModeOwnerWrite | ModeOwnerExecute | ModeGroupRead | ModeGroupExecute
	groupRunMode := ModeOwnerRead | ModeOwnerWrite | ModeOwnerExecute | ModeGroupExecute
	groupEditMode := ModeOwnerRead | ModeOwnerWrite | ModeOwnerExecute | ModeGroupRead | ModeGroupWrite | ModeGroupExecute
	publicReadMode := ModeOwnerRead | ModeOwnerWrite | ModeOwnerExecute | ModeGroupRead | ModeOtherRead
	privateMode := ModeOwnerRead | ModeOwnerWrite | ModeOwnerExecute

	tests := []struct {
		want string
		mode Mode
	}{
		{"private", privateMode},
		// group-read shares bits with member-read; member-read takes precedence.
		{"member-read", groupReadMode},
		{"group-run", groupRunMode},
		// group-edit shares bits with member-edit; member-edit takes precedence.
		{"member-edit", groupEditMode},
		{"public-read", publicReadMode},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := PresetName(tt.mode)
			if got != tt.want {
				t.Errorf("PresetName(%v) = %q, want %q", tt.mode.String(), got, tt.want)
			}
		})
	}
}

func TestPresetName_NoMatch(t *testing.T) {
	// Modes that don't correspond to any preset.
	noMatches := []Mode{
		0,
		ModeOtherRead | ModeOtherWrite | ModeOtherExecute,
		ModeOwnerRead,
		ModeGroupRead | ModeGroupWrite,
	}
	for _, m := range noMatches {
		t.Run(m.String(), func(t *testing.T) {
			got := PresetName(m)
			if got != "" {
				t.Errorf("PresetName(%v) = %q, want empty string", m.String(), got)
			}
		})
	}
}

// --- DefaultMode ---

func TestDefaultMode_IsGroupRead(t *testing.T) {
	want := ModeOwnerRead | ModeOwnerWrite | ModeOwnerExecute | ModeGroupRead | ModeGroupExecute
	if DefaultMode != want {
		t.Errorf("DefaultMode = %v, want group-read (%v)", DefaultMode.String(), want.String())
	}
	// DefaultMode bits are rwxr-x---. member-read has the same bits and
	// takes precedence over group-read in the reverse-lookup priority.
	if PresetName(DefaultMode) != "member-read" {
		t.Errorf("DefaultMode preset name = %q, want 'member-read'", PresetName(DefaultMode))
	}
}

// Spec: private = 0700 (owner-only, full access)
func TestPreset_Private_OwnerOnly(t *testing.T) {
	m, ok := PresetFromName("private")
	if !ok {
		t.Fatal("expected private preset to exist")
	}
	if !m.OwnerRead() || !m.OwnerWrite() || !m.OwnerExecute() {
		t.Error("expected full owner access for private preset")
	}
	if m.GroupRead() || m.GroupWrite() || m.GroupExecute() {
		t.Error("expected no group access for private preset")
	}
	if m.OtherRead() || m.OtherWrite() || m.OtherExecute() {
		t.Error("expected no other access for private preset")
	}
}

// Spec: group-read = owner full, group r+x
func TestPreset_GroupRead_GroupReadExecute(t *testing.T) {
	m, ok := PresetFromName("group-read")
	if !ok {
		t.Fatal("expected group-read preset to exist")
	}
	if !m.OwnerRead() || !m.OwnerWrite() || !m.OwnerExecute() {
		t.Error("expected full owner access for group-read")
	}
	if !m.GroupRead() || m.GroupWrite() || !m.GroupExecute() {
		t.Errorf("expected group r-x for group-read, got %v", m.String())
	}
	if m.OtherRead() || m.OtherWrite() || m.OtherExecute() {
		t.Error("expected no other access for group-read")
	}
}

// Spec: group-run = owner full, group execute only
func TestPreset_GroupRun_GroupExecuteOnly(t *testing.T) {
	m, ok := PresetFromName("group-run")
	if !ok {
		t.Fatal("expected group-run preset to exist")
	}
	if !m.OwnerRead() || !m.OwnerWrite() || !m.OwnerExecute() {
		t.Error("expected full owner access for group-run")
	}
	if m.GroupRead() || m.GroupWrite() || !m.GroupExecute() {
		t.Errorf("expected group --x for group-run, got %v", m.String())
	}
	if m.OtherRead() || m.OtherWrite() || m.OtherExecute() {
		t.Error("expected no other access for group-run")
	}
}

// Spec: group-edit = owner full, group full
func TestPreset_GroupEdit_GroupFull(t *testing.T) {
	m, ok := PresetFromName("group-edit")
	if !ok {
		t.Fatal("expected group-edit preset to exist")
	}
	if !m.OwnerRead() || !m.OwnerWrite() || !m.OwnerExecute() {
		t.Error("expected full owner access for group-edit")
	}
	if !m.GroupRead() || !m.GroupWrite() || !m.GroupExecute() {
		t.Errorf("expected group rwx for group-edit, got %v", m.String())
	}
	if m.OtherRead() || m.OtherWrite() || m.OtherExecute() {
		t.Error("expected no other access for group-edit")
	}
}

// Spec: member-read (rwxr-x---): owner full, members read+run, others none.
func TestPreset_MemberRead(t *testing.T) {
	m, ok := PresetFromName("member-read")
	if !ok {
		t.Fatal("expected member-read preset to exist")
	}
	if !m.OwnerRead() || !m.OwnerWrite() || !m.OwnerExecute() {
		t.Error("expected full owner access for member-read")
	}
	if !m.GroupRead() || m.GroupWrite() || !m.GroupExecute() {
		t.Errorf("expected group r-x for member-read, got %v", m.String())
	}
	if m.OtherRead() || m.OtherWrite() || m.OtherExecute() {
		t.Error("expected no other access for member-read")
	}
}

// Spec: member-edit (rwxrwx---): owner full, members full, others none.
// This is the default for new enclaves.
func TestPreset_MemberEdit_MemberFull(t *testing.T) {
	m, ok := PresetFromName("member-edit")
	if !ok {
		t.Fatal("expected member-edit preset to exist")
	}
	if !m.OwnerRead() || !m.OwnerWrite() || !m.OwnerExecute() {
		t.Error("expected full owner access for member-edit")
	}
	if !m.GroupRead() || !m.GroupWrite() || !m.GroupExecute() {
		t.Errorf("expected group rwx for member-edit, got %v", m.String())
	}
	if m.OtherRead() || m.OtherWrite() || m.OtherExecute() {
		t.Error("expected no other access for member-edit")
	}
}

// Spec: open-read (rwxrwxr--): owner full, members full, others read-only.
func TestPreset_OpenRead_OtherReadOnly(t *testing.T) {
	m, ok := PresetFromName("open-read")
	if !ok {
		t.Fatal("expected open-read preset to exist")
	}
	if !m.OwnerRead() || !m.OwnerWrite() || !m.OwnerExecute() {
		t.Error("expected full owner access for open-read")
	}
	if !m.GroupRead() || !m.GroupWrite() || !m.GroupExecute() {
		t.Errorf("expected group rwx for open-read, got %v", m.String())
	}
	if !m.OtherRead() || m.OtherWrite() || m.OtherExecute() {
		t.Errorf("expected other r-- for open-read, got %v", m.String())
	}
}

// Spec: open-run (rwxrwxr-x): owner full, members full, others read+run.
func TestPreset_OpenRun_OtherReadAndExecute(t *testing.T) {
	m, ok := PresetFromName("open-run")
	if !ok {
		t.Fatal("expected open-run preset to exist")
	}
	if !m.OwnerRead() || !m.OwnerWrite() || !m.OwnerExecute() {
		t.Error("expected full owner access for open-run")
	}
	if !m.GroupRead() || !m.GroupWrite() || !m.GroupExecute() {
		t.Errorf("expected group rwx for open-run, got %v", m.String())
	}
	if !m.OtherRead() || m.OtherWrite() || !m.OtherExecute() {
		t.Errorf("expected other r-x for open-run, got %v", m.String())
	}
}

// Spec: DefaultEnclaveMode is member-edit.
func TestDefaultEnclaveMode_IsMemberEdit(t *testing.T) {
	want := ModeOwnerRead | ModeOwnerWrite | ModeOwnerExecute |
		ModeGroupRead | ModeGroupWrite | ModeGroupExecute
	if DefaultEnclaveMode != want {
		t.Errorf("DefaultEnclaveMode = %v, want member-edit (%v)", DefaultEnclaveMode.String(), want.String())
	}
	if PresetName(DefaultEnclaveMode) != "member-edit" {
		t.Errorf("DefaultEnclaveMode preset name = %q, want 'member-edit'", PresetName(DefaultEnclaveMode))
	}
}

// Spec: public-read = owner full, group read only, other read only
func TestPreset_PublicRead_GroupAndOtherRead(t *testing.T) {
	m, ok := PresetFromName("public-read")
	if !ok {
		t.Fatal("expected public-read preset to exist")
	}
	if !m.OwnerRead() || !m.OwnerWrite() || !m.OwnerExecute() {
		t.Error("expected full owner access for public-read")
	}
	if !m.GroupRead() || m.GroupWrite() || m.GroupExecute() {
		t.Errorf("expected group r-- for public-read, got %v", m.String())
	}
	if !m.OtherRead() || m.OtherWrite() || m.OtherExecute() {
		t.Errorf("expected other r-- for public-read, got %v", m.String())
	}
}
