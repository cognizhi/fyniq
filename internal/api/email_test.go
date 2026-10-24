//go:build local

package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/email"
	"github.com/cognizhi/fyniq/internal/mailhooks"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/storage"
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

// The email settings show the deployment's providers in order, its sender and sending domains, never a key, and each
// purpose's sender; an Administrator chooses a sender on a sending domain over the choice read, and a choice of
// neither field returns it to the platform's. Each choice is audited.
// req: FR-INT-21
func TestAnOrganisationChoosesItsSendersOnTheDeploymentsDomains(t *testing.T) {
	e := newEnv(t)
	admin := e.as(authz.Administrator)
	got := e.call(http.MethodGet, "/v1/organisation/email", e.as(authz.Auditor), nil)
	if got.Status != http.StatusOK || got.str("platform_sender") != "Fyniq <no-reply@mail.example.com>" {
		t.Fatalf("the settings: %d %s", got.Status, got.Raw)
	}
	if p := got.Body["providers"].([]any); len(p) != 2 || p[0].(map[string]any)["name"] != "zeptomail" || p[0].(map[string]any)["role"] != "primary" ||
		p[1].(map[string]any)["role"] != "failover" {
		t.Fatalf("the providers: %v", p)
	}
	if strings.Contains(strings.ReplaceAll(got.Raw, "_domainkey", ""), "key") {
		t.Fatalf("the settings mention a key: %s", got.Raw)
	}
	checks, _ := got.Body["domain_checks"].([]any)
	if len(checks) != 2 || checks[0].(map[string]any)["ok"] != false || checks[0].(map[string]any)["dmarc"] != "none" ||
		checks[0].(map[string]any)["records"].([]any)[0].(map[string]any)["status"] != "wrong" || checks[1].(map[string]any)["ok"] != true {
		t.Fatalf("the domain checks: %v", got.Body["domain_checks"])
	}
	senders := got.Body["senders"].([]any)
	if len(senders) != 4 || senders[1].(map[string]any)["purpose"] != "invoices" || senders[1].(map[string]any)["platform"] != true ||
		senders[1].(map[string]any)["from"] != "Fyniq <no-reply@mail.example.com>" {
		t.Fatalf("the senders: %v", senders)
	}
	const path = "/v1/organisation/email/senders/invoices"
	tag := senders[1].(map[string]any)["etag"].(string)

	for name, c := range map[string]struct {
		body map[string]any
		code string
	}{
		"another domain":         {map[string]any{"address": "billing@gmail.com"}, CodeSenderDomain},
		"not an address":         {map[string]any{"address": "Billing <billing@mail.example.com>"}, CodeSenderAddress},
		"a name with no address": {map[string]any{"display_name": "Billing"}, CodeSenderAddress},
		"a name with a newline":  {map[string]any{"address": "billing@mail.example.com", "display_name": "Billing\nBcc: x"}, CodeSenderAddress},
	} {
		if r := e.call(http.MethodPut, path, admin, c.body, "If-Match", tag); r.Status != http.StatusUnprocessableEntity || r.problemCode() != c.code {
			t.Errorf("%s: %d %s; want %s", name, r.Status, r.Raw, c.code)
		}
	}
	if r := e.call(http.MethodPut, path, admin, map[string]any{"address": "billing@mail.example.com"}, "If-Match", `"stale"`); r.Status != http.StatusPreconditionFailed {
		t.Fatalf("over a stale choice: %d %s", r.Status, r.Raw)
	}
	set := e.call(http.MethodPut, path, admin, map[string]any{"address": "Billing@MAIL.example.com", "display_name": "Example Pte Ltd"}, "If-Match", tag,
		"Idempotency-Key", uuid.NewString())
	if set.Status != http.StatusOK || set.str("address") != "Billing@mail.example.com" || set.str("from") != `"Example Pte Ltd" <Billing@mail.example.com>` ||
		set.Body["platform"] != false || set.Header.Get("ETag") == "" || set.Header.Get("ETag") == tag {
		t.Fatalf("choosing a sender: %d %s %s", set.Status, set.Header.Get("ETag"), set.Raw)
	}
	back := e.call(http.MethodPut, path, admin, map[string]any{}, "If-Match", set.Header.Get("ETag"))
	if back.Status != http.StatusOK || back.Body["platform"] != true || back.str("from") != "Fyniq <no-reply@mail.example.com>" {
		t.Fatalf("back to the platform's sender: %d %s", back.Status, back.Raw)
	}
	if trail := auditTrail(t, e); strings.Count(trail, "email_sender.set") != 2 {
		t.Fatal("the two choices are not both in the audit trail")
	}
}

