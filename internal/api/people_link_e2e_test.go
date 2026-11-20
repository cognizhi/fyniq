//go:build local

package api

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/hydra"
	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura/nemuratest"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/linksend"
	napi "github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/apiclient"
	nauthz "github.com/cognizhi/fyniq/internal/nemura/authz"
	"github.com/cognizhi/fyniq/internal/nemura/identity"
	"github.com/cognizhi/fyniq/internal/nemura/linkauth"
	nout "github.com/cognizhi/fyniq/internal/nemura/outbox"
	"github.com/cognizhi/fyniq/internal/nemura/people"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// noNemuraSessions is Nemura's session store in this test: nobody is signed in with a cookie, only the link's client calls.
type noNemuraSessions struct{}

func (noNemuraSessions) Authenticate(context.Context, string, time.Time) (napi.Session, error) {
	return napi.Session{}, io.EOF
}
func (noNemuraSessions) RolesAt(context.Context, uuid.UUID, uuid.UUID, time.Time) ([]nauthz.Role, error) {
	return nil, io.EOF
}

// tamper is a transport that changes what it carries: the body of a request to the host it is asked to, once, as a man in the middle would.
type tamper struct {
	base     http.RoundTripper
	only     string // a path suffix: only calls to it are changed
	from, to string
	changed  int
	sent     map[string][]byte // the last event body sent to each path, as it left (before any change)
}

func (t *tamper) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Body != nil && strings.HasPrefix(r.URL.Path, "/v1/") {
		b, _ := io.ReadAll(r.Body)
		if t.sent == nil {
			t.sent = map[string][]byte{}
		}
		t.sent[r.URL.Path] = bytes.Clone(b)
		if strings.HasSuffix(r.URL.Path, t.only) && t.from != "" && bytes.Contains(b, []byte(t.from)) {
			b = bytes.Replace(b, []byte(t.from), []byte(t.to), 1)
			t.changed++
		}
		r = r.Clone(r.Context())
		r.Body, r.ContentLength = io.NopCloser(bytes.NewReader(b)), int64(len(b))
	}
	return t.base.RoundTrip(r)
}

