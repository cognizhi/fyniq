// Package packupdate is what a newer version of a statutory pack waiting to be accepted means for an organisation (PRD-001 ENG-33; p2-tax-filings design D4): the view of what
// is waiting with the rows each would add, remove or change, and the platform's own compliance tasks about it, raised by a worker job: pack.update_available while a newer version
// is waiting and pack.correction_assessment when it corrects the period the active version already covers, with the posted documents dated in that period counted. It posts
// and re-posts nothing: accepting a pack is an approved activation (SOD-10), and a correction never changes posted history.
package packupdate

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/packs"
)

// The task types, as the findings' key prefixes.
const (
	UpdateAvailable      = "pack.update_available"
	CorrectionAssessment = "pack.correction_assessment"
	updateKey            = "pack_update"
	correctionKey        = "pack_correction"
)

// Update is a newer version of an activated pack that the organisation has not accepted.
type Update struct {
	Pack       string
	Current    pack.Version
	Available  pack.Version
	Correction bool // the newer version covers the period the current one already does: its effective day is not later
	Changes    []pack.Change
}

var (
	releaseOnce sync.Once
	releaseAll  []pack.Version
	releaseErr  error
)

// Release is every version this release carries.
func Release() ([]pack.Version, error) {
	releaseOnce.Do(func() { releaseAll, releaseErr = pack.LoadAll(packs.FS) })
	return releaseAll, releaseErr
}

// Readable is the version this release holds under the stored version's ID and digest: false when it holds none, so the organisation's copy cannot be read here.
func Readable(v storage.PackVersion) (pack.Version, bool, error) {
	all, err := Release()
	if err != nil {
		return pack.Version{}, false, err
	}
	i := slices.IndexFunc(all, func(r pack.Version) bool { return r.ID == v.ID && r.Digest == v.Digest })
	if i < 0 {
		return pack.Version{}, false, nil
	}
	return all[i], true, nil
}

func newer(x, y pack.Version) bool {
	return x.EffectiveFrom.After(y.EffectiveFrom) || (x.EffectiveFrom.Equal(y.EffectiveFrom) && (x.Year > y.Year || (x.Year == y.Year && x.N > y.N)))
}

// Updates are the newer versions waiting, one for each activated pack: the newest in force on today that the organisation has not activated, over the newest it has
// activated that is in force, ordered by pack name. A pack nothing is activated of has none.
func Updates(ctx context.Context, u storage.UnitOfWork, today time.Time) ([]Update, error) {
	vs, err := u.Packs().Versions(ctx)
	if err != nil {
		return nil, err
	}
	acts, err := u.Packs().Activations(ctx)
	if err != nil {
		return nil, err
	}
	loaded := map[string]pack.Version{}
	for _, v := range vs {
		if pv, ok, err := Readable(v); err != nil {
			return nil, err
		} else if ok {
			loaded[v.ID] = pv
		}
	}
	active := func(id string) bool {
		return slices.ContainsFunc(acts, func(x storage.PackActivation) bool { return x.VersionID == id })
	}
	inForce := func(v pack.Version) bool {
		return !v.EffectiveFrom.After(today) && (v.EffectiveTo == nil || !v.EffectiveTo.Before(today))
	}
	names := map[string]bool{}
	for _, v := range loaded {
		names[v.Pack] = true
	}
	var out []Update
	for _, name := range slices.Sorted(func(yield func(string) bool) {
		for n := range names {
			if !yield(n) {
				return
			}
		}
	}) {
		var cur, next *pack.Version
		for id, v := range loaded {
			v := v
			if v.Pack == name && active(id) && inForce(v) && (cur == nil || newer(v, *cur)) {
				cur = &v
			}
		}
		if cur == nil {
			continue
		}
		for id, v := range loaded {
			v := v
			if v.Pack == name && !active(id) && inForce(v) && newer(v, *cur) && (next == nil || newer(v, *next)) {
				next = &v
			}
		}
		if next == nil {
			continue
		}
		out = append(out, Update{Pack: name, Current: *cur, Available: *next, Correction: !next.EffectiveFrom.After(cur.EffectiveFrom), Changes: pack.Diff(*cur, *next)})
	}
	return out, nil
}

// findingKey is the key of the task about an update: one open task per pack version waiting, so a newer one raises its own.
func findingKey(prefix string, up Update) string {
	return prefix + ":" + strings.NewReplacer("@", ":").Replace(up.Available.ID)
}

// documentTypes are the posted documents a correction to a pack may touch, by pack: the ones read with its values.
var documentTypes = map[string][]storage.DocumentType{
	"sg-gst": {storage.DocumentInvoice, storage.DocumentCreditNote, storage.DocumentBill, storage.DocumentVendorCredit, storage.DocumentBankTransaction, storage.DocumentExpenseClaim,
		storage.DocumentCustomerDeposit, storage.DocumentAssetDisposal},
	"sg-tax-filings": {storage.DocumentWriteOff},
}

// Affected counts the posted documents of the types the pack's values reach, dated from the day the correction takes effect to today, by type name (a pack that reaches none of
// them, or has no documents there, returns none).
func Affected(ctx context.Context, u storage.UnitOfWork, up Update, today time.Time) (map[string]int, error) {
	out := map[string]int{}
	for _, t := range documentTypes[up.Pack] {
		f := storage.DocumentFilter{Type: t, Status: storage.DocumentPosted, From: up.Available.EffectiveFrom.Format(time.DateOnly), To: today.Format(time.DateOnly), Limit: 200}
		for {
			page, err := u.Documents().Page(ctx, f)
			if err != nil {
				return nil, err
			}
			out[string(t)] += len(page)
			if len(page) < f.Limit {
				break
			}
			last := page[len(page)-1]
			f.AfterDate, f.AfterID = last.Date, &last.ID
		}
		if out[string(t)] == 0 {
			delete(out, string(t))
		}
	}
	return out, nil
}

