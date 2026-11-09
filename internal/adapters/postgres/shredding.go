package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The shredding of person keys at the end of retention and the anonymisation of contacts (§11.8, §11.9; migration
// 000309, designs D9 and D10).

func toShreddingRun(r sqlcdb.ListShreddingRunsRow) storage.ShreddingRun {
	return storage.ShreddingRun{ID: r.ID, Cohort: r.Cohort, Keys: r.Keys, ListedAt: r.ListedAt, Status: r.Status, ApprovedBy: str(r.ApprovedBy),
		ApprovedAt: r.ApprovedAt, Register: str(r.Register), DoneAt: r.DoneAt, Reapplied: r.Reapplied, Version: int(r.Version)}
}

func (c compliance) ShreddingRuns(ctx context.Context) ([]storage.ShreddingRun, error) {
	if err := c.u.check(); err != nil {
		return nil, err
	}
	rows, err := c.u.q.ListShreddingRuns(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.ShreddingRun, 0, len(rows))
	for _, r := range rows {
		out = append(out, toShreddingRun(r))
	}
	return out, nil
}

func (c compliance) DueKeyCohorts(ctx context.Context, before time.Time) ([]storage.KeyCohort, error) {
	if err := c.u.check(); err != nil {
		return nil, err
	}
	rows, err := c.u.q.DueKeyCohorts(ctx, before)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.KeyCohort, 0, len(rows))
	for _, r := range rows {
		out = append(out, storage.KeyCohort{Cohort: r.Cohort, Keys: r.Keys})
	}
	return out, nil
}

func (c compliance) ListShreddingRun(ctx context.Context, cohort time.Time, keys []uuid.UUID, at time.Time) (storage.ShreddingRun, error) {
	if err := c.u.check(); err != nil {
		return storage.ShreddingRun{}, err
	}
	row, err := c.u.q.InsertShreddingRun(ctx, sqlcdb.InsertShreddingRunParams{Cohort: cohort, Keys: keys, ListedAt: at})
	if err != nil {
		return storage.ShreddingRun{}, mapError(err)
	}
	r := toShreddingRun(sqlcdb.ListShreddingRunsRow(row))
	v := r.Version
	return r, c.u.event(ctx, "pdpa.shredding_listed", "pd_shredding_run", r.ID, &v, map[string][2]any{"cohort": {nil, cohort.Format(time.DateOnly)},
		"keys": {nil, len(keys)}})
}

func (c compliance) ApproveShreddingRun(ctx context.Context, id uuid.UUID, version int, by string, at time.Time) (storage.ShreddingRun, error) {
	if err := c.u.check(); err != nil {
		return storage.ShreddingRun{}, err
	}
	row, err := c.u.q.ApproveShreddingRun(ctx, sqlcdb.ApproveShreddingRunParams{ID: id, Version: int32(min(max(version, 0), 1<<30)), // #nosec G115 -- bounded
		ApprovedBy: &by, ApprovedAt: &at})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.ShreddingRun{}, &storage.Error{Code: storage.CodeShredding, Message: "the shredding run is not listed at that version",
			Hint: "Read the run again: it may be approved already"}
	}
	if err != nil {
		return storage.ShreddingRun{}, mapError(err)
	}
	r := toShreddingRun(sqlcdb.ListShreddingRunsRow(row))
	v := r.Version
	return r, c.u.event(ctx, "pdpa.shredding_approved", "pd_shredding_run", r.ID, &v, map[string][2]any{"status": {"listed", "approved"},
		"cohort": {nil, r.Cohort.Format(time.DateOnly)}, "keys": {nil, len(r.Keys)}})
}

func (c compliance) ShredRun(ctx context.Context, id uuid.UUID, register string) (int, error) {
	if err := c.u.check(); err != nil {
		return 0, err
	}
	if err := c.u.q.RegisterShreddingRun(ctx, sqlcdb.RegisterShreddingRunParams{ID: id, Register: &register}); err != nil {
		return 0, mapError(err)
	}
	n, err := c.u.q.ShredPersonKeys(ctx, id)
	if err != nil {
		return 0, mapError(err)
	}
	return int(n), c.u.event(ctx, "pdpa.keys_shredded", "pd_shredding_run", id, nil, map[string][2]any{"status": {"approved", "done"},
		"register": {nil, register}, "shredded": {nil, n}})
}

