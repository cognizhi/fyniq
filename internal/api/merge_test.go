//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// mergeLine is a journal line on an account, with a rate when it is not in SGD.
func mergeLine(account, amount, currency, rate string) map[string]any {
	l := map[string]any{"account_id": account, "amount": map[string]any{"amount": amount, "currency": currency}}
	if rate != "" {
		l["fx_rate"] = rate
	}
	return l
}

// expenseAccount creates an expense account under the books' Expenses group.
func (e *env) expenseAccount(code, name string) storage.Account {
	e.t.Helper()
	var acc storage.Account
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		acc, err = u.Accounts().Create(ctx, storage.NewAccount{Code: code, Name: name, Type: "expense", ParentID: &e.books.Expenses.ID})
		return err
	})
	return acc
}

// A merge in Founder mode (FR-COA-04, FM-1; PRD-001 rev 0.174, Appendix B.8), worked by hand. 6135 SaaS tools holds
// S$300.00 from 5 October and USD 100.00 at 1.3501 (S$135.01) from 6 October. Ah Kow, as the Accountant, asks to merge it
// into 6120 Software subscriptions on 31 October; nothing moves until Wei Ling approves. Then one entry moves S$300.00
// and USD 100.00 (S$135.01) into 6120: 6135 holds nothing in October, 6120 holds S$435.01, and 6135 is inactive, merged
// into 6120, taking no posting. A merge into itself, into an account of another type, dated before 6135's last posting or
// in no period is refused; a posting to the account after the request voids the approval (FR-APR-05). Every entry
// replays.
// req: FR-COA-04
// req: FM-1
// req: FR-APR-05
// req: ledger/chart-of-accounts A merge moves the balance by one entry (rev 0.174)
// req: ledger/chart-of-accounts Accounts a merge refuses
// req: ledger/chart-of-accounts A change before the approval voids it
func TestAnAccountMergesOnAnotherFoundersApproval(t *testing.T) {
	f := newFounders(t)
	e := f.e
	e.booksFrom()
	ahkow := f.as(f.ahkow, authz.Accountant, authz.Controller)
	weiling := f.as(f.weiling, authz.Accountant, authz.Controller)
	saas, subs := e.expenseAccount("6135", "SaaS tools"), e.expenseAccount("6120", "Software subscriptions")
	bank := e.books.Bank.ID.String()
	if r := e.postFounderJournal("2026-10-05", mergeLine(saas.ID.String(), "300.00", "SGD", ""),
		mergeLine(bank, "-300.00", "SGD", "")); r.Status != http.StatusOK {
		t.Fatalf("the SGD journal: %d %s", r.Status, r.Raw)
	}
	if r := e.postFounderJournal("2026-10-06",
		mergeLine(saas.ID.String(), "100.00", "USD", "1.3501"),
		mergeLine(bank, "-135.01", "SGD", "")); r.Status != http.StatusOK {
		t.Fatalf("the USD journal: %d %s", r.Status, r.Raw)
	}
	path := "/v1/accounts/" + saas.ID.String()
	tag := func() string { return e.call(http.MethodGet, path, ahkow, nil).Header.Get("ETag") }
	ask := func(into, date string) response {
		return e.call(http.MethodPost, path+"/merge-approval", ahkow, map[string]any{"into_account_id": into, "date": date}, "If-Match", tag())
	}

	e.problemIs(ask(saas.ID.String(), "2026-10-31"), "requestAccountMerge", 422, journal.CodeMergeAccount)
	e.problemIs(ask(bank, "2026-10-31"), "requestAccountMerge", 422, journal.CodeMergeAccount)
	e.problemIs(ask(subs.ID.String(), "2026-10-05"), "requestAccountMerge", 422, journal.CodeMergeDate)
	e.problemIs(ask(subs.ID.String(), "2027-03-01"), "requestAccountMerge", 422, journal.CodeMergeDate)
	e.problemIs(e.call(http.MethodPost, path+"/merge-approval", weiling, map[string]any{"into_account_id": subs.ID.String(), "date": "2026-10-31"}),
		"requestAccountMerge", 428, CodeIfMatchRequired)

	asked := ask(subs.ID.String(), "2026-10-31")
	if asked.Status != http.StatusAccepted || asked.str("object_type") != approval.ObjectAccount || asked.str("operation") != approval.OpAccountMerge ||
		!strings.Contains(asked.str("summary"), "Merge 6135 SaaS tools into 6120 Software subscriptions on 2026-10-31") {
		t.Fatalf("requestAccountMerge = %d: %s", asked.Status, asked.Raw)
	}
	if g := e.call(http.MethodGet, path, ahkow, nil); !g.Body["active"].(bool) || g.Body["merged_into_id"] != nil {
		t.Fatalf("merged before the approval: %s", g.Raw)
	}
	e.problemIs(f.decide(ahkow, asked.str("id"), "approve", hashOf(asked)), "approveApproval", 403, approval.CodeOwnRequest)
	if d := f.decide(weiling, asked.str("id"), "approve", hashOf(asked)); d.Status != http.StatusOK {
		t.Fatalf("approving: %d %s", d.Status, d.Raw)
	}

	g := e.call(http.MethodGet, path, ahkow, nil)
	if g.Body["active"].(bool) || g.str("merged_into_id") != subs.ID.String() {
		t.Fatalf("after the approval: %s", g.Raw)
	}
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		rows, err := u.Balances().ForPeriod(ctx, e.books.Oct.ID)
		if err != nil {
			return err
		}
		got := map[string]string{}
		for _, r := range rows {
			switch r.AccountID {
			case saas.ID:
				got["6135 "+r.Currency] = string(r.Amount) + "/" + string(r.BaseAmount)
			case subs.ID:
				got["6120 "+r.Currency] = string(r.Amount) + "/" + string(r.BaseAmount)
			}
		}
		want := map[string]string{"6135 SGD": "0/0", "6135 USD": "0/0", "6120 SGD": "300.0000/300.0000", "6120 USD": "100.0000/135.0100"}
		for k, v := range want {
			if got[k] != v {
				t.Errorf("October %s: %s, want %s (all %v)", k, got[k], v, got)
			}
		}
		docs, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: storage.DocumentAccountMerge, Limit: 10})
		if err != nil || len(docs) != 1 || docs[0].Status != storage.DocumentPosted {
			t.Errorf("the merge's document: %v %v", docs, err)
		}
		return nil
	})

	// Merged for good: no posting, no reactivation, no second merge.
	r := e.call(http.MethodPost, "/v1/journals", ahkow, map[string]any{"date": "2026-11-02", "memo": "Late", "lines": []map[string]any{
		mergeLine(saas.ID.String(), "10.00", "SGD", ""), mergeLine(bank, "-10.00", "SGD", "")}})
	if r.Status == http.StatusCreated {
		e.problemIs(e.call(http.MethodPost, "/v1/journals/"+r.str("id")+"/post", ahkow, nil, "If-Match", r.Header.Get("ETag")), "postJournal", 422,
			ledger.CodeInactiveAccount)
	}
	if p := e.call(http.MethodPatch, path, ahkow, map[string]any{"active": true}, "If-Match", tag()); p.Status < 400 {
		t.Errorf("reactivated a merged account: %d %s", p.Status, p.Raw)
	}
	e.problemIs(ask(subs.ID.String(), "2026-10-31"), "requestAccountMerge", 422, journal.CodeMergeAccount)

	// A posting after the request voids the approval: what moves changed.
	tools := e.expenseAccount("6140", "Old tools")
	e.postFounderJournal("2026-10-07", mergeLine(tools.ID.String(), "50.00", "SGD", ""),
		mergeLine(bank, "-50.00", "SGD", ""))
	other := e.call(http.MethodPost, "/v1/accounts/"+tools.ID.String()+"/merge-approval", ahkow,
		map[string]any{"into_account_id": subs.ID.String(), "date": "2026-10-31"}, "If-Match", e.call(http.MethodGet, "/v1/accounts/"+tools.ID.String(), ahkow, nil).Header.Get("ETag"))
	if other.Status != http.StatusAccepted {
		t.Fatalf("asking again: %d %s", other.Status, other.Raw)
	}
	e.postFounderJournal("2026-10-08", mergeLine(tools.ID.String(), "5.00", "SGD", ""),
		mergeLine(bank, "-5.00", "SGD", ""))
	e.problemIs(f.decide(weiling, other.str("id"), "approve", hashOf(other)), "approveApproval", 409, approval.CodeChanged)
	if g := e.call(http.MethodGet, "/v1/accounts/"+tools.ID.String(), ahkow, nil); !g.Body["active"].(bool) {
		t.Errorf("merged on a void approval: %s", g.Raw)
	}

	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		rep, err := journal.Replay(ctx, u)
		if err == nil && len(rep.Mismatches) != 0 {
			t.Errorf("replay: %+v", rep.Mismatches)
		}
		return err
	})
}

