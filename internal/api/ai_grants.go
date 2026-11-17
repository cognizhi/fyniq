package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// aiReadable is whether the assistant's delegated token may call op: only reads (PRD-001 §9.3 rev 0.277, AI-19). The
// read operations that take a body, such as the semantic layer's query, are named here as they arrive.
func aiReadable(op *huma.Operation) bool {
	return op.Method == http.MethodGet
}

// issueAIGrant issues the delegated read token a turn's tools read with (AI-01, AI-36; p1-ai-assistant design D2): as
// the person user, in the unit of work's organisation, for at most authn.MaxDelegated, never the person's session. Only
// its SHA-256 is kept; it ends with the turn (endAIGrant).
func (a *API) issueAIGrant(ctx context.Context, u storage.UnitOfWork, org, user, turn uuid.UUID) (string, error) {
	token, hash, err := authn.NewAIToken(org)
	if err != nil {
		return "", err
	}
	now := a.deps.Clock.Now()
	if _, err := u.AIGrants().Issue(ctx, storage.AIGrant{TokenHash: hash, UserID: user, Turn: turn, IssuedAt: now,
		ExpiresAt: now.Add(authn.MaxDelegated)}); err != nil {
		return "", err
	}
	return token, nil
}

// endAIGrant ends a turn's delegated read token, so nothing reads with it after the turn.
func (a *API) endAIGrant(ctx context.Context, u storage.UnitOfWork, turn uuid.UUID) error {
	return u.AIGrants().End(ctx, turn, a.deps.Clock.Now())
}
