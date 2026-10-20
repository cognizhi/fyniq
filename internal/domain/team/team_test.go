package team_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/team"
)

// req: access/team-modes Founder mode
// req: access/team-modes Standard mode
// req: access/team-modes Enterprise mode
func TestModes(t *testing.T) {
	if team.Default != team.Founder || team.Blocks(team.Founder) || !team.Blocks(team.Standard) || !team.Blocks(team.Enterprise) {
		t.Error("Founder mode is the default and allows conflicts; Standard and Enterprise mode block them")
	}
	for _, c := range []struct {
		current, mode, code string
	}{
		{team.Founder, team.Standard, ""},
		{team.Standard, team.Founder, ""},
		{team.Founder, team.Founder, team.CodeSame},
		{team.Founder, team.Enterprise, team.CodeUnavailable},
		{team.Founder, "solo", team.CodeUnavailable},
	} {
		ps := team.CheckMode(c.current, c.mode)
		if (len(ps) == 0) != (c.code == "") || (c.code != "" && ps[0].Code != c.code) {
			t.Errorf("%s to %s: %+v, want %q", c.current, c.mode, ps, c.code)
		}
	}
}

// req: access/team-modes Mode switching
func TestASwitchWaitsForEveryConflict(t *testing.T) {
	ann, ben := uuid.New(), uuid.New()
	inv := uuid.New()
	conflicts := []team.Conflict{
		{SoD: "SOD-6", UserID: ann, Name: "Ann", Roles: []string{"controller", "administrator"}},
		{SoD: "SOD-6", UserID: ben, Name: "Ben", Roles: []string{"accountant", "administrator"}},
	}
	codes := func(ps []team.Problem) string {
		var out []string
		for _, p := range ps {
			out = append(out, p.Code+"@"+p.Field)
		}
		return strings.Join(out, " ")
	}
	if got := codes(team.CheckOverrides(conflicts, nil)); got != "TEAM-01@mode TEAM-01@mode" {
		t.Errorf("no overrides: %s", got)
	}
	ok := []team.Override{{UserID: ann, SoD: "SOD-6", Reason: "Founder who runs the books"}, {UserID: ben, SoD: "SOD-6", Reason: "Second founder"}}
	if got := team.CheckOverrides(conflicts, ok); len(got) != 0 {
		t.Errorf("every conflict overridden: %+v", got)
	}
	for _, c := range []struct {
		name      string
		overrides []team.Override
		want      string
	}{
		{"one left", ok[:1], "TEAM-01@mode"},
		{"no reason", []team.Override{ok[0], {UserID: ben, SoD: "SOD-6", Reason: "  "}}, "TEAM-06@overrides[1].reason"},
		{"a reason too long", []team.Override{ok[0], {UserID: ben, SoD: "SOD-6", Reason: strings.Repeat("x", team.MaxReason+1)}}, "TEAM-06@overrides[1].reason"},
		{"twice", []team.Override{ok[0], ok[1], ok[1]}, "TEAM-06@overrides[2]"},
		{"no such conflict", []team.Override{ok[0], ok[1], {UserID: uuid.New(), SoD: "SOD-6", Reason: "x"}}, "TEAM-06@overrides[2]"},
		{"another conflict", []team.Override{ok[0], ok[1], {UserID: ann, SoD: "SOD-2", Reason: "x"}}, "TEAM-06@overrides[2]"},
	} {
		if got := codes(team.CheckOverrides(conflicts, c.overrides)); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
	// An invitation's conflict is never overridden: it is withdrawn.
	withInvite := append(conflicts, team.Conflict{SoD: "SOD-6", InvitationID: inv, Name: "Cat", Roles: []string{"administrator", "owner"}})
	if got := codes(team.CheckOverrides(withInvite, ok)); got != "TEAM-01@mode" {
		t.Errorf("an invitation's conflict: %s", got)
	}
	if k := withInvite[2].Key(); !strings.HasPrefix(k, "invitation:"+inv.String()+":SOD-6:administrator+owner") {
		t.Errorf("key %s", k)
	}
}
