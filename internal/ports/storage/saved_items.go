package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// The kinds of saved item (FR-UX-12).
const (
	SavedFavourite = "favourite"
	SavedView      = "view"
)

// SavedItem is a favourite or a saved view of a person's: a page, or a page with the filters, columns and sort its address
// carries. Key is the favourite's address or the view's own ID; the latest state of a key is in force.
type SavedItem struct {
	UserID uuid.UUID
	Kind   string
	Key    string
	On     bool
	Title  string
	Path   string
	Query  string
	SetAt  time.Time
}

// SavedItems are each person's own favourites and views: audited, append-only.
type SavedItems interface {
	// Save records an item's new state (on, or off to take it away).
	Save(ctx context.Context, item SavedItem) (SavedItem, error)
	// Mine is a person's items that are on, in the order they were first saved.
	Mine(ctx context.Context, user uuid.UUID) ([]SavedItem, error)
}
