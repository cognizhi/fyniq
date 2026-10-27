package ledger

import (
	"testing"
)

// An asset registered from a bill's line costs what the bill's entry put on the line's account for it (FR-FA-01;
// p1-fixed-assets design D1). Worked by hand: a bill of 6120 1,000.00 + GST 90.00 claimable, a line of claimable GST
// 9.00 alone, 6120 1,200.00 spread over 12 months, and 5010 50.00 + GST 4.50 not claimable posts 6120 1,000.00; 1210
// 1,200.00; 5010 54.50; 2200 99.00; 2010 −2,353.50. Line 1 costs 1,000.00 on 6120 and line 4 54.50 on 5010 (its GST
// stays in its cost); line 2 put no cost of its own anywhere, and line 3's is on 1210; there is no line 5. A bill in US
// dollars at 1.34565 costs its line's net at that rate: USD 1,000.00 is S$1,345.65.
// req: FR-FA-01
func TestBillLineCost(t *testing.T) {
	s := purchasing()
	spread := billLine(t, software, "1200.00", "0.00", sgd, false)
	spread.Spread = &Spread{Months: 12, From: day}
	b := sgdBill(billLine(t, software, "1000.00", "90.00", sgd, true), billLine(t, software, "0.00", "9.00", sgd, true), spread,
		billLine(t, hosting, "50.00", "4.50", sgd, false))
	e, err := PostBill(b, s)
	if err != nil {
		t.Fatal(err)
	}
	if got := legs(s, e); got != "6120 1000.00 SGD = 1000.00; 1210 1200.00 SGD = 1200.00; 5010 54.50 SGD = 54.50; 2200 99.00 SGD = 99.00; 2010 -2353.50 SGD = -2353.50" {
		t.Fatalf("the bill posted %s", got)
	}
	for _, c := range []struct {
		line    int
		account Account
		base    string
	}{{1, software, "1000.00"}, {4, hosting, "54.50"}} {
		l, ok := BillLineCost(b, e, s, c.line)
		if !ok || l.AccountID != c.account.ID || l.Base.String() != c.base {
			t.Errorf("line %d: %+v %v, want %s on %s", c.line, l, ok, c.base, c.account.Code)
		}
	}
	for _, line := range []int{0, 2, 3, 5} {
		if l, ok := BillLineCost(b, e, s, line); ok {
			t.Errorf("line %d took %+v", line, l)
		}
	}
	// An entry that does not match the bill's lines takes no cost from them.
	short := e
	short.Lines = e.Lines[:1]
	if _, ok := BillLineCost(b, short, s, 4); ok {
		t.Errorf("line 4 of an entry with one line")
	}
	other := e
	other.Lines = append([]Line{e.Lines[2]}, e.Lines[1:]...)
	if _, ok := BillLineCost(b, other, s, 1); ok {
		t.Errorf("line 1 took a line on another account")
	}

	// In US dollars: the line's net at the bill's rate (RND-FX); a claimable line without its GST stated in SGD keeps its
	// GST in its cost, as bill@1 does.
	rate := usdRate(t, "1.34565")
	f := Bill{ID: billID, Version: 1, Date: day, Memo: "Apple", Currency: usd, Rate: rate, Lines: []BillLine{
		billLine(t, software, "1000.00", "0.00", usd, false), billLine(t, hosting, "100.00", "9.00", usd, true)}}
	fe, err := PostBill(f, s)
	if err != nil {
		t.Fatal(err)
	}
	if l, ok := BillLineCost(f, fe, s, 1); !ok || l.Base.String() != "1345.65" || l.Base.Currency() != sgd {
		t.Errorf("a USD line: %+v %v", l, ok)
	}
	if l, ok := BillLineCost(f, fe, s, 2); !ok || l.AccountID != hosting.ID || l.Base.String() != "146.68" {
		t.Errorf("a USD line with its GST in its cost (109.00 × 1.34565 = 146.67585): %+v %v", l, ok)
	}
}

// An asset registered from a categorised bank line's part costs what its entry put on the part's account (FR-FA-01).
// Worked by hand: USD 100.00 out of the USD wallet at 1.35 in three parts of 33.33, 33.33 and 33.34, TX, posts 5010
// 41.28, 6120 41.28 and 6120 41.29 after the wallet's line (bank_transaction@1); a part with no base amount posts
// nothing; a spread part puts its amount on 1210, and a part of money in is a credit, which the register refuses as a cost.
// req: FR-FA-01
func TestBankPartCost(t *testing.T) {
	s := banking()
	x := bankTxn(t, wallet, "-100.00", usd, usdRate(t, "1.35"), part(t, hosting, "-33.33", usd, "0.09", 0), part(t, software, "-33.33", usd, "0.09", 0),
		part(t, software, "-33.34", usd, "0.09", 0))
	e, err := PostBankTransaction(x, s)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		part    int
		account Account
		base    string
	}{{1, hosting, "41.28"}, {2, software, "41.28"}, {3, software, "41.29"}} {
		l, ok := BankPartCost(x, e, s, c.part)
		if !ok || l.AccountID != c.account.ID || l.Base.String() != c.base {
			t.Errorf("part %d: %+v %v, want %s on %s", c.part, l, ok, c.base, c.account.Code)
		}
	}
	for _, p := range []int{0, 4} {
		if l, ok := BankPartCost(x, e, s, p); ok {
			t.Errorf("part %d took %+v", p, l)
		}
	}
	short := e
	short.Lines = e.Lines[:2]
	if _, ok := BankPartCost(x, short, s, 3); ok {
		t.Errorf("part 3 of an entry with one part's line")
	}

	spread := bankTxn(t, current, "-1308.00", sgd, nil, part(t, hosting, "-108.00", sgd, "", 0), part(t, software, "-1200.00", sgd, "", 12))
	se, err := PostBankTransaction(spread, s)
	if err != nil {
		t.Fatal(err)
	}
	if l, ok := BankPartCost(spread, se, s, 1); !ok || l.Base.String() != "108.00" {
		t.Errorf("the part before a spread one: %+v %v", l, ok)
	}
	if l, ok := BankPartCost(spread, se, s, 2); ok {
		t.Errorf("a spread part took %+v", l)
	}

	// A part too small to have a base amount at the rate posts nothing, and the part after it takes the next line: USD 0.01
	// at 0.4 is S$0.004, which RND-FX rounds to nothing.
	tiny := bankTxn(t, wallet, "-100.01", usd, usdRate(t, "0.4"), part(t, hosting, "-0.01", usd, "", 0), part(t, software, "-100.00", usd, "", 0))
	te, err := PostBankTransaction(tiny, s)
	if err != nil {
		t.Fatal(err)
	}
	if l, ok := BankPartCost(tiny, te, s, 1); ok {
		t.Errorf("a part of nothing took %+v", l)
	}
	if l, ok := BankPartCost(tiny, te, s, 2); !ok || l.AccountID != software.ID || l.Base.String() != "40.00" {
		t.Errorf("the part after one of nothing: %+v %v (%s)", l, ok, legs(s, te))
	}

	in := bankTxn(t, current, "1090.00", sgd, nil, part(t, consulting, "1090.00", sgd, "0.09", 0))
	ie, err := PostBankTransaction(in, s)
	if err != nil {
		t.Fatal(err)
	}
	if l, ok := BankPartCost(in, ie, s, 1); !ok || l.Base.String() != "-1000.00" {
		t.Errorf("money in: %+v %v", l, ok)
	}
}
