//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A person without a Fyniq account who signs in with a verified email on a domain the organisation claimed, through a
// provider a Super admin lets people join by, may ask to join; the request grants nothing until an Owner approves it,
// which makes them a member with the Employee role. Either switch off, another domain, or an unverified email, and there
// is nothing to ask; one request waits at a time; an Owner may also reject; each step is in the organisation's trail.
// req: FR-ADM-08
// asvs: V6.8.1, V8.2.1
func TestJoinByDomain(t *testing.T) {
	s := startSessionsWith(t, devZoho)
	desk, phone, org := s.founders(t) // desk: Super admin and Owner
	ownerCall := func(method, path string) response { return desk.call(method, path, "same-origin", nil) }
	applicant := func(subject, email string) *browser {
		b := s.browser()
		b.email = email
		b.signInWith("local-zoho", subject, true)
		return b
	}
	offer := func(b *browser) response { return b.call(http.MethodGet, "/v1/session/join-offer", "same-origin", nil) }

	// Nothing to ask before both switches are on.
	ann := applicant("ann-zoho", "ann@cognizhi.com")
	s.problemIs(offer(ann), "getJoinOffer", 404, CodeNotFound)
	if r := ownerCall(http.MethodPut, "/v1/organisation/join-domains/Cognizhi.com"); r.Status != 200 || r.str("domain") != "cognizhi.com" {
		t.Fatalf("claiming the domain: %d %s", r.Status, r.Raw)
	}
	s.problemIs(phone.call(http.MethodPut, "/v1/organisation/join-domains/x..y", "same-origin", nil), "claimJoinDomain", 422, CodeInvalid)
	s.problemIs(offer(ann), "getJoinOffer", 404, CodeNotFound) // the provider does not let people join yet
	s.problemIs(desk.call(http.MethodPatch, "/v1/installation/sign-in-methods/local-zoho", "same-origin",
		map[string]any{"enabled": true, "allowed_domains": []string{}, "join_by_domain": true}, "If-Match", `"0"`), "changeSignInMethod", 422, CodeInvalid)
	if r := desk.call(http.MethodPatch, "/v1/installation/sign-in-methods/local-zoho", "same-origin",
		map[string]any{"enabled": true, "allowed_domains": []string{"cognizhi.com"}, "join_by_domain": true}, "If-Match", `"0"`); r.Status != 200 ||
		r.Body["join_by_domain"] != true {
		t.Fatalf("letting people join by domain: %d %s", r.Status, r.Raw)
	}

	// Another domain does not even sign in through it now; the local page, which lets nobody join by domain, offers nothing.
	bob := s.browser()
	bob.email = "bob@example.com"
	if to := bob.signInWith("local-zoho", "bob-zoho", true); to != "/?signin_error=AUTH-SIGNIN-08" {
		t.Errorf("an account on another domain signed in: %s", to)
	}
	carol := s.browser()
	carol.email = "carol@cognizhi.com"
	carol.signIn("carol-local", true)
	s.problemIs(offer(carol), "getJoinOffer", 404, CodeNotFound)

	// Ann asks; the request grants nothing; asking twice is refused.
	if o := offer(ann); o.Status != 200 || o.str("organisation_name") != "Cognizhi Pte. Ltd." || o.str("email") != "ann@cognizhi.com" {
		t.Fatalf("Ann's offer: %d %s", o.Status, o.Raw)
	}
	asked := ann.call(http.MethodPost, "/v1/session/join-requests", "same-origin", map[string]any{"display_name": "Ann Tan"})
	if asked.Status != http.StatusCreated || asked.str("status") != "pending" {
		t.Fatalf("asking: %d %s", asked.Status, asked.Raw)
	}
	s.problemIs(ann.call(http.MethodPost, "/v1/session/join-requests", "same-origin", map[string]any{"display_name": "Ann Tan"}), "requestToJoin", 409,
		storage.CodeJoin)
	if me := ann.session(); me.str("user_id") != "" {
		t.Fatalf("a pending request granted an account: %s", me.Raw)
	}

	// Dan asks too, and is rejected; Ann is approved: a member, as an Employee.
	dan := applicant("dan-zoho", "dan@cognizhi.com")
	danAsked := dan.call(http.MethodPost, "/v1/session/join-requests", "same-origin", map[string]any{"display_name": "Dan Lee"})
	list := ownerCall(http.MethodGet, "/v1/members/join-requests")
	if items, _ := list.Body["items"].([]any); list.Status != 200 || len(items) != 2 {
		t.Fatalf("the requests: %d %s", list.Status, list.Raw)
	}
	if r := ownerCall(http.MethodPost, "/v1/members/join-requests/"+danAsked.str("id")+"/reject"); r.Status != 200 || r.str("status") != "rejected" {
		t.Fatalf("rejecting: %d %s", r.Status, r.Raw)
	}
	approved := ownerCall(http.MethodPost, "/v1/members/join-requests/"+asked.str("id")+"/approve")
	if approved.Status != 200 || approved.str("status") != "approved" || approved.str("user_id") == "" {
		t.Fatalf("approving: %d %s", approved.Status, approved.Raw)
	}
	s.problemIs(ownerCall(http.MethodPost, "/v1/members/join-requests/"+asked.str("id")+"/approve"), "approveJoinRequest", 404, CodeNotFound)
	me := ann.session()
	if me.str("user_id") != approved.str("user_id") || me.str("organisation_id") != org || !strings.Contains(me.Raw, `"employee"`) {
		t.Fatalf("Ann after approval: %s", me.Raw)
	}
	if me := dan.session(); me.str("user_id") != "" {
		t.Errorf("a rejected request granted an account: %s", me.Raw)
	}

	// Releasing the domain ends the offers; the trail has each step.
	if r := ownerCall(http.MethodDelete, "/v1/organisation/join-domains/cognizhi.com"); r.Status != 200 {
		t.Fatalf("releasing: %d %s", r.Status, r.Raw)
	}
	s.problemIs(offer(applicant("eve-zoho", "eve@cognizhi.com")), "getJoinOffer", 404, CodeNotFound)
	trail := s.trailOf(t, uuid.MustParse(org))
	for _, action := range []string{"join_domain.claimed", "join_request.created", "join_request.rejected", "join_request.approved",
		"user_membership.created", "role_assignment.created", "join_domain.released"} {
		if !strings.Contains(trail, action) {
			t.Errorf("the trail has no %s", action)
		}
	}
}
