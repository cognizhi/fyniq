// Package aipolicy decides which AI connections may receive which data (PRD-001 AI-75, AI-83, AI-88, CMP-PD-06;
// p1-ai-providers design D5). Each connection declares, per model, endpoint and feature, where prompts are processed
// (as distinct from where they are stored), how long they are kept, whether they train models, and through whom, each
// with a dated official source or a contract term; a probe never proves geography. A policy maps the data classes to
// the connections allowed. By default personal data and payroll go only where Singapore processing is evidenced, which a
// self-hosted model in the organisation's own Singapore network evidences by its own declaration; restricted data goes
// nowhere; know-how and business figures go to any enabled connection the policy does not exclude. An Owner may widen a
// class with a second approver, but personal data and payroll leave Singapore only on a connection whose transfer is
// assessed and recorded (CMP-PD-06). Unclear or conflicting evidence never counts as Singapore.
package aipolicy

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
)

// The data classes, as the connector manifests name them (AI-75).
const (
	KnowHow    = "know-how"
	Business   = "business-figures"
	Personal   = "personal-data"
	Payroll    = "payroll"
	Restricted = "restricted"
)

// Classes are every class, from the least to the most sensitive.
var Classes = []string{KnowHow, Business, Personal, Payroll, Restricted}

// Sensitive are the classes that stay in Singapore unless a transfer is assessed (AI-75's default, CMP-PD-06).
var Sensitive = []string{Personal, Payroll}

// The stable codes of the policy's refusals (ENG-23).
const (
	CodeDeclaration = "AI-DATA-01" // a data-handling entry is not complete or not evidenced
	CodePolicy      = "AI-DATA-02" // the policy names a class or connection it may not
	CodeNotAllowed  = "AI-DATA-03" // the connection may not receive a class the call carries
)

// Evidence is where a data-handling statement comes from: the provider's official page, read on a day, or a contract
// term in an evidence file (FR-DOC-05).
type Evidence struct {
	Kind      string `json:"kind"`                  // source or contract
	URL       string `json:"url,omitempty"`         // the official page, for a source
	Title     string `json:"title"`                 // what it is: the page's title, or the contract and its clause
	CheckedOn string `json:"checked_on"`            // the day it was read, YYYY-MM-DD
	File      string `json:"evidence_id,omitempty"` // the contract's evidence file
}

// Handling is a connection's data handling for one model, endpoint and feature ("*" for every model or feature).
type Handling struct {
	Model         string   `json:"model"`
	Endpoint      string   `json:"endpoint"`
	Feature       string   `json:"feature"`
	ProcessedIn   []string `json:"processed_in"`             // where prompts are processed: ISO 3166 codes
	StoredIn      []string `json:"stored_in"`                // where they are kept at rest
	RetentionDays *int     `json:"retention_days,omitempty"` // how long they are kept; 0 is zero data retention; none: unknown
	Training      string   `json:"training"`                 // no, yes or unknown
	SubProcessors []string `json:"sub_processors,omitempty"`
	Gateways      []string `json:"gateways,omitempty"` // any gateway in between, such as a LiteLLM proxy, with its own handling
	Unclear       bool     `json:"unclear,omitempty"`  // the evidence is unclear or conflicting (AI-88)
	Evidence      Evidence `json:"evidence"`
}

// Declaration is a connection's data handling: every model, endpoint and feature it is used for, and the transfer
// assessment that covers it when it processes outside Singapore (CMP-PD-06).
type Declaration struct {
	Handling  []Handling `json:"handling"`
	Inventory string     `json:"inventory_id,omitempty"` // its row in the data inventory (SEC-13)
	Transfer  string     `json:"transfer_id,omitempty"`  // the recorded transfer, when it processes outside Singapore (CMP-PD-06)
}