// What moves (Appendix B.8), worked by hand over a year end: the books start on 1 October 2026 and the second financial
// year on 1 January 2027. 6150 Cloud hosting took S$200.00 in October and S$30.00 in January; merged into 6160 on 31
// January, only January's S$30.00 moves, since October's is in the first year's result. 1260 Deposits paid took S$500.00
// in October and S$70.00 in January; merged into 1270, its whole S$570.00 moves. A journal on 6170 that reverses by itself
// on 1 December still posts to it, so its merge is refused (MERGE-03).
// req: FR-COA-04
// req: ledger/posting-rules Account merge
// req: ledger/chart-of-accounts A merge waits for what is scheduled
func TestAMergeMovesTheYearsMovementOrTheWholeBalance(t *testing.T) {
	f := newFounders(t)
	e := f.e
	e.booksFrom()
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		for _, m := range []time.Month{time.December, time.January} {
			y := 2026
			if m == time.January {
				y = 2027
			}
			start := time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
			if _, err := u.Periods().Create(ctx, storage.NewPeriod{StartsOn: start, EndsOn: start.AddDate(0, 1, -1)}); err != nil {
				return err
			}
		}
		return nil
	})
	ahkow := f.as(f.ahkow, authz.Accountant, authz.Controller)
	weiling := f.as(f.weiling, authz.Accountant, authz.Controller)
	asset := func(code, name string) storage.Account {
		var acc storage.Account
		e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
			acc, err = u.Accounts().Create(ctx, storage.NewAccount{Code: code, Name: name, Type: "asset"})
			return err
		})
		return acc
	}
	hosting, cloud := e.expenseAccount("6150", "Cloud hosting"), e.expenseAccount("6160", "Hosting")
	deposits, other := asset("1260", "Deposits paid"), asset("1270", "Other deposits")
	bank := e.books.Bank.ID.String()
	for _, j := range []struct{ date, account, amount string }{
		{"2026-10-10", hosting.ID.String(), "200.00"}, {"2027-01-10", hosting.ID.String(), "30.00"},
		{"2026-10-10", deposits.ID.String(), "500.00"}, {"2027-01-10", deposits.ID.String(), "70.00"},
	} {
		if r := e.postFounderJournal(j.date, mergeLine(j.account, j.amount, "SGD", ""), mergeLine(bank, "-"+j.amount, "SGD", "")); r.Status != http.StatusOK {
			t.Fatalf("%s %s: %d %s", j.date, j.amount, r.Status, r.Raw)
		}
	}
	merge := func(from storage.Account, into storage.Account) {
		t.Helper()
		path := "/v1/accounts/" + from.ID.String()
		asked := e.call(http.MethodPost, path+"/merge-approval", ahkow, map[string]any{"into_account_id": into.ID.String(), "date": "2027-01-31"},
			"If-Match", e.call(http.MethodGet, path, ahkow, nil).Header.Get("ETag"))
		if asked.Status != http.StatusAccepted {
			t.Fatalf("asking to merge %s: %d %s", from.Code, asked.Status, asked.Raw)
		}
		if d := f.decide(weiling, asked.str("id"), "approve", hashOf(asked)); d.Status != http.StatusOK {
			t.Fatalf("approving the merge of %s: %d %s", from.Code, d.Status, d.Raw)
		}
	}
	merge(hosting, cloud)
	merge(deposits, other)
	moved := map[string]string{}
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		periods, err := u.Periods().List(ctx)
		if err != nil {
			return err
		}
		for _, p := range periods {
			rows, err := u.Balances().ForPeriod(ctx, p.ID)
			if err != nil {
				return err
			}
			for _, r := range rows {
				for _, a := range []storage.Account{hosting, cloud, deposits, other} {
					if r.AccountID == a.ID && r.BaseAmount != "0" {
						moved[p.StartsOn.Format("2006-01")+" "+a.Code] = string(r.BaseAmount)
					}
				}
			}
		}
		return nil
	})
	want := map[string]string{"2026-10 6150": "200.0000", "2026-10 1260": "500.0000", "2027-01 6160": "30.0000", "2027-01 1260": "-500.0000",
		"2027-01 1270": "570.0000"}
	if len(moved) != len(want) {
		t.Errorf("the movements: %v, want %v", moved, want)
	}
	for k, v := range want {
		if moved[k] != v {
			t.Errorf("%s: %s, want %s (all %v)", k, moved[k], v, moved)
		}
	}

	accrued := e.expenseAccount("6170", "Accrued hosting")
	d := e.call(http.MethodPost, "/v1/journals", ahkow, map[string]any{"date": "2026-11-30", "memo": "Accrual", "reverse_on": "2026-12-01",
		"lines": []map[string]any{mergeLine(accrued.ID.String(), "40.00", "SGD", ""), mergeLine(bank, "-40.00", "SGD", "")}})
	if p := e.call(http.MethodPost, "/v1/journals/"+d.str("id")+"/post", ahkow, nil, "If-Match", d.Header.Get("ETag")); p.Status != http.StatusOK {
		t.Fatalf("the accrual: %d %s", p.Status, p.Raw)
	}
	path := "/v1/accounts/" + accrued.ID.String()
	r := e.call(http.MethodPost, path+"/merge-approval", ahkow, map[string]any{"into_account_id": cloud.ID.String(), "date": "2027-01-31"},
		"If-Match", e.call(http.MethodGet, path, ahkow, nil).Header.Get("ETag"))
	e.problemIs(r, "requestAccountMerge", 409, journal.CodeMergeScheduled)
	if !strings.Contains(r.Raw, "reverses on 2026-12-01") {
		t.Errorf("what still posts: %s", r.Raw)
	}
}
