//go:build local

package nemura_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/apiclient"
	"github.com/cognizhi/fyniq/internal/ports"
)

// fakeOAuth stands in for Nemura's Hydra: it registers and removes clients and remembers them.
type fakeOAuth struct {
	registered []ports.IntegrationRegistration
	removed    []string
	err        error
}

func (f *fakeOAuth) RegisterIntegration(_ context.Context, r ports.IntegrationRegistration) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	f.registered = append(f.registered, r)
	return "hydra-" + uuid.NewString(), nil
}

// RegisterPersonal is a Fyniq developer's token; Nemura has none, so the fake refuses it.
func (f *fakeOAuth) RegisterPersonal(context.Context, ports.PersonalRegistration) (string, string, error) {
	return "", "", errors.New("Nemura registers no personal access tokens")
}
func (f *fakeOAuth) RemoveClient(_ context.Context, id string) error {
	f.removed = append(f.removed, id)
	return nil
}

const jwk = `{"kty":"EC","crv":"P-256","x":"f83OJ3D2xF1Bg8vub9tLe1gHMzV76e8Tus9uPHvRVEU","y":"x_FEzRu9m36HLN_tue659LNpXW6pCyStikYjKIWI5a0"}`

// An API client is registered with Nemura's own OAuth server for the API's audience only, with the key it holds and a token lifetime of an hour
// at most, and its record names who registered it; a repeat of the name is refused before the OAuth server holds a second client; a server that
// fails leaves no record; revoking removes it from the OAuth server first and then marks it; every step is audited and a key never enters the
// record (NM-ARC-06, question 11).
// req: NM-ARC-06
// asvs: V6.4.1, V13.2.1, V16.3.1
func TestAnAPIClientIsRegisteredWithTheOAuthServerAndRecorded(t *testing.T) {
	ctx := context.Background()
	c := newCompany(t)
	oauth := &fakeOAuth{}
	svc := nemura.APIClientService{Store: store(t, "nemura_api"), OAuth: oauth, Clock: clock.NewFrozen(today.Add(9 * time.Hour))}
	s := api.Session{Organisation: c.org, Member: c.owner}

	got, err := svc.Register(ctx, s, "Payroll bureau", []string{"directory.read"}, "ES256", json.RawMessage(jwk))
	if err != nil || got.Audience != "nemura-api" || got.ClientID == "" || len(oauth.registered) != 1 {
		t.Fatalf("registering: %+v, %v", got, err)
	}
	r := oauth.registered[0]
	if r.Audience != apiclient.Audience || r.Class != "integration" || r.Alg != "ES256" || r.TokenLifespan > time.Hour || r.Organisation != c.org.String() || len(r.Key) == 0 {
		t.Errorf("what the OAuth server was asked: %+v", r)
	}
	// The same name twice is refused before the OAuth server holds a second client.
	if _, err := svc.Register(ctx, s, "payroll BUREAU", []string{"directory.read"}, "ES256", json.RawMessage(jwk)); err == nil || !strings.Contains(err.Error(), apiclient.CodeExists) || len(oauth.registered) != 1 {
		t.Errorf("a repeated name: %v, %d clients at the OAuth server", err, len(oauth.registered))
	}
	// An invalid registration reaches nobody.
	if _, err := svc.Register(ctx, s, "Bad", []string{"payroll.write"}, "ES256", json.RawMessage(jwk)); err == nil || len(oauth.registered) != 1 {
		t.Errorf("an invalid scope: %v", err)
	}
	// An OAuth server that fails leaves no record.
	oauth.err = errors.New("the OAuth server is down")
	if _, err := svc.Register(ctx, s, "Second", []string{"directory.read"}, "ES256", json.RawMessage(jwk)); err == nil {
		t.Error("a registration succeeded with the OAuth server down")
	}
	oauth.err = nil
	list, err := svc.List(ctx, s)
	if err != nil || len(list) != 1 || list[0].Name != "Payroll bureau" {
		t.Fatalf("the list: %+v, %v", list, err)
	}
	// No key in the record or the audit trail.
	var leaks int
	if err := connect(t, db.SuperURL()).QueryRow(ctx, `SELECT count(*) FROM nemura.audit_event WHERE organisation_id = $1 AND (body LIKE '%f83OJ3D2%' OR body LIKE '%x_FEzRu9%')`, c.org).Scan(&leaks); err != nil || leaks != 0 {
		t.Errorf("a key in the audit trail: %d, %v", leaks, err)
	}
	// Revoking: the OAuth server first, then the record; another organisation cannot revoke it; a second revocation finds nothing.
	id := uuid.MustParse(list[0].ID)
	other := newCompany(t)
	if err := svc.Revoke(ctx, api.Session{Organisation: other.org, Member: other.owner}, id); !errors.Is(err, api.ErrNoClient) {
		t.Errorf("another organisation revoking: %v", err)
	}
	if err := svc.Revoke(ctx, s, id); err != nil || len(oauth.removed) != 1 || oauth.removed[0] != got.ClientID {
		t.Fatalf("revoking: %v, removed %v", err, oauth.removed)
	}
	if list, _ := svc.List(ctx, s); len(list) != 0 {
		t.Errorf("a revoked client is still listed: %+v", list)
	}
	if err := svc.Revoke(ctx, s, id); !errors.Is(err, api.ErrNoClient) {
		t.Errorf("a second revocation: %v", err)
	}
	// The name is free again, and the record keeps its history: nothing is deleted or edited.
	if _, err := svc.Register(ctx, s, "Payroll bureau", []string{"directory.read"}, "ES256", json.RawMessage(jwk)); err != nil {
		t.Errorf("a revoked client's name is free again: %v", err)
	}
	for _, sql := range []string{"DELETE FROM nemura.api_client", "UPDATE nemura.api_client SET scopes = '{x}'"} {
		if _, err := connect(t, db.SuperURL()).Exec(ctx, sql); err == nil || !strings.Contains(err.Error(), "NM104") {
			t.Errorf("%s: %v", sql, err)
		}
	}
	var events int
	if err := connect(t, db.SuperURL()).QueryRow(ctx, "SELECT count(*) FROM nemura.audit_event WHERE organisation_id = $1 AND action IN ('nemura.client.created', 'nemura.client.revoked')", c.org).Scan(&events); err != nil || events != 3 {
		t.Errorf("audit events: %d, %v", events, err)
	}
	if _, err := store(t, "nemura_worker").VerifyAudit(ctx, c.org); err != nil {
		t.Errorf("the chain: %v", err)
	}
}

// With no OAuth server configured, registering and revoking say so; listing still works.
func TestAnAPIClientNeedsAnOAuthServer(t *testing.T) {
	c := newCompany(t)
	svc := nemura.APIClientService{Store: store(t, "nemura_api"), Clock: clock.NewFrozen(today)}
	s := api.Session{Organisation: c.org, Member: c.owner}
	if _, err := svc.Register(context.Background(), s, "x", []string{"directory.read"}, "ES256", json.RawMessage(jwk)); !errors.Is(err, api.ErrUnavailable) {
		t.Errorf("registering with no OAuth server: %v", err)
	}
	if err := svc.Revoke(context.Background(), s, uuid.New()); !errors.Is(err, api.ErrUnavailable) {
		t.Errorf("revoking with no OAuth server: %v", err)
	}
}
