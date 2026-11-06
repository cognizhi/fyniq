//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/banking"
	"github.com/cognizhi/fyniq/internal/domain/team"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// decidedReconciliation is a money account with October's statement, opening at nothing and closing at −10.00, whose
// one line of S$10.00 out on 20 October who categorised to rent, and its October reconciliation, prepared by a
// bookkeeper from the statement.
func (e *env) decidedReconciliation(who string) storage.BankReconciliation {
	e.t.Helper()
	e.bankBooks()
	m := e.newMoneyAccount()
	var lines []storage.BankLine
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		opening, closing := storage.Decimal("0.00"), storage.Decimal("-10.00")
		_, lines, err = u.Banking().Import(ctx, storage.NewBankStatement{MoneyAccountID: m.ID, Source: storage.MoneySourceFile, Format: storage.FormatCSV,
			ParserVersion: "csv@1", PeriodFrom: porttest.Day(10, 1), PeriodTo: porttest.Day(10, 31), Currency: "SGD", Opening: &opening, Closing: &closing,
			At: e.clock.Now(), Lines: []storage.NewBankLine{{Position: 1, BookingDate: porttest.Day(10, 20), Amount: "-10.00", Description: "AWS EMEA",
				Fingerprint: strings.ReplaceAll(uuid.NewString()+uuid.NewString(), "-", "")}}})
		return err
	})
	l := lines[0]
	if c := e.call(http.MethodPost, "/v1/bank-lines/"+l.ID.String()+"/categorise", who, map[string]any{
		"parts": []map[string]any{{"account_id": e.books.Rent.ID.String(), "gst_code": "TX"}}}, "If-Match", etag(l.Version)); c.Status != http.StatusOK {
		e.t.Fatalf("categorising the line = %d: %s", c.Status, c.Raw)
	}
	p := e.call(http.MethodPost, "/v1/bank-reconciliations", e.as(authz.Bookkeeper), map[string]any{"money_account_id": m.ID.String(),
		"period_id": e.books.Oct.ID.String()})
	if p.Status != http.StatusOK {
		e.t.Fatalf("preparing the reconciliation = %d: %s", p.Status, p.Raw)
	}
	return e.reconciliationNow(storage.BankReconciliation{MoneyAccountID: m.ID, PeriodID: e.books.Oct.ID})
}

// SOD-5 at the sign-off (PRD-001 rev 0.179, the owner's decisions of 6 Nov 2026): in Standard mode Cleo, a Controller
// who categorised a line of the account in October, may not sign off its October reconciliation (BANK-15). She asks for
// an override: refused without a reason, and while she decided nothing (Omar's own sign-off needs none); with a reason
// it waits for an Owner, never her, nor Omar, who holds no A on "Users, roles, team mode". Olive approves, and the
// reconciliation is signed off as Cleo, with the override and its reason kept, which C.2 #62 reports. In Founder mode
// the one who decided the lines signs off, FM-5 and the preparer rule compensating.
// req: SOD-5
// req: FR-BNK-10
// req: access/roles-and-segregation SOD-5 when signing off a reconciliation
// req: C.2 #62
func TestWhoDecidedTheLinesSignsOffOnlyUnderAnOverride(t *testing.T) {
	s := inStandardMode(newEnv(t))
	cleo, omar, olive := s.joinedIn(s.books.Org, "Cleo", "controller", "accountant"), s.joinedIn(s.books.Org, "Omar", "controller"),
		s.joinedIn(s.books.Org, "Olive", "owner")
	asCleo := s.personAs(cleo, authz.Controller, authz.Accountant)
	rec := s.decidedReconciliation(asCleo)
	url := reconciliationURL(rec)

	s.problemIs(s.call(http.MethodPost, url+"/sign", asCleo, nil, "If-Match", etag(rec.Version)), "signBankReconciliation", 403, banking.CodeDecided)
	ask := func(who, reason string) response {
		return s.call(http.MethodPost, url+"/sign-override", who, map[string]any{"reason": reason}, "If-Match", etag(rec.Version))
	}
	s.problemIs(ask(asCleo, "  "), "requestReconciliationOverride", 422, CodeInvalid)
	if r := ask(s.personAs(omar, authz.Controller), "No conflict"); r.Status != 422 || !strings.Contains(r.Raw, "there is no conflict to override") {
		t.Fatalf("an override with nothing to override = %d: %s", r.Status, r.Raw)
	}
	asked := ask(asCleo, "Omar is on leave until December")
	if asked.Status != http.StatusAccepted || asked.str("object_type") != approval.ObjectReconciliation || asked.str("operation") != approval.OpReconciliationSign ||
		!strings.Contains(asked.str("summary"), "overriding SOD-5") {
		t.Fatalf("asking for the override = %d: %s", asked.Status, asked.Raw)
	}
	decide := func(who string) response {
		return s.call(http.MethodPost, "/v1/approvals/"+asked.str("id")+"/approve", who, hashOf(asked), "If-Match", `"1"`)
	}
	s.problemIs(decide(s.personAs(omar, authz.Controller)), "approveApproval", 403, approval.CodeNotApprover)
	if d := decide(s.personAs(cleo, authz.Owner)); d.Status == http.StatusOK {
		t.Fatalf("Cleo approved her own override: %s", d.Raw)
	}
	if d := decide(s.personAs(olive, authz.Owner)); d.Status != http.StatusOK || d.str("status") != "approved" {
		t.Fatalf("Olive approving the override = %d: %s", d.Status, d.Raw)
	}
	g := s.call(http.MethodGet, url, s.as(authz.Auditor), nil)
	if g.str("signed_by") != "user:"+cleo.String() || g.str("sod_override", "approval_id") != asked.str("id") ||
		g.str("sod_override", "reason") != "Omar is on leave until December" || g.str("sod_override", "conflict") != "SOD-5" {
		t.Fatalf("the reconciliation signed under the override: %s", g.Raw)
	}
	c := s.checked("controls.sod-overrides")
	wantStatus(t, c, "warning")
	if it := itemNamed(c, rec.ID.String()); it == nil || !strings.Contains(it["detail"].(string), "Omar is on leave until December") {
		t.Errorf("C.2 #62 does not report the override: %s", c.Raw)
	}

	// Another account's reconciliation: Omar decided none of its lines, so he signs it off.
	other := s.decidedReconciliation(asCleo)
	if r := s.call(http.MethodPost, reconciliationURL(other)+"/sign", s.personAs(omar, authz.Controller), nil, "If-Match", etag(other.Version)); r.Status != http.StatusOK {
		t.Fatalf("Omar signing off = %d: %s", r.Status, r.Raw)
	}

	// Founder mode allows it.
	f := newEnv(t)
	founder := f.personAs(uuid.New(), authz.Controller, authz.Accountant)
	frec := f.decidedReconciliation(founder)
	if r := f.call(http.MethodPost, reconciliationURL(frec)+"/sign", founder, nil, "If-Match", etag(frec.Version)); r.Status != http.StatusOK {
		t.Fatalf("a founder who decided the lines signing off = %d: %s", r.Status, r.Raw)
	}
	wantStatus(t, f.checked("controls.sod-overrides"), "ok")
}

