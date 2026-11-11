package retention

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

var (
	sgd = money.MustLookup("SGD")
	usd = money.MustLookup("USD")
)

func m(s string, c money.Currency) money.Money {
	x, err := money.Parse(s, c)
	if err != nil {
		panic(err)
	}
	return x
}

var chart = map[string]Account{
	"bank": {Code: "1010", Type: "asset"}, "usd": {Code: "1020", Type: "asset"},
	"ar": {Code: "1100", Type: "asset", Role: AccountsRec}, "gst": {Code: "2200", Type: "liability"},
	"ap": {Code: "2010", Type: "liability", Role: AccountsPay}, "loan": {Code: "2500", Type: "liability", Control: true},
	"suspense": {Code: "2999", Type: "liability", Role: "suspense"}, "capital": {Code: "3010", Type: "equity"},
	"re": {Code: "3100", Type: "equity", Role: RetainedEarnings}, "revenue": {Code: "4010", Type: Revenue},
	"expense": {Code: "6010", Type: Expense},
}

func l(src, id, account, set, amount string, cur money.Currency, base string) Line {
	return Line{Source: Source{src, id}, Account: account, DimensionSet: set, Amount: m(amount, cur), Base: m(base, sgd)}
}

// show is an entry as text: its source, then each line as account@set currency amount base.
func show(e Entry) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s:", e.Source.Type, e.Source.ID)
	for _, x := range e.Lines {
		fmt.Fprintf(&b, " %s@%s %s %s %s", chart[x.Account].Code, x.DimensionSet, x.Amount.Currency().Code, x.Amount, x.Base)
	}
	return b.String()
}

