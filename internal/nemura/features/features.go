// Package features is the feature registry of an organisation (PRD-002 NM-FEAT-01 to NM-FEAT-05): People records is always on; each optional feature
// is on, off or external (another system holds its data); the dependencies are enforced and a refusal names the reason; switching off keeps the data
// and first lists what is outstanding. A feature that is off is not loaded: the guard Loaded is what every route and job of the feature asks.
package features

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// The stable codes of the refusals.
const (
	CodeRequired    = "NM_FEATURE_REQUIRED"         // another feature needs this one, or this one needs another
	CodeUnavailable = "NM_FEATURE_UNAVAILABLE"      // the feature is not built yet (hiring, performance and time arrive later) or needs an approval that is not built
	CodeObligations = "NM_FEATURE_OBLIGATIONS_OPEN" // work is outstanding and no replacement arrangement is recorded for it
	CodeInvalid     = "NM_FEATURE_INVALID"          // an unknown feature or state, or an external system with no name
)

// Refusal is a plain refusal with a stable code.
type Refusal struct{ Code, Message string }

func (r *Refusal) Error() string { return r.Code + ": " + r.Message }

// State of a feature.
const (
	On       = "on"
	Off      = "off"
	External = "external"
)

// Feature is one optional feature.
type Feature struct {
	ID    string
	Name  string
	Phase string   // when it is built: P1, P2 or P3
	Needs []string // features that must be on, or external, for this one to be
	// Built is set for a feature of a later phase once it is built: until then it cannot be switched on.
	Built bool
}

// Registry is every optional feature of NM-FEAT-01, in the order the screen shows them. People records is always on and is not listed. Payroll needs
// Leave for leave deductions (NM-FEAT-02) and Onboarding needs Letters for KETs; payslips in self-service need Payroll or an external source, which
// the payslip screens check, not this table.
var Registry = []Feature{
	{ID: "leave", Name: "Leave", Phase: "P1"},
	{ID: "payroll", Name: "Payroll", Phase: "P1", Needs: []string{"leave"}},
	{ID: "self_service", Name: "Self-service", Phase: "P1"},
	{ID: "letters", Name: "Letters", Phase: "P1"},
	{ID: "hiring", Name: "Hiring", Phase: "P2"},
	{ID: "performance", Name: "Performance", Phase: "P2"},
	{ID: "onboarding", Name: "Onboarding", Phase: "P2", Needs: []string{"letters"}, Built: true},
	{ID: "time", Name: "Time and attendance", Phase: "P3"},
}

// Find is the feature with the ID.
func Find(id string) (Feature, bool) {
	for _, f := range Registry {
		if f.ID == id {
			return f, true
		}
	}
	return Feature{}, false
}

// Switches is the organisation's states by feature ID; a feature with no entry is off.
type Switches map[string]Entry

// Entry is one feature's state.
type Entry struct{ State, System string }

// Active reports whether the feature is on or external (another system holds its data, so its dependants may rely on it).
func (s Switches) Active(id string) bool {
	st := s[id].State
	return st == On || st == External
}

// Loaded is the guard every route and job of a feature asks (NM-FEAT-01): only a feature that is on is loaded. An external feature is not Nemura's own
// to run; an off feature has never run, or has stopped with its records kept.
func (s Switches) Loaded(id string) bool { return s[id].State == On }

// Outstanding is one thing that is still open in a feature, which switching it off would leave: runs approved but not paid, KETs due, letters
// requested, government-paid leave claims open (NM-FEAT-05).
type Outstanding struct {
	Feature, What string
	Count         int
}

// Outstander lists what a feature still has open. Each feature implements it as it is built; none exists yet.
type Outstander interface {
	Outstanding(ctx context.Context, feature string) ([]Outstanding, error)
}

// Change is the verdict on setting a feature to a state: either a refusal or the list of what is outstanding to say before it is done.
func Change(current Switches, id, state, system string) error {
	f, ok := Find(id)
	if !ok {
		return &Refusal{CodeInvalid, fmt.Sprintf("There is no feature %q: choose one of %s.", id, names())}
	}
	switch state {
	case On, Off, External:
	default:
		return &Refusal{CodeInvalid, fmt.Sprintf("The state %q is not on, off or external.", state)}
	}
	if (state == External) != (strings.TrimSpace(system) != "") {
		return &Refusal{CodeInvalid, "An external feature names the system that holds its data, and only an external feature does: set system when the state is external."}
	}
	if len(system) > 100 {
		return &Refusal{CodeInvalid, "The system's name is longer than 100 characters: shorten it."}
	}
	if state != Off && f.Phase != "P1" && !f.Built {
		return &Refusal{CodeUnavailable, fmt.Sprintf("%s is built in %s and cannot be switched on yet.", f.Name, f.Phase)}
	}
	if id == "payroll" && state == On {
		return &Refusal{CodeUnavailable, "Payroll needs the Owner and a second approver to switch on; that approval arrives with payroll itself, so it cannot be switched on yet."}
	}
	next := Switches{}
	for k, v := range current {
		next[k] = v
	}
	next[id] = Entry{State: state, System: system}
	if state != Off {
		for _, n := range f.Needs {
			if !next.Active(n) {
				d, _ := Find(n)
				return &Refusal{CodeRequired, fmt.Sprintf("%s needs %s to be on or held by another system: switch %s on first.", f.Name, d.Name, d.Name)}
			}
		}
	}
	if state == Off || state == External {
		// Switching a feature off, or handing it to another system, must not leave one that needs it without it.
		for _, other := range Registry {
			if slices.Contains(other.Needs, id) && next.Active(other.ID) && !next.Active(id) {
				return &Refusal{CodeRequired, fmt.Sprintf("%s needs %s: switch %s off first.", other.Name, f.Name, other.Name)}
			}
		}
	}
	return nil
}

func names() string {
	var n []string
	for _, f := range Registry {
		n = append(n, f.ID)
	}
	return strings.Join(n, ", ")
}

// Arrangement is the replacement arrangement recorded for one outstanding thing when a feature is switched off (NM-FEAT-05).
type Arrangement struct{ What, Arrangement string }

// Settle checks that every outstanding thing has a recorded replacement arrangement, or refuses with the list (NM_FEATURE_OBLIGATIONS_OPEN).
func Settle(open []Outstanding, given []Arrangement) error {
	var missing []string
	for _, o := range open {
		ok := false
		for _, g := range given {
			ok = ok || (g.What == o.What && strings.TrimSpace(g.Arrangement) != "")
		}
		if !ok {
			missing = append(missing, fmt.Sprintf("%s (%d)", o.What, o.Count))
		}
	}
	if len(missing) > 0 {
		return &Refusal{CodeObligations, "Work is still open in this feature: " + strings.Join(missing, "; ") + ". Record how each will be handled, then switch it off."}
	}
	return nil
}
