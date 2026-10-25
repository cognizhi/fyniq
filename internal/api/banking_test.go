//go:build local

package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// moneyCodes numbers the chart accounts the banking tests make, so each code is new.
var moneyCodes atomic.Int32

func (e *env) do(fn func(ctx context.Context, u storage.UnitOfWork) error) {
	e.t.Helper()
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "bookkeeper"}})
	if err := e.store.Do(ctx, e.books.Org, fn); err != nil {
		e.t.Fatal(err)
	}
}

// chartMoneyAccount makes a chart account marked a money account, in SGD, with no money account in Banking yet.
func (e *env) chartMoneyAccount() storage.Account {
	e.t.Helper()
	var a storage.Account
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		n := moneyCodes.Add(1)
		a, err = u.Accounts().Create(ctx, storage.NewAccount{Code: fmt.Sprintf("1%04d", n), Name: fmt.Sprintf("Bank %d", n), Type: storage.Asset,
			AccountAttributes: storage.AccountAttributes{MoneyAccount: true, Currency: "SGD"}})
		return err
	})
	return a
}

// bankAccount makes a money account in Banking, with one statement of one line, booked on 20 October, and a gap.
func (e *env) bankAccount() (storage.MoneyAccount, storage.BankLine, storage.BankGap) {
	e.t.Helper()
	acc := e.chartMoneyAccount()
	var m storage.MoneyAccount
	var lines []storage.BankLine
	var gap storage.BankGap
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if m, err = u.Banking().CreateMoneyAccount(ctx, storage.NewMoneyAccount{AccountID: acc.ID, Kind: storage.MoneyBank, Name: acc.Name,
			Identifier: "072-" + acc.Code, Currency: "SGD", At: e.clock.Now()}); err != nil {
			return err
		}
		opening, found := storage.Decimal("10.00"), storage.Decimal("12.00")
		if _, lines, err = u.Banking().Import(ctx, storage.NewBankStatement{MoneyAccountID: m.ID, Source: storage.MoneySourceFile, Format: storage.FormatCSV,
			ParserVersion: "csv@1", PeriodFrom: porttest.Day(10, 20), PeriodTo: porttest.Day(10, 20), Currency: "SGD", Opening: &found, At: e.clock.Now(),
			Gap: &storage.NewBankGap{BeforeDate: porttest.Day(10, 20), ExpectedOpening: &opening, FoundOpening: &found},
			Lines: []storage.NewBankLine{{Position: 1, BookingDate: porttest.Day(10, 20), Amount: "-10.00", Description: "AWS EMEA",
				Fingerprint: strings.Repeat(fmt.Sprintf("%x", acc.Code[1:]), 64)[:64]}}}); err != nil {
			return err
		}
		gaps, err := u.Banking().Gaps(ctx, m.ID, true)
		if err == nil && len(gaps) > 0 {
			gap = gaps[0]
		}
		return err
	})
	return m, lines[0], gap
}

// mapping saves a CSV column mapping: day/month/year dates, a signed amount and a description.
func (e *env) mapping() storage.StatementMapping {
	e.t.Helper()
	var m storage.StatementMapping
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		m, err = u.Banking().SaveMapping(ctx, storage.NewStatementMapping{Name: "Matrix " + uuid.NewString()[:8], Format: "csv",
			Spec: []byte(`{"date_formats":["02/01/2006"],"date":"Date","amount":"Amount","description":["Description"]}`), At: e.clock.Now()})
		return err
	})
	return m
}

func (e *env) bankRows(table string, where string, args ...any) int {
	e.t.Helper()
	n := 0
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		switch table {
		case "sources":
			s, err := u.Banking().Sources(ctx, args[0].(uuid.UUID))
			n = len(s)
			return err
		case "statements":
			s, err := u.Banking().Statements(ctx, args[0].(uuid.UUID))
			n = len(s)
			return err
		case "registers":
			all, err := u.Banking().MoneyAccounts(ctx)
			for _, m := range all {
				if m.AccountID == args[0].(uuid.UUID) {
					n++
				}
			}
			return err
		case "mappings":
			all, err := u.Banking().Mappings(ctx)
			for _, m := range all {
				if m.Name == where {
					n++
				}
			}
			return err
		case "open gaps":
			g, err := u.Banking().Gaps(ctx, args[0].(uuid.UUID), true)
			n = len(g)
			return err
		}
		return fmt.Errorf("no table %s", table)
	})
	return n
}

