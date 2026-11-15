package approval

import (
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

// A policy's rules decide a request's steps in order: the first whose conditions all hold, else the default steps; a
// condition on something the change does not carry never holds (FR-APR-01; PRD-001 rev 0.178, the owner's decision
// of 6 Nov 2026).
// req: FR-APR-01
// req: access/approvals Conditions decide the steps
func TestTheFirstMatchingRuleDecidesTheSteps(t *testing.T) {
	sgd := money.MustLookup("SGD")
	amount := func(s string) *money.Money {
		m, err := money.Parse(s, sgd)
		if err != nil {
			t.Fatal(err)
		}
		return &m
	}
	rent, audit, sales, northwind := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	one, two, three, def := []Step{{Quorum: 1}}, []Step{{Quorum: 2}}, []Step{{Quorum: 3}}, []Step{{Quorum: 4}}
	p := Policy{Steps: def, Rules: []Rule{
		{Conditions: Conditions{AmountAtLeast: "5000.00"}, Steps: one},
		{Conditions: Conditions{Accounts: []uuid.UUID{rent}, Departments: []uuid.UUID{sales}}, Steps: two},
		{Conditions: Conditions{Vendors: []uuid.UUID{northwind}}, Steps: three},
	}}
	for name, c := range map[string]struct {
		f     Facts
		steps []Step
		rule  int
	}{
		"at the amount":                        {Facts{Amount: amount("5000.00")}, one, 1},
		"below it":                             {Facts{Amount: amount("4999.99")}, def, 0},
		"rent in sales":                        {Facts{Amount: amount("100.00"), Accounts: []uuid.UUID{audit, rent}, Values: []uuid.UUID{sales}}, two, 2},
		"rent in no department":                {Facts{Accounts: []uuid.UUID{rent}}, def, 0},
		"the first that holds, though both do": {Facts{Amount: amount("9000.00"), Vendors: []uuid.UUID{northwind}}, one, 1},
		"the vendor":                           {Facts{Vendors: []uuid.UUID{northwind}}, three, 3},
		"a change with no amount":              {Facts{}, def, 0},
	} {
		steps, rule, err := p.StepsFor(c.f)
		if err != nil || rule != c.rule || !slices.EqualFunc(steps, c.steps, func(a, b Step) bool { return a.Quorum == b.Quorum }) {
			t.Errorf("%s: rule %d, steps %+v, %v; want rule %d", name, rule, steps, err, c.rule)
		}
	}
	codes := func(fps []FieldProblem) []string {
		var out []string
		for _, x := range fps {
			out = append(out, x.Field+" "+x.Code)
		}
		return out
	}
	approves := func(string) bool { return true }
	var members []Member
	for range 4 {
		members = append(members, Member{UserID: uuid.New(), Roles: []string{"owner"}, Approves: true})
	}
	if got := codes(CheckRules("Journals", p.Rules, sgd, approves, members)); len(got) != 0 {
		t.Errorf("good rules refused: %v", got)
	}
	bad := []Rule{{Steps: one}, {Conditions: Conditions{AmountAtLeast: "-1.00"}, Steps: one}, {Conditions: Conditions{AmountAtLeast: "1"}, Steps: []Step{{Quorum: 9}}}}
	want := []string{"body.rules[0].conditions APR-16", "body.rules[1].conditions.amount_at_least APR-16", "body.rules[2].steps[0].quorum APR-08"}
	if got := codes(CheckRules("Journals", bad, sgd, approves, members)); !slices.Equal(got[:3], want) {
		t.Errorf("bad rules: %v, want %v", got, want)
	}
	if got := codes(CheckRules("Journals", make([]Rule, MaxRules+1), sgd, approves, members)); len(got) != 1 || got[0] != "body.rules APR-16" {
		t.Errorf("too many rules: %v", got)
	}
}
