package hiring

import (
	"regexp"
	"strings"
)

// The pipeline (PRD-002 NM-REC-04, NM-REC-05): the stages a candidate moves through, what a candidate and an interview kit must say, and how long an unsuccessful candidate's data is kept.

// CandidatesRecordType is the candidates' record type in the retention report and the legal holds (NM-SEC-05).
const CandidatesRecordType = "candidates"

// CandidateRetentionMonths is how long the data of a candidate who is not progressing is kept, from the day of the decision, unless they consented to be kept for future roles (NM-REC-05; NM-SEC-05's
// default is 6 months).
const CandidateRetentionMonths = 6

// Stages are the board's columns, in order; not_progressing is the last and may follow any of the first four.
var Stages = []string{"applied", "screening", "interview", "offer", "hired", "not_progressing"}

var moves = map[string][]string{
	"applied":         {"screening", "interview", "not_progressing"},
	"screening":       {"interview", "not_progressing"},
	"interview":       {"offer", "not_progressing"},
	"offer":           {"hired", "not_progressing", "interview"},
	"not_progressing": {"screening"},
}

// CheckMove refuses a move the board does not allow, saying where the candidate may go next. A move to not progressing needs the reason the decision was made for, which the candidate may ask for.
func CheckMove(from, to, reason string) error {
	ok := false
	for _, n := range moves[from] {
		ok = ok || n == to
	}
	if !ok {
		next := strings.Join(moves[from], ", ")
		if next == "" {
			return &Refusal{CodeConflict, "A hired candidate is final: the person record carries on."}
		}
		return &Refusal{CodeConflict, "A candidate at " + from + " moves to " + next + ", not to " + to + "."}
	}
	if to == "not_progressing" {
		if n := len([]rune(strings.TrimSpace(reason))); n < 3 || n > 500 {
			return invalid("Say why the candidate is not progressing, in 3 to 500 characters: it is the decision's reason.")
		}
	} else if len([]rune(reason)) > 500 {
		return invalid("The reason is at most 500 characters.")
	}
	return nil
}

var email = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

// CheckCandidate refuses a candidate that is not whole, naming the field.
func CheckCandidate(name, address, cv string) error {
	if n := len([]rune(strings.TrimSpace(name))); n < 1 || n > 120 {
		return invalid("The candidate's name is 1 to 120 characters.")
	}
	if len(address) > 200 || !email.MatchString(strings.TrimSpace(address)) {
		return invalid("The candidate's email address is not valid: write it as name@example.com.")
	}
	if len([]rune(cv)) > 20000 {
		return invalid("The CV text is at most 20000 characters.")
	}
	return nil
}

// CheckNote refuses an empty or very long note.
func CheckNote(body string) error {
	if n := len([]rune(strings.TrimSpace(body))); n < 1 || n > 4000 {
		return invalid("A note is 1 to 4000 characters.")
	}
	return nil
}

// Kit is an interview kit: the questions and the scorecard's criteria.
type Kit struct {
	Questions, Criteria []string
	RequirementNote     string
}

// CheckKit refuses a kit that is not whole and, as for a job post, one whose questions name a criterion the guidelines ask an employer not to use unless the genuine occupational requirement is
// recorded (NM-REC-03). It returns the findings either way, so the author sees what was found; it decides nothing but the saving.
func CheckKit(k Kit) ([]Finding, error) {
	if len(k.Questions) < 1 || len(k.Questions) > 30 || len(k.Criteria) < 1 || len(k.Criteria) > 10 {
		return nil, invalid("A kit has 1 to 30 questions and 1 to 10 scorecard criteria.")
	}
	for _, q := range append(append([]string{}, k.Questions...), k.Criteria...) {
		if n := len([]rune(strings.TrimSpace(q))); n < 1 || n > 500 {
			return nil, invalid("Each question and criterion is 1 to 500 characters.")
		}
	}
	found := FairWording(strings.Join(k.Questions, "\n") + "\n" + strings.Join(k.Criteria, "\n"))
	if len(found) > 0 && len([]rune(strings.TrimSpace(k.RequirementNote))) < 10 {
		return found, &Refusal{CodeWording, "A question or criterion names a criterion the guidelines ask employers not to use: reword it, or record the genuine occupational requirement (at least 10 characters)."}
	}
	return found, nil
}

// CheckScore refuses a score outside 1 to 5 and a comment that is too long.
func CheckScore(score int, comment string) error {
	if score < 1 || score > 5 {
		return invalid("A score is a whole number from 1 to 5.")
	}
	if len([]rune(comment)) > 1000 {
		return invalid("The comment is at most 1000 characters.")
	}
	return nil
}
