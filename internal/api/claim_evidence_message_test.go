//go:build local

package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/evidence"
	"github.com/cognizhi/fyniq/internal/email"
	"github.com/cognizhi/fyniq/internal/mailhooks"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/outbox"
)

// memberMail makes e's member user an email-in address of their own and has the worker fetch one message to it from sender,
// then returns the message as the member lists it.
func memberMailFetched(t *testing.T, e *env, srv *httptest.Server, worker *postgres.Store, staging *local.Dir, tok, name, sender string) map[string]any {
	t.Helper()
	received(t, e, srv, name+"@in.example.com", sender)
	k, row := nextWorkRow(t, e)
	if k != email.FetchKind {
		t.Fatalf("queued %q; want %s", k, email.FetchKind)
	}
	fetcher := email.Fetcher{Mail: strangerMail{from: sender}, Store: worker, Files: staging, Scanner: local.EICARScanner{}, Clock: e.clock,
		Quarantined: notify.Quarantined(outbox.Dispatcher{Store: worker, Queue: e.queue, Clock: e.clock})}
	if err := fetcher.Work()(context.Background(), e.books.Org, row); err != nil {
		t.Fatal(err)
	}
	fanOutAll(t, e) // the quarantine's notice, which waits in the queue ahead of the next fetch
	return memberMailAgain(t, e, tok, strings.ToLower(sender))
}

func messageAttachmentOf(m map[string]any) string {
	for _, x := range m["files"].([]any) {
		if f := x.(map[string]any); f["kind"] == "attachment" {
			return f["id"].(string)
		}
	}
	return ""
}

func messageTextOf(m map[string]any) string {
	for _, x := range m["files"].([]any) {
		if f := x.(map[string]any); f["kind"] == "body" {
			return f["id"].(string)
		}
	}
	return ""
}

