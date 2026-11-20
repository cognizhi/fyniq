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
	if g := e.gateOf(acc); g.str("state") != "blocked" {
		t.Fatalf("a request lets nothing through: %s", g.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/invoicenow/contingencies/"+c.str("id")+"/approve", acc, nil); r.Status != 403 {
		t.Errorf("an Accountant holds create, not approve: %d %s", r.Status, r.Raw)
	}
	if ap := e.call(http.MethodPost, "/v1/invoicenow/contingencies/"+c.str("id")+"/approve", ctl, nil); ap.Status != 200 || ap.str("approved_by") == "" {
		t.Fatalf("approved by someone else: %d %s", ap.Status, ap.Raw)
	}
	g = e.gateOf(acc)
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
