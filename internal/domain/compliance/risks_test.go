package compliance

import (
	"strings"
	"testing"
	"time"

	registry "github.com/cognizhi/fyniq/compliance"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The operator's starting register is §19's risks, each with an owner role, a rating, its mitigation, and its first
// review at the end of the next quarter (CMP-06, design D7).
// req: CMP-06
func TestTheOperatorsStartingRisks(t *testing.T) {
	seed, err := registry.Risks()
	if err != nil {
		t.Fatal(err)
	}
	if len(seed) != 22 || seed[0].Ref != "R1" || seed[21].Ref != "R22" {
		t.Fatalf("want R1 to R22, got %d", len(seed))
	}
	now := time.Date(2099, 11, 9, 20, 0, 0, 0, time.UTC) // 10 Nov in Singapore: the next quarter ends 31 Mar 2100
	risks := SeedRisks(seed, "user:x", now)
	for _, r := range risks {
		if r.Owner == "" || r.Likelihood == "" || r.Impact == "" || r.Mitigation == "" || r.State != "open" || r.ReviewMonths != 3 ||
			!r.NextReviewOn.Equal(time.Date(2100, 3, 31, 0, 0, 0, 0, time.UTC)) {
			t.Errorf("%s: %+v", r.Ref, r)
		}
		if problems := CheckRisk(r.RiskFields, now); len(problems) > 0 {
			t.Errorf("%s: %v", r.Ref, problems)
		}
	}
	if seed[13].Ref != "R14" || seed[13].Likelihood != "high" || seed[13].Description != "§19 rates its likelihood Certain." {
		t.Errorf("R14 keeps §19's word for its likelihood: %+v", seed[13])
	}
}

// The register records the PDPA penalty (CMP-PD-09, rev 0.244): on R7, the data breach, the figures of PDPA s48J(3)
// and reg 10A, checked 9 Nov 2026.
// req: CMP-PD-09
func TestThePDPAPenaltyIsInTheRegister(t *testing.T) {
	seed, err := registry.Risks()
	if err != nil {
		t.Fatal(err)
	}
	r7 := seed[6]
	if r7.Ref != "R7" || r7.Owner != "dpo" || !strings.Contains(r7.Description, "up to 10% of the organisation's annual turnover in Singapore when that is above S$10 million, otherwise up to S$1 million") {
		t.Errorf("R7: %+v", r7)
	}
}

// req: CMP-06
func TestReviewDates(t *testing.T) {
	for in, want := range map[time.Time]time.Time{
		time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC):    time.Date(2099, 6, 30, 0, 0, 0, 0, time.UTC),
		time.Date(2099, 3, 31, 15, 0, 0, 0, time.UTC):  time.Date(2099, 6, 30, 0, 0, 0, 0, time.UTC), // still 31 Mar in Singapore
		time.Date(2099, 3, 31, 16, 0, 0, 0, time.UTC):  time.Date(2099, 9, 30, 0, 0, 0, 0, time.UTC), // 1 Apr in Singapore
		time.Date(2099, 12, 31, 12, 0, 0, 0, time.UTC): time.Date(2100, 3, 31, 0, 0, 0, 0, time.UTC),
	} {
		if got := NextQuarterEnd(in); !got.Equal(want) {
			t.Errorf("NextQuarterEnd(%s) = %s, want %s", in, got, want)
		}
	}
	if got := NextReview(time.Date(2099, 2, 10, 2, 0, 0, 0, time.UTC), 3); !got.Equal(time.Date(2099, 5, 10, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("NextReview: %s", got)
	}
	f := storage.RiskFields{Title: " ", Mitigation: "", ReviewMonths: 13, NextReviewOn: time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC), Description: string(make([]byte, 4001))}
	if got := CheckRisk(f, time.Date(2099, 1, 1, 3, 0, 0, 0, time.UTC)); len(got) != 5 {
		t.Errorf("a risk wrong in every field: %v", got)
	}
}
