//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/closing"
	"github.com/cognizhi/fyniq/internal/domain/director"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// yearEndingSeptember sets e's financial years from 1 Oct 2025, the first ending on 30 Sep 2026, with September 2026 as
// a period, and returns that period.
func (e *env) yearEndingSeptember() storage.Period {
	e.t.Helper()
	start, end := time.Date(2025, 10, 1, 0, 0, 0, 0, time.UTC), porttest.Day(time.September, 30)
	var sep storage.Period
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		pr := o.Profile
		pr.FYStartMonth, pr.BooksStart, pr.FirstYearEnd = 10, &start, &end
		if _, err = u.UpdateOrganisation(ctx, o.Version, pr); err != nil {
			return err
		}
		sep, err = u.Periods().Create(ctx, storage.NewPeriod{StartsOn: porttest.Day(time.September, 1), EndsOn: end})
		return err
	})
	return sep
}

// passkey is a member's session, stepped up now with a passkey, holding roles.
func (c claimants) passkey(user string, roles ...authz.Role) string {
	return c.e.principal(authn.Principal{Subject: "member-" + user, UserID: uuid.MustParse(user), Client: "web", Class: authn.ClassUser,
		Organisation: c.e.books.Org, Roles: roles, StepUpAt: c.e.clock.Now(), StepUpMethod: "passkey"})
}

// FR-EQ-12 (design D13), worked by hand: the first financial year ends on 30 Sep 2026. Ah Kow lent the company
// S$1,000.00 on 15 Sep 2026. On 15 Oct 2026 his account says the year end is not confirmed. Mei, linked to no one, has
// nothing to confirm; Ah Kow, linked to his contact, must step up with a passkey, and confirm a year's end. He confirms
// 30 Sep 2026: -1,000.00 on 2500, and his account says confirmed. A further S$200.00 dated 20 Sep 2026 posted later shows
// it changed since, and the year-end close warns of it.
// req: FR-EQ-12
// req: equity/founders-money Director confirms with a passkey
// req: equity/founders-money Unconfirmed balance on the close checklist
func TestYearEndConfirmations(t *testing.T) {
	c := newClaimants(t)
	e := c.e
	loan, _ := e.directorChart()
	sep := e.yearEndingSeptember()
	kow := e.directorContact("Ah Kow")
	bank := e.books.Bank.ID.String()
	if r := e.postFounderJournal("2026-09-15", founderLine(bank, "1000.00", "", ""), founderLine(loan, "-1000.00", kow, ledger.OriginLent)); r.Status != http.StatusOK {
		t.Fatalf("Ah Kow lending: %d %s", r.Status, r.Raw)
	}
	stateOf := func() map[string]any {
		r := e.call(http.MethodGet, "/v1/director-accounts", e.as(authz.Accountant), nil)
		for _, it := range r.Body["items"].([]any) {
			if x := it.(map[string]any); x["contact_id"] == kow {
				ye, _ := x["year_end"].(map[string]any)
				return ye
			}
		}
		t.Fatalf("no account for Ah Kow: %s", r.Raw)
		return nil
	}
	if s := stateOf(); s["year_end"] != "2026-09-30" || s["status"] != director.NotConfirmed {
		t.Errorf("before confirming: %v", s)
	}

	confirm := func(token, yearEnd string) response {
		return e.call(http.MethodPost, "/v1/me/director-account/confirmations", token, map[string]any{"year_end": yearEnd})
	}
	e.problemIs(confirm(c.passkey(c.mei, authz.Employee), "2026-09-30"), "confirmMyDirectorAccount", 403, CodeNotLinked)
	ct := e.call(http.MethodGet, "/v1/contacts/"+kow, e.as(authz.Accountant), nil)
	asked := e.call(http.MethodPut, "/v1/contacts/"+kow+"/member", c.as(c.ahkow, authz.Owner, authz.Controller), map[string]any{"member_user_id": c.ahkow},
		"If-Match", ct.Header.Get("ETag"))
	if r := e.call(http.MethodPost, "/v1/approvals/"+asked.str("id")+"/approve", c.as(c.weiling, authz.Owner, authz.Controller), hashOf(asked), "If-Match", `"1"`); r.Status != http.StatusOK {
		t.Fatalf("linking Ah Kow: %d %s", r.Status, r.Raw)
	}
	e.problemIs(confirm(c.as(c.ahkow, authz.Employee), "2026-09-30"), "confirmMyDirectorAccount", 403, CodePasskeyStepUp)
	e.problemIs(confirm(c.passkey(c.ahkow, authz.Employee), "2026-08-31"), "confirmMyDirectorAccount", 422, director.CodeConfirm)
	r := confirm(c.passkey(c.ahkow, authz.Employee), "2026-09-30")
	if r.Status != http.StatusCreated || !strings.Contains(r.Raw, `"account_id":"`+loan+`"`) || !strings.Contains(r.Raw, `"amount":"-1000.00"`) {
		t.Fatalf("confirming: %d %s", r.Status, r.Raw)
	}
	if s := stateOf(); s["status"] != director.Confirmed || s["confirmed_by"] == nil {
		t.Errorf("once confirmed: %v", s)
	}

	if r := e.postFounderJournal("2026-09-20", founderLine(bank, "200.00", "", ""), founderLine(loan, "-200.00", kow, ledger.OriginLent)); r.Status != http.StatusOK {
		t.Fatalf("a later loan in the year: %d %s", r.Status, r.Raw)
	}
	if s := stateOf(); s["status"] != director.ChangedSince {
		t.Errorf("after a later entry in the year: %v", s)
	}
	var checks []closing.Check
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		checks, err = closing.Checks(ctx, u, sep, porttest.Day(time.October, 15))
		return err
	})
	found := false
	for _, k := range checks {
		if k.Key == "directors-confirmed" {
			found = true
			if k.Severity != closing.Warning || !strings.Contains(k.Detail, "Ah Kow (changed since they confirmed)") {
				t.Errorf("the year-end close: %+v", k)
			}
		}
	}
	if !found {
		t.Errorf("the year-end close says nothing of the confirmations: %+v", checks)
	}
}

