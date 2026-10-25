// Package smtp is the Email port over SMTP (FR-INT-21): Mailpit in local mode (`make deps-up`, port 1025),
// or any relay. A relay off this machine must offer STARTTLS; only a loopback or local-stack server may be
// spoken to in the clear. SMTP cannot say what became of a message after the connection closes, so Status is
// always unknown and the outbox never sends a message twice on a guess.
package smtp

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strings"
	"time"

	"github.com/cognizhi/fyniq/internal/ports"
)

// Sender sends through one SMTP server.
type Sender struct {
	addr      string // host:port
	plaintext bool   // the server may be spoken to without TLS
	domain    string // the Message-ID's domain
	// Dial connects to the server; nil dials directly. The connector runtime gives the egress guard's (SEC-36), so a
	// configured host never reaches a private address or the metadata service.
	Dial func(ctx context.Context, network, addr string) (net.Conn, error)
}

// New returns a Sender for addr. plaintext allows a server without STARTTLS, for Mailpit in the local stack;
// otherwise a server that does not offer STARTTLS is refused.
func New(addr, domain string, plaintext bool) *Sender {
	return &Sender{addr: addr, plaintext: plaintext, domain: domain}
}

// Name implements ports.Email.
func (s *Sender) Name() string { return "smtp" }

// Status implements ports.Email: SMTP has no way to ask.
func (s *Sender) Status(context.Context, ports.OutboundEmail, time.Time) (ports.EmailReceipt, error) {
	return ports.EmailReceipt{}, ports.ErrEmailUnknown
}

// clean refuses a header value that could start another header (header injection).
func clean(field, v string) error {
	if strings.ContainsAny(v, "\r\n") {
		return fmt.Errorf("%w: the %s holds a line break", ports.ErrEmailRefused, field)
	}
	return nil
}

// address parses one RFC 5322 address and returns its bare form.
func address(field, v string) (string, error) {
	if err := clean(field, v); err != nil {
		return "", err
	}
	a, err := mail.ParseAddress(v)
	if err != nil {
		return "", fmt.Errorf("%w: the %s %q is not an address", ports.ErrEmailRefused, field, v)
	}
	return a.Address, nil
}

// MessageID is the Message-ID a message with key is sent under.
func (s *Sender) MessageID(key string) string { return "<" + key + "@" + s.domain + ">" }

// Send implements ports.Email.
func (s *Sender) Send(ctx context.Context, m ports.OutboundEmail) (ports.EmailReceipt, error) {
	from, err := address("sender", m.From)
	if err != nil {
		return ports.EmailReceipt{}, err
	}
	if len(m.To) == 0 {
		return ports.EmailReceipt{}, fmt.Errorf("%w: the message has no recipient", ports.ErrEmailRefused)
	}
	to := make([]string, len(m.To))
	for i, t := range m.To {
		if to[i], err = address("recipient", t); err != nil {
			return ports.EmailReceipt{}, err
		}
	}
	if err := errors.Join(clean("subject", m.Subject), clean("key", m.Key)); err != nil || m.Key == "" {
		return ports.EmailReceipt{}, fmt.Errorf("%w: the subject or key is not valid", ports.ErrEmailRefused)
	}
	if err := ports.CheckAttachments(m.Attachments); err != nil {
		return ports.EmailReceipt{}, err
	}
	id := s.MessageID(m.Key)
	text := strings.ReplaceAll(strings.ReplaceAll(m.Text, "\r\n", "\n"), "\n", "\r\n")
	var b strings.Builder
	headers := [][2]string{
		{"From", m.From}, {"To", strings.Join(m.To, ", ")}, {"Subject", mime.QEncoding.Encode("utf-8", m.Subject)},
		{"Date", m.Date.UTC().Format(time.RFC1123Z)}, {"Message-ID", id}, {"MIME-Version", "1.0"},
	}
	if len(m.Attachments) == 0 {
		headers = append(headers, [2]string{"Content-Type", "text/plain; charset=utf-8"}, [2]string{"Content-Transfer-Encoding", "8bit"})
	} else {
		headers = append(headers, [2]string{"Content-Type", `multipart/mixed; boundary="` + boundary(m.Key) + `"`})
	}
	for _, h := range headers {
		b.WriteString(h[0] + ": " + h[1] + "\r\n")
	}
	b.WriteString("\r\n")
	if len(m.Attachments) == 0 {
		b.WriteString(text)
	} else {
		mixed(&b, boundary(m.Key), text, m.Attachments)
	}

	if err := s.deliver(ctx, from, to, b.String()); err != nil {
		return ports.EmailReceipt{}, err
	}
	return ports.EmailReceipt{ProviderID: id}, nil
}

