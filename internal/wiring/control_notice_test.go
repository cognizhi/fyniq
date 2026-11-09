//go:build local

package wiring

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/compliance"
	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/adapters/postgres/pgtest"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A control that newly fails is told to the members holding its owner role, and nobody else, as the critical notice
// compliance.control_failed, linking to the control (CMP-01, p1-compliance-center design D5).
// req: CMP-01
// req: compliance/compliance-center Failing controls block releases or page the owner
func TestAFailingControlIsToldToItsOwnerRole(t *testing.T) {
	ctx := context.Background()
	db, err := pgtest.Start(ctx)
	if err != nil {
		t.Fatalf("the notice test needs Docker: %v", err)
	}
	t.Cleanup(db.Stop)
	api, err := postgres.Open(ctx, db.URL("fyniq_api"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	worker, err := postgres.Open(ctx, db.URL("fyniq_worker"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(worker.Close)
	org, err := api.CreateOrganisation(ctx, storage.NewOrganisation{Name: "Notices Pte Ltd", BaseCurrency: "SGD"})
	if err != nil {
		t.Fatal(err)
	}
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	join := func(n byte, role string) uuid.UUID {
		hash := make([]byte, 32)
		hash[0], hash[1] = 7, n
		if err := api.Do(ctx, org.ID, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.MemberInvitations().Create(ctx, storage.NewMemberInvitation{CodeHash: hash, DisplayName: role, Roles: []string{role},
				ExpiresAt: now.Add(time.Hour), CreatedAt: now})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		j, err := api.JoinOrganisation(ctx, storage.NewJoin{CodeHash: hash, DisplayName: role, Issuer: "urn:fyniq:notice", Subject: uuid.NewString(), Now: now})
		if err != nil {
			t.Fatal(err)
		}
		return j.UserID
	}
	controller, owner := join(1, "controller"), join(2, "owner")
	k := &Kit{Store: worker, Queue: local.NewMemory()}
	c := compliance.Control{ID: "CTL-FIN-08", Owner: "Controller"}
	if err := worker.Do(ctx, org.ID, func(ctx context.Context, u storage.UnitOfWork) error {
		s, err := u.Compliance().SaveState(ctx, storage.ControlState{Control: c.ID, Status: "fail", Detail: "1 found in 2 approval(s).",
			FailingSince: &now, RunAt: now, ChangedAt: now}, 0)
		if err != nil {
			return err
		}
		return k.TellControl(clock.System{})(ctx, u, org.ID, c, s)
	}); err != nil {
		t.Fatal(err)
	}
	su, err := pgx.Connect(ctx, db.URL("fyniq"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = su.Close(ctx) })
	var e struct {
		Type, Title, Link string
		Audience          []byte
	}
	if err := su.QueryRow(ctx, `SELECT type, title, link, audience FROM fyniq.notification_event WHERE organisation_id = $1 AND type = $2`,
		org.ID, notify.ControlFailedType).Scan(&e.Type, &e.Title, &e.Link, &e.Audience); err != nil {
		t.Fatalf("no %s notice: %v", notify.ControlFailedType, err)
	}
	var audience notify.Audience
	if err := json.Unmarshal(e.Audience, &audience); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(audience.Users, []uuid.UUID{controller}) || slices.Contains(audience.Users, owner) ||
		e.Link != "/compliance/controls?control=CTL-FIN-08" || !strings.HasPrefix(e.Title, "CTL-FIN-08 failed: 1 found in 2 approval(s). See it") {
		t.Errorf("the notice: %+v, audience %v", e, audience.Users)
	}
	if typ, ok := notify.Lookup(notify.ControlFailedType); !ok || typ.Severity != "critical" || typ.Email != storage.EmailImmediate || !typ.Locked {
		t.Errorf("the notice's type: %+v", typ)
	}
}
