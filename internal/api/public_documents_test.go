//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/quote"
	"github.com/cognizhi/fyniq/internal/domain/salesdoc"
)

// open calls a public link operation with token in the Fyniq-Link header and no credentials.
func (e *env) open(method, path, token string, body any) response {
	e.t.Helper()
	return e.call(method, "/v1/public/document"+path, "", body, "Fyniq-Link", token)
}

// FR-AR-06 through the API (p1-sales task 5.3; the threat model in docs/security/threat-models/document-links.md):
// INV-0001 emailed carries a link in the URL's fragment; opened with no session it shows the invoice and nothing else,
// never cached or indexed, and the invoice's delivery becomes viewed. Its PDF comes through it too. A malformed,
// unknown, revoked or expired link answers the same 404 LINK-01 (T9); the token is never logged (T8).
// req: FR-AR-06
// req: FR-INT-01
// asvs: V8.2.1
func TestDocumentLinksOpenTheirDocumentOnly(t *testing.T) {
	e := newEnv(t)
	acc := e.as(authz.Accountant)
	id, token := e.emailedInvoiceLink("1", "100.00")
	r := e.open(http.MethodGet, "", token, nil)
	if r.Status != 200 || r.str("kind") != "invoice" || r.str("number") != "INV-0001" || r.Body["tax_invoice"] != true || r.str("amount_due", "amount") != "109.00" ||
		r.str("supplier") != "Northwind Analytics Pte. Ltd." || len(r.Body["lines"].([]any)) != 1 || r.Body["can_answer"] != false {
		t.Fatalf("the invoice through its link: %d %s", r.Status, r.Raw)
	}
	if r.Header.Get("Cache-Control") != "no-store" || r.Header.Get("X-Robots-Tag") != "noindex, nofollow" || r.Header.Get("Referrer-Policy") != "no-referrer" {
		t.Errorf("the headers: %v", r.Header)
	}
	g := e.call(http.MethodGet, "/v1/invoices/"+id, acc, nil)
	links, _ := g.Body["links"].([]any)
	if g.str("states", "delivery") != "viewed" || len(links) != 1 || links[0].(map[string]any)["status"] != "live" || links[0].(map[string]any)["first_viewed_at"] == nil {
		t.Fatalf("the invoice after it was opened: %s", g.Raw)
	}
	if strings.Contains(g.Raw, token) {
		t.Error("the invoice shows its link's token")
	}
	if p := e.open(http.MethodGet, "/pdf", token, nil); p.Status != 200 || len(p.str("pdf")) < 100 || len(p.str("sha256")) != 64 {
		t.Errorf("the PDF through the link: %d %.300s", p.Status, p.Raw)
	}
	other, _, _ := salesdoc.NewToken()
	for name, bad := range map[string]string{"malformed": "abc", "unknown": other, "a path": "../../" + token[:37]} {
		e.problemIs(e.open(http.MethodGet, "", bad, nil), "getPublicDocument", 404, salesdoc.CodeLink)
		_ = name
	}

	linkID := links[0].(map[string]any)["id"].(string)
	if rv := e.call(http.MethodPost, "/v1/document-links/"+linkID+"/revoke", acc, nil); rv.Status != 200 || rv.str("status") != "revoked" {
		t.Fatalf("revoking it: %d %s", rv.Status, rv.Raw)
	}
	e.problemIs(e.open(http.MethodGet, "", token, nil), "getPublicDocument", 404, salesdoc.CodeLink)
	e.problemIs(e.call(http.MethodPost, "/v1/document-links/"+linkID+"/revoke", acc, nil), "revokeDocumentLink", 409, "STORAGE-LINK-01")

	// Emailed again, a new link; past its 180 days it opens nothing.
	tag := e.call(http.MethodGet, "/v1/invoices/"+id, acc, nil).Header.Get("ETag")
	if r := e.call(http.MethodPost, "/v1/invoices/"+id+"/email", acc, nil, "If-Match", tag); r.Status != 200 {
		t.Fatalf("emailing it again: %d %s", r.Status, r.Raw)
	}
	again := e.linkIn()
	if again == token || e.open(http.MethodGet, "", again, nil).Status != 200 {
		t.Fatal("the new link")
	}
	e.clock.Advance((salesdoc.LinkDays + 1) * 24 * time.Hour)
	e.problemIs(e.open(http.MethodGet, "", again, nil), "getPublicDocument", 404, salesdoc.CodeLink)
	if strings.Contains(e.log.String(), token) || strings.Contains(e.log.String(), again) {
		t.Error("a link's token is in the log")
	}
}

