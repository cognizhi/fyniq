//go:build local

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura/nemuratest"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/identity"
)

// A person takes a place through the browser alone: the first person signs in with the deployment's setup code and becomes the first
// organisation's Owner (the web app's join form posts to /auth/join); the Owner's invitation takes a second person in as the member it
// names; a wrong or used code lets nobody in; and neither code is ever in a URL (NM-ARC-01).
// req: NM-ARC-01
// asvs: V2.1.1, V7.2.1, V3.3.1
func TestFirstRunAndAnInvitationTakeAPlaceThroughTheBrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the binary against a database")
	}
	ctx := context.Background()
	bin := build(t)
	d, err := nemuratest.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Stop()
	store, err := nemura.Open(ctx, d.URL("nemura_api"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	setup, hash := identity.NewSetupCode()
	if err := nemura.SetSetupCode(ctx, d.URL(nemura.Migrator), hash); err != nil {
		t.Fatal(err)
	}

	port := freePort(t)
	base := "http://127.0.0.1:" + port
	srv := exec.Command(bin)
	srv.Env = env("NEMURA_ROLE=api", "PORT="+port, "NEMURA_DATABASE_URL="+d.URL("nemura_api"), "NEMURA_PUBLIC_ORIGIN="+base)
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = srv.Process.Kill(); _ = srv.Wait() }()
	for range 200 {
		if r, err := http.Get(base + "/readyz"); err == nil {
			_ = r.Body.Close()
			if r.StatusCode == http.StatusOK {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}

	// join posts the form, then the provider's page for the subject, then the callback: where the browser lands, and the session it holds.
	join := func(form url.Values, subject string) (landing string, session string) {
		t.Helper()
		jar, _ := cookiejar.New(nil)
		c := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		hop := func(resp *http.Response, err error) string {
			t.Helper()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusSeeOther {
				t.Fatalf("%s: %d", resp.Request.URL, resp.StatusCode)
			}
			return resp.Header.Get("Location")
		}
		next := hop(c.PostForm(base+"/auth/join", form))
		if strings.Contains(next, form.Get("code")) {
			t.Fatalf("the code is in the URL %s", next)
		}
		if !strings.HasPrefix(next, "/sign-in?") {
			// The edge sends /api/... to the API with the prefix stripped; here the API is on the origin itself.
			next = hop(c.Get(base + strings.TrimPrefix(next, "/api")))
			page, _ := url.Parse(next)
			f := page.Query()
			f.Set("decision", "sign-in")
			f.Set("subject", subject)
			cb, _ := url.Parse(hop(c.PostForm(base+page.Path, f)))
			next = hop(c.Get(base + cb.Path + "?" + cb.RawQuery))
		}
		resp, err := c.Get(base + "/v1/session")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		var b bytes.Buffer
		_, _ = b.ReadFrom(resp.Body)
		return next, b.String()
	}

	// The first person: refused with a wrong code (and the code stays good), then in as the Owner with the right one.
	form := func(code string) url.Values {
		return url.Values{"provider": {"local"}, "kind": {"setup"}, "code": {code}, "organisation": {"Join Pte Ltd"}, "name": {"Fay Founder"}, "return_to": {"/people"}}
	}
	if landing, session := join(form("not-the-code"), "fay"); landing != "/sign-in?signin_error=NM_SIGNIN_08" || strings.Contains(session, `"owner"`) {
		t.Fatalf("a wrong setup code: %s %s", landing, session)
	}
	landing, session := join(form(setup), "fay")
	if landing != "/people" || !strings.Contains(session, `"owner"`) || !strings.Contains(session, `"landing":"/people"`) {
		t.Fatalf("the first person: %s %s", landing, session)
	}
	// The code works once.
	if landing, _ := join(form(setup), "eve"); landing != "/sign-in?signin_error=NM_SIGNIN_08" {
		t.Errorf("a used setup code: %s", landing)
	}

	// The Owner invites a member; the invitation takes in whoever holds it, once.
	var who struct {
		Org    uuid.UUID `json:"organisation_id"`
		Member uuid.UUID `json:"member_id"`
	}
	if err := json.Unmarshal([]byte(session), &who); err != nil {
		t.Fatal(err)
	}
	org, owner := who.Org, who.Member
	var code string
	if err := store.InOrg(ctx, org, func(ctx context.Context, tx pgx.Tx) error {
		by := audit.Actor{Type: "user", ID: owner.String()}
		member, err := nemura.AddMember(ctx, tx, org, clock.System{}.Now(), by, "Ivy Invited")
		if err != nil {
			return err
		}
		code, err = nemura.Invite(ctx, tx, org, clock.System{}.Now(), owner, member)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	inv := func(c string) url.Values {
		return url.Values{"provider": {"local"}, "kind": {"invitation"}, "code": {c}, "return_to": {"/me"}}
	}
	if landing, _ := join(inv(code+"x"), "ivy"); landing != "/sign-in?signin_error=NM_SIGNIN_08" {
		t.Errorf("a wrong invitation: %s", landing)
	}
	landing, session = join(inv(code), "ivy")
	if landing != "/me" || !strings.Contains(session, `"landing":"/me"`) {
		t.Fatalf("the invited member: %s %s", landing, session)
	}
	if landing, _ := join(inv(code), "mallory"); landing != "/sign-in?signin_error=NM_SIGNIN_08" {
		t.Errorf("a used invitation: %s", landing)
	}
}
