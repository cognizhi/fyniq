package hiring_test

import (
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/nemura/hiring"
)

func d(s string) time.Time { t, _ := time.Parse(time.DateOnly, s); return t }

// A requisition names a role, the team, why, a salary range in dollars that does not run backwards and a start date (NM-REC-01).
// req: NM-REC-01
func TestARequisitionIsWholeBeforeItIsAsked(t *testing.T) {
	ok := hiring.Requisition{Title: "Data scientist", Department: "Engineering", Reason: "replacement", ReplacesPerson: true, SalaryMin: "6500.00", SalaryMax: "8200", TargetStart: d("2027-02-01")}
	if err := hiring.Check(ok); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*hiring.Requisition){
		"no title":            func(r *hiring.Requisition) { r.Title = " " },
		"no team":             func(r *hiring.Requisition) { r.Department = "" },
		"unknown reason":      func(r *hiring.Requisition) { r.Reason = "growth" },
		"replacement of none": func(r *hiring.Requisition) { r.ReplacesPerson = false },
		"new that replaces":   func(r *hiring.Requisition) { r.Reason = "new" },
		"a float":             func(r *hiring.Requisition) { r.SalaryMin = "6.5e3" },
		"three decimals":      func(r *hiring.Requisition) { r.SalaryMax = "8200.123" },
		"negative":            func(r *hiring.Requisition) { r.SalaryMin = "-1" },
		"backwards":           func(r *hiring.Requisition) { r.SalaryMin, r.SalaryMax = "9000", "8000.00" },
		"no start":            func(r *hiring.Requisition) { r.TargetStart = time.Time{} },
	} {
		r := ok
		mutate(&r)
		if err := hiring.Check(r); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// Wording that names an age, nationality, sex, marital status, pregnancy, caregiving, race, religion, language, disability or mental-health criterion is flagged, each category once, and clean wording is not
// (NM-REC-03, NM-AI-05: a check, never a filter).
// req: NM-REC-03
func TestJobWordingIsCheckedAgainstTheCriteriaTheGuidelinesNameAndNothingIsFlaggedFalselyInPlainText(t *testing.T) {
	for text, want := range map[string]string{
		"We want a young, energetic digital native":        "age",
		"Singaporeans only":                                "nationality",
		"Female candidates preferred":                      "sex",
		"Applicants must be unmarried":                     "marital_status",
		"Not suitable if pregnant":                         "pregnancy",
		"You have no family commitments":                   "caregiving",
		"Chinese only":                                     "race",
		"Practising Christians":                            "religion",
		"Native speakers of English":                       "language",
		"Must be able-bodied":                              "disability",
		"Must be mentally sound":                           "mental_health",
		"Recent graduates and fresh graduates are welcome": "age",
		"Males between 25 to 30 years old":                 "sex",
	} {
		found := false
		for _, f := range hiring.FairWording(text) {
			found = found || f.Category == want
		}
		if !found {
			t.Errorf("%q: %s was not flagged", text, want)
		}
	}
	for _, text := range []string{
		"You will analyse data, build models and explain them to the team. Three years of experience with Python and SQL.",
		"We offer flexible hours, health cover and learning budgets. Everyone is welcome to apply.",
	} {
		if got := hiring.FairWording(text); len(got) != 0 {
			t.Errorf("%q: flagged %+v", text, got)
		}
	}
	// A word that is also a criterion is flagged for a person to read, never refused.
	if got := hiring.FairWording("Single sign-on and a mature codebase"); len(got) != 2 {
		t.Errorf("single and mature: %+v", got)
	}
	if got := hiring.FairWording("Young young YOUNG, male and female"); len(got) != 2 || got[0].Category != "age" || got[1].Category != "sex" {
		t.Errorf("each category once, in order: %+v", got)
	}
}

// For a role that needs a pass the offer waits until the advert's 14 consecutive days have run, unless an exemption is recorded; for no pass there is no rule (NM-REC-02).
// req: NM-REC-02
func TestAnOfferForAPassRoleWaitsForTheFourteenDaysOrARecordedExemption(t *testing.T) {
	posted := d("2026-10-05")
	if from, ok := hiring.OfferFrom(true, &posted, ""); !ok || from != d("2026-10-19") {
		t.Errorf("advert on 5 Oct: %v %v", from, ok)
	}
	if _, ok := hiring.OfferFrom(true, nil, ""); ok {
		t.Error("a pass role with no advert and no exemption may be offered")
	}
	if _, ok := hiring.OfferFrom(true, nil, "fewer_than_10_employees"); !ok {
		t.Error("an exempt role is blocked")
	}
	if _, ok := hiring.OfferFrom(false, nil, ""); !ok {
		t.Error("a role with no pass has no rule")
	}
	if hiring.CheckExemption("fewer_than_10_employees", "Headcount 7 on 5 Oct") != nil || hiring.CheckExemption("made_up", "x") == nil || hiring.CheckExemption("transferee", " ") == nil {
		t.Error("an exemption is a known kind with its evidence")
	}
}

// An advert that came down more than 3 months ago cannot be used; on the 3-month day it still can. A renewal names one of the four changes (NM-REC-02).
// req: NM-REC-02
func TestAnAdvertClosedOverThreeMonthsAgoCannotBeUsedAndARenewalNamesAChange(t *testing.T) {
	closed := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	for asOf, want := range map[string]bool{"2026-10-21": true, "2027-01-20": true, "2027-01-21": false, "2027-06-01": false} {
		d, _ := time.Parse(time.DateOnly, asOf)
		if got := hiring.AdvertUsable(&closed, d); got != want {
			t.Errorf("as of %s: %v, want %v", asOf, got, want)
		}
	}
	if !hiring.AdvertUsable(nil, closed) {
		t.Error("an advert not known to have come down is usable")
	}
	for _, kind := range []string{"occupation", "salary", "vacancies", "uen"} {
		if err := hiring.CheckAdvertChange(kind, ""); err != nil {
			t.Errorf("%s: %v", kind, err)
		}
	}
	if hiring.CheckAdvertChange("colour", "") == nil || hiring.CheckAdvertChange("salary", strings.Repeat("x", 501)) == nil {
		t.Error("an unknown change or a long note")
	}
	for slug, ok := range map[string]bool{"acme-careers": true, "ab": false, "Acme": false, "-acme": false, "acme-": false, "acme--co": false, "a_b_c": false, "abc": true} {
		if got := hiring.CheckSlug(slug) == nil; got != ok {
			t.Errorf("slug %q: %v, want %v", slug, got, ok)
		}
	}
}

// The board allows only the moves it names, a candidate not progressing carries the reason, a kit's questions are checked for fair wording like a post, and a score is 1 to 5 (NM-REC-03, NM-REC-04).
// req: NM-REC-03, NM-REC-04
func TestTheBoardAllowsOnlyItsMovesAndAKitIsCheckedForFairWording(t *testing.T) {
	for _, tc := range []struct {
		from, to, reason string
		ok               bool
	}{
		{"applied", "screening", "", true}, {"applied", "offer", "", false}, {"screening", "interview", "", true}, {"interview", "offer", "", true}, {"offer", "interview", "", true},
		{"offer", "hired", "", true}, {"hired", "screening", "", false}, {"screening", "not_progressing", "", false}, {"screening", "not_progressing", "No fit.", true},
		{"not_progressing", "screening", "", true}, {"not_progressing", "offer", "", false},
	} {
		if got := hiring.CheckMove(tc.from, tc.to, tc.reason) == nil; got != tc.ok {
			t.Errorf("%s to %s (%q): %v, want %v", tc.from, tc.to, tc.reason, got, tc.ok)
		}
	}
	kit := hiring.Kit{Questions: []string{"Are you a young digital native?"}, Criteria: []string{"Skill"}}
	if found, err := hiring.CheckKit(kit); err == nil || len(found) == 0 {
		t.Errorf("a flagged question without the requirement: %v %v", found, err)
	}
	kit.RequirementNote = "The role is a youth-programme mentor."
	if _, err := hiring.CheckKit(kit); err != nil {
		t.Errorf("with the requirement recorded: %v", err)
	}
	if _, err := hiring.CheckKit(hiring.Kit{Questions: []string{"Why us?"}}); err == nil {
		t.Error("a kit with no criterion")
	}
	for score, ok := range map[int]bool{0: false, 1: true, 5: true, 6: false} {
		if (hiring.CheckScore(score, "") == nil) != ok {
			t.Errorf("score %d", score)
		}
	}
	if hiring.CheckCandidate("A", "a@b", "") == nil || hiring.CheckCandidate("A", "a@b.co", "") != nil {
		t.Error("the email address")
	}
}
