// Package fx keeps exchange rates and finds the one a posting takes (PRD-001 FR-GL-05, ENG-40, RND-RATE; rev 0.70,
// provisional until the accountant's sign-off). Every rate is base currency per one unit of the foreign currency,
// converted from its quote under RND-RATE.
//
// A rate comes from the cascade (ENG-40): the document's own rate, which the document keeps itself; then the
// organisation's rate for the day; then the daily feed's for the day, or its latest rate up to LookBack days earlier,
// as the feed publishes nothing on weekends and public holidays [accountant to confirm]; then the organisation's
// monthly table, whose duplicates are averaged; otherwise there is no rate (FX-01). The source of the rate it finds
// names where it came from and the date it is of, and is recorded on every amount converted at it (FR-GL-05).
package fx

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The error codes (ENG-23).
const (
	CodeNoRate    = "FX-01" // no rate anywhere in the cascade: "✖ No exchange rate for this currency"
	CodeEntry     = "FX-02" // a rate that cannot be entered that way
	CodeWithdrawn = "FX-03" // the rate is withdrawn already
	CodeReason    = "FX-04" // withdrawing a rate needs a reason
	CodeTooMany   = "FX-05" // more rates than one answer holds
)

// Error is a refusal with the field it is about, what is wrong and how to fix it (ENG-23).
type Error struct {
	Code    string
	Field   string
	Message string
	Fix     string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Fix) }

// LookBack is how many days before a date the cascade looks for the feed's latest rate, when the feed has none for the
// date itself (rev 0.70, provisional [accountant to confirm]): a week covers weekends and Singapore's public holidays.
const LookBack = 7

// The steps of the cascade a rate can come from, after the document's own.
const (
	StepOrganisation = "organisation" // the organisation's rate for the day
	StepFeed         = "feed"         // the daily feed
	StepMonth        = "month"        // the monthly table
)

// Found is the rate the cascade found, with where it came from.
type Found struct {
	Rate    money.Rate // its Source names the step and the date, such as "MAS daily feed of 2026-10-16"
	Step    string
	Of      time.Time // the date the rate is of: the day, the feed's earlier date, or the month's first day
	Entries int       // how many monthly-table entries it is the average of; 1 otherwise
}

// Average reports whether the rate is the average of duplicate monthly-table entries, which never sets a GST amount
// (ENG-42).
func (f Found) Average() bool { return f.Entries > 1 }

// Lookup finds the rate from cur into base for the date on, from the organisation's rate for the day onwards (ENG-40):
// the document's own rate is the document's to apply. No rate is CodeNoRate.
func Lookup(ctx context.Context, u storage.UnitOfWork, base, cur money.Currency, on time.Time) (Found, error) {
	if cur == base {
		return Found{Rate: money.Identity(base), Step: "base", Of: on, Entries: 1}, nil
	}
	day := dateOf(on)
	own, err := u.FXRates().List(ctx, storage.FXRateFilter{Currency: cur.Code, Kind: storage.FXRateDay, From: day, To: day})
	if err != nil {
		return Found{}, err
	}
	if len(own) > 0 {
		r := own[len(own)-1] // one is current at most; the latest if the database ever held two
		rate, err := money.ParseRate(trim(r.Rate), cur, base, "organisation rate of "+day.Format(time.DateOnly))
		return Found{Rate: rate, Step: StepOrganisation, Of: day, Entries: 1}, err
	}
	feed, err := u.FXRates().Feed(ctx, base.Code, cur.Code, day.AddDate(0, 0, -LookBack), day)
	if err != nil {
		return Found{}, err
	}
	if len(feed) > 0 {
		r := feed[len(feed)-1] // the latest date on or before the day
		rate, err := money.ParseRate(trim(r.Rate), cur, base, fmt.Sprintf("%s daily feed of %s", r.Feed, r.On.Format(time.DateOnly)))
		return Found{Rate: rate, Step: StepFeed, Of: dateOf(r.On), Entries: 1}, err
	}
	month := time.Date(day.Year(), day.Month(), 1, 0, 0, 0, 0, time.UTC)
	table, err := u.FXRates().List(ctx, storage.FXRateFilter{Currency: cur.Code, Kind: storage.FXRateMonth, From: month, To: month})
	if err != nil {
		return Found{}, err
	}
	if len(table) > 0 {
		avg, err := average(table)
		if err != nil {
			return Found{}, err
		}
		source := "monthly table " + month.Format("2006-01")
		if len(table) > 1 {
			source += fmt.Sprintf(", average of %d entries", len(table))
		}
		rate, err := rounding.Rate(avg, money.QuoteBasePerUnit, cur, base, source)
		return Found{Rate: rate, Step: StepMonth, Of: month, Entries: len(table)}, err
	}
	return Found{}, &Error{Code: CodeNoRate, Field: "fx_rate",
		Message: fmt.Sprintf("✖ No exchange rate for %s on %s: none of the organisation's for the day, none from the daily feed in the %d days before, none in the monthly table for %s",
			cur, day.Format("2 Jan 2006"), LookBack, month.Format("January 2006")),
		Fix: fmt.Sprintf("Enter the %s rate for the day or the month in Accounting › Currencies, or type the rate on the document", cur)}
}