// boundary is a multipart boundary for the message with key: "=_" cannot occur in base64 or in the text's lines as
// Fyniq writes them, and the key's hash keeps a message's bytes the same on each attempt.
func boundary(key string) string {
	h := sha256.Sum256([]byte(key))
	return "=_fyniq_" + hex.EncodeToString(h[:12])
}

// mixed writes a multipart/mixed body: the text, then each file in base64 in lines of 76 (RFC 2045).
func mixed(b *strings.Builder, boundary, text string, files []ports.EmailAttachment) {
	b.WriteString("--" + boundary + "\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n" + text + "\r\n")
	for _, f := range files {
		b.WriteString("--" + boundary + "\r\n")
		b.WriteString("Content-Type: " + mime.FormatMediaType(f.ContentType, map[string]string{"name": f.Name}) + "\r\n")
		b.WriteString("Content-Disposition: " + mime.FormatMediaType("attachment", map[string]string{"filename": f.Name}) + "\r\n")
		b.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
		enc := base64.StdEncoding.EncodeToString(f.Content)
		for len(enc) > 76 {
			b.WriteString(enc[:76] + "\r\n")
			enc = enc[76:]
		}
		b.WriteString(enc + "\r\n")
	}
	b.WriteString("--" + boundary + "--\r\n")
}

// deliver runs one SMTP transaction. Errors before DATA is accepted are refusals (nothing was taken); an
// error after it leaves the outcome unknown.
func (s *Sender) deliver(ctx context.Context, from string, to []string, msg string) error {
	dial := s.Dial
	if dial == nil {
		var d net.Dialer
		dial = d.DialContext
	}
	conn, err := dial(ctx, "tcp", s.addr)
	if err != nil {
		return fmt.Errorf("%w: %v", ports.ErrEmailRefused, err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	host, _, _ := net.SplitHostPort(s.addr)
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("%w: %v", ports.ErrEmailRefused, err)
	}
	defer func() { _ = c.Close() }()
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("%w: STARTTLS: %v", ports.ErrEmailRefused, err)
		}
	} else if !s.plaintext {
		return fmt.Errorf("%w: %s offers no STARTTLS", ports.ErrEmailRefused, s.addr)
	}
	if err := c.Mail(from); err != nil {
		return fmt.Errorf("%w: %v", ports.ErrEmailRefused, err)
	}
	for _, t := range to {
		if err := c.Rcpt(t); err != nil {
			return fmt.Errorf("%w: %v", ports.ErrEmailRefused, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("%w: %v", ports.ErrEmailRefused, err)
	}
	if _, err := w.Write([]byte(msg)); err != nil {
		return fmt.Errorf("writing the message: %w", err) // part of it may have been taken: unknown
	}
	if err := w.Close(); err != nil {
		var reply *textproto.Error // the server answered the message with a 4xx or 5xx: it did not take it
		if errors.As(err, &reply) {
			return fmt.Errorf("%w: %v", ports.ErrEmailRefused, err)
		}
		return fmt.Errorf("finishing the message: %w", err) // the connection broke: it may have been taken
	}
	_ = c.Quit()
	return nil
}
