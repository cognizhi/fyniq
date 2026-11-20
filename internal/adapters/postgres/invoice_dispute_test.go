//go:build local

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A dispute or promise (FR-AR-08, P2) is of an invoice; a dispute carries a positive amount and a reason, a promise a day and nothing
// else; at most one open record of each kind per invoice; a record only ends, once, with how it ended, and is never deleted or changed
// otherwise; and another organisation sees none of it.
// req: FR-AR-08
// asvs: V4.2.1
func TestADisputeOrPromiseIsOfAnInvoiceAndOnlyEnds(t *testing.T) {
	ctx := context.Background()
	api := store(t, "fyniq_api")
	books := porttest.NewBooks(t, api, "Dispute Pte Ltd")
	other := porttest.NewBooks(t, api, "Another dispute Pte Ltd")
	at := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	day := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	var invoice storage.Document
	if err := api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		invoice, err = u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentInvoice, Payload: []byte(`{}`)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	amount := func(s string) *storage.Decimal { d := storage.Decimal(s); return &d }
	create := func(n storage.NewInvoiceDispute) (d storage.InvoiceDispute, err error) {
		n.InvoiceID, n.At = invoice.ID, at
		err = api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			d, err = u.InvoiceDisputes().Create(ctx, n)
			return err
		})
		return d, err
	}
	for name, bad := range map[string]storage.NewInvoiceDispute{
		"a dispute with no amount": {Kind: storage.DisputeKind, Currency: "SGD", Reason: "x"},
		"a dispute of nothing":     {Kind: storage.DisputeKind, Amount: amount("0"), Currency: "SGD", Reason: "x"},
		"a dispute with no reason": {Kind: storage.DisputeKind, Amount: amount("5"), Currency: "SGD"},
		"a promise with no day":    {Kind: storage.PromiseKind},
		"a promise with an amount": {Kind: storage.PromiseKind, PromisedOn: &day, Amount: amount("5"), Currency: "SGD"},
		"a kind that is neither":   {Kind: "other", PromisedOn: &day},
	} {
		if _, err := create(bad); err == nil {
			t.Errorf("%s was recorded", name)
		}
	}
	d, err := create(storage.NewInvoiceDispute{Kind: storage.DisputeKind, Amount: amount("40.00"), Currency: "SGD", Reason: "Not delivered"})
	if err != nil || d.Status != "open" || d.Amount == nil || *d.Amount != "40.0000" {
		t.Fatalf("a dispute: %+v, %v", d, err)
	}
	if _, err := create(storage.NewInvoiceDispute{Kind: storage.DisputeKind, Amount: amount("5.00"), Currency: "SGD", Reason: "Again"}); err == nil {
		t.Error("a second open dispute was recorded")
	}
	p, err := create(storage.NewInvoiceDispute{Kind: storage.PromiseKind, PromisedOn: &day})
	if err != nil || p.PromisedOn == nil || !p.PromisedOn.Equal(day) {
		t.Fatalf("a promise: %+v, %v", p, err)
	}
	// It ends once, with how it ended; it is not changed or deleted otherwise.
	if err := api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.InvoiceDisputes().Resolve(ctx, d.ID, "", at, d.Version)
		return err
	}); err == nil {
		t.Error("a dispute was resolved with no words")
	}
	var done storage.InvoiceDispute
	if err := api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		done, err = u.InvoiceDisputes().Resolve(ctx, d.ID, "Credit note issued", at, d.Version)
		return err
	}); err != nil || done.Status != "resolved" || done.ResolvedAt == nil {
		t.Fatalf("resolving: %+v, %v", done, err)
	}
	if err := api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.InvoiceDisputes().Resolve(ctx, d.ID, "Again", at, done.Version)
		return err
	}); err == nil {
		t.Error("a resolved dispute was resolved again")
	}
	// A new open dispute can follow a resolved one.
	if _, err := create(storage.NewInvoiceDispute{Kind: storage.DisputeKind, Amount: amount("10.00"), Currency: "SGD", Reason: "A new one"}); err != nil {
		t.Errorf("a new dispute after a resolved one: %v", err)
	}
	for _, q := range []struct{ role, sql string }{
		{"fyniq_migrator", "UPDATE fyniq.invoice_dispute SET amount = 1"},
		{"fyniq_migrator", "UPDATE fyniq.invoice_dispute SET reason = 'changed'"},
		{"fyniq_migrator", "DELETE FROM fyniq.invoice_dispute"},
		{"fyniq_worker", "UPDATE fyniq.invoice_dispute SET status = 'resolved'"},
		{"fyniq_api", "DELETE FROM fyniq.invoice_dispute"},
	} {
		c := conn(t, q.role)
		if err := asOrg(ctx, c, books.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, q.sql); return err }); err == nil {
			t.Errorf("%s: %s succeeded", q.role, q.sql)
		}
	}
	if err := api.Do(ctx, other.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		if _, err := u.InvoiceDisputes().Get(ctx, d.ID); err == nil {
			t.Error("another organisation read the dispute")
		}
		if open, err := u.InvoiceDisputes().Open(ctx); err != nil || len(open) != 0 {
			t.Errorf("another organisation sees %d open records: %v", len(open), err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
