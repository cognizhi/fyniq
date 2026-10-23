package mailhooks_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/mailhooks"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

var now = time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)

// resendSecret is a test endpoint's signing secret: whsec_ and a base64 key.
var resendSecret = "whsec_" + base64.StdEncoding.EncodeToString([]byte("a test key of thirty-two bytes!!"))

// ResendHeaders signs body as Svix does, at at.
func resendHeaders(id string, at time.Time, body string) http.Header {
	mac := hmac.New(sha256.New, []byte("a test key of thirty-two bytes!!"))
	ts := strconv.FormatInt(at.Unix(), 10)
	mac.Write([]byte(id + "." + ts + "." + body))
	h := http.Header{}
	h.Set("svix-id", id)
	h.Set("svix-timestamp", ts)
	h.Set("svix-signature", "v1,b25lIG9sZCBzaWduYXR1cmU= v1,"+base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	return h
}

// A Resend webhook is accepted only with a v1 signature over its id, time and raw body under the endpoint's secret,
// within five minutes; its delivery ID is svix-id (Svix's "Verifying payloads manually", retrieved 23 Oct 2026).
// req: FR-INT-21
func TestAResendWebhookIsReadOnlyWhenSignedLately(t *testing.T) {
	body := `{"type":"email.bounced"}`
	if id, err := mailhooks.VerifyResend(resendSecret, resendHeaders("msg_1", now, body), []byte(body), now); err != nil || id != "msg_1" {
		t.Fatalf("a signed request: %q, %v", id, err)
	}
	for name, c := range map[string]struct {
		h    http.Header
		body string
	}{
		"another body":    {resendHeaders("msg_1", now, body), `{"type":"email.delivered"}`},
		"another id":      {func() http.Header { h := resendHeaders("msg_1", now, body); h.Set("svix-id", "msg_2"); return h }(), body},
		"six minutes old": {resendHeaders("msg_1", now.Add(-6*time.Minute), body), body},
		"no signature":    {http.Header{"Svix-Id": {"msg_1"}, "Svix-Timestamp": {strconv.FormatInt(now.Unix(), 10)}}, body},
		"signed with another key": {func() http.Header {
			h := resendHeaders("msg_1", now, body)
			h.Set("svix-signature", "v1,AAAA")
			return h
		}(), body},
	} {
		if _, err := mailhooks.VerifyResend(resendSecret, c.h, []byte(c.body), now); !errors.Is(err, mailhooks.ErrSignature) {
			t.Errorf("%s: %v; want refused", name, err)
		}
	}
}

// zeptoBody is a ZeptoMail webhook as Zoho's sample reads it: one form value holding the events as JSON, and its
// producer-signature under key, sent at at.
func zeptoBody(key, payload string, at time.Time) (http.Header, []byte) {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(payload))
	h := http.Header{}
	h.Set("producer-signature", fmt.Sprintf("ts=%d;s=%s;s-algorithm=HmacSHA256", at.UnixMilli(), url.QueryEscape(base64.StdEncoding.EncodeToString(mac.Sum(nil)))))
	return h, []byte("payload=" + url.QueryEscape(payload))
}

// A ZeptoMail webhook is accepted only with its producer-signature: HMAC-SHA256 of the form value under the Agent's
// authentication key, base64, with HmacSHA256 named and a time within five minutes (Zoho's webhook page, retrieved
// 23 Oct 2026).
// req: FR-INT-21
func TestAZeptoMailWebhookIsReadOnlyWhenSignedLately(t *testing.T) {
	const payload = `{"webhook_request_id":"w-1"}`
	h, body := zeptoBody("agent-key", payload, now)
	if got, err := mailhooks.VerifyZeptoMail("agent-key", h, body, now); err != nil || string(got) != payload {
		t.Fatalf("a signed request: %q, %v", got, err)
	}
	other, _ := zeptoBody("agent-key", `{"webhook_request_id":"w-2"}`, now)
	old, oldBody := zeptoBody("agent-key", payload, now.Add(-6*time.Minute))
	for name, c := range map[string]struct {
		key  string
		h    http.Header
		body []byte
	}{
		"another key":     {"other-key", h, body},
		"another body":    {"agent-key", other, body},
		"six minutes old": {"agent-key", old, oldBody},
		"no signature":    {"agent-key", http.Header{}, body},
	} {
		if _, err := mailhooks.VerifyZeptoMail(c.key, c.h, c.body, now); !errors.Is(err, mailhooks.ErrSignature) {
			t.Errorf("%s: %v; want refused", name, err)
		}
	}
}

