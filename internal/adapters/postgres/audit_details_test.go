//go:build local

package postgres_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/fieldseal"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// detailsKey is a deployment's fields' key for the details tests.
func detailsKey(t *testing.T) fieldseal.Sealer {
	t.Helper()
	fields, err := local.LoadOrCreateFieldKeys(filepath.Join(t.TempDir(), "fields.key"))
	if err != nil {
		t.Fatal(err)
	}
	return fields
}

// signedIn is a request's audit context: a person's sign-in from an address and a browser.
func signedIn() context.Context {
	return storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "details-user"},
		Request: storage.Request{ID: "req-1", IP: "203.0.113.7", UserAgent: "Mozilla/5.0 (Macintosh) Firefox/140.0", Session: "s-1"}})
}

// recordAs writes one event with action through s.
func recordAs(t *testing.T, s *postgres.Store, ctx context.Context, org uuid.UUID, action string) {
	t.Helper()
	if err := s.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		return u.Audit().Record(ctx, storage.AuditRecord{Action: action, ObjectType: "user", ObjectID: org})
	}); err != nil {
		t.Fatal(err)
	}
}

type requestBody struct {
	EventID uuid.UUID `json:"event_id"`
	Action  string    `json:"action"`
	Request audit.Request
}

func lastEvents(t *testing.T, org uuid.UUID, n int) []requestBody {
	t.Helper()
	stored := events(t, org)
	out := make([]requestBody, 0, n)
	for _, e := range stored[len(stored)-n:] {
		var b requestBody
		if err := json.Unmarshal([]byte(e.Body), &b); err != nil {
			t.Fatal(err)
		}
		out = append(out, b)
	}
	return out
}

func detailsOf(t *testing.T, org uuid.UUID, ids ...uuid.UUID) map[uuid.UUID]storage.AuditDetail {
	t.Helper()
	var out map[uuid.UUID]storage.AuditDetail
	if err := store(t, "fyniq_worker").Do(context.Background(), org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		out, err = u.Audit().Details(ctx, ids)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// A security event (AUD-06, PRD-001 rev 0.230) keeps its IP address and user agent beside the chain, and its body
// carries only their fingerprint; any other event keeps them in its body, retained with the records. Verification checks
// the details against the fingerprint while they exist, so a changed IP is caught.
// req: AUD-06
// req: AUD-00
// req: SEC-15
// asvs: V16.2.5, V16.4.2
func TestSecurityEventsKeepTheirDetailsBesideTheChain(t *testing.T) {
	ctx := signedIn()
	fields := detailsKey(t)
	b := books(t, "Details Pte Ltd")
	api := store(t, "fyniq_api", postgres.WithFingerprints(fields))
	recordAs(t, api, ctx, b.Org, "session.started")
	recordAs(t, api, ctx, b.Org, "invoice.noted")
	got := lastEvents(t, b.Org, 2)
	signin, other := got[0], got[1]
	if signin.Request.IP != "" || signin.Request.UserAgent != "" || !strings.HasPrefix(signin.Request.Detail, audit.FingerprintPrefix) ||
		signin.Request.ID != "req-1" || signin.Request.Session != "s-1" {
		t.Errorf("a sign-in's body keeps the details' fingerprint, never them: %+v", signin.Request)
	}
	if other.Request.IP != "203.0.113.7" || other.Request.Detail != "" {
		t.Errorf("another event keeps its request in its body: %+v", other.Request)
	}
	d := detailsOf(t, b.Org, signin.EventID, other.EventID)
	if len(d) != 1 || d[signin.EventID].IP != "203.0.113.7" || d[signin.EventID].UserAgent != "Mozilla/5.0 (Macintosh) Firefox/140.0" {
		t.Fatalf("the sign-in's details beside the chain: %+v", d)
	}
	mac, err := fields.Fingerprint(ctx, []byte("audit:"+b.Org.String()), audit.DetailValue("203.0.113.7", "Mozilla/5.0 (Macintosh) Firefox/140.0"))
	if err != nil || audit.FingerprintPrefix+base64.RawURLEncoding.EncodeToString(mac) != signin.Request.Detail {
		t.Errorf("the body's fingerprint is the deployment's of the details: %v", err)
	}

	worker := store(t, "fyniq_worker")
	blobs, signer := anchoring(t)
	reports, _ := audit.VerifyAll(context.Background(), worker, blobs, signer, fields.Fingerprint)
	if r := reportFor(t, reports, b.Org); !r.OK() || r.Details != 1 || r.Unchecked {
		t.Fatalf("before the change: %+v", r)
	}
	reports, _ = audit.VerifyAll(context.Background(), worker, blobs, signer, nil)
	if r := reportFor(t, reports, b.Org); !r.OK() || !r.Unchecked {
		t.Errorf("without the key the details are reported unchecked: %+v", r)
	}
	superuser(t, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), "UPDATE fyniq.audit_request_detail SET ip = '198.51.100.9' WHERE organisation_id = $1 AND id = $2",
			b.Org, signin.EventID)
		return err
	})
	reports, _ = audit.VerifyAll(context.Background(), worker, blobs, signer, fields.Fingerprint)
	if r := reportFor(t, reports, b.Org); r.OK() || !strings.Contains(r.Problem, "request details differ") {
		t.Errorf("a changed IP must fail verification: %+v", r)
	}
}

