package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) Allocations() storage.Allocations { return allocations{u} }

type allocations struct{ u *unitOfWork }

// allocationOf is a stored allocation as the port reads it; every row type of an allocation has these fields.
func allocationOf(r sqlcdb.AllocationsOfDocumentRow) storage.Allocation {
	return storage.Allocation{ID: r.ID, SourceID: r.SourceID, SourceVersion: int(r.SourceVersion), DocumentID: r.DocumentID, On: r.OnDate,
		Currency: r.Currency, Amount: storage.Decimal(r.Amount), BaseCurrency: r.BaseCurrency,
		BaseAmount: storage.Decimal(r.BaseAmount), RealisedFX: storage.Decimal(r.RealisedFx), Tolerance: storage.Decimal(r.Tolerance),
		Tax: storage.Decimal(r.Tax), ReversesID: r.ReversesID, WrittenBy: r.WrittenBy, WrittenAt: r.WrittenAt}
}

func (r allocations) Add(ctx context.Context, n storage.NewAllocation) (storage.Allocation, error) {
	if err := r.u.check(); err != nil {
		return storage.Allocation{}, err
	}
	tolerance, tax := string(n.Tolerance), string(n.Tax)
	if tolerance == "" {
		tolerance = "0"
	}
	if tax == "" {
		tax = "0"
	}
	row, err := r.u.q.InsertAllocation(ctx, sqlcdb.InsertAllocationParams{SourceID: n.SourceID, SourceVersion: int32(n.SourceVersion), // #nosec G115 -- a document's version
		DocumentID: n.DocumentID, OnDate: dateOf(n.On), Amount: string(n.Amount), BaseAmount: string(n.BaseAmount), RealisedFx: string(n.RealisedFX), Tolerance: tolerance, Tax: tax,
		ReversesID: n.ReversesID, WrittenBy: r.u.actor(ctx), WrittenAt: r.u.store.clock.Now(), Currency: n.Currency, BaseCurrency: n.BaseCurrency})
	if errors.Is(mapError(err), ports.ErrNotFound) {
		return storage.Allocation{}, &storage.Error{Code: storage.CodeAllocation, Message: fmt.Sprintf("%s or %s is not a currency", n.Currency, n.BaseCurrency)}
	}
	if err != nil {
		return storage.Allocation{}, mapError(err)
	}
	a := allocationOf(sqlcdb.AllocationsOfDocumentRow(row))
	changes := map[string][2]any{"source_id": {nil, a.SourceID.String()}, "source_version": {nil, a.SourceVersion}, "document_id": {nil, a.DocumentID.String()},
		"on": {nil, a.On.Format("2006-01-02")}, "amount": {nil, string(a.Amount) + " " + a.Currency}, "base_amount": {nil, string(a.BaseAmount) + " " + a.BaseCurrency},
		"realised_fx": {nil, string(a.RealisedFX)}}
	if !isZeroText(string(a.Tolerance)) {
		changes["tolerance"] = [2]any{nil, string(a.Tolerance)}
	}
	if !isZeroText(string(a.Tax)) {
		changes["tax"] = [2]any{nil, string(a.Tax)}
	}
	if a.ReversesID != nil {
		changes["reverses_id"] = [2]any{nil, a.ReversesID.String()}
	}
	return a, r.u.event(ctx, "allocation.created", "allocation", a.ID, nil, changes)
}

func (r allocations) OfDocument(ctx context.Context, id uuid.UUID) ([]storage.Allocation, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.AllocationsOfDocument(ctx, id)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.Allocation, 0, len(rows))
	for _, x := range rows {
		out = append(out, allocationOf(x))
	}
	return out, nil
}

func (r allocations) OfSource(ctx context.Context, id uuid.UUID) ([]storage.Allocation, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.AllocationsOfSource(ctx, id)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.Allocation, 0, len(rows))
	for _, x := range rows {
		out = append(out, allocationOf(sqlcdb.AllocationsOfDocumentRow(x)))
	}
	return out, nil
}

func (r allocations) Settled(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]storage.Decimal, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	out := map[uuid.UUID]storage.Decimal{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := r.u.q.SettledOfDocuments(ctx, ids)
	if err != nil {
		return nil, mapError(err)
	}
	for _, x := range rows {
		out[x.DocumentID] = storage.Decimal(x.Settled)
	}
	return out, nil
}

func (r allocations) Tolerance(ctx context.Context, from, to time.Time) (storage.Decimal, error) {
	if err := r.u.check(); err != nil {
		return "", err
	}
	t, err := r.u.q.ToleranceBetween(ctx, sqlcdb.ToleranceBetweenParams{FromDay: dateOf(from), ToDay: dateOf(to)})
	return storage.Decimal(t), mapError(err)
}

// isZeroText says whether a decimal's text is zero: "0", "0.0000", "-0.00".
func isZeroText(s string) bool {
	for _, c := range s {
		if c != '0' && c != '.' && c != '-' {
			return false
		}
	}
	return true
}
