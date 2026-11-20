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
	"github.com/cognizhi/fyniq/internal/domain/portal"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// portalOf opens the portal with token, as the web page does: no session, the token in its header.
func (e *env) portalOf(path, token string, query ...string) response {
	e.t.Helper()
	p := "/v1/public/portal" + path
	if len(query) > 0 {
		p += "?" + strings.Join(query, "&")
	}
	return e.call(http.MethodGet, p, "", nil, "Fyniq-Portal", token)
}

// otherCustomer is a second customer of e's organisation and an issued invoice to it.
func (e *env) otherCustomer() (contact, invoiceID string) {
	e.t.Helper()
	e.customer() // the books' accounts
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture"}})
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		c, err := u.Contacts().Create(ctx, storage.ContactDetails{LegalName: "Zeta Retail Pte Ltd", Customer: true})
		contact = c.ID.String()
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
	d := e.invoiceDraft("1", "222.00")
	d["customer_id"] = contact
	r := e.call(http.MethodPost, "/v1/invoices", e.as(authz.Accountant), d)
	if r.Status != http.StatusCreated {
		e.t.Fatalf("an invoice to the other customer = %d: %s", r.Status, r.Raw)
	}
	if i := e.call(http.MethodPost, "/v1/invoices/"+r.str("id")+"/issue", e.as(authz.Accountant), nil, "If-Match", r.Header.Get("ETag")); i.Status != http.StatusOK {
		e.t.Fatalf("issuing it = %d: %s", i.Status, i.Raw)
	}
	return contact, r.str("id")
}

func (e *env) makePortalLink(contact string, days int) response {
	e.t.Helper()
	body := map[string]any{"label": "Sent to ap@acme.example"}
	if days > 0 {
		body["days"] = days
	}
	return e.call(http.MethodPost, "/v1/contacts/"+contact+"/portal-links", e.as(authz.Accountant), body)
}

