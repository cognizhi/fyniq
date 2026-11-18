// Package airoute keeps each agent's route (PRD-001 AI-74, AI-26; p1-ai-providers task 5.1, design D6): the data
// classes the agent handles and its ordered steps, the first the primary and the rest its fallbacks, each a connection,
// a model on that connection's allow-list, the thinking effort and whether the stable prompt and tools are cached. The
// Owner or an Administrator sets a route at once when it only narrows where data goes; a version that sends the agent's
// data to a connection or model the one before did not use, or a class it did not handle, waits for the Owner and a
// second approver, as does an agent's first route. Model IDs change only so (AI-26).
package airoute

import (
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/aipolicy"
)

// The codes of a refused route (ENG-23).
const (
	CodeRoute = "AI-ROUTE-01" // not a valid route: no class or step, an unknown class or effort, a step twice, too many steps
	CodeStep  = "AI-ROUTE-02" // a step's connection or model cannot serve: switched off, not on the allow-list, or not allowed the route's classes
	CodeAgent = "AI-ROUTE-03" // not an agent of this release
)

// Error is a refused route, in Fyniq's words (ENG-23).
type Error struct {
	Code, Field, Message, Fix string
}

func (e *Error) Error() string { return e.Code + ": " + e.Field + " " + e.Message }

func refuse(code, field, message, fix string) *Error {
	return &Error{Code: code, Field: field, Message: message, Fix: fix}
}

// MaxSteps is how many steps a route has at most: its primary and four fallbacks.
const MaxSteps = 5

// Efforts are the thinking efforts a step may ask for; "" leaves the model's default (AI-27).
var Efforts = []string{"", "low", "medium", "high"}

// Feature is what a route's calls use of a connection, for its data-handling declaration (AI-75): messages through the
// model port, never a batch.
const Feature = "messages"

// The model lines a preset pins (AI-26).
const (
	LineMain           = "main"
	LineClassification = "classification"
)

// Agent is an agent that calls a model, as the release lists it.
type Agent struct {
	ID          string
	Name        string
	Description string
	Classes     []string // the classes it may ever handle
	Line        string
}

// Step is one step of a route.
type Step struct {
	Connection uuid.UUID
	Model      string
	Effort     string
	Cache      bool
}

// Route is a version of an agent's route.
type Route struct {
	Classes []string
	Steps   []Step
}

// Check checks a route for its agent: at least one class, each one the agent handles and never restricted data, and one
// to MaxSteps steps, each a connection and model once, with an effort Fyniq knows.
func Check(a Agent, r Route) error {
	if len(r.Classes) == 0 {
		return refuse(CodeRoute, "classes", "is empty", "Name the data classes the agent handles on this route")
	}
	seen := map[string]bool{}
	for i, c := range r.Classes {
		field := fmt.Sprintf("classes[%d]", i)
		switch {
		case !slices.Contains(aipolicy.Classes, c) || c == aipolicy.Restricted:
			return refuse(CodeRoute, field, c+" is not a class an AI provider may receive", "Use know-how, business-figures, personal-data or payroll")
		case !slices.Contains(a.Classes, c):
			return refuse(CodeRoute, field, a.Name+" never handles "+c, "Leave it out of the route")
		case seen[c]:
			return refuse(CodeRoute, field, c+" is named twice", "Name each class once")
		}
		seen[c] = true
	}
	if len(r.Steps) == 0 || len(r.Steps) > MaxSteps {
		return refuse(CodeRoute, "steps", fmt.Sprintf("has %d steps, not 1 to %d", len(r.Steps), MaxSteps),
			"Give a primary model and at most four fallbacks")
	}
	pairs := map[string]bool{}
	for i, s := range r.Steps {
		field := fmt.Sprintf("steps[%d]", i)
		key := s.Connection.String() + " " + s.Model
		switch {
		case s.Connection == uuid.Nil || s.Model == "":
			return refuse(CodeRoute, field, "names no connection or model", "Name a connection and a model on its allow-list")
		case pairs[key]:
			return refuse(CodeRoute, field, "names "+s.Model+" on the same connection twice", "Name each model of a connection once")
		case !slices.Contains(Efforts, s.Effort):
			return refuse(CodeRoute, field+".effort", "is not low, medium, high or empty", "Choose an effort, or leave it for the model's default")
		}
		pairs[key] = true
	}
	return nil
}

// Widens reports whether next sends the agent's data anywhere the version in force did not: a connection and model it
// did not use, or a class it did not handle. An agent's first route always widens (AI-74, AI-26).
func Widens(was *Route, next Route) bool {
	if was == nil {
		return true
	}
	for _, c := range next.Classes {
		if !slices.Contains(was.Classes, c) {
			return true
		}
	}
	for _, s := range next.Steps {
		if !slices.ContainsFunc(was.Steps, func(w Step) bool { return w.Connection == s.Connection && w.Model == s.Model }) {
			return true
		}
	}
	return false
}

// StepFailure is a step that cannot serve its route's classes: the connection is off, the model is not on its
// allow-list, or the policy does not let a class go there (AI-75, AI-72).
func StepFailure(i int, s Step, enabled, allowed bool, policy error) error {
	field := fmt.Sprintf("steps[%d]", i)
	switch {
	case !enabled:
		return refuse(CodeStep, field+".connection_id", "is switched off or not a connection of the organisation", "Name a connection that is on")
	case !allowed:
		return refuse(CodeStep, field+".model", s.Model+" is not on the connection's allow-list", "Probe it and have it allowed first, or name another")
	case policy != nil:
		return refuse(CodeStep, field+".connection_id", "may not receive the route's classes: "+policy.Error(),
			"Route those classes to a connection the data-class policy allows, or narrow the route's classes")
	}
	return nil
}

// Unknown refuses an agent the release does not have.
func Unknown(agent string) error {
	return refuse(CodeAgent, "agent", agent+" is not an agent of this release", "Choose one of the agents GET /v1/ai/routes lists")
}
