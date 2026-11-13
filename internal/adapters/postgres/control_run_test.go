//go:build local

package postgres_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	registry "github.com/cognizhi/fyniq/compliance"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/compliance"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A verification's run is written by the worker only, read under row-level security, and never changes or goes
// (migration 000294, FY307).
// req: CMP-01
// req: QA-ENG-05
// asvs: V8.2.2
func TestAttackControlRunsAreTheWorkersRecord(t *testing.T) {
	ctx := context.Background()
	a, b := books(t, "Attack control runs A Pte Ltd"), books(t, "Attack control runs B Pte Ltd")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	run := storage.ControlRun{Control: "CTL-FIN-08", Frequency: "N", Check: "verify payments.dual-control", Since: now.Add(-time.Hour),
		RanAt: now, OK: true, Detail: "Nothing found"}
	var id uuid.UUID
	if err := store(t, "fyniq_worker").Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		r, err := u.Compliance().RecordRun(ctx, run)
		id = r.ID
		return err
	}); err != nil {
		t.Fatal(err)
	}
	insert := `INSERT INTO fyniq.control_run (control, frequency, check_how, since, ran_at, ok, item_count, items, detail)
	           VALUES ($1, 'N', $2, $3, $4, $5, $6, $7::jsonb, 'x')`
	for _, c := range []struct {
		what, role, stmt string
		args             []any
		states           []string
	}{
		{"the API writing a run", "fyniq_api", insert, []any{"CTL-FIN-08", "verify payments.dual-control", now.Add(-time.Hour), now, true, 0, "[]"}, []string{"42501"}},
		{"a run of no control", "fyniq_worker", insert, []any{"FIN-08", "verify payments.dual-control", now.Add(-time.Hour), now, true, 0, "[]"}, []string{"23514"}},
		{"a run of a check that is no verification", "fyniq_worker", insert, []any{"CTL-FIN-08", "job ledger.verify", now.Add(-time.Hour), now, true, 0, "[]"}, []string{"23514"}},
		{"a run that ends before it starts", "fyniq_worker", insert, []any{"CTL-FIN-08", "verify payments.dual-control", now, now.Add(-time.Hour), true, 0, "[]"}, []string{"23514"}},
		{"a run that found something and says it is fine", "fyniq_worker", insert, []any{"CTL-FIN-08", "verify payments.dual-control", now.Add(-time.Hour), now, true, 2, "[]"}, []string{"23514"}},
		{"a run with more items than it found", "fyniq_worker", insert, []any{"CTL-FIN-08", "verify payments.dual-control", now.Add(-time.Hour), now, false, 1, `[{}, {}]`}, []string{"23514"}},
		{"the worker changing a run", "fyniq_worker", `UPDATE fyniq.control_run SET ok = false WHERE id = $1`, []any{id}, []string{"42501"}},
		{"the owner changing a run", "fyniq_migrator", `UPDATE fyniq.control_run SET detail = 'something else' WHERE id = $1`, []any{id}, []string{"FY307"}},
		{"the owner deleting a run", "fyniq_migrator", `DELETE FROM fyniq.control_run WHERE id = $1`, []any{id}, []string{"FY307"}},
		{"the owner emptying the runs", "fyniq_migrator", `TRUNCATE fyniq.control_run`, nil, []string{"FY060", "FY061"}},
	} {
		err := asOrg(ctx, conn(t, c.role), a.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.stmt, c.args...); return err })
		wantState(t, c.what, err, c.states...)
	}
	for org, want := range map[uuid.UUID]int{a.Org: 1, b.Org: 0} {
		var n int
		if err := asOrg(ctx, conn(t, "fyniq_api"), org, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM fyniq.control_run`).Scan(&n)
		}); err != nil || n != want {
			t.Errorf("organisation %s sees %d runs (%v), want %d", org, n, err, want)
		}
	}
}

// Each verification finds a breach seeded past the database's own guards, with its triggers off as the superuser can,
// and records it in its run; and finds nothing where nothing breaks it (CTL-FIN-08, CTL-IAM-01, CTL-AUD-05;
// p1-compliance-center D4).
// req: CTL-FIN-08
// req: CTL-IAM-01
// req: CTL-AUD-05
// req: CMP-01
// asvs: V2.3.5, V6.3.3, V16.3.3
func TestEachVerificationFindsItsBreach(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Verifications Pte Ltd")
	api := store(t, "fyniq_api")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	requester, approver := founder(t, api, b.Org, 1, now), founder(t, api, b.Org, 2, now)
	controls, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	runner := compliance.Runner{Store: store(t, "fyniq_worker"), Clock: clock.System{}, Controls: controls}
	runs := func() map[string]storage.ControlRun {
		t.Helper()
		rep, err := runner.Verify(ctx, b.Org)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]storage.ControlRun{}
		for _, r := range rep.Runs {
			out[r.Control+" "+r.Check] = r
		}
		if len(out) != 3 {
			t.Fatalf("want the 3 verifications' runs, got %v", out)
		}
		return out
	}
	findings := func(r storage.ControlRun) []compliance.Finding {
		var out []compliance.Finding
		if err := json.Unmarshal(r.Items, &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	// Every member signed in with a second factor: nothing breaks a control yet.
	superuser := conn(t, "fyniq")
	for _, u := range []uuid.UUID{requester, approver} {
		if _, err := superuser.Exec(ctx, `INSERT INTO fyniq.user_session (token_hash, issuer, subject, provider, user_id, created_at,
			last_seen_at, expires_at, step_up_at, step_up_method) VALUES ($1, 'urn:fyniq:test', $2, 'local', $3, $4, $4, $5, $4, 'sign_in')`,
			sha(uuid.NewString()), uuid.NewString(), u, now.Add(-time.Hour), now.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	for key, r := range runs() {
		if strings.Contains(key, "second-factor") && !r.OK {
			// The books' own members, if any, have no session here: only the two founders are checked below.
			for _, f := range findings(r) {
				if f.ID == requester.String() || f.ID == approver.String() {
					t.Errorf("%s: a founder who signed in with a second factor was found: %+v", key, f)
				}
			}
			continue
		}
		if !r.OK {
			t.Errorf("%s found something in clean books: %s", key, r.Detail)
		}
	}

	// The breaches, seeded with the triggers off: a bank change approved by whoever asked for it, a batch released under
	// that approval, a session without a second factor, an export with no audit event, and an opening with no request.
	asked := uuid.New()
	envelope := `{"object":{"type":"payee_bank_change"}}`
	sum := sha256.Sum256([]byte(envelope))
	var approval, export uuid.UUID
	err = asOrg(ctx, superuser, b.Org, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL session_replication_role = replica`); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `INSERT INTO fyniq.approval_request (object_type, object_key, operation, summary, change, envelope,
			envelope_hash, policy_version, requested_by, requested_at, requester_mfa_method, requester_mfa_at, expires_at, status, decided_at)
			VALUES ('payee_bank_change', $1, 'payee_bank_change.approve', 'Change the bank details', '{}', $2, $3, 'p', $4, $5,
			'session_step_up', $5, $6, 'approved', $5) RETURNING id`, asked.String(), envelope, sum[:], requester, now,
			now.Add(time.Hour)).Scan(&approval); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO fyniq.approval_signature (request_id, step, signer, decision, envelope_hash, mfa_method,
			mfa_at, signed_at) VALUES ($1, 1, $2, 'approve', $3, 'session_step_up', $4, $4)`, approval, requester, sum[:], now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO fyniq.payment_batch_release (batch_id, batch_version, approval_id, released_by, released_at,
			writer, file_name, media_type, file_sealed, file_sha256, payments, currency, minor_units, total)
			VALUES ($1, 1, $2, $3, $4, 'payment-list@1', 'batch.csv', 'text/csv', $5, $6, 1, 'SGD', 2, 10)`,
			uuid.New(), approval, requester, now, make([]byte, 40), sum[:]); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `INSERT INTO fyniq.report_export (report, format, parameters, requested_by, requested_at, status)
			VALUES ('trial_balance', 'csv', '{}', $1, $2, 'pending') RETURNING id`, "user:"+requester.String(), now).Scan(&export)
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := superuser.Exec(ctx, `INSERT INTO fyniq.user_session (token_hash, issuer, subject, provider, user_id, created_at,
		last_seen_at, expires_at) VALUES ($1, 'urn:fyniq:test', $2, 'local', $3, $4, $4, $5)`,
		sha(uuid.NewString()), uuid.NewString(), approver, now.Add(-time.Hour), now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	opened := uuid.New()
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		return u.Audit().Record(ctx, storage.AuditRecord{Action: "evidence.opened", ObjectType: "evidence", ObjectID: opened})
	}); err != nil {
		t.Fatal(err)
	}

	got := runs()
	wantFound := map[string][]compliance.Finding{
		"CTL-FIN-08 verify payments.dual-control": {
			{Kind: "payee_bank_change", ID: asked.String(), Detail: "Approved with the signature of whoever prepared it or asked for it"},
			{Kind: "payment_batch", Detail: "Released under an approval that is not another person's"},
		},
		"CTL-IAM-01 verify members.second-factor": {
			{Kind: "member", ID: approver.String(), Detail: "Seen in 1 session(s) that passed no second factor"},
		},
		"CTL-AUD-05 verify audit.exports-reconciled": {
			{Kind: "report_export", ID: export.String(), Detail: "Asked for, with no audit event"},
			{Kind: "evidence.opened", Detail: "Recorded without the request it came from"},
		},
	}
	for key, want := range wantFound {
		r := got[key]
		if r.OK || r.Count < len(want) || !strings.Contains(r.Detail, "found in") {
			t.Errorf("%s: %+v", key, r)
		}
		found := findings(r)
		for _, w := range want {
			if !slices.ContainsFunc(found, func(f compliance.Finding) bool {
				return f.Kind == w.Kind && f.Detail == w.Detail && (w.ID == "" || f.ID == w.ID)
			}) {
				t.Errorf("%s: want %+v among %+v", key, w, found)
			}
		}
	}
	// The worker reads the latest run of each, for the Compliance Center.
	if err := runner.Store.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		latest, err := u.Compliance().LatestRuns(ctx)
		if err != nil {
			return err
		}
		if len(latest) != 3 || slices.ContainsFunc(latest, func(r storage.ControlRun) bool { return r.OK }) {
			t.Errorf("the latest runs: %+v", latest)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func sha(s string) []byte { h := sha256.Sum256([]byte(s)); return h[:] }

// The installation's organisation, as the store would find it.
type installationAt uuid.UUID

func (i installationAt) InstallationOrganisation(context.Context) (uuid.UUID, error) {
	return uuid.UUID(i), nil
}

type postureFake []ports.PostureFinding

func (p postureFake) Findings(context.Context) ([]ports.PostureFinding, error) { return p, nil }

type logsFake []string

func (l logsFake) Read(context.Context, time.Time, int) ([]string, error) { return l, nil }

// The platform's verifications run in the installation's organisation only: a critical finding open past its 7 days
// fails CTL-RES-04, an NRIC in the logs fails CTL-DP-03 and raises its alert, and another organisation records neither
// and shows both as not applicable (p1-security-hardening design D8).
// req: SEC-33
// req: CTL-RES-04
// req: CTL-DP-03
// asvs: V16.2.5
func TestPlatformVerificationsRunInTheInstallationOnly(t *testing.T) {
	ctx := context.Background()
	inst, other := books(t, "Platform verifications Pte Ltd"), books(t, "Platform verifications B Pte Ltd")
	all, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	var controls []registry.Control
	for _, c := range all {
		if c.ID == "CTL-RES-04" || c.ID == "CTL-DP-03" {
			controls = append(controls, c)
		}
	}
	now := clock.System{}.Now().UTC()
	var alerted []string
	runner := compliance.Runner{Store: store(t, "fyniq_worker"), Clock: clock.System{}, Controls: controls, Installation: installationAt(inst.Org),
		Posture: postureFake{{Name: "late", Severity: "CRITICAL", Category: "OPEN_FIREWALL", Resource: "fw", FirstSeen: now.AddDate(0, 0, -8)},
			{Name: "recent", Severity: "HIGH", Category: "PUBLIC_BUCKET_ACL", Resource: "bucket", FirstSeen: now.AddDate(0, 0, -2)}},
		Logs: logsFake{`{"msg":"journal posted"}`, "member S1234567D joined"},
		Alert: func(_ context.Context, _ storage.UnitOfWork, org uuid.UUID, control string, found []compliance.Finding) error {
			alerted = append(alerted, fmt.Sprintf("%s %s %d", org, control, len(found)))
			return nil
		}}
	verify := func(org uuid.UUID) map[string]storage.ControlState {
		t.Helper()
		rep, err := runner.Verify(ctx, org)
		if err == nil {
			err = runner.Statuses(ctx, org, &rep)
		}
		if err != nil {
			t.Fatal(err)
		}
		if org != inst.Org && len(rep.Runs) != 0 {
			t.Errorf("another organisation recorded %+v", rep.Runs)
		}
		if org == inst.Org {
			runs := map[string]storage.ControlRun{}
			for _, r := range rep.Runs {
				runs[r.Check] = r
			}
			posture, logs := runs["verify posture.findings"], runs["verify logs.scan"]
			if len(runs) != 2 || posture.OK || posture.Count != 1 || logs.OK ||
				logs.Detail != "1 found in 2 log entries of the last day: 1 log entry holds an NRIC or FIN number" ||
				strings.Contains(string(logs.Items), "S1234567D") {
				t.Errorf("the installation's runs: %+v", runs)
			}
		}
		out := map[string]storage.ControlState{}
		for _, s := range rep.States {
			out[s.Control] = s
		}
		return out
	}
	if got := verify(inst.Org); got["CTL-RES-04"].Status != "fail" || got["CTL-DP-03"].Status != "fail" {
		t.Errorf("the installation's statuses: %+v", got)
	}
	if want := inst.Org.String() + " CTL-DP-03 1"; len(alerted) != 1 || alerted[0] != want {
		t.Errorf("alerted %v, want [%s]", alerted, want)
	}
	if got := verify(other.Org); got["CTL-RES-04"].Status != "not_applicable" || got["CTL-DP-03"].Status != "not_applicable" {
		t.Errorf("another organisation's statuses: %+v", got)
	}
}
