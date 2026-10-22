//go:build local

package seedload_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/adapters/postgres/pgtest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/seed"
	"github.com/cognizhi/fyniq/internal/seedload"
)

// The sample company loads into an organisation through the engine, its share-capital accounts marked (INV-13) and the
// standard chart's system roles on the accounts with their codes, and its trial balance equals the seed's own totals; a
// second load into the same organisation is refused rather than mixed in (QA-LOCAL-01, DEV-04).
// req: DEV-04
// req: QA-LOCAL-01
// req: INV-13
func TestTheSampleCompanyLoadsAndBalances(t *testing.T) {
	ctx := context.Background()
	db, err := pgtest.Start(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "the load test needs Docker:", err)
		t.Fatal(err)
	}
	t.Cleanup(db.Stop)
	store, err := postgres.Open(ctx, db.URL("fyniq_api"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	org, err := store.CreateOrganisation(ctx, storage.NewOrganisation{Name: "Cognizhi Pte Ltd", BaseCurrency: "SGD"})
	if err != nil {
		t.Fatal(err)
	}
	vocab, err := seed.LoadVocabulary("../../docs/design/screens/sample-data.json")
	if err != nil {
		t.Fatal(err)
	}
	company := seed.Generate(vocab, 2, 120)
	r, err := seedload.Load(ctx, store, org.ID, company)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if r.Accounts != len(company.Accounts) || r.Periods != 12 || r.Journals != 120 {
		t.Fatalf("Load = %+v", r)
	}
	if err := store.Do(ctx, org.ID, func(ctx context.Context, u storage.UnitOfWork) error {
		a, err := u.Accounts().ByCode(ctx, "3010")
		if err == nil && a.Role != storage.RoleShareCapital {
			t.Errorf("3010 is not marked share capital: %+v", a)
		}
		if err != nil {
			return err
		}
		// The standard chart's system roles, so a bill finds 2010 and 2200 (FR-COA-03).
		for code, role := range map[string]storage.AccountRole{"2010": "accounts-payable", "2200": "gst-control", "7030": "fx-rounding", "1100": "accounts-receivable"} {
			if a, err := u.Accounts().ByCode(ctx, code); err != nil || a.Role != role {
				t.Errorf("%s: want the role %s, got %+v %v", code, role, a, err)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := seedload.Load(ctx, store, org.ID, company); err == nil || !strings.Contains(err.Error(), "chart of accounts already") {
		t.Fatalf("a second load: %v", err)
	}
}
