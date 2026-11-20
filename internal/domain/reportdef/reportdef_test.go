package reportdef

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

var sgd = money.MustLookup("SGD")

func sgdM(t *testing.T, s string) money.Money {
	t.Helper()
	m, err := money.Parse(s, sgd)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// chart: 4000 Subscriptions and 4100 Services in the group Revenue, 5000 Hosting in Cost of sales, 6000 Rent unused (all zero).
func chart() ([]storage.Account, map[string]uuid.UUID) {
	ids := map[string]uuid.UUID{}
	mk := func(code, group string) storage.Account {
		id := uuid.New()
		ids[code] = id
		return storage.Account{ID: id, Code: code, Name: code, AccountAttributes: storage.AccountAttributes{ReportGroup: group}}
	}
	return []storage.Account{mk("4000", "Revenue"), mk("4100", "Revenue"), mk("5000", "Cost of sales"), mk("6000", "Rent")}, ids
}

func def(rows ...Row) Definition {
	return Definition{Schema: Schema, Rows: rows, Columns: []Column{{Key: ColCurrent}, {Key: ColPriorPeriod}}}
}

// req: FR-RPT-04
func TestAReportIsWorkedOutFromTheFiguresByHand(t *testing.T) {
	accounts, ids := chart()
	figs := map[uuid.UUID][]money.Money{
		ids["4000"]: {sgdM(t, "600.00"), sgdM(t, "500.00")},
		ids["4100"]: {sgdM(t, "400.00"), sgdM(t, "300.00")},
		ids["5000"]: {sgdM(t, "400.00"), sgdM(t, "300.00")},
	}
	d := def(
		Row{ID: "rev", Label: "Revenue", Kind: KindGroup, Group: "Revenue"},
		Row{ID: "cogs", Label: "Hosting", Kind: KindAccount, Code: "5000"},
		Row{ID: "gm", Label: "Gross margin", Kind: KindCalc, Formula: "rev - cogs", Format: FormatAmount},
		Row{ID: "gmp", Label: "Gross margin %", Kind: KindCalc, Formula: "gm / rev", Format: FormatPercent},
		Row{ID: "x", Label: "Rent", Kind: KindAccount, Code: "6000"},
	)
	got, err := Compute(d, sgd, accounts, figs)
	if err != nil {
		t.Fatal(err)
	}
	// Revenue is 600+400 = 1000.00 now and 500+300 = 800.00 before; the margin 600.00 and 500.00; 60.0% and 62.5% (500/800 exactly).
	want := map[string][2]string{"rev": {"1000.00", "800.00"}, "cogs": {"400.00", "300.00"}, "gm": {"600.00", "500.00"}, "gmp": {"60.0", "62.5"}}
	if len(got) != len(want) { // Rent has only zeros: left out
		t.Fatalf("want %d rows (Rent left out), got %+v", len(want), got)
	}
	for _, r := range got {
		if r.Cells[0].Value != want[r.ID][0] || r.Cells[1].Value != want[r.ID][1] {
			t.Errorf("%s: %+v, want %v", r.ID, r.Cells, want[r.ID])
		}
	}
	// A row of zeros shows when the definition asks.
	d.Filters.IncludeZero = true
	if got, _ = Compute(d, sgd, accounts, figs); len(got) != 5 {
		t.Errorf("include_zero: %d rows", len(got))
	}
}

// req: FR-RPT-04
func TestACalculatedRowRoundsOnceHalfAwayFromZeroAndNeverDividesByZero(t *testing.T) {
	accounts, ids := chart()
	figs := map[uuid.UUID][]money.Money{ids["4000"]: {sgdM(t, "100.05"), sgdM(t, "0.00")}, ids["5000"]: {sgdM(t, "-100.05"), sgdM(t, "1.00")}}
	d := def(
		Row{ID: "a", Label: "A", Kind: KindAccount, Code: "4000"},
		Row{ID: "b", Label: "B", Kind: KindAccount, Code: "5000"},
		Row{ID: "half", Label: "Half of A", Kind: KindCalc, Formula: "a * 0.5", Format: FormatAmount},
		Row{ID: "nhalf", Label: "Half of B", Kind: KindCalc, Formula: "b * 0.5", Format: FormatAmount},
		Row{ID: "third", Label: "A over 3 %", Kind: KindCalc, Formula: "(a + 0) / 3 / 100", Format: FormatPercent},
		Row{ID: "ratio", Label: "A over B", Kind: KindCalc, Formula: "a / b", Format: FormatRatio},
		Row{ID: "chain", Label: "Chain", Kind: KindCalc, Formula: "ratio * 2 - -1", Format: FormatRatio},
	)
	got, err := Compute(d, sgd, accounts, figs)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]Result{}
	for _, r := range got {
		by[r.ID] = r
	}
	// 100.05 × 0.5 = 50.025, which rounds up to 50.03; -100.05 × 0.5 = -50.025, which rounds away from zero to -50.03.
	if c := by["half"].Cells[0]; c.Value != "50.03" {
		t.Errorf("half: %+v", c)
	}
	if c := by["nhalf"].Cells[0]; c.Value != "-50.03" {
		t.Errorf("nhalf: %+v", c)
	}
	// 100.05 / 3 / 100 = 0.3335, shown as 33.4% (33.35 rounds half away from zero to 33.4).
	if c := by["third"].Cells[0]; c.Value != "33.4" {
		t.Errorf("third: %+v", c)
	}
	// -1.00 and 100.05 / -100.05; the second column: 0.00 / 1.00 = 0.00.
	if c := by["ratio"].Cells[0]; c.Value != "-1.00" {
		t.Errorf("ratio: %+v", c)
	}
	// ratio × 2 - -1 = -1 on the exact quotient: -2 + 1 = -1.00.
	if c := by["chain"].Cells[0]; c.Value != "-1.00" {
		t.Errorf("chain: %+v", c)
	}
	// Dividing by zero is not available, and so is every row that uses it.
	zero := map[uuid.UUID][]money.Money{ids["4000"]: {sgdM(t, "10.00"), sgdM(t, "0.00")}, ids["5000"]: {sgdM(t, "0.00"), sgdM(t, "0.00")}}
	got, err = Compute(d, sgd, accounts, zero)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range got {
		if (r.ID == "ratio" || r.ID == "chain") && (!r.Cells[0].NA || r.Cells[0].Value != "") {
			t.Errorf("%s divides by zero and must be not available: %+v", r.ID, r.Cells[0])
		}
	}
}

