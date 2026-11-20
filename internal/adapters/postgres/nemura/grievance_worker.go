package nemura

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/nemura/grievance"
	"github.com/cognizhi/fyniq/internal/nemura/retention"
)

// The worker's two grievance jobs (PRD-002 NM-SEC-06, NM-SEC-05). The worker holds no table right on grievances: it calls three narrow functions (migration 000064) that return identities, a
// fingerprint and a count, never a word.

// RaiseGrievanceWatch queues a notice for each Owner or HR administrator who is active, not the complainant and not involved, for each grievance that waits (unrouted, or stranded because its
// handler has left), once for each grievance, person and handler. It returns how many notices it queued; a second sweep queues none.
func (s *Store) RaiseGrievanceWatch(ctx context.Context, org uuid.UUID, now time.Time) (int, error) {
	var n int
	err := s.InOrg(ctx, org, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `INSERT INTO nemura.outbox (worker_class, kind, payload, business_key, max_attempts)
SELECT 'worker', 'people.notice', jsonb_build_object('notice', 'grievance_waiting', 'person', p.id, 'change', w.grievance_id), 'grievance-waiting:' || w.grievance_id || ':' || w.notify_member || ':' || w.handler_key, 5
FROM nemura.grievances_waiting($1::date) w JOIN nemura.person p ON p.member_id = w.notify_member
ON CONFLICT (organisation_id, kind, business_key) DO NOTHING`, day(now))
		n = int(tag.RowsAffected())
		return err
	})
	return n, err
}

// grievanceSource is the grievances' side of the retention engine: decided grievances by identity and a fingerprint, deleted only through the purge function, which checks the period and the holds again.
type grievanceSource struct{ now time.Time }

func (g grievanceSource) Records(ctx context.Context, tx pgx.Tx) ([]retention.Record, error) {
	rows, err := tx.Query(ctx, `SELECT id, hold_subject, decided_on, fingerprint FROM nemura.grievances_for_retention()`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []retention.Record
	for rows.Next() {
		var r retention.Record
		if err := rows.Scan(&r.ID, &r.Subject, &r.From, &r.Content); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (g grievanceSource) Delete(ctx context.Context, tx pgx.Tx, ids []uuid.UUID) error {
	_, err := tx.Exec(ctx, `SELECT nemura.purge_grievances($1, $2::date)`, ids, day(g.now))
	return err
}

// GrievanceRetentionPolicy is how long a decided grievance is kept: five years from its outcome [owner to confirm].
var GrievanceRetentionPolicy = retention.Policy{RecordType: grievance.RecordType, MinMonths: grievance.RetentionMonths}

// RunGrievanceRetention is the nightly job: it deletes the grievances of a report a person has reviewed, if they are still due and not held, with a proof of each, then reports what is due now if
// no report is waiting for its review (never silent: NM-SEC-05). It returns how many it deleted and how many reports it made.
func (s *Store) RunGrievanceRetention(ctx context.Context, org uuid.UUID, now time.Time) (deleted, reported int, err error) {
	src := grievanceSource{now: now}
	err = s.InOrg(ctx, org, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id FROM nemura.deletion_report WHERE record_type = $1 AND state = 'reviewed' ORDER BY created_on, id`, grievance.RecordType)
		if err != nil {
			return err
		}
		var reviewed []uuid.UUID
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			reviewed = append(reviewed, id)
		}
		rows.Close()
		for _, id := range reviewed {
			n, err := RunReviewedDeletion(ctx, tx, org, now, now, id, GrievanceRetentionPolicy, src)
			if err != nil {
				return err
			}
			deleted += n
		}
		var pending int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM nemura.deletion_report WHERE record_type = $1 AND state = 'pending'`, grievance.RecordType).Scan(&pending); err != nil || pending > 0 {
			return err
		}
		id, _, err := ReportDueDeletions(ctx, tx, org, now, now, GrievanceRetentionPolicy, src)
		if id != uuid.Nil {
			reported++
		}
		return err
	})
	return deleted, reported, err
}
