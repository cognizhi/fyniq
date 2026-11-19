package preference_test

import (
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/domain/preference"
	"github.com/cognizhi/fyniq/internal/domain/settings"
)

func shipped(role string) string {
	d, ok := settings.Lookup(preference.DefaultModeKey(role))
	if !ok {
		return ""
	}
	return d.Recommended
}

// The starting mode follows FR-UX-27's table as the catalogue ships it: Basic for the Owner, Bookkeeper, AR and AP
// specialists, Approver, Employee and Board viewer, Expert for the Accountant, Controller, Tax agent, Auditor and
// Payroll administrator, and with roles that disagree the Owner wins.
// req: FR-UX-27
func TestAStartingModeFollowsTheRolesAndTheOwnerWins(t *testing.T) {
	for role, want := range map[string]string{
		"owner": "basic", "bookkeeper": "basic", "ar_specialist": "basic", "ap_specialist": "basic", "approver": "basic",
		"employee": "basic", "board_viewer": "basic", "accountant": "expert", "controller": "expert", "tax_agent": "expert",
		"auditor": "expert", "payroll_administrator": "expert",
	} {
		if got := preference.DefaultMode([]string{role}, shipped); got != want {
			t.Errorf("%s starts in %s, want %s", role, got, want)
		}
	}
	if got := preference.DefaultMode([]string{"owner", "payroll_administrator"}, shipped); got != "basic" {
		t.Errorf("an Owner who is also a Payroll administrator starts in Basic (the Owner wins), got %s", got)
	}
	if got := preference.DefaultMode([]string{"bookkeeper", "accountant"}, shipped); got != "expert" {
		t.Errorf("a Bookkeeper who is also an Accountant starts in Expert, got %s", got)
	}
	if got := preference.DefaultMode(nil, shipped); got != "basic" {
		t.Errorf("no role starts in Basic, got %s", got)
	}
	// The Administrator changes a role's default: the Owner's own setting decides for an Owner.
	changed := func(role string) string {
		if role == "owner" {
			return "expert"
		}
		return "basic"
	}
	if got := preference.DefaultMode([]string{"owner", "bookkeeper"}, changed); got != "expert" {
		t.Errorf("an Owner starts in the Owner's mode as the Administrator set it, got %s", got)
	}
	if got := preference.DefaultMode([]string{"accountant"}, changed); got != "basic" {
		t.Errorf("an Accountant starts in the mode the Administrator set, got %s", got)
	}
	for _, d := range settings.Catalogue {
		if role, ok := strings.CutPrefix(d.Key, "experience.default_mode."); ok && preference.DefaultModeKey(role) != d.Key {
			t.Errorf("%s is not a role's starting mode key", d.Key)
		}
	}
}

// A choice outside what it may take, shortcut keys that are malformed or too many, and a reset of an unknown field are
// refused with a stable code that names the field.
// req: FR-UX-23
// req: NFR-L10N-01
func TestValidateRefusesWhatACheckConstraintWould(t *testing.T) {
	bad := []struct {
		name string
		c    preference.Change
		code string
		in   string
	}{
		{"a mode", preference.Change{Set: preference.Preferences{Mode: "pro"}}, preference.CodeValue, "mode"},
		{"a number format", preference.Change{Set: preference.Preferences{NumberFormat: "1,2,3"}}, preference.CodeValue, "number_format"},
		{"a date format", preference.Change{Set: preference.Preferences{DateFormat: "yy"}}, preference.CodeValue, "date_format"},
		{"a shortcut name", preference.Change{Set: preference.Preferences{ShortcutKeys: map[string]string{"Go Home": "g h"}}}, preference.CodeKeys, "shortcut_keys"},
		{"empty keys", preference.Change{Set: preference.Preferences{ShortcutKeys: map[string]string{"go.home": ""}}}, preference.CodeKeys, "shortcut_keys"},
		{"keys with a space at the end", preference.Change{Set: preference.Preferences{ShortcutKeys: map[string]string{"go.home": "g "}}}, preference.CodeKeys, "shortcut_keys"},
		{"a control character", preference.Change{Set: preference.Preferences{ShortcutKeys: map[string]string{"go.home": "g\x00"}}}, preference.CodeKeys, "shortcut_keys"},
		{"a reset", preference.Change{Reset: []string{"password"}}, preference.CodeReset, "reset"},
	}
	for _, b := range bad {
		p := preference.Validate(b.c)
		if p == nil || p.Code != b.code || p.Field != b.in || p.Message == "" || p.Fix == "" {
			t.Errorf("%s: got %+v, want %s on %s", b.name, p, b.code, b.in)
		}
	}
	many := map[string]string{}
	for i := 0; i < 65; i++ {
		many["k"+string(rune('a'+i%26))+string(rune('a'+i/26))] = "x"
	}
	if p := preference.Validate(preference.Change{Set: preference.Preferences{ShortcutKeys: many}}); p == nil || p.Code != preference.CodeKeys {
		t.Errorf("65 shortcuts: %+v", p)
	}
	on := true
	ok := preference.Change{Set: preference.Preferences{Mode: "expert", NumberFormat: "dot_comma", DateFormat: "iso", ShortcutsOn: &on, ShortcutKeys: map[string]string{"go.home": "g h", "sheet": "?"}}, Reset: []string{"theme"}}
	if p := preference.Validate(ok); p != nil {
		t.Errorf("a valid change was refused: %+v", p)
	}
}

// A save changes only what it names, a reset takes a field back to its default, and the keys are replaced as a whole.
// req: FR-UX-23
func TestApplyChangesOnlyWhatIsNamed(t *testing.T) {
	on := true
	cur := preference.Preferences{Mode: "basic", Theme: "dark", ShortcutsOn: &on, ShortcutKeys: map[string]string{"go.home": "g h"}}
	got := preference.Apply(cur, preference.Change{Set: preference.Preferences{Mode: "expert"}})
	if got.Mode != "expert" || got.Theme != "dark" || got.ShortcutKeys["go.home"] != "g h" || got.ShortcutsOn == nil || !*got.ShortcutsOn {
		t.Errorf("only the mode changes: %+v", got)
	}
	got = preference.Apply(cur, preference.Change{Reset: []string{"theme", "shortcut_keys", "shortcuts_on"}})
	if got.Theme != "" || len(got.ShortcutKeys) != 0 || got.ShortcutsOn != nil || got.Mode != "basic" {
		t.Errorf("a reset takes the fields back: %+v", got)
	}
	got = preference.Apply(cur, preference.Change{Set: preference.Preferences{ShortcutKeys: map[string]string{"sheet": "?"}}})
	if len(got.ShortcutKeys) != 1 || got.ShortcutKeys["sheet"] != "?" {
		t.Errorf("the keys are replaced as a whole: %+v", got.ShortcutKeys)
	}
	got.ShortcutKeys["x"] = "y"
	if _, leaked := cur.ShortcutKeys["x"]; leaked {
		t.Error("Apply shares its map with the current state")
	}
}
