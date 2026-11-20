package nemura

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/ports"
)

// DeletionReportService is the reports of records due for deletion behind the API (NM-SEC-05): a list of what is due and a person's review, which the nightly jobs wait for.
type DeletionReportService struct {
	Store *Store
	Clock ports.Clock
}

func (d DeletionReportService) List(ctx context.Context, s api.Session) (out []api.DeletionReportView, err error) {
	now := d.Clock.Now()
	err = d.Store.InOrg(ctx, s.Organisation, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT r.id, r.record_type, r.due_count, r.held_count, r.created_on, r.review_by, r.state, coalesce(m.display_name, ''), r.reviewed_on
			FROM nemura.deletion_report r LEFT JOIN nemura.member m ON m.organisation_id = r.organisation_id AND m.id = r.reviewed_by ORDER BY r.created_on DESC, r.id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v api.DeletionReportView
			var created, by time.Time
			var on *time.Time
			if err := rows.Scan(&v.ID, &v.RecordType, &v.Due, &v.Held, &created, &by, &v.State, &v.ReviewedBy, &on); err != nil {
				return err
			}
			v.CreatedOn, v.ReviewBy, v.ReviewedOn = day(created), day(by), dayOrEmpty(on)
			v.Overdue = v.State == "pending" && dateOf(now).After(by)
			out = append(out, v)
		}
		return rows.Err()
	})
	return out, err
}

func (d DeletionReportService) Review(ctx context.Context, s api.Session, id uuid.UUID) error {
	now := d.Clock.Now()
	return d.Store.InOrg(ctx, s.Organisation, func(ctx context.Context, tx pgx.Tx) error {
		if err := ReviewDeletionReport(ctx, tx, s.Organisation, now, s.Member, id, now); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return errors.New("There is no such report.")
			}
			return errors.New("That report is reviewed already, or there is no such report: read the page again.")
		}
		return nil
	})
}
