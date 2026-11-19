package nemura

import (
	"context"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/nemura/leave"
)

// Calendar entry kinds (NM-LV-05).
const (
	EntryHoliday = "holiday" // a public holiday as the person observes it
	EntryAway    = "away"    // away, with no type: what a viewer without the right sees
	EntryLeave   = "leave"   // approved leave, with its type, for those who may see it
	EntryPending = "pending" // a request waiting for a decision, for those who may see it
	EntrySick    = "sick"    // sick leave, for the person and the people team only
)

// CalendarEntry is one day of one person's month.
type CalendarEntry struct {
	Date        time.Time
	Kind, Label string
}

// CalendarPerson is a person's month: their working days and what falls on the others.
type CalendarPerson struct {
	ID       uuid.UUID
	Name     string
	Weekdays []int
	Entries  []CalendarEntry
}

// CalendarView says who is looking: the people they may see the leave type of (their own, their reports', everyone's for the people team) and whether they may see
// sick leave (the HR officer and the HR administrator, and each person their own). Everyone else in their team, by manager or department, shows as "Away".
type CalendarView struct {
	Member  uuid.UUID
	On      time.Time
	All     bool
	Reports bool
	SickAll bool
}

var typeNames = map[string]string{AnnualLeave: "Annual leave"}

// LeaveCalendar is the month of the people the viewer may see: leave and sick leave as "Away" unless the viewer holds the right to the type, and each person's
// public holidays as they observe them by their working pattern (NM-LV-05, Appendix A).
func LeaveCalendar(ctx context.Context, tx pgx.Tx, v pack.Version, vw CalendarView, month time.Time) ([]CalendarPerson, error) {
	first := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.UTC)
	last := first.AddDate(0, 1, -1)
	rows, err := tx.Query(ctx, `
WITH me AS (
  SELECT j.manager_person_id, j.department FROM nemura.person p
  JOIN nemura.person_job j ON j.organisation_id = p.organisation_id AND j.person_id = p.id AND j.effective_from <= $4 AND (j.effective_to IS NULL OR j.effective_to > $4)
  WHERE p.member_id = $1)
SELECT p.id, coalesce(nullif(p.preferred_name, ''), p.legal_name), coalesce(p.member_id = $1, false),
  coalesce(p.member_id IN (SELECT l.report_id FROM nemura.reporting_line l WHERE l.manager_id = $1 AND l.effective_from <= $4 AND (l.effective_to IS NULL OR l.effective_to > $4)), false),
  coalesce((me.manager_person_id IS NOT NULL AND j.manager_person_id = me.manager_person_id) OR (me.department IS NOT NULL AND j.department = me.department), false),
  greatest(p.employment_start, $2::date)
FROM nemura.person p
LEFT JOIN nemura.person_job j ON j.organisation_id = p.organisation_id AND j.person_id = p.id AND j.effective_from <= $4 AND (j.effective_to IS NULL OR j.effective_to > $4)
LEFT JOIN me ON TRUE
WHERE p.employment_start <= $3 AND (p.leaving_date IS NULL OR p.leaving_date >= $2)
ORDER BY lower(p.legal_name), p.id`, vw.Member, day(first), day(last), day(vw.On))
	if err != nil {
		return nil, err
	}
	type who struct {
		p                      CalendarPerson
		own, report, colleague bool
		from                   time.Time
	}
	var list []who
	for rows.Next() {
		var w who
		if err := rows.Scan(&w.p.ID, &w.p.Name, &w.own, &w.report, &w.colleague, &w.from); err != nil {
			rows.Close()
			return nil, err
		}
		if vw.All || w.own || w.report || w.colleague {
			list = append(list, w)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]CalendarPerson, 0, len(list))
	for _, w := range list {
		sees := vw.All || w.own || (vw.Reports && w.report)
		sick := vw.SickAll || w.own
		pat, ok, err := WorkingPatternOn(ctx, tx, w.p.ID, w.from)
		if err != nil {
			return nil, err
		}
		var holidays map[string]string
		if ok {
			w.p.Weekdays = pat.Weekdays
			if holidays, err = leave.Observed(v, pat, first, last); err != nil {
				holidays = nil // no calendar for the year in the pack yet: the month shows no holidays
			}
		}
		for dt, name := range holidays {
			if t, err := time.Parse(time.DateOnly, dt); err == nil && !t.Before(first) && !t.After(last) {
				w.p.Entries = append(w.p.Entries, CalendarEntry{Date: t, Kind: EntryHoliday, Label: name})
			}
		}
		working := func(t time.Time) bool {
			if !ok {
				return true
			}
			_, holiday := holidays[t.Format(time.DateOnly)]
			return pat.Works(t.Weekday()) && !holiday
		}
		spans := func(q string, args ...any) ([]struct {
			from, to time.Time
			state    string
		}, error) {
			rs, err := tx.Query(ctx, q, args...)
			if err != nil {
				return nil, err
			}
			defer rs.Close()
			var got []struct {
				from, to time.Time
				state    string
			}
			for rs.Next() {
				var s struct {
					from, to time.Time
					state    string
				}
				if err := rs.Scan(&s.from, &s.to, &s.state); err != nil {
					return nil, err
				}
				got = append(got, s)
			}
			return got, rs.Err()
		}
		add := func(from, to time.Time, kind, label string) {
			for t := maxDay(from, first); !t.After(minDay(to, last)); t = t.AddDate(0, 0, 1) {
				if working(t) {
					w.p.Entries = append(w.p.Entries, CalendarEntry{Date: t, Kind: kind, Label: label})
				}
			}
		}
		reqs, err := spans(`SELECT start_date, end_date, state FROM nemura.leave_request WHERE person_id = $1 AND state IN ('approved', 'pending') AND end_date >= $2 AND start_date <= $3`, w.p.ID, day(first), day(last))
		if err != nil {
			return nil, err
		}
		for _, r := range reqs {
			switch {
			case r.state == "pending" && sees:
				add(r.from, r.to, EntryPending, typeNames[AnnualLeave])
			case r.state == "approved" && sees:
				add(r.from, r.to, EntryLeave, typeNames[AnnualLeave])
			case r.state == "approved":
				add(r.from, r.to, EntryAway, "Away")
			}
		}
		sicks, err := spans(`SELECT start_date, end_date, 'sick' FROM nemura.sick_notification WHERE person_id = $1 AND end_date >= $2 AND start_date <= $3`, w.p.ID, day(first), day(last))
		if err != nil {
			return nil, err
		}
		for _, r := range sicks {
			if sick {
				add(r.from, r.to, EntrySick, "Sick leave")
			} else {
				add(r.from, r.to, EntryAway, "Away")
			}
		}
		sort.Slice(w.p.Entries, func(i, j int) bool {
			if !w.p.Entries[i].Date.Equal(w.p.Entries[j].Date) {
				return w.p.Entries[i].Date.Before(w.p.Entries[j].Date)
			}
			return w.p.Entries[i].Kind < w.p.Entries[j].Kind
		})
		out = append(out, w.p)
	}
	return out, nil
}

func maxDay(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func minDay(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
