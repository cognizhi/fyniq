//go:build local

package postgres_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A sandbox organisation's outbound work is not run, whatever queued it (FR-INT-18): the outbox's handler ends every kind that could leave Fyniq
// (a bank sync, a payment link, a provider's events, the AI, an inbound fetch, a send to Nemura) as done with the sandbox's note and runs only the
// kinds that stay inside, by default-deny, so a new kind is blocked until it is listed. The same kinds run for a real organisation. The flag is read
// in the row's own unit of work, so no payload or earlier answer switches it off.
// req: FR-INT-18
// asvs: V8.2.1, V4.2.1
func TestAttackASandboxOrganisationsOutboundWorkIsNotRun(t *testing.T) {
	real := books(t, "Sandbox outbox Pte Ltd").Org
	st := store(t, "fyniq_api")
	creator := founder(t, st, real, 61, clock.System{}.Now().UTC().Truncate(time.Microsecond))
	sandbox := placeSandbox(t, creator)
	outbound := []string{"bank.sync", "payment_link.make", "psp.run", "psp.process", "ai.models.read", "ai.prices.read", "ai.qualify", "agents.run", "kb.compile", "inbound.fetch",
		"people_link.send", "payments.remittance", "chat.send", "a.kind.added.tomorrow"}
	inside := []string{"email.send", "notify.fanout", "checks.refresh", "evidence.commit", "reports.export", "proposal.execute"}
	var ran atomic.Int32
	kinds := map[string]outbox.Work{}
	for _, k := range append(append([]string{}, outbound...), inside...) {
		kinds[k] = func(context.Context, uuid.UUID, storage.OutboxRow) error { ran.Add(1); return nil }
	}
	h := newHarness(t, kinds)
	for _, k := range outbound {
		before := ran.Load()
		row := h.enqueue(t, sandbox, storage.NewOutboxItem{Class: storage.ClassWorker, Kind: k})
		got := outboxRow(t, sandbox, row.ID)
		if ran.Load() != before || got.State != storage.OutboxDone || !strings.Contains(got.LastError, outbox.SandboxNote) {
			t.Errorf("%s for a sandbox organisation: ran %d, %+v", k, ran.Load()-before, got)
		}
		// The same kind runs for a real organisation.
		before = ran.Load()
		if realRow := h.enqueue(t, real, storage.NewOutboxItem{Class: storage.ClassWorker, Kind: k}); k != "a.kind.added.tomorrow" && (ran.Load() != before+1 || outboxRow(t, real, realRow.ID).State != storage.OutboxDone) {
			t.Errorf("%s for a real organisation did not run", k)
		}
	}
	for _, k := range inside {
		before := ran.Load()
		h.enqueue(t, sandbox, storage.NewOutboxItem{Class: storage.ClassWorker, Kind: k})
		if ran.Load() != before+1 {
			t.Errorf("%s stays inside Fyniq and runs for a sandbox organisation, but ran %d times", k, ran.Load()-before)
		}
	}
	for _, k := range outbound {
		if outbox.RunsInSandbox(k) {
			t.Errorf("%s may run in a sandbox", k)
		}
	}
}
