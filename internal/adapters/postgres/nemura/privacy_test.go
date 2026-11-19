//go:build local

package nemura_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/nemura/accessreq"
	"github.com/cognizhi/fyniq/internal/nemura/authz"
	"github.com/cognizhi/fyniq/internal/nemura/retention"
)

// A person's request for their own file is tracked with a due date 30 days out; HR decides each category (a withheld one names a
// recorded exception), nobody handles their own request, the written outcome is given once, and an answered request and its
// decisions are kept; an unanswered one past its date with no notice in time is listed for Checks (NM-SEC-07, CMP-PD-04).
// req: NM-SEC-07
// asvs: V14.2.6, V8.2.1
func TestAnAccessRequestIsTrackedDecidedAnsweredAndKept(t *testing.T) {
	ctx := context.Background()
	c := newCompany(t)
	now := today.Add(9 * time.Hour)
	var req uuid.UUID
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		req, err = nemura.OpenAccessRequest(ctx, tx, c.org, now, c.pay, today)
		return err
	})
	hr := []authz.Role{authz.HRAdmin}
	decide := func(by uuid.UUID, held []authz.Role, category string, d accessreq.Decision, ex string) error {
		return c.do(func(ctx context.Context, tx pgx.Tx) error {
			return nemura.DecideAccessItem(ctx, tx, c.org, now, by, held, req, category, d, ex)
		})
	}
	if err := decide(c.hr, hr, "members", accessreq.Release, ""); err != nil {
		t.Fatal(err)
	}
	if err := decide(c.hr, hr, "access-requests", accessreq.Withhold, ""); err == nil {
		t.Error("a category was withheld with no exception")
	}
	if err := decide(c.hr, hr, "legal-holds", accessreq.Withhold, accessreq.Exceptions[1]); err != nil {
		t.Fatal(err)
	}
	if err := decide(c.pay, []authz.Role{authz.Employee}, "members", accessreq.Release, ""); err == nil {
		t.Error("an employee decided on a request for access")
	}
	if err := decide(c.pay, hr, "members", accessreq.Release, ""); err == nil || !strings.Contains(err.Error(), "NM_APPROVAL_SELF") {
		t.Errorf("a person handling their own request: %v", err)
	}
	// Past its date with no notice it is overdue; a written notice in time stops that.
	overdue := func(on time.Time) int {
		var n int
		c.must(func(ctx context.Context, tx pgx.Tx) error {
			ids, err := nemura.OverdueAccessRequests(ctx, tx, on)
			n = len(ids)
			return err
		})
		return n
	}
	if overdue(today.AddDate(0, 0, 30)) != 0 || overdue(today.AddDate(0, 0, 31)) != 1 {
		t.Error("the request is overdue the day after its due date")
	}
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.GiveExtendedNotice(ctx, tx, c.org, now, c.hr, req, today.AddDate(0, 0, 25))
	})
	if overdue(today.AddDate(0, 0, 40)) != 0 {
		t.Error("a written notice in time did not stop the request being overdue")
	}
	// The written outcome is given once; then nothing about the request changes.
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.AnswerAccessRequest(ctx, tx, c.org, now, c.hr, req, "We have sent you your members record and withheld one category under legal privilege.", today.AddDate(0, 0, 20))
	})
	if err := decide(c.hr, hr, "members", accessreq.Redact, ""); err == nil || !strings.Contains(err.Error(), "NM090") {
		t.Errorf("a decision after the answer: %v", err)
	}
	err := c.do(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.AnswerAccessRequest(ctx, tx, c.org, now, c.hr, req, "again", today.AddDate(0, 0, 21))
	})
	if err == nil {
		t.Error("the request was answered twice")
	}
	if _, err := connect(t, db.SuperURL()).Exec(ctx, "UPDATE nemura.access_request SET outcome = 'changed' WHERE id = $1", req); err == nil || !strings.Contains(err.Error(), "NM090") {
		t.Errorf("changing an answered request: %v", err)
	}
	var n int
	if err := connect(t, db.SuperURL()).QueryRow(ctx, "SELECT count(*) FROM nemura.audit_event WHERE organisation_id = $1 AND action LIKE 'nemura.access_request.%'", c.org).Scan(&n); err != nil || n < 5 {
		t.Errorf("audit events of the request: %d, %v", n, err)
	}
}

// probeSource is a record type for the deletion job's test: a table of records with a retention date.
type probeSource struct{ org uuid.UUID }

