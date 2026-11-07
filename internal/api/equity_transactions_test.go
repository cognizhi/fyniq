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
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/equity"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Equity transactions (FR-EQ-17, B.7, INV-13, FM-9; p1-equity-register slice 2), G4-043's shape on the frozen clock:
// an investor pays S$600,000.00 into share application monies on 1 Oct; the cash issue of 250,000 ordinary shares at
// S$2.40, approved on 15 Oct, is refused effective until the money is in, then posts Dr 2040 · Cr 3010 from the register
// on its effective date, with its movement, and marks the investor shareholder; before then they hold nothing. A
// conversion between 3010 and 3020 moves its carrying amount; a transfer moves shares with no entry, never more than the
// seller holds and never recorded by the holder it moves shares to; a reversal undoes a transaction whole, never leaving
// a holder short, and a cash issue's reversal puts the money back in share application monies for its refund.
// req: FR-EQ-17
// req: INV-13
// req: FM-9
// req: equity/cap-table-and-investors Equity transactions
// asvs: V8.2.1, V8.2.2
func TestEquityTransactions(t *testing.T) {
	c := newClaimants(t)
	e := c.e
	weiling := c.as(c.weiling, authz.Owner, authz.Controller)
	prep := c.as(c.weiling, authz.Accountant) // the Accountant drafts and asks; another person approves
	kow := c.as(c.ahkow, authz.Owner, authz.Controller)
	mei := c.as(c.mei, authz.Owner)
	approve := func(req response, token string) response {
		return e.call(http.MethodPost, "/v1/approvals/"+req.str("id")+"/approve", token, hashOf(req), "If-Match", `"1"`)
	}

	ahKow := e.directorContact("Ah Kow")
	ct := e.call(http.MethodGet, "/v1/contacts/"+ahKow, weiling, nil)
	if r := approve(e.call(http.MethodPut, "/v1/contacts/"+ahKow+"/member", kow, map[string]any{"member_user_id": c.ahkow}, "If-Match", ct.Header.Get("ETag")), weiling); r.Status != http.StatusOK {
		t.Fatalf("linking Ah Kow: %d %s", r.Status, r.Raw)
	}
	fund := e.call(http.MethodPost, "/v1/contacts", weiling, map[string]any{"legal_name": "Seedling Ventures Pte. Ltd.", "investor": true}).str("id")
	customer := e.call(http.MethodPost, "/v1/contacts", weiling, map[string]any{"legal_name": "Northwind Analytics", "customer": true}).str("id")
	ordinary := e.shareCapitalIn(e.books.Org)
	var preference, monies string
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		p, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "3020", Name: "Preference share capital", Type: storage.Equity, Role: storage.RoleShareCapital})
		if err != nil {
			return err
		}
		m, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "2040", Name: "Share application monies", Type: storage.Liability, Role: "share-application-monies"})
		preference, monies = p.ID.String(), m.ID.String()
		return err
	})

	// draft drafts a transaction, attaches its evidence and has another person approve it, and the Accountant review it
	// when every other approver benefits (FM-9); it returns the transaction and its ETag.
	draft := func(body map[string]any, review bool) response {
		t.Helper()
		r := e.call(http.MethodPost, "/v1/equity-transactions", prep, body)
		if r.Status != http.StatusCreated || r.str("status") != "draft" {
			t.Fatalf("drafting %v: %d %s", body["kind"], r.Status, r.Raw)
		}
		path := "/v1/equity-transactions/" + r.str("id")
		if f := e.upload(path+"/evidence", prep, map[string]string{"evidence_type": "Board resolution"}, "resolution.pdf", []byte("%PDF-1.7 resolution"),
			"If-Match", r.Header.Get("ETag")); f.Status != http.StatusCreated {
			t.Fatalf("attaching: %d %s", f.Status, f.Raw)
		}
		req := e.call(http.MethodPost, path+"/approval", prep, nil, "If-Match", r.Header.Get("ETag"))
		if req.Status != http.StatusAccepted || req.str("object_type") != approval.ObjectEquityTransaction {
			t.Fatalf("asking: %d %s", req.Status, req.Raw)
		}
		if !strings.Contains(req.Raw, `"field":"accountant_review","before":"","after":"`+map[bool]string{true: "true", false: "false"}[review]+`"`) {
			t.Fatalf("the accountant's review: want %v: %s", review, req.Raw)
		}
		first := mei // Mei's token holds A; with a review after, the first signer's live roles must too (FR-APR-10): Ah Kow's
		if review {
			first = kow
		}
		if a := approve(req, first); a.Status != http.StatusOK {
			t.Fatalf("approving: %d %s", a.Status, a.Raw)
		}
		if review {
			if a := approve(req, c.as(c.mei, authz.Accountant)); a.Status != http.StatusOK {
				t.Fatalf("the Accountant reviewing: %d %s", a.Status, a.Raw)
			}
		}
		got := e.call(http.MethodGet, path, weiling, nil)
		if got.str("status") != "approved" || !strings.HasPrefix(got.str("number"), "EQ-") || got.str("approved_on") != "2026-10-15" {
			t.Fatalf("approved: %s\n%s", got.Raw, e.call(http.MethodGet, "/v1/approvals/"+req.str("id"), weiling, nil).Raw)
		}
		return got
	}
	effective := func(tx response, token, on string) response {
		return e.call(http.MethodPost, "/v1/equity-transactions/"+tx.str("id")+"/effective", token,
			map[string]any{"effective_on": on, "lodgement_ref": "AA2026101500456"}, "If-Match", tx.Header.Get("ETag"))
	}
	holds := func(holder string) string {
		t.Helper()
		h := e.call(http.MethodGet, "/v1/holders/"+holder, weiling, nil)
		var parts []string
		for _, x := range h.Body["holdings"].([]any) {
			m := x.(map[string]any)
			parts = append(parts, m["shares"].(string)+" "+m["account_code"].(string))
		}
		return strings.Join(parts, ", ")
	}

	// Drafts that do not fit, every problem at once.
	if r := e.call(http.MethodPost, "/v1/equity-transactions", prep, map[string]any{"kind": "issue_cash", "holder_id": customer, "class_account_id": monies,
		"shares": "1.5", "price": "0"}); r.Status != http.StatusUnprocessableEntity || !strings.Contains(r.Raw, equity.CodeTransaction) ||
		!strings.Contains(r.Raw, "body.holder_id") || !strings.Contains(r.Raw, "body.class_account_id") || !strings.Contains(r.Raw, "body.shares") ||
		!strings.Contains(r.Raw, "body.price") || !strings.Contains(r.Raw, "body.resolution_refs") {
		t.Errorf("a cash issue to a customer on 2040: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/equity-transactions", prep, map[string]any{"kind": "class_conversion", "holder_id": fund, "class_account_id": ordinary,
		"to_class_account_id": preference, "shares": "10", "to_shares": "10", "resolution_refs": "Board resolution"}); r.Status != http.StatusUnprocessableEntity ||
		!strings.Contains(r.Raw, "body.amount") {
		t.Errorf("a conversion between accounts with no carrying amount: %d %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/equity-transactions", weiling, map[string]any{"kind": "transfer"}), "createEquityTransaction", 403, CodeForbidden)

	// The cash issue (G4-043's shape): approved, refused effective until the money is in share application monies.
	issue := draft(map[string]any{"kind": "issue_cash", "holder_id": fund, "class_account_id": ordinary, "shares": "250000", "price": "2.40",
		"resolution_refs": "Board resolution 15 Oct 2026; shareholders' resolution 15 Oct 2026"}, false)
	if issue.str("amount", "amount") != "600000.00" || issue.str("paid", "amount") != "0.00" || !strings.Contains(issue.Raw, equity.CodeEffective) {
		t.Fatalf("the approved issue, unpaid: %s", issue.Raw)
	}
	e.problemIs(effective(issue, weiling, "2026-10-15"), "recordEquityTransactionEffective", 409, equity.CodeEffective)

	// Its ACRA steps (FR-EQ-09): ⏰ the shareholders' approval to lodge within 14 days of the resolution, until it is.
	step := func(tx response, kind, on string) response {
		return e.call(http.MethodPost, "/v1/equity-transactions/"+tx.str("id")+"/acra-steps", weiling, map[string]any{"kind": kind, "on": on, "reference": "T26ACRA00123"},
			"If-Match", tx.Header.Get("ETag"))
	}
	if f := issue.Body["acra"].(map[string]any)["flags"].([]any); len(f) != 1 || f[0].(map[string]any)["code"] != equity.CodeLodgementDue ||
		f[0].(map[string]any)["due_on"] != "2026-10-29" {
		t.Errorf("the issue's flags before its steps: %v", f)
	}
	e.problemIs(step(issue, "instrument_signed", "2026-10-15"), "recordEquityStep", 422, equity.CodeStep)
	e.problemIs(step(issue, "resolved", "2026-10-16"), "recordEquityStep", 422, equity.CodeStep) // after today
	if r := step(issue, "resolved", "2026-10-15"); r.Status != http.StatusCreated {
		t.Fatalf("the resolution: %d %s", r.Status, r.Raw)
	}
	if r := step(issue, "approval_lodged", "2026-10-15"); r.Status != http.StatusCreated || len(r.Body["acra"].(map[string]any)["flags"].([]any)) != 0 ||
		len(r.Body["acra"].(map[string]any)["steps"].([]any)) != 2 {
		t.Fatalf("the approval lodged: %d %s", r.Status, r.Raw)
	}
	if r := e.postFounderJournal("2026-10-01", founderLine(e.books.Bank.ID.String(), "600000.00", "", ""), founderLine(monies, "-600000.00", "", "")); r.Status == http.StatusOK {
		t.Errorf("a line on 2040 naming no subscriber posted: %s", r.Raw)
	}
	// The investor's money into share application monies, as a journal above the threshold: evidence and a second person.
	j := e.call(http.MethodPost, "/v1/journals", prep, map[string]any{"date": "2026-10-01", "memo": "Seedling Ventures subscription",
		"lines": []map[string]any{founderLine(e.books.Bank.ID.String(), "600000.00", "", ""), founderLine(monies, "-600000.00", fund, "")}})
	if j.Status != http.StatusCreated {
		t.Fatalf("the subscription's journal: %d %s", j.Status, j.Raw)
	}
	if f := e.upload("/v1/journals/"+j.str("id")+"/evidence", prep, map[string]string{"evidence_type": "Bank statement"}, "statement.pdf",
		[]byte("%PDF-1.7 statement"), "If-Match", j.Header.Get("ETag")); f.Status != http.StatusCreated {
		t.Fatalf("its bank statement: %d %s", f.Status, f.Raw)
	}
	asked := e.call(http.MethodPost, "/v1/journals/"+j.str("id")+"/post-approval", prep, nil, "If-Match", j.Header.Get("ETag"))
	if r := approve(asked, kow); asked.Status != http.StatusAccepted || r.Status != http.StatusOK {
		t.Fatalf("posting the investor's money: %d; %d %s", asked.Status, r.Status, r.Raw)
	}
	if h := holds(fund); h != "" {
		t.Errorf("the investor holds %q before the issue is effective", h)
	}
	e.problemIs(effective(issue, weiling, "2026-10-14"), "recordEquityTransactionEffective", 409, equity.CodeEffective) // before its approval
	done := effective(issue, weiling, "2026-10-15")
	if done.Status != http.StatusOK || done.str("status") != "effective" || done.str("effective_on") != "2026-10-15" || done.str("share_issue_id") == "" ||
		len(done.Body["entries"].([]any)) != 1 || len(done.Body["movements"].([]any)) != 1 {
		t.Fatalf("the issue made effective: %d %s", done.Status, done.Raw)
	}
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		entry, err := u.Ledger().Entry(ctx, uuid.MustParse(done.Body["entries"].([]any)[0].(map[string]any)["id"].(string)))
		if err != nil {
			return err
		}
		lines := map[string]string{}
		for _, l := range entry.Lines {
			party := ""
			if l.PartyID != nil {
				party = " " + l.PartyID.String()
			}
			lines[l.AccountID.String()] = string(l.BaseAmount) + party
		}
		if entry.RuleVersion != "share_issue_cash@1" || entry.Source.Type != "share_issue" || entry.PostingDate.Format("2006-01-02") != "2026-10-15" ||
			lines[monies] != "600000.0000 "+fund || lines[ordinary] != "-600000.0000" {
			t.Errorf("the issue's entry: %s %s %v", entry.RuleVersion, entry.PostingDate, lines)
		}
		return nil
	})
	if h := holds(fund); h != "250000 3010" {
		t.Errorf("the investor holds %q once the issue is effective", h)
	}
	if c := e.call(http.MethodGet, "/v1/contacts/"+fund, weiling, nil); c.Body["shareholder"] != true {
		t.Errorf("the investor is not marked shareholder: %s", c.Raw)
	}
	e.problemIs(effective(done, weiling, "2026-10-15"), "recordEquityTransactionEffective", 409, equity.CodeEffective) // once only
	e.problemIs(step(done, "allotment_lodged", "2026-10-15"), "recordEquityStep", 409, equity.CodeEffective)           // steps end with it

	// A conversion of 100,000 ordinary into preference shares, moving their carrying amount from 3010 to 3020.
	conv := draft(map[string]any{"kind": "class_conversion", "holder_id": fund, "class_account_id": ordinary, "to_class_account_id": preference,
		"shares": "100000", "to_shares": "100000", "amount": "240000.00", "resolution_refs": "Special resolution 15 Oct 2026"}, false)
	if r := effective(conv, weiling, "2026-10-15"); r.Status != http.StatusOK || len(r.Body["entries"].([]any)) != 1 || len(r.Body["movements"].([]any)) != 2 {
		t.Fatalf("the conversion: %d %s", r.Status, r.Raw)
	}
	if h := holds(fund); h != "150000 3010, 100000 3020" {
		t.Errorf("after the conversion the investor holds %q", h)
	}

	// A transfer of 50,000 ordinary shares to Ah Kow: no entry, never recorded by Ah Kow.
	transfer := draft(map[string]any{"kind": "transfer", "seller_id": fund, "buyer_id": ahKow, "class_account_id": ordinary, "shares": "50000",
		"price": "2.40"}, false)
	if r := step(transfer, "instrument_signed", "2026-10-14"); r.Status != http.StatusCreated ||
		!strings.Contains(r.Raw, `"code":"`+equity.CodeLodgementDue+`"`) || !strings.Contains(r.Raw, `"due_on":"2026-10-28"`) {
		t.Errorf("the instrument of transfer signed: %d %s", r.Status, r.Raw)
	}
	e.problemIs(effective(transfer, kow, "2026-10-15"), "recordEquityTransactionEffective", 403, approval.CodeBeneficiary)
	if r := effective(transfer, weiling, "2026-10-15"); r.Status != http.StatusOK || len(r.Body["entries"].([]any)) != 0 || len(r.Body["movements"].([]any)) != 2 {
		t.Fatalf("the transfer: %d %s", r.Status, r.Raw)
	}
	if a, b := holds(fund), holds(ahKow); a != "100000 3010, 100000 3020" || b != "50000 3010" {
		t.Errorf("after the transfer: %q and %q", a, b)
	}
	short := draft(map[string]any{"kind": "transfer", "seller_id": fund, "buyer_id": ahKow, "class_account_id": ordinary, "shares": "200000"}, false)
	e.problemIs(effective(short, weiling, "2026-10-15"), "recordEquityTransactionEffective", 409, equity.CodeEffective)

	// Reversals: the transfer's, whole; the issue's refused while its shares are converted; the conversion's, then the
	// issue's. Ah Kow, the only other approver, now benefits from a transaction of his own, so the Accountant reviews those
	// of the investor's (FM-9).
	reverse := func(of response, why string, review bool) response {
		return draft(map[string]any{"kind": "reversal", "reverses_id": of.str("id"), "reason": why}, review)
	}
	if r := effective(reverse(transfer, "ACRA's register shows no transfer", false), weiling, "2026-10-15"); r.Status != http.StatusOK || r.str("status") != "effective" {
		t.Fatalf("reversing the transfer: %d %s", r.Status, r.Raw)
	}
	if a, b := holds(fund), holds(ahKow); a != "150000 3010, 100000 3020" || b != "" {
		t.Errorf("after the transfer's reversal: %q and %q", a, b)
	}
	if r := e.call(http.MethodGet, "/v1/equity-transactions/"+transfer.str("id"), weiling, nil); r.str("status") != "reversed" || r.str("reversed_by") == "" {
		t.Errorf("the reversed transfer: %s", r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/equity-transactions", prep, map[string]any{"kind": "reversal", "reverses_id": transfer.str("id"), "reason": "again"}); r.Status != http.StatusUnprocessableEntity ||
		!strings.Contains(r.Raw, equity.CodeTransaction) || !strings.Contains(r.Raw, "reversed already") {
		t.Errorf("reversing the transfer again: %d %s", r.Status, r.Raw)
	}
	early := reverse(issue, "ACRA's register shows 50,000 shares", true)
	e.problemIs(effective(early, weiling, "2026-10-15"), "recordEquityTransactionEffective", 409, equity.CodeEffective)
	if r := effective(reverse(conv, "The conversion was not lodged", true), weiling, "2026-10-15"); r.Status != http.StatusOK || len(r.Body["entries"].([]any)) != 1 {
		t.Fatalf("reversing the conversion: %d %s", r.Status, r.Raw)
	}
	back := e.call(http.MethodGet, "/v1/equity-transactions/"+early.str("id"), weiling, nil)
	if r := effective(back, weiling, "2026-10-15"); r.Status != http.StatusOK || len(r.Body["entries"].([]any)) != 1 {
		t.Fatalf("reversing the issue: %d %s", r.Status, r.Raw)
	}
	if h := holds(fund); h != "" {
		t.Errorf("after the reversals the investor holds %q", h)
	}
	var entries []storage.Entry
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		entries, err = u.Ledger().Entries(ctx, 0, 500)
		return err
	})
	reversals := 0
	for _, x := range entries {
		if x.ReversesEntryID != nil && (x.Source.Type == "share_issue" || x.Source.Type == "equity_transaction") {
			reversals++
		}
	}
	if reversals != 2 {
		t.Errorf("%d reversing entries; want the issue's and the conversion's", reversals)
	}
	if r := e.call(http.MethodGet, "/v1/equity-transactions?kind=reversal", e.as(authz.Auditor), nil); r.Status != http.StatusOK || len(r.Body["items"].([]any)) != 3 {
		t.Errorf("the reversals, as the Auditor reads them: %d %s", r.Status, r.Raw)
	}
}

