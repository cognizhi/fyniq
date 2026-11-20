// Package assistant is the rules of Nemura's people assistant (PRD-002 §18, Appendix D): the never-list for people work as words, the catalogue of task types it may propose with their strict
// input shapes, tiers, approver roles and expiry, and the refusals in plain words. It holds no model and no database: the runtime reads through tools as the asking person and inserts proposals,
// a named human decides, and the executor applies (NM-AI-04, NM-ARC-06). Nemura imports nothing of Fyniq's assistant (ADR-N1), so the rules exist here in their own words and a test names every
// sentence of NM-AI-04.
package assistant

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode"
)

// Codes of the assistant's refusals.
const (
	CodeNever    = "NM_AI_NEVER"
	CodeInvalid  = "NM_AI_INVALID"
	CodeUnknown  = CodeInvalid
	CodeOff      = "NM_AI_OFF"
	CodeConflict = "NM_AI_CONFLICT"
	CodeRole     = "NM_ROLE_REQUIRED"
	CodeNotFound = "NM_NOT_FOUND"
)

// Refusal is an answer in plain words with a stable code.
type Refusal struct{ Code, Message string }

func (r *Refusal) Error() string { return r.Code + ": " + r.Message }

func invalid(format string, a ...any) error {
	return &Refusal{Code: CodeInvalid, Message: fmt.Sprintf(format, a...)}
}

// never is the never-list for people work (NM-AI-04): words, or runs of words, that a task type's ID or command may not hold, with what they name.
var never = []struct {
	words []string
	what  string
}{
	{[]string{"approve", "approval", "approvals", "reject", "refuse", "decline", "decide", "decision", "decisions"}, "approving or refusing leave, requests or anything else"},
	{[]string{"issue", "issuing", "sign", "signature", "signing"}, "issuing or signing a letter"},
	{[]string{"pay", "payroll", "payment", "payments", "salary", "wage", "wages", "bonus", "payslip", "payslips"}, "changing pay or paying anyone"},
	{[]string{"bank", "account", "accounts", "payee", "payees"}, "changing a bank account"},
	{[]string{"role", "roles", "user", "users", "member", "members", "permission", "permissions", "setting", "settings", "policy", "policies", "catalogue", "feature", "features"},
		"changing a role, a user, a setting, a policy or the assistant's own catalogue"},
	{[]string{"record", "records", "update", "edit", "change", "set", "delete", "remove", "purge", "terminate", "termination", "dismiss", "dismissal", "resign", "resignation"},
		"changing, deleting or ending a record"},
	{[]string{"candidate", "candidates", "hire", "hiring", "promote", "promotion", "rating", "rate", "rank", "score", "screen", "offer"}, "moving a candidate, or deciding a hiring, a rating, a promotion or a termination"},
	{[]string{"statutory", "pack", "packs", "parameter", "parameters"}, "changing statutory parameters"},
	{[]string{"export", "audit", "file", "lodge", "submit"}, "exporting data, filing with an authority or touching the audit trail"},
	{[]string{"nric", "fin", "medical", "restricted"}, "reading another person's restricted data"},
}

// words splits an identifier into lower-case words: on anything that is not a letter or a digit and on a lower-to-upper camel-case boundary.
func words(s string) []string {
	var out []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			out = append(out, strings.ToLower(string(cur)))
			cur = nil
		}
	}
	var prev rune
	for _, r := range s {
		switch {
		case !unicode.IsLetter(r) && !unicode.IsDigit(r):
			flush()
		case unicode.IsUpper(r) && unicode.IsLower(prev):
			flush()
			cur = append(cur, r)
		default:
			cur = append(cur, r)
		}
		prev = r
	}
	flush()
	return out
}

// Refuse says what on the never-list a task type's ID or command would do, or "" when it does none of it.
func Refuse(identifier string) string {
	ws := words(identifier)
	for _, n := range never {
		for _, w := range n.words {
			if slices.Contains(ws, w) {
				return n.what
			}
		}
	}
	return ""
}

// NeverWords lists every word of the never-list, for the tests that name each sentence of NM-AI-04.
func NeverWords() []string {
	var out []string
	for _, n := range never {
		out = append(out, n.words...)
	}
	sort.Strings(out)
	return out
}

// Kinds of a field of a task's input.
const (
	UUID   = "uuid"
	Text   = "text"
	Map    = "map"
	EnumOf = "enum"
)

// Field is one field of a task's strict input shape: an unknown field, a missing required field, a value of the wrong kind or beyond its limit is refused.
type Field struct {
	Name     string
	Kind     string
	Required bool
	Max      int      // a text's most characters; a map's most entries
	ValueMax int      // a map's longest value
	Values   []string // an enum's values
}

// Task is a task type of the catalogue (Appendix D): what it proposes, its tier, who approves it and how long a proposal waits.
type Task struct {
	ID            string
	Version       int
	Tier          int
	What          string
	ApproverRoles []string // the roles that may decide a proposal of it
	ExpiryHours   int
	Fields        []Field
}

// Expiry is how long a proposal waits for a human (72 hours, Appendix D).
const Expiry = 72

