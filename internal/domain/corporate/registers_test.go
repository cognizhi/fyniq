package corporate

import (
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/equity"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// What a nominee entry and a statements approval must be, every problem at once (s386AL, FR-CS-03, ENG-22): a director
// by their officer or a shareholder by their contact, whom they act for, the days on or after they became one and ACRA
// told after the company's register; statements approved after their year ends and sent on or after approval.
// req: FR-CS-01
// req: FR-CS-03
func TestARegisterRecordKeepsItsRules(t *testing.T) {
	today := on("2026-11-16")
	holder, officer := uuid.New(), uuid.New()
	good := NomineeEntry{Kind: NomineeShareholder, Holder: &holder, Nominator: "Lion Rock Fund I", Since: on("2026-06-18"), Informed: at("2026-06-25"),
		Register: at("2026-06-30"), Central: at("2026-07-02")}
	if p := CheckNominee(good, today); len(p) != 0 {
		t.Fatalf("a good entry: %v", p)
	}
	cases := []struct {
		what  string
		edit  func(n *NomineeEntry)
		field string
	}{
		{"an unknown kind", func(n *NomineeEntry) { n.Kind = "secretary" }, "kind"},
		{"a director with no officer", func(n *NomineeEntry) { n.Kind = NomineeDirector }, "officer_id"},
		{"a shareholder named by an officer", func(n *NomineeEntry) { n.Holder, n.Officer = nil, &officer }, "holder_id"},
		{"for no one", func(n *NomineeEntry) { n.Nominator = " " }, "nominator"},
		{"informed before", func(n *NomineeEntry) { n.Informed = at("2026-06-17") }, "informed_on"},
		{"entered before", func(n *NomineeEntry) { n.Register = at("2026-06-17") }, "register_on"},
		{"ACRA told before the register", func(n *NomineeEntry) { n.Central = at("2026-06-29") }, "central_on"},
		{"ACRA told with no register", func(n *NomineeEntry) { n.Register = nil }, "central_on"},
		{"ceased before", func(n *NomineeEntry) { n.Ceased = at("2026-06-01") }, "ceased_on"},
		{"a nominee after today", func(n *NomineeEntry) { n.Since, n.Informed, n.Register, n.Central = on("2026-12-01"), nil, nil, nil }, "since"},
		{"a full NRIC", func(n *NomineeEntry) { n.Nominator = "S1234567D" }, "note"},
	}
	for _, c := range cases {
		n := good
		c.edit(&n)
		if probs := CheckNominee(n, today); !hasField(probs, CodeNominee, c.field) {
			t.Errorf("%s: want a problem with %s, got %v", c.what, c.field, probs)
		}
	}
	approved := StatementsApproval{FYEnd: on("2025-12-31"), Approved: on("2026-04-15"), Sent: at("2026-04-20")}
	if p := CheckStatementsApproval(approved, today); len(p) != 0 {
		t.Fatalf("a good approval: %v", p)
	}
	for what, a := range map[string]StatementsApproval{
		"approved_on": {FYEnd: on("2025-12-31"), Approved: on("2025-12-31")},
		"sent_on":     {FYEnd: on("2025-12-31"), Approved: on("2026-04-15"), Sent: at("2026-04-14")},
		"fy_end":      {Approved: on("2026-04-15")},
	} {
		if probs := CheckStatementsApproval(a, today); !hasField(probs, CodeApproval, what) {
			t.Errorf("an approval wrong in %s: %v", what, probs)
		}
	}
	if probs := CheckStatementsApproval(StatementsApproval{FYEnd: on("2026-06-30"), Approved: on("2026-12-01")}, today); !hasField(probs, CodeApproval, "approved_on") {
		t.Errorf("an approval after today: %v", probs)
	}
}

// The registers on a day (s386AL): an entry is in force from the day they became a nominee until they ceased, by its
// version in force; a year's approval in force is the last recorded for it.
// req: FR-CS-01
// req: FR-CS-03
func TestTheRegistersOnADay(t *testing.T) {
	doc := func(id uuid.UUID, status storage.DocumentStatus) storage.Document {
		return storage.Document{ID: id, Status: status}
	}
	first, ceased := uuid.New(), uuid.New()
	entries := map[uuid.UUID][]equity.Recorded[NomineeEntry]{first: {
		{Doc: doc(first, storage.DocumentPosted), Value: NomineeEntry{Kind: NomineeShareholder, Nominator: "Fund", Since: on("2026-06-18")}, RecordedAt: on("2026-07-01")},
		{Doc: doc(ceased, storage.DocumentPosted), Value: NomineeEntry{ID: first, Kind: NomineeShareholder, Nominator: "Fund", Since: on("2026-06-18"),
			Ceased: at("2026-10-01")}, RecordedAt: on("2026-10-02")},
	}}
	for day, want := range map[string]int{"2026-06-17": 0, "2026-06-18": 1, "2026-09-30": 1, "2026-10-01": 0} {
		if got := len(NomineesOn(entries, on(day))); got != want {
			t.Errorf("entries in force on %s: %d, want %d", day, got, want)
		}
	}
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	approvals := []equity.Recorded[StatementsApproval]{
		{Doc: doc(a, storage.DocumentPosted), Value: StatementsApproval{FYEnd: on("2025-12-31"), Approved: on("2026-04-15")}, RecordedAt: on("2026-04-16")},
		{Doc: doc(b, storage.DocumentPosted), Value: StatementsApproval{FYEnd: on("2025-12-31"), Approved: on("2026-04-15"), Sent: at("2026-04-20")},
			RecordedAt: on("2026-04-21")},
		{Doc: doc(c, storage.DocumentDraft), Value: StatementsApproval{FYEnd: on("2024-12-31"), Approved: on("2025-05-01")}},
	}
	if got := ApprovalFor(approvals, on("2025-12-31")); got == nil || got.Doc.ID != b {
		t.Errorf("the approval for 2025: %+v", got)
	}
	if got := ApprovalFor(approvals, on("2024-12-31")); got != nil {
		t.Errorf("a draft counts for nothing: %+v", got)
	}
}