// req: FR-RPT-04
func TestADefinitionThatBreaksARuleIsRefusedWithItsCode(t *testing.T) {
	cases := map[string]struct {
		d    Definition
		code string
	}{
		"a schema":              {Definition{Schema: "x", Rows: []Row{{ID: "a", Label: "A", Kind: KindHeading}}, Columns: []Column{{Key: ColCurrent}}}, CodeSchema},
		"a forward ref":         {def(Row{ID: "a", Label: "A", Kind: KindCalc, Formula: "b", Format: FormatAmount}, Row{ID: "b", Label: "B", Kind: KindAccount, Code: "4000"}), CodeReference},
		"a reference to itself": {def(Row{ID: "a", Label: "A", Kind: KindCalc, Formula: "a + 1", Format: FormatAmount}), CodeReference},
		"a duplicate ID":        {def(Row{ID: "a", Label: "A", Kind: KindHeading}, Row{ID: "a", Label: "B", Kind: KindHeading}), CodeRow},
		"a bad formula":         {def(Row{ID: "a", Label: "A", Kind: KindCalc, Formula: "1 +", Format: FormatAmount}), CodeFormula},
		"a bad character":       {def(Row{ID: "a", Label: "A", Kind: KindCalc, Formula: "1 ^ 2", Format: FormatAmount}), CodeFormula},
		"an open bracket":       {def(Row{ID: "a", Label: "A", Kind: KindCalc, Formula: "(1 + 2", Format: FormatAmount}), CodeFormula},
		"a format":              {def(Row{ID: "a", Label: "A", Kind: KindCalc, Formula: "1", Format: "hex"}), CodeRowFields},
		"an account's group":    {def(Row{ID: "a", Label: "A", Kind: KindAccount, Code: "4000", Group: "Revenue"}), CodeRowFields},
		"a column":              {Definition{Schema: Schema, Rows: []Row{{ID: "a", Label: "A", Kind: KindHeading}}, Columns: []Column{{Key: "budget"}}}, CodeColumn},
		"a repeated column":     {Definition{Schema: Schema, Rows: []Row{{ID: "a", Label: "A", Kind: KindHeading}}, Columns: []Column{{Key: ColYTD}, {Key: ColYTD}}}, CodeColumn},
		"a filter":              {Definition{Schema: Schema, Rows: []Row{{ID: "a", Label: "A", Kind: KindHeading}}, Columns: []Column{{Key: ColYTD}}, Filters: Filters{DimensionValueIDs: []string{"x"}}}, CodeFilter},
		"no rows":               {Definition{Schema: Schema, Columns: []Column{{Key: ColYTD}}}, CodeLimit},
	}
	for name, c := range cases {
		bad := Validate(c.d)
		found := false
		for _, p := range bad {
			found = found || p.Code == c.code
		}
		if !found {
			t.Errorf("%s: want %s among %+v", name, c.code, bad)
		}
	}
	many := def()
	for i := 0; i <= MaxRows; i++ {
		many.Rows = append(many.Rows, Row{ID: "r" + strings.Repeat("a", 1) + string(rune('a'+i%26)) + string(rune('a'+i/26)), Label: "x", Kind: KindHeading})
	}
	if bad := Validate(many); len(bad) == 0 || bad[0].Code != CodeLimit {
		t.Errorf("61 rows: %+v", bad)
	}
	if got := Validate(def(Row{ID: "a", Label: "A", Kind: KindAccount, Code: "4000"})); len(got) != 0 {
		t.Errorf("a sound definition: %+v", got)
	}
	// An account the chart does not have is named, not left out.
	accounts, _ := chart()
	if _, err := Compute(def(Row{ID: "a", Label: "A", Kind: KindAccount, Code: "9999"}), sgd, accounts, nil); err == nil || !strings.Contains(err.Error(), "9999") {
		t.Errorf("an unknown account: %v", err)
	}
}

