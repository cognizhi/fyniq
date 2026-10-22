package ledger

import (
	"fmt"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
)

// Realised FX and GST on foreign-currency documents (ENG-41, ENG-42; PRD-001 rev 0.74): the arithmetic the posting
// rules of invoices, bills and their settlements take, kept here so that sales, purchases and bank import share one
// version of it. Each is exact: RND-FX converts, and nothing else rounds.

// RealisedFX is the realised gain or loss of settling amount of a foreign-currency document at the settlement rate,
// against the document's stored rate (ENG-41; FR-BNK-08): RND-FX(amount × settlement rate) − RND-FX(amount × stored
// rate), in the base currency, with no tolerance. For a receivable (amount positive, money in) a positive result is a
// gain, credited to 7030; for a payable (amount negative) the signs follow the amount. Both rates convert the amount's
// currency into the same base.
func RealisedFX(amount money.Money, settlement, stored money.Rate) (money.Money, error) {
	if settlement.From != amount.Currency() || stored.From != amount.Currency() || settlement.To != stored.To {
		return money.Money{}, &money.Error{Code: money.CodeRate, Field: "fx_rate",
			Message: fmt.Sprintf("the rates convert %s→%s and %s→%s, but the amount is in %s", settlement.From, settlement.To, stored.From, stored.To, amount.Currency())}
	}
	now, err := rounding.FX(amount, settlement)
	if err != nil {
		return money.Money{}, err
	}
	then, err := rounding.FX(amount, stored)
	if err != nil {
		return money.Money{}, err
	}
	return now.Sub(then)
}

// InputTax is the base-currency split of a foreign-currency bill's GST (ENG-42): Net and Gross at the book rate, GST
// as the supplier's tax invoice (or the import permit) states it in SGD, and Difference, the stated GST less the GST at
// the book rate, which posts to 7030 on the same entry. Without a stated SGD GST the input tax is not claimed: the GST
// stays in the cost (as for code BL), Net carries it, and Claimed is false (⚠ "no SGD GST stated on the tax invoice;
// input tax not claimed").
type InputTax struct {
	Net        money.Money // the cost, in the base currency
	GST        money.Money // the input tax claimed, as stated
	Gross      money.Money // what is owed, at the book rate
	Difference money.Money // net + stated GST − gross: the stated GST less the GST at the book rate, a credit to 7030 when positive
	Claimed    bool
}

// ForeignInputTax splits a foreign-currency bill's net and GST at the book rate, taking the input tax as stated in SGD
// (ENG-42; G4-023). stated is nil when the tax invoice states no SGD GST. A rate that is an average of the monthly table
// never sets a GST amount (ENG-40): average refuses it.
func ForeignInputTax(net, gst money.Money, rate money.Rate, average bool, stated *money.Money) (InputTax, error) {
	if average {
		return InputTax{}, &money.Error{Code: money.CodeRate, Field: "fx_rate", Message: "an average of the monthly table's rates never sets a GST amount",
			Fix: "Enter the day's rate, or the rate on the tax invoice"}
	}
	if net.Currency() != gst.Currency() {
		return InputTax{}, &money.Error{Code: money.CodeCurrency, Field: "gst", Message: fmt.Sprintf("the net is in %s and the GST in %s", net.Currency(), gst.Currency())}
	}
	gross, err := net.Add(gst)
	if err != nil {
		return InputTax{}, err
	}
	out := InputTax{}
	if out.Gross, err = rounding.FX(gross, rate); err != nil {
		return InputTax{}, err
	}
	if stated == nil {
		out.Net, out.GST, out.Difference = out.Gross, money.Zero(rate.To), money.Zero(rate.To)
		return out, nil
	}
	if stated.Currency() != rate.To {
		return InputTax{}, &money.Error{Code: money.CodeCurrency, Field: "stated_gst", Message: fmt.Sprintf("the stated GST is in %s, not %s", stated.Currency(), rate.To)}
	}
	if out.Net, err = rounding.FX(net, rate); err != nil {
		return InputTax{}, err
	}
	// Net + GST at the book rate is the gross at the book rate but for a cent of RND-FX at most; the difference takes it
	// with the stated GST's, so the entry balances: Dr net, Dr GST as stated, Cr gross, Cr the difference.
	withStated, err := out.Net.Add(*stated)
	if err != nil {
		return InputTax{}, err
	}
	if out.Difference, err = withStated.Sub(out.Gross); err != nil {
		return InputTax{}, err
	}
	out.GST, out.Claimed = *stated, true
	return out, nil
}
