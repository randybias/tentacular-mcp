// Package authz implements ownership-based access control for tentacular
// workflow deployments. Permissions follow a Unix-style rwx model:
// owner bits (6-8), group bits (3-5), other bits (0-2).
package authz

import (
	"fmt"
	"strings"
)

// Mode is a 9-bit permission mask: rwxrwxrwx (owner|group|other).
// Bit layout (MSB to LSB): owner-r(8) owner-w(7) owner-x(6)
//
//	group-r(5)  group-w(4)  group-x(3)
//	other-r(2)  other-w(1)  other-x(0)
type Mode uint16

const (
	// Owner permission bits.
	ModeOwnerRead    Mode = 1 << 8
	ModeOwnerWrite   Mode = 1 << 7
	ModeOwnerExecute Mode = 1 << 6

	// Group permission bits.
	ModeGroupRead    Mode = 1 << 5
	ModeGroupWrite   Mode = 1 << 4
	ModeGroupExecute Mode = 1 << 3

	// Other permission bits.
	ModeOtherRead    Mode = 1 << 2
	ModeOtherWrite   Mode = 1 << 1
	ModeOtherExecute Mode = 1 << 0
)

// OwnerRead reports whether the owner read bit is set.
func (m Mode) OwnerRead() bool { return m&ModeOwnerRead != 0 }

// OwnerWrite reports whether the owner write bit is set.
func (m Mode) OwnerWrite() bool { return m&ModeOwnerWrite != 0 }

// OwnerExecute reports whether the owner execute bit is set.
func (m Mode) OwnerExecute() bool { return m&ModeOwnerExecute != 0 }

// GroupRead reports whether the group read bit is set.
func (m Mode) GroupRead() bool { return m&ModeGroupRead != 0 }

// GroupWrite reports whether the group write bit is set.
func (m Mode) GroupWrite() bool { return m&ModeGroupWrite != 0 }

// GroupExecute reports whether the group execute bit is set.
func (m Mode) GroupExecute() bool { return m&ModeGroupExecute != 0 }

// OtherRead reports whether the other read bit is set.
func (m Mode) OtherRead() bool { return m&ModeOtherRead != 0 }

// OtherWrite reports whether the other write bit is set.
func (m Mode) OtherWrite() bool { return m&ModeOtherWrite != 0 }

// OtherExecute reports whether the other execute bit is set.
func (m Mode) OtherExecute() bool { return m&ModeOtherExecute != 0 }

// String returns the mode as a 9-character rwx string (e.g. "rwxr-x---").
func (m Mode) String() string {
	bit := func(set bool, ch byte) byte {
		if set {
			return ch
		}
		return '-'
	}
	return string([]byte{
		bit(m.OwnerRead(), 'r'),
		bit(m.OwnerWrite(), 'w'),
		bit(m.OwnerExecute(), 'x'),
		bit(m.GroupRead(), 'r'),
		bit(m.GroupWrite(), 'w'),
		bit(m.GroupExecute(), 'x'),
		bit(m.OtherRead(), 'r'),
		bit(m.OtherWrite(), 'w'),
		bit(m.OtherExecute(), 'x'),
	})
}

// ParseMode parses a 9-character rwx string (e.g. "rwxr-x---") into a Mode.
// Returns an error if the string is not exactly 9 characters of valid rwx notation.
func ParseMode(s string) (Mode, error) {
	if len(s) != 9 {
		return 0, fmt.Errorf("mode string must be 9 characters, got %d: %q", len(s), s)
	}

	// Valid characters per position: r/-, w/-, x/-
	valid := []struct {
		pos  int
		char byte
		bit  Mode
	}{
		{0, 'r', ModeOwnerRead},
		{1, 'w', ModeOwnerWrite},
		{2, 'x', ModeOwnerExecute},
		{3, 'r', ModeGroupRead},
		{4, 'w', ModeGroupWrite},
		{5, 'x', ModeGroupExecute},
		{6, 'r', ModeOtherRead},
		{7, 'w', ModeOtherWrite},
		{8, 'x', ModeOtherExecute},
	}

	var m Mode
	for _, v := range valid {
		c := s[v.pos]
		if c == v.char {
			m |= v.bit
		} else if c != '-' {
			return 0, fmt.Errorf("invalid character %q at position %d in mode string %q", c, v.pos, s)
		}
	}
	return m, nil
}

// ValidPresetNames returns a sorted, human-readable list of valid preset names
// for use in error messages.
func ValidPresetNames() string {
	names := presetNames()
	return strings.Join(names, ", ")
}
