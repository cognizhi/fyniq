//go:build local

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/email"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// queuedEmailTexts are the texts of the emails queued since the last read, from the outbox; the queue's other tasks are
// dropped.
func queuedEmailTexts(e *env) []string {
	e.t.Helper()
	ctx := context.Background()
	var out []string
	for {
		task, err := e.queue.Next(ctx, "worker")
		if err != nil {
			return out
		}
		var m outbox.Message
		if err := json.Unmarshal(task.Payload, &m); err != nil {
			e.t.Fatal(err)
		}
		var row storage.OutboxRow
		if err := e.store.Do(ctx, m.OrganisationID, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			row, err = u.Outbox().Get(ctx, m.OutboxID)
			return err
		}); err != nil {
			e.t.Fatal(err)
		}
		if row.Kind != email.Kind {
			continue
		}
		var p struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(row.Payload, &p); err != nil {
			e.t.Fatal(err)
		}
		out = append(out, p.Text)
	}
}

// The privacy notices (CMP-PD-03, rev 0.244; p1-pdpa-retention D2): two more keys of the library, listed by kind and
// published from Fyniq's drafts as policies are. Members acknowledge the members' notice; nobody acknowledges the
// contacts' notice, whose summary ends the emails Fyniq sends to contacts, with the DPO's contact.
// req: CMP-PD-03
// req: CMP-PD-01
func TestThePrivacyNotices(t *testing.T) {
	e := newEnv(t)
	dpo, member := e.as(authz.DPO), e.as(authz.Bookkeeper)
	count := func(kind string) int {
		t.Helper()
		r := e.call(http.MethodGet, "/v1/compliance/policies"+kind, member, nil)
		items, _ := r.Body["items"].([]any)
		if r.Status != 200 {
			t.Fatalf("the library%s: %d %s", kind, r.Status, r.Raw)
		}
		return len(items)
	}
	if p, n, all := count(""), count("?kind=notice"), count("?kind=all"); p != 8 || n != 2 || all != 10 {
		t.Fatalf("policies %d, notices %d, all %d", p, n, all)
	}

	// The members' notice, published from Fyniq's draft and acknowledged.
	r := e.call(http.MethodPost, "/v1/compliance/policies/notice-members/versions", dpo, nil, "If-Match", `"r0"`, "Idempotency-Key", "nm-1")
	if r.Status != 201 || r.str("kind") != "notice" || r.Body["current"].(map[string]any)["from"] != "fyniq-notice-draft@1" ||
		!strings.Contains(r.str("summary"), "Data Protection Officer") {
		t.Fatalf("publishing the members' notice: %d %s", r.Status, r.Raw)
	}
	if a := e.call(http.MethodPost, "/v1/compliance/policies/notice-members/versions/1/acknowledgements", member, nil, "If-Match", `"v1"`,
		"Idempotency-Key", "nm-ack"); a.Status != 201 || a.Body["mine"].(map[string]any)["acknowledged_at"] == nil {
		t.Fatalf("acknowledging it: %d %s", a.Status, a.Raw)
	}

	// The contacts' notice: published, never acknowledged by a member.
	r = e.call(http.MethodPost, "/v1/compliance/policies/notice-contacts/versions", dpo, nil, "If-Match", `"r0"`, "Idempotency-Key", "nc-1")
	if r.Status != 201 || r.Body["mine"] != nil || !strings.HasPrefix(r.str("summary"), "We keep the names") {
		t.Fatalf("publishing the contacts' notice: %d %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/compliance/policies/notice-contacts/versions/1/acknowledgements", member, nil, "If-Match", `"v1"`,
		"Idempotency-Key", "nc-ack"), "acknowledgePolicyVersion", 422, CodeInvalid)
}

