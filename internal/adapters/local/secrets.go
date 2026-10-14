//go:build local

package local

import (
	"context"
	"fmt"

	"github.com/cognizhi/fyniq/internal/ports"
)

// Secrets reads FYNIQ_SECRET_<NAME> values: the git-ignored .env that `make setup` writes,
// loaded into the environment by the Makefile and Compose.
type Secrets struct {
	lookup func(string) (string, bool)
}

// NewSecrets reads secrets through lookup (os.LookupEnv in services).
func NewSecrets(lookup func(string) (string, bool)) *Secrets { return &Secrets{lookup: lookup} }

// Get returns the secret called name.
func (s *Secrets) Get(_ context.Context, name string) (string, error) {
	if v, ok := s.lookup("FYNIQ_SECRET_" + name); ok {
		return v, nil
	}
	return "", fmt.Errorf("secret %s: %w", name, ports.ErrNotFound)
}

var _ ports.Secrets = (*Secrets)(nil)
