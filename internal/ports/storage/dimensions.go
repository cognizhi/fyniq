package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// The kinds of dimension (FR-DIM-01): Department and Project once each, and up to 8 custom dimensions in places 1 to 8.
const (
	DimensionDepartment = "department"
	DimensionProject    = "project"
	DimensionCustom     = "custom"
)

// Dimension is one of the organisation's dimensions (FR-DIM-01). Place is a custom dimension's place, 1 to 8, and 0
// for Department and Project. One out of use is kept, as dimension sets name its values; its kind and place never
// change.
type Dimension struct {
	ID        uuid.UUID
	Kind      string
	Place     int
	Name      string
	Active    bool
	Version   int
	CreatedAt time.Time
}

// NewDimension adds a dimension; Place is 0 except for a custom one.
type NewDimension struct {
	Kind  string
	Place int
	Name  string
}

// DimensionChange is what changes of a dimension or a dimension value: its name, and whether it is in use.
type DimensionChange struct {
	Name   string
	Active bool
}

// DimensionValue is a value of a dimension (FR-DIM-01), such as the Engineering department. Its dimension and code
// never change; one out of use is kept, as dimension sets name it.
type DimensionValue struct {
	ID          uuid.UUID
	DimensionID uuid.UUID
	Code        string
	Name        string
	Active      bool
	Version     int
	CreatedAt   time.Time
}

// NewDimensionValue adds a value to a dimension.
type NewDimensionValue struct {
	DimensionID uuid.UUID
	Code        string
	Name        string
}

// Dimensions are the organisation's dimensions and their values (FR-DIM-01). Each write records its audit event in the
// unit of work (AUD-01).
type Dimensions interface {
	// List is every dimension: Department, Project, then the custom ones by place. Get is one, ports.ErrNotFound for none.
	List(ctx context.Context) ([]Dimension, error)
	Get(ctx context.Context, id uuid.UUID) (Dimension, error)
	Create(ctx context.Context, n NewDimension) (Dimension, error)
	// Change changes a dimension at version: CodeVersion when it changed since.
	Change(ctx context.Context, id uuid.UUID, version int, c DimensionChange) (Dimension, error)
	// Values are the values of a dimension, or of every dimension when dimension is nil, by dimension as List orders
	// them, then by code. GetValue is one, ports.ErrNotFound for none.
	Values(ctx context.Context, dimension *uuid.UUID) ([]DimensionValue, error)
	GetValue(ctx context.Context, id uuid.UUID) (DimensionValue, error)
	CreateValue(ctx context.Context, n NewDimensionValue) (DimensionValue, error)
	// ChangeValue changes a value at version: CodeVersion when it changed since.
	ChangeValue(ctx context.Context, id uuid.UUID, version int, c DimensionChange) (DimensionValue, error)
}