func (c compliance) ReapplyShredding(ctx context.Context, r storage.ShreddingRun) (int, error) {
	if err := c.u.check(); err != nil {
		return 0, err
	}
	approvedAt := time.Time{}
	if r.ApprovedAt != nil {
		approvedAt = *r.ApprovedAt
	}
	n, err := c.u.q.ReapplyShredding(ctx, sqlcdb.ReapplyShreddingParams{ID: r.ID, Cohort: r.Cohort, Keys: r.Keys, ListedAt: r.ListedAt,
		ApprovedBy: r.ApprovedBy, ApprovedAt: approvedAt, Register: r.Register})
	if err != nil {
		return 0, mapError(err)
	}
	if n == 0 {
		return 0, nil
	}
	return int(n), c.u.event(ctx, "pdpa.keys_shredded", "pd_shredding_run", r.ID, nil, map[string][2]any{"register": {nil, r.Register},
		"shredded": {nil, n}, "reapplied": {nil, true}})
}

func (c compliance) Anonymisations(ctx context.Context) ([]storage.Anonymisation, error) {
	if err := c.u.check(); err != nil {
		return nil, err
	}
	rows, err := c.u.q.ListAnonymisations(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.Anonymisation, 0, len(rows))
	for _, r := range rows {
		out = append(out, storage.Anonymisation{ID: r.ID, ContactID: r.ContactID, RequestID: r.RequestID, Reason: r.Reason, Register: r.Register,
			AnonymisedBy: r.AnonymisedBy, AnonymisedAt: r.AnonymisedAt, Reapplied: r.Reapplied})
	}
	return out, nil
}

func (c compliance) Anonymise(ctx context.Context, a storage.Anonymisation) (storage.Anonymisation, error) {
	if err := c.u.check(); err != nil {
		return storage.Anonymisation{}, err
	}
	var err error
	if a.Reapplied {
		a.ID, err = c.u.q.InsertReappliedAnonymisation(ctx, sqlcdb.InsertReappliedAnonymisationParams{ContactID: a.ContactID, RequestID: a.RequestID,
			Reason: a.Reason, Register: a.Register, AnonymisedBy: a.AnonymisedBy, AnonymisedAt: a.AnonymisedAt})
	} else {
		a.ID, err = c.u.q.InsertAnonymisation(ctx, sqlcdb.InsertAnonymisationParams{ContactID: a.ContactID, RequestID: a.RequestID, Reason: a.Reason,
			Register: a.Register, AnonymisedBy: a.AnonymisedBy, AnonymisedAt: a.AnonymisedAt})
	}
	if err != nil {
		return storage.Anonymisation{}, mapError(err)
	}
	if err := c.u.event(ctx, "pd_anonymisation.recorded", "pd_anonymisation", a.ID, nil, map[string][2]any{"contact_id": {nil, a.ContactID},
		"reason": {nil, a.Reason}, "register": {nil, a.Register}, "reapplied": {nil, a.Reapplied}}); err != nil {
		return storage.Anonymisation{}, err
	}
	changed, err := c.u.q.AnonymiseContact(ctx, a.ContactID)
	if err != nil {
		return storage.Anonymisation{}, mapError(err)
	}
	if changed > 0 {
		// The fields only, never their values: the old ones stay sealed in the events that recorded them.
		if err := c.u.event(ctx, "contact.anonymised", "contact", a.ContactID, nil, map[string][2]any{
			"anonymised": {nil, []any{"billing_email", "remittance_email", "phone", "address"}}, "anonymisation_id": {nil, a.ID}}); err != nil {
			return storage.Anonymisation{}, err
		}
	}
	return a, nil
}