// A year's lines carried forward by hand (AUD-10, the rule of rev 0.238). In FY2025 (SGD books):
//   - INV-1 owes 1,090.00 on receivables, 1,000.00 of revenue for "sales" and 90.00 of GST; PAY-1 pays it to the bank.
//   - INV-2 owes 545.00, 500.00 of revenue for "eng" and 45.00 of GST, still open.
//   - BILL-1 is 300.00 of expense owed on payables.
//   - INV-3 owes USD 100.00 at 1.35: 135.00 of revenue.
//   - SHR-1 puts USD 50.00 at 1.3333333333 (66.67) in the USD bank for share capital.
//   - LOAN-1 is a director's loan of 200.00 into the bank, a control account by its flag.
//
// The boundary's entry carries the bank (1,290.00), GST (-135.00), the USD bank (USD 50.00 at 1.3334, 66.67), share
// capital (-66.67) and retained earnings by dimension set: "" 165.00 (300.00 of expense, -135.00 of USD revenue),
// "eng" -500.00, "sales" -1,000.00. Each source's control lines carry in an entry of its own, balanced through
// suspense: INV-1 1,090.00, PAY-1 -1,090.00, INV-2 545.00, BILL-1 -300.00, INV-3 USD 100.00 at 1.35 (135.00),
// LOAN-1 -200.00. Their offsets sum to 180.00, which the boundary's entry takes back on suspense, so suspense nets to zero.
// req: AUD-10
// req: INV-01
// req: INV-07
func TestTheCarryForwardOfAYearByHand(t *testing.T) {
	in := Input{Base: sgd, Accounts: chart, RetainedEarnings: "re", Suspense: "suspense", Boundary: Source{"ledger_boundary", "B"}, Lines: []Line{
		l("invoice", "INV-1", "ar", "", "1090.00", sgd, "1090.00"), l("invoice", "INV-1", "revenue", "sales", "-1000.00", sgd, "-1000.00"),
		l("invoice", "INV-1", "gst", "", "-90.00", sgd, "-90.00"),
		l("payment", "PAY-1", "bank", "", "1090.00", sgd, "1090.00"), l("payment", "PAY-1", "ar", "", "-1090.00", sgd, "-1090.00"),
		l("invoice", "INV-2", "ar", "", "545.00", sgd, "545.00"), l("invoice", "INV-2", "revenue", "eng", "-500.00", sgd, "-500.00"),
		l("invoice", "INV-2", "gst", "", "-45.00", sgd, "-45.00"),
		l("bill", "BILL-1", "expense", "", "300.00", sgd, "300.00"), l("bill", "BILL-1", "ap", "", "-300.00", sgd, "-300.00"),
		l("invoice", "INV-3", "ar", "", "100.00", usd, "135.00"), l("invoice", "INV-3", "revenue", "", "-100.00", usd, "-135.00"),
		l("share_issue", "SHR-1", "usd", "", "50.00", usd, "66.67"), l("share_issue", "SHR-1", "capital", "", "-66.67", sgd, "-66.67"),
		l("journal", "LOAN-1", "bank", "", "200.00", sgd, "200.00"), l("journal", "LOAN-1", "loan", "", "-200.00", sgd, "-200.00"),
	}}
	got, err := CarryForward(in)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"ledger_boundary B: 1010@ SGD 1290.00 1290.00 1020@ USD 50.00 66.67 2200@ SGD -135.00 -135.00 3010@ SGD -66.67 -66.67" +
			" 3100@ SGD 165.00 165.00 3100@eng SGD -500.00 -500.00 3100@sales SGD -1000.00 -1000.00 2999@ SGD 180.00 180.00",
		"bill BILL-1: 2010@ SGD -300.00 -300.00 2999@ SGD 300.00 300.00",
		"invoice INV-1: 1100@ SGD 1090.00 1090.00 2999@ SGD -1090.00 -1090.00",
		"invoice INV-2: 1100@ SGD 545.00 545.00 2999@ SGD -545.00 -545.00",
		"invoice INV-3: 1100@ USD 100.00 135.00 2999@ SGD -135.00 -135.00",
		"journal LOAN-1: 2500@ SGD -200.00 -200.00 2999@ SGD 200.00 200.00",
		"payment PAY-1: 1100@ SGD -1090.00 -1090.00 2999@ SGD 1090.00 1090.00",
	}
	if len(got) != len(want) {
		t.Fatalf("%d entries, want %d:\n%v", len(got), len(want), got)
	}
	for i := range want {
		if s := show(got[i]); s != want[i] {
			t.Errorf("entry %d:\n got %s\nwant %s", i, s, want[i])
		}
	}
	for _, e := range got {
		for _, x := range e.Lines {
			if x.Account == "usd" && x.Rate.Decimal().Text('f') != "1.3334" {
				t.Errorf("the USD bank's rate: %s", x.Rate.Decimal().Text('f'))
			}
			if x.Account == "ar" && x.Amount.Currency() == usd && x.Rate.Decimal().Text('f') != "1.35" {
				t.Errorf("INV-3's rate: %s", x.Rate.Decimal().Text('f'))
			}
		}
	}
}

