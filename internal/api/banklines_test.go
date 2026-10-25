//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/banking"
	"github.com/cognizhi/fyniq/internal/domain/banktransaction"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// bankBooks gives e's books, once, what lines post to besides the sample's: GST control and suspense.
func (e *env) bankBooks() {
	e.t.Helper()
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		have := map[string]bool{}
		accounts, err := u.Accounts().List(ctx)
		if err != nil {
			return err
		}
		for _, a := range accounts {
			have[string(a.Role)] = true
		}
		for _, a := range []storage.NewAccount{
			{Code: "2200", Name: "GST control", Type: storage.Liability, Role: storage.AccountRole(ledger.RoleGSTControl)},
			{Code: "2999", Name: "Suspense", Type: storage.Liability, Role: storage.AccountRole(ledger.RoleSuspense)},
		} {
			if have[string(a.Role)] {
				continue
			}
			if _, err := u.Accounts().Create(ctx, a); err != nil {
				return err
			}
		}
		return nil
	})
}

// newMoneyAccount makes an SGD money account in Banking on a new chart account.
func (e *env) newMoneyAccount() storage.MoneyAccount {
	e.t.Helper()
	acc := e.chartMoneyAccount()
	var m storage.MoneyAccount
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		m, err = u.Banking().CreateMoneyAccount(ctx, storage.NewMoneyAccount{AccountID: acc.ID, Kind: storage.MoneyBank, Name: acc.Name, Currency: "SGD", At: e.clock.Now()})
		return err
	})
	return m
}

// lineOn imports a statement of one line of amount on m, booked on 20 October: a possible duplicate of dup when it is set.
func (e *env) lineOn(m storage.MoneyAccount, amount string, dup *uuid.UUID) storage.BankLine {
	e.t.Helper()
	var lines []storage.BankLine
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		_, lines, err = u.Banking().Import(ctx, storage.NewBankStatement{MoneyAccountID: m.ID, Source: storage.MoneySourceFile, Format: storage.FormatCSV,
			ParserVersion: "csv@1", PeriodFrom: porttest.Day(10, 20), PeriodTo: porttest.Day(10, 20), Currency: "SGD", At: e.clock.Now(),
			Lines: []storage.NewBankLine{{Position: 1, BookingDate: porttest.Day(10, 20), Amount: storage.Decimal(amount), Description: "Line " + amount,
				Fingerprint: strings.ReplaceAll(uuid.NewString()+uuid.NewString(), "-", ""), DuplicateOf: dup}}})
		return err
	})
	return lines[0]
}

// bankLine is a waiting line of amount on a new money account, with the books it posts to.
func (e *env) bankLine(amount string) (storage.MoneyAccount, storage.BankLine) {
	e.t.Helper()
	e.bankBooks()
	m := e.newMoneyAccount()
	return m, e.lineOn(m, amount, nil)
}

// duplicateLine is a line flagged as a possible duplicate of another on its account.
func (e *env) duplicateLine() storage.BankLine {
	e.t.Helper()
	m, l := e.bankLine("-10.00")
	return e.lineOn(m, "-10.00", &l.ID)
}

func (e *env) bank() journal.BankTransactions { return journal.BankTransactions{Store: e.store} }

func (e *env) bankCtx() context.Context {
	return storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "bookkeeper"}})
}

// categorisedLine is a line of S$10.90 out categorised to rent at TX.
func (e *env) categorisedLine() storage.BankLine {
	e.t.Helper()
	_, l := e.bankLine("-10.90")
	c, err := e.bank().Categorise(e.bankCtx(), e.books.Org, journal.Categorisation{Basis: storage.BasisPerson, At: e.clock.Now(),
		Draft: banktransaction.Draft{LineID: l.ID, Parts: []banktransaction.Part{{AccountID: e.books.Rent.ID, GSTCode: "TX"}}}})
	if err != nil {
		e.t.Fatal(err)
	}
	return c.Line
}

