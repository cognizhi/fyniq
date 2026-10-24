package journal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ recurring journals (FR-GL-02, FR-AUT-01, FR-AUT-02)
//
// The founders decided on 21 Oct 2026 (PRD-001 rev 0.58) that the scheduler makes each occurrence of a recurring
// journal as a draft, which a person posts under the usual approvals: nothing recurring posts by itself. Built in rev
// 0.60, provisional until sign-off: the schedule follows FR-AR-07 (every 1, 3, 6 or 12 months on a day of the month, the
// 29th to the 31st meaning the last day of a shorter month), each draft is dated on its occurrence, and a recurring
// journal paused and resumed skips the occurrences it missed.

// The codes of recurring journals.
const (
	CodeRecurringSchedule = "RECURRING-01" // the schedule has no occurrence: it ends before its first
	CodeRecurringPaused   = "RECURRING-02" // pausing a paused one, or resuming an active or ended one
)

// The codes of a recurring journal's statuses that are not problems of its content.
const (
	StatusRecurringReady  = "RECURRING-OK"     // active, its content would post: ✔ OK
	StatusRecurringDue    = "RECURRING-DUE"    // an occurrence before today has no draft yet: ⚠ (Appendix C.1 #28)
	StatusRecurringPaused = "RECURRING-PAUSED" // paused by a person: ✔ Paused
	StatusRecurringEnded  = "RECURRING-ENDED"  // its schedule has ended: ✔ Ended
)

// RecurringMaker is the scheduler's principal for recurring journals (FR-AUT-02): the database's create_scheduled_draft
// saves every draft it makes as "scheduler:journals.recurring", and its audit events name it too.
var RecurringMaker = storage.Actor{Type: "scheduler", ID: "journals.recurring"}

// RecurringReason is the reason the scheduler's drafts carry.
const RecurringReason = "Recurring journal"

// MaxRecurringDue is how many due recurring journals a run takes in one organisation; maxCatchUp how many missed
// occurrences of one it makes in a run. The rest wait for the next run.
const (
	MaxRecurringDue = 500
	maxCatchUp      = 12
)

// onDay is day d of a month, or its last day when the month is shorter (FR-AR-07).
func onDay(y int, m time.Month, d int) time.Time {
	last := time.Date(y, m+1, 0, 0, 0, 0, 0, time.UTC).Day()
	return time.Date(y, m, min(d, last), 0, 0, 0, 0, time.UTC)
}

