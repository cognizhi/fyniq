package assistant

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/cognizhi/fyniq/internal/nemura/hiring"
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
	Title       string // a requisition's role: untrusted, written by a person
	Department  string // a requisition's team: untrusted
}

// FairFindings lists the criteria the Tripartite Guidelines ask employers not to use that a text names, as "category: phrase". The assistant's own words never reach a person with a finding in them (NM-AI-05).
func FairFindings(text string) []string {
	var out []string
	for _, f := range hiring.FairWording(text) {
		out = append(out, f.Category+": "+f.Phrase)
	}
	return out
}

// plain is a role's title or a team's name as it may appear in a draft: trimmed, short and with no criterion the guidelines name; a title that names one is replaced by a placeholder for the person to fill, so
// the assistant never repeats it.
func plain(s string, max int, placeholder string) (string, bool) {
	s = trim(s, max)
	if s == "" || len([]rune(s)) > 80 || !plainName.MatchString(s) || len(FairFindings(s)) > 0 {
		return placeholder, false
	}
	return s, true
}

// plainName is what a role's title or a team's name looks like: words, digits and the few marks a name carries. A title with a sentence's punctuation in it is a sentence, not a name, and is not repeated.
var plainName = regexp.MustCompile(`^[\p{L}\p{N} \-/&,()'.]+$`)

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
	payload, why, err := draft(task, f)
	if err != nil || (task != "people.draft_job_post" && task != "people.draft_interview_kit") {
		return payload, why, err
	}
	// The hiring drafts pass the fair-wording check as the assistant writes them: a draft that names a criterion the guidelines ask employers not to use is never proposed (NM-AI-05).
	var all []string
	for _, v := range payload {
		if text, ok := v.(string); ok {
			all = append(all, text)
		}
	}
	if found := FairFindings(strings.Join(all, "\n")); len(found) > 0 {
		return nil, "", &Refusal{Code: CodeConflict, Message: "The draft would name a criterion the Tripartite Guidelines ask employers not to use (" + strings.Join(found, "; ") + "): it is not proposed."}
	}
	return payload, why, nil
}

func draft(task string, f RequestFacts) (map[string]any, string, error) {
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
	case "people.draft_job_post":
		if f.Kind != "requisition" {
			return nil, "", invalid("requisition_id: a job post draft is for a requisition.")
		}
		title, okTitle := plain(f.Title, 120, "[A role title that describes the work]")
		team, okTeam := plain(f.Department, 120, "[the team]")
		body := fmt.Sprintf("We are looking for a %s to join %s.\n\nWhat you will do\n[The recruiter adds the duties of the role.]\n\nWhat we ask for\n[The recruiter adds the skills and experience the work needs, described by what the work needs and never by who the person is.]\n\nHow to apply\nApply on our careers page.", title, team)
		why := "Written from the role and the team alone: the draft holds no salary and nothing about a person. The duties and the requirements are for the recruiter to add, and the fair-wording check runs on what they write."
		if !okTitle || !okTeam {
			why = "The requisition's own words name a criterion the guidelines ask employers not to use, so the draft does not repeat them: a placeholder stands where they were. " + why
		}
		return map[string]any{"title": title, "body": body}, why, nil
	case "people.draft_interview_kit":
		if f.Kind != "requisition" {
			return nil, "", invalid("requisition_id: an interview kit draft is for a requisition.")
		}
		title, _ := plain(f.Title, 120, "the role")
		questions := []string{
			fmt.Sprintf("Tell us about a piece of work that shows how you would approach the %s role.", title),
			"Describe a time you had to explain something complicated to someone who was not an expert.",
			"Tell us about a problem you solved with limited information: what did you do first, and why?",
			"Describe a time you worked with others to finish something none of you could finish alone.",
			"What would you want to learn in your first three months, and how would you go about it?",
		}
		criteria := []string{"Skills the role needs", "Problem solving", "Communication", "Working with others", "Learning"}
		return map[string]any{"questions": strings.Join(questions, "\n"), "criteria": strings.Join(criteria, "\n")}, "Questions about the work and how it is done, each scored against the same criteria for every candidate. None asks about who the person is.", nil
	}
	return nil, "", &Refusal{Code: CodeUnknown, Message: task + " is not a task type of the catalogue."}
}
