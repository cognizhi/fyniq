//go:build local

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Consent keeps every purpose and record (migration 000302, FY311): a purpose is added and a consent given and
// withdrawn, each audited; a consent given twice, withdrawn when none stands, or for someone the purpose is not for, and
// a purpose or record changed or deleted are refused; the worker writes none of it; another organisation sees none. The
// library's widened keys take the notices (000300).
// req: CMP-PD-03
// req: AUD-01
// req: QA-ENG-05
// asvs: V8.2.2
func TestAttackConsentKeepsItsHistory(t *testing.T) {
	ctx := context.Background()
	a, b := books(t, "Consent A Pte Ltd"), books(t, "Consent B Pte Ltd")
	api := store(t, "fyniq_api")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	dpo := founder(t, api, a.Org, 1, now)
	contact := "contact:" + uuid.NewString()
	var purpose storage.ConsentPurpose
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		p, err := u.Compliance().CreateConsentPurpose(ctx, storage.ConsentPurpose{Name: "Newsletter", Description: "A monthly email.",
			Audience: "contacts", NeedsConsent: true, CreatedBy: "user:" + dpo.String(), CreatedAt: now})
		if err != nil {
			return err
		}
		purpose = p
		if _, err := u.Compliance().RecordConsent(ctx, storage.ConsentRecord{PurposeID: p.ID, Person: contact, Event: "given", At: now,
			RecordedBy: "user:" + dpo.String()}); err != nil {
			return err
		}
		_, err = u.Compliance().RecordConsent(ctx, storage.ConsentRecord{PurposeID: p.ID, Person: contact, Event: "withdrawn", At: now.Add(time.Second),
			RecordedBy: "user:" + dpo.String(), Note: "By email"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var actions []string
	if err := asOrg(ctx, conn(t, "fyniq_api"), a.Org, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT action FROM fyniq.audit_event WHERE object_type IN ('consent_purpose', 'consent_record') ORDER BY seq`)
		if err != nil {
			return err
		}
		actions, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	}); err != nil || len(actions) != 3 || actions[0] != "consent_purpose.created" || actions[1] != "consent.given" || actions[2] != "consent.withdrawn" {
		t.Errorf("consent's audit events: %v %v", actions, err)
	}
	record := `INSERT INTO fyniq.consent_record (purpose_id, person, event, at, recorded_by) VALUES ($1, $2, $3, now(), 'user:x')`
	for _, c := range []struct {
		what, role, stmt string
		args             []any
		states           []string
	}{
		{"the worker adding a purpose", "fyniq_worker", `INSERT INTO fyniq.consent_purpose (name, description, audience, needs_consent, created_by, created_at)
		  VALUES ('x', 'y', 'members', true, 'user:x', now())`, nil, []string{"42501"}},
		{"a consent withdrawn twice", "fyniq_api", record, []any{purpose.ID, contact, "withdrawn"}, []string{"FY311"}},
		{"a member under a purpose for contacts", "fyniq_api", record, []any{purpose.ID, "member:" + uuid.NewString(), "given"}, []string{"FY311"}},
		{"a person who is neither", "fyniq_api", record, []any{purpose.ID, "someone", "given"}, []string{"23514"}},
		{"the API changing a record", "fyniq_api", `UPDATE fyniq.consent_record SET event = 'given'`, nil, []string{"42501"}},
		{"the owner changing a purpose", "fyniq_migrator", `UPDATE fyniq.consent_purpose SET needs_consent = false WHERE id = $1`, []any{purpose.ID}, []string{"FY311"}},
		{"the owner deleting a record", "fyniq_migrator", `DELETE FROM fyniq.consent_record WHERE purpose_id = $1`, []any{purpose.ID}, []string{"FY311"}},
		{"a notice's version from an unknown draft", "fyniq_api", `INSERT INTO fyniq.policy_version (key, version, title, body, sha256, from_draft, published_by, published_at)
		  VALUES ('notice-members', 1, 'N', 'b', sha256('b'), 'fyniq-other-draft@1', 'user:x', now())`, nil, []string{"23514"}},
	} {
		err := asOrg(ctx, conn(t, c.role), a.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.stmt, c.args...); return err })
		wantState(t, c.what, err, c.states...)
	}
	// A consent given again after its withdrawal stands, and a notice is a key of the library.
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		if _, err := u.Compliance().RecordConsent(ctx, storage.ConsentRecord{PurposeID: purpose.ID, Person: contact, Event: "given",
			At: now.Add(2 * time.Second), RecordedBy: "user:" + dpo.String()}); err != nil {
			return err
		}
		_, err := u.Compliance().PublishPolicy(ctx, storage.PublishedPolicy{Key: "notice-members", Version: 1, Title: "Notice", Body: "# Notice\n",
			From: "fyniq-notice-draft@1", PublishedBy: "user:" + dpo.String(), PublishedAt: now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for org, want := range map[uuid.UUID]int{a.Org: 3, b.Org: 0} {
		if err := api.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			rs, err := u.Compliance().Consents(ctx)
			if err == nil && len(rs) != want {
				t.Errorf("organisation %s sees %d consents, want %d", org, len(rs), want)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
}
