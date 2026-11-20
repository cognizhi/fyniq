//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/invoicenow"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// invoiceNowReady makes e's books ready for InvoiceNow, once: GST-registered with its UEN and address, a customer on the
// network (a UEN and a Peppol ID, an address), and the requirement marked as applying from 1 Oct 2026.
func (e *env) invoiceNowReady() {
	e.t.Helper()
	e.readyToEmail()
	ctl := e.as(authz.Controller)
	tag := e.call(http.MethodGet, "/v1/organisation/profile", ctl, nil).Header.Get("ETag")
	if p := e.call(http.MethodPatch, "/v1/organisation/profile", ctl, map[string]any{"uen": "202312345K"}, "If-Match", tag); p.Status != 200 {
		e.t.Fatalf("the profile: %d %s", p.Status, p.Raw)
	}
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture"}})
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		c, err := u.Contacts().Get(ctx, uuid.MustParse(e.customer()))
		if err != nil {
			return err
		}
		c.UENOrTaxID, c.PeppolID, c.Country = "201912345A", "0195:SGUEN201912345A", "SG"
		if _, err := u.Contacts().Update(ctx, c.ID, c.Version, c.ContactDetails, c.Active); err != nil {
			return err
		}
		_, err = u.Settings().Record(ctx, "invoicenow.applies", "false", "true", time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), 1)
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
}

// num is a number of the body, as a whole number.
func (r response) num(path ...string) int {
	var v any = r.Body
	for _, p := range path {
		m, _ := v.(map[string]any)
		v = m[p]
	}
	f, _ := v.(float64)
	return int(f)
}

func (e *env) gateOf(tok string) response {
	e.t.Helper()
	r := e.call(http.MethodGet, "/v1/compliance/invoicenow-gate?from=2026-10-01&to=2026-12-31", tok, nil)
	if r.Status != 200 {
		e.t.Fatalf("the gate: %d %s", r.Status, r.Raw)
	}
	return r
}

