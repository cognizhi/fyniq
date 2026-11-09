package differential

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// mutated is golden/bank.json with change made to it, as JSON.
func mutated(t *testing.T, change func(s map[string]any)) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(golden, "bank.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s map[string]any
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	change(s)
	out, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// A scenario that breaks a rule of the P1 sources, the statements or the KPIs is invalid, as the oracle finds it
// (test/differential/README.md, "Invalid scenario or rejected document").
// req: QA-ENG-02
func TestP1FormatRules(t *testing.T) {
	if _, err := ParseScenario(mutated(t, func(map[string]any) {})); err != nil {
		t.Fatalf("golden/bank.json: %v", err)
	}
	settings := func(s map[string]any) map[string]any { return s["settings"].(map[string]any) }
	for name, change := range map[string]func(s map[string]any){
		"no as_of":                func(s map[string]any) { delete(s, "as_of") },
		"no settings":             func(s map[string]any) { delete(s, "settings") },
		"as_of before a document": func(s map[string]any) { s["as_of"] = "2026-04-06" },
		"a reversal of a bank line": func(s map[string]any) {
			s["reversals"] = []any{map[string]any{"document": "BT-0001", "date": "2026-03-06"}}
		},
		"a settings account not in accounts": func(s map[string]any) { settings(s)["accounts"].(map[string]any)["fx"] = "7999" },
		"a money account not in accounts":    func(s map[string]any) { settings(s)["money_accounts"].(map[string]any)["1099"] = "SGD" },
		"a money account's undeclared currency": func(s map[string]any) {
			settings(s)["money_accounts"].(map[string]any)["1021"] = "EUR"
		},
		"a books start that is not a date":     func(s map[string]any) { settings(s)["books_start"] = "2026-02-30" },
		"statements with no retained earnings": func(s map[string]any) { s["statements"] = true },
		"kpis with no statements": func(s map[string]any) {
			s["kpis"] = map[string]any{"year_start_month": 1, "counts": map[string]any{}}
		},
		"votes per share off share capital": func(s map[string]any) { // README, "Cap tables"
			s["accounts"].([]any)[0].(map[string]any)["votes_per_share"] = "1"
		},
		"a cap table day that is not a date": func(s map[string]any) { s["cap_tables"] = []any{"2026-02-30"} },
		"two retained-earnings accounts": func(s map[string]any) {
			accounts := s["accounts"].([]any)
			for _, i := range []int{8, 9} { // 2400 and 2999, made equity
				a := accounts[i].(map[string]any)
				a["type"], a["role"] = "equity", "retained-earnings"
			}
		},
		"retained earnings on a liability": func(s map[string]any) { s["accounts"].([]any)[9].(map[string]any)["role"] = "retained-earnings" },
	} {
		var invalid *InvalidError
		if _, err := ParseScenario(mutated(t, change)); !errors.As(err, &invalid) {
			t.Errorf("%s: want an invalid scenario, got %v", name, err)
		}
	}
}

// The bank lines of golden/bank.json, worked out by hand: BT-0001 pays 109.00 coded TX at 9% while registered, so
// 2200 takes 109.00 × 9 ÷ 109 = 9.00 of input tax and 6010 the 100.00 rest; BT-0002 receives 327.00 coded SR and
// spread over 3 months, so 2200 takes 27.00 of output tax and 2400 the 300.00 rest, released 100.00 a month: on
// its own date in March (the month starts before it), on 1 April, and not on 1 May, after as_of (30 April). The
// releases come after every document, in canonical form; the money account's line, a part on a money account and a
// spread on an asset (BT-0005, BT-0006) are refused.
// req: FR-BNK-06
// req: FR-GL-03
// req: ENG-05
// req: QA-ENG-02
func TestP1EntriesAndReleases(t *testing.T) {
	r := run(t, load(t, "bank.json"))
	if want := []string{"BT-0005", "BT-0006"}; !reflect.DeepEqual(r.Rejected, want) {
		t.Errorf("rejected %v, want %v", r.Rejected, want)
	}
	var order []string
	for _, e := range r.Entries {
		order = append(order, e.Kind+" "+e.Document+" "+e.PostingDate)
	}
	want := []string{"post BT-0001 2026-03-05", "post BT-0002 2026-03-10", "post BT-0003 2026-03-12", "post BT-0004 2026-03-20",
		"post TR-0001 2026-04-02", "post TR-0002 2026-04-03", "post BT-0007 2026-04-07",
		"release BT-0002 2026-03-10", "release BT-0002 2026-04-01"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("entries %v\nwant %v", order, want)
	}
	for i, lines := range map[int][]EntryLine{
		0: {el("1010", "SGD", "-109.00", "-109.00"), el("2200", "SGD", "9.00", "9.00"), el("6010", "SGD", "100.00", "100.00")},
		1: {el("1010", "SGD", "327.00", "327.00"), el("2200", "SGD", "-27.00", "-27.00"), el("2400", "SGD", "-300.00", "-300.00")},
		7: {el("2400", "SGD", "100.00", "100.00"), el("4010", "SGD", "-100.00", "-100.00")},
		8: {el("2400", "SGD", "100.00", "100.00"), el("4010", "SGD", "-100.00", "-100.00")},
	} {
		if got := r.Entries[i].Lines; !reflect.DeepEqual(got, lines) {
			t.Errorf("%s: %v, want %v", order[i], got, lines)
		}
	}
}

// The bills and payments of golden/purchases.json, worked out by hand (README, "Bills ..."): BILL-0001 is USD 1,000.00
// at 1.34 (6010 1,340.00) with S$121.50 of GST stated, so 2010 takes USD 1,090.00 = 1,460.60 and 7030 the 0.90 left;
// PAY-0001 pays it at 1.30, 1,417.00, a gain of 43.60 to 7030. PAY-0002 pays 1,089.50 of BILL-0002's 1,090.00 in full,
// 0.50 within the S$1.00 tolerance, to 7030. BILL-0003 spreads 1,200.00 over 12 months from January, so its date in
// March releases January to March's 100.00 each in one entry, and 1 April the fourth. PAY-0003 pays 500.00 of its
// 1,308.00 and keeps 20.00 unapplied; PAY-0004's 900.00 is above the 808.00 left, and BILL-0005 spreads an asset's
// cost, so both are refused.
// req: FR-AP-02
// req: ENG-41
// req: FR-BNK-08
// req: FR-GL-03
// req: QA-ENG-02
func TestP1PurchasesSettleWhatIsOpen(t *testing.T) {
	r := run(t, load(t, "purchases.json"))
	if want := []string{"BILL-0005", "PAY-0004"}; !reflect.DeepEqual(r.Rejected, want) {
		t.Errorf("rejected %v, want %v", r.Rejected, want)
	}
	byKey := map[string][]EntryLine{}
	for _, e := range r.Entries {
		byKey[e.Kind+" "+e.Document+" "+e.PostingDate] = e.Lines
	}
	for key, lines := range map[string][]EntryLine{
		"post BILL-0001 2026-03-05": {el("2010", "USD", "-1090.00", "-1460.60"), el("2200", "SGD", "121.50", "121.50"), el("6010", "USD", "1000.00", "1340.00"),
			el("7030", "SGD", "-0.90", "-0.90")},
		"post PAY-0001 2026-03-20":     {el("1021", "USD", "-1090.00", "-1417.00"), el("2010", "USD", "1090.00", "1460.60"), el("7030", "SGD", "-43.60", "-43.60")},
		"post PAY-0002 2026-03-25":     {el("1010", "SGD", "-1089.50", "-1089.50"), el("2010", "SGD", "1090.00", "1090.00"), el("7030", "SGD", "-0.50", "-0.50")},
		"post PAY-0003 2026-04-02":     {el("1010", "SGD", "-520.00", "-520.00"), el("2010", "SGD", "520.00", "520.00")},
		"release BILL-0003 2026-03-15": {el("1210", "SGD", "-300.00", "-300.00"), el("6010", "SGD", "300.00", "300.00")},
		"release BILL-0003 2026-04-01": {el("1210", "SGD", "-100.00", "-100.00"), el("6010", "SGD", "100.00", "100.00")},
	} {
		if got := byKey[key]; !reflect.DeepEqual(got, lines) {
			t.Errorf("%s: %v, want %v", key, got, lines)
		}
	}
}

// The sales of golden/sales.json, worked out by hand (README, "Invoices ..."): INV-0004 is 100.00 zero-rated and
// deducts 100.00 of DP-0001's 1,090.00 (S$90.00 of GST), so under RND-DEPOSIT 2410 takes 100.00 less 90 × 100 ÷ 1,090
// = 8.26 of GST, and 1100 nothing. CN-0001 (10.95) and CN-0002 (20.10 + 1.81) credit INV-0001's 32.86 together, so
// CN-0003's 0.01 is refused (FR-AR-04, rev 0.155). RF-0001 refunds 545.00 of the 990.00 left: 990.00 × 545 ÷ 990 of
// its base and 81.74 × 545 ÷ 990 = 45.00 of its GST. RC-0001 receives USD 9,710.00 of INV-0005 at 1.32 against 1.35,
// a loss of 291.30, and WO-0001 writes off the USD 290.00 left, clearing its 391.50. RC-0003's 11.00 is above
// INV-0002's 10.95, and INV-0006 spreads an expense, so both are refused.
// req: FR-AR-03
// req: FR-AR-04
// req: FR-AR-09
// req: FR-AR-17
// req: ENG-41
// req: QA-ENG-02
func TestP1SalesCreditDepositsAndWriteOffs(t *testing.T) {
	r := run(t, load(t, "sales.json"))
	if want := []string{"CN-0003", "RC-0003", "INV-0006"}; !reflect.DeepEqual(r.Rejected, want) {
		t.Errorf("rejected %v, want %v", r.Rejected, want)
	}
	byKey := map[string][]EntryLine{}
	for _, e := range r.Entries {
		byKey[e.Kind+" "+e.Document+" "+e.PostingDate] = e.Lines
	}
	for key, lines := range map[string][]EntryLine{
		"post INV-0004 2026-03-31": {el("2200", "SGD", "8.26", "8.26"), el("2410", "SGD", "91.74", "91.74"), el("4020", "SGD", "-100.00", "-100.00")},
		"post CN-0002 2026-04-03":  {el("1100", "SGD", "-21.91", "-21.91"), el("2200", "SGD", "1.81", "1.81"), el("4010", "SGD", "20.10", "20.10")},
		"post RF-0001 2026-04-10":  {el("1010", "SGD", "-545.00", "-545.00"), el("2200", "SGD", "45.00", "45.00"), el("2410", "SGD", "500.00", "500.00")},
		"post RC-0001 2026-03-20":  {el("1021", "USD", "9710.00", "12817.20"), el("1100", "USD", "-9710.00", "-13108.50"), el("7030", "SGD", "291.30", "291.30")},
		"post WO-0001 2026-04-30":  {el("1100", "USD", "-290.00", "-391.50"), el("7030", "SGD", "391.50", "391.50")},
	} {
		if got := byKey[key]; !reflect.DeepEqual(got, lines) {
			t.Errorf("%s: %v, want %v", key, got, lines)
		}
	}
}

// The claims of golden/claims.json, worked out by hand (README, "Expense claims ..."): EC-0001 owes 100.00 + 9.00 of
// GST + a trip of 12.35 km, 12.4 km under RND-MILEAGE × 0.60 = 7.44, all 116.44 off ADV-0001's 500.00 on 1250;
// EC-0002 owes 450.00, the 383.56 left of the advance on 1250 and 66.44 on 2150. EC-0003 is USD 50.00 at 1.35, so 2150
// owes a fixed S$67.50 (FR-AP-17, rev 0.169) and RE-0001 pays exactly that. EC-0004's 7.3 km × 0.25 = 1.83 and
// 100 km × 0.60 = 60.00 owe 61.83; RE-0002 pays 66.44 + 61.83 = 128.27, and RE-0003's 0.01 more is refused. EC-0006
// is kept as a loan, 218.00 on 2500 naming P0001; a trip in USD, a loan with an advance, a loan to someone not among
// the people and a reimbursement of the loan are refused. EC-0009, in USD at 1.35, states it paid S$148.02 for a
// TX receipt of USD 109.00 whose GST it states as S$12.20, and owes an OP receipt of USD 20.00 at the rate, 27.00:
// 175.02 owed, 150.00 of it settling ADV-0003 and 25.02 on 2150, with 7030 taking 175.02 − 135.00 − 27.00 − 12.20 =
// 0.82. EC-0010 states S$53.80 for USD 40.00 at 1.35 (54.00), 7030 taking −0.20. `paid` on an SGD claim, and of 0.00,
// are refused.
// req: FR-AP-16
// req: FR-AP-17
// req: FR-EQ-02
// req: QA-ENG-02
func TestP1ClaimsOweAFixedSGDAmount(t *testing.T) {
	date := func(r Result, doc string) string { // a document's posting date, as its entry gives it
		for _, e := range r.Entries {
			if e.Document == doc {
				return e.PostingDate
			}
		}
		return ""
	}
	r := run(t, load(t, "claims.json"))
	if want := []string{"RE-0003", "EC-0005", "EC-0007", "EC-0008", "RE-0004", "EC-0011", "EC-0012"}; !reflect.DeepEqual(r.Rejected, want) {
		t.Errorf("rejected %v, want %v", r.Rejected, want)
	}
	byKey := map[string][]EntryLine{}
	for _, e := range r.Entries {
		byKey[e.Kind+" "+e.Document+" "+e.PostingDate] = e.Lines
	}
	loan := el("2500", "SGD", "-218.00", "-218.00")
	loan.Party = "P0001"
	for key, lines := range map[string][]EntryLine{
		"post EC-0001 2026-03-05": {el("1250", "SGD", "-116.44", "-116.44"), el("2200", "SGD", "9.00", "9.00"), el("6010", "SGD", "100.00", "100.00"),
			el("6110", "SGD", "7.44", "7.44")},
		"post EC-0002 2026-03-06": {el("1250", "SGD", "-383.56", "-383.56"), el("2150", "SGD", "-66.44", "-66.44"), el("6010", "SGD", "450.00", "450.00")},
		"post EC-0003 2026-03-10": {el("2150", "SGD", "-67.50", "-67.50"), el("6010", "USD", "50.00", "67.50")},
		"post RE-0001 2026-03-31": {el("1010", "SGD", "-67.50", "-67.50"), el("2150", "SGD", "67.50", "67.50")},
		"post EC-0004 2026-03-12": {el("2150", "SGD", "-61.83", "-61.83"), el("6110", "SGD", "61.83", "61.83")},
		"post EC-0006 2026-04-08": {el("2200", "SGD", "18.00", "18.00"), loan, el("6010", "SGD", "200.00", "200.00")},
		"post EC-0009 " + date(r, "EC-0009"): {el("1250", "SGD", "-150.00", "-150.00"), el("2150", "SGD", "-25.02", "-25.02"),
			el("2200", "SGD", "12.20", "12.20"), el("6010", "USD", "100.00", "135.00"), el("6110", "USD", "20.00", "27.00"), el("7030", "SGD", "0.82", "0.82")},
		"post EC-0010 " + date(r, "EC-0010"): {el("2150", "SGD", "-53.80", "-53.80"), el("6010", "USD", "40.00", "54.00"), el("7030", "SGD", "-0.20", "-0.20")},
	} {
		if got := byKey[key]; !reflect.DeepEqual(got, lines) {
			t.Errorf("%s: %v, want %v", key, got, lines)
		}
	}
}

// The assets of golden/assets.json, worked out by hand (README, "Fixed assets ..."): FA-0001 depreciates 3,600.00 over
// 36 months, 100.00 a month on each month's last day; AD-0001 sells it on 10 April for 3,270.00 coded SR, 270.00 of it
// output GST, against 3,300.00 carried after January to March, a loss of 300.00. FA-0002 was bought in November 2025,
// before the books start, so the opening balances carry November and December and it posts 100.00 a month from
// January. FA-0003's 1,000.00 over 3 months from March is 333.33, 333.34, 333.33 under ALLOC-EVEN. FA-0004 (2,000.00 less
// 200.00 over 18 months) is sold on the clearing account for 1,500.00 against 1,600.00, a loss of 100.00, and depreciates
// no more from May. A second disposal of FA-0001 and an asset on an expense account are refused.
// req: FR-FA-01
// req: FR-FA-02
// req: FR-FA-03
// req: QA-ENG-02
func TestP1AssetsDepreciateAndDispose(t *testing.T) {
	r := run(t, load(t, "assets.json"))
	if want := []string{"AD-0003", "FA-0005"}; !reflect.DeepEqual(r.Rejected, want) {
		t.Errorf("rejected %v, want %v", r.Rejected, want)
	}
	months := map[string][]string{}
	byKey := map[string][]EntryLine{}
	for _, e := range r.Entries {
		byKey[e.Kind+" "+e.Document+" "+e.PostingDate] = e.Lines
		if e.Kind == "depreciation" {
			months[e.Document] = append(months[e.Document], e.PostingDate+" "+e.Lines[1].Amount)
		}
	}
	for doc, want := range map[string][]string{
		"FA-0001": {"2026-01-31 100.00", "2026-02-28 100.00", "2026-03-31 100.00"},
		"FA-0002": {"2026-01-31 100.00", "2026-02-28 100.00", "2026-03-31 100.00", "2026-04-30 100.00", "2026-05-31 100.00"},
		"FA-0003": {"2026-03-31 333.33", "2026-04-30 333.34", "2026-05-31 333.33"},
		"FA-0004": {"2026-01-31 100.00", "2026-02-28 100.00", "2026-03-31 100.00", "2026-04-30 100.00"},
	} {
		if !reflect.DeepEqual(months[doc], want) {
			t.Errorf("%s's months %v, want %v", doc, months[doc], want)
		}
	}
	for key, lines := range map[string][]EntryLine{
		"post AD-0001 2026-04-10": {el("1010", "SGD", "3270.00", "3270.00"), el("1500", "SGD", "-3600.00", "-3600.00"), el("1590", "SGD", "300.00", "300.00"),
			el("2200", "SGD", "-270.00", "-270.00"), el("7050", "SGD", "300.00", "300.00")},
		"post AD-0002 2026-05-05": {el("1160", "SGD", "1500.00", "1500.00"), el("1510", "SGD", "-2000.00", "-2000.00"), el("1590", "SGD", "400.00", "400.00"),
			el("7050", "SGD", "100.00", "100.00")},
	} {
		if got := byKey[key]; !reflect.DeepEqual(got, lines) {
			t.Errorf("%s: %v, want %v", key, got, lines)
		}
	}
}

// The founders' money of golden/founders.json, worked out by hand (README, "Costs before the books start" to
// "Conversions"): P0001 paid 1,315.00 and 763.00 before the books start, waived 1,000.00 to 3300 and was repaid 500.00,
// owing them 578.00 on 2500; P0002 paid 3,815.00 (315.00 of GST claimed), was voted a 6,000.00 fee on 2120 and repaid
// 2,000.00 of it and 815.00 of the loan. RND-SHARE converts 99.60 of P0001's 100.00 at 1.20 (83 shares, 0.40 left), 299.60
// of 300.00 at 0.70 (428 shares) worth 321.00 at fair value, a loss of 21.40; 1,000.00 of P0002's loan at 2.00 worth
// 750.00, a gain of 250.00; and 1,498.90 of their fees at 1.30 (1,153 shares). What each is still owed: P0001 2500 −178.80;
// P0002 2120 −2,501.10, 2500 −2,000.00.
// req: FR-EQ-01
// req: FR-EQ-03
// req: FR-EQ-04
// req: FR-EQ-05
// req: FR-EQ-06
// req: FR-EQ-07
// req: RND-SHARE
// req: QA-ENG-02
func TestP1FoundersMoneyByPerson(t *testing.T) {
	r := run(t, load(t, "founders.json"))
	want := map[string]map[string]string{"P0001": {"2500": "-178.80"}, "P0002": {"2120": "-2501.10", "2500": "-2000.00"}}
	if !reflect.DeepEqual(r.People, want) {
		t.Errorf("people %v, want %v", r.People, want)
	}
	byDoc := map[string][]EntryLine{}
	for _, e := range r.Entries {
		byDoc[e.Document] = e.Lines
	}
	party := func(l EntryLine, p string) EntryLine { l.Party = p; return l }
	for doc, lines := range map[string][]EntryLine{
		"SI-0001":  {party(el("2500", "SGD", "99.60", "99.60"), "P0001"), el("3010", "SGD", "-99.60", "-99.60")},
		"SI-0002":  {party(el("2500", "SGD", "1000.00", "1000.00"), "P0002"), el("3010", "SGD", "-750.00", "-750.00"), el("4900", "SGD", "-250.00", "-250.00")},
		"SI-0003":  {party(el("2500", "SGD", "299.60", "299.60"), "P0001"), el("3010", "SGD", "-321.00", "-321.00"), el("7060", "SGD", "21.40", "21.40")},
		"SI-0004":  {party(el("2120", "SGD", "1498.90", "1498.90"), "P0002"), el("3010", "SGD", "-1498.90", "-1498.90")},
		"PIC-0002": {el("1600", "SGD", "3500.00", "3500.00"), el("2200", "SGD", "315.00", "315.00"), party(el("2500", "SGD", "-3815.00", "-3815.00"), "P0002")},
	} {
		if got := byDoc[doc]; !reflect.DeepEqual(got, lines) {
			t.Errorf("%s: %v, want %v", doc, got, lines)
		}
	}
}

// G4-043 in golden/equity.json, worked out by hand (README, "Cash share issues"): an investor's S$600,000.00 arrives on
// 26 Feb on a bank line naming them on 2040; on 12 Mar, 250,000 shares at 2.40 take it into 3010 from the register.
// P0004's S$1,000.00, refunded from the bank on 10 Mar, pays nothing towards their issue on 11 Mar, which is refused; so
// are a person on a line with no part on 2040, a part on 2040 naming no one, a person who is no subscriber and a class
// on an expense account.
// req: FR-EQ-17
// req: QA-ENG-02
func TestP1CashShareIssue(t *testing.T) {
	r := run(t, load(t, "equity.json"))
	if want := []string{"EQ-0002", "BT-0004", "BT-0005", "BT-0006", "EQ-0003"}; !reflect.DeepEqual(r.Rejected, want) {
		t.Errorf("rejected %v, want %v", r.Rejected, want)
	}
	byDoc := map[string][]EntryLine{}
	for _, e := range r.Entries {
		byDoc[e.Document] = e.Lines
	}
	party := func(l EntryLine, p string) EntryLine { l.Party = p; return l }
	for doc, lines := range map[string][]EntryLine{
		"BT-0001": {el("1010", "SGD", "600000.00", "600000.00"), party(el("2040", "SGD", "-600000.00", "-600000.00"), "P0003")},
		"EQ-0001": {party(el("2040", "SGD", "600000.00", "600000.00"), "P0003"), el("3010", "SGD", "-600000.00", "-600000.00")},
	} {
		if got := byDoc[doc]; !reflect.DeepEqual(got, lines) {
			t.Errorf("%s: %v, want %v", doc, got, lines)
		}
	}
	if len(r.People) != 0 {
		t.Errorf("people %v: every subscriber's monies net to nothing by 31 Mar", r.People)
	}
}

// The cap tables of golden/cap-table.json, worked out by hand with exact fractions (README, "Cap tables"): G.3's round
// on 12 Mar, 1,000,000, 200,000 and 52,083 of 1,252,083 shares, 79.8669…, 15.9733… and 4.1597…%; on 15 Apr, after
// 50,000 moved from the founder to the investor and 10,000 preference shares with no votes, 950,000 of 1,262,083 is
// 75.2724… of the shares and 75.8736… of the 1,252,083 votes, and the investor's 260,000 is 20.6009…; on 31 May the
// SAFE holder's 52,083 are the founder's, 1,002,083: 79.3995… and 80.0332…. The transfers post nothing; seven are
// refused, EQ-0013 because it leaves P0003 below zero from 10 May though they hold the share on 30 Apr (FY288).
// req: FR-EQ-20
// req: FR-EQ-17
// req: QA-ENG-02
func TestP1CapTable(t *testing.T) {
	r := run(t, load(t, "cap-table.json"))
	if want := []string{"EQ-0006", "EQ-0007", "EQ-0008", "EQ-0009", "EQ-0010", "EQ-0011", "EQ-0013"}; !reflect.DeepEqual(r.Rejected, want) {
		t.Errorf("rejected %v, want %v", r.Rejected, want)
	}
	var docs []string
	for _, e := range r.Entries {
		docs = append(docs, e.Document)
	}
	if want := []string{"BT-0001", "EQ-0001", "BT-0002", "BT-0003", "EQ-0002", "EQ-0003", "BT-0004", "EQ-0005"}; !reflect.DeepEqual(docs, want) {
		t.Errorf("entries %v, want %v: a transfer posts nothing", docs, want)
	}
	pct := func(s string) *string { return &s }
	fig := func(issued, votes, ownership, voting string) CapFigures {
		f := CapFigures{Issued: issued, Votes: votes, FullyDiluted: issued, Ownership: pct(ownership), Voting: pct(voting), Diluted: pct(ownership)}
		return f
	}
	line := func(holder, class, issued, votes, ownership, voting string) CapLine {
		return CapLine{Holder: holder, Class: class, CapFigures: fig(issued, votes, ownership, voting)}
	}
	holder := func(h, issued, votes, ownership, voting string) CapHolder {
		return CapHolder{Holder: h, CapFigures: fig(issued, votes, ownership, voting)}
	}
	want := []CapTable{
		{At: "2027-03-11", Issued: "1000000", Votes: "1000000", Options: "0", Unallocated: "0", FullyDiluted: "1000000",
			Lines:   []CapLine{line("P0001", "3010", "1000000", "1000000", "100.00", "100.00")},
			Holders: []CapHolder{holder("P0001", "1000000", "1000000", "100.00", "100.00")}},
		{At: "2027-03-12", Issued: "1252083", Votes: "1252083", Options: "0", Unallocated: "0", FullyDiluted: "1252083",
			Lines: []CapLine{line("P0001", "3010", "1000000", "1000000", "79.87", "79.87"), line("P0002", "3010", "200000", "200000", "15.97", "15.97"),
				line("P0003", "3010", "52083", "52083", "4.16", "4.16")},
			Holders: []CapHolder{holder("P0001", "1000000", "1000000", "79.87", "79.87"), holder("P0002", "200000", "200000", "15.97", "15.97"),
				holder("P0003", "52083", "52083", "4.16", "4.16")}},
		{At: "2027-04-15", Issued: "1262083", Votes: "1252083", Options: "0", Unallocated: "0", FullyDiluted: "1262083",
			Lines: []CapLine{line("P0001", "3010", "950000", "950000", "75.27", "75.87"), line("P0002", "3010", "250000", "250000", "19.81", "19.97"),
				line("P0002", "3020", "10000", "0", "0.79", "0.00"), line("P0003", "3010", "52083", "52083", "4.13", "4.16")},
			Holders: []CapHolder{holder("P0001", "950000", "950000", "75.27", "75.87"), holder("P0002", "260000", "250000", "20.60", "19.97"),
				holder("P0003", "52083", "52083", "4.13", "4.16")}},
		{At: "2027-05-31", Issued: "1262083", Votes: "1252083", Options: "0", Unallocated: "0", FullyDiluted: "1262083",
			Lines: []CapLine{line("P0001", "3010", "1002083", "1002083", "79.40", "80.03"), line("P0002", "3010", "250000", "250000", "19.81", "19.97"),
				line("P0002", "3020", "10000", "0", "0.79", "0.00")},
			Holders: []CapHolder{holder("P0001", "1002083", "1002083", "79.40", "80.03"), holder("P0002", "260000", "250000", "20.60", "19.97")}},
	}
	if err := sameCapTables(r.CapTables, want); err != nil {
		t.Error(err)
	}
}

// The statements and KPIs of golden/statements.json, worked out by hand (README, "Statements" and "KPIs"): in April
// 50.00 is paid to 1990, an account with no cash-flow line. It sits in no section, so the balance sheet does not
// balance and `unplaced` names it, but the cash flow keeps it under Other accounts: operating −50.00, the net change
// −50.00, and the cash flow ties; net burn is the true 50.00. February's reversed cost adds 99.99 to profit, so its
// operating expenses are +99.99 and the three months to April average costs below zero: DPO is undefined. January is
// the books' first month, so its burn multiple is undefined.
// req: FR-RPT-01
// req: INV-10
// req: INV-11
// req: FR-KPI-03
// req: QA-ENG-02
func TestP1StatementsAndKPIs(t *testing.T) {
	r := run(t, load(t, "statements.json"))
	if r.Statements == nil || r.KPIs == nil {
		t.Fatal("no statements or KPIs")
	}
	at := func(tree map[string]any, path ...string) any {
		var x any = tree
		for _, p := range path {
			m, ok := x.(map[string]any)
			if !ok {
				return nil
			}
			x = m[p]
		}
		return x
	}
	st, kpis := *r.Statements, *r.KPIs
	for _, c := range []struct {
		tree map[string]any
		path []string
		want any
	}{
		{st, []string{"2026-04", "cash_flow", "operating", "other_accounts"}, "-50.00"},
		{st, []string{"2026-04", "cash_flow", "operating", "total"}, "-50.00"},
		{st, []string{"2026-04", "cash_flow", "net_change"}, "-50.00"},
		{st, []string{"2026-04", "checks", "balances"}, false},
		{st, []string{"2026-04", "checks", "ties"}, true},
		{st, []string{"2026-02", "profit_and_loss", "operating_expenses", "total"}, "99.99"},
		{kpis, []string{"2026-04", "net_burn"}, "50.00"},
		{kpis, []string{"2026-04", "dpo"}, nil},
		{kpis, []string{"2026-01", "burn_multiple"}, nil},
	} {
		if got := at(c.tree, c.path...); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%v: %v, want %v", c.path, got, c.want)
		}
	}
	if got := at(st, "2026-04", "checks", "unplaced"); !reflect.DeepEqual(got, []any{"1990"}) {
		t.Errorf("April's unplaced accounts: %v", got)
	}
}