func (probeSource) Records(ctx context.Context, tx pgx.Tx) ([]retention.Record, error) {
	rows, err := tx.Query(ctx, "SELECT id, subject, held_from FROM nemura.retention_probe ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []retention.Record
	for rows.Next() {
		var r retention.Record
		if err := rows.Scan(&r.ID, &r.Subject, &r.From); err != nil {
			return nil, err
		}
		r.Content = []byte(r.ID.String())
		out = append(out, r)
	}
	return out, rows.Err()
}

func (probeSource) Delete(ctx context.Context, tx pgx.Tx, ids []uuid.UUID) error {
	_, err := tx.Exec(ctx, "DELETE FROM nemura.retention_probe WHERE id = ANY($1)", ids)
	return err
}

// A due deletion is a report for review, never silent: it deletes nothing until a person reviews it, a legal hold keeps what it names
// and is listed, an unreviewed report goes overdue after 30 days, every deletion leaves a proof with a fingerprint and no content,
// and a policy below the statutory minimum is refused (NM-SEC-05, NM-SEC-04).
// req: NM-SEC-05
// req: NM-SEC-04
// asvs: V14.2.6, V16.3.1
func TestADueDeletionIsReportedReviewedHeldAndProved(t *testing.T) {
	ctx := context.Background()
	c := newCompany(t)
	now := today.Add(9 * time.Hour)
	super := connect(t, db.SuperURL())
	if _, err := super.Exec(ctx, `CREATE TABLE IF NOT EXISTS nemura.retention_probe (organisation_id uuid NOT NULL DEFAULT nemura.current_org(), id uuid NOT NULL, subject uuid NOT NULL, held_from date NOT NULL);
ALTER TABLE nemura.retention_probe ENABLE ROW LEVEL SECURITY; ALTER TABLE nemura.retention_probe FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS t ON nemura.retention_probe;
CREATE POLICY t ON nemura.retention_probe USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
GRANT SELECT, INSERT, DELETE ON nemura.retention_probe TO nemura_api, nemura_worker`); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = super.Exec(ctx, "DROP TABLE nemura.retention_probe") }()
	kept, gone, onHold := uuid.New(), uuid.New(), uuid.New()
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		for _, r := range []struct {
			id      uuid.UUID
			subject uuid.UUID
			from    time.Time
		}{{kept, c.hr, today.AddDate(-1, 0, 0)}, {gone, c.owner, today.AddDate(-5, 0, 0)}, {onHold, c.pay, today.AddDate(-5, 0, 0)}} {
			if _, err := tx.Exec(ctx, "INSERT INTO nemura.retention_probe (id, subject, held_from) VALUES ($1, $2, $3)", r.id, r.subject, r.from.Format(time.DateOnly)); err != nil {
				return err
			}
		}
		_, err := nemura.PlaceHold(ctx, tx, c.org, now, c.owner, "probe", &c.pay, "A claim by Pia is being defended", today)
		return err
	})
	policy := retention.Policy{RecordType: "probe", MinMonths: 24}
	src := probeSource{org: c.org}
	worker := store(t, "nemura_worker")
	var report uuid.UUID
	var plan retention.Plan
	// The refusal first: a company policy below the statutory minimum is refused before anything is planned.
	below := retention.Policy{RecordType: "probe", MinMonths: 24, KeepMonths: 6}
	if err := worker.InOrg(ctx, c.org, func(ctx context.Context, tx pgx.Tx) error {
		_, _, err := nemura.ReportDueDeletions(ctx, tx, c.org, now, today, below, src)
		return err
	}); err == nil || !strings.Contains(err.Error(), retention.CodeBelowMinimum) {
		t.Errorf("a policy below the minimum: %v", err)
	}
	if err := worker.InOrg(ctx, c.org, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		report, plan, err = nemura.ReportDueDeletions(ctx, tx, c.org, now, today, policy, src)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(plan.Due) != 1 || plan.Due[0].ID != gone || len(plan.Held) != 1 || plan.Held[0].ID != onHold {
		t.Fatalf("the plan: %d due, %d held", len(plan.Due), len(plan.Held))
	}
	count := func() (n int) {
		c.must(func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, "SELECT count(*) FROM nemura.retention_probe").Scan(&n)
		})
		return n
	}
	if count() != 3 {
		t.Fatal("the report deleted records: a deletion is never silent")
	}
	// It cannot run before it is reviewed.
	run := func() (int, error) {
		var n int
		err := worker.InOrg(ctx, c.org, func(ctx context.Context, tx pgx.Tx) error {
			var err error
			n, err = nemura.RunReviewedDeletion(ctx, tx, c.org, now, today, report, policy, src)
			return err
		})
		return n, err
	}
	if _, err := run(); err == nil || !strings.Contains(err.Error(), "not been reviewed") {
		t.Errorf("deleting before the review: %v", err)
	}
	// Left unreviewed past its date the report is overdue; the review takes it out of that.
	overdue := func(on time.Time) (n int) {
		c.must(func(ctx context.Context, tx pgx.Tx) error {
			ids, err := nemura.OverdueDeletionReports(ctx, tx, on)
			n = len(ids)
			return err
		})
		return n
	}
	if overdue(today.AddDate(0, 0, 30)) != 0 || overdue(today.AddDate(0, 0, 31)) != 1 {
		t.Error("an unreviewed report is overdue the day after its review date")
	}
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.ReviewDeletionReport(ctx, tx, c.org, now, c.hr, report, today.AddDate(0, 0, 3))
	})
	if overdue(today.AddDate(0, 0, 60)) != 0 {
		t.Error("a reviewed report is still overdue")
	}
	// Only the due record that no hold keeps is deleted, with a proof.
	if n, err := run(); err != nil || n != 1 {
		t.Fatalf("the reviewed deletion: %d, %v", n, err)
	}
	if count() != 2 {
		t.Errorf("%d records remain, want 2", count())
	}
	var proofs, withContent int
	if err := super.QueryRow(ctx, "SELECT count(*), count(*) FILTER (WHERE encode(fingerprint, 'hex') LIKE '%' || $2 || '%') FROM nemura.deletion_proof WHERE report_id = $1", report, gone.String()).Scan(&proofs, &withContent); err != nil || proofs != 1 || withContent != 0 {
		t.Errorf("proofs %d (want 1), proofs holding the record's id %d (want 0), %v", proofs, withContent, err)
	}
	for _, sql := range []string{"UPDATE nemura.deletion_proof SET deleted_on = deleted_on", "DELETE FROM nemura.deletion_proof"} {
		if _, err := super.Exec(ctx, sql); err == nil || !strings.Contains(err.Error(), "NM091") {
			t.Errorf("%s: %v", sql, err)
		}
	}
	// Run again, the held record stays: the hold ends only when released.
	var hold uuid.UUID
	if err := super.QueryRow(ctx, "SELECT id FROM nemura.legal_hold WHERE organisation_id = $1", c.org).Scan(&hold); err != nil {
		t.Fatal(err)
	}
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.ReleaseHold(ctx, tx, c.org, now, c.owner, hold, today.AddDate(0, 0, 10))
	})
	var again retention.Plan
	if err := worker.InOrg(ctx, c.org, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		_, again, err = nemura.ReportDueDeletions(ctx, tx, c.org, now, today.AddDate(0, 0, 11), policy, src)
		return err
	}); err != nil || len(again.Due) != 1 || again.Due[0].ID != onHold {
		t.Errorf("once the hold is released the record is due: %+v, %v", again, err)
	}
	if _, err := store(t, "nemura_worker").VerifyAudit(ctx, c.org); err != nil {
		t.Errorf("the chain: %v", err)
	}
}