// FR-AR-16 through the API (p1-sales task 5.3): the customer answers QT-0001 through its link. Accepting needs a name;
// accepted by Jane Tan with a comment, the quote shows it, recorded as the link (T5); a second answer is refused
// (QUOTE-07). Another quote is declined with a comment. An invoice's link takes no answer.
// req: FR-AR-16
// req: FR-DOC-05
func TestAQuoteIsAnsweredThroughItsLink(t *testing.T) {
	e := newEnv(t)
	acc := e.as(authz.Accountant)
	id, token := e.emailedQuoteLink()
	if r := e.open(http.MethodGet, "", token, nil); r.Status != 200 || r.str("kind") != "quote" || r.str("status") != "sent" || r.Body["can_answer"] != true ||
		r.str("valid_until") != "2026-11-13" {
		t.Fatalf("the quote through its link: %d %s", r.Status, r.Raw)
	}
	e.problemIs(e.open(http.MethodPost, "/accept", token, map[string]any{"comment": "Yes"}), "acceptQuoteByLink", 422, CodeInvalid)
	answer := map[string]any{"name": "Jane Tan", "comment": "Please start on 1 November"}
	if pv := e.call(http.MethodPost, "/v1/public/document/accept", "", answer, "Fyniq-Link", token, "Prefer", "return=preview"); pv.Status != 200 ||
		pv.Header.Get("Preference-Applied") != "return=preview" || pv.str("status") != "accepted" {
		t.Fatalf("a preview of the acceptance: %d %s", pv.Status, pv.Raw)
	}
	key := "accept-qt-0001"
	a := e.call(http.MethodPost, "/v1/public/document/accept", "", answer, "Fyniq-Link", token, "Idempotency-Key", key)
	if a.Status != 200 || a.str("status") != "accepted" || a.str("answer", "name") != "Jane Tan" || a.Body["can_answer"] != false {
		t.Fatalf("accepting it: %d %s", a.Status, a.Raw)
	}
	if again := e.call(http.MethodPost, "/v1/public/document/accept", "", answer, "Fyniq-Link", token, "Idempotency-Key", key); again.Status != 200 ||
		again.Header.Get("Idempotent-Replayed") != "true" {
		t.Errorf("the same acceptance again: %d %s", again.Status, again.Raw)
	}
	q := e.call(http.MethodGet, "/v1/quotes/"+id, acc, nil)
	if q.str("status") != "accepted" || q.str("answer", "name") != "Jane Tan" || q.str("answer", "note") != "Please start on 1 November" ||
		q.str("delivery") != "viewed" {
		t.Errorf("the quote after it was accepted: %s", q.Raw)
	}
	e.problemIs(e.open(http.MethodPost, "/decline", token, map[string]any{"comment": "Changed our mind"}), "declineQuoteByLink", 409, quote.CodeAnswered)

	_, other := e.emailedQuoteLink()
	if d := e.open(http.MethodPost, "/decline", other, map[string]any{"comment": "Too expensive"}); d.Status != 200 || d.str("status") != "declined" {
		t.Errorf("declining another: %d %s", d.Status, d.Raw)
	}
	_, invoiceLink := e.emailedInvoiceLink("1", "50.00")
	e.problemIs(e.open(http.MethodPost, "/accept", invoiceLink, map[string]any{"name": "Jane Tan"}), "acceptQuoteByLink", 404, salesdoc.CodeLink)
}

// A link is opened at most 30 times a minute, then 429 with Retry-After (T6).
// req: FR-AR-06
// req: FR-INT-16
func TestDocumentLinksAreRateLimited(t *testing.T) {
	e := newEnv(t)
	_, token := e.emailedInvoiceLink("1", "100.00")
	for i := range 30 {
		if r := e.open(http.MethodGet, "", token, nil); r.Status != 200 {
			t.Fatalf("opening %d: %d %s", i+1, r.Status, r.Raw)
		}
	}
	r := e.open(http.MethodGet, "", token, nil)
	if r.Status != http.StatusTooManyRequests || r.Header.Get("Retry-After") == "" || r.problemCode() != CodeRateLimited {
		t.Fatalf("the 31st: %d %v %s", r.Status, r.Header, r.Raw)
	}
}
