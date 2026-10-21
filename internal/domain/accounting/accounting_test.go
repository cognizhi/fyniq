package accounting

import (
	"errors"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/period"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func date(s string) time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return t
}

func signed(by string) *string { return &by }

// FR-ORG-10, G4-017 (rev 0.62): a year's statements are "management statements" and name no framework until its
// framework is confirmed and every policy in force is approved; then they show the framework and edition, and a year
// from 1 Jan 2027 under SFRS for Small Entities shows the Third Edition. Versions apply from their year until a later
// one replaces them.
// req: FR-ORG-10
func TestAYearsStatementsAreManagementStatementsUntilTheAccountantApproves(t *testing.T) {
	y2026 := period.Span{StartsOn: date("2026-01-01"), EndsOn: date("2026-12-31")}
	y2027 := period.Span{StartsOn: date("2027-01-01"), EndsOn: date("2027-12-31")}

	empty := YearOf(y2026, nil, nil)
	if empty.Approved || empty.Title != "Management statements" || empty.Basis != "" || len(empty.Checks) != 1+len(Areas) ||
		empty.Checks[0].Code != CodeNoBasis || empty.Edition != EditionOf2024 {
		t.Fatalf("nothing recorded: %+v", empty)
	}

	fw := []storage.FrameworkVersion{{Version: 1, FromOn: y2026.StartsOn, Framework: FrameworkSmallEntities, Edition: EditionOf2024, WrittenBy: "user:a"}}
	var ps []storage.PolicyVersion
	for i, a := range Areas {
		ps = append(ps, storage.PolicyVersion{Area: a.Key, Version: 1, FromOn: y2026.StartsOn, Wording: "As the accountant decided", WrittenBy: "user:a"})
		if i > 0 {
			ps[i].ApprovedBy = signed("user:acc")
		}
	}
	y := YearOf(y2026, fw, ps)
	if y.Approved || len(y.Checks) != 2 || y.Checks[0].Code != CodeUnsigned || y.Checks[1].Code != CodeUnapprove || y.Checks[1].Area != "revenue" {
		t.Fatalf("unconfirmed, revenue unapproved: %+v", y.Checks)
	}

	fw[0].ConfirmedBy, ps[0].ApprovedBy = signed("user:acc"), signed("user:acc")
	y = YearOf(y2026, fw, ps)
	if !y.Approved || y.Title != "Financial statements" || len(y.Checks) != 0 ||
		y.Basis != "Prepared under SFRS for Small Entities (effective for periods beginning on or after 1 January 2024)" {
		t.Fatalf("all approved: %+v", y)
	}

	// The 2026 framework carries into 2027, where the Third Edition applies.
	y = YearOf(y2027, fw, ps)
	if y.Approved || len(y.Checks) != 1 || y.Checks[0].Code != CodeEdition || y.Edition != ThirdEdition {
		t.Fatalf("2027 under the 2024 edition: %+v", y)
	}
	fw = append(fw, storage.FrameworkVersion{Version: 2, FromOn: y2027.StartsOn, Framework: FrameworkSmallEntities, Edition: ThirdEdition,
		WrittenBy: "user:a", ConfirmedBy: signed("user:acc")})
	if y = YearOf(y2027, fw, ps); !y.Approved || y.Basis != "Prepared under SFRS for Small Entities (Third Edition)" {
		t.Fatalf("2027 under the Third Edition: %+v", y)
	}
	if y = YearOf(y2026, fw, ps); !y.Approved || y.Framework.Version != 1 {
		t.Fatalf("2026 keeps its own: %+v", y)
	}

	// A new version of a policy from 2026 waits for approval, and so the year does again.
	ps = append(ps, storage.PolicyVersion{Area: "leases", Version: 2, FromOn: y2026.StartsOn, Wording: "Revised", WrittenBy: "user:a"})
	if y = YearOf(y2026, fw, ps); y.Approved || y.Policies["leases"].Version != 2 || len(y.Checks) != 1 || y.Checks[0].Area != "leases" {
		t.Fatalf("a revised policy: %+v", y.Checks)
	}
	if !PolicyReplaced(ps, ps[2]) || PolicyReplaced(ps, ps[len(ps)-1]) {
		t.Error("version 1 of leases is replaced by version 2; version 2 is not")
	}
}

// FR-ORG-10 (rev 0.62): a version applies from the first day of a financial year, at most a year after the current one;
// the accountant confirms or approves it once, and never the person who wrote it.
// req: FR-ORG-10
func TestRecordingAndSigningTheFrameworkAndPolicies(t *testing.T) {
	cal := period.Calendar{BooksStart: date("2026-03-15"), FirstYearEnd: date("2026-12-31")}
	today := date("2026-10-21")
	cases := []struct {
		from  string
		field string
	}{
		{"2026-03-15", ""},
		{"2027-01-01", ""},
		{"2026-01-01", "from_on"}, // before the books start
		{"2026-04-01", "from_on"}, // not a year's first day
		{"2028-01-01", "from_on"}, // more than a year ahead
	}
	for _, c := range cases {
		got := CheckFramework(cal, date(c.from), today, FrameworkSmallEntities, EditionOf2024)
		if (c.field == "") != (len(got) == 0) || (c.field != "" && got[0].Field != c.field) {
			t.Errorf("from %s: %+v", c.from, got)
		}
	}
	if got := CheckFramework(cal, date("2026-03-15"), today, "ifrs", " "); len(got) != 2 || got[0].Field != "framework" || got[1].Field != "edition" {
		t.Errorf("an unknown framework, no edition: %+v", got)
	}
	if got := CheckPolicy(cal, date("2026-03-15"), today, "  "); len(got) != 1 || got[0].Field != "wording" || got[0].Code != CodeInput {
		t.Errorf("no words: %+v", got)
	}

	var e *Error
	if err := MaySign("policy", "user:a", "user:a", nil, false); !errors.As(err, &e) || e.Code != CodeOwn {
		t.Errorf("its writer: %v", err)
	}
	if err := MaySign("policy", "user:a", "user:b", signed("user:c"), false); !errors.As(err, &e) || e.Code != CodeSigned {
		t.Errorf("twice: %v", err)
	}
	if err := MaySign("policy", "user:a", "user:b", nil, true); !errors.As(err, &e) || e.Code != CodeSigned {
		t.Errorf("replaced: %v", err)
	}
	if err := MaySign("policy", "user:a", "user:b", nil, false); err != nil {
		t.Errorf("another member: %v", err)
	}
}