// The two apps carry each other's events end to end, in one process, through one OAuth server (FR-PPL-04, 15, 16; PRD-002 NM-ARC-04, NM-INT-03, 10, 11): the link is made in the three legs of its handshake by
// two people on each side (Nemura proposes, Fyniq links, Nemura approves), each app signs with a key of its own and gets its token with another, and then a hold travels from Nemura to Fyniq's projection and a
// run's posting answer from Fyniq to Nemura's run, each applied once, a repeat of an event does nothing, a changed envelope is refused on the way in, an event outside the replay window is refused, and a token
// made for the other app's API reaches neither inbox (no passthrough).
// part of NM-INT-10 (both apps' transport, inbox and replay window; gap repair and the other event types are not built)
// asvs: V8.2.1, V10.3.1, V11.2.1, V13.2.1, V4.2.1
func TestTheTwoAppsLinkAndCarryEventsBothWaysThroughOneOAuthServer(t *testing.T) {
	ctx := context.Background()
	s := startOAuth(t, 0)
	e := s.e
	e.api.deps.OAuthClients, e.api.deps.APIAudience = s.admin, apiAudience
	f := foundersIn(e)
	admin, owner := f.as(f.ahkow, authz.Owner, authz.Administrator), f.as(f.weiling, authz.Owner)
	fyniqNow, nemuraNow := clock.NewFrozen(clock.System{}.Now()), clock.NewFrozen(clock.System{}.Now())

	// Fyniq's API as an integration reaches it: Hydra's tokens introspected at every call, the receiver's clock its own.
	fmux := http.NewServeMux()
	fsrv := httptest.NewServer(fmux)
	t.Cleanup(fsrv.Close)
	New(fmux, Deps{Store: e.store, Clock: fyniqNow, Auth: authn.Local{Dev: e.tokens, OAuth: &authn.Introspector{OAuth: s.admin, Audience: apiAudience, Clock: fyniqNow, Clients: e.store}},
		Log: slog.New(slog.DiscardHandler), Version: "0.0.0-test", OAuthClients: s.admin, APIAudience: apiAudience})

	// Nemura: its own database, its API and its OAuth audience; an Owner, a second approver and a person with a hold to come.
	ndb, err := nemuratest.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ndb.Stop)
	nst, err := nemura.Open(ctx, ndb.URL("nemura_api"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nst.Close)
	nwk, err := nemura.Open(ctx, ndb.URL("nemura_worker"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nwk.Close)
	norg := uuid.New()
	real := clock.System{}.Now()
	day := time.Date(real.Year(), real.Month(), real.Day(), 0, 0, 0, 0, time.UTC)
	system := audit.Actor{Type: "service", ID: "test"}
	var nowner, nsecond, nperson uuid.UUID
	if err := nst.InOrg(ctx, norg, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "INSERT INTO nemura.organisation (name) VALUES ($1)", "Nemura end to end Pte Ltd"); err != nil {
			return err
		}
		if err := nemura.Audit(ctx, tx, norg, real, system, "nemura.org.created", audit.Object{Type: "organisation", ID: norg}, map[string]audit.Change{"name": {nil, "e2e"}}, "set up"); err != nil {
			return err
		}
		var err error
		if nowner, err = nemura.AddMember(ctx, tx, norg, real, system, "Owen Owner"); err != nil {
			return err
		}
		if nsecond, err = nemura.AddMember(ctx, tx, norg, real, system, "Sam Second"); err != nil {
			return err
		}
		if err := nemura.SetupOwner(ctx, tx, norg, real, nowner, day); err != nil {
			return err
		}
		if _, err := nemura.GrantRole(ctx, tx, norg, real, nowner, nsecond, nauthz.Administrator, day, nil); err != nil {
			return err
		}
		nperson, err = nemura.AddPerson(ctx, tx, norg, real, nowner, people.Person{LegalName: "Ben Lim", EmploymentStart: day.AddDate(0, -6, 0)},
			people.Job{Title: "Engineer", Department: "Engineering", Worksite: "SG", EmploymentType: "full_time"}, &people.Classification{ContractType: "service", EACovered: true, PartIVClass: "none", Residency: "citizen"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	nmux := http.NewServeMux()
	nsrv := httptest.NewServer(nmux)
	t.Cleanup(nsrv.Close)
	napi.New(nmux, napi.Deps{LinkAuth: &linkauth.Introspector{OAuth: s.admin, Routes: nst, Audience: apiclient.Audience, Clock: nemuraNow}, Sessions: noNemuraSessions{}, Clock: nemuraNow,
		Link: nemura.LinkService{Store: nst, OAuth: s.admin, Clock: nemuraNow}, LinkHolds: nemura.LinkService{Store: nst, OAuth: s.admin, Clock: nemuraNow}, LinkSync: nemura.LinkService{Store: nst, OAuth: s.admin, Clock: nemuraNow}, CookieNames: []string{"nemura_session"}})

	// The handshake. Leg 1: an Owner proposes, and Nemura makes its keys and the ID Fyniq's client will have at its OAuth server.
	svc := nemura.LinkService{Store: nst, OAuth: s.admin, Clock: clock.System{}}
	fyniqOrg := e.books.Org
	nowT := clock.System{}.Now()
	offer, err := svc.Propose(ctx, napi.Session{Organisation: norg, Member: nowner, StepUpAt: &nowT}, fyniqOrg)
	if err != nil {
		t.Fatal(err)
	}
	// Leg 2: Fyniq links with Nemura's half, dual controlled; it makes its keys and registers Nemura's client.
	var clientJWK map[string]any
	_ = json.Unmarshal(offer.ClientKey, &clientJWK)
	asked := e.call(http.MethodPost, "/v1/people-link", admin, map[string]any{"nemura_organisation_id": norg.String(), "nemura_origin": nemuraOrigin, "nemura_client_id": offer.PeerClientID, "nemura_ask_client_id": offer.PeerAskClientID,
		"link_key": base64.RawURLEncoding.EncodeToString(offer.LinkKey), "client_name": "Nemura link", "client_public_key": clientJWK,
		"expires_at": e.clock.Now().AddDate(0, 6, 0).UTC().Format(time.RFC3339), "features": []string{"records", "payroll"}})
	if asked.Status != http.StatusAccepted {
		t.Fatalf("asking to link: %d %s", asked.Status, asked.Raw)
	}
	if d := f.decide(owner, asked.str("id"), "approve", hashOf(asked)); d.Status != http.StatusOK || d.str("status") != "approved" {
		t.Fatalf("approving the link: %d %s", d.Status, d.Raw)
	}
	bundle := e.call(http.MethodGet, "/v1/people-link", admin, nil)
	if bundle.str("state") != "active" || bundle.str("oauth_client_id") == "" || bundle.str("fyniq_link_key") == "" {
		t.Fatalf("Fyniq's half: %s", bundle.Raw)
	}
	// Leg 3: another Owner or Administrator approves Nemura's side with Fyniq's half; Fyniq's client is registered at Nemura's OAuth server under the ID chosen in leg 1.
	fyniqKey, _ := base64.RawURLEncoding.DecodeString(bundle.str("fyniq_link_key"))
	fyniqClientKey, _ := json.Marshal(bundle.Body["fyniq_client_public_key"])
	fyniqAskKey, _ := json.Marshal(bundle.Body["fyniq_ask_client_public_key"])
	nowT = clock.System{}.Now()
	if err := svc.Activate(ctx, napi.Session{Organisation: norg, Member: nsecond, StepUpAt: &nowT}, ed25519.PublicKey(fyniqKey), bundle.str("oauth_client_id"), fyniqClientKey, fyniqAskKey); err != nil {
		t.Fatalf("approving Nemura's side: %v", err)
	}

	// Each app's sender, with the other's endpoints; the transports reach the OAuth server's public port by its issuer's name.
	tokenURL := "http://hydra.test/oauth2/token"
	nemuraHTTP, fyniqHTTP := &tamper{base: s.browser().Transport}, &tamper{base: s.browser().Transport}
	nsender := &nemura.LinkSender{Store: nwk, Clock: clock.System{}, HTTP: &http.Client{Transport: nemuraHTTP}, APIURL: fsrv.URL, TokenURL: tokenURL, Audience: apiAudience}
	fsender := &linksend.Sender{Store: workerStore(t), Clock: clock.System{}, HTTP: &http.Client{Transport: fyniqHTTP}, APIURL: nsrv.URL, TokenURL: tokenURL, Audience: apiclient.Audience}

	// Nemura to Fyniq: a hold is put on in Nemura, queued with the change, sent, and applied in Fyniq's projection.
	if err := nst.InOrg(ctx, norg, func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.PutTaxHold(ctx, tx, norg, real, nowner, nperson, day, "IR21 filed")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	rowOf := func(kind string) (id uuid.UUID, payload []byte) {
		t.Helper()
		if err := nst.InOrg(ctx, norg, func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT id, payload FROM nemura.outbox WHERE kind = $1 ORDER BY created_at DESC LIMIT 1`, kind).Scan(&id, &payload)
		}); err != nil {
			t.Fatal(err)
		}
		return id, payload
	}
	holdRow, holdPayload := rowOf(nemura.KindLinkSend)
	if strings.Contains(string(holdPayload), "IR21") {
		t.Errorf("the reason of the hold is in the outbox row: %s", holdPayload)
	}
	deliver := func(row uuid.UUID, payload []byte) error {
		return nsender.Work()(ctx, norg, outboxRow(row, payload))
	}
	if err := deliver(holdRow, holdPayload); err != nil {
		t.Fatalf("the hold's delivery: %v", err)
	}
	var held bool
	projection := func() {
		t.Helper()
		if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			p, err := u.PeopleLinks().Person(ctx, nperson)
			held = p.Known() && p.Held
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	projection()
	if !held {
		t.Error("the hold did not reach Fyniq's projection")
	}
	// The person who existed before the link went ahead with it (a snapshot at activation): Ben reaches Fyniq's projection with the figures accounting needs, and a later change is the next version.
	var ahead []queuedRow
	if err := nst.InOrg(ctx, norg, func(ctx context.Context, tx pgx.Tx) error {
		rs, err := tx.Query(ctx, `SELECT id, payload FROM nemura.outbox WHERE kind = $1 AND payload->>'type' = 'people.person.upserted' ORDER BY created_at`, nemura.KindLinkSend)
		if err != nil {
			return err
		}
		defer rs.Close()
		for rs.Next() {
			var o queuedRow
			if err := rs.Scan(&o.id, &o.payload); err != nil {
				return err
			}
			ahead = append(ahead, o)
		}
		return rs.Err()
	}); err != nil || len(ahead) != 1 {
		t.Fatalf("the person queued with the link: %d %v", len(ahead), err)
	}
	if err := deliver(ahead[0].id, ahead[0].payload); err != nil {
		t.Fatalf("the person's delivery: %v", err)
	}
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		p, err := u.PeopleLinks().Person(ctx, nperson)
		if err != nil {
			return err
		}
		if !p.Known() || p.DisplayName != "Ben Lim" || p.Status != "active" || p.Version != 1 || p.Removed || p.BankVersion != 0 {
			t.Errorf("Ben in Fyniq's projection: %+v", p)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// The same event again is a duplicate in Fyniq: the delivery succeeds and nothing is applied twice.
	if err := deliver(holdRow, holdPayload); err != nil {
		t.Errorf("a retry of the same event: %v", err)
	}
	// A changed envelope is refused on the way in and changes nothing: release the hold in Nemura, send it through a transport that changes "released" to "held".
	nemuraHTTP.only, nemuraHTTP.from, nemuraHTTP.to = "/hold-events", `"held"`, `"released"`
	// The first event's ID was used, so a new event of the same hold is queued and sent through a transport that changes its status after it was signed.
	queueHold := func() {
		t.Helper()
		if err := nst.InOrg(ctx, norg, func(ctx context.Context, tx pgx.Tx) error {
			_, err := nemura.QueueLinkEvent(ctx, tx, norg, real, "people.tax_hold.changed", "tax_hold", nperson.String(), 1)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	queueHold()
	row2, payload2 := rowOf(nemura.KindLinkSend)
	if err := deliver(row2, payload2); err == nil || !strings.Contains(err.Error(), "PPL-03") || nemuraHTTP.changed != 1 {
		t.Errorf("a changed envelope was not refused with its digest code: %v (changed %d)", err, nemuraHTTP.changed)
	}
	nemuraHTTP.from = ""
	projection()
	if !held {
		t.Error("a changed envelope changed the projection")
	}

	var first, second string
	fyniqRow := func(event string) (row storage.OutboxRow) {
		t.Helper()
		if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			r, err := u.Outbox().ByKey(ctx, linksend.Kind, "link:"+event)
			if r != nil {
				row = *r
			}
			return err
		}); err != nil || row.ID == uuid.Nil {
			t.Fatalf("the queued answer %s: %v", event, err)
		}
		return row
	}
	// Fyniq to Nemura: the answer to a pay run is queued in a unit of work of Fyniq's, sent, and kept on the run in Nemura.
	run := uuid.New()
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		id, err := linksend.Queue(ctx, u, linksend.TypeRunPosted, "payroll_run", run.String(), 1, linksend.RunPosted{RunID: run.String(), Version: 1, Status: "posted", PostingRef: uuid.NewString()})
		if err == nil && id == "" {
			t.Error("nothing was queued for a linked organisation")
		}
		first = id
		return err
	}); err != nil {
		t.Fatal(err)
	}
	frow := fyniqRow(first)
	if err := fsender.Work()(ctx, e.books.Org, frow); err != nil {
		t.Fatalf("the answer's delivery: %v", err)
	}
	postings := func() (n int) {
		if err := nst.InOrg(ctx, norg, func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM nemura.link_run_posting WHERE run_id = $1 AND status = 'posted'`, run).Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if postings() != 1 {
		t.Fatalf("the posting answer reached Nemura %d times", postings())
	}
	if err := fsender.Work()(ctx, e.books.Org, frow); err != nil || postings() != 1 {
		t.Errorf("a retry of the same answer: %v, %d postings", err, postings())
	}
	// A changed envelope on this leg too, and an event outside the replay window.
	fyniqHTTP.only, fyniqHTTP.from, fyniqHTTP.to = "/v1/link/events", `"posted"`, `"refused"`
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		second, err = linksend.Queue(ctx, u, linksend.TypeRunPosted, "payroll_run", run.String(), 2, linksend.RunPosted{RunID: run.String(), Version: 2, Status: "posted", PostingRef: uuid.NewString()})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	next := fyniqRow(second)
	if err := fsender.Work()(ctx, e.books.Org, next); err == nil || !strings.Contains(err.Error(), "422") || fyniqHTTP.changed != 1 {
		t.Errorf("a changed answer was not refused: %v (changed %d)", err, fyniqHTTP.changed)
	}
	fyniqHTTP.from = ""
	nemuraNow.Set(clock.System{}.Now().Add(10 * time.Minute)) // Nemura's clock is ten minutes ahead of the signer's: outside the five-minute window
	if err := fsender.Work()(ctx, e.books.Org, next); err == nil || !strings.Contains(err.Error(), "422") {
		t.Errorf("an event outside the replay window: %v", err)
	}
	nemuraNow.Set(clock.System{}.Now())
	if err := fsender.Work()(ctx, e.books.Org, next); err != nil || postings() != 2 {
		t.Errorf("the second answer, inside the window: %v, %d postings", err, postings())
	}
	fyniqNow.Set(clock.System{}.Now().Add(-10 * time.Minute))
	queueHold()
	row3, payload3 := rowOf(nemura.KindLinkSend)
	if err := deliver(row3, payload3); err == nil || !strings.Contains(err.Error(), "PPL-04") {
		t.Errorf("a hold outside Fyniq's replay window: %v", err)
	}
	fyniqNow.Set(clock.System{}.Now())

	// The ask at release: Fyniq's API (not the worker) asks Nemura, with the read-only client and its own key, whether the person is held; Nemura answers with the hold's latest version.
	asker := &linksend.HoldAsker{HTTP: &http.Client{Transport: s.browser().Transport}, APIURL: nsrv.URL, TokenURL: tokenURL, Audience: apiclient.Audience, Clock: clock.System{}}
	var askKey storage.PeopleLinkAskKey
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		l, err := u.PeopleLinks().Live(ctx)
		if err != nil {
			return err
		}
		askKey, err = u.PeopleLinks().AskKey(ctx, l.ID)
		return err
	}); err != nil || askKey.ClientID != offer.PeerAskClientID {
		t.Fatalf("the API's ask key: %+v %v", askKey, err)
	}
	if ans, err := asker.Ask(ctx, askKey, nperson); err != nil || ans.Status != "held" || ans.Version != 1 || ans.HoldID == "" {
		t.Errorf("the ask for a held person: %+v %v", ans, err)
	}
	if ans, err := asker.Ask(ctx, askKey, uuid.New()); err != nil || ans.Status != "none" || ans.Version != 0 {
		t.Errorf("the ask for a person with no hold: %+v %v", ans, err)
	}
	if err := workerStore(t).Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		l, err := u.PeopleLinks().Live(ctx)
		if err != nil {
			return err
		}
		_, err = u.PeopleLinks().AskKey(ctx, l.ID)
		return err
	}); err == nil {
		t.Error("the worker read the ask key")
	}
	// The ask's client cannot send an event, and the events' client cannot ask.
	badAsk := &linksend.HoldAsker{HTTP: &http.Client{Transport: s.browser().Transport}, APIURL: nsrv.URL, TokenURL: tokenURL, Audience: apiclient.Audience, Clock: clock.System{}}
	if _, err := badAsk.Ask(ctx, storage.PeopleLinkAskKey{LinkID: askKey.LinkID, ClientID: fyniqKeys0(t, e, workerStore(t)), PrivateDER: askKey.PrivateDER}, nperson); err == nil {
		t.Error("a client that is not the asking client asked")
	}

	// No passthrough: a token made for one app's API reaches neither inbox. Each app's own client gets a token for the other's audience (which its record allows only for the other's API).
	var nsec nemura.LinkSecrets
	var nlink nemura.LinkRow
	if err := nwk.InOrg(ctx, norg, func(ctx context.Context, tx pgx.Tx) (err error) {
		if nlink, err = nemura.LiveLink(ctx, tx); err != nil {
			return err
		}
		nsec, err = nemura.SecretsOf(ctx, tx, nlink.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	toFyniq := &hydra.TokenSource{TokenURL: tokenURL, ClientID: nlink.FyniqClientID, Key: nsec.Client, Audience: apiAudience, HTTP: &http.Client{Transport: s.browser().Transport}, Clock: clock.System{}}
	fyniqToken, err := toFyniq.Token(ctx, "people.holds.write")
	if err != nil {
		t.Fatal(err)
	}
	post := func(url, token string, body []byte) int {
		req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}
	if status := post(nsrv.URL+"/v1/link/events", fyniqToken, fyniqHTTP.sent["/v1/link/events"]); status != http.StatusUnauthorized {
		t.Errorf("Fyniq's token at Nemura's inbox: %d", status)
	}
	var fyniqSec storage.PeopleLinkSecrets
	var fyniqKeys storage.PeopleLinkKeys
	if err := workerStore(t).Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		l, err := u.PeopleLinks().Live(ctx)
		if err != nil {
			return err
		}
		if fyniqSec, err = u.PeopleLinks().Secrets(ctx, l.ID); err != nil {
			return err
		}
		fyniqKeys, err = u.PeopleLinks().Keys(ctx, l.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	der, err := x509.ParsePKCS8PrivateKey(fyniqSec.ClientPrivate)
	if err != nil {
		t.Fatal(err)
	}
	toNemura := &hydra.TokenSource{TokenURL: tokenURL, ClientID: fyniqKeys.NemuraClientID, Key: der.(*ecdsa.PrivateKey), Audience: apiclient.Audience, HTTP: &http.Client{Transport: s.browser().Transport}, Clock: clock.System{}}
	nemuraToken, err := toNemura.Token(ctx, linksend.Scope)
	if err != nil {
		t.Fatal(err)
	}
	if status := post(fsrv.URL+"/v1/people-link/hold-events", nemuraToken, nemuraHTTP.sent["/v1/people-link/hold-events"]); status != http.StatusUnauthorized {
		t.Errorf("Nemura's token at Fyniq's inbox: %d", status)
	}
	// Each app's own tokens do work at its own door, so the two refusals above are about the audience and nothing else.
	if status := post(nsrv.URL+"/v1/link/events", nemuraToken, fyniqHTTP.sent["/v1/link/events"]); status != http.StatusOK {
		t.Errorf("Nemura's token for Fyniq's client at its own inbox, with Fyniq's last event again: %d (a duplicate answers 200)", status)
	}

	// One sign-in across the apps (design D7): each app tells the other what a sign-in identity may enter, signed, through the real transport and inboxes. Owen Owner signs in to both with one identity.
	idp, owen := "https://idp.example.test", "owen"
	if err := nst.InOrg(ctx, norg, func(ctx context.Context, tx pgx.Tx) error {
		return nemura.LinkIdentity(ctx, tx, norg, real, system, nowner, identity.Identity{Issuer: idp, Subject: owen, Tenant: "t"})
	}); err != nil {
		t.Fatal(err)
	}
	accessRow, accessPayload := rowOf(nemura.KindLinkSend)
	if !strings.Contains(string(accessPayload), nemura.TypeIdentityAccess) {
		t.Fatalf("linking Owen's identity queued: %s", accessPayload)
	}
	if err := deliver(accessRow, accessPayload); err != nil {
		t.Fatalf("Nemura's word on Owen: %v", err)
	}
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		a, err := u.PeopleLinks().Access(ctx, idp, owen)
		if err != nil || !a.PeopleAccess || !a.PayrollApprover {
			t.Errorf("what Fyniq keeps of Owen: %+v %v", a, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var told string
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		told, err = linksend.Queue(ctx, u, linksend.TypeIdentityAccess, "identity", uuid.NewString(), 0, linksend.IdentityAccess{Issuer: idp, Subject: owen, Finance: true, Passkey: false})
		return err
	}); err != nil || told == "" {
		t.Fatalf("Fyniq queueing its word on Owen: %q %v", told, err)
	}
	if err := fsender.Work()(ctx, e.books.Org, fyniqRow(told)); err != nil {
		t.Fatalf("Fyniq's word on Owen: %v", err)
	}
	ws := nemura.WorkspaceService{Store: nst, FinanceOrigin: "https://app.example.test"}
	if u, err := ws.FinanceURL(ctx, napi.Session{Organisation: norg, Member: nowner, Issuer: idp, Subject: owen, Provider: "google"}, nil); err != nil || u != "https://app.example.test/?switch=1&provider=google" {
		t.Errorf("Owen's switch in Nemura after Fyniq said he holds a finance role: %q %v", u, err)
	}
	if u, _ := ws.FinanceURL(ctx, napi.Session{Organisation: norg, Member: nsecond, Issuer: idp, Subject: "someone-else", Provider: "google"}, nil); u != "" {
		t.Errorf("a switch for an identity Fyniq said nothing of: %q", u)
	}
	rl := nemura.RunLinkService{Store: nst, Clock: clock.System{}}
	if env, _ := rl.Envelope(ctx, napi.Session{Organisation: norg, Member: nowner, Issuer: idp, Subject: owen}, uuid.New(), 1); env.FyniqPasskey != "missing" { // (there is no such run: the hint is read before it)
		t.Errorf("Fyniq said Owen has no passkey enrolled: %q", env.FyniqPasskey)
	}

	// Nemura tells Fyniq Owen signed out, through the scope identity.sessions.write the link's client was registered with: Fyniq has no user of that identity, so it answers and ends nothing.
	var tok string
	if err := nst.InOrg(ctx, norg, func(ctx context.Context, tx pgx.Tx) (err error) {
		tok, err = nemura.StartSession(ctx, tx, norg, real, nowner, identity.Via{Provider: "zoho", Issuer: idp, Subject: owen})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := nst.CloseSession(ctx, tok, real); err != nil {
		t.Fatal(err)
	}
	signoutRow, signoutPayload := rowOf(nemura.KindLinkSend)
	if !strings.Contains(string(signoutPayload), nemura.TypeSessionRevoked) {
		t.Fatalf("signing out queued: %s", signoutPayload)
	}
	if err := deliver(signoutRow, signoutPayload); err != nil {
		t.Fatalf("Nemura's sign-out reaching Fyniq's door for sessions: %v", err)
	}
	// Fyniq's read-only client reads Nemura's log by identity with its other scope; the client that sends events cannot get a token for it.
	page, err := asker.Sync(ctx, askKey, "")
	if err != nil || len(page.Events) == 0 || page.HighestSequence < int64(len(page.Events)) {
		t.Fatalf("the link's event log by identity: %+v %v", page, err)
	}
	for _, ev := range page.Events {
		if ev.EventID == "" || ev.Type == "" || ev.State == "" {
			t.Errorf("an event of the log: %+v", ev)
		}
	}
	if _, err := toNemura.Token(ctx, apiclient.SyncScope); err == nil {
		t.Error("the client that sends events got a token for the log")
	}
	if _, err := toFyniq.Token(ctx, "link.sync.read"); err == nil {
		t.Error("Nemura's client got a token for a Nemura scope")
	}
}

// outboxRow is a row of Nemura's outbox as the worker's handler receives it.
type queuedRow struct {
	id      uuid.UUID
	payload []byte
}

func outboxRow(id uuid.UUID, payload []byte) nout.Row { return nout.Row{ID: id, Payload: payload} }

// fyniqKeys0 is the ID of the events' client at Nemura, read with the worker's role: what a client that is not the asking one would sign in as.
func fyniqKeys0(t *testing.T, e *env, worker *postgres.Store) string {
	t.Helper()
	var id string
	if err := worker.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		l, err := u.PeopleLinks().Live(ctx)
		if err != nil {
			return err
		}
		k, err := u.PeopleLinks().Keys(ctx, l.ID)
		id = k.NemuraClientID
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return id
}
