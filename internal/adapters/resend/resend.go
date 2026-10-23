// Package resend sends email through Resend's API (FR-INT-21), the failover (PRD-001 open question 20): one POST per
// message under the outbox's key as Idempotency-Key. Resend keeps a key 24 hours and answers the same request under
// it with its first answer (PRD-001 rev 0.19, checked 14 Oct 2026), so a send whose answer was lost is reconciled by
// sending it again under its key while Resend still keeps it; after that, Status cannot say.
//
// The request and answers follow Resend's "Send Email" API page (retrieved 23 Oct 2026): POST {URL} with
// Authorization: Bearer, Idempotency-Key (at most 256 characters), from, to, subject and text; the email's id when
// it accepted the message.
package resend

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	netmail "net/mail"
	"strings"
	"time"

	"github.com/cognizhi/fyniq/internal/ports"
)

// DefaultURL is Resend's email endpoint.
const DefaultURL = "https://api.resend.com/emails"

// Window is how long after an attempt a send is reconciled by sending it again under its key: Resend keeps a key
// 24 hours, less an hour's margin.
const Window = 23 * time.Hour

// Sender is the Resend adapter of ports.Email.
type Sender struct {
	URL  string       // the email endpoint
	Key  string       // the API key
	HTTP *http.Client // nil: one with a 30-second limit
}

// New returns a sender to url with the API key key.
func New(url, key string) *Sender {
	return &Sender{URL: url, Key: key, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

// Name implements ports.Email.
func (s *Sender) Name() string { return "resend" }

// Send implements ports.Email.
func (s *Sender) Send(ctx context.Context, m ports.OutboundEmail) (ports.EmailReceipt, error) {
	return s.post(ctx, m)
}

// Status implements ports.Email: within Window of the attempt it sends m again under its key, which Resend answers
// with its first answer if it took m then, or takes it now if it never did; later it cannot say.
func (s *Sender) Status(ctx context.Context, m ports.OutboundEmail, attempted time.Time) (ports.EmailReceipt, error) {
	if m.Date.Sub(attempted) >= Window {
		return ports.EmailReceipt{}, ports.ErrEmailUnknown
	}
	return s.post(ctx, m)
}

type tag struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type request struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	Text    string   `json:"text"`
	Tags    []tag    `json:"tags,omitempty"`
}

// RefTag is the tag carrying a message's reference, which Resend echoes in its webhooks.
const RefTag = "fyniq_ref"

func (s *Sender) post(ctx context.Context, m ports.OutboundEmail) (ports.EmailReceipt, error) {
	from, err := netmail.ParseAddress(m.From)
	if err != nil {
		return ports.EmailReceipt{}, fmt.Errorf("%w: the sender %q is not an address", ports.ErrEmailRefused, m.From)
	}
	if len(m.To) == 0 || len(m.To) > 50 || strings.ContainsAny(m.Subject, "\r\n") || m.Key == "" || len(m.Key) > 256 {
		return ports.EmailReceipt{}, fmt.Errorf("%w: no recipient or more than 50, a subject with a line break, or no key", ports.ErrEmailRefused)
	}
	body := request{From: written(from), Subject: m.Subject, Text: m.Text}
	if m.Reference != "" {
		body.Tags = []tag{{RefTag, m.Reference}}
	}
	for _, t := range m.To {
		a, err := netmail.ParseAddress(t)
		if err != nil {
			return ports.EmailReceipt{}, fmt.Errorf("%w: the recipient %q is not an address", ports.ErrEmailRefused, t)
		}
		body.To = append(body.To, written(a))
	}
	b, err := json.Marshal(body)
	if err != nil {
		return ports.EmailReceipt{}, fmt.Errorf("%w: %v", ports.ErrEmailRefused, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.URL, bytes.NewReader(b))
	if err != nil {
		return ports.EmailReceipt{}, fmt.Errorf("%w: %v", ports.ErrEmailRefused, err)
	}
	req.Header.Set("Authorization", "Bearer "+s.Key)
	req.Header.Set("Idempotency-Key", m.Key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	client := s.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	res, err := client.Do(req)
	if err != nil {
		if neverSent(err) {
			return ports.EmailReceipt{}, fmt.Errorf("%w: %v", ports.ErrEmailRefused, err)
		}
		return ports.EmailReceipt{}, fmt.Errorf("resend: no answer, so the message may have been taken: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	switch {
	case res.StatusCode >= 200 && res.StatusCode < 300:
		var out struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &out); err != nil || out.ID == "" {
			return ports.EmailReceipt{ProviderID: "resend:" + m.Key}, nil
		}
		return ports.EmailReceipt{ProviderID: out.ID}, nil
	case res.StatusCode == http.StatusConflict:
		// The key is in use by a request still running, or was used with another body: it may have been taken.
		return ports.EmailReceipt{}, fmt.Errorf("resend answered 409 %s, so the message may have been taken", reason(raw))
	case res.StatusCode >= 400 && res.StatusCode < 500:
		return ports.EmailReceipt{}, fmt.Errorf("%w: resend answered %d %s", ports.ErrEmailRefused, res.StatusCode, reason(raw))
	}
	return ports.EmailReceipt{}, fmt.Errorf("resend answered %d %s, so the message may have been taken", res.StatusCode, reason(raw))
}

// written is an address as Resend's examples write it: bare, or with its name as "Name <address>" (quoted when
// the name needs it).
func written(a *netmail.Address) string {
	if a.Name == "" {
		return a.Address
	}
	return a.String()
}

// reason is an answer's error name and message, kept short.
func reason(raw []byte) string {
	var out struct {
		Name    string `json:"name"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(raw, &out)
	s := strings.TrimSpace(out.Name + " " + out.Message)
	if r := []rune(s); len(r) > 200 {
		s = string(r[:200])
	}
	return s
}

// neverSent reports whether err happened before the request could reach the provider: no connection was made.
func neverSent(err error) bool {
	var op *net.OpError
	if errors.As(err, &op) && op.Op == "dial" {
		return true
	}
	var dns *net.DNSError
	return errors.As(err, &dns)
}
