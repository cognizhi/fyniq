package storage

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// CodeDashboardLayout is the database's refusal of a dashboard layout changed other than as its next version, or deleted (FYR08).
const CodeDashboardLayout = "STORAGE-DASH-01"

// The scopes of a dashboard layout.
const (
	DashboardRole = "role"
	DashboardUser = "user"
)

// DashboardLayout is the tiles a role or a person arranged, at their current version.
type DashboardLayout struct {
	ID        uuid.UUID
	Scope     string
	Role      string
	UserID    uuid.UUID
	Tiles     json.RawMessage
	Version   int
	UpdatedBy string
	UpdatedAt time.Time
}

// DashboardLayouts keeps the arranged dashboards (FR-VIZ-09, FR-KPI-05).
type DashboardLayouts interface {
	// OfRole is a role's layout, or ports.ErrNotFound.
	OfRole(ctx context.Context, role string) (DashboardLayout, error)
	// OfUser is a person's own layout, or ports.ErrNotFound.
	OfUser(ctx context.Context, user uuid.UUID) (DashboardLayout, error)
	// SaveRole saves a role's layout: its first version when version is 0, else its next after version, refused with CodeVersion when it is no longer at version.
	SaveRole(ctx context.Context, role string, tiles json.RawMessage, by string, at time.Time, version int) (DashboardLayout, error)
	// SaveUser saves a person's own layout, as SaveRole does.
	SaveUser(ctx context.Context, user uuid.UUID, tiles json.RawMessage, by string, at time.Time, version int) (DashboardLayout, error)
}
