package main

import (
	"cmp"
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"

	nemuraweb "github.com/cognizhi/fyniq/apps/nemura-web"
	"github.com/cognizhi/fyniq/internal/adapters/hydra"
	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/app"
	"github.com/cognizhi/fyniq/internal/egress"
	"github.com/cognizhi/fyniq/internal/httpserver"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/apiclient"
	"github.com/cognizhi/fyniq/internal/nemura/assistant"
	"github.com/cognizhi/fyniq/internal/nemura/auth"
	"github.com/cognizhi/fyniq/internal/nemura/authz"
	"github.com/cognizhi/fyniq/internal/nemura/identity"
	"github.com/cognizhi/fyniq/internal/nemura/letters"
	"github.com/cognizhi/fyniq/internal/nemura/linkauth"
	"github.com/cognizhi/fyniq/internal/passkey"
	"github.com/cognizhi/fyniq/internal/ports"
)

// signInWiring builds the sign-in the api role offers: the provider's flow behind the shared SignIn port. The local build (local.go) wires
// the local sign-in page; the release build's is Identity Platform, one tenant per organisation (PRD-002 question 2), which is the cloud
// code of p1-nemura-platform task 10.1 [live-verify]: until then the release api serves no sign-in and says so in its log.
var signInWiring = func(app.Deps, func(string) string, *httpserver.Server) (ports.SignIn, error) { return nil, nil }

// hydraAdmin is Nemura's own OAuth server's admin API at admin, through the platform's egress guard to admin's host only, with a short timeout.
// The local flavour's guard lets the Docker network through (local.go); the release flavour's reaches public addresses only.
// restrictedWiring is the key service and the file store for restricted people data. The release build has none until the cloud adapters exist
// (Cloud KMS and Cloud Storage, [live-verify]): the sealed fields and documents then answer NM_UNAVAILABLE. The local build wires files under NEMURA_DATA_DIR.
var restrictedWiring = func(getenv func(string) string) (ports.Cipher, ports.BlobStore) { return nil, nil }

// erasureWiring is the register of erasures (NM-SEC-08): a write-once store outside Nemura's database, and the key that signs its entries. The release build has none until the cloud adapters exist
// ([live-verify]: its own bucket with an object retention lock, and a Cloud KMS key); without it an erasure is refused as unavailable and nothing is erased.
var erasureWiring = func(getenv func(string) string) (ports.BlobStore, ports.Signer) { return nil, nil }

var hydraAdmin = func(admin string) ports.OAuthClients {
	return hydra.New(admin, (&egress.Guard{Propagate: true}).Bounded(0, 10*time.Second).For(egress.HostOf(admin)))
}

// linkHTTP is the client the worker reaches Fyniq with (the link's token endpoint and API): the platform's egress guard, to those hosts only, with a timeout. The local flavour's guard lets
// the Docker network through (local.go).
var linkHTTP = func(hosts ...string) *http.Client {
	return (&egress.Guard{Propagate: true}).Bounded(0, 15*time.Second).For(hosts...)
}

// signInInfo is what the sign-in page shows: the providers behind the shared SignIn port, and whether setup is done.
type signInInfo struct {
	provider ports.SignIn
	store    *nemura.Store
}

func (i signInInfo) Providers() []api.Provider {
	var out []api.Provider
	for _, p := range i.provider.Providers() {
		out = append(out, api.Provider{ID: p.ID, Name: p.Name})
	}
	return out
}

func (i signInInfo) SetupDone(ctx context.Context) (bool, error) { return i.store.SetupDone(ctx) }

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

func (s sessions) StartSession(ctx context.Context, org, member uuid.UUID, now time.Time, via identity.Via) (string, error) {
	return s.Store.OpenSession(ctx, org, member, now, via)
}

func (s sessions) EndSession(ctx context.Context, token string, now time.Time) error {
	return s.Store.CloseSession(ctx, token, now)
}

