package nemura

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/features"
	"github.com/cognizhi/fyniq/internal/ports"
)

// FeatureSwitches reads the organisation's feature states; a feature with no row is off (NM-FEAT-01).
func FeatureSwitches(ctx context.Context, tx pgx.Tx) (features.Switches, error) {
	rows, err := tx.Query(ctx, "SELECT feature, state, coalesce(external_system, '') FROM nemura.feature_state")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := features.Switches{}
	for rows.Next() {
		var f, st, sys string
		if err := rows.Scan(&f, &st, &sys); err != nil {
			return nil, err
		}
		out[f] = features.Entry{State: st, System: sys}
	}
	return out, rows.Err()
}

// SetFeature switches a feature, with its audit event, after the dependency rules and the outstanding work are checked (NM-FEAT-02 to 05).
func SetFeature(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by uuid.UUID, id, state, system string, open []features.Outstanding, given []features.Arrangement) error {
	current, err := FeatureSwitches(ctx, tx)
	if err != nil {
		return err
	}
	if err := features.Change(current, id, state, system); err != nil {
		return err
	}
	if state == features.Off || state == features.External {
		if err := features.Settle(open, given); err != nil {
			return err
		}
	}
	was := current[id].State
	if was == "" {
		was = features.Off
	}
	var row uuid.UUID
	err = tx.QueryRow(ctx, "UPDATE nemura.feature_state SET state = $2, external_system = $3, changed_by = $4, changed_at = $5 WHERE feature = $1 RETURNING id", id, state, orNil(system), by, at).Scan(&row)
	if errors.Is(err, pgx.ErrNoRows) {
		row = uuid.Must(uuid.NewV7())
		_, err = tx.Exec(ctx, "INSERT INTO nemura.feature_state (id, feature, state, external_system, changed_by) VALUES ($1, $2, $3, $4, $5)", row, id, state, orNil(system), by)
	}
	if err != nil {
		return err
	}
	changes := map[string]audit.Change{"feature": {nil, id}, "state": {was, state}, "external_system": {nil, system}}
	if len(given) > 0 {
		var list []string
		for _, g := range given {
			list = append(list, g.What+": "+g.Arrangement)
		}
		changes["arrangements"] = audit.Change{nil, list}
	}
	return Audit(ctx, tx, org, at, user(by), "nemura.feature.changed", audit.Object{Type: "feature_state", ID: row}, changes, "")
}

// FeatureService is the feature registry behind the API.
type FeatureService struct {
	Store       *Store
	Clock       ports.Clock
	Outstanders []features.Outstander // each feature's own, as it is built
}

var _ api.Features = FeatureService{}

func (f FeatureService) List(ctx context.Context, s api.Session) (features.Switches, error) {
	var out features.Switches
	err := f.Store.InOrg(ctx, s.Organisation, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = FeatureSwitches(ctx, tx)
		return err
	})
	return out, err
}

func (f FeatureService) Set(ctx context.Context, s api.Session, id, state, system string, given []features.Arrangement) error {
	return f.Store.InOrg(ctx, s.Organisation, func(ctx context.Context, tx pgx.Tx) error {
		var open []features.Outstanding
		if state != features.On {
			for _, o := range f.Outstanders {
				list, err := o.Outstanding(ctx, id)
				if err != nil {
					return err
				}
				open = append(open, list...)
			}
		}
		return SetFeature(ctx, tx, s.Organisation, f.Clock.Now(), s.Member, id, state, system, open, given)
	})
}
