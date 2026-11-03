package checks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/closing"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The background work and the platform job of the checks (design D5).
const (
	RefreshKind = "checks.refresh" // outbox: a change the checks react to committed
	RequestKind = "checks.request" // outbox: Run now
	Job         = "checks.run"     // platform job: every check of every organisation, nightly at 04:15 Singapore time
)

// Retention of the runs (design D4): a run on a change or on request is kept 90 days, a nightly run 13 months. The purge
// leaves a day's margin, as the database refuses a run within its retention by its own clock.
const (
	keepRuns    = 91 * 24 * time.Hour
	keepNightly = 13 // months, plus a day
)

// maxEvents is how many audit events a run on a change reads since the last run; past it, it runs every check that reacts
// to a change, as a burst of that size concerns most of them.
const maxEvents = 20000

// areas are the order the Checks page lists the areas in.
var areas = []string{"Books", "Setup", "Bank", "Sales", "Purchases", "Journals", "Assets", "Recurring", "Closing", "FX", "Planning", "Ledger", "Audit"}

// catalogue is every check built: by area, then in its slice's order.
var catalogue = func() []Definition {
	out := slices.Concat(booksChecks, setupChecks, documentChecks, closeChecks, controlChecks, paymentChecks, platformChecks, foundersChecks)
	slices.SortStableFunc(out, func(a, b Definition) int { return slices.Index(areas, a.Area) - slices.Index(areas, b.Area) })
	return out
}()

// Catalogue is every check built, in the order the Checks page lists them in their areas.
func Catalogue() []Definition { return slices.Clone(catalogue) }

// Find is the catalogue's definition of key.
func Find(key string) (Definition, bool) {
	i := slices.IndexFunc(catalogue, func(d Definition) bool { return d.Key == key })
	if i < 0 {
		return Definition{}, false
	}
	return catalogue[i], true
}

// RefreshItem is the outbox item a unit of work adds when it changed something the checks react to.
func RefreshItem() storage.NewOutboxItem {
	return storage.NewOutboxItem{Class: storage.ClassWorker, Kind: RefreshKind, MaxAttempts: 5}
}

// RequestItem is the outbox item of Run now, asked for by actor ("type:id").
func RequestItem(by string) (storage.NewOutboxItem, error) {
	payload, err := json.Marshal(map[string]string{"requested_by": by})
	return storage.NewOutboxItem{Class: storage.ClassWorker, Kind: RequestKind, Payload: payload, MaxAttempts: 3}, err
}

// RequestedBy reads who asked for a run on request from its outbox payload.
func RequestedBy(payload []byte) (string, error) {
	var p struct {
		By string `json:"requested_by"`
	}
	if err := json.Unmarshal(payload, &p); err != nil || p.By == "" {
		return "", fmt.Errorf("a run on request names who asked: %s", payload)
	}
	return p.By, nil
}

// Runner runs the checks for organisations and records what they find (design D4, D5).
type Runner struct {
	Store storage.Store
	Clock ports.Clock
	// Defs are the definitions it runs; nil, the catalogue.
	Defs []Definition
	// Failed hears a check whose run failed, for the server's log; the result says only that it failed (SEC-15).
	Failed func(ctx context.Context, org uuid.UUID, key string, err error)
	// Platform reads the scheduler's heartbeats, and Jobs are the scheduled jobs C.2 #12 watches, with how long each may
	// go without one; with no Platform, C.2 #12 is not assessed.
	Platform storage.Platform
	Jobs     []WatchedJob
}

// WatchedJob is a scheduled job C.2 #12 watches: its name, as the schedule and its heartbeat call it, and how long it
// may go without a heartbeat before it is late (outbox.Schedule's Stale).
type WatchedJob struct {
	Name  string
	Stale time.Duration
}

// Report says what a run did.
type Report struct {
	Organisation uuid.UUID
	Run          storage.CheckRun
	Changed      int // checks whose status changed
}

func (r Runner) defs() []Definition {
	if r.Defs != nil {
		return r.Defs
	}
	return Catalogue()
}