// equityTxFixture is the matrix's equity transactions: a drafted transfer with a file, and an approved cash issue whose
// subscriber's S$10.00 is in share application monies, ready to be made effective.
type equityTxFixture struct{ draft, draftFile, approved string }

var matrixEquityTxOf = map[*env]equityTxFixture{}

func matrixEquityTx(e *env) equityTxFixture {
	e.t.Helper()
	if f, ok := matrixEquityTxOf[e]; ok {
		return f
	}
	acc := e.as(authz.Accountant)
	var capital, monies string
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		all, err := u.Accounts().List(ctx)
		for _, a := range all {
			switch {
			case a.Role == storage.RoleShareCapital && a.Active && !a.Group:
				capital = a.ID.String()
			case a.Role == "share-application-monies":
				monies = a.ID.String()
			}
		}
		if err != nil || monies != "" {
			return err
		}
		m, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "2040", Name: "Share application monies", Type: storage.Liability, Role: "share-application-monies"})
		monies = m.ID.String()
		return err
	})
	if capital == "" {
		capital = e.shareCapitalIn(e.books.Org)
	}
	sub := e.call(http.MethodPost, "/v1/contacts", acc, map[string]any{"legal_name": "Matrix subscriber", "investor": true}).str("id")
	other := e.call(http.MethodPost, "/v1/contacts", acc, map[string]any{"legal_name": "Matrix buyer", "investor": true}).str("id")
	if r := e.postFounderJournal("2026-10-08", founderLine(e.books.Bank.ID.String(), "10.00", "", ""), founderLine(monies, "-10.00", sub, "")); r.Status != http.StatusOK {
		e.t.Fatalf("the matrix subscriber's money: %d %s", r.Status, r.Raw)
	}
	drafted := func(body map[string]any) (response, response) {
		r := e.call(http.MethodPost, "/v1/equity-transactions", acc, body)
		f := e.upload("/v1/equity-transactions/"+r.str("id")+"/evidence", acc, map[string]string{"evidence_type": "Board resolution"}, "resolution.pdf",
			[]byte("%PDF-1.7 matrix resolution"), "If-Match", r.Header.Get("ETag"))
		if r.Status != http.StatusCreated || f.Status != http.StatusCreated {
			e.t.Fatalf("the matrix's equity transaction: %d %d %s %s", r.Status, f.Status, r.Raw, f.Raw)
		}
		return r, f
	}
	d, df := drafted(map[string]any{"kind": "transfer", "seller_id": sub, "buyer_id": other, "class_account_id": capital, "shares": "1"})
	issue, _ := drafted(map[string]any{"kind": "issue_cash", "holder_id": sub, "class_account_id": capital, "shares": "10", "price": "1.00",
		"resolution_refs": "Matrix resolution"})
	e.approveAsAnother(approval.ObjectEquityTransaction, issue.str("id"), approval.OpEquityTransactionApprove, func(ctx context.Context, u storage.UnitOfWork) error {
		settings, err := journal.LoadSettings(ctx, u)
		if err != nil {
			return err
		}
		_, err = equity.ApproveTransactionIn(ctx, u, uuid.MustParse(issue.str("id")), time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC), settings)
		return err
	})
	f := equityTxFixture{draft: d.str("id"), draftFile: df.str("id"), approved: issue.str("id")}
	matrixEquityTxOf[e] = f
	return f
}

