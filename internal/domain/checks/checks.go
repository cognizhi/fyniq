// Package checks is the checks engine (PRD-001 FR-CTL-01, FR-CTL-02, FR-CTL-05; p1-checks design D1 to D9): one
// catalogue of definitions, one per row of Appendix C whose module is built, that the Checks page, books health on Home
// and the period close all read, so a check never says two things.
//
// Each definition runs against the books as they stand and gives a result: ok, ⚠ warning, ✖ error, ⏰ due, not assessed
// for want of an input it names, or failed when the run itself failed, which counts as ✖ (rev 0.162). The runner keeps
// each check's latest result and records each run with the ledger revision and the audit event it read through. Checks
// run when a change they react to commits (the outbox item checks.refresh, coalesced per organisation), nightly as the
// platform job checks.run, and on request (Run now).
package checks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/closing"
	"github.com/cognizhi/fyniq/internal/domain/financials"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Status is what a check found.
type Status string

// The statuses (FR-CTL-01, rev 0.162; the symbols of FR-UX-09).
const (
	OK          Status = "ok"           // ✔
	Warning     Status = "warning"      // ⚠ included, please review
	Error       Status = "error"        // ✖ cannot post or close until fixed
	Due         Status = "due"          // ⏰ due or overdue
	NotAssessed Status = "not_assessed" // an input it needs is missing, which it names
	Failed      Status = "failed"       // the run itself failed: counts as ✖
	// Unchanged is what a check returns when the run has nothing new to say, such as a check fed by a nightly job that
	// has not run since: its state stays as it was.
	Unchanged Status = ""
)

// Problem reports whether s counts as ✖ in books health.
func (s Status) Problem() bool { return s == Error || s == Failed }

// Review reports whether s counts as ⚠ or ⏰ in books health.
func (s Status) Review() bool { return s == Warning || s == Due }

// MaxItems is how many offending items a result keeps; Count says how many there were.
const MaxItems = 50

// Item is one offending item: what it is, its ID where it has one, and words for it. The web app links it by kind and ID
// (FR-UX-04).
type Item struct {
	Kind   string `json:"kind"` // account, period, document, role
	ID     string `json:"id,omitempty"`
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
	// Severity is the item's own, for a check that finds both (C.1 #8, #18): error or warning; "" for the check's.
	Severity string `json:"severity,omitempty"`
	// Version is the item's version as a review names it (rev 0.164): a review holds until the item changes.
	Version string `json:"version,omitempty"`
}

// severity is the item's severity within a result of status.
func (it Item) severity(status Status) Status {
	if it.Severity != "" {
		return Status(it.Severity)
	}
	return status
}

// ReviewKey names an item of a check at its version, as a review records it.
func ReviewKey(key, kind, id, version string) string { return key + "\x00" + kind + "\x00" + id + "\x00" + version }

// WithoutReviewed takes the ⚠ items marked reviewed out of a result (FR-CTL-01, ENG-21; rev 0.164): what is left decides
// its status, ✖ while an item that cannot post remains, ⚠ while one to review does, OK once every item found was
// reviewed. reviewed reports whether the review key of an item is recorded.
func WithoutReviewed(key string, status Status, count int, items []Item, reviewed func(string) bool) (Status, int, []Item, int) {
	if status != Warning && status != Error {
		return status, count, items, 0
	}
	kept := make([]Item, 0, len(items))
	dropped, errs := 0, 0
	for _, it := range items {
		sev := it.severity(status)
		if sev == Warning && it.ID != "" && reviewed(ReviewKey(key, it.Kind, it.ID, it.Version)) {
			dropped++
			continue
		}
		if sev == Error {
			errs++
		}
		kept = append(kept, it)
	}
	if dropped == 0 {
		return status, count, items, 0
	}
	count = max(count-dropped, len(kept))
	switch {
	case len(kept) == 0 && count == 0:
		status = OK
	case errs == 0 && status == Error:
		status = Warning
	}
	return status, count, kept, dropped
}

// Result is a check's outcome.
type Result struct {
	Status Status
	Count  int    // how many items it found
	Items  []Item // the first MaxItems of them
	Detail string // what it found, in plain words
	Fix    string // how to fix it, in words
	// Needs and Setting say what a check not assessed is missing and where it is set (ENG-23).
	Needs, Setting string
}

// Fine is an OK result with words for it.
func Fine(detail string) Result { return Result{Status: OK, Detail: detail} }

// Missing is a not-assessed result: the input it needs and the setting that holds it.
func Missing(needs, setting string) Result {
	return Result{Status: NotAssessed, Needs: needs, Setting: setting, Detail: "Not assessed: it needs " + needs}
}

// Found is a result with items: status, how to fix them, and the words for what was found. It keeps every item; the
// state keeps the first MaxItems once the reviewed ones are out.
func Found(status Status, items []Item, detail, fix string) Result {
	return Result{Status: status, Count: len(items), Items: items, Detail: detail, Fix: fix}
}

// Definition is one check of the catalogue (design D1).
type Definition struct {
	Key   string // stable, such as books.debits-equal-credits
	Ref   string // its Appendix C row, such as C.1 #1
	Area  string // Books, Setup, Ledger, Audit…
	Worst Status // the worst it can find: error, warning or due
	// Title is what it checks; Explain why it matters, in plain words; Rules the rule IDs Expert adds.
	Title, Explain, Rules string
	// Owner is the role that owns it unless a person is named (design D8), as the API names roles: owner, controller…
	Owner string
	// View is the Appendix D row whose view right opens its items (design D7); Area of a qualified reader: sales,
	// purchases or payroll, whose readers see it, or "" when only unqualified readers do.
	View, Qualifier string
	// Module is the feature switch it needs on (FR-ORG-06), such as features.payroll; "" for none.
	Module string
	// Triggers are the audit object types whose change reruns it (design D5). Nil: it runs nightly and on request only.
	Triggers []string
	// CloseKeys are the period close's own checks this check is (design D2): the close runs them, so the catalogue's
	// close hook does not add them again.
	CloseKeys []string
	// Run works it out. An error records the check as failed; the error goes to the server's log, never the result.
	Run func(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error)
}