// req: FR-RPT-07
func TestAScheduleRunsOnItsWeekdayOrItsDayOfTheMonth(t *testing.T) {
	day := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 15, 30, 0, 0, time.UTC) }
	// 19 Oct 2026 is a Monday: a weekly Monday schedule asked after Monday runs the next Monday, and on a Sunday (7) after Saturday.
	if got := NextRun("weekly", 1, 0, day(2026, 10, 19)); got.Format(time.DateOnly) != "2026-10-26" {
		t.Errorf("after a Monday: %s", got)
	}
	if got := NextRun("weekly", 1, 0, day(2026, 10, 18)); got.Format(time.DateOnly) != "2026-10-19" {
		t.Errorf("after a Sunday: %s", got)
	}
	if got := NextRun("weekly", 7, 0, day(2026, 10, 24)); got.Format(time.DateOnly) != "2026-10-25" {
		t.Errorf("a Sunday schedule after a Saturday: %s", got)
	}
	// Monthly on the 28th, after 28 Feb, runs on 28 Mar; on the 3rd, after 3 Dec, on 3 Jan.
	if got := NextRun("monthly", 0, 28, day(2026, 2, 28)); got.Format(time.DateOnly) != "2026-03-28" {
		t.Errorf("28th: %s", got)
	}
	if got := NextRun("monthly", 0, 3, day(2026, 12, 3)); got.Format(time.DateOnly) != "2027-01-03" {
		t.Errorf("3rd over the year end: %s", got)
	}
}
