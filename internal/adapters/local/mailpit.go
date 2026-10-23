//go:build local

package local

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/cognizhi/fyniq/internal/ports"
)

// MailpitInbound is local mode's substitute for receiving mail (ADR-T4, PRD-001 §8.24 "Mailpit plus a local webhook"):
// mail sent to Mailpit's SMTP for the capture domain is announced by Mailpit's webhook and read through its API
// (GET /api/v1/message/{ID}, and each attachment at /api/v1/message/{ID}/part/{PartID}; checked against Mailpit
// 1.31). Mailpit checks no SPF, DKIM or DMARC, so it reports them as passing: local mode only, never a release build.
type MailpitInbound struct {
	URL  string       // Mailpit's web address, such as http://localhost:8025
	HTTP *http.Client // nil: one with a 30-second limit
}

// Name implements ports.InboundMail.
func (m MailpitInbound) Name() string { return "mailpit" }

func (m MailpitInbound) client() *http.Client {
	if m.HTTP == nil {
		return &http.Client{Timeout: 30 * time.Second}
	}
	return m.HTTP
}

func (m MailpitInbound) get(ctx context.Context, path string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.URL+path, nil)
	if err != nil {
		return nil, err
	}
	res, err := m.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("mailpit: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mailpit answered %d for %s", res.StatusCode, path)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, ports.ErrTooLarge
	}
	return b, nil
}

// Fetch implements ports.InboundMail.
func (m MailpitInbound) Fetch(ctx context.Context, id string) (ports.InboundEmail, error) {
	b, err := m.get(ctx, "/api/v1/message/"+url.PathEscape(id), 32<<20)
	if err != nil {
		return ports.InboundEmail{}, err
	}
	var in struct {
		From struct {
			Address string `json:"Address"`
		} `json:"From"`
		Subject     string `json:"Subject"`
		Text        string `json:"Text"`
		HTML        string `json:"HTML"`
		Attachments []struct {
			PartID      string `json:"PartID"`
			FileName    string `json:"FileName"`
			ContentType string `json:"ContentType"`
			Size        int64  `json:"Size"`
		} `json:"Attachments"`
	}
	if err := json.Unmarshal(b, &in); err != nil {
		return ports.InboundEmail{}, err
	}
	out := ports.InboundEmail{From: in.From.Address, Subject: in.Subject, Text: in.Text, HTML: in.HTML, SPF: "pass", DKIM: "pass", DMARC: "pass"}
	for _, a := range in.Attachments {
		out.Attachments = append(out.Attachments, ports.InboundAttachment{ID: a.PartID, FileName: a.FileName, ContentType: a.ContentType, Size: a.Size})
	}
	return out, nil
}

// Attachment implements ports.InboundMail.
func (m MailpitInbound) Attachment(ctx context.Context, id string, a ports.InboundAttachment, limit int64) ([]byte, error) {
	if a.Size > limit {
		return nil, ports.ErrTooLarge
	}
	return m.get(ctx, "/api/v1/message/"+url.PathEscape(id)+"/part/"+url.PathEscape(a.ID), limit)
}
