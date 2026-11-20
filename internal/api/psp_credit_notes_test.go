//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/connector"
	"github.com/cognizhi/fyniq/internal/egress"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/psp"
)

func providerCreditNote(id, invoice, total, amount string, day int, extra func(*ports.CreditNoteData)) ports.ProviderObject {
	cn := &ports.CreditNoteData{InvoiceID: invoice, Number: "CN-" + id, Status: "issued", Reason: "order_change", Total: total,
		Lines: []ports.CreditNoteLine{{ID: "cnl_" + id, Description: "Credit for the workshop", Amount: amount}}}
	if extra != nil {
		extra(cn)
	}
	return ports.ProviderObject{Kind: ports.ProviderCreditNote, ID: id, Currency: "SGD", Amount: total, Created: porttestDay(day), CreditNote: cn}
}

// FR-PSP-07 (Model B; p1-payment-providers task 5, rev 0.601): a provider's credit note against an invoice it billed is mirrored; it becomes a draft AR credit note
// of that invoice's document through the database's narrow function only once the invoice is issued here (before that it waits), once, saved by psp:credit_notes;
// the API issues it only when its total is the provider's to the cent, and a different total, a discount or an invoice that is not here parks it for a person with
// why; a second run does nothing; and a reader cannot issue it.
// req: FR-PSP-07
// req: FR-PSP-03
// asvs: V4.2.1
func TestAProviderCreditNoteBecomesADraftOfItsInvoiceAndPostsOnlyAtTheProvidersTotal(t *testing.T) {
	e := newEnv(t)
	f := &fakeLedgerProvider{payouts: map[string][]string{}, balance: "0.00"}
	b := fakeLedgerConnector(t, f)
	cat, err := connector.NewCatalogue(b)
	if err != nil {
		t.Fatal(err)
	}
	_ = &connector.Runtime{Catalogue: cat, Secrets: e.api.deps.Connectors.Runtime.Secrets, Egress: &egress.Guard{Local: true}, Clock: e.clock}
	var c storage.InstalledConnector
	m := b.Manifest
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if c, err = u.Connectors().InstallOrganisation(ctx, storage.NewConnector{Connector: m.ID, ManifestVersion: m.Version, ManifestHash: m.Hash(),
			SecretNames: []string{"token"}, Consent: m.Consent(), Position: 1, At: e.clock.Now()}); err != nil {
			return err
		}
		for _, step := range []struct{ s, state string }{{storage.StepConfigure, storage.ConnectorConfigured}, {storage.StepTest, storage.ConnectorTested},
			{storage.StepEnable, storage.ConnectorEnabled}} {
			if step.s == storage.StepTest {
				c.Test = &storage.ConnectorTest{Passed: true, Reason: "Read.", At: e.clock.Now()}
			}
			if c, err = u.Connectors().ChangeOrganisation(ctx, c.ID, c.Version, storage.ConnectorChange{Step: step.s, State: step.state,
				Configuration: map[string]any{}, Position: 1, Test: c.Test, ManifestVersion: m.Version, ManifestHash: m.Hash(), SecretNames: []string{"token"},
				Consent: m.Consent(), At: e.clock.Now()}); err != nil {
				return err
			}
		}
		return nil
	})
	e.customer() // the books' receivable, GST and deferred accounts
	clearing := e.fedAccount("SGD", c)
	worker, err := postgres.Open(context.Background(), db.URL("fyniq_worker"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(worker.Close)
	ctx := context.Background()
	mirror := func(o ports.ProviderObject) {
		t.Helper()
		if err := worker.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			park, err := psp.MirrorObject(ctx, u, c.ID, o, e.clock.Now())
			if park != "" {
				t.Errorf("parked the event: %s", park)
			}
			if err != nil {
				return err
			}
			if o.Kind == ports.ProviderInvoice {
				_, err = psp.MakeDrafts(ctx, u, c.ID)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	drafts := func() int {
		t.Helper()
		n := 0
		if err := worker.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			n, err = psp.MakeCreditDrafts(ctx, u, c.ID)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.PSP().SaveAccount(ctx, storage.NewPSPAccount{MoneyAccountID: clearing.ID, ConnectorID: c.ID, FeeAccountID: e.books.Rent.ID, At: e.clock.Now()}, 0)
		return err
	})
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		a, err := u.PSP().Account(ctx, clearing.ID)
		if err != nil {
			return err
		}
		_, err = u.PSP().SaveAccount(ctx, storage.NewPSPAccount{MoneyAccountID: clearing.ID, ConnectorID: c.ID, FeeAccountID: e.books.Rent.ID, RevenueAccountID: &e.books.Sales.ID, At: e.clock.Now()}, a.Version)
		return err
	})
	for _, o := range []ports.ProviderObject{providerInvoice("in_good", "cus_1", "100.00", "100.00", false, nil), providerInvoice("in_late", "cus_1", "50.00", "50.00", false, nil)} {
		mirror(o)
	}
	// Credit notes while their invoices are drafts only: they wait, and a credit note of an invoice that is not here at all waits too.
	good := providerCreditNote("cn_good", "in_good", "30.00", "30.00", 15, nil)
	bad := providerCreditNote("cn_bad", "in_good", "40.00", "30.00", 15, nil)
	disc := providerCreditNote("cn_disc", "in_good", "20.00", "25.00", 15, func(c *ports.CreditNoteData) { c.Unsupported = "a discount" })
	late := providerCreditNote("cn_late", "in_late", "10.00", "10.00", 16, nil)
	orphan := providerCreditNote("cn_orphan", "in_unknown", "5.00", "5.00", 16, nil)
	for _, o := range []ports.ProviderObject{good, bad, disc, late, orphan, good} { // the good one twice: a redelivery
		mirror(o)
	}
	if n := drafts(); n != 0 {
		t.Fatalf("%d credit note drafts were made before their invoices were issued", n)
	}
	var seen map[string]storage.PSPCreditNote
	read := func() {
		t.Helper()
		seen = map[string]storage.PSPCreditNote{}
		e.do(func(ctx context.Context, u storage.UnitOfWork) error {
			all, err := u.PSPMirror().CreditNotes(ctx, "")
			for _, x := range all {
				seen[x.ProviderID] = x
			}
			return err
		})
	}
	read()
	if len(seen) != 5 || seen["cn_disc"].State != storage.PSPInvoiceParked || !strings.Contains(seen["cn_disc"].ParkedReason, "discount") || seen["cn_good"].State != storage.PSPInvoiceMirrored {
		t.Fatalf("the mirror after the first reading: %+v", seen)
	}
	// The invoices are issued; the next sweep makes the drafts once.
	if r := e.call(http.MethodPost, "/v1/payment-providers/invoices/post", e.as(authz.Accountant), nil); r.Status != http.StatusOK || r.Body["posted"] != float64(2) {
		t.Fatalf("posting the invoices: %d %s", r.Status, r.Raw)
	}
	if n := drafts(); n != 3 { // cn_good, cn_bad and cn_late; not the orphan, not the parked
		t.Fatalf("%d credit note drafts, want 3", n)
	}
	if n := drafts(); n != 0 {
		t.Errorf("a second sweep made %d more drafts", n)
	}
	read()
	var invoiceDoc string
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		inv, err := u.PSPMirror().Invoice(ctx, c.ID, "in_good")
		if err == nil {
			invoiceDoc = inv.DocumentID.String()
		}
		return err
	})
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		x := seen["cn_good"]
		if x.State != storage.PSPInvoiceDrafted || x.DocumentID == nil {
			t.Fatalf("the good credit note: %+v", x)
		}
		doc, err := u.Documents().Get(ctx, *x.DocumentID)
		if err != nil || doc.Type != storage.DocumentCreditNote || doc.Status != storage.DocumentDraft || doc.SavedBy != "psp:credit_notes" ||
			!strings.Contains(string(doc.Payload), invoiceDoc) {
			t.Errorf("the draft: %+v, saved by %q, %v", doc.Type, doc.SavedBy, err)
		}
		return nil
	})
	if seen["cn_orphan"].State != storage.PSPInvoiceMirrored || seen["cn_orphan"].DocumentID != nil {
		t.Errorf("the credit note of an invoice that is not here: %+v", seen["cn_orphan"])
	}
	// A reader cannot issue; the API issues the good ones and parks the one whose total differs.
	if r := e.call(http.MethodPost, "/v1/payment-providers/invoices/post", e.as(authz.Auditor), nil); r.Status != http.StatusForbidden {
		t.Errorf("an Auditor posted the provider's credit notes: %d", r.Status)
	}
	r := e.call(http.MethodPost, "/v1/payment-providers/invoices/post", e.as(authz.Accountant), nil)
	if r.Status != http.StatusOK || r.Body["credit_notes_posted"] != float64(2) || r.Body["credit_notes_parked"] != float64(1) {
		t.Fatalf("postPaymentProviderInvoices = %d: %s", r.Status, r.Raw)
	}
	if again := e.call(http.MethodPost, "/v1/payment-providers/invoices/post", e.as(authz.Accountant), nil); again.Body["credit_notes_posted"] != float64(0) || again.Body["credit_notes_parked"] != float64(0) {
		t.Errorf("a second run did something: %s", again.Raw)
	}
	list := e.call(http.MethodGet, "/v1/payment-providers/invoices", e.as(authz.Accountant), nil)
	states := map[string]string{}
	for _, x := range list.Body["credit_notes"].([]any) {
		m := x.(map[string]any)
		states[m["provider_credit_note_id"].(string)] = m["state"].(string)
		if m["provider_credit_note_id"] == "cn_bad" && !strings.Contains(m["parked_reason"].(string), "40.00") {
			t.Errorf("the parked reason: %v", m["parked_reason"])
		}
	}
	if states["cn_good"] != "posted" || states["cn_late"] != "posted" || states["cn_bad"] != "parked" || states["cn_disc"] != "parked" || states["cn_orphan"] != "mirrored" {
		t.Fatalf("the states: %v in %s", states, list.Raw)
	}
	read()
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Get(ctx, *seen["cn_good"].DocumentID)
		if err != nil || doc.Status != storage.DocumentPosted || doc.Number == "" {
			t.Errorf("the good credit note's document: %+v %v", doc, err)
		}
		bad, err := u.Documents().Get(ctx, *seen["cn_bad"].DocumentID)
		if err != nil || bad.Status != storage.DocumentDraft {
			t.Errorf("the refused credit note's document: %+v %v", bad.Status, err)
		}
		return nil
	})
}
