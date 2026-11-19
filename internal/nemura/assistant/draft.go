package assistant

import (
	"fmt"
	"sort"
	"strings"
)

// Drafts (NM-AI-02, Appendix D): what the assistant writes for a request, for a person to edit and approve. The request's own words (a letter's purpose, a flexible-work reason) are untrusted data: they are
// quoted, trimmed and never read as an instruction, and a draft never states a decision (it never approves, refuses, issues or signs anything).

// RequestFacts are what the runtime read of a request, as the person who asked for the draft.
type RequestFacts struct {
	Kind        string            // letter or flex
	Person      string            // the legal name of the person the request is for
	Purpose     string            // a letter's purpose: untrusted
	Data        map[string]string // the values the letter request was filled with from the record
	Arrangement string            // a flexible-work request's arrangement
	From, To    string
	Reason      string // a flexible-work request's reason: untrusted
}

func trim(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// Draft writes the proposal's payload and its rationale for a task and a request. The scripted writer fills a letter's fields from the values the request already holds from the record and quotes its
// purpose, and writes a flexible-work reply that acknowledges the request and leaves the decision and, for a refusal, its business reason to the person who decides.
func Draft(task string, f RequestFacts) (map[string]any, string, error) {
	switch task {
	case "people.draft_letter":
		if f.Kind != "letter" {
			return nil, "", invalid("request_id: a letter draft is for a letter request.")
		}
		fields := map[string]any{}
		keys := make([]string, 0, len(f.Data))
		for k := range f.Data {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if len(fields) >= 29 {
				break
			}
			fields[trim(k, 60)] = trim(f.Data[k], 500)
		}
		fields["purpose"] = trim(f.Purpose, 300)
		return map[string]any{"fields": fields}, "The fields are the values the request holds from the person's record; the purpose is quoted from the request, which is not an instruction.", nil
	case "people.draft_fwa_reply":
		if f.Kind != "flex" {
			return nil, "", invalid("request_id: a reply draft is for a flexible-work request.")
		}
		span := f.From
		if f.To != "" {
			span += " to " + f.To
		}
		reply := fmt.Sprintf("Dear %s,\n\nThank you for your request for a flexible work arrangement (%s) for %s. We have read it and the person who decides it will reply to you in writing. "+
			"[The decision-maker adds the decision here and, if the request is not agreed, the business reason.]\n\nBest regards,\nThe people team", trim(f.Person, 80), strings.ReplaceAll(f.Arrangement, "_", " "), span)
		return map[string]any{"reply": reply}, "A courteous acknowledgement. It states no decision and no reason: those are for the person who decides.", nil
	}
	return nil, "", &Refusal{Code: CodeUnknown, Message: task + " is not a task type of the catalogue."}
}
