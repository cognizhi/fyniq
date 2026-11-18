package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/cron"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ScheduleJob queues each enabled agent's slots due before the next tick, every 5 minutes (AI-62, ADR-T7): the schedule
// lives in the database, and each slot is delivered at its time, so nothing runs between ticks.
const ScheduleJob = "agents.schedule"

// RunKind is the worker's work of a queued run.
const RunKind = "agents.run"

// Entries are a schedule's entries, by their settings' names: the daily monitor and the weekly cross-check.
var Entries = []string{"monitor", "cross_check"}

// Window is how far ahead a tick queues slots: past the next tick, so each slot is queued once before it falls due.
const Window = outbox.TickInterval + 2*time.Minute

// Lookback is how far back a tick still queues a slot it missed, such as one the tick was down for; an older one is the
// watchdog's to raise (AI-67).
const Lookback = time.Hour

// Scheduler queues the agents' runs for their slots.
type Scheduler struct {
	Store      storage.Store
	Dispatcher outbox.Dispatcher
	Clock      ports.Clock
	Skills     *Skills
}

// RunWork is a run's outbox payload.
type RunWork struct {
	RunID uuid.UUID `json:"run_id"`
}

// ScheduleAll queues every organisation's due slots, and gives how many runs it queued.
func (s Scheduler) ScheduleAll(ctx context.Context) (int, error) {
	orgs, err := s.Store.Organisations(ctx)
	if err != nil {
		return 0, fmt.Errorf("list the organisations: %w", err)
	}
	total := 0
	for _, org := range orgs {
		n, err := s.ScheduleIn(ctx, org)
		total += n
		if err != nil {
			return total, fmt.Errorf("organisation %s: %w", org, err)
		}
	}
	return total, nil
}

// ScheduleIn queues org's due slots: for each agent turned on and not paused, each entry's slots from the later of an
// hour ago and when it was turned on, to the window's end, each once.
func (s Scheduler) ScheduleIn(ctx context.Context, org uuid.UUID) (int, error) {
	n := 0
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		n = 0
		now := s.Clock.Now()
		for _, skill := range s.Skills.All() {
			on, err := Enabled(ctx, u, skill, now)
			if err != nil || !on.Running() {
				return err
			}
			from := now.Add(-Lookback)
			if on.Since.After(from) {
				from = on.Since
			}
			for _, entry := range Entries {
				spec, err := on.Entry(entry)
				if err != nil {
					return err
				}
				for slot := spec.Next(from.Add(-time.Minute)); !slot.IsZero() && !slot.After(now.Add(Window)); slot = spec.Next(slot) {
					_, queued, err := s.queue(ctx, u, org, skill, on.ActsFor, "schedule:"+entry, slot, now, nil)
					if err != nil {
						return err
					}
					if queued {
						n++
					}
				}
			}
		}
		return nil
	})
	return n, err
}

// Queue queues a run of skill for its trigger and slot, acting for actsFor (who chose Run now, when requestedBy is set),
// and enqueues it for its slot; queued is false when the slot was queued already.
func (s Scheduler) Queue(ctx context.Context, u storage.UnitOfWork, org uuid.UUID, skill Skill, actsFor uuid.UUID, trigger string,
	slot time.Time, requestedBy *uuid.UUID) (bool, error) {
	_, queued, err := s.queue(ctx, u, org, skill, actsFor, trigger, slot, s.Clock.Now(), requestedBy)
	return queued, err
}

// RunNow queues a run of skill's agent at once, as a person's Run now asks (AI-62): requested by them, acting for the
// Owner who turned it on (AI-63).
func (s Scheduler) RunNow(ctx context.Context, u storage.UnitOfWork, org uuid.UUID, skill Skill, actsFor, by uuid.UUID) (storage.AgentRun, error) {
	now := s.Clock.Now()
	run, _, err := s.queue(ctx, u, org, skill, actsFor, TriggerRunNow, now, now, &by)
	return run, err
}

// TriggerRunNow is the trigger of a run a person asked for.
const TriggerRunNow = "run_now"

func (s Scheduler) queue(ctx context.Context, u storage.UnitOfWork, org uuid.UUID, skill Skill, actsFor uuid.UUID, trigger string,
	slot, now time.Time, requestedBy *uuid.UUID) (storage.AgentRun, bool, error) {
	run, queued, err := u.AgentRuns().Queue(ctx, storage.NewAgentRun{Agent: skill.Name, SkillVersion: skill.Version, SkillSHA256: skill.SHA256,
		Trigger: trigger, SlotAt: slot, ActsFor: actsFor, RequestedBy: requestedBy, QueuedAt: now})
	if err != nil || !queued {
		return run, false, err
	}
	payload, err := json.Marshal(RunWork{RunID: run.ID})
	if err != nil {
		return run, false, err
	}
	_, err = s.Dispatcher.EnqueueAt(ctx, u, org, storage.NewOutboxItem{Class: storage.ClassWorker, Kind: RunKind, Payload: payload,
		BusinessKey: run.ID.String()}, slot)
	return run, err == nil, err
}

// State is an agent's configuration in an organisation on a day (AI-62, AI-63; design D2): whether it is on and who
// turned it on, whether it is paused, and its settings.
type State struct {
	On      bool
	Paused  bool
	ActsFor uuid.UUID // the person who asked to turn it on, under FM-1
	Since   time.Time // when turning it on was approved
	values  map[string]string
}

// Running reports whether the agent runs: on, turned on by someone, and not paused.
func (st State) Running() bool { return st.On && !st.Paused && st.ActsFor != uuid.Nil }

// Value is the agent's setting name (such as "quiet_hours") on the day.
func (st State) Value(name string) string { return st.values[name] }

// Entry is the schedule's entry name, parsed.
func (st State) Entry(name string) (cron.Spec, error) { return cron.Parse(st.values[name]) }

// Enabled reads skill's agent's settings in the organisation on at's day, in Singapore time, and who turned it on: the
// person who asked for the switch's approved change (turning it on always waits for a second approver).
func Enabled(ctx context.Context, u storage.UnitOfWork, skill Skill, at time.Time) (State, error) {
	prefix := "agents." + skill.Agent() + "."
	y, m, d := at.In(clock.Singapore).Date()
	day := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	st := State{values: map[string]string{}}
	for _, def := range settings.Catalogue {
		name, ok := cutPrefix(def.Key, prefix)
		if !ok {
			continue
		}
		changes, err := u.Settings().Changes(ctx, def.Key)
		if err != nil {
			return State{}, err
		}
		st.values[name] = settings.On(def, changes, day).Value
	}
	st.On, st.Paused = st.values["enabled"] == "true", st.values["paused"] == "true"
	if !st.On {
		return st, nil
	}
	r, err := u.Approvals().LastApproved(ctx, approval.ObjectSetting, prefix+"enabled")
	switch {
	case errors.Is(err, ports.ErrNotFound):
		return st, nil // on with nobody who asked: it does not run
	case err != nil:
		return State{}, err
	}
	st.ActsFor = r.RequestedBy
	if r.DecidedAt != nil {
		st.Since = *r.DecidedAt
	}
	return st, nil
}

func cutPrefix(s, prefix string) (string, bool) {
	rest, ok := strings.CutPrefix(s, prefix)
	return rest, ok && rest != ""
}
