package mcpserver

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/mcpdelegation"
)

// Delegate obtains the API-audience token for a person (open question 40, option (b): internal/mcpdelegation). The MCP
// token never goes to the API; this token does, and lives at most five minutes.
type Delegate interface {
	For(ctx context.Context, subject string, scopes []string) (mcpdelegation.Token, error)
}

// APIBackend runs a tool through the API's one MCP operation (design D3): it asks the OAuth server for a delegated token for
// the person the validated MCP token names, in the organisation they consented in, and posts the call with that token and its own
// service token. Every gate is the API's; this only carries the call and turns the answer into what MCP says.
type APIBackend struct {
	URL          string // the API's internal base, such as http://api:8080/internal/v1
	ServiceToken func(ctx context.Context) (string, error)
	Delegate     Delegate
	HTTP         *http.Client
}

var _ Backend = APIBackend{}

// maxAnswer bounds what the API may answer: a result is cut at the organisation's ceiling, which is far smaller.
const maxAnswer = 4 << 20

// CallTool implements Backend.
func (b APIBackend) CallTool(ctx context.Context, c Call) (Result, error) {
	org, orgErr := uuid.Parse(c.Token.Organisation)
	user, userErr := uuid.Parse(c.Token.Subject)
	if orgErr != nil || userErr != nil || org == uuid.Nil || user == uuid.Nil {
		return Result{}, &Refusal{Status: http.StatusUnauthorized, Code: CodeAuth, Detail: "The token does not name a person in an organisation.", Fix: "Connect the app again and sign in"}
	}
	delegated, err := b.Delegate.For(ctx, authn.DelegatedSubject(org, user), c.Token.Scopes)
	if err != nil {
		if errors.Is(err, mcpdelegation.ErrRefused) {
			return Result{}, &Refusal{Status: http.StatusForbidden, Code: CodeScope, Detail: "Fyniq would not give this app access for you now.", Fix: "Connect the app again, or ask an Owner"}
		}
		return Result{}, err
	}
	service, err := b.ServiceToken(ctx)
	if err != nil {
		return Result{}, err
	}
	sum := sha256.Sum256([]byte(c.Bearer))
	ip, _, _ := net.SplitHostPort(c.RemoteIP)
	body, _ := json.Marshal(map[string]any{"app": c.Token.ClientID, "tool": c.Tool, "input": c.Args, "ip": cmp.Or(ip, c.RemoteIP),
		"app_version": truncate(c.Client, 200), "token_id": hex.EncodeToString(sum[:8])})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(b.URL, "/")+"/mcp/tool-calls", bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+delegated.AccessToken)
	req.Header.Set("X-Fyniq-Service-Token", service)
	if b.HTTP == nil {
		return Result{}, errors.New("the API backend has no HTTP client: it is built with one from internal/egress")
	}
	resp, err := b.HTTP.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("the API did not answer: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	if err != nil {
		return Result{}, err
	}
	if resp.StatusCode == http.StatusOK {
		return Result{Structured: json.RawMessage(raw), Text: string(raw)}, nil
	}
	var p struct {
		Code   string `json:"code"`
		Detail string `json:"detail"`
		Fix    string `json:"fix"`
	}
	if json.Unmarshal(raw, &p) != nil || p.Code == "" {
		return Result{}, fmt.Errorf("the API answered %d", resp.StatusCode)
	}
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return Result{}, &Refusal{Status: 401, Code: CodeAuth, Detail: p.Detail, Fix: p.Fix}
	case http.StatusForbidden:
		r := &Refusal{Status: 403, Code: p.Code, Detail: p.Detail, Fix: p.Fix}
		r.Scopes = strings.Fields(resp.Header.Get("X-Fyniq-Needed-Scopes"))
		return Result{}, r
	case http.StatusTooManyRequests:
		secs, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
		return Result{}, &Refusal{Status: 429, Code: p.Code, Detail: p.Detail, Fix: p.Fix, RetryAfter: time.Duration(secs) * time.Second}
	case http.StatusUnprocessableEntity, http.StatusBadRequest, http.StatusNotFound:
		// What the model can act on: a wrong input, a result too large, a record that is not there.
		return Result{IsError: true, Text: strings.TrimSpace(p.Detail + " " + p.Fix + " (" + p.Code + ")")}, nil
	}
	return Result{}, fmt.Errorf("the API answered %d (%s)", resp.StatusCode, p.Code)
}