// FR-AR-14 through the API (p2-customer-portal-subscriptions task 1.1; the threat model in docs/security/threat-models/customer-portal.md):
// a portal link opens one customer's invoices still due, their invoice PDFs and statement, and nothing else: never another customer's
// of the same organisation, whose invoice ID answers as one that does not exist; an unknown, malformed, expired and revoked link answer
// the same 404 PORTAL-01 (T9); no staff, integration or AI token opens the portal, and the portal's token opens no staff route (T3, T4);
// the answers are never cached or indexed, and the token is never in a log, an answer or the staff's list of links (T8).
// req: FR-AR-14
// req: FR-INT-01
// asvs: V4.1.1
// asvs: V8.2.1
// asvs: V13.2.1
func TestAPortalOpensItsCustomersDocumentsOnly(t *testing.T) {
	e := newEnv(t)
	acc := e.as(authz.Accountant)
	contactA := e.customer()
	idA, _ := e.issuedInvoice("1", "100.00")
	draftA, _ := e.createInvoice("1", "55.00")
	contactB, idB := e.otherCustomer()

	made := e.makePortalLink(contactA, 0)
	if made.Status != http.StatusCreated || len(made.str("token")) != 43 || !strings.HasPrefix(made.str("path"), "/portal/#") || made.str("status") != "live" {
		t.Fatalf("createPortalLink = %d: %s", made.Status, made.Raw)
	}
	token, linkA := made.str("token"), made.str("id")

	// It opens the customer's invoices still due, and only theirs.
	r := e.portalOf("", token)
	if r.Status != 200 || r.str("customer") != "Acme Analytics Pte Ltd" || len(r.Body["invoices"].([]any)) != 1 || strings.Contains(r.Raw, "Zeta") || strings.Contains(r.Raw, "222.00") {
		t.Fatalf("the portal: %d %s", r.Status, r.Raw)
	}
	inv := r.Body["invoices"].([]any)[0].(map[string]any)
	if inv["id"] != idA || inv["number"] != "INV-0001" || inv["open"].(map[string]any)["amount"] != "100.00" || inv["overdue"] != false {
		t.Fatalf("the invoice shown: %v", inv)
	}
	if r.Header.Get("Cache-Control") != "no-store" || r.Header.Get("X-Robots-Tag") != "noindex, nofollow" || r.Header.Get("Referrer-Policy") != "no-referrer" {
		t.Errorf("the headers: %v", r.Header)
	}
	// Its PDF and statement come through it; another customer's invoice, a draft and one that does not exist answer alike.
	if p := e.portalOf("/invoices/"+idA+"/pdf", token); p.Status != 200 || len(p.str("pdf")) < 100 || len(p.str("sha256")) != 64 {
		t.Errorf("the PDF: %d %.300s", p.Status, p.Raw)
	}
	gone := e.portalOf("/invoices/"+uuid.NewString()+"/pdf", token)
	e.problemIs(gone, "getPortalInvoicePDF", 404, portal.CodeLink)
	for what, id := range map[string]string{"another customer's invoice": idB, "a draft": draftA} {
		x := e.portalOf("/invoices/"+id+"/pdf", token)
		e.problemIs(x, "getPortalInvoicePDF", 404, portal.CodeLink)
		_ = what
		if x.str("detail") != gone.str("detail") || x.str("fix") != gone.str("fix") {
			t.Errorf("%s answered differently from one that does not exist: %s", what, x.Raw)
		}
	}
	st := e.portalOf("/statement", token)
	if st.Status != 200 || st.str("currency") != "SGD" || strings.Contains(st.Raw, "Zeta") || strings.Contains(st.Raw, "222.00") || st.str("closing", "amount") != "100.00" {
		t.Fatalf("the statement: %d %s", st.Status, st.Raw)
	}
	if bad := e.portalOf("/statement", token, "from=2099-01-01&to=2099-02-01"); bad.Status != http.StatusUnprocessableEntity && bad.Status != http.StatusBadRequest {
		t.Errorf("a statement of the future = %d: %s", bad.Status, bad.Raw)
	}

	// A link that opens nothing answers one 404, whatever the reason.
	missing := e.portalOf("", "abc")
	e.problemIs(missing, "getPortal", 404, portal.CodeLink)
	for name, tok := range map[string]string{"unknown": strings.Repeat("A", 43), "a path": "../../" + token[:37]} {
		x := e.portalOf("", tok)
		e.problemIs(x, "getPortal", 404, portal.CodeLink)
		_ = name
		if x.str("detail") != missing.str("detail") {
			t.Errorf("%s answered differently: %s", name, x.Raw)
		}
	}
	// The first opening is recorded; the staff's list never shows a token.
	list := e.call(http.MethodGet, "/v1/contacts/"+contactA+"/portal-links", acc, nil)
	links, _ := list.Body["links"].([]any)
	if list.Status != 200 || len(links) != 1 || links[0].(map[string]any)["first_opened_at"] == nil || strings.Contains(list.Raw, token) {
		t.Fatalf("the links of the customer: %d %s", list.Status, list.Raw)
	}
	// No staff, integration or AI token opens the portal, and the portal's token opens no staff route.
	if x := e.call(http.MethodGet, "/v1/public/portal", acc, nil); x.Status == 200 || strings.Contains(x.Raw, "INV-0001") {
		t.Errorf("a staff token opened the portal: %d %s", x.Status, x.Raw)
	}
	for _, path := range []string{"/v1/invoices", "/v1/contacts", "/v1/session", "/v1/contacts/" + contactA + "/portal-links"} {
		if x := e.call(http.MethodGet, path, token, nil); x.Status != http.StatusUnauthorized {
			t.Errorf("the portal's token as a bearer token on %s = %d: %.200s", path, x.Status, x.Raw)
		}
		if x := e.call(http.MethodGet, path, "", nil, "Fyniq-Portal", token); x.Status != http.StatusUnauthorized {
			t.Errorf("the portal's header on %s = %d: %.200s", path, x.Status, x.Raw)
		}
	}
	// The other customer has a portal of their own, which shows theirs alone.
	madeB := e.makePortalLink(contactB, 0)
	if rb := e.portalOf("", madeB.str("token")); rb.Status != 200 || !strings.Contains(rb.Raw, "222.00") || strings.Contains(rb.Raw, "INV-0001") {
		t.Fatalf("the other customer's portal: %d %s", rb.Status, rb.Raw)
	}
	if x := e.portalOf("/invoices/"+idA+"/pdf", madeB.str("token")); x.Status != http.StatusNotFound {
		t.Errorf("the other customer's link opened the first customer's invoice: %d", x.Status)
	}
	// Revoking ends it at once; a link past its days ends alone.
	if rv := e.call(http.MethodPost, "/v1/portal-links/"+linkA+"/revoke", acc, nil); rv.Status != 200 || rv.str("status") != "revoked" {
		t.Fatalf("revoking: %d %s", rv.Status, rv.Raw)
	}
	e.problemIs(e.portalOf("", token), "getPortal", 404, portal.CodeLink)
	if again := e.call(http.MethodPost, "/v1/portal-links/"+linkA+"/revoke", acc, nil); again.Status == 200 {
		t.Error("a link was revoked twice")
	}
	short := e.makePortalLink(contactA, 1)
	if e.portalOf("", short.str("token")).Status != 200 {
		t.Fatal("a one-day link did not open")
	}
	e.clock.Advance(48 * time.Hour)
	e.problemIs(e.portalOf("", short.str("token")), "getPortal", 404, portal.CodeLink)
	// Nothing leaks the tokens.
	if strings.Contains(e.log.String(), token) || strings.Contains(e.log.String(), short.str("token")) {
		t.Error("a portal token is in the log")
	}
}

