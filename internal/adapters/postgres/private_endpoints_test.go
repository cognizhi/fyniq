//go:build local

package postgres_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/clock"
)

// The database keeps registered private endpoints as SEC-37 asks (migration 000310): only an active Super admin registers
// one, and with two Super admins only under the other's approved change, applied once; only a private range of at most
// /24 or /64, a pin that is a SHA-256, an organisation that exists, at the version it follows; no application role
// writes the table, whose versions are never changed or removed; and an organisation reads only its own, for one
// credential.
// req: SEC-37
// req: FR-ADM-07
// req: QA-ENG-05
// asvs: V8.2.1, V1.3.6
func TestPrivateEndpointsAreRegisteredOnlyAsTheDatabaseAllows(t *testing.T) {
	ctx := context.Background()
	org := books(t, "Private endpoints Pte Ltd").Org
	other := books(t, "Private endpoints elsewhere Pte Ltd").Org
	st := store(t, "fyniq_api")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	first, second, stranger := founder(t, st, org, 41, now), founder(t, st, org, 42, now), founder(t, st, org, 43, now)
	makeSuperAdmin(t, first)
	makeSuperAdmin(t, second)
	api := conn(t, "fyniq_api")
	sum := sha256.Sum256([]byte("the endpoint's SubjectPublicKeyInfo"))
	pin := base64.StdEncoding.EncodeToString(sum[:])
	credential, id := uuid.New(), uuid.Must(uuid.NewV7())
	type reg struct {
		by      uuid.UUID
		change  *uuid.UUID
		version int
		address string
		pin     string
		serves  uuid.UUID
	}
	register := func(r reg) error {
		return asOrg(ctx, api, org, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `SELECT fyniq.register_private_endpoint($1, $2, $3, $4, $5, 8443, $6, 'connector', $7, $8, false, 'Acme''s ledger API', $9)`,
				r.by, r.change, id, r.version, r.address, r.pin, r.serves, credential, now)
			return err
		})
	}
	ok := reg{by: first, address: "10.20.0.0/28", pin: pin, serves: org}
	with := func(r reg, f func(*reg)) reg { f(&r); return r }

	if err := register(with(ok, func(r *reg) { r.by = stranger })); sqlstate(err) != "FY085" {
		t.Fatalf("a registration by someone who is not a Super admin: %v", err)
	}
	if err := register(ok); sqlstate(err) != "FY213" {
		t.Fatalf("with two Super admins, a registration without the other's approval: %v", err)
	}
	envelope := `{"connector":"private-endpoint:` + id.String() + `","step":"private_endpoint","at":"` + now.Format(time.RFC3339Nano) + `"}`
	hash := sha256.Sum256([]byte(envelope))
	c := requestChange(t, org, first, nil, "private-endpoint:"+id.String(), "private_endpoint", now)
	if err := decide(t, org, second, c, hash[:], true, now); err != nil {
		t.Fatal(err)
	}
	approved := with(ok, func(r *reg) { r.change = &c.ID })
	for name, r := range map[string]reg{
		"a public address":                  with(approved, func(r *reg) { r.address = "8.8.8.8" }),
		"a range wider than a /24":          with(approved, func(r *reg) { r.address = "10.20.0.0/16" }),
		"loopback":                          with(approved, func(r *reg) { r.address = "127.0.0.1" }),
		"the metadata service":              with(approved, func(r *reg) { r.address = "169.254.169.254" }),
		"not an address":                    with(approved, func(r *reg) { r.address = "acme.internal" }),
		"a pin that is not a SHA-256":       with(approved, func(r *reg) { r.pin = "c2hvcnQ=" }),
		"an organisation that is not there": with(approved, func(r *reg) { r.serves = uuid.New() }),
	} {
		if err := register(r); sqlstate(err) != "FY319" {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := register(with(approved, func(r *reg) { r.version = 1 })); sqlstate(err) != "FY320" {
		t.Fatalf("a version that follows none: %v", err)
	}
	if err := register(approved); err != nil {
		t.Fatalf("the approved registration: %v", err)
	}
	if err := register(with(approved, func(r *reg) { r.version = 1 })); sqlstate(err) != "FY213" {
		t.Fatalf("an approved change applied twice: %v", err)
	}

	// An organisation reads its own registrations only, and only for the credential they are bound to.
	reach := func(as uuid.UUID, cred uuid.UUID) int {
		var n int
		if err := asOrg(ctx, conn(t, "fyniq_worker"), as, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM fyniq.private_endpoints_for('connector', $1)`, cred).Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if reach(org, credential) != 1 || reach(other, credential) != 0 || reach(org, uuid.New()) != 0 {
		t.Fatalf("what each organisation reaches: %d %d %d", reach(org, credential), reach(other, credential), reach(org, uuid.New()))
	}

	// No application role writes the table; even its owner cannot change or remove a version.
	for _, sql := range []string{
		`INSERT INTO fyniq.private_endpoint (id, version, address, port, tls_pin, service, serves, credential, reason, recorded_by, recorded_at)
		 VALUES (gen_random_uuid(), 1, '10.9.9.9/32', 443, '` + pin + `', 'connector', '` + org.String() + `', gen_random_uuid(), 'x x', '` + first.String() + `', now())`,
		`UPDATE fyniq.private_endpoint SET address = '10.9.9.0/24'`,
		`DELETE FROM fyniq.private_endpoint`,
	} {
		if err := asOrg(ctx, api, org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, sql); return err }); sqlstate(err) != "42501" {
			t.Errorf("the API role ran %.40s: %v", sql, err)
		}
	}
	for _, sql := range []string{`UPDATE fyniq.private_endpoint SET address = '10.9.9.0/24'`, `DELETE FROM fyniq.private_endpoint`} {
		if _, err := conn(t, "fyniq").Exec(ctx, sql); sqlstate(err) != "FY321" {
			t.Errorf("the owner ran %.40s: %v", sql, err)
		}
	}
}
