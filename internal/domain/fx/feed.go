package fx

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// FeedZone is the feed's own calendar: MAS publishes Singapore's business days.
var FeedZone = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Singapore")
	if err != nil {
		return time.FixedZone("SGT", 8*60*60)
	}
	return loc
}()

// FeedRun is what a run of the feed stored.
type FeedRun struct {
	Days   []time.Time // the days it fetched: those of the last LookBack with no rate yet
	Stored int         // rates stored
}

// RecordFeed stores the daily feed's rates for each of the last LookBack days, up to today in the feed's calendar,
// that has none yet, so a missed run catches up (FR-GL-05). A day the feed published nothing for is fetched again on
// the next run, until it falls out of the window. A quote that is not a rate, or a rate the store holds differently
// for that date, is reported; the other rates are still stored.
func RecordFeed(ctx context.Context, store storage.FeedRates, feed ports.RateFeed, now time.Time) (FeedRun, error) {
	today := dateOf(now.In(FeedZone))
	from := today.AddDate(0, 0, -(LookBack - 1))
	var run FeedRun
	have := map[time.Time]bool{}
	days, err := store.FeedDays(ctx, "SGD", from, today)
	if err != nil {
		return run, err
	}
	for _, d := range days {
		have[dateOf(d)] = true
	}
	var errs []error
	for d := from; !d.After(today); d = d.AddDate(0, 0, 1) {
		if have[d] {
			continue
		}
		run.Days = append(run.Days, d)
		quotes, err := feed.Fetch(ctx, d)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", d.Format(time.DateOnly), err))
			continue
		}
		for _, q := range quotes {
			r, err := feedRate(feed.Name(), d, q)
			if err == nil {
				_, err = store.RecordFeedRate(ctx, r)
			}
			if err != nil {
				errs = append(errs, fmt.Errorf("%s %s: %w", d.Format(time.DateOnly), q.Currency, err))
				continue
			}
			run.Stored++
		}
	}
	return run, errors.Join(errs...)
}

// feedRate is a published quote as the rate the feed stores, under RND-RATE.
func feedRate(name string, day time.Time, q ports.RateQuote) (storage.FeedRate, error) {
	base, err := money.Lookup(q.Base)
	if err != nil {
		return storage.FeedRate{}, err
	}
	cur, err := money.Lookup(q.Currency)
	if err != nil {
		return storage.FeedRate{}, err
	}
	quote, err := money.ParseQuote(q.Quote, "quote")
	if err != nil {
		return storage.FeedRate{}, err
	}
	rate, err := rounding.Rate(quote, money.QuoteForm(q.Form), cur, base, name)
	if err != nil {
		return storage.FeedRate{}, err
	}
	return storage.FeedRate{Base: base.Code, Currency: cur.Code, On: day, Rate: storage.Decimal(rate.String()), Quote: storage.Decimal(q.Quote),
		QuoteForm: q.Form, Feed: name}, nil
}
