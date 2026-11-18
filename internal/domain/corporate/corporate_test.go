package corporate

import (
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/equity"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func on(s string) time.Time {
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return d
}

func at(s string) *time.Time {
	d := on(s)
	return &d
}

func hasField(probs ledger.Problems, code, field string) bool {
	return slices.ContainsFunc(probs, func(p ledger.Problem) bool { return p.Code == code && p.Field == field })
}

// What a detail or an officer must be, every problem at once (FR-CS-01, FR-CS-06, ENG-22): a five-digit SSIC code, a
// change lodged on or after it, nothing after today; a natural person unless an auditor (s145, s171), an identifier as
// its last 4 characters, the days in order, a nominee only as a director and naming whom they act for, and never an NRIC
// or FIN in full (SEC-15).
// req: FR-CS-01
// req: FR-CS-06
func TestARecordKeepsItsRules(t *testing.T) {
	today := on("2026-11-16")
	good := Detail{From: on("2026-03-02"), Name: "Cognizhi Pte. Ltd.", Office: "1 North Bridge Road", SSIC: "62011", Lodged: at("2026-03-09")}
	if p := CheckDetail(good, today); len(p) != 0 {
		t.Fatalf("a good detail: %v", p)
	}
	bad := Detail{From: on("2026-12-01"), Name: " ", Office: "Somewhere", SSIC: "6201", Lodged: at("2026-11-30"), Note: "S1234567D"}
	probs := CheckDetail(bad, today)
	for _, f := range []string{"from", "name", "ssic", "lodged_on", "note"} {
		if !hasField(probs, CodeDetail, f) {
			t.Errorf("a bad detail: want %s among %v", f, probs)
		}
	}

	member := uuid.New()
	director := Officer{Role: RoleDirector, Name: "Tan Mei Lin", Kind: "individual", IDType: "nric", IDLast4: "567A", Appointed: on("2026-03-02"),
		AppointmentLodged: at("2026-03-09"), Member: &member}
	auditor := Officer{Role: RoleAuditor, Name: "Ledger & Co LLP", Kind: "entity", IDType: "uen", Registration: "T08LL1234A", Appointed: on("2026-05-01")}
	nominee := Officer{Role: RoleDirector, Name: "Lee Wei", Kind: "individual", Appointed: on("2026-04-01"), Nominee: true, Nominator: "Lion Rock Ventures"}
	for _, o := range []Officer{director, auditor, nominee} {
		if p := CheckOfficer(o, today); len(p) != 0 {
			t.Errorf("%s: %v", o.Name, p)
		}
	}
	cases := []struct {
		what  string
		edit  func(o *Officer)
		field string
	}{
		{"an unknown role", func(o *Officer) { o.Role = "chairman" }, "role"},
		{"a company as a director", func(o *Officer) { o.Kind, o.IDType, o.IDLast4 = "entity", "", "" }, "kind"},
		{"an identifier longer than 4", func(o *Officer) { o.IDLast4 = "4567A" }, "id_last4"},
		{"an identifier with no type", func(o *Officer) { o.IDType = "" }, "id_last4"},
		{"ceased before appointed", func(o *Officer) { o.Ceased = at("2026-03-01") }, "ceased_on"},
		{"lodged before appointed", func(o *Officer) { o.AppointmentLodged = at("2026-03-01") }, "appointment_lodged_on"},
		{"a cessation lodged with no cessation", func(o *Officer) { o.CessationLodged = at("2026-04-01") }, "cessation_lodged_on"},
		{"appointed after today", func(o *Officer) { o.Appointed, o.AppointmentLodged = on("2026-12-01"), nil }, "appointed_on"},
		{"a nominee secretary", func(o *Officer) { o.Role, o.Nominee, o.Nominator = RoleSecretary, true, "Lion Rock" }, "nominee"},
		{"a nominee for no one", func(o *Officer) { o.Nominee = true }, "nominator"},
		{"a nominator with no nominee", func(o *Officer) { o.Nominator = "Lion Rock" }, "nominator"},
		{"a full NRIC", func(o *Officer) { o.Note = "Her NRIC is S1234567D" }, "note"},
		{"no name", func(o *Officer) { o.Name = "" }, "name"},
	}
	for _, c := range cases {
		o := director
		c.edit(&o)
		if probs := CheckOfficer(o, today); !hasField(probs, CodeOfficer, c.field) {
			t.Errorf("%s: want a problem with %s, got %v", c.what, c.field, probs)
		}
	}
}

