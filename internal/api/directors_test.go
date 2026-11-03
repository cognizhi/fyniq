//go:build local

package api

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// directorChart gives e's chart the director's accounts, 2500 and 2120, once, and returns their IDs.
func (e *env) directorChart() (loan, deferred string) {
	e.t.Helper()
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		all, err := u.Accounts().List(ctx)
		if err != nil {
			return err
		}
		for _, a := range all {
			switch a.Role {
			case ledger.RoleDirectorLoan:
				loan = a.ID.String()
			case ledger.RoleDeferredRemuneration:
				deferred = a.ID.String()
			}
		}
		if loan == "" {
			a, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "2500", Name: "Loan from director / shareholder", Type: storage.Liability,
				Role: ledger.RoleDirectorLoan})
			if err != nil {
				return err
			}
			loan = a.ID.String()
		}
		if deferred == "" {
			a, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "2120", Name: "Deferred remuneration payable", Type: storage.Liability,
				Role: ledger.RoleDeferredRemuneration})
			if err != nil {
				return err
			}
			deferred = a.ID.String()
		}
		return nil
	})
	return loan, deferred
}

// directorContact adds a contact marked director and shareholder, as an Accountant, and returns its ID.
func (e *env) directorContact(name string) string {
	e.t.Helper()
	r := e.call(http.MethodPost, "/v1/contacts", e.as(authz.Accountant), map[string]any{"legal_name": name, "director": true, "shareholder": true})
	if r.Status != http.StatusCreated || r.Body["director"] != true || r.Body["shareholder"] != true {
		e.t.Fatalf("createContact of a director = %d: %s", r.Status, r.Raw)
	}
	return r.str("id")
}

// founderLine is a journal line; party and origin name the person of a line on a director's account.
func founderLine(account, amount, party, origin string) map[string]any {
	l := map[string]any{"account_id": account, "amount": map[string]any{"amount": amount, "currency": "SGD"}}
	if party != "" {
		l["party_id"] = party
	}
	if origin != "" {
		l["origin"] = origin
	}
	return l
}

// postFounderJournal saves and posts a journal of lines on date as an Accountant, and returns the post's answer.
func (e *env) postFounderJournal(date string, lines ...map[string]any) response {
	e.t.Helper()
	r := e.call(http.MethodPost, "/v1/journals", e.as(authz.Accountant), map[string]any{"date": date, "memo": "Founders", "lines": lines})
	if r.Status != http.StatusCreated {
		e.t.Fatalf("createJournal = %d: %s", r.Status, r.Raw)
	}
	return e.call(http.MethodPost, "/v1/journals/"+r.str("id")+"/post", e.as(authz.Accountant), nil, "If-Match", r.Header.Get("ETag"))
}

