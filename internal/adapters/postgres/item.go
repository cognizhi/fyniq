package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) Items() storage.Items { return items{u} }

type items struct{ u *unitOfWork }

// itemRow is every item query's row: they all select the same columns.
type itemRow = sqlcdb.GetItemRow

func toItem(r itemRow) storage.Item {
	return storage.Item{ID: r.ID, Code: r.Code, Active: r.IsActive, Version: int(r.Version), ItemDetails: storage.ItemDetails{
		Name: r.Name, Description: str(r.Description), Type: storage.ItemType(r.Type), Unit: str(r.Unit),
		RevenueAccountID: r.RevenueAccountID, GSTCode: str(r.GstCode)}}
}

// withPrices reads the prices of the items, by currency.
func (r items) withPrices(ctx context.Context, its []storage.Item) error {
	if len(its) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(its))
	at := make(map[uuid.UUID]int, len(its))
	for i, it := range its {
		ids[i], at[it.ID] = it.ID, i
	}
	rows, err := r.u.q.ItemPrices(ctx, ids)
	if err != nil {
		return mapError(err)
	}
	for _, p := range rows {
		it := &its[at[p.ItemID]]
		it.Prices = append(it.Prices, storage.ItemPrice{Currency: p.Currency, UnitPrice: storage.Decimal(p.UnitPrice)})
	}
	return nil
}

// setPrices replaces the item's prices with ps: each one is checked as a plain decimal first, so nothing
// but a number reaches the column.
func (r items) setPrices(ctx context.Context, id uuid.UUID, ps []storage.ItemPrice) error {
	if err := r.u.q.DeleteItemPrices(ctx, id); err != nil {
		return mapError(err)
	}
	for _, p := range ps {
		if !p.UnitPrice.Valid() {
			return &storage.Error{Code: storage.CodeInvalid, Message: fmt.Sprintf("the %s unit price %q is not a plain decimal", p.Currency, p.UnitPrice),
				Hint: "Send digits with an optional decimal point, such as 120.50"}
		}
		if err := r.u.q.InsertItemPrice(ctx, sqlcdb.InsertItemPriceParams{ItemID: id, Currency: p.Currency, UnitPrice: string(p.UnitPrice)}); err != nil {
			return mapError(err)
		}
	}
	return nil
}

// prices is the item's prices as one value for its audit event, such as "SGD 120.000000, USD 90.000000".
func prices(ps []storage.ItemPrice) any {
	if len(ps) == 0 {
		return nil
	}
	parts := make([]string, len(ps))
	for i, p := range ps {
		parts[i] = p.Currency + " " + string(p.UnitPrice)
	}
	return strings.Join(parts, ", ")
}

// itemChanges are the fields that differ, by column name; a zero item gives every field.
func itemChanges(a, b storage.Item) map[string][2]any {
	out := map[string][2]any{}
	diff := func(k string, x, y any) {
		if fmt.Sprint(x) != fmt.Sprint(y) {
			out[k] = [2]any{x, y}
		}
	}
	account := func(p *uuid.UUID) any {
		if p == nil {
			return nil
		}
		return p.String()
	}
	diff("code", nilIfEmpty(a.Code), nilIfEmpty(b.Code))
	diff("name", nilIfEmpty(a.Name), nilIfEmpty(b.Name))
	diff("description", nilIfEmpty(a.Description), nilIfEmpty(b.Description))
	diff("type", nilIfEmpty(string(a.Type)), nilIfEmpty(string(b.Type)))
	diff("unit", nilIfEmpty(a.Unit), nilIfEmpty(b.Unit))
	diff("revenue_account_id", account(a.RevenueAccountID), account(b.RevenueAccountID))
	diff("gst_code", nilIfEmpty(a.GSTCode), nilIfEmpty(b.GSTCode))
	diff("prices", prices(a.Prices), prices(b.Prices))
	diff("is_active", a.Active, b.Active)
	return out
}

func (r items) Create(ctx context.Context, code string, d storage.ItemDetails) (storage.Item, error) {
	if err := r.u.check(); err != nil {
		return storage.Item{}, err
	}
	row, err := r.u.q.InsertItem(ctx, sqlcdb.InsertItemParams{Code: code, Name: d.Name, Description: opt(d.Description), Type: string(d.Type),
		Unit: opt(d.Unit), RevenueAccountID: d.RevenueAccountID, GstCode: opt(d.GSTCode)})
	if err != nil {
		return storage.Item{}, mapError(err)
	}
	it := toItem(itemRow(row))
	ps := []storage.Item{it}
	if err := r.setPrices(ctx, it.ID, d.Prices); err != nil {
		return storage.Item{}, err
	}
	if err := r.withPrices(ctx, ps); err != nil {
		return storage.Item{}, err
	}
	it = ps[0]
	if err := r.u.event(ctx, "item.created", "item", it.ID, &it.Version, itemChanges(storage.Item{}, it)); err != nil {
		return storage.Item{}, err
	}
	return it, nil
}

func (r items) Get(ctx context.Context, id uuid.UUID) (storage.Item, error) {
	if err := r.u.check(); err != nil {
		return storage.Item{}, err
	}
	row, err := r.u.q.GetItem(ctx, id)
	if err != nil {
		return storage.Item{}, mapError(err)
	}
	its := []storage.Item{toItem(row)}
	if err := r.withPrices(ctx, its); err != nil {
		return storage.Item{}, err
	}
	return its[0], nil
}

func (r items) Update(ctx context.Context, id uuid.UUID, version int, d storage.ItemDetails, active bool) (storage.Item, error) {
	if err := r.u.check(); err != nil {
		return storage.Item{}, err
	}
	cur, err := r.u.q.LockItem(ctx, id)
	if err != nil {
		return storage.Item{}, mapError(err)
	}
	before := []storage.Item{toItem(itemRow(cur))}
	if err := r.withPrices(ctx, before); err != nil {
		return storage.Item{}, err
	}
	if before[0].Version != version {
		return storage.Item{}, &storage.Error{Code: storage.CodeVersion,
			Message: fmt.Sprintf("the item changed since it was read: it is at version %d, not %d", before[0].Version, version)}
	}
	row, err := r.u.q.UpdateItem(ctx, sqlcdb.UpdateItemParams{ID: id, Name: d.Name, Description: opt(d.Description), Type: string(d.Type),
		Unit: opt(d.Unit), RevenueAccountID: d.RevenueAccountID, GstCode: opt(d.GSTCode), IsActive: active})
	if err != nil {
		return storage.Item{}, mapError(err)
	}
	if err := r.setPrices(ctx, id, d.Prices); err != nil {
		return storage.Item{}, err
	}
	after := []storage.Item{toItem(itemRow(row))}
	if err := r.withPrices(ctx, after); err != nil {
		return storage.Item{}, err
	}
	changes := itemChanges(before[0], after[0])
	changes["version"] = [2]any{before[0].Version, after[0].Version}
	if err := r.u.event(ctx, "item.updated", "item", id, &after[0].Version, changes); err != nil {
		return storage.Item{}, err
	}
	return after[0], nil
}

func (r items) Page(ctx context.Context, after string, limit int) ([]storage.Item, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 10000 {
		return nil, &storage.Error{Code: storage.CodeInvalid, Message: fmt.Sprintf("a page of %d items", limit), Hint: "Ask for 1 to 10,000"}
	}
	rows, err := r.u.q.PageItems(ctx, sqlcdb.PageItemsParams{After: after, PageSize: int32(limit)})
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.Item, 0, len(rows))
	for _, row := range rows {
		out = append(out, toItem(itemRow(row)))
	}
	if err := r.withPrices(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}
