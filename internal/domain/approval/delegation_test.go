package approval

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func date(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

// req: FR-APR-06
func TestADelegationIsLiveOnItsDaysAndNotAfterItIsEnded(t *testing.T) {
	d := Delegation{StartsOn: date(2099, 3, 10), EndsOn: date(2099, 3, 14)}
	for day, want := range map[int]bool{9: false, 10: true, 12: true, 14: true, 15: false} {
		if got := d.Live(date(2099, 3, day)); got != want {
			t.Errorf("on %d March: live = %v, want %v", day, got, want)
		}
	}
	d.Ended = true
	if d.Live(date(2099, 3, 12)) {
		t.Error("an ended delegation is not live")
	}
}

// req: FR-APR-06
func TestADelegationNeedsAnotherActiveMemberWhoMayApproveAndAtMostNinetyDays(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	today := date(2099, 3, 10)
	cases := []struct {
		name                  string
		delegate              uuid.UUID
		from, to              time.Time
		active, approves      bool
		wantField, wantNoProb string
	}{
		{"fine", b, today, date(2099, 3, 20), true, true, "", "ok"},
		{"ninety days", b, today, date(2099, 6, 8), true, true, "", "ok"},
		{"ninety-one days", b, today, date(2099, 6, 9), true, true, "body.ends_on", ""},
		{"oneself", a, today, date(2099, 3, 20), true, true, "body.delegate", ""},
		{"inactive", b, today, date(2099, 3, 20), false, true, "body.delegate", ""},
		{"no approval right", b, today, date(2099, 3, 20), true, false, "body.delegate", ""},
		{"ends before it starts", b, date(2099, 3, 12), date(2099, 3, 11), true, true, "body.ends_on", ""},
		{"ends in the past", b, date(2099, 3, 1), date(2099, 3, 9), true, true, "body.ends_on", ""},
	}
	for _, c := range cases {
		got := CheckDelegation(a, c.delegate, c.from, c.to, today, c.active, c.approves)
		if c.wantNoProb != "" {
			if len(got) != 0 {
				t.Errorf("%s: %v", c.name, got)
			}
			continue
		}
		if len(got) == 0 || got[0].Field != c.wantField || got[0].Code != CodeDelegation || got[0].Fix == "" {
			t.Errorf("%s: %v, want a problem on %s with a fix", c.name, got, c.wantField)
		}
	}
}

// req: FR-APR-06
func TestADelegateStandsInOnlyForAStepTheDelegatorCouldSign(t *testing.T) {
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	today := date(2099, 3, 12)
	live := []Delegation{{Delegator: a, Delegate: b, StartsOn: date(2099, 3, 10), EndsOn: date(2099, 3, 14)}}
	roles := func(u uuid.UUID) ([]string, bool) {
		if u == a {
			return []string{"controller"}, true
		}
		return nil, false
	}
	never := func(uuid.UUID) bool { return false }
	yes := func([]string) bool { return true }
	// the step names the delegator: the delegate stands in
	if got, ok := StandIn(Step{Quorum: 1, Users: []uuid.UUID{a}}, b, live, today, never, roles, yes); !ok || got != a {
		t.Errorf("a step naming the delegator: %v %v", got, ok)
	}
	// the step names a role the delegator holds
	if _, ok := StandIn(Step{Quorum: 1, Roles: []string{"controller"}}, b, live, today, never, roles, yes); !ok {
		t.Error("a step naming the delegator's role")
	}
	// the step names someone else
	if _, ok := StandIn(Step{Quorum: 1, Users: []uuid.UUID{c}}, b, live, today, never, roles, yes); ok {
		t.Error("a step naming someone else needs that person")
	}
	// someone else's delegation is not b's
	if _, ok := StandIn(Step{Quorum: 1, Users: []uuid.UUID{a}}, c, live, today, never, roles, yes); ok {
		t.Error("a delegation lends authority to its delegate alone")
	}
	// the delegator signed already, or a delegator who no longer holds the right
	if _, ok := StandIn(Step{Quorum: 1, Users: []uuid.UUID{a}}, b, live, today, func(u uuid.UUID) bool { return u == a }, roles, yes); ok {
		t.Error("a delegator who signed is not signed for again")
	}
	if _, ok := StandIn(Step{Quorum: 1, Users: []uuid.UUID{a}}, b, live, today, never, roles, func([]string) bool { return false }); ok {
		t.Error("a delegator without the right to sign lends none")
	}
	// outside its days, and once ended
	if _, ok := StandIn(Step{Quorum: 1, Users: []uuid.UUID{a}}, b, live, date(2099, 3, 15), never, roles, yes); ok {
		t.Error("after its last day")
	}
	ended := []Delegation{{Delegator: a, Delegate: b, StartsOn: date(2099, 3, 10), EndsOn: date(2099, 3, 14), Ended: true}}
	if _, ok := StandIn(Step{Quorum: 1, Users: []uuid.UUID{a}}, b, ended, today, never, roles, yes); ok {
		t.Error("an ended delegation")
	}
}
