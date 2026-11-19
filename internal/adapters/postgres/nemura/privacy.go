package nemura

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/accessreq"
	"github.com/cognizhi/fyniq/internal/nemura/authz"
	"github.com/cognizhi/fyniq/internal/nemura/retention"
)

func day(t time.Time) string { return t.Format(time.DateOnly) }

// OpenAccessRequest records a person's request for their own data, received on a day and due 30 days after (NM-SEC-07).
func OpenAccessRequest(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, requester uuid.UUID, received time.Time) (uuid.UUID, error) {
	id := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, "INSERT INTO nemura.access_request (id, requester_id, received_on, due_on) VALUES ($1, $2, $3, $4)",
		id, requester, day(received), day(accessreq.Due(received))); err != nil {
		return uuid.Nil, err
	}
	return id, Audit(ctx, tx, org, at, user(requester), "nemura.access_request.opened", audit.Object{Type: "access_request", ID: id},
		map[string]audit.Change{"received_on": {nil, day(received)}, "due_on": {nil, day(accessreq.Due(received))}}, "")
}

// DecideAccessItem records HR's decision for one category of record of a request. Only a person who handles access requests (the
// HR administrator; the HR officer handles them) decides, never the person who asked (NM-SEC-07, Appendix B).
func DecideAccessItem(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by uuid.UUID, held []authz.Role, request uuid.UUID, category string, d accessreq.Decision, exception string) error {
	if !handlesAccessRequests(held) {
		return &accessreq.Refusal{Code: "NM_ROLE_REQUIRED", Message: "Only HR handles requests for a person's own data. Ask an HR officer or administrator."}
	}
	if err := accessreq.Check(d, exception); err != nil {
		return err
	}
	var requester uuid.UUID
	if err := tx.QueryRow(ctx, "SELECT requester_id FROM nemura.access_request WHERE id = $1", request).Scan(&requester); err != nil {
		return fmt.Errorf("there is no access request with that ID: %w", err)
	}
	if requester == by {
		return &accessreq.Refusal{Code: "NM_APPROVAL_SELF", Message: "You cannot handle your own request. Ask another HR person."}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.access_request_item (request_id, category, decision, exception, decided_by) VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (organisation_id, request_id, category) DO UPDATE SET decision = EXCLUDED.decision, exception = EXCLUDED.exception, decided_by = EXCLUDED.decided_by`,
		request, category, string(d), nullable(exception), by); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "UPDATE nemura.access_request SET state = 'in_review', handled_by = $2 WHERE id = $1 AND state = 'open'", request, by); err != nil {
		return err
	}
	return Audit(ctx, tx, org, at, user(by), "nemura.access_request.reviewed", audit.Object{Type: "access_request", ID: request},
		map[string]audit.Change{"category": {nil, category}, "decision": {nil, string(d)}, "exception": {nil, exception}}, "")
}

func handlesAccessRequests(held []authz.Role) bool {
	for _, r := range held {
		if authz.Can(r, "Own file beyond routine release (NM-SEC-07)", authz.Create) || authz.Can(r, "Own file beyond routine release (NM-SEC-07)", authz.Approve) {
			if r != authz.Employee && r != authz.LineManager {
				return true
			}
		}
	}
	return false
}

// AnswerAccessRequest gives the written outcome. Once answered, the request and its decisions are kept.
func AnswerAccessRequest(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, request uuid.UUID, outcome string, on time.Time) error {
	tag, err := tx.Exec(ctx, "UPDATE nemura.access_request SET state = 'answered', outcome = $2, answered_on = $3, handled_by = $4 WHERE id = $1 AND state IN ('open', 'in_review')",
		request, outcome, day(on), by)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("there is no open access request with that ID")
	}
	return Audit(ctx, tx, org, at, user(by), "nemura.access_request.answered", audit.Object{Type: "access_request", ID: request},
		map[string]audit.Change{"answered_on": {nil, day(on)}}, "")
}

// GiveExtendedNotice records that the person was told in writing, on a day, that the answer will take longer (CMP-PD-04).
func GiveExtendedNotice(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, request uuid.UUID, on time.Time) error {
	tag, err := tx.Exec(ctx, "UPDATE nemura.access_request SET extended_notice_on = $2 WHERE id = $1 AND state <> 'answered'", request, day(on))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("there is no unanswered access request with that ID")
	}
	return Audit(ctx, tx, org, at, user(by), "nemura.access_request.notified", audit.Object{Type: "access_request", ID: request},
		map[string]audit.Change{"extended_notice_on": {nil, day(on)}}, "")
}

// OverdueAccessRequests lists the requests past their due date that are not answered and had no written notice in time, for Checks.
func OverdueAccessRequests(ctx context.Context, tx pgx.Tx, now time.Time) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `SELECT id FROM nemura.access_request WHERE state <> 'answered' AND due_on < $1 AND (extended_notice_on IS NULL OR extended_notice_on > due_on) ORDER BY due_on`, day(now))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// PlaceHold keeps a person's records of a type (or every record of the type) past their retention, with the reason.
func PlaceHold(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by uuid.UUID, recordType string, subject *uuid.UUID, reason string, on time.Time) (uuid.UUID, error) {
	id := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, "INSERT INTO nemura.legal_hold (id, record_type, subject_id, reason, placed_by, placed_on) VALUES ($1, $2, $3, $4, $5, $6)",
		id, recordType, subject, reason, by, day(on)); err != nil {
		return uuid.Nil, err
	}
	return id, Audit(ctx, tx, org, at, user(by), "nemura.legal_hold.placed", audit.Object{Type: "legal_hold", ID: id},
		map[string]audit.Change{"record_type": {nil, recordType}}, reason)
}

// ReleaseHold ends a hold on a day.
func ReleaseHold(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, hold uuid.UUID, on time.Time) error {
	tag, err := tx.Exec(ctx, "UPDATE nemura.legal_hold SET released_on = $2 WHERE id = $1 AND released_on IS NULL", hold, day(on))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("there is no standing hold with that ID")
	}
	return Audit(ctx, tx, org, at, user(by), "nemura.legal_hold.released", audit.Object{Type: "legal_hold", ID: hold},
		map[string]audit.Change{"released_on": {nil, day(on)}}, "")
}

// Holds lists the holds in force on a day.
func Holds(ctx context.Context, tx pgx.Tx, on time.Time) ([]retention.Hold, error) {
	rows, err := tx.Query(ctx, `SELECT record_type, subject_id, placed_on, released_on FROM nemura.legal_hold WHERE placed_on <= $1 AND (released_on IS NULL OR released_on > $1)`, day(on))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []retention.Hold
	for rows.Next() {
		var h retention.Hold
		if err := rows.Scan(&h.RecordType, &h.Subject, &h.From, &h.Until); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// Source is a record type's side of the deletion job: the records of the type that may be due, and how to delete some. A later
// change registers one for each record type it adds; the engine never touches a record any other way.
type Source interface {
	Records(ctx context.Context, tx pgx.Tx) ([]retention.Record, error)
	Delete(ctx context.Context, tx pgx.Tx, ids []uuid.UUID) error
}

// ReportDueDeletions is the nightly job for one record type: it plans what is due and what a hold keeps and, when something is due,
// writes a report for review with its review date. It deletes nothing (never silent: NM-SEC-05).
func ReportDueDeletions(ctx context.Context, tx pgx.Tx, org uuid.UUID, at, now time.Time, p retention.Policy, src Source) (uuid.UUID, retention.Plan, error) {
	records, err := src.Records(ctx, tx)
	if err != nil {
		return uuid.Nil, retention.Plan{}, err
	}
	holds, err := Holds(ctx, tx, now)
	if err != nil {
		return uuid.Nil, retention.Plan{}, err
	}
	plan, err := p.Plan(now, records, holds)
	if err != nil || len(plan.Due) == 0 {
		return uuid.Nil, plan, err
	}
	id := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, "INSERT INTO nemura.deletion_report (id, record_type, due_count, held_count, created_on, review_by) VALUES ($1, $2, $3, $4, $5, $6)",
		id, p.RecordType, len(plan.Due), len(plan.Held), day(now), day(retention.ReviewBy(now))); err != nil {
		return uuid.Nil, plan, err
	}
	return id, plan, Audit(ctx, tx, org, at, audit.Actor{Type: "service", ID: "nemura-worker"}, "nemura.retention.reported", audit.Object{Type: "deletion_report", ID: id},
		map[string]audit.Change{"record_type": {nil, p.RecordType}, "due": {nil, len(plan.Due)}, "held": {nil, len(plan.Held)}}, "")
}

// ReviewDeletionReport records a person's review of a report: they have seen what is due and what a hold keeps. Only then may the
// deletion run.
func ReviewDeletionReport(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, report uuid.UUID, on time.Time) error {
	tag, err := tx.Exec(ctx, "UPDATE nemura.deletion_report SET state = 'reviewed', reviewed_by = $2, reviewed_on = $3 WHERE id = $1 AND state = 'pending'", report, by, day(on))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("there is no pending deletion report with that ID")
	}
	return Audit(ctx, tx, org, at, user(by), "nemura.retention.reviewed", audit.Object{Type: "deletion_report", ID: report},
		map[string]audit.Change{"reviewed_on": {nil, day(on)}}, "")
}

// RunReviewedDeletion deletes the records of a reviewed report that are still due and not held when it runs, one proof each, and
// marks the report deleted. A record that a hold now keeps, or that is no longer due, stays.
func RunReviewedDeletion(ctx context.Context, tx pgx.Tx, org uuid.UUID, at, now time.Time, report uuid.UUID, p retention.Policy, src Source) (int, error) {
	var state string
	if err := tx.QueryRow(ctx, "SELECT state FROM nemura.deletion_report WHERE id = $1 AND record_type = $2", report, p.RecordType).Scan(&state); err != nil {
		return 0, fmt.Errorf("there is no deletion report with that ID for %s: %w", p.RecordType, err)
	}
	if state != "reviewed" {
		return 0, errors.New("the deletion report has not been reviewed: review it first")
	}
	records, err := src.Records(ctx, tx)
	if err != nil {
		return 0, err
	}
	holds, err := Holds(ctx, tx, now)
	if err != nil {
		return 0, err
	}
	plan, err := p.Plan(now, records, holds)
	if err != nil {
		return 0, err
	}
	ids := make([]uuid.UUID, 0, len(plan.Due))
	for _, r := range plan.Due {
		ids = append(ids, r.ID)
		if _, err := tx.Exec(ctx, "INSERT INTO nemura.deletion_proof (id, report_id, record_type, record_id, fingerprint, deleted_on) VALUES ($1, $2, $3, $4, $5, $6)",
			uuid.Must(uuid.NewV7()), report, p.RecordType, r.ID, retention.Fingerprint(p.RecordType, r.ID, r.Content), day(now)); err != nil {
			return 0, err
		}
	}
	if len(ids) > 0 {
		if err := src.Delete(ctx, tx, ids); err != nil {
			return 0, err
		}
	}
	if _, err := tx.Exec(ctx, "UPDATE nemura.deletion_report SET state = 'deleted' WHERE id = $1", report); err != nil {
		return 0, err
	}
	return len(ids), Audit(ctx, tx, org, at, audit.Actor{Type: "service", ID: "nemura-worker"}, "nemura.retention.deleted", audit.Object{Type: "deletion_report", ID: report},
		map[string]audit.Change{"record_type": {nil, p.RecordType}, "deleted": {nil, len(ids)}}, "")
}

// OverdueDeletionReports lists the reports not reviewed by their review date, for Checks (✖).
func OverdueDeletionReports(ctx context.Context, tx pgx.Tx, now time.Time) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, "SELECT id FROM nemura.deletion_report WHERE state = 'pending' AND review_by < $1 ORDER BY review_by", day(now))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// RecordTransferAssessment records an assessment of a flow of personal data to a country outside Singapore, valid until a day.
func RecordTransferAssessment(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by uuid.UUID, flow, country string, assessed, validUntil time.Time) error {
	id := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, "INSERT INTO nemura.transfer_assessment (id, flow, country, assessed_on, valid_until, assessed_by) VALUES ($1, $2, $3, $4, $5, $6)",
		id, flow, country, day(assessed), day(validUntil), by); err != nil {
		return err
	}
	return Audit(ctx, tx, org, at, user(by), "nemura.transfer.assessed", audit.Object{Type: "transfer_assessment", ID: id},
		map[string]audit.Change{"flow": {nil, flow}, "country": {nil, country}, "valid_until": {nil, day(validUntil)}}, "")
}

// TransferAllowed reports whether a flow of personal data to a country outside Singapore has a current assessment on a day; data
// leaves Singapore only after one (NM-SEC-03, CMP-PD-06). Singapore needs none.
func TransferAllowed(ctx context.Context, tx pgx.Tx, flow, country string, on time.Time) (bool, error) {
	if country == "SG" {
		return true, nil
	}
	var ok bool
	err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM nemura.transfer_assessment WHERE flow = $1 AND country = $2 AND assessed_on <= $3 AND valid_until > $3)", flow, country, day(on)).Scan(&ok)
	return ok, err
}