// matrixEquityTxState is what a refused call must leave as it was: the transactions, a file, and the holdings.
func matrixEquityTxState(e *env, f equityTxFixture) string {
	acc := e.as(authz.Accountant)
	var out []string
	for _, p := range []string{"/v1/equity-transactions/" + f.draft, "/v1/equity-transactions/" + f.draft + "/evidence", "/v1/equity-transactions/" + f.approved,
		"/v1/equity-transactions", "/v1/holders"} {
		r := e.call(http.MethodGet, p, acc, nil)
		out = append(out, r.Header.Get("ETag")+r.Raw)
	}
	return strings.Join(out, "|")
}

// matrixEquityTransactions are the authorisation matrix's rows for equity transactions (FR-EQ-17): writes as previews, so
// a refused one shows it changed nothing.
var matrixEquityTransactions = map[string]func(e *env, tok string) (response, func() bool){
	"listEquityTransactions": func(e *env, tok string) (response, func() bool) {
		matrixEquityTx(e)
		return e.call(http.MethodGet, "/v1/equity-transactions", tok, nil), nil
	},
	"getEquityTransaction": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/equity-transactions/"+matrixEquityTx(e).draft, tok, nil), nil
	},
	"createEquityTransaction": func(e *env, tok string) (response, func() bool) {
		f := matrixEquityTx(e)
		was := matrixEquityTxState(e, f)
		cur := e.call(http.MethodGet, "/v1/equity-transactions/"+f.draft, e.as(authz.Accountant), nil)
		r := e.call(http.MethodPost, "/v1/equity-transactions", tok, cur.Body["details"], "Prefer", "return=preview")
		return r, func() bool { return matrixEquityTxState(e, f) == was }
	},
	"replaceEquityTransaction": func(e *env, tok string) (response, func() bool) {
		f := matrixEquityTx(e)
		was := matrixEquityTxState(e, f)
		cur := e.call(http.MethodGet, "/v1/equity-transactions/"+f.draft, e.as(authz.Accountant), nil)
		r := e.call(http.MethodPut, "/v1/equity-transactions/"+f.draft, tok, cur.Body["details"], "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixEquityTxState(e, f) == was }
	},
	"requestEquityTransactionApproval": func(e *env, tok string) (response, func() bool) {
		f := matrixEquityTx(e)
		was := matrixEquityTxState(e, f)
		r := e.call(http.MethodPost, "/v1/equity-transactions/"+f.draft+"/approval", steppedUp(e, tok), nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixEquityTxState(e, f) == was }
	},
	"recordEquityStep": func(e *env, tok string) (response, func() bool) {
		f := matrixEquityTx(e)
		was := matrixEquityTxState(e, f)
		r := e.call(http.MethodPost, "/v1/equity-transactions/"+f.approved+"/acra-steps", steppedUp(e, tok), map[string]any{"kind": "resolved", "on": "2026-10-15"},
			"If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixEquityTxState(e, f) == was }
	},
	"recordEquityTransactionEffective": func(e *env, tok string) (response, func() bool) {
		f := matrixEquityTx(e)
		was := matrixEquityTxState(e, f)
		r := e.call(http.MethodPost, "/v1/equity-transactions/"+f.approved+"/effective", steppedUp(e, tok), map[string]any{"effective_on": "2026-10-15"},
			"If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixEquityTxState(e, f) == was }
	},
	"draftInstrumentConversions": func(e *env, tok string) (response, func() bool) {
		f := matrixEquityTx(e)
		was := matrixEquityTxState(e, f)
		r := e.call(http.MethodPost, "/v1/equity-transactions/"+f.approved+"/conversions", tok, nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixEquityTxState(e, f) == was }
	},
	"addEquityTransactionEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixEquityTx(e)
		was := matrixEquityTxState(e, f)
		r := e.upload("/v1/equity-transactions/"+f.draft+"/evidence", tok, map[string]string{"evidence_type": "Instrument of transfer"}, "transfer.pdf",
			[]byte("%PDF-1.7 transfer"), "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixEquityTxState(e, f) == was }
	},
	"listEquityTransactionEvidence": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/equity-transactions/"+matrixEquityTx(e).draft+"/evidence", tok, nil), nil
	},
	"getEquityTransactionEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixEquityTx(e)
		return e.call(http.MethodGet, "/v1/equity-transactions/"+f.draft+"/evidence/"+f.draftFile, tok, nil), nil
	},
	"removeEquityTransactionEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixEquityTx(e)
		was := matrixEquityTxState(e, f)
		r := e.call(http.MethodDelete, "/v1/equity-transactions/"+f.draft+"/evidence/"+f.draftFile, tok, nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixEquityTxState(e, f) == was }
	},
}

