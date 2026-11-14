package differential

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// memStore is the database the statements and the KPIs read, held in memory for a scenario (README, "Statements" and
// "KPIs"): its chart with the accounts' attributes, a calendar month per period, and the entries the engine posted,
// so financials.Lay and kpi.Read run on the engine's own figures. It embeds the storage ports and overrides only what
// they read: any other call panics, because they never make it.
type memStore struct {
	storage.UnitOfWork
	base     money.Currency
	accounts []storage.Account
	byCode   map[string]storage.Account
	periods  []storage.Period
	entries  []storage.Entry
	issues   []storage.ShareIssue
	settings map[string][]storage.SettingChange
	inputs   []storage.KPIInput
}

func (m *memStore) Organisation(context.Context) (storage.Organisation, error) {
	return storage.Organisation{BaseCurrency: m.base.Code}, nil // no financial years set: nothing folds
}
func (m *memStore) Accounts() storage.Accounts       { return memAccounts{m: m} }
func (m *memStore) Periods() storage.Periods         { return memPeriods{m: m} }
func (m *memStore) Ledger() storage.Ledger           { return memLedger{m: m} }
func (m *memStore) Balances() storage.Balances       { return memBalances{m: m} }
func (m *memStore) ShareIssues() storage.ShareIssues { return memIssues{m: m} }
func (m *memStore) Settings() storage.Settings       { return memSettings{m: m} }
func (m *memStore) KPIInputs() storage.KPIInputs     { return memInputs{m: m} }
func (m *memStore) Retention() storage.Retention     { return memRetention{} }

// memRetention is a ledger never purged after retention (AUD-10): the balance service reads every date.
type memRetention struct{ storage.Retention }

func (memRetention) LedgerBoundaries(context.Context) ([]storage.LedgerBoundary, error) {
	return nil, nil
}

type memAccounts struct {
	storage.Accounts
	m *memStore
}

func (a memAccounts) List(context.Context) ([]storage.Account, error) {
	return slices.Clone(a.m.accounts), nil
}

func (a memAccounts) ByCode(_ context.Context, code string) (storage.Account, error) {
	if x, ok := a.m.byCode[code]; ok {
		return x, nil
	}
	return storage.Account{}, ports.ErrNotFound
}

func (a memAccounts) Get(_ context.Context, id uuid.UUID) (storage.Account, error) {
	for _, x := range a.m.accounts {
		if x.ID == id {
			return x, nil
		}
	}
	return storage.Account{}, ports.ErrNotFound
}

type memPeriods struct {
	storage.Periods
	m *memStore
}

func (p memPeriods) List(context.Context) ([]storage.Period, error) {
	return slices.Clone(p.m.periods), nil
}

func (p memPeriods) Get(_ context.Context, id uuid.UUID) (storage.Period, error) {
	for _, x := range p.m.periods {
		if x.ID == id {
			return x, nil
		}
	}
	return storage.Period{}, ports.ErrNotFound
}

type memLedger struct {
	storage.Ledger
	m *memStore
}

func (l memLedger) Entries(_ context.Context, after int64, limit int) ([]storage.Entry, error) {
	var out []storage.Entry
	for _, e := range l.m.entries {
		if e.LedgerRevision > after && len(out) < limit {
			out = append(out, e)
		}
	}
	return out, nil
}

func (l memLedger) EntriesOfPeriod(_ context.Context, period uuid.UUID, after int64, limit int) ([]storage.Entry, error) {
	var out []storage.Entry
	for _, e := range l.m.entries {
		if e.PeriodID == period && e.LedgerRevision > after && len(out) < limit {
			out = append(out, e)
		}
	}
	return out, nil
}

func (l memLedger) Revision(context.Context) (int64, error) { return int64(len(l.m.entries)), nil }

func (l memLedger) AccountLinesAfter(ctx context.Context, account uuid.UUID, from, to time.Time, after *storage.LineKey,
	limit int) ([]storage.AccountLine, error) {
	all, err := l.AccountLines(ctx, account, from, to, math.MaxInt)
	if err != nil {
		return nil, err
	}
	key := func(k storage.LineKey) string {
		return fmt.Sprintf("%s|%020d|%08d", k.PostingDate.Format(time.DateOnly), k.Revision, k.LineNo)
	}
	slices.SortFunc(all, func(a, b storage.AccountLine) int { return strings.Compare(key(a.Key()), key(b.Key())) })
	out := []storage.AccountLine{}
	for _, x := range all {
		if (after == nil || key(x.Key()) > key(*after)) && len(out) < limit {
			out = append(out, x)
		}
	}
	return out, nil
}