// A receipt a member forwarded to their own email-in address attaches to their own claim (FR-AP-14, FR-DOC-12; rev 0.322):
// the file is checked again as an upload is and kept as evidence from email; only an attachment of accepted mail at the
// member's own address qualifies, so another member's mail, quarantined mail, a message's text and an unknown file are all
// EML-10 and tell nothing; only the claimant attaches; a stale ETag is refused; the same Idempotency-Key attaches once.
// req: FR-AP-14
// req: FR-DOC-12
// req: purchases/expense-claims Receipt forwarded to the member's own address (rev 0.322)
// asvs: V8.2.2, V4.2.1
func TestAReceiptForwardedToTheMembersAddressAttachesToTheirClaim(t *testing.T) {
	c := newClaimants(t)
	e := c.e
	ahkow, mei := c.as(c.ahkow, authz.Employee), c.as(c.mei, authz.Employee)
	name := "ahkow-" + uuid.NewString()[:6]
	if r := e.call(http.MethodPut, "/v1/organisation/email/capture-addresses/mine", ahkow, map[string]any{"name": name}, "If-Match", `"none"`); r.Status != 200 {
		t.Fatalf("naming Ah Kow's address: %d %s", r.Status, r.Raw)
	}
	mux := http.NewServeMux()
	(&mailhooks.Receiver{Store: e.store, Clock: e.clock, ResendSecret: hookSecret,
		Inbound: &mailhooks.Inbound{Domain: "in.example.com", Routes: e.store, Dispatcher: outbox.Dispatcher{Store: e.store, Queue: e.queue, Clock: e.clock}}}).Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	worker, err := postgres.Open(context.Background(), db.URL("fyniq_worker"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(worker.Close)
	staging, err := local.NewDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e.api.deps.Staging = staging
	drainWork(t, e)

	// Two messages: a friend's, whom Ah Kow accepts, and a stranger's, which stays in quarantine.
	friend := memberMailFetched(t, e, srv, worker, staging, ahkow, name, "friend@example.net")
	stranger := memberMailFetched(t, e, srv, worker, staging, ahkow, name, "stranger@example.org")
	if friend["status"] != "quarantined" || stranger["status"] != "quarantined" {
		t.Fatalf("both start quarantined: %v %v", friend["status"], stranger["status"])
	}
	if r := e.call(http.MethodPost, "/v1/organisation/email/capture-senders/mine", ahkow, map[string]any{"sender": "friend@example.net", "decision": "accepted"}); r.Status != 200 {
		t.Fatalf("accepting the friend: %d %s", r.Status, r.Raw)
	}
	friend = memberMailAgain(t, e, ahkow, "friend@example.net")
	file := messageAttachmentOf(friend)
	if friend["status"] != "accepted" || file == "" {
		t.Fatalf("the friend's mail: %v", friend)
	}

	travel, _ := e.claimSetup()
	claim := e.call(http.MethodPost, "/v1/expense-claims", ahkow, map[string]any{"date": "2026-10-15", "lines": []any{taxi(travel)}})
	if claim.Status != http.StatusCreated {
		t.Fatalf("drafting the claim: %d %s", claim.Status, claim.Raw)
	}
	path := "/v1/expense-claims/" + claim.str("id") + "/evidence:from-message"
	tag := claim.Header.Get("ETag")
	attach := func(tok string, body map[string]any, headers ...string) response {
		t.Helper()
		return e.call(http.MethodPost, path, tok, body, headers...)
	}
	good := map[string]any{"message_id": friend["id"], "file_id": file, "note": "Forwarded from the cab app"}

	// Refused without the claim's version, or with a stale one; a preview saves nothing.
	e.problemIs(attach(ahkow, good), "addExpenseClaimEvidenceFromMessage", 428, CodeIfMatchRequired)
	e.problemIs(attach(ahkow, good, "If-Match", `"0-stale"`), "addExpenseClaimEvidenceFromMessage", 412, CodeStale)
	if r := attach(ahkow, good, "If-Match", tag, "Prefer", "return=preview"); r.Status != http.StatusOK || r.Header.Get("Preference-Applied") != "return=preview" {
		t.Fatalf("a preview: %d %s", r.Status, r.Raw)
	}
	if got := e.call(http.MethodGet, "/v1/expense-claims/"+claim.str("id")+"/evidence", ahkow, nil).Body["items"].([]any); len(got) != 0 {
		t.Fatalf("a preview kept a file: %v", got)
	}

	key := uuid.NewString()
	r := attach(ahkow, good, "If-Match", tag, "Idempotency-Key", key)
	if r.Status != http.StatusCreated || r.str("source") != "email" || r.str("file_name") != "INV-0042.pdf" || r.str("media_type") != evidence.PDF ||
		r.str("evidence_type") != "receipt" || r.str("note") != "Forwarded from the cab app" || r.str("claim_id") != claim.str("id") {
		t.Fatalf("attaching the forwarded receipt: %d %s", r.Status, r.Raw)
	}
	again := attach(ahkow, good, "If-Match", tag, "Idempotency-Key", key)
	if again.Status != http.StatusCreated || again.Header.Get("Idempotent-Replayed") != "true" || again.str("id") != r.str("id") {
		t.Fatalf("the same Idempotency-Key: %d %s", again.Status, again.Raw)
	}
	if got := e.call(http.MethodGet, "/v1/expense-claims/"+claim.str("id")+"/evidence", ahkow, nil).Body["items"].([]any); len(got) != 1 {
		t.Fatalf("the claim holds %d files; want 1", len(got))
	}

	// What does not qualify is EML-10 and says no more.
	next := e.call(http.MethodGet, "/v1/expense-claims/"+claim.str("id"), ahkow, nil).Header.Get("ETag")
	refused := func(name string, tok string, body map[string]any) {
		t.Helper()
		e.problemIs(attach(tok, body, "If-Match", next), "addExpenseClaimEvidenceFromMessage", 422, CodeMessageFile)
		_ = name
	}
	refused("the message's own text", ahkow, map[string]any{"message_id": friend["id"], "file_id": messageTextOf(friend)})
	refused("quarantined mail", ahkow, map[string]any{"message_id": stranger["id"], "file_id": messageAttachmentOf(stranger)})
	refused("an unknown file", ahkow, map[string]any{"message_id": friend["id"], "file_id": uuid.NewString()})
	refused("an unknown message", ahkow, map[string]any{"message_id": uuid.NewString(), "file_id": file})
	// Mei's own claim cannot take Ah Kow's mail: the message is not at her address, and she cannot tell whether it exists.
	meiClaim, _ := e.ownClaimOf(uuid.MustParse(c.mei))
	e.problemIs(e.call(http.MethodPost, "/v1/expense-claims/"+meiClaim+"/evidence:from-message", mei, good, "If-Match", "*"),
		"addExpenseClaimEvidenceFromMessage", 422, CodeMessageFile)
	// Ah Kow's claim with Ah Kow's mail is no more Mei's: the mail refuses her first. The AI never attaches.
	e.problemIs(e.call(http.MethodPost, path, mei, good, "If-Match", next), "addExpenseClaimEvidenceFromMessage", 422, CodeMessageFile)
	if r := e.call(http.MethodPost, path, e.as(authz.AI), good, "If-Match", next); r.Status != http.StatusForbidden {
		t.Errorf("the AI attached a receipt: %d %s", r.Status, r.Raw)
	}
	if trail := auditTrail(t, e); !strings.Contains(trail, `"email"`) {
		t.Errorf("the audit trail does not record the email source: %.300s", trail)
	}
}

// memberMail2 lists the member's mail again and returns the message from sender.
func memberMailAgain(t *testing.T, e *env, tok, sender string) map[string]any {
	t.Helper()
	for _, x := range e.call(http.MethodGet, "/v1/organisation/email/inbound-messages/mine", tok, nil).Body["items"].([]any) {
		if m := x.(map[string]any); m["sender"] == sender {
			return m
		}
	}
	t.Fatalf("no message from %s", sender)
	return nil
}