// Share application monies from the bank (B.7, rev 0.206, the owner's decision of 7 Nov 2026): a line of money in is
// categorised to 2040 naming its subscriber, Dr Bank · Cr 2040; a refund paid from the bank is the line out, Dr 2040 ·
// Cr Bank; a line on 2040 naming no one, or a customer, is refused.
// req: FR-EQ-17
// req: ledger/invariants Share application monies name their subscriber (rev 0.205)
func TestShareApplicationMoniesFromTheBank(t *testing.T) {
	e := newEnv(t)
	acc := e.as(authz.Accountant)
	_, in := e.bankLine("600000.00")
	var monies string
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		m, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "2040", Name: "Share application monies", Type: storage.Liability, Role: "share-application-monies"})
		monies = m.ID.String()
		return err
	})
	fund := e.call(http.MethodPost, "/v1/contacts", acc, map[string]any{"legal_name": "Seedling Ventures Pte. Ltd.", "investor": true}).str("id")
	customer := e.call(http.MethodPost, "/v1/contacts", acc, map[string]any{"legal_name": "Northwind Analytics", "customer": true}).str("id")
	categorise := func(l storage.BankLine, contact string) response {
		body := map[string]any{"parts": []map[string]any{{"account_id": monies, "gst_code": "NA"}}}
		if contact != "" {
			body["contact_id"] = contact
		}
		return e.call(http.MethodPost, "/v1/bank-lines/"+l.ID.String()+"/categorise", acc, body, "If-Match", etag(l.Version))
	}
	for name, who := range map[string]string{"no one": "", "a customer": customer} {
		if r := categorise(in, who); r.Status == http.StatusOK || !strings.Contains(r.Raw, "LEDGER-41") {
			t.Errorf("a line on 2040 naming %s: %d %s", name, r.Status, r.Raw)
		}
	}
	if r := categorise(in, fund); r.Status != http.StatusOK {
		t.Fatalf("the subscription in: %d %s", r.Status, r.Raw)
	}
	m, _ := e.bankLine("-1.00")
	out := e.lineOn(m, "-600000.00", nil)
	if r := categorise(out, fund); r.Status != http.StatusOK {
		t.Fatalf("the refund out: %d %s", r.Status, r.Raw)
	}
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		base, _ := money.Lookup("SGD")
		for _, day := range []time.Time{time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC), time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)} {
			held, err := balance.ApplicationMonies(ctx, u, base, uuid.MustParse(monies), uuid.MustParse(fund), day)
			if err != nil {
				return err
			}
			if held.String() != "0.00" { // in and out on 20 Oct
				t.Errorf("on %s the investor's monies hold %s", day.Format(time.DateOnly), held)
			}
		}
		return nil
	})
}
