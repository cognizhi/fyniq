package capallow

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/asset"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/domain/period"
	"github.com/cognizhi/fyniq/packs"
)

var (
	sgd      = money.MustLookup("SGD")
	calendar = period.Calendar{BooksStart: day(2026, 1, 1), FirstYearEnd: day(2026, 12, 31)}
)

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

func version(t *testing.T) pack.Version {
	t.Helper()
	v, err := pack.Load(packs.FS, "sg-capital-allowances/2026.1")
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func item(t *testing.T, number, cost string, on time.Time, e *asset.TaxElection) Asset {
	t.Helper()
	c, err := money.Parse(cost, sgd)
	if err != nil {
		t.Fatal(err)
	}
	return Asset{ID: uuid.New(), Number: number, Name: number, PurchasedOn: on, Cost: c, Election: e}
}

// figures is what each row of a schedule claims, as "number allowance/carried", in the order of its rows.
func figures(s Schedule) string {
	var out []string
	for _, r := range s.Rows {
		out = append(out, fmt.Sprintf("%s %s/%s", r.Asset.Number, r.Allowance, r.CarriedFw))
	}
	return strings.Join(out, " ")
}

func reasons(s Schedule) string {
	var out []string
	for _, a := range s.Apart {
		out = append(out, a.Asset.Number+":"+a.Reason)
	}
	return strings.Join(out, " ")
}

// IRAS's own example of the three-year write-off: office equipment of S$3,000 bought in the financial year 2020 claims S$1,000 in each of YA 2021, 2022 and 2023, with a tax
// written down value of 2,000, 1,000 and 0 carried forward (here the same asset bought in 2026, so in YAs 2027, 2028 and 2029); and the financial year decides the year of
// assessment: a purchase on 31 December 2026 is YA 2027, the day after is YA 2028.
// req: FR-FA-04
func TestThreeYearWriteOffClaimsAThirdAYearAndCarriesTheRestForward(t *testing.T) {
	v := version(t)
	eq := item(t, "FA-0001", "3000.00", day(2026, 12, 31), &asset.TaxElection{Method: asset.TaxS19A1})
	for ya, want := range map[int]string{2027: "FA-0001 1000.00/2000.00", 2028: "FA-0001 1000.00/1000.00", 2029: "FA-0001 1000.00/0.00"} {
		s, err := Work(v, calendar, sgd, ya, []Asset{eq})
		if err != nil {
			t.Fatal(err)
		}
		if got := figures(s); got != want {
			t.Errorf("YA %d: %s, want %s", ya, got, want)
		}
	}
	for _, ya := range []int{2026, 2030} {
		if s, _ := Work(v, calendar, sgd, ya, []Asset{eq}); len(s.Rows) != 0 || len(s.Apart) != 0 {
			t.Errorf("YA %d: %v %v: before the first claim and after the last there is nothing to say", ya, s.Rows, s.Apart)
		}
	}
	next := item(t, "FA-0002", "3000.00", day(2027, 1, 1), &asset.TaxElection{Method: asset.TaxS19A1})
	if s, _ := Work(v, calendar, sgd, 2028, []Asset{next}); figures(s) != "FA-0002 1000.00/2000.00" {
		t.Errorf("a purchase on 1 January 2027 is YA 2028: %s", figures(s))
	}
}

// A motor vehicle of S$10,000 elected over its prescribed 6 years under section 19: the initial allowance of 20%, 2,000, and the first of six annual allowances of the other
// 8,000 in the first year, then the other five; the cents of 8,000 / 6 fall by ALLOC-EVEN; the allowances add to the cost and the written down value ends at nothing.
// req: FR-FA-04
func TestSectionNineteenClaimsTheInitialAllowanceAndTheAnnualOnesOverTheElectedLife(t *testing.T) {
	v := version(t)
	car := item(t, "FA-0001", "10000.00", day(2026, 6, 1), &asset.TaxElection{Method: asset.TaxS19, PrescribedYears: 6, Years: 6})
	want := []string{"3333.33/6666.67", "1333.34/5333.33", "1333.33/4000.00", "1333.33/2666.67", "1333.34/1333.33", "1333.33/0.00"}
	for i, w := range want {
		s, err := Work(v, calendar, sgd, 2027+i, []Asset{car})
		if err != nil {
			t.Fatal(err)
		}
		if got := figures(s); got != "FA-0001 "+w {
			t.Errorf("YA %d: %s, want %s", 2027+i, got, w)
		}
		if i == 0 && (s.Rows[0].Initial.String() != "2000.00" || s.Rows[0].Annual.String() != "1333.33") {
			t.Errorf("the first year splits into %s and %s", s.Rows[0].Initial, s.Rows[0].Annual)
		}
		if i == 1 && s.Rows[0].Initial.String() != "0.00" {
			t.Errorf("the initial allowance is in the first year only: %s", s.Rows[0].Initial)
		}
	}
	if s, _ := Work(v, calendar, sgd, 2033, []Asset{car}); len(s.Rows) != 0 {
		t.Errorf("fully written off in YA 2032: %v", s.Rows)
	}
}

// IRAS's example of the one-year write-off of low-value assets: seven pieces of S$4,400 bought in one year cost S$30,800, over the S$30,000 cap, so the cap takes six whole pieces and
// the seventh is listed apart, to be claimed another way; an asset of S$5,000.01 is not a low-value asset at all; the totals foot.
// req: FR-FA-04
func TestTheCapTakesWholeLowValueAssetsAndTheRestAreListedApart(t *testing.T) {
	v := version(t)
	var assets []Asset
	for i := 1; i <= 7; i++ {
		assets = append(assets, item(t, fmt.Sprintf("FA-%04d", i), "4400.00", day(2026, 3, i), &asset.TaxElection{Method: asset.TaxS19A10A}))
	}
	assets = append(assets, item(t, "FA-0008", "5000.01", day(2026, 4, 1), &asset.TaxElection{Method: asset.TaxS19A10A}))
	s, err := Work(v, calendar, sgd, 2027, assets)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Rows) != 6 || s.Totals.Allowance.String() != "26400.00" || s.Totals.CarriedFw.String() != "0.00" || s.Totals.BroughtFw.String() != "26400.00" {
		t.Errorf("%d rows, totals %s from %s to %s", len(s.Rows), s.Totals.Allowance, s.Totals.BroughtFw, s.Totals.CarriedFw)
	}
	if got := reasons(s); got != "FA-0007:over_cap FA-0008:over_cap" {
		t.Errorf("listed apart: %s", got)
	}
	// The next year there is nothing to claim of them, and the two stay apart.
	if s, _ = Work(v, calendar, sgd, 2028, assets); len(s.Rows) != 0 || reasons(s) != "FA-0007:over_cap FA-0008:over_cap" {
		t.Errorf("YA 2028: %v %s", s.Rows, reasons(s))
	}
}

