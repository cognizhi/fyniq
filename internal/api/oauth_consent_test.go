//go:build local

package api

import (
	"context"

	"github.com/cognizhi/fyniq/internal/authn"
)

// fakeConsentFlow answers the consent screen's two steps without an OAuth server, for the matrix.
type fakeConsentFlow struct{}

func (fakeConsentFlow) ConsentView(context.Context, string, authn.Principal) (map[string]any, error) {
	return map[string]any{
		"client":           map[string]any{"id": "claude-code", "name": "Claude Code", "class": "agent", "verified": true, "proposal_only": true},
		"requested_scopes": []string{"openid"},
		"decision":         map[string]any{"scopes": []string{"openid"}, "refused_scopes": []string{}, "audiences": []string{}},
	}, nil
}

func (fakeConsentFlow) AnswerConsent(context.Context, string, bool, []string, authn.Principal) (string, error) {
	return "http://localhost:8080/oauth/oauth2/auth?consent_verifier=abc", nil
}
