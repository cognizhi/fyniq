package pack_test

import (
	"errors"
	"testing"

	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/packs"
)

func acraPack(t *testing.T) pack.Version {
	t.Helper()
	v, err := pack.Load(packs.FS, "sg-acra/2026.1")
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// req: FR-CS-02
// req: governance/company-secretary FR-CS-02 Guidance kept apart from the Act
// The 14 days for share changes, which the Act does not state, is kept as guidance with ACRA's page as its source; an
// officer's change, which s173A states, is the Act's. A row that calls guidance the Act, or the Act guidance, is
// refused, as is one that breaks the pack's other rules.
func TestTheACRAPackKeepsGuidanceApartFromTheAct(t *testing.T) {
	v := acraPack(t)
	on := day(t, "2027-01-04")
	kinds := map[string]string{}
	for _, s := range v.Sources {
		kinds[s.ID] = s.Kind
	}
	share, err := pack.PeriodOn(v, "share_change", on)
	if err != nil || share.Verdict != pack.VerdictGuidance || share.Count != 14 || share.Unit != pack.UnitDays {
		t.Fatalf("share_change: %+v, %v; want 14 days of guidance", share, err)
	}
	for _, id := range share.Sources {
		if kinds[id] != "official guidance" {
			t.Errorf("share_change cites %s, a %s; want ACRA's guidance alone", id, kinds[id])
		}
	}
	officer, err := pack.PeriodOn(v, "officer_change", on)
	if err != nil || officer.Verdict != pack.VerdictAct || officer.Section != "s173A" {
		t.Fatalf("officer_change: %+v, %v; want the Act's s173A", officer, err)
	}
	if r, ok := pack.CorporateRuleOn(v, "secretary_not_sole_director", on); !ok || r.Verdict != pack.VerdictAct || r.Section != "s171(1E)" {
		t.Fatalf("secretary_not_sole_director: %+v, %v; want the Act's s171(1E)", r, ok)
	}
	if value, unit, ok := pack.AcraThreshold(v, "audit_exempt_revenue", on); !ok || value != "10000000" || unit != pack.ThresholdSGD {
		t.Fatalf("audit_exempt_revenue: %s %s %v; want 10000000 SGD", value, unit, ok)
	}

	const dir = "sg-acra/2026.1/"
	cases := map[string]struct {
		edits map[string]string
		code  string
	}{
		"guidance called the Act":     {map[string]string{dir + "periods.csv": `"s63, s126",guidance→"s63, s126",act`}, pack.CodeSource},
		"the Act called guidance":     {map[string]string{dir + "rules.csv": `s171(1E),act,2026-11-16,,"The secretary is not the same person as the sole director","acra-officers coa-officers"→s171(1E),guidance,2026-11-16,,"The secretary is not the same person as the sole director",coa-officers`}, pack.CodeSource},
		"the Act without a section":   {map[string]string{dir + "periods.csv": "14,days,s143,act→14,days,,act"}, pack.CodeValue},
		"an unknown verdict":          {map[string]string{dir + "periods.csv": "14,days,s143,act→14,days,s143,unclear"}, pack.CodeValue},
		"an unknown unit":             {map[string]string{dir + "periods.csv": "2,business_days,s386AH→2,working_days,s386AH"}, pack.CodeValue},
		"a count beyond its unit":     {map[string]string{dir + "periods.csv": "6,months,s171(4A),act→60,months,s171(4A),act"}, pack.CodeValue},
		"a count of none":             {map[string]string{dir + "periods.csv": ",7,days,s386AL→,0,days,s386AL"}, pack.CodeValue},
		"after an unknown rule":       {map[string]string{dir + "periods.csv": "controller_register_updated,controller_register,→controller_register_updated,controller_registers,"}, pack.CodeValue},
		"a rule before the version":   {map[string]string{dir + "periods.csv": "s143,act,2026-11-16→s143,act,2026-01-01"}, pack.CodeDates},
		"a rule twice at once":        {map[string]string{dir + "periods.csv": "rule,event,after,count,unit,section,verdict,effective_from,effective_to,description,source\n→rule,event,after,count,unit,section,verdict,effective_from,effective_to,description,source\noffice_change,office_change,,30,days,s143,act,2026-12-01,,Twice,coa-officers\n"}, pack.CodeDates},
		"a count with a fraction":     {map[string]string{dir + "thresholds.csv": "audit_exempt_employees,50,→audit_exempt_employees,50.5,"}, pack.CodeValue},
		"an unknown threshold unit":   {map[string]string{dir + "thresholds.csv": "simplified_xbrl_revenue,500000,SGD→simplified_xbrl_revenue,500000,USD"}, pack.CodeValue},
		"no periods":                  {map[string]string{dir + "periods.csv": ""}, pack.CodeTable},
		"an undeclared source":        {map[string]string{dir + "rules.csv": ",acra-xbrl\n→,a-blog\n"}, pack.CodeSource},
		"a [verify] tag":              {map[string]string{dir + "rules.csv": "with the annual return→with the annual return [verify]"}, pack.CodeVerify},
		"a wrong pinned due date":     {map[string]string{dir + "pinned/periods.csv": "2027-03-02,2027-03-16,\"an officer→2027-03-02,2027-03-17,\"an officer"}, pack.CodePinned},
		"a wrong pinned business day": {map[string]string{dir + "pinned/periods.csv": "2027-04-09,2027-04-13,\"entered on Friday 9 Apr, filed centrally→2027-04-09,2027-04-11,\"entered on Friday 9 Apr, filed centrally"}, pack.CodePinned},
		"a wrong pinned threshold":    {map[string]string{dir + "pinned/thresholds.csv": "2027-01-01,50,→2027-01-01,49,"}, pack.CodePinned},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			v, err := pack.Load(fixtureOf(t, "sg-acra/2026.1", c.edits), "sg-acra/2026.1")
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
}

// req: FR-CS-02
// Two business days skip public holidays as well as weekends: entered on Tuesday 9 Mar 2027, the eve of Hari Raya
// Puasa, the central register is due on Friday 12 Mar, not Thursday 11 Mar. A year whose holidays sg-deadlines does not
// hold has no answer rather than a guess.
func TestBusinessDaysSkipPublicHolidays(t *testing.T) {
	v, cal := acraPack(t), deadlinesPack(t)
	_, due, err := pack.AcraDue(v, "controller_central", day(t, "2027-03-09"), pack.WorkdaysOf(cal))
	if err != nil || !due.Equal(day(t, "2027-03-12")) {
		t.Fatalf("got %v, %v; want 2027-03-12", due, err)
	}
	if _, due, _ := pack.AcraDue(v, "controller_central", day(t, "2027-03-09"), pack.Weekdays); !due.Equal(day(t, "2027-03-11")) {
		t.Fatalf("weekdays alone give %v; want 2027-03-11", due)
	}
	if _, _, err := pack.AcraDue(v, "nominee_central", day(t, "2031-12-30"), pack.WorkdaysOf(cal)); !errors.Is(err, pack.ErrNoHolidays) {
		t.Fatalf("got %v; want ErrNoHolidays", err)
	}
	if _, _, err := pack.AcraDue(v, "no_such_rule", day(t, "2027-03-09"), pack.Weekdays); !errors.Is(err, pack.ErrNoRule) {
		t.Fatalf("got %v; want ErrNoRule", err)
	}
}

// req: FR-CS-02
// A standing rule applies to an event before the version's first day when asked for since: a company registered on 2
// Mar 2026 appoints its secretary by 2 Sep 2026, though the rule's first day is 16 Nov 2026; asked for as on the
// event's day, the version has no rule then.
func TestAStandingRuleReachesBackToItsEvent(t *testing.T) {
	v := acraPack(t)
	if _, due, err := pack.AcraDueSince(v, "secretary_appointed", day(t, "2026-03-02"), pack.Weekdays); err != nil || !due.Equal(day(t, "2026-09-02")) {
		t.Fatalf("since: %v, %v; want 2026-09-02", due, err)
	}
	if _, _, err := pack.AcraDue(v, "secretary_appointed", day(t, "2026-03-02"), pack.Weekdays); !errors.Is(err, pack.ErrNoRule) {
		t.Fatalf("on the event's day: %v; want ErrNoRule", err)
	}
}
