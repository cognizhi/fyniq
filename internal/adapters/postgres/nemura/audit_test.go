//go:build local

package nemura_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/audit"
)

var actor = audit.Actor{Type: "user", ID: "test"}

// createOrg makes an organisation and its event in one transaction, as the unit of work does.
func createOrg(t *testing.T, org uuid.UUID, name string) {
	t.Helper()
	err := store(t, "nemura_api").InOrg(context.Background(), org, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "INSERT INTO nemura.organisation (name) VALUES ($1)", name); err != nil {
			return err
		}
		return nemura.Audit(ctx, tx, org, clock.System{}.Now(), actor, "nemura.org.created",
			audit.Object{Type: "organisation", ID: org}, map[string]audit.Change{"name": {nil, name}}, "set up")
	})
	if err != nil {
		t.Fatal(err)
	}
}

func record(t *testing.T, org uuid.UUID, action string, changes map[string]audit.Change, reason string) {
	t.Helper()
	err := store(t, "nemura_api").InOrg(context.Background(), org, func(ctx context.Context, tx pgx.Tx) error {
		return nemura.Audit(ctx, tx, org, clock.System{}.Now(), actor, action, audit.Object{Type: "organisation", ID: org}, changes, reason)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Every saved change is audited in its own transaction (AUD-01): the event commits with the change, an audited change with
// no event is refused at commit (AUD-08, NM061), and a change that rolls back leaves no event behind.
// asvs: V16.3.1
func TestAChangeAndItsAuditEventCommitTogether(t *testing.T) {
	ctx := context.Background()
	org := uuid.New()
	createOrg(t, org, "Audit One Pte Ltd")
	worker := store(t, "nemura_worker")
	if n, err := worker.VerifyAudit(ctx, org); err != nil || n != 1 {
		t.Fatalf("the trail after one change: %d events, %v", n, err)
	}

	// A change with no event of its own is refused when the transaction commits.
	other := uuid.New()
	err := store(t, "nemura_api").InOrg(ctx, other, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "INSERT INTO nemura.organisation (name) VALUES ('No Event Pte Ltd')")
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "NM061") {
		t.Errorf("an organisation with no audit event: %v", err)
	}

	// A change that rolls back takes its event with it.
	third := uuid.New()
	_ = store(t, "nemura_api").InOrg(ctx, third, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "INSERT INTO nemura.organisation (name) VALUES ('Rolled Back Pte Ltd')"); err != nil {
			return err
		}
		if err := nemura.Audit(ctx, tx, third, clock.System{}.Now(), actor, "nemura.org.created", audit.Object{Type: "organisation", ID: third}, nil, ""); err != nil {
			return err
		}
		return context.Canceled // the work failed after its event was queued
	})
	var n int
	if err := connect(t, db.SuperURL()).QueryRow(ctx, "SELECT count(*) FROM nemura.audit_event WHERE organisation_id = $1", third).Scan(&n); err != nil || n != 0 {
		t.Errorf("a rolled-back change left %d events, %v", n, err)
	}
}

