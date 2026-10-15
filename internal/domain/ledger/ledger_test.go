package ledger

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"pgregory.net/rapid"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

var (
	sgd = money.MustLookup("SGD")
	usd = money.MustLookup("USD")
	jpy = money.MustLookup("JPY")

	bank    = Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000001010"), Code: "1010", Active: true}
	sales   = Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000004010"), Code: "4010", Active: true}
	usdBank = Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000001020"), Code: "1020", Active: true}
	group   = Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000006000"), Code: "6000", Group: true, Active: true}
	closed  = Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000006990"), Code: "6990"}

	settings = Settings{Base: sgd, Accounts: map[uuid.UUID]Account{
		bank.ID: bank, sales.ID: sales, usdBank.ID: usdBank, group.ID: group, closed.ID: closed}}
	docID = uuid.MustParse("0192f1a0-0000-7000-8000-00000000d0c1")
	day   = time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
)

func amt(t testing.TB, s string, c money.Currency) money.Money {
	t.Helper()
	m, err := money.Parse(s, c)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func usdRate(t testing.TB, s string) *money.Rate {
	t.Helper()
	r, err := money.ParseRate(s, usd, sgd, "document")
	if err != nil {
		t.Fatal(err)
	}
	return &r
}

func sale(t testing.TB, amount string) Journal {
	return Journal{ID: docID, Version: 1, Date: day, Memo: "October retainer", Lines: []JournalLine{
		{AccountID: bank.ID, Amount: amt(t, amount, sgd)},
		{AccountID: sales.ID, Amount: amt(t, "-"+amount, sgd), Description: "Acme retainer"},
	}}
}

// req: ENG-01
func TestPostingIsAPureFunction(t *testing.T) {
	j := sale(t, "1200.00")
	j.Lines = append(j.Lines,
		JournalLine{AccountID: usdBank.ID, Amount: amt(t, "100.00", usd), Rate: usdRate(t, "1.3472")},
		JournalLine{AccountID: sales.ID, Amount: amt(t, "-134.72", sgd)})
	first, err := PostJournal(j, settings)
	if err != nil {
		t.Fatal(err)
	}
	second, err := PostJournal(j, settings)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(second)
	if string(a) != string(b) {
		t.Fatalf("the same journal posted twice gave different entries:\n%s\n%s", a, b)
	}
}

// req: ENG-03
// req: ENG-14
func TestEntriesCarryTheirProvenance(t *testing.T) {
	j := sale(t, "50.00")
	j.Lines = []JournalLine{
		{AccountID: usdBank.ID, Amount: amt(t, "37.11", usd), Rate: usdRate(t, "1.3472")}, // 49.994... → 49.99
		{AccountID: sales.ID, Amount: amt(t, "-49.99", sgd)},
	}
	e, err := PostJournal(j, settings)
	if err != nil {
		t.Fatal(err)
	}
	if e.Source != (Source{Type: "manual_journal", ID: docID.String(), Version: 1}) || e.RuleVersion != "manual_journal@1" ||
		e.PostingIdentity != "manual_journal:"+docID.String()+":v1:manual_journal@1" || !e.PostingDate.Equal(day) {
		t.Fatalf("provenance = %+v", e)
	}
	usdLine := e.Lines[0]
	if usdLine.Base.String() != "49.99" || usdLine.Rate.String() != "1.3472" || usdLine.Rate.Source != "document" || usdLine.Amount.Currency() != usd {
		t.Fatalf("the USD line = %+v; want base 49.99 SGD at 1.3472 from the document", usdLine)
	}
	if l := e.Lines[1]; l.Rate.String() != "1" || l.Rate.Source != "base" || !l.Base.Equal(l.Amount) {
		t.Fatalf("the SGD line = %+v; want rate 1 from base and base = amount", l)
	}
}

// req: ENG-22
func TestEveryValidationCodeIsReachable(t *testing.T) {
	long := strings.Repeat("x", MaxMemo+1)
	cases := map[string]struct {
		j     func() Journal
		field string
	}{
		CodeNoDate:      {func() Journal { j := sale(t, "1.00"); j.Date = time.Time{}; return j }, "date"},
		CodeTooFewLines: {func() Journal { j := sale(t, "1.00"); j.Lines = j.Lines[:1]; return j }, "lines"},
		CodeZeroLine: {func() Journal {
			j := sale(t, "1.00")
			j.Lines = append(j.Lines, JournalLine{AccountID: bank.ID, Amount: money.Zero(sgd)})
			return j
		}, "lines[3].amount"},
		CodeUnknownAccount:  {func() Journal { j := sale(t, "1.00"); j.Lines[0].AccountID = uuid.Nil; return j }, "lines[1].account"},
		CodeGroupAccount:    {func() Journal { j := sale(t, "1.00"); j.Lines[0].AccountID = group.ID; return j }, "lines[1].account"},
		CodeInactiveAccount: {func() Journal { j := sale(t, "1.00"); j.Lines[1].AccountID = closed.ID; return j }, "lines[2].account"},
		CodeMissingRate: {func() Journal {
			j := sale(t, "1.00")
			j.Lines[0] = JournalLine{AccountID: usdBank.ID, Amount: amt(t, "1.00", usd)}
			return j
		}, "lines[1].fx_rate"},
		CodeWrongRate: {func() Journal {
			j := sale(t, "1.00")
			r, _ := money.ParseRate("0.0091", jpy, sgd, "feed")
			j.Lines[0] = JournalLine{AccountID: usdBank.ID, Amount: amt(t, "1.00", usd), Rate: &r}
			return j
		}, "lines[1].fx_rate"},
		CodeUnbalancedBase: {func() Journal { j := sale(t, "1.00"); j.Lines[1].Amount = amt(t, "-0.99", sgd); return j }, "lines"},
		CodeUnbalancedCur: {func() Journal {
			// Base balances (0.10 − 0.10) but the yen do not (100 − 104).
			r, _ := money.ParseRate("0.001", jpy, sgd, "feed")
			return Journal{ID: docID, Version: 1, Date: day, Lines: []JournalLine{
				{AccountID: bank.ID, Amount: amt(t, "100", jpy), Rate: &r},
				{AccountID: sales.ID, Amount: amt(t, "-104", jpy), Rate: &r}}}
		}, "lines"},
		CodeTooLong: {func() Journal { j := sale(t, "1.00"); j.Memo = long; return j }, "memo"},
		CodeRateOnBase: {func() Journal {
			j := sale(t, "1.00")
			j.Lines[0].Rate = &money.Rate{From: sgd, To: sgd, Source: "document"}
			return j
		}, "lines[1].fx_rate"},
		CodeBaseRange: {func() Journal {
			r, _ := money.ParseRate("10", usd, sgd, "document")
			big := amt(t, "9999999999999999.99", usd)
			return Journal{ID: docID, Version: 1, Date: day, Lines: []JournalLine{
				{AccountID: usdBank.ID, Amount: big, Rate: &r}, {AccountID: sales.ID, Amount: amt(t, "-1.00", sgd)}}}
		}, "lines[1].amount"},
	}
	shareCapitalCase := func() Journal { j := sale(t, "1.00"); j.Lines[1].AccountID = shareCapital.ID; return j }
	if _, err := PostJournal(shareCapitalCase(), withShareCapital()); err == nil || !err.(Problems).Has(CodeShareCapital) {
		t.Errorf("%s: got %v", CodeShareCapital, err)
	}
	if e, _ := PostJournal(sale(t, "1.00"), settings); e.Lines != nil {
		if _, err := Reverse(e, day.AddDate(0, 0, -1), ""); err == nil || !err.(Problems).Has(CodeReversalDate) {
			t.Errorf("%s: got %v", CodeReversalDate, err)
		}
	}
	for code, c := range cases {
		_, err := PostJournal(c.j(), settings)
		probs, ok := err.(Problems)
		if !ok || !probs.Has(code) {
			t.Errorf("%s: got %v", code, err)
			continue
		}
		for _, p := range probs {
			if p.Code == code && (p.Field != c.field || p.Message == "" || p.Fix == "") {
				t.Errorf("%s: field %q (want %q), message %q, fix %q", code, p.Field, c.field, p.Message, p.Fix)
			}
		}
	}
	if _, err := Reverse(Entry{}, day, ""); err == nil || !err.(Problems).Has(CodeNotPosted) {
		t.Errorf("%s: reversing nothing gave %v", CodeNotPosted, err)
	}
	// Every code the engine declares is covered here.
	for _, code := range []string{CodeNoDate, CodeTooFewLines, CodeZeroLine, CodeUnknownAccount, CodeGroupAccount,
		CodeInactiveAccount, CodeMissingRate, CodeWrongRate, CodeUnbalancedBase, CodeUnbalancedCur, CodeTooLong, CodeRateOnBase, CodeBaseRange} {
		if _, ok := cases[code]; !ok {
			t.Errorf("no case for %s", code)
		}
	}
}

// A journal the generator makes: random lines across currencies, balanced in base by a last SGD
// line, so the property tests see many shapes.
func randomJournal(t *rapid.T) Journal {
	j := Journal{ID: uuid.UUID(rapid.SliceOfN(rapid.Byte(), 16, 16).Draw(t, "id")), Version: rapid.IntRange(1, 9).Draw(t, "version"), Date: day}
	var base int64
	for i := range rapid.IntRange(1, 6).Draw(t, "lines") {
		cents := rapid.Int64Range(-1_000_000_00, 1_000_000_00).Filter(func(c int64) bool { return c != 0 }).Draw(t, fmt.Sprint("cents", i))
		if rapid.Bool().Draw(t, fmt.Sprint("usd", i)) {
			r4 := rapid.Int64Range(1, 40_000).Draw(t, fmt.Sprint("rate", i))
			r, err := money.ParseRate(fmt.Sprintf("%d.%04d", r4/10_000, r4%10_000), usd, sgd, "test")
			if err != nil {
				t.Fatal(err)
			}
			m := money.FromMinor(cents, usd)
			j.Lines = append(j.Lines, JournalLine{AccountID: usdBank.ID, Amount: m, Rate: &r})
			// independent RND-FX on integers: round half away from zero of cents·r4 ÷ 10⁴
			n, q := cents*r4, int64(10_000)
			b := n / q
			if rem := n % q; 2*abs(rem) >= q {
				if n < 0 {
					b--
				} else {
					b++
				}
			}
			base += b
		} else {
			j.Lines = append(j.Lines, JournalLine{AccountID: bank.ID, Amount: money.FromMinor(cents, sgd)})
			base += cents
		}
	}
	if base == 0 { // already balanced: add a pair that nets to zero, so the shape still varies
		j.Lines = append(j.Lines, JournalLine{AccountID: bank.ID, Amount: money.FromMinor(1, sgd)},
			JournalLine{AccountID: sales.ID, Amount: money.FromMinor(-1, sgd)})
		return j
	}
	j.Lines = append(j.Lines, JournalLine{AccountID: sales.ID, Amount: money.FromMinor(-base, sgd)})
	return j
}

func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

func baseSum(t *rapid.T, e Entry) int64 {
	var s int64
	for _, l := range e.Lines {
		n, ok := l.Base.Minor()
		if !ok {
			t.Fatalf("base %s does not fit", l.Base)
		}
		s += n
	}
	return s
}

// req: QA-ENG-03
// req: INV-01
func TestEveryPostedEntryBalancesAndReversalsNetToZero(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		j := randomJournal(t)
		e, err := PostJournal(j, settings)
		if err != nil {
			t.Fatalf("a balanced journal was refused: %v", err)
		}
		if s := baseSum(t, e); s != 0 {
			t.Fatalf("the entry is out by %d cents", s)
		}
		again, _ := PostJournal(j, settings)
		if a, b := fmt.Sprint(e), fmt.Sprint(again); a != b || e.PostingIdentity != again.PostingIdentity {
			t.Fatal("reposting the same version gave a different entry or identity")
		}
		r, err := Reverse(e, day.AddDate(0, 0, 1), "test")
		if err != nil {
			t.Fatal(err)
		}
		net := map[string]int64{}
		for _, l := range append(append([]Line{}, e.Lines...), r.Lines...) {
			n, _ := l.Amount.Minor()
			b, _ := l.Base.Minor()
			net[l.AccountID.String()+"/"+l.Amount.Currency().Code] += n
			net[l.AccountID.String()+"/base"] += b
		}
		for k, v := range net {
			if v != 0 {
				t.Fatalf("original + reversal leaves %d on %s", v, k)
			}
		}
		if r.PostingIdentity == e.PostingIdentity || r.Source != e.Source {
			t.Fatalf("the reversal's identity %q or source %+v is wrong", r.PostingIdentity, r.Source)
		}
	})
}

