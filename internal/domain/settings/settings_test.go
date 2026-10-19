package settings

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func day(s string) time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return t
}

func code(p *Problem) string {
	if p == nil {
		return ""
	}
	return p.Code
}

func codes(ps []Problem) []string {
	var out []string
	for _, p := range ps {
		out = append(out, p.Code)
	}
	return out
}

// anchor is the anchor a Markdown heading gets: lower case, spaces as hyphens, punctuation dropped.
func anchor(heading string) string {
	return strings.ReplaceAll(regexp.MustCompile(`[^a-z0-9 -]`).ReplaceAllString(strings.ToLower(heading), ""), " ", "-")
}

// Every setting ships valid at its recommended value, in its range, with a reason and a guide section that
// exists; keys are unique, sorted (lists page by key) and fit the database's key format.
// req: FR-ORG-08
func TestTheCatalogueShipsEverySettingAtAValidRecommendedValue(t *testing.T) {
	page, err := os.ReadFile("../../../docs/user-guide/organisation-settings.md")
	if err != nil {
		t.Fatal(err)
	}
	headings := map[string]bool{}
	for _, l := range strings.Split(string(page), "\n") {
		if h, ok := strings.CutPrefix(l, "## "); ok {
			headings[anchor(h)] = true
		}
	}
	keyFormat := regexp.MustCompile(`^[a-z]+(\.[a-z_]+)+$`)
	keys := make([]string, len(Catalogue))
	for i, d := range Catalogue {
		keys[i] = d.Key
		if !keyFormat.MatchString(d.Key) || len(d.Key) > 60 {
			t.Errorf("%s: not the database's key format", d.Key)
		}
		if d.Label == "" || d.Group == "" || d.Reason == "" || d.Requirement == "" {
			t.Errorf("%s: a label, group, reason or requirement is missing", d.Key)
		}
		section, ok := strings.CutPrefix(d.Guide, "/help/organisation-settings#")
		if !ok || !headings[section] {
			t.Errorf("%s: the guide link %q has no section in the guide", d.Key, d.Guide)
		}
		if d.Fixed {
			continue
		}
		if v, p := Normalise(d, d.Recommended); p != nil || v != d.Recommended {
			t.Errorf("%s: the recommended value %q is not valid: %q %v", d.Key, d.Recommended, v, p)
		}
		if d.Kind != Switch {
			if _, p := Normalise(d, d.Min); p != nil {
				t.Errorf("%s: its minimum is out of range: %v", d.Key, p)
			}
			if _, p := Normalise(d, d.Max); p != nil {
				t.Errorf("%s: its maximum is out of range: %v", d.Key, p)
			}
		}
	}
	if !slices.IsSorted(keys) || len(slices.Compact(slices.Clone(keys))) != len(keys) {
		t.Errorf("keys are not unique and sorted: %v", keys)
	}
	if _, ok := Lookup("features.payroll"); !ok {
		t.Error("Lookup misses a setting")
	}
	if _, ok := Lookup("features.nothing"); ok {
		t.Error("Lookup found a setting that does not exist")
	}
}

// Values are read strictly by kind and range, and stored in one form.
// req: FR-ORG-08
// req: FR-ORG-04
func TestValuesAreReadByKindAndRange(t *testing.T) {
	terms, _ := Lookup("sales.payment_terms_days")
	capDoc, _ := Lookup("bank.tolerance_cap_document")
	pct, _ := Lookup("bank.tolerance_foreign_percent")
	sw, _ := Lookup("features.projects")
	region, _ := Lookup("data.region")
	for _, c := range []struct {
		d       Definition
		in, out string
	}{
		{terms, "0", "0"}, {terms, "365", "365"}, {terms, "45", "45"},
		{capDoc, "0", "0.00"}, {capDoc, "50", "50.00"}, {capDoc, "10000.00", "10000.00"}, {capDoc, "12.5", "12.50"},
		{pct, "0", "0"}, {pct, "10", "10"}, {pct, "2.5", "2.5"}, {pct, "2.75", "2.75"},
		{sw, "true", "true"}, {sw, "false", "false"},
	} {
		if v, p := Normalise(c.d, c.in); p != nil || v != c.out {
			t.Errorf("%s = %q: %q %v; want %q", c.d.Key, c.in, v, p, c.out)
		}
	}
	for _, c := range []struct {
		d  Definition
		in string
	}{
		{terms, "-1"}, {terms, "366"}, {terms, "030"}, {terms, "1.5"}, {terms, "thirty"},
		{capDoc, "-0.01"}, {capDoc, "10000.01"}, {capDoc, "1.005"}, {capDoc, "NaN"},
		{pct, "10.01"}, {pct, "2.555"}, {pct, "-1"}, {pct, "1e1"},
		{sw, "yes"}, {sw, "TRUE"},
		{region, "europe-west1"},
	} {
		if v, p := Normalise(c.d, c.in); code(p) != CodeValue || v != "" {
			t.Errorf("%s = %q: want %s, got %q %v", c.d.Key, c.in, CodeValue, v, p)
		}
	}
}