func dateOf(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// FirstOn is the schedule's first occurrence on or after from (and on or after its start), or nil when the schedule
// has ended by then. Occurrences fall every EveryMonths months from the start's month.
func FirstOn(s storage.Schedule, from time.Time) *time.Time {
	start, from := dateOf(s.StartsOn), dateOf(from)
	if from.Before(start) {
		from = start
	}
	if s.EveryMonths < 1 || s.DayOfMonth < 1 {
		return nil
	}
	k := 0
	if months := (from.Year()-start.Year())*12 + int(from.Month()) - int(start.Month()); months > 0 {
		k = months / s.EveryMonths
	}
	for ; ; k++ {
		m := int(start.Month()) - 1 + k*s.EveryMonths
		t := onDay(start.Year()+m/12, time.Month(m%12+1), s.DayOfMonth)
		if t.Before(from) {
			continue
		}
		if s.EndsOn != nil && t.After(dateOf(*s.EndsOn)) {
			return nil
		}
		return &t
	}
}

// NextAfter is the schedule's first occurrence after on, or nil when it has ended.
func NextAfter(s storage.Schedule, on time.Time) *time.Time {
	return FirstOn(s, dateOf(on).AddDate(0, 0, 1))
}

// RecurringContent is what each draft of a recurring journal says: its memo and lines, and whether it reverses on the
// first day of the next month (an accrual, FR-GL-02).
type RecurringContent struct {
	Memo     string
	Lines    []ledger.JournalLine
	Reverses bool
}

// contentPayload is the content as a recurring journal version stores it: a journal's payload without its date, which
// create_scheduled_draft adds.
type contentPayload struct {
	Memo     string        `json:"memo,omitempty"`
	Lines    []payloadLine `json:"lines"`
	Reverses bool          `json:"reverses,omitempty"`
}

// EncodeContent is a recurring journal's content as its version stores it.
func EncodeContent(c RecurringContent) ([]byte, error) {
	p := contentPayload{Memo: c.Memo, Lines: linesOf(c.Lines), Reverses: c.Reverses}
	if p.Lines == nil {
		p.Lines = []payloadLine{}
	}
	return json.Marshal(p)
}

// ReadContent reads a recurring journal's content: its memo and lines as a saved journal's, and whether it reverses.
func ReadContent(b []byte) (Saved, bool, error) {
	var p contentPayload
	if err := json.Unmarshal(b, &p); err != nil {
		return Saved{}, false, &Error{CodeBadDraft, fmt.Sprintf("the recurring journal cannot be read: %v", err), "Save the recurring journal again"}
	}
	out := Saved{Memo: p.Memo, Lines: make([]SavedLine, 0, len(p.Lines))}
	for _, l := range p.Lines {
		out.Lines = append(out.Lines, SavedLine{AccountID: l.AccountID, Amount: l.Amount, FXRate: l.FXRate,
			FXRateSource: l.FXRateSource, Description: l.Description})
	}
	return out, p.Reverses, nil
}

// draftOn is a recurring journal's content as the draft of an occurrence on day: what create_scheduled_draft writes,
// for checking the content as the posting rules would.
func draftOn(content []byte, day time.Time) (storage.Document, error) {
	var p map[string]any
	if err := json.Unmarshal(content, &p); err != nil {
		return storage.Document{}, err
	}
	delete(p, "reverses")
	p["date"] = day.Format(time.DateOnly)
	b, err := json.Marshal(p)
	return storage.Document{Type: storage.DocumentJournal, Status: storage.DocumentDraft, Payload: b}, err
}

// RecurringInput is what a person sets on a recurring journal.
type RecurringInput struct {
	Name     string
	Schedule storage.Schedule
	Content  RecurringContent
}

func noOccurrence() error {
	return &Error{CodeRecurringSchedule, "the schedule ends before its first occurrence",
		"Set the end on or after the first occurrence, or leave it out"}
}

// CreateRecurring adds a recurring journal, active, whose first draft is for the schedule's first occurrence on or
// after its start: one in the past is made by the next run.
func (s Service) CreateRecurring(ctx context.Context, org uuid.UUID, in RecurringInput) (storage.RecurringJournal, error) {
	next := FirstOn(in.Schedule, in.Schedule.StartsOn)
	if next == nil {
		return storage.RecurringJournal{}, noOccurrence()
	}
	b, err := EncodeContent(in.Content)
	if err != nil {
		return storage.RecurringJournal{}, err
	}
	var out storage.RecurringJournal
	err = s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		out, err = u.RecurringJournals().Create(ctx, storage.RecurringJournalDetails{Name: in.Name, Status: storage.RecurringActive,
			Schedule: in.Schedule, NextOn: next, Content: b})
		return err
	})
	return out, err
}

func sameSchedule(a, b storage.Schedule) bool {
	return a.EveryMonths == b.EveryMonths && a.DayOfMonth == b.DayOfMonth && a.StartsOn.Equal(b.StartsOn) &&
		((a.EndsOn == nil && b.EndsOn == nil) || (a.EndsOn != nil && b.EndsOn != nil && a.EndsOn.Equal(*b.EndsOn)))
}

// sameJSON says whether two JSON documents are the same once canonical.
func sameJSON(a, b []byte) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	ca, _ := json.Marshal(x)
	cb, _ := json.Marshal(y)
	return bytes.Equal(ca, cb)
}

// UpdateRecurring changes a recurring journal at its version. Its content, when it changes, is a new version written
// by the actor; drafts already made keep theirs. A new schedule takes effect after the last occurrence made.
func (s Service) UpdateRecurring(ctx context.Context, org, id uuid.UUID, version int, in RecurringInput) (storage.RecurringJournal, error) {
	b, err := EncodeContent(in.Content)
	if err != nil {
		return storage.RecurringJournal{}, err
	}
	var out storage.RecurringJournal
	err = s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		cur, err := u.RecurringJournals().Lock(ctx, id)
		if err != nil {
			return err
		}
		content := b
		if sameJSON(cur.Content, b) {
			content = nil
		}
		next := cur.NextOn
		if !sameSchedule(cur.Schedule, in.Schedule) {
			from := in.Schedule.StartsOn
			if cur.LastOn != nil && !cur.LastOn.Before(from) {
				from = cur.LastOn.AddDate(0, 0, 1)
			}
			if next = FirstOn(in.Schedule, from); next == nil && cur.LastOn == nil {
				return noOccurrence()
			}
		}
		out, err = u.RecurringJournals().Update(ctx, id, version, storage.RecurringJournalDetails{Name: in.Name, Status: cur.Status,
			Schedule: in.Schedule, NextOn: next, Content: content})
		return err
	})
	return out, err
}

