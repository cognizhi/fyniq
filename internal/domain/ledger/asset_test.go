package ledger

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
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

var (
	depreciationAcc = Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000006610"), Code: "6610", Active: true, Type: "expense", Role: RoleDepreciation}
	accumulatedAcc  = Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000001590"), Code: "1590", Active: true, Type: "asset", Role: RoleAccumulatedDepreciation}
	laptopID        = uuid.MustParse("0192f1a0-0000-7000-8000-0000000fa001")
)

// depreciating is the chart depreciation posts against: 6610 and 1590 by role.
func depreciating() Settings {
	s := Settings{Base: sgd, Accounts: map[uuid.UUID]Account{}}
	for _, a := range []Account{depreciationAcc, accumulatedAcc, bank} {
		s.Accounts[a.ID] = a
	}
	return s
}

// laptopVersion is a S$3,600 laptop's version carrying carried from its first month from to end.
func laptopVersion(t testing.TB, version int, cost, residual, carried string, from, end time.Time) DepreciableAsset {
	return DepreciableAsset{ID: laptopID, Version: version, Memo: "FA-0001 MacBook Pro 14", Cost: amt(t, cost, sgd), Residual: amt(t, residual, sgd),
		Carried: amt(t, carried, sgd), From: from, End: end}
}

// sum is the total of a schedule's parts.
func sum(t testing.TB, ds []Depreciation) string {
	total := amt(t, "0.00", sgd)
	for _, d := range ds {
		total, _ = total.Add(d.Amount)
	}
	return total.String()
}

// Worked by hand (B.4, ALLOC-EVEN): S$3,600 over 36 months from March 2026 is S$100.00 a month, the first dated 31 March
// 2026 and the twelfth 28 February 2027; S$1,000 over 3 months is 333.33, 333.34 and 333.33. An asset bought in March
// 2025 that the opening balances carry at S$1,200 from January 2026 depreciates the S$2,400 left over its 26 months
// from January 2026 to February 2028. A change of life to 48 months after 6 months (S$600 posted) spreads the S$3,000
// left over the 42 months from September 2026 to February 2030. Nothing left, or no month left, has no schedule.
// req: FR-FA-02
// req: ledger/posting-rules Depreciation
// req: ALLOC-EVEN
func TestDepreciationSchedule(t *testing.T) {
	ds, err := DepreciationSchedule(laptopVersion(t, 1, "3600.00", "0.00", "0.00", month(t, "2026-03"), month(t, "2029-02")))
	if err != nil || len(ds) != 36 {
		t.Fatalf("36 months: %d %v", len(ds), err)
	}
	for i, d := range ds {
		if d.Amount.String() != "100.00" || d.Part != i+1 || d.Of != 36 || d.Version != 1 || d.AssetID != laptopID || d.Memo != "FA-0001 MacBook Pro 14" {
			t.Errorf("month %d: %+v", i+1, d)
		}
	}
	if !ds[0].Month.Equal(month(t, "2026-03")) || !ds[0].Date.Equal(time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)) ||
		!ds[11].Date.Equal(time.Date(2027, 2, 28, 0, 0, 0, 0, time.UTC)) || !ds[35].Month.Equal(month(t, "2029-02")) {
		t.Errorf("dates: %v %v %v", ds[0].Date, ds[11].Date, ds[35].Month)
	}
	three, err := DepreciationSchedule(laptopVersion(t, 1, "1000.00", "0.00", "0.00", month(t, "2026-10"), month(t, "2026-12")))
	if err != nil || len(three) != 3 || three[0].Amount.String() != "333.33" || three[1].Amount.String() != "333.34" || three[2].Amount.String() != "333.33" {
		t.Errorf("S$1,000 over 3 months: %+v %v", three, err)
	}
	opening, err := DepreciationSchedule(laptopVersion(t, 1, "3600.00", "0.00", "1200.00", month(t, "2026-01"), month(t, "2028-02")))
	if err != nil || len(opening) != 26 || sum(t, opening) != "2400.00" || opening[0].Amount.String() != "92.31" {
		t.Errorf("from the books start: %d %s %v", len(opening), sum(t, opening), err)
	}
	changed, err := DepreciationSchedule(laptopVersion(t, 2, "3600.00", "0.00", "600.00", month(t, "2026-09"), month(t, "2030-02")))
	if err != nil || len(changed) != 42 || sum(t, changed) != "3000.00" || changed[0].Version != 2 || changed[0].Of != 42 {
		t.Errorf("a longer life after 6 months: %d %s %v", len(changed), sum(t, changed), err)
	}
	withResidual, err := DepreciationSchedule(laptopVersion(t, 1, "3600.00", "600.00", "0.00", month(t, "2026-03"), month(t, "2029-02")))
	if err != nil || sum(t, withResidual) != "3000.00" || withResidual[0].Amount.String() != "83.33" {
		t.Errorf("a residual of S$600: %s %v", sum(t, withResidual), err)
	}
	for name, a := range map[string]DepreciableAsset{
		"fully depreciated":    laptopVersion(t, 2, "3600.00", "0.00", "3600.00", month(t, "2029-03"), month(t, "2029-02")),
		"down to its residual": laptopVersion(t, 2, "3600.00", "600.00", "3000.00", month(t, "2028-01"), month(t, "2029-02")),
		"its life over":        laptopVersion(t, 2, "3600.00", "0.00", "3500.00", month(t, "2029-03"), month(t, "2029-02")),
	} {
		if ds, err := DepreciationSchedule(a); err != nil || len(ds) != 0 {
			t.Errorf("%s: %+v %v", name, ds, err)
		}
	}
	if _, err := DepreciationSchedule(laptopVersion(t, 2, "3600.00", "600.00", "3000.01", month(t, "2028-01"), month(t, "2029-02"))); err == nil {
		t.Errorf("carrying more than the cost less the residual")
	}
	usdLaptop := laptopVersion(t, 1, "3600.00", "0.00", "0.00", month(t, "2026-03"), month(t, "2029-02"))
	usdLaptop.Residual = amt(t, "0.00", usd)
	if _, err := DepreciationSchedule(usdLaptop); err == nil {
		t.Errorf("a residual in another currency")
	}
}

