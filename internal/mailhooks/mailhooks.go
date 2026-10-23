// Package mailhooks receives the email providers' webhooks (FR-INT-21; PRD-001 rev 0.103): each request's signature
// is checked against the provider's secret before anything in it is read; each event about a message Fyniq sent is
// recorded once, however often the provider retries it; and a hard bounce, a complaint or the provider's own
// suppression puts the recipient on the organisation's suppression list. Opens and clicks are not recorded.
//
// An event names its message by the reference Fyniq sent with it (ZeptoMail's client_reference, Resend's fyniq_ref
// tag): the organisation and the message. The reference is trusted only because the signature is: an event whose
// message is not found, or whose recipient is not the message's, is ignored.
package mailhooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/cognizhi/fyniq/internal/adapters/resend"
	"github.com/cognizhi/fyniq/internal/email"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// MaxBody is the largest webhook request read.
const MaxBody = 256 << 10

// Receiver records the providers' webhooks. A provider without its secret has no receiver.
type Receiver struct {
	Store        storage.Store
	Clock        ports.Clock
	ResendSecret string // the endpoint's signing secret, whsec_…
	ZeptoMailKey string // the Mail Agent's webhook authentication key
	Log          *slog.Logger
}

// Mux is where the receivers are mounted.
type Mux interface {
	HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request))
}

// Register mounts POST /webhooks/email/resend and POST /webhooks/email/zeptomail, each only with its secret.
func (r *Receiver) Register(mux Mux) {
	if r.ResendSecret != "" {
		mux.HandleFunc("POST /webhooks/email/resend", r.handle("resend", func(h http.Header, body []byte, now time.Time) ([]Event, error) {
			id, err := VerifyResend(r.ResendSecret, h, body, now)
			if err != nil {
				return nil, err
			}
			return ParseResend(id, body, now)
		}))
	}
	if r.ZeptoMailKey != "" {
		mux.HandleFunc("POST /webhooks/email/zeptomail", r.handle("zeptomail", func(h http.Header, body []byte, now time.Time) ([]Event, error) {
			payload, err := VerifyZeptoMail(r.ZeptoMailKey, h, body, now)
			if err != nil {
				return nil, err
			}
			return ParseZeptoMail(payload, now)
		}))
	}
}

// Event is a provider's report about one message, as both providers' are read.
type Event struct {
	Reference  string // the organisation and the message, as Fyniq sent them
	DeliveryID string // the provider's ID of the delivery, the same on each retry
	Kind       string // storage.Event…
	Recipient  string
	Detail     string
	OccurredAt time.Time
}

// errMalformed means a signed request could not be read.
var errMalformed = errors.New("the request is signed but cannot be read")

