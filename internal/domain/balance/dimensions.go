package balance

import (
	"context"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// SetRow is one account's base-currency total for a period on the lines of one dimension set (FR-DIM-02): Values are
// the set's values in ascending order, nil for the lines with no dimension.
type SetRow struct {
	AccountID uuid.UUID
	Values    []uuid.UUID
	Base      money.Money
}

// BySet is a period's base-currency totals by account and dimension set, each set's currencies summed, by account and
// then set, the lines with no dimension first (FR-DIM-02). Appendix C.2 #60 reads it for the amounts with no value of a
// dimension an account requires.
func BySet(ctx context.Context, u storage.UnitOfWork, base money.Currency, period uuid.UUID) ([]SetRow, error) {
	rows, err := u.Balances().ForPeriodBySet(ctx, period)
	if err != nil {
		return nil, err
	}
	var out []SetRow
	at := map[string]int{} // account and set → its row in out
	for _, r := range rows {
		amount, err := money.ParseStored(string(r.BaseAmount), base)
		if err != nil {
			return nil, err
		}
		k := setKey(r.AccountID, r.Values)
		i, ok := at[k]
		if !ok {
			at[k] = len(out)
			out = append(out, SetRow{AccountID: r.AccountID, Values: r.Values, Base: amount})
			continue
		}
		if out[i].Base, err = out[i].Base.Add(amount); err != nil {
			return nil, err
		}
	}
	slices.SortStableFunc(out, func(a, b SetRow) int {
		if c := strings.Compare(a.AccountID.String(), b.AccountID.String()); c != 0 {
			return c
		}
		return strings.Compare(setKey(uuid.Nil, a.Values), setKey(uuid.Nil, b.Values))
	})
	return out, nil
}

func setKey(account uuid.UUID, values []uuid.UUID) string {
	parts := []string{account.String()}
	for _, v := range values {
		parts = append(parts, v.String())
	}
	return strings.Join(parts, ",")
}