// What Fyniq sends ends with the privacy footer (CMP-PD-01, CMP-PD-03): an email to a contact, the contacts' notice's
// summary and the DPO's contact; an email to a member, the DPO's contact alone.
// req: CMP-PD-03
// req: CMP-PD-01
func TestWhatFyniqSendsEndsWithThePrivacyFooter(t *testing.T) {
	e := newEnv(t)
	if r := e.call(http.MethodPost, "/v1/compliance/policies/notice-contacts/versions", e.as(authz.DPO), nil, "If-Match", `"r0"`,
		"Idempotency-Key", "nc-1"); r.Status != 201 {
		t.Fatalf("publishing the contacts' notice: %d %s", r.Status, r.Raw)
	}
	e.readyToEmail()
	if p := e.call(http.MethodPatch, "/v1/organisation/profile", e.as(authz.Controller), map[string]any{"dpo_name": "Grace Lim",
		"dpo_email": "dpo@northwind.example", "email": "accounts@northwind.example"}, "If-Match", "*"); p.Status != 200 {
		t.Fatalf("naming the DPO: %d %s", p.Status, p.Raw)
	}
	queuedEmailTexts(e)
	dpo := "Questions about your personal data: Grace Lim, Data Protection Officer, dpo@northwind.example"

	id, tag := e.issuedInvoice("1", "100.00")
	if r := e.call(http.MethodPost, "/v1/invoices/"+id+"/email", e.as(authz.Accountant), nil, "If-Match", tag); r.Status != 200 {
		t.Fatalf("emailInvoice: %d %s", r.Status, r.Raw)
	}
	texts := queuedEmailTexts(e)
	if len(texts) != 1 || !strings.Contains(texts[0], "\n-- \nWe keep the names") || !strings.HasSuffix(texts[0], dpo+"\n") {
		t.Errorf("the invoice's email: %q", texts)
	}

	if s := e.call(http.MethodPost, "/v1/organisation/email/test-messages", e.as(authz.Administrator), nil); s.Status != http.StatusCreated {
		t.Fatalf("a test message: %d %s", s.Status, s.Raw)
	}
	texts = queuedEmailTexts(e)
	if len(texts) != 1 || strings.Contains(texts[0], "We keep the names") || !strings.HasSuffix(texts[0], "\n-- \n"+dpo+"\n") {
		t.Errorf("an email to a member: %q", texts)
	}
	if !email.ToContacts(email.PurposeInvoice) || email.ToContacts("notification") {
		t.Error("which purposes go to contacts")
	}
}

