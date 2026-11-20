package storage

import (
	"context"
	"time"
)

// UsageMonth is what an organisation used in one calendar month (UTC), read from what the platform already keeps: the AI calls, the reports exported and the files uploaded
// as evidence (FR-ADM-05).
type UsageMonth struct {
	Month         time.Time // the first day of the month
	AICalls       int64
	InputTokens   int64
	OutputTokens  int64
	AICost        []UsageCost // exactly, in each currency a price used
	Exports       int64       // report exports that were made ready
	ExportBytes   int64
	EvidenceFiles int64
	EvidenceBytes int64
}

// UsageCost is an exact amount in one currency.
type UsageCost struct {
	Currency string
	Amount   Decimal
}

// StoredUsage is what the organisation holds in storage now.
type StoredUsage struct {
	EvidenceFiles, EvidenceBytes int64
	CaptureFiles, CaptureBytes   int64
	ExportFiles, ExportBytes     int64
}

// Usage reads the organisation's usage; it keeps nothing of its own and writes nothing.
type Usage interface {
	// Months are the months from the one that holds since, with a row only for a month that had any use, earliest first.
	Months(ctx context.Context, since time.Time) ([]UsageMonth, error)
	// Stored is what the organisation holds in storage now.
	Stored(ctx context.Context) (StoredUsage, error)
}
