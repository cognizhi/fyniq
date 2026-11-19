package selfservice_test

import (
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/nemura/selfservice"
)

func refused(err error) string {
	if r, ok := err.(*selfservice.Refusal); ok {
		return r.Code + " " + r.Message
	}
	return ""
}

// A person changes a phone number, an email, an address and up to three emergency contacts at once, and each refusal names the field (NM-ESS-03).
// req: NM-ESS-03
func TestContactDetailsAndEmergencyContactsAreCheckedAndNamed(t *testing.T) {
	if err := selfservice.CheckContact(selfservice.Contact{Phone: "+65 9123 4567", PersonalEmail: "ben@example.com", Address: "1 Raffles Place", PostalCode: "048616"}); err != nil {
		t.Fatal(err)
	}
	if err := selfservice.CheckContact(selfservice.Contact{}); err != nil {
		t.Errorf("empty details are allowed: %v", err)
	}
	for _, tc := range []struct {
		c    selfservice.Contact
		want string
	}{
		{selfservice.Contact{Phone: "call me"}, "phone number"}, {selfservice.Contact{PersonalEmail: "ben"}, "personal email"},
		{selfservice.Contact{PersonalEmail: "Ben <ben@example.com>"}, "personal email"}, {selfservice.Contact{Address: strings.Repeat("a", 301)}, "address"},
		{selfservice.Contact{PostalCode: strings.Repeat("1", 13)}, "postal code"},
	} {
		if got := refused(selfservice.CheckContact(tc.c)); !strings.HasPrefix(got, "NM_REQUEST_INVALID") || !strings.Contains(got, tc.want) {
			t.Errorf("%+v: %q", tc.c, got)
		}
	}
	ok := selfservice.EmergencyContact{Name: "Mei Lim", Relationship: "spouse", Phone: "9123 4567"}
	if err := selfservice.CheckEmergency([]selfservice.EmergencyContact{ok, ok, ok}); err != nil {
		t.Error(err)
	}
	if got := refused(selfservice.CheckEmergency([]selfservice.EmergencyContact{ok, ok, ok, ok})); !strings.Contains(got, "up to 3") {
		t.Errorf("four: %q", got)
	}
	for _, bad := range []selfservice.EmergencyContact{{Relationship: "x", Phone: "9123 4567"}, {Name: "x", Phone: "9123 4567"}, {Name: "x", Relationship: "y", Phone: "abc"}} {
		if got := refused(selfservice.CheckEmergency([]selfservice.EmergencyContact{ok, bad})); !strings.Contains(got, "Emergency contact 2") {
			t.Errorf("%+v: %q", bad, got)
		}
	}
}

// A change that needs evidence and approval is whole: a name, or a residency with the day residence began for a permanent resident only, or an NRIC change that holds no
// number; and a note says what changed (NM-ESS-03).
// req: NM-ESS-03
func TestAChangeThatNeedsApprovalIsWholeAndHoldsNoNRIC(t *testing.T) {
	pr := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	for _, c := range []selfservice.Change{
		{Kind: selfservice.LegalName, LegalName: "Ben Lim Wei", Note: "Deed poll."},
		{Kind: selfservice.Residency, Residency: "permanent_resident", PRStart: &pr, Note: "Became a PR."},
		{Kind: selfservice.Residency, Residency: "citizen", Note: "Naturalised."},
		{Kind: selfservice.NRIC, Note: "My FIN became an NRIC."},
	} {
		if err := selfservice.CheckChange(c); err != nil {
			t.Errorf("%+v: %v", c, err)
		}
	}
	for _, tc := range []struct {
		c    selfservice.Change
		want string
	}{
		{selfservice.Change{Kind: "bank", Note: "x"}, "kind of change"}, {selfservice.Change{Kind: selfservice.LegalName, LegalName: "Ben"}, "what changed"},
		{selfservice.Change{Kind: selfservice.LegalName, Note: "x"}, "new legal name"}, {selfservice.Change{Kind: selfservice.Residency, Residency: "alien", Note: "x"}, "residency is not"},
		{selfservice.Change{Kind: selfservice.Residency, Residency: "permanent_resident", Note: "x"}, "day permanent residence began"},
		{selfservice.Change{Kind: selfservice.Residency, Residency: "citizen", PRStart: &pr, Note: "x"}, "day permanent residence began"},
		{selfservice.Change{Kind: selfservice.NRIC, LegalName: "S1234567D", Note: "x"}, "holds no number"},
	} {
		if got := refused(selfservice.CheckChange(tc.c)); !strings.HasPrefix(got, "NM_REQUEST_INVALID") || !strings.Contains(got, tc.want) {
			t.Errorf("%+v: %q", tc.c, got)
		}
	}
	if refused(selfservice.CheckQuestion(" ")) == "" || refused(selfservice.CheckQuestion(strings.Repeat("a", 1001))) == "" || selfservice.CheckQuestion("When is my review?") != nil {
		t.Error("questions")
	}
}

// Each state maps to the step a person follows: received, with HR, ready or closed (NM-ESS-04).
// req: NM-ESS-04
func TestEachStateIsOneOfFourSteps(t *testing.T) {
	for state, want := range map[string]string{"requested": "received", "received": "received", "open": "received", "checked": "with_hr", "issued": "ready", "approved": "ready",
		"answered": "ready", "replied": "ready", "refused": "closed", "withdrawn": "closed"} {
		if got := selfservice.Step("letter", state); got != want {
			t.Errorf("%s: %s, want %s", state, got, want)
		}
	}
}
