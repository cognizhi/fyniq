package proposal_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/proposal"
)

// A payload is kept, hashed and executed as its canonical JSON: each object's keys sorted, no spaces, numbers as sent;
// the same payload sent any other way hashes the same.
// req: ai/proposals Proposal content and payload hash
func TestAPayloadIsKeptAsItsCanonicalJSON(t *testing.T) {
	a, sumA, err := proposal.Canonical(json.RawMessage(`{ "title": "Hold the AGM", "due": "2027-06-30", "n": 12345678901234567890, "tags": [{"b":1,"a":2}] }`))
	if err != nil {
		t.Fatal(err)
	}
	if a != `{"due":"2027-06-30","n":12345678901234567890,"tags":[{"a":2,"b":1}],"title":"Hold the AGM"}` || len(sumA) != 64 {
		t.Errorf("canonical: %s %s", a, sumA)
	}
	_, sumB, _ := proposal.Canonical(json.RawMessage(`{"tags":[{"a":2,"b":1}],"title":"Hold the AGM","n":12345678901234567890,"due":"2027-06-30"}`))
	if sumB != sumA {
		t.Error("the same payload hashed differently")
	}
	for _, bad := range []string{`[1]`, `"x"`, `null`, `{`} {
		if _, _, err := proposal.Canonical(json.RawMessage(bad)); err == nil {
			t.Errorf("%s was a payload", bad)
		}
	}
}

// A payload keeps to its task type's limits: its items, each item's amount and their total in exact decimals; a task
// type without an amount limit moves no money.
// req: ai/task-catalogue Catalogue entry attributes
func TestAPayloadKeepsToItsLimits(t *testing.T) {
	c, err := load("test.annotate_note", fixture(func(m map[string]any) {
		m["limits"] = map[string]any{"items": 3, "amount_per_item": "100.00", "amount_total": "250.00", "currency": "SGD"}
	}))
	if err != nil {
		t.Fatal(err)
	}
	tt, _ := c.Get("test.annotate_note")
	plain, _ := load("test.annotate_note", fixture(nil))
	none, _ := plain.Get("test.annotate_note")
	for _, x := range []struct {
		tt   proposal.TaskType
		m    proposal.Measure
		want string
	}{
		{tt, proposal.Measure{Items: 3, Amounts: []string{"100.00", "100.00", "50.00"}}, ""},
		{tt, proposal.Measure{Items: 4}, "it holds 4 items"},
		{tt, proposal.Measure{Items: 1, Amounts: []string{"100.01"}}, "more than the SGD 100.00 one item may hold"},
		{tt, proposal.Measure{Items: 3, Amounts: []string{"100.00", "100.00", "50.01"}}, "come to SGD 250.01"},
		{tt, proposal.Measure{Items: 1, Amounts: []string{"1e2"}}, ""},
		{tt, proposal.Measure{Items: 1, Amounts: []string{"-1"}}, "not an exact decimal"},
		{tt, proposal.Measure{Items: 1, Amounts: []string{"NaN"}}, "not an exact decimal"},
		{none, proposal.Measure{Items: 0}, ""},
		{none, proposal.Measure{Items: 1, Amounts: []string{"1.00"}}, "moves money"},
	} {
		got := x.tt.Within(x.m)
		if (x.want == "") != (got == "") || !strings.Contains(got, x.want) {
			t.Errorf("%+v: %q, want %q", x.m, got, x.want)
		}
	}
}
