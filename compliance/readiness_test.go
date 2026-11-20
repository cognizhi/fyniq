package compliance

import (
	"slices"
	"testing"
)

// The Annex A numbers a mapping names, ranges spelled out (CMP-08).
// req: CMP-08
func TestTheISOClausesAMappingNames(t *testing.T) {
	if len(AnnexA) != 93 || AnnexA[0] != "5.1" || AnnexA[36] != "5.37" || AnnexA[37] != "6.1" || AnnexA[92] != "8.34" {
		t.Fatalf("Annex A: %d numbers, %v ...", len(AnnexA), AnnexA[:3])
	}
	for _, c := range []struct {
		mapping string
		want    []string
	}{
		{"ISO 8.29; ASVS", []string{"8.29"}},
		{"ISO 5.33, 8.15; SOC2 CC7.2", []string{"5.33", "8.15"}},
		{"ISO 5.24–5.28; SOC2 CC7.3", []string{"5.24", "5.25", "5.26", "5.27", "5.28"}},
		{"ISO 5.15–5.18, 8.5", []string{"5.15", "5.16", "5.17", "5.18", "8.5"}},
		{"COSO P10; SOC2 CC6.1", nil},
		{"SOC2 CC8.1; ISO 8.31", []string{"8.31"}},
		{"PDPA (PDPC/CSA advisory); ISO 5.23", []string{"5.23"}},
	} {
		if got := ISOClauses(c.mapping); !slices.Equal(got, c.want) {
			t.Errorf("%q: %v, want %v", c.mapping, got, c.want)
		}
	}
}

// Every Annex A number a control names is one of Annex A's 93, and every framework has a stage in words.
// req: CMP-08
func TestEveryISOClauseAControlNamesIsAnnexAs(t *testing.T) {
	controls, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	covered := map[string]bool{}
	for _, c := range controls {
		for _, n := range ISOClauses(c.Mapping) {
			covered[n] = true
		}
	}
	if len(covered) == 0 || len(covered) >= len(AnnexA) {
		t.Errorf("the registry covers %d of %d Annex A numbers: more than none and not all, as a readiness view shows gaps", len(covered), len(AnnexA))
	}
	for _, f := range Frameworks {
		if FrameworkStages[f.Key] == "" {
			t.Errorf("framework %s has no stage", f.Key)
		}
	}
}
