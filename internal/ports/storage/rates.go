package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// StandardRole is the role name of the organisation's standard cost rate: a rate with no person (FR-PRJ-06).
const StandardRole = "Standard"

// CostRate is a person's cost of an hour from a day, in the base currency; with no person it is the organisation's standard
// rate. It is payroll data (FR-PAY-13): only the Owner and the Payroll administrator see it, and the API never sends one to
// anyone else. A rate never changes: a later day's replaces it from that day.
type CostRate struct {
	ID        uuid.UUID
	UserID    *uuid.UUID
	Role      string
	From      time.Time
	Hourly    Decimal
	Currency  string
	CreatedBy string
	CreatedAt time.Time
}

// NewCostRate enters a cost rate; Role is StandardRole when UserID is nil.
type NewCostRate struct {
	UserID   *uuid.UUID
	Role     string
	From     time.Time
	Hourly   Decimal
	Currency string
}

// BillingRate is the price of an hour from a day, for a project or a customer's projects, for a person, a role or everyone,
// in the project's currency (FR-PRJ-06).
type BillingRate struct {
	ID         uuid.UUID
	ProjectID  *uuid.UUID
	CustomerID *uuid.UUID
	UserID     *uuid.UUID
	Role       string
	From       time.Time
	Hourly     Decimal
	Currency   string
	CreatedAt  time.Time
}

// NewBillingRate enters a billing rate for a project or a customer, not both.
type NewBillingRate struct {
	ProjectID  *uuid.UUID
	CustomerID *uuid.UUID
	UserID     *uuid.UUID
	Role       string
	From       time.Time
	Hourly     Decimal
	Currency   string
}

// Rates are cost rates and billing rates (FR-PRJ-06). Each write records its audit event in the unit of work; a cost rate is
// sensitive there (AUD-02). Nothing is changed or deleted.
type Rates interface {
	// CostRates are every cost rate, person and standard, by day then person.
	CostRates(ctx context.Context) ([]CostRate, error)
	AddCostRate(ctx context.Context, n NewCostRate) (CostRate, error)
	// BillingRates are a project's own rates and its customer's (customer may be nil), by day.
	BillingRates(ctx context.Context, project uuid.UUID, customer *uuid.UUID) ([]BillingRate, error)
	AddBillingRate(ctx context.Context, n NewBillingRate) (BillingRate, error)
}

// CodeRate: a rate never changes or goes, and one starts on a day once for its person or scope (FR-PRJ-06; FY605).
const CodeRate = "STORAGE-RATE-01"
