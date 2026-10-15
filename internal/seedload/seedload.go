// Package seedload loads the sample company (DEV-04, internal/seed) into an organisation of a
// database, the way the API writes: through the storage port and the journal service, so every change
// is audited and every journal is posted by the engine under its rules. It then checks the trial
// balance against the seed's own amounts, summed in integer cents without the engine: the golden check
// of QA-LOCAL-01. Local and test tooling: the sample company is synthetic (DEV-08).
package seedload

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/seed"
)

// Result is what a load wrote.
type Result struct {
	Accounts int
	Periods  int
	Journals int // posted, the first of them reversed the day after
}

// Actor is who the sample company's changes are recorded as.
var Actor = storage.Actor{Type: "service", ID: "seed"}

// AccountType is a sample account's type from its code's first digit, the workbook's ranges.
func AccountType(code string) storage.AccountType {
	switch code[0] {
	case '1':
		return storage.Asset
	case '2':
		return storage.Liability
	case '3':
		return storage.Equity
	case '4':
		return storage.Revenue
	}
	return storage.Expense
}

// Load writes the sample company into org: its chart (share capital marked, INV-13), the twelve
// periods of 2026, and its journals, posted, with the first reversed the day after, as the golden
// scenarios do. An organisation that has a chart already is refused, so a load never mixes with
// real or earlier data.
func Load(ctx context.Context, store storage.Store, org uuid.UUID, c seed.Company) (Result, error) {
	ctx = storage.WithAudit(ctx, storage.AuditContext{Actor: &Actor, Reason: "the sample company (DEV-04)"})
	var r Result
	ids := map[string]uuid.UUID{}
	codes := map[uuid.UUID]string{}
	var periods []uuid.UUID
	var base money.Currency
	err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		if o.BaseCurrency != c.Organisation.BaseCurrency {
			return fmt.Errorf("the organisation keeps its books in %s; the sample company in %s", o.BaseCurrency, c.Organisation.BaseCurrency)
		}
		if base, err = money.Lookup(o.BaseCurrency); err != nil {
			return err
		}
		existing, err := u.Accounts().List(ctx)
		if err != nil {
			return err
		}
		if len(existing) > 0 {
			return fmt.Errorf("the organisation has a chart of accounts already (%d accounts): load the sample company into a new installation (make reset)", len(existing))
		}
		for _, a := range c.Accounts {
			acc, err := u.Accounts().Create(ctx, storage.NewAccount{Code: a.Code, Name: a.Name, Type: AccountType(a.Code), Role: storage.AccountRole(a.Role)})
			if err != nil {
				return fmt.Errorf("account %s: %w", a.Code, err)
			}
			ids[a.Code], codes[acc.ID] = acc.ID, a.Code
		}
		for m := time.January; m <= time.December; m++ {
			start := time.Date(2026, m, 1, 0, 0, 0, 0, time.UTC)
			p, err := u.Periods().Create(ctx, storage.NewPeriod{StartsOn: start, EndsOn: start.AddDate(0, 1, -1)})
			if err != nil {
				return fmt.Errorf("period %s: %w", start.Format("2006-01"), err)
			}
			periods = append(periods, p.ID)
		}
		return nil
	})
	if err != nil {
		return r, err
	}
	r.Accounts, r.Periods = len(ids), len(periods)

	want := map[string]int64{} // the seed's own totals, in integer cents: never from the engine
	svc := journal.Service{Store: store}
	var first uuid.UUID
	for i, j := range c.Journals {
		date, err := time.Parse(time.DateOnly, j.Date)
		if err != nil {
			return r, fmt.Errorf("%s: %w", j.ID, err)
		}
		d := journal.Draft{Date: date, Memo: j.ID}
		for _, l := range j.Lines {
			signed, err := cents(l.Debit)
			if err != nil {
				return r, err
			}
			credit, err := cents(l.Credit)
			if err != nil {
				return r, err
			}
			signed -= credit
			want[l.Account] += signed
			d.Lines = append(d.Lines, ledger.JournalLine{AccountID: ids[l.Account], Amount: money.FromMinor(signed, base)})
		}
		doc, err := svc.Create(ctx, org, d)
		if err != nil {
			return r, fmt.Errorf("%s: %w", j.ID, err)
		}
		if _, err := svc.Post(ctx, org, doc.ID); err != nil {
			return r, fmt.Errorf("%s: %w", j.ID, err)
		}
		if i == 0 {
			first = doc.ID
		}
		r.Journals++
	}
	if len(c.Journals) > 0 {
		date, _ := time.Parse(time.DateOnly, c.Journals[0].Date)
		if _, err := svc.Reverse(ctx, org, first, date.AddDate(0, 0, 1), "the sample company's first journal, reversed"); err != nil {
			return r, fmt.Errorf("reverse %s: %w", c.Journals[0].ID, err)
		}
		for _, l := range c.Journals[0].Lines {
			d, _ := cents(l.Debit)
			cr, _ := cents(l.Credit)
			want[l.Account] -= d - cr
		}
	}
	return r, check(ctx, store, org, base, periods, codes, want)
}

// check compares the balance service's trial balance with the seed's totals, account by account.
func check(ctx context.Context, store storage.Store, org uuid.UUID, base money.Currency, periods []uuid.UUID, codes map[uuid.UUID]string, want map[string]int64) error {
	var tb balance.TrialBalance
	if err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		tb, err = balance.Trial(ctx, u, base, periods...)
		return err
	}); err != nil {
		return err
	}
	if !tb.Total.IsZero() {
		return fmt.Errorf("the trial balance nets to %s, not zero (INV-09)", tb.Total)
	}
	got := map[string]int64{}
	for _, row := range tb.Rows {
		n, _ := row.Base.Minor()
		got[codes[row.AccountID]] = n
	}
	var wrong []string
	for code, w := range want {
		if got[code] != w {
			wrong = append(wrong, fmt.Sprintf("%s: the balance service %d, the seed %d cents", code, got[code], w))
		}
	}
	for code, g := range got {
		if _, ok := want[code]; !ok && g != 0 {
			wrong = append(wrong, fmt.Sprintf("%s: %d cents the seed never posted", code, g))
		}
	}
	if len(wrong) > 0 {
		sort.Strings(wrong)
		return fmt.Errorf("the trial balance differs from the sample company's own totals: %s", strings.Join(wrong, "; "))
	}
	return nil
}

// cents reads a seed amount ("1234.56") as integer cents.
func cents(s string) (int64, error) {
	whole, frac, ok := strings.Cut(s, ".")
	w, err1 := strconv.ParseInt(whole, 10, 64)
	f, err2 := strconv.ParseInt(frac, 10, 64)
	if !ok || len(frac) != 2 || err1 != nil || err2 != nil || w < 0 {
		return 0, fmt.Errorf("seed amount %q is not a decimal with two places", s)
	}
	return w*100 + f, nil
}
