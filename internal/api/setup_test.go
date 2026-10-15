//go:build local

package api

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/adapters/postgres/pgtest"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/members"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/setupcode"
	revoke "github.com/cognizhi/fyniq/internal/tokens"
)

// freshInstallation is an API over a database of its own: first-run setup runs once per installation.
func freshInstallation(t *testing.T) (*env, *pgtest.DB) {
	t.Helper()
	ctx := context.Background()
	fresh, err := pgtest.Start(ctx)
	if err != nil {
		t.Fatalf("a fresh database: %v", err)
	}
	t.Cleanup(fresh.Stop)
	st, err := postgres.Open(ctx, fresh.URL("fyniq_api"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	e := &env{t: t, store: st, clock: clock.NewFrozen(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)),
		tokens: &tokens{m: map[string]authn.Principal{}}, log: &bytes.Buffer{}}
	mux := http.NewServeMux()
	e.api = New(mux, Deps{Store: st, Installation: st, Outbox: &outbox.Dispatcher{Store: st, Queue: local.NewMemory(), Clock: e.clock},
		Clock: e.clock, Auth: e.tokens, Log: slog.New(slog.NewJSONHandler(e.log, nil)), Version: "0.0.0-test"})
	e.srv = httptest.NewServer(mux)
	t.Cleanup(e.srv.Close)
	return e, fresh
}

// req: FR-ADM-06
// req: CTL-IAM-07
// req: SEC-02
// asvs: V8.2.1, V15.4.2, V2.3.4
func TestFirstRunSetup(t *testing.T) {
	ctx := context.Background()
	e, fresh := freshInstallation(t)
	st := e.store

	founder := e.principal(authn.Principal{Subject: "founder-1", Client: "web"})
	request := func(code string) map[string]any {
		return map[string]any{"setup_code": code, "display_name": "Mei Ling Tan",
			"organisation": map[string]any{"name": "Cognizhi Pte. Ltd.", "base_currency": "SGD"}}
	}
	if r := e.call(http.MethodGet, "/v1/setup", "", nil); r.Status != 200 || r.Body["done"] != false {
		t.Fatalf("a new installation's status, asked without a credential: %d %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/setup", founder, request("ANY-CODE-AT-ALL-BEFORE-ONE-EXISTS")), "completeSetup", 409, CodeSetupRefused)

	code, hash := setupcode.New()
	if err := postgres.SetSetupCode(ctx, fresh.URL("fyniq_migrator"), hash); err != nil {
		t.Fatal(err)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/setup", "", request(code)), "completeSetup", 401, CodeUnauthenticated)
	oauthClient := e.principal(authn.Principal{Via: authn.ViaToken, Subject: "founder-1", Client: "some-client", Class: authn.ClassUser})
	e.problemIs(e.call(http.MethodPost, "/v1/setup", oauthClient, request(code)), "completeSetup", 403, CodeSessionRequired)
	ai := e.principal(authn.Principal{Subject: "founder-1", Client: "agent", Class: authn.ClassAgent})
	if r := e.call(http.MethodPost, "/v1/setup", ai, request(code)); r.Status != 403 {
		t.Fatalf("the AI completed setup: %d %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/setup", founder, request(code+"X")), "completeSetup", 409, CodeSetupRefused)
	if done, _ := st.SetupDone(ctx); done {
		t.Fatal("a wrong code completed setup")
	}

	// Two people with the right code at once: one becomes the Super admin, the other is refused.
	other := e.principal(authn.Principal{Subject: "someone-else", Client: "web"})
	var wg sync.WaitGroup
	results := make([]response, 2)
	for i, who := range []string{founder, other} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body := request(strings.ToLower(strings.ReplaceAll(code, "-", " "))) // typed loosely
			results[i] = e.call(http.MethodPost, "/v1/setup", who, body)
		}()
	}
	wg.Wait()
	statuses := []int{results[0].Status, results[1].Status}
	slices.Sort(statuses)
	if statuses[0] != 201 || statuses[1] != 409 {
		t.Fatalf("two setups at once: %d %s / %d %s", results[0].Status, results[0].Raw, results[1].Status, results[1].Raw)
	}
	winner := 0
	if results[0].Status != 201 {
		winner = 1
	}
	won := results[winner]
	if codes, _ := won.Body["recovery_codes"].([]any); len(codes) != 10 || codes[0] == codes[1] {
		t.Fatalf("setup gave the Super admin %d recovery codes, want ten different ones: %s", len(codes), won.Raw)
	}
	if r := e.call(http.MethodGet, "/v1/setup", "", nil); r.Body["done"] != true {
		t.Fatalf("after setup: %s", r.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/setup", founder, request(code)), "completeSetup", 409, CodeSetupRefused) // never twice
	if err := postgres.SetSetupCode(ctx, fresh.URL("fyniq_migrator"), hash); storage.CodeOf(err) != storage.CodeSetup {
		t.Fatalf("a new setup code after setup: %v", err)
	}

	// What setup made: the Super admin, the Owner of the first organisation, and two audit trails.
	user, _ := uuid.Parse(won.str("user_id"))
	org, _ := uuid.Parse(won.str("organisation_id"))
	inst, _ := uuid.Parse(won.str("installation_organisation_id"))
	subject := []string{"founder-1", "someone-else"}[winner]
	if got, err := st.UserForIdentity(ctx, "urn:fyniq:test", subject); err != nil || got != user {
		t.Fatalf("the winner's identity: %s, %v", got, err)
	}
	if err := st.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		roles, err := u.Members().Roles(ctx, user, e.clock.Now())
		if err != nil || len(roles) != 1 || roles[0].Role != "owner" {
			t.Errorf("the Super admin's roles in the first organisation: %+v, %v", roles, err)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	reader, err := postgres.Open(ctx, fresh.URL("fyniq_readonly"))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	trail := func(o uuid.UUID) string {
		var b strings.Builder
		_ = reader.Do(ctx, o, func(ctx context.Context, u storage.UnitOfWork) error {
			events, err := u.Audit().Events(ctx, 0, 100)
			for _, ev := range events {
				b.WriteString(ev.Body + "\n")
			}
			return err
		})
		return b.String()
	}
	actor := `"actor":{"type":"user","id":"` + user.String() + `"}`
	for o, actions := range map[uuid.UUID][]string{inst: {"setup.completed", "super_admin.added"}, org: {"user_membership.created", "role_assignment.created"}} {
		tr := trail(o)
		for _, a := range actions {
			if !strings.Contains(tr, `"action":"`+a+`"`) {
				t.Errorf("organisation %s's trail lacks %s", o, a)
			}
		}
		if !strings.Contains(tr, actor) {
			t.Errorf("organisation %s's trail does not name the new user as the actor:\n%s", o, tr)
		}
	}
	if err := st.Do(ctx, inst, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Members().Add(ctx, user)
		return err
	}); storage.CodeOf(err) != storage.CodeMembership {
		t.Errorf("a membership of the installation organisation: %v", err)
	}

	// A role change revokes the member's tokens in the same unit of work (AI-38), and the database
	// holds exactly the roles of §7.1 a person can hold.
	d := outbox.Dispatcher{Store: st, Queue: local.NewMemory(), Clock: e.clock}
	for role := range authz.Roles {
		_, err := members.Assign(ctx, st, d, org, user, string(role), nil, "test of every role")
		personal := role != authz.LineManager && role != authz.HROfficer
		switch {
		case personal && role == authz.Owner && storage.CodeOf(err) != storage.CodeDuplicate:
			t.Errorf("assigning owner twice: %v", err)
		case personal && role != authz.Owner && err != nil:
			t.Errorf("assigning %s: %v", role, err)
		case !personal && storage.CodeOf(err) != storage.CodeMembership:
			t.Errorf("assigning %s, which is not a role a person holds: %v", role, err)
		}
	}
	if err := st.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		rows, err := u.Sweeping().Due(ctx, e.clock.Now().Add(time.Hour), e.clock.Now().Add(time.Hour))
		n := 0
		for _, r := range rows {
			if r.Kind == revoke.Kind {
				n++
			}
		}
		if n < 15 {
			t.Errorf("%d revocations queued for 15 role changes", n)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
