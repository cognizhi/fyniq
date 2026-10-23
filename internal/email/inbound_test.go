package email_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/email"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A sender is authenticated when DMARC passes, or, where the domain publishes no DMARC policy, SPF or DKIM passes.
// req: FR-INT-22
func TestASenderIsAuthenticatedByTheirDomain(t *testing.T) {
	for _, c := range []struct {
		spf, dkim, dmarc string
		want             bool
	}{
		{"fail", "fail", "pass", true}, {"pass", "", "", true}, {"", "pass", "none", true}, {"pass", "pass", "fail", false},
		{"softfail", "", "", false}, {"", "", "", false},
	} {
		if got := email.Authenticated(c.spf, c.dkim, c.dmarc); got != c.want {
			t.Errorf("spf %q dkim %q dmarc %q: %v; want %v", c.spf, c.dkim, c.dmarc, got, c.want)
		}
	}
}

// The owner's rules (23 Oct 2026): a member's own address accepts their verified sign-in email; the organisation's its
// members' and contacts' emails; a decision at that address comes first, a rejection even without authentication; an
// unauthenticated sender is never accepted; anyone else is quarantined.
// req: FR-INT-22
func TestTheStandingOfASender(t *testing.T) {
	mei, ahkow := uuid.New(), uuid.New()
	members := map[uuid.UUID]string{mei: "mei@example.com", ahkow: "ahkow@example.com"}
	contacts := []string{"ar@supplier.example"}
	decisions := []storage.CaptureSender{
		{Address: "new@supplier.example", Decision: storage.SenderAccepted},
		{Address: "spam@example.net", Decision: storage.SenderRejected},
		{Address: "friend@example.net", UserID: &mei, Decision: storage.SenderAccepted},
	}
	for _, c := range []struct {
		name   string
		sender string
		user   *uuid.UUID
		auth   bool
		want   string
	}{
		{"a member at the organisation's", "Mei@Example.com", nil, true, storage.InboundAccepted},
		{"a contact at the organisation's", "ar@supplier.example", nil, true, storage.InboundAccepted},
		{"an accepted sender", "new@supplier.example", nil, true, storage.InboundAccepted},
		{"a rejected sender, unauthenticated", "spam@example.net", nil, false, storage.InboundRejected},
		{"a stranger", "x@example.org", nil, true, storage.InboundQuarantined},
		{"a contact failing DMARC", "ar@supplier.example", nil, false, storage.InboundQuarantined},
		{"a member at their own", "mei@example.com", &mei, true, storage.InboundAccepted},
		{"another member at Mei's", "ahkow@example.com", &mei, true, storage.InboundQuarantined},
		{"a contact at Mei's", "ar@supplier.example", &mei, true, storage.InboundQuarantined},
		{"Mei's accepted sender at hers", "friend@example.net", &mei, true, storage.InboundAccepted},
		{"Mei's accepted sender at the organisation's", "friend@example.net", nil, true, storage.InboundQuarantined},
	} {
		if got := email.Standing(c.sender, c.user, c.auth, decisions, members, contacts); got != c.want {
			t.Errorf("%s: %s; want %s", c.name, got, c.want)
		}
	}
}
