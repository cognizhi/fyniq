//go:build local

package postgres_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The database keeps an API client's rules whoever writes (golden rule 2; FR-INT-02, SEC-07, AI-34): it is registered
// live at its first version, with an owner who is a member, narrow scopes, networks as CIDR prefixes, its key's thumbprint
// and its approval for an integration, and an expiry within a year (90 days for a personal token); then it is only
// revoked, once, or (a personal token) reclassed while live, and never removed or rewritten. An integration client is
// always of class integration. Its route finds it, and its member's roles now, before any tenant is set; another
// organisation sees none of it; the worker and the AI write none of it.
// req: FR-INT-02
// req: SEC-07
// req: AI-34
// asvs: V8.2.2, V10.4.11
// thumbprint stands for an RFC 7638 key thumbprint: 43 base64url characters.
var thumbprint = strings.Repeat("t", 43)

func TestAttackAnAPIClientKeepsItsRules(t *testing.T) {
	ctx := context.Background()
	a, b := books(t, "Clients A Pte Ltd"), books(t, "Clients B Pte Ltd")
	api := store(t, "fyniq_api")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	owner := founder(t, api, a.Org, 71, now)
	approval := uuid.Must(uuid.NewV7())
	c := storage.APIClient{Kind: storage.APIClientIntegration, ClientID: "hydra-" + uuid.NewString(), Name: "Canary", Class: "integration", OwnerID: owner,
		Scopes: []string{"fyniq.read.accounting", "fyniq.write.sales"}, IPAllow: []string{"203.0.113.0/24"}, WritesPerMinute: 10,
		KeyThumbprint: thumbprint, ExpiresAt: now.AddDate(0, 6, 0), ApprovalID: &approval, CreatedBy: "user:" + owner.String(),
		CreatedAt: now}
	create := func(c storage.APIClient) (out storage.APIClient, err error) {
		err = api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			out, err = u.APIClients().Create(ctx, c)
			return err
		})
		return out, err
	}
	c, err := create(c)
	if err != nil || c.Version != 1 || c.RevokedAt != nil || c.ReadsPerMinute != 0 || c.WritesPerMinute != 10 {
		t.Fatalf("registering: %+v, %v", c, err)
	}
	acc, err := api.APIClientAccess(ctx, c.ClientID, now)
	if err != nil || acc.Organisation != a.Org || acc.ID != c.ID || !acc.OwnerActive || len(acc.Roles) != 1 || acc.Roles[0] != "owner" ||
		acc.Revoked || acc.WritesPerMinute != 10 || acc.ReadsPerMinute != 0 || len(acc.IPAllow) != 1 {
		t.Fatalf("its route: %+v, %v", acc, err)
	}
	if _, err := api.APIClientAccess(ctx, "not-ours", now); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("a client Fyniq does not know: %v", err)
	}
	for name, bad := range map[string]func(x *storage.APIClient){
		"an integration of class agent":        func(x *storage.APIClient) { x.Class = "agent" },
		"a wildcard scope":                     func(x *storage.APIClient) { x.Scopes = []string{"fyniq.write.*"} },
		"an approve scope":                     func(x *storage.APIClient) { x.Scopes = []string{"fyniq.approve.sales"} },
		"no scope":                             func(x *storage.APIClient) { x.Scopes = []string{} },
		"an expiry beyond a year":              func(x *storage.APIClient) { x.ExpiresAt = now.AddDate(1, 1, 0) },
		"a personal token beyond 90 days":      func(x *storage.APIClient) { x.Kind, x.Class, x.ExpiresAt = "personal", "agent", now.AddDate(0, 4, 0) },
		"an integration without its key":       func(x *storage.APIClient) { x.KeyThumbprint = "" },
		"an integration without its approval":  func(x *storage.APIClient) { x.ApprovalID = nil },
		"a limit above the platform's":         func(x *storage.APIClient) { x.ReadsPerMinute = 601 },
		"an owner who is not a member":         func(x *storage.APIClient) { x.OwnerID = uuid.New() },
		"the client ID of a registered client": func(x *storage.APIClient) {},
		"an expiry before its registration":    func(x *storage.APIClient) { x.ExpiresAt = now.Add(-time.Minute) },
	} {
		x := c
		x.ID, x.ClientID, x.Version = uuid.Nil, "hydra-"+uuid.NewString(), 0
		if name == "the client ID of a registered client" {
			x.ClientID = c.ClientID
		}
		bad(&x)
		if _, err := create(x); err == nil {
			t.Errorf("%s: registered", name)
		}
	}
	revoke := func(version int) (out storage.APIClient, err error) {
		err = api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			out, err = u.APIClients().Revoke(ctx, c.ID, version, "user:"+owner.String(), "The integration was retired.", now)
			return err
		})
		return out, err
	}
	if _, err := revoke(7); !storageCode(err, storage.CodeVersion) {
		t.Errorf("a revocation of an old version: %v", err)
	}
	r, err := revoke(1)
	if err != nil || r.Version != 2 || r.RevokedAt == nil || r.RevokeReason == "" {
		t.Fatalf("revoking: %+v, %v", r, err)
	}
	if _, err := revoke(2); !storageCode(err, storage.CodeAPIClient) {
		t.Errorf("revoking twice: %v", err)
	}
	if acc, err := api.APIClientAccess(ctx, c.ClientID, now); err != nil || !acc.Revoked {
		t.Errorf("a revoked client's route: %+v, %v", acc, err)
	}
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		all, err := u.APIClients().List(ctx, storage.APIClientIntegration)
		if err == nil && len(all) != 0 {
			t.Errorf("another organisation sees %d clients", len(all))
		}
		_, err = u.APIClients().Get(ctx, c.ID)
		if !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("another organisation reads the client: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, x := range []struct {
		what, role, stmt string
		states           []string
	}{
		{"the owner removing it", "fyniq_migrator", `DELETE FROM fyniq.api_client`, []string{"FY420"}},
		{"the owner widening its scopes", "fyniq_migrator", `UPDATE fyniq.api_client SET scopes = '{fyniq.write.accounting}', version = version + 1`, []string{"FY420"}},
		{"the owner bringing it back", "fyniq_migrator", `UPDATE fyniq.api_client SET revoked_at = NULL, revoked_by = NULL, revoke_reason = NULL,
		  version = version + 1`, []string{"FY420"}},
		{"the owner emptying them", "fyniq_migrator", `TRUNCATE fyniq.api_client`, []string{"FY060", "FY061", "0A000"}},
		{"the API widening its scopes", "fyniq_api", `UPDATE fyniq.api_client SET scopes = '{fyniq.write.accounting}'`, []string{"42501"}},
		{"the API moving its expiry", "fyniq_api", `UPDATE fyniq.api_client SET expires_at = expires_at + interval '1 day'`, []string{"42501"}},
		{"the worker revoking one", "fyniq_worker", `UPDATE fyniq.api_client SET revoked_at = now()`, []string{"42501"}},
		{"the AI reading them", "fyniq_ai", `SELECT count(*) FROM fyniq.api_client`, []string{"42501"}},
		{"the API reading the routes", "fyniq_api", `SELECT count(*) FROM fyniq.api_client_route`, []string{"42501"}},
		{"the worker reading a client's access", "fyniq_worker", `SELECT * FROM fyniq.api_client_access('x', now())`, []string{"42501"}},
	} {
		err := asOrg(ctx, conn(t, x.role), a.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, x.stmt); return err })
		wantState(t, x.what, err, x.states...)
	}
	// A personal token is an agent until reclassed; it is reclassed only while live, and never as anything but the two
	// classes; its expiry is within 90 days.
	p := storage.APIClient{Kind: storage.APIClientPersonal, ClientID: "hydra-" + uuid.NewString(), Name: "Laptop", Class: "agent", OwnerID: owner,
		Scopes: []string{"fyniq.read.reports"}, ExpiresAt: now.AddDate(0, 0, 90), CreatedBy: "user:" + owner.String(), CreatedAt: now}
	if p, err = create(p); err != nil {
		t.Fatalf("a personal token: %v", err)
	}
	for _, x := range []struct {
		what, stmt string
		states     []string
	}{
		{"reclassing it as a user", `UPDATE fyniq.api_client SET class = 'user', version = version + 1 WHERE id = $1`, []string{"23514"}},
		{"an update that neither revokes nor reclasses", `UPDATE fyniq.api_client SET version = version + 1 WHERE id = $1`, []string{"FY420"}},
		{"revoking and reclassing at once", `UPDATE fyniq.api_client SET class = 'integration', revoked_at = now(), revoked_by = 'user:x',
		  revoke_reason = 'x', version = version + 1 WHERE id = $1`, []string{"FY420"}},
	} {
		err := asOrg(ctx, conn(t, "fyniq_api"), a.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, x.stmt, p.ID); return err })
		wantState(t, x.what, err, x.states...)
	}
}