// Env is what a run gives each check: the organisation, its base currency and today, its periods, the ledger revision
// the run read at, and the audit events since the last run.
type Env struct {
	Org      storage.Organisation
	Base     money.Currency
	Today    time.Time        // the organisation's today, a date at UTC midnight
	Periods  []storage.Period // earliest first
	Revision int64
	// Latest are the newest event of each audit action read since the last run, as their bodies: how the checks fed by
	// a nightly job read its outcome (design D6).
	Latest map[string]Event
	// Prior is each check's latest state before this run, by key.
	Prior map[string]storage.CheckState
	// Reviewed are the review keys of the items marked reviewed (rev 0.164).
	Reviewed map[string]bool

	close      *closeRun
	statements map[string]financials.Statement
}

// Event is an audit event as a check reads it.
type Event struct {
	Seq        int64
	Action     string
	ObjectType string
	OccurredAt string
	Changes    map[string][2]any
}

// readEvent reads what the checks need of a stored event's canonical body.
func readEvent(seq int64, body string) (Event, error) {
	var b struct {
		Action     string `json:"action"`
		OccurredAt string `json:"occurred_at"`
		Object     struct {
			Type string `json:"type"`
		} `json:"object"`
		Changes map[string][2]any `json:"changes"`
	}
	if err := json.Unmarshal([]byte(body), &b); err != nil {
		return Event{}, fmt.Errorf("audit event %d: %w", seq, err)
	}
	return Event{Seq: seq, Action: b.Action, ObjectType: b.Object.Type, OccurredAt: b.OccurredAt, Changes: b.Changes}, nil
}

// closeRun is the period close's checks, run once per run of the catalogue for the period to close next (design D2).
type closeRun struct {
	done   bool
	period *storage.Period
	checks []closing.Check
	err    error
}

// ToClose is the period to close next: the earliest open period that has started, or nil. A month the books are still
// in is the one to close next once every earlier month is closed.
func (e *Env) ToClose() *storage.Period {
	for i := range e.Periods {
		p := e.Periods[i]
		if p.Status != storage.PeriodClosed && !p.StartsOn.After(e.Today) {
			return &p
		}
	}
	return nil
}

// Started are the open periods that have started, earliest first: the months a month check reads (design D3).
func (e *Env) Started() []storage.Period {
	var out []storage.Period
	for _, p := range e.Periods {
		if p.Status != storage.PeriodClosed && !p.StartsOn.After(e.Today) {
			out = append(out, p)
		}
	}
	return out
}

// Close is the close's findings of keys, for the period to close next, and that period; nil when no period is open.
func (e *Env) Close(ctx context.Context, u storage.UnitOfWork, keys ...string) ([]closing.Check, *storage.Period, error) {
	if e.close == nil {
		e.close = &closeRun{}
	}
	c := e.close
	if !c.done {
		c.done = true
		if c.period = e.ToClose(); c.period != nil {
			c.checks, c.err = closing.Checks(ctx, u, *c.period, e.Today)
		}
	}
	if c.err != nil || c.period == nil {
		return nil, c.period, c.err
	}
	var out []closing.Check
	for _, k := range keys {
		for _, x := range c.checks {
			if x.Key == k {
				out = append(out, x)
			}
		}
	}
	return out, c.period, nil
}

// Month is a period's month, as a check names it: October 2026.
func Month(p storage.Period) string { return p.StartsOn.Format("January 2006") }

// PeriodItem is an item naming a period, whose version is what was found in it: a review of it holds until the finding
// changes.
func PeriodItem(p storage.Period, detail string) Item {
	return Item{Kind: "period", ID: p.ID.String(), Label: Month(p), Detail: detail, Version: Fingerprint(detail)}
}

// Fingerprint is a short version of words that change when what they describe does.
func Fingerprint(words string) string {
	sum := sha256.Sum256([]byte(words))
	return hex.EncodeToString(sum[:6])
}

// reviewType is the audit object type of an item marked reviewed: it reruns the checks that find items to review.
const reviewType = "check_review"

// reacts reports whether a change of any of types reruns d: one of its triggers, or a review for a check that finds
// items to review.
func (d Definition) reacts(types []string) bool {
	if d.Triggers == nil {
		return false
	}
	for _, t := range d.Triggers {
		if slices.Contains(types, t) {
			return true
		}
	}
	return d.Worst != Due && slices.Contains(types, reviewType)
}

// Triggered are the definitions of defs that a change of any of types reruns, and those that never ran (no prior state).
func Triggered(defs []Definition, types []string, prior map[string]storage.CheckState) []Definition {
	var out []Definition
	for _, d := range defs {
		if _, ran := prior[d.Key]; (!ran && d.Triggers != nil) || d.reacts(types) {
			out = append(out, d)
		}
	}
	return out
}

// Reacts reports whether a change of any of types reruns a definition of defs: what decides a unit of work asks for a
// refresh (design D5).
func Reacts(defs []Definition, types []string) bool {
	return slices.ContainsFunc(defs, func(d Definition) bool { return d.reacts(types) })
}
