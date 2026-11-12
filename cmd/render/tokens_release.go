//go:build !local

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/cognizhi/fyniq/internal/adapters/gcp"
	"github.com/cognizhi/fyniq/internal/egress"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/render"
)

// callers are the render service's callers (PRD-001 §12.3, rev 0.96): FYNIQ_GCP's render entry names its audience and
// the api's and the worker's accounts, whose Google-signed tokens it accepts. The service reads nothing else of it.
func callers(clock ports.Clock) (ports.TaskTokens, error) {
	var c struct {
		Render gcp.Callers `json:"render"`
	}
	if err := json.Unmarshal([]byte(os.Getenv("FYNIQ_GCP")), &c); err != nil {
		return nil, fmt.Errorf("FYNIQ_GCP is not the Google Cloud wiring as JSON: %w", err)
	}
	if !strings.HasPrefix(c.Render.Audience, "https://") || len(c.Render.ServiceAccounts) == 0 {
		return nil, errors.New("FYNIQ_GCP lacks render (its audience and its callers' service accounts)")
	}
	return &gcp.CallerTokens{Class: render.Class, Callers: c.Render, Keys: googleKeys(), Clock: clock}, nil
}

// googleKeys are Google's signing keys for the callers' ID tokens, fetched through the egress guard from Google's
// certificate host only (SEC-36).
func googleKeys() oidc.KeySet {
	client := (&egress.Guard{}).For(egress.HostOf(gcp.GoogleCerts))
	return oidc.NewRemoteKeySet(oidc.ClientContext(context.Background(), client), gcp.GoogleCerts)
}
