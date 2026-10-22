//go:build local

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// FR-GL-05 (rev 0.70): an organisation's exchange rate is never changed or deleted, only withdrawn once; it is never of
// the base currency, a monthly rate is dated on its month's first day, one day's rate is current per currency and day,
// and the rate is its quote under RND-RATE, whatever writes it.
// req: FR-GL-05
// req: RND-RATE
func TestAttackExchangeRatesAreWithdrawnNeverChanged(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "accountant"}})
	b := books(t, "Rates Pte Ltd")
	s := store(t, "fyniq_api")
	oct16 := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	day := storage.NewFXRate{Kind: storage.FXRateDay, Currency: "USD", On: oct16, Rate: "1.2953", Quote: "1.2953", QuoteForm: "base-per-unit", Note: "DBS board rate"}
	var first storage.FXRate
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		if first, err = u.FXRates().Enter(ctx, day); err != nil {
			return err
		}
		jpy := storage.NewFXRate{Kind: storage.FXRateMonth, Currency: "JPY", On: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), Rate: "0.008521",
			Quote: "0.8521", QuoteForm: "base-per-100"}
		if _, err := u.FXRates().Enter(ctx, jpy); err != nil {
			return err
		}
		eur := storage.NewFXRate{Kind: storage.FXRateMonth, Currency: "EUR", On: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), Rate: "0.6666666667",
			Quote: "1.5", QuoteForm: "foreign-per-base"}
		_, err = u.FXRates().Enter(ctx, eur)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if first.EnteredBy != "user:accountant" || first.WithdrawnAt != nil || first.Note != "DBS board rate" {
		t.Errorf("the rate entered: %+v", first)
	}
	refused := func(what, code string, fn func(ctx context.Context, u storage.UnitOfWork) error) {
		t.Helper()
		if err := s.Do(ctx, b.Org, fn); !refusedAs(err, code) {
			t.Errorf("%s: want %s, got %v", what, code, err)
		}
	}
	refused("a rate of the base currency", storage.CodeFXRate, func(ctx context.Context, u storage.UnitOfWork) error {
		r := day
		r.Currency = "SGD"
		_, err := u.FXRates().Enter(ctx, r)
		return err
	})
	refused("a monthly rate not on its month's first day", storage.CodeFXRate, func(ctx context.Context, u storage.UnitOfWork) error {
		r := day
		r.Kind = storage.FXRateMonth
		_, err := u.FXRates().Enter(ctx, r)
		return err
	})
	refused("a second current rate for the day", storage.CodeDuplicate, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.FXRates().Enter(ctx, day)
		return err
	})
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		w, err := u.FXRates().Withdraw(ctx, first.ID, "Typed the wrong day")
		if err == nil && (w.WithdrawnBy == nil || *w.WithdrawnBy != "user:accountant" || *w.WithdrawnReason != "Typed the wrong day") {
			t.Errorf("the withdrawal: %+v", w)
		}
		if err != nil {
			return err
		}
		_, err = u.FXRates().Enter(ctx, day) // the day is free again
		return err
	}); err != nil {
		t.Fatal(err)
	}
	refused("a second withdrawal", storage.CodeFXRate, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.FXRates().Withdraw(ctx, first.ID, "Again")
		return err
	})
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		current, err := u.FXRates().List(ctx, storage.FXRateFilter{Currency: "USD"})
		if err == nil && len(current) != 1 {
			t.Errorf("current USD rates: %+v", current)
		}
		all, err := u.FXRates().List(ctx, storage.FXRateFilter{Withdrawn: true})
		if err == nil && len(all) != 4 {
			t.Errorf("every rate, withdrawn too: %d, want 4", len(all))
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}

	id := first.ID.String()
	insert := func(kind, cur, on, rate, quote, form string) string {
		return "INSERT INTO fyniq.fx_rate (kind, currency, on_date, rate, quote, quote_form, entered_by, entered_at) VALUES ('" + kind + "', '" +
			cur + "', '" + on + "', " + rate + ", " + quote + ", '" + form + "', 'user:a', now())"
	}
	for _, c := range []struct{ role, stmt, state string }{
		// The API's role enters and withdraws, nothing else.
		{"fyniq_api", "UPDATE fyniq.fx_rate SET rate = 2", "42501"},
		{"fyniq_api", "DELETE FROM fyniq.fx_rate", "42501"},
		{"fyniq_worker", insert("day", "USD", "2026-10-20", "1.3", "1.3", "base-per-unit"), "42501"},
		// Not even the owner of the table rewrites, un-withdraws or removes one.
		{"fyniq_migrator", "UPDATE fyniq.fx_rate SET rate = 2, quote = 2 WHERE id = '" + id + "'", "FY167"},
		{"fyniq_migrator", "UPDATE fyniq.fx_rate SET withdrawn_by = NULL, withdrawn_at = NULL, withdrawn_reason = NULL WHERE id = '" + id + "'", "FY167"},
		{"fyniq_migrator", "DELETE FROM fyniq.fx_rate", "FY167"},
		{"fyniq_migrator", "TRUNCATE fyniq.fx_rate", "FY002"},
		// The rate is its quote under RND-RATE.
		{"fyniq_api", insert("day", "USD", "2026-10-20", "1.3", "1.31", "base-per-unit"), "23514"},
		{"fyniq_api", insert("day", "JPY", "2026-10-20", "0.8521", "0.8521", "base-per-100"), "23514"},
		{"fyniq_api", insert("day", "EUR", "2026-10-20", "0.6666666666", "1.5", "foreign-per-base"), "23514"},
		{"fyniq_api", insert("day", "EUR", "2026-10-20", "1.5", "1.5", "per-pound"), "23514"},
		{"fyniq_api", insert("week", "EUR", "2026-10-20", "1.5", "1.5", "base-per-unit"), "23514"},
		{"fyniq_api", insert("day", "USD", "2026-10-20", "0", "0", "base-per-unit"), "23514"},
		// Never the base currency, and a month on its first day, by any path.
		{"fyniq_api", insert("day", "SGD", "2026-10-20", "1", "1", "base-per-unit"), "FY166"},
		{"fyniq_api", insert("month", "USD", "2026-10-20", "1.3", "1.3", "base-per-unit"), "FY166"},
	} {
		err := asOrg(ctx, conn(t, c.role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.stmt); return err })
		wantState(t, c.role+": "+c.stmt, err, c.state)
	}
}

