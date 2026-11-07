package pack_test

import (
	"errors"
	"testing"

	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/packs"
)

func deadlinesPack(t *testing.T) pack.Version {
	t.Helper()
	v, err := pack.Load(packs.FS, "sg-deadlines/2026.1")
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// req: FR-TAX-07
// req: CMP-02
// A month's last day moves to the target month's last day, as IRAS's quarters fall due (30 Jun → 31 Jul); any other
// day keeps its day, or the month's last day when the target month lacks it.
func TestDueOnFollowsMonthEnds(t *testing.T) {
	for _, c := range []struct {
		from   string
		months int
		want   string
	}{
		{"2027-03-31", 1, "2027-04-30"},
		{"2027-06-30", 1, "2027-07-31"},
		{"2027-02-28", 1, "2027-03-31"},
		{"2027-07-05", 1, "2027-08-05"},
		{"2027-01-30", 1, "2027-02-28"},
		{"2027-08-30", 6, "2028-02-29"},
		{"2026-12-31", 7, "2027-07-31"},
		{"2026-11-15", 0, "2026-11-15"},
		{"2026-11-15", 14, "2028-01-15"},
	} {
		if got := pack.AddMonths(day(t, c.from), c.months).Format("2006-01-02"); got != c.want {
			t.Errorf("%s + %d months = %s; want %s", c.from, c.months, got, c.want)
		}
	}
	v := deadlinesPack(t)
	d, err := pack.DueOn(v, "eci", day(t, "2027-03-31"), nil)
	if err != nil || d.Rule.Source != "iras-eci" || d.Rule.Area != pack.AreaTax || d.Extended || d.Rolled {
		t.Errorf("ECI for 31 Mar 2027 = %+v, %v; want its rule from iras-eci, neither extended nor rolled", d, err)
	}
	if d, err := pack.DueOn(v, "gst_f5", day(t, "2026-11-30"), nil); err != nil || !d.Extended || d.On.Format("2006-01-02") != "2027-01-09" {
		t.Errorf("F5 for November 2026 = %+v, %v; want extended to 9 Jan 2027", d, err)
	}
	if d, err := pack.DueOn(v, "cpf", day(t, "2026-10-31"), nil); err != nil || !d.Rolled {
		t.Errorf("CPF for October 2026 = %+v, %v; want rolled past the weekend", d, err)
	}
	if d, err := pack.DueOn(v, "form_cs", day(t, "2027-03-31"), nil); err != nil || d.On.Format("2006-01-02") != "2028-11-30" {
		t.Errorf("Form C-S for a year ending 31 Mar 2027 = %+v, %v; want 30 Nov 2028, counted from 31 Dec 2027", d, err)
	}
	if _, err := pack.DueOn(v, "eci", day(t, "2026-07-31"), nil); !errors.Is(err, pack.ErrNoRule) {
		t.Errorf("ECI due 31 Oct 2026, before the rules were read, gave %v; want ErrNoRule", err)
	}
	if _, err := pack.DueOn(v, "unknown", day(t, "2027-03-31"), nil); !errors.Is(err, pack.ErrNoRule) {
		t.Errorf("an unknown obligation gave %v; want ErrNoRule", err)
	}
	if _, err := pack.DueOn(v, "agm", day(t, "2026-12-31"), nil); !errors.Is(err, pack.ErrNoRule) {
		t.Errorf("an AGM without its route gave %v; want ErrNoRule: the facts must name it", err)
	}
}

// req: FR-TAX-07
// req: CMP-02
// A working day is a weekday that is no public holiday of the pack; a Sunday holiday's Monday is one.
func TestNextWorkingDaySkipsWeekendsAndHolidays(t *testing.T) {
	v := deadlinesPack(t)
	for from, want := range map[string]string{
		"2026-11-06": "2026-11-06", // a Friday
		"2026-11-08": "2026-11-10", // Deepavali on a Sunday, its Monday a holiday
		"2026-02-17": "2026-02-19", // Chinese New Year on Tuesday and Wednesday
		"2026-12-31": "2026-12-31",
		"2027-01-01": "2027-01-04", // New Year's Day on a Friday
	} {
		got, err := pack.NextWorkingDay(v, day(t, from))
		if err != nil || got.Format("2006-01-02") != want {
			t.Errorf("after %s: %v, %v; want %s", from, got, err, want)
		}
	}
	hs := pack.Holidays(v)
	if len(hs) != 26 || !hs[8].Observed || hs[8].Name != "Vesak Day" {
		t.Errorf("holidays: %d, the ninth %+v; want 26, the ninth Vesak Day observed", len(hs), hs[8])
	}
}

// req: FR-TAX-07
// req: FR-CS-03
// A day in a year without public holidays is not known: never a guess that it is a working day.
func TestAYearWithoutHolidaysIsNotAssessed(t *testing.T) {
	v := deadlinesPack(t)
	if _, err := pack.NextWorkingDay(v, day(t, "2028-01-14")); !errors.Is(err, pack.ErrNoHolidays) {
		t.Errorf("14 Jan 2028 gave %v; want ErrNoHolidays", err)
	}
	if _, err := pack.NextWorkingDay(v, day(t, "2027-12-31")); err != nil {
		t.Errorf("31 Dec 2027, a Friday of a year the pack holds: %v", err)
	}
	if got, err := pack.NextWorkingDay(v, day(t, "2027-12-25")); err != nil || got.Format("2006-01-02") != "2027-12-27" {
		t.Errorf("after Christmas 2027, a Saturday: %v, %v; want Monday 27 Dec", got, err)
	}
	if _, err := pack.NextWorkingDay(v, day(t, "2028-01-01")); !errors.Is(err, pack.ErrNoHolidays) {
		t.Errorf("1 Jan 2028 gave %v; want ErrNoHolidays", err)
	}
	if _, err := pack.DueOn(v, "cpf", day(t, "2027-12-31"), nil); !errors.Is(err, pack.ErrNoHolidays) {
		t.Errorf("CPF for December 2027 gave %v; want ErrNoHolidays", err)
	}
}

// req: FR-TAX-05
// req: ENG-30
// sg-gst@2026.3 adds the registration threshold: S$1,000,000 from 7 Nov 2026, the day it was read on IRAS's page, and
// none before, as its history is not recorded. The version before it holds no threshold.
func TestTheRegistrationThresholdIsInTheGSTPackFromItsVersion(t *testing.T) {
	versions, err := pack.LoadAll(packs.FS)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]pack.Version{}
	for _, v := range versions {
		byID[v.ID] = v
	}
	v2, v3 := byID["sg-gst@2026.2"], byID["sg-gst@2026.3"]
	if v3.Supersedes != "sg-gst@2026.2" {
		t.Fatalf("2026.3 supersedes %q; want sg-gst@2026.2", v3.Supersedes)
	}
	for _, c := range []struct {
		v          pack.Version
		on, amount string
	}{
		{v3, "2026-11-06", ""},
		{v3, "2026-11-07", "1000000.00"},
		{v3, "2031-12-31", "1000000.00"},
		{v2, "2026-11-07", ""},
	} {
		amount, cur, ok := pack.Limit(c.v, pack.LimitRegistrationThreshold, day(t, c.on))
		if amount != c.amount || ok != (c.amount != "") || (ok && cur != "SGD") {
			t.Errorf("%s on %s: got %q %q %v; want %q SGD", c.v.ID, c.on, amount, cur, ok, c.amount)
		}
	}
	if amount, _, ok := pack.Limit(v3, pack.LimitSimplifiedInvoice, day(t, "2026-11-07")); !ok || amount != "1000.00" {
		t.Errorf("2026.3 lost the simplified-invoice limit: %q %v", amount, ok)
	}
}

