package main

import (
	"cmp"
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/app"
	"github.com/cognizhi/fyniq/internal/httpserver"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/auth"
	"github.com/cognizhi/fyniq/internal/nemura/authz"
	"github.com/cognizhi/fyniq/internal/nemura/identity"
	"github.com/cognizhi/fyniq/internal/ports"
)

// signInWiring builds the sign-in the api role offers: the provider's flow behind the shared SignIn port. The local build (local.go) wires
// the local sign-in page; the release build's is Identity Platform, one tenant per organisation (PRD-002 question 2), which is the cloud
// code of p1-nemura-platform task 10.1 [live-verify]: until then the release api serves no sign-in and says so in its log.
var signInWiring = func(app.Deps, func(string) string, *httpserver.Server) (ports.SignIn, error) { return nil, nil }

// sessions is the database as the sign-in flow and the API see it.
type sessions struct{ *nemura.Store }

func (s sessions) MembershipsOf(ctx context.Context, who identity.Identity) ([]auth.Membership, error) {
	ms, err := s.Store.MembershipsOf(ctx, who)
	out := make([]auth.Membership, len(ms))
	for i, m := range ms {
		out[i] = auth.Membership{Organisation: m.Organisation, Member: m.Member}
	}
	return out, err
}

func (s sessions) StartSession(ctx context.Context, org, member uuid.UUID, now time.Time) (string, error) {
	return s.Store.OpenSession(ctx, org, member, now)
}

func (s sessions) EndSession(ctx context.Context, token string, now time.Time) error {
	return s.Store.CloseSession(ctx, token, now)
}

func (s sessions) Authenticate(ctx context.Context, token string, now time.Time) (api.Session, error) {
	got, err := s.Store.Authenticate(ctx, token, now)
	return api.Session{Organisation: got.Organisation, Member: got.Member}, err
}

func (s sessions) RolesAt(ctx context.Context, org, member uuid.UUID, on time.Time) ([]authz.Role, error) {
	return s.Store.RolesAt(ctx, org, member, on)
}

// apiRoutes registers the public API and, when a sign-in is wired, the browser's way in and out.
func apiRoutes(s *httpserver.Server, deps app.Deps, getenv func(string) string, store *nemura.Store) error {
	origin := cmp.Or(getenv("NEMURA_PUBLIC_ORIGIN"), "http://localhost:"+cmp.Or(getenv("PORT"), "8080"))
	base := getenv("NEMURA_API_BASE") // where the API is mounted on the origin: "/api" behind the edge, "" directly
	ss := sessions{store}
	api.New(s, api.Deps{Sessions: ss, Clock: deps.Clock, CookieNames: []string{auth.SessionCookie(true), auth.SessionCookie(false)}})
	provider, err := signInWiring(deps, getenv, s)
	if err != nil {
		return err
	}
	if provider == nil {
		return nil
	}
	flow := auth.Flow{SignIn: provider, Store: ss, Clock: deps.Clock, Origin: origin, Base: base, Tenant: cmp.Or(getenv("NEMURA_TENANT"), "default")}
	flow.Register(mux{s})
	return nil
}

// mux adapts the server's HandleFunc to the flow's registration.
type mux struct{ s *httpserver.Server }

func (m mux) HandleFunc(pattern string, h func(http.ResponseWriter, *http.Request)) {
	m.s.HandleFunc(pattern, h)
}
