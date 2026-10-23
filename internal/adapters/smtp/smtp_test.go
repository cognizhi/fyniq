package smtp

import (
	"bufio"
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/ports"
)

// server is a minimal SMTP server: it records each message, and answers DATA with reply.
type server struct {
	ln       net.Listener
	reply    string // the answer to the end of DATA, "250 OK" by default
	starttls bool   // advertise STARTTLS
	mu       sync.Mutex
	got      []string // envelope and data, one entry per message
}

func newServer(t *testing.T) *server {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &server{ln: ln, reply: "250 OK"}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(c)
		}
	}()
	return s
}

func (s *server) serve(c net.Conn) {
	defer func() { _ = c.Close() }()
	r := bufio.NewReader(c)
	say := func(l string) { _, _ = c.Write([]byte(l + "\r\n")) }
	say("220 test ESMTP")
	var msg strings.Builder
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(cmd, "EHLO"):
			if s.starttls {
				say("250-test")
				say("250 STARTTLS")
			} else {
				say("250 test")
			}
		case strings.HasPrefix(cmd, "MAIL"), strings.HasPrefix(cmd, "RCPT"):
			msg.WriteString(strings.TrimSpace(line) + "\n")
			say("250 OK")
		case cmd == "DATA":
			say("354 go on")
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				msg.WriteString(l)
			}
			s.mu.Lock()
			s.got = append(s.got, msg.String())
			s.mu.Unlock()
			msg.Reset()
			say(s.reply)
		case cmd == "QUIT":
			say("221 bye")
			return
		default:
			say("250 OK")
		}
	}
}

func (s *server) messages() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.got...)
}

func message() ports.OutboundEmail {
	return ports.OutboundEmail{Key: "message-one", From: "Fyniq <no-reply@fyniq.local>", To: []string{"Accounts <accounts@example.com>"},
		Subject: "Test message: café", Text: "Hello.\nIt works.", Date: time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)}
}

// A message reaches the server once, under the Message-ID its key gives it, with the subject encoded.
// req: FR-INT-21
func TestAMessageIsDeliveredUnderItsKey(t *testing.T) {
	srv := newServer(t)
	s := New(srv.ln.Addr().String(), "fyniq.local", true)
	r, err := s.Send(context.Background(), message())
	if err != nil || r.ProviderID != "<message-one@fyniq.local>" {
		t.Fatalf("Send: %v %+v", err, r)
	}
	got := srv.messages()
	if len(got) != 1 {
		t.Fatalf("%d messages; want 1", len(got))
	}
	for _, want := range []string{"MAIL FROM:<no-reply@fyniq.local>", "RCPT TO:<accounts@example.com>", "Message-ID: <message-one@fyniq.local>",
		"Subject: =?utf-8?q?Test_message:_caf=C3=A9?=", "Date: Tue, 20 Oct 2026 09:00:00 +0000", "Hello.\r\nIt works."} {
		if !strings.Contains(got[0], want) {
			t.Errorf("the message lacks %q:\n%s", want, got[0])
		}
	}
	if _, err := s.Status(context.Background(), ports.OutboundEmail{Key: "message-one"}, time.Time{}); !errors.Is(err, ports.ErrEmailUnknown) {
		t.Errorf("SMTP cannot say what became of a message: %v", err)
	}
}

// A header that could start another header, a bad address, no recipient, a server that refuses the message
// and a relay without STARTTLS are refused before anything is taken, so sending again is safe.
// req: FR-INT-21
// req: SEC-15
// asvs: V1.2.4
func TestWhatCannotBeSentIsRefused(t *testing.T) {
	srv := newServer(t)
	s := New(srv.ln.Addr().String(), "fyniq.local", true)
	injected, bad, none := message(), message(), message()
	injected.Subject = "Hi\r\nBcc: everyone@example.com"
	bad.To = []string{"not an address"}
	none.To = nil
	for name, m := range map[string]ports.OutboundEmail{"a header injection": injected, "a bad address": bad, "no recipient": none} {
		if _, err := s.Send(context.Background(), m); !errors.Is(err, ports.ErrEmailRefused) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if n := len(srv.messages()); n != 0 {
		t.Fatalf("%d refused messages reached the server", n)
	}
	srv.reply = "554 rejected"
	if _, err := s.Send(context.Background(), message()); !errors.Is(err, ports.ErrEmailRefused) {
		t.Errorf("a server that rejects the message: %v", err)
	}
	if _, err := New(srv.ln.Addr().String(), "fyniq.local", false).Send(context.Background(), message()); !errors.Is(err, ports.ErrEmailRefused) ||
		!strings.Contains(err.Error(), "STARTTLS") {
		t.Errorf("a relay without STARTTLS: %v", err)
	}
}
