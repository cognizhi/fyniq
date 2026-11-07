package equity

import (
	"strings"
	"testing"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

// A SAFE's conversion at a priced round (FR-EQ-18, RND-SHARE, Appendix G.3, G4-044), every figure worked by hand:
//   - G4-044's post-money SAFE, S$125,000 with a S$2,500,000 cap, at the G.3 round (1,000,000 shares before it, S$3.00 a
//     share): its cap price is 2,500,000 × (1 − 125,000 ÷ 2,500,000) ÷ 1,000,000 = 2.375; 52,631 shares, S$124,998.63
//     converted (52,631 × 2.375 = 124,998.625), S$1.37 left on 2700; it converts by itself once its template is enabled;
//   - G.3's pre-money SAFE with the same cap and a 20% discount: 2,500,000 ÷ 1,000,000 = 2.50 against 3.00 × 0.80 =
//     2.40; 52,083 shares at 2.40, S$124,999.20, S$0.80 left; and the post-money one with the discount still takes 2.375;
//   - two post-money SAFEs, S$125,000 at a S$2,500,000 cap and S$250,000 at S$5,000,000, at S$5.00 a share: together
//     they convert into 0.05 + 0.05 of the company, so 2,500,000 × 0.9 ÷ 1,000,000 = 2.25 and 5,000,000 × 0.9 ÷ 1,000,000
//     = 4.5; 55,555 shares each (S$124,998.75 and S$249,997.50), 5% of the company each;
//   - a round at S$2.00, below G4-044's cap price: 62,500 shares at the round's price, for the accountant;
//   - a cap price of 1,000,000 ÷ 3,000,000 = 0.3333… cut to 4 decimals: 0.3333.
//
// req: FR-EQ-18
// req: RND-SHARE
func TestASAFEConvertsAtAPricedRound(t *testing.T) {
	sgd, err := money.Lookup("SGD")
	if err != nil {
		t.Fatal(err)
	}
	amount := func(s string) money.Money {
		m, err := money.Parse(s, sgd)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	round := func(price, before string) Round {
		p, err := money.ParseUnitPrice(price, sgd, "price")
		if err != nil {
			t.Fatal(err)
		}
		var b apd.Decimal
		if _, _, err := b.SetString(before); err != nil {
			t.Fatal(err)
		}
		return Round{Price: p, Before: b}
	}
	safe := func(form, money, cap, discount string) Converting {
		i := Instrument{Form: form, Amount: amount(money), ValuationCap: cap, Discount: discount, Trigger: "priced_round",
			Denominator: "post_money_capitalisation", PriceDecimals: 4}
		if form == FormPostMoneySAFE && discount == "" {
			i.Template = "yc-sg-post-money-cap@1"
		}
		if form == FormPreMoneySAFE {
			i.Denominator = "pre_money_shares"
		}
		if form == FormMFNSAFE {
			i.MFN = true
		}
		return Converting{ID: uuid.New(), Instrument: i}
	}
	on := func(string) bool { return true }
	off := func(string) bool { return false }
	type want struct{ cap, discount, price, shares, converted, left, uncovered string }
	check := func(name string, got Conversion, w want) {
		t.Helper()
		g := want{got.CapPrice, got.DiscountPrice, got.Price.String(), got.Shares.Text('f'), got.Converted.String(), got.Left.String(),
			strings.Join(got.Uncovered, "; ")}
		if g != w {
			t.Errorf("%s:\n got %+v\nwant %+v", name, g, w)
		}
		if got.Covered != (w.uncovered == "") {
			t.Errorf("%s: covered %v with %q", name, got.Covered, w.uncovered)
		}
	}
	convert := func(r Round, enabled func(string) bool, safes ...Converting) []Conversion {
		t.Helper()
		out, err := Convert(r, safes, enabled)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}

	g3 := round("3.00", "1000000")
	check("G4-044", convert(g3, on, safe(FormPostMoneySAFE, "125000.00", "2500000", ""))[0],
		want{"2.375", "", "2.375", "52631", "124998.63", "1.37", ""})
	check("G4-044 with its template not enabled", convert(g3, off, safe(FormPostMoneySAFE, "125000.00", "2500000", ""))[0],
		want{"2.375", "", "2.375", "52631", "124998.63", "1.37", "its template is not enabled"})
	check("G.3 pre-money", convert(g3, on, safe(FormPreMoneySAFE, "125000.00", "2500000", "20"))[0],
		want{"2.5", "2.4", "2.40", "52083", "124999.20", "0.80", "it follows no template; a discount"})
	check("G.3 post-money with the discount", convert(g3, on, safe(FormPostMoneySAFE, "125000.00", "2500000", "20"))[0],
		want{"2.375", "2.4", "2.375", "52631", "124998.63", "1.37", "it follows no template; a discount"})

	two := convert(round("5.00", "1000000"), on, safe(FormPostMoneySAFE, "125000.00", "2500000", ""), safe(FormPostMoneySAFE, "250000.00", "5000000", ""))
	check("the first of two", two[0], want{"2.25", "", "2.25", "55555", "124998.75", "1.25", ""})
	check("the second of two", two[1], want{"4.5", "", "4.50", "55555", "249997.50", "2.50", ""})

	check("a round below the cap price", convert(round("2.00", "1000000"), on, safe(FormPostMoneySAFE, "125000.00", "2500000", ""))[0],
		want{"2.375", "", "2.00", "62500", "125000.00", "0.00", "the round's price is below its cap price"})
	check("a cap price cut to 4 decimals", convert(round("1.00", "3000000"), on, safe(FormPreMoneySAFE, "1000.00", "1000000", ""))[0],
		want{"0.3333333333", "", "0.3333", "3000", "999.90", "0.10", "it follows no template"})
	check("an MFN SAFE at the round's price", convert(g3, on, safe(FormMFNSAFE, "30000.00", "", ""))[0],
		want{"", "", "3.00", "10000", "30000.00", "0.00", "it follows no template; a most-favoured-nation clause"})

	if _, err := Convert(round("3.00", "0"), []Converting{safe(FormPostMoneySAFE, "125000.00", "2500000", "")}, on); err == nil {
		t.Error("no shares before the round")
	}
	if _, err := Convert(g3, []Converting{safe(FormPostMoneySAFE, "2500000.00", "2500000", "")}, on); err == nil {
		t.Error("SAFEs that convert into the whole company")
	}
}
