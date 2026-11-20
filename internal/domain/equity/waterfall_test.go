package equity

import (
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

var sgd = money.MustLookup("SGD")

func sg(t *testing.T, s string) money.Money {
	t.Helper()
	m, err := money.Parse(s, sgd)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func holders(t *testing.T, hs ...any) []WaterfallHolder {
	var out []WaterfallHolder
	for i := 0; i < len(hs); i += 3 {
		out = append(out, WaterfallHolder{ID: hs[i].(string), Shares: hs[i+1].(int64), Invested: sg(t, hs[i+2].(string))})
	}
	return out
}

func totals(r WaterfallResult) map[string]string {
	out := map[string]string{}
	for _, c := range r.Classes {
		out[c.ID] = c.Total.String()
	}
	return out
}

// Worked by hand, SGD. Founders hold 8,000,000 ordinary shares (6,000,000 and 2,000,000); Series A holds 2,000,000 shares for S$2,000,000, a 1x non-participating
// preference, split 1,500,000 and 500,000 between its two investors.
//   - Exit 5,000,000: as converted A would take 2/10 of it, 1,000,000, less than its preference of 2,000,000, so it takes the preference; the other 3,000,000 goes
//     to the ordinary shares (2,250,000 and 750,000).
//   - Exit 20,000,000: as converted A takes 4,000,000, more than 2,000,000, so it converts: 4,000,000 and 16,000,000.
//   - Exit 1,000,000: the preference takes all of it.
//   - Exit 10,000,000: as converted 2,000,000 equals the preference of 2,000,000: it does not convert (only a strictly better choice converts).
//
// req: FR-EQ-29
// req: QA-ENG-02
func TestAnExitPaysTheNonParticipatingPreferenceOrTheConversionWhicheverIsMore(t *testing.T) {
	classes := func() []WaterfallClass {
		return []WaterfallClass{
			{ID: "ord", Name: "Ordinary", Holders: holders(t, "f1", int64(6000000), "0.00", "f2", int64(2000000), "0.00")},
			{ID: "a", Name: "Series A", Preference: true, Multiple: "1", Seniority: 1, Holders: holders(t, "x", int64(1500000), "1500000.00", "y", int64(500000), "500000.00")},
		}
	}
	for exit, want := range map[string]map[string]string{
		"5000000":  {"ord": "3000000.00", "a": "2000000.00"},
		"20000000": {"ord": "16000000.00", "a": "4000000.00"},
		"1000000":  {"ord": "0.00", "a": "1000000.00"},
		"10000000": {"ord": "8000000.00", "a": "2000000.00"},
	} {
		r, err := ModelWaterfall(sg(t, exit), classes())
		if err != nil {
			t.Fatal(err)
		}
		got := totals(r)
		if got["ord"] != want["ord"] || got["a"] != want["a"] {
			t.Errorf("exit %s: %v, want %v", exit, got, want)
		}
	}
	r, _ := ModelWaterfall(sg(t, "5000000"), classes())
	if r.Classes[0].Holders[0].Total.String() != "2250000.00" || r.Classes[0].Holders[1].Total.String() != "750000.00" ||
		r.Classes[1].Holders[0].Total.String() != "1500000.00" || r.Classes[1].Holders[1].Total.String() != "500000.00" || r.Classes[1].Converts {
		t.Errorf("the holders at 5,000,000: %+v", r)
	}
	// The as-converted split is each share the same: Series A would take 1,000,000 at 5,000,000.
	if r.AsConverted[1].Total.String() != "1000000.00" || r.AsConverted[0].Total.String() != "4000000.00" {
		t.Errorf("as converted: %+v", r.AsConverted)
	}
	if r, _ := ModelWaterfall(sg(t, "20000000"), classes()); !r.Classes[1].Converts {
		t.Error("Series A did not convert at 20,000,000")
	}
}

// An uncapped participating preference takes its preference and then shares the rest as converted: Series B holds 1,000,000 shares for S$1,000,000 (1x,
// participating) beside 9,000,000 ordinary shares. Exit 10,000,000: B takes 1,000,000, and of the other 9,000,000 its tenth, 900,000: 1,900,000 in all, and
// the ordinary shares 8,100,000. Two classes of one seniority share the preferences pari passu: A (2,000,000) and B (3,000,000) at 4,000,000 get 1,600,000 and
// 2,400,000; of different seniority, the senior is paid in full first: A 2,000,000 then B 2,000,000 of its 3,000,000.
// req: FR-EQ-29
func TestParticipationSeniorityAndPariPassu(t *testing.T) {
	r, err := ModelWaterfall(sg(t, "10000000"), []WaterfallClass{
		{ID: "ord", Name: "Ordinary", Holders: holders(t, "f", int64(9000000), "0.00")},
		{ID: "b", Name: "Series B", Preference: true, Participating: true, Multiple: "1", Seniority: 1, Holders: holders(t, "i", int64(1000000), "1000000.00")},
	})
	if err != nil || totals(r)["b"] != "1900000.00" || totals(r)["ord"] != "8100000.00" || r.Classes[1].Preference.String() != "1000000.00" || r.Classes[1].Residual.String() != "900000.00" {
		t.Fatalf("participating: %v %v", totals(r), err)
	}
	pair := func(sa, sb int) []WaterfallClass {
		return []WaterfallClass{
			{ID: "ord", Name: "Ordinary", Holders: holders(t, "f", int64(10000000), "0.00")},
			{ID: "a", Name: "A", Preference: true, Multiple: "1", Seniority: sa, Holders: holders(t, "x", int64(2000000), "2000000.00")},
			{ID: "b", Name: "B", Preference: true, Multiple: "1", Seniority: sb, Holders: holders(t, "y", int64(3000000), "3000000.00")},
		}
	}
	if r, err = ModelWaterfall(sg(t, "4000000"), pair(1, 1)); err != nil || totals(r)["a"] != "1600000.00" || totals(r)["b"] != "2400000.00" || totals(r)["ord"] != "0.00" {
		t.Errorf("pari passu: %v %v", totals(r), err)
	}
	if r, err = ModelWaterfall(sg(t, "4000000"), pair(1, 2)); err != nil || totals(r)["a"] != "2000000.00" || totals(r)["b"] != "2000000.00" || totals(r)["ord"] != "0.00" {
		t.Errorf("seniority: %v %v", totals(r), err)
	}
	// With the money to pay both in full, the rest is the ordinary shares', and neither converts: 6,000,000 = 2M + 3M + 1M.
	if r, err = ModelWaterfall(sg(t, "6000000"), pair(1, 2)); err != nil || totals(r)["ord"] != "1000000.00" || r.Classes[1].Converts || r.Classes[2].Converts {
		t.Errorf("enough for both: %v %v", totals(r), err)
	}
	// A multiple of 1.5 and a holder split with a remainder: 1,000.00 shared 1/3 and 2/3 of three shares is 333.33 and 666.67 (largest remainder, ALLOC-PRORATA).
	r, err = ModelWaterfall(sg(t, "1000"), []WaterfallClass{{ID: "ord", Name: "Ordinary", Holders: holders(t, "p", int64(1), "0.00", "q", int64(2), "0.00")}})
	if err != nil || r.Classes[0].Holders[0].Total.String() != "333.33" || r.Classes[0].Holders[1].Total.String() != "666.67" {
		t.Errorf("remainder: %+v %v", r, err)
	}
}

// A structure that is not confirmed is refused by name, never approximated, and the proceeds always add up to the exit value.
// req: FR-EQ-29
func TestAnUnconfirmedStructureIsRefused(t *testing.T) {
	_, err := ModelWaterfall(sg(t, "1000"), []WaterfallClass{
		{ID: "ord", Name: "Ordinary", Holders: holders(t, "f", int64(10), "0.00")},
		{ID: "c", Name: "Series C", Preference: true, Participating: true, Capped: true, Multiple: "1", Seniority: 1, Holders: holders(t, "i", int64(5), "500.00")},
	})
	if err == nil || !strings.Contains(err.Error(), "capped participating") || !strings.Contains(err.Error(), "Series C") {
		t.Errorf("a capped participating preference: %v", err)
	}
	if _, err = ModelWaterfall(sg(t, "-1"), nil); err == nil {
		t.Error("an exit below zero")
	}
	if _, err = ModelWaterfall(sg(t, "100"), []WaterfallClass{{ID: "ord", Name: "Ordinary"}}); err == nil {
		t.Error("a class with no shares")
	}
}
