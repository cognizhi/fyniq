//go:build local

package postgres_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The option pool and its grants keep their rules whoever writes them (FR-EQ-19; FY293, FY249): a resolution's or
// grant's own keys only, a whole size or number above zero, an exercise price from zero with at most 6 decimals, a
// holder marked investor or shareholder and a share-capital class. A grant is recorded only with a recorded
// resolution of its class in force on its grant date, and the pool covers the grants on every day after: a grant past
// what the pool leaves, an earlier-dated one that shorts a later day and a smaller resolution are refused, and of two
// grants recorded at once that both need the last of the pool, the second waits for the first and is refused. The
// holder of a recorded grant stays marked.
// req: FR-EQ-19
// req: QA-ENG-05
// asvs: V8.2.2
func TestAttackOptionsKeepTheirRules(t *testing.T) {
	ctx := context.Background()
	d := newDirectorBooks(t, "Attack options Pte Ltd")
	class := shareCapital(t, d)
	api := store(t, "fyniq_api")
	create := func(typ storage.DocumentType, payload string) (storage.Document, error) {
		var doc storage.Document
		err := api.Do(ctx, d.org, func(ctx context.Context, u storage.UnitOfWork) error {
			var err error
			doc, err = u.Documents().Create(ctx, storage.NewDocument{Type: typ, Payload: []byte(payload)})
			return err
		})
		return doc, err
	}
	record := func(id uuid.UUID) error {
		return api.Do(ctx, d.org, func(ctx context.Context, u storage.UnitOfWork) error {
			return u.Documents().SetStatus(ctx, id, storage.DocumentPosted)
		})
	}
	pool := func(size, on string, account fmt.Stringer) string {
		return fmt.Sprintf(`{"date":"2026-11-07","size":%q,"class_account_id":%q,"resolution_ref":"Ordinary resolution","resolved_on":%q}`, size, account, on)
	}
	grant := func(holder fmt.Stringer, number, price, on string, account fmt.Stringer) string {
		return fmt.Sprintf(`{"date":"2026-11-07","holder_id":%q,"number":%q,"exercise_price":%q,"currency":"SGD","granted_on":%q,"cliff_months":12,`+
			`"vesting_months":48,"acceleration":"none","expires_on":"2036-01-01","class_account_id":%q}`, holder, number, price, on, account)
	}
	for name, payload := range map[string]string{
		"a key of its own":               strings.Replace(pool("100", "2026-01-02", class), `"date"`, `"approved":true,"date"`, 1),
		"no shares":                      pool("0", "2026-01-02", class),
		"a fraction of a share":          pool("10.5", "2026-01-02", class),
		"a class on the director's loan": pool("100", "2026-01-02", d.loan),
	} {
		if _, err := create(storage.DocumentOptionPool, payload); storage.CodeOf(err) != storage.CodeOption {
			t.Errorf("a pool with %s: %v", name, err)
		}
	}
	for name, payload := range map[string]string{
		"a key of its own":               strings.Replace(grant(d.director.ID, "10", "0.10", "2026-06-01", class), `"date"`, `"vested":"10","date"`, 1),
		"a fraction of an option":        grant(d.director.ID, "1.5", "0.10", "2026-06-01", class),
		"a price of 7 decimals":          grant(d.director.ID, "10", "0.1234567", "2026-06-01", class),
		"a negative price":               grant(d.director.ID, "10", "-0.10", "2026-06-01", class),
		"a customer as its holder":       grant(d.customer.ID, "10", "0.10", "2026-06-01", class),
		"a class on the director's loan": grant(d.director.ID, "10", "0.10", "2026-06-01", d.loan),
	} {
		if _, err := create(storage.DocumentOptionGrant, payload); storage.CodeOf(err) != storage.CodeOption {
			t.Errorf("a grant with %s: %v", name, err)
		}
	}
	drafted := func(typ storage.DocumentType, payload string) uuid.UUID {
		t.Helper()
		doc, err := create(typ, payload)
		if err != nil {
			t.Fatalf("drafting %s: %v", payload, err)
		}
		return doc.ID
	}
	early := drafted(storage.DocumentOptionGrant, grant(d.director.ID, "10", "0.10", "2026-06-01", class))
	if err := record(early); storage.CodeOf(err) != storage.CodeOption {
		t.Errorf("a grant with no pool: %v", err)
	}
	if err := record(drafted(storage.DocumentOptionPool, pool("100", "2026-01-02", class))); err != nil {
		t.Fatalf("recording the pool of 100: %v", err)
	}
	if err := record(drafted(storage.DocumentOptionGrant, grant(d.director.ID, "60", "0.10", "2026-06-01", class))); err != nil {
		t.Fatalf("a grant of 60 on 1 Jun: %v", err)
	}
	other := func() uuid.UUID {
		var id uuid.UUID
		if err := api.Do(ctx, d.org, func(ctx context.Context, u storage.UnitOfWork) error {
			a, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "3020", Name: "Preference share capital", Type: storage.Equity, Role: storage.RoleShareCapital})
			id = a.ID
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return id
	}()
	for name, payload := range map[string]string{
		"50 more on 1 Jun, past the pool":    grant(d.director.ID, "50", "0.10", "2026-06-01", class),
		"50 on 1 Feb, shorting 1 Jun":        grant(d.director.ID, "50", "0.10", "2026-02-01", class),
		"10 over a class the pool is not of": grant(d.director.ID, "10", "0.10", "2026-06-01", other),
		"10 before the pool's resolution":    grant(d.director.ID, "10", "0.10", "2026-01-01", class),
	} {
		if err := record(drafted(storage.DocumentOptionGrant, payload)); storage.CodeOf(err) != storage.CodeOption {
			t.Errorf("recording a grant of %s: %v", name, err)
		}
	}
	if err := record(drafted(storage.DocumentOptionPool, pool("50", "2026-03-01", class))); storage.CodeOf(err) != storage.CodeOption {
		t.Errorf("a resolution of 50 on 1 Mar, short of June's 60: %v", err)
	}

	// Two grants of 30 recorded at once, with 40 left: the second waits for the first's lock on the pool, then is refused.
	first := drafted(storage.DocumentOptionGrant, grant(d.director.ID, "30", "0.10", "2026-06-01", class))
	second := drafted(storage.DocumentOptionGrant, grant(d.director.ID, "30", "0.10", "2026-06-01", class))
	started, release, done := held(api, d.org, func(ctx context.Context, u storage.UnitOfWork) error {
		return u.Documents().SetStatus(ctx, first, storage.DocumentPosted)
	})
	if err := <-started; err != nil {
		t.Fatalf("recording the first 30: %v", err)
	}
	later := async(api, d.org, func(ctx context.Context, u storage.UnitOfWork) error {
		return u.Documents().SetStatus(ctx, second, storage.DocumentPosted)
	})
	stillBlocked(t, "recording the second 30", later)
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("the first 30 committed: %v", err)
	}
	if err := <-later; storage.CodeOf(err) != storage.CodeOption {
		t.Errorf("the second 30, once the first took the room: %v", err)
	}

	unmark := api.Do(ctx, d.org, func(ctx context.Context, u storage.UnitOfWork) error {
		c, err := u.Contacts().Get(ctx, d.director.ID)
		if err != nil {
			return err
		}
		det := c.ContactDetails
		det.Shareholder, det.Investor = false, false
		_, err = u.Contacts().Update(ctx, c.ID, c.Version, det, true)
		return err
	})
	if storage.CodeOf(unmark) != storage.CodeParty {
		t.Errorf("unmarking the holder of a recorded grant: %v", unmark)
	}
}