// TodayOf is the organisation's today at now, as a date at UTC midnight.
func TodayOf(now time.Time, o storage.Organisation) time.Time {
	loc, err := time.LoadLocation(o.Profile.TimeZone)
	if err != nil || o.Profile.TimeZone == "" {
		loc = time.UTC
	}
	y, m, d := now.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// prepared is what a run reads before it runs its checks.
type prepared struct {
	env   *Env
	head  int64    // the audit event the run reads through
	types []string // the object types changed since the last run
	burst bool     // more events than maxEvents
	off   map[string]bool
}

func (r Runner) prepare(ctx context.Context, org uuid.UUID) (prepared, error) {
	var out prepared
	err := r.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		base, err := money.Lookup(o.BaseCurrency)
		if err != nil {
			return err
		}
		e := &Env{Org: o, Base: base, Today: TodayOf(r.Clock.Now(), o), Now: r.Clock.Now(), Latest: map[string]Event{}, Prior: map[string]storage.CheckState{},
			jobs: r.Jobs}
		if r.Platform != nil {
			if e.beats, err = r.Platform.Heartbeats(ctx); err != nil {
				return err
			}
		}
		if e.Periods, err = u.Periods().List(ctx); err != nil {
			return err
		}
		if e.Revision, err = u.Ledger().Revision(ctx); err != nil {
			return err
		}
		states, err := u.Checks().States(ctx)
		if err != nil {
			return err
		}
		for _, s := range states {
			e.Prior[s.Key] = s
		}
		reviews, err := u.Checks().Reviews(ctx)
		if err != nil {
			return err
		}
		e.Reviewed = map[string]bool{}
		for _, r := range reviews {
			e.Reviewed[ReviewKey(r.Key, r.ItemKind, r.ItemID, r.ItemVersion)] = true
		}
		head, err := u.Audit().Head(ctx)
		if err != nil {
			return err
		}
		mark, err := u.Checks().Watermark(ctx)
		if err != nil {
			return err
		}
		types := map[string]bool{}
		for after, read := mark, 0; after < head.Seq; {
			batch, err := u.Audit().Events(ctx, after, 1000)
			if err != nil {
				return err
			}
			if len(batch) == 0 {
				break
			}
			for _, b := range batch {
				if b.Seq > head.Seq {
					break
				}
				ev, err := readEvent(b.Seq, b.Body)
				if err != nil {
					return err
				}
				types[ev.ObjectType] = true
				e.Latest[ev.Action] = ev
			}
			after = batch[len(batch)-1].Seq
			if read += len(batch); read >= maxEvents {
				out.burst = true
				break
			}
		}
		changes, err := u.Settings().Changes(ctx, "")
		if err != nil {
			return err
		}
		out.off = map[string]bool{}
		for _, d := range r.defs() {
			if d.Module == "" {
				continue
			}
			s, ok := settings.Lookup(d.Module)
			out.off[d.Module] = !ok || settings.On(s, changes, e.Today).Value != "true"
		}
		out.env, out.head = e, head.Seq
		out.types = make([]string, 0, len(types))
		for t := range types {
			out.types = append(out.types, t)
		}
		slices.Sort(out.types)
		return nil
	})
	return out, err
}

// Refresh runs, for org, the checks that the changes since the last run concern, and those that never ran (FR-CTL-01,
// design D5): the outbox work of RefreshKind.
func (r Runner) Refresh(ctx context.Context, org uuid.UUID) (Report, error) {
	return r.run(ctx, org, storage.CheckOnChange, "", func(p prepared, defs []Definition) []Definition {
		if p.burst {
			return slices.DeleteFunc(slices.Clone(defs), func(d Definition) bool { return d.Triggers == nil })
		}
		return Triggered(defs, p.types, p.env.Prior)
	})
}

// Request runs every check for org, as by asked (Run now, design D5): the outbox work of RequestKind.
func (r Runner) Request(ctx context.Context, org uuid.UUID, by string) (Report, error) {
	return r.run(ctx, org, storage.CheckOnRequest, by, func(_ prepared, defs []Definition) []Definition { return defs })
}

