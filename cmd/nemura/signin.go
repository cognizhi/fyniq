package main

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/hydra"
	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/app"
	"github.com/cognizhi/fyniq/internal/egress"
	"github.com/cognizhi/fyniq/internal/httpserver"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/auth"
	"github.com/cognizhi/fyniq/internal/nemura/authz"
	"github.com/cognizhi/fyniq/internal/nemura/identity"
	"github.com/cognizhi/fyniq/internal/passkey"
	"github.com/cognizhi/fyniq/internal/ports"
)

// signInWiring builds the sign-in the api role offers: the provider's flow behind the shared SignIn port. The local build (local.go) wires
// the local sign-in page; the release build's is Identity Platform, one tenant per organisation (PRD-002 question 2), which is the cloud
// code of p1-nemura-platform task 10.1 [live-verify]: until then the release api serves no sign-in and says so in its log.
var signInWiring = func(app.Deps, func(string) string, *httpserver.Server) (ports.SignIn, error) { return nil, nil }

// hydraAdmin is Nemura's own OAuth server's admin API at admin, through the platform's egress guard to admin's host only, with a short timeout.
// The local flavour's guard lets the Docker network through (local.go); the release flavour's reaches public addresses only.
var hydraAdmin = func(admin string) ports.OAuthClients {
	return hydra.New(admin, (&egress.Guard{Propagate: true}).Bounded(0, 10*time.Second).For(egress.HostOf(admin)))
}

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
	return api.Session{ID: got.ID, Organisation: got.Organisation, Member: got.Member, StepUpAt: got.StepUpAt}, err
}

func (s sessions) RolesAt(ctx context.Context, org, member uuid.UUID, on time.Time) ([]authz.Role, error) {
	return s.Store.RolesAt(ctx, org, member, on)
}

// apiRoutes registers the public API and, when a sign-in is wired, the browser's way in and out.
func apiRoutes(s *httpserver.Server, deps app.Deps, getenv func(string) string, store *nemura.Store) error {
	origin := cmp.Or(getenv("NEMURA_PUBLIC_ORIGIN"), "http://localhost:"+cmp.Or(getenv("PORT"), "8080"))
	base := getenv("NEMURA_API_BASE") // where the API is mounted on the origin: "/api" behind the edge, "" directly
	ss := sessions{store}
	pk, err := newPasskeys(store, origin, getenv("NEMURA_RP_ID"), deps.Clock)
	if err != nil {
		return err
	}
	var oauth ports.OAuthClients
	if admin := getenv("NEMURA_OAUTH_ADMIN_URL"); admin != "" {
		oauth = hydraAdmin(admin)
	}
	api.New(s, api.Deps{Sessions: ss, Passkeys: pk, Clients: nemura.APIClientService{Store: store, OAuth: oauth, Clock: deps.Clock}, Clock: deps.Clock, CookieNames: []string{auth.SessionCookie(true), auth.SessionCookie(false)}})
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

// passkeys runs the passkey ceremonies of the signed-in person against the database, with Nemura's relying party: the registrable domain
// Fyniq and Nemura share is its ID (NEMURA_RP_ID; question 10), so one passkey works in both.
type passkeys struct {
	store *nemura.Store
	rp    *passkey.RelyingParty
	clock ports.Clock
}

func newPasskeys(store *nemura.Store, origin, rpID string, clock ports.Clock) (*passkeys, error) {
	if rpID == "" {
		u, err := url.Parse(origin)
		if err != nil {
			return nil, err
		}
		rpID = u.Hostname()
	}
	rp, err := passkey.NewFor(origin, rpID, "Nemura")
	if err != nil {
		return nil, err
	}
	return &passkeys{store: store, rp: rp, clock: clock}, nil
}

func refusal(err error) error {
	switch {
	case errors.Is(err, passkey.ErrRefused):
		return api.ErrPasskeyRefused
	case errors.Is(err, nemura.ErrNoChallenge):
		return api.ErrNoCeremony
	case errors.Is(err, nemura.ErrNoPasskey):
		return api.ErrNoPasskey
	}
	return err
}

func (p *passkeys) BeginRegistration(ctx context.Context, s api.Session) ([]byte, error) {
	name, err := p.store.MemberName(ctx, s.Organisation, s.Member)
	if err != nil {
		return nil, err
	}
	b, err := p.store.BeginRegistrationFor(ctx, p.rp, s.Organisation, p.clock.Now(), s.ID, s.Member, name)
	return b, refusal(err)
}

func (p *passkeys) CompleteRegistration(ctx context.Context, s api.Session, passkeyName string, credential []byte) (uuid.UUID, error) {
	name, err := p.store.MemberName(ctx, s.Organisation, s.Member)
	if err != nil {
		return uuid.Nil, err
	}
	id, err := p.store.CompleteRegistration(ctx, p.rp, s.Organisation, p.clock.Now(), s.ID, s.Member, name, passkeyName, credential)
	return id, refusal(err)
}

func (p *passkeys) BeginStepUp(ctx context.Context, s api.Session) ([]byte, error) {
	name, err := p.store.MemberName(ctx, s.Organisation, s.Member)
	if err != nil {
		return nil, err
	}
	b, err := p.store.BeginStepUpFor(ctx, p.rp, s.Organisation, p.clock.Now(), s.ID, s.Member, name)
	return b, refusal(err)
}

func (p *passkeys) CompleteStepUp(ctx context.Context, s api.Session, credential []byte) error {
	name, err := p.store.MemberName(ctx, s.Organisation, s.Member)
	if err != nil {
		return err
	}
	return refusal(p.store.CompleteStepUp(ctx, p.rp, s.Organisation, p.clock.Now(), s.ID, s.Member, name, credential))
}