func (s sessions) Authenticate(ctx context.Context, token string, now time.Time) (api.Session, error) {
	got, err := s.Store.Authenticate(ctx, token, now)
	return api.Session{ID: got.ID, Organisation: got.Organisation, Member: got.Member, StepUpAt: got.StepUpAt, Leaver: got.Leaver, LeaverUntil: got.LeaverUntil, Person: got.Person, Provider: got.Via.Provider, Issuer: got.Via.Issuer, Subject: got.Via.Subject}, err
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
	provider, err := signInWiring(deps, getenv, s)
	if err != nil {
		return err
	}
	d := api.Deps{Sessions: ss, Passkeys: pk, Clients: nemura.APIClientService{Store: store, OAuth: oauth, Clock: deps.Clock}, People: nemura.PeopleService{Store: store, Clock: deps.Clock}, Features: nemura.FeatureService{Store: store, Clock: deps.Clock}, Restricted: restricted(store, deps, getenv), Packs: packsService(store, deps), Leave: leaveService(store, deps), Requests: leaveRequests(store, deps), Sick: sickService(store, deps, getenv), Government: governmentService(store, deps), Flex: flexService(store, deps), Time: timeService(store, deps), Letters: lettersService(store, deps, getenv), SelfService: selfService(store, deps, getenv), Onboarding: onboardingService(store, deps), Payroll: payrollService(store, deps, getenv), Idempotency: nemura.IdempotencyStore{Store: store}, Assistant: nemura.AssistantService{Store: store, Clock: deps.Clock, AllowScripted: scriptedAllowed}, Asker: askService(store, deps), Clock: deps.Clock,
		CookieNames: []string{auth.SessionCookie(true), auth.SessionCookie(false)}}
	if provider != nil {
		d.SignIn = signInInfo{provider: provider, store: store}
	}
	// A pay run is approved with the approver's passkey over the approval envelope, answered at Nemura's own origin for the relying party the two apps share (NM-INT-06).
	d.RunLink = nemura.RunLinkService{Store: store, Clock: deps.Clock, Origin: origin, RPID: relyingPartyID(origin, getenv("NEMURA_RP_ID"))}
	d.LinkOps = nemura.LinkOpsService{Store: store, Clock: deps.Clock}
	d.LetterVerify = nemura.LetterVerifyService{Store: store, Clock: deps.Clock}                                          // the public look-up of an issued letter (NM-LTR-05)
	d.Performance = nemura.PerformanceService{Store: store, Clock: deps.Clock}                                            // goals and KPIs (NM-PRF-01, NM-PRF-02)
	d.Hiring = nemura.HiringService{Store: store, Clock: deps.Clock}                                                      // requisitions and job posts (NM-REC-01 to NM-REC-03)
	d.Pipeline = nemura.PipelineService{Store: store, Clock: deps.Clock}                                                  // candidates, interview kits and consent (NM-REC-04, NM-REC-05)
	d.Careers = nemura.CareersService{Store: store, Clock: deps.Clock}                                                    // the public careers page (NM-REC-02)
	d.Plans = nemura.PlanService{Store: store, Clock: deps.Clock}                                                         // onboarding plans (NM-ONB-01)
	d.Grievances = nemura.GrievanceService{Store: store, Clock: deps.Clock}                                               // the confidential grievance channel (NM-SEC-06)
	d.Erasure = nemura.ErasureOpsService{Store: store, Clock: deps.Clock}                                                 // asking and approving an erasure; the worker erases (NM-SEC-08)
	d.Workspace = nemura.WorkspaceService{Store: store, FinanceOrigin: getenv("NEMURA_FYNIQ_WEB_ORIGIN")}                 // where the Finance | People switch leads; none: it does not show
	d.LinkAdmin = nemura.LinkAdminService{LinkService: nemura.LinkService{Store: store, OAuth: oauth, Clock: deps.Clock}} // an Owner proposes and another approves; approving needs the OAuth server
	// The link's inbox takes Fyniq's client's bearer token, checked with Nemura's own OAuth server; without one there is no inbox (the link needs the server for its client anyway).
	if server, ok := oauth.(ports.OAuthServer); ok {
		d.LinkAuth = &linkauth.Introspector{OAuth: server, Routes: store, Audience: apiclient.Audience, Clock: deps.Clock, TTL: linkauth.MaxCache}
		d.Link = nemura.LinkService{Store: store, OAuth: oauth, Clock: deps.Clock}
		d.LinkHolds = nemura.LinkService{Store: store, OAuth: oauth, Clock: deps.Clock}
		d.LinkSync = nemura.LinkService{Store: store, OAuth: oauth, Clock: deps.Clock}
	}
	api.New(s, d)
	s.Handle("/", webApp(nemuraweb.Files()))
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

// relyingPartyID is the relying party's ID: the one configured, else the origin's host.
func relyingPartyID(origin, configured string) string {
	if configured != "" {
		return configured
	}
	u, err := url.Parse(origin)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func newPasskeys(store *nemura.Store, origin, rpID string, clock ports.Clock) (api.Passkeys, error) {
	if rpID == "" {
		u, err := url.Parse(origin)
		if err != nil {
			return nil, err
		}
		rpID = u.Hostname()
	}
	if net.ParseIP(rpID) != nil {
		// A passkey belongs to a domain name; opened at an IP address (a test, a bare container) the service runs without them.
		return noPasskeys{}, nil
	}
	rp, err := passkey.NewFor(origin, rpID, "Nemura")
	if err != nil {
		return nil, err
	}
	return &passkeys{store: store, rp: rp, clock: clock}, nil
}

// noPasskeys answers every ceremony with NM_UNAVAILABLE: the origin is an IP address, which WebAuthn does not accept as a relying party.
type noPasskeys struct{}

func (noPasskeys) BeginRegistration(context.Context, api.Session) ([]byte, error) {
	return nil, api.ErrUnavailable
}
func (noPasskeys) CompleteRegistration(context.Context, api.Session, string, []byte) (uuid.UUID, error) {
	return uuid.Nil, api.ErrUnavailable
}
func (noPasskeys) BeginStepUp(context.Context, api.Session) ([]byte, error) {
	return nil, api.ErrUnavailable
}
func (noPasskeys) CompleteStepUp(context.Context, api.Session, []byte) error {
	return api.ErrUnavailable
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

func restricted(store *nemura.Store, deps app.Deps, getenv func(string) string) nemura.RestrictedService {
	cipher, blobs := restrictedWiring(getenv)
	return nemura.RestrictedService{Store: store, Cipher: cipher, Blobs: blobs, Clock: deps.Clock}
}

func packsService(store *nemura.Store, deps app.Deps) api.Packs {
	p, err := nemura.NewPacksService(store, deps.Clock)
	if err != nil {
		panic(err) // a pack that does not validate is a build error: the files are embedded
	}
	return p
}

func leaveService(store *nemura.Store, deps app.Deps) api.Leave {
	p, err := nemura.NewPacksService(store, deps.Clock)
	if err != nil {
		panic(err) // a pack that does not validate is a build error: the files are embedded
	}
	return nemura.LeaveService{Store: store, Clock: deps.Clock, Packs: p}
}

func leaveRequests(store *nemura.Store, deps app.Deps) api.LeaveRequests {
	p, err := nemura.NewPacksService(store, deps.Clock)
	if err != nil {
		panic(err) // a pack that does not validate is a build error: the files are embedded
	}
	return nemura.LeaveRequestService{Store: store, Clock: deps.Clock, Packs: p}
}

func sickService(store *nemura.Store, deps app.Deps, getenv func(string) string) api.Sick {
	p, err := nemura.NewPacksService(store, deps.Clock)
	if err != nil {
		panic(err) // a pack that does not validate is a build error: the files are embedded
	}
	_, blobs := restrictedWiring(getenv)
	return nemura.SickService{Store: store, Clock: deps.Clock, Packs: p, Blobs: blobs}
}

func governmentService(store *nemura.Store, deps app.Deps) api.Government {
	p, err := nemura.NewPacksService(store, deps.Clock)
	if err != nil {
		panic(err) // a pack that does not validate is a build error: the files are embedded
	}
	return nemura.GovernmentService{Store: store, Clock: deps.Clock, Packs: p}
}

func flexService(store *nemura.Store, deps app.Deps) api.Flex {
	p, err := nemura.NewPacksService(store, deps.Clock)
	if err != nil {
		panic(err) // a pack that does not validate is a build error: the files are embedded
	}
	return nemura.FlexService{Store: store, Clock: deps.Clock, Packs: p}
}

func timeService(store *nemura.Store, deps app.Deps) api.Time {
	p, err := nemura.NewPacksService(store, deps.Clock)
	if err != nil {
		panic(err) // a pack that does not validate is a build error: the files are embedded
	}
	return nemura.TimeService{Store: store, Clock: deps.Clock, Packs: p}
}

// letterRenderer is the renderer of letters. The release build has none until the platform's render service is wired (`[live-verify]`): a letter then cannot be checked,
// rather than drawn by a renderer made for the local build, whose pages say so.
var letterRenderer letters.Renderer

func lettersService(store *nemura.Store, deps app.Deps, getenv func(string) string) api.Letters {
	_, blobs := restrictedWiring(getenv)
	p, err := nemura.NewPacksService(store, deps.Clock)
	if err != nil {
		panic(err) // a pack that does not validate is a build error: the files are embedded
	}
	return nemura.LettersService{Packs: p, Store: store, Clock: deps.Clock, Blobs: blobs, Renderer: letterRenderer}
}

func selfService(store *nemura.Store, deps app.Deps, getenv func(string) string) api.SelfService {
	_, blobs := restrictedWiring(getenv)
	return nemura.SelfService{Store: store, Clock: deps.Clock, Blobs: blobs}
}

func onboardingService(store *nemura.Store, deps app.Deps) api.Onboarding {
	p, err := nemura.NewPacksService(store, deps.Clock)
	if err != nil {
		panic(err) // a pack that does not validate is a build error: the files are embedded
	}
	return nemura.OnboardingService{Store: store, Clock: deps.Clock, Packs: p}
}

func payrollService(store *nemura.Store, deps app.Deps, getenv func(string) string) api.Payroll {
	cipher, blobs := restrictedWiring(getenv)
	p, err := nemura.NewPacksService(store, deps.Clock)
	if err != nil {
		panic(err) // a pack that does not validate is a build error: the files are embedded
	}
	return nemura.PayrollService{Store: store, Clock: deps.Clock, Packs: p, Blobs: blobs, Renderer: letterRenderer, Cipher: cipher}
}

// scriptedAllowed lets an Owner choose local mode's scripted model as the assistant's route: the local build sets it, the release build never does.
var scriptedAllowed = false

// askService answers a person's question. Local mode gives it the scripted model; the release build has none until a model route is evidenced in Singapore, and answers that the assistant is not available.
func askService(store *nemura.Store, deps app.Deps) api.Asker {
	p, err := nemura.NewPacksService(store, deps.Clock)
	if err != nil {
		panic(err) // a pack that does not validate is a build error: the files are embedded
	}
	var m assistant.Model
	if scriptedAllowed {
		m = assistant.Scripted{}
	}
	return nemura.AskService{Store: store, Clock: deps.Clock, Packs: p, Model: m}
}