// The profile on a day (FR-CS-01): the detail in force is the latest recorded to take effect by then, a correction of the
// same day winning; an officer is in office from the day appointed until the day they ceased, by their version in force;
// drafts count for nothing.
// req: FR-CS-01
func TestTheProfileOnADay(t *testing.T) {
	rec := func(id uuid.UUID, status storage.DocumentStatus, recorded string, _ any) storage.Document {
		d := storage.Document{ID: id, Status: status}
		if recorded != "" {
			r := on(recorded)
			d.PostedAt = &r
		}
		return d
	}
	first, office, fix, draft := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	details := []equity.Recorded[Detail]{
		{Doc: rec(first, storage.DocumentPosted, "2026-03-05", nil), Value: Detail{From: on("2026-03-02"), Office: "First"}, RecordedAt: on("2026-03-05")},
		{Doc: rec(office, storage.DocumentPosted, "2026-08-05", nil), Value: Detail{From: on("2026-08-01"), Office: "Second"}, RecordedAt: on("2026-08-05")},
		{Doc: rec(fix, storage.DocumentPosted, "2026-08-20", nil), Value: Detail{From: on("2026-08-01"), Office: "Second, corrected"}, RecordedAt: on("2026-08-20")},
		{Doc: rec(draft, storage.DocumentDraft, "", nil), Value: Detail{From: on("2026-09-01"), Office: "Draft"}},
	}
	for day, want := range map[string]string{"2026-03-01": "", "2026-03-02": "First", "2026-07-31": "First", "2026-08-01": "Second, corrected",
		"2026-10-01": "Second, corrected"} {
		got := ""
		if d := DetailOn(details, on(day)); d != nil {
			got = d.Value.Office
		}
		if got != want {
			t.Errorf("the detail on %s: %q, want %q", day, got, want)
		}
	}

	tan, kow := uuid.New(), uuid.New()
	tanCeased := uuid.New()
	officers := map[uuid.UUID][]equity.Recorded[Officer]{
		tan: {
			{Doc: rec(tan, storage.DocumentPosted, "2026-03-05", nil), Value: Officer{Role: RoleDirector, Name: "Tan Mei Lin", Appointed: on("2026-03-02")},
				RecordedAt: on("2026-03-05")},
			{Doc: rec(tanCeased, storage.DocumentPosted, "2026-10-02", nil), Value: Officer{ID: tan, Role: RoleDirector, Name: "Tan Mei Lin",
				Appointed: on("2026-03-02"), Ceased: at("2026-09-30")}, RecordedAt: on("2026-10-02")},
		},
		kow: {
			{Doc: rec(kow, storage.DocumentPosted, "2026-03-05", nil), Value: Officer{Role: RoleSecretary, Name: "Ah Kow", Appointed: on("2026-03-02")},
				RecordedAt: on("2026-03-05")},
		},
	}
	names := func(day string) []string {
		var out []string
		for _, o := range OfficersOn(officers, on(day)) {
			out = append(out, o.Value.Role+" "+o.Value.Name)
		}
		return out
	}
	if got := names("2026-06-01"); !slices.Equal(got, []string{"director Tan Mei Lin", "secretary Ah Kow"}) {
		t.Errorf("in office on 1 Jun: %v", got)
	}
	if got := names("2026-09-30"); !slices.Equal(got, []string{"secretary Ah Kow"}) {
		t.Errorf("in office on 30 Sep, the day she ceased: %v", got)
	}
	if got := names("2026-03-01"); len(got) != 0 {
		t.Errorf("in office before incorporation: %v", got)
	}
}
