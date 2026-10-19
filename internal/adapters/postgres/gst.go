package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) GSTRegistrations() storage.GSTRegistrations { return gstRegistrations{u} }

type gstRegistrations struct{ u *unitOfWork }

func gstOf(r sqlcdb.ListGSTRegistrationsRow) storage.GSTRegistration {
	return storage.GSTRegistration{ID: r.ID, RegisteredFrom: r.RegisteredFrom, DeregisteredFrom: nullDate(r.DeregisteredFrom),
		GSTNumber: r.GstNumber, ReturnFrequency: r.ReturnFrequency, QuarterStartMonth: int(r.QuarterStartMonth),
		AccountingBasis: r.AccountingBasis, Version: int(r.Version), RecordedAt: r.RecordedAt}
}

func (r gstRegistrations) List(ctx context.Context) ([]storage.GSTRegistration, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ListGSTRegistrations(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.GSTRegistration, len(rows))
	for i, row := range rows {
		out[i] = gstOf(row)
	}
	return out, nil
}

func (r gstRegistrations) Get(ctx context.Context, id uuid.UUID) (storage.GSTRegistration, error) {
	if err := r.u.check(); err != nil {
		return storage.GSTRegistration{}, err
	}
	row, err := r.u.q.GetGSTRegistration(ctx, id)
	if err != nil {
		return storage.GSTRegistration{}, mapError(err)
	}
	return gstOf(sqlcdb.ListGSTRegistrationsRow(row)), nil
}

// quarterStart is the quarters' start month as the column holds it, refused outside 1 to 3.
func quarterStart(m int) (int16, error) {
	if m < 1 || m > 3 {
		return 0, &storage.Error{Code: storage.CodeOrganisation, Message: fmt.Sprintf("%d is not a quarters' start month: 1, 2 or 3", m)}
	}
	return int16(m), nil // 1 to 3, checked above
}

func (r gstRegistrations) Create(ctx context.Context, n storage.NewGSTRegistration) (storage.GSTRegistration, error) {
	if err := r.u.check(); err != nil {
		return storage.GSTRegistration{}, err
	}
	q, err := quarterStart(n.QuarterStartMonth)
	if err != nil {
		return storage.GSTRegistration{}, err
	}
	row, err := r.u.q.InsertGSTRegistration(ctx, sqlcdb.InsertGSTRegistrationParams{RegisteredFrom: n.RegisteredFrom,
		DeregisteredFrom: pgDate(n.DeregisteredFrom), GstNumber: n.GSTNumber, ReturnFrequency: n.ReturnFrequency,
		QuarterStartMonth: q, AccountingBasis: n.AccountingBasis, RecordedAt: n.RecordedAt})
	if err != nil {
		return storage.GSTRegistration{}, mapError(err)
	}
	g := gstOf(sqlcdb.ListGSTRegistrationsRow(row))
	if err := r.u.event(ctx, "gst_registration.created", "gst_registration", g.ID, &g.Version, gstChanges(storage.GSTRegistration{}, g)); err != nil {
		return storage.GSTRegistration{}, err
	}
	return g, nil
}

func (r gstRegistrations) Update(ctx context.Context, id uuid.UUID, version int, n storage.NewGSTRegistration) (storage.GSTRegistration, error) {
	if err := r.u.check(); err != nil {
		return storage.GSTRegistration{}, err
	}
	q, err := quarterStart(n.QuarterStartMonth)
	if err != nil {
		return storage.GSTRegistration{}, err
	}
	cur, err := r.u.q.LockGSTRegistration(ctx, id)
	if err != nil {
		return storage.GSTRegistration{}, mapError(err)
	}
	before := gstOf(sqlcdb.ListGSTRegistrationsRow(cur))
	if before.Version != version {
		return storage.GSTRegistration{}, &storage.Error{Code: storage.CodeVersion,
			Message: fmt.Sprintf("the GST registration changed since it was read: it is at version %d, not %d", before.Version, version)}
	}
	row, err := r.u.q.UpdateGSTRegistration(ctx, sqlcdb.UpdateGSTRegistrationParams{ID: id, RegisteredFrom: n.RegisteredFrom,
		DeregisteredFrom: pgDate(n.DeregisteredFrom), GstNumber: n.GSTNumber, ReturnFrequency: n.ReturnFrequency,
		QuarterStartMonth: q, AccountingBasis: n.AccountingBasis, RecordedAt: n.RecordedAt})
	if err != nil {
		return storage.GSTRegistration{}, mapError(err)
	}
	after := gstOf(sqlcdb.ListGSTRegistrationsRow(row))
	changes := gstChanges(before, after)
	changes["version"] = [2]any{before.Version, after.Version}
	if err := r.u.event(ctx, "gst_registration.updated", "gst_registration", after.ID, &after.Version, changes); err != nil {
		return storage.GSTRegistration{}, err
	}
	return after, nil
}

// gstChanges are the fields that differ; from a zero registration, every field.
func gstChanges(a, b storage.GSTRegistration) map[string][2]any {
	out := map[string][2]any{}
	text := func(k, x, y string) {
		if x != y {
			out[k] = [2]any{nilIfEmpty(x), nilIfEmpty(y)}
		}
	}
	from := func(t storage.GSTRegistration) string {
		if t.RegisteredFrom.IsZero() {
			return ""
		}
		return day(t.RegisteredFrom)
	}
	text("registered_from", from(a), from(b))
	text("deregistered_from", dayOf(a.DeregisteredFrom), dayOf(b.DeregisteredFrom))
	text("gst_number", a.GSTNumber, b.GSTNumber)
	text("return_frequency", a.ReturnFrequency, b.ReturnFrequency)
	text("accounting_basis", a.AccountingBasis, b.AccountingBasis)
	if a.QuarterStartMonth != b.QuarterStartMonth {
		out["quarter_start_month"] = [2]any{a.QuarterStartMonth, b.QuarterStartMonth}
	}
	return out
}
