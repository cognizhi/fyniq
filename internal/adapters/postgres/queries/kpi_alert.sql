-- name: RaiseKPIAlert :one
INSERT INTO fyniq.kpi_alert (kind, category, month, currency, spend, mean, history_months, ledger_revision, raised_at)
VALUES (sqlc.arg(kind), sqlc.arg(category), sqlc.arg(month), sqlc.arg(currency), sqlc.arg(spend), sqlc.arg(mean), sqlc.arg(history_months),
        sqlc.arg(ledger_revision), sqlc.arg(raised_at))
ON CONFLICT (organisation_id, kind, category, month) DO NOTHING
RETURNING *;

-- name: ListKPIAlerts :many
SELECT * FROM fyniq.kpi_alert ORDER BY month DESC, category, id;
