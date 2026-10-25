package ports

import (
	"context"
	"errors"
	"fmt"
	"regexp"
)

// SecretStore keeps the secrets people type in Settings › Integrations (FR-INT-20): Secret Manager in production
// (SEC-12), files under .data in the local flavour. The database keeps only a SecretRef, which names one version of
// one secret; a version never changes, so a value may be cached by its reference.
type SecretStore interface {
	// Put adds value as a new version of the secret name and returns its reference. The secret must exist: in
	// production, OpenTofu creates each one (the platform may only add versions to it); locally, Put creates it.
	Put(ctx context.Context, name, value string) (SecretRef, error)
	// Get reads the version ref names: ErrNotFound when it was destroyed or never was, ErrSecretDisabled when it is
	// disabled.
	Get(ctx context.Context, ref SecretRef) (string, error)
	// Latest returns the reference of the newest enabled version of name, which the first-start bootstrap pins
	// (rev 0.139): ErrNotFound when there is none.
	Latest(ctx context.Context, name string) (SecretRef, error)
	// Disable stops ref being read; Destroy removes its value for good. Each is idempotent.
	Disable(ctx context.Context, ref SecretRef) error
	Destroy(ctx context.Context, ref SecretRef) error
}

// SecretRef names one version of a secret, such as projects/p/secrets/connector-zeptomail-send_token/versions/3.
// It is not secret: it is what the database, the audit trail and the API show instead of the value.
type SecretRef string

// ErrSecretDisabled is a secret version that was disabled, as a rotation does to the old one after its overlap.
var ErrSecretDisabled = errors.New("the secret version is disabled")

// secretName is a name Secret Manager and a file system both accept.
var secretName = regexp.MustCompile(`^[a-z][a-z0-9_-]{2,254}$`)

// CheckSecretName refuses a name that could not be a secret's in every store.
func CheckSecretName(name string) error {
	if !secretName.MatchString(name) {
		return fmt.Errorf("secret name %q is not 3 to 255 lower-case letters, digits, hyphens and underscores", name)
	}
	return nil
}