// SOD-6 when acting (TEAM-04, PRD-001 rev 0.179): in Standard mode a person whose roles hold SOD-6 with no override
// neither approves a transaction's request, nor signs off a reconciliation, as they already never post; a request on a
// row that is no transaction's, such as a setting, is not theirs to refuse here.
// req: SOD-6
// req: access/roles-and-segregation SOD-6 when approving
func TestAConflictOfRolesNeverApprovesATransaction(t *testing.T) {
	s := inStandardMode(newEnv(t))
	conflicted := s.personAs(uuid.New(), authz.Administrator, authz.Controller)
	if len(authz.RoleConflicts([]authz.Role{authz.Administrator, authz.Controller})) != 1 {
		t.Fatal("Administrator and Controller should hold SOD-6")
	}
	id, tag := s.createDraft("2000.01")
	asked := s.call(http.MethodPost, "/v1/journals/"+id+"/post-approval", steppedUp(s, s.as(authz.Accountant)), nil, "If-Match", tag)
	if asked.Status != http.StatusAccepted {
		t.Fatalf("asking to post = %d: %s", asked.Status, asked.Raw)
	}
	s.problemIs(s.call(http.MethodPost, "/v1/approvals/"+asked.str("id")+"/approve", conflicted, hashOf(asked), "If-Match", `"1"`),
		"approveApproval", 403, team.CodeAction)
	s.problemIs(s.call(http.MethodPost, "/v1/approvals/"+asked.str("id")+"/reject", conflicted, map[string]any{"envelope_hash": asked.str("envelope_hash"),
		"reason": "Not ours"}, "If-Match", `"1"`), "rejectApproval", 403, team.CodeAction)
	cora := s.joinedIn(s.books.Org, "Cora", "controller")
	if d := s.call(http.MethodPost, "/v1/approvals/"+asked.str("id")+"/approve", s.personAs(cora, authz.Controller), hashOf(asked), "If-Match", `"1"`); d.Status != http.StatusOK {
		t.Fatalf("a Controller without the conflict approving = %d: %s", d.Status, d.Raw)
	}

	rec := s.decidedReconciliation(s.personAs(uuid.New(), authz.Accountant))
	s.problemIs(s.call(http.MethodPost, reconciliationURL(rec)+"/sign", conflicted, nil, "If-Match", etag(rec.Version)), "signBankReconciliation", 403, team.CodeAction)
}
