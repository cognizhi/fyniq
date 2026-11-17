package compliance

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/aiexclusion"
	"github.com/cognizhi/fyniq/internal/domain/aipolicy"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// AIPayrollSetting is the switch that lets the AI read pay (AI-02; p1-ai-assistant task 4.1).
const AIPayrollSetting = "ai.payroll_access"

// aiTraces (CTL-AI-06, AI-02, AI-20; p1-ai-assistant task 4.3): none of the last day's turns of the assistant keeps an
// NRIC or FIN number or a bank account number in what the model wrote or read (the answer, its segments, the facts it
// read, the inputs of its reads), and none read payroll on a day AI payroll access was off. The question is its
// person's words, kept as they typed it and filtered on its way to the model, so it is not read. A finding names the
// turn and the kind of value, never the value.
func aiTraces(ctx context.Context, u storage.UnitOfWork, since time.Time) ([]Finding, string, error) {
	turns, calls, err := u.AIConversations().Page(ctx, storage.AITurnQuery{From: since, To: time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC), Limit: Sample})
	if err != nil {
		return nil, "", err
	}
	changes, err := u.Settings().Changes(ctx, AIPayrollSetting)
	if err != nil {
		return nil, "", err
	}
	def, _ := settings.Lookup(AIPayrollSetting)
	payroll := func(at time.Time) bool {
		y, m, d := at.In(clock.Singapore).Date()
		return settings.On(def, changes, time.Date(y, m, d, 0, 0, 0, 0, time.UTC)).Value == "true"
	}
	return traceFindings(turns, calls, payroll), fmt.Sprintf("%d turn(s) of the assistant, up to %d", len(turns), Sample), nil
}

// traceFindings are the turns whose kept answer, segments, facts or reads' inputs hold a value the AI exclusions keep
// from a model, and those that read payroll on a day AI payroll access was off.
func traceFindings(turns []storage.AITurn, calls map[uuid.UUID][]storage.AIToolCall, payroll func(time.Time) bool) []Finding {
	var out []Finding
	for _, t := range turns {
		var kinds []string
		note := func(where string, texts []string) {
			for _, s := range texts {
				for _, k := range aiexclusion.Found(s) {
					if w := k + " in " + where; !slices.Contains(kinds, w) {
						kinds = append(kinds, w)
					}
				}
			}
		}
		note("its answer", []string{t.Answer})
		note("its segments", jsonStrings(t.Segments))
		note("the facts it read", jsonStrings(t.Facts))
		for _, c := range calls[t.ID] {
			note("the input of a read ("+c.Tool+")", jsonStrings(c.Input))
		}
		for _, k := range kinds {
			out = append(out, Finding{Kind: "ai_turn", ID: t.ID.String(), At: day(t.AskedAt), Detail: "The turn keeps " + k})
		}
		if slices.Contains(t.Classes, aipolicy.Payroll) && !payroll(t.AskedAt) {
			out = append(out, Finding{Kind: "ai_turn", ID: t.ID.String(), At: day(t.AskedAt), Detail: "The turn read payroll on a day AI payroll access was off"})
		}
	}
	return out
}

// jsonStrings are the strings of a JSON document, at any depth; none when it is not one.
func jsonStrings(raw json.RawMessage) []string {
	var v any
	if len(raw) == 0 || json.Unmarshal(raw, &v) != nil {
		return nil
	}
	var out []string
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case string:
			out = append(out, x)
		case []any:
			for _, e := range x {
				walk(e)
			}
		case map[string]any:
			for _, e := range x {
				walk(e)
			}
		}
	}
	walk(v)
	return out
}