// matrixConfirmOrg is the organisation confirmations are tried in, made once per env: years from 1 Oct 2025, the first
// ending on 30 Sep 2026, a director's loan account, and a member linked to a director (another member asked, a third
// approved); its ID and that member's user.
var matrixConfirmOrg = map[*env][2]uuid.UUID{}

func matrixConfirm(e *env) (uuid.UUID, uuid.UUID) {
	if f, ok := matrixConfirmOrg[e]; ok {
		return f[0], f[1]
	}
	b := porttest.NewBooks(e.t, e.store, "Matrix confirmations "+uuid.NewString()[:8])
	start, end := time.Date(2025, 10, 1, 0, 0, 0, 0, time.UTC), porttest.Day(time.September, 30)
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture"}})
	if err := e.store.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		pr := o.Profile
		pr.FYStartMonth, pr.BooksStart, pr.FirstYearEnd = 10, &start, &end
		if _, err := u.UpdateOrganisation(ctx, o.Version, pr); err != nil {
			return err
		}
		_, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "2500", Name: "Loan from director / shareholder", Type: storage.Liability, Role: ledger.RoleDirectorLoan})
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
	in := func(roles ...authz.Role) string {
		return e.principal(authn.Principal{Client: "web", Class: authn.ClassUser, Organisation: b.Org, Roles: roles})
	}
	// Every invitation is made during the founder bootstrap, before anyone joins, so each keeps its role (as newClaimants).
	invite := func(name string) string {
		return e.call(http.MethodPost, "/v1/members/invitations", in(authz.Owner), map[string]any{"display_name": name, "roles": []string{"owner"}}).str("code")
	}
	join := func(name, code string) uuid.UUID {
		j := e.call(http.MethodPost, "/v1/members", e.principal(authn.Principal{Subject: name + uuid.NewString()[:4], Client: "web"}),
			map[string]any{"invitation_code": code, "display_name": name})
		if j.Status != http.StatusCreated {
			e.t.Fatalf("%s joining: %d %s", name, j.Status, j.Raw)
		}
		return uuid.MustParse(j.str("user_id"))
	}
	c1, c2, c3 := invite("Matrix director"), invite("Matrix asker"), invite("Matrix approver")
	linked, asker, approver := join("Matrix director", c1), join("Matrix asker", c2), join("Matrix approver", c3)
	as := func(user uuid.UUID) string {
		return e.principal(authn.Principal{Subject: "member-" + user.String(), UserID: user, Client: "web", Class: authn.ClassUser, Organisation: b.Org,
			Roles: []authz.Role{authz.Owner, authz.Controller}, StepUpAt: e.clock.Now()})
	}
	contact := e.call(http.MethodPost, "/v1/contacts", in(authz.Accountant), map[string]any{"legal_name": "Matrix director", "director": true, "shareholder": true})
	asked := e.call(http.MethodPut, "/v1/contacts/"+contact.str("id")+"/member", as(asker), map[string]any{"member_user_id": linked.String()},
		"If-Match", contact.Header.Get("ETag"))
	if r := e.call(http.MethodPost, "/v1/approvals/"+asked.str("id")+"/approve", as(approver), hashOf(asked), "If-Match", `"1"`); r.Status != http.StatusOK {
		e.t.Fatalf("the matrix's link: %d %s / %s", r.Status, r.Raw, asked.Raw)
	}
	matrixConfirmOrg[e] = [2]uuid.UUID{b.Org, linked}
	return b.Org, linked
}

var matrixConfirmations = map[string]func(e *env, tok string) (response, func() bool){
	// A person confirms their own: tok's roles, as the linked member of the confirmations' organisation, stepped up with
	// a passkey; an allowed role previews, which saves nothing.
	"confirmMyDirectorAccount": func(e *env, tok string) (response, func() bool) {
		org, user := matrixConfirm(e)
		e.tokens.mu.Lock()
		p := e.tokens.m[tok]
		e.tokens.mu.Unlock()
		if p.Organisation != uuid.Nil {
			p.Organisation = org
		}
		if p.Class == authn.ClassUser {
			p.UserID, p.Subject, p.StepUpAt, p.StepUpMethod = user, "member-"+user.String(), e.clock.Now(), "passkey"
		}
		count := func() int {
			n := 0
			_ = e.store.Do(context.Background(), org, func(ctx context.Context, u storage.UnitOfWork) error {
				docs, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: storage.DocumentDirectorConfirmation, Limit: 500})
				n = len(docs)
				return err
			})
			return n
		}
		was := count()
		r := e.call(http.MethodPost, "/v1/me/director-account/confirmations", e.principal(p), map[string]any{"year_end": "2026-09-30"}, "Prefer", "return=preview")
		return r, func() bool { return count() == was }
	},
}
