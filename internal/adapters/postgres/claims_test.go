//go:build local

package postgres_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Claim categories keep the expense policy honest (migration 000160): a category costs an active, postable expense or
// asset account that is not a money, control or system account, in the base currency; one live category per name and
// one live mileage category; it keeps its mileage flag and currency, changes one version at a time and is never
// deleted. Every change is audited; another organisation sees none of it.
// req: FR-AP-15
// req: INV-06
// req: QA-ENG-05
// asvs: V8.4.1, V2.3.2
func TestClaimCategoriesKeepTheirPolicy(t *testing.T) {
	ctx := context.Background()
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	a := books(t, "Claims organisation A Pte Ltd")
	b := books(t, "Claims organisation B Pte Ltd")
	st := store(t, "fyniq_api")
	do := func(fn func(ctx context.Context, u storage.UnitOfWork) error) error { return st.Do(ctx, a.Org, fn) }
	limit, offScale := storage.Decimal("200.00"), storage.Decimal("10.005")

	var money, control, payable, equipment storage.Account
	if err := do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if money, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "1010", Name: "DBS current", Type: storage.Asset,
			AccountAttributes: storage.AccountAttributes{MoneyAccount: true, Currency: "SGD"}}); err != nil {
			return err
		}
		if control, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "1100", Name: "Trade debtors", Type: storage.Asset,
			AccountAttributes: storage.AccountAttributes{Control: true}}); err != nil {
			return err
		}
		if payable, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "2150", Name: "Expense claims payable", Type: storage.Liability,
			Role: "expense-claims-payable"}); err != nil {
			return err
		}
		equipment, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "1510", Name: "IT equipment", Type: storage.Asset})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// Two categories: meals with a limit, and mileage; an asset account can be costed too.
	var meals, mileage storage.ClaimCategory
	if err := do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if meals, err = u.ClaimCategories().Create(ctx, storage.NewClaimCategory{Name: "Meals", AccountID: a.Rent.ID, Currency: "SGD",
			LineLimit: &limit, Active: true, At: now}); err != nil {
			return err
		}
		if mileage, err = u.ClaimCategories().Create(ctx, storage.NewClaimCategory{Name: "Mileage", AccountID: a.Rent.ID, Mileage: true,
			Currency: "SGD", Active: true, At: now}); err != nil {
			return err
		}
		_, err = u.ClaimCategories().Create(ctx, storage.NewClaimCategory{Name: "Equipment", AccountID: equipment.ID, Currency: "SGD",
			Active: true, At: now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if meals.Version != 1 || meals.LineLimit == nil || *meals.LineLimit != "200.0000" || !mileage.Mileage {
		t.Fatalf("created: %+v %+v", meals, mileage)
	}

	// Changed one version at a time; a stale version is refused.
	if err := do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		meals, err = u.ClaimCategories().Change(ctx, meals.ID, meals.Version, storage.NewClaimCategory{Name: "Meals & entertainment",
			AccountID: a.Rent.ID, Currency: "SGD", Active: true, At: now})
		return err
	}); err != nil || meals.Version != 2 || meals.LineLimit != nil {
		t.Fatalf("change a category: %+v, %v", meals, err)
	}
	if err := do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.ClaimCategories().Change(ctx, meals.ID, 1, storage.NewClaimCategory{Name: "Meals", AccountID: a.Rent.ID, Active: true, At: now})
		return err
	}); storage.CodeOf(err) != storage.CodeVersion {
		t.Fatalf("a stale change: %v", err)
	}

	// Refused: a money, control, system, group or revenue account; another currency; a second live name or mileage category.
	for _, x := range []struct {
		what string
		n    storage.NewClaimCategory
		code string
	}{
		{"a money account", storage.NewClaimCategory{Name: "Bank", AccountID: money.ID, Currency: "SGD"}, storage.CodeClaim},
		{"a control account", storage.NewClaimCategory{Name: "Debtors", AccountID: control.ID, Currency: "SGD"}, storage.CodeClaim},
		{"a system account", storage.NewClaimCategory{Name: "Claims", AccountID: payable.ID, Currency: "SGD"}, storage.CodeClaim},
		{"a group account", storage.NewClaimCategory{Name: "Expenses", AccountID: a.Expenses.ID, Currency: "SGD"}, storage.CodeClaim},
		{"a revenue account", storage.NewClaimCategory{Name: "Sales", AccountID: a.Sales.ID, Currency: "SGD"}, storage.CodeClaim},
		{"limits in USD", storage.NewClaimCategory{Name: "Travel", AccountID: a.Rent.ID, Currency: "USD"}, storage.CodeClaim},
		{"a second live name", storage.NewClaimCategory{Name: " meals & ENTERTAINMENT ", AccountID: a.Rent.ID, Currency: "SGD"}, storage.CodeInvalid},
		{"a second mileage category", storage.NewClaimCategory{Name: "Car", AccountID: a.Rent.ID, Mileage: true, Currency: "SGD"}, storage.CodeInvalid},
		{"a limit off the currency's scale", storage.NewClaimCategory{Name: "Taxi", AccountID: a.Rent.ID, Currency: "SGD",
			LineLimit: &offScale}, storage.CodeInvalid},
	} {
		x.n.Active, x.n.At = true, now
		if err := do(func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.ClaimCategories().Create(ctx, x.n)
			return err
		}); storage.CodeOf(err) != x.code {
			t.Errorf("%s: %v, want %s", x.what, err, x.code)
		}
	}

	if got := actions(t, a.Org, meals.ID); !slices.Contains(got, "claim_category.created") || !slices.Contains(got, "claim_category.changed") {
		t.Errorf("audit: %v", got)
	}
	for _, x := range []struct{ role, what, stmt, state string }{
		{"fyniq_migrator", "delete a category", "DELETE FROM fyniq.claim_category WHERE id = $1", "FY232"},
		{"fyniq_migrator", "turn a category into mileage", "UPDATE fyniq.claim_category SET mileage = NOT mileage, version = version + 1 WHERE id = $1", "FY232"},
		{"fyniq_migrator", "skip a version", "UPDATE fyniq.claim_category SET name = 'Meals', version = version + 2 WHERE id = $1", "FY232"},
		{"fyniq_api", "change a category's mileage flag as the API", "UPDATE fyniq.claim_category SET mileage = true WHERE id = $1", "42501"},
		{"fyniq_api", "delete a category as the API", "DELETE FROM fyniq.claim_category WHERE id = $1", "42501"},
	} {
		err := asOrg(ctx, conn(t, x.role), a.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, x.stmt, meals.ID); return err })
		wantState(t, x.what, err, x.state)
	}
	if n := count(t, b.Org, "claim_category"); n != 0 {
		t.Errorf("organisation B counts %d categories", n)
	}
}
