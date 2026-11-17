//go:build local

package api

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The assistant's delegated read token (PRD-001 §9.3 rev 0.277, AI-01, AI-36; p1-ai-assistant design D2): issued per
// turn, it reads as the person, through the lesser of their rights and the AI column's; it changes nothing, holds no
// session, and reads nothing once its turn ended, past 5 minutes, for someone who is not a member, or when it names
// another organisation.
// req: AI-01
// req: ai/proposals The orchestrator holds no write credential
// asvs: V7.2.3, V7.4.1, V8.2.1, V8.2.2
func TestTheAssistantsTokenReadsAsThePersonForItsTurn(t *testing.T) {
	e := newEnv(t)
	f := foundersIn(e)
	// Someone who is a member only of another organisation.
	elsewhereEnv := newEnv(t)
	code := elsewhereEnv.call(http.MethodPost, "/v1/members/invitations", elsewhereEnv.as(authz.Owner),
		map[string]any{"display_name": "Stranger", "roles": []string{"bookkeeper"}}).str("code")
	outsider := elsewhereEnv.call(http.MethodPost, "/v1/members", elsewhereEnv.principal(authn.Principal{Subject: "stranger-" + uuid.NewString(), Client: "web"}),
		map[string]any{"invitation_code": code, "display_name": "Stranger"}).str("user_id")
	if outsider == "" {
		t.Fatal("the stranger did not join")
	}
	issue := func(user string) (string, uuid.UUID) {
		t.Helper()
		turn := uuid.New()
		var token string
		e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
			token, err = e.api.issueAIGrant(ctx, u, e.books.Org, uuid.MustParse(user), turn)
			return err
		})
		return token, turn
	}

	token, turn := issue(f.ahkow)
	if !strings.HasPrefix(token, authn.AITokenPrefix) {
		t.Fatalf("the token: %q", token)
	}
	if r := e.call(http.MethodGet, "/v1/accounts", token, nil); r.Status != http.StatusOK {
		t.Fatalf("reading as the person: %d %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/accounts", token, map[string]any{"code": "6990", "name": "Assistant's", "type": "expense"}),
		"createAccount", 403, CodeForbidden)
	if r := e.call(http.MethodGet, "/v1/session", token, nil); r.Status != http.StatusForbidden {
		t.Errorf("the token as a session: %d %s", r.Status, r.Raw)
	}

	// Naming another organisation finds nothing: the organisation travels in the token, the secret does not.
	raw, _ := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(token, authn.AITokenPrefix))
	elsewhere := uuid.New()
	forged := authn.AITokenPrefix + base64.RawURLEncoding.EncodeToString(append(elsewhere[:], raw[16:]...))
	if r := e.call(http.MethodGet, "/v1/accounts", forged, nil); r.Status != http.StatusUnauthorized {
		t.Errorf("a token naming another organisation: %d", r.Status)
	}

	// Ended with its turn, it reads nothing.
	e.do(func(ctx context.Context, u storage.UnitOfWork) error { return e.api.endAIGrant(ctx, u, turn) })
	if r := e.call(http.MethodGet, "/v1/accounts", token, nil); r.Status != http.StatusUnauthorized {
		t.Errorf("after its turn: %d", r.Status)
	}
	// Nor past 5 minutes, nor for someone who is not a member here.
	late, _ := issue(f.weiling)
	e.clock.Advance(authn.MaxDelegated + time.Second)
	if r := e.call(http.MethodGet, "/v1/accounts", late, nil); r.Status != http.StatusUnauthorized {
		t.Errorf("past 5 minutes: %d", r.Status)
	}
	stranger, _ := issue(outsider)
	if r := e.call(http.MethodGet, "/v1/accounts", stranger, nil); r.Status != http.StatusUnauthorized {
		t.Errorf("a token for someone who is not a member: %d", r.Status)
	}
}
