//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/bankfeed"
	"github.com/cognizhi/fyniq/internal/connector"
	"github.com/cognizhi/fyniq/internal/domain/banking"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/paymentlink"
	"github.com/cognizhi/fyniq/internal/egress"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/psp"
)

// fakeLinkProvider is a payment provider that also makes payment requests: it records what it was asked for.
type fakeLinkProvider struct {
	fakeLedgerProvider
	lmu      sync.Mutex
	requests []ports.PaymentRequest
	refuse   bool
}

func (f *fakeLinkProvider) CreatePaymentRequest(_ context.Context, r ports.PaymentRequest) (ports.PaymentRequestMade, error) {
	f.lmu.Lock()
	defer f.lmu.Unlock()
	if f.refuse {
		return ports.PaymentRequestMade{}, ports.ErrPaymentRequestRefused
	}
	f.requests = append(f.requests, r)
	id := "pr_" + string(rune('0'+len(f.requests)))
	return ports.PaymentRequestMade{ID: id, URL: "https://pay.example.com/" + id, Status: "pending", ExpiresAt: r.ExpiresAt}, nil
}

func fakeLinkConnector(t testing.TB, f *fakeLinkProvider) connector.Binding {
	t.Helper()
	b := fakeLedgerConnector(t, &f.fakeLedgerProvider)
	m, err := connector.Parse([]byte(strings.Replace(`{"id":"example-link-psp","name":"Example provider","publisher":"Fyniq","version":"1.0.0","min_platform":"0.1.0",
	  "summary":"A fake payment provider.","scope":"organisation","capabilities":[
	  {"kind":"payment-provider","trigger":"system-rule","trigger_rule":"A verified webhook or the sweep.","on_pause":"hold","on_disable":"hold","in_flight":"finish","parser_version":"example@1"},
	  {"kind":"payment-link","trigger":"system-rule","trigger_rule":"A person asks for a link.","on_pause":"hold","on_disable":"hold","in_flight":"finish"},
	  {"kind":"bank-data","trigger":"system-rule","trigger_rule":"The sync schedule.","on_pause":"hold","on_disable":"hold","in_flight":"finish"}],
	  "configuration":{"type":"object","additionalProperties":false,"properties":{}},
	  "secrets":[{"name":"token","title":"Token","description":"Its key.","used_by":["worker"],"rotation_overlap_hours":1}],
	  "provider_scopes":[],"fyniq_scopes":[],"data_classes":[],"regions":["SG"],
	  "destinations":[{"host":"psp.example.com"}],"webhooks":[],"health_check":"Counts.","test_action":"Reads.","local_substitute":"A fake.",
	  "limits":{"requests_per_minute":6000,"breaker_failures":50,"breaker_cooldown_seconds":60}}`, "\n", "", -1)))
	if err != nil {
		t.Fatal(err)
	}
	b.Manifest = m
	b.PaymentLinks = func(connector.Config) (ports.PaymentLinks, error) { return f, nil }
	return b
}