func (r *Receiver) handle(provider string, read func(http.Header, []byte, time.Time) ([]Event, error)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
		body, err := io.ReadAll(io.LimitReader(req.Body, MaxBody+1))
		if err != nil || len(body) > MaxBody {
			http.Error(w, "the request is too large or unreadable", http.StatusRequestEntityTooLarge)
			return
		}
		events, err := read(req.Header, body, r.Clock.Now())
		switch {
		case errors.Is(err, ErrSignature):
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		case err != nil:
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ctx := storage.WithAudit(req.Context(), storage.AuditContext{Actor: &storage.Actor{Type: "integration", ID: provider}})
		for _, e := range events {
			if err := r.record(ctx, provider, e); err != nil {
				if r.Log != nil {
					r.Log.ErrorContext(ctx, "mailhooks.failure", "provider", provider, "delivery_id", e.DeliveryID, "error", err.Error())
				}
				http.Error(w, "the event could not be recorded; send it again", http.StatusInternalServerError)
				return
			}
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// record records e about its message, and suppresses the recipient after a hard bounce, a complaint or the provider's
// suppression. An event about no message of the organisation's, or about another recipient, is ignored.
func (r *Receiver) record(ctx context.Context, provider string, e Event) error {
	org, message, ok := email.ParseReference(e.Reference)
	if !ok {
		return nil // not a message Fyniq sent with a reference
	}
	return r.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		m, err := u.EmailMessages().Get(ctx, message)
		if errors.Is(err, ports.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if !strings.EqualFold(m.Recipient, e.Recipient) {
			return nil
		}
		ev, created, err := u.EmailMessages().RecordEvent(ctx, storage.EmailEvent{MessageID: message, Provider: provider, DeliveryID: e.DeliveryID,
			Kind: e.Kind, Recipient: m.Recipient, Detail: e.Detail, OccurredAt: e.OccurredAt})
		if err != nil || !created {
			return err
		}
		switch e.Kind {
		case storage.EventHardBounce, storage.EventComplaint, storage.EventSuppressed:
			_, _, err = u.EmailSuppressions().Suppress(ctx, m.Recipient, e.Kind, ev.ID)
		}
		return err
	})
}

// resendKinds are the Resend events recorded, and their kinds; the rest (sent, opened, clicked, scheduled,
// received) are not.
var resendKinds = map[string]string{
	"email.delivered":        storage.EventDelivered,
	"email.delivery_delayed": storage.EventDelayed,
	"email.complained":       storage.EventComplaint,
	"email.suppressed":       storage.EventSuppressed,
	"email.failed":           storage.EventFailed,
}

// ParseResend reads a Resend webhook delivered as svix-id id: one event per recipient, in the shape of Resend's
// event-type pages (retrieved 23 Oct 2026): type, created_at, and data with to, tags and, for a bounce, bounce.type
// ("Permanent" is a hard bounce, anything else a soft one) and bounce.message.
func ParseResend(id string, body []byte, now time.Time) ([]Event, error) {
	var in struct {
		Type      string `json:"type"`
		CreatedAt string `json:"created_at"`
		Data      struct {
			To     json.RawMessage `json:"to"`
			Tags   json.RawMessage `json:"tags"`
			Bounce *struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"bounce"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &in); err != nil || in.Type == "" {
		return nil, errMalformed
	}
	kind, detail := resendKinds[in.Type], ""
	if in.Type == "email.bounced" {
		kind = storage.EventSoftBounce
		if in.Data.Bounce != nil {
			if in.Data.Bounce.Type == "Permanent" {
				kind = storage.EventHardBounce
			}
			detail = in.Data.Bounce.Message
		}
	}
	if kind == "" {
		return nil, nil
	}
	ref := resendRef(in.Data.Tags)
	var to []string
	if err := json.Unmarshal(in.Data.To, &to); err != nil {
		var one string
		if json.Unmarshal(in.Data.To, &one) != nil {
			return nil, errMalformed
		}
		to = []string{one}
	}
	at := when(in.CreatedAt, now)
	out := make([]Event, 0, len(to))
	for i, t := range to {
		out = append(out, Event{Reference: ref, DeliveryID: delivery(id, i, len(to)), Kind: kind, Recipient: bare(t), Detail: detail, OccurredAt: at})
	}
	return out, nil
}

// resendRef is the fyniq_ref tag, whether Resend writes tags as a list of names and values or as an object.
func resendRef(raw json.RawMessage) string {
	var list []struct{ Name, Value string }
	if json.Unmarshal(raw, &list) == nil {
		for _, t := range list {
			if t.Name == resend.RefTag {
				return t.Value
			}
		}
		return ""
	}
	var obj map[string]string
	if json.Unmarshal(raw, &obj) == nil {
		return obj[resend.RefTag]
	}
	return ""
}

// zeptoKinds are the ZeptoMail events recorded, by their event_data object, and their kinds; opens and clicks are
// not recorded.
var zeptoKinds = map[string]string{
	"hardbounce":    storage.EventHardBounce,
	"softbounce":    storage.EventSoftBounce,
	"fbl_complaint": storage.EventComplaint,
}

// ParseZeptoMail reads a ZeptoMail webhook's events, in the shape of Zoho's webhook page (retrieved 23 Oct 2026):
// webhook_request_id, and event_message (one or many), each with email_info (client_reference, to) and event_data
// (one or many: object and details with reason and diagnostic_message). Either one or many is accepted where the page
// does not show which.
func ParseZeptoMail(payload []byte, now time.Time) ([]Event, error) {
	var in struct {
		RequestID    string          `json:"webhook_request_id"`
		EventMessage json.RawMessage `json:"event_message"`
	}
	if err := json.Unmarshal(payload, &in); err != nil || in.RequestID == "" {
		return nil, errMalformed
	}
	var out []Event
	for _, raw := range many(in.EventMessage) {
		var m struct {
			EmailInfo struct {
				ClientReference string          `json:"client_reference"`
				To              json.RawMessage `json:"to"`
			} `json:"email_info"`
			EventData json.RawMessage `json:"event_data"`
		}
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, errMalformed
		}
		to := addresses(m.EmailInfo.To)
		for _, d := range many(m.EventData) {
			var data struct {
				Object  string          `json:"object"`
				Details json.RawMessage `json:"details"`
			}
			if err := json.Unmarshal(d, &data); err != nil {
				return nil, errMalformed
			}
			kind := zeptoKinds[data.Object]
			if kind == "" {
				continue
			}
			var detail struct {
				Reason     string `json:"reason"`
				Diagnostic string `json:"diagnostic_message"`
				Time       string `json:"time"`
			}
			if ds := many(data.Details); len(ds) > 0 {
				_ = json.Unmarshal(ds[0], &detail)
			}
			for _, t := range to {
				out = append(out, Event{Reference: m.EmailInfo.ClientReference, Kind: kind, Recipient: t,
					Detail: strings.TrimSpace(detail.Reason + " " + detail.Diagnostic), OccurredAt: when(detail.Time, now)})
			}
		}
	}
	for i := range out {
		out[i].DeliveryID = delivery(in.RequestID, i, len(out))
	}
	return out, nil
}

// many is raw as a list: its items when it is an array, itself when it is an object, nothing otherwise.
func many(raw json.RawMessage) []json.RawMessage {
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) == nil {
		return list
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) == nil {
		return []json.RawMessage{raw}
	}
	return nil
}

// addresses are every "address" in raw, however deep: ZeptoMail nests them under email_address.
func addresses(raw json.RawMessage) []string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	var out []string
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case []any:
			for _, y := range x {
				walk(y)
			}
		case map[string]any:
			if a, ok := x["address"].(string); ok {
				out = append(out, a)
			}
			for k, y := range x {
				if k != "address" {
					walk(y)
				}
			}
		}
	}
	walk(v)
	return out
}

// bare is an address without its name: "Name <a@b>" is a@b.
func bare(s string) string {
	if i, j := strings.LastIndex(s, "<"), strings.LastIndex(s, ">"); i >= 0 && j > i {
		return s[i+1 : j]
	}
	return strings.TrimSpace(s)
}

// delivery is the delivery ID of the ith of n events in one request.
func delivery(id string, i, n int) string {
	if n == 1 {
		return id
	}
	return fmt.Sprintf("%s#%d", id, i)
}

// when is s as a time, if it is one, else now.
func when(s string, now time.Time) time.Time {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999-07", "2006-01-02T15:04:05.999-0700"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return now
}
