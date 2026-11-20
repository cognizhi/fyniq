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

// A subscription contract (FR-AR-15) is of a contact, has a non-negative monthly value and a term that does not end before it starts; it never changes
// but in its version, and never goes; an amendment is append-only, one per contract and day, a cancellation is worth nothing, and it moves the contract's
// version; another organisation sees none of it.
// req: FR-AR-15
// asvs: V4.2.1
func TestASubscriptionContractNeverChangesAndItsAmendmentsOnlyAccumulate(t *testing.T) {
	ctx := context.Background()
	api := store(t, "fyniq_api")
	books := porttest.NewBooks(t, api, "Subscription Pte Ltd")
	other := porttest.NewBooks(t, api, "Another subscription Pte Ltd")
	at := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	start := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	before := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var contact storage.Contact
	if err := api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		contact, err = u.Contacts().Create(ctx, storage.ContactDetails{LegalName: "Lumen Health Pte Ltd", Customer: true, Country: "SG", Currency: "SGD"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	create := func(n storage.NewSubscriptionContract) (c storage.SubscriptionContract, err error) {
		n.At = at
		err = api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			c, err = u.Subscriptions().Create(ctx, n)
			return err
		})
		return c, err
	}
	for name, bad := range map[string]storage.NewSubscriptionContract{
		"no name":               {ContactID: contact.ID, Currency: "SGD", StartsOn: start, MRR: "100"},
		"a negative value":      {ContactID: contact.ID, Name: "Plan", Currency: "SGD", StartsOn: start, MRR: "-1"},
		"an end before start":   {ContactID: contact.ID, Name: "Plan", Currency: "SGD", StartsOn: start, EndsOn: &before, MRR: "100"},
		"a currency not a code": {ContactID: contact.ID, Name: "Plan", Currency: "sgd", StartsOn: start, MRR: "100"},
	} {
		if _, err := create(bad); err == nil {
			t.Errorf("%s was recorded", name)
		}
	}
	c, err := create(storage.NewSubscriptionContract{ContactID: contact.ID, Name: "Growth plan", Currency: "SGD", StartsOn: start, MRR: "100.00"})
	if err != nil || c.Version != 1 || c.MRR != "100.0000" {
		t.Fatalf("a contract: %+v, %v", c, err)
	}
	amend := func(version int, n storage.NewSubscriptionAmendment) (a storage.SubscriptionAmendment, err error) {
		n.ContractID, n.At = c.ID, at
		err = api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			a, err = u.Subscriptions().Amend(ctx, n, version)
			return err
		})
		return a, err
	}
	day := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	if _, err := amend(1, storage.NewSubscriptionAmendment{Kind: storage.AmendCancel, EffectiveOn: day, MRR: "10", Reason: "x"}); err == nil {
		t.Error("a cancellation worth something was recorded")
	}
	if _, err := amend(1, storage.NewSubscriptionAmendment{Kind: "other", EffectiveOn: day, MRR: "10", Reason: "x"}); err == nil {
		t.Error("an amendment of an unknown kind was recorded")
	}
	if _, err := amend(1, storage.NewSubscriptionAmendment{Kind: storage.AmendUpgrade, EffectiveOn: day, MRR: "150", Reason: ""}); err == nil {
		t.Error("an amendment with no reason was recorded")
	}
	a, err := amend(1, storage.NewSubscriptionAmendment{Kind: storage.AmendUpgrade, EffectiveOn: day, MRR: "150.00", Reason: "Larger plan"})
	if err != nil || a.MRR != "150.0000" {
		t.Fatalf("an amendment: %+v, %v", a, err)
	}
	if _, err := amend(1, storage.NewSubscriptionAmendment{Kind: storage.AmendUpgrade, EffectiveOn: day.AddDate(0, 1, 0), MRR: "160", Reason: "Stale"}); err == nil {
		t.Error("an amendment at a stale version was recorded")
	}
	if _, err := amend(2, storage.NewSubscriptionAmendment{Kind: storage.AmendUpgrade, EffectiveOn: day, MRR: "160", Reason: "Same day"}); err == nil {
		t.Error("a second amendment of the same day was recorded")
	}
	if err := api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		got, err := u.Subscriptions().Get(ctx, c.ID)
		if err != nil || got.Version != 2 {
			t.Errorf("the contract after one amendment: %+v, %v", got, err)
		}
		ams, err := u.Subscriptions().Amendments(ctx, c.ID)
		if err != nil || len(ams) != 1 {
			t.Errorf("its amendments: %v, %v", ams, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, q := range []struct{ role, sql string }{
		{"fyniq_migrator", "UPDATE fyniq.subscription_contract SET mrr = 1"},
		{"fyniq_migrator", "UPDATE fyniq.subscription_contract SET name = 'changed'"},
		{"fyniq_migrator", "DELETE FROM fyniq.subscription_contract"},
		{"fyniq_migrator", "UPDATE fyniq.subscription_amendment SET mrr = 1"},
		{"fyniq_migrator", "DELETE FROM fyniq.subscription_amendment"},
		{"fyniq_api", "UPDATE fyniq.subscription_amendment SET reason = 'changed'"},
		{"fyniq_api", "DELETE FROM fyniq.subscription_amendment"},
		{"fyniq_api", "DELETE FROM fyniq.subscription_contract"},
		{"fyniq_worker", "UPDATE fyniq.subscription_contract SET version = 9"},
	} {
		cn := conn(t, q.role)
		if err := asOrg(ctx, cn, books.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, q.sql); return err }); err == nil {
			t.Errorf("%s: %s succeeded", q.role, q.sql)
		}
	}
	if err := api.Do(ctx, other.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		if _, err := u.Subscriptions().Get(ctx, c.ID); err == nil {
			t.Error("another organisation read the contract")
		}
		if all, err := u.Subscriptions().AllAmendments(ctx); err != nil || len(all) != 0 {
			t.Errorf("another organisation sees %d amendments: %v", len(all), err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
