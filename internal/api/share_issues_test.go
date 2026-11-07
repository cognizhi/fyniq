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
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/director"
	"github.com/cognizhi/fyniq/internal/domain/equity"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
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
			_, err := equity.RecordIssue(ctx, u, storage.ShareIssue{ID: uuid.Must(uuid.NewV7()), HolderID: uuid.MustParse(f.person), ClassAccountID: uuid.MustParse(g[0]),
				Shares: "1000", Price: "1.000000", Currency: "SGD", Amount: "1000.00", Consideration: storage.ConsiderationCash, ResolutionRefs: "Incorporation subscription",
				EffectiveOn: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), RecordedBy: uuid.MustParse(matrixApprover(e)), RecordedAt: e.clock.Now()}, nil)
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

// FR-EQ-06 (design D11, D12), worked by hand on Appendix G.3: Ah Kow, linked to his contact, lent S$12,345.67 on 5 Oct
// 2026 (in seven journals); the financial years run with the calendar from 1 Jan 2026. Wei Ling drafts converting it at S$1.20 a share as a
// lender (the default price basis and the carrying amount): 10,288 shares, S$12,345.60 converted, S$0.07 stays owed, and
// the draft shows Dr 2500, Cr 3010. Asking is refused until the shareholders' approval, the board resolution, the
// conversion agreement and the balance confirmation are all attached; Ah Kow never approves it (APR-15), Mei does:
// decided, posting nothing, pending ACRA, lapsing on 30 Jun 2027 (six months after the year it falls in). ACRA's date
// may not be tomorrow nor before the approval, and Ah Kow does not record it; Wei Ling records 15 Oct 2026: the register
// gains 10,288 shares, the entry posts Dr 2500 12,345.60 naming him (converted), Cr 3010, and S$0.07 is still owed.
// Recording it again is refused.
// req: FR-EQ-06
// req: FR-EQ-08
// req: FM-9
// req: equity/founders-money Approve the conversion
// req: equity/founders-money Approval documents and capacity
// req: equity/founders-money The conversion becomes effective
// req: equity/founders-money Pending until registered
// req: FR-EQ-09
// req: equity/founders-money Shareholders' approval lodged within 14 days
func TestADirectorsBalanceIsConvertedToShares(t *testing.T) {
	c := newClaimants(t)
	e := c.e
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
	loan, _ := e.directorChart()
	class := e.shareCapitalIn(e.books.Org)
	kow := e.directorContact("Ah Kow")
	// In parts of no more than S$2,000.00, the most a journal posts with no second person's approval.
	for _, part := range []string{"2000.00", "2000.00", "2000.00", "2000.00", "2000.00", "2000.00", "345.67"} {
		if r := e.postFounderJournal("2026-10-05", founderLine(e.books.Bank.ID.String(), part, "", ""), founderLine(loan, "-"+part, kow, ledger.OriginLent)); r.Status != http.StatusOK {
			t.Fatalf("Ah Kow lending: %d %s", r.Status, r.Raw)
		}
	}
	weiling := c.as(c.weiling, authz.Owner, authz.Controller)
	ahkow := c.as(c.ahkow, authz.Owner, authz.Controller)
	ct := e.call(http.MethodGet, "/v1/contacts/"+kow, e.as(authz.Accountant), nil)
	linked := e.call(http.MethodPut, "/v1/contacts/"+kow+"/member", ahkow, map[string]any{"member_user_id": c.ahkow}, "If-Match", ct.Header.Get("ETag"))
	if r := e.call(http.MethodPost, "/v1/approvals/"+linked.str("id")+"/approve", weiling, hashOf(linked), "If-Match", `"1"`); r.Status != http.StatusOK {
		t.Fatalf("linking Ah Kow: %d %s", r.Status, r.Raw)
	}

	d := e.call(http.MethodPost, "/v1/director-settlements", weiling, map[string]any{"contact_id": kow, "account_id": loan,
		"amount": map[string]any{"amount": "12345.67", "currency": "SGD"}, "route": "convert",
		"conversion": map[string]any{"class_account_id": class, "price": "1.20", "capacity": "lender"}})
	if d.Status != http.StatusCreated || d.str("shares", "shares") != "10288" || d.str("shares", "converted", "amount") != "12345.60" ||
		d.str("shares", "remainder", "amount") != "0.07" || d.str("conversion", "price_basis") != director.PriceBasisRound || d.str("conversion", "measurement") != director.MeasurementCarrying ||
		!strings.Contains(d.Raw, `"account_id":"`+class+`","amount":{"amount":"-12345.60"`) {
		t.Fatalf("drafting the conversion: %d %s", d.Status, d.Raw)
	}
	path := "/v1/director-settlements/" + d.str("id")
	attach := func(kind string) {
		t.Helper()
		g := e.call(http.MethodGet, path, weiling, nil)
		if f := e.upload(path+"/evidence", weiling, map[string]string{"evidence_type": kind}, kind+".pdf", []byte("%PDF-1.7 "+kind), "If-Match", g.Header.Get("ETag")); f.Status != http.StatusCreated {
			t.Fatalf("attaching the %s: %d %s", kind, f.Status, f.Raw)
		}
	}
	attach("shareholders' approval")
	attach("board resolution")
	r := e.call(http.MethodPost, path+"/approval", weiling, nil, "If-Match", d.Header.Get("ETag"))
	if r.Status != http.StatusUnprocessableEntity || !strings.Contains(r.Raw, "no conversion agreement, no balance confirmation") {
		t.Fatalf("asking with two documents missing: %d %s", r.Status, r.Raw)
	}
	attach("conversion agreement")
	attach("balance confirmation")
	asked := e.call(http.MethodPost, path+"/approval", weiling, nil, "If-Match", d.Header.Get("ETag"))
	if asked.Status != http.StatusAccepted || !strings.Contains(asked.Raw, "10288 at 1.20 a share (latest_priced_round)") || !strings.Contains(asked.Raw, `"credit 3010"`) {
		t.Fatalf("asking for the conversion's approval: %d %s", asked.Status, asked.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/approvals/"+asked.str("id")+"/approve", ahkow, hashOf(asked), "If-Match", `"1"`), "approveApproval", 403, approval.CodeBeneficiary)
	if a := e.call(http.MethodPost, "/v1/approvals/"+asked.str("id")+"/approve", c.as(c.mei, authz.Owner), hashOf(asked), "If-Match", `"1"`); a.Status != http.StatusOK {
		t.Fatalf("Mei approving the conversion: %d %s", a.Status, a.Raw)
	}
	g := e.call(http.MethodGet, path, weiling, nil)
	if g.str("status") != "decided" || g.str("lapses_on") != "2027-06-30" || g.Body["share_issue"] != nil {
		t.Fatalf("approved, pending ACRA: %s", g.Raw)
	}
	if st := e.call(http.MethodGet, "/v1/director-accounts/"+kow+"/statement?from=2026-10-01&to=2026-10-31", weiling, nil); !strings.Contains(st.Raw, `"closing":{"amount":"-12345.67"`) {
		t.Errorf("pending, the balance stays owed: %s", st.Raw)
	}

	// FR-EQ-09 (design D12): its ACRA steps. Until the approval is lodged it is ⏰ due 14 days after the resolution, here 6
	// Oct, so by 20 Oct; ACRA's acknowledgement is attached after the approval; an AGM date recorded earlier than the one
	// worked out moves the lapse, and within 30 days of it the conversion is ⚠.
	if g.str("acra", "agm_due_on") != "2027-06-30" || !strings.Contains(g.Raw, `"code":"`+director.CodeApprovalDue+`"`) || !strings.Contains(g.Raw, `"due_on":"2026-10-29"`) {
		t.Errorf("the ACRA steps on approval: %s", g.Raw)
	}
	step := func(kind, on, ref string) response {
		return e.call(http.MethodPost, path+"/acra-steps", weiling, map[string]any{"kind": kind, "on": on, "reference": ref}, "If-Match", g.Header.Get("ETag"))
	}
	if r := e.call(http.MethodPost, path+"/acra-steps", weiling, map[string]any{"kind": "resolved", "on": "2026-10-06"}); r.Status != http.StatusPreconditionRequired {
		t.Errorf("a step without If-Match: %d %s", r.Status, r.Raw)
	}
	e.problemIs(step("approval_lodged", "2026-10-16", "T26A001"), "recordConversionStep", 422, director.CodeStep)
	if r := step("resolved", "2026-10-06", "Written resolution"); r.Status != http.StatusCreated || !strings.Contains(r.Raw, `"due_on":"2026-10-20"`) {
		t.Errorf("the resolution recorded: %d %s", r.Status, r.Raw)
	}
	attach("ACRA acknowledgement")
	if r := step("approval_lodged", "2026-10-12", "T26A001"); r.Status != http.StatusCreated || strings.Contains(r.Raw, director.CodeApprovalDue) || r.str("acra", "approval_lodged_on") != "2026-10-12" {
		t.Errorf("the approval lodged: %d %s", r.Status, r.Raw)
	}
	if r := step("agm_due", "2026-10-31", "AGM notice"); r.Status != http.StatusCreated || r.str("lapses_on") != "2026-10-31" || !strings.Contains(r.Raw, `"code":"`+director.CodeApprovalLapsing+`"`) {
		t.Errorf("an earlier AGM date: %d %s", r.Status, r.Raw)
	}
	g = e.call(http.MethodGet, path, weiling, nil)

	effective := func(tok, on string) response {
		return e.call(http.MethodPost, path+"/effective", tok, map[string]any{"lodgement_ref": "AA2026101500456", "resolution_refs": "Written resolution of 6 Oct 2026",
			"effective_on": on}, "If-Match", g.Header.Get("ETag"))
	}
	e.problemIs(effective(weiling, "2026-10-16"), "recordConversionEffective", 422, director.CodeEffective)
	e.problemIs(effective(weiling, "2026-10-14"), "recordConversionEffective", 422, director.CodeEffective)
	e.problemIs(effective(ahkow, "2026-10-15"), "recordConversionEffective", 403, approval.CodeBeneficiary)
	done := effective(weiling, "2026-10-15")
	if done.Status != http.StatusOK || done.str("status") != "effective" || done.str("share_issue", "shares") != "10288" ||
		done.str("share_issue", "amount", "amount") != "12345.60" || done.str("share_issue", "consideration") != "debt_capitalised" {
		t.Fatalf("recording it effective: %d %s", done.Status, done.Raw)
	}
	for _, want := range []string{`"account_id":"` + loan + `","amount":{"amount":"12345.60"`, `"party_id":"` + kow + `"`, `"account_id":"` + class + `","amount":{"amount":"-12345.60"`} {
		if !strings.Contains(done.Raw, want) {
			t.Errorf("the conversion lacks %s: %s", want, done.Raw)
		}
	}
	st := e.call(http.MethodGet, "/v1/director-accounts/"+kow+"/statement?from=2026-10-01&to=2026-10-31", weiling, nil)
	if !strings.Contains(st.Raw, `"origin":"`+ledger.OriginConverted+`"`) || !strings.Contains(st.Raw, `"closing":{"amount":"-0.07"`) {
		t.Errorf("his statement after the conversion: %s", st.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/share-issues", weiling, nil); !strings.Contains(l.Raw, `"settlement_id":"`+d.str("id")+`"`) || !strings.Contains(l.Raw, `"lodgement_ref":"AA2026101500456"`) {
		t.Errorf("the register: %s", l.Raw)
	}
	again := e.call(http.MethodPost, path+"/effective", weiling, map[string]any{"lodgement_ref": "AA2026101500456", "resolution_refs": "Again", "effective_on": "2026-10-15"},
		"If-Match", done.Header.Get("ETag"))
	e.problemIs(again, "recordConversionEffective", 409, director.CodeNotApproved)
	e.problemIs(step("allotment_lodged", "2026-10-15", "T26A002"), "recordConversionStep", 409, director.CodeNotApproved)
	if f := e.call(http.MethodGet, path, weiling, nil); len(f.Body["acra"].(map[string]any)["flags"].([]any)) != 0 {
		t.Errorf("an effective conversion still raises flags: %s", f.Raw)
	}
}

// matrixConversionOf is the conversion the matrix records effective, approved once per env in its organisation: S$100.00
// of the matrix director's loan at S$1.00 a preference share, its four documents attached.
var matrixConversionOf = map[*env]string{}

func matrixConversion(e *env) string {
	if id, ok := matrixConversionOf[e]; ok {
		return id
	}
	loan, _ := e.directorChart()
	person := matrixDirector(e)
	if r := e.postFounderJournal("2026-10-08", founderLine(e.books.Bank.ID.String(), "100.00", "", ""), founderLine(loan, "-100.00", person, ledger.OriginLent)); r.Status != http.StatusOK {
		e.t.Fatalf("the matrix director lending: %d %s", r.Status, r.Raw)
	}
	var class string
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		a, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "3020", Name: "Preference share capital", Type: storage.Equity, Role: storage.RoleShareCapital})
		class = a.ID.String()
		return err
	})
	owner := e.as(authz.Owner)
	c := e.call(http.MethodPost, "/v1/director-settlements", owner, map[string]any{"contact_id": person, "account_id": loan,
		"amount": map[string]any{"amount": "100.00", "currency": "SGD"}, "route": "convert",
		"conversion": map[string]any{"class_account_id": class, "price": "1.00", "capacity": "shareholder"}})
	if c.Status != http.StatusCreated {
		e.t.Fatalf("the matrix's conversion: %d %s", c.Status, c.Raw)
	}
	for _, kind := range director.ConversionDocuments {
		g := e.call(http.MethodGet, "/v1/director-settlements/"+c.str("id"), owner, nil)
		if r := e.upload("/v1/director-settlements/"+c.str("id")+"/evidence", owner, map[string]string{"evidence_type": kind}, "doc.pdf",
			[]byte("%PDF-1.7 "+kind), "If-Match", g.Header.Get("ETag")); r.Status != http.StatusCreated {
			e.t.Fatalf("the matrix's %s: %d %s", kind, r.Status, r.Raw)
		}
	}
	e.approveAsAnother(approval.ObjectDirectorSettlement, c.str("id"), approval.OpSettlementApprove, func(ctx context.Context, u storage.UnitOfWork) error {
		_, _, err := e.api.settlements.ApproveIn(ctx, u, uuid.MustParse(c.str("id")), time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC))
		return err
	})
	matrixConversionOf[e] = c.str("id")
	return c.str("id")
}

