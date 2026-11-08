package balance

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The roles of the director's accounts, whose lines name their person (FR-EQ-01, B.7; ledger.RoleDirectorLoan and
// ledger.RoleDeferredRemuneration).
const (
	roleDirectorLoan         = "director-loan"
	roleDeferredRemuneration = "deferred-remuneration"
)

// DirectorAccounts are the chart's director's accounts (2500, 2120), in code order.
func DirectorAccounts(ctx context.Context, u storage.UnitOfWork) ([]storage.Account, error) {
	all, err := u.Accounts().List(ctx)
	if err != nil {
		return nil, err
	}
	var out []storage.Account
	for _, a := range all {
		if a.Role == roleDirectorLoan || a.Role == roleDeferredRemuneration {
			out = append(out, a)
		}
	}
	slices.SortFunc(out, func(a, b storage.Account) int { return cmp.Compare(a.Code, b.Code) })
	return out, nil
}

// PersonBalance is one person's balances on the director's accounts through a day, in the base currency and signed as
// the ledger holds them: a credit (negative) is what the company owes the person, a debit what they owe it.
type PersonBalance struct {
	// Party is the director or shareholder; uuid.Nil gathers lines that name no one, which only lines posted before
	// director's accounts named their person can be (the close names them, INV-07).
	Party    uuid.UUID
	Accounts map[uuid.UUID]money.Money // by director's account; only those with a line
	Net      money.Money               // their sum: a debit means the person owes the company (FR-EQ-11)
}

// PersonLine is a line on a director's account, with the account it is on.
type PersonLine struct {
	AccountID uuid.UUID
	storage.AccountLine
}

// People are each person's balances on the director's accounts through a day (FR-EQ-01, ENG-04), by person in the
// order their first line posted. The person is not in the cached balances, so the lines are read a period at a time,
// as Control reads a control account's, under the same limit (TooMany).
func People(ctx context.Context, u storage.UnitOfWork, base money.Currency, through time.Time) ([]PersonBalance, error) {
	if err := CheckDay(ctx, u, through); err != nil {
		return nil, err
	}
	accounts, err := DirectorAccounts(ctx, u)
	if err != nil {
		return nil, err
	}
	byParty := map[uuid.UUID]*PersonBalance{}
	var order []uuid.UUID
	for _, a := range accounts {
		err := controlLines(ctx, u, a.ID, through, func(l storage.AccountLine) error {
			b, err := money.ParseStored(string(l.BaseAmount), base)
			if err != nil {
				return err
			}
			party := uuid.Nil
			if l.PartyID != nil {
				party = *l.PartyID
			}
			p := byParty[party]
			if p == nil {
				p = &PersonBalance{Party: party, Accounts: map[uuid.UUID]money.Money{}, Net: money.Zero(base)}
				byParty[party] = p
				order = append(order, party)
			}
			sum, ok := p.Accounts[a.ID]
			if !ok {
				sum = money.Zero(base)
			}
			if p.Accounts[a.ID], err = sum.Add(b); err != nil {
				return err
			}
			p.Net, err = p.Net.Add(b)
			return err
		})
		if err != nil {
			return nil, err
		}
	}
	out := make([]PersonBalance, 0, len(order))
	for _, party := range order {
		out = append(out, *byParty[party])
	}
	return out, nil
}

// PersonLines are one person's lines on the director's accounts dated from..to, in posting order (date, then
// revision, then line): the movements of their statement (FR-EQ-01).
func PersonLines(ctx context.Context, u storage.UnitOfWork, party uuid.UUID, from, to time.Time) ([]PersonLine, error) {
	if err := CheckDay(ctx, u, from); err != nil {
		return nil, err
	}
	accounts, err := DirectorAccounts(ctx, u)
	if err != nil {
		return nil, err
	}
	var out []PersonLine
	for _, a := range accounts {
		err := controlLines(ctx, u, a.ID, to, func(l storage.AccountLine) error {
			if l.PostingDate.Before(from) || l.PartyID == nil || *l.PartyID != party {
				return nil
			}
			out = append(out, PersonLine{AccountID: a.ID, AccountLine: l})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	slices.SortStableFunc(out, func(a, b PersonLine) int {
		switch {
		case !a.PostingDate.Equal(b.PostingDate):
			return a.PostingDate.Compare(b.PostingDate)
		case a.LedgerRevision != b.LedgerRevision:
			return cmp.Compare(a.LedgerRevision, b.LedgerRevision)
		}
		return cmp.Compare(a.LineNo, b.LineNo)
	})
	return out, nil
}

// ApplicationMonies are what one subscriber's share application monies hold on account through a day (B.7): the credit
// balance of their lines, money the company holds for shares not yet allotted; zero when they have none. The lines are
// read as People reads a director's account's.
func ApplicationMonies(ctx context.Context, u storage.UnitOfWork, base money.Currency, account, subscriber uuid.UUID, through time.Time) (money.Money, error) {
	if err := CheckDay(ctx, u, through); err != nil {
		return money.Money{}, err
	}
	held := money.Zero(base)
	err := controlLines(ctx, u, account, through, func(l storage.AccountLine) error {
		if l.PartyID == nil || *l.PartyID != subscriber {
			return nil
		}
		b, err := money.ParseStored(string(l.BaseAmount), base)
		if err != nil {
			return err
		}
		held, err = held.Sub(b)
		return err
	})
	return held, err
}

// Instrument is what one SAFE or convertible note holds on account through a day (FR-EQ-18): the credit balance of its
// lines on convertible instruments, money the company received under it and has not converted or repaid; zero when it
// has none. The lines are read as ApplicationMonies reads a subscriber's.
func Instrument(ctx context.Context, u storage.UnitOfWork, base money.Currency, account, instrument uuid.UUID, through time.Time) (money.Money, error) {
	if err := CheckDay(ctx, u, through); err != nil {
		return money.Money{}, err
	}
	held := money.Zero(base)
	err := controlLines(ctx, u, account, through, func(l storage.AccountLine) error {
		if l.InstrumentID == nil || *l.InstrumentID != instrument {
			return nil
		}
		b, err := money.ParseStored(string(l.BaseAmount), base)
		if err != nil {
			return err
		}
		held, err = held.Sub(b)
		return err
	})
	return held, err
}