// Nightly runs every check for every organisation and keeps the full results, the evidence the Compliance Center reads
// (FR-CTL-05), then purges the runs past their retention (design D4): the platform job checks.run. One organisation's
// failure does not stop the others; the error names how many failed.
func (r Runner) Nightly(ctx context.Context) ([]Report, error) {
	orgs, err := r.Store.Organisations(ctx)
	if err != nil {
		return nil, err
	}
	var out []Report
	var failed []error
	for _, org := range orgs {
		rep, err := r.run(ctx, org, storage.CheckNightly, "", func(_ prepared, defs []Definition) []Definition { return defs })
		if err == nil {
			now := r.Clock.Now()
			err = r.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
				_, err := u.Checks().Purge(ctx, now.Add(-keepRuns), now.AddDate(0, -keepNightly, -1))
				return err
			})
		}
		if err != nil {
			failed = append(failed, fmt.Errorf("organisation %s: %w", org, err))
			continue
		}
		out = append(out, rep)
	}
	if len(failed) > 0 {
		return out, fmt.Errorf("the checks of %d organisation(s) did not run: %w", len(failed), errors.Join(failed...))
	}
	return out, nil
}

// result is one check's outcome in a run.
type result struct {
	def Definition
	res Result
}

func (r Runner) run(ctx context.Context, org uuid.UUID, trigger, by string, choose func(prepared, []Definition) []Definition) (Report, error) {
	started := r.Clock.Now()
	p, err := r.prepare(ctx, org)
	if err != nil {
		return Report{}, err
	}
	defs := slices.DeleteFunc(slices.Clone(r.defs()), func(d Definition) bool { return d.Module != "" && p.off[d.Module] })
	var results []result
	for _, d := range choose(p, defs) {
		res := r.one(ctx, org, d, p.env)
		var dropped int
		res.Status, res.Count, res.Items, dropped = WithoutReviewed(d.Key, res.Status, res.Count, res.Items, func(k string) bool {
			return d.ReviewedIn == "" && p.env.Reviewed[k]
		})
		if dropped > 0 && res.Status == OK {
			res.Detail, res.Fix = fmt.Sprintf("Nothing left to review: %d item(s) marked reviewed", dropped), ""
		}
		results = append(results, result{d, res})
	}
	finished := r.Clock.Now()
	rep := Report{Organisation: org}
	err = r.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		states, err := u.Checks().States(ctx) // as they stand now: another run may have written since this one began
		if err != nil {
			return err
		}
		current := map[string]storage.CheckState{}
		for _, s := range states {
			current[s.Key] = s
		}
		var ran []string
		var nightly []map[string]any
		for _, x := range results {
			ran = append(ran, x.def.Key)
			if x.res.Status == Unchanged {
				continue
			}
			before, had := current[x.def.Key]
			s, err := stateOf(x.def.Key, x.res, finished, trigger, p.env.Revision)
			if err != nil {
				return err
			}
			s.ChangedAt = finished
			if had && before.Status == s.Status {
				s.ChangedAt = before.ChangedAt
			} else {
				rep.Changed++
			}
			if _, err := u.Checks().Save(ctx, s, before.Version); err != nil {
				return err
			}
			if trigger == storage.CheckNightly {
				nightly = append(nightly, map[string]any{"key": x.def.Key, "ref": x.def.Ref, "status": s.Status, "count": s.Count, "detail": s.Detail})
			}
		}
		n := storage.NewCheckRun{Trigger: trigger, RequestedBy: by, StartedAt: started, FinishedAt: finished, LedgerRevision: p.env.Revision,
			AuditSeq: p.head, Keys: ran}
		if trigger == storage.CheckNightly {
			if nightly == nil {
				nightly = []map[string]any{}
			}
			if n.Results, err = json.Marshal(nightly); err != nil {
				return err
			}
		}
		rep.Run, err = u.Checks().RecordRun(ctx, n)
		return err
	})
	return rep, err
}