// inTransit is a transfer of S$10.00 out of a new account to another, whose line has not arrived; it returns the
// transfer and the receiving account.
func (e *env) inTransit() (storage.BankTransfer, storage.MoneyAccount) {
	e.t.Helper()
	_, out := e.bankLine("-10.00")
	to := e.newMoneyAccount()
	ten, _ := money.Parse("10.00", money.MustLookup("SGD"))
	t, err := e.bank().RecordTransfer(e.bankCtx(), e.books.Org, journal.TransferRequest{FromLineID: &out.ID, ToAccountID: to.ID, Received: &ten,
		Basis: storage.BasisPerson, At: e.clock.Now()})
	if err != nil {
		e.t.Fatal(err)
	}
	return t.Transfer, to
}

func (e *env) bankLineNow(id uuid.UUID) storage.BankLine {
	e.t.Helper()
	var l storage.BankLine
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		l, err = u.Banking().Line(ctx, id)
		return err
	})
	return l
}

func (e *env) transferNow(id uuid.UUID) storage.BankTransfer {
	e.t.Helper()
	var t storage.BankTransfer
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		t, err = u.BankMatching().Transfer(ctx, id)
		return err
	})
	return t
}

func sgd(amount string) map[string]any { return map[string]any{"amount": amount, "currency": "SGD"} }

// lineIs says a line's status is still status: a refused call changed nothing.
func (e *env) lineIs(id uuid.UUID, status string) func() bool {
	return func() bool { return e.bankLineNow(id).Status == status }
}

