//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/claim"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// FR-AP-15 (p1-expense-claims task 5.1, design D6): the expense policy's categories are read by those who claim and those
// who see claims, and changed by those who approve claims; a category costs a plain expense or asset account, has a
// name of its own among those in use and limits in the base currency, keeps whether it is the mileage category, and is
// put out of use, never deleted. The starter set loads into an organisation with none.
// req: FR-AP-15
// req: ENG-22
func TestTheExpensePolicysCategories(t *testing.T) {
	e := newEnv(t)
	e.claimSetup()
	controller := e.as(authz.Controller)
	var money, travelAcc string
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		m, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "1010", Name: "DBS", Type: storage.Asset,
			AccountAttributes: storage.AccountAttributes{MoneyAccount: true, Currency: "SGD"}})
		if err != nil {
			return err
		}
		x, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "6510", Name: "Local transport", Type: storage.Expense})
		money, travelAcc = m.ID.String(), x.ID.String()
		return err
	})
	if l := e.call(http.MethodGet, "/v1/claim-categories", e.as(authz.Employee), nil); l.Status != 200 || len(l.Body["items"].([]any)) != 2 {
		t.Fatalf("an Employee lists them: %d %s", l.Status, l.Raw)
	}

	// Added by the Controller, not the Owner; every problem at once.
	draft := map[string]any{"name": "Taxis", "account_id": travelAcc, "limit": sgd("80.00"), "receipt_above": sgd("0.00"), "attendees_above": sgd("50.00")}
	e.problemIs(e.call(http.MethodPost, "/v1/claim-categories", e.as(authz.Owner), draft), "createClaimCategory", 403, CodeForbidden)
	r := e.call(http.MethodPost, "/v1/claim-categories", controller, draft)
	if r.Status != 201 || r.str("account_code") != "6510" || r.str("limit", "amount") != "80.00" || r.str("receipt_above", "amount") != "0.00" || r.Body["mileage"] != false {
		t.Fatalf("adding Taxis: %d %s", r.Status, r.Raw)
	}
	bad := e.call(http.MethodPost, "/v1/claim-categories", controller, map[string]any{"name": " taxis ", "account_id": money,
		"limit": map[string]any{"amount": "10.00", "currency": "USD"}, "receipt_above": sgd("-1.00")})
	e.problemIs(bad, "createClaimCategory", 422, CodeInvalid)
	f := fields(bad)
	for field, code := range map[string]string{"body.name": claim.CodeCategoryName, "body.account_id": claim.CodeCategoryAccount,
		"body.limit.currency": claim.CodeCategoryLimit, "body.receipt_above": claim.CodeCategoryLimit} {
		if f[field] != code {
			t.Errorf("%s: %q, want %s (all: %v)", field, f[field], code, f)
		}
	}
	if !strings.Contains(bad.Raw, "account 1010 is a money account") {
		t.Errorf("the money account's reason: %s", bad.Raw)
	}

	// Changed: no limit; whether it is the mileage category never changes; out of use, kept.
	path := "/v1/claim-categories/" + r.str("id")
	c := e.call(http.MethodPut, path, controller, map[string]any{"name": "Taxis and rides", "account_id": travelAcc}, "If-Match", r.Header.Get("ETag"))
	if c.Status != 200 || c.Body["limit"] != nil || c.Body["version"] != float64(2) || c.str("name") != "Taxis and rides" {
		t.Fatalf("changing it: %d %s", c.Status, c.Raw)
	}
	e.problemIs(e.call(http.MethodPut, path, controller, map[string]any{"name": "Taxis", "account_id": travelAcc, "mileage": true}, "If-Match", c.Header.Get("ETag")),
		"replaceClaimCategory", 422, CodeInvalid)
	off := e.call(http.MethodPut, path, controller, map[string]any{"name": "Taxis and rides", "account_id": travelAcc, "active": false}, "If-Match", c.Header.Get("ETag"))
	if off.Status != 200 || off.Body["active"] != false {
		t.Errorf("putting it out of use: %d %s", off.Status, off.Raw)
	}

	// The starter set loads only into an organisation with none.
	e.problemIs(e.call(http.MethodPost, "/v1/claim-categories:load-starter", controller, nil), "loadStarterClaimCategories", 409, claim.CodeStarterLoaded)
	fresh := e.freshClaimsOrg()
	if l := e.call(http.MethodPost, "/v1/claim-categories:load-starter", e.principalIn(fresh, authz.Controller), nil); l.Status != 201 || l.Body["categories"] != float64(2) {
		t.Errorf("loading the starter set: %d %s", l.Status, l.Raw)
	}
}

