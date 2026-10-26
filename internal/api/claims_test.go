//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/claim"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// claimSetup gives e's organisation what a claim posts with, once: 2150 Expense claims payable, 6530 Meals, and two
// categories, Local travel on Rent with no limit and Meals with a limit of S$50.00 a line. It returns their IDs.
func (e *env) claimSetup() (travel, meals string) {
	e.t.Helper()
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "accountant"}})
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		cats, err := u.ClaimCategories().List(ctx)
		if err != nil {
			return err
		}
		for _, c := range cats {
			switch c.Name {
			case "Local travel":
				travel = c.ID.String()
			case "Meals":
				meals = c.ID.String()
			}
		}
		if travel != "" {
			return nil
		}
		if _, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "2150", Name: "Expense claims payable", Type: storage.Liability,
			Role: storage.AccountRole(ledger.RoleExpenseClaimsPayable)}); err != nil {
			return err
		}
		food, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "6530", Name: "Meals & entertainment", Type: storage.Expense})
		if err != nil {
			return err
		}
		limit := storage.Decimal("50.00")
		t, err := u.ClaimCategories().Create(ctx, storage.NewClaimCategory{Name: "Local travel", AccountID: e.books.Rent.ID, Currency: "SGD", Active: true,
			At: e.clock.Now()})
		if err != nil {
			return err
		}
		m, err := u.ClaimCategories().Create(ctx, storage.NewClaimCategory{Name: "Meals", AccountID: food.ID, Currency: "SGD", LineLimit: &limit,
			Active: true, At: e.clock.Now()})
		travel, meals = t.ID.String(), m.ID.String()
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
	return travel, meals
}

// taxi is a receipt line of a S$27.30 taxi on 14 October with no GST (UC-EXP-01).
func taxi(category string) map[string]any {
	return map[string]any{"date": "2026-10-14", "merchant": "ComfortDelGro", "category_id": category, "description": "Taxi to Acme",
		"amount": map[string]any{"amount": "27.30", "currency": "SGD"}, "gst_code": "NR"}
}

// lunch is a receipt line of a S$64.80 meal on 14 October with no GST claimed, above the Meals limit (UC-EXP-04).
func lunch(category string, attendees string) map[string]any {
	return map[string]any{"date": "2026-10-14", "merchant": "Din Tai Fung", "category_id": category, "description": "Lunch with Acme",
		"amount": map[string]any{"amount": "64.80", "currency": "SGD"}, "gst_code": "NR", "attendees": attendees}
}

// ownClaimOf saves a draft claim of user's in e's organisation, through the posting service, and returns its ID and ETag.
func (e *env) ownClaimOf(user uuid.UUID) (id, tag string) {
	e.t.Helper()
	travel, _ := e.claimSetup()
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: user.String()}})
	sgd := money.MustLookup("SGD")
	amount, err := money.Parse("27.30", sgd)
	if err != nil {
		e.t.Fatal(err)
	}
	doc, err := e.api.claims.Create(ctx, e.books.Org, claim.Draft{ClaimantID: user, Date: e.clock.Now(), Lines: []claim.Line{{Kind: claim.LineReceipt,
		Date: e.clock.Now(), Merchant: "ComfortDelGro", CategoryID: uuid.MustParse(travel), Amount: amount, GSTCode: "NR", GST: money.Zero(sgd)}}})
	if err != nil {
		e.t.Fatal(err)
	}
	return doc.ID.String(), etag(doc.Version, doc.Status)
}

// claimants are an organisation in Founder mode whose members were all invited during the founder bootstrap: Ah Kow, an
// Owner and Controller who also claims (Employee), Wei Ling, an Owner and Controller, and Mei, an Approver who claims.
type claimants struct {
	e                   *env
	ahkow, weiling, mei string
}

