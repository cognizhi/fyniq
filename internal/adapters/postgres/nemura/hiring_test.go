//go:build local

package nemura_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/hiring"
)

func hireCode(err error) string {
	var r *hiring.Refusal
	if errors.As(err, &r) {
		return r.Code
	}
	return ""
}

// A requisition is asked for whole, decided once by the Owner, and its post is written as a draft, published only for an approved requisition and, when the wording check flags a criterion, only with the
// genuine requirement recorded; a published post never changes, the advert's date and an exemption are recorded once, and an offer for a pass role waits for the 14 days or an exemption (NM-REC-01 to NM-REC-03).
// req: NM-REC-01, NM-REC-02, NM-REC-03
// asvs: V8.3.1
func TestARequisitionIsApprovedOnceAndItsPostIsCheckedPublishedOnceAndKeepsItsAdvertDates(t *testing.T) {
	c := newCompany(t)
	now := today.Add(11 * time.Hour)
	in := api.RequisitionInput{Title: "Data scientist", Department: "Engineering", Reason: "new", SalaryMin: "6500.00", SalaryMax: "8200", TargetStart: "2099-06-01", PassNeeded: true}
	var req uuid.UUID
	c.must(func(ctx context.Context, tx pgx.Tx) (err error) {
		req, err = nemura.RequestRequisition(ctx, tx, c.org, now, c.hr, in)
		return
	})
	bad := in
	bad.SalaryMin = "9000"
	if err := c.do(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.RequestRequisition(ctx, tx, c.org, now, c.hr, bad)
		return err
	}); hireCode(err) != hiring.CodeInvalid {
		t.Errorf("a salary range that runs backwards: %v", err)
	}
	view := func() api.Requisition {
		var r api.Requisition
		c.must(func(ctx context.Context, tx pgx.Tx) (err error) { r, err = nemura.OneRequisition(ctx, tx, req); return })
		return r
	}
	if r := view(); r.State != "requested" || r.SalaryMax != "8200.00" || r.PlanStatus != "unknown" || r.RequestedBy != "Hana HR" {
		t.Fatalf("%+v", r)
	}
	write := func(title, body string) error {
		return c.do(func(ctx context.Context, tx pgx.Tx) error {
			return nemura.WriteJobPost(ctx, tx, c.org, now, c.hr, req, title, body)
		})
	}
	publish := func(note string) error {
		return c.do(func(ctx context.Context, tx pgx.Tx) error {
			return nemura.PublishJobPost(ctx, tx, c.org, now, c.hr, req, note)
		})
	}
	if err := write("Data scientist", "You will analyse data. We want a young, energetic digital native."); err != nil {
		t.Fatal(err)
	}
	if r := view(); len(r.JobPost.Findings) == 0 || r.JobPost.Findings[0].Category != "age" || !r.JobPost.OfferBlocked {
		t.Errorf("the findings and the offer block: %+v", r.JobPost)
	}
	// Not yet approved: no publication. Decided once; the Owner approves.
	if err := publish(""); hireCode(err) != hiring.CodeConflict {
		t.Errorf("publishing an unapproved requisition: %v", err)
	}
	decide := func(approve bool) error {
		return c.do(func(ctx context.Context, tx pgx.Tx) error {
			return nemura.DecideRequisition(ctx, tx, c.org, now, c.owner, req, approve, "In the plan.")
		})
	}
	if err := decide(true); err != nil {
		t.Fatal(err)
	}
	if err := decide(false); hireCode(err) != hiring.CodeConflict {
		t.Errorf("deciding twice: %v", err)
	}
	// A flagged criterion is kept only with the genuine requirement recorded.
	if err := publish(""); hireCode(err) != hiring.CodeWording {
		t.Errorf("publishing flagged wording with no requirement: %v", err)
	}
	if err := write("Data scientist", "You will analyse data and explain your models to the team."); err != nil {
		t.Fatal(err)
	}
	if err := publish(""); err != nil {
		t.Fatal(err)
	}
	if r := view(); r.JobPost.State != "published" || r.JobPost.PublishedOn != "2099-03-01" || r.JobPost.RequirementNote != "" {
		t.Errorf("%+v", r.JobPost)
	}
	if err := write("Another", "words"); hireCode(err) != hiring.CodeConflict {
		t.Errorf("changing a published post: %v", err)
	}
	if err := c.do(func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE nemura.job_post SET body = 'changed straight to the database'`)
		return err
	}); pgErr(err) != "NM213" {
		t.Errorf("a published post changed in the database: %v", err)
	}
	// The advert: once, a day up to today; the offer waits 14 days from it. An exemption is recorded once and lifts the wait.
	advert := func(day string) error {
		return c.do(func(ctx context.Context, tx pgx.Tx) error {
			return nemura.RecordJobAdvert(ctx, tx, c.org, now, c.hr, req, day, "MCF-2099-0042")
		})
	}
	if err := advert("2099-03-02"); hireCode(err) != hiring.CodeInvalid {
		t.Errorf("an advert dated in the future: %v", err)
	}
	if err := advert("2099-02-20"); err != nil {
		t.Fatal(err)
	}
	if err := advert("2099-02-21"); hireCode(err) != hiring.CodeConflict {
		t.Errorf("the advert twice: %v", err)
	}
	if r := view(); r.JobPost.OfferFrom != "2099-03-06" || r.JobPost.OfferBlocked {
		t.Errorf("14 days from 20 Feb: %+v", r.JobPost)
	}
	exempt := func() error {
		return c.do(func(ctx context.Context, tx pgx.Tx) error {
			return nemura.RecordJobExemption(ctx, tx, c.org, now, c.hr, req, "fewer_than_10_employees", "Headcount 7 on 20 Feb.")
		})
	}
	if err := exempt(); err != nil {
		t.Fatal(err)
	}
	if err := exempt(); hireCode(err) != hiring.CodeConflict {
		t.Errorf("an exemption twice: %v", err)
	}
	if r := view(); r.JobPost.OfferFrom != "" || r.JobPost.OfferBlocked || r.JobPost.Exemption != "fewer_than_10_employees" {
		t.Errorf("an exempt role is held back: %+v", r.JobPost)
	}
	// A role that needs no pass has nothing to exempt; a post closes once.
	var req2 uuid.UUID
	in.PassNeeded = false
	c.must(func(ctx context.Context, tx pgx.Tx) (err error) {
		req2, err = nemura.RequestRequisition(ctx, tx, c.org, now, c.hr, in)
		return
	})
	if err := c.do(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.RecordJobExemption(ctx, tx, c.org, now, c.hr, req2, "transferee", "Intra-company transfer.")
	}); hireCode(err) == "" {
		t.Errorf("an exemption for a role with no pass: %v", err)
	}
	closeIt := func() error {
		return c.do(func(ctx context.Context, tx pgx.Tx) error { return nemura.CloseJobPost(ctx, tx, c.org, now, c.hr, req) })
	}
	if err := closeIt(); err != nil {
		t.Fatal(err)
	}
	if err := closeIt(); hireCode(err) != hiring.CodeConflict {
		t.Errorf("closing twice: %v", err)
	}
	// A requisition is never deleted and what was asked for never changes, even straight to the database.
	for name, sql := range map[string]string{"deleting": `DELETE FROM nemura.requisition`, "a new salary": `UPDATE nemura.requisition SET salary_max = salary_max + 1`, "backwards": `UPDATE nemura.requisition SET state = 'requested' WHERE state = 'approved'`} {
		if err := c.do(func(ctx context.Context, tx pgx.Tx) error { _, err := tx.Exec(ctx, sql); return err }); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	// Another organisation sees none of it.
	other := newCompany(t)
	var seen []api.Requisition
	if err := other.do(func(ctx context.Context, tx pgx.Tx) (err error) { seen, err = nemura.Requisitions(ctx, tx); return }); err != nil || len(seen) != 0 {
		t.Errorf("another organisation sees %d requisitions: %v", len(seen), err)
	}
}

// In founder mode the Owner asks for a requisition and approves it themselves: the record names the same person twice and the approval is
// accepted (the owner's decision of 21 Nov 2026, the one exception to NM-ROLE-05, for requisitions only).
// req: NM-REC-01
// asvs: V8.3.1
func TestTheOwnerMayApproveARequisitionTheyAskedFor(t *testing.T) {
	c := newCompany(t)
	now := today.Add(11 * time.Hour)
	in := api.RequisitionInput{Title: "Bookkeeper", Department: "Finance", Reason: "new", SalaryMin: "4000.00", SalaryMax: "5000.00", TargetStart: "2099-06-01"}
	var req uuid.UUID
	c.must(func(ctx context.Context, tx pgx.Tx) (err error) {
		req, err = nemura.RequestRequisition(ctx, tx, c.org, now, c.owner, in)
		return
	})
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.DecideRequisition(ctx, tx, c.org, now, c.owner, req, true, "My own request, in founder mode.")
	})
	var r api.Requisition
	c.must(func(ctx context.Context, tx pgx.Tx) (err error) { r, err = nemura.OneRequisition(ctx, tx, req); return })
	if r.State != "approved" || r.RequestedBy == "" || r.RequestedBy != r.DecidedBy {
		t.Errorf("%+v", r)
	}
}
