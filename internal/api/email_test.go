//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/email"
)

// matrixProfileEmail gives the organisation an email address if it has none.
func matrixProfileEmail(e *env) {
	if e.call(http.MethodGet, "/v1/organisation/profile", e.as(authz.Controller), nil).str("email") == "" {
		e.call(http.MethodPatch, "/v1/organisation/profile", e.as(authz.Controller), map[string]any{"email": "accounts@example.com"}, "If-Match", "*")
	}
}

// matrixEmail sends a test message as an Administrator and returns the message's ID.
func matrixEmail(e *env) string {
	matrixProfileEmail(e)
	r := e.call(http.MethodPost, "/v1/organisation/email/test-messages", e.as(authz.Administrator), nil)
	if r.Status != http.StatusCreated {
		e.t.Fatalf("sendTestEmail = %d: %s", r.Status, r.Raw)
	}
	return r.str("id")
}

// drainWork empties the queue of the worker class's tasks.
func drainWork(t *testing.T, e *env) {
	t.Helper()
	for nextWork(t, e) != "" {
	}
}

// A test message goes only to the organisation's own address from its profile: refused while the profile has
// none; queued once with Idempotency-Key, its send in the outbox for the worker; a preview queues nothing; the
// record shows its outcome and never its body, and its queueing is audited.
// req: FR-INT-21
// req: FR-NTF-05
func TestATestEmailGoesOnlyToTheOrganisationsOwnAddress(t *testing.T) {
	e := newEnv(t)
	admin := e.as(authz.Administrator)
	const path = "/v1/organisation/email/test-messages"

	r := e.call(http.MethodPost, path, admin, nil, "Idempotency-Key", uuid.NewString())
	if r.Status != http.StatusConflict || r.problemCode() != CodeNoEmailAddress {
		t.Fatalf("with no email in the profile: %d %s", r.Status, r.Raw)
	}
	if k := nextWork(t, e); k != "" {
		t.Fatalf("a refused test queued %s", k)
	}

	if p := e.call(http.MethodPatch, "/v1/organisation/profile", e.as(authz.Controller), map[string]any{"email": "accounts@example.com"},
		"If-Match", "*"); p.Status != http.StatusOK {
		t.Fatalf("setting the profile's email: %d %s", p.Status, p.Raw)
	}

	pv := e.call(http.MethodPost, path, admin, nil, "Prefer", "return=preview")
	if pv.Status != http.StatusOK || pv.Header.Get("Preference-Applied") != "return=preview" || pv.str("recipient") != "accounts@example.com" {
		t.Fatalf("a preview: %d %s %s", pv.Status, pv.Header.Get("Preference-Applied"), pv.Raw)
	}
	if k := nextWork(t, e); k != "" {
		t.Fatalf("a preview queued %s", k)
	}

	key := uuid.NewString()
	sent := e.call(http.MethodPost, path, admin, nil, "Idempotency-Key", key)
	id := sent.str("id")
	if sent.Status != http.StatusCreated || sent.str("status") != "queued" || sent.str("purpose") != "test" ||
		sent.str("recipient") != "accounts@example.com" || sent.str("subject") != "Fyniq test message" ||
		sent.Header.Get("Location") != "/v1/organisation/email/messages/"+id {
		t.Fatalf("a test message: %d %s %s", sent.Status, sent.Header.Get("Location"), sent.Raw)
	}
	for _, field := range []string{"text", "body"} {
		if _, ok := sent.Body[field]; ok {
			t.Errorf("the message's record shows its %s", field)
		}
	}
	again := e.call(http.MethodPost, path, admin, nil, "Idempotency-Key", key)
	if again.Status != http.StatusCreated || again.str("id") != id || again.Header.Get("Idempotent-Replayed") != "true" {
		t.Fatalf("a replay: %d %s %s", again.Status, again.Header.Get("Idempotent-Replayed"), again.Raw)
	}
	if k := nextWork(t, e); k != email.Kind {
		t.Fatalf("the send is queued as %q; want %q", k, email.Kind)
	}
	if k := nextWork(t, e); k != "" {
		t.Fatalf("the replay queued a second %s", k)
	}

	got := e.call(http.MethodGet, "/v1/organisation/email/messages/"+id, e.as(authz.Auditor), nil)
	if got.Status != http.StatusOK || got.str("status") != "queued" || got.Body["attempts"] != float64(0) {
		t.Fatalf("reading the message: %d %s", got.Status, got.Raw)
	}
	if r := e.call(http.MethodGet, "/v1/organisation/email/messages/"+uuid.NewString(), e.as(authz.Auditor), nil); r.Status != http.StatusNotFound ||
		r.problemCode() != CodeNotFound {
		t.Fatalf("an unknown message: %d %s", r.Status, r.Raw)
	}
	if trail := auditTrail(t, e); !strings.Contains(trail, "email_message.queued") || !strings.Contains(trail, id) {
		t.Fatal("the test message's queueing is not in the audit trail")
	}
}
