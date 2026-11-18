package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the full tenant export's reads (FR-MIG-04)

func (u *unitOfWork) TenantData() storage.TenantData { return tenantData{u} }

type tenantData struct{ u *unitOfWork }

func (r tenantData) Tables(ctx context.Context) ([]storage.TenantTable, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.TenantTables(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.TenantTable, len(rows))
	for i, x := range rows {
		out[i] = storage.TenantTable{Name: x.Name, Columns: x.Columns}
	}
	return out, nil
}

func (r tenantData) Rows(ctx context.Context, table string, after *uuid.UUID, limit int) ([]string, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	if limit < 1 || limit > storage.MaxTenantRows {
		return nil, fmt.Errorf("postgres: a page of %d rows; at most %d", limit, storage.MaxTenantRows)
	}
	rows, err := r.u.q.TenantRows(ctx, sqlcdb.TenantRowsParams{TableName: table, After: after, RowLimit: int32(limit)}) // #nosec G115 -- limit is checked above
	if err != nil {
		return nil, mapError(err)
	}
	return rows, nil
}