// matrixBanking are the authorisation matrix's rows of the banking operations (QA-SEC-03): each allowed call succeeds,
// and each refused one changes nothing.
var matrixBanking = map[string]func(e *env, tok string) (response, func() bool){
	"listMoneyAccounts": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/money-accounts", tok, nil), nil
	},
	"createMoneyAccount": func(e *env, tok string) (response, func() bool) {
		acc := e.chartMoneyAccount()
		r := e.call(http.MethodPost, "/v1/money-accounts", tok, map[string]any{"account_id": acc.ID.String(), "kind": "bank", "name": acc.Name})
		return r, func() bool { return e.bankRows("registers", "", acc.ID) == 0 }
	},
	"getMoneyAccount": func(e *env, tok string) (response, func() bool) {
		m, _, _ := e.bankAccount()
		return e.call(http.MethodGet, "/v1/money-accounts/"+m.ID.String(), tok, nil), nil
	},
	"replaceMoneyAccount": func(e *env, tok string) (response, func() bool) {
		m, _, _ := e.bankAccount()
		r := e.call(http.MethodPut, "/v1/money-accounts/"+m.ID.String(), tok, map[string]any{"kind": "bank", "name": "Matrix changed"}, "If-Match", etag(m.Version))
		return r, func() bool {
			g := e.call(http.MethodGet, "/v1/money-accounts/"+m.ID.String(), e.as(authz.Accountant), nil)
			return g.str("name") == m.Name
		}
	},
	"addMoneyAccountSource": func(e *env, tok string) (response, func() bool) {
		m, _, _ := e.bankAccount()
		r := e.call(http.MethodPost, "/v1/money-accounts/"+m.ID.String()+"/sources", tok, map[string]any{"kind": "file", "from_date": "2026-12-01",
			"report_format": "camt053"}, "If-Match", etag(m.Version))
		return r, func() bool { return e.bankRows("sources", "", m.ID) == 0 }
	},
	"resolveBankGap": func(e *env, tok string) (response, func() bool) {
		m, _, g := e.bankAccount()
		r := e.call(http.MethodPost, "/v1/money-accounts/"+m.ID.String()+"/gaps/"+g.ID.String()+"/resolve", tok, map[string]any{"resolution": "Matrix"})
		return r, func() bool { return e.bankRows("open gaps", "", m.ID) == 1 }
	},
	"listStatementMappings": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/statement-mappings", tok, nil), nil
	},
	"createStatementMapping": func(e *env, tok string) (response, func() bool) {
		name := "Matrix " + uuid.NewString()[:8]
		r := e.call(http.MethodPost, "/v1/statement-mappings", tok, map[string]any{"name": name, "format": "csv",
			"spec": map[string]any{"date_formats": []string{"02/01/2006"}, "date": "Date", "amount": "Amount", "number": map[string]any{}}})
		return r, func() bool { return e.bankRows("mappings", name) == 0 }
	},
	"replaceStatementMapping": func(e *env, tok string) (response, func() bool) {
		m := e.mapping()
		name := "Matrix " + uuid.NewString()[:8]
		r := e.call(http.MethodPut, "/v1/statement-mappings/"+m.ID.String(), tok, map[string]any{"name": name, "format": "csv",
			"spec": map[string]any{"date_formats": []string{"02/01/2006"}, "date": "Date", "amount": "Amount", "number": map[string]any{}}}, "If-Match", etag(m.Version))
		return r, func() bool { return e.bankRows("mappings", name) == 0 }
	},
	"importBankStatements": func(e *env, tok string) (response, func() bool) {
		m, _, _ := e.bankAccount()
		mp := e.mapping()
		r := e.upload("/v1/bank-imports", tok, map[string]string{"format": "csv", "money_account_id": m.ID.String(), "mapping_id": mp.ID.String()},
			"statement.csv", []byte("Date,Amount,Description\r\n21/10/2026,-25.00,"+uuid.NewString()[:8]+"\r\n"))
		return r, func() bool { return e.bankRows("statements", "", m.ID) == 1 }
	},
	"listBankStatements": func(e *env, tok string) (response, func() bool) {
		m, _, _ := e.bankAccount()
		return e.call(http.MethodGet, "/v1/money-accounts/"+m.ID.String()+"/statements", tok, nil), nil
	},
	"listBankLines": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/bank-lines?status=waiting", tok, nil), nil
	},
	"addMoneyAccountEvidence": func(e *env, tok string) (response, func() bool) {
		m, _, _ := e.bankAccount()
		path := "/v1/money-accounts/" + m.ID.String() + "/evidence"
		r := e.upload(path, tok, map[string]string{"evidence_type": "agreement"}, "agreement.pdf", []byte("%PDF-1.7\n% an agreement\n"), "If-Match", etag(m.Version))
		return r, func() bool {
			items, _ := e.call(http.MethodGet, path, e.as(authz.Auditor), nil).Body["items"].([]any)
			return len(items) == 0
		}
	},
	"listMoneyAccountEvidence": func(e *env, tok string) (response, func() bool) {
		m, _, _ := e.bankAccount()
		return e.call(http.MethodGet, "/v1/money-accounts/"+m.ID.String()+"/evidence", tok, nil), nil
	},
	"getBankLine": func(e *env, tok string) (response, func() bool) {
		_, l, _ := e.bankAccount()
		return e.call(http.MethodGet, "/v1/bank-lines/"+l.ID.String(), tok, nil), nil
	},
}

