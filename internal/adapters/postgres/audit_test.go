//go:build local

package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The audit trail end to end (p0-audit-chain).

func events(t *testing.T, org uuid.UUID) []audit.Stored {
	t.Helper()
	var out []audit.Stored
	if err := store(t, "fyniq_worker").Do(context.Background(), org, func(ctx context.Context, u storage.UnitOfWork) error {
		batch, err := u.Audit().Events(ctx, 0, 100_000)
		for _, e := range batch {
			out = append(out, audit.Stored{ID: e.ID, Seq: e.Seq, Body: e.Body, PrevHash: e.PrevHash, Hash: e.Hash})
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// anchoring returns a fresh bucket and signer, as the jobs use locally.
func anchoring(t *testing.T) (*local.Dir, *local.Ed25519) {
	t.Helper()
	blobs, err := local.NewDir(filepath.Join(t.TempDir(), "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	signer, err := local.NewEd25519(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return blobs, signer
}

func reportFor(t *testing.T, reports []audit.Report, org uuid.UUID) audit.Report {
	t.Helper()
	for _, r := range reports {
		if r.Organisation == org {
			return r
		}
	}
	t.Fatalf("no verification report for %s", org)
	return audit.Report{}
}

// req: AUD-01
func TestEveryEventMatchesTheAUD01Schema(t *testing.T) {
	ctx := context.Background()
	frozen := clock.NewFrozen(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))
	api := store(t, "fyniq_api")
	b := porttest.NewBooks(t, api, "AUD-01 Pte Ltd")
	actor := storage.Actor{Type: "user", ID: "mei.lin.tan"}
	ctx = storage.WithAudit(ctx, storage.AuditContext{Actor: &actor, Reason: "month-end", Request: storage.Request{ID: "req-1", IP: "203.0.113.7"}})
	svc := journal.Service{Store: postgresStore(t, "fyniq_api", frozen)}
	doc, err := svc.Create(ctx, b.Org, saleDraft(t, b, porttest.Day(10, 3), "88.00"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Post(ctx, b.Org, doc.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Reverse(ctx, b.Org, doc.ID, porttest.Day(10, 4), "duplicate"); err != nil {
		t.Fatal(err)
	}
	if err := api.Do(ctx, b.Org, closePeriod(b.Oct.ID)); err != nil {
		t.Fatal(err)
	}
	if err := store(t, "fyniq_worker").Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Balances().Rebuild(ctx)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	blobs, signer := anchoring(t)
	worker := store(t, "fyniq_worker")
	if _, err := audit.AnchorAll(ctx, worker, blobs, signer, frozen); err != nil {
		t.Fatal(err)
	}
	reports, _ := audit.VerifyAll(ctx, worker, blobs, signer)
	if r := reportFor(t, reports, b.Org); !r.OK() {
		t.Fatalf("verification: %s", r.Problem)
	}

	c := jsonschema.NewCompiler()
	c.AssertFormat()
	schema, err := c.Compile("../../../contracts/audit/audit-event.v1.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	stored := events(t, b.Org)
	actions := map[string]int{}
	for _, e := range stored {
		v, err := jsonschema.UnmarshalJSON(strings.NewReader(e.Body))
		if err != nil {
			t.Fatal(err)
		}
		if err := schema.Validate(v); err != nil {
			t.Errorf("event %d does not match the AUD-01 schema: %v\n%s", e.Seq, err, e.Body)
		}
		var body audit.Event
		_ = json.Unmarshal([]byte(e.Body), &body)
		actions[body.Action]++
	}
	for _, want := range []string{"organisation.created", "account.created", "period.created", "document.created",
		"journal_entry.posted", "document.posted", "document.reversed", "period.closed", "balance.rebuilt",
		"audit.anchor.written", "audit.verification.completed"} {
		if actions[want] == 0 {
			t.Errorf("no %s event; the trail has %v", want, actions)
		}
	}
	if _, err := audit.Chain(stored); err != nil {
		t.Fatalf("the chain does not verify: %v", err)
	}
	// The request's actor, reason and origin reach the events it caused.
	var posted audit.Event
	for _, e := range stored {
		var ev audit.Event
		_ = json.Unmarshal([]byte(e.Body), &ev)
		if ev.Action == "document.reversed" {
			posted = ev
			break
		}
	}
	if posted.Actor != (audit.Actor{Type: "user", ID: "mei.lin.tan"}) || posted.Reason != "duplicate" ||
		posted.Request.IP != "203.0.113.7" || posted.OccurredAt != "2026-10-15T09:00:00Z" {
		t.Fatalf("document.reversed = %+v", posted)
	}
}

// postgresStore opens a store with a frozen clock.
func postgresStore(t *testing.T, role string, c ports.Clock) storage.Store {
	t.Helper()
	return store(t, role, postgres.WithClock(c))
}

// req: AUD-08
// req: QA-ENG-05
// asvs: V16.3.3
func TestAttackChangesWithoutAnAuditEventFail(t *testing.T) {
	ctx := context.Background()
	b := books(t, "AUD-08 Pte Ltd")
	for name, stmt := range map[string]string{
		"a new account":          "INSERT INTO fyniq.account (code, name, type) VALUES ('9001', 'Unaudited', 'asset')",
		"a renamed account":      "UPDATE fyniq.account SET name = 'Renamed quietly' WHERE code = '1000'",
		"a new period":           "INSERT INTO fyniq.period (starts_on, ends_on) VALUES ('2027-01-01', '2027-01-31')",
		"a closed period":        "UPDATE fyniq.period SET status = 'closed' WHERE starts_on = '2026-11-01'",
		"a renamed company":      "UPDATE fyniq.organisation SET name = 'Someone Else Pte Ltd'",
		"a new document":         "WITH d AS (INSERT INTO fyniq.document (type) VALUES ('journal') RETURNING id) INSERT INTO fyniq.document_version (document_id, version, payload) SELECT id, 1, '{}' FROM d",
		"an entry with no event": "INSERT INTO fyniq.journal_entry (posting_date, posting_identity, source_type, source_id, source_version, rule_version) VALUES ('2026-10-14', 'quiet', 'attack', 'quiet', 1, 'attack@1')",
	} {
		err := asOrg(ctx, conn(t, "fyniq_api"), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, stmt); return err })
		if name == "an entry with no event" {
			wantState(t, name, err, "FY001", "FY061") // also unbalanced; either backstop refuses it
			continue
		}
		wantState(t, name, err, "FY061")
	}
	// The trail itself cannot be changed or switched off, by any role.
	for _, role := range allRoles {
		for _, stmt := range []string{
			"UPDATE fyniq.audit_event SET body = body",
			"DELETE FROM fyniq.audit_event",
			"TRUNCATE fyniq.audit_event",
			"UPDATE fyniq.audit_chain SET seq = 0",
			"SET session_replication_role = replica",
		} {
			err := asOrg(ctx, conn(t, role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, stmt); return err })
			wantState(t, role+": "+stmt, err, "42501", "FY060")
		}
	}
	err := asOrg(ctx, conn(t, "fyniq_ai"), b.Org, func(tx pgx.Tx) error {
		return auditRaw(ctx, tx, "account.created", "account", uuid.New())
	})
	wantState(t, "fyniq_ai writes an audit event", err, "42501")
}

// superuser runs fn as the database superuser with triggers off: what an attacker with full
// database control could do.
func superuser(t *testing.T, fn func(tx pgx.Tx) error) {
	t.Helper()
	ctx := context.Background()
	c := conn(t, "fyniq")
	tx, err := c.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SET LOCAL session_replication_role = replica"); err != nil {
		t.Fatal(err)
	}
	if err := fn(tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

// req: INV-12
// req: AUD-03
func TestTamperingWithTheTrailIsDetected(t *testing.T) {
	ctx := context.Background()
	worker := store(t, "fyniq_worker")
	blobs, signer := anchoring(t)
	frozen := clock.NewFrozen(time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC))

	edited, rewritten := books(t, "Tampered Pte Ltd"), books(t, "Rewritten Pte Ltd")
	if _, err := audit.AnchorAll(ctx, worker, blobs, signer, frozen); err != nil {
		t.Fatal(err)
	}
	reports, _ := audit.VerifyAll(ctx, worker, blobs, signer)
	for _, org := range []uuid.UUID{edited.Org, rewritten.Org} {
		if r := reportFor(t, reports, org); !r.OK() || r.Anchors != 1 {
			t.Fatalf("before tampering: %+v", r)
		}
	}

	// One event's body changed in place.
	superuser(t, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE fyniq.audit_event SET body = replace(body, 'Bank', 'Bunk') WHERE organisation_id = $1 AND seq = 2", edited.Org)
		return err
	})
	// The whole chain rewritten consistently after the change: only the anchor can tell.
	stored := events(t, rewritten.Org)
	superuser(t, func(tx pgx.Tx) error {
		prev := audit.Genesis
		for i, e := range stored {
			body := e.Body
			if i == 1 {
				body = strings.Replace(body, "Bank", "Bunk", 1)
			}
			h := audit.Link(prev, body)
			if _, err := tx.Exec(ctx, "UPDATE fyniq.audit_event SET body = $1, prev_hash = $2, hash = $3 WHERE organisation_id = $4 AND seq = $5",
				body, prev, h, rewritten.Org, e.Seq); err != nil {
				return err
			}
			prev = h
		}
		_, err := tx.Exec(ctx, "UPDATE fyniq.audit_chain SET head_hash = $1 WHERE organisation_id = $2", prev, rewritten.Org)
		return err
	})

	reports, err := audit.VerifyAll(ctx, worker, blobs, signer)
	if err == nil {
		t.Fatal("VerifyAll passed with two tampered trails")
	}
	if r := reportFor(t, reports, edited.Org); r.OK() || !strings.Contains(r.Problem, "altered") {
		t.Errorf("the edited trail: %+v", r)
	}
	if r := reportFor(t, reports, rewritten.Org); r.OK() || !strings.Contains(r.Problem, "rewritten") {
		t.Errorf("the rewritten trail: %+v", r)
	}
	// The failed verification is itself recorded in the trail.
	last := events(t, edited.Org)
	var e audit.Event
	_ = json.Unmarshal([]byte(last[len(last)-1].Body), &e)
	if e.Action != audit.ActionVerified || e.Changes["status"][1] != "failed" {
		t.Errorf("the last event is %s %v; want a failed verification", e.Action, e.Changes["status"])
	}
}

// req: CTL-AUD-02
func TestAnchorsAreSignedAndWrittenOnce(t *testing.T) {
	ctx := context.Background()
	worker := store(t, "fyniq_worker")
	blobs, signer := anchoring(t)
	frozen := clock.NewFrozen(time.Date(2026, 10, 15, 11, 0, 0, 0, time.UTC))
	b := books(t, "Anchored Pte Ltd")
	anchors := func() []string {
		keys, err := blobs.List(ctx, audit.AnchorPrefix(b.Org))
		if err != nil {
			t.Fatal(err)
		}
		return keys
	}
	if _, err := audit.AnchorAll(ctx, worker, blobs, signer, frozen); err != nil {
		t.Fatal(err)
	}
	keys := anchors()
	if len(keys) != 1 {
		t.Fatalf("anchors after the first run: %v", keys)
	}
	rc, err := blobs.Get(ctx, keys[0])
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := readAll(rc)
	var signed audit.Signed
	if err := json.Unmarshal(raw, &signed); err != nil {
		t.Fatal(err)
	}
	msg, _ := json.Marshal(signed.Anchor)
	if err := signer.Verify(signed.Anchor.KeyID, msg, signed.Signature); err != nil || signed.Anchor.OrganisationID != b.Org {
		t.Fatalf("the anchor's signature: %v (%+v)", err, signed.Anchor)
	}
	// Nothing new since the anchor: no new anchor.
	if _, err := audit.AnchorAll(ctx, worker, blobs, signer, frozen); err != nil {
		t.Fatal(err)
	}
	if got := anchors(); len(got) != 1 {
		t.Fatalf("an idle chain was anchored again: %v", got)
	}
	// A change, then a new anchor.
	if err := store(t, "fyniq_api").Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "1100", Name: "Receivables", Type: storage.Asset})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := audit.AnchorAll(ctx, worker, blobs, signer, frozen); err != nil {
		t.Fatal(err)
	}
	if got := anchors(); len(got) != 2 {
		t.Fatalf("anchors after a change: %v", got)
	}
	// Anchors are write-once, and a forged one fails verification.
	if err := blobs.Create(ctx, keys[0], strings.NewReader("{}")); !errors.Is(err, ports.ErrExists) {
		t.Fatalf("overwriting an anchor: %v", err)
	}
	forged := bytes.Replace(raw, []byte(signed.Anchor.HeadHash), []byte(strings.Repeat("0", 64)), 1)
	if err := blobs.Put(ctx, keys[0], bytes.NewReader(forged)); err != nil { // as someone bypassing the lock would
		t.Fatal(err)
	}
	reports, _ := audit.VerifyAll(ctx, worker, blobs, signer)
	if r := reportFor(t, reports, b.Org); r.OK() || !strings.Contains(r.Problem, "signature") {
		t.Fatalf("a forged anchor: %+v", r)
	}
}

func readAll(rc interface {
	Read([]byte) (int, error)
	Close() error
}) ([]byte, error) {
	defer func() { _ = rc.Close() }()
	var buf bytes.Buffer
	_, err := buf.ReadFrom(rc)
	return buf.Bytes(), err
}

// req: INV-12
// req: QA-ENG-05
// asvs: V16.4.2, V15.4.1
func TestTheChainStaysLinearUnderConcurrentWrites(t *testing.T) {
	ctx := context.Background()
	s := store(t, "fyniq_api")
	b := books(t, "Concurrent audit Pte Ltd")
	var wg sync.WaitGroup
	for i := range 24 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
				if _, err := u.Accounts().Create(ctx, storage.NewAccount{Code: fmt.Sprintf("7%03d", i), Name: "Concurrent", Type: storage.Expense}); err != nil {
					return err
				}
				if i%3 == 0 {
					return errors.New("abort after writing") // its event must leave no gap
				}
				return nil
			})
		}()
	}
	wg.Wait()
	stored := events(t, b.Org)
	if _, err := audit.Chain(stored); err != nil {
		t.Fatalf("the chain after concurrent writes: %v", err)
	}
	var n int
	_ = asOrg(ctx, conn(t, "fyniq_readonly"), b.Org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM fyniq.account WHERE code LIKE '7%'").Scan(&n)
	})
	if n != 16 {
		t.Fatalf("%d concurrent accounts committed; want 16", n)
	}
}

// req: SEC-15
// asvs: V16.2.5
func TestStoredEventsMaskSensitiveValues(t *testing.T) {
	ctx := context.Background()
	b := books(t, "SEC-15 Pte Ltd")
	if err := store(t, "fyniq_worker").Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		return u.Audit().Record(storage.WithReason(ctx, "updated for S1234567D"), storage.AuditRecord{
			Action: "payee.updated", ObjectType: "payee", ObjectID: uuid.New(),
			Changes: map[string][2]any{"bank_account_number": {"012-345678-9", "123-456789-001"}, "api_token": {nil, "tok_abcdefghijklmnop"}}})
	}); err != nil {
		t.Fatal(err)
	}
	stored := events(t, b.Org)
	body := stored[len(stored)-1].Body
	for _, leaked := range []string{"345678", "456789", "S1234567D", "tok_abcdefghijklmnop"} {
		if strings.Contains(body, leaked) {
			t.Errorf("the stored event contains %q: %s", leaked, body)
		}
	}
}