// hookKey is the test Resend endpoint's key; hookSecret its signing secret as Resend shows it.
var (
	hookKey    = []byte("a test key of thirty-two bytes!!")
	hookSecret = "whsec_" + base64.StdEncoding.EncodeToString(hookKey)
)

// hooks serves the email webhooks of e's store.
func hooks(e *env) *httptest.Server {
	mux := http.NewServeMux()
	(&mailhooks.Receiver{Store: e.store, Clock: e.clock, ResendSecret: hookSecret, ZeptoMailKey: "agent-key",
		Notify: &outbox.Dispatcher{Store: e.store, Queue: e.queue, Clock: e.clock}}).Register(mux)
	srv := httptest.NewServer(mux)
	e.t.Cleanup(srv.Close)
	return srv
}

// resendHook posts body to srv as Resend delivery id, signed with the key when signed.
func resendHook(t *testing.T, e *env, srv *httptest.Server, id, body string, signed bool) int {
	t.Helper()
	ts := strconv.FormatInt(e.clock.Now().Unix(), 10)
	mac := hmac.New(sha256.New, hookKey)
	mac.Write([]byte(id + "." + ts + "." + body))
	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	if !signed {
		sig = base64.StdEncoding.EncodeToString([]byte("forged"))
	}
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/webhooks/email/resend", strings.NewReader(body))
	req.Header.Set("svix-id", id)
	req.Header.Set("svix-timestamp", ts)
	req.Header.Set("svix-signature", "v1,"+sig)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	return res.StatusCode
}

// zeptoHook posts the events payload to srv as ZeptoMail does: one form value, signed with the Agent's key.
func zeptoHook(t *testing.T, e *env, srv *httptest.Server, payload string) int {
	t.Helper()
	mac := hmac.New(sha256.New, []byte("agent-key"))
	mac.Write([]byte(payload))
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/webhooks/email/zeptomail", strings.NewReader("payload="+url.QueryEscape(payload)))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("producer-signature", fmt.Sprintf("ts=%d;s=%s;s-algorithm=HmacSHA256", e.clock.Now().UnixMilli(),
		url.QueryEscape(base64.StdEncoding.EncodeToString(mac.Sum(nil)))))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	return res.StatusCode
}

