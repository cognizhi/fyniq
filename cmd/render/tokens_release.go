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
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/render/service"
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
	return &gcp.CallerTokens{Class: service.Class, Callers: c.Render, Keys: oidc.NewRemoteKeySet(context.Background(), gcp.GoogleCerts), Clock: clock}, nil
}
