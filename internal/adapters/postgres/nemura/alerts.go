package nemura

import (
	"context"
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
