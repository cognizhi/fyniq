//go:build local

package api

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/checks"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/payrollimport"
	"github.com/cognizhi/fyniq/internal/domain/peoplelink"
	"github.com/cognizhi/fyniq/internal/domain/salarybatch"
	"github.com/cognizhi/fyniq/internal/linksend"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// salaryWorld is a payrollWorld with a posted run (October 2026, net pay 26,780.00 on 2100), three people of the projection, a bank account to pay from, three more members (the Payroll
// administrator, a second Owner who approves, the Payment releaser) and Nemura's hold authority as a fake.
type salaryWorld struct {
	*runWorld
	run                         string
	ana, ben, cai               uuid.UUID
	bank                        uuid.UUID
	money                       storage.MoneyAccount
	payroll, approver, releaser string // sessions of the three members, stepped up now
	payrollU, approverU, relU   uuid.UUID
	holds                       *fakeHolds
	version                     int // the lines' object version the world has sent
}

func newSalaryWorld(t *testing.T) *salaryWorld {
	t.Helper()
	w := newSalaryWorldWithoutGate(t)
	w.arrange(t, "2026-10", storage.ArrangePayslips, storage.ArrangeKETs, storage.ArrangeCPF, storage.ArrangeAIS, storage.ArrangeIR21)
	return w
}