// The director accounts (FR-EQ-01, FR-EQ-11, FR-EQ-14; PRD-001 rev 0.167), worked by hand. Mei Lin lends S$1,500.00 on 1
// October and is repaid S$400.00 on 5 October: the company owes her S$1,100.00, a credit of −1,100.00 on 2500. Arjun
// pays S$300.00 of rent personally on 7 October and is paid S$500.00 on 8 October: he owes the company S$200.00, a debit,
// so his account is in debit. A line on 2500 with no person, or a person on the bank's line, never posts. Mei Lin's
// statement from 3 October opens at −1,500.00, moves +400.00 paid out and closes at −1,100.00. (Each journal is below the
// self-approval threshold of S$2,000.00, so it posts at once.)
// req: FR-EQ-01
// req: FR-EQ-11
// req: FR-EQ-14
// req: QA-RPT-01
// req: equity/founders-money Statement per person
// req: equity/founders-money Every line carries the person
func TestDirectorAccountsAndTheirStatements(t *testing.T) {
	e := newEnv(t)
	loan, _ := e.directorChart()
	bank, rent := e.books.Bank.ID.String(), e.books.Rent.ID.String()
	mei, arjun := e.directorContact("Mei Lin Tan"), e.directorContact("Arjun Rao")
	customer := e.customer()

	for _, x := range []struct {
		date  string
		lines []map[string]any
	}{
		{"2026-10-01", []map[string]any{founderLine(bank, "1500.00", "", ""), founderLine(loan, "-1500.00", mei, ledger.OriginLent)}},
		{"2026-10-05", []map[string]any{founderLine(loan, "400.00", mei, ledger.OriginPaidOut), founderLine(bank, "-400.00", "", "")}},
		{"2026-10-07", []map[string]any{founderLine(rent, "300.00", "", ""), founderLine(loan, "-300.00", arjun, ledger.OriginPaidPersonally)}},
		{"2026-10-08", []map[string]any{founderLine(loan, "500.00", arjun, ledger.OriginPaidOut), founderLine(bank, "-500.00", "", "")}},
	} {
		if r := e.postFounderJournal(x.date, x.lines...); r.Status != http.StatusOK {
			t.Fatalf("post %s = %d: %s", x.date, r.Status, r.Raw)
		}
	}
	for name, lines := range map[string][]map[string]any{
		"no person":         {founderLine(bank, "10.00", "", ""), founderLine(loan, "-10.00", "", "")},
		"a customer":        {founderLine(bank, "10.00", "", ""), founderLine(loan, "-10.00", customer, ledger.OriginLent)},
		"no origin":         {founderLine(bank, "10.00", "", ""), founderLine(loan, "-10.00", mei, "")},
		"a person on bank":  {founderLine(bank, "10.00", mei, ledger.OriginLent), founderLine(loan, "-10.00", mei, ledger.OriginLent)},
		"an unknown origin": {founderLine(bank, "10.00", "", ""), founderLine(loan, "-10.00", mei, "gift")},
	} {
		r := e.call(http.MethodPost, "/v1/journals", e.as(authz.Accountant), map[string]any{"date": "2026-10-09", "lines": lines})
		if r.Status == http.StatusCreated {
			r = e.call(http.MethodPost, "/v1/journals/"+r.str("id")+"/post", e.as(authz.Accountant), nil, "If-Match", r.Header.Get("ETag"))
		}
		// The engine refuses it at posting (LEDGER-41), or the contract at saving: an origin it does not know.
		if refused := r.Status == http.StatusUnprocessableEntity && (strings.Contains(r.Raw, ledger.CodeParty) || name == "an unknown origin"); !refused {
			t.Errorf("%s: the journal posted or was refused otherwise: %d %s", name, r.Status, r.Raw)
		}
	}

	l := e.call(http.MethodGet, "/v1/director-accounts?as_at=2026-10-31", e.as(authz.Bookkeeper), nil)
	if l.Status != http.StatusOK || l.str("currency") != "SGD" || l.str("unkeyed", "amount") != "0.00" {
		t.Fatalf("listDirectorAccounts = %d: %s", l.Status, l.Raw)
	}
	nets := map[string]string{}
	debit := map[string]bool{}
	for _, it := range l.Body["items"].([]any) {
		x := it.(map[string]any)
		nets[x["legal_name"].(string)] = x["net"].(map[string]any)["amount"].(string)
		debit[x["legal_name"].(string)] = x["in_debit"].(bool)
		if len(x["balances"].([]any)) != 2 {
			t.Errorf("%s has %d balances, not one per director's account", x["legal_name"], len(x["balances"].([]any)))
		}
	}
	if nets["Mei Lin Tan"] != "-1100.00" || debit["Mei Lin Tan"] || nets["Arjun Rao"] != "200.00" || !debit["Arjun Rao"] {
		t.Errorf("the nets: %v, in debit: %v", nets, debit)
	}
	early := e.call(http.MethodGet, "/v1/director-accounts?as_at=2026-10-04", e.as(authz.Auditor), nil)
	if !strings.Contains(early.Raw, `"amount":"-1500.00"`) {
		t.Errorf("as at 4 October: %s", early.Raw)
	}

	st := e.call(http.MethodGet, "/v1/director-accounts/"+mei+"/statement?from=2026-10-03&to=2026-10-31", e.as(authz.TaxAgent), nil)
	if st.Status != http.StatusOK {
		t.Fatalf("getDirectorStatement = %d: %s", st.Status, st.Raw)
	}
	var on2500 map[string]any
	for _, a := range st.Body["accounts"].([]any) {
		if x := a.(map[string]any); x["code"] == "2500" {
			on2500 = x
		}
	}
	origins := on2500["by_origin"].([]any)
	if on2500["opening"].(map[string]any)["amount"] != "-1500.00" || on2500["closing"].(map[string]any)["amount"] != "-1100.00" ||
		len(origins) != 1 || origins[0].(map[string]any)["origin"] != ledger.OriginPaidOut ||
		origins[0].(map[string]any)["amount"].(map[string]any)["amount"] != "400.00" || len(st.Body["movements"].([]any)) != 1 {
		t.Errorf("Mei Lin's statement on 2500: %v, movements %v", on2500, st.Body["movements"])
	}
	e.problemIs(e.call(http.MethodGet, "/v1/director-accounts/"+customer+"/statement?from=2026-10-01&to=2026-10-31", e.as(authz.Owner), nil),
		"getDirectorStatement", 422, "FOUNDER-01")
	e.problemIs(e.call(http.MethodGet, "/v1/director-accounts/"+mei+"/statement?from=2026-10-31&to=2026-10-01", e.as(authz.Owner), nil),
		"getDirectorStatement", 422, "FOUNDER-02")

	x := e.call(http.MethodGet, "/v1/director-accounts/"+mei+"/statement/export?from=2026-10-01&to=2026-10-31&format=csv", e.as(authz.Controller), nil)
	csv, err := base64.StdEncoding.DecodeString(x.str("content_base64"))
	if x.Status != http.StatusOK || err != nil || !strings.Contains(string(csv), "paid_out") || !strings.Contains(string(csv), "400.00") ||
		!strings.Contains(string(csv), "-1500.00") || !strings.HasSuffix(x.str("file_name"), ".csv") {
		t.Errorf("exportDirectorStatement = %d: %s / %s", x.Status, x.Raw, csv)
	}

	people := e.call(http.MethodGet, "/v1/contacts?role=director", e.as(authz.Accountant), nil)
	if n := len(people.Body["items"].([]any)); n != 2 || strings.Contains(people.Raw, "Matrix") {
		t.Errorf("the directors and shareholders: %d, %s", n, people.Raw)
	}
	// A director named by lines stays a director or shareholder (FY249).
	c := e.call(http.MethodGet, "/v1/contacts/"+mei, e.as(authz.Accountant), nil)
	r := e.call(http.MethodPatch, "/v1/contacts/"+mei, e.as(authz.Accountant), map[string]any{"director": false, "shareholder": false, "vendor": true},
		"If-Match", c.Header.Get("ETag"))
	e.problemIs(r, "updateContact", 409, storage.CodeParty)
}