// Resend's bounces, complaints, suppressions, delays and deliveries are read with the reference Fyniq tagged them
// with; a permanent bounce is hard, any other soft; sends, opens and clicks are not recorded.
// req: FR-INT-21
func TestResendsEventsAreReadWithTheirReference(t *testing.T) {
	const ref = "01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d00_01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e40"
	bounced := `{"type":"email.bounced","created_at":"2026-10-23T08:59:00.000Z","data":{"email_id":"e-1","to":["founder@example.com"],` +
		`"tags":[{"name":"fyniq_ref","value":"` + ref + `"}],"bounce":{"type":"Permanent","subType":"General","message":"mailbox does not exist"}}}`
	got, err := mailhooks.ParseResend("msg_1", []byte(bounced), now)
	if err != nil || len(got) != 1 || got[0].Kind != storage.EventHardBounce || got[0].Reference != ref || got[0].Recipient != "founder@example.com" ||
		got[0].DeliveryID != "msg_1" || got[0].Detail != "mailbox does not exist" || !got[0].OccurredAt.Equal(now.Add(-time.Minute)) {
		t.Fatalf("a hard bounce: %+v, %v", got, err)
	}
	soft := `{"type":"email.bounced","data":{"to":"Founder <founder@example.com>","tags":{"fyniq_ref":"` + ref + `"},"bounce":{"type":"Transient"}}}`
	if got, err := mailhooks.ParseResend("msg_2", []byte(soft), now); err != nil || len(got) != 1 || got[0].Kind != storage.EventSoftBounce ||
		got[0].Reference != ref || got[0].Recipient != "founder@example.com" || !got[0].OccurredAt.Equal(now) {
		t.Fatalf("a soft bounce, tags as an object: %+v, %v", got, err)
	}
	for typ, want := range map[string]string{"email.delivered": storage.EventDelivered, "email.complained": storage.EventComplaint,
		"email.suppressed": storage.EventSuppressed, "email.delivery_delayed": storage.EventDelayed, "email.opened": "", "email.sent": ""} {
		got, err := mailhooks.ParseResend("m", []byte(`{"type":"`+typ+`","data":{"to":["a@example.com"]}}`), now)
		if err != nil || (want == "") != (len(got) == 0) || (want != "" && got[0].Kind != want) {
			t.Errorf("%s: %+v, %v; want %q", typ, got, err, want)
		}
	}
	if _, err := mailhooks.ParseResend("m", []byte(`not json`), now); err == nil {
		t.Error("a body that is not JSON was read")
	}
}

// ZeptoMail's hard and soft bounces and complaints are read by their event_data object, with client_reference as
// the reference; opens and clicks are not recorded; event_message and event_data may each be one or many.
// req: FR-INT-21
func TestZeptoMailsEventsAreReadByTheirObject(t *testing.T) {
	payload := `{"event_name":"hard bounce","webhook_request_id":"w-9","event_message":[{"email_info":{"client_reference":"ref-1",` +
		`"to":[{"email_address":{"address":"founder@example.com","name":"Founder"}}]},"event_data":[{"object":"hardbounce",` +
		`"details":[{"reason":"Mailbox not found","diagnostic_message":"550 5.1.1 unknown user"}]}],"request_id":"r-1"}]}`
	got, err := mailhooks.ParseZeptoMail([]byte(payload), now)
	if err != nil || len(got) != 1 || got[0].Kind != storage.EventHardBounce || got[0].Reference != "ref-1" || got[0].Recipient != "founder@example.com" ||
		got[0].DeliveryID != "w-9" || got[0].Detail != "Mailbox not found 550 5.1.1 unknown user" {
		t.Fatalf("a hard bounce: %+v, %v", got, err)
	}
	one := `{"webhook_request_id":"w-10","event_message":{"email_info":{"client_reference":"ref-2","to":{"email_address":[{"address":"a@example.com"}]}},` +
		`"event_data":{"object":"fbl_complaint","details":{"from":"x"}}}}`
	if got, err := mailhooks.ParseZeptoMail([]byte(one), now); err != nil || len(got) != 1 || got[0].Kind != storage.EventComplaint || got[0].Recipient != "a@example.com" {
		t.Fatalf("a complaint, one of each: %+v, %v", got, err)
	}
	open := `{"webhook_request_id":"w-11","event_message":[{"email_info":{"client_reference":"ref-3","to":[]},"event_data":[{"object":"email_open"}]}]}`
	if got, err := mailhooks.ParseZeptoMail([]byte(open), now); err != nil || len(got) != 0 {
		t.Fatalf("an open is not recorded: %+v, %v", got, err)
	}
}

// Resend's email.received is read as a received message: its ID, sender, subject and every address it was sent to;
// its body and attachments are fetched later.
// req: FR-INT-22
func TestAReceivedEmailIsReadFromResendsWebhook(t *testing.T) {
	body := `{"type":"email.received","created_at":"2026-10-23T08:59:30Z","data":{"email_id":"e-1","created_at":"2026-10-23T08:59:00Z",` +
		`"from":"AR <ar@supplier.example>","to":["bills@in.example.com"],"received_for":["bills@in.example.com"],"subject":"Invoice",` +
		`"message_id":"<x@supplier.example>","attachments":[{"id":"a-1","filename":"INV.pdf"}]}}`
	got, err := mailhooks.ParseResend("msg_9", []byte(body), now)
	if err != nil || len(got) != 1 || got[0].Received == nil || got[0].Received.ID != "e-1" || got[0].Received.From != "AR <ar@supplier.example>" ||
		got[0].Received.Subject != "Invoice" || len(got[0].Received.To) != 2 || !got[0].Received.ReceivedAt.Equal(now.Add(-time.Minute)) {
		t.Fatalf("%+v, %v", got, err)
	}
	if _, err := mailhooks.ParseResend("msg_9", []byte(`{"type":"email.received","data":{}}`), now); err == nil {
		t.Fatal("a received email with no ID was read")
	}
}
