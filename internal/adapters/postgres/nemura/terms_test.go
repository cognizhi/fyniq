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
	"github.com/cognizhi/fyniq/internal/nemura/letters"
	"github.com/cognizhi/fyniq/internal/nemura/people"
)

// The key employment terms are written in two parts by two roles, history is only ever ended, the letter is filled from the terms, the working pattern, the classification and
// the pack, a pay fact that is not recorded is named instead of guessed, an issued letter silences N.1 #1, and the pay never enters the audit event (NM-LTR-06, NM-LTR-01).
// req: NM-LTR-06, NM-LTR-01
func TestTheKeyEmploymentTermsAreRecordedInTwoPartsAndFilledFromTheRecord(t *testing.T) {
	ctx := context.Background()
	lt := newLeaveTeam(t)
	now := d("2026-11-20").Add(10 * time.Hour)
	blobs, err := local.NewDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	packs, err := nemura.NewPacksService(store(t, "nemura_api"), clock.NewFrozen(now))
	if err != nil {
		t.Fatal(err)
	}
	svc := nemura.LettersService{Packs: packs, Store: store(t, "nemura_api"), Clock: clock.NewFrozen(now), Blobs: blobs, Renderer: letters.Local{}}
	hr := api.Session{Organisation: lt.c.org, Member: lt.c.hr}
	pay := api.Session{Organisation: lt.c.org, Member: lt.c.pay}
	owner := api.Session{Organisation: lt.c.org, Member: lt.c.owner, StepUpAt: &now}
	base := &api.TermsBase{MainDuties: "Keeps the books and files the returns.", NoticePeriod: "One month"}
	money := &api.TermsPay{SalaryPeriod: "monthly", BasicSalary: "4500.00", FixedAllowances: "Transport 100.00", OvertimePeriod: "Monthly, with the next salary"}

	// Pay terms cannot start before the base terms; the people team cannot write pay and payroll cannot write the base.
	if err := svc.SetTerms(ctx, pay, lt.cy, d("2026-01-05"), nil, money); letterCode(err) != letters.CodeConflict {
		t.Errorf("pay before the base terms: %v", err)
	}
	if err := svc.SetTerms(ctx, hr, lt.cy, d("2026-01-05"), &api.TermsBase{MainDuties: " ", NoticePeriod: "x"}, nil); letterCode(err) != letters.CodeInvalid {
		t.Errorf("blank duties: %v", err)
	}
	if err := svc.SetTerms(ctx, hr, lt.cy, d("2026-01-05"), base, nil); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetTerms(ctx, pay, lt.cy, d("2026-01-05"), nil, money); letterCode(err) != letters.CodeConflict {
		t.Errorf("new terms on the day the current ones began: %v", err)
	}
	if err := svc.SetTerms(ctx, pay, lt.cy, d("2026-02-01"), nil, &api.TermsPay{SalaryPeriod: "monthly", BasicSalary: "abc"}); letterCode(err) != letters.CodeInvalid {
		t.Errorf("a bad salary: %v", err)
	}
	if err := svc.SetTerms(ctx, pay, lt.cy, d("2026-02-01"), nil, &api.TermsPay{BasicSalary: "100.00"}); letterCode(err) != letters.CodeInvalid {
		t.Errorf("a salary with no period: %v", err)
	}
	if err := svc.SetTerms(ctx, pay, lt.cy, d("2026-02-01"), nil, money); err != nil {
		t.Fatal(err)
	}
	// The base carried forward; the pay is visible only to those who may read it.
	if got, ok, err := svc.Terms(ctx, hr, lt.cy, d("2026-03-01"), false); err != nil || !ok || got.Pay != nil || got.Base.MainDuties != base.MainDuties || got.From != "2026-02-01" {
		t.Errorf("the people team's view: %+v %v %v", got, ok, err)
	}
	if got, _, _ := svc.Terms(ctx, pay, lt.cy, d("2026-03-01"), true); got.Pay == nil || got.Pay.BasicSalary != "4500.00" {
		t.Errorf("payroll's view: %+v", got)
	}
	if got, _, _ := svc.Terms(ctx, hr, lt.cy, d("2026-01-20"), true); got.From != "2026-01-05" || got.To != "2026-02-01" || got.Pay.BasicSalary != "" {
		t.Errorf("the first terms, ended: %+v", got)
	}
	// History is only ever ended, once; the database says so too.
	su := connect(t, db.SuperURL())
	if _, err := su.Exec(ctx, "UPDATE nemura.person_terms SET notice_period = 'None' WHERE effective_to IS NOT NULL"); err == nil || !strings.Contains(err.Error(), "NM163") {
		t.Errorf("an ended record changed: %v", err)
	}
	if _, err := su.Exec(ctx, "DELETE FROM nemura.person_terms"); err == nil || !strings.Contains(err.Error(), "NM163") {
		t.Errorf("a record deleted: %v", err)
	}
	// N.1 #1 sees Cy: started 5 Jan 2026, no KETs issued.
	var due []checks.KETDue
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		due, err = nemura.KETFacts(ctx, tx)
		return err
	})
	found := false
	for _, x := range due {
		if x.Person == lt.cy.String() {
			found = true
		}
	}
	if !found {
		t.Fatalf("Cy is not in the people without KETs: %+v", due)
	}
	if f := checks.KETs(due, 14, now); len(f) == 0 || f[0].Status != checks.Fail {
		t.Errorf("the check: %+v", f)
	}

	// The letter: the letterhead, the type and the standard text; then the request is filled from the terms.
	if err := svc.SetSettings(ctx, hr, api.LetterSettings{Name: "Acme Pte. Ltd.", UEN: "201912345A", Address: "1 Raffles Place"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetType(ctx, hr, "kets", true, 14); err != nil {
		t.Fatal(err)
	}
	blocks := []api.LetterBlock{}
	for _, b := range letters.KETDefault {
		blocks = append(blocks, api.LetterBlock{Kind: string(b.Kind), Text: b.Text})
	}
	draft, err := svc.SaveDraft(ctx, hr, "kets", blocks, "The standard text.")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Publish(ctx, owner, mustUUID(draft)); err != nil {
		t.Fatal(err)
	}
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.SetClassification(ctx, tx, lt.c.org, now, lt.c.owner, lt.cy, d("2026-11-01"), people.Classification{ContractType: "service", EACovered: true, PartIVClass: "workman", Residency: "citizen"})
	})
	id, _, err := svc.RequestFor(ctx, hr, lt.cy, "kets", "The key employment terms")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	// Ben (HR administrator, a different person) checks; the Owner signs; the PDF carries the facts.
	ref, sha, err := svc.Check(ctx, hr, mustUUID(id))
	if err != nil || !strings.HasPrefix(ref, "KE-2026-") {
		t.Fatalf("check: %s %v", ref, err)
	}
	if _, err := svc.Sign(ctx, owner, mustUUID(id), sha); err != nil {
		t.Fatal(err)
	}
	_, pdf, err := svc.File(ctx, owner, mustUUID(id), true)
	if err != nil || !bytes.HasPrefix(pdf, []byte("%PDF")) {
		t.Fatalf("the letter: %v", err)
	}
	var data string
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT data::text FROM nemura.letter_request WHERE id = $1", id).Scan(&data)
	})
	for _, want := range []string{"S$4,500.00 a month", "Keeps the books", "One month", "Monday, Tuesday, Wednesday, Thursday, Friday", "44.0 hours a week", "Annual leave is 7 days", "Not applicable: the contract is not for a fixed term", "Monthly, with the next salary", "At least 1.50 times the hourly basic rate of pay, for a basic monthly salary up to S$4500"} {
		if !strings.Contains(data, want) {
			t.Errorf("the letter's data lacks %q", want)
		}
	}
	// Issued: the check is silent for Cy, and the audit trail never holds the pay.
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		due, err = nemura.KETFacts(ctx, tx)
		return err
	})
	for _, x := range due {
		if x.Person == lt.cy.String() {
			t.Error("Cy still has no KETs after the letter was issued")
		}
	}
	var leak int
	if err := su.QueryRow(ctx, "SELECT count(*) FROM nemura.audit_event WHERE organisation_id = $1 AND (body LIKE '%4500%' OR body LIKE '%Transport%')", lt.c.org).Scan(&leak); err != nil || leak != 0 {
		t.Errorf("the pay is in %d audit events", leak)
	}
	// A pay fact that is not recorded is named: Ben has no terms at all.
	if _, _, err := svc.RequestFor(ctx, hr, lt.ben, "kets", "x"); letterCode(err) != letters.CodeInvalid {
		t.Errorf("a request with no terms recorded: %v", err)
	}
}

func mustUUID(s string) uuid.UUID { return uuid.MustParse(s) }
