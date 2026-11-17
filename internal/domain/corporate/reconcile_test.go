package corporate

import (
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/equity"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The profile against ACRA's business profile of 1 Oct 2026 (FR-CS-01, C.2 #34, design D3). The profile has Tan Mei Lin
// a director from 2 Mar, Ah Kow the secretary from 2 Mar and Lee Wei a director who ceased on 30 Jun; the office at 1
// North Bridge Road. ACRA lists "TAN MEI LIN" from 2 Mar (the same, whatever the case), Ah Kow from 3 Mar, Lim Hui a
// director the profile does not have, and the office at 2 Science Park Drive; it does not list Lee Wei, who ceased. So:
// Ah Kow's day differs, Lim Hui is missing in Fyniq, and the office differs. With no extract, an extract with no
// officers, or one older than the last recorded change, it is not assessed and says why.
// req: FR-CS-01
// req: C.2 #34
func TestTheProfileReconciledToACRA(t *testing.T) {
	posted := func(d storage.Document) storage.Document {
		d.ID, d.Status = uuid.New(), storage.DocumentPosted
		return d
	}
	details := []equity.Recorded[Detail]{{Doc: posted(storage.Document{}), Value: Detail{From: on("2026-03-02"), Name: "Cognizhi Pte. Ltd.",
		Office: "1 North Bridge Road"}, RecordedAt: on("2026-03-05")}}
	officer := func(o Officer) []equity.Recorded[Officer] {
		return []equity.Recorded[Officer]{{Doc: posted(storage.Document{}), Value: o, RecordedAt: on("2026-07-01")}}
	}
	officers := map[uuid.UUID][]equity.Recorded[Officer]{
		uuid.New(): officer(Officer{Role: RoleDirector, Name: "Tan Mei Lin", Appointed: on("2026-03-02")}),
		uuid.New(): officer(Officer{Role: RoleSecretary, Name: "Ah Kow", Appointed: on("2026-03-02")}),
		uuid.New(): officer(Officer{Role: RoleDirector, Name: "Lee Wei", Appointed: on("2026-03-02"), Ceased: at("2026-06-30")}),
	}
	extract := &equity.Recorded[equity.AcraExtract]{Doc: posted(storage.Document{}), Value: equity.AcraExtract{ProfileOn: on("2026-10-01"),
		Office: "2 Science Park Drive", Officers: []equity.AcraOfficer{
			{Name: "TAN  MEI LIN", Role: RoleDirector, Appointed: on("2026-03-02")},
			{Name: "Ah Kow", Role: RoleSecretary, Appointed: on("2026-03-03")},
			{Name: "Lim Hui", Role: RoleDirector, Appointed: on("2026-09-01")},
		}}}
	r := ReconcileProfile(extract, details, officers)
	var got []string
	for _, d := range r.Differences {
		got = append(got, d.Kind+" "+d.Role+" "+d.Name+" "+d.Profile+" "+d.ACRA)
	}
	slices.Sort(got)
	want := []string{"appointed_differs secretary Ah Kow 2026-03-02 2026-03-03", "missing_in_fyniq director Lim Hui  2026-09-01",
		"office_differs   1 North Bridge Road 2 Science Park Drive"}
	if !r.Assessed || !slices.Equal(got, want) {
		t.Errorf("the differences: %v %q\n got %q\nwant %q", r.Assessed, r.Reason, got, want)
	}

	extract.Value.Officers = append(extract.Value.Officers[:1], equity.AcraOfficer{Name: "Ah Kow", Role: RoleSecretary, Appointed: on("2026-03-02")})
	extract.Value.Office = "1 North Bridge Road."
	if r := ReconcileProfile(extract, details, officers); !r.Assessed || len(r.Differences) != 0 {
		t.Errorf("reconciled: %+v", r)
	}
	missing := extract.Value.Officers
	extract.Value.Officers = missing[:1]
	if r := ReconcileProfile(extract, details, officers); len(r.Differences) != 1 || r.Differences[0].Kind != DiffMissingAtACRA || r.Differences[0].Name != "Ah Kow" {
		t.Errorf("Ah Kow missing at ACRA: %+v", r.Differences)
	}
	extract.Value.Officers = missing

	for what, x := range map[string]*equity.Recorded[equity.AcraExtract]{
		"no extract":  nil,
		"no officers": {Doc: extract.Doc, Value: equity.AcraExtract{ProfileOn: on("2026-10-01")}},
		"out of date": {Doc: extract.Doc, Value: equity.AcraExtract{ProfileOn: on("2026-06-01"), Officers: missing}},
	} {
		if r := ReconcileProfile(x, details, officers); r.Assessed || r.Reason == "" {
			t.Errorf("%s: want not assessed with a reason, got %+v", what, r)
		}
	}
}