// matrixBankLines are the matrix's rows of the line actions and transfers (QA-SEC-03).
var matrixBankLines = map[string]func(e *env, tok string) (response, func() bool){
	"categoriseBankLine": func(e *env, tok string) (response, func() bool) {
		_, l := e.bankLine("-10.90")
		r := e.call(http.MethodPost, "/v1/bank-lines/"+l.ID.String()+"/categorise", tok, map[string]any{
			"parts": []map[string]any{{"account_id": e.books.Rent.ID.String(), "gst_code": "TX"}}}, "If-Match", etag(l.Version))
		return r, e.lineIs(l.ID, storage.LineUnreconciled)
	},
	"suspendBankLine": func(e *env, tok string) (response, func() bool) {
		_, l := e.bankLine("-10.00")
		return e.call(http.MethodPost, "/v1/bank-lines/"+l.ID.String()+"/suspend", tok, nil, "If-Match", etag(l.Version)), e.lineIs(l.ID, storage.LineUnreconciled)
	},
	"matchBankLineInvoices": func(e *env, tok string) (response, func() bool) {
		inv, _ := e.issuedInvoice("1", "100.00")
		_, l := e.bankLine("10.00")
		r := e.call(http.MethodPost, "/v1/bank-lines/"+l.ID.String()+"/match-invoices", tok, map[string]any{"customer_id": e.customer(),
			"invoices": []map[string]any{{"invoice_id": inv, "amount": sgd("10.00")}}}, "If-Match", etag(l.Version))
		return r, e.lineIs(l.ID, storage.LineUnreconciled)
	},
	"matchBankLineBills": func(e *env, tok string) (response, func() bool) {
		bill, _ := e.postedBill("10.00", "0.90")
		_, l := e.bankLine("-5.00")
		r := e.call(http.MethodPost, "/v1/bank-lines/"+l.ID.String()+"/match-bills", tok, map[string]any{"supplier_id": e.supplier(),
			"bills": []map[string]any{{"bill_id": bill, "amount": sgd("5.00")}}}, "If-Match", etag(l.Version))
		return r, e.lineIs(l.ID, storage.LineUnreconciled)
	},
	"matchBankLineRecorded": func(e *env, tok string) (response, func() bool) {
		inv, _ := e.issuedInvoice("1", "100.00")
		m, l := e.bankLine("10.00")
		rec := e.call(http.MethodPost, "/v1/customer-receipts", e.as(authz.Accountant), map[string]any{"date": "2026-10-20",
			"money_account_id": m.AccountID.String(), "currency": "SGD", "invoices": []map[string]any{{"invoice_id": inv, "amount": sgd("10.00")}}})
		r := e.call(http.MethodPost, "/v1/bank-lines/"+l.ID.String()+"/match-recorded", tok, map[string]any{"document_id": rec.str("receipt", "id")},
			"If-Match", etag(l.Version))
		return r, e.lineIs(l.ID, storage.LineUnreconciled)
	},
	"keepBankLine": func(e *env, tok string) (response, func() bool) {
		l := e.duplicateLine()
		return e.call(http.MethodPost, "/v1/bank-lines/"+l.ID.String()+"/keep", tok, nil, "If-Match", etag(l.Version)), e.lineIs(l.ID, storage.LinePossibleDuplicate)
	},
	"excludeBankLine": func(e *env, tok string) (response, func() bool) {
		l := e.duplicateLine()
		r := e.call(http.MethodPost, "/v1/bank-lines/"+l.ID.String()+"/exclude", tok, map[string]any{"reason": "The same line twice"}, "If-Match", etag(l.Version))
		return r, e.lineIs(l.ID, storage.LinePossibleDuplicate)
	},
	"unreconcileBankLine": func(e *env, tok string) (response, func() bool) {
		l := e.categorisedLine()
		r := e.call(http.MethodPost, "/v1/bank-lines/"+l.ID.String()+"/unreconcile", tok, map[string]any{"reason": "Coded to the wrong account"},
			"If-Match", etag(l.Version))
		return r, e.lineIs(l.ID, storage.LineCategorised)
	},
	"autoReconcileBankLines": func(e *env, tok string) (response, func() bool) {
		t, to := e.inTransit()
		in := e.lineOn(to, "10.00", nil)
		r := e.call(http.MethodPost, "/v1/bank-lines:auto-reconcile", tok, map[string]any{"money_account_id": to.ID.String()})
		return r, func() bool { return e.transferNow(t.ID).ToLineID == nil && e.lineIs(in.ID, storage.LineUnreconciled)() }
	},
	"recordBankTransfer": func(e *env, tok string) (response, func() bool) {
		_, out := e.bankLine("-10.00")
		_, in := e.bankLine("10.00")
		r := e.call(http.MethodPost, "/v1/bank-transfers", tok, map[string]any{"from_line_id": out.ID.String(), "to_line_id": in.ID.String()})
		return r, func() bool {
			return e.lineIs(out.ID, storage.LineUnreconciled)() && e.lineIs(in.ID, storage.LineUnreconciled)()
		}
	},
	"listBankTransfers": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/bank-transfers", tok, nil), nil
	},
	"getBankTransfer": func(e *env, tok string) (response, func() bool) {
		t, _ := e.inTransit()
		return e.call(http.MethodGet, "/v1/bank-transfers/"+t.ID.String(), tok, nil), nil
	},
	"completeBankTransfer": func(e *env, tok string) (response, func() bool) {
		t, to := e.inTransit()
		in := e.lineOn(to, "10.00", nil)
		r := e.call(http.MethodPost, "/v1/bank-transfers/"+t.ID.String()+"/complete", tok, map[string]any{"line_id": in.ID.String()}, "If-Match", etag(t.Version))
		return r, e.lineIs(in.ID, storage.LineUnreconciled)
	},
	"undoBankTransfer": func(e *env, tok string) (response, func() bool) {
		t, _ := e.inTransit()
		r := e.call(http.MethodPost, "/v1/bank-transfers/"+t.ID.String()+"/undo", tok, map[string]any{"reason": "Two payments, not a transfer"},
			"If-Match", etag(t.Version))
		return r, func() bool { return e.transferNow(t.ID).UndoneAt == nil }
	},
}

