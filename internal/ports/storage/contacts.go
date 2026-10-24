package storage

import (
	"context"

	"github.com/google/uuid"
)

// ContactDetails are what a contact holds (FR-CT-01). Empty text is not set.
type ContactDetails struct {
	LegalName        string
	Customer, Vendor bool // at least one
	UENOrTaxID       string
	PeppolID         string
	BillingEmail     string
	RemittanceEmail  string
	Phone            string
	Address          string
	Country          string // ISO 3166-1 alpha-2
	TaxResidence     string // ISO 3166-1 alpha-2, where it is resident for tax when that is not Country (FR-AP-19); empty: Country
	Currency         string // ISO 4217
	PaymentTermsDays *int   // nil when not set
	// The early-payment discount a supplier offers (FR-AP-13; rev 0.93): DiscountPercent off, as an exact decimal such
	// as "2", if paid within DiscountDays of the bill's date. Both set, or neither.
	DiscountPercent  string
	DiscountDays     *int
	DefaultAccountID *uuid.UUID // the default category
	DefaultGSTCode   string

	// The customer group whose reminder schedule a customer follows (FR-AR-08); nil: the standard one.
	CustomerGroupID *uuid.UUID
}

// Contact is a customer, a vendor or both.
type Contact struct {
	ID uuid.UUID
	ContactDetails
	Active  bool
	Version int
}

// ContactDuplicate is another contact a contact may duplicate, and why (FR-CT-04).
type ContactDuplicate struct {
	ID                           uuid.UUID
	LegalName                    string
	SameName, SameUEN, SameEmail bool
}

// Contacts are the organisation's contacts. Every create and change is audited; a contact is never deleted.
type Contacts interface {
	Create(ctx context.Context, d ContactDetails) (Contact, error)
	Get(ctx context.Context, id uuid.UUID) (Contact, error)
	// Update changes a contact at its version, and returns it at its next: CodeVersion when it changed since.
	Update(ctx context.Context, id uuid.UUID, version int, d ContactDetails, active bool) (Contact, error)
	// Page returns at most limit contacts added after the contact after (uuid.Nil for the first page),
	// customers or vendors only when role is "customer" or "vendor".
	Page(ctx context.Context, after uuid.UUID, role string, limit int) ([]Contact, error)
	// Duplicates returns the other contacts with the same name (case and spacing aside), UEN or tax ID, or
	// email as d; self is the contact being changed (uuid.Nil for a new one).
	Duplicates(ctx context.Context, self uuid.UUID, d ContactDetails) ([]ContactDuplicate, error)
}