// Catalogue is what the people assistant may propose: two drafts for requests (P1) and, from P2, a draft of a job post and a draft of an interview kit for a requisition (rev 0.76), each for a human to edit
// and approve. The review summary is P2 too (NM-PRF-04). The hiring drafts are asked for and decided by the Recruiter as well as the people team (the HR officer, the HR administrator and the Owner), the Recruiter through a list of
// these two task types alone (rev 0.77): no wider than Hiring: create, which already writes the post and the kit [owner to confirm].
var Catalogue = []Task{
	{ID: "people.draft_letter", Version: 1, Tier: 1, ApproverRoles: []string{"hr_officer", "hr_admin", "owner"}, ExpiryHours: Expiry,
		What:   "A letter from a template for a request, with the fields filled from the record",
		Fields: []Field{{Name: "request_id", Kind: UUID, Required: true}, {Name: "fields", Kind: Map, Required: true, Max: 30, ValueMax: 500}, {Name: "rationale", Kind: Text, Max: 500}}},
	{ID: "people.draft_fwa_reply", Version: 1, Tier: 1, ApproverRoles: []string{"hr_officer", "hr_admin", "owner"}, ExpiryHours: Expiry,
		What:   "A written reply to a flexible-work request, with the reason for a refusal when the decision-maker has refused",
		Fields: []Field{{Name: "request_id", Kind: UUID, Required: true}, {Name: "reply", Kind: Text, Required: true, Max: 4000}, {Name: "reason", Kind: Text, Max: 1000}, {Name: "rationale", Kind: Text, Max: 500}}},
	{ID: "people.draft_job_post", Version: 1, Tier: 1, ApproverRoles: []string{"recruiter", "hr_officer", "hr_admin", "owner"}, ExpiryHours: Expiry,
		What:   "A job post for a requisition, written from the role and the team alone, with fair-wording flags",
		Fields: []Field{{Name: "requisition_id", Kind: UUID, Required: true}, {Name: "title", Kind: Text, Required: true, Max: 120}, {Name: "body", Kind: Text, Required: true, Max: 8000}, {Name: "rationale", Kind: Text, Max: 500}}},
	{ID: "people.draft_interview_kit", Version: 1, Tier: 1, ApproverRoles: []string{"recruiter", "hr_officer", "hr_admin", "owner"}, ExpiryHours: Expiry,
		What:   "Interview questions and a scorecard for a role, one on each line, with fair-wording flags",
		Fields: []Field{{Name: "requisition_id", Kind: UUID, Required: true}, {Name: "questions", Kind: Text, Required: true, Max: 6000}, {Name: "criteria", Kind: Text, Required: true, Max: 1500}, {Name: "rationale", Kind: Text, Max: 500}}},
}

// IsHiringDraft says whether a task type is one of the two hiring drafts, the only ones the Recruiter asks for, sees and decides (NM-AI-05).
func IsHiringDraft(id string) bool {
	return id == "people.draft_job_post" || id == "people.draft_interview_kit"
}

// HiringDrafts are the task types of the hiring drafts.
var HiringDrafts = []string{"people.draft_job_post", "people.draft_interview_kit"}

// Lookup returns a task type by ID.
func Lookup(id string) (Task, bool) {
	for _, t := range Catalogue {
		if t.ID == id {
			return t, true
		}
	}
	return Task{}, false
}

// CheckCatalogue refuses a catalogue that holds a task type the never-list names, with the sentence: it runs as the catalogue loads, and Propose runs Refuse again for each proposal, so adding a word to
// the catalogue proposes nothing.
func CheckCatalogue(c []Task) error {
	for _, t := range c {
		if what := Refuse(t.ID); what != "" {
			return &Refusal{Code: CodeNever, Message: fmt.Sprintf("%s is refused: the assistant never does anything that means %s.", t.ID, what)}
		}
		for _, f := range t.Fields {
			if what := Refuse(f.Name); what != "" {
				return &Refusal{Code: CodeNever, Message: fmt.Sprintf("%s.%s is refused: the assistant never does anything that means %s.", t.ID, f.Name, what)}
			}
		}
		if t.Tier != 1 || t.ExpiryHours != Expiry || len(t.ApproverRoles) == 0 {
			return invalid("%s must be tier 1, expire in %d hours and name its approver roles.", t.ID, Expiry)
		}
	}
	return nil
}

// Check validates a proposal's payload against its task's strict shape and returns it normalised to the shape's fields. It names the field and says what is wrong.
func Check(t Task, payload map[string]any) error {
	known := map[string]Field{}
	for _, f := range t.Fields {
		known[f.Name] = f
	}
	for k := range payload {
		if _, ok := known[k]; !ok {
			return invalid("%s: %q is not a field of %s.", k, k, t.ID)
		}
	}
	for _, f := range t.Fields {
		v, present := payload[f.Name]
		if !present || v == nil {
			if f.Required {
				return invalid("%s: %s is required.", f.Name, f.Name)
			}
			continue
		}
		switch f.Kind {
		case UUID:
			s, ok := v.(string)
			if !ok || len(s) != 36 || strings.Count(s, "-") != 4 {
				return invalid("%s: write the ID of a record from this conversation.", f.Name)
			}
		case Text:
			s, ok := v.(string)
			if !ok || (f.Required && strings.TrimSpace(s) == "") || len([]rune(s)) > f.Max {
				return invalid("%s: text of up to %d characters.", f.Name, f.Max)
			}
		case EnumOf:
			s, ok := v.(string)
			if !ok || !slices.Contains(f.Values, s) {
				return invalid("%s: one of %s.", f.Name, strings.Join(f.Values, ", "))
			}
		case Map:
			m, ok := v.(map[string]any)
			if !ok || len(m) > f.Max || (f.Required && len(m) == 0) {
				return invalid("%s: up to %d named values.", f.Name, f.Max)
			}
			for k, e := range m {
				s, ok := e.(string)
				if !ok || len([]rune(s)) > f.ValueMax || len([]rune(k)) > 60 {
					return invalid("%s.%s: text of up to %d characters.", f.Name, k, f.ValueMax)
				}
			}
		}
	}
	return nil
}

// MayDecide says whether a holder of these roles may decide a proposal of the task.
func MayDecide(t Task, roles []string) bool {
	for _, r := range roles {
		if slices.Contains(t.ApproverRoles, r) {
			return true
		}
	}
	return false
}
