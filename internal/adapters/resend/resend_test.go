package resend_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/adapters/resend"
	"github.com/cognizhi/fyniq/internal/ports"
)

// messageID is the message's ID, which is also its key.
const messageID = "01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e40"

var message = ports.OutboundEmail{Key: messageID, From: "Example Pte Ltd <accounts@mail.example.com>",
	To: []string{"founder@example.com"}, Subject: "Invoice INV-0042", Text: "Hello.\n", Date: time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)}

// The send is the request Resend's API page documents, under the API key and the outbox's key as Idempotency-Key;
// the email's id is the provider's reference.
// req: FR-INT-21
func TestASendIsTheDocumentedRequest(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer re_key" || r.Header.Get("Idempotency-Key") != message.Key {
			t.Errorf("%s with %v", r.Method, r.Header)
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{"id":"49a3999c-0ce1-4ea6-ab68-afcd6dc2e794"}`))
	}))
	defer srv.Close()
	r, err := resend.New(srv.URL, "re_key").Send(context.Background(), message)
	if err != nil || r.ProviderID != "49a3999c-0ce1-4ea6-ab68-afcd6dc2e794" {
		t.Fatalf("%+v, %v", r, err)
	}
	if got["from"] != `"Example Pte Ltd" <accounts@mail.example.com>` || got["subject"] != "Invoice INV-0042" || got["text"] != "Hello.\n" ||
		len(got["to"].([]any)) != 1 || got["to"].([]any)[0] != "founder@example.com" || len(got) != 4 {
		t.Fatalf("the request: %v", got)
	}
}

// A 4xx is a refusal, but a 409 on the key is not: a request under it may be running or have been taken; a 5xx may
// have been taken too.
// req: FR-INT-21
func TestResendsAnswersAreRefusalsOnlyWhenNothingWasTaken(t *testing.T) {
	for name, c := range map[string]struct {
		status  int
		body    string
		refused bool
		says    string
	}{
		"invalid":     {422, `{"statusCode":422,"name":"validation_error","message":"Invalid from field"}`, true, "validation_error Invalid from field"},
		"a bad key":   {401, `{"name":"missing_api_key","message":"Missing API key"}`, true, "missing_api_key"},
		"too many":    {429, `{"name":"rate_limit_exceeded","message":"Too many requests"}`, true, "rate_limit_exceeded"},
		"a key clash": {409, `{"name":"concurrent_idempotent_requests","message":"in progress"}`, false, "may have been taken"},
		"unavailable": {503, ``, false, "may have been taken"},
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(c.body))
			}))
			defer srv.Close()
			_, err := resend.New(srv.URL, "k").Send(context.Background(), message)
			if err == nil || errors.Is(err, ports.ErrEmailRefused) != c.refused || !strings.Contains(err.Error(), c.says) {
				t.Fatalf("%v; want refused=%v, saying %q", err, c.refused, c.says)
			}
		})
	}
}

// A send whose answer was lost is reconciled by sending it again under its key while Resend keeps the key, which
// answers with its first answer; after the window, Status cannot say and sends nothing.
// req: FR-INT-21
func TestStatusSendsAgainUnderTheKeyWithinTheWindow(t *testing.T) {
	var mu sync.Mutex
	var keys []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		keys = append(keys, r.Header.Get("Idempotency-Key"))
		mu.Unlock()
		_, _ = w.Write([]byte(`{"id":"first-answer"}`))
	}))
	defer srv.Close()
	s := resend.New(srv.URL, "k")
	r, err := s.Status(context.Background(), message, message.Date.Add(-22*time.Hour))
	if err != nil || r.ProviderID != "first-answer" || len(keys) != 1 || keys[0] != message.Key {
		t.Fatalf("within the window: %+v, %v, keys %v", r, err, keys)
	}
	if _, err := s.Status(context.Background(), message, message.Date.Add(-resend.Window)); !errors.Is(err, ports.ErrEmailUnknown) || len(keys) != 1 {
		t.Fatalf("after the window: %v, %d requests; want unknown and none", err, len(keys))
	}
}

// A message's reference goes as the fyniq_ref tag, which Resend echoes in its webhooks.
// req: FR-INT-21
func TestTheReferenceGoesAsATag(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{"id":"e-1"}`))
	}))
	defer srv.Close()
	m := message
	m.Reference = "org_message"
	if _, err := resend.New(srv.URL, "k").Send(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	tags, _ := got["tags"].([]any)
	if len(tags) != 1 || tags[0].(map[string]any)["name"] != resend.RefTag || tags[0].(map[string]any)["value"] != "org_message" {
		t.Fatalf("the tags: %v", got["tags"])
	}
}
