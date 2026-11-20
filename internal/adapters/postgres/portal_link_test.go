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

// A customer portal link (FR-AR-14): only of an active customer; a token's hash alone is kept, 64 hex digits; it lives at most 90 days; it
// changes only in its first opening and its revocation, once each; it is never deleted, and nor is its registry entry; the worker reads and
// cannot write it; and another organisation sees none of it.
// req: FR-AR-14
// asvs: V4.2.1
func TestAPortalLinkIsOfACustomerAndChangesOnlyInItsOpeningAndRevocation(t *testing.T) {
	ctx := context.Background()
	api := store(t, "fyniq_api")
	books := porttest.NewBooks(t, api, "Portal link Pte Ltd")
	other := porttest.NewBooks(t, api, "Another portal Pte Ltd")
	at := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	var customer, vendor storage.Contact
	if err := api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if customer, err = u.Contacts().Create(ctx, storage.ContactDetails{LegalName: "Acme", Customer: true}); err != nil {
			return err
		}
		vendor, err = u.Contacts().Create(ctx, storage.ContactDetails{LegalName: "Vend", Vendor: true})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	hash := func(c byte) string { return string(make([]byte, 0)) + hex64(c) }
	make := func(contact storage.Contact, h string, days int) (l storage.PortalLink, err error) {
		err = api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			l, err = u.PortalLinks().Create(ctx, storage.NewPortalLink{ContactID: contact.ID, Label: "ap@acme.example", TokenHash: h, ExpiresAt: at.AddDate(0, 0, days), At: at})
			return err
		})
		return l, err
	}
	if _, err := make(vendor, hash('a'), 30); err == nil {
		t.Error("a portal link was made for a vendor")
	}
	if _, err := make(customer, "not-a-hash", 30); err == nil {
		t.Error("a portal link was made with a token, not a hash")
	}
	if _, err := make(customer, hash('b'), 91); err == nil {
		t.Error("a portal link of 91 days was made")
	}
	l, err := make(customer, hash('c'), 90)
	if err != nil || l.FirstOpenedAt != nil || l.RevokedAt != nil {
		t.Fatalf("a link of 90 days: %+v, %v", l, err)
	}
	if _, err := make(customer, hash('c'), 30); err == nil {
		t.Error("the same hash was registered twice")
	}
	// It opens once, and is revoked once.
	if err := api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		if _, first, err := u.PortalLinks().Opened(ctx, l.ID, at); err != nil || !first {
			t.Errorf("the first opening: %v %v", first, err)
		}
		if _, first, err := u.PortalLinks().Opened(ctx, l.ID, at.Add(time.Hour)); err != nil || first {
			t.Errorf("a second opening was recorded: %v %v", first, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.PortalLinks().Revoke(ctx, l.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.PortalLinks().Revoke(ctx, l.ID)
		return err
	}); err == nil {
		t.Error("a link was revoked twice")
	}
	// No role changes or deletes a link or its registry entry, and the worker only reads.
	for _, q := range []struct{ role, sql string }{
		{"fyniq_migrator", "UPDATE fyniq.portal_link SET expires_at = expires_at + interval '1 day'"},
		{"fyniq_migrator", "UPDATE fyniq.portal_link SET contact_id = gen_random_uuid()"},
		{"fyniq_migrator", "DELETE FROM fyniq.portal_link"},
		{"fyniq_migrator", "UPDATE fyniq.portal_link_token SET owner = gen_random_uuid()"},
		{"fyniq_migrator", "DELETE FROM fyniq.portal_link_token"},
		{"fyniq_worker", "UPDATE fyniq.portal_link SET revoked_at = NULL, revoked_by = NULL"},
		{"fyniq_api", "DELETE FROM fyniq.portal_link"},
		{"fyniq_api", "SELECT * FROM fyniq.portal_link_token"},
	} {
		c := conn(t, q.role)
		if err := asOrg(ctx, c, books.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, q.sql); return err }); err == nil {
			t.Errorf("%s: %s succeeded", q.role, q.sql)
		}
	}
	// Another organisation sees none of it.
	if err := api.Do(ctx, other.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		if _, err := u.PortalLinks().Get(ctx, l.ID); err == nil {
			t.Error("another organisation read the link")
		}
		if ls, err := u.PortalLinks().OfContact(ctx, customer.ID); err != nil || len(ls) != 0 {
			t.Errorf("another organisation lists %d links: %v", len(ls), err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func hex64(c byte) string {
	b := make([]byte, 64)
	for i := range b {
		b[i] = "0123456789abcdef"[int(c)%16]
	}
	return string(b)
}