// FR-GL-05 (rev 0.70): the daily feed is the installation's, written by the worker only through record_feed_rate, read
// through its view, and a feed rate never changes or goes.
// req: FR-GL-05
func TestTheDailyFeedIsTheWorkersAndNeverChanges(t *testing.T) {
	ctx := context.Background()
	w := store(t, "fyniq_worker")
	on := time.Date(2001, 3, 5, 0, 0, 0, 0, time.UTC)
	r := storage.FeedRate{Base: "SGD", Currency: "USD", On: on, Rate: "1.7651", Quote: "1.7651", QuoteForm: "base-per-unit", Feed: "Test feed"}
	if added, err := w.RecordFeedRate(ctx, r); err != nil || !added {
		t.Fatalf("a new feed rate: %v, %v", added, err)
	}
	if added, err := w.RecordFeedRate(ctx, r); err != nil || added {
		t.Errorf("the same rate again: want kept as it is, got %v, %v", added, err)
	}
	changed := r
	changed.Rate, changed.Quote = "1.7652", "1.7652"
	if _, err := w.RecordFeedRate(ctx, changed); !refusedAs(err, storage.CodeFXRate) {
		t.Errorf("a different rate for the same date: want %s, got %v", storage.CodeFXRate, err)
	}
	days, err := w.FeedDays(ctx, "SGD", on.AddDate(0, 0, -3), on)
	if err != nil || len(days) != 1 || !days[0].Equal(on) {
		t.Errorf("the feed's days: %v, %v", days, err)
	}
	b := books(t, "Feed reader Pte Ltd")
	if err := store(t, "fyniq_api").Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		got, err := u.FXRates().Feed(ctx, "SGD", "USD", on, on)
		if err == nil && (len(got) != 1 || got[0].Rate != "1.7651000000" || got[0].Feed != "Test feed") {
			t.Errorf("the feed as an organisation reads it: %+v", got)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ role, stmt, state string }{
		{"fyniq_api", "SELECT fyniq.record_feed_rate('SGD', 'EUR', '2001-03-05', 1.6, 1.6, 'base-per-unit', 'X')", "42501"},
		{"fyniq_api", "SELECT count(*) FROM fyniq.fx_feed_rate", "42501"},
		{"fyniq_api", "INSERT INTO fyniq.fx_feed_rate (base_currency, currency, on_date, rate, quote, quote_form, feed) VALUES ('SGD', 'EUR', '2001-03-05', 1.6, 1.6, 'base-per-unit', 'X')", "42501"},
		{"fyniq_worker", "SELECT fyniq.record_feed_rate('SGD', 'SGD', '2001-03-05', 1, 1, 'base-per-unit', 'X')", "23514"},
		{"fyniq_worker", "SELECT fyniq.record_feed_rate('SGD', 'JPY', '2001-03-05', 0.8521, 0.8521, 'base-per-100', 'X')", "23514"},
		{"fyniq_migrator", "UPDATE fyniq.fx_feed_rate SET rate = 2, quote = 2", "FY168"},
		{"fyniq_migrator", "DELETE FROM fyniq.fx_feed_rate", "FY168"},
		{"fyniq_migrator", "TRUNCATE fyniq.fx_feed_rate", "FY168"},
	} {
		_, err := conn(t, c.role).Exec(ctx, c.stmt)
		wantState(t, c.role+": "+c.stmt, err, c.state)
	}
	var n int
	if err := conn(t, "fyniq_readonly").QueryRow(ctx, "SELECT count(*) FROM fyniq.fx_feed_rates WHERE on_date = '2001-03-05'").Scan(&n); err != nil || n != 1 {
		t.Errorf("the view for the read-only role: %d, %v", n, err)
	}
}