// A sale goes from the gate's list to IRAS's acknowledgement ID through the access point (FR-AR-12, FR-TAX-10, FR-TAX-11):
// the gate waits for it, a queued, sent and delivered document still blocks, only the acknowledgement ID lets it through; a
// document left out with its reason, or covered by a contingency a second person approved, lets the return be filed too.
// req: FR-AR-12
// req: FR-TAX-10
// req: FR-TAX-11
// req: QA-SEC-03
// asvs: V8.3.1
func TestInvoiceNowFromTheGateToIRASsAcknowledgement(t *testing.T) {
	e := newEnv(t)
	e.invoiceNowReady()
	acc, ctl := e.as(authz.Accountant), e.as(authz.Controller)
	first, _ := e.issuedInvoice("10", "100.00")

	g := e.gateOf(acc)
	if g.str("state") != "blocked" || g.num("documents") != 1 || g.num("acknowledged") != 0 {
		t.Fatalf("a sale with no submission: %s", g.Raw)
	}
	q := e.call(http.MethodPost, "/v1/invoicenow/submissions", acc, map[string]any{"document_id": first})
	if q.Status != 201 || q.str("status") != "queued" || q.str("kind") != "peppol" || q.str("recipient") != "0195:SGUEN201912345A" || q.num("version") != 1 {
		t.Fatalf("queued: %d %s", q.Status, q.Raw)
	}
	sub := q.str("id")
	if got := e.gateOf(acc).Raw; !strings.Contains(got, `"state":"queued"`) {
		t.Errorf("a queued document still blocks the return: %s", got)
	}
	// three reads of the access point: queued, delivered, then IRAS's acknowledgement ID
	var last response
	for range 4 {
		last = e.call(http.MethodPost, "/v1/invoicenow/submissions/"+sub+"/send", acc, nil)
		if last.Status != 200 {
			t.Fatalf("send: %d %s", last.Status, last.Raw)
		}
		if last.str("status") == "acknowledged" {
			break
		}
		if g := e.gateOf(acc); g.str("state") != "blocked" {
			t.Fatalf("a document with no acknowledgement ID passed the gate at %s: %s", last.str("status"), g.Raw)
		}
	}
	if last.str("status") != "acknowledged" || !strings.HasPrefix(last.str("iras_ack"), "IRAS-") || last.str("transmission_id") == "" {
		t.Fatalf("not acknowledged: %s", last.Raw)
	}
	if g := e.gateOf(acc); g.str("state") != "ready" || g.num("acknowledged") != 1 {
		t.Fatalf("an acknowledged sale: %s", g.Raw)
	}

	// a customer with no Peppol ID is a solution-extracted sale, to IRAS alone
	second, _ := e.issuedInvoice("1", "50.00")
	cctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture"}})
	if err := e.store.Do(cctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		c, err := u.Contacts().Get(ctx, uuid.MustParse(e.customer()))
		if err != nil {
			return err
		}
		c.PeppolID = ""
		_, err = u.Contacts().Update(ctx, c.ID, c.Version, c.ContactDetails, c.Active)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	x := e.call(http.MethodPost, "/v1/invoicenow/submissions", acc, map[string]any{"document_id": second})
	if x.Status != 201 || x.str("kind") != "extracted_sale" || x.str("recipient") != "" {
		t.Fatalf("no Peppol ID: %d %s", x.Status, x.Raw)
	}

	// an exclusion with its reason lets a document through, once; withdrawing it asks for the submission again
	third, _ := e.issuedInvoice("2", "20.00")
	if r := e.call(http.MethodPost, "/v1/invoicenow/exclusions", acc, map[string]any{"document_id": third, "reason": "other"}); r.Status != 422 {
		t.Errorf("a reason of other without its words: %d %s", r.Status, r.Raw)
	}
	ex := e.call(http.MethodPost, "/v1/invoicenow/exclusions", acc, map[string]any{"document_id": third, "reason": "reverse_charge"})
	if ex.Status != 201 || ex.str("reason") != "reverse_charge" {
		t.Fatalf("exclusion: %d %s", ex.Status, ex.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/invoicenow/exclusions", acc, map[string]any{"document_id": third, "reason": "import_permit"}); r.Status != 409 || r.problemCode() != storage.CodeInvoiceNow {
		t.Errorf("a second live exclusion: %d %s", r.Status, r.Raw)
	}
	if g := e.gateOf(acc); g.num("excluded") != 1 || g.num("documents") != 3 {
		t.Fatalf("the gate counts the exclusion: %s", g.Raw)
	}
	// the second sale is still queued: blocked until a contingency covers it
	if g := e.gateOf(acc); g.str("state") != "blocked" {
		t.Fatalf("the queued extracted sale blocks: %s", g.Raw)
	}
	body := map[string]any{"period_from": "2026-10-01", "period_to": "2026-12-31", "reason": "The access point was down on 30 and 31 October",
		"evidence": "The provider's incident notice of 31 October, and its reference INC-2026-1031"}
	c := e.call(http.MethodPost, "/v1/invoicenow/contingencies", acc, body)
	if c.Status != 201 || c.str("approved_by") != "" {
		t.Fatalf("contingency requested: %d %s", c.Status, c.Raw)
	}
	if g := e.gateOf(acc); g.str("state") != "blocked" || !strings.Contains(g.Raw, `"pending_contingency":{`) || !strings.Contains(g.Raw, c.str("id")) || strings.Contains(g.Raw, `"contingency":{`) {
		t.Fatalf("a request lets nothing through and is shown as pending: %s", g.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/invoicenow/contingencies/"+c.str("id")+"/approve", acc, nil); r.Status != 403 {
		t.Errorf("an Accountant holds create, not approve: %d %s", r.Status, r.Raw)
	}
	if ap := e.call(http.MethodPost, "/v1/invoicenow/contingencies/"+c.str("id")+"/approve", ctl, nil); ap.Status != 200 || ap.str("approved_by") == "" {
		t.Fatalf("approved by someone else: %d %s", ap.Status, ap.Raw)
	}
	g = e.gateOf(acc)
	if strings.Contains(g.Raw, `"pending_contingency"`) {
		t.Errorf("an approved request is no longer pending: %s", g.Raw)
	}
	if g.str("state") != "ready" || !strings.Contains(g.Raw, `"contingency"`) || !strings.Contains(g.Raw, `"state":"queued"`) {
		t.Fatalf("an approved contingency lets the return be filed, the gap still listed: %s", g.Raw)
	}
}

// The person who requested a contingency cannot approve it: the database refuses the same person twice, even for someone
// who holds the approve right (FR-TAX-11).
// req: FR-TAX-11
// asvs: V8.3.1
func TestAContingencyIsNeverApprovedByItsRequester(t *testing.T) {
	e := newEnv(t)
	e.invoiceNowReady()
	// one person who holds both the create right (an Accountant's) and the approve right (a Finance controller's)
	ctl := e.as(authz.Accountant, authz.Controller)
	c := e.call(http.MethodPost, "/v1/invoicenow/contingencies", ctl, map[string]any{"period_from": "2026-10-01", "period_to": "2026-12-31",
		"reason": "IRAS's service was unavailable for two days", "evidence": "IRAS's notice of the outage, saved in the filing folder"})
	if c.Status != 201 {
		t.Fatalf("requested: %d %s", c.Status, c.Raw)
	}
	r := e.call(http.MethodPost, "/v1/invoicenow/contingencies/"+c.str("id")+"/approve", ctl, nil)
	if r.Status != 409 || r.problemCode() != storage.CodeInvoiceNow {
		t.Fatalf("the requester approving: %d %s", r.Status, r.Raw)
	}
	if g := e.gateOf(ctl); strings.Contains(g.Raw, `"contingency"`) {
		t.Fatalf("a refused approval lets nothing through: %s", g.Raw)
	}
}

// An outage leaves a queued submission queued, answers "not available" and loses nothing; a document that breaks a rule is
// refused with the field and rule, and never queued (G4-033).
// req: FR-AR-12
// req: FR-TAX-11
func TestAnOutageKeepsTheQueueAndABrokenDocumentIsRefused(t *testing.T) {
	e := newEnv(t)
	e.invoiceNowReady()
	acc := e.as(authz.Accountant)
	id, _ := e.issuedInvoice("1", "10.00")
	q := e.call(http.MethodPost, "/v1/invoicenow/submissions", acc, map[string]any{"document_id": id})
	if q.Status != 201 {
		t.Fatalf("queued: %d %s", q.Status, q.Raw)
	}
	e.ap.SetOutage(true)
	if r := e.call(http.MethodPost, "/v1/invoicenow/submissions/"+q.str("id")+"/send", acc, nil); r.Status != 503 {
		t.Fatalf("send during an outage: %d %s", r.Status, r.Raw)
	}
	other, _ := e.issuedInvoice("1", "11.00")
	if r := e.call(http.MethodPost, "/v1/invoicenow/submissions", acc, map[string]any{"document_id": other}); r.Status != 503 {
		t.Fatalf("queue during an outage: %d %s", r.Status, r.Raw)
	}
	e.ap.SetOutage(false)
	l := e.call(http.MethodGet, "/v1/invoicenow/submissions?status=queued", acc, nil)
	if l.Status != 200 || strings.Count(l.Raw, `"status":"queued"`) != 1 {
		t.Fatalf("the queue after the outage: %s", l.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/invoicenow/submissions/"+q.str("id")+"/send", acc, nil); r.Status != 200 || r.str("status") != "sent" {
		t.Fatalf("send after the outage: %d %s", r.Status, r.Raw)
	}
	// a customer with no address breaks BR-107-GST-SG
	e.customerDetails("", "ap@acme.example")
	third, _ := e.issuedInvoice("1", "12.00")
	r := e.call(http.MethodPost, "/v1/invoicenow/submissions", acc, map[string]any{"document_id": third})
	if r.Status != 422 || !strings.Contains(r.Raw, "BR-107-GST-SG") {
		t.Fatalf("a customer with no address: %d %s", r.Status, r.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/invoicenow/submissions?document_id="+third, acc, nil); strings.Contains(l.Raw, `"id"`) {
		t.Errorf("a refused document was queued: %s", l.Raw)
	}
}

// matrixInvoiceNow are the authorisation matrix's rows for InvoiceNow (FR-TAX-10, FR-TAX-11): a refused call changes nothing,
// and an allowed one reaches a document or record that does not exist, so it changes nothing either.
var matrixInvoiceNow = map[string]func(e *env, tok string) (response, func() bool){
	"getInvoiceNowGate": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/compliance/invoicenow-gate?from=2026-10-01&to=2026-12-31", tok, nil), nil
	},
	"listInvoiceNowSubmissions": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/invoicenow/submissions", tok, nil), nil
	},
	"queueInvoiceNowSubmission": func(e *env, tok string) (response, func() bool) {
		was := e.call(http.MethodGet, "/v1/invoicenow/submissions", e.as(authz.Accountant), nil).Raw
		r := e.call(http.MethodPost, "/v1/invoicenow/submissions", tok, map[string]any{"document_id": uuid.NewString()})
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/invoicenow/submissions", e.as(authz.Accountant), nil).Raw == was
		}
	},
	"sendInvoiceNowSubmission": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/invoicenow/submissions/"+uuid.NewString()+"/send", tok, nil), nil
	},
	"retryInvoiceNowSubmission": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/invoicenow/submissions/"+uuid.NewString()+"/retry", tok, nil), nil
	},
	"createInvoiceNowExclusion": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/invoicenow/exclusions", tok, map[string]any{"document_id": uuid.NewString(), "reason": "reverse_charge"}), nil
	},
	"withdrawInvoiceNowExclusion": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/invoicenow/exclusions/"+uuid.NewString()+"/withdraw", tok, nil), nil
	},
	"requestInvoiceNowContingency": func(e *env, tok string) (response, func() bool) {
		was := e.call(http.MethodGet, "/v1/compliance/invoicenow-gate?from=2090-01-01&to=2090-03-31", e.as(authz.Accountant), nil).Raw
		r := e.call(http.MethodPost, "/v1/invoicenow/contingencies", tok, map[string]any{"period_from": "2090-01-01", "period_to": "2090-03-31",
			"reason": "The access point was down", "evidence": "The provider's incident notice"})
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/compliance/invoicenow-gate?from=2090-01-01&to=2090-03-31", e.as(authz.Accountant), nil).Raw == was
		}
	},
	"approveInvoiceNowContingency": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/invoicenow/contingencies/"+uuid.NewString()+"/approve", tok, nil), nil
	},
}