// An asset the schedule cannot place is listed apart with why and claims nothing: none elected, a disposal that stands, acquired before YA 2023 (the pack's rules start there), a
// working life the Sixth Schedule's life does not allow, and a base currency that is not Singapore dollars.
// req: FR-FA-04
func TestAnAssetTheScheduleCannotPlaceIsListedApartAndClaimsNothing(t *testing.T) {
	v := version(t)
	none := item(t, "FA-0001", "1000.00", day(2026, 2, 1), nil)
	sold := item(t, "FA-0002", "1000.00", day(2026, 2, 1), &asset.TaxElection{Method: asset.TaxS19A1})
	sold.Disposed = true
	old := item(t, "FA-0003", "1000.00", day(2021, 6, 1), &asset.TaxElection{Method: asset.TaxS19A1})
	badLife := item(t, "FA-0004", "1000.00", day(2026, 2, 1), &asset.TaxElection{Method: asset.TaxS19, PrescribedYears: 6, Years: 16})
	ok := item(t, "FA-0005", "1000.00", day(2026, 2, 1), &asset.TaxElection{Method: asset.TaxS19, PrescribedYears: 16, Years: 16})
	s, err := Work(v, calendar, sgd, 2027, []Asset{none, sold, old, badLife, ok})
	if err != nil {
		t.Fatal(err)
	}
	if got := reasons(s); got != "FA-0003:before_pack FA-0001:no_election FA-0002:disposed FA-0004:bad_life" {
		t.Errorf("listed apart: %s", got)
	}
	if figures(s) != "FA-0005 250.00/750.00" {
		t.Errorf("rows: %s (IA 200 and 800/16 = 50)", figures(s))
	}
	usd := money.MustLookup("USD")
	if s, _ = Work(v, calendar, usd, 2027, []Asset{none}); reasons(s) != "FA-0001:not_sgd" {
		t.Errorf("a USD base: %s", reasons(s))
	}
}

