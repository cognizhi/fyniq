//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/connector"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/receivables"
	"github.com/cognizhi/fyniq/internal/egress"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/psp"
)

func providerInvoice(id, cust, total, amount string, tax bool, extra func(*ports.InvoiceData)) ports.ProviderObject {
	fin := porttestDay(11)
	inv := &ports.InvoiceData{CustomerID: cust, CustomerName: "Lumen Health Pte Ltd", CustomerEmail: "ap@lumen.example", Number: "ACME-" + id, Status: "open", Total: total, AmountPaid: "0.00",
		AttemptCount: 1, Finalized: &fin, Lines: []ports.InvoiceLine{{ID: "il_" + id, Description: "Analytics, Nov 2026", Amount: amount, PeriodStart: porttestDay(1), PeriodEnd: porttestDay(1).AddDate(0, 1, 0)}}}
	_ = tax
	if extra != nil {
		extra(inv)
	}
	return ports.ProviderObject{Kind: ports.ProviderInvoice, ID: id, Currency: "SGD", Amount: total, Created: fin, Invoice: inv}
}

func porttestDay(d int) time.Time { return time.Date(2026, 10, d, 9, 0, 0, 0, time.UTC) }

// FR-PSP-03, FR-PSP-07 (Model B; p1-payment-providers 3.1 to 4.3): a provider whose account names a revenue account is mirrored: an open invoice the
// invoice rules can take becomes a draft through the database's narrow function, its customer a contact made once, and the API posts it only when its total
// is the provider's to the cent; one with a different total (GST the rules do not charge), or a discount, is parked for a person with why; the same invoice
// reported again changes nothing; its reminders never run; and the worker made no document of its own.
// req: FR-PSP-03
// req: FR-PSP-07
// req: banking/payment-providers Model B mirrors a provider invoice
func TestAProviderInvoiceBecomesADraftAndPostsOnlyWhenItsTotalIsTheProviders(t *testing.T) {
	e := newEnv(t)
	f := &fakeLedgerProvider{payouts: map[string][]string{}, balance: "0.00"}
	b := fakeLedgerConnector(t, f)
	cat, err := connector.NewCatalogue(b)
	if err != nil {
		t.Fatal(err)
	}
	rt := &connector.Runtime{Catalogue: cat, Secrets: e.api.deps.Connectors.Runtime.Secrets, Egress: &egress.Guard{Local: true}, Clock: e.clock}
	_ = rt
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
	handle := func(o ports.ProviderObject) {
		t.Helper()
		if err := worker.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			park, err := psp.MirrorObject(ctx, u, c.ID, o, e.clock.Now())
			if park != "" {
				t.Errorf("parked the event: %s", park)
			}
			if err != nil {
				return err
			}
			_, err = psp.MakeDrafts(ctx, u, c.ID)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	// With no revenue account named, the provider is not billing through Fyniq's books: nothing is mirrored.
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.PSP().SaveAccount(ctx, storage.NewPSPAccount{MoneyAccountID: clearing.ID, ConnectorID: c.ID, FeeAccountID: e.books.Rent.ID, At: e.clock.Now()}, 0)
		return err
	})
	handle(providerInvoice("in_0", "cus_1", "100.00", "100.00", false, nil))
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		all, err := u.PSPMirror().Invoices(ctx, "")
		if len(all) != 0 {
			t.Errorf("a provider with no revenue account mirrored %d invoices", len(all))
		}
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

	// A good invoice: S$100.00 with no tax, over three months (spread). A bad one: S$109.00, tax the invoice rules do not charge (the organisation is not
	// GST-registered). An unsupported one: a discount.
	good := providerInvoice("in_good", "cus_1", "100.00", "100.00", false, func(i *ports.InvoiceData) { i.Lines[0].PeriodEnd = porttestDay(1).AddDate(0, 3, 0) })
	bad := providerInvoice("in_bad", "cus_1", "109.00", "100.00", true, nil)
	disc := providerInvoice("in_disc", "cus_1", "90.00", "100.00", false, func(i *ports.InvoiceData) { i.Unsupported = "a discount" })
	for _, o := range []ports.ProviderObject{good, bad, disc, good} { // the good one twice: a redelivery
		handle(o)
	}
	var made storage.PSPInvoice
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		all, err := u.PSPMirror().Invoices(ctx, "")
		if len(all) != 3 {
			t.Errorf("mirrored %d invoices, want 3", len(all))
		}
		for _, x := range all {
			switch x.ProviderID {
			case "in_good":
				made = x
			case "in_disc":
				if x.State != storage.PSPInvoiceParked || !strings.Contains(x.ParkedReason, "discount") || x.DocumentID != nil {
					t.Errorf("the discounted invoice: %+v", x)
				}
			}
		}
		return err
	})
	if made.State != storage.PSPInvoiceDrafted || made.DocumentID == nil {
		t.Fatalf("the good invoice: %+v", made)
	}
	// The draft is one document of the provider's invoice, by the narrow function, its customer made a contact once.
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		docs, err := u.PSPMirror().Documents(ctx)
		if len(docs) != 2 {
			t.Errorf("%d provider documents, want 2 (good and bad, each made once)", len(docs))
		}
		cust, err := u.PSPMirror().Customer(ctx, c.ID, "cus_1")
		if err != nil || cust.ContactID == nil {
			t.Fatalf("the customer: %+v %v", cust, err)
		}
		contact, err := u.Contacts().Get(ctx, *cust.ContactID)
		if err != nil || contact.LegalName != "Lumen Health Pte Ltd" || !contact.Customer {
			t.Errorf("the contact: %+v %v", contact, err)
		}
		doc, err := u.Documents().Get(ctx, *made.DocumentID)
		if err != nil || doc.Status != storage.DocumentDraft || doc.SavedBy != "psp:invoices" || !strings.Contains(string(doc.Payload), `"spread_months": 3`) {
			t.Errorf("the draft: saved by %q, %s, %v", doc.SavedBy, doc.Payload, err)
		}
		return err
	})
	// The worker cannot make a document itself.
	if err := worker.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentInvoice, Payload: []byte(`{}`)})
		return err
	}); err == nil {
		t.Error("the worker made a document of its own")
	}

	// Posting: the API issues the good draft; the bad one is parked, never issued.
	r := e.call(http.MethodPost, "/v1/payment-providers/invoices/post", e.as(authz.Accountant), nil)
	if r.Status != http.StatusOK || r.Body["posted"] != float64(1) || r.Body["parked"] != float64(1) {
		t.Fatalf("postPaymentProviderInvoices = %d: %s", r.Status, r.Raw)
	}
	if again := e.call(http.MethodPost, "/v1/payment-providers/invoices/post", e.as(authz.Accountant), nil); again.Body["posted"] != float64(0) || again.Body["parked"] != float64(0) {
		t.Errorf("a second run did something: %s", again.Raw)
	}
	list := e.call(http.MethodGet, "/v1/payment-providers/invoices", e.as(authz.Accountant), nil)
	states := map[string]string{}
	for _, x := range list.Body["invoices"].([]any) {
		m := x.(map[string]any)
		states[m["provider_invoice_id"].(string)] = m["state"].(string)
		if m["provider_invoice_id"] == "in_bad" && !strings.Contains(m["parked_reason"].(string), "109.00") {
			t.Errorf("the parked reason: %v", m["parked_reason"])
		}
	}
	if states["in_good"] != "posted" || states["in_bad"] != "parked" || states["in_disc"] != "parked" {
		t.Fatalf("the states: %v in %s", states, list.Raw)
	}
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Get(ctx, *made.DocumentID)
		if err != nil || doc.Status != storage.DocumentPosted || doc.Number == "" {
			t.Errorf("the good invoice's document: %+v %v", doc, err)
		}
		accounts, _ := u.Accounts().List(ctx)
		var ar uuid.UUID
		for _, a := range accounts {
			if string(a.Role) == ledger.RoleAccountsReceivable {
				ar = a.ID
			}
		}
		org, _ := u.Organisation(ctx)
		_ = org
		l, err := receivables.Open(ctx, u, money.MustLookup("SGD"), ar, e.clock.Now())
		if err != nil || len(l.Customers) != 1 || len(l.Customers[0].Items) != 1 || l.Customers[0].Items[0].Open.String() != "100.00" {
			t.Errorf("what the customer owes: %+v %v", l, err)
		}
		return err
	})
	// A reader cannot issue; the reminders skip a provider-billed invoice.
	if r := e.call(http.MethodPost, "/v1/payment-providers/invoices/post", e.as(authz.Auditor), nil); r.Status != http.StatusForbidden {
		t.Errorf("an Auditor posted the provider's invoices: %d", r.Status)
	}
	for id := range e.remindersDue() {
		if id == made.DocumentID.String() {
			t.Error("a provider-billed invoice is chased by the reminders")
		}
	}
}