// Personal data leaves Singapore only after an assessment that is current (NM-SEC-03, CMP-PD-06): none, an expired one and one for
// another country allow nothing; Singapore needs none; assessments are append-only.
// req: NM-SEC-03
// asvs: V14.2.6
func TestPersonalDataLeavesSingaporeOnlyAfterAnAssessment(t *testing.T) {
	ctx := context.Background()
	c := newCompany(t)
	now := today.Add(9 * time.Hour)
	allowed := func(flow, country string, on time.Time) (ok bool) {
		c.must(func(ctx context.Context, tx pgx.Tx) error {
			var err error
			ok, err = nemura.TransferAllowed(ctx, tx, flow, country, on)
			return err
		})
		return ok
	}
	if !allowed("email", "SG", today) || allowed("email", "US", today) {
		t.Fatal("before any assessment: Singapore only")
	}
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.RecordTransferAssessment(ctx, tx, c.org, now, c.owner, "email", "US", today, today.AddDate(1, 0, 0))
	})
	if !allowed("email", "US", today) || !allowed("email", "US", today.AddDate(0, 6, 0)) || allowed("email", "US", today.AddDate(1, 0, 1)) ||
		allowed("email", "AU", today) || allowed("ai", "US", today) || allowed("email", "US", today.AddDate(0, 0, -1)) {
		t.Error("an assessment allows its flow to its country, for its dates only")
	}
	if err := c.do(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.RecordTransferAssessment(ctx, tx, c.org, now, c.owner, "email", "SG", today, today.AddDate(1, 0, 0))
	}); err == nil {
		t.Error("an assessment was recorded for Singapore")
	}
	if _, err := connect(t, db.SuperURL()).Exec(ctx, "DELETE FROM nemura.transfer_assessment"); err == nil || !strings.Contains(err.Error(), "NM091") {
		t.Errorf("deleting an assessment: %v", err)
	}
}
