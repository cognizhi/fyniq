package payroll

import (
	"github.com/cockroachdb/apd/v3"
)

// What each pay item of a year is in the IR8A (PRD-002 NM-PAY-14; p2-payroll-outputs design D2). The mapping from a pay item's nature to an IR8A box is the accountant's decision, made
// here on IRAS's own item names [accountant to confirm]: basic pay, overtime, other ordinary items and unpaid leave are the salary (C3); a bonus is the bonus (C4); an allowance and
// anything else is the allowances (C9); a commission is the gross commission (C10); notice pay is the gratuity, notice pay and ex-gratia payment (C11); a retrenchment benefit is the compensation for
// loss of office (C12); leave pay on leaving is the salary. An item with no nature is classed by its CPF class: ordinary wages are the salary and additional wages the bonus.

// MapItem is a pay item of a person's year: its nature, its CPF class and its amount.
type MapItem struct {
	Nature, Class string
	Amount        *apd.Decimal
}

// IR8ABoxes are a person's year in the IR8A's own items, in whole dollars, nil for a box with nothing in it.
type IR8ABoxes struct {
	Salary, Bonus, Allowances, Commission, Gratuity, CompensationForLoss *int64
}

var round0 = func() *apd.Context {
	c := apd.BaseContext.WithPrecision(40)
	c.Rounding = apd.RoundHalfUp
	return c
}()

// wholeDollars rounds a sum to whole dollars, half away from zero (IRAS's items are Number(9)) [accountant to confirm].
func wholeDollars(d *apd.Decimal) (int64, bool) {
	var r apd.Decimal
	if _, err := round0.Quantize(&r, d, 0); err != nil {
		return 0, false
	}
	n, err := r.Int64()
	return n, err == nil
}

// IR8AFrom sums a year's items into the IR8A's boxes: each box is the sum of its items rounded once to whole dollars; a box whose items sum to nothing is blank.
func IR8AFrom(items []MapItem) IR8ABoxes {
	sums := map[string]*apd.Decimal{}
	for _, it := range items {
		if it.Amount == nil {
			continue
		}
		box := "allowances"
		switch it.Nature {
		case "basic", "overtime", "unpaid_leave", NatureLeavePay:
			box = "salary"
		case "bonus":
			box = "bonus"
		case "commission":
			box = "commission"
		case NatureNoticePay:
			box = "gratuity"
		case NatureRetrenchment:
			box = "loss"
		case "", "other":
			switch it.Class {
			case Ordinary:
				box = "salary"
			case Additional:
				box = "bonus"
			}
		}
		if sums[box] == nil {
			sums[box] = apd.New(0, 0)
		}
		if _, err := work.Add(sums[box], sums[box], it.Amount); err != nil {
			panic("payroll: " + err.Error())
		}
	}
	pick := func(box string) *int64 {
		d := sums[box]
		if d == nil || d.IsZero() {
			return nil
		}
		n, ok := wholeDollars(d)
		if !ok {
			return nil
		}
		return &n
	}
	return IR8ABoxes{Salary: pick("salary"), Bonus: pick("bonus"), Allowances: pick("allowances"), Commission: pick("commission"), Gratuity: pick("gratuity"), CompensationForLoss: pick("loss")}
}
