package director

import (
	"context"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/period"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// RelatedParty is one director's or shareholder's row of the related-party schedule (FR-EQ-10; p1-founders-money design
// D14), in the base currency: what the company owes them at a day on the director's loan account (2500) and on deferred
// remuneration (2120), positive when it owes them and negative when they owe it; and, over the financial year to that
// day, the directors' fees owed to them, and what was converted to shares and what they waived, each positive.
type RelatedParty struct {
	Contact   storage.Contact
	Loan      money.Money
	Deferred  money.Money
	Fees      money.Money
	Converted money.Money
	Waived    money.Money
}

// Empty is a row with nothing to disclose.
func (r RelatedParty) Empty() bool {
	for _, m := range []money.Money{r.Loan, r.Deferred, r.Fees, r.Converted, r.Waived} {
		if m.Sign() != 0 {
			return false
		}
	}
	return true
}

// RelatedParties is the related-party schedule as at a day, from the balance service (ENG-04): a row for each director
// and shareholder with a balance or a movement to disclose, in the order they were added, and the first day of the
// year the movements are read from: the financial year asAt falls in, or the calendar year when the financial years are
// not set or asAt is before the books start. The notes to the financial statements (FR-RPT-11) read it.
func RelatedParties(ctx context.Context, u storage.UnitOfWork, base money.Currency, p storage.OrganisationProfile, asAt time.Time) (time.Time, []RelatedParty, error) {
	from := time.Date(asAt.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
	if cal, err := period.CalendarOf(p); err == nil {
		if n := cal.Number(asAt); n > 0 {
			from = cal.Year(n).StartsOn
		}
	}
	people, accounts, _, err := Accounts(ctx, u, base, asAt)
	if err != nil {
		return from, nil, err
	}
	origins := originReader{ctx: ctx, u: u, saved: map[string][]string{}}
	z := money.Zero(base)
	var out []RelatedParty
	for _, x := range people {
		r := RelatedParty{Contact: x.Contact, Loan: z, Deferred: z, Fees: z, Converted: z, Waived: z}
		for _, a := range accounts {
			to := &r.Loan
			if a.Role == ledger.RoleDeferredRemuneration {
				to = &r.Deferred
			}
			if *to, err = to.Sub(x.Balances[a.ID]); err != nil { // a credit is owed to the person
				return from, nil, err
			}
		}
		lines, err := balance.PersonLines(ctx, u, x.Contact.ID, from, asAt)
		if err != nil {
			return from, nil, err
		}
		for _, l := range lines {
			amount, err := money.ParseStored(string(l.BaseAmount), base)
			if err != nil {
				return from, nil, err
			}
			origin, err := origins.of(l, amount)
			if err != nil {
				return from, nil, err
			}
			switch origin {
			case ledger.OriginFee: // credited to them
				r.Fees, err = r.Fees.Sub(amount)
			case ledger.OriginConverted: // debited off what they were owed
				r.Converted, err = r.Converted.Add(amount)
			case ledger.OriginWaived:
				r.Waived, err = r.Waived.Add(amount)
			}
			if err != nil {
				return from, nil, err
			}
		}
		if !r.Empty() {
			out = append(out, r)
		}
	}
	return from, out, nil
}
