//go:build local

package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/domain/superadmin"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/setupcode"
	revoke "github.com/cognizhi/fyniq/internal/tokens"
)

// The second Super admin, recovery codes and the two-person recoveries, on an installation of their own.
// req: FR-ADM-06
// req: FR-ADM-07
// req: FR-ADM-09
// req: SEC-35
// req: CTL-IAM-07
// asvs: V6.4.1, V6.4.2, V8.2.1
func TestSuperAdmins(t *testing.T) {
	ctx := context.Background()
	e, fresh := freshInstallation(t)
	st := e.store
	person := func(subject string, steppedUp time.Duration) string {
		p := authn.Principal{Subject: subject, Client: "web"}
		if steppedUp >= 0 {
			p.StepUpAt = e.clock.Now().Add(-steppedUp)
		}
		return e.principal(p)
	}
	codes := func(r response) []string {
		var out []string
		list, _ := r.Body["recovery_codes"].([]any)
		for _, c := range list {
			s, _ := c.(string)
			out = append(out, s)
		}
		if len(out) != 10 || len(slices.Compact(slices.Sorted(slices.Values(out)))) != 10 {
			t.Fatalf("want ten different recovery codes: %s", r.Raw)
		}
		return out
	}
	shown := []string{} // every code the API showed: none may reach a log or the trail (SEC-15)
	keep := func(cs ...string) { shown = append(shown, cs...) }

	// Before setup there is no Super admin and no code.
	e.problemIs(e.call(http.MethodPost, "/v1/super-admins/invitations", person("founder", 0), nil), "createSuperAdminInvitation", 403, CodeNotSuperAdmin)
	e.problemIs(e.call(http.MethodPost, "/v1/super-admins", person("cofounder", -1), map[string]any{"invitation_code": "ABCDE-FGHIJ-KLMNO-PQRST-UVWXY-Z",
		"display_name": "Wei Jie Lim"}), "acceptSuperAdminInvitation", 409, CodeCodeRefused)

	// Setup: the founder is the first Super admin, with ten recovery codes.
	code, hash := setupcode.New()
	if err := postgres.SetSetupCode(ctx, fresh.URL("fyniq_migrator"), hash); err != nil {
		t.Fatal(err)
	}
	setup := e.call(http.MethodPost, "/v1/setup", person("founder", -1), map[string]any{"setup_code": code, "display_name": "Mei Ling Tan",
		"organisation": map[string]any{"name": "Cognizhi Pte. Ltd.", "base_currency": "SGD"}})
	if setup.Status != 201 {
		t.Fatalf("setup: %d %s", setup.Status, setup.Raw)
	}
	founderCodes := codes(setup)
	keep(founderCodes...)
	founder, _ := uuid.Parse(setup.str("user_id"))
	inst, _ := uuid.Parse(setup.str("installation_organisation_id"))

	// Inviting needs a Super admin, signed in to Fyniq, who passed step-up in the last 5 minutes.
	invite := "/v1/super-admins/invitations"
	e.problemIs(e.call(http.MethodPost, invite, person("founder", -1), nil), "createSuperAdminInvitation", 403, CodeStepUpRequired)
	e.problemIs(e.call(http.MethodPost, invite, person("founder", 6*time.Minute), nil), "createSuperAdminInvitation", 403, CodeStepUpRequired)
	e.problemIs(e.call(http.MethodPost, invite, person("someone-else", 0), nil), "createSuperAdminInvitation", 403, CodeNotSuperAdmin)
	e.problemIs(e.call(http.MethodPost, invite, e.principal(authn.Principal{Via: authn.ViaToken, Subject: "founder", Client: "some-client",
		Class: authn.ClassUser, StepUpAt: e.clock.Now()}), nil), "createSuperAdminInvitation", 403, CodeSessionRequired)
	if r := e.call(http.MethodPost, invite, e.principal(authn.Principal{Subject: "founder", Client: "agent", Class: authn.ClassAgent,
		StepUpAt: e.clock.Now()}), nil); r.Status != 403 {
		t.Fatalf("the AI invited a Super admin: %d %s", r.Status, r.Raw)
	}
	first := e.call(http.MethodPost, invite, person("founder", time.Minute), nil)
	second := e.call(http.MethodPost, invite, person("founder", time.Minute), nil)
	if first.Status != 201 || second.Status != 201 || second.str("expires_at") != "2026-10-22T09:00:00Z" {
		t.Fatalf("invitations: %d %s / %d %s", first.Status, first.Raw, second.Status, second.Raw)
	}
	keep(first.str("code"), second.str("code"))
	accept := func(who, invitation string) response {
		return e.call(http.MethodPost, "/v1/super-admins", who, map[string]any{"invitation_code": invitation, "display_name": "Wei Jie Lim"})
	}
	e.problemIs(accept(person("cofounder", -1), first.str("code")), "acceptSuperAdminInvitation", 409, CodeCodeRefused) // replaced

	// Two people with the same invitation at once: one becomes the second Super admin.
	var wg sync.WaitGroup
	results := make([]response, 2)
	for i, who := range []string{"cofounder", "intruder"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = accept(person(who, -1), strings.ToLower(second.str("code")))
		}()
	}
	wg.Wait()
	if s := []int{results[0].Status, results[1].Status}; !slices.Contains(s, 201) || !slices.Contains(s, 409) {
		t.Fatalf("two redemptions at once: %d %s / %d %s", results[0].Status, results[0].Raw, results[1].Status, results[1].Raw)
	}
	winner := map[bool]string{true: "cofounder", false: "intruder"}[results[0].Status == 201]
	won := results[0]
	if won.Status != 201 {
		won = results[1]
	}
	cofounder, _ := uuid.Parse(won.str("user_id"))
	keep(codes(won)...)
	if got, err := st.SuperAdminFor(ctx, "urn:fyniq:test", winner); err != nil || got != cofounder {
		t.Fatalf("the second Super admin: %s, %v", got, err)
	}
	if n, err := st.CountSuperAdmins(ctx); err != nil || n != 2 {
		t.Fatalf("%d Super admins, %v", n, err)
	}
	// At most two in P0 (FR-ADM-07): no third invitation.
	e.problemIs(e.call(http.MethodPost, invite, person("founder", 0), nil), "createSuperAdminInvitation", 409, CodeSuperAdminRefused)

	// The second Super admin replaces their codes; the old ones stop working.
	old := codes(won)
	e.problemIs(e.call(http.MethodPost, "/v1/super-admins/recovery-codes", person(winner, -1), nil), "replaceSuperAdminRecoveryCodes", 403, CodeStepUpRequired)
	replaced := e.call(http.MethodPost, "/v1/super-admins/recovery-codes", person(winner, 0), nil)
	if replaced.Status != 200 {
		t.Fatalf("replace codes: %d %s", replaced.Status, replaced.Raw)
	}
	cofounderCodes := codes(replaced)
	keep(cofounderCodes...)

	// Recovery with two proofs (SEC-35): the founder confirms; the cofounder, signed in with a new
	// identity, gives a recovery code with the confirmation.
	confirm := func(who string, of uuid.UUID) response {
		return e.call(http.MethodPost, "/v1/super-admins/"+of.String()+"/recovery-confirmations", who, nil)
	}
	e.problemIs(confirm(person("founder", 0), founder), "confirmSuperAdminRecovery", 409, CodeSuperAdminRefused)
	e.problemIs(confirm(person("founder", -1), cofounder), "confirmSuperAdminRecovery", 403, CodeStepUpRequired)
	e.problemIs(confirm(person("founder", 0), uuid.New()), "confirmSuperAdminRecovery", 409, CodeSuperAdminRefused)
	confirmation := confirm(person("founder", 0), cofounder)
	if confirmation.Status != 201 || confirmation.str("expires_at") != "2026-10-16T09:00:00Z" {
		t.Fatalf("confirmation: %d %s", confirmation.Status, confirmation.Raw)
	}
	keep(confirmation.str("code"))
	recover := func(who, recoveryCode, confirmationCode string) response {
		body := map[string]any{"confirmation_code": confirmationCode}
		if recoveryCode != "" {
			body["recovery_code"] = recoveryCode
		}
		return e.call(http.MethodPost, "/v1/super-admins/recovery", who, body)
	}
	newCofounder := person("cofounder-new-phone", -1)
	for _, bad := range [][2]string{
		{"", confirmation.str("code")},                 // one proof is not enough
		{old[0], confirmation.str("code")},             // a replaced code
		{founderCodes[0], confirmation.str("code")},    // another Super admin's code
		{cofounderCodes[0], cofounderCodes[1]},         // two recovery codes are one proof
		{cofounderCodes[0], "ABCDE-FGHIJ-KLMNO-PQRST"}, // a made-up confirmation
	} {
		e.problemIs(recover(newCofounder, bad[0], bad[1]), "recoverSuperAdmin", 409, CodeCodeRefused)
	}
	e.problemIs(recover(e.principal(authn.Principal{Via: authn.ViaToken, Subject: "cofounder-new-phone", Client: "c", Class: authn.ClassUser}),
		cofounderCodes[0], confirmation.str("code")), "recoverSuperAdmin", 403, CodeSessionRequired)
	recovered := recover(newCofounder, cofounderCodes[0], confirmation.str("code"))
	if recovered.Status != 200 || recovered.str("user_id") != cofounder.String() {
		t.Fatalf("recovery: %d %s", recovered.Status, recovered.Raw)
	}
	keep(codes(recovered)...)
	if got, err := st.SuperAdminFor(ctx, "urn:fyniq:test", "cofounder-new-phone"); err != nil || got != cofounder {
		t.Errorf("after recovery the new sign-in is the Super admin: %s, %v", got, err)
	}
	if _, err := st.SuperAdminFor(ctx, "urn:fyniq:test", winner); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("after recovery the replaced sign-in is still a Super admin: %v", err)
	}
	e.problemIs(recover(newCofounder, cofounderCodes[1], confirmation.str("code")), "recoverSuperAdmin", 409, CodeCodeRefused) // used once
	revocations := map[string]bool{}
	if err := st.Do(ctx, inst, func(ctx context.Context, u storage.UnitOfWork) error {
		rows, err := u.Sweeping().Due(ctx, e.clock.Now().Add(time.Hour), e.clock.Now().Add(time.Hour))
		for _, r := range rows {
			if r.Kind == revoke.Kind {
				revocations[string(r.Payload)] = true
			}
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, subject := range []string{winner, cofounder.String()} {
		found := false
		for p := range revocations {
			found = found || strings.Contains(p, `"`+subject+`"`)
		}
		if !found {
			t.Errorf("no revocation of %s's tokens after the recovery (AI-38): %v", subject, revocations)
		}
	}

	// The deployment's two-person recovery: the founder lost every way in.
	migrator := fresh.URL("fyniq_migrator")
	now := e.clock.Now()
	for _, operators := range []string{"Mei Ling Tan", "Mei Ling Tan, mei ling tan", ", Wei Jie Lim", "A, B, C"} {
		if err := postgres.IssueDeploymentRecovery(ctx, migrator, founder, setupcode.Hash(operators+"-x"), operators, now, now.Add(24*time.Hour)); storage.CodeOf(err) != storage.CodeGrant {
			t.Errorf("a deployment recovery issued by %q: %v", operators, err)
		}
	}
	deployment, dhash := setupcode.New()
	keep(deployment)
	if err := postgres.IssueDeploymentRecovery(ctx, migrator, founder, dhash, "Mei Ling Tan, Wei Jie Lim", now, now.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	e.clock.Advance(25 * time.Hour)
	e.problemIs(recover(person("founder-laptop", -1), "", deployment), "recoverSuperAdmin", 409, CodeCodeRefused) // expired
	e.clock.Advance(-25 * time.Hour)
	if r := recover(person("founder-laptop", -1), "", deployment); r.Status != 200 || r.str("user_id") != founder.String() {
		t.Fatalf("deployment recovery: %d %s", r.Status, r.Raw)
	} else {
		keep(codes(r)...)
	}

	// The installation's trail: every change, by whom, and never a code.
	reader, err := postgres.Open(ctx, fresh.URL("fyniq_readonly"))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var trail strings.Builder
	_ = reader.Do(ctx, inst, func(ctx context.Context, u storage.UnitOfWork) error {
		events, err := u.Audit().Events(ctx, 0, 500)
		for _, ev := range events {
			trail.WriteString(ev.Body + "\n")
		}
		return err
	})
	type event struct {
		Action string `json:"action"`
		Actor  struct{ Type, ID string }
	}
	by := map[string]bool{}
	for line := range strings.Lines(trail.String()) {
		var ev event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatal(err)
		}
		by[ev.Action+" "+ev.Actor.Type+":"+ev.Actor.ID] = true
	}
	for action, actor := range map[string]uuid.UUID{
		"super_admin.invitation_created": founder, "super_admin.added": cofounder, "super_admin.recovery_codes_replaced": cofounder,
		"super_admin.recovery_confirmed": founder, "super_admin.recovered": cofounder, revoke.ActionRevokeRequested: cofounder,
	} {
		if !by[action+" user:"+actor.String()] {
			t.Errorf("the installation's trail has no %s by %s", action, actor)
		}
	}
	if !strings.Contains(trail.String(), `"action":"super_admin.deployment_recovery_issued"`) || !strings.Contains(trail.String(), `Mei Ling Tan, Wei Jie Lim`) {
		t.Errorf("the deployment recovery is not in the trail with its two operators:\n%s", trail.String())
	}
	for _, c := range shown {
		bare := strings.ReplaceAll(c, "-", "")
		for where, text := range map[string]string{"trail": trail.String(), "log": e.log.String()} {
			if strings.Contains(text, c) || strings.Contains(text, bare) {
				t.Errorf("a one-time code reached the %s (SEC-15)", where)
			}
		}
	}

	// The nightly count (CTL-IAM-07), as the worker: two now; none fails and is recorded.
	worker, err := postgres.Open(ctx, fresh.URL("fyniq_worker"))
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close()
	if v, err := superadmin.Verify(ctx, worker, worker); err != nil || v.Active != 2 || !v.SetUp {
		t.Fatalf("the nightly count: %+v, %v", v, err)
	}
	admin, err := pgx.Connect(ctx, fresh.URL("fyniq"))
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	if _, err := admin.Exec(ctx, "UPDATE fyniq.app_user SET status = 'deactivated'"); err != nil {
		t.Fatal(err)
	}
	if v, err := superadmin.Verify(ctx, worker, worker); err == nil || v.Active != 0 {
		t.Fatalf("no Super admin left, and the nightly count passed: %+v", v)
	}
	var failed bool
	_ = reader.Do(ctx, inst, func(ctx context.Context, u storage.UnitOfWork) error {
		events, err := u.Audit().Events(ctx, 0, 500)
		for _, ev := range events {
			failed = failed || (strings.Contains(ev.Body, `"control.check.failed"`) && strings.Contains(ev.Body, superadmin.Control))
		}
		return err
	})
	if !failed {
		t.Error("the failed count is not in the installation's trail")
	}

	// The database is the backstop: never a third, never none, never deleted; grants only through
	// the functions; the deployment recovery only as the migrator.
	sqlstate := func(err error) string {
		var pg *pgconn.PgError
		if errors.As(err, &pg) {
			return pg.Code
		}
		return ""
	}
	tx, err := admin.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "UPDATE fyniq.super_admin SET removed_at = now() WHERE user_id = $1", founder); err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx, "UPDATE fyniq.super_admin SET removed_at = now() WHERE user_id = $1", cofounder)
	if sqlstate(err) != "FY083" {
		t.Errorf("removing the last Super admin: %v", err)
	}
	_ = tx.Rollback(ctx)
	for sql, want := range map[string]string{
		"INSERT INTO fyniq.super_admin (user_id) SELECT user_id FROM fyniq.app_user WHERE user_id NOT IN (SELECT user_id FROM fyniq.super_admin) LIMIT 1": "FY084",
		"DELETE FROM fyniq.super_admin": "FY083",
	} {
		if _, err := admin.Exec(ctx, "INSERT INTO fyniq.app_user (display_name) VALUES ('Third')"); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.Exec(ctx, sql); sqlstate(err) != want {
			t.Errorf("%s: want %s, got %v", sql, want, err)
		}
	}
	api, err := pgx.Connect(ctx, fresh.URL("fyniq_api"))
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close(ctx)
	for _, sql := range []string{
		"SELECT count(*) FROM fyniq.super_admin_grant",
		"SELECT fyniq.issue_deployment_recovery('" + founder.String() + "', sha256('x'), 'A, B', now(), now() + interval '1 day')",
	} {
		if _, err := api.Exec(ctx, sql); sqlstate(err) != "42501" {
			t.Errorf("the API ran %q: %v", sql, err)
		}
	}
}
