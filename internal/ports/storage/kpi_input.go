package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// CodeKPIInput is the database's refusal of a month's KPI counts written other than at version 1, changed other than as
// their next version, or deleted (FY244).
const CodeKPIInput = "STORAGE-KPI-01"

// KPICounts are a month's customer counts and headcount as a person enters them (FR-KPI-02, rev 0.161): the three
// customer counts together or none of them, and the headcount, each a whole number from zero.
type KPICounts struct {
	Paying, New, Lost *int64
	Headcount         *int64
}

// KPIInput is a month's entered counts at their current version.
type KPIInput struct {
	ID        uuid.UUID
	Month     time.Time // the first day of the month
	Counts    KPICounts
	EnteredBy string // "type:id"
	EnteredAt time.Time
	Version   int
}

// KPIInputs are the months' entered counts (FR-KPI-02, rev 0.161). Each change is audited on the month; a month is
// changed only as its next version and never deleted (CodeKPIInput).
type KPIInputs interface {
	// Get is a month's counts, or ports.ErrNotFound when none were entered.
	Get(ctx context.Context, month time.Time) (KPIInput, error)
	// Range are the months' counts from from to to, both first days of a month, earliest first.
	Range(ctx context.Context, from, to time.Time) ([]KPIInput, error)
	// Set enters a month's counts: the month's first version when version is 0, else its next after version, refused
	// with CodeVersion when the month is no longer at version.
	Set(ctx context.Context, month time.Time, c KPICounts, enteredBy string, at time.Time, version int) (KPIInput, error)
}
