package zeptomail_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/adapters/zeptomail"
	"github.com/cognizhi/fyniq/internal/ports"
)

// messageID is the message's ID, which is also its key.
const messageID = "01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e40"

var message = ports.OutboundEmail{Key: messageID, From: "Example Pte Ltd <accounts@mail.example.com>",
	To: []string{"founder@example.com"}, Subject: "Invoice INV-0042", Text: "Hello.\n", Date: time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)}

// The send is the request Zoho's API page documents, under the Mail Agent's token, with the outbox's key as
// client_reference; ZeptoMail's request_id is the provider's reference.
// req: FR-INT-21
func TestASendIsTheDocumentedRequest(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Zoho-enczapikey token-1" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("%s with %v", r.Method, r.Header)
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{"data":[{"code":"EM_104","additional_info":[],"message":"Email request received"}],"message":"OK","request_id":"req-42"}`))
	}))
	defer srv.Close()
	r, err := zeptomail.New(srv.URL, "token-1").Send(context.Background(), message)
	if err != nil || r.ProviderID != "req-42" {
		t.Fatalf("%+v, %v; want req-42", r, err)
	}
	want := `{"client_reference":"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e40","from":{"address":"accounts@mail.example.com","name":"Example Pte Ltd"},` +
		`"subject":"Invoice INV-0042","textbody":"Hello.\n","to":[{"email_address":{"address":"founder@example.com"}}]}`
	if b, _ := json.Marshal(got); string(b) != want {
		t.Fatalf("the request:\n%s\nwant\n%s", b, want)
	}
}

// A 4xx is a refusal, so the failover may send it; a 5xx or an answer that never came may have been taken, so it is
// not a refusal; a connection never made is. ZeptoMail cannot be asked about a message, so Status cannot say.
// req: FR-INT-21
func TestZeptoMailsAnswersAreRefusalsOnlyWhenNothingWasTaken(t *testing.T) {
	for name, c := range map[string]struct {
		status  int
		body    string
		refused bool
		says    string
	}{
		"a bad request":  {400, `{"error":{"code":"TM_3201","message":"Mandatory field missing","request_id":"r"}}`, true, "TM_3201 Mandatory field missing"},
		"a bad token":    {401, `{"data":{"error_code":"TM_4001","message":"Invalid API Token found"},"message":"error"}`, true, "TM_4001"},
		"too many":       {429, `{}`, true, "429"},
		"a server fault": {500, `{}`, false, "may have been taken"},
		"a bad gateway":  {502, ``, false, "may have been taken"},
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(c.body))
			}))
			defer srv.Close()
			_, err := zeptomail.New(srv.URL, "t").Send(context.Background(), message)
			if err == nil || errors.Is(err, ports.ErrEmailRefused) != c.refused || !strings.Contains(err.Error(), c.says) {
				t.Fatalf("%v; want refused=%v, saying %q", err, c.refused, c.says)
			}
		})
	}
	t.Run("no connection", func(t *testing.T) {
		l, _ := net.Listen("tcp", "127.0.0.1:0")
		addr := l.Addr().String()
		_ = l.Close()
		if _, err := zeptomail.New("http://"+addr+"/v1.1/email", "t").Send(context.Background(), message); !errors.Is(err, ports.ErrEmailRefused) {
			t.Fatalf("%v; want a refusal: nothing was sent", err)
		}
	})
	t.Run("a sender that is not an address", func(t *testing.T) {
		m := message
		m.From = "nobody"
		if _, err := zeptomail.New("http://127.0.0.1:1", "t").Send(context.Background(), m); !errors.Is(err, ports.ErrEmailRefused) {
			t.Fatalf("%v; want a refusal", err)
		}
	})
	if _, err := zeptomail.New("http://127.0.0.1:1", "t").Status(context.Background(), message, message.Date); !errors.Is(err, ports.ErrEmailUnknown) {
		t.Fatalf("Status: %v; want unknown", err)
	}
}

// A message's reference goes as client_reference, which ZeptoMail echoes in its webhooks.
// req: FR-INT-21
func TestTheReferenceGoesAsClientReference(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{"request_id":"r-1"}`))
	}))
	defer srv.Close()
	m := message
	m.Reference = "org_message"
	if _, err := zeptomail.New(srv.URL, "t").Send(context.Background(), m); err != nil || got["client_reference"] != "org_message" {
		t.Fatalf("client_reference %v, %v", got["client_reference"], err)
	}
}