// The value on a day is the latest change in effect then; of two from the same day the later version wins;
// before any change it is the recommended value.
// req: FR-ORG-08
func TestTheValueOnADayIsTheLatestChangeInEffect(t *testing.T) {
	d, _ := Lookup("sales.payment_terms_days")
	changes := []storage.SettingChange{
		{Key: d.Key, Value: "60", EffectiveFrom: day("2026-12-01"), Version: 3},
		{Key: d.Key, Value: "14", EffectiveFrom: day("2026-11-01"), Version: 1},
		{Key: d.Key, Value: "21", EffectiveFrom: day("2026-11-01"), Version: 2},
		{Key: "purchases.payment_terms_days", Value: "7", EffectiveFrom: day("2026-01-01"), Version: 1},
	}
	for on, want := range map[string]Value{
		"2026-10-31": {Value: "30"},
		"2026-11-01": {Value: "21", Version: 2},
		"2026-11-30": {Value: "21", Version: 2},
		"2026-12-01": {Value: "60", Version: 3},
	} {
		got := On(d, changes, day(on))
		if got.Value != want.Value || got.Version != want.Version || (want.Version == 0) != (got.EffectiveFrom == nil) {
			t.Errorf("on %s: %+v; want %+v", on, got, want)
		}
	}
}

// The platform's settings never change; those under dual control (FM-1) wait for the approval engine; a
// change never takes effect in the past; and the records' retention only lengthens, now or later.
// req: FR-ORG-07
// req: FR-ORG-08
func TestChangesAreGatedNeverBackdatedAndRetentionOnlyLengthens(t *testing.T) {
	region, _ := Lookup("data.region")
	tolerance, _ := Lookup("bank.tolerance_base")
	terms, _ := Lookup("sales.payment_terms_days")
	if code(Gate(region)) != CodeFixed || code(Gate(tolerance)) != CodeDualControl || Gate(terms) != nil {
		t.Errorf("gates: region %v, tolerance %v, terms %v", Gate(region), Gate(tolerance), Gate(terms))
	}
	for _, d := range Catalogue {
		if d.DualControl && Gate(d) == nil {
			t.Errorf("%s is under dual control but can change", d.Key)
		}
	}
	today := day("2026-10-20")
	if p := Check(terms, "45", today, today, nil); p != nil {
		t.Errorf("a change from today: %v", p)
	}
	if got := codes(Check(terms, "45", day("2026-10-19"), today, nil)); !slices.Equal(got, []string{CodeBackdated}) {
		t.Errorf("a change from yesterday: %v", got)
	}

	// The records' retention, as it will be checked once the approval engine applies an approved change.
	records, _ := Lookup("retention.records_years")
	records.DualControl = false
	later := []storage.SettingChange{{Key: records.Key, Value: "10", EffectiveFrom: day("2027-01-01"), Version: 1}}
	for _, c := range []struct {
		value, from string
		changes     []storage.SettingChange
		want        []string
	}{
		{"7", "2026-10-20", nil, nil},
		{"8", "2026-10-20", nil, nil},
		{"6", "2026-10-20", nil, []string{CodeShortened}},                // below the recommended 7
		{"9", "2027-02-01", later, []string{CodeShortened}},              // below the 10 in effect then
		{"12", "2026-11-01", later, []string{CodeShortened}},             // the 10 recorded for 2027 would then shorten it
		{"10", "2026-11-01", later, nil},                                 // the same as the later change
		{"6", "2026-10-19", nil, []string{CodeBackdated, CodeShortened}}, // every rule at once
	} {
		if got := codes(Check(records, c.value, day(c.from), today, c.changes)); !slices.Equal(got, c.want) {
			t.Errorf("records' retention %s from %s: %v; want %v", c.value, c.from, got, c.want)
		}
	}
}