// A scenario asking for the statements keeps every month's balance below 10^16: the database refuses the posting that
// would take one there, which neither side models, so the runner stops and names the balance instead of laying
// statements on a figure the product could not hold (README, "Balances within NUMERIC(20,4)").
// req: QA-ENG-02
// req: INV-05
func TestP1StatementsStopAtABalanceTheDatabaseCannotHold(t *testing.T) {
	var s Scenario
	err := json.Unmarshal([]byte(`{"schema": "fyniq-scenario@1", "name": "beyond", "base_currency": "SGD", "currencies": {"SGD": 2},
		"accounts": [{"code": "1000", "type": "asset", "group": false, "active": true},
			{"code": "1001", "type": "asset", "group": false, "active": true},
			{"code": "9100", "type": "equity", "group": false, "active": true, "role": "retained-earnings"}],
		"documents": [
			{"type": "manual_journal", "id": "JNL-0001", "version": 1, "date": "2025-11-01", "memo": "", "lines": [
				{"account": "1000", "currency": "SGD", "amount": "-0.01"}, {"account": "1001", "currency": "SGD", "amount": "0.01"}]},
			{"type": "manual_journal", "id": "JNL-0002", "version": 1, "date": "2025-11-01", "memo": "", "lines": [
				{"account": "1001", "currency": "SGD", "amount": "9999999999999999.99"},
				{"account": "1000", "currency": "SGD", "amount": "-9999999999999999.99"}]}],
		"reversals": [], "statements": true}`), &s)
	if err != nil {
		t.Fatal(err)
	}
	_, err = RunEngine(s)
	if err == nil || !strings.Contains(err.Error(), "account 1000's balance for 2025-11 is -10000000000000000.00 SGD") ||
		!strings.Contains(err.Error(), "outside the format") {
		t.Errorf("the statements beyond NUMERIC(20,4): %v", err)
	}
	s.Statements = false
	if r := run(t, s); len(r.Rejected) != 0 { // without statements both sides accept it, as the README says
		t.Errorf("rejected %v", r.Rejected)
	}
	// Only a period's balance is bounded: the statements sum the months without a limit, so figures above 10^16 from
	// months that each fit are laid, as the product's own financials and KPIs lay them.
	s.Statements, s.Documents = true, nil
	s.Accounts[1].CashFlowLine = "Cash & cash equivalents"
	for i, date := range []string{"2025-11-01", "2025-12-01"} {
		s.Documents = append(s.Documents, Document{Type: "manual_journal", ID: fmt.Sprintf("BIG-%d", i+1), Version: 1, Date: date,
			Lines: []Line{{Account: "1001", Currency: "SGD", Amount: "9000000000000000.00"}, {Account: "1000", Currency: "SGD", Amount: "-9000000000000000.00"}}})
	}
	r := run(t, s)
	if got := (*r.Statements)["2025-12"].(map[string]any)["balance_sheet"].(map[string]any)["current_assets"].(map[string]any)["groups"].(map[string]any)["1001"]; got != "18000000000000000.00" {
		t.Errorf("1001 at the end of December: %v", got)
	}
}

