package resend

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

// DefaultBase is Resend's API.
const DefaultBase = "https://api.resend.com"

// Receiving is the Resend adapter of ports.InboundMail (FR-INT-22), from Resend's "Retrieve Received Email" and
// "Retrieve Received Email Attachment" API pages (retrieved 23 Oct 2026): GET /emails/receiving/{id} gives the message
// with authentication (spf, dkim, dmarc) and its attachments' descriptions; GET /emails/receiving/{id}/attachments/
// {attachment} gives a download_url, signed for an hour, to fetch the file from.
type Receiving struct {
	Base string       // the API, DefaultBase
	Key  string       // the API key
	HTTP *http.Client // nil: one with a 60-second limit
}

// NewReceiving returns the receiving adapter of base with the API key key.
func NewReceiving(base, key string) *Receiving {
	return &Receiving{Base: base, Key: key, HTTP: &http.Client{Timeout: 60 * time.Second}}
}

// Name implements ports.InboundMail.
func (r *Receiving) Name() string { return "resend" }

func (r *Receiving) client() *http.Client {
	if r.HTTP == nil {
		return &http.Client{Timeout: 60 * time.Second}
	}
	return r.HTTP
}

// get reads the API's JSON answer at path into out.
func (r *Receiving) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.Base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+r.Key)
	req.Header.Set("Accept", "application/json")
	res, err := r.client().Do(req)
	if err != nil {
		return fmt.Errorf("resend receiving: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("resend receiving answered %d %s", res.StatusCode, reason(raw))
	}
	return json.Unmarshal(raw, out)
}

// Fetch implements ports.InboundMail.
func (r *Receiving) Fetch(ctx context.Context, id string) (ports.InboundEmail, error) {
	var in struct {
		From           string `json:"from"`
		Subject        string `json:"subject"`
		Text           string `json:"text"`
		HTML           string `json:"html"`
		Authentication *struct {
			SPF   string `json:"spf"`
			DKIM  string `json:"dkim"`
			DMARC string `json:"dmarc"`
		} `json:"authentication"`
		Attachments []struct {
			ID          string `json:"id"`
			Filename    string `json:"filename"`
			ContentType string `json:"content_type"`
			Size        int64  `json:"size"`
		} `json:"attachments"`
	}
	if err := r.get(ctx, "/emails/receiving/"+url.PathEscape(id), &in); err != nil {
		return ports.InboundEmail{}, err
	}
	out := ports.InboundEmail{From: in.From, Subject: in.Subject, Text: in.Text, HTML: in.HTML}
	if a := in.Authentication; a != nil {
		out.SPF, out.DKIM, out.DMARC = a.SPF, a.DKIM, a.DMARC
	}
	for _, a := range in.Attachments {
		out.Attachments = append(out.Attachments, ports.InboundAttachment{ID: a.ID, FileName: a.Filename, ContentType: a.ContentType, Size: a.Size})
	}
	return out, nil
}

// Attachment implements ports.InboundMail.
func (r *Receiving) Attachment(ctx context.Context, id string, a ports.InboundAttachment, limit int64) ([]byte, error) {
	if a.Size > limit {
		return nil, ports.ErrTooLarge
	}
	var meta struct {
		DownloadURL string `json:"download_url"`
	}
	if err := r.get(ctx, "/emails/receiving/"+url.PathEscape(id)+"/attachments/"+url.PathEscape(a.ID), &meta); err != nil {
		return nil, err
	}
	u, err := url.Parse(meta.DownloadURL)
	if err != nil || u.Scheme != "https" {
		return nil, fmt.Errorf("resend receiving: the download URL of attachment %s is not https", a.ID)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	res, err := r.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("resend receiving: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("resend receiving: downloading attachment %s answered %d", a.ID, res.StatusCode)
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