// The GST F5's filing is held to the gate (FR-TAX-11): while a document of the return's period has neither IRAS's acknowledgement ID nor an exclusion the filing
// cannot be recorded, and once the sale is acknowledged it can; with the requirement not marked as applying the gate is not assessed and holds nothing back.
// req: FR-TAX-11
// asvs: V8.3.1
func TestAnF5IsNotFiledWhileTheGateIsBlocked(t *testing.T) {
	e := newEnv(t)
	e.invoiceNowReady()
	owner, acc := e.as(authz.Owner), e.as(authz.Accountant)
	id, _ := e.issuedInvoice("3", "40.00")
	file := func() response {
		d := e.call(http.MethodPost, "/v1/compliance/filings", owner, map[string]any{"key": "gst_f5:2026-12-31", "outcome": "filed", "filed_on": "2026-10-15", "reference": "F5-Q4"})
		if d.Status != http.StatusCreated {
			t.Fatalf("the draft: %d %s", d.Status, d.Raw)
		}
		up := e.upload("/v1/compliance/filings/"+d.str("id")+"/evidence", owner, map[string]string{"evidence_type": "IRAS acknowledgement"}, "ack.pdf", []byte("%PDF-1.7 ack"),
			"If-Match", d.Header.Get("ETag"))
		if up.Status != http.StatusCreated {
			t.Fatalf("the acknowledgement: %d %s", up.Status, up.Raw)
		}
		cur := e.call(http.MethodGet, "/v1/compliance/filings/"+d.str("id"), owner, nil)
		return e.call(http.MethodPost, "/v1/compliance/filings/"+d.str("id")+"/record", owner, nil, "If-Match", cur.Header.Get("ETag"))
	}
	if r := file(); r.Status != 409 || r.problemCode() != CodeInvoiceNowGate {
		t.Fatalf("the F5 filed with a sale never sent: %d %s", r.Status, r.Raw)
	}
	q := e.call(http.MethodPost, "/v1/invoicenow/submissions", acc, map[string]any{"document_id": id})
	for range 4 {
		e.call(http.MethodPost, "/v1/invoicenow/submissions/"+q.str("id")+"/send", acc, nil)
	}
	if r := file(); r.Status != http.StatusOK || r.str("status") != "recorded" {
		t.Fatalf("the F5 filed once the sale is acknowledged: %d %s", r.Status, r.Raw)
	}
}

