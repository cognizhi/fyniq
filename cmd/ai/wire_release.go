//go:build !local

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/cognizhi/fyniq/internal/adapters/gcp"
	"github.com/cognizhi/fyniq/internal/assistant"
	"github.com/cognizhi/fyniq/internal/egress"
	"github.com/cognizhi/fyniq/internal/ports"
)

// wire is the AI service in the cloud (design D3, D4) [live-verify]: FYNIQ_GCP's ai entry names its audience and the api's
// account, whose Google-signed tokens it accepts; its api entry names the API's internal URL and the audience the AI
// service's own token is for, which the API accepts from the ai invoker. It reads nothing else.
func wire(clock ports.Clock) (ports.TaskTokens, assistant.Backend, error) {
	var c struct {
		AI  gcp.Callers `json:"ai"`
		API gcp.Callers `json:"api"`
	}
	if err := json.Unmarshal([]byte(os.Getenv("FYNIQ_GCP")), &c); err != nil {
		return nil, nil, fmt.Errorf("FYNIQ_GCP is not the Google Cloud wiring as JSON: %w", err)
	}
	if !strings.HasPrefix(c.AI.Audience, "https://") || len(c.AI.ServiceAccounts) == 0 {
		return nil, nil, errors.New("FYNIQ_GCP lacks ai (its audience and the api's service account)")
	}
	if !strings.HasPrefix(c.API.URL, "https://") || c.API.Audience == "" {
		return nil, nil, errors.New("FYNIQ_GCP lacks api (the API's internal URL and its audience)")
	}
	keys := oidc.NewRemoteKeySet(oidc.ClientContext(context.Background(), (&egress.Guard{Propagate: true}).For(egress.HostOf(gcp.GoogleCerts))), gcp.GoogleCerts)
	callers := &gcp.CallerTokens{Class: assistant.Class, Callers: c.AI, Keys: keys, Clock: clock}
	return callers, &assistant.Client{URL: c.API.URL, ServiceToken: gcp.IdentityTokens(c.API.Audience, nil),
		HTTP: (&egress.Guard{Propagate: true}).Bounded(8<<20, 5*time.Minute).For(egress.HostOf(c.API.URL))}, nil
}
