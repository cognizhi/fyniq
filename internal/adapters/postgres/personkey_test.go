//go:build local

package postgres_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Personal values in the trail are sealed under the person's key (migration 000308, fyniq-audit-event@3; §11.8, design
// D8): an individual contact's and an invitee's names, emails, phone and address never reach the body, while a
// company's do; one key serves a person's events of a financial year; the values open with OpenSealed, and once the
// key is gone they read as shredded; the chain still verifies; the API, the worker and the executor add and read keys
// and none deletes one, a key never changes, the owner deletes none outside a shredding run, and another organisation
// sees none.
// req: AUD-02
// req: SEC-11
// req: QA-ENG-05
// req: data/backup-and-recovery Personal data in immutable records is crypto-shredded at the end of retention
// asvs: V11.3.2, V8.2.2
func TestPersonalValuesAreSealedUnderThePersonsKey(t *testing.T) {
	ctx := context.Background()
	a, b := books(t, "Sealed A Pte Ltd"), books(t, "Sealed B Pte Ltd")
	fields := detailsKey(t)
	api := store(t, "fyniq_api", postgres.WithFingerprints(fields))
	later := store(t, "fyniq_api", postgres.WithFingerprints(fields), postgres.WithClock(clock.NewFrozen(time.Date(2099, 6, 1, 2, 0, 0, 0, time.UTC))))
	var person, company, director storage.Contact
	var invitation storage.MemberInvitation
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		if person, err = u.Contacts().Create(ctx, storage.ContactDetails{LegalName: "Ada Tan", Customer: true, Individual: true,
			BillingEmail: "ada@example.com", Phone: "+65 9123 4567", Address: "2 Ada Road"}); err != nil {
			return err
		}
		if company, err = u.Contacts().Create(ctx, storage.ContactDetails{LegalName: "Northwind Pte Ltd", Vendor: true,
			BillingEmail: "ap@northwind.example"}); err != nil {
			return err
		}
		if director, err = u.Contacts().Create(ctx, storage.ContactDetails{LegalName: "Ben Lim", Director: true}); err != nil {
			return err
		}
		code := sha256.Sum256([]byte(uuid.NewString()))
		now := clock.System{}.Now()
		invitation, err = u.MemberInvitations().Create(ctx, storage.NewMemberInvitation{CodeHash: code[:], DisplayName: "Cara Ong",
			Roles: []string{"employee"}, ExpiresAt: now.Add(time.Hour), CreatedAt: now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// A change in a later financial year is sealed under the person's key for that year.
	if err := later.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		d := person.ContactDetails
		d.Phone = "+65 8000 0000"
		_, err := u.Contacts().Update(ctx, person.ID, person.Version, d, true)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	stored := events(t, a.Org)
	bodies := map[uuid.UUID][]string{}
	var sealed []storage.SealedValue
	var want []string
	for _, e := range stored {
		var ev audit.Event
		if err := json.Unmarshal([]byte(e.Body), &ev); err != nil {
			t.Fatal(err)
		}
		bodies[ev.Object.ID] = append(bodies[ev.Object.ID], e.Body)
		if ev.Object.ID != person.ID {
			continue
		}
		for field, c := range ev.Changes {
			for _, v := range c {
				if k, ct, ok := audit.SealedOf(v); ok {
					sealed = append(sealed, storage.SealedValue{EventID: ev.EventID, Field: field, Key: k, Text: ct})
					want = append(want, field)
				}
			}
		}
	}
	for _, clear := range []string{"Ada Tan", "ada@example.com", "9123 4567", "8000 0000", "2 Ada Road"} {
		if strings.Contains(strings.Join(bodies[person.ID], "\n"), clear) {
			t.Errorf("the individual's events hold %q", clear)
		}
	}
	if j := strings.Join(bodies[director.ID], "\n"); strings.Contains(j, "Ben Lim") || !strings.Contains(j, `"pk"`) {
		t.Errorf("the director's name is not sealed: %s", j)
	}
	if j := strings.Join(bodies[invitation.ID], "\n"); strings.Contains(j, "Cara Ong") || !strings.Contains(j, `"pk"`) {
		t.Errorf("the invitee's name is not sealed: %s", j)
	}
	if j := strings.Join(bodies[company.ID], "\n"); !strings.Contains(j, "Northwind Pte Ltd") || !strings.Contains(j, "ap@northwind.example") {
		t.Errorf("a company's values were sealed: %s", j)
	}
	keys := map[uuid.UUID]bool{}
	for _, s := range sealed {
		keys[s.Key] = true
	}
	if len(sealed) < 5 || len(keys) != 2 {
		t.Fatalf("the individual's sealed values: %d under %d keys, one per financial year (%v)", len(sealed), len(keys), want)
	}
	var opened []storage.OpenedValue
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		opened, err = u.Audit().OpenSealed(ctx, sealed)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, o := range opened {
		got[o.Value] = !o.Shredded
	}
	for _, v := range []string{"Ada Tan", "ada@example.com", "+65 9123 4567", "+65 8000 0000", "2 Ada Road"} {
		if !got[v] {
			t.Errorf("%q does not open: %v", v, opened)
		}
	}
	if _, err := audit.Chain(stored); err != nil {
		t.Errorf("the chain: %v", err)
	}

	// The key table's own rules, on the first year's key.
	keyID := sealed[0].Key
	for _, c := range []struct {
		what, role, stmt string
		args             []any
		states           []string
	}{
		{"the API deleting a key", "fyniq_api", `DELETE FROM fyniq.person_key`, nil, []string{"42501"}},
		{"the worker deleting a key", "fyniq_worker", `DELETE FROM fyniq.person_key`, nil, []string{"42501"}},
		{"the API changing a key", "fyniq_api", `UPDATE fyniq.person_key SET wrapped = wrapped`, nil, []string{"42501"}},
		{"the owner changing a key", "fyniq_migrator", `UPDATE fyniq.person_key SET cohort = cohort WHERE id = $1`, []any{keyID}, []string{"FY317"}},
		{"the owner deleting a key outside a run", "fyniq_migrator", `DELETE FROM fyniq.person_key WHERE id = $1`, []any{keyID}, []string{"FY317"}},
		{"a key for no one", "fyniq_api", `INSERT INTO fyniq.person_key (person, cohort, wrapped) VALUES ('Ada Tan', DATE '2026-12-31', '\x00112233445566778899aabbccddeeff')`,
			nil, []string{"23514"}},
		{"a second key for a person's year", "fyniq_api", `INSERT INTO fyniq.person_key (person, cohort, wrapped)
		   SELECT person, cohort, wrapped FROM fyniq.person_key WHERE id = $1`, []any{keyID}, []string{"23505"}},
		{"the read-only role reading keys", "fyniq_readonly", `SELECT wrapped FROM fyniq.person_key`, nil, []string{"42501"}},
	} {
		err := asOrg(ctx, conn(t, c.role), a.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.stmt, c.args...); return err })
		wantState(t, c.what, err, c.states...)
	}
	for _, role := range []string{"fyniq_worker", "fyniq_executor"} {
		if err := asOrg(ctx, conn(t, role), a.Org, func(tx pgx.Tx) error {
			var n int
			return tx.QueryRow(ctx, `SELECT count(*) FROM fyniq.person_key`).Scan(&n)
		}); err != nil {
			t.Errorf("%s reads no keys: %v", role, err)
		}
	}

	// Another organisation sees no key: its values do not open there.
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		o, err := u.Audit().OpenSealed(ctx, sealed[:1])
		if err == nil && !o[0].Shredded {
			t.Error("another organisation opened a value")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// Once the key is gone (as the shredding run deletes it), the values read as shredded and the chain still verifies.
	if err := asOrg(ctx, conn(t, "fyniq_migrator"), a.Org, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT set_config('fyniq.shredding_run', $1, true)`, uuid.NewString()); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM fyniq.person_key WHERE id = $1`, keyID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		opened, err = u.Audit().OpenSealed(ctx, sealed)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for i, o := range opened {
		switch {
		case sealed[i].Key == keyID && (!o.Shredded || o.Value != ""):
			t.Errorf("a value of the shredded year: %+v", o)
		case sealed[i].Key != keyID && (o.Shredded || o.Value == ""):
			t.Errorf("a value of the later year does not open: %+v", o)
		}
	}
	if _, err := audit.Chain(events(t, a.Org)); err != nil {
		t.Errorf("the chain after shredding: %v", err)
	}
}
