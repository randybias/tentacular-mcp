package authz

import (
	"testing"
)

// --- ParseMode ---

func TestParseMode_ValidStrings(t *testing.T) {
	tests := []struct {
		input string
		want  Mode
	}{
		{"rwxrwxrwx", ModeOwnerRead | ModeOwnerWrite | ModeOwnerExecute | ModeGroupRead | ModeGroupWrite | ModeGroupExecute | ModeOtherRead | ModeOtherWrite | ModeOtherExecute},
		{"rwxr-x---", ModeOwnerRead | ModeOwnerWrite | ModeOwnerExecute | ModeGroupRead | ModeGroupExecute},
		{"rwx------", ModeOwnerRead | ModeOwnerWrite | ModeOwnerExecute},
		{"rwxr-xr-x", ModeOwnerRead | ModeOwnerWrite | ModeOwnerExecute | ModeGroupRead | ModeGroupExecute | ModeOtherRead | ModeOtherExecute},
		{"---------", 0},
		{"r--------", ModeOwnerRead},
		{"---r-----", ModeGroupRead},
		{"------r--", ModeOtherRead},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseMode(tt.input)
			if err != nil {
				t.Fatalf("ParseMode(%q) returned unexpected error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("ParseMode(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseMode_InvalidStrings(t *testing.T) {
	tests := []struct {
		input string
		desc  string
	}{
		{"", "empty string"},
		{"rwxrwx", "too short (6 chars)"},
		{"rwxrwxrwxrwx", "too long (12 chars)"},
		{"rwxr-xabc", "invalid char 'a' at position 6"},
		{"RWXr-x---", "uppercase R at position 0"},
		{"rwx r-x--", "space in string"},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			_, err := ParseMode(tt.input)
			if err == nil {
				t.Errorf("ParseMode(%q) expected error, got nil", tt.input)
			}
		})
	}
}

// --- String round-trip ---

func TestMode_StringRoundTrip(t *testing.T) {
	inputs := []string{
		"rwxrwxrwx",
		"rwxr-x---",
		"rwx------",
		"---------",
		"rwxr-xr-x",
		"r--r--r--",
	}
	for _, s := range inputs {
		t.Run(s, func(t *testing.T) {
			m, err := ParseMode(s)
			if err != nil {
				t.Fatalf("ParseMode(%q): %v", s, err)
			}
			got := m.String()
			if got != s {
				t.Errorf("round-trip: ParseMode(%q).String() = %q", s, got)
			}
		})
	}
}

// --- Bit accessors ---

func TestMode_OwnerBits(t *testing.T) {
	m := ModeOwnerRead | ModeOwnerWrite | ModeOwnerExecute

	if !m.OwnerRead() {
		t.Error("expected OwnerRead() = true for 0700")
	}
	if !m.OwnerWrite() {
		t.Error("expected OwnerWrite() = true for 0700")
	}
	if !m.OwnerExecute() {
		t.Error("expected OwnerExecute() = true for 0700")
	}
	if m.GroupRead() {
		t.Error("expected GroupRead() = false for 0700")
	}
	if m.OtherRead() {
		t.Error("expected OtherRead() = false for 0700")
	}
}

func TestMode_GroupBits(t *testing.T) {
	m := ModeGroupRead | ModeGroupExecute

	if m.OwnerRead() {
		t.Error("expected OwnerRead() = false")
	}
	if !m.GroupRead() {
		t.Error("expected GroupRead() = true")
	}
	if m.GroupWrite() {
		t.Error("expected GroupWrite() = false")
	}
	if !m.GroupExecute() {
		t.Error("expected GroupExecute() = true")
	}
	if m.OtherRead() {
		t.Error("expected OtherRead() = false")
	}
}

func TestMode_OtherBits(t *testing.T) {
	m := ModeOtherRead | ModeOtherExecute

	if m.OwnerRead() {
		t.Error("expected OwnerRead() = false")
	}
	if m.GroupRead() {
		t.Error("expected GroupRead() = false")
	}
	if !m.OtherRead() {
		t.Error("expected OtherRead() = true")
	}
	if m.OtherWrite() {
		t.Error("expected OtherWrite() = false")
	}
	if !m.OtherExecute() {
		t.Error("expected OtherExecute() = true")
	}
}

func TestMode_AllZero(t *testing.T) {
	var m Mode
	if m.OwnerRead() || m.OwnerWrite() || m.OwnerExecute() {
		t.Error("expected all owner bits false for zero mode")
	}
	if m.GroupRead() || m.GroupWrite() || m.GroupExecute() {
		t.Error("expected all group bits false for zero mode")
	}
	if m.OtherRead() || m.OtherWrite() || m.OtherExecute() {
		t.Error("expected all other bits false for zero mode")
	}
}

func TestMode_AllSet(t *testing.T) {
	m := ModeOwnerRead | ModeOwnerWrite | ModeOwnerExecute |
		ModeGroupRead | ModeGroupWrite | ModeGroupExecute |
		ModeOtherRead | ModeOtherWrite | ModeOtherExecute

	if !m.OwnerRead() || !m.OwnerWrite() || !m.OwnerExecute() {
		t.Error("expected all owner bits true for rwxrwxrwx")
	}
	if !m.GroupRead() || !m.GroupWrite() || !m.GroupExecute() {
		t.Error("expected all group bits true for rwxrwxrwx")
	}
	if !m.OtherRead() || !m.OtherWrite() || !m.OtherExecute() {
		t.Error("expected all other bits true for rwxrwxrwx")
	}
}

// --- String output format ---

func TestMode_StringFormat(t *testing.T) {
	tests := []struct {
		want string
		mode Mode
	}{
		{"rwxr-x---", ModeOwnerRead | ModeOwnerWrite | ModeOwnerExecute | ModeGroupRead | ModeGroupExecute},
		{"rwx------", ModeOwnerRead | ModeOwnerWrite | ModeOwnerExecute},
		{"---------", 0},
		{"rwxrwxrwx", ModeOwnerRead | ModeOwnerWrite | ModeOwnerExecute | ModeGroupRead | ModeGroupWrite | ModeGroupExecute | ModeOtherRead | ModeOtherWrite | ModeOtherExecute},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.mode.String()
			if got != tt.want {
				t.Errorf("Mode(%04o).String() = %q, want %q", tt.mode, got, tt.want)
			}
		})
	}
}

func TestMode_ParseMode_0750Equivalent(t *testing.T) {
	// Spec scenario: "rwxr-x---" == owner full, group r+x, others none
	m, err := ParseMode("rwxr-x---")
	if err != nil {
		t.Fatalf("ParseMode: %v", err)
	}
	if !m.OwnerRead() || !m.OwnerWrite() || !m.OwnerExecute() {
		t.Error("expected full owner access for rwxr-x---")
	}
	if !m.GroupRead() || m.GroupWrite() || !m.GroupExecute() {
		t.Error("expected group r-x for rwxr-x---")
	}
	if m.OtherRead() || m.OtherWrite() || m.OtherExecute() {
		t.Error("expected no other access for rwxr-x---")
	}
}
