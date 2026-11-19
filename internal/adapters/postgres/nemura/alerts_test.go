//go:build local

package nemura_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/nemura/alert"
	"github.com/cognizhi/fyniq/internal/nemura/authz"
	"github.com/cognizhi/fyniq/internal/nemura/outbox"
)

type notices struct {
	mu   sync.Mutex
	got  []alert.Payload
	late []bool
}

func (n *notices) Notify(_ context.Context, _ uuid.UUID, p alert.Payload, late bool) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.got, n.late = append(n.got, p), append(n.late, late)
	return nil
}

// A pay-day job that dies alerts the Payroll administrator and the Owner, once each, in the sweep's own transaction: nobody else, nobody whose role
// ended, and no alert for work that is not the pay-day path's; a second sweep adds nothing; the notice waits in the outbox while its consumer is
// down and goes out when it returns, on time or marked late (NM-NFR-05, NM-NFR-03).
// req: NM-NFR-05, NM-NFR-03
// asvs: V16.3.3
func TestADeadPayDayJobAlertsThePayrollAdministratorAndTheOwnerOnce(t *testing.T) {
	ctx := context.Background()
	c := newCompany(t)
	now := today.Add(12 * time.Hour)
	// Pia is the Payroll administrator, by the Owner's approval; Hana (HR) holds neither role.
	var req uuid.UUID
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		req, err = nemura.RequestRoleGrant(ctx, tx, c.org, now, c.hr, c.pay, authz.PayrollAdmin, today, today.AddDate(1, 0, 0))
		if err != nil {
			return err
		}
		return nemura.DecideRoleGrant(ctx, tx, c.org, now, c.owner, req, true, "")
	})
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.GrantRole(ctx, tx, c.org, now, c.hr, c.pay, authz.PayrollAdmin, today, &req)
		return err
	})

	worker := store(t, "nemura_worker")
	die := func(kind, key string) uuid.UUID {
		var id uuid.UUID
		c.must(func(ctx context.Context, tx pgx.Tx) error {
			row, err := nemura.Enqueue(ctx, tx, outbox.NewItem{Class: outbox.ClassWorker, Kind: kind, BusinessKey: key, MaxAttempts: 1})
			id = row.ID
			return err
		})
		if err := worker.MarkDead(ctx, c.org, id, "out of attempts", clock.System{}.Now()); err != nil {
			t.Fatal(err)
		}
		return id
	}
	other := die("test.work", "not-pay-day")
	n, err := worker.RaisePayDayAlerts(ctx, c.org, now)
	if err != nil || n != 0 {
		t.Fatalf("a job that is not the pay-day path's raised %d alerts, %v", n, err)
	}
	dead := die("payday.handoff", "run-1")
	if n, err = worker.RaisePayDayAlerts(ctx, c.org, now); err != nil || n != 2 {
		t.Fatalf("a dead pay-day job raised %d alerts, %v; want the Payroll administrator and the Owner", n, err)
	}
	if n, err = worker.RaisePayDayAlerts(ctx, c.org, now); err != nil || n != 0 {
		t.Fatalf("a second sweep raised %d more, %v", n, err)
	}
	// Told: Pia and Owen only. The notice waits, pending, with no consumer running.
	super := connect(t, db.SuperURL())
	rows, err := super.Query(ctx, "SELECT id, state, payload FROM nemura.outbox WHERE organisation_id = $1 AND kind = $2", c.org, alert.KindNotify)
	if err != nil {
		t.Fatal(err)
	}
	var queued []outbox.Row
	told := map[uuid.UUID]bool{}
	for rows.Next() {
		var r outbox.Row
		var state string
		if err := rows.Scan(&r.ID, &state, &r.Payload); err != nil {
			t.Fatal(err)
		}
		var p alert.Payload
		if err := json.Unmarshal(r.Payload, &p); err != nil || p.Source != dead || p.Kind != "payday.handoff" || p.FailedAt.IsZero() {
			t.Fatalf("a notice: %+v, %v", p, err)
		}
		if state != "pending" {
			t.Errorf("a notice with no consumer is %s, not waiting", state)
		}
		told[p.Member] = true
		queued = append(queued, r)
	}
	rows.Close()
	if len(told) != 2 || !told[c.owner] || !told[c.pay] || told[c.hr] {
		t.Errorf("told %v; want only the Owner %s and the Payroll administrator %s", told, c.owner, c.pay)
	}
	_ = other

	// The tick's sweep raises them too, for every organisation, with nothing else to run (the scheduler is all a deployment keeps running).
	c2 := newCompany(t)
	c2.must(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.Enqueue(ctx, tx, outbox.NewItem{Class: outbox.ClassWorker, Kind: "payday.payment_file", BusinessKey: "run-9", MaxAttempts: 1})
		return err
	})
	if err := worker.MarkDead(ctx, c2.org, deadOf(t, c2.org), "out of attempts", clock.System{}.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := outbox.Sweep(ctx, outbox.Dispatcher{Store: worker, Queue: &queue{}, Clock: clock.NewFrozen(now), Raise: worker.RaisePayDayAlerts}, now); err != nil {
		t.Fatal(err)
	}
	var swept int
	if err := super.QueryRow(ctx, "SELECT count(*) FROM nemura.outbox WHERE organisation_id = $1 AND kind = $2", c2.org, alert.KindNotify).Scan(&swept); err != nil || swept != 1 {
		t.Errorf("the sweep raised %d notices for the organisation whose only Owner is told, %v", swept, err)
	}

	// The consumer returns: each notice reaches its person once, on time when it is within the window and marked late when it is not.
	n2 := &notices{}
	fresh := clock.NewFrozen(clock.System{}.Now().Add(10 * time.Minute))
	late := clock.NewFrozen(clock.System{}.Now().Add(alert.Window + time.Minute))
	work := alert.Work(n2, fresh)
	for _, r := range queued {
		if err := work(ctx, c.org, r); err != nil {
			t.Fatal(err)
		}
	}
	if err := alert.Work(n2, late)(ctx, c.org, queued[0]); err != nil {
		t.Fatal(err)
	}
	if len(n2.got) != 3 || n2.late[0] || n2.late[1] || !n2.late[2] {
		t.Errorf("delivered %d notices, late flags %v", len(n2.got), n2.late)
	}
	if bad := alert.Work(n2, fresh)(ctx, c.org, outbox.Row{ID: uuid.New(), Payload: []byte(`{"member":"x"}`)}); bad == nil {
		t.Error("a malformed notice was delivered")
	}
}

// deadOf is the id of the organisation's one pay-day row.
func deadOf(t *testing.T, org uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := connect(t, db.SuperURL()).QueryRow(context.Background(), "SELECT id FROM nemura.outbox WHERE organisation_id = $1 AND kind LIKE 'payday.%'", org).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