// A waiver, a conversion and a repayment's settlements on one account settle at most what the company owes the person
// there at the end of the document's date, from the documents before it (README, "P1 readings"): golden/founders.json
// ends owing P0001 178.80 on 2500 and P0002 2,501.10 on 2120 and 2,000.00 on 2500. A credit dated after the settlement
// does not count, so a waiver on 2120 the day before P0002's fee settles nothing.
// req: FR-EQ-05
// req: QA-ENG-02
func TestP1FoundersSettleAtMostWhatIsOwed(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(golden, "founders.json"))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	var extra []any
	if err := json.Unmarshal([]byte(`[
		{"type": "director_waiver", "id": "OW-1", "version": 1, "date": "2026-04-30", "person": "P0001", "account": "2500", "credit": "3300", "amount": "178.81"},
		{"type": "director_waiver", "id": "OW-2", "version": 1, "date": "2026-04-30", "person": "P0001", "account": "2500", "credit": "3300", "amount": "178.80"},
		{"type": "director_conversion", "id": "OW-3", "version": 1, "date": "2026-04-30", "person": "P0002", "account": "2120", "class": "3010", "amount": "2501.11", "price": "1.00"},
		{"type": "director_repayment", "id": "OW-4", "version": 1, "date": "2026-04-30", "person": "P0002", "account": "1010",
			"settlements": [{"account": "2500", "amount": "1000.00"}, {"account": "2500", "amount": "1000.01"}]},
		{"type": "director_repayment", "id": "OW-5", "version": 1, "date": "2026-04-30", "person": "P0002", "account": "1010",
			"settlements": [{"account": "2500", "amount": "1000.00"}, {"account": "2500", "amount": "1000.00"}]},
		{"type": "director_waiver", "id": "OW-6", "version": 1, "date": "2026-03-30", "person": "P0002", "account": "2120", "credit": "3300", "amount": "0.01"}]`), &extra); err != nil {
		t.Fatal(err)
	}
	raw["documents"] = append(raw["documents"].([]any), extra...)
	b, _ = json.Marshal(raw)
	var s Scenario
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	r := run(t, s)
	refused := map[string]bool{}
	for _, id := range r.Rejected {
		refused[id] = true
	}
	for id, want := range map[string]bool{"OW-1": true, "OW-2": false, "OW-3": true, "OW-4": true, "OW-5": false, "OW-6": true} {
		if refused[id] != want {
			t.Errorf("%s refused = %v, want %v", id, refused[id], want)
		}
	}
	if got := r.People["P0001"]["2500"]; got != "" && got != "0.00" {
		t.Errorf("P0001 on 2500 after waiving all 178.80: %q", got)
	}
}

