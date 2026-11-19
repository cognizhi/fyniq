//go:build local

package nemura_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/nemura/people"
)

func personRefusal(err error) string {
	var r *people.Refusal
	if errors.As(err, &r) {
		return r.Code
	}
	return ""
}

func d(s string) time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return t
}

// A person is the HR record, with or without a login: their job, manager and department are effective-dated history that is changed by
// ending one row and starting the next, never edited, with no overlap; their legal classification is the same and a contractor is never
// covered by the Employment Act; the members' reporting line follows the manager when both have a login; the directory shows who works
// here on a day; no audit event carries a date of birth; one organisation never sees another's people (NM-PPL-01, NM-PPL-06, NM-PPL-08).
// req: NM-PPL-01, NM-PPL-06, NM-PPL-08
// asvs: V8.2.1, V8.3.1
func TestAPersonHasEffectiveDatedHistoryAndTheDirectoryShowsWhoWorksHere(t *testing.T) {
	ctx := context.Background()
	c := newCompany(t)
	other := newCompany(t)
	now := today.Add(9 * time.Hour)
	var ana, ben, cy uuid.UUID
	job := func(title string, mgr *uuid.UUID) people.Job {
		return people.Job{Title: title, Department: "Engineering", Worksite: "SG", EmploymentType: "full_time", Manager: mgr}
	}
	// Ana is the Owner's person (with a login); Ben reports to Ana and has a login too; Cy is a contractor with no login.
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		ana, err = nemura.AddPerson(ctx, tx, c.org, now, c.owner, people.Person{LegalName: "Ana Tan", WorkEmail: "ana@acme.example", EmploymentStart: d("2099-01-05"), MemberID: &c.owner}, job("CEO", nil),
			&people.Classification{ContractType: "service", EACovered: true, PartIVClass: "none", Residency: "citizen", BirthDate: ptr(d("1980-03-04"))})
		if err != nil {
			return err
		}
		ben, err = nemura.AddPerson(ctx, tx, c.org, now, c.owner, people.Person{LegalName: "Ben Lim", PreferredName: "Benny", EmploymentStart: d("2099-02-01"), MemberID: &c.hr}, job("Engineer", &ana), nil)
		if err != nil {
			return err
		}
		cy, err = nemura.AddPerson(ctx, tx, c.org, now, c.owner, people.Person{LegalName: "Cy Wong", EmploymentStart: d("2099-02-15"), LeavingDate: ptr(d("2099-06-30"))},
			people.Job{Title: "Designer", Worksite: "SG", EmploymentType: "contract"}, &people.Classification{ContractType: "for_service", EACovered: false, PartIVClass: "none", Residency: "foreigner"})
		return err
	})

	// The reporting line of members followed Ben's manager (both have a login).
	var line int
	if err := connect(t, db.SuperURL()).QueryRow(ctx, "SELECT count(*) FROM nemura.reporting_line WHERE organisation_id = $1 AND report_id = $2 AND manager_id = $3 AND effective_to IS NULL", c.org, c.hr, c.owner).Scan(&line); err != nil || line != 1 {
		t.Fatalf("the reporting line: %d, %v", line, err)
	}

	// A change of job ends the old row the day it starts the new; the history keeps both; the directory shows the new on that day only.
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.ChangeJob(ctx, tx, c.org, now, c.owner, ben, d("2099-04-01"), job("Senior Engineer", &ana))
	})
	h := historyOf(t, c, ben)
	if len(h) != 2 || h[0].Title != "Engineer" || h[0].To == nil || !h[0].To.Equal(d("2099-04-01")) || h[1].Title != "Senior Engineer" || h[1].To != nil {
		t.Fatalf("job history: %+v", h)
	}
	for on, want := range map[string]string{"2099-03-01": "Engineer", "2099-04-01": "Senior Engineer"} {
		var got string
		c.must(func(ctx context.Context, tx pgx.Tx) error {
			r, err := nemura.PersonOn(ctx, tx, ben, d(on))
			got = r.Title
			return err
		})
		if got != want {
			t.Errorf("Ben on %s is %q, want %q", on, got, want)
		}
	}
	// A new manager changes the members' line; a manager who is a person with no login leaves none.
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.ChangeJob(ctx, tx, c.org, now, c.owner, ben, d("2099-05-01"), job("Senior Engineer", &cy))
	})
	if err := connect(t, db.SuperURL()).QueryRow(ctx, "SELECT count(*) FROM nemura.reporting_line WHERE organisation_id = $1 AND report_id = $2 AND effective_to IS NULL", c.org, c.hr).Scan(&line); err != nil || line != 0 {
		t.Errorf("a manager with no login leaves no open line: %d, %v", line, err)
	}

	// History is never edited, overlapped or deleted; a change dated on or before the current start is refused.
	err := c.do(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.ChangeJob(ctx, tx, c.org, now, c.owner, ben, d("2099-05-01"), job("Lead", nil))
	})
	if personRefusal(err) != people.CodeHistory {
		t.Errorf("a change dated on the current start: %v", err)
	}
	for _, sql := range []string{"UPDATE nemura.person_job SET job_title = 'Boss'", "DELETE FROM nemura.person_job", "UPDATE nemura.person_classification SET residency = 'citizen'", "DELETE FROM nemura.person", "UPDATE nemura.person SET employment_start = employment_start + 1"} {
		if _, err := connect(t, db.SuperURL()).Exec(ctx, sql); err == nil || !strings.Contains(err.Error(), "NM12") {
			t.Errorf("%s: %v", sql, err)
		}
	}
	if _, err := connect(t, db.SuperURL()).Exec(ctx, `INSERT INTO nemura.person_job (organisation_id, person_id, effective_from, job_title, worksite, employment_type) VALUES ($1, $2, '2099-02-10', 'Overlap', 'SG', 'full_time')`, c.org, ben); err == nil || !strings.Contains(err.Error(), "NM122") {
		t.Errorf("an overlapping job: %v", err)
	}

	// The classification: a contractor is never covered by the Employment Act; a Part IV class needs cover; a permanent resident has a start.
	for name, cl := range map[string]people.Classification{
		"a covered contractor":   {ContractType: "for_service", EACovered: true, PartIVClass: "none", Residency: "foreigner"},
		"Part IV with no cover":  {ContractType: "service", EACovered: false, PartIVClass: "workman", Residency: "citizen"},
		"a PR with no start":     {ContractType: "service", EACovered: true, PartIVClass: "none", Residency: "permanent_resident"},
		"a citizen with a start": {ContractType: "service", EACovered: true, PartIVClass: "none", Residency: "citizen", PRStart: ptr(d("2090-01-01"))},
		"an unknown residency":   {ContractType: "service", EACovered: true, PartIVClass: "none", Residency: "tourist"},
	} {
		err := c.do(func(ctx context.Context, tx pgx.Tx) error {
			return nemura.SetClassification(ctx, tx, c.org, now, c.owner, ben, d("2099-03-01"), cl)
		})
		if personRefusal(err) != people.CodeInvalid {
			t.Errorf("%s: %v", name, err)
		}
	}
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		if err := nemura.SetClassification(ctx, tx, c.org, now, c.owner, ben, d("2099-03-01"), people.Classification{ContractType: "service", EACovered: true, PartIVClass: "non_workman", Residency: "permanent_resident", PRStart: ptr(d("2090-01-01"))}); err != nil {
			return err
		}
		return nemura.SetClassification(ctx, tx, c.org, now, c.owner, ben, d("2099-09-01"), people.Classification{ContractType: "service", EACovered: true, PartIVClass: "none", Residency: "citizen"})
	})
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		cl, ok, err := nemura.ClassificationOn(ctx, tx, ben, d("2099-05-01"))
		if err != nil || !ok || cl.PartIVClass != "non_workman" || cl.Residency != "permanent_resident" {
			t.Errorf("Ben's classification in May: %+v %v %v", cl, ok, err)
		}
		cl, _, _ = nemura.ClassificationOn(ctx, tx, ben, d("2099-10-01"))
		if cl.Residency != "citizen" {
			t.Errorf("Ben's classification in October: %+v", cl)
		}
		return nil
	})

	// The directory: who works here on a day (started, not yet left), by name, with the manager's name.
	list := func(on string) []string {
		var names []string
		c.must(func(ctx context.Context, tx pgx.Tx) error {
			rows, err := nemura.People(ctx, tx, d(on))
			for _, r := range rows {
				names = append(names, r.LegalName+"/"+r.ManagerName)
			}
			return err
		})
		return names
	}
	if got := strings.Join(list("2099-03-01"), ","); got != "Ana Tan/,Ben Lim/Ana Tan,Cy Wong/" {
		t.Errorf("the directory in March: %s", got)
	}
	if got := strings.Join(list("2099-07-01"), ","); got != "Ana Tan/,Ben Lim/Cy Wong" {
		t.Errorf("the directory in July (Cy has left, Ben reports to Cy's record): %s", got)
	}
	if got := list("2099-01-06"); len(got) != 1 {
		t.Errorf("the directory before Ben started: %v", got)
	}

	// The audit trail never carries a date of birth, and one organisation never sees another's people.
	var leaked int
	if err := connect(t, db.SuperURL()).QueryRow(ctx, "SELECT count(*) FROM nemura.audit_event WHERE organisation_id = $1 AND body LIKE '%1980-03-04%'", c.org).Scan(&leaked); err != nil || leaked != 0 {
		t.Errorf("a date of birth in the trail: %d, %v", leaked, err)
	}
	other.must(func(ctx context.Context, tx pgx.Tx) error {
		if got, _ := nemura.People(ctx, tx, d("2099-03-01")); len(got) != 0 {
			t.Errorf("another organisation sees %d people", len(got))
		}
		if _, err := nemura.PersonOn(ctx, tx, ana, d("2099-03-01")); personRefusal(err) != people.CodeNotFound {
			t.Errorf("another organisation reads a person: %v", err)
		}
		return nil
	})
	if _, err := store(t, "nemura_worker").VerifyAudit(ctx, c.org); err != nil {
		t.Errorf("the audit chain: %v", err)
	}
}

func ptr[T any](v T) *T { return &v }

func historyOf(t *testing.T, c *company, person uuid.UUID) []nemura.JobRow {
	t.Helper()
	var h []nemura.JobRow
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		h, err = nemura.JobHistory(ctx, tx, person)
		return err
	})
	return h
}
