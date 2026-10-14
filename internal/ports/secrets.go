package ports

import (
	"context"
	"errors"
)

// ErrNotFound is returned when a named secret, object or task does not exist.
var ErrNotFound = errors.New("not found")

// Secrets reads secrets by name. Production: Secret Manager (SEC-12). Local: the git-ignored
// .env that `make setup` writes.
type Secrets interface {
	Get(ctx context.Context, name string) (string, error)
}
