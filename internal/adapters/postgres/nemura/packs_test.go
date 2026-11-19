//go:build local

package nemura_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/checks"
	"github.com/cognizhi/fyniq/internal/nemura/people"
	"github.com/cognizhi/fyniq/internal/nemura/policy"
)

func statutePolicy() policy.Rules {
	return policy.Rules{AnnualLeaveDays: []int{7, 8, 9, 10, 11, 12, 13, 14}, SickOutpatientDays: []int{5, 8, 11, 14}, SickHospitalDays: []int{15, 30, 45, 60},
		ChildcareCitizenDays: 6, ExtendedChildcareDays: 2, ChildcareNonCitizenDays: 2}
}

// An organisation accepts a pack version (which verifies nothing), records a decision with a reason for each researched value it uses, and sets a policy that
// is checked rule by rule against the statute: a policy below it changes nothing and is refused with its rules; the statute or more is set and a later
// policy ends the earlier; the checks show a researched value in use until it is decided, a work pass about to expire, and nothing for another
// organisation's data (NM-PACK-02, 04, 07, 08; N.1 #3, #8, #13).
// req: NM-PACK-04, NM-PACK-07
// asvs: V8.2.1
func TestACompanyAcceptsAPackDecidesOnResearchedValuesAndSetsAPolicyAgainstTheStatute(t *testing.T) {
	ctx := context.Background()
	c := newCompany(t)
	other := newCompany(t)
	now := today.Add(9 * time.Hour)
	svc, err := nemura.NewPacksService(store(t, "nemura_api"), clock.NewFrozen(now))
	if err != nil {
		t.Fatal(err)
	}
	s := api.Session{Organisation: c.org, Member: c.owner}
	list, err := svc.Versions(ctx, s)
	if err != nil || len(list) != 9 || list[0].ID != "sg-people@2026.1" || list[8].ID != "sg-people@2026.9" || list[0].Accepted || list[8].Researched < 50 || len(list[0].Sources) != 7 || len(list[8].Sources) != 19 {
		t.Fatalf("%d versions: %v", len(list), err)
	}
	if err := svc.Accept(ctx, s, "sg-people@2026.1"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Accept(ctx, s, "sg-people@2026.1"); personRefusal(err) != people.CodeExists {
		t.Errorf("accepting twice: %v", err)
	}
	if err := svc.Accept(ctx, s, "sg-people@2099.1"); personRefusal(err) != people.CodeNotFound {
		t.Errorf("an unknown version: %v", err)
	}
	if list, _ := svc.Versions(ctx, s); !list[0].Accepted {
		t.Errorf("accepted: %+v", list[0])
	}
	if o, _ := svc.Versions(ctx, api.Session{Organisation: other.org, Member: other.owner}); o[0].Accepted {
		t.Error("another organisation's acceptance")
	}
	// The policy: below the statute is refused with every rule and changes nothing; the statute is set.
	from := d("2099-03-02")
	below := statutePolicy()
	below.AnnualLeaveDays = []int{6}
	below.ChildcareCitizenDays = 5
	_, err = svc.SetPolicy(ctx, s, from, below)
	if personRefusal(err) != policy.CodeBelowStatute || !strings.Contains(err.Error(), "Annual leave in year 1") || !strings.Contains(err.Error(), "Childcare leave for a citizen child under 7") {
		t.Fatalf("a policy below the statute: %v", err)
	}
	if _, _, found, _ := svc.Policy(ctx, s, from); found {
		t.Error("a refused policy was set")
	}
	more := statutePolicy()
	more.ChildcareNonCitizenDays = 4
	notes, err := svc.SetPolicy(ctx, s, from, more)
	if err != nil || len(notes) != 1 || !strings.Contains(notes[0], "more than the statutory 2") {
		t.Fatalf("setting a policy: %v %v", notes, err)
	}
	if _, err := svc.SetPolicy(ctx, s, from, statutePolicy()); personRefusal(err) != people.CodeHistory {
		t.Errorf("a second policy on the same day: %v", err)
	}
	if _, err := svc.SetPolicy(ctx, s, d("2099-06-01"), statutePolicy()); err != nil {
		t.Fatal(err)
	}
	if r, v, found, _ := svc.Policy(ctx, s, d("2099-04-01")); !found || v != "sg-people@2026.9" || r.ChildcareNonCitizenDays != 4 {
		t.Errorf("the policy in April: %+v %s %v", r, v, found)
	}
	if r, _, _, _ := svc.Policy(ctx, s, d("2099-07-01")); r.ChildcareNonCitizenDays != 2 {
		t.Errorf("the policy in July: %+v", r)
	}
	// #13: the researched values the policy reads are in use with no decision, ✖, until the company decides on each.
	on := d("2099-07-01")
	count := func(check int) int {
		got, err := svc.Checks(ctx, s, on)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, f := range got {
			if f.Check == check {
				n++
			}
		}
		return n
	}
	before := count(13)
	if before < 10 {
		t.Fatalf("researched values in use: %d", before)
	}
	if err := svc.Decide(ctx, s, "sg-people@2026.9", "annual_leave", "1", "short"); personRefusal(err) != people.CodeInvalid {
		t.Errorf("a decision with no reason: %v", err)
	}
	if err := svc.Decide(ctx, s, "sg-people@2026.9", "annual_leave", "99", "We read MOM's page and it matches."); personRefusal(err) != people.CodeInvalid {
		t.Errorf("a value that is not in the pack: %v", err)
	}
	if err := svc.Decide(ctx, s, "sg-people@2026.9", "annual_leave", "1", "We read MOM's page and it matches our contracts."); err != nil {
		t.Fatal(err)
	}
	if err := svc.Decide(ctx, s, "sg-people@2026.9", "annual_leave", "1", "We read MOM's page and it matches our contracts."); personRefusal(err) != people.CodeExists {
		t.Errorf("deciding twice: %v", err)
	}
	if after := count(13); after != before-1 {
		t.Errorf("a decision clears one value: %d, then %d", before, after)
	}
	if got, _ := svc.Checks(ctx, api.Session{Organisation: other.org, Member: other.owner}, on); len(got) != 0 {
		t.Errorf("another organisation's checks: %+v", got)
	}
	// #3: a work pass about to expire is found, and a renewal clears it.
	cipher, _ := local.NewFieldKeys(bytes.Repeat([]byte{3}, 64))
	var ana uuid.UUID
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		ana, err = nemura.AddPerson(ctx, tx, c.org, now, c.owner, people.Person{LegalName: "Ana Tan", EmploymentStart: d("2099-01-05")}, people.Job{Title: "Analyst", Worksite: "SG", EmploymentType: "full_time"}, nil)
		if err != nil {
			return err
		}
		_, err = nemura.AddWorkPass(ctx, tx, cipher, c.org, now, c.hr, ana, "s_pass", "SP12345678", d("2099-01-01"), d("2099-07-15"))
		return err
	})
	if got := count(3); got != 1 {
		t.Errorf("a pass expiring in 14 days: %d findings", got)
	}
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.AddWorkPass(ctx, tx, cipher, c.org, now, c.hr, ana, "s_pass", "SP87654321", d("2099-07-15"), d("2101-07-15"))
		return err
	})
	if got := count(3); got != 0 {
		t.Errorf("a renewed pass: %d findings", got)
	}
	// A worksite outside Singapore is refused (this release has the Singapore pack only).
	if err := c.do(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.AddPerson(ctx, tx, c.org, now, c.owner, people.Person{LegalName: "Dev Rao", EmploymentStart: d("2099-01-05")}, people.Job{Title: "Dev", Worksite: "MY-JHR", EmploymentType: "full_time"}, nil)
		return err
	}); personRefusal(err) != people.CodeInvalid {
		t.Errorf("a worksite in Malaysia: %v", err)
	}
	// Acceptances, decisions and policies are never changed or removed.
	super := connect(t, db.SuperURL())
	for _, sql := range []string{"UPDATE nemura.pack_acceptance SET version = version", "DELETE FROM nemura.pack_decision", "UPDATE nemura.company_policy SET rules = '{}'", "DELETE FROM nemura.company_policy"} {
		if _, err := super.Exec(ctx, sql); err == nil || !(strings.Contains(err.Error(), "NM129") || strings.Contains(err.Error(), "NM130")) {
			t.Errorf("%s: %v", sql, err)
		}
	}
	_ = checks.Fail
	if _, err := store(t, "nemura_worker").VerifyAudit(ctx, c.org); err != nil {
		t.Errorf("the chain: %v", err)
	}
}