// one runs a check in a unit of work of its own, so that one failing check never fails the others (design D1).
func (r Runner) one(ctx context.Context, org uuid.UUID, d Definition, e *Env) (out Result) {
	defer func() {
		if p := recover(); p != nil {
			out = r.failed(ctx, org, d, fmt.Errorf("panic: %v", p))
		}
	}()
	err := r.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		out, err = d.Run(ctx, u, e)
		return err
	})
	if err != nil {
		return r.failed(ctx, org, d, err)
	}
	return out
}

func (r Runner) failed(ctx context.Context, org uuid.UUID, d Definition, err error) Result {
	if r.Failed != nil {
		r.Failed(ctx, org, d.Key, err)
	}
	return Result{Status: Failed, Detail: "The check could not run, so it counts as a problem; support has its log",
		Fix: "Run the checks again; if it fails again, report it"}
}

// clip cuts s to at most n characters.
func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}

// stateOf is a result as the state the store keeps.
func stateOf(key string, res Result, at time.Time, trigger string, revision int64) (storage.CheckState, error) {
	items := res.Items[:min(len(res.Items), MaxItems)]
	for i := range items {
		items[i].Label, items[i].Detail = clip(items[i].Label, 200), clip(items[i].Detail, 500)
	}
	b, err := json.Marshal(items)
	if err != nil {
		return storage.CheckState{}, err
	}
	if items == nil {
		b = []byte("[]")
	}
	s := storage.CheckState{Key: key, Status: string(res.Status), Count: max(res.Count, len(items)), Items: b, Detail: clip(res.Detail, 2000),
		Fix: clip(res.Fix, 500), RunAt: at, Trigger: trigger, LedgerRevision: revision}
	if res.Status == NotAssessed {
		s.Needs, s.Setting = clip(res.Needs, 300), clip(res.Setting, 200)
		if s.Needs == "" {
			s.Needs = "an input it names"
		}
	}
	return s, nil
}

// ItemsOf reads a state's items.
func ItemsOf(s storage.CheckState) ([]Item, error) {
	var out []Item
	if len(s.Items) == 0 {
		return out, nil
	}
	err := json.Unmarshal(s.Items, &out)
	return out, err
}

// ForClose are the checks the period close adds to its own (closing.Service.More, design D2): each check of the catalogue
// that can find ✖ and that the close does not run itself, whose latest state found ✖ or failed to run, refuses the
// close. Nothing is said of the others, as the Checks page shows them.
func ForClose(ctx context.Context, u storage.UnitOfWork, _ storage.Period, _ time.Time) ([]closing.Check, error) {
	states, err := u.Checks().States(ctx)
	if err != nil {
		return nil, err
	}
	byKey := map[string]storage.CheckState{}
	for _, s := range states {
		byKey[s.Key] = s
	}
	var out []closing.Check
	for _, d := range Catalogue() {
		if s, ok := byKey[d.Key]; ok && d.Worst == Error && len(d.CloseKeys) == 0 && Status(s.Status).Problem() {
			out = append(out, closing.Check{Key: "checks." + d.Key, Severity: closing.Failed, Title: d.Title, Detail: s.Detail})
		}
	}
	return out, nil
}

// Health is books health (FR-CTL-02, design D9): problems when any check found ✖ or failed, review when any found ⚠ or
// ⏰, OK otherwise; with the count of each, and of the checks not assessed, beside it.
type Health struct {
	Score       string // ok, review or problems
	Problems    int
	Warnings    int
	Due         int
	OK          int
	NotAssessed int
}

// HealthOf is the health of statuses; a check with no status yet counts as not assessed.
func HealthOf(statuses []Status) Health {
	var h Health
	for _, s := range statuses {
		switch s {
		case Error, Failed:
			h.Problems++
		case Warning:
			h.Warnings++
		case Due:
			h.Due++
		case OK:
			h.OK++
		default:
			h.NotAssessed++
		}
	}
	switch {
	case h.Problems > 0:
		h.Score = "problems"
	case h.Warnings > 0 || h.Due > 0:
		h.Score = "review"
	default:
		h.Score = "ok"
	}
	return h
}