// The general ledger's shapes the format refuses as a whole (README, "Opening balances, automatic reversals and
// edits"), each a change to golden/general-ledger.json: a second set of opening balances, opening balances without a
// books' start or dated another day, or with the statements; a reverse_on without as_of; a void of the opening
// balances; an edit of them dated another day or reversing by itself; an edit of a P1 document; and an edit dated
// after as_of.
// req: FR-GL-02
// req: FR-GL-06
// req: FR-GL-12
// req: QA-ENG-01
func TestGeneralLedgerFormatRules(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(golden, "general-ledger.json"))
	if err != nil {
		t.Fatal(err)
	}
	change := func(f func(s map[string]any)) []byte {
		var s map[string]any
		if err := json.Unmarshal(b, &s); err != nil {
			t.Fatal(err)
		}
		f(s)
		out, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	if _, err := ParseScenario(change(func(map[string]any) {})); err != nil {
		t.Fatalf("golden/general-ledger.json: %v", err)
	}
	docs := func(s map[string]any) []any { return s["documents"].([]any) }
	doc := func(s map[string]any, i int) map[string]any { return docs(s)[i].(map[string]any) }
	settings := func(s map[string]any) map[string]any { return s["settings"].(map[string]any) }
	edit := func(s map[string]any, i int) map[string]any {
		return s["reversals"].([]any)[i].(map[string]any)["edit"].(map[string]any)
	}
	for _, c := range []struct {
		name, says string
		change     func(s map[string]any)
	}{
		{"two opening balances", "two opening balances", func(s map[string]any) {
			second := maps.Clone(doc(s, 0))
			second["id"] = "OB-0002"
			s["documents"] = append(docs(s), second)
		}},
		{"opening balances without a books' start", "need settings with books_start", func(s map[string]any) { delete(settings(s), "books_start") }},
		{"opening balances dated another day", "not the books' start", func(s map[string]any) { doc(s, 0)["date"] = "2026-01-02" }},
		{"opening balances with the statements", "statements of a scenario with opening balances", func(s map[string]any) { s["statements"] = true }},
		{"a reverse_on without as_of", "reverses by itself, which needs as_of", func(s map[string]any) {
			s["documents"], s["reversals"] = docs(s)[1:], []any{} // without the opening balances, which need as_of themselves
			delete(s, "as_of")
		}},
		{"a void of the opening balances", "change only by an edit", func(s map[string]any) {
			s["reversals"] = append(s["reversals"].([]any), map[string]any{"document": "OB-0001", "date": "2026-01-01"})
		}},
		{"an edit of the opening balances dated another day", "post at the books' start", func(s map[string]any) { edit(s, 0)["date"] = "2026-01-02" }},
		{"an edit of the opening balances reversing by itself", "post at the books' start", func(s map[string]any) { edit(s, 0)["reverse_on"] = "2026-02-01" }},
		{"an edit dated after as_of", "before the reversal or edit", func(s map[string]any) { edit(s, 5)["date"] = "2026-04-16" }},
		{"a reverse_on that is not a date", "not a calendar date", func(s map[string]any) { doc(s, 1)["reverse_on"] = "2026-02-30" }},
		{"an edit of a P1 document", "a P1 document, which the format cannot edit", func(s map[string]any) {
			s["documents"] = append(docs(s), map[string]any{"type": "director_fee", "id": "DF-0001", "version": 1, "date": "2026-03-31",
				"person": "P0001", "account": "6050", "amount": "10.00"})
			s["reversals"] = append(s["reversals"].([]any), map[string]any{"document": "DF-0001",
				"edit": map[string]any{"version": 2, "date": "2026-03-31", "memo": "", "lines": doc(s, 5)["lines"]}})
		}},
	} {
		var invalid *InvalidError
		if _, err := ParseScenario(change(c.change)); !errors.As(err, &invalid) || !strings.Contains(err.Error(), c.says) {
			t.Errorf("%s: want an invalid scenario saying %q, got %v", c.name, c.says, err)
		}
	}
}

