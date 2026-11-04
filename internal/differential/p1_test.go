package differential

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
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
// the people and a reimbursement of the loan are refused.
// req: FR-AP-16
// req: FR-AP-17
// req: FR-EQ-02
// req: QA-ENG-02
func TestP1ClaimsOweAFixedSGDAmount(t *testing.T) {
	r := run(t, load(t, "claims.json"))
	if want := []string{"RE-0003", "EC-0005", "EC-0007", "EC-0008", "RE-0004"}; !reflect.DeepEqual(r.Rejected, want) {
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
