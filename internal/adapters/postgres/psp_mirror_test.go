//go:build local

package postgres_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The Model B mirror (FR-PSP-03, FR-PSP-07): the worker mirrors a provider's customers, subscriptions and invoices; the narrow function alone makes the AR
// document of an invoice, once, from the mirror row and the provider account's revenue account, saved by "psp:invoices", with the customer's contact made
// once; the worker cannot mark an invoice drafted or posted, change its provider, customer or currency, delete a row or make a document; the API can only move
// a drafted invoice on; a mirrored invoice's identity and AR document never change; and another organisation sees none of it.
// req: FR-PSP-03
// req: FR-PSP-07
// asvs: V4.2.1
func TestTheNarrowFunctionMakesTheDraftOfAMirroredInvoiceOnce(t *testing.T) {
	ctx := context.Background()
	api, worker := store(t, "fyniq_api"), store(t, "fyniq_worker")
	books := porttest.NewBooks(t, api, "Mirror Pte Ltd")
	other := porttest.NewBooks(t, api, "Another mirror Pte Ltd")
	provider := pspConnector(t, books.Org)
	at := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	day := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	var clearing storage.MoneyAccount
	if err := api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		acc, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "1062", Name: "Stripe clearing", Type: storage.Asset, AccountAttributes: storage.AccountAttributes{MoneyAccount: true, Currency: "SGD"}})
		if err != nil {
			return err
		}
		if clearing, err = u.Banking().CreateMoneyAccount(ctx, storage.NewMoneyAccount{AccountID: acc.ID, Kind: storage.MoneyPaymentGateway, Name: "Stripe", Identifier: "SGD", Currency: "SGD", At: at}); err != nil {
			return err
		}
		_, err = u.PSP().SaveAccount(ctx, storage.NewPSPAccount{MoneyAccountID: clearing.ID, ConnectorID: provider, FeeAccountID: books.Rent.ID, RevenueAccountID: &books.Sales.ID, At: at}, 0)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	lines, _ := json.Marshal([]map[string]any{{"description": "Analytics", "quantity": "1", "unit_price": "100.00"}})
	in := storage.NewPSPInvoice{ConnectorID: provider, ProviderID: "in_1", ProviderCustomerID: "cus_1", Number: "A-1", Status: "open", Currency: "SGD", Total: "100.00", AmountPaid: "0.00",
		FinalizedOn: &day, Lines: lines, SeenAt: at}
	mirror := func(f func(u storage.UnitOfWork) error) error {
		return worker.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error { return f(u) })
	}
	if err := mirror(func(u storage.UnitOfWork) error {
		if _, _, err := u.PSPMirror().SaveCustomer(ctx, storage.NewPSPCustomer{ConnectorID: provider, ProviderID: "cus_1", Name: "Lumen", Email: "ap@lumen.example", SeenAt: at}); err != nil {
			return err
		}
		if _, _, err := u.PSPMirror().SaveSubscription(ctx, storage.NewPSPSubscription{ConnectorID: provider, ProviderID: "sub_1", ProviderCustomerID: "cus_1", Status: "active", Currency: "SGD",
			Amount: "100.00", IntervalUnit: "month", IntervalCount: 1, SeenAt: at}); err != nil {
			return err
		}
		_, _, err := u.PSPMirror().SaveInvoice(ctx, in)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var doc, contact [16]byte
	if err := mirror(func(u storage.UnitOfWork) error {
		d, c, made, err := u.PSPMirror().MakeDraft(ctx, provider, "in_1")
		doc, contact = d, c
		if err == nil && !made {
			t.Error("the customer's contact was not made")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// Once: a second call, and a call for an invoice that is not mirrored, are refused.
	if err := mirror(func(u storage.UnitOfWork) error {
		_, _, _, err := u.PSPMirror().MakeDraft(ctx, provider, "in_1")
		return err
	}); err == nil {
		t.Error("the draft of a provider invoice was made twice")
	}
	if err := mirror(func(u storage.UnitOfWork) error {
		_, _, _, err := u.PSPMirror().MakeDraft(ctx, provider, "in_missing")
		return err
	}); err == nil {
		t.Error("a draft was made of an invoice that is not mirrored")
	}
	if err := api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, _, _, err := u.PSPMirror().MakeDraft(ctx, provider, "in_1")
		return err
	}); err == nil {
		t.Error("the API called the narrow function")
	}
	if err := api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		i, err := u.PSPMirror().Invoice(ctx, provider, "in_1")
		if err != nil || i.State != storage.PSPInvoiceDrafted || i.DocumentID == nil || [16]byte(*i.DocumentID) != doc {
			t.Errorf("the mirror row: %+v %v", i, err)
		}
		d, err := u.Documents().Get(ctx, *i.DocumentID)
		if err != nil || d.Status != storage.DocumentDraft || d.SavedBy != "psp:invoices" {
			t.Errorf("the document: %+v %v", d, err)
		}
		cust, err := u.PSPMirror().Customer(ctx, provider, "cus_1")
		if err != nil || cust.ContactID == nil || [16]byte(*cust.ContactID) != contact {
			t.Errorf("the customer: %+v %v", cust, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// What the roles may not do to the mirror.
	for _, q := range []struct{ role, sql string }{
		{"fyniq_worker", "UPDATE fyniq.psp_invoice SET state = 'posted'"},
		{"fyniq_worker", "UPDATE fyniq.psp_invoice SET document_id = gen_random_uuid()"},
		{"fyniq_worker", "UPDATE fyniq.psp_invoice SET provider_customer_id = 'cus_2'"},
		{"fyniq_worker", "DELETE FROM fyniq.psp_invoice"},
		{"fyniq_worker", "DELETE FROM fyniq.psp_customer"},
		{"fyniq_worker", "UPDATE fyniq.psp_customer SET contact_id = gen_random_uuid()"},
		{"fyniq_api", "UPDATE fyniq.psp_invoice SET total = 1"},
		{"fyniq_api", "UPDATE fyniq.psp_invoice SET document_id = NULL"},
		{"fyniq_api", "INSERT INTO fyniq.psp_invoice (connector_id, provider_invoice_id, provider_customer_id, status, currency, total, seen_at) VALUES (gen_random_uuid(), 'x', 'y', 'open', 'SGD', 1, now())"},
		{"fyniq_migrator", "UPDATE fyniq.psp_invoice SET currency = 'USD'"},
		{"fyniq_migrator", "DELETE FROM fyniq.psp_subscription"},
		{"fyniq_worker", "INSERT INTO fyniq.document (type) VALUES ('invoice')"},
	} {
		c := conn(t, q.role)
		if err := asOrg(ctx, c, books.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, q.sql); return err }); err == nil {
			t.Errorf("%s: %s succeeded", q.role, q.sql)
		}
	}
	// The API moves a drafted invoice to posted once; another organisation sees none of it.
	if err := api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		i, err := u.PSPMirror().Invoice(ctx, provider, "in_1")
		if err != nil {
			return err
		}
		return u.PSPMirror().Posted(ctx, i.ID)
	}); err != nil {
		t.Fatal(err)
	}
	if err := api.Do(ctx, other.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		if all, err := u.PSPMirror().Invoices(ctx, ""); err != nil || len(all) != 0 {
			t.Errorf("another organisation sees %d invoices: %v", len(all), err)
		}
		if subs, err := u.PSPMirror().Subscriptions(ctx); err != nil || len(subs) != 0 {
			t.Errorf("another organisation sees %d subscriptions: %v", len(subs), err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
