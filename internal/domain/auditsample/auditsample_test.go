package auditsample

import (
	"fmt"
	"slices"
	"testing"
)

func lines(n int) []Line {
	out := make([]Line, 0, n)
	for i := 0; i < n; i++ {
		acct := "6120"
		if i%3 == 0 {
			acct = "4010"
		}
		out = append(out, Line{EntryID: fmt.Sprintf("e%03d", i), LineNo: i%2 + 1, PostingDate: fmt.Sprintf("2026-%02d-15", i%12+1), Account: acct, BaseAmount: fmt.Sprintf("%d.50", i*10-100), SourceType: "manual_journal"})
	}
	return out
}

// The draw is reproducible: the same seed over the same lines gives the same sample, another seed another, and the population and the filters are exact.
// req: FR-UX-42
func TestTheDrawIsReproducibleAndKeepsToTheRule(t *testing.T) {
	ls := lines(60)
	rule := Rule{From: "2026-03-01", To: "2026-09-30", Account: "6120", MinAmount: "50.00", Count: 5}
	pop, a, err := Draw(rule, "seed-1", ls)
	if err != nil || len(a) != 5 {
		t.Fatalf("the draw: %d %v %v", pop, a, err)
	}
	pop2, b, _ := Draw(rule, "seed-1", slices.Clone(ls))
	if pop2 != pop || !slices.Equal(a, b) {
		t.Errorf("the same seed drew another sample")
	}
	reordered := slices.Clone(ls)
	slices.Reverse(reordered)
	if _, c, _ := Draw(rule, "seed-1", reordered); !slices.Equal(a, c) {
		t.Errorf("the order of the ledger changed the sample")
	}
	if _, d, _ := Draw(rule, "seed-2", ls); slices.Equal(a, d) {
		t.Errorf("another seed drew the same sample")
	}
	want := 0
	for _, l := range ls {
		if ok, _ := rule.Qualifies(l); ok {
			want++
			if l.Account != "6120" || l.PostingDate < "2026-03-01" || l.PostingDate > "2026-09-30" {
				t.Errorf("%+v qualifies against the rule", l)
			}
		}
	}
	if pop != want {
		t.Errorf("the population is %d, not %d", pop, want)
	}
	for _, l := range a {
		if ok, _ := rule.Qualifies(l); !ok {
			t.Errorf("%+v is in the sample but does not qualify", l)
		}
	}
	// A minimum takes the absolute amount: a credit of -250.00 is as large as a debit of 250.00.
	if ok, _ := (Rule{From: "2026-01-01", To: "2026-12-31", MinAmount: "250.00", Count: 1}).Qualifies(Line{PostingDate: "2026-05-05", BaseAmount: "-250.00"}); !ok {
		t.Error("a credit of 250.00 is below a minimum of 250.00")
	}
	// A short population gives all of it; a bad rule is refused.
	if pop, s, _ := Draw(Rule{From: "2026-01-01", To: "2026-12-31", Account: "4010", MinAmount: "400", Count: 200}, "s", ls); len(s) != pop {
		t.Errorf("a short population: %d of %d", len(s), pop)
	}
	for _, bad := range []Rule{{Count: 0}, {Count: 201}, {From: "a", To: "b", MinAmount: "x", Count: 1}, {From: "a", To: "b", MinAmount: "-1", Count: 1}} {
		if _, _, err := Draw(bad, "s", []Line{{PostingDate: "a", BaseAmount: "1.00"}}); err == nil {
			t.Errorf("%+v was drawn", bad)
		}
	}
	if _, _, err := Draw(rule, "", ls); err == nil {
		t.Error("a draw with no seed")
	}
}
