package closing

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// dimensionChecks: an account that requires a dimension in use, with a movement in the period on lines with no value of
// it (Appendix C.2 #60, ⚠; FR-DIM-01, rev 0.171). A person's journal line cannot leave one (DIM-05), so a posting the
// system made by itself did; a journal whose lines on the account net to zero moves it to its value (rev 0.172). Nothing
// when no account requires a dimension in use.
func dimensionChecks(ctx context.Context, u storage.UnitOfWork, cur money.Currency, p storage.Period) ([]Check, error) {
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return nil, err
	}
	dims, err := u.Dimensions().List(ctx)
	if err != nil {
		return nil, err
	}
	inUse := map[uuid.UUID]storage.Dimension{}
	for _, d := range dims {
		if d.Active {
			inUse[d.ID] = d
		}
	}
	requiring := slices.DeleteFunc(slices.Clone(accounts), func(a storage.Account) bool {
		return !slices.ContainsFunc(a.RequiredDimensions, func(d uuid.UUID) bool { _, ok := inUse[d]; return ok })
	})
	if len(requiring) == 0 {
		return nil, nil
	}
	values, err := u.Dimensions().Values(ctx, nil)
	if err != nil {
		return nil, err
	}
	of := map[uuid.UUID]uuid.UUID{} // value → its dimension
	for _, v := range values {
		of[v.ID] = v.DimensionID
	}
	rows, err := balance.BySet(ctx, u, cur, p.ID)
	if err != nil {
		return nil, err
	}
	var found []string
	for _, a := range requiring {
		for _, d := range a.RequiredDimensions {
			dim, ok := inUse[d]
			if !ok {
				continue
			}
			without := money.Zero(cur)
			for _, r := range rows {
				if r.AccountID != a.ID || slices.ContainsFunc(r.Values, func(v uuid.UUID) bool { return of[v] == d }) {
					continue
				}
				if without, err = without.Add(r.Base); err != nil {
					return nil, err
				}
			}
			if !without.IsZero() {
				found = append(found, fmt.Sprintf("%s %s: %s %s with no %s", a.Code, a.Name, without, cur.Code, dim.Name))
			}
		}
	}
	if len(found) == 0 {
		return []Check{{"dimensions", OK, "Every amount carries the dimensions its account requires", ""}}, nil
	}
	return []Check{{"dimensions", Warning, "Amounts lack a dimension their account requires",
		listed(found) + ": move each to its value with a journal whose lines on the account net to zero"}}, nil
}
