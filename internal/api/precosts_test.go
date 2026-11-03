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
	"github.com/cognizhi/fyniq/internal/domain/director"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// booksFromSeptember sets e's books start to 1 September 2026, with September as a period, and returns that period.
func (e *env) booksFromSeptember() storage.Period {
	e.t.Helper()
	start, end := porttest.Day(time.September, 1), porttest.Day(time.December, 31)
	var sep storage.Period
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		pr := o.Profile
		pr.FYStartMonth, pr.BooksStart, pr.FirstYearEnd = 1, &start, &end
		if _, err = u.UpdateOrganisation(ctx, o.Version, pr); err != nil {
			return err
		}
		sep, err = u.Periods().Create(ctx, storage.NewPeriod{StartsOn: start, EndsOn: porttest.Day(time.September, 30)})
		return err
	})
	return sep
}

// FR-EQ-03 (PRD-001 rev 0.167; p1-founders-money design D7), worked by hand: the books start on 1 Sep 2026. Wei Ling, an
// Owner, records the S$1,090.00 (GST S$90.00) Ah Kow paid the corporate secretary on 28 Aug 2026: it needs the
// accountant's decision (⚠) and, with no receipt, cannot be decided (✖). A cost dated on the books start, or paid by a
// contact who is not a director, is refused; only an Owner or the Controller records, and only the accountant decides.
// With its receipt attached, the accountant decides it is capital, on 1510: it posts on the books start, Dr 1510
// 1,090.00 (no GST claimed while not registered), Cr 2500 1,090.00 naming Ah Kow, paid personally. Ah Kow, signed in as
// the accountant, never decides his own cost. Once decided it changes no more. With September closed, the next cost
// posts on the day of the decision, 15 Oct 2026. The list says what the costs come to for him.
// req: FR-EQ-03
// req: equity/founders-money Recorded with the original date and flagged
// req: equity/founders-money Income tax treatment
func TestCostsBeforeTheBooksStart(t *testing.T) {
	c := newClaimants(t)
	e := c.e
	loan, _ := e.directorChart()
	equipment := e.computerEquipment().ID.String()
	kow := e.directorContact("Ah Kow")
	september := e.booksFromSeptember()
	owner := c.as(c.weiling, authz.Owner)
	accountant := e.as(authz.Accountant)
	sgd := func(a string) map[string]any { return map[string]any{"amount": a, "currency": "SGD"} }
	cost := func(date, contact string) map[string]any {
		return map[string]any{"date": date, "supplier": "Corporate secretary", "description": "Incorporation: ACRA fee and constitution",
			"contact_id": contact, "gross": sgd("1090.00"), "gst": sgd("90.00")}
	}

	// Recorded by an Owner: numbered, needing a decision and a receipt.
	e.problemIs(e.call(http.MethodPost, "/v1/pre-incorporation-costs", accountant, cost("2026-08-28", kow)), "recordPreIncorporationCost", 403, CodeForbidden)
	r := e.call(http.MethodPost, "/v1/pre-incorporation-costs", owner, cost("2026-08-28", kow))
	if r.Status != http.StatusCreated || r.str("number") != "PIC-0001" || r.str("status") != "needs_decision" || r.str("paid_by") != "Ah Kow" {
		t.Fatalf("recording: %d %s", r.Status, r.Raw)
	}
	if !strings.Contains(r.Raw, `"code":"`+director.CodeNoReceipt+`"`) || !strings.Contains(r.Raw, `"severity":"warning"`) {
		t.Errorf("its checks: %s", r.Raw)
	}
	path := "/v1/pre-incorporation-costs/" + r.str("id")

	// Refused: on the books start, and paid by a contact who is not a director or shareholder.
	e.problemIs(e.call(http.MethodPost, "/v1/pre-incorporation-costs", owner, cost("2026-09-01", kow)), "recordPreIncorporationCost", 422, director.CodePreCost)
	e.problemIs(e.call(http.MethodPost, "/v1/pre-incorporation-costs", owner, cost("2026-08-28", e.supplier())), "recordPreIncorporationCost", 422,
		director.CodeNotPerson)

	decide := func(token, etag string, claimGST bool) response {
		return e.call(http.MethodPost, path+"/decide", token, map[string]any{"treatment": director.TreatmentCapital, "account_id": equipment,
			"claim_gst": claimGST, "note": "Incorporation cost: capital."}, "If-Match", etag)
	}
	// No receipt, no decision; and an Owner does not decide.
	e.problemIs(decide(accountant, r.Header.Get("ETag"), false), "decidePreIncorporationCost", 422, director.CodeNoReceipt)
	e.problemIs(decide(owner, r.Header.Get("ETag"), false), "decidePreIncorporationCost", 403, CodeForbidden)
	if f := e.upload(path+"/evidence", owner, map[string]string{"evidence_type": "receipt"}, "invoice.pdf", []byte("%PDF-1.7 secretary"), "If-Match",
		r.Header.Get("ETag")); f.Status != http.StatusCreated {
		t.Fatalf("attaching the receipt: %d %s", f.Status, f.Raw)
	}

	// Ah Kow, linked to his contact and signed in as the accountant, never decides his own cost.
	ct := e.call(http.MethodGet, "/v1/contacts/"+kow, e.as(authz.Accountant), nil)
	asked := e.call(http.MethodPut, "/v1/contacts/"+kow+"/member", c.as(c.ahkow, authz.Owner, authz.Controller), map[string]any{"member_user_id": c.ahkow},
		"If-Match", ct.Header.Get("ETag"))
	if a := e.call(http.MethodPost, "/v1/approvals/"+asked.str("id")+"/approve", c.as(c.weiling, authz.Owner, authz.Controller), hashOf(asked), "If-Match", `"1"`); a.Status != http.StatusOK {
		t.Fatalf("linking Ah Kow: %d %s", a.Status, a.Raw)
	}
	e.problemIs(decide(c.as(c.ahkow, authz.Accountant), r.Header.Get("ETag"), false), "decidePreIncorporationCost", 403, CodeOwnCost)

	// GST is not claimed while the organisation is not registered; decided capital, it posts on the books start.
	e.problemIs(decide(accountant, r.Header.Get("ETag"), true), "decidePreIncorporationCost", 422, ledger.CodePreIncorporationCost)
	d := decide(accountant, r.Header.Get("ETag"), false)
	if d.Status != http.StatusOK || d.str("status") != "decided" || d.str("decision", "post_on") != "2026-09-01" || d.str("decision", "treatment") != "capital" {
		t.Fatalf("deciding: %d %s", d.Status, d.Raw)
	}
	for _, want := range []string{`"account_id":"` + equipment + `","amount":{"amount":"1090.00"`, `"account_id":"` + loan + `","amount":{"amount":"-1090.00"`,
		`"party_id":"` + kow + `"`} {
		if !strings.Contains(d.Raw, want) {
			t.Errorf("its ledger impact lacks %s: %s", want, d.Raw)
		}
	}
	st := e.call(http.MethodGet, "/v1/director-accounts/"+kow+"/statement?from=2026-09-01&to=2026-09-30", owner, nil)
	if !strings.Contains(st.Raw, `"origin":"`+ledger.OriginPaidPersonally+`"`) || !strings.Contains(st.Raw, `"-1090.00"`) {
		t.Errorf("his statement: %s", st.Raw)
	}
	// Decided, it changes no more.
	e.problemIs(e.call(http.MethodPut, path, owner, cost("2026-08-27", kow), "If-Match", d.Header.Get("ETag")), "replacePreIncorporationCost", 409,
		director.CodePreCostDone)
	e.problemIs(decide(accountant, d.Header.Get("ETag"), false), "decidePreIncorporationCost", 409, director.CodePreCostDone)

	// With September closed, the next cost posts on the day of the decision.
	next := e.call(http.MethodPost, "/v1/pre-incorporation-costs", owner, map[string]any{"date": "2026-07-15", "supplier": "Figma",
		"description": "Annual plan", "contact_id": kow, "gross": sgd("763.00")})
	e.upload("/v1/pre-incorporation-costs/"+next.str("id")+"/evidence", owner, map[string]string{"evidence_type": "receipt"}, "figma.pdf",
		[]byte("%PDF-1.7 figma"), "If-Match", next.Header.Get("ETag"))
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Periods().Close(ctx, september.ID)
		return err
	})
	later := e.call(http.MethodPost, "/v1/pre-incorporation-costs/"+next.str("id")+"/decide", accountant, map[string]any{"treatment": director.TreatmentDeductible,
		"account_id": e.books.Rent.ID.String()}, "If-Match", next.Header.Get("ETag"))
	if later.Status != http.StatusOK || later.str("decision", "post_on") != "2026-10-15" {
		t.Fatalf("deciding with the books start's period closed: %d %s", later.Status, later.Raw)
	}

	list := e.call(http.MethodGet, "/v1/pre-incorporation-costs", accountant, nil)
	if list.Status != http.StatusOK || !strings.Contains(list.Raw, `"decided":{"amount":"1853.00"`) || !strings.Contains(list.Raw, `"books_start":"2026-09-01"`) {
		t.Errorf("the list: %s", list.Raw) // 1,090.00 + 763.00
	}
	e.problemIs(e.call(http.MethodGet, "/v1/pre-incorporation-costs", c.as(c.mei, authz.Employee), nil), "listPreIncorporationCosts", 403, CodeForbidden)
}