// FR-BNK-01, FR-BNK-02, FR-BNK-04, FR-BNK-14 and FR-BNK-16 through the API (p1-bank-import-reconcile task 9.1): a money
// account set up on a chart account, its identifier shown by its last four characters; a mapping saved; a CSV
// statement previewed (nothing saved), imported, and imported again (every line dropped on proof by its running
// balance, or kept as a possible duplicate); the file kept; the lines listed and read with their suggestions; a source
// from a later day; a gap resolved; refusals with their codes.
// req: FR-BNK-01
// req: FR-BNK-02
// req: FR-BNK-04
// req: FR-BNK-16
// req: FR-INT-01
// req: SEC-15
// asvs: V16.2.5
func TestBankingThroughTheAPI(t *testing.T) {
	e := newEnv(t)
	bookkeeper, admin := e.as(authz.Bookkeeper), e.as(authz.Administrator)
	acc := e.chartMoneyAccount()
	c := e.call(http.MethodPost, "/v1/money-accounts", bookkeeper, map[string]any{"account_id": acc.ID.String(), "kind": "bank", "name": "DBS current",
		"institution": "DBS", "identifier": "072-123456-7"})
	if c.Status != 201 || c.str("identifier") != "…4567" || c.str("currency") != "SGD" || c.str("account_code") != acc.Code || c.Header.Get("ETag") == "" {
		t.Fatalf("setting up the money account: %d %s", c.Status, c.Raw)
	}
	id, tag := c.str("id"), c.Header.Get("ETag")
	if strings.Contains(c.Raw, "123456") {
		t.Errorf("the identifier shown in full: %s", c.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/money-accounts", bookkeeper, map[string]any{"account_id": acc.ID.String(), "kind": "bank", "name": "Again"}),
		"createMoneyAccount", 409, storage.CodeDuplicate)

	mp := e.call(http.MethodPost, "/v1/statement-mappings", bookkeeper, map[string]any{"name": "DBS IDEAL CSV", "institution": "DBS", "format": "csv",
		"spec": map[string]any{"date_formats": []string{"02/01/2006"}, "date": "Date", "amount": "Amount", "description": []string{"Description"},
			"balance": "Balance", "number": map[string]any{"thousands": ","}}})
	if mp.Status != 201 {
		t.Fatalf("saving the mapping: %d %s", mp.Status, mp.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/statement-mappings", bookkeeper, map[string]any{"name": "No amount", "format": "csv",
		"spec": map[string]any{"date_formats": []string{"02/01/2006"}, "date": "Date", "number": map[string]any{}}}), "createStatementMapping", 422, "BANKFILE-06")

	csv := []byte("Date,Description,Amount,Balance\r\n20/10/2026,GIRO ACME,\"1,090.00\",\"1,090.00\"\r\n21/10/2026,AWS EMEA,-20.00,\"1,070.00\"\r\n")
	fields := map[string]string{"format": "csv", "money_account_id": id, "mapping_id": mp.str("id")}
	pv := e.upload("/v1/bank-imports", bookkeeper, fields, "dbs.csv", csv, "Prefer", "return=preview")
	if pv.Status != 200 || pv.Header.Get("Preference-Applied") != "return=preview" || !strings.Contains(pv.Raw, `"added":2`) {
		t.Fatalf("the preview: %d %s", pv.Status, pv.Raw)
	}
	if n := e.bankRows("statements", "", uuid.MustParse(id)); n != 0 {
		t.Fatalf("a preview saved %d statement(s)", n)
	}
	im := e.upload("/v1/bank-imports", bookkeeper, fields, "dbs.csv", csv)
	if im.Status != 201 || !strings.Contains(im.Raw, `"added":2`) || !strings.Contains(im.Raw, `"auto_reconciled":{"reconciled":0`) {
		t.Fatalf("the import: %d %s", im.Status, im.Raw)
	}
	again := e.upload("/v1/bank-imports", bookkeeper, fields, "dbs.csv", csv)
	if again.Status != 201 || !strings.Contains(again.Raw, `"dropped":2`) || !strings.Contains(again.Raw, `"outcome":"duplicate"`) {
		t.Fatalf("the same statement again: %d %s", again.Status, again.Raw)
	}
	e.problemIs(e.upload("/v1/bank-imports", bookkeeper, map[string]string{"format": "csv", "money_account_id": id, "mapping_id": mp.str("id")}, "bad.csv",
		[]byte("Date,Description,Amount,Balance\r\n31/02/2026,X,1.00,1.00\r\n")), "importBankStatements", 422, "BANKFILE-04")
	e.problemIs(e.upload("/v1/bank-imports", bookkeeper, map[string]string{"format": "csv", "money_account_id": id}, "dbs.csv", csv),
		"importBankStatements", 422, "BANKFILE-06")
	e.problemIs(e.call(http.MethodPost, "/v1/statement-mappings", bookkeeper, map[string]any{"name": "DBS IDEAL CSV", "format": "csv",
		"spec": map[string]any{"date_formats": []string{"02/01/2006"}, "date": "Date", "amount": "Amount", "number": map[string]any{}}}),
		"createStatementMapping", 409, storage.CodeDuplicate)

	st := e.call(http.MethodGet, "/v1/money-accounts/"+id+"/statements", bookkeeper, nil)
	if items, _ := st.Body["items"].([]any); st.Status != 200 || len(items) != 1 || !strings.Contains(st.Raw, `"has_original":true`) {
		t.Fatalf("the statements: %d %s", st.Status, st.Raw)
	}
	lines := e.call(http.MethodGet, "/v1/bank-lines?money_account_id="+id+"&status=waiting&page_size=1", bookkeeper, nil)
	items, _ := lines.Body["items"].([]any)
	if lines.Status != 200 || len(items) != 1 || lines.str("next_cursor") == "" {
		t.Fatalf("the queue's first page: %d %s", lines.Status, lines.Raw)
	}
	next := e.call(http.MethodGet, "/v1/bank-lines?money_account_id="+id+"&status=waiting&page_size=1&cursor="+lines.str("next_cursor"), bookkeeper, nil)
	if items, _ := next.Body["items"].([]any); len(items) != 1 || next.str("next_cursor") != "" {
		t.Fatalf("the queue's second page: %s", next.Raw)
	}
	first := items[0].(map[string]any)
	g := e.call(http.MethodGet, "/v1/bank-lines/"+first["id"].(string), bookkeeper, nil)
	if g.Status != 200 || g.str("status") != "unreconciled" || g.Body["suggestions"] == nil || g.Header.Get("ETag") == "" {
		t.Fatalf("a line: %d %s", g.Status, g.Raw)
	}

	// A source from a later day: emailed reports need a right on "Bank data… connections".
	src := map[string]any{"kind": "email", "from_date": "2026-11-01", "senders": []string{"ideal@dbs.example"}, "report_format": "csv",
		"mapping_id": mp.str("id")}
	e.problemIs(e.call(http.MethodPost, "/v1/money-accounts/"+id+"/sources", bookkeeper, src, "If-Match", tag), "addMoneyAccountSource", 403, CodeForbidden)
	s := e.call(http.MethodPost, "/v1/money-accounts/"+id+"/sources", admin, src, "If-Match", tag)
	if s.Status != 201 || s.str("source", "kind") != "email" || s.str("source", "from_date") != "2026-11-01" {
		t.Fatalf("the emailed reports: %d %s", s.Status, s.Raw)
	}
	src["from_date"] = "2026-10-01"
	e.problemIs(e.call(http.MethodPost, "/v1/money-accounts/"+id+"/sources", admin, src, "If-Match", tag), "addMoneyAccountSource", 409, storage.CodeBanking)

	// Changed; closed; a gap resolved.
	r := e.call(http.MethodPut, "/v1/money-accounts/"+id, bookkeeper, map[string]any{"kind": "bank", "name": "DBS current (SGD)"}, "If-Match", tag)
	if r.Status != 200 || r.str("name") != "DBS current (SGD)" || r.str("identifier") != "…4567" {
		t.Fatalf("changing it: %d %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodPut, "/v1/money-accounts/"+id, bookkeeper, map[string]any{"kind": "bank", "name": "Stale"}, "If-Match", tag),
		"replaceMoneyAccount", 412, CodeStale)
	m, _, gap := e.bankAccount()
	res := e.call(http.MethodPost, "/v1/money-accounts/"+m.ID.String()+"/gaps/"+gap.ID.String()+"/resolve", bookkeeper,
		map[string]any{"resolution": "The bank's statement for 19 October imported"})
	if res.Status != 200 || res.str("resolution") == "" {
		t.Fatalf("resolving the gap: %d %s", res.Status, res.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/money-accounts/"+m.ID.String()+"/gaps/"+gap.ID.String()+"/resolve", bookkeeper,
		map[string]any{"resolution": "Again"}), "resolveBankGap", 409, storage.CodeBanking)
	if l := e.call(http.MethodGet, "/v1/money-accounts", e.as(authz.Owner), nil); l.Status != 200 || !strings.Contains(l.Raw, `"name":"DBS current (SGD)"`) {
		t.Errorf("the list: %d %s", l.Status, l.Raw)
	}
}