// req: ENG-22
func TestLengthLimitsAreInclusive(t *testing.T) {
	j := sale(t, "1.00")
	j.Memo = strings.Repeat("m", MaxMemo)
	j.Lines[0].Description = strings.Repeat("d", MaxDescription)
	e, err := PostJournal(j, settings)
	if err != nil {
		t.Fatalf("a memo of %d and a description of %d characters were refused: %v", MaxMemo, MaxDescription, err)
	}
	j.Lines[1].Description = strings.Repeat("d", MaxDescription+1)
	if _, err := PostJournal(j, settings); err == nil || !err.(Problems).Has(CodeTooLong) {
		t.Fatalf("a description of %d characters: %v", MaxDescription+1, err)
	}

	r, err := Reverse(e, day, "")
	if err != nil || r.Memo != "Reversal" {
		t.Fatalf("a reversal without a reason has memo %q, %v; want \"Reversal\"", r.Memo, err)
	}
	fits := strings.Repeat("r", MaxMemo-len("Reversal: "))
	if r, err := Reverse(e, day, fits); err != nil || len(r.Memo) != MaxMemo {
		t.Fatalf("a reason that fills the memo exactly: %d characters, %v", len(r.Memo), err)
	}
	if _, err := Reverse(e, day, fits+"r"); err == nil || !err.(Problems).Has(CodeTooLong) {
		t.Fatalf("a reason one character too long: %v", err)
	}
	if _, err := Reverse(e, time.Time{}, "x"); err == nil || !err.(Problems).Has(CodeNoDate) {
		t.Fatalf("a reversal without a date: %v", err)
	}
}