// average is the mean of the monthly-table entries, rounded once under RND-RATE by the caller: the workbook's average
// of duplicates (ENG-40).
func average(table []storage.FXRate) (*apd.Decimal, error) {
	c := apd.BaseContext.WithPrecision(60)
	sum := new(apd.Decimal)
	for _, r := range table {
		d, _, err := apd.NewFromString(string(r.Rate))
		if err != nil {
			return nil, fmt.Errorf("monthly rate %s: %w", r.ID, err)
		}
		if _, err := c.Add(sum, sum, d); err != nil {
			return nil, err
		}
	}
	out := new(apd.Decimal)
	_, err := c.Quo(out, sum, apd.New(int64(len(table)), 0))
	return out, err
}

// Entry is a rate as a person enters it.
type Entry struct {
	Kind      storage.FXRateKind
	Currency  string
	On        time.Time // the day; for a month, any day of it is refused but the first
	Quote     string
	QuoteForm money.QuoteForm // base-per-unit when empty
	Note      string
}

// Check turns an entry into the rate it records, or says what is wrong with each field.
func Check(e Entry, base money.Currency) (storage.NewFXRate, []*Error) {
	var bad []*Error
	add := func(field, msg, fix string) {
		bad = append(bad, &Error{Code: CodeEntry, Field: field, Message: msg, Fix: fix})
	}
	if e.Kind != storage.FXRateDay && e.Kind != storage.FXRateMonth {
		add("kind", fmt.Sprintf("%q is neither day nor month", e.Kind), "Say whether the rate is for a day or for a month")
	}
	cur, err := money.Lookup(strings.TrimSpace(e.Currency))
	switch {
	case err != nil:
		add("currency", fmt.Sprintf("%q is not a currency Fyniq knows", e.Currency), "Use an ISO 4217 code, such as USD")
	case cur == base:
		add("currency", fmt.Sprintf("%s is the base currency, which takes rate 1", cur), "Enter rates of other currencies only")
	}
	if e.On.IsZero() {
		add("on", "the rate has no date", "Give the day, or the month's first day, the rate is for")
	} else if e.Kind == storage.FXRateMonth && e.On.Day() != 1 {
		add("on", fmt.Sprintf("a monthly rate is dated on its month's first day, not %s", e.On.Format(time.DateOnly)),
			fmt.Sprintf("Date it %s", time.Date(e.On.Year(), e.On.Month(), 1, 0, 0, 0, 0, time.UTC).Format(time.DateOnly)))
	}
	form := e.QuoteForm
	if form == "" {
		form = money.QuoteBasePerUnit
	}
	if !form.Known() {
		add("quote_form", fmt.Sprintf("%q is not a way to quote a rate", form), "Use base-per-unit, base-per-100 or foreign-per-base")
	}
	if n := len([]rune(strings.TrimSpace(e.Note))); n > 200 {
		add("note", fmt.Sprintf("the note has %d characters; the most is 200", n), "Shorten the note")
	}
	q, err := money.ParseQuote(strings.TrimSpace(e.Quote), "quote")
	if err != nil {
		msg, fix := explain(err)
		add("quote", msg, fix)
	}
	if len(bad) > 0 {
		return storage.NewFXRate{}, bad
	}
	rate, err := rounding.Rate(q, form, cur, base, "entered")
	if err != nil {
		msg, fix := explain(err)
		return storage.NewFXRate{}, []*Error{{Code: CodeEntry, Field: "quote", Message: msg, Fix: fix}}
	}
	return storage.NewFXRate{Kind: e.Kind, Currency: cur.Code, On: dateOf(e.On), Rate: storage.Decimal(rate.String()),
		Quote: storage.Decimal(q.Text('f')), QuoteForm: string(form), Note: strings.TrimSpace(e.Note)}, nil
}

