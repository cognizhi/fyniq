package api

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ what an approval would post (FR-APR-02, rev 0.177)
//
// While a request waits, the approver sees what approving it would do to the ledger now: its change is carried out in a
// unit of work that is rolled back, as Prefer: return=preview does, and the entries it posted, the balances of the
// accounts they post to before and after, and whatever would refuse it are read before the rollback. Nothing is kept.

// ApprovalImpact is what approving a request would post now (FR-APR-02).
type ApprovalImpact struct {
	Entries  []Entry         `json:"entries" nullable:"false" doc:"The entries approving it would post now, debits positive: its ledger impact."`
	Balances []ImpactBalance `json:"balances" nullable:"false" doc:"Each account and currency those entries post to, with its balance through their latest date before and after."`
	Refused  *ImpactRefusal  `json:"refused,omitempty" doc:"What would refuse it if it were approved now, such as a check that fails or a month closed since it was asked for; absent when nothing would."`
}

// ImpactBalance is an account's balance in a currency before and after the entries.
type ImpactBalance struct {
	AccountID  string `json:"account_id" format:"uuid" doc:"The account." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d60"`
	Code       string `json:"code" doc:"Its code." example:"6010"`
	Name       string `json:"name" doc:"Its name." example:"Rent"`
	Through    string `json:"through" format:"date" doc:"The day the balances are through: the entries' latest date." example:"2026-10-31"`
	Before     Money  `json:"before" doc:"Its balance in the currency before, debit positive."`
	After      Money  `json:"after" doc:"After the entries."`
	BaseBefore Money  `json:"base_before" doc:"Its value in the base currency before."`
	BaseAfter  Money  `json:"base_after" doc:"After the entries."`
}

// ImpactRefusal is what would refuse a request now.
type ImpactRefusal struct {
	Code    string `json:"code" doc:"The rule." example:"STORAGE-INV-03"`
	Message string `json:"message" doc:"What would stop it, in plain words." example:"The posting date is in a closed period."`
	Fix     string `json:"fix,omitempty" doc:"What to do." example:"Ask for the period to be reopened first."`
}

// impactOf works out what approving request id would post now, as p sees it, in a unit of work that is rolled back:
// nil for a request that is not pending, or whose change posts nothing and is not refused.
func (a *API) impactOf(ctx context.Context, p authn.Principal, id uuid.UUID) (*ApprovalImpact, error) {
	var out *ApprovalImpact
	refuse := func(err error) {
		pr := problemFrom(ctx, err)
		out = &ApprovalImpact{Entries: []Entry{}, Balances: []ImpactBalance{}, Refused: &ImpactRefusal{Code: pr.Code, Message: pr.Detail, Fix: pr.Fix}}
	}
	err := a.deps.Store.Do(storage.WithPreview(ctx), p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
		r, err := u.Approvals().Get(ctx, id)
		if err != nil || r.Status != string(approval.Pending) {
			return err
		}
		s, err := storedOf(r)
		if err != nil {
			return err
		}
		rev, err := u.Ledger().Revision(ctx)
		if err != nil {
			return err
		}
		if err := a.executable(ctx, u, r, s, p.UserID); err != nil {
			refuse(err)
			return errRefused
		}
		if err := a.execute(ctx, u, r, s, p.UserID); err != nil {
			refuse(err) // the database's refusal ends the transaction: it rolls back at once
			return errRefused
		}
		entries, err := u.Ledger().Entries(ctx, rev, 100)
		if err != nil || len(entries) == 0 {
			return err
		}
		out, err = impactOfEntries(ctx, u, entries)
		return err
	})
	if errors.Is(err, storage.ErrPreviewed) || errors.Is(err, errRefused) {
		err = nil
	}
	return out, err
}

// errRefused ends the rolled-back unit of work of a change that would be refused, which impactOf has recorded.
var errRefused = errors.New("api: the change would be refused")

// impactOfEntries are entries' lines and the balances of what they post to, before and after, through their latest date.
func impactOfEntries(ctx context.Context, u storage.UnitOfWork, entries []storage.Entry) (*ApprovalImpact, error) {
	o, err := u.Organisation(ctx)
	if err != nil {
		return nil, err
	}
	base, err := money.Lookup(o.BaseCurrency)
	if err != nil {
		return nil, err
	}
	out := &ApprovalImpact{Entries: []Entry{}, Balances: []ImpactBalance{}}
	type key struct {
		account  uuid.UUID
		currency string
	}
	var keys []key
	moved := map[key][2]money.Money{}
	var through time.Time
	for _, e := range entries {
		out.Entries = append(out.Entries, entryOf(e))
		if e.PostingDate.After(through) {
			through = e.PostingDate
		}
		for _, l := range e.Lines {
			cur, err := money.Lookup(l.Currency)
			if err != nil {
				return nil, err
			}
			amount, err := money.ParseStored(string(l.Amount), cur)
			if err != nil {
				return nil, err
			}
			value, err := money.ParseStored(string(l.BaseAmount), base)
			if err != nil {
				return nil, err
			}
			k := key{l.AccountID, l.Currency}
			sum, ok := moved[k]
			if !ok {
				keys = append(keys, k)
				sum = [2]money.Money{money.Zero(cur), money.Zero(base)}
			}
			if sum[0], err = sum[0].Add(amount); err != nil {
				return nil, err
			}
			if sum[1], err = sum[1].Add(value); err != nil {
				return nil, err
			}
			moved[k] = sum
		}
	}
	for _, k := range keys {
		acc, err := u.Accounts().Get(ctx, k.account)
		if err != nil {
			return nil, err
		}
		cur, err := money.Lookup(k.currency)
		if err != nil {
			return nil, err
		}
		after, afterBase, err := balance.Through(ctx, u, k.account, cur, base, through)
		if err != nil {
			return nil, err
		}
		before, err := after.Sub(moved[k][0])
		if err != nil {
			return nil, err
		}
		beforeBase, err := afterBase.Sub(moved[k][1])
		if err != nil {
			return nil, err
		}
		out.Balances = append(out.Balances, ImpactBalance{AccountID: k.account.String(), Code: acc.Code, Name: acc.Name, Through: through.Format(time.DateOnly),
			Before: amountOf(before), After: amountOf(after), BaseBefore: amountOf(beforeBase), BaseAfter: amountOf(afterBase)})
	}
	slices.SortStableFunc(out.Balances, func(x, y ImpactBalance) int {
		return cmp.Or(cmp.Compare(x.Code, y.Code), cmp.Compare(x.Before.Currency, y.Before.Currency))
	})
	return out, nil
}