// preCostFixture is the matrix's organisation for costs before the books start: its ID, its cost, the cost's receipt and
// person, and an account a cost may post to.
type preCostFixture struct{ org, cost, file, person, rent string }

// matrixPreCostOrg is the organisation the costs before the books start are tried in, made once per env: books from
// 1 October 2026, a director's loan account, a director, and a cost of 28 September 2026 with its receipt, needing a
// decision.
var matrixPreCostOrg = map[*env]preCostFixture{}

// matrixPreCost returns tok's principal moved into that organisation, and the organisation's fixture.
func matrixPreCost(e *env, tok string) (string, preCostFixture) {
	f, ok := matrixPreCostOrg[e]
	if !ok {
		b, _ := matrixOpening(e, e.as(authz.Owner), false)
		f.org, f.rent = b.Org.String(), b.Rent.ID.String()
		ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture"}})
		if err := e.store.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "2500", Name: "Loan from director / shareholder", Type: storage.Liability,
				Role: ledger.RoleDirectorLoan})
			return err
		}); err != nil {
			e.t.Fatal(err)
		}
		in := func(r authz.Role) string {
			return e.principal(authn.Principal{Client: "web", Class: authn.ClassUser, Organisation: b.Org, Roles: []authz.Role{r}})
		}
		f.person = e.call(http.MethodPost, "/v1/contacts", in(authz.Accountant), map[string]any{"legal_name": "Matrix director", "director": true,
			"shareholder": true}).str("id")
		c := e.call(http.MethodPost, "/v1/pre-incorporation-costs", in(authz.Owner), map[string]any{"date": "2026-09-28", "supplier": "Corporate secretary",
			"description": "Incorporation", "contact_id": f.person, "gross": map[string]any{"amount": "300.00", "currency": "SGD"}})
		if c.Status != http.StatusCreated {
			e.t.Fatalf("the matrix's cost: %d %s", c.Status, c.Raw)
		}
		f.cost = c.str("id")
		r := e.upload("/v1/pre-incorporation-costs/"+f.cost+"/evidence", in(authz.Owner), map[string]string{"evidence_type": "receipt"}, "receipt.pdf",
			[]byte("%PDF-1.7 matrix"), "If-Match", c.Header.Get("ETag"))
		if r.Status != http.StatusCreated {
			e.t.Fatalf("the matrix's receipt: %d %s", r.Status, r.Raw)
		}
		f.file = r.str("id")
		matrixPreCostOrg[e] = f
	}
	e.tokens.mu.Lock()
	p := e.tokens.m[tok]
	e.tokens.mu.Unlock()
	if p.Organisation != uuid.Nil {
		p.Organisation = uuid.MustParse(f.org)
	}
	return e.principal(p), f
}

