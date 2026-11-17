package postgres

import (
	"context"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) Help() storage.HelpStates { return helpStates{u} }

type helpStates struct{ u *unitOfWork }

func (r helpStates) Set(ctx context.Context, s storage.HelpState) (storage.HelpState, error) {
	if err := r.u.check(); err != nil {
		return storage.HelpState{}, err
	}
	row, err := r.u.q.InsertUserHelpState(ctx, sqlcdb.InsertUserHelpStateParams{UserID: s.UserID, Tour: s.Tour, Step: int16(min(max(s.Step, 0), 6)), SetAt: r.u.store.clock.Now()}) // #nosec G115 -- capped
	if err != nil {
		return storage.HelpState{}, mapError(err)
	}
	out := storage.HelpState{UserID: row.UserID, Tour: row.Tour, Step: int(row.Step), SetAt: row.SetAt}
	if err := r.u.event(ctx, "user_help_state.set", "user_help_state", row.ID, nil, map[string][2]any{
		"tour": {nil, out.Tour}, "step": {nil, out.Step}}); err != nil {
		return storage.HelpState{}, err
	}
	return out, nil
}

func (r helpStates) Latest(ctx context.Context, user uuid.UUID) (storage.HelpState, bool, error) {
	if err := r.u.check(); err != nil {
		return storage.HelpState{}, false, err
	}
	rows, err := r.u.q.LatestUserHelpState(ctx, user)
	if err != nil {
		return storage.HelpState{}, false, mapError(err)
	}
	if len(rows) == 0 {
		return storage.HelpState{}, false, nil
	}
	return storage.HelpState{UserID: rows[0].UserID, Tour: rows[0].Tour, Step: int(rows[0].Step), SetAt: rows[0].SetAt}, true, nil
}