// owner is the role the task goes to: who activates a pack (two people: the Owner and the Finance controller, SOD-10).
const owner = "controller"

func task(prefix, title, detail string, up Update, now time.Time) storage.NewComplianceTask {
	return storage.NewComplianceTask{FindingKey: findingKey(prefix, up), Title: title, Detail: detail, OwnerRole: owner, CreatedAt: now}
}

// Tasks are the tasks an update calls for: pack.update_available always; pack.correction_assessment too when the newer version corrects the period the current one covers,
// with the documents dated in that period counted. The detail of each says what the update changes in a line.
func Tasks(ctx context.Context, u storage.UnitOfWork, up Update, today, now time.Time) ([]storage.NewComplianceTask, error) {
	added, removed, changed := 0, 0, 0
	for _, c := range up.Changes {
		switch c.Kind {
		case "added":
			added++
		case "removed":
			removed++
		default:
			changed++
		}
	}
	line := fmt.Sprintf("%s to %s: %d row(s) added, %d removed, %d changed.", up.Current.ID, up.Available.ID, added, removed, changed)
	out := []storage.NewComplianceTask{task(updateKey, fmt.Sprintf("Statutory pack update waiting: %s", up.Available.ID),
		line+" Review the changes on Compliance > Year-end tax, then accept it in Settings > Statutory packs (two people).", up, now)}
	if up.Correction {
		n, err := Affected(ctx, u, up, today)
		if err != nil {
			return nil, err
		}
		detail := fmt.Sprintf("%s corrects %s for the period from %s. %s", up.Available.ID, up.Current.ID, up.Available.EffectiveFrom.Format(time.DateOnly), line)
		if len(n) == 0 {
			detail += " No posted document of the types it reaches is dated in that period."
		} else {
			parts := make([]string, 0, len(n))
			for _, t := range slices.Sorted(func(yield func(string) bool) {
				for k := range n {
					if !yield(k) {
						return
					}
				}
			}) {
				parts = append(parts, fmt.Sprintf("%d %s", n[t], strings.ReplaceAll(t, "_", " ")))
			}
			detail += " Posted documents dated in that period: " + strings.Join(parts, ", ") + ". Nothing is re-posted: assess each, and correct by reversal where the correction changes a figure."
		}
		out = append(out, task(correctionKey, fmt.Sprintf("Assess a pack correction: %s", up.Available.ID), detail, up, now))
	}
	return out, nil
}

// Raise brings an organisation's tasks in line with its updates: a task for each update waiting that has none open, an open task whose update is no longer waiting cleared
// (it was accepted, or a newer one took its place). It raises nothing twice and changes no other task.
func Raise(ctx context.Context, u storage.UnitOfWork, today, now time.Time) (raised, cleared int, err error) {
	ups, err := Updates(ctx, u, today)
	if err != nil {
		return 0, 0, err
	}
	wanted := map[string]bool{}
	for _, up := range ups {
		ts, err := Tasks(ctx, u, up, today, now)
		if err != nil {
			return raised, cleared, err
		}
		for _, t := range ts {
			wanted[t.FindingKey] = true
			if _, err := u.ComplianceTasks().OpenFor(ctx, t.FindingKey); err == nil {
				continue
			} else if err != ports.ErrNotFound {
				return raised, cleared, err
			}
			if _, err := u.ComplianceTasks().RaiseSystem(ctx, t); err != nil {
				return raised, cleared, err
			}
			raised++
		}
	}
	open, err := u.ComplianceTasks().List(ctx, storage.ComplianceTaskQuery{Statuses: []string{storage.TaskOpen}})
	if err != nil {
		return raised, cleared, err
	}
	for _, t := range open {
		if t.Source != "system" || wanted[t.FindingKey] || !(strings.HasPrefix(t.FindingKey, updateKey+":") || strings.HasPrefix(t.FindingKey, correctionKey+":")) {
			continue
		}
		if _, err := u.ComplianceTasks().Clear(ctx, t.ID, t.Version, now); err != nil {
			return raised, cleared, err
		}
		cleared++
	}
	return raised, cleared, nil
}

// Job is the platform job that keeps each organisation's pack-update tasks current (ENG-33), once a day. It runs as the worker role: it raises the platform's own tasks and
// clears them, and changes nothing else.
const Job = "packs.updates"

// Result is what a run did.
type Result struct{ Raised, Cleared int }

// RaiseAll is the job: every organisation's tasks brought in line, an organisation at a time in a unit of work of its own, so one that fails does not stop the others.
func RaiseAll(ctx context.Context, store storage.Store, now func() time.Time) (Result, error) {
	var out Result
	orgs, err := store.Organisations(ctx)
	if err != nil {
		return out, err
	}
	sg, err := time.LoadLocation("Asia/Singapore")
	if err != nil {
		sg = time.FixedZone("SGT", 8*3600)
	}
	var errs []error
	for _, org := range orgs {
		at := now()
		y, m, d := at.In(sg).Date()
		today := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
		if err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			r, c, err := Raise(ctx, u, today, at)
			out.Raised, out.Cleared = out.Raised+r, out.Cleared+c
			return err
		}); err != nil {
			errs = append(errs, fmt.Errorf("organisation %s: %w", org, err))
		}
	}
	return out, errors.Join(errs...)
}
