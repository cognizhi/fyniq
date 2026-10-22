-- name: InsertFXRate :one
INSERT INTO fyniq.fx_rate (kind, currency, on_date, rate, quote, quote_form, note, entered_by, entered_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING id, kind, currency, on_date, rate, quote, quote_form, note, entered_by, entered_at, withdrawn_by, withdrawn_at, withdrawn_reason;

-- name: WithdrawFXRate :one
-- Only a current rate is withdrawn; the guard refuses a second withdrawal whoever tries it (FY167).
UPDATE fyniq.fx_rate SET withdrawn_by = $2, withdrawn_at = $3, withdrawn_reason = $4
WHERE id = $1 AND withdrawn_at IS NULL
RETURNING id, kind, currency, on_date, rate, quote, quote_form, note, entered_by, entered_at, withdrawn_by, withdrawn_at, withdrawn_reason;

-- name: GetFXRate :one
SELECT id, kind, currency, on_date, rate, quote, quote_form, note, entered_by, entered_at, withdrawn_by, withdrawn_at, withdrawn_reason
FROM fyniq.fx_rate WHERE id = $1;

-- name: ListFXRates :many
-- The organisation's rates by currency, date and the order they were entered; an empty currency or kind and a null date
-- do not narrow the list.
SELECT id, kind, currency, on_date, rate, quote, quote_form, note, entered_by, entered_at, withdrawn_by, withdrawn_at, withdrawn_reason
FROM fyniq.fx_rate
WHERE (sqlc.arg(currency)::text = '' OR currency = sqlc.arg(currency)::text)
  AND (sqlc.arg(kind)::text = '' OR kind = sqlc.arg(kind)::text)
  AND (sqlc.narg(from_on)::date IS NULL OR on_date >= sqlc.narg(from_on)::date)
  AND (sqlc.narg(to_on)::date IS NULL OR on_date <= sqlc.narg(to_on)::date)
  AND (sqlc.arg(withdrawn)::boolean OR withdrawn_at IS NULL)
ORDER BY currency, on_date, entered_at, id;

-- name: ListFeedRates :many
SELECT base_currency, currency, on_date, rate, quote, quote_form, feed, recorded_at
FROM fyniq.fx_feed_rates
WHERE base_currency = sqlc.arg(base)::text
  AND (sqlc.arg(currency)::text = '' OR currency = sqlc.arg(currency)::text)
  AND on_date BETWEEN sqlc.arg(from_on)::date AND sqlc.arg(to_on)::date
ORDER BY currency, on_date;

-- name: RecordFeedRate :one
SELECT fyniq.record_feed_rate(sqlc.arg(base)::text, sqlc.arg(currency)::text, sqlc.arg(on_date)::date, sqlc.arg(rate)::numeric,
  sqlc.arg(quote)::numeric, sqlc.arg(quote_form)::text, sqlc.arg(feed)::text)::boolean AS new;

-- name: FeedDays :many
SELECT DISTINCT on_date FROM fyniq.fx_feed_rates
WHERE base_currency = sqlc.arg(base)::text AND on_date BETWEEN sqlc.arg(from_on)::date AND sqlc.arg(to_on)::date
ORDER BY on_date;

-- name: CreateScheduledRevaluation :one
-- The scheduler's one way to make a document of a month-end revaluation (FR-GL-04, FR-AUT-02; rev 0.75): a posted
-- fx_revaluation of an open period, saved and posted by scheduler:fx.revalue (FY169 otherwise).
SELECT fyniq.create_scheduled_revaluation(sqlc.arg(period_id)::uuid, sqlc.arg(payload)::jsonb, sqlc.arg(posted_at)::timestamptz)::uuid AS id;
