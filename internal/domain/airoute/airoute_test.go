package airoute

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/aiqual"
	"github.com/cognizhi/fyniq/internal/ports"
)

var chat = Agent{ID: "chat", Name: "Assistant", Classes: []string{"know-how", "business-figures", "personal-data"}, Line: LineMain}

// A route names the classes its agent handles, never restricted data, and one to five steps, each a connection and model
// once, with a known effort (AI-74).
// req: AI-74
func TestARouteIsChecked(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	good := Route{Classes: []string{"know-how", "business-figures"}, Steps: []Step{{Connection: a, Model: "m1", Effort: "high", Cache: true},
		{Connection: b, Model: "m1"}, {Connection: a, Model: "m2", Effort: "low"}}}
	if err := Check(chat, good); err != nil {
		t.Fatal(err)
	}
	six := Route{Classes: []string{"know-how"}}
	for range 6 {
		six.Steps = append(six.Steps, Step{Connection: uuid.New(), Model: "m"})
	}
	var e *Error
	for name, tc := range map[string]struct {
		r     Route
		field string
	}{
		"no class":          {Route{Steps: good.Steps}, "classes"},
		"restricted data":   {Route{Classes: []string{"restricted"}, Steps: good.Steps}, "classes[0]"},
		"a class it lacks":  {Route{Classes: []string{"payroll"}, Steps: good.Steps}, "classes[0]"},
		"a class twice":     {Route{Classes: []string{"know-how", "know-how"}, Steps: good.Steps}, "classes[1]"},
		"no step":           {Route{Classes: []string{"know-how"}}, "steps"},
		"six steps":         {six, "steps"},
		"a step twice":      {Route{Classes: []string{"know-how"}, Steps: []Step{{Connection: a, Model: "m1"}, {Connection: a, Model: "m1"}}}, "steps[1]"},
		"no model":          {Route{Classes: []string{"know-how"}, Steps: []Step{{Connection: a}}}, "steps[0]"},
		"an unknown effort": {Route{Classes: []string{"know-how"}, Steps: []Step{{Connection: a, Model: "m1", Effort: "max"}}}, "steps[0].effort"},
	} {
		if err := Check(chat, tc.r); !errors.As(err, &e) || e.Code != CodeRoute || e.Field != tc.field {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// A version widens where data goes when it names a connection and model, or a class, the one in force did not; an
// agent's first route always does. Reordering, dropping a step or a class, and changing the effort or caching do not
// (AI-74, AI-26).
// req: AI-74
// req: AI-26
func TestWideningNeedsASecondApprover(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	was := &Route{Classes: []string{"know-how", "business-figures"}, Steps: []Step{{Connection: a, Model: "m1"}, {Connection: b, Model: "m1"}}}
	for name, tc := range map[string]struct {
		next Route
		want bool
	}{
		"reordered":          {Route{Classes: was.Classes, Steps: []Step{{Connection: b, Model: "m1"}, {Connection: a, Model: "m1", Effort: "high"}}}, false},
		"a step dropped":     {Route{Classes: []string{"know-how"}, Steps: []Step{{Connection: a, Model: "m1", Cache: true}}}, false},
		"another model":      {Route{Classes: was.Classes, Steps: []Step{{Connection: a, Model: "m2"}}}, true},
		"another connection": {Route{Classes: was.Classes, Steps: []Step{{Connection: uuid.New(), Model: "m1"}}}, true},
		"another class":      {Route{Classes: []string{"personal-data"}, Steps: was.Steps}, true},
	} {
		if got := Widens(was, tc.next); got != tc.want {
			t.Errorf("%s: widens %v", name, got)
		}
	}
	if !Widens(nil, *was) {
		t.Error("a first route does not widen")
	}
	var e *Error
	if err := StepFailure(0, Step{Model: "m1"}, true, true, nil); err != nil {
		t.Error(err)
	}
	for name, err := range map[string]error{
		"off":         StepFailure(1, Step{Model: "m1"}, false, true, nil),
		"not allowed": StepFailure(1, Step{Model: "m1"}, true, false, nil),
		"the policy":  StepFailure(1, Step{Model: "m1"}, true, true, errors.New("personal-data goes only to Singapore")),
	} {
		if !errors.As(err, &e) || e.Code != CodeStep {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := Unknown("dreamer"); !errors.As(err, &e) || e.Code != CodeAgent {
		t.Errorf("an unknown agent: %v", err)
	}
}

// A preset pins the newest allowed model of each line from the discovered list, never a model ID (AI-26): A in
// Singapore, B on the global endpoint without personal data or payroll, C by the agent's classes; a dated model before
// a moving alias; only a model that passed the agent's evals (AI-78); none when no connection on the target has a model
// of the lines.
// req: AI-26
// req: AI-74
// req: AI-78
func TestPresetsPinTheNewestModelOfTheirLine(t *testing.T) {
	day := func(d int) *time.Time { t := time.Date(2026, 10, d, 0, 0, 0, 0, time.UTC); return &t }
	sg, gl, other := uuid.New(), uuid.New(), uuid.New()
	cs := []Candidate{
		{Connection: sg, Name: "Claude SG", Connector: "anthropic", Auth: "workload-identity", Location: "asia-southeast1", Models: []CandidateModel{
			{ID: "claude-opus-4-6", Created: day(1)}, {ID: "claude-opus-4-7", Created: day(9)}, {ID: "claude-opus-latest", AliasOf: "claude-opus-4-7", Created: day(10)},
			{ID: "claude-sonnet-4-6", Created: day(1)}}},
		{Connection: gl, Name: "Claude global", Connector: "anthropic", Auth: "workload-identity", Location: "global", Models: []CandidateModel{
			{ID: "claude-opus-5-5", Created: day(5)}, {ID: "claude-haiku-5-5", Created: day(5)}}},
		{Connection: other, Name: "OpenAI", Connector: "openai", Auth: "api-key", Models: []CandidateModel{{ID: "claude-opus-9"}}},
	}
	// Every model passed every agent's evals, but for a newer Opus in Singapore that passed none (AI-78).
	for i := range cs {
		for j := range cs[i].Models {
			cs[i].Models[j].Qualified = []string{"chat", "bank_categorisation", "report_commentary", "payroll_only"}
		}
	}
	cs[0].Models = append(cs[0].Models, CandidateModel{ID: "claude-opus-4-8", Created: day(20)})
	classify := Agent{ID: "bank_categorisation", Name: "Bank categorisation", Classes: []string{"business-figures", "personal-data"}, Line: LineClassification}
	report := Agent{ID: "report_commentary", Name: "Report commentary", Classes: []string{"business-figures"}, Line: LineMain}
	payroll := Agent{ID: "payroll_only", Name: "Payroll", Classes: []string{"payroll"}, Line: LineMain}
	steps := func(p Proposal) string {
		var out []string
		for _, s := range p.Route.Steps {
			out = append(out, s.Model+"@"+map[uuid.UUID]string{sg: "sg", gl: "global", other: "other"}[s.Connection])
		}
		return strings.Join(out, ",")
	}
	a, _ := PresetOf("A")
	b, _ := PresetOf("B")
	c, _ := PresetOf("C")
	for name, tc := range map[string]struct {
		p              Preset
		agent          Agent
		steps, classes string
		why            string
	}{
		"A for chat":               {a, chat, "claude-opus-4-7@sg,claude-sonnet-4-6@sg", "know-how,business-figures,personal-data", ""},
		"A for classification":     {a, classify, "claude-sonnet-4-6@sg,claude-opus-4-7@sg", "business-figures,personal-data", ""},
		"B leaves personal data":   {b, classify, "claude-haiku-5-5@global,claude-opus-5-5@global", "business-figures", ""},
		"C keeps personal data in": {c, classify, "claude-sonnet-4-6@sg,claude-opus-4-7@sg", "business-figures,personal-data", ""},
		"C sends the rest abroad":  {c, report, "claude-opus-5-5@global,claude-haiku-5-5@global", "business-figures", ""},
		"B for payroll alone":      {b, payroll, "", "", "handles only personal data or payroll"},
	} {
		got := Apply(tc.p, tc.agent, cs)
		if steps(got) != tc.steps || strings.Join(got.Route.Classes, ",") != tc.classes || !strings.Contains(got.Why, tc.why) {
			t.Errorf("%s: %s %v %q", name, steps(got), got.Route.Classes, got.Why)
		}
	}
	if got := Apply(a, chat, cs[1:]); got.Why == "" || len(got.Route.Steps) != 0 {
		t.Errorf("no Singapore connection: %+v", got)
	}
	if got := Apply(b, classify, cs); len(got.Notes) != 1 || !strings.Contains(got.Notes[0], "personal-data stays with preset A") {
		t.Errorf("the note: %v", got.Notes)
	}
	if _, ok := PresetOf("D"); ok {
		t.Error("a preset D")
	}
}

// A step is tried only when it meets the call's classes and capabilities (AI-76, AI-83, AI-27, AI-87); an attempt's
// error decides whether the next eligible step may take the call (AI-76).
// req: AI-76
// req: AI-83
// req: AI-87
// req: AI-78
func TestTheRoutersRules(t *testing.T) {
	req := ports.ModelRequest{Tools: []ports.ModelTool{{Name: "x"}}, Output: &ports.ModelSchema{Name: "a"},
		Messages: []ports.ModelMessage{{Parts: []ports.ModelPart{{Kind: ports.PartImage}, {Kind: ports.PartDocument}}}}}
	n := NeedsOf(req, true)
	if n != (Needs{Tools: true, StrictOutput: true, Streaming: true, Images: true, PDF: true}) {
		t.Fatalf("the needs: %+v", n)
	}
	all := ports.ModelCapabilities{Tools: true, StrictOutput: true, Streaming: true, Images: true, PDF: true}
	if n.Lacking(all) != "" || n.Lacking(ports.ModelCapabilities{Tools: true}) != "strict structured output" {
		t.Error("lacking")
	}
	ok := StepState{Enabled: true, Allowed: true, Callable: true, Standing: aiqual.Qualified, Priced: true, Billing: "usage", Caps: all}
	if why := Why([]string{"personal-data"}, n, ok, "m"); why != "" {
		t.Errorf("an eligible step: %s", why)
	}
	for name, tc := range map[string]struct {
		s    func(StepState) StepState
		want string
	}{
		"off":          {func(s StepState) StepState { s.Enabled = false; return s }, "switched off"},
		"not allowed":  {func(s StepState) StepState { s.Allowed = false; return s }, "allow-list"},
		"not callable": {func(s StepState) StepState { s.Callable = false; return s }, "no longer listed"},
		"the policy":   {func(s StepState) StepState { s.Policy = errors.New("no"); return s }, "data-class policy"},
		"an alias":     {func(s StepState) StepState { s.Alias = true; return s }, "moving alias"},
		"unqualified":  {func(s StepState) StepState { s.Standing = aiqual.Unqualified; return s }, "has not passed this agent's evals"},
		"no standing":  {func(s StepState) StepState { s.Standing = ""; return s }, "has not passed this agent's evals"},
		"out of date":  {func(s StepState) StepState { s.Standing = aiqual.OutOfDate; return s }, "qualification is out of date"},
		"unpriced":     {func(s StepState) StepState { s.Priced = false; return s }, "no price"},
		"unreachable":  {func(s StepState) StepState { s.Built = errors.New("no key"); return s }, "could not be reached"},
		"no PDF":       {func(s StepState) StepState { s.Caps.PDF = false; return s }, "PDF input"},
	} {
		if why := Why([]string{"personal-data"}, n, tc.s(ok), "m"); !strings.Contains(why, tc.want) {
			t.Errorf("%s: %q", name, why)
		}
	}
	alias := ok
	alias.Alias = true
	if why := Why([]string{"business-figures"}, n, alias, "m"); why != "" {
		t.Errorf("an alias for business figures: %q", why)
	}
	stale := ok
	stale.Standing = aiqual.OutOfDate
	if why := Why([]string{"business-figures"}, n, stale, "m"); why != "" {
		t.Errorf("an out-of-date qualification for business figures: %q", why)
	}
	for _, tc := range []struct {
		err       error
		delivered bool
		outcome   string
		next      bool
	}{
		{ports.ErrModelBusy, false, Busy, true}, {ports.ErrModelNotFound, false, NotFound, true}, {context.DeadlineExceeded, false, TimedOut, true},
		{errors.New("broken"), false, Failed, true}, {ports.ErrModelRequest, false, Refused, false}, {ports.ErrModelBusy, true, Failed, false},
		{context.Canceled, false, Stopped, false},
	} {
		if outcome, next := Failover(tc.err, tc.delivered); outcome != tc.outcome || next != tc.next {
			t.Errorf("%v (delivered %v): %s %v", tc.err, tc.delivered, outcome, next)
		}
	}
	var e *Error
	if err := Unhandled([]string{"business-figures", "payroll"}, []string{"business-figures"}); !errors.As(err, &e) || e.Code != CodeUnhandled ||
		!strings.Contains(e.Message, "payroll") {
		t.Errorf("an unhandled class: %v", err)
	}
	if Unhandled([]string{"know-how"}, []string{"know-how"}) != nil {
		t.Error("a handled class")
	}
}