// The year of assessment follows the financial year the day falls in: with a first year to 31 March 2027 a day before the books start belongs to the year to 31 March 2026.
// req: FR-FA-04
func TestTheYearOfAssessmentFollowsTheFinancialYear(t *testing.T) {
	apr := period.Calendar{BooksStart: day(2026, 4, 1), FirstYearEnd: day(2027, 3, 31)}
	for _, c := range []struct {
		cal  period.Calendar
		on   time.Time
		want int
	}{
		{calendar, day(2026, 1, 1), 2027}, {calendar, day(2026, 12, 31), 2027}, {calendar, day(2027, 1, 1), 2028},
		{apr, day(2026, 4, 1), 2028}, {apr, day(2027, 3, 31), 2028}, {apr, day(2027, 4, 1), 2029},
		{apr, day(2026, 3, 31), 2027}, {apr, day(2025, 4, 1), 2027}, {apr, day(2025, 3, 31), 2026},
		{calendar, day(2025, 12, 31), 2026},
	} {
		if got := YearOfAssessment(c.cal, c.on); got != c.want {
			t.Errorf("%v with first year to %v: YA %d, want %d", c.on.Format(time.DateOnly), c.cal.FirstYearEnd.Format(time.DateOnly), got, c.want)
		}
	}
}

// The engine's schedule and the independent reference's (oracle/src/oracle/capital_allowances.py, written from IRAS's page) agree to the cent on the 120 schedules
// test/differential/capital-allowances/cases.json holds: the year of assessment of each purchase, each row's allowance, initial allowance, tax written down value brought and
// carried forward, the assets listed apart and why, and the totals.
// req: FR-FA-04
// req: QA-ENG-02
func TestTheScheduleAgreesWithTheOracleToTheCent(t *testing.T) {
	raw, err := os.ReadFile("../../../test/differential/capital-allowances/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		BooksStart   string `json:"books_start"`
		FirstYearEnd string `json:"first_year_end"`
		YA           int    `json:"ya"`
		Assets       []struct {
			ID         string  `json:"id"`
			Bought     string  `json:"bought"`
			Cost       string  `json:"cost"`
			Method     *string `json:"method"`
			Prescribed int     `json:"prescribed"`
			Years      int     `json:"years"`
			Disposed   bool    `json:"disposed"`
		} `json:"assets"`
		Result struct {
			Rows []struct {
				ID, Allowance, Initial, Brought, Carried string
			} `json:"rows"`
			Apart []struct {
				ID, Reason string
			} `json:"apart"`
			TotalAllowance string `json:"total_allowance"`
			TotalCarried   string `json:"total_carried"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 100 {
		t.Fatalf("only %d cases", len(cases))
	}
	v := version(t)
	rows := 0
	for n, c := range cases {
		books, _ := time.Parse(time.DateOnly, c.BooksStart)
		end, _ := time.Parse(time.DateOnly, c.FirstYearEnd)
		var in []Asset
		for _, a := range c.Assets {
			bought, _ := time.Parse(time.DateOnly, a.Bought)
			x := item(t, a.ID, a.Cost, bought, nil)
			if a.Method != nil {
				x.Election = &asset.TaxElection{Method: *a.Method, PrescribedYears: a.Prescribed, Years: a.Years}
			}
			x.Disposed = a.Disposed
			in = append(in, x)
		}
		got, err := Work(v, period.Calendar{BooksStart: books, FirstYearEnd: end}, sgd, c.YA, in)
		if err != nil {
			t.Fatalf("case %d: %v", n, err)
		}
		if len(got.Rows) != len(c.Result.Rows) {
			t.Fatalf("case %d (YA %d): engine has %d rows %s, reference %d", n, c.YA, len(got.Rows), figures(got), len(c.Result.Rows))
		}
		for i, w := range c.Result.Rows {
			g := got.Rows[i]
			if g.Asset.Number != w.ID || g.Allowance.String() != w.Allowance || g.Initial.String() != w.Initial || g.BroughtFw.String() != w.Brought || g.CarriedFw.String() != w.Carried {
				t.Errorf("case %d (YA %d) %s: engine %s (initial %s, from %s to %s), reference %s (initial %s, from %s to %s)", n, c.YA, w.ID, g.Allowance, g.Initial, g.BroughtFw, g.CarriedFw,
					w.Allowance, w.Initial, w.Brought, w.Carried)
			}
			rows++
		}
		var apart []string
		for _, a := range got.Apart {
			apart = append(apart, a.Asset.Number+":"+a.Reason)
		}
		var want []string
		for _, a := range c.Result.Apart {
			want = append(want, a.ID+":"+a.Reason)
		}
		slices.Sort(apart)
		slices.Sort(want)
		if !slices.Equal(apart, want) {
			t.Errorf("case %d (YA %d): engine lists apart %v, reference %v", n, c.YA, apart, want)
		}
		if got.Totals.Allowance.String() != c.Result.TotalAllowance || got.Totals.CarriedFw.String() != c.Result.TotalCarried {
			t.Errorf("case %d: totals %s/%s, reference %s/%s", n, got.Totals.Allowance, got.Totals.CarriedFw, c.Result.TotalAllowance, c.Result.TotalCarried)
		}
	}
	if rows < 80 {
		t.Errorf("only %d rows compared", rows)
	}
}