// FR-AR-14: who may make a link, and for whom: a reader cannot, a vendor-only contact and a contact that is not there cannot have one,
// and a link lasts at most 90 days; the token is shown once and never again.
// req: FR-AR-14
// asvs: V4.2.1
func TestAPortalLinkIsMadeByThoseWhoMayPostAndForCustomersOnly(t *testing.T) {
	e := newEnv(t)
	contact := e.customer()
	if r := e.call(http.MethodPost, "/v1/contacts/"+contact+"/portal-links", e.as(authz.Auditor), map[string]any{}); r.Status != http.StatusForbidden {
		t.Errorf("an Auditor made a portal link: %d", r.Status)
	}
	if r := e.call(http.MethodPost, "/v1/contacts/"+uuid.NewString()+"/portal-links", e.as(authz.Accountant), map[string]any{}); r.Status != http.StatusNotFound {
		t.Errorf("a link for no contact = %d: %s", r.Status, r.Raw)
	}
	var vendor uuid.UUID
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		c, err := u.Contacts().Create(ctx, storage.ContactDetails{LegalName: "Only A Vendor Pte Ltd", Vendor: true})
		vendor = c.ID
		return err
	})
	if r := e.call(http.MethodPost, "/v1/contacts/"+vendor.String()+"/portal-links", e.as(authz.Accountant), map[string]any{}); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("a link for a vendor = %d: %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/contacts/"+contact+"/portal-links", e.as(authz.Accountant), map[string]any{"days": 91}); r.Status != http.StatusUnprocessableEntity && r.Status != http.StatusBadRequest {
		t.Errorf("a link of 91 days = %d: %s", r.Status, r.Raw)
	}
	made := e.makePortalLink(contact, 90)
	if made.Status != http.StatusCreated {
		t.Fatalf("90 days = %d: %s", made.Status, made.Raw)
	}
	if got := e.call(http.MethodGet, "/v1/contacts/"+contact+"/portal-links", e.as(authz.Auditor), nil); got.Status != 200 || strings.Contains(got.Raw, made.str("token")) {
		t.Errorf("the links: %d %s", got.Status, got.Raw)
	}
}

// FR-AR-14 (T6): a link takes at most 60 reads a minute; the 61st is refused with Retry-After, and the next minute opens again.
// req: FR-AR-14
// asvs: V2.4.1
func TestAPortalLinkIsRateLimitedPerLink(t *testing.T) {
	e := newEnv(t)
	token := e.makePortalLink(e.customer(), 0).str("token")
	var last response
	for i := 0; i < portalReads+2; i++ {
		last = e.portalOf("", token)
	}
	if last.Status != http.StatusTooManyRequests || last.Header.Get("Retry-After") == "" {
		t.Fatalf("the %dth read = %d: %.200s", portalReads+2, last.Status, last.Raw)
	}
	e.clock.Advance(2 * time.Minute)
	if r := e.portalOf("", token); r.Status != 200 {
		t.Errorf("after the window = %d: %.200s", r.Status, r.Raw)
	}
}

// matrixPortal are the authorisation matrix's rows for the customer portal: the portal's own operations are public, each opened by a
// link's token, so every caller opens them; making and revoking a link need the right to post on Invoices, reading them to view.
var matrixPortal = map[string]func(e *env, tok string) (response, func() bool){
	"getPortal": func(e *env, tok string) (response, func() bool) {
		return e.portalOf("", e.portalTokenOf()), nil
	},
	"getPortalInvoicePDF": func(e *env, tok string) (response, func() bool) {
		return e.portalOf("/invoices/"+e.portalInvoice+"/pdf", e.portalTokenOf()), nil
	},
	"getPortalStatement": func(e *env, tok string) (response, func() bool) {
		return e.portalOf("/statement", e.portalTokenOf()), nil
	},
	"createPortalLink": func(e *env, tok string) (response, func() bool) {
		contact := e.customer()
		count := func() int {
			var n int
			e.do(func(ctx context.Context, u storage.UnitOfWork) error {
				ls, err := u.PortalLinks().OfContact(ctx, uuid.MustParse(contact))
				n = len(ls)
				return err
			})
			return n
		}
		before := count()
		return e.call(http.MethodPost, "/v1/contacts/"+contact+"/portal-links", tok, map[string]any{}), func() bool { return count() == before }
	},
	"getContactPortalLinks": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/contacts/"+e.customer()+"/portal-links", tok, nil), nil
	},
	"revokePortalLink": func(e *env, tok string) (response, func() bool) {
		made := e.makePortalLink(e.customer(), 0)
		return e.call(http.MethodPost, "/v1/portal-links/"+made.str("id")+"/revoke", tok, nil), func() bool {
			return e.portalOf("", made.str("token")).Status == 200 // refused: the link still opens
		}
	},
}

// portalTokenOf is a portal link of the matrix's customer, made once, and the customer's issued invoice.
func (e *env) portalTokenOf() string {
	e.t.Helper()
	if e.portalToken == "" {
		e.portalInvoice, _ = e.issuedInvoice("1", "10.00")
		e.portalToken = e.makePortalLink(e.customer(), 0).str("token")
	}
	return e.portalToken
}
