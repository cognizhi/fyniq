package features

import (
	"errors"
	"testing"
)

func code(err error) string {
	var r *Refusal
	if errors.As(err, &r) {
		return r.Code
	}
	return ""
}

// The dependencies are enforced and each refusal names its reason: Payroll needs Leave on or held elsewhere, Onboarding needs Letters, a feature
// another needs cannot be switched off, a later phase and Payroll's second approver are not available yet, an external feature names its system, and
// switching off with work open needs a replacement arrangement for each (NM-FEAT-01 to NM-FEAT-05).
// req: NM-FEAT-02, NM-FEAT-03, NM-FEAT-05
func TestTheDependenciesAreEnforcedAndEveryRefusalIsPlain(t *testing.T) {
	on := func(ids ...string) Switches {
		s := Switches{}
		for _, id := range ids {
			s[id] = Entry{State: On}
		}
		return s
	}
	for _, tc := range []struct {
		name              string
		cur               Switches
		id, state, system string
		want              string
	}{
		{"leave on", on(), "leave", On, "", ""},
		{"letters on", on(), "letters", On, "", ""},
		{"self-service on", on(), "self_service", On, "", ""},
		{"payroll needs the approval that is not built", on("leave"), "payroll", On, "", CodeUnavailable},
		{"payroll held elsewhere needs leave", on(), "payroll", External, "Acme Payroll", CodeRequired},
		{"payroll held elsewhere with leave on", on("leave"), "payroll", External, "Acme Payroll", ""},
		{"leave cannot go off under payroll", Switches{"leave": {State: On}, "payroll": {State: External, System: "Acme"}}, "leave", Off, "", CodeRequired},
		{"leave can be held elsewhere under payroll", Switches{"leave": {State: On}, "payroll": {State: External, System: "Acme"}}, "leave", External, "Other HR", ""},
		{"onboarding is later", on("letters"), "onboarding", On, "", CodeUnavailable},
		{"hiring is later", on(), "hiring", On, "", CodeUnavailable},
		{"time is later", on(), "time", On, "", CodeUnavailable},
		{"an unknown feature", on(), "magic", On, "", CodeInvalid},
		{"an unknown state", on(), "leave", "maybe", "", CodeInvalid},
		{"external with no system", on(), "leave", External, "  ", CodeInvalid},
		{"a system with a feature that is on", on(), "leave", On, "Acme", CodeInvalid},
		{"a system name too long", on(), "leave", External, string(make([]byte, 101)), CodeInvalid},
		{"off is always allowed for a later phase", on(), "hiring", Off, "", ""},
	} {
		err := Change(tc.cur, tc.id, tc.state, tc.system)
		if code(err) != tc.want {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
	// Only a feature that is on is loaded; an external feature is another system's.
	s := Switches{"leave": {State: On}, "payroll": {State: External, System: "Acme"}, "letters": {State: Off}}
	if !s.Loaded("leave") || s.Loaded("payroll") || s.Loaded("letters") || s.Loaded("self_service") {
		t.Error("the guard loads more than the features that are on")
	}
	if !s.Active("payroll") || s.Active("letters") {
		t.Error("an external feature is active for the features that need it, an off one is not")
	}
	// Open work needs an arrangement for each item.
	open := []Outstanding{{Feature: "leave", What: "leave requests waiting for approval", Count: 3}, {Feature: "leave", What: "government-paid leave claims open", Count: 1}}
	if code(Settle(open, nil)) != CodeObligations || code(Settle(open, []Arrangement{{"leave requests waiting for approval", "Approved in the old system"}})) != CodeObligations ||
		code(Settle(open, []Arrangement{{"leave requests waiting for approval", " "}, {"government-paid leave claims open", "Filed by HR"}})) != CodeObligations {
		t.Error("open work was settled without an arrangement for each item")
	}
	if err := Settle(open, []Arrangement{{"leave requests waiting for approval", "Approved in the old system"}, {"government-paid leave claims open", "Filed by HR"}}); err != nil {
		t.Errorf("an arrangement for each item: %v", err)
	}
	if err := Settle(nil, nil); err != nil {
		t.Errorf("nothing open: %v", err)
	}
}