// Overseas reports whether any of the declaration's entries processes outside Singapore, or is unclear about it: a
// transfer that needs its assessment recorded (CMP-PD-06).
func (d Declaration) Overseas() bool {
	for _, h := range d.Handling {
		if h.Unclear || slices.ContainsFunc(h.ProcessedIn, func(c string) bool { return c != "SG" }) {
			return true
		}
	}
	return false
}

// Error is a refusal, with the field it is about and how to fix it.
type Error struct {
	Code, Field, Message, Fix string
}

func (e *Error) Error() string { return e.Code + ": " + e.Field + " " + e.Message }

func refuse(code, field, message, fix string) error {
	return &Error{Code: code, Field: field, Message: message, Fix: fix}
}

var (
	country = regexp.MustCompile(`^[A-Z]{2}$`)
	name    = regexp.MustCompile(`^(\*|[A-Za-z0-9][A-Za-z0-9._:@/-]{0,127})$`)
)

// Check checks a declaration: each entry for a model and feature, on an endpoint, with where it is processed and
// stored, its retention, its training, and dated evidence; the same model, endpoint and feature once.
func Check(d Declaration, today time.Time) error {
	seen := map[string]bool{}
	for i, h := range d.Handling {
		f := func(field string) string { return fmt.Sprintf("handling[%d].%s", i, field) }
		switch {
		case !name.MatchString(h.Model):
			return refuse(CodeDeclaration, f("model"), "is not a model ID or *", "Name the model, or * for every model")
		case h.Endpoint == "" || len(h.Endpoint) > 253:
			return refuse(CodeDeclaration, f("endpoint"), "is empty", "Name the endpoint's host")
		case !name.MatchString(h.Feature):
			return refuse(CodeDeclaration, f("feature"), "is not a feature or *", "Name the feature, such as messages or batch, or *")
		case len(h.ProcessedIn) == 0:
			return refuse(CodeDeclaration, f("processed_in"), "is empty", "Say where prompts are processed, as country codes")
		case h.Training != "no" && h.Training != "yes" && h.Training != "unknown":
			return refuse(CodeDeclaration, f("training"), "is not no, yes or unknown", "Say whether the provider trains on prompts")
		case h.RetentionDays != nil && (*h.RetentionDays < 0 || *h.RetentionDays > 3650):
			return refuse(CodeDeclaration, f("retention_days"), "is outside 0 to 3,650", "Give the days prompts are kept, 0 for zero retention")
		}
		for _, list := range [][]string{h.ProcessedIn, h.StoredIn} {
			for _, c := range list {
				if !country.MatchString(c) {
					return refuse(CodeDeclaration, f("processed_in"), fmt.Sprintf("names %q, which is not a country code", c), "Use ISO 3166 codes, such as SG")
				}
			}
		}
		if err := checkEvidence(h.Evidence, f("evidence"), today); err != nil {
			return err
		}
		key := h.Model + "|" + h.Endpoint + "|" + h.Feature
		if seen[key] {
			return refuse(CodeDeclaration, f("model"), "repeats a model, endpoint and feature", "Declare each once")
		}
		seen[key] = true
	}
	return nil
}

func checkEvidence(e Evidence, field string, today time.Time) error {
	day, err := time.Parse(time.DateOnly, e.CheckedOn)
	switch {
	case strings.TrimSpace(e.Title) == "":
		return refuse(CodeDeclaration, field+".title", "is empty", "Name the page or the contract and its clause")
	case err != nil || day.After(today):
		return refuse(CodeDeclaration, field+".checked_on", "is not a day up to today", "Give the day the evidence was read")
	}
	switch e.Kind {
	case "source":
		u, err := url.Parse(e.URL)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			return refuse(CodeDeclaration, field+".url", "is not an https address", "Link the provider's official page")
		}
	case "contract":
		if e.File == "" {
			return refuse(CodeDeclaration, field+".evidence_id", "is empty: a contract term is kept as evidence", "Attach the contract as evidence (FR-DOC-05)")
		}
	default:
		return refuse(CodeDeclaration, field+".kind", "is not source or contract", "Evidence an official page or a contract term")
	}
	return nil
}

