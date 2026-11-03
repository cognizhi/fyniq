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
	"github.com/cognizhi/fyniq/internal/domain/equity"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// shareCapitalIn gives org's chart 3010 Ordinary share capital, and returns its ID.
func (e *env) shareCapitalIn(org uuid.UUID) string {
	e.t.Helper()
	var id string
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture"}})
	if err := e.store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		a, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "3010", Name: "Ordinary share capital", Type: storage.Equity, Role: storage.RoleShareCapital})
		id = a.ID.String()
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
	return id
}

// FR-EQ-08 (design D11), worked by hand: the books start on 1 Jan 2026. Mei Lin Tan subscribed 10,000 ordinary shares
// at S$1.00 on incorporation, 2 Jan 2025: recorded in the register without an entry, S$10,000.00 paid up. On or after the
// books start, of an account that is not share capital, a fractional number of shares, or a holder who is a customer:
// each refused. An Accountant does not record one; whoever reads the founders' accounts reads the register.
// req: FR-EQ-08
// req: INV-13
// req: equity/founders-money Share capital only from the register
func TestShareIssuesBeforeTheBooks(t *testing.T) {
	e := newEnv(t)
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
	class := e.shareCapitalIn(e.books.Org)
	mei := e.directorContact("Mei Lin Tan")
	customer := e.call(http.MethodPost, "/v1/contacts", e.as(authz.Accountant), map[string]any{"legal_name": "Northwind", "customer": true}).str("id")
	owner := steppedUp(e, e.as(authz.Owner))
	issue := func(over map[string]any) map[string]any {
		b := map[string]any{"holder_id": mei, "class_account_id": class, "shares": "10000", "price": "1.00", "consideration": "cash",
			"resolution_refs": "Incorporation subscription, 2 Jan 2025", "lodgement_ref": "AA2025010200123", "effective_on": "2025-01-02"}
		for k, v := range over {
			b[k] = v
		}
		return b
	}

	for name, over := range map[string]map[string]any{
		"on the books start":         {"effective_on": "2026-01-01"},
		"not share capital":          {"class_account_id": e.books.Sales.ID.String()},
		"a fraction of a share":      {"shares": "1.5"},
		"a holder who is a customer": {"holder_id": customer},
		"no resolution":              {"resolution_refs": " "},
	} {
		if r := e.call(http.MethodPost, "/v1/share-issues", owner, issue(over)); r.Status != http.StatusUnprocessableEntity || !strings.Contains(r.Raw, equity.CodeIssue) {
			t.Errorf("%s = %d: %s", name, r.Status, r.Raw)
		}
	}
	e.problemIs(e.call(http.MethodPost, "/v1/share-issues", steppedUp(e, e.as(authz.Accountant)), issue(nil)), "recordShareIssueBeforeBooks", 403, CodeForbidden)

	r := e.call(http.MethodPost, "/v1/share-issues", owner, issue(nil))
	if r.Status != http.StatusCreated || r.str("amount", "amount") != "10000.00" || r.Body["before_books"] != true || r.str("holder_name") != "Mei Lin Tan" ||
		r.str("class_code") != "3010" || r.str("entry_id") != "" {
		t.Fatalf("recording the subscription = %d: %s", r.Status, r.Raw)
	}
	l := e.call(http.MethodGet, "/v1/share-issues", e.principal(authn.Principal{Client: "web", Class: authn.ClassUser, Organisation: e.books.Org,
		Roles: []authz.Role{authz.BoardViewer}}), nil)
	if l.Status != http.StatusOK || !strings.Contains(l.Raw, `"shares":"10000"`) || !strings.Contains(l.Raw, `"effective_on":"2025-01-02"`) {
		t.Errorf("the register, as the Board viewer reads it = %d: %s", l.Status, l.Raw)
	}
	if !strings.Contains(auditTrail(t, e), `"action":"share_issue.recorded"`) {
		t.Error("the issue was not audited")
	}
}

// matrixShareIssueOrg is, per env, the matrix's organisation with books from 1 Oct 2026 (the pre-incorporation costs'),
// its share capital and one issue before the books start.
var matrixShareIssueOrg = map[*env][2]string{}

// matrixShareIssue returns tok's principal moved into that organisation and stepped up as the matrix's member, and its
// share-capital account and director.
func matrixShareIssue(e *env, tok string) (string, [2]string) {
	t, f := matrixPreCost(e, tok)
	g, ok := matrixShareIssueOrg[e]
	if !ok {
		org := uuid.MustParse(f.org)
		g = [2]string{e.shareCapitalIn(org), f.person}
		ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture"}})
		if err := e.store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.ShareIssues().Record(ctx, storage.ShareIssue{ID: uuid.Must(uuid.NewV7()), HolderID: uuid.MustParse(f.person), ClassAccountID: uuid.MustParse(g[0]),
				Shares: "1000", Price: "1.000000", Currency: "SGD", Amount: "1000.00", Consideration: storage.ConsiderationCash, ResolutionRefs: "Incorporation subscription",
				EffectiveOn: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), RecordedBy: uuid.MustParse(matrixApprover(e)), RecordedAt: e.clock.Now()})
			return err
		}); err != nil {
			e.t.Fatal(err)
		}
		matrixShareIssueOrg[e] = g
	}
	e.tokens.mu.Lock()
	p := e.tokens.m[t]
	e.tokens.mu.Unlock()
	if p.Class == authn.ClassUser && p.Organisation != uuid.Nil {
		p.UserID, p.StepUpAt = uuid.MustParse(matrixApprover(e)), e.clock.Now()
	}
	return e.principal(p), g
}

var matrixShareIssues = map[string]func(e *env, tok string) (response, func() bool){
	"listShareIssues": func(e *env, tok string) (response, func() bool) {
		t, _ := matrixShareIssue(e, tok)
		return e.call(http.MethodGet, "/v1/share-issues", t, nil), nil
	},
	// The write: an allowed role previews, which saves nothing; a refused one changes nothing.
	"recordShareIssueBeforeBooks": func(e *env, tok string) (response, func() bool) {
		t, g := matrixShareIssue(e, tok)
		owner, _ := matrixShareIssue(e, e.as(authz.Owner))
		state := func() string { return e.call(http.MethodGet, "/v1/share-issues", owner, nil).Raw }
		was := state()
		r := e.call(http.MethodPost, "/v1/share-issues", t, map[string]any{"holder_id": g[1], "class_account_id": g[0], "shares": "500", "price": "2.00",
			"consideration": "cash", "resolution_refs": "Second subscription", "effective_on": "2026-09-15"}, "Prefer", "return=preview")
		return r, func() bool { return state() == was }
	},
}