// The chain links each event to the one before and holds from the first event to the head; an edited, deleted or truncated
// event is refused by the database, and when someone with the owner's rights switches the triggers off and tampers anyway,
// the verification names the break (AUD-03, AUD-08, INV-12).
// asvs: V16.4.2
func TestTheChainDetectsTampering(t *testing.T) {
	ctx := context.Background()
	org := uuid.New()
	createOrg(t, org, "Chain Pte Ltd")
	for i := range 4 {
		record(t, org, "nemura.setting.changed", map[string]audit.Change{"n": {i, i + 1}}, "")
	}
	worker := store(t, "nemura_worker")
	if n, err := worker.VerifyAudit(ctx, org); err != nil || n != 5 {
		t.Fatalf("a clean chain: %d, %v", n, err)
	}

	super := connect(t, db.SuperURL())
	// The database refuses to change the trail, whoever asks, even the owner (FORCE ROW LEVEL SECURITY aside, the trigger is the
	// backstop).
	for _, sql := range []string{
		"UPDATE nemura.audit_event SET body = body WHERE organisation_id = '" + org.String() + "'",
		"DELETE FROM nemura.audit_event WHERE organisation_id = '" + org.String() + "'",
		"TRUNCATE nemura.audit_event",
		"DELETE FROM nemura.audit_chain WHERE organisation_id = '" + org.String() + "'",
	} {
		if _, err := super.Exec(ctx, sql); err == nil || !strings.Contains(err.Error(), "NM060") {
			t.Errorf("%s: %v", sql, err)
		}
	}
	// The services cannot even try: no role holds UPDATE or DELETE.
	api := connect(t, db.URL("nemura_api"))
	if _, err := api.Exec(ctx, "DELETE FROM nemura.audit_event"); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("the API deleted from the trail: %v", err)
	}

	// Someone with the owner's rights turns the trigger off and edits an event: the chain no longer holds.
	if _, err := super.Exec(ctx, "ALTER TABLE nemura.audit_event DISABLE TRIGGER audit_event_immutable"); err != nil {
		t.Fatal(err)
	}
	if _, err := super.Exec(ctx, `UPDATE nemura.audit_event SET body = replace(body, '[1,2]', '[1,9]') WHERE organisation_id = $1 AND seq = 3`, org); err != nil {
		t.Fatal(err)
	}
	if _, err := worker.VerifyAudit(ctx, org); err == nil || !strings.Contains(err.Error(), "event 3") || !strings.Contains(err.Error(), "altered") {
		t.Errorf("an edited event: %v", err)
	}
	// An event removed from the middle breaks the sequence.
	if _, err := super.Exec(ctx, "DELETE FROM nemura.audit_event WHERE organisation_id = $1 AND seq = 2", org); err != nil {
		t.Fatal(err)
	}
	if _, err := worker.VerifyAudit(ctx, org); err == nil || !strings.Contains(err.Error(), "missing or out of order") {
		t.Errorf("a removed event: %v", err)
	}
	if _, err := super.Exec(ctx, "ALTER TABLE nemura.audit_event ENABLE TRIGGER audit_event_immutable"); err != nil {
		t.Fatal(err)
	}
}

// Secrets, tokens, full NRIC or FIN and bank numbers never reach an audit event (SEC-15): values under a secret's name are
// removed, numbers keep their last four characters, and tokens and NRIC shapes in free text are masked.
// asvs: V16.2.5, V14.1.1
func TestNoSecretReachesTheAuditTrail(t *testing.T) {
	ctx := context.Background()
	org := uuid.New()
	createOrg(t, org, "Mask Pte Ltd")
	record(t, org, "nemura.client.created", map[string]audit.Change{
		"client_secret": {nil, "s3cr3t-value-0123456789"},
		"nric":          {nil, "S1234567D"},
		"bank_account":  {nil, "0123456789"},
	}, "token eyJhbGciOiJI.eyJzdWIiOiIx.abcdef and S7654321A and Bearer abc.def.ghi")
	var body string
	if err := connect(t, db.SuperURL()).QueryRow(ctx,
		"SELECT body FROM nemura.audit_event WHERE organisation_id = $1 AND action = 'nemura.client.created'", org).Scan(&body); err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"s3cr3t-value", "S1234567D", "0123456789", "S7654321A", "eyJhbGci", "abc.def.ghi"} {
		if strings.Contains(body, leak) {
			t.Errorf("the stored event contains %q: %s", leak, body)
		}
	}
	if !strings.Contains(body, `"schema":"`+nemura.AuditSchema+`"`) {
		t.Errorf("the event is not in Nemura's format: %s", body)
	}
}

// One organisation's trail is its own: another organisation reads and verifies none of it.
// asvs: V8.2.2
func TestOneOrganisationsTrailIsInvisibleToAnother(t *testing.T) {
	ctx := context.Background()
	a, b := uuid.New(), uuid.New()
	createOrg(t, a, "Trail A Pte Ltd")
	createOrg(t, b, "Trail B Pte Ltd")
	record(t, a, "nemura.setting.changed", nil, "")
	var n int
	if err := store(t, "nemura_worker").InOrg(ctx, b, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM nemura.audit_event WHERE organisation_id = $1", a).Scan(&n)
	}); err != nil || n != 0 {
		t.Errorf("B read %d of A's events, %v", n, err)
	}
	if c, err := store(t, "nemura_worker").VerifyAudit(ctx, b); err != nil || c != 1 {
		t.Errorf("B's own chain: %d, %v", c, err)
	}
}
