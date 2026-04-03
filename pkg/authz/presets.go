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

	// Enclave-specific presets (member = group slot).

	// member-read (rwxr-x---): owner full, members read+run, others none.
	// Use case: members can view and run tentacles, only owner deploys.
	"member-read": ModeOwnerRead | ModeOwnerWrite | ModeOwnerExecute |
		ModeGroupRead | ModeGroupExecute,

	// member-edit (rwxrwx---): owner full, members full, others none.
	// DEFAULT for new enclaves — full collaboration within enclave.
	"member-edit": ModeOwnerRead | ModeOwnerWrite | ModeOwnerExecute |
		ModeGroupRead | ModeGroupWrite | ModeGroupExecute,

	// open-read (rwxrwxr--): owner full, members full, others read-only.
	// Use case: visitors can see what's running.
	"open-read": ModeOwnerRead | ModeOwnerWrite | ModeOwnerExecute |
		ModeGroupRead | ModeGroupWrite | ModeGroupExecute |
		ModeOtherRead,

	// open-run (rwxrwxr-x): owner full, members full, others read+run.
	// Use case: visitors can view and trigger tentacles.
	"open-run": ModeOwnerRead | ModeOwnerWrite | ModeOwnerExecute |
		ModeGroupRead | ModeGroupWrite | ModeGroupExecute |
		ModeOtherRead | ModeOtherExecute,
}

// DefaultMode is the mode applied when no Share preset is specified at deploy
// time for non-enclave namespaces. Equivalent to "group-read": owner full
// access, group read+execute.
var DefaultMode = presets["group-read"]

// DefaultEnclaveMode is the mode applied to new enclaves when no mode is
// explicitly specified. Equivalent to "member-edit": owner full, members full,
// others none.
var DefaultEnclaveMode = presets["member-edit"]

// PresetFromName returns the Mode for a named preset.
// Returns (mode, true) if found, (0, false) if not.
func PresetFromName(name string) (Mode, bool) {
	m, ok := presets[name]
	return m, ok
}

// presetPriority defines the canonical order for PresetName reverse-lookups.
// When two presets share the same mode bits, the first matching name wins.
// Enclave-specific names (member-*) take precedence over their legacy
// equivalents (group-*) so that reverse-lookups return the enclave-aware name.
var presetPriority = []string{
	"private",
	"member-edit",
	"member-read",
	"group-run",
	"open-read",
	"open-run",
	"public-read",
	"group-edit",
	"group-read",
}

// PresetName returns the preset name that matches the given mode, or "" if
// no preset matches. Used for reverse-lookup in permissions_get and wf_describe.
// When multiple preset names share the same mode bits, the enclave-aware name
// (member-edit, member-read) takes precedence over the legacy group name.
func PresetName(m Mode) string {
	for _, name := range presetPriority {
		if mode, ok := presets[name]; ok && mode == m {
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