var matrixConversions = map[string]func(e *env, tok string) (response, func() bool){
	"recordConversionStep": func(e *env, tok string) (response, func() bool) {
		id := matrixConversion(e)
		state := func() string {
			return e.call(http.MethodGet, "/v1/director-settlements/"+id, e.as(authz.Owner), nil).Raw
		}
		was := state()
		g := e.call(http.MethodGet, "/v1/director-settlements/"+id, e.as(authz.Owner), nil)
		r := e.call(http.MethodPost, "/v1/director-settlements/"+id+"/acra-steps", steppedUp(e, tok), map[string]any{"kind": "approval_lodged", "on": "2026-10-15",
			"reference": "T26MATRIX"}, "Prefer", "return=preview", "If-Match", g.Header.Get("ETag"))
		return r, func() bool { return state() == was }
	},
	// An allowed role, stepped up, previews recording it effective, which saves nothing; a refused one changes nothing.
	"recordConversionEffective": func(e *env, tok string) (response, func() bool) {
		id := matrixConversion(e)
		state := func() string {
			return e.call(http.MethodGet, "/v1/director-settlements/"+id, e.as(authz.Owner), nil).Raw + "|" + e.call(http.MethodGet, "/v1/share-issues", e.as(authz.Owner), nil).Raw
		}
		was := state()
		r := e.call(http.MethodPost, "/v1/director-settlements/"+id+"/effective", steppedUp(e, tok), map[string]any{"lodgement_ref": "AA2026101500999",
			"resolution_refs": "Matrix resolution", "effective_on": "2026-10-15"}, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return state() == was }
	},
}
