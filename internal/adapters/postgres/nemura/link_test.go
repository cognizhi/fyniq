//go:build local

package nemura_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/apiclient"
	"github.com/cognizhi/fyniq/internal/nemura/authz"
	"github.com/cognizhi/fyniq/internal/nemura/link"
	"github.com/cognizhi/fyniq/internal/nemura/people"
)

// Nemura's side of the link (NM-ARC-04, NM-INT-03, 08, 10): the link has its epoch and its own key, held for the worker alone; what Fyniq sends is verified against Fyniq's key, once by its
// event ID, strictly, and a payment's state goes through the one function of nemura.pay_payment_event; a run's posting answer is kept; a hold is signed from the hold table's own row at the next
// sequence; nothing a forger sends changes anything, and the inbox cannot be changed.
// asvs: V8.2.1, V4.2.1, V11.2.1
func TestTheLinkReceivesFyniqsEventsOnceAndSignsTheHoldsItSends(t *testing.T) {
	ctx := context.Background()
	lt := newLeaveTeam(t)
	now := d("2026-11-20").Add(10 * time.Hour)
	st := store(t, "nemura_api")
	packsSvc, err := nemura.NewPacksService(st, clock.NewFrozen(now))
	if err != nil {
		t.Fatal(err)
	}
	blobs, _ := local.NewDir(t.TempDir())
	svc := nemura.PayrollService{Store: st, Clock: clock.NewFrozen(now), Packs: packsSvc, Blobs: blobs, Cipher: testCipher(t)}
	prep := api.Session{Organisation: lt.c.org, Member: lt.c.pay}
	approver := api.Session{Organisation: lt.c.org, Member: lt.c.owner, StepUpAt: &now}
	birth := d("1990-05-20")
	gid, _ := svc.CreateGroup(ctx, prep, api.PayGroupInput{Name: "Monthly", PayDay: 25, CutoffDays: 7, ApprovalDays: 3})
	if err := svc.SetPay(ctx, prep, lt.ben, d("2026-01-05"), api.PersonPayInput{GroupID: gid, BasicMonthly: "4500.00", SHGFund: "CDAC"}); err != nil {
		t.Fatal(err)
	}
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.SetClassification(ctx, tx, lt.c.org, now, lt.c.owner, lt.ben, d("2026-01-06"), people.Classification{ContractType: "service", EACovered: true, PartIVClass: "none", Residency: "citizen", BirthDate: &birth})
	})
	run, err := svc.StartRun(ctx, prep, d("2026-06-01"), mustUUID(gid))
	if err != nil {
		t.Fatal(err)
	}
	runID := mustUUID(run.ID)
	if err := svc.Approve(ctx, approver, runID, 1, run.Latest.Digest); err != nil {
		t.Fatal(err)
	}
	owed := run.Latest.Totals.Disburse

	fyniqPub, fyniqPriv, _ := ed25519.GenerateKey(rand.Reader)
	fyniqOrg := uuid.New()
	var l nemura.LinkRow
	lt.c.must(func(ctx context.Context, tx pgx.Tx) (err error) {
		l, err = nemura.MakeLink(ctx, tx, lt.c.org, now, audit.Actor{Type: "user", ID: lt.c.owner.String()}, fyniqOrg, fyniqPub)
		return err
	})
	if l.Epoch != 1 || len(l.Public) != ed25519.PublicKeySize {
		t.Fatalf("the link: %+v", l)
	}
	if err := lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.MakeLink(ctx, tx, lt.c.org, now, audit.Actor{Type: "user", ID: "x"}, fyniqOrg, fyniqPub)
		return err
	}); err == nil {
		t.Error("an organisation has one live link")
	}
	signed := func(key ed25519.PrivateKey, epoch int, typ, objectType, objectID string, version int, payload any, edit func(*link.Header)) link.Envelope {
		raw, _ := json.Marshal(payload)
		at := now.UTC().Format(time.RFC3339)
		h := link.Header{EventID: uuid.NewString(), Type: typ, SchemaVersion: 1, SourceSystem: "fyniq", SourceOrganisation: fyniqOrg.String(), TargetOrganisation: lt.c.org.String(), Epoch: epoch,
			Sequence: int64(version), ObjectType: objectType, ObjectID: objectID, ObjectVersion: version, EffectiveAt: at, RecordedAt: at, SignedAt: at, CorrelationID: uuid.NewString()}
		if edit != nil {
			edit(&h)
		}
		e, err := link.Sign(key, h, raw)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	receive := func(e link.Envelope) (string, error) {
		var out string
		err := lt.c.do(func(ctx context.Context, tx pgx.Tx) (err error) {
			out, err = nemura.ReceiveLinkEvent(ctx, tx, lt.c.org, now, e)
			if out == "refused" {
				return nil // the refusal is kept in the inbox: the transaction commits
			}
			return err
		})
		return out, err
	}
	status := func(state, instruction string) map[string]any {
		return map[string]any{"run_id": runID.String(), "person_id": lt.ben.String(), "state": state, "instruction_id": instruction, "amount": owed, "paid_on": "2026-06-25", "evidence_ref": ""}
	}
	first := signed(fyniqPriv, 1, nemura.TypePayrollPaymentStatus, "payment", lt.ben.String(), 1, status("scheduled", "INSTR-1"), nil)
	if out, err := receive(first); err != nil || out != "applied" {
		t.Fatalf("a payment state: %q %v", out, err)
	}
	if out, err := receive(first); err != nil || out != "duplicate" {
		t.Errorf("the same event again: %q %v", out, err)
	}
	if out, err := receive(signed(fyniqPriv, 1, nemura.TypePayrollPaymentStatus, "payment", lt.ben.String(), 2, status("paid", "INSTR-1"), nil)); err != nil || out != "applied" {
		t.Errorf("paid: %q %v", out, err)
	}
	// An illegal move is refused after the signature held, kept in the inbox, and changes nothing.
	if out, err := receive(signed(fyniqPriv, 1, nemura.TypePayrollPaymentStatus, "payment", lt.ben.String(), 3, status("scheduled", "INSTR-2"), nil)); out != "refused" {
		t.Errorf("paid to scheduled: %q %v", out, err)
	}
	// Forgeries change nothing and are not kept.
	_, otherPriv, _ := ed25519.GenerateKey(rand.Reader)
	for name, e := range map[string]link.Envelope{
		"another key":    signed(otherPriv, 1, nemura.TypePayrollPaymentStatus, "payment", lt.ben.String(), 4, status("returned", "INSTR-1"), nil),
		"an old epoch":   signed(fyniqPriv, 2, nemura.TypePayrollPaymentStatus, "payment", lt.ben.String(), 4, status("returned", "INSTR-1"), nil),
		"a stale time":   signed(fyniqPriv, 1, nemura.TypePayrollPaymentStatus, "payment", lt.ben.String(), 4, status("returned", "INSTR-1"), func(h *link.Header) { h.SignedAt = now.Add(-time.Hour).UTC().Format(time.RFC3339) }),
		"another target": signed(fyniqPriv, 1, nemura.TypePayrollPaymentStatus, "payment", lt.ben.String(), 4, status("returned", "INSTR-1"), func(h *link.Header) { h.TargetOrganisation = uuid.NewString() }),
	} {
		if out, err := receive(e); err == nil || out == "applied" {
			t.Errorf("%s: %q %v", name, out, err)
		}
	}
	altered := signed(fyniqPriv, 1, nemura.TypePayrollPaymentStatus, "payment", lt.ben.String(), 4, status("returned", "INSTR-1"), nil)
	altered.Payload = json.RawMessage(strings.Replace(string(altered.Payload), "returned", "paid", 1))
	if out, err := receive(altered); err == nil || out == "applied" {
		t.Errorf("altered payload: %q %v", out, err)
	}
	extra := status("returned", "INSTR-1")
	extra["nric"] = "S1234567D"
	if out, err := receive(signed(fyniqPriv, 1, nemura.TypePayrollPaymentStatus, "payment", lt.ben.String(), 4, extra, nil)); out != "refused" {
		t.Errorf("a field the contract does not list: %q %v", out, err)
	}
	// A run's posting answer is kept, once.
	posted := map[string]any{"run_id": runID.String(), "version": 1, "status": "refused", "posting_ref": "", "code": "STORAGE-INV-03", "message": "The period is closed."}
	if out, err := receive(signed(fyniqPriv, 1, nemura.TypePayrollRunPosted, "payroll_run", runID.String(), 1, posted, nil)); err != nil || out != "applied" {
		t.Errorf("a posting answer: %q %v", out, err)
	}
	if out, err := receive(signed(fyniqPriv, 1, nemura.TypePayrollRunPosted, "payroll_run", runID.String(), 2, posted, nil)); err != nil || out != "ignored_older" {
		t.Errorf("the same answer again: %q %v", out, err)
	}
	var n int
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM nemura.pay_payment_event WHERE run_id = $1 AND person_id = $2`, runID, lt.ben).Scan(&n)
	})
	if n != 2 {
		t.Errorf("only scheduled and paid were recorded: %d events", n)
	}
	// The inbox keeps each answer once and cannot be changed.
	for _, x := range []struct{ role, stmt string }{
		{"nemura_migrator", "UPDATE nemura.link_inbox SET outcome = 'applied'"},
		{"nemura_migrator", "DELETE FROM nemura.link_run_posting"},
		{"nemura_api", "SELECT link_private FROM nemura.fyniq_link_secret"},
	} {
		err := store(t, x.role).InOrg(ctx, lt.c.org, func(ctx context.Context, tx pgx.Tx) error { _, err := tx.Exec(ctx, x.stmt); return err })
		if err == nil {
			t.Errorf("%s ran %q", x.role, x.stmt)
		}
	}
	// A hold goes to Fyniq signed with Nemura's own key from the hold table's row, at the next sequence (the hold's own change queued the first: sequence 2 here); the worker alone reads the key.
	var hold link.Envelope
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.PutTaxHold(ctx, tx, lt.c.org, now, lt.c.hr, lt.ben, d("2026-06-30"), "resigned")
		return err
	})
	if err := lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
		_, _, err := nemura.TaxHoldChanged(ctx, tx, lt.c.org, now, lt.ben)
		return err
	}); err == nil {
		t.Error("the API read the link's private key")
	}
	if err := store(t, "nemura_worker").InOrg(ctx, lt.c.org, func(ctx context.Context, tx pgx.Tx) error {
		e, ok, err := nemura.TaxHoldChanged(ctx, tx, lt.c.org, now, lt.ben)
		hold = e
		if err == nil && !ok {
			t.Error("a hold to send")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := hold.Verify(link.Link{Key: l.Public, Epoch: 1, SourceOrganisation: lt.c.org.String(), TargetOrganisation: fyniqOrg.String(), SourceSystem: "nemura"}, now); err != nil {
		t.Errorf("the hold verifies with Nemura's public key: %v", err)
	}
	if hold.Header.Type != "people.tax_hold.changed" || hold.Header.ObjectVersion != 1 || hold.Header.Sequence != 2 || strings.Contains(string(hold.Payload), "resigned") {
		t.Errorf("the hold event: %+v %s", hold.Header, hold.Payload)
	}
}

// A linked organisation's API client needs a second approver, from the day the link exists and no longer than it does (NM-ARC-04, rev 0.14; p1-payroll-link design D11): an Owner alone
// registers one before the link; with the link a request waits for an Owner or Administrator who is not the asker, with a fresh passkey; after unlinking the Owner registers one alone again;
// and a new link brings the second approver back.
// part of NM-ARC-04 (the second approver; the link flow around it is not built)
// asvs: V8.2.1, V6.4.1, V13.2.1
func TestAFinalLinkNeedsASecondApproverForAPIClientsOnlyWhileItLasts(t *testing.T) {
	ctx := context.Background()
	lt := newLeaveTeam(t)
	now := today.Add(10 * time.Hour)
	oauth := &fakeOAuth{}
	svc := nemura.APIClientService{Store: store(t, "nemura_api"), OAuth: oauth, Clock: clock.NewFrozen(now)}
	owner := api.Session{Organisation: lt.c.org, Member: lt.c.owner, StepUpAt: &now}
	stale := now.Add(-time.Hour)
	register := func(name string) error {
		_, err := svc.Register(ctx, owner, name, []string{"directory.read"}, "ES256", json.RawMessage(jwk))
		return err
	}
	if err := register("Before the link"); err != nil {
		t.Fatalf("an Owner alone registers a client before the link: %v", err)
	}
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	link := func() {
		lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
			_, err := nemura.MakeLink(ctx, tx, lt.c.org, now, audit.Actor{Type: "user", ID: lt.c.owner.String()}, uuid.New(), pub)
			return err
		})
	}
	link()
	var r *apiclient.Refusal
	if err := register("While linked"); !errors.As(err, &r) || r.Code != nemura.CodeSecondApprover {
		t.Fatalf("a linked organisation registers alone: %v", err)
	}
	asked, err := svc.RequestRegistration(ctx, owner, "While linked", []string{"directory.read"}, "ES256", json.RawMessage(jwk))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RequestRegistration(ctx, api.Session{Organisation: lt.c.org, Member: lt.c.owner, StepUpAt: &stale}, "Stale", []string{"directory.read"}, "ES256", json.RawMessage(jwk)); err == nil {
		t.Error("a request without a fresh passkey")
	}
	if _, err := svc.ApproveRegistration(ctx, owner, asked); !errors.As(err, &r) || r.Code != nemura.CodeSecondApprover {
		t.Errorf("the asker approved their own request: %v", err)
	}
	hr := api.Session{Organisation: lt.c.org, Member: lt.c.hr, StepUpAt: &now}
	if _, err := svc.ApproveRegistration(ctx, hr, asked); !errors.As(err, &r) || r.Code != nemura.CodeSecondApprover {
		t.Errorf("someone who is neither Owner nor Administrator approved: %v", err)
	}
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.GrantRole(ctx, tx, lt.c.org, now, lt.c.owner, lt.c.hr, authz.Administrator, today, nil)
		return err
	})
	if _, err := svc.ApproveRegistration(ctx, api.Session{Organisation: lt.c.org, Member: lt.c.hr, StepUpAt: &stale}, asked); err == nil {
		t.Error("an approval without a fresh passkey")
	}
	before := len(oauth.registered)
	if c, err := svc.ApproveRegistration(ctx, hr, asked); err != nil || c.Name != "While linked" || len(oauth.registered) != before+1 {
		t.Fatalf("the second approver approves: %+v %v", c, err)
	}
	if _, err := svc.ApproveRegistration(ctx, hr, asked); err == nil {
		t.Error("a request decided twice")
	}
	// Unlinked, the Owner registers alone again; a new link brings the second approver back.
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.EndLink(ctx, tx, lt.c.org, now, audit.Actor{Type: "user", ID: lt.c.owner.String()})
	})
	if err := register("After unlinking"); err != nil {
		t.Errorf("an Owner alone registers a client after unlinking: %v", err)
	}
	link()
	if err := register("Linked again"); !errors.As(err, &r) || r.Code != nemura.CodeSecondApprover {
		t.Errorf("a second link: %v", err)
	}
	var epoch int
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		l, err := nemura.LiveLink(ctx, tx)
		epoch = l.Epoch
		return err
	})
	if epoch != 2 {
		t.Errorf("a later link is the next epoch: %d", epoch)
	}
}
