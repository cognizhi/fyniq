package equity

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// Information rights are structured data a calendar task can be made from (FR-EQ-28): each of the four kinds once, due in 1 to 365 days, at most eight, and they
// survive being saved and read back.
// req: FR-EQ-28
func TestInformationRightsKeepToTheFourKindsOnceAndTheDaysAllowed(t *testing.T) {
	today := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	ok := InvestorRights{HolderID: uuid.New(), From: today.AddDate(0, -1, 0), Agreements: "Shareholders' agreement",
		InformationRights: []InformationRight{{Kind: InfoQuarterlyAccounts, Days: 45}, {Kind: InfoBoardPack, Days: 20}}}
	if probs := CheckRights(ok, today); len(probs) != 0 {
		t.Fatalf("rights as the agreements give them: %v", probs)
	}
	for name, mutate := range map[string]func(*InvestorRights){
		"an unknown kind":  func(r *InvestorRights) { r.InformationRights[0].Kind = "weekly_accounts" },
		"no days":          func(r *InvestorRights) { r.InformationRights[0].Days = 0 },
		"a year and a day": func(r *InvestorRights) { r.InformationRights[0].Days = 366 },
		"a kind twice":     func(r *InvestorRights) { r.InformationRights[1].Kind = InfoQuarterlyAccounts },
		"nine of them": func(r *InvestorRights) {
			r.InformationRights = nil
			for i := 0; i < 9; i++ {
				r.InformationRights = append(r.InformationRights, InformationRight{Kind: InfoBoardPack, Days: 5})
			}
		},
	} {
		bad := ok
		bad.InformationRights = append([]InformationRight(nil), ok.InformationRights...)
		mutate(&bad)
		probs := CheckRights(bad, today)
		if len(probs) == 0 {
			t.Errorf("%s was accepted", name)
			continue
		}
		for _, p := range probs {
			if p.Code != CodeRights {
				t.Errorf("%s: code %s", name, p.Code)
			}
		}
	}
	// Saved and read back, in order; rights without any are as they were.
	ok.Date = today
	b, err := EncodeRights(ok)
	if err != nil {
		t.Fatal(err)
	}
	if !jsonHas(b, `"information_rights":[{"kind":"quarterly_accounts","days":45},{"kind":"board_pack","days":20}]`) {
		t.Errorf("the encoding: %s", b)
	}
	ok.InformationRights = nil
	if b, _ = EncodeRights(ok); jsonHas(b, "information_rights") {
		t.Errorf("rights with none carry the key: %s", b)
	}
}

func jsonHas(b []byte, s string) bool {
	for i := 0; i+len(s) <= len(b); i++ {
		if string(b[i:i+len(s)]) == s {
			return true
		}
	}
	return false
}
