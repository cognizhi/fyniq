//go:build local

package postgres_test

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A sandbox organisation is the database's rule, not the API's alone (FR-INT-18; the owner's decision of 21 Nov 2026, migration 001250): only an
// active Super admin makes one, with two Super admins only under the other's approved change, applied once; it is a NEW organisation, labelled
// "Sandbox: ", whose creator is its Owner; no application role reads or writes the registry, runs the function but the API's, or can flag an
// organisation that exists; and not even the registry's owner removes or changes a flag, so no real organisation becomes a sandbox and no sandbox
// becomes a real one. Every role that works in an organisation reads the one boolean.
// req: FR-INT-18
// req: FR-ADM-07
// asvs: V8.2.1, V8.3.1, V4.2.1
func TestAttackASandboxOrganisationIsMadeNewFlaggedForGoodAndNeverFromARealOne(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "super"}})
	real := books(t, "Sandbox attack Pte Ltd").Org
	st := store(t, "fyniq_api")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	first, second, stranger := founder(t, st, real, 51, now), founder(t, st, real, 52, now), founder(t, st, real, 53, now)
	makeSuperAdmin(t, first)
	makeSuperAdmin(t, second)
	api := conn(t, "fyniq_api")
	run := func(by uuid.UUID, change *uuid.UUID, id uuid.UUID, name string) error {
		return asOrg(ctx, api, real, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `SELECT fyniq.create_sandbox_organisation($1, $2, $3, $4, 'SGD', $5)`, by, change, id, name, now)
			return err
		})
	}
	id := uuid.Must(uuid.NewV7())
	if err := run(stranger, nil, id, "Sandbox: x"); sqlstate(err) != "FY085" {
		t.Fatalf("someone who is no Super admin makes one: %v", err)
	}
	if err := run(first, nil, id, "Sandbox: x"); sqlstate(err) != "FY213" {
		t.Fatalf("with two Super admins, one without the other's approval: %v", err)
	}
	c := requestChange(t, real, first, nil, "sandbox:"+id.String(), "sandbox", now)
	envelope := `{"connector":"sandbox:` + id.String() + `","step":"sandbox","at":"` + now.Format(time.RFC3339Nano) + `"}`
	hash := sha256.Sum256([]byte(envelope))
	if err := decide(t, real, second, c, hash[:], true, now); err != nil {
		t.Fatal(err)
	}
	if err := run(first, &c.ID, id, "A real name"); sqlstate(err) != "FY961" {
		t.Fatalf("a name without the label: %v", err)
	}
	other := uuid.Must(uuid.NewV7())
	if err := run(first, &c.ID, other, "Sandbox: y"); sqlstate(err) != "FY213" {
		t.Fatalf("an approval for another sandbox: %v", err)
	}
	if err := run(first, &c.ID, real, "Sandbox: z"); sqlstate(err) != "FY213" && sqlstate(err) != "FY960" {
		t.Fatalf("an approval used for an organisation that exists: %v", err)
	}
	// The function's success, its two organisations' trails and the creator's Owner role are proved through the API (TestASuperAdminMakesASandbox...): the
	// audit trigger wants the events the adapter writes. Here a sandbox is placed as the migrator, with triggers off, to attack the registry.
	id = placeSandbox(t, first)
	// The flag, from every role that works in an organisation.
	for _, role := range []string{"fyniq_api", "fyniq_worker", "fyniq_executor", "fyniq_readonly"} {
		for org, want := range map[uuid.UUID]bool{id: true, real: false} {
			var is bool
			if err := asOrg(ctx, conn(t, role), org, func(tx pgx.Tx) error {
				return tx.QueryRow(ctx, `SELECT fyniq.organisation_is_sandbox($1)`, org).Scan(&is)
			}); err != nil || is != want {
				t.Errorf("%s reads the flag of %s: %v %v, want %v", role, org, is, err, want)
			}
		}
	}
	// Only the API's role runs the function; no application role reads or writes the registry.
	for _, role := range []string{"fyniq_worker", "fyniq_readonly", "fyniq_executor"} {
		err := asOrg(ctx, conn(t, role), real, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `SELECT fyniq.create_sandbox_organisation($1, NULL, gen_random_uuid(), 'Sandbox: w', 'SGD', now())`, first)
			return err
		})
		wantState(t, role+" runs it", err, "42501")
	}
	for _, role := range []string{"fyniq_api", "fyniq_worker", "fyniq_readonly", "fyniq_executor"} {
		for _, sql := range []string{`SELECT * FROM fyniq.sandbox_organisation`, `DELETE FROM fyniq.sandbox_organisation`, `UPDATE fyniq.sandbox_organisation SET created_by = created_by`,
			`INSERT INTO fyniq.sandbox_organisation (organisation_id, created_by, created_at) VALUES ('` + real.String() + `', '` + first.String() + `', now())`} {
			err := asOrg(ctx, conn(t, role), real, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, sql); return err })
			wantState(t, role+" runs "+sql[:20], err, "42501")
		}
	}
	// Not even the registry's owner flags an organisation that existed before, or removes or changes a flag.
	owner := conn(t, "fyniq")
	if _, err := owner.Exec(ctx, `INSERT INTO fyniq.sandbox_organisation (organisation_id, created_by, created_at) VALUES ($1, $2, now())`, real, first); sqlstate(err) != "FY960" {
		t.Errorf("flagging a real organisation: %v", err)
	}
	for _, sql := range []string{`DELETE FROM fyniq.sandbox_organisation`, `UPDATE fyniq.sandbox_organisation SET created_by = created_by`} {
		if _, err := owner.Exec(ctx, sql); sqlstate(err) != "FY960" {
			t.Errorf("%s by the owner: %v", sql, err)
		}
	}
	if _, err := owner.Exec(ctx, `UPDATE fyniq.sandbox_organisation SET organisation_id = $1`, real); sqlstate(err) != "FY960" {
		t.Errorf("moving a flag to a real organisation: %v", err)
	}
	// The port says the same, and caches what it read.
	for org, want := range map[uuid.UUID]bool{id: true, real: false} {
		var is bool
		if err := st.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			is, err = u.Sandboxes().Is(ctx)
			return err
		}); err != nil || is != want {
			t.Errorf("the port reads the flag of %s: %v %v", org, is, err)
		}
	}
}

// placeSandbox places a sandbox organisation as the migrator with triggers off, to test what a sandbox does: the function that makes one wants the
// audit events the adapter writes, and is proved through the API.
func placeSandbox(t *testing.T, creator uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	id := uuid.Must(uuid.NewV7())
	tx, err := conn(t, "fyniq").Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, sql := range []string{`SET LOCAL session_replication_role = replica`, `SELECT set_config('app.org_id', '` + id.String() + `', true)`,
		`INSERT INTO fyniq.organisation (name, base_currency, base_minor_units) VALUES ('Sandbox: x', 'SGD', 2)`,
		`INSERT INTO fyniq.tenant_directory (organisation_id) VALUES ('` + id.String() + `')`,
		`INSERT INTO fyniq.sandbox_organisation (organisation_id, created_by, created_at) VALUES ('` + id.String() + `', '` + creator.String() + `', now())`} {
		if _, err := tx.Exec(ctx, sql); err != nil {
			t.Fatalf("%.50s: %v", sql, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return id
}