// A provider's signed webhook records its event on the message once, however often it is retried; a hard bounce
// suppresses the address, a soft bounce does not; a forged request, or an event about another recipient, records
// nothing. A new suppression is notified to those who release it; an Administrator releases it, once; each step is
// audited.
// req: FR-INT-21
// req: FR-NTF-01
func TestTheProvidersWebhooksRecordEventsAndSuppressHardBounces(t *testing.T) {
	e := newEnv(t)
	srv := hooks(e)
	id := matrixEmail(e)
	ref := email.Reference(e.books.Org, uuid.MustParse(id))
	bounce := func(to string) string {
		return `{"type":"email.bounced","created_at":"` + e.clock.Now().Add(-time.Minute).UTC().Format(time.RFC3339) + `","data":{"email_id":"e-1","to":["` + to + `"],` +
			`"tags":[{"name":"fyniq_ref","value":"` + ref + `"}],"bounce":{"type":"Permanent","message":"mailbox does not exist"}}}`
	}
	if got := resendHook(t, e, srv, "msg_forged", bounce("accounts@example.com"), false); got != http.StatusUnauthorized {
		t.Fatalf("a forged webhook: %d", got)
	}
	if got := resendHook(t, e, srv, "msg_other", bounce("someone@example.com"), true); got != http.StatusNoContent {
		t.Fatalf("an event about another recipient: %d", got)
	}
	for i := 0; i < 2; i++ { // Resend retries the same delivery
		if got := resendHook(t, e, srv, "msg_1", bounce("accounts@example.com"), true); got != http.StatusNoContent {
			t.Fatalf("the bounce, delivery %d: %d", i+1, got)
		}
	}
	soft := `{"webhook_request_id":"w-1","event_message":[{"email_info":{"client_reference":"` + ref + `","to":[{"email_address":` +
		`{"address":"accounts@example.com"}}]},"event_data":[{"object":"softbounce","details":[{"reason":"Mailbox full"}]}]}]}`
	if got := zeptoHook(t, e, srv, soft); got != http.StatusNoContent {
		t.Fatalf("a soft bounce from ZeptoMail: %d", got)
	}

	m := e.call(http.MethodGet, "/v1/organisation/email/messages/"+id, e.as(authz.Auditor), nil)
	evs, _ := m.Body["events"].([]any)
	if len(evs) != 2 || evs[0].(map[string]any)["kind"] != "hard_bounce" || evs[0].(map[string]any)["provider"] != "resend" ||
		evs[0].(map[string]any)["detail"] != "mailbox does not exist" || evs[1].(map[string]any)["kind"] != "soft_bounce" {
		t.Fatalf("the message's events: %s", m.Raw)
	}
	list := e.call(http.MethodGet, "/v1/organisation/email/suppressions", e.as(authz.Auditor), nil)
	items, _ := list.Body["items"].([]any)
	if list.Status != http.StatusOK || len(items) != 1 || items[0].(map[string]any)["address"] != "accounts@example.com" ||
		items[0].(map[string]any)["reason"] != "hard_bounce" {
		t.Fatalf("the suppressions: %d %s", list.Status, list.Raw)
	}
	sid := items[0].(map[string]any)["id"].(string)
	rel := e.call(http.MethodPost, "/v1/organisation/email/suppressions/"+sid+"/release", e.as(authz.Administrator), nil, "Idempotency-Key", uuid.NewString())
	if rel.Status != http.StatusOK || rel.str("released_at") == "" {
		t.Fatalf("releasing it: %d %s", rel.Status, rel.Raw)
	}
	if again := e.call(http.MethodPost, "/v1/organisation/email/suppressions/"+sid+"/release", e.as(authz.Administrator), nil); again.Status != http.StatusConflict ||
		again.problemCode() != CodeReleased {
		t.Fatalf("releasing it again: %d %s", again.Status, again.Raw)
	}
	if after := e.call(http.MethodGet, "/v1/organisation/email/suppressions", e.as(authz.Auditor), nil); len(after.Body["items"].([]any)) != 0 {
		t.Fatalf("a released suppression is still listed: %s", after.Raw)
	}
	if all := e.call(http.MethodGet, "/v1/organisation/email/suppressions?all=true", e.as(authz.Auditor), nil); len(all.Body["items"].([]any)) != 1 {
		t.Fatalf("all=true leaves out the released one: %s", all.Raw)
	}
	trail := auditTrail(t, e)
	for _, action := range []string{"email_message.event_recorded", "email_suppression.added", "email_suppression.released", "integration"} {
		if !strings.Contains(trail, action) {
			t.Errorf("the audit trail lacks %s", action)
		}
	}
	if strings.Count(trail, "email_message.event_recorded") != 2 {
		t.Errorf("%d events recorded; want 2", strings.Count(trail, "email_message.event_recorded"))
	}
	if n := strings.Count(trail, `"email.suppressed"`); n != 1 {
		t.Errorf("%d notifications of the suppression; want 1, to those who release it", n)
	}
}

// matrixSuppression suppresses a fresh address, as a provider's hard bounce would, and returns the suppression's ID.
func matrixSuppression(e *env) string {
	id := uuid.MustParse(matrixEmail(e))
	var sid string
	if err := e.store.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		address := "bounced-" + uuid.NewString() + "@example.com"
		ev, _, err := u.EmailMessages().RecordEvent(ctx, storage.EmailEvent{MessageID: id, Provider: "resend", DeliveryID: uuid.NewString(),
			Kind: storage.EventHardBounce, Recipient: address, OccurredAt: e.clock.Now()})
		if err != nil {
			return err
		}
		s, _, err := u.EmailSuppressions().Suppress(ctx, address, storage.EventHardBounce, ev.ID)
		sid = s.ID.String()
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
	return sid
}