// The month-end revaluation's shapes the format refuses as a whole (README, "The revalued accounts and the period-end
// rates"), each a change to golden/fx-revaluation.json: the revalue flag on an account that is not an asset or a
// liability, or on prepayments; a flagged account without the unrealised-FX account, or with one that is not an
// income or expense account without a role; the statements with a flagged account; and a period-end rate off a
// month's last day, for the base currency, twice for a day, or whose quote stores no rate under RND-RATE.
// req: FR-GL-04
// req: RND-RATE
// req: QA-ENG-01
func TestRevaluationFormatRules(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(golden, "fx-revaluation.json"))
	if err != nil {
		t.Fatal(err)
	}
	change := func(f func(s map[string]any)) []byte {
		var s map[string]any
		if err := json.Unmarshal(b, &s); err != nil {
			t.Fatal(err)
		}
		f(s)
		out, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	if _, err := ParseScenario(change(func(map[string]any) {})); err != nil {
		t.Fatalf("golden/fx-revaluation.json: %v", err)
	}
	settings := func(s map[string]any) map[string]any { return s["settings"].(map[string]any) }
	account := func(s map[string]any, code string) map[string]any {
		for _, a := range s["accounts"].([]any) {
			if a := a.(map[string]any); a["code"] == code {
				return a
			}
		}
		t.Fatalf("no account %s", code)
		return nil
	}
	rates := func(s map[string]any) []any { return settings(s)["period_end_rates"].([]any) }
	rate := func(s map[string]any, i int) map[string]any { return rates(s)[i].(map[string]any) }
	for _, c := range []struct {
		name, says string
		change     func(s map[string]any)
	}{
		{"the flag on an expense account", "only an asset or a liability", func(s map[string]any) { account(s, "7030")["revalue"] = true }},
		{"the flag on prepayments", "prepayments or deferred revenue", func(s map[string]any) {
			settings(s)["accounts"].(map[string]any)["prepayments"] = "1021"
		}},
		{"no unrealised-FX account", "needs as_of and settings.accounts.unrealised_fx", func(s map[string]any) {
			delete(settings(s)["accounts"].(map[string]any), "unrealised_fx")
		}},
		{"an unrealised-FX account with a role", "not an active revenue or expense account", func(s map[string]any) {
			settings(s)["accounts"].(map[string]any)["unrealised_fx"] = "1100"
		}},
		{"the statements with a flagged account", "statements of a scenario with an account marked revalue", func(s map[string]any) {
			s["statements"] = true
			s["accounts"] = append(s["accounts"].([]any), map[string]any{"code": "3900", "type": "equity", "group": false, "active": true, "role": "retained-earnings"})
		}},
		{"a rate off a month's last day", "not on a month's last day", func(s map[string]any) { rate(s, 0)["date"] = "2026-03-30" }},
		{"a rate for the base currency", "for the base currency", func(s map[string]any) { rate(s, 0)["currency"] = "SGD" }},
		{"two rates for a day", "two period-end rates", func(s map[string]any) {
			settings(s)["period_end_rates"] = append(rates(s), maps.Clone(rate(s, 0)))
		}},
		{"a quote that stores no rate", "cannot be stored", func(s map[string]any) { rate(s, 1)["quote"] = "0.000000001" }},
	} {
		var invalid *InvalidError
		if _, err := ParseScenario(change(c.change)); !errors.As(err, &invalid) || !strings.Contains(err.Error(), c.says) {
			t.Errorf("%s: want an invalid scenario saying %q, got %v", c.name, c.says, err)
		}
	}
}