// A month's depreciation posts Dr 6610 · Cr 1590 with its part, on the month's last day, under asset_depreciation@1,
// with the asset and its version as its source; the replay reproduces it (ENG-06). Every refusal comes back together.
// req: FR-FA-02
// req: ledger/posting-rules Depreciation
// req: ENG-06
// req: ENG-22
func TestPostDepreciation(t *testing.T) {
	s := depreciating()
	ds, err := DepreciationSchedule(laptopVersion(t, 1, "3600.00", "0.00", "0.00", month(t, "2026-03"), month(t, "2029-02")))
	if err != nil {
		t.Fatal(err)
	}
	e, err := PostDepreciation(ds[11], s)
	if err != nil {
		t.Fatal(err)
	}
	if got := legs(s, e); got != "6610 100.00 SGD = 100.00; 1590 -100.00 SGD = -100.00" {
		t.Errorf("legs: %s", got)
	}
	if !e.PostingDate.Equal(time.Date(2027, 2, 28, 0, 0, 0, 0, time.UTC)) || e.RuleVersion != RuleAssetDepreciation ||
		e.Source != (Source{SourceAssetDepreciation, laptopID.String(), 1}) || e.PostingIdentity != "asset_depreciation:"+laptopID.String()+":v1:2027-02:asset_depreciation@1" ||
		e.Memo != "FA-0001 MacBook Pro 14: depreciation for February 2027 (12 of 36)" {
		t.Errorf("the entry: %+v", e)
	}
	again, err := ReplayDepreciation(RuleAssetDepreciation, ds[11], s)
	if err != nil || legs(s, again) != legs(s, e) || again.PostingIdentity != e.PostingIdentity {
		t.Errorf("the replay: %+v %v", again, err)
	}
	if _, err := ReplayDepreciation("asset_depreciation@9", ds[11], s); !Problems.Has(err.(Problems), CodeUnknownRule) {
		t.Errorf("an unknown rule version: %v", err)
	}

	bare := Settings{Base: sgd, Accounts: map[uuid.UUID]Account{}}
	d := ds[0]
	d.Date, d.Month, d.Amount, d.Memo = time.Time{}, time.Time{}, amt(t, "0.00", sgd), strings.Repeat("x", MaxMemo)
	_, err = PostDepreciation(d, bare)
	probs, _ := err.(Problems)
	var codes []string
	for _, p := range probs {
		codes = append(codes, p.Field+":"+p.Code)
	}
	want := []string{"date:" + CodeNoDate, "amount:" + CodeDepreciation, "memo:" + CodeTooLong, "lines:" + CodeSystemRole, "lines:" + CodeSystemRole}
	if !slices.Equal(codes, want) {
		t.Errorf("refusals: %v, want %v", codes, want)
	}
	f := ds[0]
	f.Amount = amt(t, "100.00", usd)
	if _, err := PostDepreciation(f, s); err == nil || !err.(Problems).Has(CodeDepreciation) {
		t.Errorf("a depreciation in USD: %v", err)
	}
	f.Amount = amt(t, "-100.00", sgd)
	if _, err := PostDepreciation(f, s); err == nil || !err.(Problems).Has(CodeDepreciation) {
		t.Errorf("a depreciation of less than nothing: %v", err)
	}
}
