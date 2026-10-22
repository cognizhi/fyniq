package ledger

import (
	"testing"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

func rateOf(t *testing.T, s string, from money.Currency) money.Rate {
	t.Helper()
	r, err := money.ParseRate(s, from, sgd, "test")
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// G4-039's receipt, worked by hand: USD 4,000.00 of an invoice stored at 1.35 (S$5,400.00) received at 1.32
// (S$5,280.00) realises a S$120.00 loss, debited to 7030; paying USD 1,000.00 of a bill stored at 1.35 at 1.30 realises a
// S$50.00 gain. Realised FX takes no tolerance (FR-BNK-08).
// req: ENG-41
func TestRealisedFXIsTheSettledAmountAtTheTwoRates(t *testing.T) {
	for _, c := range []struct {
		amount, settlement, stored, want string
	}{
		{"4000.00", "1.32", "1.35", "-120.00"}, // a receivable received for less: a loss
		{"-1000.00", "1.30", "1.35", "50.00"},  // a payable paid for less: a gain
		{"0.01", "1.3549", "1.35", "0.00"},     // under a cent apart: RND-FX of each side, 0.01 and 0.01
		{"333.33", "1.335", "1.3349", "0.04"},  // 445.00 (444.995 away from zero) − 444.96 (444.9622…)
	} {
		got, err := RealisedFX(amt(t, c.amount, usd), rateOf(t, c.settlement, usd), rateOf(t, c.stored, usd))
		if err != nil || got.String() != c.want || got.Currency() != sgd {
			t.Errorf("%s at %s, stored at %s: want %s SGD, got %v %v", c.amount, c.settlement, c.stored, c.want, got, err)
		}
	}
	if _, err := RealisedFX(amt(t, "1.00", usd), rateOf(t, "0.0091", jpy), rateOf(t, "1.35", usd)); err == nil {
		t.Error("a rate of another currency: want refused")
	}
}

// G4-023, worked by hand: a USD bill of 1,000.00 net + 90.00 GST = 1,090.00 at the book rate 1.34, whose tax invoice
// states S$121.50 of GST: Dr expense 1,340.00, Dr 2200 121.50 as stated (not 120.60 at the book rate), Cr AP 1,460.60,
// Cr 7030 0.90, and the entry balances. Without a stated SGD GST the GST stays in the cost: Dr expense 1,460.60, Cr AP
// 1,460.60. An average rate never sets a GST amount.
// req: ENG-42
func TestForeignInputTaxIsAsStatedInSGD(t *testing.T) {
	stated := amt(t, "121.50", sgd)
	got, err := ForeignInputTax(amt(t, "1000.00", usd), amt(t, "90.00", usd), rateOf(t, "1.34", usd), false, &stated)
	if err != nil {
		t.Fatal(err)
	}
	if got.Net.String() != "1340.00" || got.GST.String() != "121.50" || got.Gross.String() != "1460.60" || got.Difference.String() != "0.90" || !got.Claimed {
		t.Errorf("G4-023: %+v", got)
	}
	debits, _ := got.Net.Add(got.GST)
	credits, _ := got.Gross.Add(got.Difference)
	if !debits.Equal(credits) {
		t.Errorf("G4-023 does not balance: %s against %s", debits, credits)
	}
	none, err := ForeignInputTax(amt(t, "1000.00", usd), amt(t, "90.00", usd), rateOf(t, "1.34", usd), false, nil)
	if err != nil || none.Claimed || none.Net.String() != "1460.60" || !none.GST.IsZero() || !none.Difference.IsZero() {
		t.Errorf("no stated GST: %+v %v", none, err)
	}
	// A cent of rounding between the net and the GST at the book rate goes to 7030 with the difference, so the entry
	// balances: 0.01 + 0.01 USD at 1.5 is 0.02 + 0.02 apart but 0.03 together (RND-FX of 0.015 and of 0.03).
	odd, err := ForeignInputTax(amt(t, "0.01", usd), amt(t, "0.01", usd), rateOf(t, "1.5", usd), false, &stated)
	if err != nil {
		t.Fatal(err)
	}
	debits, _ = odd.Net.Add(odd.GST)
	credits, _ = odd.Gross.Add(odd.Difference)
	if !debits.Equal(credits) || odd.Gross.String() != "0.03" {
		t.Errorf("a cent of rounding: %+v", odd)
	}
	if _, err := ForeignInputTax(amt(t, "1000.00", usd), amt(t, "90.00", usd), rateOf(t, "1.34", usd), true, &stated); err == nil {
		t.Error("an average rate: want refused")
	}
	other := amt(t, "121.50", usd)
	if _, err := ForeignInputTax(amt(t, "1000.00", usd), amt(t, "90.00", usd), rateOf(t, "1.34", usd), false, &other); err == nil {
		t.Error("a stated GST not in SGD: want refused")
	}
}
