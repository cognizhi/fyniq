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
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The checks of the founders' money (design D15), each passing and then fired by its fixture; the financial year ended
// on 30 Sep 2026 and today is 15 Oct 2026:
//   - C.2 #23: Ah Kow lent the company S$1,000.00 in September and has not confirmed his balance at 30 Sep 2026;
//   - C.2 #21: he was then paid S$1,500.00, so he owes the company S$500.00 at the month end, with s162's words;
//   - C.2 #25: shares issued before the books start, recorded in the register, are not in the ledger's share capital;
//   - C.2 #22: Mei Lin's approved conversion is ⏰ due to have its approval lodged with ACRA, until that step is recorded.
//
// req: C.2 #21
// req: C.2 #22
// req: C.2 #23
// req: C.2 #25
// req: FR-EQ-09
// req: FR-EQ-11
// req: FR-EQ-12
// req: INV-13
func TestTheFoundersChecks(t *testing.T) {
	c := newClaimants(t)
	e := c.e
	e.yearEndingSeptember()
	loan, _ := e.directorChart()
	kow := e.directorContact("Ah Kow")
	for _, key := range []string{"founders.director-in-debit", "founders.conversion-registered", "founders.balances-confirmed", "founders.share-capital-registered"} {
		wantStatus(t, e.checked(key), "ok")
	}

	// C.2 #23: a balance at the year end, not confirmed.
	if r := e.postFounderJournal("2026-09-10", founderLine(e.books.Bank.ID.String(), "1000.00", "", ""), founderLine(loan, "-1000.00", kow, ledger.OriginLent)); r.Status != http.StatusOK {
		t.Fatalf("Ah Kow lending: %d %s", r.Status, r.Raw)
	}
	r := e.checked("founders.balances-confirmed")
	wantStatus(t, r, "warning")
	if it := itemNamed(r, kow); it == nil || !strings.Contains(it["detail"].(string), "link the member who signs in as them first") || !strings.Contains(it["detail"].(string), "30 Sep 2026") {
		t.Errorf("C.2 #23's item: %s", r.Raw)
	}

	// C.2 #21: paid out more than he lent, a debit at the month end.
	if r := e.postFounderJournal("2026-09-20", founderLine(loan, "1500.00", kow, ledger.OriginPaidOut), founderLine(e.books.Bank.ID.String(), "-1500.00", "", "")); r.Status != http.StatusOK {
		t.Fatalf("paying Ah Kow: %d %s", r.Status, r.Raw)
	}
	r = e.checked("founders.director-in-debit")
	wantStatus(t, r, "warning")
	if it := itemNamed(r, kow); it == nil || !strings.Contains(it["detail"].(string), "Owes the company 500.00 SGD at 30 Sep 2026") || !strings.Contains(it["detail"].(string), "section 162") {
		t.Errorf("C.2 #21's item: %s", r.Raw)
	}

	// C.2 #25: the incorporation subscription in the register, not in the ledger.
	class := e.shareCapitalIn(e.books.Org)
	wantStatus(t, e.checked("founders.share-capital-registered"), "ok")
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.ShareIssues().Record(ctx, storage.ShareIssue{ID: uuid.Must(uuid.NewV7()), HolderID: uuid.MustParse(kow), ClassAccountID: uuid.MustParse(class),
			Shares: "1000", Price: "1.000000", Currency: "SGD", Amount: "1000.00", Consideration: storage.ConsiderationCash, ResolutionRefs: "Incorporation subscription",
			EffectiveOn: time.Date(2025, 9, 1, 0, 0, 0, 0, time.UTC), RecordedBy: uuid.MustParse(c.weiling), RecordedAt: e.clock.Now()})
		return err
	})
	r = e.checked("founders.share-capital-registered")
	wantStatus(t, r, "error")
	if it := itemNamed(r, class); it == nil || !strings.Contains(it["detail"].(string), "The account holds 0.00; the register's issues of the class come to 1000.00") {
		t.Errorf("C.2 #25's item: %s", r.Raw)
	}

	// C.2 #22: an approved conversion of Mei Lin's loan, its approval not lodged.
	mei := e.directorContact("Mei Lin")
	if r := e.postFounderJournal("2026-10-05", founderLine(e.books.Bank.ID.String(), "100.00", "", ""), founderLine(loan, "-100.00", mei, ledger.OriginLent)); r.Status != http.StatusOK {
		t.Fatalf("Mei Lin lending: %d %s", r.Status, r.Raw)
	}
	weiling := c.as(c.weiling, authz.Owner, authz.Controller)
	d := e.call(http.MethodPost, "/v1/director-settlements", weiling, map[string]any{"contact_id": mei, "account_id": loan,
		"amount": map[string]any{"amount": "100.00", "currency": "SGD"}, "route": "convert",
		"conversion": map[string]any{"class_account_id": class, "price": "1.00", "capacity": "shareholder"}})
	if d.Status != http.StatusCreated {
		t.Fatalf("drafting the conversion: %d %s", d.Status, d.Raw)
	}
	path := "/v1/director-settlements/" + d.str("id")
	for _, kind := range []string{"shareholders' approval", "board resolution", "conversion agreement", "balance confirmation"} {
		g := e.call(http.MethodGet, path, weiling, nil)
		if f := e.upload(path+"/evidence", weiling, map[string]string{"evidence_type": kind}, "doc.pdf", []byte("%PDF-1.7 "+kind), "If-Match", g.Header.Get("ETag")); f.Status != http.StatusCreated {
			t.Fatalf("attaching the %s: %d %s", kind, f.Status, f.Raw)
		}
	}
	asked := e.call(http.MethodPost, path+"/approval", weiling, nil, "If-Match", d.Header.Get("ETag"))
	if a := e.call(http.MethodPost, "/v1/approvals/"+asked.str("id")+"/approve", c.as(c.ahkow, authz.Owner), hashOf(asked), "If-Match", `"1"`); a.Status != http.StatusOK {
		t.Fatalf("approving the conversion: %d %s", a.Status, a.Raw)
	}
	r = e.checked("founders.conversion-registered")
	wantStatus(t, r, "due")
	if !strings.Contains(r.Raw, "The shareholders' approval to issue the shares is due to be lodged with ACRA by 29 Oct 2026") {
		t.Errorf("C.2 #22's item: %s", r.Raw)
	}
	g := e.call(http.MethodGet, path, weiling, nil)
	if s := e.call(http.MethodPost, path+"/acra-steps", weiling, map[string]any{"kind": "approval_lodged", "on": "2026-10-15", "reference": "T26A009"},
		"If-Match", g.Header.Get("ETag")); s.Status != http.StatusCreated {
		t.Fatalf("recording the approval lodged: %d %s", s.Status, s.Raw)
	}
	wantStatus(t, e.checked("founders.conversion-registered"), "ok")
}
