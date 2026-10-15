package gcp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/cognizhi/fyniq/internal/ports"
)

// GoogleCerts are the keys that sign Google's OIDC tokens, the ones Cloud Tasks and Cloud Scheduler
// mint for a service account.
const GoogleCerts = "https://www.googleapis.com/oauth2/v3/certs"

// GoogleIssuer is their issuer.
const GoogleIssuer = "https://accounts.google.com"

// Invoker is who may call a worker class's handler: the service account Cloud Tasks (or Cloud
// Scheduler, for the tick) signs as, and the audience it asks for.
type Invoker struct {
	ServiceAccount string `json:"service_account"`
	Audience       string `json:"audience"`
}

// TaskTokens is the TaskTokens port on Google's OIDC tokens (§12.11): a handler accepts a request only
// with a token Google signed, for its own audience, as its own class's invoker account. So the
// executor runs nothing another class sent (Proposal executor queue isolation), and only Cloud
// Scheduler can call the tick.
type TaskTokens struct {
	Invokers map[string]Invoker // worker class (worker, executor, jobs, scheduler) → who may call it
	Keys     oidc.KeySet        // oidc.NewRemoteKeySet(ctx, GoogleCerts)
	Clock    ports.Clock
}

var _ ports.TaskTokens = (*TaskTokens)(nil)

// Token implements ports.TaskTokens. In Google Cloud, Cloud Tasks and Cloud Scheduler mint the tokens
// for their tasks; Fyniq's services never do.
func (t *TaskTokens) Token(class string) (string, error) {
	return "", fmt.Errorf("cloud tasks mints %s's tokens; the service does not", class)
}

// Verify implements ports.TaskTokens.
func (t *TaskTokens) Verify(token, class string) error {
	inv, ok := t.Invokers[class]
	if !ok || token == "" {
		return ports.ErrUnauthorised
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	v := oidc.NewVerifier(GoogleIssuer, t.Keys, &oidc.Config{ClientID: inv.Audience, SupportedSigningAlgs: []string{oidc.RS256}, Now: t.Clock.Now})
	id, err := v.Verify(ctx, token)
	if err != nil {
		return errors.Join(ports.ErrUnauthorised, err)
	}
	var c struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
	}
	if err := id.Claims(&c); err != nil || c.Email != inv.ServiceAccount || !c.EmailVerified {
		return errors.Join(ports.ErrUnauthorised, fmt.Errorf("the token is not %s's invoker", class))
	}
	return nil
}
