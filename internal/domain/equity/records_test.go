package equity

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
)

func fields(p ledger.Problems) string {
	var out []string
	for _, x := range p {
		out = append(out, x.Field)
	}
	return strings.Join(out, ",")
}

// A holder record keeps an individual's identifier as its type and last 4 characters only, never a full NRIC or FIN
// anywhere in it, and an entity's as its UEN or foreign registration number (FR-EQ-15; the owner's decision of 7 Nov
// 2026). Every problem comes back at once.
// req: FR-EQ-15
// req: CMP-PD-07
func TestAHolderRecordKeepsTheLast4Only(t *testing.T) {
	ok := HolderRecord{ContactID: uuid.New(), Kind: "individual", IDType: "nric", IDLast4: "567D", Address: "1 Raffles Place",
		ContactPeople: []ContactPerson{{Name: "Jane Lim", Email: "jane@example.com"}}, Date: time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)}
	if p := CheckHolderRecord(ok); len(p) > 0 {
		t.Fatalf("a valid record: %v", p)
	}
	for name, c := range map[string]struct {
		r    func(r *HolderRecord)
		want string
	}{
		"a full NRIC":                {func(r *HolderRecord) { r.IDLast4 = "S1234567D" }, "id_last4"},
		"a full FIN in the note":     {func(r *HolderRecord) { r.Note = "FIN G7654321K" }, "note"},
		"a type without its last 4":  {func(r *HolderRecord) { r.IDLast4 = "" }, "id_last4"},
		"a passport type misspelled": {func(r *HolderRecord) { r.IDType = "pass" }, "id_type"},
		"no kind":                    {func(r *HolderRecord) { r.Kind = "" }, "kind"},
		"an individual's UEN":        {func(r *HolderRecord) { r.Registration = "202312345K" }, "registration_number"},
		"a contact without a name":   {func(r *HolderRecord) { r.ContactPeople[0].Name = " " }, "contact_people[0].name"},
		"a contact's bad email":      {func(r *HolderRecord) { r.ContactPeople[0].Email = "jane" }, "contact_people[0].email"},
	} {
		r := ok
		r.ContactPeople = append([]ContactPerson(nil), ok.ContactPeople...)
		c.r(&r)
		if got := fields(CheckHolderRecord(r)); !strings.Contains(got, c.want) {
			t.Errorf("%s: problems on %q, want %s", name, got, c.want)
		}
	}
	entity := HolderRecord{ContactID: uuid.New(), Kind: "entity", IDType: "uen", Registration: "202312345K"}
	if p := CheckHolderRecord(entity); len(p) > 0 {
		t.Errorf("an entity with its UEN: %v", p)
	}
	entity.Registration = "12345"
	if got := fields(CheckHolderRecord(entity)); got != "registration_number" {
		t.Errorf("an entity with a UEN that is not one: %q", got)
	}
}

// A share class belongs to an active share-capital account and records its rights as plain numbers and known choices
// (FR-EQ-16); a participating cap needs a participating preference.
// req: FR-EQ-16
func TestAShareClassRecordsItsRights(t *testing.T) {
	capital, sales := uuid.New(), uuid.New()
	s := ledger.Settings{Accounts: map[uuid.UUID]ledger.Account{capital: {ID: capital, Code: "3010", Active: true, ShareCapital: true},
		sales: {ID: sales, Code: "4000", Active: true}}}
	ok := ShareClass{Name: "Series A preference", Kind: "preference", Series: "A", AccountID: capital, VotesPerShare: "1",
		Liquidation: Liquidation{Multiple: "1", Seniority: 1}, Dividend: Dividend{Kind: "non_cumulative", Rate: "8"}, ConversionRatio: "1",
		AntiDilution: "broad_based", ProRata: true}
	if p := CheckShareClass(ok, s); len(p) > 0 {
		t.Fatalf("a valid class: %v", p)
	}
	bad := ok
	bad.AccountID, bad.VotesPerShare, bad.AntiDilution, bad.Liquidation.CapMultiple = sales, "one", "weighted", "3"
	if got := fields(CheckShareClass(bad, s)); got != "account_id,votes_per_share,liquidation.cap_multiple,anti_dilution" {
		t.Errorf("a class on sales with words for numbers: %q", got)
	}
}
