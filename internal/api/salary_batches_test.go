//go:build local

package api

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
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
	payroll, approver, releaser string // sessions of the three members, stepped up now
	payrollU, approverU, relU   uuid.UUID
	holds                       *fakeHolds
	version                     int // the lines' object version the world has sent
}

func newSalaryWorld(t *testing.T) *salaryWorld {
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
		_, err = u.Banking().CreateMoneyAccount(ctx, storage.NewMoneyAccount{AccountID: chart.ID, Kind: storage.MoneyBank, Name: chart.Name, Identifier: "072-10010", Currency: "SGD", At: now})
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
// req: FR-PPL-12
// req: FR-PPL-13
// req: FR-PPL-17
// req: FR-PPL-10
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
	refA, refB := uuid.New(), uuid.New()
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
// req: FR-PPL-10
// req: FR-PAY-11
// req: SOD-4
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
