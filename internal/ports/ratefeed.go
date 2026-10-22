package ports

import (
	"context"
	"time"
)

// RateQuote is one rate as a feed publishes it: Quote units of Base per unit of Currency, or as Form says (RND-RATE's
// forms: base-per-unit, base-per-100, foreign-per-base).
type RateQuote struct {
	Base     string
	Currency string
	Quote    string // a plain decimal, as published
	Form     string
}

// RateFeed is the daily exchange-rate feed (FR-GL-05; PRD-001 rev 0.70). Production: MAS's exchange rates from its
// API catalogue, once MAS confirms the rates may be redistributed [verify] (a cloud-gate task). Local: a directory of
// rates files, one per date.
type RateFeed interface {
	// Name names the feed in every rate's source, such as "MAS".
	Name() string
	// Fetch returns the rates the feed published for day; none for a day it published nothing, such as a weekend.
	Fetch(ctx context.Context, day time.Time) ([]RateQuote, error)
}