// freshClaimsOrg is a new organisation of e's database with the standard chart's 6510 Local transport only: the starter
// set makes Local travel and Mileage of it.
func (e *env) freshClaimsOrg() uuid.UUID {
	e.t.Helper()
	b := porttest.NewBooks(e.t, e.store, "Claims "+uuid.NewString()[:8]+" Pte Ltd")
	if err := e.store.Do(e.bankCtx(), b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "6510", Name: "Local transport", Type: storage.Expense})
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
	return b.Org
}

// principalIn is a token for a principal of organisation org holding role, as tok's principal is in e's.
func (e *env) principalIn(org uuid.UUID, roles ...authz.Role) string {
	e.t.Helper()
	tok := e.as(roles...)
	e.tokens.mu.Lock()
	p := e.tokens.m[tok]
	e.tokens.mu.Unlock()
	p.Organisation = org
	return e.principal(p)
}

// matrixCategories are the authorisation matrix's rows for the expense policy's categories.
var matrixCategories = map[string]func(e *env, tok string) (response, func() bool){
	"listClaimCategories": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/claim-categories", tok, nil), nil
	},
	"createClaimCategory": func(e *env, tok string) (response, func() bool) {
		e.claimSetup()
		name := "Category " + uuid.NewString()[:8]
		r := e.call(http.MethodPost, "/v1/claim-categories", tok, map[string]any{"name": name, "account_id": e.books.Rent.ID.String()})
		return r, func() bool {
			return !strings.Contains(e.call(http.MethodGet, "/v1/claim-categories?page_size=500", e.as(authz.Owner), nil).Raw, name)
		}
	},
	"replaceClaimCategory": func(e *env, tok string) (response, func() bool) {
		var c storage.ClaimCategory
		e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
			c, err = u.ClaimCategories().Create(ctx, storage.NewClaimCategory{Name: "Category " + uuid.NewString()[:8], AccountID: e.books.Rent.ID,
				Currency: "SGD", Active: true, At: e.clock.Now()})
			return err
		})
		r := e.call(http.MethodPut, "/v1/claim-categories/"+c.ID.String(), tok, map[string]any{"name": c.Name + " changed", "account_id": e.books.Rent.ID.String()},
			"If-Match", etag(c.Version))
		return r, func() bool {
			var now storage.ClaimCategory
			e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
				now, err = u.ClaimCategories().Get(ctx, c.ID)
				return err
			})
			return now.Version == 1
		}
	},
	// The starter set loads only into an organisation with none, so the row loads it into one of its own.
	"loadStarterClaimCategories": func(e *env, tok string) (response, func() bool) {
		org := e.freshClaimsOrg()
		e.tokens.mu.Lock()
		p := e.tokens.m[tok]
		e.tokens.mu.Unlock()
		if p.Organisation == e.books.Org {
			p.Organisation = org
			tok = e.principal(p)
		}
		r := e.call(http.MethodPost, "/v1/claim-categories:load-starter", tok, nil)
		return r, func() bool {
			n := 0
			_ = e.store.Do(context.Background(), org, func(ctx context.Context, u storage.UnitOfWork) error {
				cats, err := u.ClaimCategories().List(ctx)
				n = len(cats)
				return err
			})
			return n == 0
		}
	},
}
