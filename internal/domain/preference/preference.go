// Package preference is a person's own preferences (PRD-001 FR-UX-23, FR-UX-27, FR-UX-08, FR-UX-06, FR-UX-38,
// NFR-L10N-01): the experience mode, the Plain or Accountant view, how the app looks, the keyboard shortcuts and the
// number and date formats. They are presentation only: they never change what a person may do, what the API checks or
// what is saved. A choice never made is empty, and the organisation's default for the person's roles, or the standard
// choice, applies.
package preference

import (
	"regexp"
	"slices"
	"strings"
)

// The values each choice may take; the database's CHECK constraints keep the same lists.
var (
	Modes         = []string{"basic", "expert"}
	LedgerViews   = []string{"plain", "accountant"}
	Themes        = []string{"light", "dark", "system"}
	Contrasts     = []string{"standard", "more", "system"}
	Densities     = []string{"comfortable", "compact"}
	Motions       = []string{"on", "reduce"}
	NumberFormats = []string{"comma_dot", "dot_comma", "space_comma", "space_dot"}
	DateFormats   = []string{"d_mmm_yyyy", "dd_mm_yyyy", "mm_dd_yyyy", "iso"}
)

// Fields are the choices a reset may name, in the order the API lists them.
var Fields = []string{"mode", "ledger_view", "theme", "contrast", "density", "motion", "shortcuts_on", "shortcut_keys", "number_format", "date_format"}

// Preferences is a person's saved state. An empty string, a nil ShortcutsOn and an empty ShortcutKeys are choices
// never made.
type Preferences struct {
	Mode         string
	LedgerView   string
	Theme        string
	Contrast     string
	Density      string
	Motion       string
	ShortcutsOn  *bool
	ShortcutKeys map[string]string
	NumberFormat string
	DateFormat   string
}

// Change is what a save asks for: the choices it sets, and the fields it takes back to their defaults. A nil
// ShortcutKeys leaves the keys alone; an empty one clears them.
type Change struct {
	Set   Preferences
	Reset []string
}

// The codes of a preference's rules.
const (
	CodeValue = "PRF-01" // a choice outside the values it may take
	CodeKeys  = "PRF-02" // the shortcut keys are not valid
	CodeReset = "PRF-03" // a reset names no field
)

// Problem is one rule that failed.
type Problem struct{ Code, Field, Message, Fix string }

var (
	shortcutID  = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,31}$`)
	maxKeys     = 64
	maxKeyChars = 12
)

func oneOf(field, v string, list []string) *Problem {
	if v == "" || slices.Contains(list, v) {
		return nil
	}
	return &Problem{CodeValue, field, "The " + strings.ReplaceAll(field, "_", " ") + " \"" + v + "\" is not one of: " + strings.Join(list, ", ") + ".", "Choose one of the listed values"}
}

// Validate refuses a change whose values are outside what each choice may take, whose shortcut keys are malformed or
// too many, or whose reset names an unknown field.
func Validate(c Change) *Problem {
	s := c.Set
	for _, p := range []*Problem{
		oneOf("mode", s.Mode, Modes), oneOf("ledger_view", s.LedgerView, LedgerViews), oneOf("theme", s.Theme, Themes),
		oneOf("contrast", s.Contrast, Contrasts), oneOf("density", s.Density, Densities), oneOf("motion", s.Motion, Motions),
		oneOf("number_format", s.NumberFormat, NumberFormats), oneOf("date_format", s.DateFormat, DateFormats),
	} {
		if p != nil {
			return p
		}
	}
	if len(s.ShortcutKeys) > maxKeys {
		return &Problem{CodeKeys, "shortcut_keys", "At most 64 shortcuts can have their own keys.", "Reset the ones you do not need"}
	}
	for id, keys := range s.ShortcutKeys {
		if !shortcutID.MatchString(id) {
			return &Problem{CodeKeys, "shortcut_keys", "\"" + id + "\" is not a shortcut's name.", "Name a shortcut as the shortcut sheet lists it"}
		}
		if keys == "" || len(keys) > maxKeyChars || strings.TrimSpace(keys) != keys || strings.ContainsFunc(keys, func(r rune) bool { return r < 0x20 || r > 0x7e }) {
			return &Problem{CodeKeys, "shortcut_keys", "The keys for \"" + id + "\" must be 1 to 12 printable characters with no space at either end.", "Type the keys again"}
		}
	}
	for _, f := range c.Reset {
		if !slices.Contains(Fields, f) {
			return &Problem{CodeReset, "reset", "\"" + f + "\" is not a preference that can be reset.", "Reset one of: " + strings.Join(Fields, ", ")}
		}
	}
	return nil
}

// Apply is the preferences after change: its resets first, then its choices.
func Apply(cur Preferences, c Change) Preferences {
	out := cur
	out.ShortcutKeys = clone(cur.ShortcutKeys)
	for _, f := range c.Reset {
		switch f {
		case "mode":
			out.Mode = ""
		case "ledger_view":
			out.LedgerView = ""
		case "theme":
			out.Theme = ""
		case "contrast":
			out.Contrast = ""
		case "density":
			out.Density = ""
		case "motion":
			out.Motion = ""
		case "shortcuts_on":
			out.ShortcutsOn = nil
		case "shortcut_keys":
			out.ShortcutKeys = map[string]string{}
		case "number_format":
			out.NumberFormat = ""
		case "date_format":
			out.DateFormat = ""
		}
	}
	set := func(dst *string, v string) {
		if v != "" {
			*dst = v
		}
	}
	set(&out.Mode, c.Set.Mode)
	set(&out.LedgerView, c.Set.LedgerView)
	set(&out.Theme, c.Set.Theme)
	set(&out.Contrast, c.Set.Contrast)
	set(&out.Density, c.Set.Density)
	set(&out.Motion, c.Set.Motion)
	set(&out.NumberFormat, c.Set.NumberFormat)
	set(&out.DateFormat, c.Set.DateFormat)
	if c.Set.ShortcutsOn != nil {
		on := *c.Set.ShortcutsOn
		out.ShortcutsOn = &on
	}
	if c.Set.ShortcutKeys != nil {
		out.ShortcutKeys = clone(c.Set.ShortcutKeys)
	}
	return out
}

func clone(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// DefaultModeKey is the organisation setting that holds a role's starting mode (FR-UX-27).
func DefaultModeKey(role string) string { return "experience.default_mode." + role }

// DefaultMode is the mode a person starts in when they have not chosen (FR-UX-27): with several roles, Expert if the
// organisation's starting mode for any of them is Expert, except that the Owner wins: a person who holds the Owner role
// starts in the Owner's mode. startingMode reads the organisation's setting for a role ("basic" or "expert"; "" for a
// role it has none for).
func DefaultMode(roles []string, startingMode func(role string) string) string {
	if slices.Contains(roles, "owner") {
		if m := startingMode("owner"); m == "expert" {
			return "expert"
		}
		return "basic"
	}
	for _, r := range roles {
		if startingMode(r) == "expert" {
			return "expert"
		}
	}
	return "basic"
}
