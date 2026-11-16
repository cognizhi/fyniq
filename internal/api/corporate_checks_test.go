//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The corporate cross-checks on 5 Dec 2026 (FR-CS-03, C.2 #32 to #35; p1-company-secretary task 3.1, design D4). With
// nothing recorded, the secretary, the statements and the ACRA profile are not assessed, each naming its input. Cognizhi
// was incorporated on 2 Mar 2026, so its secretary was due by 2 Sep: none in office is ✖; Tan Mei Lin, its sole director,
// recorded as the secretary is ✖ too; with Lim Hui a second director it is fine. Its first year ended on 30 Jun 2026 and
// the AGM is due on 31 Dec: with no approval recorded the statements are ⏰, and fine once recorded. Lim Hui's
// appointment on 17 Nov, not lodged by 1 Dec, and a nominee entered on Thursday 26 Nov, not filed centrally by Monday
// 30 Nov, are past their deadlines (⏰, G4-102). ACRA's business profile of 1 Dec agrees with the profile but for the
// registered office (⚠).
// req: FR-CS-03
// req: FR-CS-06
// req: C.2 #32
// req: C.2 #33
// req: C.2 #34
// req: C.2 #35
// req: G4-102
func TestTheCorporateChecks(t *testing.T) {
	c := newClaimants(t)
	e := c.e
	e.clock.Set(time.Date(2026, 12, 5, 9, 0, 0, 0, time.UTC)) // before the sessions, so their step-up is fresh
	prep := c.as(c.weiling, authz.Accountant)
	kow := c.as(c.ahkow, authz.Owner)
	owner := c.as(c.weiling, authz.Owner, authz.Controller)
	record := func(path string, body map[string]any) response {
		t.Helper()
		d := e.call(http.MethodPost, path, prep, body)
		if d.Status != http.StatusCreated {
			t.Fatalf("drafting at %s: %d %s", path, d.Status, d.Raw)
		}
		at := path + "/" + d.str("id")
		e.upload(at+"/evidence", prep, map[string]string{"evidence_type": "BizFile+"}, "ack.pdf", []byte("%PDF-1.7 ack"), "If-Match", d.Header.Get("ETag"))
		cur := e.call(http.MethodGet, at, prep, nil)
		req := e.call(http.MethodPost, at+"/approval", prep, nil, "If-Match", cur.Header.Get("ETag"))
		if r := e.call(http.MethodPost, "/v1/approvals/"+req.str("id")+"/approve", kow, hashOf(req), "If-Match", `"1"`); r.Status != http.StatusOK {
			t.Fatalf("approving %s: %d %s", at, r.Status, r.Raw)
		}
		return d
	}
	for key, needs := range map[string]string{"corporate.secretary": "the company's details from incorporation",
		"corporate.statements-approved": "the books start", "corporate.acra-profile": "a current ACRA business profile"} {
		if r := e.checked(key); r.Body["status"] != "not_assessed" || !strings.Contains(r.Raw, needs) {
			t.Errorf("%s with nothing recorded: %s", key, r.Raw)
		}
	}
	wantStatus(t, e.checked("corporate.lodgements"), "ok")

	start, end := time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		pr := o.Profile
		pr.FYStartMonth, pr.BooksStart, pr.FirstYearEnd = 7, &start, &end
		_, err = u.UpdateOrganisation(ctx, o.Version, pr)
		return err
	})
	record("/v1/corporate-profile/details", map[string]any{"from": "2026-03-02", "name": "Cognizhi Pte. Ltd.", "registered_office": "1 North Bridge Road",
		"ssic": "62011"})
	officers := "/v1/corporate-profile/officers"
	record(officers, map[string]any{"role": "director", "name": "Tan Mei Lin", "kind": "individual", "appointed_on": "2026-03-02",
		"appointment_lodged_on": "2026-03-02", "nominee": false})
	s := e.checked("corporate.secretary")
	if wantStatus(t, s, "error"); !strings.Contains(s.Raw, "since the six months after incorporation on 2 Mar 2026 ended on 2 Sep 2026") {
		t.Errorf("no secretary: %s", s.Raw)
	}
	record(officers, map[string]any{"role": "secretary", "name": "Tan  Mei Lin", "kind": "individual", "appointed_on": "2026-03-02",
		"appointment_lodged_on": "2026-03-02", "nominee": false})
	if s := e.checked("corporate.secretary"); s.Body["status"] != "error" || itemNamed(s, "sole-director") == nil {
		t.Errorf("the sole director as the secretary: %s", s.Raw)
	}
	lim := record(officers, map[string]any{"role": "director", "name": "Lim Hui", "kind": "individual", "appointed_on": "2026-11-17", "nominee": false})
	wantStatus(t, e.checked("corporate.secretary"), "ok")

	st := e.checked("corporate.statements-approved")
	if wantStatus(t, st, "due"); itemNamed(st, "agm:2026-06-30") == nil {
		t.Errorf("the statements for 2026: %s", st.Raw)
	}
	record("/v1/corporate-profile/statements-approvals", map[string]any{"fy_end": "2026-06-30", "approved_on": "2026-11-30"})
	wantStatus(t, e.checked("corporate.statements-approved"), "ok")

	holder := e.call(http.MethodPost, "/v1/contacts", owner, map[string]any{"legal_name": "Seedling Nominees Pte. Ltd.", "shareholder": true}).str("id")
	entry := record("/v1/corporate-profile/nominees", map[string]any{"kind": "shareholder", "holder_id": holder, "nominator": "Fund I",
		"since": "2026-11-20", "informed_on": "2026-11-20", "register_on": "2026-11-26"})
	l := e.checked("corporate.lodgements")
	if wantStatus(t, l, "due"); itemNamed(l, "register_officer_appointment:"+lim.str("id")+":2026-11-17") == nil ||
		itemNamed(l, "register_nominee_central:"+entry.str("id")+":2026-11-26") == nil {
		t.Errorf("the lodgements past their deadlines: %s", l.Raw)
	}

	ordinary := e.shareCapitalIn(e.books.Org)
	x := e.call(http.MethodPost, "/v1/acra-extracts", prep, map[string]any{"profile_on": "2026-12-01", "registered_office": "2 Science Park Drive",
		"holdings": []any{map[string]any{"holder_id": holder, "class_account_id": ordinary, "shares": "1000"}},
		"officers": []any{
			map[string]any{"name": "Tan Mei Lin", "role": "director", "appointed_on": "2026-03-02"},
			map[string]any{"name": "Tan Mei Lin", "role": "secretary", "appointed_on": "2026-03-02"},
			map[string]any{"name": "Lim Hui", "role": "director", "appointed_on": "2026-11-17"},
		}})
	e.upload("/v1/acra-extracts/"+x.str("id")+"/evidence", prep, map[string]string{"evidence_type": "Business profile"}, "bp.pdf", []byte("%PDF-1.7 bp"),
		"If-Match", x.Header.Get("ETag"))
	cur := e.call(http.MethodGet, "/v1/acra-extracts/"+x.str("id"), prep, nil)
	if r := e.call(http.MethodPost, "/v1/acra-extracts/"+x.str("id")+"/record", prep, nil, "If-Match", cur.Header.Get("ETag")); r.Status != http.StatusOK {
		t.Fatalf("recording the extract: %d %s", r.Status, r.Raw)
	}
	a := e.checked("corporate.acra-profile")
	if wantStatus(t, a, "warning"); len(itemsOf(a)) != 1 || itemNamed(a, "office") == nil {
		t.Errorf("the profile against ACRA: %s", a.Raw)
	}
}