// arrange records, directly in the store, how each item is met from a month: a provider for payslips, a manual process for the rest.
func (w *salaryWorld) arrange(t *testing.T, month string, items ...string) {
	t.Helper()
	from, _ := time.Parse("2006-01", month)
	evidence := uuid.MustParse(w.evidence(t))
	if err := w.e.store.Do(context.Background(), w.org, func(ctx context.Context, u storage.UnitOfWork) error {
		for _, item := range items {
			a := storage.PayrollArrangement{Item: item, Method: storage.MethodManual, EvidenceID: evidence, EffectiveFrom: from, RecordedBy: "user:" + w.payrollU.String(), RecordedAt: w.e.clock.Now()}
			if item == storage.ArrangePayslips {
				a.Method, a.Provider = storage.MethodProvider, "Talenox"
			}
			if _, err := u.PayrollOutside().Arrange(ctx, a); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func newSalaryWorldWithoutGate(t *testing.T) *salaryWorld {
	t.Helper()
	ctx := context.Background()
	w := &salaryWorld{runWorld: payrollWorld(t), run: uuid.NewString(), ana: uuid.New(), ben: uuid.New(), cai: uuid.New(), holds: &fakeHolds{answer: map[uuid.UUID]linksend.HoldAnswer{}}, version: 1}
	e := w.e
	e.api.deps.Fields = testFields(t)
	e.api.deps.Holds = w.holds
	if r := w.send(w.approved(w.run, 1, "20000.00", "5100.00", nil), nil); r.Status != http.StatusOK || r.str("outcome") != "posted" {
		t.Fatalf("posting the run: %d %s", r.Status, r.Raw)
	}
	now := e.clock.Now()
	if err := e.store.Do(ctx, w.org, func(ctx context.Context, u storage.UnitOfWork) error {
		for i, p := range []struct {
			id   uuid.UUID
			name string
		}{{w.ana, "Ana Lim"}, {w.ben, "Ben Ong"}, {w.cai, "Cai Yu"}} {
			if err := u.PeopleLinks().SavePerson(ctx, peoplelink.Person{ID: p.id, Version: 1, DisplayName: p.name, BankVersion: 1, BankRecordID: uuid.New(), UpdatedAt: now.Add(time.Duration(i))}); err != nil {
				return err
			}
		}
		chart, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "10010", Name: "DBS payroll account", Type: storage.Asset, AccountAttributes: storage.AccountAttributes{MoneyAccount: true, Currency: "SGD"}})
		if err != nil {
			return err
		}
		w.bank = chart.ID
		l, err := u.PeopleLinks().Live(ctx)
		if err != nil {
			return err
		}
		k, err := newAskKey(l.ID, "fyniq-ask-test")
		if err != nil {
			return err
		}
		if err := u.PeopleLinks().SaveAskKey(ctx, k); err != nil {
			return err
		}
		w.money, err = u.Banking().CreateMoneyAccount(ctx, storage.NewMoneyAccount{AccountID: chart.ID, Kind: storage.MoneyBank, Name: chart.Name, Identifier: "072-10010", Currency: "SGD", At: now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	mei, err := e.store.UserForIdentity(ctx, w.preparer.Issuer, w.preparer.Subject)
	if err != nil {
		t.Fatal(err)
	}
	owner := e.principal(authn.Principal{Subject: "salary-owner", UserID: mei, Client: "web", Class: authn.ClassUser, Organisation: w.org, Roles: []authz.Role{authz.Owner}, StepUpAt: e.clock.Now()})
	join := func(name string, roles ...string) (string, uuid.UUID) {
		inv := e.call(http.MethodPost, "/v1/members/invitations", owner, map[string]any{"display_name": name, "roles": roles})
		if inv.Status != http.StatusAccepted {
			t.Fatalf("inviting %s: %d %s", name, inv.Status, inv.Raw)
		}
		wei := e.principal(authn.Principal{Subject: "salary-wei", UserID: w.approverUser, Client: "web", Class: authn.ClassUser, Organisation: w.org, Roles: []authz.Role{authz.Owner}, StepUpAt: e.clock.Now()})
		if d := e.call(http.MethodPost, "/v1/approvals/"+inv.str("id")+"/approve", wei, hashOf(inv), "If-Match", `"1"`); d.Status != http.StatusOK {
			t.Fatalf("approving %s's invitation: %d %s", name, d.Status, d.Raw)
		}
		j := e.call(http.MethodPost, "/v1/members", e.principal(authn.Principal{Subject: "salary-" + name, Client: "web"}), map[string]any{"invitation_code": inv.str("invitation_code"), "display_name": name})
		if j.Status != http.StatusCreated {
			t.Fatalf("%s joining: %d %s", name, j.Status, j.Raw)
		}
		user := uuid.MustParse(j.str("user_id"))
		return user.String(), user
	}
	pu, payrollU := join("Hui Min Teo", "payroll_administrator")
	au, approverU := join("Priya Nair", "owner")
	ru, relU := join("Ravi Kumar", "payment_releaser")
	w.payroll, w.approver, w.releaser = w.as(pu, authz.PayrollAdmin), w.as(au, authz.Owner), w.as(ru, authz.PaymentReleaser)
	w.payrollU, w.approverU, w.relU = payrollU, approverU, relU
	return w
}

// as is a member's session, stepped up now, holding roles.
func (w *salaryWorld) as(user string, roles ...authz.Role) string {
	return w.e.principal(authn.Principal{Subject: "salary-" + user, UserID: uuid.MustParse(user), Client: "web", Class: authn.ClassUser, Organisation: w.org, Roles: roles, StepUpAt: w.e.clock.Now()})
}

func (w *salaryWorld) line(ref, person uuid.UUID, name, amount string, bankVersion int) map[string]any {
	return map[string]any{"line_id": ref.String(), "person_id": person.String(), "payee_name": name, "bank_bic": "DBSSSGSG", "account_name": name, "account_number": "0123456789",
		"amount": amount, "bank_detail_id": uuid.NewSHA1(uuid.NameSpaceOID, []byte(person.String()+name)).String(), "bank_detail_version": bankVersion, "bank_detail_digest": strings.Repeat(string(rune('a'+bankVersion)), 64)}
}

// payments sends payroll.payments.ready for the run's version 1 under the parties the run was posted with, at the next lines version.
func (w *salaryWorld) payments(run string, lines ...map[string]any) response {
	return w.paymentsWith(run, w.preparer, w.approver0(), lines...)
}

func (w *salaryWorld) approver0() peoplelink.Party { return w.runWorld.approver }

func (w *salaryWorld) paymentsWith(run string, prep, appr peoplelink.Party, lines ...map[string]any) response {
	w.e.t.Helper()
	payload, _ := json.Marshal(map[string]any{"run_id": run, "run_version": 1, "pay_date": "2026-10-31", "preparer": prep, "approver": appr, "lines": lines})
	now := w.e.clock.Now().UTC().Format(time.RFC3339)
	env, err := peoplelink.Sign(w.linkKey, peoplelink.Header{EventID: uuid.NewString(), Type: peoplelink.TypePayrollPaymentsReady, SchemaVersion: 1, SourceSystem: "nemura", SourceOrganisation: w.nemura,
		TargetOrganisation: w.org.String(), Epoch: 1, Sequence: int64(w.version), ObjectType: "payroll_run", ObjectID: run, ObjectVersion: w.version, EffectiveAt: now, RecordedAt: now, SignedAt: now,
		CorrelationID: uuid.NewString()}, payload)
	if err != nil {
		w.e.t.Fatal(err)
	}
	b, _ := json.Marshal(env)
	var body map[string]any
	_ = json.Unmarshal(b, &body)
	return w.e.call(http.MethodPost, "/v1/people-link/payroll-payments", w.tok, body)
}

// decideSalary approves the batch's request as tok.
func (w *salaryWorld) decideSalary(tok string, asked response) response {
	return w.e.call(http.MethodPost, "/v1/approvals/"+asked.str("id")+"/approve", tok, hashOf(asked), "If-Match", `"1"`)
}

func (w *salaryWorld) trailHas(t *testing.T, needles ...string) []string {
	t.Helper()
	var hit []string
	if err := w.e.store.Do(context.Background(), w.org, func(ctx context.Context, u storage.UnitOfWork) error {
		evs, err := u.Audit().Events(ctx, 0, 5000)
		if err != nil {
			return err
		}
		raw, _ := json.Marshal(evs)
		for _, n := range needles {
			if strings.Contains(string(raw), n) {
				hit = append(hit, n)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return hit
}

// The payment lines of a posted pay run make a salary batch that a Payment releaser can release and nobody else's eyes reach (FR-PAY-11, FR-PPL-12, 13, 14, 17; PRD-002 Appendix E
// payroll.payments.ready). Nemura sends three people's lines for the run Fyniq posted; the lines are accepted only for the run version posted last, under the preparer and approver it was
// posted under, for people of the projection and for no more than the run's net pay on 2100. A Payroll administrator sees each person and the account masked; the Payment releaser sees the
// total, the count and the state and no line; no audit event holds a name, an account number or a person's amount. A Payroll administrator prepares it and asks for its approval, which a
// second Owner signs for a total and a digest of the lines; the person who prepared the pay run in Nemura cannot approve it (SOD-4). Ana is held for tax clearance in the projection and
// Ben is held at Nemura but not yet in the projection: the release pays Cai and leaves the two waiting, the payment list holds Cai alone, each line has its instruction ID and no line is paid
// (paid comes only from the bank feed or a person's record with evidence); when the hold is lifted and Ben's, the rest goes in a second release; a second release with nothing free is SB-03.
// req: FR-PAY-11
// part of FR-PPL-12 (paid from the bank feed or a person's evidence is task 6.2), FR-PPL-13 (claims and advances outside a batch are open) and FR-PPL-17 (exports, search and AI tools beyond the API)
// asvs: V8.2.1, V11.2.1, V14.2.4
func TestASalaryBatchIsApprovedThenReleasedWithTheHeldLeftWaiting(t *testing.T) {
	w := newSalaryWorld(t)
	e := w.e
	refA, refB, refC := uuid.New(), uuid.New(), uuid.New()
	lines := []map[string]any{w.line(refA, w.ana, "Ana Lim", "9000.11", 1), w.line(refB, w.ben, "Ben Ong", "9999.99", 1), w.line(refC, w.cai, "Cai Yu", "7000.33", 1)}

	// What Nemura may send: only for the run posted, its version and its parties, for known people, and no more than the net pay.
	e.problemIs(w.payments(uuid.NewString(), lines...), "receivePayrollPayments", 409, peoplelink.CodePayRun)
	e.problemIs(w.paymentsWith(w.run, w.runWorld.approver, w.preparer, lines...), "receivePayrollPayments", 422, peoplelink.CodePayLines)
	e.problemIs(w.payments(w.run, w.line(uuid.New(), uuid.New(), "Stranger", "1.00", 1)), "receivePayrollPayments", 409, peoplelink.CodePayPerson)
	e.problemIs(w.payments(w.run, w.line(uuid.New(), w.ana, "Ana Lim", "26780.01", 1)), "receivePayrollPayments", 422, peoplelink.CodePayTotal)
	e.problemIs(w.payments(w.run, w.line(uuid.New(), w.ana, "Ana Lim", "12.5", 1)), "receivePayrollPayments", 422, peoplelink.CodePayLines)
	e.problemIs(w.sendAsTok(e.principal(authn.Principal{Client: "web", Class: authn.ClassUser, Organisation: w.org, Roles: []authz.Role{authz.Owner}}), lines), "receivePayrollPayments", 403, "API-AUTHZ-01")
	got := w.payments(w.run, lines...)
	if got.Status != http.StatusOK || got.str("outcome") != "received" || got.str("number") != "SB-0001" || got.Body["lines"] != float64(3) {
		t.Fatalf("the lines: %d %s", got.Status, got.Raw)
	}
	id := got.str("batch_id")
	if again := w.payments(w.run, lines...); again.str("outcome") != "unchanged" && again.Status != http.StatusOK { // the same submission at the same lines version
		t.Errorf("the same lines again: %s", again.Raw)
	}

	// Who sees what (FR-PPL-17): the Payroll administrator the lines, the Payment releaser the total, the count and the state alone.
	seen := e.call(http.MethodGet, "/v1/salary-batches/"+id, w.payroll, nil)
	if seen.Status != 200 || seen.Body["lines_visible"] != true || len(seen.Body["lines"].([]any)) != 3 || !strings.Contains(seen.Raw, "DBSSSGSG ••••6789") || strings.Contains(seen.Raw, "0123456789") {
		t.Fatalf("the Payroll administrator: %d %s", seen.Status, seen.Raw)
	}
	blind := e.call(http.MethodGet, "/v1/salary-batches/"+id, w.releaser, nil)
	if blind.Status != 200 || blind.Body["lines_visible"] != false || blind.Body["lines"] != nil || blind.str("total", "amount") != "26000.43" || blind.Body["line_count"] != float64(3) ||
		strings.Contains(blind.Raw, "Ana") || strings.Contains(blind.Raw, "9000.11") {
		t.Fatalf("the Payment releaser: %d %s", blind.Status, blind.Raw)
	}
	for _, role := range []authz.Role{authz.Bookkeeper, authz.APSpecialist, authz.Employee} {
		if r := e.call(http.MethodGet, "/v1/salary-batches/"+id, e.principal(authn.Principal{Client: "web", Class: authn.ClassUser, Organisation: w.org, Roles: []authz.Role{role}}), nil); r.Status != http.StatusForbidden {
			t.Errorf("a %s reading the salary batch: %d %s", role, r.Status, r.Raw)
		}
	}
	if hit := w.trailHas(t, "Ana Lim", "Ben Ong", "Cai Yu", "0123456789", "9000.11", "9999.99", "7000.33"); len(hit) > 0 {
		t.Errorf("the audit trail holds %v", hit)
	}

	// Prepare, ask, approve: the approver is neither the requester nor the run's preparer or approver.
	tag := seen.Header.Get("ETag")
	body := map[string]any{"money_account_id": w.bank.String(), "payment_date": "2026-10-31", "method": "giro"}
	e.problemIs(e.call(http.MethodPost, "/v1/salary-batches/"+id+"/approval", w.payroll, nil, "If-Match", tag), "requestSalaryBatchApproval", 409, salarybatch.CodeNotPrepared)
	e.problemIs(e.call(http.MethodPost, "/v1/salary-batches/"+id+"/prepare", w.payroll, map[string]any{"money_account_id": w.bank.String(), "payment_date": "2026-09-30", "method": "giro"}, "If-Match", tag),
		"prepareSalaryBatch", 422, salarybatch.CodeDate)
	prep := e.call(http.MethodPost, "/v1/salary-batches/"+id+"/prepare", w.payroll, body, "If-Match", tag)
	if prep.Status != 200 || prep.str("payment_date") != "2026-10-31" || prep.Body["version"] != float64(2) {
		t.Fatalf("preparing: %d %s", prep.Status, prep.Raw)
	}
	asked := e.call(http.MethodPost, "/v1/salary-batches/"+id+"/approval", w.payroll, nil, "If-Match", prep.Header.Get("ETag"))
	if asked.Status != http.StatusAccepted || asked.str("object_type") != "salary_batch" || strings.Contains(asked.Raw, "Ana") || strings.Contains(asked.Raw, "0123456789") ||
		!strings.Contains(asked.str("summary"), "salary batch SB-0001: 3 people, 26000.43 SGD") {
		t.Fatalf("asking: %d %s", asked.Status, asked.Raw)
	}
	if d := w.decideSalary(w.payroll, asked); d.Status == http.StatusOK { // the requester never approves their own
		t.Fatalf("the requester approved: %s", d.Raw)
	}
	if d := w.decideSalary(w.approver, asked); d.Status != http.StatusOK {
		t.Fatalf("the second Owner approving: %d %s", d.Status, d.Raw)
	}
	ok := e.call(http.MethodGet, "/v1/salary-batches/"+id, w.payroll, nil)
	if ok.str("status") != "approved" || ok.Body["reapproval_needed"] != false {
		t.Fatalf("approved: %s", ok.Raw)
	}

	// Ana is held in the projection; Ben is held at Nemura, which the projection has not heard: asked at the release, Nemura says so.
	w.holdInProjection(t, w.ana, true, 1)
	w.holds.answer[w.ben] = linksend.HoldAnswer{Status: "held", Version: 1}
	w.holds.answer[w.cai] = linksend.HoldAnswer{Status: "released", Version: 0}
	tag = ok.Header.Get("ETag")
	e.problemIs(e.call(http.MethodPost, "/v1/salary-batches/"+id+"/release", w.payroll, nil, "If-Match", tag), "releaseSalaryBatch", 403, "API-AUTHZ-01")
	rel := e.call(http.MethodPost, "/v1/salary-batches/"+id+"/release", w.releaser, nil, "If-Match", tag)
	if rel.Status != http.StatusOK || rel.Body["released"] != float64(1) || rel.Body["waiting"] != float64(1) || rel.str("state") != "released_in_part" || rel.Body["paid"] != float64(0) {
		t.Fatalf("the release: %d %s", rel.Status, rel.Raw)
	}
	if strings.Contains(rel.Raw, "Cai") {
		t.Errorf("the releaser saw a line: %s", rel.Raw)
	}
	releases := rel.Body["releases"].([]any)
	rid := releases[0].(map[string]any)["id"].(string)
	file := e.call(http.MethodGet, "/v1/salary-batches/"+id+"/releases/"+rid+"/file", w.releaser, nil)
	data, _ := base64.StdEncoding.DecodeString(file.str("content_base64"))
	sum := sha256.Sum256(data)
	if file.Status != 200 || hex.EncodeToString(sum[:]) != file.str("sha256") || !strings.Contains(string(data), "Cai Yu") || strings.Contains(string(data), "Ana Lim") || strings.Contains(string(data), "Ben Ong") ||
		!strings.Contains(string(data), "SB0001-R1-L01") || !strings.Contains(string(data), "7000.33") {
		t.Fatalf("the payment list: %d %s", file.Status, string(data))
	}
	if hit := w.trailHas(t, "Cai Yu", "0123456789", "7000.33", "Ben Ong"); len(hit) > 0 {
		t.Errorf("after the release, the audit trail holds %v", hit)
	}
	for _, l := range e.call(http.MethodGet, "/v1/salary-batches/"+id, w.payroll, nil).Body["lines"].([]any) {
		m := l.(map[string]any)
		switch m["person_name"] {
		case "Cai Yu":
			if m["state"] != "released" || m["instruction_id"] != "SB0001-R1-L01" {
				t.Errorf("Cai's line: %v", m)
			}
		case "Ana Lim": // held in the projection: it says so
			if m["state"] != "scheduled" || m["waiting"] != "held" {
				t.Errorf("Ana's line: %v", m)
			}
		default: // held at Nemura, which the projection has not heard: left unreleased, not paid
			if m["state"] != "scheduled" {
				t.Errorf("Ben's line: %v", m)
			}
		}
	}
	// Nothing is free now: both wait.
	e.problemIs(e.call(http.MethodPost, "/v1/salary-batches/"+id+"/release", w.releaser, nil, "If-Match", tag), "releaseSalaryBatch", 409, salarybatch.CodeNothing)

	// The holds are lifted: the rest goes in a second release, with the next instruction IDs.
	w.holdInProjection(t, w.ana, false, 2)
	w.holds.answer[w.ben] = linksend.HoldAnswer{Status: "released", Version: 2}
	// Nemura has not said yet that Ana's hold is lifted (it answers an older version than the projection saw): her line waits, failing closed (PPL-22).
	w.holds.answer[w.ana] = linksend.HoldAnswer{Status: "held", Version: 1}
	if r := e.call(http.MethodPost, "/v1/salary-batches/"+id+"/release", w.releaser, nil, "If-Match", tag); r.Status != http.StatusOK || r.Body["released"] != float64(2) {
		t.Fatalf("the release with Ana's answer older than the projection: %d %s", r.Status, r.Raw)
	}
	w.holds.answer[w.ana] = linksend.HoldAnswer{Status: "released", Version: 2}
	rel2 := e.call(http.MethodPost, "/v1/salary-batches/"+id+"/release", w.releaser, nil, "If-Match", tag)
	if rel2.Status != http.StatusOK || rel2.Body["released"] != float64(3) || rel2.Body["waiting"] != float64(0) || rel2.str("state") != "released" || len(rel2.Body["releases"].([]any)) != 3 {
		t.Fatalf("the second release: %d %s", rel2.Status, rel2.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/salary-batches/"+id+"/release", w.releaser, nil, "If-Match", tag), "releaseSalaryBatch", 409, salarybatch.CodeNothing)
	e.problemIs(e.call(http.MethodPost, "/v1/salary-batches/"+id+"/cancel", w.payroll, map[string]any{"reason": "too late"}, "If-Match", tag), "cancelSalaryBatch", 409, salarybatch.CodeNotOpen)
}

func (w *salaryWorld) sendAsTok(tok string, lines []map[string]any) response {
	payload, _ := json.Marshal(map[string]any{"run_id": w.run, "run_version": 1, "pay_date": "2026-10-31", "preparer": w.preparer, "approver": w.runWorld.approver, "lines": lines})
	now := w.e.clock.Now().UTC().Format(time.RFC3339)
	env, _ := peoplelink.Sign(w.linkKey, peoplelink.Header{EventID: uuid.NewString(), Type: peoplelink.TypePayrollPaymentsReady, SchemaVersion: 1, SourceSystem: "nemura", SourceOrganisation: w.nemura,
		TargetOrganisation: w.org.String(), Epoch: 1, Sequence: 9, ObjectType: "payroll_run", ObjectID: w.run, ObjectVersion: 9, EffectiveAt: now, RecordedAt: now, SignedAt: now, CorrelationID: uuid.NewString()}, payload)
	b, _ := json.Marshal(env)
	var body map[string]any
	_ = json.Unmarshal(b, &body)
	return w.e.call(http.MethodPost, "/v1/people-link/payroll-payments", tok, body)
}

// holdInProjection sets a person's hold in the projection at a version.
func (w *salaryWorld) holdInProjection(t *testing.T, person uuid.UUID, held bool, version int) {
	t.Helper()
	if err := w.e.store.Do(context.Background(), w.org, func(ctx context.Context, u storage.UnitOfWork) error {
		p, err := u.PeopleLinks().Person(ctx, person)
		if err != nil {
			return err
		}
		p.Held, p.HoldVersion, p.HoldID = held, version, uuid.New()
		return u.PeopleLinks().SavePerson(ctx, p)
	}); err != nil {
		t.Fatal(err)
	}
}

// preparedBatch is a batch of two people's lines for the run, prepared, with its approval asked for, and the request.
func (w *salaryWorld) preparedBatch(t *testing.T) (id string, tag string, asked response, lines map[string]uuid.UUID) {
	t.Helper()
	e := w.e
	refA, refB := orderedRefs()
	got := w.payments(w.run, w.line(refA, w.ana, "Ana Lim", "9000.11", 1), w.line(refB, w.cai, "Cai Yu", "7000.33", 1))
	if got.Status != http.StatusOK || got.str("outcome") != "received" {
		t.Fatalf("the lines: %d %s", got.Status, got.Raw)
	}
	id = got.str("batch_id")
	read := e.call(http.MethodGet, "/v1/salary-batches/"+id, w.payroll, nil)
	prep := e.call(http.MethodPost, "/v1/salary-batches/"+id+"/prepare", w.payroll, map[string]any{"money_account_id": w.bank.String(), "payment_date": "2026-10-31", "method": "fast"},
		"If-Match", read.Header.Get("ETag"))
	if prep.Status != 200 {
		t.Fatalf("preparing: %d %s", prep.Status, prep.Raw)
	}
	asked = e.call(http.MethodPost, "/v1/salary-batches/"+id+"/approval", w.payroll, nil, "If-Match", prep.Header.Get("ETag"))
	if asked.Status != http.StatusAccepted {
		t.Fatalf("asking: %d %s", asked.Status, asked.Raw)
	}
	return id, prep.Header.Get("ETag"), asked, map[string]uuid.UUID{"ana": refA, "cai": refB}
}

// A salary batch is signed off and released by people other than the ones who prepared and approved the run in Nemura, in every team mode (FR-PPL-10, SOD-4; PRD-002 NM-INT-07): the
// run's approver in Nemura holds the Owner's right in Fyniq here and cannot approve its salary batch (SB-06), the run's preparer cannot either, and neither releases it, though each holds
// the right the row asks for. Whoever asked for the approval never approves it, and the database backs all of it: a batch is not approved without a second person who is neither of
// them, a release is for an approved batch under its own approval and never by the run's preparer or approver, and a released batch is not cancelled. A salary batch has no break-glass.
// part of FR-PPL-10 (Nemura's own half is p1-nemura-fyniq-link's); no requirement is tagged for SOD-4 beyond this
// asvs: V8.2.1, V8.3.1, V2.3.5
func TestAttackNoOneWhoPreparedOrApprovedTheRunApprovesOrReleasesItsSalary(t *testing.T) {
	w := newSalaryWorld(t)
	e := w.e
	id, _, asked, _ := w.preparedBatch(t)
	mei, err := e.store.UserForIdentity(context.Background(), w.preparer.Issuer, w.preparer.Subject)
	if err != nil {
		t.Fatal(err)
	}
	wei := w.approverUser
	// Wei Jie approved the run in Nemura and has the Owner's right here; Mei Ling prepared it and is an Owner here too.
	asWei := w.as(wei.String(), authz.Owner, authz.PaymentReleaser)
	asMei := w.as(mei.String(), authz.Owner, authz.PaymentReleaser)
	for who, tok := range map[string]string{"the run's approver": asWei, "the run's preparer": asMei} {
		d := w.decideSalary(tok, asked)
		if d.Status == http.StatusOK || !strings.Contains(d.Raw, salarybatch.CodeSeparation) {
			t.Fatalf("%s approving the salary batch: %d %s", who, d.Status, d.Raw)
		}
	}
	// The database: a batch is not approved with no second person's approval (the executor never ran), nor by a request signed by the run's own people.
	if err := e.store.Do(context.Background(), w.org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.SalaryBatches().Approve(ctx, uuid.MustParse(id), uuid.New(), e.clock.Now())
		return err
	}); storage.CodeOf(err) != storage.CodeSalaryBatch {
		t.Fatalf("approving with no approval: %v", err)
	}
	if d := w.decideSalary(w.approver, asked); d.Status != http.StatusOK {
		t.Fatalf("a second Owner approving: %d %s", d.Status, d.Raw)
	}
	tag := e.call(http.MethodGet, "/v1/salary-batches/"+id, w.payroll, nil).Header.Get("ETag")
	for who, tok := range map[string]string{"the run's approver": asWei, "the run's preparer": asMei} {
		r := e.call(http.MethodPost, "/v1/salary-batches/"+id+"/release", tok, nil, "If-Match", tag)
		if r.Status != http.StatusConflict && r.Status != http.StatusForbidden || r.problemCode() != salarybatch.CodeSeparation {
			t.Fatalf("%s releasing: %d %s", who, r.Status, r.Raw)
		}
	}
	// The database refuses a release by either of them even if the application did not (FY912), and a cancel of a batch with lines released.
	approval := e.call(http.MethodGet, "/v1/salary-batches/"+id, w.payroll, nil).str("approval_id")
	for _, user := range []uuid.UUID{wei, mei} {
		if err := e.store.Do(context.Background(), w.org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.SalaryBatches().Release(ctx, storage.SalaryRelease{BatchID: uuid.MustParse(id), BatchVersion: 2, ApprovalID: uuid.MustParse(approval), ReleasedBy: user, ReleasedAt: e.clock.Now(),
				Writer: "payment-list@1", FileName: "x.csv", MediaType: "text/csv", Sealed: make([]byte, 64), SHA256: make([]byte, 32), Total: "1.00"}, nil, e.clock.Now())
			return err
		}); storage.CodeOf(err) != storage.CodeSalaryBatch {
			t.Errorf("the database releasing for the run's own person %s: %v", user, err)
		}
	}
	// The releaser Ravi, who is neither, releases; the batch is then never cancelled, and its lines never rewritten.
	rel := e.call(http.MethodPost, "/v1/salary-batches/"+id+"/release", w.releaser, nil, "If-Match", tag)
	if rel.Status != http.StatusOK {
		t.Fatalf("an unrelated releaser: %d %s", rel.Status, rel.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/salary-batches/"+id+"/cancel", w.payroll, map[string]any{"reason": "x"}, "If-Match", tag), "cancelSalaryBatch", 409, salarybatch.CodeNotOpen)
	if err := e.store.Do(context.Background(), w.org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.SalaryBatches().Cancel(ctx, uuid.MustParse(id), "bypassing the application", e.clock.Now())
		return err
	}); storage.CodeOf(err) != storage.CodeSalaryBatch {
		t.Errorf("the database cancelling a batch with lines released: %v", err)
	}
}

// A newer bank detail blocks the line until the batch is approved again with the current details (FR-PPL-14; PRD-002 NM-INT-09). Cai's bank details change in Nemura after the batch was
// approved: the projection hears of version 2, Cai's line waits (the rest goes), the lines Nemura sends again with the new details are a new version of the line (never an update), the batch
// is not released until it is approved again (SB-05) with a digest that moved, and a released line is not revised (PPL-27). Revised lines for a run that was cancelled are refused.
// req: FR-PPL-14
// req: FR-APR-05
// asvs: V8.2.1, V16.3.2
func TestAChangedBankDetailBlocksTheLineUntilTheBatchIsApprovedAgain(t *testing.T) {
	w := newSalaryWorld(t)
	e := w.e
	id, _, asked, refs := w.preparedBatch(t)
	if d := w.decideSalary(w.approver, asked); d.Status != http.StatusOK {
		t.Fatalf("approving: %d %s", d.Status, d.Raw)
	}
	w.holds.answer[w.ana] = linksend.HoldAnswer{Status: "released"}
	w.holds.answer[w.cai] = linksend.HoldAnswer{Status: "released"}
	// Nemura reports Cai's new bank details (version 2): the projection moves; her line waits, Ana's goes.
	if err := e.store.Do(context.Background(), w.org, func(ctx context.Context, u storage.UnitOfWork) error {
		p, err := u.PeopleLinks().Person(ctx, w.cai)
		if err != nil {
			return err
		}
		p.BankVersion, p.BankRecordID = 2, uuid.New()
		return u.PeopleLinks().SavePerson(ctx, p)
	}); err != nil {
		t.Fatal(err)
	}
	read := e.call(http.MethodGet, "/v1/salary-batches/"+id, w.payroll, nil)
	waiting := ""
	for _, l := range read.Body["lines"].([]any) {
		if m := l.(map[string]any); m["person_name"] == "Cai Yu" {
			waiting, _ = m["waiting"].(string)
		}
	}
	if waiting != "bank_details" || read.Body["waiting"] != float64(1) {
		t.Fatalf("Cai's line: %s", read.Raw)
	}
	rel := e.call(http.MethodPost, "/v1/salary-batches/"+id+"/release", w.releaser, nil, "If-Match", read.Header.Get("ETag"))
	if rel.Status != 200 || rel.Body["released"] != float64(1) || rel.Body["waiting"] != float64(1) {
		t.Fatalf("the release with Cai's line blocked: %d %s", rel.Status, rel.Raw)
	}
	// Nemura sends Cai's line again with version 2 of her details: a revised line, the batch must be approved again; a line that was released is not revised.
	w.version = 2
	revised := w.payments(w.run, w.line(refs["cai"], w.cai, "Cai Yu", "7000.33", 2), w.line(refs["ana"], w.ana, "Ana Lim", "9000.11", 1))
	if revised.Status != 200 || revised.str("outcome") != "revised" || revised.Body["lines"] != float64(1) {
		t.Fatalf("the revised lines: %d %s", revised.Status, revised.Raw)
	}
	w.version = 3
	e.problemIs(w.payments(w.run, w.line(refs["ana"], w.ana, "Ana Lim", "9000.12", 1)), "receivePayrollPayments", 409, peoplelink.CodePayRelease)
	after := e.call(http.MethodGet, "/v1/salary-batches/"+id, w.payroll, nil)
	if after.Body["reapproval_needed"] != true || after.Body["lines_version"] != float64(2) {
		t.Fatalf("after the revision: %s", after.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/salary-batches/"+id+"/release", w.releaser, nil, "If-Match", after.Header.Get("ETag")), "releaseSalaryBatch", 409, salarybatch.CodeReapprove)
	again := e.call(http.MethodPost, "/v1/salary-batches/"+id+"/approval", w.payroll, nil, "If-Match", after.Header.Get("ETag"))
	if again.Status != http.StatusAccepted || !strings.Contains(again.str("summary"), "Approve again") {
		t.Fatalf("asking again: %d %s", again.Status, again.Raw)
	}
	if d := w.decideSalary(w.approver, again); d.Status != http.StatusOK {
		t.Fatalf("approving again: %d %s", d.Status, d.Raw)
	}
	final := e.call(http.MethodPost, "/v1/salary-batches/"+id+"/release", w.releaser, nil, "If-Match", after.Header.Get("ETag"))
	if final.Status != 200 || final.Body["released"] != float64(2) || final.Body["waiting"] != float64(0) {
		t.Fatalf("the release after approving again: %d %s", final.Status, final.Raw)
	}
}

// matrixSalaryBatches are the authorisation matrix's rows of the salary batch: read under any right of the pay-run row (the Payment releaser's "R (salaries)" too), prepared, asked for and
// cancelled under C, released and downloaded under R by a person stepped up, and the payment lines refused to every role (only the link's client calls it). An allowed role finds no such batch.
var matrixSalaryBatches = map[string]func(e *env, tok string) (response, func() bool){
	"listSalaryBatches": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/salary-batches", tok, nil), nil
	},
	"getSalaryBatch": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/salary-batches/"+uuid.NewString(), tok, nil), nil
	},
	"prepareSalaryBatch": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/salary-batches/"+uuid.NewString()+"/prepare", tok, map[string]any{"money_account_id": uuid.NewString(), "payment_date": "2026-10-31", "method": "fast"},
			"If-Match", `"1-open"`), nil
	},
	"requestSalaryBatchApproval": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/salary-batches/"+uuid.NewString()+"/approval", steppedUp(e, tok), nil, "If-Match", `"1-open"`), nil
	},
	"releaseSalaryBatch": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/salary-batches/"+uuid.NewString()+"/release", steppedUp(e, tok), nil, "If-Match", `"1-approved"`), nil
	},
	"getSalaryBatchFile": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/salary-batches/"+uuid.NewString()+"/releases/"+uuid.NewString()+"/file", steppedUp(e, tok), nil), nil
	},
	"cancelSalaryBatch": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/salary-batches/"+uuid.NewString()+"/cancel", tok, map[string]any{"reason": "Matrix"}, "If-Match", `"1-open"`), nil
	},
	"receivePayrollPayments": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/people-link/payroll-payments", tok, map[string]any{}), nil
	},
}

// statementLine imports a statement of one debit into the world's bank account and returns the line's ID.
func (w *salaryWorld) statementLine(t *testing.T, amount string, day int) string {
	t.Helper()
	var id uuid.UUID
	if err := w.e.store.Do(context.Background(), w.org, func(ctx context.Context, u storage.UnitOfWork) error {
		opening := storage.Decimal("0.00")
		_, lines, err := u.Banking().Import(ctx, storage.NewBankStatement{MoneyAccountID: w.money.ID, Source: storage.MoneySourceFile, Format: storage.FormatCSV, ParserVersion: "csv@1",
			PeriodFrom: time.Date(2026, 10, day, 0, 0, 0, 0, time.UTC), PeriodTo: time.Date(2026, 10, day, 0, 0, 0, 0, time.UTC), Currency: "SGD", Opening: &opening, At: w.e.clock.Now(),
			Lines: []storage.NewBankLine{{Position: 1, BookingDate: time.Date(2026, 10, day, 0, 0, 0, 0, time.UTC), Amount: storage.Decimal("-" + amount), Description: "GIRO SALARY",
				Fingerprint: strings.Repeat(hex.EncodeToString([]byte(amount)), 20)[:64]}}})
		if err == nil {
			id = lines[0].ID
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return id.String()
}

// evidence is a file of evidence on a document of the organisation, which a person names when they record what became of a payment.
func (w *salaryWorld) evidence(t *testing.T) string {
	t.Helper()
	var id uuid.UUID
	if err := w.e.store.Do(context.Background(), w.org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentJournal, Payload: []byte(`{}`)})
		if err != nil {
			return err
		}
		ev, err := u.Evidence().Attach(ctx, storage.NewEvidence{DocumentID: doc.ID, EvidenceType: "bank advice", FileName: "advice.pdf", MediaType: "application/pdf", Size: 10, SHA256: strings.Repeat("d", 64),
			OriginalSHA256: strings.Repeat("d", 64), Source: storage.SourceUpload, DataClass: storage.ClassPayroll, StagingKey: "staging/" + w.org.String() + "/" + uuid.NewString(), UploadedBy: "user:" + w.payrollU.String()})
		id = ev.ID
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return id.String()
}

// statuses are the payroll.payments.status events queued for Nemura, as the outbox holds them.
func (w *salaryWorld) statuses(t *testing.T) []linksend.Queued {
	var out []linksend.Queued
	for _, q := range w.queued(t) {
		if q.Type == linksend.TypePaymentsStatus {
			out = append(out, q)
		}
	}
	return out
}

// Paid means paid (FR-PPL-12; PRD-002 NM-PAY-10): a release makes a line released and nothing more, and each state goes back to Nemura as payroll.payments.status with no figure in the outbox row.
// A person records what became of a payment with evidence, never the run's preparer or approver in Nemura (SB-06), and only a move a payment makes (paid goes back only to returned; an unknown
// outcome is resolved only to paid, rejected or returned); an unknown outcome blocks Nemura's new line for the person in the run (PPL-28) and keeps the person's other lines waiting until resolved. The
// bank feed pays a line too: the one debit of the release, matched to the lines it paid, exactly (Dr 2100, Cr the bank), and the lines are then paid from the bank feed on the bank line's date; a
// debit of another amount, another account or a line already paid is refused. A rejected line is no longer owed in the batch, so Nemura's new line for the person fits under the run's net pay.
// req: FR-PPL-12
// asvs: V2.3.5, V8.2.1, V11.2.1
func TestAPaymentIsPaidOnlyFromTheBankFeedOrAPersonsEvidenceAndEveryStateGoesBackToNemura(t *testing.T) {
	w := newSalaryWorld(t)
	e := w.e
	id, _, asked, refs := w.preparedBatch(t)
	if d := w.decideSalary(w.approver, asked); d.Status != http.StatusOK {
		t.Fatalf("approving: %d %s", d.Status, d.Raw)
	}
	w.holds.answer[w.ana] = linksend.HoldAnswer{Status: "released"}
	w.holds.answer[w.cai] = linksend.HoldAnswer{Status: "released"}
	tag := e.call(http.MethodGet, "/v1/salary-batches/"+id, w.payroll, nil).Header.Get("ETag")
	rel := e.call(http.MethodPost, "/v1/salary-batches/"+id+"/release", w.releaser, nil, "If-Match", tag)
	if rel.Status != http.StatusOK || rel.Body["released"] != float64(2) || rel.Body["paid"] != float64(0) || rel.str("state") != "released" {
		t.Fatalf("the release: %d %s", rel.Status, rel.Raw)
	}
	// Nemura is told each line was released, by the state's ID alone: no name, account or amount waits in the outbox.
	queued := w.statuses(t)
	if len(queued) != 2 {
		t.Fatalf("the statuses queued after the release: %d", len(queued))
	}
	for _, q := range queued {
		if !strings.Contains(string(q.Payload), "state_event_id") || strings.Contains(string(q.Payload), "9000") || strings.Contains(string(q.Payload), "7000") || strings.Contains(string(q.Payload), "Ana") {
			t.Errorf("a queued status holds more than the state's ID: %s", q.Payload)
		}
	}
	evidence := w.evidence(t)
	url := func(ref uuid.UUID) string { return "/v1/salary-batches/" + id + "/lines/" + ref.String() + "/state" }
	body := func(state string) map[string]any {
		return map[string]any{"state": state, "evidence_id": evidence, "on": "2026-10-15"}
	}
	mei, _ := e.store.UserForIdentity(context.Background(), w.preparer.Issuer, w.preparer.Subject)
	// Not the run's own people, not without evidence, not a move a payment never makes, not a line that was not released.
	e.problemIs(e.call(http.MethodPost, url(refs["ana"]), w.as(mei.String(), authz.PayrollAdmin), body("paid")), "recordSalaryLineState", 403, salarybatch.CodeSeparation)
	e.problemIs(e.call(http.MethodPost, url(refs["ana"]), w.payroll, map[string]any{"state": "paid", "evidence_id": uuid.NewString(), "on": "2026-10-15"}), "recordSalaryLineState", 422, salarybatch.CodeState)
	e.problemIs(e.call(http.MethodPost, url(refs["ana"]), w.payroll, body("released")), "recordSalaryLineState", 422, "API-INPUT-02")
	e.problemIs(e.call(http.MethodPost, url(uuid.New()), w.payroll, body("paid")), "recordSalaryLineState", 404, salarybatch.CodeLine)

	// Ana's payment: sent, then lost: unknown, which blocks a new line for her in this run and keeps her out of any other release.
	if r := e.call(http.MethodPost, url(refs["ana"]), w.payroll, body("sent")); r.Status != 200 {
		t.Fatalf("sent: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, url(refs["ana"]), w.payroll, body("unknown")); r.Status != 200 {
		t.Fatalf("unknown: %d %s", r.Status, r.Raw)
	}
	w.version = 2
	e.problemIs(w.payments(w.run, w.line(uuid.New(), w.ana, "Ana Lim", "9000.11", 1)), "receivePayrollPayments", 409, peoplelink.CodePayBlocked)
	e.problemIs(e.call(http.MethodPost, url(refs["ana"]), w.payroll, body("scheduled")), "recordSalaryLineState", 422, "API-INPUT-02")
	// Resolved as rejected: the line is no longer owed, so Nemura's new line for her fits under the net pay, and she is paid in a new line of the same batch.
	if r := e.call(http.MethodPost, url(refs["ana"]), w.payroll, body("rejected")); r.Status != 200 {
		t.Fatalf("rejected: %d %s", r.Status, r.Raw)
	}
	again := w.payments(w.run, w.line(uuid.New(), w.ana, "Ana Lim", "9000.11", 1))
	if again.Status != 200 || again.str("outcome") != "revised" {
		t.Fatalf("a new line for Ana after a rejection: %d %s", again.Status, again.Raw)
	}

	// Cai's debit: the bank feed pays her line, exactly.
	cur := func() string {
		return e.call(http.MethodGet, "/v1/salary-batches/"+id, w.payroll, nil).Header.Get("ETag")
	}
	debit := w.statementLine(t, "7000.33", 15)
	for what, input := range map[string]map[string]any{
		"another amount":  {"bank_line_id": w.statementLine(t, "7000.32", 14), "line_ids": []string{refs["cai"].String()}},
		"a line not paid": {"bank_line_id": debit, "line_ids": []string{refs["ana"].String()}},
	} {
		if r := e.call(http.MethodPost, "/v1/salary-batches/"+id+"/bank-match", w.payroll, input, "If-Match", e.call(http.MethodGet, "/v1/salary-batches/"+id, w.payroll, nil).Header.Get("ETag")); r.Status == 200 {
			t.Errorf("%s was matched: %s", what, r.Raw)
		}
	}
	e.problemIs(e.call(http.MethodPost, "/v1/salary-batches/"+id+"/bank-match", w.as(mei.String(), authz.PayrollAdmin), map[string]any{"bank_line_id": debit, "line_ids": []string{refs["cai"].String()}}, "If-Match", cur()),
		"matchSalaryBankLine", 403, salarybatch.CodeSeparation)
	match := e.call(http.MethodPost, "/v1/salary-batches/"+id+"/bank-match", w.payroll, map[string]any{"bank_line_id": debit, "line_ids": []string{refs["cai"].String()}}, "If-Match", cur())
	if match.Status != 200 || match.Body["paid"] != float64(1) {
		t.Fatalf("matching the debit: %d %s", match.Status, match.Raw)
	}
	if again := e.call(http.MethodPost, "/v1/salary-batches/"+id+"/bank-match", w.payroll, map[string]any{"bank_line_id": debit, "line_ids": []string{refs["cai"].String()}}, "If-Match", cur()); again.Status == 200 {
		t.Errorf("the debit matched twice: %s", again.Raw)
	}
	for _, l := range match.Body["lines"].([]any) {
		if m := l.(map[string]any); m["person_name"] == "Cai Yu" && m["state"] != "paid" {
			t.Errorf("Cai's line after the bank feed: %v", m)
		}
	}
	// A paid payment only comes back as returned, with evidence.
	e.problemIs(e.call(http.MethodPost, url(refs["cai"]), w.payroll, body("rejected")), "recordSalaryLineState", 422, salarybatch.CodeState)
	if r := e.call(http.MethodPost, url(refs["cai"]), w.payroll, body("returned")); r.Status != 200 {
		t.Fatalf("returned: %d %s", r.Status, r.Raw)
	}
	// Every state went to Nemura (released x2, sent, unknown, rejected, paid, returned), each by the state's ID.
	sent := w.statuses(t)
	if len(sent) != 7 {
		t.Fatalf("statuses queued: %d, want 7", len(sent))
	}
	// What the worker sends is read from the sources when it is sent, in the shape Nemura reads strictly (its inbox refuses a field it does not list).
	type nemuraStatus struct {
		RunID       string `json:"run_id"`
		PersonID    string `json:"person_id"`
		State       string `json:"state"`
		Instruction string `json:"instruction_id"`
		Amount      string `json:"amount"`
		PaidOn      string `json:"paid_on"`
		EvidenceRef string `json:"evidence_ref"`
	}
	states := map[string]nemuraStatus{}
	if err := e.store.Do(context.Background(), w.org, func(ctx context.Context, u storage.UnitOfWork) error {
		for _, q := range sent {
			raw, err := linksend.StatusPayload(ctx, u, q.Payload)
			if err != nil {
				return err
			}
			d := json.NewDecoder(strings.NewReader(string(raw)))
			d.DisallowUnknownFields()
			var s nemuraStatus
			if err := d.Decode(&s); err != nil {
				return err
			}
			if s.PersonID == w.cai.String() {
				states[s.State] = s
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if p := states["paid"]; p.Amount != "7000.33" || p.PaidOn != "2026-10-15" || p.Instruction != "SB0001-R1-L02" || p.RunID != w.run || p.EvidenceRef != "" {
		t.Errorf("the paid status Nemura is sent: %+v", p)
	}
	if r := states["returned"]; r.EvidenceRef != evidence || r.Amount != "7000.33" {
		t.Errorf("the returned status Nemura is sent: %+v", r)
	}
	if hit := w.trailHas(t, "Ana Lim", "Cai Yu", "9000.11", "0123456789"); len(hit) > 0 { // the bank line and its posting are the bank's own record and hold the debit
		t.Errorf("the audit trail holds %v", hit)
	}
}

// matrixSalaryStates are the authorisation matrix's rows of what became of a salary payment: under C on the pay-run row by a person stepped up.
var matrixSalaryStates = map[string]func(e *env, tok string) (response, func() bool){
	"recordSalaryLineState": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/salary-batches/"+uuid.NewString()+"/lines/"+uuid.NewString()+"/state", steppedUp(e, tok), map[string]any{"state": "paid", "evidence_id": uuid.NewString(), "on": "2026-10-15"}), nil
	},
	"matchSalaryBankLine": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/salary-batches/"+uuid.NewString()+"/bank-match", steppedUp(e, tok), map[string]any{"bank_line_id": uuid.NewString(), "line_ids": []string{uuid.NewString()}}, "If-Match", `"1-open"`), nil
	},
}

// The payroll go-live gate holds a pay run back from approval for payment until how the month's statutory outputs are met is recorded as evidence (FR-PAY-15, G4-009; PRD-001 FR-PPL-09's
// end): with nothing recorded a salary batch is not asked for (PPL-30, naming each item and no person); an arrangement needs the evidence, a provider's name, and a reason when it is "not
// applicable" (the Auto-Inclusion Scheme and IR21 only); Nemura's own issuing of payslips and KETs counts only where payroll lives in Nemura; a later month's arrangement does not cover an
// earlier wage month; and once every item is met the batch is asked for.
// part of FR-PAY-15 (the ✖ check on Checks, and a director on "No salary", are open)
// asvs: V8.2.1, V16.3.2
func TestThePayrollGoLiveGateHoldsAPayRunUntilItsArrangementsAreRecorded(t *testing.T) {
	w := newSalaryWorldWithoutGate(t)
	e := w.e
	id, tag, _, _ := w.preparedBatchWithoutAsking(t)
	e.problemIs(e.call(http.MethodPost, "/v1/salary-batches/"+id+"/approval", w.payroll, nil, "If-Match", tag), "requestSalaryBatchApproval", 409, peoplelink.CodeGate)
	read := e.call(http.MethodGet, "/v1/payroll-arrangements?month=2026-10", w.payroll, nil)
	if read.Status != 200 || read.Body["met"] != false || len(read.Body["missing"].([]any)) != 5 {
		t.Fatalf("the arrangements: %d %s", read.Status, read.Raw)
	}
	ev := w.evidence(t)
	rec := func(body map[string]any) response {
		return e.call(http.MethodPost, "/v1/payroll-arrangements", w.payroll, body, "Idempotency-Key", uuid.NewString())
	}
	for what, body := range map[string]map[string]any{
		"Nemura's where payroll is not in Nemura": {"item": "payslips", "method": "nemura", "effective_from": "2026-10"},
		"a provider with no name":                 {"item": "payslips", "method": "provider", "evidence_id": ev, "effective_from": "2026-10"},
		"a manual process with no evidence":       {"item": "kets", "method": "manual", "effective_from": "2026-10"},
		"not applicable for payslips":             {"item": "payslips", "method": "not_applicable", "note": "none", "evidence_id": ev, "effective_from": "2026-10"},
		"not applicable with no reason":           {"item": "ais", "method": "not_applicable", "evidence_id": ev, "effective_from": "2026-10"},
		"evidence that is not a file":             {"item": "kets", "method": "manual", "evidence_id": uuid.NewString(), "effective_from": "2026-10"},
	} {
		if r := rec(body); r.Status != http.StatusUnprocessableEntity && r.Status != http.StatusConflict {
			t.Errorf("%s: %d %s", what, r.Status, r.Raw)
		}
	}
	// A later month's arrangement does not cover October.
	if r := rec(map[string]any{"item": "kets", "method": "manual", "evidence_id": ev, "effective_from": "2026-11"}); r.Status != http.StatusCreated {
		t.Fatalf("a later arrangement: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodGet, "/v1/payroll-arrangements?month=2026-10", w.payroll, nil); len(r.Body["missing"].([]any)) != 5 {
		t.Errorf("a later month's arrangement covered an earlier month: %s", r.Raw)
	}
	for _, body := range []map[string]any{{"item": "payslips", "method": "provider", "provider": "Talenox", "evidence_id": ev, "effective_from": "2026-10"},
		{"item": "kets", "method": "manual", "evidence_id": ev, "effective_from": "2026-10"}, {"item": "cpf_sdl_shg", "method": "manual", "note": "EZPay", "evidence_id": ev, "effective_from": "2026-10"},
		{"item": "ais", "method": "not_applicable", "note": "Fewer than five employees", "evidence_id": ev, "effective_from": "2026-10"}} {
		if r := rec(body); r.Status != http.StatusCreated {
			t.Fatalf("recording %v: %d %s", body["item"], r.Status, r.Raw)
		}
	}
	e.problemIs(e.call(http.MethodPost, "/v1/salary-batches/"+id+"/approval", w.payroll, nil, "If-Match", tag), "requestSalaryBatchApproval", 409, peoplelink.CodeGate)
	if r := e.call(http.MethodGet, "/v1/payroll-arrangements?month=2026-10", w.payroll, nil); fmt.Sprint(r.Body["missing"]) != "[ir21]" {
		t.Errorf("one item is left: %s", r.Raw)
	}
	// Where payroll lives in Nemura, its issuing of payslips and KETs is the arrangement of those two.
	if r := rec(map[string]any{"item": "ir21", "method": "manual", "evidence_id": ev, "effective_from": "2026-10"}); r.Status != http.StatusCreated {
		t.Fatalf("ir21: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/salary-batches/"+id+"/approval", w.payroll, nil, "If-Match", tag); r.Status != http.StatusAccepted {
		t.Fatalf("with every item met: %d %s", r.Status, r.Raw)
	}
	if hit := w.trailHas(t, "Ana Lim", "0123456789"); len(hit) > 0 {
		t.Errorf("the audit trail holds %v", hit)
	}
}

// preparedBatchWithoutAsking is preparedBatch up to the approval request: the batch received and prepared.
func (w *salaryWorld) preparedBatchWithoutAsking(t *testing.T) (id, tag string, _ response, lines map[string]uuid.UUID) {
	t.Helper()
	e := w.e
	refA, refB := orderedRefs()
	got := w.payments(w.run, w.line(refA, w.ana, "Ana Lim", "9000.11", 1), w.line(refB, w.cai, "Cai Yu", "7000.33", 1))
	if got.Status != http.StatusOK {
		t.Fatalf("the lines: %d %s", got.Status, got.Raw)
	}
	id = got.str("batch_id")
	read := e.call(http.MethodGet, "/v1/salary-batches/"+id, w.payroll, nil)
	prep := e.call(http.MethodPost, "/v1/salary-batches/"+id+"/prepare", w.payroll, map[string]any{"money_account_id": w.bank.String(), "payment_date": "2026-10-31", "method": "fast"}, "If-Match", read.Header.Get("ETag"))
	if prep.Status != 200 {
		t.Fatalf("preparing: %d %s", prep.Status, prep.Raw)
	}
	return id, prep.Header.Get("ETag"), response{}, map[string]uuid.UUID{"ana": refA, "cai": refB}
}

// orderedRefs are two line IDs in the order a batch lists its lines: both lines of a submission share one transaction's timestamp, so the
// listing breaks the tie by line ID, and an instruction ID's ordinal (SB0001-R1-L02) follows it. The test names the second line's, so the
// IDs it makes must sort the way it expects (they were random, and the test failed one run in two).
func orderedRefs() (first, second uuid.UUID) {
	first, second = uuid.New(), uuid.New()
	if second.String() < first.String() {
		first, second = second, first
	}
	return first, second
}

func csvOf(rows ...string) string {
	return base64.StdEncoding.EncodeToString([]byte(strings.Join(rows, "\n") + "\n"))
}

// A provider's journal or a file import is a draft, once per source, period and version (FR-PPL-05, FR-PPL-09's "Imports stay drafts"; PRD-002 NM-EXT-02, NM-EXT-03): the CSV by account and
// department becomes a draft journal under Journals, never posted by the import; the same version and file again is a repeat, another file for the same version is refused, a later version
// drafts only its difference once the earlier journal is posted, and a preview shows the lines and saves nothing; a file that names a person, an account the chart lacks, a row with both a debit and
// a credit, or debits and credits that differ is refused with every problem; and while payroll lives in Nemura (the link is live and its source for payroll is Nemura) no import is taken.
// part of FR-PPL-05 (salary payment lines from an external system and a provider connector are open)
// req: NM-EXT-03
// asvs: V2.2.1, V8.2.1, V16.3.2
func TestAProvidersPayrollJournalIsOnlyEverADraftOncePerPeriodAndVersion(t *testing.T) {
	w := newSalaryWorldWithoutGate(t)
	e := w.e
	post := func(system string, version int, content string, prefer ...string) response {
		hdr := []string{"Idempotency-Key", uuid.NewString()}
		if len(prefer) > 0 {
			hdr = append(hdr, "Prefer", prefer[0])
		}
		return e.call(http.MethodPost, "/v1/payroll-imports", w.payroll, map[string]any{"source_system": system, "period": "2026-10", "version": version, "content_base64": content}, hdr...)
	}
	v1 := csvOf("account,department,debit,credit,memo", "6013,GA,10000.00,,G&A", "6011,RD,20000.00,,R&D", "6020,,5100.00,,CPF", "2110,,,35100.00,Payable")
	if r := post("talenox", 1, v1, "return=preview"); r.Status != 200 || r.str("outcome") != "preview" || len(r.Body["lines"].([]any)) != 4 || r.str("journal_id") != "" {
		t.Fatalf("the preview: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodGet, "/v1/payroll-imports", w.payroll, nil); len(r.Body["items"].([]any)) != 0 {
		t.Fatalf("the preview saved something: %s", r.Raw)
	}
	first := post("talenox", 1, v1)
	if first.Status != http.StatusCreated || first.str("outcome") != "drafted" || first.str("journal_id") == "" || first.str("posting_date") != "2026-10-31" {
		t.Fatalf("the import: %d %s", first.Status, first.Raw)
	}
	var status storage.DocumentStatus
	if err := e.store.Do(context.Background(), w.org, func(ctx context.Context, u storage.UnitOfWork) error {
		d, err := u.Documents().Get(ctx, uuid.MustParse(first.str("journal_id")))
		status = d.Status
		return err
	}); err != nil || status != storage.DocumentDraft {
		t.Fatalf("the journal is %s (%v), not a draft", status, err)
	}
	if again := post("talenox", 1, v1); again.Status != http.StatusCreated || again.str("outcome") != "unchanged" || again.str("journal_id") != first.str("journal_id") {
		t.Errorf("the same file again: %d %s", again.Status, again.Raw)
	}
	e.problemIs(post("talenox", 1, csvOf("account,department,debit,credit", "6013,GA,1.00,,", "2110,,,1.00")), "importPayrollJournal", 409, storage.CodePayrollRun)
	e.problemIs(post("talenox", 3, v1), "importPayrollJournal", 422, CodeInvalid)
	// Version 2 waits for version 1's journal to post; then only the difference is drafted.
	v2 := csvOf("account,department,debit,credit,memo", "6013,GA,10000.00,,G&A", "6011,RD,20200.00,,R&D", "6020,,5100.00,,CPF", "2110,,,35300.00,Payable")
	e.problemIs(post("talenox", 2, v2), "importPayrollJournal", 409, storage.CodePayrollRun)
	if _, err := (journal.Service{Store: e.store}).Post(context.Background(), w.org, uuid.MustParse(first.str("journal_id"))); err != nil {
		t.Fatalf("posting the first draft: %v", err)
	}
	second := post("talenox", 2, v2)
	if second.Status != http.StatusCreated || second.str("outcome") != "drafted" || len(second.Body["lines"].([]any)) != 2 {
		t.Fatalf("version 2: %d %s", second.Status, second.Raw)
	}
	for _, l := range second.Body["lines"].([]any) {
		if a := l.(map[string]any)["amount"].(map[string]any)["amount"].(string); a != "200.00" && a != "-200.00" {
			t.Errorf("a difference line: %s", a)
		}
	}
	// Refusals name every problem and no value of the file.
	bad := post("file", 1, csvOf("employee,account,debit,credit", "Ana,6013,100.00,", "Ben,9999,,50.00"))
	if bad.Status != http.StatusUnprocessableEntity || bad.problemCode() != payrollimport.CodePerson || strings.Contains(bad.Raw, "Ana") {
		t.Errorf("a file that names a person: %d %s", bad.Status, bad.Raw)
	}
	if r := post("file", 1, csvOf("account,debit,credit", "9999,100.00,", "6013,,100.00")); r.Status != 422 || !strings.Contains(r.Raw, payrollimport.CodeAccount) {
		t.Errorf("an account the chart lacks: %d %s", r.Status, r.Raw)
	}
	if r := post("file", 1, csvOf("account,debit,credit", "6013,100.00,", "2110,,99.00")); r.Status != 422 || !strings.Contains(r.Raw, payrollimport.CodeUnbalance) {
		t.Errorf("debits and credits that differ: %d %s", r.Status, r.Raw)
	}
	if r := post("file", 1, csvOf("account,debit,credit", "6013,100.00,100.00", "2110,,")); r.Status != 422 || !strings.Contains(r.Raw, payrollimport.CodeAmount) {
		t.Errorf("a row with both: %d %s", r.Status, r.Raw)
	}
	// While payroll lives in Nemura nothing is taken.
	if err := e.store.Do(context.Background(), w.org, func(ctx context.Context, u storage.UnitOfWork) error {
		return u.PeopleLinks().SetSource(ctx, storage.PeopleSource{Feature: storage.FeaturePayroll, Source: storage.SourceNemura, EffectiveFrom: e.clock.Now(), ApprovalID: uuid.New(), SetAt: e.clock.Now()})
	}); err != nil {
		t.Fatal(err)
	}
	e.problemIs(post("talenox", 3, v2), "importPayrollJournal", 409, peoplelink.CodeInNemura)
	// And there, Nemura's issuing of payslips and KETs is those two items' arrangement.
	if r := e.call(http.MethodGet, "/v1/payroll-arrangements?month=2026-10", w.payroll, nil); fmt.Sprint(r.Body["missing"]) != "[cpf_sdl_shg ais ir21]" {
		t.Errorf("with payroll in Nemura the missing items are: %s", r.Raw)
	}
}

// matrixPayrollOutside are the authorisation matrix's rows of the go-live gate's arrangements and the provider imports.
var matrixPayrollOutside = map[string]func(e *env, tok string) (response, func() bool){
	"getPayrollArrangements": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/payroll-arrangements", tok, nil), nil
	},
	"setPayrollArrangement": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/payroll-arrangements", steppedUp(e, tok), map[string]any{"item": "ais", "method": "not_applicable", "note": "Matrix", "evidence_id": uuid.NewString(),
			"effective_from": "2026-10"}, "Prefer", "return=preview"), nil
	},
	"listPayrollImports": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/payroll-imports", tok, nil), nil
	},
	"importPayrollJournal": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/payroll-imports", tok, map[string]any{"source_system": "matrix", "period": "2026-10", "version": 1, "content_base64": csvOf("account,debit,credit", "9999,1.00,")},
			"Prefer", "return=preview"), nil
	},
}

// runCheck works one check of the catalogue out against the world's books, as the Checks engine does.
func (w *salaryWorld) runCheck(t *testing.T, key string) checks.Result {
	t.Helper()
	d, ok := checks.Find(key)
	if !ok {
		t.Fatalf("no check %s", key)
	}
	var out checks.Result
	if err := w.e.store.Do(context.Background(), w.org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		out, err = d.Run(ctx, u, &checks.Env{Base: money.MustLookup("SGD"), Today: time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// The checks of payroll that Fyniq sees for itself (PRD-001 Appendix C: C.2 #45 with FR-PAY-15, C.1 #21, C.2 #44): a month with payroll and no recorded arrangement is ✖ until each is recorded, and with
// payroll in Nemura its own payslips and KETs count; a salary account with a posting besides a posted pay run's is ⚠; a payment released to a person who was held when it was released is ✖; and no item
// names a person or an amount (FR-PPL-17).
// part of FR-PAY-15, FR-PAY-10, FR-PAY-07 (the checks' pages and the other payroll rows of Appendix C are open)
// asvs: V8.2.1, V16.3.2
func TestThePayrollChecksFindWhatTheyAreFor(t *testing.T) {
	w := newSalaryWorldWithoutGate(t)
	e := w.e
	// C.2 #45: October has a pay run and a salary batch, and nothing recorded.
	if got := w.runCheck(t, "payroll.arrangements-recorded"); got.Status != checks.Error || got.Count != 1 || got.Items[0].ID != "2026-10" || !strings.Contains(got.Items[0].Detail, "itemised payslips") ||
		strings.Contains(got.Items[0].Detail, "Ana") {
		t.Fatalf("with nothing recorded: %+v", got)
	}
	w.arrange(t, "2026-10", storage.ArrangePayslips, storage.ArrangeKETs, storage.ArrangeCPF, storage.ArrangeAIS)
	if got := w.runCheck(t, "payroll.arrangements-recorded"); got.Status != checks.Error || !strings.Contains(got.Items[0].Detail, "IR21") {
		t.Fatalf("with IR21 left: %+v", got)
	}
	w.arrange(t, "2026-10", storage.ArrangeIR21)
	if got := w.runCheck(t, "payroll.arrangements-recorded"); got.Status != checks.OK {
		t.Fatalf("with everything recorded: %+v", got)
	}

	// C.1 #21: only the pay run posts to the salary accounts of its month; then a journal does too.
	if got := w.runCheck(t, "payroll.counted-twice"); got.Status != checks.OK {
		t.Fatalf("pay run alone: %+v", got)
	}
	var gaID, cpfID uuid.UUID
	for id, code := range w.code {
		switch code {
		case "6013":
			gaID = id
		case "2110":
			cpfID = id
		}
	}
	sgd := money.MustLookup("SGD")
	hundred, _ := money.Parse("100.00", sgd)
	draft, err := (journal.Service{Store: e.store}).Create(context.Background(), w.org, journal.Draft{Date: time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC), Memo: "Salary again",
		Lines: []ledger.JournalLine{{AccountID: gaID, Amount: hundred}, {AccountID: cpfID, Amount: hundred.Neg()}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (journal.Service{Store: e.store}).Post(context.Background(), w.org, draft.ID); err != nil {
		t.Fatal(err)
	}
	if got := w.runCheck(t, "payroll.counted-twice"); got.Status != checks.Warning || got.Count != 1 || !strings.Contains(got.Items[0].Label, "6013") || strings.Contains(got.Detail+got.Items[0].Detail, "100") {
		t.Fatalf("with a journal on the salary account: %+v", got)
	}

	// C.2 #44: a payment released to a person who was held by then: Nemura's hold arrives late, dated before the release.
	if got := w.runCheck(t, "payroll.held-payment-released"); got.Status != checks.OK {
		t.Fatalf("nothing released: %+v", got)
	}
	id, _, asked, _ := w.preparedBatch(t)
	if d := w.decideSalary(w.approver, asked); d.Status != http.StatusOK {
		t.Fatalf("approving: %d %s", d.Status, d.Raw)
	}
	w.holds.answer[w.ana] = linksend.HoldAnswer{Status: "released"}
	w.holds.answer[w.cai] = linksend.HoldAnswer{Status: "released"}
	tag := e.call(http.MethodGet, "/v1/salary-batches/"+id, w.payroll, nil).Header.Get("ETag")
	if rel := e.call(http.MethodPost, "/v1/salary-batches/"+id+"/release", w.releaser, nil, "If-Match", tag); rel.Status != http.StatusOK || rel.Body["released"] != float64(2) {
		t.Fatalf("the release: %d %s", rel.Status, rel.Raw)
	}
	if got := w.runCheck(t, "payroll.held-payment-released"); got.Status != checks.OK {
		t.Fatalf("released to people who were free: %+v", got)
	}
	if err := e.store.Do(context.Background(), w.org, func(ctx context.Context, u storage.UnitOfWork) error {
		p, err := u.PeopleLinks().Person(ctx, w.ana)
		if err != nil {
			return err
		}
		from := e.clock.Now().Add(-time.Hour)
		p.Held, p.HoldVersion, p.HoldID, p.HoldFrom = true, 1, uuid.New(), &from
		return u.PeopleLinks().SavePerson(ctx, p)
	}); err != nil {
		t.Fatal(err)
	}
	got := w.runCheck(t, "payroll.held-payment-released")
	if got.Status != checks.Error || got.Count != 1 || got.Items[0].Label != "SB-0001" || strings.Contains(got.Detail+got.Items[0].Detail, "Ana") {
		t.Fatalf("released to a person held by then: %+v", got)
	}
}

// C.1 #20 "Pay lines to review": a salary batch with a person held for tax clearance waits for a decision, and the item counts lines and reasons, never a person or an amount (FR-PPL-17).
// part of FR-PPL-12, FR-PPL-17
// asvs: V8.2.1
func TestPayLinesToReviewListsTheBatchesWithLinesThatWait(t *testing.T) {
	w := newSalaryWorldWithoutGate(t)
	e := w.e
	if got := w.runCheck(t, "payroll.pay-lines-to-review"); got.Status != checks.OK {
		t.Fatalf("no batch: %+v", got)
	}
	w.preparedBatchWithoutAsking(t)
	if got := w.runCheck(t, "payroll.pay-lines-to-review"); got.Status != checks.OK {
		t.Fatalf("a prepared batch whose people are free: %+v", got)
	}
	if err := e.store.Do(context.Background(), w.org, func(ctx context.Context, u storage.UnitOfWork) error {
		p, err := u.PeopleLinks().Person(ctx, w.ana)
		if err != nil {
			return err
		}
		from := e.clock.Now().Add(-time.Hour)
		p.Held, p.HoldVersion, p.HoldID, p.HoldFrom = true, 1, uuid.New(), &from
		return u.PeopleLinks().SavePerson(ctx, p)
	}); err != nil {
		t.Fatal(err)
	}
	got := w.runCheck(t, "payroll.pay-lines-to-review")
	if got.Status != checks.Warning || got.Count != 1 || !strings.Contains(got.Items[0].Detail, "1 held") || strings.Contains(got.Detail+got.Items[0].Detail+got.Items[0].Label, "Ana") || strings.Contains(got.Detail+got.Items[0].Detail, "9000") {
		t.Fatalf("a held person's line: %+v", got)
	}
}

// Nemura sends a bank's code and name, and Fyniq looks its BIC up (design D26, the owner's decision of 21 Nov 2026): a known code gives the BIC with none sent, a code the table lacks leaves the line
// without one, a batch with such a line is not asked for approval (SB-14, no name and no figure), a person fills the BIC in when preparing the batch (a new version of the line, so an earlier approval is stale),
// a malformed BIC and a line that already has one are refused, and the file Fyniq writes carries the BIC.
// req: FR-PPL-12
// part of FR-PAY-11
// asvs: V8.2.1, V8.3.1
func TestABankCodeGivesTheBICAndAnUnknownBankNeedsAPersonToFillItIn(t *testing.T) {
	w := newSalaryWorld(t)
	e := w.e
	refA, refB := orderedRefs()
	byCode := func(line map[string]any, code string) map[string]any {
		delete(line, "bank_bic")
		line["bank_code"], line["bank_name"] = code, "A bank"
		return line
	}
	got := w.payments(w.run, byCode(w.line(refA, w.ana, "Ana Lim", "9000.11", 1), "7339"), byCode(w.line(refB, w.cai, "Cai Yu", "7000.33", 1), "9999"))
	if got.Status != http.StatusOK || got.str("outcome") != "received" {
		t.Fatalf("a submission with bank codes and no BIC: %d %s", got.Status, got.Raw)
	}
	id := got.str("batch_id")
	read := e.call(http.MethodGet, "/v1/salary-batches/"+id, w.payroll, nil)
	prepare := func(etag string, bics map[string]string, preview ...string) response {
		body := map[string]any{"money_account_id": w.bank.String(), "payment_date": "2026-10-31", "method": "fast"}
		if bics != nil {
			body["bank_bics"] = bics
		}
		return e.call(http.MethodPost, "/v1/salary-batches/"+id+"/prepare", w.payroll, body, append([]string{"If-Match", etag}, preview...)...)
	}
	prep := prepare(read.Header.Get("ETag"), nil)
	if prep.Status != http.StatusOK {
		t.Fatalf("preparing: %d %s", prep.Status, prep.Raw)
	}
	if ask := e.call(http.MethodPost, "/v1/salary-batches/"+id+"/approval", w.payroll, nil, "If-Match", prep.Header.Get("ETag")); ask.Status != http.StatusConflict && ask.Status != http.StatusUnprocessableEntity ||
		!strings.Contains(ask.Raw, salarybatch.CodeBIC) || strings.Contains(ask.Raw, "Cai") || strings.Contains(ask.Raw, "7000") || strings.Contains(ask.Raw, "0123456789") {
		t.Fatalf("asking with a line that has no BIC: %d %s", ask.Status, ask.Raw)
	}
	// A line that has one, and a BIC that is not one, are refused; so is a line that is not in the batch.
	for what, bics := range map[string]map[string]string{"a line that has a BIC": {refA.String(): "DBSSSGSG"}, "a malformed BIC": {refB.String(): "DBS"}, "a line not in the batch": {uuid.NewString(): "DBSSSGSG"}} {
		if r := prepare(prep.Header.Get("ETag"), bics); r.Status < 400 || r.Status > 499 {
			t.Errorf("%s: %d %s", what, r.Status, r.Raw)
		}
	}
	// Preview saves nothing; the fill is a new version of Cai's line and the batch moves on.
	before := e.call(http.MethodGet, "/v1/salary-batches/"+id, w.payroll, nil)
	if r := prepare(prep.Header.Get("ETag"), map[string]string{refB.String(): "ocbcsgsg"}, "Prefer", "return=preview"); r.Status != http.StatusOK {
		t.Fatalf("the preview: %d %s", r.Status, r.Raw)
	}
	if after := e.call(http.MethodGet, "/v1/salary-batches/"+id, w.payroll, nil); after.Header.Get("ETag") != before.Header.Get("ETag") {
		t.Errorf("a preview changed the batch")
	}
	filled := prepare(prep.Header.Get("ETag"), map[string]string{refB.String(): "ocbcsgsg"})
	if filled.Status != http.StatusOK {
		t.Fatalf("filling the BIC: %d %s", filled.Status, filled.Raw)
	}
	if filled.Header.Get("ETag") == prep.Header.Get("ETag") {
		t.Errorf("the batch's version did not move on")
	}
	ask := e.call(http.MethodPost, "/v1/salary-batches/"+id+"/approval", w.payroll, nil, "If-Match", filled.Header.Get("ETag"))
	if ask.Status != http.StatusAccepted {
		t.Fatalf("asking once the BIC is filled in: %d %s", ask.Status, ask.Raw)
	}
	// The file carries the derived BIC and the one filled in.
	if d := w.decideSalary(w.approver, ask); d.Status != http.StatusOK {
		t.Fatalf("approving: %d %s", d.Status, d.Raw)
	}
	w.holds.answer[w.ana] = linksend.HoldAnswer{Status: "released"}
	w.holds.answer[w.cai] = linksend.HoldAnswer{Status: "released"}
	tag := e.call(http.MethodGet, "/v1/salary-batches/"+id, w.payroll, nil).Header.Get("ETag")
	if rel := e.call(http.MethodPost, "/v1/salary-batches/"+id+"/release", w.releaser, nil, "If-Match", tag); rel.Status != http.StatusOK || rel.Body["released"] != float64(2) {
		t.Fatalf("the release: %d %s", rel.Status, rel.Raw)
	}
	rel := e.call(http.MethodGet, "/v1/salary-batches/"+id, w.releaser, nil)
	rid := rel.Body["releases"].([]any)[0].(map[string]any)["id"].(string)
	fileResp := e.call(http.MethodGet, "/v1/salary-batches/"+id+"/releases/"+rid+"/file", w.releaser, nil)
	data, _ := base64.StdEncoding.DecodeString(fileResp.str("content_base64"))
	file := string(data)
	if !strings.Contains(file, "OCBCSGSG") {
		t.Errorf("the file lacks the BIC the bank code gave or the one filled in: %q", file)
	}
}
