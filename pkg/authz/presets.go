package authz

import "sort"

// presets maps preset names to their Mode values.
// These cover the most common sharing patterns.
var presets = map[string]Mode{
	// private: owner full access, group none, other none.
	"private": ModeOwnerRead | ModeOwnerWrite | ModeOwnerExecute,

	// group-read: owner full access, group read+execute, other none.
	"group-read": ModeOwnerRead | ModeOwnerWrite | ModeOwnerExecute |
		ModeGroupRead | ModeGroupExecute,

	// group-run: owner full access, group execute only, other none.
	"group-run": ModeOwnerRead | ModeOwnerWrite | ModeOwnerExecute |
		ModeGroupExecute,

	// group-edit: owner full access, group full access, other none.
	"group-edit": ModeOwnerRead | ModeOwnerWrite | ModeOwnerExecute |
		ModeGroupRead | ModeGroupWrite | ModeGroupExecute,

	// public-read: owner full access, group read only, other read only.
	"public-read": ModeOwnerRead | ModeOwnerWrite | ModeOwnerExecute |
		ModeGroupRead |
		ModeOtherRead,
}

// DefaultMode is the mode applied when no Share preset is specified at deploy
// time. Equivalent to "group-read": owner full access, group read+execute.
var DefaultMode = presets["group-read"]

// PresetFromName returns the Mode for a named preset.
// Returns (mode, true) if found, (0, false) if not.
func PresetFromName(name string) (Mode, bool) {
	m, ok := presets[name]
	return m, ok
}

// PresetName returns the preset name that matches the given mode, or "" if
// no preset matches. Used for reverse-lookup in permissions_get and wf_describe.
func PresetName(m Mode) string {
	for name, mode := range presets {
		if mode == m {
			return name
		}
	}
	return ""
}

// presetNames returns a sorted slice of all preset names.
func presetNames() []string {
	names := make([]string, 0, len(presets))
	for name := range presets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