// Consent where a purpose needs it (CMP-PD-03): none by default; the DPO adds a purpose that needs it and records a
// contact's consent, then its withdrawal; a consent given twice, one for a purpose that does not need it, one for a
// member under a contacts' purpose, and anyone but the DPO are refused.
// req: CMP-PD-03
// req: access/roles-and-segregation Permission matrix: Data protection (PDPA)
func TestConsentWhereAPurposeNeedsIt(t *testing.T) {
	e := newEnv(t)
	dpo := e.as(authz.DPO)
	if r := e.call(http.MethodGet, "/v1/compliance/consent-purposes", e.as(authz.Auditor), nil); r.Status != 200 || len(r.Body["items"].([]any)) != 0 {
		t.Fatalf("no purpose by default: %d %s", r.Status, r.Raw)
	}
	news := e.call(http.MethodPost, "/v1/compliance/consent-purposes", dpo, map[string]any{"name": "Newsletter",
		"description": "A monthly email about our products, to contacts who ask for it.", "audience": "contacts", "needs_consent": true},
		"Idempotency-Key", "p-1")
	billing := e.call(http.MethodPost, "/v1/compliance/consent-purposes", dpo, map[string]any{"name": "Billing",
		"description": "Invoices and statements.", "audience": "contacts", "needs_consent": false}, "Idempotency-Key", "p-2")
	if news.Status != 201 || billing.Status != 201 || news.Body["given"] != float64(0) {
		t.Fatalf("adding purposes: %d %s %d", news.Status, news.Raw, billing.Status)
	}
	contact := "contact:" + e.directorContact("Ada Tan")
	consent := func(purpose, person, event, key string) response {
		return e.call(http.MethodPost, "/v1/compliance/consents", dpo, map[string]any{"purpose_id": purpose, "person": person, "event": event,
			"note": "Asked by email"}, "Idempotency-Key", key)
	}
	if r := consent(news.str("id"), contact, "given", "c-1"); r.Status != 201 || r.str("event") != "given" {
		t.Fatalf("a consent: %d %s", r.Status, r.Raw)
	}
	e.problemIs(consent(news.str("id"), contact, "given", "c-2"), "recordConsent", 409, "STORAGE-CONSENT-01")
	e.problemIs(consent(billing.str("id"), contact, "given", "c-3"), "recordConsent", 422, CodeInvalid)
	e.problemIs(consent(news.str("id"), "member:01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10", "given", "c-4"), "recordConsent", 422, CodeInvalid)
	if r := e.call(http.MethodGet, "/v1/compliance/consent-purposes", dpo, nil); r.Body["items"].([]any)[0].(map[string]any)["given"] != float64(1) {
		t.Errorf("the consent stands: %s", r.Raw)
	}
	if r := consent(news.str("id"), contact, "withdrawn", "c-5"); r.Status != 201 {
		t.Fatalf("a withdrawal: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodGet, "/v1/compliance/consents?person="+contact, dpo, nil); len(r.Body["items"].([]any)) != 2 {
		t.Errorf("the contact's consents: %s", r.Raw)
	}
	if trail := auditTrail(t, e); !strings.Contains(trail, "consent.given") || !strings.Contains(trail, "consent.withdrawn") ||
		!strings.Contains(trail, "consent_purpose.created") {
		t.Error("consent is not in the audit trail")
	}
	e.problemIs(e.call(http.MethodGet, "/v1/compliance/consents", e.as(authz.Controller), nil), "listConsents", 403, CodeForbidden)
	e.problemIs(e.call(http.MethodPost, "/v1/compliance/consent-purposes", e.as(authz.Owner), map[string]any{"name": "X", "description": "Y",
		"audience": "members", "needs_consent": true}), "createConsentPurpose", 403, CodeForbidden)
}

// matrixConsent are the authorisation matrix's rows for consent (CMP-PD-03, Appendix D "Data protection (PDPA)").
var matrixConsent = map[string]func(e *env, tok string) (response, func() bool){
	"listConsentPurposes": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/compliance/consent-purposes", tok, nil), nil
	},
	"listConsents": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/compliance/consents", tok, nil), nil
	},
	"createConsentPurpose": func(e *env, tok string) (response, func() bool) {
		was := matrixConsentState(e)
		r := e.call(http.MethodPost, "/v1/compliance/consent-purposes", tok, map[string]any{"name": "Matrix", "description": "A purpose.",
			"audience": "members", "needs_consent": true}, "Prefer", "return=preview")
		return r, func() bool { return matrixConsentState(e) == was }
	},
	"recordConsent": func(e *env, tok string) (response, func() bool) {
		id := matrixConsentPurpose(e)
		was := matrixConsentState(e)
		r := e.call(http.MethodPost, "/v1/compliance/consents", tok, map[string]any{"purpose_id": id,
			"person": "member:01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10", "event": "given"}, "Prefer", "return=preview")
		return r, func() bool { return matrixConsentState(e) == was }
	},
}

var matrixConsentPurposeOf = map[*env]string{}

// matrixConsentPurpose is a purpose for members that needs consent, so the matrix's consent is one the API takes.
func matrixConsentPurpose(e *env) string {
	e.t.Helper()
	if id, ok := matrixConsentPurposeOf[e]; ok {
		return id
	}
	r := e.call(http.MethodPost, "/v1/compliance/consent-purposes", e.as(authz.DPO), map[string]any{"name": "Matrix consent",
		"description": "A purpose that needs consent.", "audience": "members", "needs_consent": true}, "Idempotency-Key", "matrix-consent-purpose")
	if r.Status != http.StatusCreated {
		e.t.Fatalf("the matrix's consent purpose: %d %s", r.Status, r.Raw)
	}
	matrixConsentPurposeOf[e] = r.str("id")
	return r.str("id")
}

func matrixConsentState(e *env) string {
	return e.call(http.MethodGet, "/v1/compliance/consent-purposes", e.as(authz.DPO), nil).Raw +
		e.call(http.MethodGet, "/v1/compliance/consents", e.as(authz.DPO), nil).Raw
}
