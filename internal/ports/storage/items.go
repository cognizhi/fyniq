package storage

import (
	"context"

	"github.com/google/uuid"
)

// ItemType is what an item is (FR-ITM-01).
type ItemType string

// The item types.
const (
	ItemProduct ItemType = "product"
	ItemService ItemType = "service"
)

// ItemPrice is an item's unit price in one currency: a plain decimal with at most six decimals, NUMERIC(20,6)
// (§11.3). A price read back has the column's scale ("12.500000").
type ItemPrice struct {
	Currency  string // ISO 4217
	UnitPrice Decimal
}

// ItemDetails are what an item holds (FR-ITM-01). Empty text is not set.
type ItemDetails struct {
	Name             string
	Description      string // what documents print
	Type             ItemType
	Unit             string     // such as hour, month or piece
	RevenueAccountID *uuid.UUID // where its sales post
	GSTCode          string
	Prices           []ItemPrice // at most one per currency, by currency
}

// Item is a product or service the organisation sells. Its code never changes.
type Item struct {
	ID   uuid.UUID
	Code string
	ItemDetails
	Active  bool
	Version int
}

// Items are the organisation's items. Every create and change is audited, its prices with it; an item is
// never deleted.
type Items interface {
	// Create adds an item: CodeDuplicate when its code is in use.
	Create(ctx context.Context, code string, d ItemDetails) (Item, error)
	Get(ctx context.Context, id uuid.UUID) (Item, error)
	// Update changes an item at its version, prices included, and returns it at its next: CodeVersion when it
	// changed since.
	Update(ctx context.Context, id uuid.UUID, version int, d ItemDetails, active bool) (Item, error)
	// Page returns at most limit items by code, after the code after ("" for the first page).
	Page(ctx context.Context, after string, limit int) ([]Item, error)
}
