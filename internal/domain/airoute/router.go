package airoute

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/cognizhi/fyniq/internal/domain/aipolicy"
	"github.com/cognizhi/fyniq/internal/domain/aiprice"
	"github.com/cognizhi/fyniq/internal/domain/aiqual"
	"github.com/cognizhi/fyniq/internal/ports"
)

// The codes of a run the router stops (ENG-23).
const (
	CodeNoRoute   = "AI-ROUTER-01" // the agent has no route, so it does not run
	CodeUnhandled = "AI-ROUTER-02" // the call carries a class of data its route does not handle
	CodeNoStep    = "AI-ROUTER-03" // no step left can take the call: nothing was sent where it may not go (AI-76, AI-83)
	CodeRefused   = "AI-ROUTER-04" // the provider refused the request as invalid, or a stream broke after it began
)

// Stop is a run the router stopped, in Fyniq's words.
func Stop(code, message, fix string) *Error {
	return refuse(code, "route", message, fix)
}

// The outcomes of an attempt, as the activity log keeps them.
const (
	Served   = "ok"
	Skipped  = "skipped"
	Busy     = "busy"
	TimedOut = "timeout"
	NotFound = "not_found"
	Refused  = "refused"
	Failed   = "failed"
	Stopped  = "stopped"
)

// Needs are what a call needs of a step's provider (AI-27, AI-71): an agent that needs one is never routed to a
// connection that has not proved it.
type Needs struct {
	Tools        bool
	StrictOutput bool
	Streaming    bool
	Images       bool
	PDF          bool
}

// NeedsOf are what a request needs: tools when it offers any, strict output when it asks for a schema, streaming when
// it is streamed, and image or PDF input when a message carries one.
func NeedsOf(r ports.ModelRequest, streaming bool) Needs {
	n := Needs{Tools: len(r.Tools) > 0, StrictOutput: r.Output != nil, Streaming: streaming}
	for _, m := range r.Messages {
		for _, p := range m.Parts {
			n.Images = n.Images || p.Kind == ports.PartImage
			n.PDF = n.PDF || p.Kind == ports.PartDocument
		}
	}
	return n
}

// Lacking is the first capability a provider lacks that the call needs, "" when it has them all.
func (n Needs) Lacking(c ports.ModelCapabilities) string {
	for _, x := range []struct {
		need, has bool
		name      string
	}{{n.Tools, c.Tools, "tool calling"}, {n.StrictOutput, c.StrictOutput, "strict structured output"}, {n.Streaming, c.Streaming, "streaming"},
		{n.Images, c.Images, "image input"}, {n.PDF, c.PDF, "PDF input"}} {
		if x.need && !x.has {
			return x.name
		}
	}
	return ""
}

// StepState is what the router knows of a step before calling it.
type StepState struct {
	Enabled    bool
	Allowed    bool   // on the connection's allow-list
	Callable   bool   // listed, not past its shutdown, not refused by the provider (AI-81)
	Alias      bool   // a moving alias, as the provider's list says (AI-87)
	Standing   string // the model's qualification for the call's agent on this connection (AI-78): aiqual's standings
	Priced     bool
	Billing    string
	SelfHosted bool
	Policy     error // the data-class policy's refusal of the call's classes there, if any (AI-75)
	Built      error // why the step's provider could not be reached
	Caps       ports.ModelCapabilities
}

// Why is why a step cannot take a call carrying classes and needing n, "" when it can: a step is tried only when it
// meets the call's data classes and capabilities, before the first attempt and every fallback (AI-76, AI-83).
func Why(classes []string, n Needs, s StepState, model string) string {
	switch {
	case !s.Enabled:
		return "the connection is switched off"
	case !s.Allowed:
		return model + " is not on the connection's allow-list"
	case !s.Callable:
		return model + " is no longer listed, is past its shutdown, or the provider refused it"
	case s.Standing != aiqual.Qualified && s.Standing != aiqual.OutOfDate:
		return model + " has not passed this agent's evals on this connection (AI-78)"
	case s.Policy != nil:
		var e *aipolicy.Error
		if errors.As(s.Policy, &e) {
			return "the data-class policy keeps the call from this connection: " + e.Message
		}
		return "the data-class policy keeps the call from this connection"
	case s.Alias && sensitive(classes):
		return model + " is a moving alias, which serves only know-how and business figures (AI-87)"
	case s.Standing == aiqual.OutOfDate && sensitive(classes):
		return model + "'s qualification is out of date: it carries no personal data or payroll until it is qualified again (AI-87)"
	}
	if err := aiprice.Serves(s.Billing, s.SelfHosted, s.Priced, model); err != nil {
		return model + " has no price in force on this usage-based connection (AI-82)"
	}
	if s.Built != nil {
		return "the connection could not be reached: " + s.Built.Error()
	}
	if lack := n.Lacking(s.Caps); lack != "" {
		return "the connection has not proved " + lack + ", which the call needs (AI-27)"
	}
	return ""
}

// sensitive is whether classes hold personal data or payroll.
func sensitive(classes []string) bool {
	return slices.ContainsFunc(classes, func(c string) bool { return slices.Contains(aipolicy.Sensitive, c) })
}

// Failover is what an attempt's error means for the run (AI-76): its outcome, and whether the next eligible step may
// take the call. A provider's refusal of the request is the request's fault, so no other is asked; a stream that broke
// after its first event cannot be taken back, so it stops; the caller's own cancellation stops the run.
func Failover(err error, delivered bool) (outcome string, next bool) {
	switch {
	case errors.Is(err, context.Canceled):
		return Stopped, false
	case delivered:
		return Failed, false
	case errors.Is(err, ports.ErrModelRequest):
		return Refused, false
	case errors.Is(err, context.DeadlineExceeded):
		return TimedOut, true
	case errors.Is(err, ports.ErrModelBusy):
		return Busy, true
	case errors.Is(err, ports.ErrModelNotFound):
		return NotFound, true
	}
	return Failed, true
}

// Unhandled refuses a call that carries a class its route does not handle.
func Unhandled(classes, handled []string) error {
	var extra []string
	for _, c := range classes {
		if !slices.Contains(handled, c) {
			extra = append(extra, c)
		}
	}
	if len(extra) == 0 {
		return nil
	}
	return Stop(CodeUnhandled, "the call carries "+strings.Join(extra, ", ")+", which the agent's route does not handle",
		"Ask the Owner to widen the route to those classes, with a second approver, or leave that data out")
}
