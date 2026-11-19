package api

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports/storage"
	oauthtokens "github.com/cognizhi/fyniq/internal/tokens"
)

// endAIAppTokens ends the tokens AI apps hold when an organisation's setting takes them away (PRD-001 AI-35, AI-38; p1-mcp-first-party
// task 3.3): turning `features.mcp` off revokes every member's AI app tokens, and taking an app out of `mcp.clients` revokes that one
// app's tokens for every member, once the change commits. The per-call gate refuses the next call at once either way (it asks the switch
// and the allowlist on every call); this ends the tokens at the OAuth server too, so a token cannot outlive the decision. A person's
// other tokens (their web session, their integrations) are left alone. Never on the AI's or MCP's tool list.
func (a *API) endAIAppTokens(ctx context.Context, u storage.UnitOfWork, key, was, now string) error {
	if a.deps.Outbox == nil {
		return nil
	}
	var removed []string
	switch key {
	case "features.mcp":
		if was != "true" || now == "true" {
			return nil
		}
	case "mcp.clients":
		after := map[string]bool{}
		for _, c := range strings.Split(now, ",") {
			after[strings.TrimSpace(c)] = true
		}
		for _, c := range strings.Split(was, ",") {
			if c = strings.TrimSpace(c); c != "" && !after[c] {
				removed = append(removed, c)
			}
		}
		if len(removed) == 0 {
			return nil
		}
	default:
		return nil
	}
	o, err := u.Organisation(ctx)
	if err != nil {
		return err
	}
	members, err := u.Members().List(ctx, a.deps.Clock.Now())
	if err != nil {
		return err
	}
	seen := map[uuid.UUID]bool{}
	for _, m := range members {
		if seen[m.UserID] {
			continue
		}
		seen[m.UserID] = true
		if key == "features.mcp" {
			if err := oauthtokens.RevokeAgents(ctx, *a.deps.Outbox, u, o.ID, m.UserID.String(), "the organisation switched AI apps off"); err != nil {
				return err
			}
			continue
		}
		for _, c := range removed {
			if err := oauthtokens.RevokeAgentClient(ctx, *a.deps.Outbox, u, o.ID, m.UserID.String(), c, "the organisation took the app off its allowlist"); err != nil {
				return err
			}
		}
	}
	return nil
}