func (l memLedger) OfSourceType(_ context.Context, sourceType string) ([]storage.Entry, error) {
	var out []storage.Entry
	for _, e := range l.m.entries {
		if e.Source.Type == sourceType {
			out = append(out, e)
		}
	}
	return out, nil
}

func (l memLedger) AccountLines(_ context.Context, account uuid.UUID, from, to time.Time, limit int) ([]storage.AccountLine, error) {
	var out []storage.AccountLine
	for _, e := range l.m.entries {
		if e.PostingDate.Before(from) || e.PostingDate.After(to) {
			continue
		}
		for _, x := range e.Lines {
			if x.AccountID == account && len(out) < limit {
				out = append(out, storage.AccountLine{EntryID: e.ID, LedgerRevision: e.LedgerRevision, PostingDate: e.PostingDate, Source: e.Source,
					LineNo: x.LineNo, Currency: x.Currency, Amount: x.Amount, BaseAmount: x.BaseAmount, PartyID: x.PartyID})
			}
		}
	}
	return out, nil
}

type memBalances struct {
	storage.Balances
	m *memStore
}

func (b memBalances) ForPeriod(ctx context.Context, period uuid.UUID) ([]storage.Balance, error) {
	return b.ForPeriodAt(ctx, period, int64(len(b.m.entries)))
}

// ForPeriodAt sums the period's lines by account and currency, as the materialised balances hold them (ENG-04).
func (b memBalances) ForPeriodAt(_ context.Context, period uuid.UUID, revision int64) ([]storage.Balance, error) {
	type key struct {
		account uuid.UUID
		cur     string
	}
	sums := map[key]*[2]money.Money{}
	counts := map[key]int64{}
	var order []key
	for _, e := range b.m.entries {
		if e.PeriodID != period || e.LedgerRevision > revision {
			continue
		}
		for _, x := range e.Lines {
			cur, err := money.Lookup(x.Currency)
			if err != nil {
				return nil, err
			}
			amount, err := money.ParseStored(string(x.Amount), cur)
			if err != nil {
				return nil, err
			}
			base, err := money.ParseStored(string(x.BaseAmount), b.m.base)
			if err != nil {
				return nil, err
			}
			k := key{x.AccountID, x.Currency}
			s, ok := sums[k]
			if !ok {
				s = &[2]money.Money{money.Zero(cur), money.Zero(b.m.base)}
				sums[k] = s
				order = append(order, k)
			}
			if s[0], err = s[0].Add(amount); err != nil {
				return nil, err
			}
			if s[1], err = s[1].Add(base); err != nil {
				return nil, err
			}
			counts[k]++
		}
	}
	out := make([]storage.Balance, 0, len(order))
	for _, k := range order {
		s := sums[k]
		for _, x := range s {
			if _, err := money.ParseStored(x.String(), x.Currency()); err != nil {
				return nil, b.beyond(k.account, period, x)
			}
		}
		out = append(out, storage.Balance{AccountID: k.account, PeriodID: period, Currency: k.cur, Amount: storage.Decimal(s[0].String()),
			BaseAmount: storage.Decimal(s[1].String()), LineCount: counts[k]})
	}
	return out, nil
}

// beyond says which balance the database could not hold: it refuses the posting that would take an account's balance
// for a period to 10^16, which neither side models, so a scenario asking for the statements keeps below it (README,
// "Balances within NUMERIC(20,4)").
func (b memBalances) beyond(account, period uuid.UUID, x money.Money) error {
	code, month := account.String(), period.String()
	if a, err := (memAccounts{m: b.m}).Get(context.Background(), account); err == nil {
		code = a.Code
	}
	for _, p := range b.m.periods {
		if p.ID == period {
			month = p.StartsOn.Format("2006-01")
		}
	}
	return fmt.Errorf("account %s's balance for %s is %s %s, which NUMERIC(20,4) cannot hold: the database refuses the posting "+
		"that takes it there, so the scenario is outside the format (README, \"Balances within NUMERIC(20,4)\")", code, month, x, x.Currency())
}

type memIssues struct {
	storage.ShareIssues
	m *memStore
}

func (s memIssues) List(context.Context) ([]storage.ShareIssue, error) {
	return slices.Clone(s.m.issues), nil
}

type memSettings struct {
	storage.Settings
	m *memStore
}

func (s memSettings) Changes(_ context.Context, key string) ([]storage.SettingChange, error) {
	return slices.Clone(s.m.settings[key]), nil
}

type memInputs struct {
	storage.KPIInputs
	m *memStore
}

func (k memInputs) Range(_ context.Context, from, to time.Time) ([]storage.KPIInput, error) {
	var out []storage.KPIInput
	for _, x := range k.m.inputs {
		if !x.Month.Before(from) && !x.Month.After(to) {
			out = append(out, x)
		}
	}
	return out, nil
}
