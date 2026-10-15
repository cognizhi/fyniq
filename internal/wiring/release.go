//go:build !local

package wiring

import (
	"context"
	"errors"

	"github.com/cognizhi/fyniq/internal/ports"
)

// Build needs the GCP adapters (Cloud Tasks, Cloud Run jobs, Cloud Storage, Cloud KMS, OIDC tokens),
// which arrive with p0-infra-opentofu; until then a release build serves without background work.
func Build(context.Context, string, ports.Clock) (*Kit, error) {
	return nil, errors.New("background work needs the GCP adapters, which arrive with p0-infra-opentofu")
}