// The details are kept as the database itself allows (000285): never changed, never truncated, and deleted only by the
// erasure function, run by the worker, for details at least 30 days old. Whatever any role tries, nothing else goes.
// req: AUD-06
// req: AUD-08
// req: QA-ENG-05
// asvs: V16.4.2, V8.2.3
func TestAttackRequestDetailsKeepTheirRules(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Detail Attacks Pte Ltd")
	recordAs(t, store(t, "fyniq_api"), signedIn(), b.Org, "session.started")
	for _, role := range allRoles {
		for _, stmt := range []string{
			"UPDATE fyniq.audit_request_detail SET ip = ip",
			"DELETE FROM fyniq.audit_request_detail",
			"TRUNCATE fyniq.audit_request_detail",
		} {
			err := asOrg(ctx, conn(t, role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, stmt); return err })
			wantState(t, role+": "+stmt, err, "42501", "FY303", "FY060")
		}
		if role == "fyniq_worker" || role == "fyniq_migrator" { // the job's role, and the function's owner
			continue
		}
		err := asOrg(ctx, conn(t, role), b.Org, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "SELECT fyniq.audit_erase_details(now() - interval '400 days')")
			return err
		})
		wantState(t, role+" erases details", err, "42501")
	}
	for name, stmt := range map[string]string{
		"a cutoff yesterday":   "SELECT fyniq.audit_erase_details(now() - interval '1 day')",
		"no cutoff":            "SELECT fyniq.audit_erase_details(NULL)",
		"the flag set by hand": "SELECT set_config('fyniq.audit_erasing', 'on', true); DELETE FROM fyniq.audit_request_detail",
	} {
		err := asOrg(ctx, conn(t, "fyniq_worker"), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, stmt); return err })
		wantState(t, "the worker with "+name, err, "FY303", "42501")
	}
	// The function's owner, the only role that may delete at all, cannot delete a young detail even with the flag set:
	// the trigger checks each row's age by the database's clock.
	for name, stmt := range map[string]string{
		"without the flag": "DELETE FROM fyniq.audit_request_detail",
		"with the flag":    "SELECT set_config('fyniq.audit_erasing', 'on', true); DELETE FROM fyniq.audit_request_detail",
	} {
		err := asOrg(ctx, conn(t, "fyniq_migrator"), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, stmt); return err })
		wantState(t, "the owner deletes a young detail "+name, err, "FY303")
	}
	if d := detailsOf(t, b.Org, lastEvents(t, b.Org, 1)[0].EventID); len(d) != 1 {
		t.Errorf("a young detail was erased: %+v", d)
	}
}

// After the security-log retention (400 days, the owner's decision of rev 0.230) the erasure job erases a security
// event's details, never before. It writes the signed register entry first, then erases, then records the erasure; the
// event stays and the chain verifies. A second run finds nothing more.
// req: AUD-06
// req: AUD-09
// asvs: V16.4.2
func TestSecurityDetailsAreErasedAfterTheirRetention(t *testing.T) {
	ctx := signedIn()
	fields := detailsKey(t)
	b := books(t, "Erased Details Pte Ltd")
	signedOn := time.Date(2024, 1, 10, 9, 0, 0, 0, time.UTC)
	recordAs(t, store(t, "fyniq_api", postgres.WithFingerprints(fields), postgres.WithClock(clock.NewFrozen(signedOn))), ctx, b.Org, "session.started")
	ev := lastEvents(t, b.Org, 1)[0]
	worker := store(t, "fyniq_worker")
	blobs, signer := anchoring(t)

	if _, err := audit.EraseAll(context.Background(), worker, blobs, signer, clock.NewFrozen(signedOn.AddDate(0, 0, 399))); err != nil {
		t.Fatal(err)
	}
	if d := detailsOf(t, b.Org, ev.EventID); len(d) != 1 {
		t.Fatalf("details erased after 399 days: %+v", d)
	}
	if keys, _ := blobs.List(context.Background(), audit.ErasurePrefix(b.Org)); len(keys) != 0 {
		t.Fatalf("a register entry with nothing to erase: %v", keys)
	}

	later := clock.NewFrozen(signedOn.AddDate(0, 0, 401))
	if _, err := audit.EraseAll(context.Background(), worker, blobs, signer, later); err != nil {
		t.Fatal(err)
	}
	if d := detailsOf(t, b.Org, ev.EventID); len(d) != 0 {
		t.Fatalf("details kept after 401 days: %+v", d)
	}
	keys, err := blobs.List(context.Background(), audit.ErasurePrefix(b.Org))
	if err != nil || len(keys) != 1 {
		t.Fatalf("the register: %v %v", keys, err)
	}
	rc, err := blobs.Get(context.Background(), keys[0])
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(rc)
	_ = rc.Close()
	var entry audit.SignedErasure
	if err := json.Unmarshal(raw, &entry); err != nil || entry.Check(signer) != nil || entry.Erasure.Count != 1 ||
		entry.Erasure.Cutoff != signedOn.AddDate(0, 0, 1).Format(time.RFC3339Nano) {
		t.Errorf("the register's entry: %+v %v", entry, err)
	}
	last := lastEvents(t, b.Org, 1)[0]
	if last.Action != audit.ActionDetailsErased {
		t.Errorf("the erasure is not in the trail: %s", last.Action)
	}
	if lastEvents(t, b.Org, 2)[0].EventID != ev.EventID {
		t.Error("the sign-in itself must stay")
	}
	reports, _ := audit.VerifyAll(context.Background(), worker, blobs, signer, fields.Fingerprint)
	if r := reportFor(t, reports, b.Org); !r.OK() {
		t.Errorf("the chain after the erasure: %+v", r)
	}
	n, err := audit.EraseAll(context.Background(), worker, blobs, signer, later)
	if err != nil || n != 0 {
		t.Errorf("a second run: %d %v", n, err)
	}
}
