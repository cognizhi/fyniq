//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/equity"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// An investor's information rights become tasks on the statutory calendar (FR-EQ-28; p2-reporting-planning task 3.4, design D8), worked by hand. On 20 Nov
// 2026, with the financial year the calendar year, a holder owed quarterly accounts within 45 days and the board pack within 20 days of each month's end from
// 1 Jan 2026: the quarter ended 30 Sep was due 14 Nov (overdue) and the one ended 31 Dec is due 14 Feb 2027; the pack for October is due on 20 Nov (due soon)
// and September's was due 20 Oct (overdue). Sending the holder's member the quarterly update of September's pack closes the quarter's task with that update as
// its evidence, and publishing the pack closes the month's, to a holder a member signs in as; the other quarters and months stay open.
// req: FR-EQ-28
func TestAnInvestorsInformationRightsBecomeCalendarTasksClosedByDelivery(t *testing.T) {
	f := newFounders(t)
	e := f.e
	kpiBooks(e)
	e.clock.Set(time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC))
	kow, ling := f.as(f.ahkow), f.as(f.weiling)
	start, end := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		pr := o.Profile
		pr.FYStartMonth, pr.BooksStart, pr.FirstYearEnd = 1, &start, &end
		_, err = u.UpdateOrganisation(ctx, o.Version, pr)
		return err
	})
	holder := e.directorContact("Ah Kow")
	rights := equity.InvestorRights{HolderID: uuid.MustParse(holder), From: start, Agreements: "Shareholders' agreement",
		InformationRights: []equity.InformationRight{{Kind: equity.InfoQuarterlyAccounts, Days: 45}, {Kind: equity.InfoBoardPack, Days: 20}}}
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := equity.DraftRightsIn(ctx, u, rights, e.clock.Now())
		if err != nil {
			return err
		}
		return u.Documents().SetStatus(ctx, doc.ID, storage.DocumentPosted)
	})
	tasks := func() map[string]string {
		t.Helper()
		out := map[string]string{}
		for _, line := range calendarItems(t, e.call(http.MethodGet, "/v1/compliance/calendar", e.as(authz.Auditor), nil)) {
			if strings.HasPrefix(line, "investor_information:") {
				parts := strings.Fields(line)
				out[strings.TrimPrefix(parts[0], "investor_information:"+holder+":")] = parts[1] + " " + parts[2]
			}
		}
		return out
	}
	got := tasks()
	for key, want := range map[string]string{
		"quarterly_accounts:2026-09-30": "2026-11-14 overdue",
		"quarterly_accounts:2026-12-31": "2027-02-14 open",
		"board_pack:2026-10-31":         "2026-11-20 due_soon",
		"board_pack:2026-09-30":         "2026-10-20 overdue",
	} {
		if got[key] != want {
			t.Errorf("%s = %q, want %q (all: %v)", key, got[key], want, got)
		}
	}

	// A member signs in as the holder, the September pack is published and its quarterly update sent to them: both tasks close with their evidence (a closed task due before the window is left out).
	c := e.call(http.MethodGet, "/v1/contacts/"+holder, e.as(authz.Accountant), nil)
	req := e.call(http.MethodPut, "/v1/contacts/"+holder+"/member", kow, map[string]any{"member_user_id": f.ahkow}, "If-Match", c.Header.Get("ETag"))
	if req.Status != http.StatusAccepted {
		t.Fatalf("linking = %d: %s", req.Status, req.Raw)
	}
	if r := f.decide(ling, req.str("id"), "approve", hashOf(req)); r.Status != http.StatusOK {
		t.Fatalf("approving the link = %d: %s", r.Status, r.Raw)
	}
	controller := f.as(f.weiling, authz.Controller)
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Periods().Create(ctx, storage.NewPeriod{StartsOn: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), EndsOn: time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)})
		return err
	})
	pack := e.call(http.MethodPost, "/v1/board-packs", controller, map[string]any{"month": "2026-09"})
	if pack.Status != http.StatusCreated {
		t.Fatalf("the pack = %d: %s", pack.Status, pack.Raw)
	}
	if p := e.call(http.MethodPost, "/v1/board-packs/"+pack.str("id")+"/publish", controller, nil, "If-Match", `"1"`); p.Status != http.StatusOK {
		t.Fatalf("publishing = %d: %s", p.Status, p.Raw)
	}
	made := e.call(http.MethodPost, "/v1/investor-updates", controller, map[string]any{"pack_id": pack.str("id"), "kind": "quarterly", "title": "Q3 2026 investor update"})
	if made.Status != http.StatusCreated {
		t.Fatalf("the update = %d: %s", made.Status, made.Raw)
	}
	if s := e.call(http.MethodPost, "/v1/investor-updates/"+made.str("id")+"/send", controller, map[string]any{"recipients": []string{f.ahkow}}, "If-Match", `"1"`); s.Status != http.StatusOK {
		t.Fatalf("sending = %d: %s", s.Status, s.Raw)
	}
	got = tasks()
	if got["quarterly_accounts:2026-09-30"] != "2026-11-14 filed" || got["board_pack:2026-09-30"] != "" || got["board_pack:2026-08-31"] != "2026-09-20 overdue" ||
		got["quarterly_accounts:2026-12-31"] != "2027-02-14 open" {
		t.Errorf("after the delivery: %v", got)
	}
	r := e.call(http.MethodGet, "/v1/compliance/calendar", e.as(authz.Auditor), nil)
	for _, x := range r.Body["deadlines"].([]any) {
		it := x.(map[string]any)
		if it["key"] == "investor_information:"+holder+":quarterly_accounts:2026-09-30" {
			d, _ := it["delivery"].(map[string]any)
			if d["kind"] != "update" || d["id"] != made.str("id") || d["sent_on"] != "2026-11-20" || it["register"] != true {
				t.Errorf("the quarter's evidence: %v", it)
			}
		}
	}
}

