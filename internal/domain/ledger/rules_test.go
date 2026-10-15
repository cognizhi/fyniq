package ledger

import (
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

// The rules PRD-001 rev 0.24 decided for a P0 manual journal and its reversal (p0-manual-journal-rules),
// each worked by hand from the PRD rows.

var (
	idr          = money.MustLookup("IDR")
	shareCapital = Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000003010"), Code: "3010", Active: true, ShareCapital: true}
)

func withShareCapital() Settings {
	s := Settings{Base: settings.Base, Accounts: map[uuid.UUID]Account{shareCapital.ID: shareCapital}}
	for id, a := range settings.Accounts {
		s.Accounts[id] = a
	}
	return s
}

func codes(err error) []string {
	probs, _ := err.(Problems)
	var out []string
	for _, p := range probs {
		out = append(out, p.Code)
	}
	slices.Sort(out)
	return out
}

// ENG-24: each rule refuses the journal whole with its own code and field, and a journal that breaks
// three rules reports all three at once (FR-INT-07). The engine never adds, drops or changes a line.
// req: ENG-24
// req: ENG-22
func TestAManualJournalIsRejectedWholeWithEveryFailure(t *testing.T) {
	rate := usdRate(t, "1.35")
	for name, c := range map[string]struct {
		j    Journal
		want []string
	}{
		"rule 2: one line": {Journal{ID: docID, Version: 1, Date: day, Lines: []JournalLine{{AccountID: bank.ID, Amount: amt(t, "1.00", sgd)}}},
			[]string{CodeTooFewLines, CodeUnbalancedBase, CodeUnbalancedCur}},
		"rule 3: a zero line": {Journal{ID: docID, Version: 1, Date: day, Lines: []JournalLine{
			{AccountID: bank.ID, Amount: amt(t, "1.00", sgd)}, {AccountID: sales.ID, Amount: amt(t, "-1.00", sgd)},
			{AccountID: sales.ID, Amount: money.Zero(sgd)}}}, []string{CodeZeroLine}},
		"rule 4: an unknown account": {Journal{ID: docID, Version: 1, Date: day, Lines: []JournalLine{
			{AccountID: uuid.New(), Amount: amt(t, "1.00", sgd)}, {AccountID: sales.ID, Amount: amt(t, "-1.00", sgd)}}}, []string{CodeUnknownAccount}},
		"rule 5: a foreign line without a rate": {Journal{ID: docID, Version: 1, Date: day, Lines: []JournalLine{
			{AccountID: usdBank.ID, Amount: amt(t, "1.00", usd)}, {AccountID: sales.ID, Amount: amt(t, "-1.35", sgd)}}}, []string{CodeMissingRate}},
		"rule 5: a rate on a base-currency line": {Journal{ID: docID, Version: 1, Date: day, Lines: []JournalLine{
			{AccountID: bank.ID, Amount: amt(t, "1.00", sgd), Rate: &money.Rate{From: sgd, To: sgd, Source: "document"}},
			{AccountID: sales.ID, Amount: amt(t, "-1.00", sgd)}}}, []string{CodeRateOnBase}},
		"rule 7: unbalanced in base": {Journal{ID: docID, Version: 1, Date: day, Lines: []JournalLine{
			{AccountID: usdBank.ID, Amount: amt(t, "1.00", usd), Rate: rate}, {AccountID: sales.ID, Amount: amt(t, "-1.34", sgd)}}},
			[]string{CodeUnbalancedBase}},
		// SGD 5.00 + 1.00 − USD 5.00 at 1.35 (SGD 6.75) leaves SGD −0.75 in base; two currencies, so no rule 8.
		"three rules at once (4, 5, 7)": {Journal{ID: docID, Version: 1, Date: day, Lines: []JournalLine{
			{AccountID: closed.ID, Amount: amt(t, "5.00", sgd)},
			{AccountID: bank.ID, Amount: amt(t, "1.00", sgd), Rate: &money.Rate{From: sgd, To: sgd, Source: "document"}},
			{AccountID: sales.ID, Amount: amt(t, "-5.00", usd), Rate: rate}}},
			[]string{CodeInactiveAccount, CodeRateOnBase, CodeUnbalancedBase}},
		"rules 7 and 8 together in one currency": {Journal{ID: docID, Version: 1, Date: day, Lines: []JournalLine{
			{AccountID: bank.ID, Amount: amt(t, "5.00", sgd)}, {AccountID: sales.ID, Amount: amt(t, "-4.00", sgd)}}},
			[]string{CodeUnbalancedBase, CodeUnbalancedCur}},
	} {
		e, err := PostJournal(c.j, settings)
		if got := codes(err); !slices.Equal(got, sortedCopy(c.want)) || len(e.Lines) != 0 {
			t.Errorf("%s: codes %v (want %v), %d lines posted", name, got, sortedCopy(c.want), len(e.Lines))
		}
	}
}

func sortedCopy(s []string) []string {
	out := slices.Clone(s)
	slices.Sort(out)
	return out
}

// ENG-24 rules 7 and 8, open question 49: USD 0.01 + 0.01 − 0.02 at 1.5 balances in dollars but becomes
// SGD 0.02 + 0.02 − 0.03 = 0.01 in base (0.015 rounds half away from zero to 0.02; 0.03 exactly). The
// journal is refused as unbalanced in base, and no rounding line is added.
// req: ENG-24
// req: INV-01
func TestAForeignJournalUnbalancedOnlyInBaseIsRefused(t *testing.T) {
	r := usdRate(t, "1.5")
	j := Journal{ID: docID, Version: 1, Date: day, Lines: []JournalLine{
		{AccountID: usdBank.ID, Amount: amt(t, "0.01", usd), Rate: r},
		{AccountID: usdBank.ID, Amount: amt(t, "0.01", usd), Rate: r},
		{AccountID: sales.ID, Amount: amt(t, "-0.02", usd), Rate: r}}}
	e, err := PostJournal(j, settings)
	if got := codes(err); !slices.Equal(got, []string{CodeUnbalancedBase}) || len(e.Lines) != 0 {
		t.Fatalf("codes %v, %d lines", got, len(e.Lines))
	}
	for _, p := range err.(Problems) {
		if p.Message != "debits and credits differ by 0.01 SGD in the base currency" {
			t.Errorf("the difference is not named: %q", p.Message)
		}
	}
}

// ENG-03: a base-currency line stores rate 1 from "base"; a converted line keeps the rate and source it
// came with. A rate entered on a base-currency line is refused (rule 5).
// req: ENG-03
func TestABaseCurrencyLineStoresRateOneFromBase(t *testing.T) {
	r := usdRate(t, "1.35")
	e, err := PostJournal(Journal{ID: docID, Version: 1, Date: day, Lines: []JournalLine{
		{AccountID: usdBank.ID, Amount: amt(t, "100.00", usd), Rate: r},
		{AccountID: sales.ID, Amount: amt(t, "-135.00", sgd)}}}, settings)
	if err != nil {
		t.Fatal(err)
	}
	if l := e.Lines[1]; l.Rate.String() != "1" || l.Rate.Source != "base" {
		t.Errorf("the base line's rate: %s from %q", l.Rate, l.Rate.Source)
	}
	if l := e.Lines[0]; l.Rate.String() != "1.35" || l.Rate.Source != "document" || l.Base.String() != "135.00" {
		t.Errorf("the dollar line: rate %s from %q, base %s", l.Rate, l.Rate.Source, l.Base)
	}
	one, _ := money.ParseRate("1", sgd, sgd, "document")
	_, err = PostJournal(Journal{ID: docID, Version: 1, Date: day, Lines: []JournalLine{
		{AccountID: bank.ID, Amount: amt(t, "1.00", sgd), Rate: &one}, {AccountID: sales.ID, Amount: amt(t, "-1.00", sgd)}}}, settings)
	if got := codes(err); !slices.Equal(got, []string{CodeRateOnBase}) {
		t.Errorf("a rate of 1 entered on a base-currency line: %v", got)
	}
}

// INV-05 (rev 0.24) and ENG-24 rule 3: a non-zero line whose base amount rounds to 0.00 posts with base
// 0.00. IDR 50 at SGD 0.000085 is SGD 0.00425, which RND-FX rounds to 0.00.
// req: INV-05
// req: ENG-24
func TestANonZeroLineWhoseBaseRoundsToZeroPosts(t *testing.T) {
	r, err := money.ParseRate("0.000085", idr, sgd, "document")
	if err != nil {
		t.Fatal(err)
	}
	e, err := PostJournal(Journal{ID: docID, Version: 1, Date: day, Lines: []JournalLine{
		{AccountID: usdBank.ID, Amount: amt(t, "50", idr), Rate: &r},
		{AccountID: sales.ID, Amount: amt(t, "-50", idr), Rate: &r}}}, settings)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range e.Lines {
		if l.Base.String() != "0.00" || l.Amount.IsZero() {
			t.Errorf("line %s IDR: base %s", l.Amount, l.Base)
		}
	}
}

// ENG-44 (rev 0.24) and rule 6: a computed base amount its column cannot hold is refused with a field
// error on the line, never truncated. USD 9,999,999,999,999,999.99 at 10 has 17 integer digits; a stored
// amount holds 16.
// req: ENG-44
// req: INV-05
func TestABaseAmountOutOfRangeIsRefusedNotTruncated(t *testing.T) {
	big := amt(t, "9999999999999999.99", usd)
	r := usdRate(t, "10")
	_, err := PostJournal(Journal{ID: docID, Version: 1, Date: day, Lines: []JournalLine{
		{AccountID: usdBank.ID, Amount: big, Rate: r}, {AccountID: usdBank.ID, Amount: big.Neg(), Rate: r}}}, settings)
	probs, _ := err.(Problems)
	if got := codes(err); !slices.Equal(got, []string{CodeBaseRange, CodeBaseRange}) || probs[0].Field != "lines[1].amount" {
		t.Fatalf("codes %v: %v", got, err)
	}
}

// INV-13 from P0: a manual journal line to a share-capital account is refused; the register of share
// issues and opening balances post there (P1).
// req: INV-13
// req: ENG-24
func TestAManualJournalToShareCapitalIsRefused(t *testing.T) {
	_, err := PostJournal(Journal{ID: docID, Version: 1, Date: day, Lines: []JournalLine{
		{AccountID: bank.ID, Amount: amt(t, "1000.00", sgd)}, {AccountID: shareCapital.ID, Amount: amt(t, "-1000.00", sgd)}}}, withShareCapital())
	probs, _ := err.(Problems)
	if got := codes(err); !slices.Equal(got, []string{CodeShareCapital}) || probs[0].Field != "lines[2].account" {
		t.Fatalf("codes %v: %v", got, err)
	}
}

// ENG-14 (rev 0.24): a reversal negates the stored lines, amount and base amount as stored, and never
// re-converts them: a stored base that today's rate would not give (USD 1.00 at 1.5 stored as 1.49) is
// reversed as −1.49.
// req: ENG-14
func TestAReversalNegatesTheStoredBaseAmounts(t *testing.T) {
	r := usdRate(t, "1.5")
	stored := Entry{PostingDate: day, Source: Source{Type: SourceManualJournal, ID: docID.String(), Version: 1}, RuleVersion: RuleManualJournal,
		PostingIdentity: JournalIdentity(docID, 1, RuleManualJournal), Lines: []Line{
			{AccountID: usdBank.ID, Amount: amt(t, "1.00", usd), Rate: *r, Base: amt(t, "1.49", sgd)},
			{AccountID: sales.ID, Amount: amt(t, "-1.49", sgd), Rate: money.Identity(sgd), Base: amt(t, "-1.49", sgd)}}}
	rev, err := Reverse(stored, day, "duplicate")
	if err != nil {
		t.Fatal(err)
	}
	if rev.Lines[0].Base.String() != "-1.49" || rev.Lines[0].Amount.String() != "-1.00" || rev.Lines[1].Base.String() != "1.49" ||
		rev.Lines[0].Rate.String() != "1.5" {
		t.Fatalf("reversal lines: %+v", rev.Lines)
	}
}

// FR-GL-12 (rev 0.24, from P0 for manual journals): a reversal is dated on or after its entry. The day
// before is refused; the same day and a later month post.
// req: FR-GL-12
func TestAReversalIsDatedOnOrAfterItsEntry(t *testing.T) {
	e, err := PostJournal(sale(t, "100.00"), settings)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Reverse(e, day.AddDate(0, 0, -1), ""); !slices.Equal(codes(err), []string{CodeReversalDate}) {
		t.Errorf("the day before: %v", err)
	}
	for _, d := range []time.Time{day, day.AddDate(0, 1, 3), day.Add(23 * time.Hour)} {
		if rev, err := Reverse(e, d, ""); err != nil || rev.PostingDate.Before(e.PostingDate) {
			t.Errorf("reversing on %s: %v", d, err)
		}
	}
}
