package postgres

import (
	"context"
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The sign-in providers' switches (FR-ADM-08, rev 0.197; migration 000240): a global table, read by the sign-in flow and
// the API, changed only through set_sign_in_method in a unit of work of the installation organisation, whose trail
// records each change.

func toSignInMethod(r sqlcdb.FyniqSignInMethod) storage.SignInMethod {
	return storage.SignInMethod{Provider: r.Provider, Enabled: r.Enabled, AllowedDomains: r.AllowedDomains, JoinByDomain: r.JoinByDomain,
		Version: int(r.Version), ChangedBy: r.ChangedBy, ChangedAt: r.ChangedAt}
}

// SignInMethods implements storage.Sessions.
func (s *Store) SignInMethods(ctx context.Context) ([]storage.SignInMethod, error) {
	rows, err := s.q().SignInMethods(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.SignInMethod, 0, len(rows))
	for _, r := range rows {
		out = append(out, toSignInMethod(r))
	}
	return out, nil
}

// SignInEssential implements storage.Sessions.
func (s *Store) SignInEssential(ctx context.Context) ([]string, error) {
	out, err := s.q().SignInEssential(ctx)
	return out, mapError(err)
}

// SignInCounts implements storage.Sessions.
func (s *Store) SignInCounts(ctx context.Context, since time.Time) (map[string]int, error) {
	rows, err := s.q().SignInCounts(ctx, since)
	if err != nil {
		return nil, mapError(err)
	}
	out := make(map[string]int, len(rows))
	for _, r := range rows {
		out[r.Provider] = int(r.SignIns)
	}
	return out, nil
}

func (u *unitOfWork) SignInMethods() storage.SignInMethodWriter { return signInMethods{u} }

type signInMethods struct{ u *unitOfWork }

func (r signInMethods) List(ctx context.Context) ([]storage.SignInMethod, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.SignInMethods(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.SignInMethod, 0, len(rows))
	for _, x := range rows {
		out = append(out, toSignInMethod(x))
	}
	return out, nil
}

func (r signInMethods) Essential(ctx context.Context) ([]string, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	out, err := r.u.q.SignInEssential(ctx)
	return out, mapError(err)
}

func (r signInMethods) Set(ctx context.Context, by uuid.UUID, change *uuid.UUID, c storage.SignInMethodChange, at time.Time) (storage.SignInMethod, error) {
	if err := (passkeys{r.u}).installation(ctx); err != nil { // the installation's trail records sign-in changes too
		return storage.SignInMethod{}, err
	}
	before := storage.MethodOf(nil, c.Provider)
	if all, err := r.u.q.SignInMethods(ctx); err != nil {
		return storage.SignInMethod{}, mapError(err)
	} else {
		for _, m := range all {
			if m.Provider == c.Provider {
				before = toSignInMethod(m)
			}
		}
	}
	if c.Version < 0 || c.Version > math.MaxInt32 {
		return storage.SignInMethod{}, &storage.Error{Code: storage.CodeVersion, Message: "the provider's version is out of range"}
	}
	domains := c.AllowedDomains
	if domains == nil {
		domains = []string{}
	}
	row, err := r.u.q.SetSignInMethod(ctx, sqlcdb.SetSignInMethodParams{ByUser: by, ChangeID: change, Provider: c.Provider,
		Version: int32(c.Version), Enabled: c.Enabled, Domains: domains, JoinByDomain: c.JoinByDomain, EndSessions: c.EndSessions, At: at}) // #nosec G115 -- a version number, checked above
	if err != nil {
		return storage.SignInMethod{}, mapError(err)
	}
	m := toSignInMethod(row)
	changes := map[string][2]any{"provider": {before.Provider, m.Provider}, "enabled": {before.Enabled, m.Enabled},
		"allowed_domains": {before.AllowedDomains, m.AllowedDomains}, "join_by_domain": {before.JoinByDomain, m.JoinByDomain}}
	if !m.Enabled && c.EndSessions {
		changes["sessions_ended"] = [2]any{false, true}
	}
	if change != nil {
		changes["change_id"] = [2]any{nil, change.String()}
	}
	version := m.Version
	return m, r.u.event(as(ctx, by), "sign_in_method.changed", "sign_in_method", uuid.NewSHA1(uuid.NameSpaceURL, []byte("fyniq:sign-in:"+m.Provider)),
		&version, changes)
}