// Each person's and each instrument's balance carries on its own (FR-EQ-01, FR-EQ-18, INV-13): one journal lends 300.00
// from P-1 and 200.00 from P-2 on the director's loan, a control account, and another puts USD 100.00 (135.00) into
// S-1 and 50.00 into S-2 on the convertible instruments, which is not; both persons and both instruments keep their
// own line.
// req: AUD-10
// req: INV-13
func TestEachPersonAndInstrumentKeepsTheirBalance(t *testing.T) {
	accounts := map[string]Account{"safe": {Code: "2700", Type: "liability", Role: "convertible-instruments"}}
	for k, a := range chart {
		accounts[k] = a
	}
	tag := func(x Line, party, instrument string) Line { x.Party, x.Instrument = party, instrument; return x }
	in := Input{Base: sgd, Accounts: accounts, RetainedEarnings: "re", Suspense: "suspense", Boundary: Source{BoundarySource, "B"}, Lines: []Line{
		l("journal", "LOAN-2", "bank", "", "500.00", sgd, "500.00"),
		tag(l("journal", "LOAN-2", "loan", "", "-300.00", sgd, "-300.00"), "P-1", ""), tag(l("journal", "LOAN-2", "loan", "", "-200.00", sgd, "-200.00"), "P-2", ""),
		l("journal", "SAFE-1", "usd", "", "100.00", usd, "135.00"), l("journal", "SAFE-1", "bank", "", "50.00", sgd, "50.00"),
		tag(l("journal", "SAFE-1", "safe", "", "-100.00", usd, "-135.00"), "", "S-1"), tag(l("journal", "SAFE-1", "safe", "", "-50.00", sgd, "-50.00"), "", "S-2"),
	}}
	got, err := CarryForward(in)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, e := range got {
		fmt.Fprintf(&b, "%s %s:", e.Source.Type, e.Source.ID)
		for _, x := range e.Lines {
			fmt.Fprintf(&b, " %s/%s/%s %s %s", accounts[x.Account].Code, x.Party, x.Instrument, x.Amount.Currency().Code, x.Amount)
		}
		b.WriteString("\n")
	}
	// Keys run by currency before instrument; the boundary's entry takes back the loan's 500.00 on suspense.
	want := "ledger_boundary B: 1010// SGD 550.00 1020// USD 100.00 2700//S-2 SGD -50.00 2700//S-1 USD -100.00 2999// SGD -500.00\n" +
		"journal LOAN-2: 2500/P-1/ SGD -300.00 2500/P-2/ SGD -200.00 2999// SGD 500.00\n"
	if b.String() != want {
		t.Errorf("got\n%swant\n%s", b.String(), want)
	}
}

// A document whose control lines net to zero carries nothing: INV-9 posted and reversed under its own source leaves no
// open item, and its revenue nets to zero in retained earnings, so the year carries no entry at all (INV-07).
// req: AUD-10
// req: INV-07
func TestADocumentThatNetsToZeroCarriesNothing(t *testing.T) {
	in := Input{Base: sgd, Accounts: chart, RetainedEarnings: "re", Suspense: "suspense", Boundary: Source{BoundarySource, "B"}, Lines: []Line{
		l("invoice", "INV-9", "ar", "", "109.00", sgd, "109.00"), l("invoice", "INV-9", "revenue", "", "-100.00", sgd, "-100.00"),
		l("invoice", "INV-9", "gst", "", "-9.00", sgd, "-9.00"),
		l("invoice", "INV-9", "ar", "", "-109.00", sgd, "-109.00"), l("invoice", "INV-9", "revenue", "", "100.00", sgd, "100.00"),
		l("invoice", "INV-9", "gst", "", "9.00", sgd, "9.00"),
	}}
	got, err := CarryForward(in)
	if err != nil || len(got) != 0 {
		t.Errorf("carried %v, %v", got, err)
	}
}

