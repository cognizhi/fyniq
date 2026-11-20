package slack

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/ports"
)

func poster(h http.HandlerFunc) (*Poster, func()) {
	srv := httptest.NewServer(h)
	return &Poster{Base: srv.URL, Token: "xoxb-test", HTTP: &http.Client{Transport: http.DefaultTransport}}, srv.Close
}

// The client sends the one documented call, with the bot token and a JSON body, and reads Slack's answers as the contract records them: ok, a refusal that will not change, and the
// wait Slack asks for (contracts/partners/slack.yaml; docs.slack.dev, retrieved 21 Nov 2026). [live-verify]
// req: FR-NTF-04
func TestSlackPostsOneMessageAndReadsItsAnswers(t *testing.T) {
	var got struct {
		method, path, auth, ctype, body string
	}
	answer := `{"ok":true,"channel":"C1","ts":"1503435956.000247"}`
	status := http.StatusOK
	retry := ""
	p, stop := poster(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got.method, got.path, got.auth, got.ctype, got.body = r.Method, r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Content-Type"), string(b)
		if retry != "" {
			w.Header().Set("Retry-After", retry)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(answer))
	})
	defer stop()
	m := ports.ChatMessage{Channel: "C1", Text: "2 approvals wait for you: https://app.fyniq.example/inbox"}
	r, err := p.Post(context.Background(), m)
	if err != nil || r.ID != "1503435956.000247" {
		t.Fatalf("a post: %+v %v", r, err)
	}
	if got.method != "POST" || got.path != "/api/chat.postMessage" || got.auth != "Bearer xoxb-test" || !strings.HasPrefix(got.ctype, "application/json") ||
		got.body != `{"channel":"C1","text":"2 approvals wait for you: https://app.fyniq.example/inbox"}` {
		t.Errorf("the request: %+v", got)
	}
	for _, e := range []string{"channel_not_found", "not_in_channel", "invalid_auth", "is_archived"} {
		answer = `{"ok":false,"error":"` + e + `"}`
		if _, err := p.Post(context.Background(), m); !errors.Is(err, ports.ErrChatRefused) || !strings.Contains(err.Error(), e) {
			t.Errorf("%s: %v", e, err)
		}
	}
	answer, retry = `{"ok":false,"error":"ratelimited"}`, "7"
	var lim ports.ChatLimited
	if _, err := p.Post(context.Background(), m); !errors.As(err, &lim) || lim.After != 7*time.Second {
		t.Errorf("rate limited: %v", err)
	}
	status = http.StatusTooManyRequests
	if _, err := p.Post(context.Background(), m); !errors.As(err, &lim) {
		t.Errorf("a 429: %v", err)
	}
	status, answer = http.StatusServiceUnavailable, "down"
	if _, err := p.Post(context.Background(), m); err == nil || errors.Is(err, ports.ErrChatRefused) {
		t.Errorf("an outage is retried, not refused: %v", err)
	}
	if _, err := p.Post(context.Background(), ports.ChatMessage{Channel: "C1"}); err == nil {
		t.Error("a message with no text was sent")
	}
}

// The client refuses every other method and path before it leaves, so it cannot read a channel or change a workspace.
// req: FR-NTF-04
func TestTheSlackClientSendsNothingButAMessage(t *testing.T) {
	hits := 0
	p, stop := poster(func(http.ResponseWriter, *http.Request) { hits++ })
	defer stop()
	for _, c := range []struct{ method, path string }{{"GET", PostPath}, {"POST", "/api/conversations.history"}, {"POST", "/api/chat.delete"}, {"POST", "/api/admin.users.remove"}} {
		req, _ := http.NewRequest(c.method, p.Base+c.path, nil)
		if _, err := p.client().Do(req); err == nil || !errors.Is(err, ErrMethod) {
			t.Errorf("%s %s: %v", c.method, c.path, err)
		}
	}
	if hits != 0 {
		t.Errorf("%d refused requests reached the server", hits)
	}
}