// req: ENG-30
// req: QA-ENG-08
// A version of sg-deadlines whose rules, holidays or extensions break the pack's rules is refused, and a wrong pinned
// case fails.
func TestADeadlinesPackKeepsItsRules(t *testing.T) {
	const dir = "sg-deadlines/2026.1/"
	cases := map[string]struct {
		edits map[string]string
		code  string
	}{
		"no rules":                {map[string]string{dir + "rules.csv": ""}, pack.CodeTable},
		"no pinned working days":  {map[string]string{dir + "pinned/working_days.csv": ""}, pack.CodeTable},
		"an unknown anchor":       {map[string]string{dir + "rules.csv": "gst_f5,tax,deadline,gst_period_end→gst_f5,tax,deadline,period_start"}, pack.CodeValue},
		"an unknown area":         {map[string]string{dir + "rules.csv": "eci,tax,→eci,taxes,"}, pack.CodeValue},
		"a fraction of a month":   {map[string]string{dir + "rules.csv": "gst_period_end,1,,0→gst_period_end,1.5,,0"}, pack.CodeValue},
		"a day 32":                {map[string]string{dir + "rules.csv": "month_end,1,14,→month_end,1,32,"}, pack.CodeValue},
		"an unknown roll":         {map[string]string{dir + "rules.csv": "next_working_day,,2026→previous_working_day,,2026"}, pack.CodeValue},
		"a malformed condition":   {map[string]string{dir + "rules.csv": "agm_route=hold→agm route"}, pack.CodeValue},
		"overlapping rules":       {map[string]string{dir + "rules.csv": "iras-gst-due\n→iras-gst-due\ngst_f5,tax,deadline,gst_period_end,2,,0,none,,2026-12-01,,Twice,iras-gst-due\n"}, pack.CodeDates},
		"a rule before":           {map[string]string{dir + "rules.csv": "none,,2026-11-07,,GST F5→none,,2020-01-01,,GST F5"}, pack.CodeDates},
		"a holiday twice":         {map[string]string{dir + "holidays.csv": "2026-01-01,New Year's Day,false,mom-holidays\n→2026-01-01,New Year's Day,false,mom-holidays\n2026-01-01,Again,false,mom-holidays\n"}, pack.CodeValue},
		"an unnamed holiday":      {map[string]string{dir + "holidays.csv": "Good Friday→ "}, pack.CodeValue},
		"an unknown extension":    {map[string]string{dir + "extensions.csv": "gst_f5,2026→gst_f8,2026"}, pack.CodeValue},
		"a backwards extension":   {map[string]string{dir + "extensions.csv": "2027-01-03,2027-01-09→2027-01-03,2027-01-02"}, pack.CodeDates},
		"an undeclared source":    {map[string]string{dir + "holidays.csv": "Good Friday,false,mom-holidays→Good Friday,false,blog"}, pack.CodeSource},
		"a [verify] tag":          {map[string]string{dir + "rules.csv": "unless waived→unless waived [verify]"}, pack.CodeVerify},
		"a wrong pinned due date": {map[string]string{dir + "pinned/due_dates.csv": ",2027-07-31,\"the Apr→,2027-07-30,\"the Apr"}, pack.CodePinned},
		"a wrong working day":     {map[string]string{dir + "pinned/working_days.csv": "2026-11-07,2026-11-10→2026-11-07,2026-11-09"}, pack.CodePinned},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			v, err := pack.Load(fixtureOf(t, "sg-deadlines/2026.1", c.edits), "sg-deadlines/2026.1")
			if c.code == pack.CodePinned {
				if err != nil || len(pack.Failed(pack.RunPinned(v))) != 1 {
					t.Fatalf("got %v and %v; want one failed pinned case", err, pack.Failed(pack.RunPinned(v)))
				}
				return
			}
			ps, ok := err.(pack.Problems)
			if !ok {
				t.Fatalf("got %v; want the version refused with %s", err, c.code)
			}
			for _, p := range ps {
				if p.Code == c.code {
					return
				}
			}
			t.Fatalf("got %v; want %s among the problems", ps, c.code)
		})
	}
	if fails := pack.Failed(pack.RunPinned(deadlinesPack(t))); len(fails) != 0 {
		t.Errorf("the shipped version fails %v", fails)
	}
}
