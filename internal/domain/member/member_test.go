package member

import (
	"slices"
	"testing"
	"time"
)

func codes(ps []Problem) []string {
	var out []string
	for _, p := range ps {
		out = append(out, p.Code+" "+p.Field)
	}
	return out
}

// Roles are those a member can hold, each once; an expiry is in the future and never on the Owner role.
// req: FR-ADM-01
func TestRolesAreKnownOnceAndExpireOnlyInTheFuture(t *testing.T) {
	now := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	later, earlier := now.Add(24*time.Hour), now.Add(-time.Second)
	if p := CheckRoles([]string{"owner", "controller", "accountant"}, nil, now); p != nil {
		t.Errorf("a founder's roles: %v", p)
	}
	if p := CheckRoles([]string{"auditor"}, &later, now); p != nil {
		t.Errorf("an external auditor until tomorrow: %v", p)
	}
	for name, c := range map[string]struct {
		roles   []string
		expires *time.Time
		want    []string
	}{
		"none":                    {nil, nil, []string{"MEM-03 roles"}},
		"unknown and twice":       {[]string{"super_admin", "owner", "owner", "line_manager"}, nil, []string{"MEM-01 roles[0]", "MEM-03 roles[2]", "MEM-01 roles[3]"}},
		"expired":                 {[]string{"auditor"}, &now, []string{"MEM-04 roles_expire_at"}},
		"in the past":             {[]string{"auditor"}, &earlier, []string{"MEM-04 roles_expire_at"}},
		"an Owner with an expiry": {[]string{"owner"}, &later, []string{"MEM-04 roles_expire_at"}},
	} {
		if got := codes(CheckRoles(c.roles, c.expires, now)); !slices.Equal(got, c.want) {
			t.Errorf("%s: %v; want %v", name, got, c.want)
		}
	}
	if got := codes(CheckRoles(slices.Repeat([]string{"employee"}, 17), nil, now)); len(got) == 0 || got[0] != "MEM-03 roles" {
		t.Errorf("seventeen roles: %v", got)
	}
	if got := codes(CheckName(" ")); !slices.Equal(got, []string{"MEM-02 display_name"}) {
		t.Errorf("a blank name: %v", got)
	}
	if p := CheckName("Wei Ling Tan"); p != nil {
		t.Errorf("a name: %v", p)
	}
	for _, r := range Assignable {
		if r == "super_admin" || r == "ai" || r == "line_manager" || r == "hr_officer" {
			t.Errorf("%s is assignable", r)
		}
	}
	if !slices.IsSorted(Assignable) || len(Assignable) != 16 {
		t.Errorf("the assignable roles are not the database's 16, sorted: %v", Assignable)
	}
}

// Until a second member joins the Owner acts alone; after that a role change needs a second approver.
// req: FR-ADM-01
// req: FM-1
func TestTheFounderBootstrapEndsWithTheSecondMember(t *testing.T) {
	if p := Bootstrap(1); p != nil {
		t.Errorf("one member: %v", p)
	}
	for _, n := range []int{2, 3} {
		if p := Bootstrap(n); p == nil || p.Code != CodeSecondApprover {
			t.Errorf("%d members: %v", n, p)
		}
	}
}
