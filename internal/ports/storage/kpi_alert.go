package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// CodeKPIAlert is the database's refusal of a raised alert changed or deleted (FYR07).
const CodeKPIAlert = "STORAGE-KPIALERT-01"

// AlertSpendAnomaly is an expense category's closed month three standard deviations above its trend (FR-KPI-06).
const AlertSpendAnomaly = "spend_anomaly"

// KPIAlert is an alert as it was raised.
type KPIAlert struct {
	ID             uuid.UUID
	Kind           string
	Category       string
	Month          time.Time
	Currency       string
	Spend          Decimal
	Mean           Decimal
	HistoryMonths  int
	LedgerRevision int64
	RaisedAt       time.Time
}

// KPIAlerts are the alerts raised, once for a category and month.
type KPIAlerts interface {
	// Raise records an alert; it returns false when the category's month has one already.
	Raise(ctx context.Context, a KPIAlert) (KPIAlert, bool, error)
	// List are the alerts, latest month first.
	List(ctx context.Context) ([]KPIAlert, error)
}
