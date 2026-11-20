package automation

import (
	"testing"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"
)

func dec(s string) *apd.Decimal {
	d, _, err := apd.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return d
}

// req: FR-AUT-03
func TestARuleMatchesWhenEveryConditionItSetsHolds(t *testing.T) {
	bill := Posted{DocumentType: "bill", AmountBase: dec("5000.00")}
	for _, c := range []struct {
		name string
		rule Rule
		p    Posted
		want bool
	}{
		{"a bill of exactly the amount", Rule{DocumentType: "bill", AmountAtLeast: dec("5000")}, bill, true},
		{"a bill above it", Rule{DocumentType: "bill", AmountAtLeast: dec("4999.9999")}, bill, true},
		{"a bill below it", Rule{DocumentType: "bill", AmountAtLeast: dec("5000.01")}, bill, false},
		{"another kind", Rule{DocumentType: "invoice", AmountAtLeast: dec("1")}, bill, false},
		{"any kind of any amount", Rule{}, bill, true},
		{"any kind of an amount", Rule{AmountAtLeast: dec("5000.00")}, bill, true},
		{"a posting with no amount known", Rule{AmountAtLeast: dec("1")}, Posted{DocumentType: "bill"}, false},
		{"a kind with no amount condition", Rule{DocumentType: "bill"}, Posted{DocumentType: "bill"}, true},
	} {
		if got := Matches(c.rule, c.p); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

// req: FR-AUT-03
func TestARuleIsCheckedFieldByFieldWithAFixForEach(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	kinds, members := []string{"bill", "invoice"}, []string{a.String(), b.String()}
	if p := Check("Large bills", "bill", "5000.00", []uuid.UUID{a, b}, kinds, members); len(p) != 0 {
		t.Fatalf("a good rule: %v", p)
	}
	for field, c := range map[string]struct {
		name, kind, amount string
		who                []uuid.UUID
	}{
		"body.name":            {"  ", "bill", "", []uuid.UUID{a}},
		"body.document_type":   {"x", "posting", "", []uuid.UUID{a}},
		"body.amount_at_least": {"x", "bill", "-1", []uuid.UUID{a}},
		"body.recipients":      {"x", "bill", "", nil},
	} {
		got := Check(c.name, c.kind, c.amount, c.who, kinds, members)
		if len(got) == 0 || got[0].Field != field || got[0].Code != CodeRule || got[0].Fix == "" {
			t.Errorf("%s: %v", field, got)
		}
	}
	for _, amount := range []string{"1.23456", "abc", "NaN", "Infinity"} {
		if p := Check("x", "", amount, []uuid.UUID{a}, kinds, members); len(p) == 0 {
			t.Errorf("amount %q accepted", amount)
		}
	}
	if p := Check("x", "", "", []uuid.UUID{a, a}, kinds, members); len(p) == 0 {
		t.Error("a recipient twice")
	}
	if p := Check("x", "", "", []uuid.UUID{uuid.New()}, kinds, members); len(p) == 0 {
		t.Error("a recipient who is not an active member")
	}
	many := make([]uuid.UUID, MaxRecipients+1)
	for i := range many {
		many[i] = uuid.New()
	}
	if p := Check("x", "", "", many, kinds, members); len(p) == 0 {
		t.Error("too many recipients")
	}
}
