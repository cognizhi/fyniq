package authn

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The assistant's delegated read tokens (PRD-001 §9.3 rev 0.277, AI-01, AI-36; p1-ai-assistant design D2): for each
// turn the API issues one to the AI service, which presents it on its reads. It is opaque, never a session: the
// organisation in the clear, then 32 random bytes; the store keeps only its SHA-256, and ends it with the turn.
const (
	// AITokenPrefix starts a delegated AI read token.
	AITokenPrefix = "fyai_"
	// AssistantClient is the client a delegated AI read token's calls come through.
	AssistantClient = "fyniq-assistant"
	// AIScope is all a delegated AI read token grants: reading through the AI's tools.
	AIScope = "ai.read"
)

var aiTokenEncoding = base64.RawURLEncoding

// NewAIToken returns a fresh delegated AI read token for the organisation org, and its SHA-256.
func NewAIToken(org uuid.UUID) (token string, hash []byte, err error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", nil, err
	}
	token = AITokenPrefix + aiTokenEncoding.EncodeToString(append(org[:], secret...))
	sum := sha256.Sum256([]byte(token))
	return token, sum[:], nil
}

// ParseAIToken returns a delegated AI read token's organisation and SHA-256; false when token is not one.
func ParseAIToken(token string) (org uuid.UUID, hash []byte, ok bool) {
	raw, err := aiTokenEncoding.DecodeString(strings.TrimPrefix(token, AITokenPrefix))
	if !strings.HasPrefix(token, AITokenPrefix) || err != nil || len(raw) != 16+32 {
		return uuid.Nil, nil, false
	}
	org, err = uuid.FromBytes(raw[:16])
	if err != nil || org == uuid.Nil {
		return uuid.Nil, nil, false
	}
	sum := sha256.Sum256([]byte(token))
	return org, sum[:], true
}

// AIGrants authenticates delegated AI read tokens: the store resolves the token, in the organisation it names, to the
// person and their roles there now, while its turn is live and for at most 5 minutes. The caller is the AI acting for
// the person (class agent, AI-34): it gets the lesser of the person's rights and the AI column's, is audited as the AI
// on the person's behalf, and only reads.
type AIGrants struct {
	Store storage.AIGrantLookup
	Clock ports.Clock
}

var _ Authenticator = AIGrants{}

// Authenticate implements Authenticator. It reads only the bearer token.
func (a AIGrants) Authenticate(ctx context.Context, c Credentials) (Principal, error) {
	org, hash, ok := ParseAIToken(strings.TrimPrefix(c.Authorization, "Bearer "))
	if !ok || !strings.HasPrefix(c.Authorization, "Bearer ") {
		return Principal{}, ErrUnauthenticated
	}
	g, err := a.Store.AIGrant(ctx, org, hash, a.Clock.Now())
	if errors.Is(err, ports.ErrNotFound) {
		return Principal{}, ErrUnauthenticated
	}
	if err != nil {
		return Principal{}, err
	}
	p := Principal{Via: ViaToken, Subject: g.UserID.String(), UserID: g.UserID, Client: AssistantClient, Class: ClassAgent, Organisation: org,
		Scopes: []string{AIScope}, Turn: g.Turn}
	for _, r := range g.Roles {
		if _, known := authz.Roles[authz.Role(r)]; known {
			p.Roles = append(p.Roles, authz.Role(r))
		}
	}
	return p, nil
}