// FR-BNK-04…09 and FR-GL-12 through the API (p1-bank-import-reconcile task 9.1): a line categorised after a preview
// that saves nothing, with a stale ETag refused; put back in the queue with its reason, and refused when it waits
// already; put in suspense and classified from there; possible duplicates kept or excluded, and an excluded line
// included again; money in matched to an invoice in part and the receipt voided by an undo; money in matched to bills
// refused; a transfer recorded with both lines, undone once; a transfer in transit listed and completed by its line;
// those who only view refused.
// req: FR-BNK-04
// req: FR-BNK-05
// req: FR-BNK-06
// req: FR-BNK-07
// req: FR-BNK-09
// req: FR-GL-12
// req: FR-INT-01
func TestBankLinesThroughTheAPI(t *testing.T) {
	e := newEnv(t)
	bk := e.as(authz.Bookkeeper)
	at := func(l storage.BankLine, action string) string {
		return "/v1/bank-lines/" + l.ID.String() + "/" + action
	}

	// Categorised, after a preview.
	_, l := e.bankLine("-10.90")
	rent := map[string]any{"parts": []map[string]any{{"account_id": e.books.Rent.ID.String(), "gst_code": "TX", "description": "October rent"}}}
	pv := e.call(http.MethodPost, at(l, "categorise"), bk, rent, "If-Match", etag(l.Version), "Prefer", "return=preview")
	if pv.Status != 200 || pv.Header.Get("Preference-Applied") != "return=preview" || pv.Body["entry"] == nil || e.bankLineNow(l.ID).Status != storage.LineUnreconciled {
		t.Fatalf("the preview: %d %s", pv.Status, pv.Raw)
	}
	c := e.call(http.MethodPost, at(l, "categorise"), bk, rent, "If-Match", etag(l.Version))
	if c.Status != 200 || c.str("line", "status") != storage.LineCategorised || c.str("line", "match", "kind") != storage.MatchCategorised ||
		c.str("document_id") == "" || c.str("entry", "id") == "" || c.Header.Get("ETag") != etag(l.Version+1) {
		t.Fatalf("categorising: %d %s", c.Status, c.Raw)
	}
	e.problemIs(e.call(http.MethodPost, at(l, "categorise"), bk, rent, "If-Match", etag(l.Version)), "categoriseBankLine", 412, CodeStale)
	e.problemIs(e.call(http.MethodPost, at(l, "categorise"), e.as(authz.ARSpecialist), rent, "If-Match", c.Header.Get("ETag")), "categoriseBankLine", 403, CodeForbidden)

	// Back in the queue, with a reason; once.
	u := e.call(http.MethodPost, at(l, "unreconcile"), bk, map[string]any{"reason": "Coded to the wrong account"}, "If-Match", c.Header.Get("ETag"))
	if u.Status != 200 || u.str("line", "status") != storage.LineUnreconciled || u.Body["line"].(map[string]any)["match"] != nil {
		t.Fatalf("the undo: %d %s", u.Status, u.Raw)
	}
	e.problemIs(e.call(http.MethodPost, at(l, "unreconcile"), bk, map[string]any{"reason": "Again"}, "If-Match", u.Header.Get("ETag")),
		"unreconcileBankLine", 409, banking.CodeLineState)

	// Suspense, then classified.
	s := e.call(http.MethodPost, at(l, "suspend"), bk, nil, "If-Match", u.Header.Get("ETag"))
	if s.Status != 200 || s.str("line", "status") != storage.LineSuspense || s.str("line", "match", "kind") != storage.MatchSuspense {
		t.Fatalf("suspense: %d %s", s.Status, s.Raw)
	}
	e.problemIs(e.call(http.MethodPost, at(l, "unreconcile"), bk, map[string]any{"reason": "Not known"}, "If-Match", s.Header.Get("ETag")),
		"unreconcileBankLine", 409, journal.CodeNotCategory)
	if r := e.call(http.MethodPost, at(l, "categorise"), bk, rent, "If-Match", s.Header.Get("ETag")); r.Status != 200 || r.str("line", "status") != storage.LineCategorised {
		t.Fatalf("classifying it: %d %s", r.Status, r.Raw)
	}

	// Possible duplicates.
	d := e.duplicateLine()
	if k := e.call(http.MethodPost, at(d, "keep"), bk, nil, "If-Match", etag(d.Version)); k.Status != 200 || k.str("line", "status") != storage.LineUnreconciled {
		t.Fatalf("keeping it: %d %s", k.Status, k.Raw)
	} else {
		e.problemIs(e.call(http.MethodPost, at(d, "keep"), bk, nil, "If-Match", k.Header.Get("ETag")), "keepBankLine", 409, banking.CodeLineState)
	}
	d = e.duplicateLine()
	x := e.call(http.MethodPost, at(d, "exclude"), bk, map[string]any{"reason": "The same line twice"}, "If-Match", etag(d.Version))
	if x.Status != 200 || x.str("line", "status") != storage.LineExcluded {
		t.Fatalf("excluding it: %d %s", x.Status, x.Raw)
	}
	if i := e.call(http.MethodPost, at(d, "unreconcile"), bk, map[string]any{"reason": "Not a duplicate after all"}, "If-Match", x.Header.Get("ETag")); i.Status != 200 ||
		i.str("line", "status") != storage.LineUnreconciled {
		t.Fatalf("including it again: %d %s", i.Status, i.Raw)
	}

	// Money in matched to an invoice in part; the undo voids the receipt.
	inv, _ := e.issuedInvoice("1", "100.00")
	_, in := e.bankLine("10.00")
	m := e.call(http.MethodPost, at(in, "match-invoices"), bk, map[string]any{"customer_id": e.customer(),
		"invoices": []map[string]any{{"invoice_id": inv, "amount": sgd("10.00")}}}, "If-Match", etag(in.Version))
	if m.Status != 200 || m.str("line", "status") != storage.LineMatched || m.str("line", "match", "kind") != storage.MatchReceipt || m.str("document_id") == "" {
		t.Fatalf("matching an invoice: %d %s", m.Status, m.Raw)
	}
	receiptID := m.str("document_id")
	if r := e.call(http.MethodPost, at(in, "unreconcile"), bk, map[string]any{"reason": "Paid by another customer"}, "If-Match", m.Header.Get("ETag")); r.Status != 200 {
		t.Fatalf("undoing the match: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodGet, "/v1/customer-receipts/"+receiptID, e.as(authz.Accountant), nil); r.str("status") != "reversed" {
		t.Errorf("the receipt after the undo: %s", r.Raw)
	}
	bill, _ := e.postedBill("10.00", "0.90")
	e.problemIs(e.call(http.MethodPost, at(in, "match-bills"), bk, map[string]any{"supplier_id": e.supplier(),
		"bills": []map[string]any{{"bill_id": bill, "amount": sgd("10.00")}}}, "If-Match", etag(in.Version+2)), "matchBankLineBills", 422, journal.CodeDirection)

	// A transfer with both lines, undone once.
	_, out := e.bankLine("-25.00")
	_, arrived := e.bankLine("25.00")
	tr := e.call(http.MethodPost, "/v1/bank-transfers", bk, map[string]any{"from_line_id": out.ID.String(), "to_line_id": arrived.ID.String()})
	if tr.Status != 201 || tr.str("transfer", "status") != "complete" || tr.str("entry", "id") == "" || len(tr.Body["lines"].([]any)) != 2 ||
		e.bankLineNow(out.ID).Status != storage.LineTransferred {
		t.Fatalf("the transfer: %d %s", tr.Status, tr.Raw)
	}
	id := tr.str("transfer", "id")
	un := e.call(http.MethodPost, "/v1/bank-transfers/"+id+"/undo", bk, map[string]any{"reason": "Two payments, not a transfer"}, "If-Match", tr.Header.Get("ETag"))
	if un.Status != 200 || un.str("transfer", "status") != "undone" || e.bankLineNow(arrived.ID).Status != storage.LineUnreconciled {
		t.Fatalf("undoing the transfer: %d %s", un.Status, un.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/bank-transfers/"+id+"/undo", bk, map[string]any{"reason": "Again"}, "If-Match", un.Header.Get("ETag")),
		"undoBankTransfer", 409, journal.CodeTransferUndone)

	// In transit, then completed by its line.
	_, out = e.bankLine("-40.00")
	to := e.newMoneyAccount()
	it := e.call(http.MethodPost, "/v1/bank-transfers", bk, map[string]any{"from_line_id": out.ID.String(), "to_money_account_id": to.ID.String(),
		"received": sgd("40.00")})
	if it.Status != 201 || it.str("transfer", "status") != "in_transit" {
		t.Fatalf("in transit: %d %s", it.Status, it.Raw)
	}
	if list := e.call(http.MethodGet, "/v1/bank-transfers", e.as(authz.Auditor), nil); !strings.Contains(list.Raw, it.str("transfer", "id")) {
		t.Errorf("the transfers in transit: %s", list.Raw)
	}
	line := e.lineOn(to, "40.00", nil)
	done := e.call(http.MethodPost, "/v1/bank-transfers/"+it.str("transfer", "id")+"/complete", bk, map[string]any{"line_id": line.ID.String()},
		"If-Match", it.Header.Get("ETag"))
	if done.Status != 200 || done.str("transfer", "status") != "complete" || e.bankLineNow(line.ID).Status != storage.LineTransferred {
		t.Fatalf("completing it: %d %s", done.Status, done.Raw)
	}
}

// Reconcile now (FR-BNK-14; the owner's decision of 26 Oct 2026, rev 0.143): lines that arrive by feed wait in the
// queue until a person asks; a preview says what it would do and posts nothing; the run reconciles a transfer's
// arriving side as the system principal, for the person who asked; a second run finds nothing waiting to reconcile.
// req: FR-BNK-14
// req: banking/bank-transactions Lines that arrive by feed wait for Reconcile now
func TestReconcileNowThroughTheAPI(t *testing.T) {
	e := newEnv(t)
	bk := e.as(authz.Bookkeeper)
	tr, to := e.inTransit()
	line := e.lineOn(to, "10.00", nil)
	body := map[string]any{"money_account_id": to.ID.String()}
	pv := e.call(http.MethodPost, "/v1/bank-lines:auto-reconcile", bk, body, "Prefer", "return=preview")
	if pv.Status != 200 || pv.Header.Get("Preference-Applied") != "return=preview" || pv.Body["reconciled"] != 1.0 || e.bankLineNow(line.ID).Status != storage.LineUnreconciled {
		t.Fatalf("the preview: %d %s", pv.Status, pv.Raw)
	}
	r := e.call(http.MethodPost, "/v1/bank-lines:auto-reconcile", bk, body)
	if r.Status != 200 || r.Body["reconciled"] != 1.0 || e.bankLineNow(line.ID).Status != storage.LineTransferred || e.transferNow(tr.ID).ToLineID == nil {
		t.Fatalf("reconciling now: %d %s", r.Status, r.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/bank-lines/"+line.ID.String(), bk, nil); g.str("match", "basis") != storage.BasisTransfer ||
		g.str("match", "made_by") != "service:bank.auto-reconcile" {
		t.Errorf("the match: %s", g.Raw)
	}
	if again := e.call(http.MethodPost, "/v1/bank-lines:auto-reconcile", bk, body); again.Status != 200 || again.Body["reconciled"] != 0.0 {
		t.Errorf("a second run: %d %s", again.Status, again.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/bank-lines:auto-reconcile", e.as(authz.Auditor), body), "autoReconcileBankLines", 403, CodeForbidden)
}
