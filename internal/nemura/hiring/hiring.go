// Package hiring holds the rules of Nemura's hiring (PRD-002 NM-REC-01 to NM-REC-03): what a requisition must say, the check of a job post's wording against the Tripartite Guidelines on Fair
// Employment Practices, and the Fair Consideration Framework's advertising period before an offer. It is pure: no storage, no clock, no network.
package hiring

import (
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"time"
)

// Codes of a refusal; each is in the API's catalogue.
const (
	CodeInvalid  = "NM_HIRING_INVALID"
	CodeConflict = "NM_HIRING_CONFLICT"
	CodeNotFound = "NM_NOT_FOUND"
	CodeWording  = "NM_HIRING_WORDING"
)

// Refusal is a plain refusal with a stable code.
type Refusal struct{ Code, Message string }

func (r *Refusal) Error() string { return r.Code + ": " + r.Message }

func invalid(format string, a ...any) error { return &Refusal{CodeInvalid, fmt.Sprintf(format, a...)} }

// Requisition is what a requisition asks for.
type Requisition struct {
	Title, Department, Reason string // reason: new or replacement
	ReplacesPerson            bool   // a replacement names the person it replaces
	SalaryMin, SalaryMax      string // decimal strings in SGD, never a float
	TargetStart               time.Time
}

var money = regexp.MustCompile(`^\d{1,9}(\.\d{1,2})?$`)

// Check refuses a requisition that is not whole, naming the field.
func Check(r Requisition) error {
	if n := len([]rune(strings.TrimSpace(r.Title))); n < 1 || n > 120 {
		return invalid("The role's title is 1 to 120 characters, such as Data scientist.")
	}
	if n := len([]rune(strings.TrimSpace(r.Department))); n < 1 || n > 120 {
		return invalid("The team is 1 to 120 characters, such as Engineering.")
	}
	switch r.Reason {
	case "new":
		if r.ReplacesPerson {
			return invalid("A new role replaces no one: choose the reason replacement, or leave the person out.")
		}
	case "replacement":
		if !r.ReplacesPerson {
			return invalid("A replacement names the person it replaces: choose them from the directory.")
		}
	default:
		return invalid("The reason is new or replacement.")
	}
	if !money.MatchString(r.SalaryMin) || !money.MatchString(r.SalaryMax) {
		return invalid("The salary range is two amounts in Singapore dollars with at most two decimals, such as 5500.00.")
	}
	lo, _ := new(big.Rat).SetString(r.SalaryMin)
	hi, _ := new(big.Rat).SetString(r.SalaryMax)
	if lo.Cmp(hi) > 0 {
		return invalid("The top of the salary range is below its bottom: check both amounts.")
	}
	if r.TargetStart.IsZero() {
		return invalid("The target start date is required.")
	}
	return nil
}

// Finding is a criterion in a text that the guidelines ask an employer not to use unless it is a genuine occupational requirement.
type Finding struct {
	Category string // age, nationality, sex, marital_status, pregnancy, caregiving, race, religion, language, disability or mental_health
	Phrase   string // what was found, as written
}

// Categories are the criteria NM-REC-03 names, in the order the page lists them.
var Categories = []string{"age", "nationality", "sex", "marital_status", "pregnancy", "caregiving", "race", "religion", "language", "disability", "mental_health"}