// The account merges of golden/account-merge.json, worked out by hand in oracle/tests/test_merge.py (README, "Account
// merges"; FR-COA-04, Appendix B.8): 1600's JPY 300,000,000 carried at S$2,710,000.00 moves at RND-RATE 0.0090333333
// (S$2,709,999.99) and its S$0.01 left over in SGD beside the S$1,000.00 it holds; its OPS dollars carried at −1.00 move
// at a rate of 1 as the signs differ, S$11.00 left over; its ENG dollars at 1.3366666667. 6120's second year alone
// moves into 6100. 6130 holds nothing, so posts nothing; the refused merges (AM-0014 of a revalued account), the later lines on merged accounts and the
// void and edits of journals on them are rejected.
// req: FR-COA-04
// req: QA-ENG-02
// req: ledger/chart-of-accounts A merge moves the balance by one entry (rev 0.174)
// req: ledger/chart-of-accounts Accounts a merge refuses
// req: ledger/chart-of-accounts A merge waits for what is scheduled
func TestP1AccountMergesMoveWhatTheAccountHolds(t *testing.T) {
	r := run(t, load(t, "account-merge.json"))
	byDoc := map[string][]EntryLine{}
	for _, e := range r.Entries {
		if e.Kind == "post" {
			byDoc[e.Document] = e.Lines
		}
	}
	dim := func(l EntryLine, d string) EntryLine { l.Dimensions = map[string]string{"Department": d}; return l }
	for doc, lines := range map[string][]EntryLine{
		"AM-0001": {el("1600", "JPY", "-300000000", "-2709999.99"), el("1600", "SGD", "-1000.01", "-1000.01"), dim(el("1600", "SGD", "11.00", "11.00"), "OPS"),
			dim(el("1600", "USD", "-150.00", "-200.50"), "ENG"), dim(el("1600", "USD", "-10.00", "-10.00"), "OPS"),
			el("1610", "JPY", "300000000", "2709999.99"), el("1610", "SGD", "1000.01", "1000.01"), dim(el("1610", "SGD", "-11.00", "-11.00"), "OPS"),
			dim(el("1610", "USD", "150.00", "200.50"), "ENG"), dim(el("1610", "USD", "10.00", "10.00"), "OPS")},
		"AM-0002": {el("6100", "SGD", "300.00", "300.00"), dim(el("6100", "SGD", "450.00", "450.00"), "ENG"), el("6120", "SGD", "-300.00", "-300.00"),
			dim(el("6120", "SGD", "-450.00", "-450.00"), "ENG")},
	} {
		if got := byDoc[doc]; !reflect.DeepEqual(got, lines) {
			t.Errorf("%s: %v, want %v", doc, got, lines)
		}
	}
	if _, posted := byDoc["AM-0003"]; posted {
		t.Error("a merge of an account holding nothing posted an entry")
	}
	want := []string{"AM-0004", "AM-0005", "AM-0006", "AM-0007", "AM-0008", "AM-0009", "AM-0010", "AM-0011", "AM-0012", "AM-0013", "AM-0014", "JNL-0014",
		"BT-0003",
		"JNL-0001/reversal", "JNL-0008/edit", "JNL-0015/edit"}
	if !reflect.DeepEqual(r.Rejected, want) {
		t.Errorf("rejected %v, want %v", r.Rejected, want)
	}
}