// FR-AR-13, FR-PSP-02 (rev 0.510): a payment link is asked for on an issued invoice for its open balance read on the server;
// the provider's charge under it is mirrored by the worker's sweep, and auto-reconcile records the receipt once. A second link,
// a link on a draft, a replay and a charge of another amount never post.
// req: FR-AR-13
// req: FR-PSP-02
// asvs: V2.3.1
func TestAPaymentLinkIsMadeForTheOpenBalanceAndSettlesTheInvoiceOnce(t *testing.T) {
	e := newEnv(t)
	f := &fakeLinkProvider{fakeLedgerProvider: fakeLedgerProvider{payouts: map[string][]string{}, balance: "0.00"}}
	b := fakeLinkConnector(t, f)
	cat, err := connector.NewCatalogue(b)
	if err != nil {
		t.Fatal(err)
	}
	rt := &connector.Runtime{Catalogue: cat, Secrets: e.api.deps.Connectors.Runtime.Secrets, Egress: &egress.Guard{Local: true}, Clock: e.clock}
	e.api.deps.Connectors = ConnectorDeps{Catalogue: cat, Secrets: e.api.deps.Connectors.Secrets, Runtime: rt}
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
	clearing := e.fedAccount("SGD", c)
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.PSP().SaveAccount(ctx, storage.NewPSPAccount{MoneyAccountID: clearing.ID, ConnectorID: c.ID, FeeAccountID: e.books.Rent.ID, At: e.clock.Now()}, 0)
		return err
	})
	invoiceID, _ := e.issuedInvoice("1", "100.00") // S$100.00 open (the organisation is not GST-registered)
	draftID, _ := e.createInvoice("1", "50.00")

	// The link is for the open balance, whatever the client sends; the provider is asked once, with the invoice's number.
	r := e.call(http.MethodPost, "/v1/invoices/"+invoiceID+"/payment-link", e.as(authz.Accountant), map[string]any{})
	if r.Status != http.StatusCreated {
		t.Fatalf("createInvoicePaymentLink = %d: %s", r.Status, r.Raw)
	}
	if got := r.str("amount", "amount"); got != "100.00" || r.str("status") != "live" || !strings.HasPrefix(r.str("url"), "https://") {
		t.Fatalf("the link: %s", r.Raw)
	}
	if len(f.requests) != 1 || f.requests[0].Amount != "100.00" || f.requests[0].Currency != "SGD" || !strings.HasPrefix(f.requests[0].Reference, "INV-") {
		t.Fatalf("the provider was asked %+v", f.requests)
	}
	// A second live link, a draft, a preview and a reader without the right ask nothing of the provider.
	if r := e.call(http.MethodPost, "/v1/invoices/"+invoiceID+"/payment-link", e.as(authz.Accountant), map[string]any{}); r.Status != http.StatusConflict || !strings.Contains(r.Raw, paymentlink.CodeLive) {
		t.Fatalf("a second link = %d: %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/invoices/"+draftID+"/payment-link", e.as(authz.Accountant), map[string]any{}); r.Status != http.StatusUnprocessableEntity || !strings.Contains(r.Raw, paymentlink.CodeNotIssued) {
		t.Fatalf("a link on a draft = %d: %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/invoices/"+invoiceID+"/payment-link", e.as(authz.Auditor), map[string]any{}); r.Status != http.StatusForbidden {
		t.Fatalf("an Auditor asked for a link: %d", r.Status)
	}
	if len(f.requests) != 1 {
		t.Fatalf("the provider was asked %d times", len(f.requests))
	}
	if g := e.call(http.MethodGet, "/v1/invoices/"+invoiceID+"/payment-link", e.as(authz.Auditor), nil); g.Status != http.StatusOK || g.str("status") != "live" {
		t.Fatalf("getInvoicePaymentLink = %d: %s", g.Status, g.Raw)
	}
	var request string
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		ls, err := u.PaymentLinks().OfInvoice(ctx, uuid.MustParse(invoiceID))
		if len(ls) == 1 {
			request = ls[0].ProviderRequestID
		}
		return err
	})
	if request == "" {
		t.Fatal("no link was recorded")
	}

	// The provider's charge under the link, and a second one of another amount under the same link.
	good := bt("ch_good", "charge", "100.00", "3.60", "96.40", 11, ports.ProviderCharge, "ch_good")
	good.Links[ports.ProviderInvoice] = []string{request}
	other := bt("ch_other", "charge", "50.00", "1.80", "48.20", 12, ports.ProviderCharge, "ch_other")
	other.Links[ports.ProviderInvoice] = []string{request}
	f.txns = []ports.ProviderObject{good}
	worker, err := postgres.Open(context.Background(), db.URL("fyniq_worker"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(worker.Close)
	runner := psp.Runner{Store: worker, Runtime: rt, Clock: e.clock}
	sync := bankfeed.Syncer{Store: worker, Runtime: rt, Clock: e.clock}
	txns := journal.BankTransactions{Store: e.store, Clock: e.clock}
	ctx := context.Background()
	if got, err := sync.Sync(ctx, e.books.Org, c.ID); err != nil || len(got.Errors) != 0 {
		t.Fatalf("the sync: %+v, %v", got, err)
	}
	if res, err := runner.Run(ctx, e.books.Org, c.ID); err != nil || len(res.Errors) != 0 {
		t.Fatalf("the sweep: %+v, %v", res, err)
	}
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		p, err := u.PaymentLinks().PaymentByCharge(ctx, c.ID, "ch_good")
		if err != nil || string(p.Amount) == "" {
			t.Fatalf("the charge was not mirrored as a payment of the link: %v", err)
		}
		return nil
	})
	auto, err := banking.AutoReconcile(ctx, txns, e.books.Org, nil, e.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	var receipts int
	for _, x := range auto.Reconciled {
		if x.Line.Reference == "ch_good" {
			receipts++
			if x.Basis != storage.BasisExact {
				t.Errorf("the receipt's basis is %q", x.Basis)
			}
		}
	}
	if receipts != 1 {
		t.Fatalf("auto-reconcile recorded %d receipts for the charge: %+v", receipts, auto)
	}
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		if _, err := paymentlink.OpenInvoice(ctx, u, uuid.MustParse(invoiceID), e.clock.Now()); err == nil {
			t.Error("the invoice still has something open after its payment link was paid")
		}
		return nil
	})
	// A replay posts nothing twice; a charge of another amount under the same link waits for a person, with its reason.
	f.txns = []ports.ProviderObject{good, other}
	if _, err := sync.Sync(ctx, e.books.Org, c.ID); err != nil {
		t.Fatal(err)
	}
	if res, err := runner.Run(ctx, e.books.Org, c.ID); err != nil || len(res.Errors) != 0 {
		t.Fatalf("the second sweep: %+v, %v", res, err)
	}
	again, err := banking.AutoReconcile(ctx, txns, e.books.Org, nil, e.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range again.Reconciled {
		if x.Line.Reference == "ch_good" || x.Line.Reference == "ch_other" {
			t.Errorf("a replay or a mismatched charge was reconciled: %s", x.Line.Reference)
		}
	}
	waits := false
	for _, l := range again.Left {
		if l.Line.Reference == "ch_other" && l.Why == banking.LeftLink {
			waits = true
		}
	}
	if !waits {
		t.Fatalf("the charge of another amount was not left as a mismatch: %+v", again.Left)
	}
}