// matrixPreCostState is the cost's status and version, and how many files it has, as an Owner of its organisation reads them.
func matrixPreCostState(e *env, f preCostFixture) string {
	owner := e.principal(authn.Principal{Client: "web", Class: authn.ClassUser, Organisation: uuid.MustParse(f.org), Roles: []authz.Role{authz.Owner}})
	c := e.call(http.MethodGet, "/v1/pre-incorporation-costs/"+f.cost, owner, nil)
	ev := e.call(http.MethodGet, "/v1/pre-incorporation-costs/"+f.cost+"/evidence", owner, nil)
	l := e.call(http.MethodGet, "/v1/pre-incorporation-costs", owner, nil)
	return c.Header.Get("ETag") + "|" + c.str("status") + "|" + ev.Raw + "|" + l.str("total", "amount")
}

var matrixPreCosts = map[string]func(e *env, tok string) (response, func() bool){
	"listPreIncorporationCosts": func(e *env, tok string) (response, func() bool) {
		t, _ := matrixPreCost(e, tok)
		return e.call(http.MethodGet, "/v1/pre-incorporation-costs", t, nil), nil
	},
	"getPreIncorporationCost": func(e *env, tok string) (response, func() bool) {
		t, f := matrixPreCost(e, tok)
		return e.call(http.MethodGet, "/v1/pre-incorporation-costs/"+f.cost, t, nil), nil
	},
	"listPreIncorporationCostEvidence": func(e *env, tok string) (response, func() bool) {
		t, f := matrixPreCost(e, tok)
		return e.call(http.MethodGet, "/v1/pre-incorporation-costs/"+f.cost+"/evidence", t, nil), nil
	},
	"getPreIncorporationCostEvidence": func(e *env, tok string) (response, func() bool) {
		t, f := matrixPreCost(e, tok)
		return e.call(http.MethodGet, "/v1/pre-incorporation-costs/"+f.cost+"/evidence/"+f.file, t, nil), nil
	},
	// The writes: an allowed role previews, which saves nothing; a refused one changes nothing.
	"recordPreIncorporationCost": func(e *env, tok string) (response, func() bool) {
		t, f := matrixPreCost(e, tok)
		was := matrixPreCostState(e, f)
		r := e.call(http.MethodPost, "/v1/pre-incorporation-costs", t, map[string]any{"date": "2026-09-20", "supplier": "Lawyer", "description": "Agreement",
			"contact_id": f.person, "gross": map[string]any{"amount": "100.00", "currency": "SGD"}}, "Prefer", "return=preview")
		return r, func() bool { return matrixPreCostState(e, f) == was }
	},
	"replacePreIncorporationCost": func(e *env, tok string) (response, func() bool) {
		t, f := matrixPreCost(e, tok)
		was := matrixPreCostState(e, f)
		owner := e.principal(authn.Principal{Client: "web", Class: authn.ClassUser, Organisation: uuid.MustParse(f.org), Roles: []authz.Role{authz.Owner}})
		c := e.call(http.MethodGet, "/v1/pre-incorporation-costs/"+f.cost, owner, nil)
		r := e.call(http.MethodPut, "/v1/pre-incorporation-costs/"+f.cost, t, map[string]any{"date": "2026-09-27", "supplier": "Corporate secretary",
			"description": "Incorporation", "contact_id": f.person, "gross": map[string]any{"amount": "300.00", "currency": "SGD"}},
			"If-Match", c.Header.Get("ETag"), "Prefer", "return=preview")
		return r, func() bool { return matrixPreCostState(e, f) == was }
	},
	"decidePreIncorporationCost": func(e *env, tok string) (response, func() bool) {
		t, f := matrixPreCost(e, tok)
		was := matrixPreCostState(e, f)
		owner := e.principal(authn.Principal{Client: "web", Class: authn.ClassUser, Organisation: uuid.MustParse(f.org), Roles: []authz.Role{authz.Owner}})
		c := e.call(http.MethodGet, "/v1/pre-incorporation-costs/"+f.cost, owner, nil)
		r := e.call(http.MethodPost, "/v1/pre-incorporation-costs/"+f.cost+"/decide", t, map[string]any{"treatment": director.TreatmentCapital,
			"account_id": f.rent}, "If-Match", c.Header.Get("ETag"), "Prefer", "return=preview")
		return r, func() bool { return matrixPreCostState(e, f) == was }
	},
	"addPreIncorporationCostEvidence": func(e *env, tok string) (response, func() bool) {
		t, f := matrixPreCost(e, tok)
		was := matrixPreCostState(e, f)
		r := e.upload("/v1/pre-incorporation-costs/"+f.cost+"/evidence", t, map[string]string{"evidence_type": "receipt"}, "more.pdf",
			[]byte("%PDF-1.7 more"), "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixPreCostState(e, f) == was }
	},
	"removePreIncorporationCostEvidence": func(e *env, tok string) (response, func() bool) {
		t, f := matrixPreCost(e, tok)
		was := matrixPreCostState(e, f)
		r := e.call(http.MethodDelete, "/v1/pre-incorporation-costs/"+f.cost+"/evidence/"+f.file, t, nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixPreCostState(e, f) == was }
	},
}