// A foreign balance whose base no rate of 10 decimals gives exactly is carried as a line at the nearest rate and a
// base-only line for the residual; a balance of zero foreign and some base (a revaluation's) is one base-only line;
// a balance of zero in both is not carried; a balance whose rate would not be above zero, or would not fit 10 integer
// digits, is carried at 1 with the rest of its base base-only (INV-05).
// req: AUD-10
// req: INV-05
func TestTheCarryForwardOfForeignResiduals(t *testing.T) {
	in := Input{Base: sgd, Accounts: chart, RetainedEarnings: "re", Suspense: "suspense", Boundary: Source{"ledger_boundary", "B"}, Lines: []Line{
		l("journal", "J1", "usd", "", "999999999.99", usd, "1234567890.12"), l("journal", "J1", "capital", "", "-1234567890.12", sgd, "-1234567890.12"),
		l("journal", "J2", "usd", "", "0.00", usd, "5.00"), l("journal", "J2", "capital", "", "-5.00", sgd, "-5.00"),
		l("journal", "J3", "gst", "", "10.00", sgd, "10.00"), l("journal", "J4", "gst", "", "-10.00", sgd, "-10.00"),
	}}
	got, err := CarryForward(in)
	if err != nil {
		t.Fatal(err)
	}
	// The USD balance is 999,999,999.99 with a base of 1,234,567,895.12 (J2's revaluation of 5.00 included). Base ÷ amount
	// rounds to the rate 1.2345678951, which gives 1,234,567,895.09, so a base-only line of 0.03 follows it.
	want := "ledger_boundary B: 1020@ USD 999999999.99 1234567895.09 1020@ USD 0.00 0.03 3010@ SGD -1234567895.12 -1234567895.12"
	if len(got) != 1 {
		t.Fatalf("entries: %v", got)
	}
	lines := got[0].Lines
	if s := show(got[0]); s != want || lines[0].Rate.Decimal().Text('f') != "1.2345678951" {
		t.Errorf("the USD balance's lines:\n got %s\nwant %s", show(got[0]), want)
	}

	// USD 0.01 worth 100,000,000.00 would need the rate 10,000,000,000; USD 5.00 worth -1.00 a rate below zero.
	for _, c := range []struct{ amount, base, capital, want string }{
		{"0.01", "100000000.00", "-100000000.00", "ledger_boundary B: 1020@ USD 0.01 0.01 1020@ USD 0.00 99999999.99 3010@ SGD -100000000.00 -100000000.00"},
		{"5.00", "-1.00", "1.00", "ledger_boundary B: 1020@ USD 5.00 5.00 1020@ USD 0.00 -6.00 3010@ SGD 1.00 1.00"},
	} {
		in.Lines = []Line{l("journal", "J1", "usd", "", c.amount, usd, c.base), l("journal", "J1", "capital", "", c.capital, sgd, c.capital)}
		got, err := CarryForward(in)
		if err != nil {
			t.Fatal(err)
		}
		if s := show(got[0]); s != c.want || got[0].Lines[0].Rate.Decimal().Text('f') != "1" {
			t.Errorf("USD %s:\n got %s\nwant %s", c.amount, s, c.want)
		}
	}
}

// The rule refuses what it cannot carry: unbalanced purged lines, a line naming no account, a base in another currency,
// and a retained earnings or suspense account that is a control account, revenue or expense.
// req: AUD-10
// req: INV-01
func TestTheCarryForwardRefusesWhatItCannotCarry(t *testing.T) {
	base := Input{Base: sgd, Accounts: chart, RetainedEarnings: "re", Suspense: "suspense", Boundary: Source{"ledger_boundary", "B"}}
	for name, lines := range map[string][]Line{
		"unbalanced":    {l("journal", "J1", "bank", "", "10.00", sgd, "10.00")},
		"no account":    {l("journal", "J1", "nowhere", "", "10.00", sgd, "10.00")},
		"foreign base":  {{Source: Source{"journal", "J1"}, Account: "bank", Amount: m("10.00", sgd), Base: m("10.00", usd)}},
		"base mismatch": {l("journal", "J1", "bank", "", "10.00", sgd, "11.00"), l("journal", "J1", "capital", "", "-11.00", sgd, "-11.00")},
	} {
		in := base
		in.Lines = lines
		if _, err := CarryForward(in); err == nil {
			t.Errorf("%s: carried", name)
		}
	}
	for name, in := range map[string]Input{"suspense a control account": {Suspense: "ar"}, "retained earnings revenue": {RetainedEarnings: "revenue"},
		"no suspense account": {Suspense: "nowhere"}} {
		x := base
		if in.Suspense != "" {
			x.Suspense = in.Suspense
		}
		if in.RetainedEarnings != "" {
			x.RetainedEarnings = in.RetainedEarnings
		}
		if _, err := CarryForward(x); err == nil {
			t.Errorf("%s: carried", name)
		}
	}
}