// A US-dollar invoice is sent with its GST and totals in Singapore dollars taken from its posting (BR-113-GST-SG): 100.00 USD at 1.35 is 135.00 SGD before GST, the GST
// 12.15 (9% of 135.00, as the engine posts it) and 147.15 in all; the document passes the rules and is queued.
// req: FR-AR-12
func TestAForeignCurrencyInvoiceIsSentWithItsSGDTotalsFromThePosting(t *testing.T) {
	e := newEnv(t)
	e.invoiceNowReady()
	acc := e.as(authz.Accountant)
	if r := e.call(http.MethodPost, "/v1/fx/rates", acc, map[string]any{"kind": "day", "currency": "USD", "on": "2026-10-14", "quote": "1.35"}); r.Status != 201 {
		t.Fatalf("the rate: %d %s", r.Status, r.Raw)
	}
	draft := e.invoiceDraft("1", "100.00")
	draft["currency"] = "USD"
	c := e.call(http.MethodPost, "/v1/invoices", acc, draft)
	if c.Status != 201 {
		t.Fatalf("the invoice: %d %s", c.Status, c.Raw)
	}
	id := c.str("id")
	if r := e.call(http.MethodPost, "/v1/invoices/"+id+"/issue", acc, nil, "If-Match", c.Header.Get("ETag")); r.Status != 200 {
		t.Fatalf("issuing it: %d %s", r.Status, r.Raw)
	}
	q := e.call(http.MethodPost, "/v1/invoicenow/submissions", acc, map[string]any{"document_id": id})
	if q.Status != 201 || q.str("kind") != "peppol" {
		t.Fatalf("queued: %d %s", q.Status, q.Raw)
	}
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture"}})
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Get(ctx, uuid.MustParse(id))
		if err != nil {
			return err
		}
		d, err := invoicenow.FromInvoice(ctx, u, doc, uuid.New(), money.MustLookup("SGD"))
		if err != nil {
			return err
		}
		if d.SGDTax == nil || d.SGDNet == nil || d.SGDGross == nil || d.SGDTax.String() != "12.15" || d.SGDNet.String() != "135.00" || d.SGDGross.String() != "147.15" ||
			d.TaxTotal.String() != "9.00" || d.Gross.String() != "109.00" || d.Currency.Code != "USD" {
			t.Errorf("the totals are %v %v %v, in USD %s %s", d.SGDTax, d.SGDNet, d.SGDGross, d.TaxTotal, d.Gross)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// A supplier's invoice received through InvoiceNow becomes a draft bill of that supplier (FR-AP-05): the sender's Peppol ID finds the supplier, the lines go to its default account
// with the GST read as TX at the document's rate, nothing is posted, a second invoice of the same number is refused naming the first, a document that is not PINT-SG is kept
// and refused, one from an unknown sender waits for its supplier and is drafted once the supplier exists, and an outage keeps nothing.
// req: FR-AP-05
// asvs: V8.3.1
func TestAReceivedInvoiceBecomesADraftBill(t *testing.T) {
	e := newEnv(t)
	e.invoiceNowReady()
	acc := e.as(authz.Accountant)
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture"}})
	var supplier uuid.UUID
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		c, err := u.Contacts().Create(ctx, storage.ContactDetails{LegalName: "Rent Co Pte Ltd", Vendor: true, UENOrTaxID: "202012345K", PeppolID: "0195:SGUEN202012345K", Country: "SG",
			DefaultAccountID: &e.books.Rent.ID})
		supplier = c.ID
		return err
	}); err != nil {
		t.Fatal(err)
	}
	sgd := money.MustLookup("SGD")
	m := func(s string) money.Money { v, _ := money.Parse(s, sgd); return v }
	addr := invoicenow.Address{Line1: "1 Raffles Place", PostCode: "048616", Country: "SG"}
	xml, err := invoicenow.Build(invoicenow.Document{Kind: invoicenow.Invoice, Number: "RC-77", UUID: "0a1b2c3d-1111-4222-8333-444455556666", Issued: time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC),
		Currency: sgd, Seller: invoicenow.Party{Name: "Rent Co Pte Ltd", UEN: "202012345K", GST: "M12345678X", Address: addr},
		Buyer: invoicenow.Party{Name: "Buyer", UEN: "202312345K", Address: addr},
		Lines: []invoicenow.Line{{ID: "1", Name: "Office rent, October", Quantity: "1", Unit: "C62", NetPrice: "1000.00", Net: m("1000.00"), Category: "SR", Rate: "9"}},
		Tax:   []invoicenow.TaxSubtotal{{Category: "SR", Rate: "9", Taxable: m("1000.00"), Tax: m("90.00")}}, Net: m("1000.00"), TaxTotal: m("90.00"), Gross: m("1090.00")})
	if err != nil {
		t.Fatal(err)
	}
	other, err := invoicenow.Build(invoicenow.Document{Kind: invoicenow.Invoice, Number: "NC-1", UUID: "0a1b2c3d-1111-4222-8333-444455556677", Issued: time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC),
		Currency: sgd, Seller: invoicenow.Party{Name: "Newcomer Pte Ltd", UEN: "999912345Z", GST: "M99999999X", Address: addr},
		Buyer: invoicenow.Party{Name: "Buyer", UEN: "202312345K", Address: addr},
		Lines: []invoicenow.Line{{ID: "1", Name: "Cleaning", Quantity: "2", Unit: "C62", NetPrice: "50.00", Net: m("100.00"), Category: "ZR", Rate: "0"}},
		Tax:   []invoicenow.TaxSubtotal{{Category: "ZR", Rate: "0", Taxable: m("100.00"), Tax: m("0.00")}}, Net: m("100.00"), TaxTotal: m("0.00"), Gross: m("100.00")})
	if err != nil {
		t.Fatal(err)
	}
	e.ap.Receive("0195:SGUEN202012345K", xml)
	e.ap.Receive("0195:SGUEN202012345K", xml) // the same number again
	e.ap.Receive("0195:SGUEN202012345K", []byte("<Order/>"))
	e.ap.Receive("0195:SGUEN999912345Z", other) // a sender who is no supplier yet
	p := e.call(http.MethodPost, "/v1/invoicenow/inbound/poll", acc, nil)
	if p.Status != 200 {
		t.Fatalf("poll: %d %s", p.Status, p.Raw)
	}
	items := p.Body["items"].([]any)
	status := func(i int) string { return items[i].(map[string]any)["status"].(string) }
	if len(items) != 4 || status(0) != "drafted" || status(1) != "refused" || status(2) != "refused" || status(3) != "received" {
		t.Fatalf("the four documents: %s", p.Raw)
	}
	if !strings.Contains(items[1].(map[string]any)["note"].(string), "RC-77") || !strings.Contains(items[2].(map[string]any)["note"].(string), "cannot be read") ||
		!strings.Contains(items[3].(map[string]any)["note"].(string), "no active supplier") {
		t.Errorf("the notes: %s", p.Raw)
	}
	bill := e.call(http.MethodGet, "/v1/bills/"+items[0].(map[string]any)["bill_id"].(string), e.as(authz.Accountant), nil)
	if bill.Status != 200 || bill.str("status") != "draft" || bill.str("supplier_ref") != "RC-77" || !strings.Contains(bill.Raw, `"gst_code":"TX"`) || !strings.Contains(bill.Raw, `"amount":"1000.00"`) ||
		!strings.Contains(bill.Raw, `"amount":"90.00"`) || !strings.Contains(bill.Raw, supplier.String()) {
		t.Fatalf("the draft bill: %d %s", bill.Status, bill.Raw)
	}
	if again := e.call(http.MethodPost, "/v1/invoicenow/inbound/poll", acc, nil); again.Status != 200 || len(again.Body["items"].([]any)) != 0 {
		t.Errorf("a second poll finds nothing new: %s", again.Raw)
	}
	// the supplier is added: the waiting document is drafted by hand
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Contacts().Create(ctx, storage.ContactDetails{LegalName: "Newcomer Pte Ltd", Vendor: true, UENOrTaxID: "999912345Z", Country: "SG", DefaultAccountID: &e.books.Rent.ID})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	waiting := items[3].(map[string]any)["id"].(string)
	if d := e.call(http.MethodPost, "/v1/invoicenow/inbound/"+waiting+"/draft", acc, nil); d.Status != 200 || d.str("status") != "drafted" || d.str("bill_id") == "" {
		t.Fatalf("drafting the waiting document: %d %s", d.Status, d.Raw)
	}
	if d := e.call(http.MethodPost, "/v1/invoicenow/inbound/"+waiting+"/draft", acc, nil); d.Status != 412 {
		t.Errorf("drafting it twice: %d %s", d.Status, d.Raw)
	}
	e.ap.Receive("0195:SGUEN202012345K", other)
	e.ap.SetOutage(true)
	if r := e.call(http.MethodPost, "/v1/invoicenow/inbound/poll", acc, nil); r.Status != 503 {
		t.Errorf("poll during an outage: %d %s", r.Status, r.Raw)
	}
	e.ap.SetOutage(false)
	if l := e.call(http.MethodGet, "/v1/invoicenow/inbound", acc, nil); l.Status != 200 || strings.Count(l.Raw, `"message_id"`) != 4 {
		t.Errorf("the list after the outage: %s", l.Raw)
	}
}