// PauseRecurring stops a recurring journal making drafts; ResumeRecurring starts it again from today: the occurrences
// it missed while paused are skipped, not made late.
func (s Service) PauseRecurring(ctx context.Context, org, id uuid.UUID, version int) (storage.RecurringJournal, error) {
	return s.setRecurring(ctx, org, id, version, storage.RecurringPaused, time.Time{})
}

// ResumeRecurring starts a paused recurring journal again; its next draft is for its first occurrence on or after
// today (the organisation's date) and after the last one made.
func (s Service) ResumeRecurring(ctx context.Context, org, id uuid.UUID, version int, today time.Time) (storage.RecurringJournal, error) {
	return s.setRecurring(ctx, org, id, version, storage.RecurringActive, today)
}

func (s Service) setRecurring(ctx context.Context, org, id uuid.UUID, version int, to storage.RecurringStatus, today time.Time) (storage.RecurringJournal, error) {
	var out storage.RecurringJournal
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		cur, err := u.RecurringJournals().Lock(ctx, id)
		if err != nil {
			return err
		}
		if cur.Status == to || cur.NextOn == nil {
			state := "ended"
			if cur.NextOn != nil {
				state = string(cur.Status)
			}
			return &Error{CodeRecurringPaused, "the recurring journal is " + state, "Pause an active recurring journal, or resume a paused one"}
		}
		next := cur.NextOn
		if to == storage.RecurringActive {
			from := today
			if cur.LastOn != nil && !cur.LastOn.Before(from) {
				from = cur.LastOn.AddDate(0, 0, 1)
			}
			next = FirstOn(cur.Schedule, from)
		}
		out, err = u.RecurringJournals().Update(ctx, id, version, storage.RecurringJournalDetails{Name: cur.Name, Status: to,
			Schedule: cur.Schedule, NextOn: next})
		return err
	})
	return out, err
}

// RecurringStatuses evaluates recurring journals as the workbook's checks did (Appendix C.1 #28, #29; ENG-20, ENG-21):
// every problem the posting rules find in the content, dated on its next occurrence (or today), as "Templates
// incomplete" (✖); then ⚠ when an occurrence before today has no draft yet; or OK, Paused or Ended. It reads in u and
// writes nothing.
func RecurringStatuses(ctx context.Context, u storage.UnitOfWork, rs []storage.RecurringJournal, today time.Time) (map[uuid.UUID][]Status, error) {
	out := map[uuid.UUID][]Status{}
	if len(rs) == 0 {
		return out, nil
	}
	settings, err := loadSettings(ctx, u)
	if err != nil {
		return nil, err
	}
	open := func(time.Time) *storage.Period { return &storage.Period{Status: storage.PeriodOpen} }
	for _, r := range rs {
		on := today
		if r.NextOn != nil {
			on = *r.NextOn
		}
		doc, err := draftOn(r.Content, on)
		if err != nil {
			return nil, err
		}
		// The draft takes its rates when it posts, from the cascade as it then is: a rate not in it yet is no problem now.
		all, err := draftStatuses(doc, settings, open, func(j *ledger.Journal) (ledger.Problems, error) {
			_, err := rated(ctx, u, j, settings.Base)
			return nil, err
		})
		if err != nil {
			return nil, err
		}
		var st []Status
		for _, x := range all {
			if x.Code == ledger.CodeMissingRate {
				continue
			}
			if x.Severity == SeverityError {
				st = append(st, x)
			}
		}
		switch {
		case r.NextOn == nil:
			st = append(st, status(SeverityOK, StatusRecurringEnded, "", "Its schedule has ended: it makes no more drafts.", ""))
		case r.Status == storage.RecurringPaused:
			st = append(st, status(SeverityOK, StatusRecurringPaused, "", "It is paused: it makes no drafts until it is resumed.", ""))
		case r.NextOn.Before(today):
			st = append(st, status(SeverityWarning, StatusRecurringDue, "next_on", "Its draft for "+r.NextOn.Format(time.DateOnly)+" is not made yet.",
				"The scheduler makes it within the hour; if it stays, the scheduler may have failed: tell an administrator"))
		case len(st) == 0:
			st = append(st, status(SeverityOK, StatusRecurringReady, "", "Its next draft is made on "+r.NextOn.Format(time.DateOnly)+".", ""))
		}
		out[r.ID] = st
	}
	return out, nil
}