// FR-AR-13: a provider's refusal is an error in plain words that keeps no body, and leaves no link.
// req: FR-AR-13
func TestARefusedPaymentRequestLeavesNoLinkAndShowsNoProviderWords(t *testing.T) {
	e := newEnv(t)
	f := &fakeLinkProvider{fakeLedgerProvider: fakeLedgerProvider{payouts: map[string][]string{}}, refuse: true}
	b := fakeLinkConnector(t, f)
	cat, err := connector.NewCatalogue(b)
	if err != nil {
		t.Fatal(err)
	}
	rt := &connector.Runtime{Catalogue: cat, Secrets: e.api.deps.Connectors.Runtime.Secrets, Egress: &egress.Guard{Local: true}, Clock: e.clock}
	e.api.deps.Connectors = ConnectorDeps{Catalogue: cat, Secrets: e.api.deps.Connectors.Secrets, Runtime: rt}
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
	clearing := e.fedAccount("SGD", c)
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.PSP().SaveAccount(ctx, storage.NewPSPAccount{MoneyAccountID: clearing.ID, ConnectorID: c.ID, FeeAccountID: e.books.Rent.ID, At: e.clock.Now()}, 0)
		return err
	})
	id, _ := e.issuedInvoice("1", "100.00")
	r := e.call(http.MethodPost, "/v1/invoices/"+id+"/payment-link", e.as(authz.Accountant), map[string]any{})
	if r.Status != http.StatusUnprocessableEntity || !strings.Contains(r.Raw, CodeLinkConnector) {
		t.Fatalf("a refused request = %d: %s", r.Status, r.Raw)
	}
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		ls, err := u.PaymentLinks().OfInvoice(ctx, uuid.MustParse(id))
		if len(ls) != 0 {
			t.Errorf("a link was recorded for a refused request")
		}
		return err
	})
	// With no connector enabled at all the answer says so.
	e.api.deps.Connectors = testConnectors(t, e.clock)
	if r := e.call(http.MethodPost, "/v1/invoices/"+id+"/payment-link", e.as(authz.Accountant), map[string]any{}); r.Status != http.StatusUnprocessableEntity || !strings.Contains(r.Raw, CodeLinkConnector) {
		t.Fatalf("no connector = %d: %s", r.Status, r.Raw)
	}
}
