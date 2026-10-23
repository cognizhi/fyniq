// Package zeptomail sends email through Zoho ZeptoMail's API (FR-INT-21; ZeptoMail is now Zoho CPaaS, PRD-001
// §8.24): one POST per message, its send token as Zoho-enczapikey. ZeptoMail takes no idempotency key: the outbox's
// key goes in client_reference, which matches a message to its webhooks and prevents nothing. So a send whose answer
// was lost cannot be asked about and is never sent again on a guess: Status always says it cannot say.
//
// The request and answers follow Zoho's "Send email" API page (retrieved 23 Oct 2026): POST {URL} with from, to,
// subject, textbody and client_reference; 200 with request_id when it accepted the message; a 4xx with an error code
// when it did not.
package zeptomail

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

// DefaultURL is the email endpoint Zoho's API page names. An account in another data centre sets its own.
const DefaultURL = "https://cpaas.zoho.com/v1.1/email"

// Sender is the ZeptoMail adapter of ports.Email.
type Sender struct {
	URL  string       // the account's email endpoint
	Key  string       // the Mail Agent's send token
	HTTP *http.Client // nil: one with a 30-second limit
}

// New returns a sender to url with the send token key.
func New(url, key string) *Sender {
	return &Sender{URL: url, Key: key, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

// Name implements ports.Email.
func (s *Sender) Name() string { return "zeptomail" }

// Status implements ports.Email: ZeptoMail cannot be asked about a message by its reference.
func (s *Sender) Status(context.Context, ports.OutboundEmail, time.Time) (ports.EmailReceipt, error) {
	return ports.EmailReceipt{}, ports.ErrEmailUnknown
}

type address struct {
	Address string `json:"address"`
	Name    string `json:"name,omitempty"`
}

type recipient struct {
	EmailAddress address `json:"email_address"`
}

type request struct {
	From            address     `json:"from"`
	To              []recipient `json:"to"`
	Subject         string      `json:"subject"`
	TextBody        string      `json:"textbody"`
	ClientReference string      `json:"client_reference"`
}

// Send implements ports.Email.
func (s *Sender) Send(ctx context.Context, m ports.OutboundEmail) (ports.EmailReceipt, error) {
	from, err := netmail.ParseAddress(m.From)
	if err != nil {
		return ports.EmailReceipt{}, fmt.Errorf("%w: the sender %q is not an address", ports.ErrEmailRefused, m.From)
	}
	if len(m.To) == 0 || strings.ContainsAny(m.Subject, "\r\n") || m.Key == "" {
		return ports.EmailReceipt{}, fmt.Errorf("%w: no recipient, a subject with a line break, or no key", ports.ErrEmailRefused)
	}
	body := request{From: address{Address: from.Address, Name: from.Name}, Subject: m.Subject, TextBody: m.Text, ClientReference: m.Key}
	for _, t := range m.To {
		a, err := netmail.ParseAddress(t)
		if err != nil {
			return ports.EmailReceipt{}, fmt.Errorf("%w: the recipient %q is not an address", ports.ErrEmailRefused, t)
		}
		body.To = append(body.To, recipient{address{Address: a.Address, Name: a.Name}})
	}
	b, err := json.Marshal(body)
	if err != nil {
		return ports.EmailReceipt{}, fmt.Errorf("%w: %v", ports.ErrEmailRefused, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.URL, bytes.NewReader(b))
	if err != nil {
		return ports.EmailReceipt{}, fmt.Errorf("%w: %v", ports.ErrEmailRefused, err)
	}
	req.Header.Set("Authorization", "Zoho-enczapikey "+s.Key)
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
		return ports.EmailReceipt{}, fmt.Errorf("zeptomail: no answer, so the message may have been taken: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	switch {
	case res.StatusCode >= 200 && res.StatusCode < 300:
		var out struct {
			RequestID string `json:"request_id"`
		}
		if err := json.Unmarshal(raw, &out); err != nil || out.RequestID == "" {
			// It said yes; only the reference is missing.
			return ports.EmailReceipt{ProviderID: "zeptomail:" + m.Key}, nil
		}
		return ports.EmailReceipt{ProviderID: out.RequestID}, nil
	case res.StatusCode >= 400 && res.StatusCode < 500:
		return ports.EmailReceipt{}, fmt.Errorf("%w: zeptomail answered %d %s", ports.ErrEmailRefused, res.StatusCode, reason(raw))
	}
	return ports.EmailReceipt{}, fmt.Errorf("zeptomail answered %d %s, so the message may have been taken", res.StatusCode, reason(raw))
}

// reason is the error code and message of an answer, in either shape Zoho documents, kept short.
func reason(raw []byte) string {
	var out struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Data struct {
			Code    string `json:"error_code"`
			Message string `json:"message"`
		} `json:"data"`
	}
	_ = json.Unmarshal(raw, &out)
	code, msg := out.Error.Code, out.Error.Message
	if code == "" {
		code, msg = out.Data.Code, out.Data.Message
	}
	s := strings.TrimSpace(code + " " + msg)
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
