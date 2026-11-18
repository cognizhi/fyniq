package wiring

import (
	"context"
	"fmt"
	"net"
	"net/url"

	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// pointedAt is the service's own database URL pointed at the restore drill's copy (DR-06): its host, Cloud SQL's private
// address with the port kept, or its database locally. The role, its password and the TLS settings stay the service's.
func pointedAt(base string, t ports.CloneTarget) (string, error) {
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		return "", fmt.Errorf("the database URL is not a postgres:// URL, so the drill's copy cannot be reached with it")
	}
	if t.Host != "" {
		if port := u.Port(); port != "" {
			u.Host = net.JoinHostPort(t.Host, port)
		} else {
			u.Host = t.Host
		}
	}
	if t.Database != "" {
		u.Path = "/" + t.Database
	}
	return u.String(), nil
}

// copyOpener opens the drill's copy as the service's own role, with its store's options.
func copyOpener(base string, opts ...postgres.Option) func(ctx context.Context, t ports.CloneTarget) (storage.Store, func(), error) {
	return func(_ context.Context, t ports.CloneTarget) (storage.Store, func(), error) {
		u, err := pointedAt(base, t)
		if err != nil {
			return nil, func() {}, err
		}
		s, err := postgres.Connect(u, opts...)
		if err != nil {
			return nil, func() {}, err
		}
		return s, s.Close, nil
	}
}