// ReplacedReason is the reason an organisation's rate for a day carries when a new one for the same day replaces it.
const ReplacedReason = "Replaced by a new rate for the same day"

// Enter records an organisation's rate (FR-GL-05). A day's rate replaces the current one for its currency and day,
// which is withdrawn in the same unit of work: the feed is overridden by one rate a day at most. The monthly table
// keeps duplicates, which the cascade averages and the page shows with ⚠ (ENG-40).
func Enter(ctx context.Context, u storage.UnitOfWork, r storage.NewFXRate) (storage.FXRate, error) {
	if r.Kind == storage.FXRateDay {
		current, err := u.FXRates().List(ctx, storage.FXRateFilter{Currency: r.Currency, Kind: storage.FXRateDay, From: r.On, To: r.On})
		if err != nil {
			return storage.FXRate{}, err
		}
		for _, c := range current {
			if _, err := u.FXRates().Withdraw(ctx, c.ID, ReplacedReason); err != nil {
				return storage.FXRate{}, err
			}
		}
	}
	return u.FXRates().Enter(ctx, r)
}

// Withdraw withdraws an organisation's rate with a reason: the cascade no longer finds it, and it stays listed with who
// withdrew it, when and why.
func Withdraw(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, reason string) (storage.FXRate, error) {
	reason = strings.TrimSpace(reason)
	if n := len([]rune(reason)); n == 0 || n > 500 {
		return storage.FXRate{}, &Error{Code: CodeReason, Field: "reason", Message: "withdrawing a rate needs a reason of up to 500 characters",
			Fix: "Say why the rate is wrong or no longer used"}
	}
	r, err := u.FXRates().Get(ctx, id)
	if err != nil {
		return storage.FXRate{}, err
	}
	if r.WithdrawnAt != nil {
		return storage.FXRate{}, &Error{Code: CodeWithdrawn, Message: fmt.Sprintf("the rate was withdrawn on %s", r.WithdrawnAt.Format("2 Jan 2006")),
			Fix: "Nothing to do: the cascade no longer uses it"}
	}
	return u.FXRates().Withdraw(ctx, id, reason)
}

// Duplicates returns, per currency, the months of the monthly table with more than one current entry (ENG-40's ⚠ until
// resolved; C.1 #9 in the checks catalogue).
func Duplicates(rates []storage.FXRate) map[string][]time.Time {
	count := map[string]map[time.Time]int{}
	for _, r := range rates {
		if r.Kind != storage.FXRateMonth || r.WithdrawnAt != nil {
			continue
		}
		if count[r.Currency] == nil {
			count[r.Currency] = map[time.Time]int{}
		}
		count[r.Currency][dateOf(r.On)]++
	}
	out := map[string][]time.Time{}
	for cur, months := range count {
		for m, n := range months {
			if n > 1 {
				out[cur] = append(out[cur], m)
			}
		}
	}
	return out
}

// explain is what a money error says, and how to fix it.
func explain(err error) (string, string) {
	var me *money.Error
	if errors.As(err, &me) {
		return me.Message, me.Fix
	}
	return err.Error(), "Check the quote"
}

// trim drops the trailing zeros the database's NUMERIC(20,10) gives a rate: 1.3472000000 is 1.3472.
func trim(d storage.Decimal) string {
	s := string(d)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return s
}

// Trim is trim for callers that show a stored rate.
func Trim(d storage.Decimal) string { return trim(d) }

func dateOf(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
