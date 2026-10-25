package connectors_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/connectors"
	"github.com/cognizhi/fyniq/internal/connector"
	"github.com/cognizhi/fyniq/internal/ports"
)

var message = ports.OutboundEmail{Key: "01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e40", Reference: "org_msg", From: "Fyniq <no-reply@mail.example.com>",
	To: []string{"founder@example.com"}, Subject: "Test", Text: "Hello.\n", Date: time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)}

func catalogue(t *testing.T) *connector.Catalogue {
	t.Helper()
	c, err := connectors.Catalogue()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func get(t *testing.T, id string) connector.Binding {
	t.Helper()
	b, ok := catalogue(t).Get(id)
	if !ok {
		t.Fatalf("%s is not in the catalogue", id)
	}
	return b
}

// The catalogue is exactly the connectors whose directories hold a manifest: one added without its binding, or a
// binding without its directory, fails.
// req: FR-INT-25
func TestTheCatalogueIsTheCompiledSet(t *testing.T) {
	paths, err := filepath.Glob("*/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var dirs, ids []string
	for _, p := range paths {
		dirs = append(dirs, filepath.Dir(p))
	}
	for _, b := range catalogue(t).All() {
		ids = append(ids, b.Manifest.ID)
	}
	slices.Sort(dirs)
	slices.Sort(ids)
	if !slices.Equal(dirs, ids) {
		t.Fatalf("directories %v, catalogue %v", dirs, ids)
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() && !slices.Contains(dirs, e.Name()) {
			t.Errorf("connectors/%s has no manifest.json", e.Name())
		}
	}
}

// Each email connector's binding builds an adapter that sends through its provider's fake, under the secret and
// endpoint it was configured with, and names a secret that is not set.
// req: FR-INT-20
// req: FR-INT-21
// req: FR-INT-25
func TestTheEmailConnectorsSendThroughTheirFakes(t *testing.T) {
	cases := []struct {
		id, secret, header, answer string
	}{
		{"zeptomail", "send_token", "Zoho-enczapikey token-1", `{"data":[{"code":"EM_104"}],"message":"OK","request_id":"req-42"}`},
		{"resend", "api_key", "Bearer token-1", `{"id":"req-42"}`},
	}
	for _, c := range cases {
		var calls int
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			if r.Method != http.MethodPost || r.Header.Get("Authorization") != c.header {
				t.Errorf("%s: %s with %v", c.id, r.Method, r.Header)
			}
			_, _ = io.Copy(io.Discard, r.Body)
			_, _ = w.Write([]byte(c.answer))
		}))
		b := get(t, c.id)
		email, err := b.OutboundEmail(connector.Config{Values: map[string]any{"url": srv.URL}, Secrets: map[string]string{c.secret: "token-1"}})
		if err != nil {
			t.Fatalf("%s: %v", c.id, err)
		}
		if email.Name() != c.id {
			t.Errorf("%s names itself %s", c.id, email.Name())
		}
		r, err := email.Send(context.Background(), message)
		if err != nil || r.ProviderID != "req-42" || calls != 1 {
			t.Errorf("%s: %+v, %v after %d calls", c.id, r, err, calls)
		}
		srv.Close()
		if _, err := b.OutboundEmail(connector.Config{}); err == nil || !strings.Contains(err.Error(), c.secret) {
			t.Errorf("%s without its secret: %v", c.id, err)
		}
	}
}

// Resend's inbound capability fetches a received message from its fake, under the same key.
// req: FR-INT-22
// req: FR-INT-25
func TestResendReceivesThroughItsFake(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/emails/receiving/rcv-1" || r.Header.Get("Authorization") != "Bearer token-1" {
			t.Errorf("%s %v", r.URL.Path, r.Header)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"from": "supplier@example.com", "subject": "Bill", "text": "Attached."})
	}))
	defer srv.Close()
	in, err := get(t, "resend").InboundEmail(connector.Config{Values: map[string]any{"api_base": srv.URL}, Secrets: map[string]string{"api_key": "token-1"}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := in.Fetch(context.Background(), "rcv-1")
	if err != nil || got.From != "supplier@example.com" || got.Subject != "Bill" {
		t.Fatalf("%+v, %v", got, err)
	}
}

// smtpFake is an SMTP server without TLS that records each message's envelope.
type smtpFake struct {
	mu  sync.Mutex
	got []string
}

func (f *smtpFake) serve(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go f.session(c)
		}
	}()
	return ln.Addr().String()
}

func (f *smtpFake) session(c net.Conn) {
	defer func() { _ = c.Close() }()
	r := bufio.NewReader(c)
	say := func(l string) { _, _ = c.Write([]byte(l + "\r\n")) }
	say("220 fake ESMTP")
	var data bool
	var envelope strings.Builder
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		if data {
			if line == ".\r\n" {
				data = false
				f.mu.Lock()
				f.got = append(f.got, envelope.String())
				f.mu.Unlock()
				say("250 OK")
			}
			continue
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			say("250 fake")
		case strings.HasPrefix(cmd, "MAIL"), strings.HasPrefix(cmd, "RCPT"):
			envelope.WriteString(strings.TrimSpace(line) + "\n")
			say("250 OK")
		case cmd == "DATA":
			data = true
			say("354 go on")
		case cmd == "QUIT":
			say("221 bye")
			return
		default:
			say("250 OK")
		}
	}
}

// The SMTP connector sends to the server it is configured with. Only the local flavour speaks to a server without
// STARTTLS, as it does to Mailpit; the release flavour refuses one, so nothing travels in clear text (DEV-07).
// req: FR-INT-21
// req: FR-INT-25
// req: DEV-07
func TestTheSMTPConnectorSpeaksPlainTextOnlyInTheLocalFlavour(t *testing.T) {
	f := &smtpFake{}
	addr := f.serve(t)
	b := get(t, "smtp")
	email, err := b.OutboundEmail(connector.Config{Values: map[string]any{"address": addr}, SenderDomain: "mail.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = email.Send(context.Background(), message)
	f.mu.Lock()
	defer f.mu.Unlock()
	if connector.LocalFlavour {
		if err != nil || len(f.got) != 1 || !strings.Contains(f.got[0], "founder@example.com") {
			t.Fatalf("local flavour: %v, %v", err, f.got)
		}
	} else if !errors.Is(err, ports.ErrEmailRefused) || !strings.Contains(err.Error(), "STARTTLS") || len(f.got) != 0 {
		t.Fatalf("release flavour: %v, %v", err, f.got)
	}
	if _, err := b.OutboundEmail(connector.Config{}); err == nil || !strings.Contains(err.Error(), "SMTP server") {
		t.Fatalf("without a server: %v", err)
	}
}