// covering is the declaration's entry for a model and feature: the most specific one.
func (d Declaration) covering(model, feature string) (Handling, bool) {
	best, rank := Handling{}, -1
	for _, h := range d.Handling {
		if (h.Model != "*" && h.Model != model) || (h.Feature != "*" && h.Feature != feature) {
			continue
		}
		r := 0
		if h.Model != "*" {
			r += 2
		}
		if h.Feature != "*" {
			r++
		}
		if r > rank {
			best, rank = h, r
		}
	}
	return best, rank >= 0
}

// Singapore reports whether the declaration evidences that a model's feature is processed in Singapore only, with
// clear evidence, and every gateway declared (AI-88).
func (d Declaration) Singapore(model, feature string) bool {
	h, ok := d.covering(model, feature)
	return ok && !h.Unclear && len(h.ProcessedIn) == 1 && h.ProcessedIn[0] == "SG"
}

// Policy maps each class to the connections the Owner allowed it on, beyond the defaults; Exclude takes connections
// off a class's default.
type Policy struct {
	Allow   map[string][]string `json:"allow,omitempty"`
	Exclude map[string][]string `json:"exclude,omitempty"`
}

// Connection is what the policy needs of a connection.
type Connection struct {
	ID          string
	Enabled     bool
	Declaration Declaration
}

// CheckPolicy checks a policy: known classes, restricted allowed nowhere, and personal data and payroll widened only
// to a connection whose transfer is recorded (CMP-PD-06).
func CheckPolicy(p Policy, connections map[string]Connection) error {
	for class, ids := range p.Allow {
		if !slices.Contains(Classes, class) {
			return refuse(CodePolicy, "allow."+class, "is not a data class", "Use know-how, business-figures, personal-data or payroll")
		}
		if class == Restricted && len(ids) > 0 {
			return refuse(CodePolicy, "allow."+class, "allows restricted data to an AI provider, which never receives it",
				"Leave restricted data out of every connection")
		}
		for _, id := range ids {
			c, ok := connections[id]
			if !ok {
				return refuse(CodePolicy, "allow."+class, "names "+id+", which is not a connection of the organisation", "Name its connections")
			}
			if slices.Contains(Sensitive, class) && c.Declaration.Transfer == "" {
				return refuse(CodePolicy, "allow."+class, "sends "+class+" to a connection whose transfer is not assessed (CMP-PD-06)",
					"Record the transfer assessment in the Compliance Center and name it in the connection's data handling first")
			}
		}
	}
	for class := range p.Exclude {
		if !slices.Contains(Classes, class) {
			return refuse(CodePolicy, "exclude."+class, "is not a data class", "Use a data class")
		}
	}
	return nil
}

// Allowed decides whether a call carrying classes may go to a connection's model and feature (AI-75, AI-83): every
// class, by the policy and its defaults. It names the first class that may not go and why.
func Allowed(p Policy, c Connection, model, feature string, classes []string) error {
	if !c.Enabled {
		return refuse(CodeNotAllowed, "connection", "is switched off", "Switch it on, or route to another")
	}
	for _, class := range classes {
		if slices.Contains(p.Exclude[class], c.ID) {
			return refuse(CodeNotAllowed, "classes", class+" is excluded from this connection by the policy", "Route it to another connection")
		}
		if slices.Contains(p.Allow[class], c.ID) {
			continue
		}
		switch class {
		case KnowHow, Business:
		case Personal, Payroll:
			if !c.Declaration.Singapore(model, feature) {
				return refuse(CodeNotAllowed, "classes", class+" goes only where Singapore processing is evidenced (AI-75, AI-88)",
					"Route it to a Singapore connection, or ask the Owner to allow it here with the transfer assessed")
			}
		default:
			return refuse(CodeNotAllowed, "classes", class+" never goes to an AI provider", "Leave it out of the call")
		}
	}
	return nil
}
