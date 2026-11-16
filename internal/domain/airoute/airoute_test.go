package airoute

import (
	"errors"
	"testing"

	"github.com/google/uuid"
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