func newClaimants(t *testing.T) claimants {
	e := newEnv(t)
	owner := e.as(authz.Owner)
	// Every invitation is made during the founder bootstrap, so each keeps its roles when accepted after.
	invite := func(name string, roles ...string) string {
		return e.call(http.MethodPost, "/v1/members/invitations", owner, map[string]any{"display_name": name, "roles": roles}).str("code")
	}
	join := func(name, code string) string {
		j := e.call(http.MethodPost, "/v1/members", e.principal(authn.Principal{Subject: name, Client: "web"}),
			map[string]any{"invitation_code": code, "display_name": name})
		if j.Status != 201 {
			t.Fatalf("%s joining: %d %s", name, j.Status, j.Raw)
		}
		return j.str("user_id")
	}
	a, w, m := invite("Ah Kow", "owner", "controller", "employee"), invite("Wei Ling", "owner", "controller"), invite("Mei", "approver", "employee")
	return claimants{e: e, ahkow: join("Ah Kow", a), weiling: join("Wei Ling", w), mei: join("Mei", m)}
}

// as is a member's session, stepped up now, holding roles.
func (c claimants) as(user string, roles ...authz.Role) string {
	return c.e.principal(authn.Principal{Subject: "member-" + user, UserID: uuid.MustParse(user), Client: "web", Class: authn.ClassUser,
		Organisation: c.e.books.Org, Roles: roles, StepUpAt: c.e.clock.Now()})
}