// The phrases that suggest a protected criterion. The list is a starting point built from the criteria NM-REC-03 names, not the guidelines' own wording: a phrase flagged is a prompt for a person to
// decide, and a criterion missed here is still the author's to avoid [verify the list against the Tripartite Guidelines on Fair Employment Practices before relying on it].
var phrases = map[string][]string{
	"age":            {`young`, `youthful`, `recent graduates?`, `fresh graduates?`, `digital natives?`, `energetic youngsters?`, `mature`, `aged? (?:below|under|above|over|between)? ?\d{2}`, `\d{2} ?(?:to|-) ?\d{2} years old`, `under \d{2}`, `years old`},
	"nationality":    {`singaporeans? only`, `citizens? only`, `locals? only`, `prs? only`, `singapore citizens?`, `no foreigners?`, `foreigners? (?:need not|do not|don't) apply`, `malaysians? only`, `nationality`},
	"sex":            {`males?`, `females?`, `salesmen`, `salesman`, `saleswomen`, `saleswoman`, `waitress`, `handsome`, `gentlemen`, `ladies`, `lady`, `men only`, `women only`},
	"marital_status": {`married`, `unmarried`, `single`, `bachelor`, `marital status`},
	"pregnancy":      {`pregnan\w*`, `no plans to (?:have|start) (?:children|a family)`, `plans? to start a family`},
	"caregiving":     {`no family (?:commitments|responsibilit\w*)`, `no caregiving`, `no (?:young )?children`, `without family commitments`},
	"race":           {`chinese only`, `malay only`, `indian only`, `of chinese descent`, `ethnic\w*`, `race`, `racial`},
	"religion":       {`christians?`, `muslims?`, `buddhists?`, `hindus?`, `religio\w*`, `church-going`},
	"language":       {`native english`, `native speakers?`, `mother tongue`, `mandarin[- ]speaking only`},
	"disability":     {`able-bodied`, `healthy individuals?`, `no disabilit\w*`, `without any disabilit\w*`, `physically fit`},
	"mental_health":  {`mentally sound`, `no history of mental\w*`, `no mental (?:illness|health)\w*`, `psychologically stable`},
}

var compiled = func() map[string][]*regexp.Regexp {
	out := map[string][]*regexp.Regexp{}
	for cat, ps := range phrases {
		for _, p := range ps {
			out[cat] = append(out[cat], regexp.MustCompile(`(?i)\b`+p+`\b`))
		}
	}
	return out
}()

// FairWording lists the criteria found in a job post or an interview question, each once, in the order of Categories. It decides nothing: the author keeps a flagged criterion only by recording
// the genuine occupational requirement for it (NM-REC-03), and the assistant never screens or ranks anyone (NM-AI-05).
func FairWording(text string) []Finding {
	var out []Finding
	for _, cat := range Categories {
		for _, re := range compiled[cat] {
			if m := re.FindString(text); m != "" {
				out = append(out, Finding{Category: cat, Phrase: m})
				break
			}
		}
	}
	return out
}

// Exemptions from the advert, with what each means in plain words (MOM, Fair Consideration Framework; PRD-002 NM-REC-02).
var Exemptions = map[string]string{
	"fewer_than_10_employees": "The company has fewer than 10 employees",
	"ep_salary_22500":         "An Employment Pass with a fixed monthly salary of S$22,500 or more",
	"role_one_month_or_less":  "A role of 1 month or less",
	"transferee":              "A local or intra-corporate transferee",
	"s_pass_exempt":           "An exemption for an S Pass recorded with its evidence",
}

// AdvertDays is the Fair Consideration Framework's minimum: the advert runs at least 14 consecutive days and no offer is made during them (NM-REC-02).
const AdvertDays = 14

// OfferFrom is the first day an offer may be made for a role that needs a pass: the day after the advert's 14 consecutive days. ok is false when no offer may be made yet because neither the advert's
// date nor an exemption is recorded; a role that needs no pass has no rule and may be offered at once.
func OfferFrom(passNeeded bool, advertPosted *time.Time, exemption string) (from time.Time, ok bool) {
	if !passNeeded || exemption != "" {
		return time.Time{}, true
	}
	if advertPosted == nil {
		return time.Time{}, false
	}
	return advertPosted.AddDate(0, 0, AdvertDays), true
}

// CheckExemption refuses an exemption that is not one of the known kinds or has no note (the evidence is a note and, later, a document).
func CheckExemption(kind, note string) error {
	if _, ok := Exemptions[kind]; !ok {
		return invalid("Choose one of the exemptions: fewer_than_10_employees, ep_salary_22500, role_one_month_or_less, transferee or s_pass_exempt.")
	}
	if n := len([]rune(strings.TrimSpace(note))); n < 1 || n > 500 {
		return invalid("Say what the evidence is, in up to 500 characters: it is the record that the exemption applies.")
	}
	return nil
}

// CheckPost refuses a job post that is not whole.
func CheckPost(title, body string) error {
	if n := len([]rune(strings.TrimSpace(title))); n < 1 || n > 120 {
		return invalid("The post's title is 1 to 120 characters.")
	}
	if n := len([]rune(strings.TrimSpace(body))); n < 1 || n > 8000 {
		return invalid("The post's text is 1 to 8000 characters.")
	}
	return nil
}
