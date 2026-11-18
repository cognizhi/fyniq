package storage

import (
	"context"

	"github.com/google/uuid"
)

// TenantTable is one of the organisation's tables as the full tenant export reads it (FR-MIG-04): its name and its
// columns in order.
type TenantTable struct {
	Name    string
	Columns []string
}

// MaxTenantRows is the most rows one page of TenantData.Rows holds.
const MaxTenantRows = 10000

// TenantData reads the organisation's tables whole, for the full tenant export (FR-MIG-04, design D10), under the
// caller's own grants and row-level security.
type TenantData interface {
	// Tables are every table of the schema that holds an organisation_id, partitions left out, by name.
	Tables(ctx context.Context) ([]TenantTable, error)
	// Rows are up to limit (at most MaxTenantRows) of the organisation's rows of table, each as JSON, after the row whose
	// id is after (nil: from the first), in id order; a table without an id comes whole.
	Rows(ctx context.Context, table string, after *uuid.UUID, limit int) ([]string, error)
}