func init() {
	matrixInvoiceNow["listInvoiceNowInbound"] = func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/invoicenow/inbound", tok, nil), nil
	}
	matrixInvoiceNow["pollInvoiceNowInbound"] = func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/invoicenow/inbound/poll", tok, nil, "Prefer", "return=preview"), nil
	}
	matrixInvoiceNow["draftInvoiceNowInbound"] = func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/invoicenow/inbound/"+uuid.NewString()+"/draft", tok, nil), nil
	}
}

// The dispatch job, as the worker role, sends what is queued and reads what was sent up to IRAS's acknowledgement ID, run after run; an outage leaves the queue as it was and
// the next run sends it (FR-AR-12, FR-TAX-10, G4-033). The worker moves submissions along but queued none of them.
// req: FR-AR-12
// req: FR-TAX-10
// asvs: V8.2.1
func TestTheDispatchJobSendsTheQueueAsTheWorker(t *testing.T) {
	e := newEnv(t)
	e.invoiceNowReady()
	acc := e.as(authz.Accountant)
	id, _ := e.issuedInvoice("5", "10.00")
	q := e.call(http.MethodPost, "/v1/invoicenow/submissions", acc, map[string]any{"document_id": id})
	if q.Status != 201 {
		t.Fatalf("queued: %d %s", q.Status, q.Raw)
	}
	now := func() time.Time { return e.clock.Now() }
	worker := workerStore(t)
	e.ap.SetOutage(true)
	if r, err := invoicenow.DispatchAll(context.Background(), worker, e.ap, now); err != nil || !r.Outage || r.Sent != 0 {
		t.Fatalf("a run during an outage: %+v, %v", r, err)
	}
	if l := e.call(http.MethodGet, "/v1/invoicenow/submissions?status=queued", acc, nil); strings.Count(l.Raw, `"status":"queued"`) != 1 {
		t.Fatalf("the queue after the outage: %s", l.Raw)
	}
	e.ap.SetOutage(false)
	var total invoicenow.DispatchResult
	for range 4 {
		r, err := invoicenow.DispatchAll(context.Background(), worker, e.ap, now)
		if err != nil {
			t.Fatal(err)
		}
		total.Sent += r.Sent
		total.Refreshed += r.Refreshed
	}
	if total.Sent < 1 || total.Refreshed < 1 { // other organisations of the shared database have queues of their own
		t.Errorf("the runs sent %d and moved %d to acknowledged", total.Sent, total.Refreshed)
	}
	l := e.call(http.MethodGet, "/v1/invoicenow/submissions?status=acknowledged", acc, nil)
	if !strings.Contains(l.Raw, `"iras_ack":"IRAS-`) {
		t.Fatalf("not acknowledged: %s", l.Raw)
	}
	if g := e.gateOf(acc); g.str("state") != "ready" {
		t.Errorf("the gate: %s", g.Raw)
	}
}