// The SAFEs of golden/safe-conversion.json and golden/safe-conversion-caps.json through the engine, worked out by hand
// with exact fractions (README, "SAFEs and their conversion"; G4-044, Appendix G.3):
//   - G4-044 on 12 Mar: f = 0.05 over C₀ = 1,000,000, so 2,500,000 × 0.95 ÷ 1,000,000 = 2.375; 52,631 shares for
//     S$124,998.63 (52,631 × 2.375 = 124,998.625), and 1,000,000, 200,000 and 52,631 of 1,252,631 are 79.83%, 15.97%
//     and 4.20%;
//   - on 1 Jun at S$4.00 over 1,252,631: 15% off is 3.40, so 14,705 shares for S$49,997.00; the MFN SAFE 7,500 at
//     4.00; the pre-money SAFE's 3,000,000 ÷ 1,252,631 = 2.394959… cut to 2.3949, so 8,351 for S$19,999.81;
//   - two caps at S$3.00 over 1,000,000: f = 0.1, so 2.25 (55,555 for S$124,998.75) and 4.50, above the round's, so
//     3.00 (83,333 for S$249,999.00); G.3's pre-money SAFE the lower of 2.50 and 2.40: 52,083 for S$124,999.20;
//   - a founder's S$600.00 of 2500 into the SAFE they hold: Dr 2500 · Cr 2700 naming it (director_safe@1), and six refused.
//
// req: FR-EQ-18
// req: RND-SHARE
// req: QA-ENG-02
func TestP1SAFEConversion(t *testing.T) {
	lines := func(r Result, doc string) string {
		for _, e := range r.Entries {
			if e.Document == doc {
				var out []string
				for _, l := range e.Lines {
					out = append(out, strings.TrimSpace(l.Account+" "+l.Amount+" "+l.Instrument))
				}
				return strings.Join(out, "; ")
			}
		}
		return ""
	}
	owners := func(c CapTable) string {
		var out []string
		for _, h := range c.Holders {
			out = append(out, h.Holder+" "+h.Issued+" "+*h.Ownership)
		}
		return strings.Join(out, ", ")
	}
	r := run(t, load(t, "safe-conversion.json"))
	if want := []string{"BT-0090", "BT-0091", "BT-0092", "JNL-0001", "SAFE-0090", "SAFE-0091", "SAFE-0092", "SAFE-0093", "SAFE-0094", "SAFE-0095",
		"EQ-0099", "EQ-0098", "EQ-0097", "EQ-0096", "EQ-0095"}; !reflect.DeepEqual(r.Rejected, want) {
		t.Errorf("rejected %v, want %v", r.Rejected, want)
	}
	for doc, want := range map[string]string{
		"BT-0002": "1010 125000.00; 2700 -125000.00 SAFE-0001",
		"EQ-0003": "2700 124998.63 SAFE-0001; 3010 -124998.63",
		"EQ-0005": "2700 49997.00 SAFE-0002; 2700 30000.00 SAFE-0003; 2700 19999.81 SAFE-0004; 3010 -99996.81",
		"BT-0009": "1010 -3.00; 2700 3.00 SAFE-0002",
	} {
		if got := lines(r, doc); got != want {
			t.Errorf("%s: %s, want %s", doc, got, want)
		}
	}
	if len(r.CapTables) != 3 || owners(r.CapTables[1]) != "P0001 1000000 79.83, P0002 200000 15.97, P0003 52631 4.20" ||
		owners(r.CapTables[2]) != "P0001 1000000 72.30, P0002 300000 21.69, P0003 52631 3.81, P0004 14705 1.06, P0005 7500 0.54, P0006 8351 0.60" {
		t.Errorf("the cap tables: %+v", r.CapTables)
	}
	caps := run(t, load(t, "safe-conversion-caps.json"))
	if got, want := lines(caps, "EQ-0003"), "2700 124998.75 SAFE-0001; 2700 249999.00 SAFE-0002; 2700 124999.20 SAFE-0003; 3010 -499996.95"; got != want {
		t.Errorf("two caps and G.3's pre-money SAFE: %s, want %s", got, want)
	}
	founder := run(t, load(t, "safe-from-a-founder.json"))
	if want := []string{"DS-0090", "DS-0091", "DS-0092", "DS-0093", "DS-0094", "DS-0095"}; !reflect.DeepEqual(founder.Rejected, want) {
		t.Errorf("a founder's balance into their SAFE: rejected %v, want %v", founder.Rejected, want)
	}
	if got, want := lines(founder, "DS-0001"), "2500 600.00; 2700 -600.00 SAFE-0001"; got != want {
		t.Errorf("DS-0001: %s, want %s", got, want)
	}
	if len(caps.Rejected) != 0 || len(caps.CapTables) != 2 ||
		owners(caps.CapTables[1]) != "P0001 1000000 77.40, P0002 101000 7.82, P0003 55555 4.30, P0004 83333 6.45, P0005 52083 4.03" {
		t.Errorf("the conversion of 15 Mar: %v %+v", caps.Rejected, caps.CapTables)
	}
}
