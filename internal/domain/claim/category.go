package claim

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Starter are the categories an organisation starts with when it loads the standard chart (p1-expense-claims design
// D6), each on the standard chart's account of its code, with no limits until the organisation sets them.
var Starter = []struct {
	Name    string
	Code    string
	Mileage bool
}{
	{"Local travel", "6510", false},
	{"Overseas travel", "6520", false},
	{"Meals & entertainment", "6530", false},
	{"Software & subscriptions", "6210", false},
	{"Telephone & internet", "6330", false},
	{"Office supplies", "6340", false},
	{"Mileage", "6510", true},
}

// LoadStarter adds the starter categories to an organisation that has none, each whose account the chart has and a
// claim can cost, and says how many it added. An organisation with categories keeps its own.
func LoadStarter(ctx context.Context, u storage.UnitOfWork, at time.Time) (int, error) {
	have, err := u.ClaimCategories().List(ctx)
	if err != nil || len(have) > 0 {
		return 0, err
	}
	org, err := u.Organisation(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, s := range Starter {
		acc, err := u.Accounts().ByCode(ctx, s.Code)
		if errors.Is(err, ports.ErrNotFound) {
			continue
		}
		if err != nil {
			return n, err
		}
		if CheckAccount(acc) != "" {
			continue
		}
		if _, err := u.ClaimCategories().Create(ctx, storage.NewClaimCategory{Name: s.Name, AccountID: acc.ID, Mileage: s.Mileage,
			Currency: org.BaseCurrency, Active: true, At: at}); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// CheckAccount says why a claim category cannot cost acc, in plain words, or "" when it can: a claim costs an active,
// postable expense or asset account, never a money, control or system account (FR-AP-15; the database's FY232).
func CheckAccount(acc storage.Account) string {
	switch {
	case acc.Group:
		return fmt.Sprintf("account %s is a group heading", acc.Code)
	case !acc.Active:
		return fmt.Sprintf("account %s is inactive", acc.Code)
	case acc.MoneyAccount:
		return fmt.Sprintf("account %s is a money account, which money moves through", acc.Code)
	case acc.Control:
		return fmt.Sprintf("account %s is a control account with a sub-ledger", acc.Code)
	case acc.Role != "":
		return fmt.Sprintf("account %s is a system account (%s) that rules post to by themselves", acc.Code, acc.Role)
	case acc.Type != storage.Expense && acc.Type != storage.Asset:
		return fmt.Sprintf("account %s is a %s account, not an expense or asset", acc.Code, acc.Type)
	}
	return ""
}