// The member a director or shareholder signs in as is linked under dual control (FR-EQ-01, FM-1; PRD-001 rev 0.167): Ah
// Kow asks to link his own contact to himself, and Wei Ling approves it; then Wei Ling asks to link her contact to
// herself, and Ah Kow cannot approve his own request, nor she hers: each is linked only by the other. A member signs in
// as one director or shareholder at most, and an unlinked person's Money I put in says so.
// req: FR-EQ-01
// req: FR-EQ-14
// req: FM-1
// req: equity/founders-money The person is a contact, optionally a member
// asvs: V8.2.1
func TestLinkingAMemberNeedsAnotherFounder(t *testing.T) {
	f := newFounders(t)
	e := f.e
	kow, ling := f.as(f.ahkow), f.as(f.weiling)
	kowContact, lingContact := e.directorContact("Ah Kow"), e.directorContact("Wei Ling")

	link := func(tok, contact, member string) response {
		c := e.call(http.MethodGet, "/v1/contacts/"+contact, e.as(authz.Accountant), nil)
		body := map[string]any{} // none unlinks
		if member != "" {
			body["member_user_id"] = member
		}
		return e.call(http.MethodPut, "/v1/contacts/"+contact+"/member", tok, body, "If-Match", c.Header.Get("ETag"))
	}
	if r := e.call(http.MethodGet, "/v1/me/director-account", kow, nil); r.Status != http.StatusOK || r.Body["linked"] != false {
		t.Fatalf("Ah Kow's account before the link: %d %s", r.Status, r.Raw)
	}
	req := link(kow, kowContact, f.ahkow)
	if req.Status != http.StatusAccepted || req.str("object_type") != approval.ObjectContactMemberLink {
		t.Fatalf("linkContactMember = %d: %s", req.Status, req.Raw)
	}
	e.problemIs(f.decide(kow, req.str("id"), "approve", hashOf(req)), "approveApproval", 403, approval.CodeOwnRequest)
	if r := f.decide(ling, req.str("id"), "approve", hashOf(req)); r.Status != http.StatusOK || r.str("status") != "approved" {
		t.Fatalf("Wei Ling approving = %d: %s", r.Status, r.Raw)
	}
	mine := e.call(http.MethodGet, "/v1/me/director-account", kow, nil)
	if mine.Body["linked"] != true || mine.str("account", "contact_id") != kowContact || mine.str("account", "member_user_id") != f.ahkow {
		t.Errorf("Ah Kow's account after the link: %s", mine.Raw)
	}

	// Wei Ling asks for her own link; Ah Kow, who did not ask, approves it. Her request is never hers to approve.
	req = link(ling, lingContact, f.weiling)
	e.problemIs(f.decide(ling, req.str("id"), "approve", hashOf(req)), "approveApproval", 403, approval.CodeOwnRequest)
	if r := f.decide(kow, req.str("id"), "approve", hashOf(req)); r.Status != http.StatusOK {
		t.Fatalf("Ah Kow approving Wei Ling's link = %d: %s", r.Status, r.Raw)
	}
	// Ah Kow asks to move Wei Ling's link to himself: Wei Ling is the member unlinked, so she cannot approve it.
	e.problemIs(link(kow, lingContact, f.ahkow), "linkContactMember", 409, storage.CodeParty)
	req = link(kow, lingContact, "")
	if req.Status != http.StatusAccepted {
		t.Fatalf("unlinking Wei Ling = %d: %s", req.Status, req.Raw)
	}
	e.problemIs(f.decide(ling, req.str("id"), "approve", hashOf(req)), "approveApproval", 403, CodeOwnLink)
	e.problemIs(link(e.as(authz.Accountant), kowContact, ""), "linkContactMember", 403, CodeForbidden)
	e.problemIs(link(kow, e.customer(), f.ahkow), "linkContactMember", 422, "FOUNDER-01")
}

