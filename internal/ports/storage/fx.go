package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// FXRateKind is what an organisation's rate is for (ENG-40): a day, whose rate overrides the daily feed's on that date,
// or a month of the monthly table.
type FXRateKind string

// The kinds of an organisation's rate.
const (
	FXRateDay   FXRateKind = "day"
	FXRateMonth FXRateKind = "month"
)

// NewFXRate is an organisation's rate as entered: base currency per one unit of Currency (RND-RATE), the quote it came
// from and its form.
type NewFXRate struct {
	Kind      FXRateKind
	Currency  string
	On        time.Time // the day, or the month's first day
	Rate      Decimal
	Quote     Decimal
	QuoteForm string
	Note      string // where the rate came from, in the person's words; may be empty
}

// FXRate is an organisation's rate (FR-GL-05). It never changes; it is withdrawn once, with who, when and why.
type FXRate struct {
	ID uuid.UUID
	NewFXRate
	EnteredBy       string
	EnteredAt       time.Time
	WithdrawnBy     *string
	WithdrawnAt     *time.Time
	WithdrawnReason *string
}

// FXRateFilter narrows a list of an organisation's rates; zero fields do not narrow it.
type FXRateFilter struct {
	Currency  string
	Kind      FXRateKind
	From, To  time.Time // on or after From, on or before To
	Withdrawn bool      // include withdrawn rates too
}

// FeedRate is a rate of the daily rate feed (FR-GL-05): the same for every organisation, never changed.
type FeedRate struct {
	Base       string
	Currency   string
	On         time.Time
	Rate       Decimal
	Quote      Decimal
	QuoteForm  string
	Feed       string // the feed's name, such as MAS
	RecordedAt time.Time
}

// FXRates are the organisation's exchange rates and its reads of the daily feed (FR-GL-05, ENG-40). The writer is the
// unit of work's actor, and each change is audited with it.
type FXRates interface {
	// Enter records a rate: CodeFXRate for a rate of the base currency, or a monthly rate not dated on a month's first
	// day. A day's rate while another is current for its currency and date is CodeDuplicate: withdraw that one first.
	Enter(ctx context.Context, r NewFXRate) (FXRate, error)
	// Withdraw withdraws a current rate with a reason: ErrNotFound when there is no such rate; CodeFXRate when it is
	// withdrawn already.
	Withdraw(ctx context.Context, id uuid.UUID, reason string) (FXRate, error)
	Get(ctx context.Context, id uuid.UUID) (FXRate, error)
	// List returns the rates that match f, by currency, date and then the order they were entered.
	List(ctx context.Context, f FXRateFilter) ([]FXRate, error)
	// Feed returns the feed's rates into base between from and to, both inclusive, by currency and then date; for one
	// currency when currency is not empty.
	Feed(ctx context.Context, base, currency string, from, to time.Time) ([]FeedRate, error)
}

// FeedRates is the daily feed's own store: the installation's, not an organisation's. Only the feed job writes it.
type FeedRates interface {
	// RecordFeedRate stores one rate of the feed: true when it is new, false when the same rate is there already;
	// CodeFXRate when the feed already has a different rate for that date.
	RecordFeedRate(ctx context.Context, r FeedRate) (bool, error)
	// FeedDays returns the dates between from and to, both inclusive, for which the feed has any rate into base.
	FeedDays(ctx context.Context, base string, from, to time.Time) ([]time.Time, error)
}
