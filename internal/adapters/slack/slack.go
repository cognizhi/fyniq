// Package slack posts a message to a Slack channel with a bot token (PRD-001 FR-NTF-04; p2-api-third-parties design D9): `POST https://slack.com/api/chat.postMessage` with a JSON
// body and an `Authorization: Bearer` header (docs.slack.dev/reference/methods/chat.postMessage, retrieved 21 Nov 2026: "channel" is the channel to send to, "text" the message, the
// bot scope is `chat:write`, the answer is `ok` true with `channel` and `ts`, or `ok` false with an `error`, and "ratelimited" says to read the `Retry-After` header). The client sends
// that one call and refuses any other method or path before it leaves, so a bug cannot read a channel or change a workspace. Incoming webhooks are not used: they are fixed to one
// channel. [live-verify: a real workspace's answers; the bot's membership of each channel; Slack's data residency before the standard content level is offered.]
package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/cognizhi/fyniq/internal/egress"
	"github.com/cognizhi/fyniq/internal/ports"
)

// DefaultBase is Slack's API host.
const DefaultBase = "https://slack.com"

// PostPath is the one path the client calls.
const PostPath = "/api/chat.postMessage"

// ErrMethod is a request the client refused to send.
var ErrMethod = errors.New("slack: the client only posts a message")

// Poster posts messages.
type Poster struct {
	Base  string // DefaultBase, or a mock's
	Token string // the bot token (xoxb-…)
	HTTP  *http.Client
}

var _ ports.Messaging = (*Poster)(nil)

type postOnly struct{ next http.RoundTripper }

func (p postOnly) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method != http.MethodPost || r.URL.Path != PostPath {
		return nil, fmt.Errorf("%w: refused %s %s", ErrMethod, r.Method, r.URL.Path)
	}
	return p.next.RoundTrip(r)
}

func (p *Poster) client() *http.Client {
	base := p.HTTP
	if base == nil || base.Transport == nil {
		base = egress.Public()
	}
	wrapped := *base
	wrapped.Transport = postOnly{next: base.Transport}
	return &wrapped
}

type answer struct {
	OK    bool   `json:"ok"`
	TS    string `json:"ts"`
	Error string `json:"error"`
}

// Post implements ports.Messaging. A "ratelimited" answer, or a 429, is a ChatLimited with the seconds Slack asked for; a refusal that will not change (a channel that is not
// there, a token revoked or lacking chat:write) is ErrChatRefused; anything else is an error to retry. The body of an answer is never kept beyond its error name.
func (p *Poster) Post(ctx context.Context, m ports.ChatMessage) (ports.ChatReceipt, error) {
	if m.Channel == "" || m.Text == "" {
		return ports.ChatReceipt{}, fmt.Errorf("slack: a message needs a channel and a text")
	}
	body, err := json.Marshal(map[string]string{"channel": m.Channel, "text": m.Text})
	if err != nil {
		return ports.ChatReceipt{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.Base+PostPath, bytes.NewReader(body))
	if err != nil {
		return ports.ChatReceipt{}, err
	}
	req.Header.Set("Authorization", "Bearer "+p.Token)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := p.client().Do(req)
	if err != nil {
		return ports.ChatReceipt{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode == http.StatusTooManyRequests {
		return ports.ChatReceipt{}, limited(resp)
	}
	if resp.StatusCode >= 500 {
		return ports.ChatReceipt{}, fmt.Errorf("slack: answered %d", resp.StatusCode)
	}
	var a answer
	if err := json.Unmarshal(raw, &a); err != nil {
		return ports.ChatReceipt{}, fmt.Errorf("slack: the answer is not JSON (status %d)", resp.StatusCode)
	}
	switch {
	case a.OK:
		return ports.ChatReceipt{ID: a.TS}, nil
	case a.Error == "ratelimited":
		return ports.ChatReceipt{}, limited(resp)
	case a.Error == "internal_error" || a.Error == "service_unavailable" || a.Error == "fatal_error" || a.Error == "request_timeout":
		return ports.ChatReceipt{}, fmt.Errorf("slack: %s", a.Error)
	}
	return ports.ChatReceipt{}, fmt.Errorf("%w: %s", ports.ErrChatRefused, a.Error)
}

func limited(r *http.Response) error {
	secs, _ := strconv.Atoi(r.Header.Get("Retry-After"))
	if secs <= 0 {
		secs = 30
	}
	return ports.ChatLimited{After: time.Duration(secs) * time.Second}
}
