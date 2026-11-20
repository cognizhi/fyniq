package nemura

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/nemura/alert"
)

// RaisePayDayAlerts writes one alert.notify outbox row for each person who holds a recipient role on the day, for each dead pay-day job of
// the organisation, in one transaction (NM-NFR-05). A second call writes nothing new: the business key is the job and the person. A person
// whose role ended, or who was deactivated, is not told. It returns how many rows it wrote.
func (s *Store) RaisePayDayAlerts(ctx context.Context, org uuid.UUID, now time.Time) (int, error) {
	roles := make([]string, len(alert.Recipients))
	for i, r := range alert.Recipients {
		roles[i] = string(r)
	}
	var n int
	err := s.InOrg(ctx, org, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
INSERT INTO nemura.outbox (worker_class, kind, payload, business_key, max_attempts)
SELECT 'worker', $1,
       jsonb_build_object('member', m.id, 'source', d.id, 'kind', d.kind, 'failed_at', d.updated_at),
       'dead:' || d.id || ':' || m.id, $2
FROM nemura.outbox d
CROSS JOIN LATERAL (
  SELECT DISTINCT m.id FROM nemura.member m JOIN nemura.role_assignment a ON a.organisation_id = m.organisation_id AND a.member_id = m.id
  WHERE m.deactivated_at IS NULL AND a.role = ANY($3) AND a.effective_from <= $4 AND (a.effective_to IS NULL OR a.effective_to > $4)
) m
WHERE d.state = 'dead' AND d.kind LIKE $5
ON CONFLICT (organisation_id, kind, business_key) DO NOTHING`, alert.KindNotify, alert.Attempts, roles, now.Format(time.DateOnly), alert.PayDayPrefix+"%")
		n = int(tag.RowsAffected())
		return err
	})
	return n, err
}

// RaiseLinkAlerts tells the Payroll administrator and the Owner, once for each oldest stuck event and person, that events for Fyniq are not getting through (FR-PPL-08, NM-INT-04): after an hour while an
// approved pay run still waits for Fyniq's posting (the alert near pay day, within the NM-NFR-05 window), and after 24 hours at any time. An event is stuck while it is queued or failing. Nothing is raised
// for a link that is not active. A second sweep writes nothing new: the business key is the kind, the oldest stuck event and the person. It returns how many rows it wrote.
func (s *Store) RaiseLinkAlerts(ctx context.Context, org uuid.UUID, now time.Time) (int, error) {
	roles := make([]string, len(alert.Recipients))
	for i, r := range alert.Recipients {
		roles[i] = string(r)
	}
	var n int
	err := s.InOrg(ctx, org, func(ctx context.Context, tx pgx.Tx) error {
		l, err := LiveLink(ctx, tx)
		if noLink(err) || (err == nil && l.State != "active") {
			return nil
		}
		if err != nil {
			return err
		}
		var oldest uuid.UUID
		var since time.Time
		if err := tx.QueryRow(ctx, `SELECT id, queued_at FROM nemura.link_event WHERE link_id = $1 AND state IN ('queued', 'failed') ORDER BY sequence LIMIT 1`, l.ID).Scan(&oldest, &since); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			return err
		}
		waiting := false // an approved version no posting of Fyniq's answers yet
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM nemura.pay_run_version v WHERE v.approved_by IS NOT NULL AND v.state <> 'superseded'
AND NOT EXISTS (SELECT 1 FROM nemura.link_run_posting p WHERE p.run_id = v.run_id AND p.version = v.version AND p.status = 'posted'))`).Scan(&waiting); err != nil {
			return err
		}
		kinds := []string{}
		if waiting && !since.After(now.Add(-time.Hour)) {
			kinds = append(kinds, alert.KindLinkNearPayDay)
		}
		if !since.After(now.Add(-24 * time.Hour)) {
			kinds = append(kinds, alert.KindLink24Hours)
		}
		for _, kind := range kinds {
			tag, err := tx.Exec(ctx, `
INSERT INTO nemura.outbox (worker_class, kind, payload, business_key, max_attempts)
SELECT 'worker', $1, jsonb_build_object('member', m.id, 'source', $5::uuid, 'kind', $6::text, 'failed_at', $7::timestamptz),
       'link:' || $6::text || ':' || $5::text || ':' || m.id, $2
FROM (
  SELECT DISTINCT m.id FROM nemura.member m JOIN nemura.role_assignment a ON a.organisation_id = m.organisation_id AND a.member_id = m.id
  WHERE m.deactivated_at IS NULL AND a.role = ANY($3) AND a.effective_from <= $4 AND (a.effective_to IS NULL OR a.effective_to > $4)
) m
ON CONFLICT (organisation_id, kind, business_key) DO NOTHING`, alert.KindNotify, alert.Attempts, roles, now.Format(time.DateOnly), oldest, kind, since)
			if err != nil {
				return err
			}
			n += int(tag.RowsAffected())
		}
		return nil
	})
	return n, err
}