// Each reason a document may be left out has its own fixture (FR-TAX-10, G4-034): a document of the period is excluded for it, the gate counts it and lets the return through for it, the
// reason and its words are kept, and withdrawing the exclusion puts the document back among what the gate waits for. "Other" needs its words.
// req: FR-TAX-10
func TestEveryExclusionReasonLetsADocumentThroughAndWithdrawingBringsItBack(t *testing.T) {
	e := newEnv(t)
	e.invoiceNowReady()
	acc := e.as(authz.Accountant)
	for _, reason := range []string{"reverse_charge", "import_permit", "deemed_supply", "financial_services", "unregistered_supplier", "blocked_input_tax", "out_of_scope", "other"} {
		id, _ := e.issuedInvoice("1", "10.00")
		body := map[string]any{"document_id": id, "reason": reason}
		if reason == "other" {
			body["detail"] = "A supply IRAS told us needs none"
		}
		ex := e.call(http.MethodPost, "/v1/invoicenow/exclusions", acc, body)
		if ex.Status != 201 || ex.str("reason") != reason || ex.Body["withdrawn"] != false {
			t.Fatalf("%s: %d %s", reason, ex.Status, ex.Raw)
		}
		if reason == "other" && ex.str("detail") != "A supply IRAS told us needs none" {
			t.Errorf("the words of other are kept: %s", ex.Raw)
		}
		if g := e.gateOf(acc); g.str("state") != "ready" || strings.Contains(g.Raw, id) {
			t.Fatalf("%s: an excluded document is not waited for: %s", reason, g.Raw)
		}
		w := e.call(http.MethodPost, "/v1/invoicenow/exclusions/"+ex.str("id")+"/withdraw", acc, nil)
		if w.Status != 200 || w.Body["withdrawn"] != true {
			t.Fatalf("%s: withdrawn: %d %s", reason, w.Status, w.Raw)
		}
		if g := e.gateOf(acc); g.str("state") != "blocked" || !strings.Contains(g.Raw, id) {
			t.Fatalf("%s: a withdrawn exclusion brings the document back: %s", reason, g.Raw)
		}
		// leave it out again so the next document's gate is judged alone
		if r := e.call(http.MethodPost, "/v1/invoicenow/exclusions", acc, body); r.Status != 201 {
			t.Fatalf("%s: excluded again: %d %s", reason, r.Status, r.Raw)
		}
	}
}