// RecurringRun is what a run of the recurring journals did in one organisation.
type RecurringRun struct {
	Organisation uuid.UUID
	Day          time.Time // the organisation's day the run made drafts up to
	Made         int
}

// MakeDue makes the draft journal of every occurrence of org's active recurring journals on or before day (FR-GL-02,
// FR-AUT-01), each in its own transaction, under the scheduler's principal (FR-AUT-02); at most maxCatchUp occurrences
// of one recurring journal a run. A draft is dated on its occurrence, whatever its period: its statuses say whether it
// can post. One that was paused, ended or changed meanwhile is left for the next run.
func (s Service) MakeDue(ctx context.Context, org uuid.UUID, day time.Time) (RecurringRun, error) {
	ctx = storage.WithAudit(ctx, storage.AuditContext{Actor: &RecurringMaker, Reason: RecurringReason})
	run := RecurringRun{Organisation: org, Day: day}
	var due []uuid.UUID
	if err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		due, err = u.RecurringJournals().Due(ctx, day.Format(time.DateOnly), MaxRecurringDue)
		return err
	}); err != nil {
		return run, err
	}
	for _, id := range due {
		for range maxCatchUp {
			made := false
			err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
				r, err := u.RecurringJournals().Lock(ctx, id)
				if err != nil || r.Status != storage.RecurringActive || r.NextOn == nil || r.NextOn.After(day) {
					return err
				}
				draft, err := u.RecurringJournals().MakeDraft(ctx, id, *r.NextOn, NextAfter(r.Schedule, *r.NextOn))
				if err != nil {
					return err
				}
				made = true
				if s.AfterMaking == nil {
					return nil
				}
				return s.AfterMaking(ctx, u, org, r.Name, draft, *r.NextOn)
			})
			if storage.CodeOf(err) == storage.CodeRecurring {
				break // changed meanwhile: the next run sees it as it is
			}
			if err != nil {
				return run, fmt.Errorf("recurring journal %s: %w", id, err)
			}
			if !made {
				break
			}
			run.Made++
		}
	}
	return run, nil
}

// MakeDueAll runs the recurring journals of every organisation up to its own today, in its time zone, at now. An
// organisation that fails does not stop the others; their errors come back together.
func MakeDueAll(ctx context.Context, store storage.Store, now time.Time) ([]RecurringRun, error) {
	return Service{Store: store}.MakeDueAll(ctx, now)
}

// MakeDueAll is MakeDueAll with s's hooks.
func (s Service) MakeDueAll(ctx context.Context, now time.Time) ([]RecurringRun, error) {
	store := s.Store
	orgs, err := store.Organisations(ctx)
	if err != nil {
		return nil, err
	}
	var runs []RecurringRun
	var errs []error
	for _, org := range orgs {
		day, err := todayIn(ctx, store, org, now)
		if err != nil {
			errs = append(errs, fmt.Errorf("organisation %s: %w", org, err))
			continue
		}
		run, err := s.MakeDue(ctx, org, day)
		runs = append(runs, run)
		if err != nil {
			errs = append(errs, fmt.Errorf("organisation %s: %w", org, err))
		}
	}
	return runs, errors.Join(errs...)
}

// todayIn is org's date at now, in its time zone (UTC when it has none).
func todayIn(ctx context.Context, store storage.Store, org uuid.UUID, now time.Time) (time.Time, error) {
	var day time.Time
	err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		loc, err := time.LoadLocation(o.Profile.TimeZone)
		if err != nil || o.Profile.TimeZone == "" {
			loc = time.UTC
		}
		day = dateOf(now.In(loc))
		return nil
	})
	return day, err
}