// matrixDirector returns the ID of a director and shareholder of e's organisation, adding one first if needed: also a
// customer and a vendor, so a row that takes the first contact still finds one it can use.
func matrixDirector(e *env) string {
	l := e.call(http.MethodGet, "/v1/contacts?role=director", e.as(authz.Accountant), nil)
	if items, _ := l.Body["items"].([]any); len(items) > 0 {
		return items[0].(map[string]any)["id"].(string)
	}
	return e.call(http.MethodPost, "/v1/contacts", e.as(authz.Accountant), map[string]any{"legal_name": "Matrix director", "director": true,
		"shareholder": true, "customer": true, "vendor": true}).str("id")
}

var matrixDirectors = map[string]func(e *env, tok string) (response, func() bool){
	"listDirectorAccounts": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/director-accounts", tok, nil), nil
	},
	// A person's own: the matrix's member, linked to no one, is answered that nothing is linked.
	"getMyDirectorAccount": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/me/director-account", steppedUp(e, tok), nil), nil
	},
	"getDirectorStatement": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/director-accounts/"+matrixDirector(e)+"/statement?from=2026-10-01&to=2026-10-31", tok, nil), nil
	},
	"exportDirectorStatement": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/director-accounts/"+matrixDirector(e)+"/statement/export?from=2026-10-01&to=2026-10-31&format=csv", tok, nil), nil
	},
	// Always a request for another person's approval: an allowed role, stepped up, previews the request it would make.
	"linkContactMember": func(e *env, tok string) (response, func() bool) {
		id := matrixDirector(e)
		was := requestsOf(e, approval.ObjectContactMemberLink)
		c := e.call(http.MethodGet, "/v1/contacts/"+id, e.as(authz.Accountant), nil)
		r := e.call(http.MethodPut, "/v1/contacts/"+id+"/member", steppedUp(e, tok), map[string]any{"member_user_id": matrixApprover(e)},
			"If-Match", c.Header.Get("ETag"), "Prefer", "return=preview")
		return r, func() bool { return requestsOf(e, approval.ObjectContactMemberLink) == was }
	},
}