// Drafting investor rights takes the information rights as a list: one of each of the four kinds, due in 1 to 365 days; a kind twice or a day count outside
// that is refused with the field named, and the list comes back as it was drafted (FR-EQ-28).
// req: FR-EQ-28
func TestInvestorRightsTakeTheirInformationRightsAsAList(t *testing.T) {
	e := newEnv(t)
	acc := e.as(authz.Accountant)
	holder := e.call(http.MethodPost, "/v1/contacts", acc, map[string]any{"legal_name": "Lion Rock Ventures Pte. Ltd.", "investor": true, "shareholder": true}).str("id")
	draft := func(info []map[string]any) response {
		return e.call(http.MethodPost, "/v1/investor-rights", acc, map[string]any{"holder_id": holder, "from": "2026-06-18", "board_seats": 0, "observer": false,
			"board_majority": false, "pro_rata": false, "consents": []string{}, "agreements": "Side letter", "information_rights": info})
	}
	r := draft([]map[string]any{{"kind": "quarterly_accounts", "days": 45}, {"kind": "board_pack", "days": 20}})
	if r.Status != http.StatusCreated || !strings.Contains(r.Raw, `"information_rights":[{"kind":"quarterly_accounts","days":45},{"kind":"board_pack","days":20}]`) {
		t.Fatalf("drafting = %d: %s", r.Status, r.Raw)
	}
	for name, info := range map[string][]map[string]any{
		"a kind twice":  {{"kind": "board_pack", "days": 20}, {"kind": "board_pack", "days": 30}},
		"no days":       {{"kind": "board_pack", "days": 0}},
		"too many days": {{"kind": "annual_accounts", "days": 366}},
	} {
		if x := draft(info); x.Status != http.StatusUnprocessableEntity {
			t.Errorf("%s: %d %s", name, x.Status, x.Raw)
		}
	}
}
