//go:build local

package api

import (
	"context"
	"encoding/base64"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/accessreview"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/member"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// reviewMember is a member as the access review lists them, by name.
func reviewMember(r response, name string) map[string]any {
	for _, x := range r.Body["members"].([]any) {
		if m := x.(map[string]any); m["display_name"] == name {
			return m
		}
	}
	return nil
}

// The access review shows who can do what, live, to those who view "Users, roles, team mode"; an Owner attests the
// latest quarter that has ended, once, after step-up and on the version they read; the attestation answers for the
// quarters before it, so C.2 #7 stops warning; the CSV export is audited. Memberships carry the database's own time,
// so the quarters are those of 2099 and 2100, after any day this test runs on.
// req: FR-CTL-06
// req: SEC-06
// req: C.2 #7
// asvs: V8.2.1, V16.3.2
func TestTheAccessReview(t *testing.T) {
	f := newTrio(t)
	e := f.e
	read := func(who string) response {
		r := e.call(http.MethodGet, "/v1/access-review", who, nil)
		if r.Status != http.StatusOK {
			t.Fatalf("getAccessReview: %d %s", r.Status, r.Raw)
		}
		return r
	}

	// Every founder administers users and approves transactions: SOD-6, which Founder mode allows.
	r := read(f.as(f.ann))
	ann := reviewMember(r, "Ann")
	if r.Body["team_mode"] != "founder" || len(r.Body["members"].([]any)) != 3 || ann == nil || ann["external"] != false ||
		ann["second_factor"] != "none" || !strings.Contains(r.Raw, `"conflicts":[{"sod":"SOD-6","roles":["controller","owner","administrator"],"covered":"founder_mode"}]`) ||
		r.Body["to_attest"] != nil || r.Body["overdue"] != nil || r.Header.Get("ETag") == "" {
		t.Fatalf("the review: %s", r.Raw)
	}
	for _, role := range []authz.Role{authz.Controller, authz.Auditor, authz.DPO, authz.Administrator} {
		if g := e.call(http.MethodGet, "/v1/access-review", e.as(role), nil); g.Status != http.StatusOK {
			t.Errorf("%s reading the review: %d", role, g.Status)
		}
	}
	for _, role := range []authz.Role{authz.Bookkeeper, authz.Accountant, authz.AI} {
		if g := e.call(http.MethodGet, "/v1/access-review", e.as(role), nil); g.Status != http.StatusForbidden {
			t.Errorf("%s reading the review: %d, want 403", role, g.Status)
		}
	}

	// Into January 2100: the fourth quarter of 2099 is to attest, and the third is overdue.
	e.clock.Set(time.Date(2100, 1, 5, 9, 0, 0, 0, time.UTC))
	ann1, ben := f.as(f.ann), f.as(f.ben)
	r = read(ann1)
	etag := r.Header.Get("ETag")
	if !strings.Contains(r.Raw, `"to_attest":{"quarter":"2099-10-01","label":"Q4 2099","due_by":"2100-01-14"}`) ||
		!strings.Contains(r.Raw, `"overdue":{"quarter":"2099-07-01","label":"Q3 2099","due_by":"2099-10-14"}`) || reviewMember(r, "Ann")["dormant"] != true {
		t.Fatalf("the quarters: %s", r.Raw)
	}
	wantStatus(t, e.checked("controls.access-review-overdue"), "warning")
	if c := e.checked("controls.access-review-overdue"); c.Body["reviewed_in"] != "access_review" || itemNamed(c, "2099-07-01") == nil {
		t.Fatalf("C.2 #7: %s", c.Raw)
	}
	// It is attested, never marked reviewed on Checks.
	e.problemIs(e.call(http.MethodPost, "/v1/checks/controls.access-review-overdue/reviews", e.as(authz.Controller),
		map[string]any{"kind": "access_review", "id": "2099-07-01"}), "reviewCheckItem", 422, CodeNotToReview)

	attest := func(who, quarter, etag string, headers ...string) response {
		return e.call(http.MethodPost, "/v1/access-review/attestations", who, map[string]any{"quarter": quarter, "note": "  Ended nobody's access.  "},
			append([]string{"If-Match", etag}, headers...)...)
	}
	// The refusals: a Controller, no step-up, no If-Match, a stale version, the wrong quarter, not a quarter's first day.
	if x := attest(e.as(authz.Controller), "2099-10-01", etag); x.Status != http.StatusForbidden {
		t.Errorf("a Controller attesting: %d", x.Status)
	}
	stale := e.principal(authn.Principal{Subject: "founder-" + f.ann, UserID: uuid.MustParse(f.ann), Client: "web", Class: authn.ClassUser,
		Organisation: e.books.Org, Roles: []authz.Role{authz.Owner}, StepUpAt: e.clock.Now().Add(-time.Hour)})
	e.problemIs(attest(stale, "2099-10-01", etag), "attestAccessReview", 403, CodeStepUpRequired)
	e.problemIs(e.call(http.MethodPost, "/v1/access-review/attestations", ann1, map[string]any{"quarter": "2099-10-01"}), "attestAccessReview", 428, CodeIfMatchRequired)
	e.problemIs(attest(ann1, "2099-10-01", `"stale"`), "attestAccessReview", 412, CodeStale)
	e.problemIs(attest(ann1, "2099-07-01", etag), "attestAccessReview", 409, accessreview.CodeAttest)
	e.problemIs(attest(ann1, "2099-10-02", etag), "attestAccessReview", 422, CodeInvalid)
	if p := attest(ann1, "2099-10-01", etag, "Prefer", "return=preview"); p.Status != http.StatusOK || p.Header.Get("Preference-Applied") != "return=preview" {
		t.Fatalf("a preview: %d %s", p.Status, p.Raw)
	}
	if read(ben).Body["to_attest"] == nil {
		t.Fatal("a preview attested the quarter")
	}

	// Ann attests the fourth quarter; it answers for the third too.
	key := uuid.NewString()
	ok := attest(ann1, "2099-10-01", etag, "Idempotency-Key", key)
	if ok.Status != http.StatusOK || ok.Body["to_attest"] != nil || ok.Body["overdue"] != nil || ok.Header.Get("ETag") == "" {
		t.Fatalf("attesting: %d %s", ok.Status, ok.Raw)
	}
	got := ok.Body["attestations"].([]any)[0].(map[string]any)
	if got["attested_by"] != "user:"+f.ann || got["attested_by_name"] != "Ann" || got["note"] != "Ended nobody's access." ||
		!regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(got["report_sha256"].(string)) || !strings.Contains(ok.Raw, `"label":"Q4 2099"`) {
		t.Fatalf("the attestation: %v", got)
	}
	if again := attest(ann1, "2099-10-01", etag, "Idempotency-Key", key); again.Status != http.StatusOK || again.Header.Get("Idempotent-Replayed") != "true" {
		t.Fatalf("a replay: %d %s", again.Status, again.Raw)
	}
	e.problemIs(attest(ben, "2099-10-01", ok.Header.Get("ETag")), "attestAccessReview", 409, accessreview.CodeAttest)
	wantStatus(t, e.checked("controls.access-review-overdue"), "ok")

	// What was attested is kept as the database hashed it, with its audit event.
	e.fixture(func(ctx context.Context, u storage.UnitOfWork) error {
		as, err := u.AccessReviews().List(ctx)
		if err != nil || len(as) != 1 || !strings.Contains(as[0].Report, `"display_name":"Ben"`) || !strings.Contains(as[0].Report, `"to_attest":{"quarter":"2099-10-01"`) {
			t.Fatalf("the attestation kept: %v %v", as, err)
		}
		return nil
	})
	if !strings.Contains(auditTrail(t, e), `"access_review.attested"`) {
		t.Error("the attestation has no audit event")
	}

	// The export: one row per member, nothing a spreadsheet would evaluate, and its audit event.
	x := e.call(http.MethodGet, "/v1/access-review/export", e.as(authz.Auditor), nil)
	if x.Status != http.StatusOK || x.Body["media_type"] != "text/csv; charset=utf-8" || x.Body["rows"] != float64(3) {
		t.Fatalf("the export: %d %s", x.Status, x.Raw)
	}
	csv, _ := base64.StdEncoding.DecodeString(x.Body["content_base64"].(string))
	if !strings.HasPrefix(string(csv), `"Kind","Name","Roles"`) || !strings.Contains(string(csv), `"member","Ann","administrator; controller; owner"`) {
		t.Fatalf("the CSV: %s", csv)
	}
	if trail := auditTrail(t, e); !strings.Contains(trail, `"access_review.exported"`) || !strings.Contains(trail, x.Body["sha256"].(string)) {
		t.Error("the export has no audit event with its SHA-256")
	}
}

// A member is removed: alone during the founder bootstrap, by an approved request after it; never the last Owner. An
// external user whose roles have all ended is still a member (C.2 #6, ✖) until they are removed or given a role again
// with a new end date, which ends the expired assignment first.
// req: FR-ADM-01
// req: SEC-06
// req: C.2 #6
// asvs: V7.4.2, V8.2.1, V16.3.2
func TestRemovingAMember(t *testing.T) {
	// During the bootstrap the Owner acts alone, but never removes the last Owner.
	{
		e := newEnv(t)
		code := e.call(http.MethodPost, "/v1/members/invitations", e.as(authz.Owner), map[string]any{"display_name": "Olive", "roles": []string{"owner"}}).str("code")
		olive := e.call(http.MethodPost, "/v1/members", e.principal(authn.Principal{Subject: "olive", Client: "web"}),
			map[string]any{"invitation_code": code, "display_name": "Olive"}).str("member_id")
		e.problemIs(e.call(http.MethodDelete, "/v1/members/"+olive, e.as(authz.Owner), nil), "removeMember", 409, member.CodeLastOwner)
		e.problemIs(e.call(http.MethodDelete, "/v1/members/"+uuid.NewString(), e.as(authz.Owner), nil), "removeMember", 404, CodeNotFound)
	}

	f := newFounders(t)
	e := f.e
	kow, wei := f.as(f.ahkow), f.as(f.weiling)
	members := func() response { return e.call(http.MethodGet, "/v1/members", kow, nil) }
	third := func() map[string]any {
		for _, x := range members().Body["items"].([]any) {
			if m := x.(map[string]any); m["id"] == f.third {
				return m
			}
		}
		return nil
	}

	// Third's role ends on 1 January 2099; they stay a member with nothing.
	e.clock.Set(time.Date(2099, 1, 2, 9, 0, 0, 0, time.UTC))
	kow, wei = f.as(f.ahkow), f.as(f.weiling)
	if m := third(); m == nil || len(m["roles"].([]any)) != 0 || !strings.Contains(e.call(http.MethodGet, "/v1/members", kow, nil).Raw,
		`"expired_roles":[{"role":"tax_agent","expires_at":"2099-01-01T00:00:00Z"}]`) {
		t.Fatalf("Third after their end date: %v", m)
	}
	if x := reviewMember(e.call(http.MethodGet, "/v1/access-review", kow, nil), "Third"); x["external"] != true || x["past_expiry"] != true {
		t.Fatalf("Third in the access review: %v", x)
	}
	c := e.checked("controls.external-users-past-expiry")
	wantStatus(t, c, "error")
	if it := itemNamed(c, f.third); it == nil || it["detail"] != "Roles ended: tax_agent on 1 Jan 2099" {
		t.Fatalf("C.2 #6's item: %s", c.Raw)
	}

	// A new end date: the expired assignment ends first, so the role can be given again.
	r := e.call(http.MethodPost, "/v1/members/"+f.third+"/roles", kow, map[string]any{"role": "tax_agent", "expires_at": "2099-06-30T00:00:00Z"})
	if r.Status != http.StatusAccepted {
		t.Fatalf("asking to give the role again: %d %s", r.Status, r.Raw)
	}
	if a := f.decide(wei, r.str("id"), "approve", hashOf(r)); a.Status != http.StatusOK {
		t.Fatalf("approving it: %d %s", a.Status, a.Raw)
	}
	if m := third(); len(m["roles"].([]any)) != 1 || len(m["expired_roles"].([]any)) != 0 {
		t.Fatalf("Third with a new end date: %v", m)
	}
	wantStatus(t, e.checked("controls.external-users-past-expiry"), "ok")

	// It ends again; Ah Kow asks to remove Third, Wei Ling approves.
	e.clock.Set(time.Date(2099, 7, 1, 9, 0, 0, 0, time.UTC))
	kow, wei = f.as(f.ahkow), f.as(f.weiling)
	if p := e.call(http.MethodDelete, "/v1/members/"+f.third, kow, nil, "Prefer", "return=preview"); p.Status != http.StatusAccepted {
		t.Fatalf("a preview: %d %s", p.Status, p.Raw)
	}
	r = e.call(http.MethodDelete, "/v1/members/"+f.third, kow, nil)
	if r.Status != http.StatusAccepted || r.Body["operation"] != approval.OpMemberRemove || r.Body["summary"] != "Remove Third from the organisation" {
		t.Fatalf("asking to remove Third: %d %s", r.Status, r.Raw)
	}
	e.problemIs(f.decide(kow, r.str("id"), "approve", hashOf(r)), "approveApproval", 403, approval.CodeOwnRequest)
	if a := f.decide(wei, r.str("id"), "approve", hashOf(r)); a.Status != http.StatusOK {
		t.Fatalf("approving the removal: %d %s", a.Status, a.Raw)
	}
	if third() != nil || len(members().Body["items"].([]any)) != 2 {
		t.Fatalf("Third is still a member: %s", members().Raw)
	}
	if reviewMember(e.call(http.MethodGet, "/v1/access-review", kow, nil), "Third") != nil {
		t.Fatal("Third is still in the access review")
	}
	wantStatus(t, e.checked("controls.external-users-past-expiry"), "ok")
	e.problemIs(e.call(http.MethodDelete, "/v1/members/"+f.third, kow, nil), "removeMember", 404, CodeNotFound)
	if !strings.Contains(auditTrail(t, e), `"user_membership.removed"`) {
		t.Error("the removal has no audit event")
	}
}

// C.2 #4: in Founder mode, a logged action still unreviewed 14 days after it executed is overdue; it is reviewed in the
// review log, never marked reviewed on Checks. C.2 #5: in Standard mode, a member whose roles hold a conflict with no
// override is a ✖.
// req: C.2 #4
// req: C.2 #5
// req: FM-3
func TestTheControlsChecks(t *testing.T) {
	f := newTrio(t)
	e := f.e
	wantStatus(t, e.checked("controls.review-log-overdue"), "ok")
	e.clock.Advance(approval.ReviewOverdue + time.Hour)
	c := e.checked("controls.review-log-overdue")
	wantStatus(t, c, "warning")
	if c.Body["count"] != float64(3) || c.Body["reviewed_in"] != "review_log" || !strings.Contains(c.Raw, `"label":"Invite Ann as owner, controller, administrator"`) {
		t.Fatalf("C.2 #4: %s", c.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/checks/controls.review-log-overdue/reviews", e.as(authz.Controller),
		map[string]any{"kind": "review_log", "id": itemsOf(c)[0]["id"]}), "reviewCheckItem", 422, CodeNotToReview)
	var ids []string
	for _, it := range itemsOf(c) {
		ids = append(ids, it["id"].(string))
	}
	if r := e.call(http.MethodPost, "/v1/review-log/reviews", f.as(f.ben), map[string]any{"entry_ids": ids}); r.Status != http.StatusOK {
		t.Fatalf("Ben's review: %d %s", r.Status, r.Raw)
	}
	wantStatus(t, e.checked("controls.review-log-overdue"), "ok")
	wantStatus(t, e.checked("controls.sod-conflicts"), "ok") // Founder mode allows SOD-6

	g := newFounders(t)
	standard := func() {
		r := g.e.call(http.MethodPatch, "/v1/team-mode", g.as(g.ahkow, authz.Administrator), map[string]any{"mode": "standard"}, "If-Match", `"0"`)
		if r.Status != http.StatusAccepted {
			t.Fatalf("asking for Standard mode: %d %s", r.Status, r.Raw)
		}
		if a := g.decide(g.as(g.weiling), r.str("id"), "approve", hashOf(r)); a.Status != http.StatusOK {
			t.Fatalf("approving Standard mode: %d %s", a.Status, a.Raw)
		}
	}
	standard()
	wantStatus(t, g.e.checked("controls.sod-conflicts"), "ok") // an Owner who approves transactions holds no conflict
	// Wei Ling is made an Administrator outside the API, as only a fault could: nothing covers it.
	var wei storage.Member
	g.e.fixture(func(ctx context.Context, u storage.UnitOfWork) error {
		ms, err := u.Members().List(ctx, g.e.clock.Now())
		for _, m := range ms {
			if m.UserID.String() == g.weiling {
				wei = m
			}
		}
		if err != nil {
			return err
		}
		_, err = u.Members().Assign(ctx, wei.ID, "administrator", nil)
		return err
	})
	c = g.e.checked("controls.sod-conflicts")
	wantStatus(t, c, "error")
	if it := itemNamed(c, wei.ID.String()); it == nil || it["detail"] != "SOD-6: controller, owner and administrator, which standard mode blocks, with no override" {
		t.Fatalf("C.2 #5's item: %s", c.Raw)
	}
}
