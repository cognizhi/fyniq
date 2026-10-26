package claim

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
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

// The refusals of a claim category (ENG-23; FR-AP-15).
const (
	CodeCategoryName    = "CLAIM-20" // a category with no name, a name too long, or one another category in use has
	CodeCategoryAccount = "CLAIM-21" // an account a claim cannot cost: not an active, postable expense or asset account, or a money, control or system one
	CodeCategoryLimit   = "CLAIM-22" // a limit or threshold below zero, or with more decimals than the base currency has
	CodeStarterLoaded   = "CLAIM-23" // the organisation has categories already: the starter set loads only into none
)

// MaxCategoryName is the longest name a category keeps (db/migrations/000160).
const MaxCategoryName = 80

// Problem is one reason a category cannot be saved: its stable code, the field, what is wrong and how to fix it.
type Problem struct {
	Code    string
	Field   string
	Message string
	Fix     string
}

// CheckCategory is every reason a category as entered cannot be saved, as problems with their fields (ENG-22): its
// name against the organisation's other categories in use (self, being changed, aside), its account (nil: none of the
// organisation's), and its limits, which are amounts in base.
func CheckCategory(n storage.NewClaimCategory, acc *storage.Account, base money.Currency, others []storage.ClaimCategory, self uuid.UUID) []Problem {
	var out []Problem
	add := func(code, field, msg, fix string) { out = append(out, Problem{code, field, msg, fix}) }
	name := strings.TrimSpace(n.Name)
	switch {
	case name == "":
		add(CodeCategoryName, "name", "the category has no name", "Name what its claims are for, such as Local travel")
	case len(name) > MaxCategoryName:
		add(CodeCategoryName, "name", fmt.Sprintf("the name has %d characters; the most is %d", len(name), MaxCategoryName), "Shorten it")
	case n.Active:
		for _, o := range others {
			if o.Active && o.ID != self && strings.EqualFold(strings.TrimSpace(o.Name), name) {
				add(CodeCategoryName, "name", "another category in use is called "+o.Name, "Choose another name, or change that category")
				break
			}
		}
	}
	if acc == nil {
		add(CodeCategoryAccount, "account_id", "the account does not exist in this organisation", "Choose an expense or asset account from the chart")
	} else if why := CheckAccount(*acc); why != "" {
		add(CodeCategoryAccount, "account_id", why, "Choose an expense or asset account that is not a money, control or system account")
	}
	for _, x := range []struct {
		field string
		v     *storage.Decimal
	}{{"limit", n.LineLimit}, {"receipt_above", n.ReceiptAbove}, {"attendees_above", n.AttendeesAbove}} {
		if x.v == nil {
			continue
		}
		m, err := money.ParseField(string(*x.v), base, x.field)
		switch {
		case err != nil:
			add(CodeCategoryLimit, x.field, fmt.Sprintf("%q is not an amount in %s", *x.v, base),
				fmt.Sprintf("Enter it in %s with at most %d decimals", base, base.MinorUnits))
		case m.Sign() < 0 || x.field == "limit" && m.IsZero():
			add(CodeCategoryLimit, x.field, fmt.Sprintf("it is %s %s", m, base), "Enter an amount above zero, or leave it out for none")
		}
	}
	return out
}
