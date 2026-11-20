//go:build local

package nemura_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
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
	"github.com/cognizhi/fyniq/internal/nemura/authz"
	"github.com/cognizhi/fyniq/internal/nemura/identity"
	"github.com/cognizhi/fyniq/internal/nemura/link"
	"github.com/cognizhi/fyniq/internal/nemura/people"
)

const (
	testOrigin = "https://people.example.test"
	testRPID   = "example.test"
)

// answer is what an authenticator makes of a challenge: authenticator data for the relying party with the flags, the client data JSON and some signature bytes (Nemura cannot check the signature: the
// credential is the one enrolled in Fyniq).
func answer(challenge []byte, origin, rpID string, flags byte) link.RunApproval {
	rp := sha256.Sum256([]byte(rpID))
	ad := append(append([]byte{}, rp[:]...), flags, 0, 0, 0, 1)
	cd, _ := json.Marshal(map[string]any{"type": "webauthn.get", "challenge": base64.RawURLEncoding.EncodeToString(challenge), "origin": origin})
	enc := base64.RawURLEncoding.EncodeToString
	return link.RunApproval{AuthenticatorData: enc(ad), ClientData: enc(cd), Signature: enc([]byte("a signature only Fyniq can check"))}
}

// A pay run approved while the apps are linked carries its proof to Fyniq (NM-INT-03, 06, 07; p1-payroll-link design D3): the plain approval is closed; the approval envelope is made from the run's own rows
// by Fyniq's departments and mapping version, with the preparer and the approver as issuer and subject, and lists in plain words why it cannot be signed yet; only an answer for exactly its digest, at Nemura's
// origin, for the shared relying party, with the person present and verified, approves; the approval keeps what was signed and queues the submission in the same transaction; the sender posts exactly those
// bytes with the assertion (no figure in any outbox row); a refusal that Fyniq's side can put right is retried as a new event with the same proof, and a posting answer is shown on the run.
// part of NM-INT-06 (Fyniq verifies the signature: the two apps' own test proves the pair)
// part of NM-INT-03
// asvs: V8.2.1, V6.4.1, V7.2.1, V13.2.1
func TestAPayRunApprovedWhileLinkedCarriesItsProofAndWaitsOnFyniqsAnswer(t *testing.T) {
	ctx := context.Background()
	lt := newLeaveTeam(t)
	now := today.Add(10 * time.Hour)
	st := store(t, "nemura_api")
	packsSvc, err := nemura.NewPacksService(st, clock.NewFrozen(now))
	if err != nil {
		t.Fatal(err)
	}
	blobs, _ := local.NewDir(t.TempDir())
	svc := nemura.PayrollService{Store: st, Clock: clock.NewFrozen(now), Packs: packsSvc, Blobs: blobs}
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

	offer, fyniqPriv, fyniqOrg := linked(t, lt, now, &fakeOAuth{})
	rl := nemura.RunLinkService{Store: st, Clock: clock.NewFrozen(now), Origin: testOrigin, RPID: testRPID}

	// The plain approval is closed while linked, and so is the proof's before Fyniq has told Nemura its mapping and the people have an identity.
	if err := svc.Approve(ctx, approver, runID, 1, run.Latest.Digest); err == nil || !strings.Contains(err.Error(), "linked to Fyniq") {
		t.Fatalf("the plain approval while linked: %v", err)
	}
	env, err := rl.Envelope(ctx, approver, runID, 1)
	if err != nil || !env.Linked || env.Ready || len(env.Issues) < 2 || env.EnvelopeSHA256 != "" || env.Challenge != "" {
		t.Fatalf("an envelope with no mapping and no identities: %+v %v", env, err)
	}
	// Fyniq tells Nemura its mapping: version 3, with Engineering as its own department value.
	engineering := uuid.New()
	deliver := func(version int, departments []map[string]any) (string, error) {
		raw, _ := json.Marshal(map[string]any{"mapping_version": version, "departments": departments})
		at := now.UTC().Format(time.RFC3339)
		e, err := link.Sign(fyniqPriv, link.Header{EventID: uuid.NewString(), Type: nemura.TypeDepartmentChanged, SchemaVersion: 1, SourceSystem: "fyniq", SourceOrganisation: fyniqOrg.String(),
			TargetOrganisation: lt.c.org.String(), Epoch: 1, Sequence: int64(version), ObjectType: "payroll_mapping", ObjectID: fyniqOrg.String(), ObjectVersion: version, EffectiveAt: at, RecordedAt: at, SignedAt: at,
			CorrelationID: uuid.NewString()}, raw)
		if err != nil {
			t.Fatal(err)
		}
		var out string
		err = lt.c.do(func(ctx context.Context, tx pgx.Tx) (err error) {
			out, err = nemura.ReceiveLinkEvent(ctx, tx, lt.c.org, now, e)
			if out == "refused" {
				return nil
			}
			return err
		})
		return out, err
	}
	if out, err := deliver(3, []map[string]any{{"department_id": engineering.String(), "name": "Engineering", "cost_centre_id": nil}}); err != nil || out != "applied" {
		t.Fatalf("Fyniq's mapping: %q %v", out, err)
	}
	if out, err := deliver(2, []map[string]any{{"department_id": uuid.NewString(), "name": "Old", "cost_centre_id": nil}}); err != nil || out != "ignored_older" {
		t.Errorf("an older mapping version arriving late: %q %v", out, err)
	}
	if out, _ := deliver(4, []map[string]any{{"department_id": engineering.String(), "name": "Engineering", "cost_centre_id": nil}, {"department_id": engineering.String(), "name": "Again", "cost_centre_id": nil}}); out != "refused" {
		t.Errorf("a department twice in one mapping: %q", out)
	}
	if env, _ = rl.Envelope(ctx, approver, runID, 1); env.Ready || len(env.Issues) != 2 || !strings.Contains(env.Issues[0], "sign-in identity") {
		t.Fatalf("with the mapping, only the identities are missing: %+v", env.Issues)
	}
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		if err := nemura.LinkIdentity(ctx, tx, lt.c.org, now, audit.Actor{Type: "user", ID: lt.c.owner.String()}, lt.c.pay, identity.Identity{Issuer: "https://idp.example.test", Subject: "preparer-sub", Tenant: "t"}); err != nil {
			return err
		}
		return nemura.LinkIdentity(ctx, tx, lt.c.org, now, audit.Actor{Type: "user", ID: lt.c.owner.String()}, lt.c.owner, identity.Identity{Issuer: "https://idp.example.test", Subject: "approver-sub", Tenant: "t"})
	})
	env, err = rl.Envelope(ctx, approver, runID, 1)
	if err != nil || !env.Ready || len(env.Issues) != 0 || len(env.EnvelopeSHA256) != 64 || env.RPID != testRPID || env.Origin != testOrigin {
		t.Fatalf("the envelope: %+v %v", env, err)
	}
	// The approver is named by the identity of the session that signs (a member can have one identity per issuer), not by the earliest one: the same run for the same person signed in with another identity is another envelope,
	// and Fyniq's last word on that identity's passkey is shown before they sign (a hint: Fyniq's own check after it decides).
	if env.FyniqPasskey != "unknown" {
		t.Errorf("a session with no recorded identity has no word from Fyniq: %q", env.FyniqPasskey)
	}
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.LinkIdentity(ctx, tx, lt.c.org, now, audit.Actor{Type: "user", ID: lt.c.owner.String()}, lt.c.owner, identity.Identity{Issuer: "https://idp-b.example.test", Subject: "approver-sub-b", Tenant: "t"})
	})
	viaB := api.Session{Organisation: lt.c.org, Member: lt.c.owner, StepUpAt: &now, Provider: "zoho", Issuer: "https://idp-b.example.test", Subject: "approver-sub-b"}
	envB, err := rl.Envelope(ctx, viaB, runID, 1)
	if err != nil || !envB.Ready || envB.EnvelopeSHA256 == env.EnvelopeSHA256 || envB.FyniqPasskey != "unknown" {
		t.Fatalf("the envelope of the session signed in with the second identity: %+v %v", envB, err)
	}
	viaA := approver
	viaA.Provider, viaA.Issuer, viaA.Subject = "google", "https://idp.example.test", "approver-sub"
	if envA, err := rl.Envelope(ctx, viaA, runID, 1); err != nil || envA.EnvelopeSHA256 != env.EnvelopeSHA256 {
		t.Errorf("the session signed in with the first identity is the member's earliest-linked one: %v", err)
	}
	for i, passkey := range []bool{true, false} {
		raw, _ := json.Marshal(map[string]any{"issuer": "https://idp-b.example.test", "subject": "approver-sub-b", "finance": false, "passkey": passkey})
		at := now.UTC().Format(time.RFC3339)
		ev, err := link.Sign(fyniqPriv, link.Header{EventID: uuid.NewString(), Type: nemura.TypeIdentityAccess, SchemaVersion: 1, SourceSystem: "fyniq", SourceOrganisation: fyniqOrg.String(),
			TargetOrganisation: lt.c.org.String(), Epoch: 1, Sequence: int64(60 + i), ObjectType: "identity", ObjectID: "u", ObjectVersion: 60 + i, EffectiveAt: at, RecordedAt: at, SignedAt: at, CorrelationID: uuid.NewString()}, raw)
		if err != nil {
			t.Fatal(err)
		}
		if out, err := lt.receive(t, ev, now); err != nil || out != "applied" {
			t.Fatalf("Fyniq's word on the passkey: %q %v", out, err)
		}
		want := map[bool]string{true: "enrolled", false: "missing"}[passkey]
		if envB, err = rl.Envelope(ctx, viaB, runID, 1); err != nil || envB.FyniqPasskey != want {
			t.Errorf("Fyniq said passkey=%v: the approver is shown %q (%v)", passkey, envB.FyniqPasskey, err)
		}
	}
	e := env.Envelope
	if e.MappingVersion != 3 || e.Version != 1 || e.Supersedes != 0 || e.WageMonth != "2026-06" || e.ApproverRight != "payroll.approve" || len(e.Departments) != 1 || e.Departments[0].DepartmentID != engineering.String() ||
		e.Departments[0].Gross != run.Latest.Totals.Gross || e.EmployeeCPF != run.Latest.Totals.CPFEmployee || e.EmployerCPF != run.Latest.Totals.CPFEmployer || e.SDL != run.Latest.Totals.SDLPayable || e.SHG != run.Latest.Totals.SHG {
		t.Errorf("the envelope's figures are the run's, by Fyniq's department: %+v against %+v", e, run.Latest.Totals)
	}
	digest, _ := link.Challenge(env.EnvelopeSHA256)
	if env.Challenge != base64.RawURLEncoding.EncodeToString(digest) {
		t.Errorf("the challenge is the digest's 32 raw bytes: %s", env.Challenge)
	}
	// The same envelope for the same person is the same digest; for another approver it is another (the approver is in it).
	if again, _ := rl.Envelope(ctx, approver, runID, 1); again.EnvelopeSHA256 != env.EnvelopeSHA256 {
		t.Error("the digest of an envelope is stable")
	}

	approve := func(in api.LinkedApprovalInput) error { return rl.Approve(ctx, approver, runID, in) }
	good := answer(digest, testOrigin, testRPID, 0x05) // present and verified
	input := func(a link.RunApproval) api.LinkedApprovalInput {
		return api.LinkedApprovalInput{Version: 1, Digest: run.Latest.Digest, EnvelopeSHA256: env.EnvelopeSHA256, Approval: api.RunProof(a)}
	}
	other := sha256.Sum256([]byte("another envelope"))
	for name, in := range map[string]api.LinkedApprovalInput{
		"another challenge": input(answer(other[:], testOrigin, testRPID, 0x05)),
		"another origin":    input(answer(digest, "https://evil.example.test", testRPID, 0x05)),
		"another site":      input(answer(digest, testOrigin, "other.example.test", 0x05)),
		"no verification":   input(answer(digest, testOrigin, testRPID, 0x01)),
		"no presence":       input(answer(digest, testOrigin, testRPID, 0x04)),
		"not an assertion":  input(link.RunApproval{AuthenticatorData: "x", ClientData: "y", Signature: "z"}),
		"a stale envelope":  {Version: 1, Digest: run.Latest.Digest, EnvelopeSHA256: strings.Repeat("0", 64), Approval: api.RunProof(good)},
		"another digest":    {Version: 1, Digest: strings.Repeat("0", 64), EnvelopeSHA256: env.EnvelopeSHA256, Approval: api.RunProof(good)},
	} {
		if err := approve(in); err == nil {
			t.Errorf("%s: the run was approved", name)
		}
	}
	// The preparer cannot approve it either, with a perfect answer: the segregation is Nemura's and Fyniq's both (SOD-4).
	if err := rl.Approve(ctx, api.Session{Organisation: lt.c.org, Member: lt.c.pay, StepUpAt: &now}, runID, input(good)); err == nil {
		t.Error("the preparer approved their own run")
	}
	var proofs, queued int
	count := func() {
		lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM nemura.link_run_approval`).Scan(&proofs); err != nil {
				return err
			}
			return tx.QueryRow(ctx, `SELECT count(*) FROM nemura.link_event WHERE type = 'payroll.run.approved'`).Scan(&queued)
		})
	}
	if count(); proofs != 0 || queued != 0 {
		t.Fatalf("a refused approval left %d proofs and %d events", proofs, queued)
	}
	// An approver who is not signed in with a fresh passkey is the API's refusal; here the answer is good.
	if err := approve(input(good)); err != nil {
		t.Fatalf("the approval with proof: %v", err)
	}
	if count(); proofs != 1 || queued != 1 {
		t.Fatalf("an approval keeps its proof and queues one submission: %d proofs, %d events", proofs, queued)
	}
	if err := approve(input(good)); err == nil {
		t.Error("an approved version was approved again")
	}
	rows := queuedRows(t, lt)
	run1 := rows[len(rows)-1]
	for _, leak := range []string{run.Latest.Totals.Gross, "4500", "envelope", "authenticator", env.EnvelopeSHA256} {
		if strings.Contains(string(run1.Payload), leak) {
			t.Errorf("the outbox row carries %q: %s", leak, run1.Payload)
		}
	}

	// The sender posts exactly the bytes that were signed, with the assertion, to the run endpoint with the journal scope.
	fy := newFyniqInMiniature(t, offer, "/v1/people-link/payroll-runs", "/v1/people-link/payroll-payments")
	cipher, _ := local.NewFieldKeys(bytes.Repeat([]byte{7}, 64))
	sender := fy.sender(t, now)
	sender.Cipher = cipher
	work := sender.Work()
	if err := work(ctx, lt.c.org, run1); err != nil {
		t.Fatalf("the submission: %v", err)
	}
	if len(fy.calls) != 1 || fy.calls[0].Header.Type != link.TypePayrollRunApproved || fy.calls[0].Header.ObjectID != runID.String() || fy.calls[0].Header.ObjectVersion != 1 {
		t.Fatalf("the call: %+v", fy.calls)
	}
	var sub link.RunSubmission
	if err := json.Unmarshal(fy.calls[0].Payload, &sub); err != nil || sub.EnvelopeSHA256 != env.EnvelopeSHA256 || sub.Approval != good {
		t.Fatalf("the submission: %+v %v", sub, err)
	}
	canonical, err := link.Canonical(sub.Envelope)
	if sum := sha256.Sum256(canonical); err != nil || base64.RawURLEncoding.EncodeToString(sum[:]) != env.Challenge {
		t.Errorf("the envelope sent is the envelope signed: %v", err)
	}
	var sent link.RunEnvelope
	if err := json.Unmarshal(sub.Envelope, &sent); err != nil || sent.Approver != (link.Party{Issuer: "https://idp.example.test", Subject: "approver-sub"}) || sent.Preparer != (link.Party{Issuer: "https://idp.example.test", Subject: "preparer-sub"}) ||
		sent.Organisation != lt.c.org.String() || sent.RunID != runID.String() || sent.MappingVersion != 3 {
		t.Errorf("who prepared and who approved, by identity: %+v %v", sent, err)
	}
	if err := fy.calls[0].Verify(link.Link{Key: offer.LinkKey, Epoch: 1, SourceOrganisation: lt.c.org.String(), TargetOrganisation: fyniqOrg.String(), SourceSystem: "nemura"}, now); err != nil {
		t.Errorf("the link's signature: %v", err)
	}
	sv, err := rl.Submission(ctx, approver, runID)
	if err != nil || sv.State != "sent" || sv.Version != 1 || sv.Posting != nil || sv.Retryable {
		t.Errorf("a sent submission: %+v %v", sv, err)
	}
	if err := rl.Retry(ctx, approver, runID, 1); err == nil {
		t.Error("a submission Fyniq took was sent again")
	}

	// Fyniq refuses on the way in: the approver has no passkey there. The row finishes, the run shows the code, and sending again is allowed because Fyniq's side can put it right.
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.QueueLinkEvent(ctx, tx, lt.c.org, now, link.TypePayrollRunApproved, "payroll_run", runID.String(), 1)
		return err
	})
	rows = queuedRows(t, lt)
	fy.status, fy.code = 422, "PPL-19"
	if err := work(ctx, lt.c.org, rows[len(rows)-1]); err != nil {
		t.Fatalf("a final refusal finishes the row: %v", err)
	}
	if sv, _ = rl.Submission(ctx, approver, runID); sv.State != "refused" || sv.Code != "PPL-19" || !sv.Retryable {
		t.Errorf("a refused submission: %+v", sv)
	}
	if err := rl.Retry(ctx, approver, runID, 2); err == nil {
		t.Error("another version than the newest approved was sent again")
	}
	if err := rl.Retry(ctx, approver, runID, 1); err != nil {
		t.Fatalf("sending again after a refusal Fyniq's side can put right: %v", err)
	}
	fy.status = 0
	rows = queuedRows(t, lt)
	if err := work(ctx, lt.c.org, rows[len(rows)-1]); err != nil || len(fy.calls) != 3 {
		t.Fatalf("the second sending: %v (%d calls)", err, len(fy.calls))
	}
	if fy.calls[2].Header.EventID == fy.calls[1].Header.EventID || fy.calls[2].Header.Sequence <= fy.calls[1].Header.Sequence || string(fy.calls[2].Payload) != string(fy.calls[0].Payload) {
		t.Errorf("a new event with the same proof: %+v", fy.calls[2].Header)
	}
	// Fyniq's answer comes back through the inbox: refused with its code and words, then posted; the run shows the newest.
	answerFrom := func(seq int, body map[string]any) {
		raw, _ := json.Marshal(body)
		at := now.UTC().Format(time.RFC3339)
		e, err := link.Sign(fyniqPriv, link.Header{EventID: uuid.NewString(), Type: nemura.TypePayrollRunPosted, SchemaVersion: 1, SourceSystem: "fyniq", SourceOrganisation: fyniqOrg.String(),
			TargetOrganisation: lt.c.org.String(), Epoch: 1, Sequence: int64(100 + seq), ObjectType: "payroll_run", ObjectID: runID.String(), ObjectVersion: seq, EffectiveAt: at, RecordedAt: at, SignedAt: at, CorrelationID: uuid.NewString()}, raw)
		if err != nil {
			t.Fatal(err)
		}
		lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
			_, err := nemura.ReceiveLinkEvent(ctx, tx, lt.c.org, now, e)
			return err
		})
	}
	answerFrom(1, map[string]any{"run_id": runID.String(), "version": 1, "status": "refused", "posting_ref": "", "code": "PPL-19", "message": "The approver has no passkey enrolled in Fyniq."})
	if sv, _ = rl.Submission(ctx, approver, runID); sv.Posting == nil || sv.Posting.Status != "refused" || sv.Posting.Code != "PPL-19" || sv.Posting.Message == "" || !sv.Retryable {
		t.Errorf("Fyniq's refusal on the run: %+v", sv)
	}
	if err := rl.Retry(ctx, approver, runID, 1); err != nil {
		t.Errorf("sending again after Fyniq's answer was a refusal it can change: %v", err)
	}
	// Ben's bank, with its code, is on record before Fyniq posts: the posting queues his payment line (design D28).
	var change uuid.UUID
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		change, err = nemura.ProposeBankChangeAt(ctx, tx, cipher, lt.c.org, now, lt.benLogin, lt.ben, "123-456-7890", "7171", "DBS Bank")
		return err
	})
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.DecideBankChange(ctx, tx, lt.c.org, now, lt.c.owner, []authz.Role{authz.Owner}, change, true, "")
	})
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.ConfirmBankChange(ctx, tx, lt.c.org, now, lt.benLogin, change, true)
	})
	before := len(queuedRows(t, lt))
	answerFrom(2, map[string]any{"run_id": runID.String(), "version": 1, "status": "posted", "posting_ref": uuid.NewString(), "code": "", "message": ""})
	pending := queuedRows(t, lt)
	if len(pending) != before+1 || strings.Contains(string(pending[len(pending)-1].Payload), "1234567890") || strings.Contains(string(pending[len(pending)-1].Payload), "7171") {
		t.Fatalf("Fyniq's posting queued %d rows, or one holds the bank: %s", len(pending)-before, pending[len(pending)-1].Payload)
	}
	calls := len(fy.calls)
	if err := work(ctx, lt.c.org, pending[len(pending)-1]); err != nil || len(fy.calls) != calls+1 {
		t.Fatalf("sending the payment lines: %v (%d calls)", err, len(fy.calls)-calls)
	}
	readyEnv := fy.calls[len(fy.calls)-1]
	var ready nemura.PaymentsReadyEvent
	if err := json.Unmarshal(readyEnv.Payload, &ready); err != nil || readyEnv.Header.Type != nemura.TypePaymentsReady || readyEnv.Header.ObjectID != runID.String() || readyEnv.Header.ObjectVersion != 1 {
		t.Fatalf("the payment lines' envelope: %+v %v", readyEnv.Header, err)
	}
	if ready.RunVersion != 1 || len(ready.Lines) != 1 || ready.Lines[0].PersonID != lt.ben || ready.Lines[0].AccountNumber != "1234567890" || ready.Lines[0].BankCode != "7171" || ready.Lines[0].BankName != "DBS Bank" ||
		ready.Lines[0].BankDetailID != change || ready.Lines[0].BankDetailVersion != 1 || len(ready.Lines[0].BankDetailDigest) != 64 || ready.Lines[0].LineID != nemura.LineIDOf(runID, lt.ben) || ready.Approver.Subject == "" || ready.Preparer.Subject == "" {
		t.Errorf("the payment lines: %+v", ready)
	}
	if strings.Contains(string(readyEnv.Payload), "nric") || strings.Contains(string(readyEnv.Payload), "bank_bic") {
		t.Errorf("the payload carries more than Appendix E lists: %s", readyEnv.Payload)
	}
	var plainInTrail, reads int
	su := connect(t, db.SuperURL())
	if err := su.QueryRow(ctx, `SELECT count(*) FROM nemura.audit_event WHERE body::text LIKE '%1234567890%' OR body::text LIKE '%123-456-7890%'`).Scan(&plainInTrail); err != nil {
		t.Fatal(err)
	}
	if err := su.QueryRow(ctx, `SELECT count(*) FROM nemura.audit_event WHERE body::text LIKE '%salary bank account for a payment line%' AND body::text LIKE '%"service"%'`).Scan(&reads); err != nil {
		t.Fatal(err)
	}
	if plainInTrail != 0 || reads < 1 {
		t.Errorf("the account is in the audit trail %d times, and the service's read was audited %d times", plainInTrail, reads)
	}
	if sv, _ = rl.Submission(ctx, approver, runID); sv.Posting == nil || sv.Posting.Status != "posted" || sv.Posting.Reference == "" {
		t.Errorf("Fyniq's posting on the run: %+v", sv)
	}
	// The proof is append-only, and the API cannot read the keys nor change what was signed.
	for _, x := range []struct{ role, stmt string }{
		{"nemura_migrator", "UPDATE nemura.link_run_approval SET envelope_sha256 = repeat('0', 64)"},
		{"nemura_migrator", "DELETE FROM nemura.link_run_approval"},
		{"nemura_api", "UPDATE nemura.link_run_approval SET signature = '\\x00'"},
		{"nemura_worker", "INSERT INTO nemura.link_run_approval (run_id, version) VALUES (gen_random_uuid(), 9)"},
	} {
		err := store(t, x.role).InOrg(ctx, lt.c.org, func(ctx context.Context, tx pgx.Tx) error { _, err := tx.Exec(ctx, x.stmt); return err })
		if err == nil {
			t.Errorf("%s ran %q", x.role, x.stmt)
		}
	}
}
