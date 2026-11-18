package compliance

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The trace scan of CTL-AI-06 (AI-02, AI-20): a kept answer, segment, fact or read's input holding an NRIC or a bank
// account number is a finding, named by turn and kind and never by value; so is a turn that read payroll on a day AI
// payroll access was off; decimals, dates, UUIDs and masked values are not.
// req: CTL-AI-06
// asvs: V14.2.1
func TestTheAITracesHoldNoExcludedValue(t *testing.T) {
	on := time.Date(2026, 11, 10, 2, 0, 0, 0, time.UTC)
	turn := func(answer, segments, facts string, classes ...string) storage.AITurn {
		return storage.AITurn{ID: uuid.New(), Answer: answer, Segments: json.RawMessage(segments), Facts: json.RawMessage(facts), Classes: classes, AskedAt: on}
	}
	clean := turn("Revenue was SGD 125,000.00 on 2026-10-15; Tan's NRIC is S****567D.", `[{"text":"Revenue was "},{"fact":"F00000000000000aa"}]`,
		`[{"id":"F00000000000000aa","value":"125000.00","member":"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2b","lineage":"/v1/accounts/0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2b"}]`)
	leaky := turn("Their NRIC is S1234567D.", `[{"text":"Pay 012-345678-9"}]`, `[]`)
	searched := turn("I found nobody.", `[]`, `[]`)
	paid := turn("Payroll cost SGD 9,000.00.", `[]`, `[]`, "business-figures", "payroll")
	calls := map[uuid.UUID][]storage.AIToolCall{searched.ID: {{Tool: "records.search", Input: json.RawMessage(`{"kind":"contact","q":"S1234567D"}`)}}}
	off := func(time.Time) bool { return false }
	found := traceFindings([]storage.AITurn{clean, leaky, searched, paid}, calls, off)
	var got []string
	for _, f := range found {
		if f.Kind != "ai_turn" || f.At != "2026-11-10T02:00:00Z" {
			t.Errorf("a finding: %+v", f)
		}
		got = append(got, f.ID+" "+f.Detail)
	}
	want := []string{
		leaky.ID.String() + " The turn keeps an NRIC or FIN number in its answer",
		leaky.ID.String() + " The turn keeps a bank account number in its segments",
		searched.ID.String() + " The turn keeps an NRIC or FIN number in the input of a read (records.search)",
		paid.ID.String() + " The turn read payroll on a day AI payroll access was off",
	}
	if !slices.Equal(got, want) {
		t.Errorf("the findings:\n%q\nwant\n%q", got, want)
	}
	for _, f := range found {
		if strings.Contains(f.Detail, "1234567") || strings.Contains(f.Detail, "345678") {
			t.Errorf("a finding holds the value: %+v", f)
		}
	}
	if found := traceFindings([]storage.AITurn{paid}, nil, func(time.Time) bool { return true }); len(found) != 0 {
		t.Errorf("payroll read while AI payroll access was on: %+v", found)
	}
}
