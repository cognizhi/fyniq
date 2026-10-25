//go:build local

package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// capture runs fn in a unit of work of org as the API.
func capture(t *testing.T, org uuid.UUID, fn func(ctx context.Context, c storage.CaptureAddresses) error) error {
	t.Helper()
	return store(t, "fyniq_api").Do(context.Background(), org, func(ctx context.Context, u storage.UnitOfWork) error {
		return fn(ctx, u.CaptureAddresses())
	})
}

// An organisation's email-in address and a member's own each route to their organisation, before any tenant is set;
// a new one retires the one it replaces; a name belongs to one organisation for good, which may take it back after
// retiring it, while another may never (FY183).
// req: FR-INT-22
func TestAnEmailInAddressNameBelongsToOneOrganisation(t *testing.T) {
	ctx := context.Background()
	a, b := books(t, "Capture A Pte Ltd"), books(t, "Capture B Pte Ltd")
	api := store(t, "fyniq_api")
	member := founder(t, api, a.Org, 41, clock.System{}.Now())
	name := "bills-" + uuid.NewString()[:8]
	var org, mine storage.CaptureAddress
	if err := capture(t, a.Org, func(ctx context.Context, c storage.CaptureAddresses) error {
		var err error
		if org, err = c.Create(ctx, name, nil, storage.CaptureDocuments); err != nil {
			return err
		}
		mine, err = c.Create(ctx, "Mei."+name, &member, storage.CaptureDocuments)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if r, err := api.CaptureRoute(ctx, name); err != nil || r.Organisation != a.Org || r.Address != org.ID || r.UserID != nil {
		t.Fatalf("the organisation's route: %+v, %v", r, err)
	}
	if r, err := api.CaptureRoute(ctx, "MEI."+name); err != nil || r.Address != mine.ID || r.UserID == nil || *r.UserID != member || mine.LocalPart != "mei."+name {
		t.Fatalf("the member's route: %+v, %v (%s)", r, err, mine.LocalPart)
	}
	err := capture(t, b.Org, func(ctx context.Context, c storage.CaptureAddresses) error {
		_, err := c.Create(ctx, name, nil, storage.CaptureDocuments)
		return err
	})
	if storage.CodeOf(err) != storage.CodeCaptureName {
		t.Fatalf("another organisation took the name: %v", err)
	}
	// A new name retires the old one, whose mail no longer routes; then the organisation takes its old name back.
	if err := capture(t, a.Org, func(ctx context.Context, c storage.CaptureAddresses) error {
		_, err := c.Create(ctx, "invoices-"+name, nil, storage.CaptureDocuments)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := api.CaptureRoute(ctx, name); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("a retired name still routes: %v", err)
	}
	err = capture(t, b.Org, func(ctx context.Context, c storage.CaptureAddresses) error {
		_, err := c.Create(ctx, name, nil, storage.CaptureDocuments)
		return err
	})
	if storage.CodeOf(err) != storage.CodeCaptureName {
		t.Fatalf("another organisation took a retired name: %v", err)
	}
	if err := capture(t, a.Org, func(ctx context.Context, c storage.CaptureAddresses) error {
		live, err := c.Create(ctx, name, nil, storage.CaptureDocuments)
		if err != nil {
			return err
		}
		all, err := c.Live(ctx)
		if err != nil || len(all) != 2 || all[0].ID != live.ID || all[1].ID != mine.ID {
			t.Errorf("the live addresses: %+v, %v", all, err)
		}
		return nil
	}); err != nil {
		t.Fatalf("taking its own name back: %v", err)
	}
	if got := actions(t, a.Org, org.ID); len(got) != 2 || got[0] != "capture_address.created" || got[1] != "capture_address.retired" {
		t.Fatalf("the first address's audit trail: %v", got)
	}
}

// An address is only ever retired, once (FY182); no role reads or writes the name registry but through its trigger
// and capture_route; a sender decision never changes or goes (FY184), the latest in force.
// req: FR-INT-22
func TestEmailInAddressesAndSenderDecisionsKeepTheirHistory(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Capture record Pte Ltd")
	var addr storage.CaptureAddress
	var first storage.CaptureSender
	if err := capture(t, b.Org, func(ctx context.Context, c storage.CaptureAddresses) error {
		var err error
		if addr, err = c.Create(ctx, "record-"+uuid.NewString()[:8], nil, storage.CaptureDocuments); err != nil {
			return err
		}
		if first, err = c.Decide(ctx, "Supplier@Example.com", nil, storage.CaptureDocuments, storage.SenderRejected); err != nil {
			return err
		}
		if _, err = c.Decide(ctx, "supplier@example.com", nil, storage.CaptureDocuments, storage.SenderAccepted); err != nil {
			return err
		}
		ds, err := c.Decisions(ctx)
		if err != nil || len(ds) != 1 || ds[0].Decision != storage.SenderAccepted || ds[0].Address != "supplier@example.com" {
			t.Errorf("the decisions in force: %+v, %v", ds, err)
		}
		if _, err := c.Retire(ctx, addr.ID); err != nil {
			return err
		}
		if _, err := c.Retire(ctx, addr.ID); storage.CodeOf(err) != storage.CodeVersion {
			t.Errorf("a second retirement: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, x := range []struct{ role, what, stmt string }{
		{"fyniq_migrator", "rename an address as the owner", "UPDATE fyniq.capture_address SET local_part = 'other' WHERE id = $1"},
		{"fyniq_migrator", "delete an address as the owner", "DELETE FROM fyniq.capture_address WHERE id = $1"},
		{"fyniq_migrator", "retire an address twice as the owner", "UPDATE fyniq.capture_address SET retired_at = now() WHERE id = $1"},
		{"fyniq_worker", "make an address as the worker", "INSERT INTO fyniq.capture_address (local_part, created_by, created_at) SELECT 'w-' || $1::text, 'service:worker', now()"},
		{"fyniq_api", "read the name registry", "SELECT owner FROM fyniq.capture_name WHERE local_part = $1::text"},
		{"fyniq_api", "write the name registry", "INSERT INTO fyniq.capture_name (local_part, owner) SELECT $1::text, fyniq.current_org()"},
	} {
		err := asOrg(ctx, conn(t, x.role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, x.stmt, addr.ID.String()); return err })
		wantState(t, x.what, err, "FY182", "42501")
	}
	for _, x := range []struct{ role, what, stmt string }{
		{"fyniq_migrator", "change a decision as the owner", "UPDATE fyniq.capture_sender SET decision = 'accepted' WHERE id = $1"},
		{"fyniq_migrator", "delete a decision as the owner", "DELETE FROM fyniq.capture_sender WHERE id = $1"},
		{"fyniq_worker", "decide as the worker", "INSERT INTO fyniq.capture_sender (address, decision, decided_by, decided_at) SELECT 'x@example.com', 'accepted', 'service:worker', now() WHERE $1::uuid IS NOT NULL"},
	} {
		err := asOrg(ctx, conn(t, x.role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, x.stmt, first.ID); return err })
		wantState(t, x.what, err, "FY184", "42501")
	}
}