// FR-AP-07, FR-AP-14 and FR-AP-17 (p1-expense-claims task 2.1; the owner's decisions of 26 Oct 2026, rev 0.147): a member
// drafts their own claim and submits it, stepped up; one person other than the claimant who holds A on "Expense claims"
// approves it, and the Controller too when a line is above its category's limit; the last approval posts it, Dr each
// expense, Cr 2150. A rejection returns it to its claimant with the comment, who fixes it as a new version and submits
// it again (UC-EXP-04). Nobody approves their own claim; an Employee sees and changes only their own.
// req: FR-AP-07
// req: FR-AP-14
// req: FR-AP-17
// req: purchases/expense-claims Claim returned for a policy breach
// req: purchases/expense-claims Out-of-pocket receipt claim
// asvs: V8.2.2, V8.3.1
func TestExpenseClaimsAreApprovedByAnother(t *testing.T) {
	c := newClaimants(t)
	e := c.e
	travel, meals := e.claimSetup()
	ahkow := c.as(c.ahkow, authz.Employee)
	ahkowAsController := c.as(c.ahkow, authz.Employee, authz.Owner, authz.Controller)
	weiling := c.as(c.weiling, authz.Owner, authz.Controller)
	weilingAsEmployee := c.as(c.weiling, authz.Employee)
	mei := c.as(c.mei, authz.Approver, authz.Employee)

	// Ah Kow drafts his own claim: numbered, his, owing S$27.30.
	draft := map[string]any{"date": "2026-10-15", "title": "Acme visit", "lines": []any{taxi(travel)}}
	r := e.call(http.MethodPost, "/v1/expense-claims", ahkow, draft)
	if r.Status != 201 || r.str("number") != "CLM-0001" || r.str("stage") != "draft" || r.str("claimant", "id") != c.ahkow ||
		r.str("claimant", "name") != "Ah Kow" || r.str("total", "amount") != "27.30" {
		t.Fatalf("drafting: %d %s", r.Status, r.Raw)
	}
	id, tag := r.str("id"), r.Header.Get("ETag")
	path := "/v1/expense-claims/" + id
	e.problemIs(e.call(http.MethodPost, "/v1/expense-claims", weiling, draft), "createExpenseClaim", 403, CodeForbidden)

	// Another Employee neither sees nor changes it; those who hold a right on everyone's see it but do not change it.
	e.problemIs(e.call(http.MethodGet, path, weilingAsEmployee, nil), "getExpenseClaim", 404, CodeNotFound)
	if l := e.call(http.MethodGet, "/v1/expense-claims?whose=everyone", weilingAsEmployee, nil); len(l.Body["items"].([]any)) != 0 {
		t.Errorf("another Employee lists %s", l.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/expense-claims?whose=everyone", weiling, nil); len(l.Body["items"].([]any)) != 1 {
		t.Errorf("the Controller lists %s", l.Raw)
	}
	if g := e.call(http.MethodGet, path, weiling, nil); g.Status != 200 || len(g.Body["ledger_impact"].([]any)) != 2 {
		t.Errorf("the Controller reads it: %d %s", g.Status, g.Raw)
	}
	e.problemIs(e.call(http.MethodPut, path, weilingAsEmployee, draft, "If-Match", tag), "replaceExpenseClaim", 404, CodeNotFound)
	e.problemIs(e.call(http.MethodPost, path+"/submit", c.as(c.weiling, authz.Employee, authz.Controller), nil, "If-Match", tag),
		"submitExpenseClaim", 403, CodeClaimNotYours)

	// He submits it: one step, anyone else who holds A. He cannot change it while it waits, nor approve it himself.
	asked := e.call(http.MethodPost, path+"/submit", ahkow, nil, "If-Match", tag)
	if asked.Status != 202 || asked.str("object_type") != approval.ObjectClaim || asked.str("operation") != approval.OpClaimPost ||
		len(asked.Body["steps"].([]any)) != 1 || !strings.Contains(asked.str("summary"), "Expense claim CLM-0001 of Ah Kow, 27.30 SGD") {
		t.Fatalf("submitting: %d %s", asked.Status, asked.Raw)
	}
	if s := e.call(http.MethodGet, path, ahkow, nil); s.str("stage") != "submitted" || s.str("approval_asked") != asked.str("id") {
		t.Errorf("submitted: %s", s.Raw)
	}
	e.problemIs(e.call(http.MethodPut, path, ahkow, draft, "If-Match", tag), "replaceExpenseClaim", 409, CodeClaimWaiting)
	e.problemIs(e.call(http.MethodGet, "/v1/approvals/"+asked.str("id"), ahkow, nil), "getApproval", 403, CodeForbidden)

	// He withdraws it from the claim, a draft again, and submits it again.
	if w := e.call(http.MethodPost, path+"/withdraw", ahkow, nil, "If-Match", tag); w.Status != 200 || w.str("stage") != "draft" {
		t.Fatalf("withdrawing: %d %s", w.Status, w.Raw)
	}
	e.problemIs(e.call(http.MethodPost, path+"/withdraw", ahkow, nil, "If-Match", tag), "withdrawExpenseClaim", 409, approval.CodeNotPending)
	if asked = e.call(http.MethodPost, path+"/submit", ahkow, nil, "If-Match", tag); asked.Status != 202 {
		t.Fatalf("submitting after withdrawing: %d %s", asked.Status, asked.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/approvals/"+asked.str("id")+"/approve", ahkowAsController, hashOf(asked), "If-Match", `"1"`),
		"approveApproval", 403, approval.CodeOwnRequest)
	e.problemIs(e.call(http.MethodGet, "/v1/approvals/"+asked.str("id"), weilingAsEmployee, nil), "getApproval", 403, CodeForbidden)

	// Wei Ling returns it with a comment: a draft again, its comment shown to him.
	reject := hashOf(asked)
	reject["reason"] = "Add the lunch with Acme too."
	if d := e.call(http.MethodPost, "/v1/approvals/"+asked.str("id")+"/reject", weiling, reject, "If-Match", `"1"`); d.Status != 200 {
		t.Fatalf("returning it: %d %s", d.Status, d.Raw)
	}
	back := e.call(http.MethodGet, path, ahkow, nil)
	if back.str("stage") != "returned" || back.str("returned", "comment") != "Add the lunch with Acme too." || back.str("returned", "by", "name") != "Wei Ling" ||
		back.str("status") != "draft" {
		t.Fatalf("returned: %s", back.Raw)
	}

	// He adds the S$64.80 lunch, above the Meals limit of S$50.00: a new version, the first kept, and a warning.
	draft["lines"] = []any{taxi(travel), lunch(meals, "Ah Kow; Tan Wei, Acme")}
	fixed := e.call(http.MethodPut, path, ahkow, draft, "If-Match", back.Header.Get("ETag"))
	if fixed.Status != 200 || fixed.Body["version"] != float64(2) || fixed.str("total", "amount") != "92.10" {
		t.Fatalf("fixing it: %d %s", fixed.Status, fixed.Raw)
	}
	g := e.call(http.MethodGet, path, ahkow, nil)
	if !strings.Contains(g.Raw, `"code":"CLAIM-OVER-LIMIT"`) || !strings.Contains(g.Raw, "64.80 SGD is above the Meals category's limit of 50.00 SGD") {
		t.Errorf("the warning: %s", g.Raw)
	}
	if err := e.store.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Documents().Version(ctx, uuid.MustParse(id), 1)
		return err
	}); err != nil {
		t.Errorf("the returned version is not kept: %v", err)
	}

	// Submitted again: two steps, finance's second. Mei approves the first; it waits for the Controller; she cannot sign
	// again, and Wei Ling's approval posts it.
	again := e.call(http.MethodPost, path+"/submit", ahkow, nil, "If-Match", g.Header.Get("ETag"))
	if again.Status != 202 || len(again.Body["steps"].([]any)) != 2 {
		t.Fatalf("submitting again: %d %s", again.Status, again.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/expense-claims?whose=waiting_for_me", mei, nil); len(l.Body["items"].([]any)) != 1 {
		t.Errorf("waiting for Mei: %s", l.Raw)
	}
	if d := e.call(http.MethodPost, "/v1/approvals/"+again.str("id")+"/approve", mei, hashOf(again), "If-Match", `"1"`); d.Status != 200 ||
		d.str("status") != "pending" || d.Body["current_step"] != float64(2) {
		t.Fatalf("Mei approving the first step: %d %s", d.Status, d.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/expense-claims?whose=waiting_for_me", mei, nil); len(l.Body["items"].([]any)) != 0 {
		t.Errorf("still waiting for Mei: %s", l.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/approvals/"+again.str("id")+"/approve", c.as(c.mei, authz.Approver, authz.Controller), hashOf(again),
		"If-Match", `"1"`), "approveApproval", 403, approval.CodeNotYourStep)
	if s := e.call(http.MethodGet, path, ahkow, nil); s.str("status") != "draft" {
		t.Fatalf("posted before finance approved: %s", s.Raw)
	}
	if d := e.call(http.MethodPost, "/v1/approvals/"+again.str("id")+"/approve", weiling, hashOf(again), "If-Match", `"1"`); d.Status != 200 ||
		d.str("status") != "approved" {
		t.Fatalf("Wei Ling approving: %d %s", d.Status, d.Raw)
	}
	done := e.call(http.MethodGet, path, ahkow, nil)
	if done.str("status") != "posted" || done.str("stage") != "approved" || done.str("total", "amount") != "92.10" || done.str("posted_by", "id") != c.weiling {
		t.Fatalf("approved: %s", done.Raw)
	}
	var legs []string
	for _, x := range done.Body["ledger_impact"].([]any) {
		l := x.(map[string]any)
		legs = append(legs, l["base_amount"].(map[string]any)["amount"].(string))
	}
	if got := strings.Join(legs, " "); got != "27.30 64.80 -92.10" {
		t.Errorf("the entry: %s", got)
	}
	e.problemIs(e.call(http.MethodPost, path+"/submit", ahkow, nil, "If-Match", done.Header.Get("ETag")), "submitExpenseClaim", 409, claim.CodeNotDraft)
	if l := e.call(http.MethodGet, "/v1/expense-claims?stage=approved", ahkow, nil); len(l.Body["items"].([]any)) != 1 {
		t.Errorf("his approved claims: %s", l.Raw)
	}
}

// What stops a claim being submitted comes back whole (ENG-22): a receipt with no merchant on a category that does not
// exist, and the claim's date before the receipt's.
// req: FR-AP-07
// req: ENG-22
func TestAnExpenseClaimIsSubmittedWhole(t *testing.T) {
	c := newClaimants(t)
	e := c.e
	e.claimSetup()
	ahkow := c.as(c.ahkow, authz.Employee)
	bad := taxi(uuid.NewString())
	bad["merchant"] = ""
	r := e.call(http.MethodPost, "/v1/expense-claims", ahkow, map[string]any{"date": "2026-10-13", "lines": []any{bad}})
	if r.Status != 201 {
		t.Fatalf("drafting: %d %s", r.Status, r.Raw)
	}
	s := e.call(http.MethodPost, "/v1/expense-claims/"+r.str("id")+"/submit", ahkow, nil, "If-Match", r.Header.Get("ETag"))
	e.problemIs(s, "submitExpenseClaim", 422, claim.CodeReceipt)
	f := fields(s)
	for field, code := range map[string]string{"lines[0].merchant": claim.CodeReceipt, "lines[0].date": claim.CodeReceipt, "lines[0].category_id": claim.CodeCategory} {
		if f[field] != code {
			t.Errorf("%s: %q, want %s (all: %v)", field, f[field], code, f)
		}
	}
	if requestsOf(e, approval.ObjectClaim) != 0 {
		t.Error("a claim with problems asked for approval")
	}
}

// matrixClaims are the authorisation matrix's rows for expense claims: each as the matrix's member, the claimant.
var matrixClaims = map[string]func(e *env, tok string) (response, func() bool){
	"listExpenseClaims": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/expense-claims?whose=everyone", steppedUp(e, tok), nil), nil
	},
	"getExpenseClaim": func(e *env, tok string) (response, func() bool) {
		id, _ := e.ownClaimOf(uuid.MustParse(matrixApprover(e)))
		return e.call(http.MethodGet, "/v1/expense-claims/"+id, steppedUp(e, tok), nil), nil
	},
	"createExpenseClaim": func(e *env, tok string) (response, func() bool) {
		travel, _ := e.claimSetup()
		was := claimsOf(e)
		r := e.call(http.MethodPost, "/v1/expense-claims", steppedUp(e, tok), map[string]any{"date": "2026-10-15", "lines": []any{taxi(travel)}})
		return r, func() bool { return claimsOf(e) == was }
	},
	"replaceExpenseClaim": func(e *env, tok string) (response, func() bool) {
		travel, _ := e.claimSetup()
		id, tag := e.ownClaimOf(uuid.MustParse(matrixApprover(e)))
		r := e.call(http.MethodPut, "/v1/expense-claims/"+id, steppedUp(e, tok), map[string]any{"date": "2026-10-15", "title": "Changed",
			"lines": []any{taxi(travel)}}, "If-Match", tag)
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/expense-claims/"+id, e.as(authz.Owner), nil).Body["version"] == float64(1)
		}
	},
	"submitExpenseClaim": func(e *env, tok string) (response, func() bool) {
		was := requestsOf(e, approval.ObjectClaim)
		id, tag := e.ownClaimOf(uuid.MustParse(matrixApprover(e)))
		person := steppedUp(e, tok)
		r := e.call(http.MethodPost, "/v1/expense-claims/"+id+"/submit", person, nil, "If-Match", tag)
		if r.Status == http.StatusAccepted {
			e.call(http.MethodPost, "/v1/expense-claims/"+id+"/withdraw", person, nil, "If-Match", tag)
		}
		return r, func() bool { return requestsOf(e, approval.ObjectClaim) == was }
	},
	"withdrawExpenseClaim": func(e *env, tok string) (response, func() bool) {
		id, tag := e.ownClaimOf(uuid.MustParse(matrixApprover(e)))
		claimant := steppedUp(e, e.as(authz.Employee))
		if r := e.call(http.MethodPost, "/v1/expense-claims/"+id+"/submit", claimant, nil, "If-Match", tag); r.Status != http.StatusAccepted {
			e.t.Fatalf("submitting the matrix's claim: %d %s", r.Status, r.Raw)
		}
		r := e.call(http.MethodPost, "/v1/expense-claims/"+id+"/withdraw", steppedUp(e, tok), nil, "If-Match", tag)
		stage := func() string {
			return e.call(http.MethodGet, "/v1/expense-claims/"+id, e.as(authz.Owner), nil).str("stage")
		}
		if r.Status != http.StatusOK { // refused: still submitted, then withdrawn by its claimant so it waits for no one
			unchanged := stage() == "submitted"
			e.call(http.MethodPost, "/v1/expense-claims/"+id+"/withdraw", claimant, nil, "If-Match", tag)
			return r, func() bool { return unchanged }
		}
		return r, nil
	},
}

// claimsOf counts e's organisation's claims.
func claimsOf(e *env) int {
	n := 0
	if err := e.store.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		docs, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: storage.DocumentExpenseClaim, Limit: 1000})
		n = len(docs)
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
	return n
}
