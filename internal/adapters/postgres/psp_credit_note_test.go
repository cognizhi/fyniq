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

// A provider's credit note (FR-PSP-07; rev 0.601): the worker mirrors it and may only park it; the narrow function alone makes its AR credit note, and only against
// the AR invoice of the provider invoice it credits once that is issued, never for a credit note already made, a void one, or one the API asks for; the worker cannot
// mark a credit note drafted or posted, change its provider, invoice or currency, set its document, delete it or make a document; the API can only move a drafted one
// on; and another organisation sees none of it. The function's whole path (the invoice issued, the draft made once, issued only at the provider's total) is
// TestAProviderCreditNoteBecomesADraftOfItsInvoiceAndPostsOnlyAtTheProvidersTotal in the API's tests.
// req: FR-PSP-07
// asvs: V4.2.1
func TestAProviderCreditNoteIsMirroredByTheWorkerAndDraftedOnlyByTheNarrowFunction(t *testing.T) {
	ctx := context.Background()
	api, worker := store(t, "fyniq_api"), store(t, "fyniq_worker")
	books := porttest.NewBooks(t, api, "Credit mirror Pte Ltd")
	other := porttest.NewBooks(t, api, "Another credit mirror Pte Ltd")
	provider := pspConnector(t, books.Org)
	at := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	day := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	if err := api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		acc, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "1062", Name: "Stripe clearing", Type: storage.Asset, AccountAttributes: storage.AccountAttributes{MoneyAccount: true, Currency: "SGD"}})
		if err != nil {
			return err
		}
		m, err := u.Banking().CreateMoneyAccount(ctx, storage.NewMoneyAccount{AccountID: acc.ID, Kind: storage.MoneyPaymentGateway, Name: "Stripe", Identifier: "SGD", Currency: "SGD", At: at})
		if err != nil {
			return err
		}
		_, err = u.PSP().SaveAccount(ctx, storage.NewPSPAccount{MoneyAccountID: m.ID, ConnectorID: provider, FeeAccountID: books.Rent.ID, RevenueAccountID: &books.Sales.ID, At: at}, 0)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	lines, _ := json.Marshal([]map[string]any{{"description": "Credit", "quantity": "1", "unit_price": "30.00"}})
	cn := storage.NewPSPCreditNote{ConnectorID: provider, ProviderID: "cn_1", ProviderInvoiceID: "in_1", Number: "A-1-CN", Status: "issued", Currency: "SGD", Total: "30.00",
		Reason: "order_change", IssuedOn: &day, Lines: lines, SeenAt: at}
	worked := func(role storage.Store, f func(u storage.UnitOfWork) error) error {
		return role.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error { return f(u) })
	}
	if err := worked(worker, func(u storage.UnitOfWork) error {
		_, made, err := u.PSPMirror().SaveCreditNote(ctx, cn)
		if err == nil && !made {
			t.Error("the credit note was not mirrored")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := worked(worker, func(u storage.UnitOfWork) error { // the same credit note again changes nothing
		_, changed, err := u.PSPMirror().SaveCreditNote(ctx, cn)
		if err == nil && changed {
			t.Error("a second report changed the mirror")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// Its invoice is not mirrored, so the narrow function makes nothing; nor does it for the API.
	for _, who := range []storage.Store{worker, api} {
		if err := worked(who, func(u storage.UnitOfWork) error {
			_, err := u.PSPMirror().MakeCreditDraft(ctx, provider, "cn_1")
			return err
		}); err == nil {
			t.Error("a credit note was made into a document with no issued invoice behind it")
		}
	}
	if err := worked(worker, func(u storage.UnitOfWork) error {
		_, err := u.PSPMirror().MakeCreditDraft(ctx, provider, "cn_missing")
		return err
	}); err == nil {
		t.Error("a draft was made of a credit note that is not mirrored")
	}
	// What the roles may not do to the mirror.
	for _, q := range []struct{ role, sql string }{
		{"fyniq_worker", "UPDATE fyniq.psp_credit_note SET state = 'posted'"},
		{"fyniq_worker", "UPDATE fyniq.psp_credit_note SET state = 'drafted'"},
		{"fyniq_worker", "UPDATE fyniq.psp_credit_note SET document_id = gen_random_uuid()"},
		{"fyniq_worker", "UPDATE fyniq.psp_credit_note SET provider_invoice_id = 'in_2'"},
		{"fyniq_worker", "DELETE FROM fyniq.psp_credit_note"},
		{"fyniq_api", "UPDATE fyniq.psp_credit_note SET total = 1"},
		{"fyniq_api", "UPDATE fyniq.psp_credit_note SET document_id = NULL"},
		{"fyniq_api", "INSERT INTO fyniq.psp_credit_note (connector_id, provider_credit_note_id, provider_invoice_id, status, currency, total, seen_at) VALUES (gen_random_uuid(), 'x', 'y', 'issued', 'SGD', 1, now())"},
		{"fyniq_migrator", "UPDATE fyniq.psp_credit_note SET currency = 'USD'"},
		{"fyniq_migrator", "DELETE FROM fyniq.psp_credit_note"},
		{"fyniq_worker", "INSERT INTO fyniq.document (type) VALUES ('credit_note')"},
		{"fyniq_worker", "TRUNCATE fyniq.psp_credit_note"},
	} {
		c := conn(t, q.role)
		if err := asOrg(ctx, c, books.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, q.sql); return err }); err == nil {
			t.Errorf("%s: %s succeeded", q.role, q.sql)
		}
	}
	// The worker parks it; a parked credit note is never made into a document; the API cannot park, move or post it before it is drafted.
	var id [16]byte
	if err := worked(worker, func(u storage.UnitOfWork) error {
		c, err := u.PSPMirror().CreditNote(ctx, provider, "cn_1")
		id = c.ID
		if err != nil {
			return err
		}
		return u.PSPMirror().ParkCreditNote(ctx, c.ID, "Not made into a credit note: a test (FR-PSP-07).")
	}); err != nil {
		t.Fatal(err)
	}
	if err := worked(worker, func(u storage.UnitOfWork) error {
		_, err := u.PSPMirror().MakeCreditDraft(ctx, provider, "cn_1")
		return err
	}); err == nil {
		t.Error("a parked credit note was made into a document")
	}
	if err := api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error { return u.PSPMirror().CreditNotePosted(ctx, id) }); err != nil {
		t.Logf("posting a parked credit note: %v", err) // no row moves: the state must be drafted
	}
	if err := api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		c, err := u.PSPMirror().CreditNote(ctx, provider, "cn_1")
		if err != nil || c.State != storage.PSPInvoiceParked || c.DocumentID != nil {
			t.Errorf("the credit note after the attempts: %+v %v", c, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := api.Do(ctx, other.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		if all, err := u.PSPMirror().CreditNotes(ctx, ""); err != nil || len(all) != 0 {
			t.Errorf("another organisation sees %d credit notes: %v", len(all), err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
