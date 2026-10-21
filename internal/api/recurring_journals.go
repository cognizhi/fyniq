package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ recurring journals (FR-GL-02, FR-AUT-01; rev 0.60)
//
// A recurring journal is a journal's content and a schedule. The scheduler makes the draft of each occurrence, which a
// person posts under the usual approvals (the founders' decision of 21 Oct 2026, PRD-001 rev 0.58): nothing recurring
// posts by itself.

// RecurringSchedule is when a recurring journal recurs (FR-GL-02, FR-AR-07).
type RecurringSchedule struct {
	EveryMonths int    `json:"every_months" enum:"1,3,6,12" doc:"Every 1, 3, 6 or 12 months: monthly, quarterly, half-yearly or yearly." example:"1"`
	DayOfMonth  int    `json:"day_of_month" minimum:"1" maximum:"31" doc:"The day of the month of each occurrence; the 29th to the 31st are the last day of a shorter month (FR-AR-07)." example:"1"`
	StartsOn    string `json:"starts_on" format:"date" doc:"The first occurrence is the first on or after this date, in the months from this one; one in the past is made by the next run." example:"2026-11-01"`
	EndsOn      string `json:"ends_on,omitempty" format:"date" doc:"No occurrence after this date; leave it out for no end." example:"2027-10-31"`
}

// RecurringJournalInput is what a person sets on a recurring journal.
type RecurringJournalInput struct {
	Name     string             `json:"name" minLength:"1" maxLength:"120" doc:"What it is, as the list shows it." example:"Office rent accrual"`
	Schedule RecurringSchedule  `json:"schedule"`
	Memo     string             `json:"memo,omitempty" maxLength:"1000" doc:"The memo of each draft." example:"Office rent accrual"`
	Lines    []JournalLineDraft `json:"lines" maxItems:"1000" doc:"The lines of each draft: debits positive, credits negative, as on a journal."`
	Reverses bool               `json:"reverses,omitempty" doc:"Each draft reverses by itself on the first day of the next month, as an accrual does (FR-GL-02)." example:"true"`
}

// RecurringJournal is a recurring journal at its version (FR-GL-02; PRD-001 rev 0.60).
type RecurringJournal struct {
	ID             string             `json:"id" format:"uuid" doc:"The recurring journal's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e10"`
	Name           string             `json:"name" doc:"What it is." example:"Office rent accrual"`
	Status         string             `json:"status" enum:"active,paused,ended" doc:"active makes its drafts; paused makes none until it is resumed; ended has no occurrence left." example:"active"`
	Schedule       RecurringSchedule  `json:"schedule"`
	NextOn         string             `json:"next_on,omitempty" format:"date" doc:"The next occurrence the scheduler makes a draft for, on that day; absent once it has ended." example:"2026-12-01"`
	LastOn         string             `json:"last_on,omitempty" format:"date" doc:"The last occurrence a draft was made for." example:"2026-11-01"`
	LastJournalID  string             `json:"last_journal_id,omitempty" format:"uuid" doc:"That draft." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d70"`
	Memo           string             `json:"memo,omitempty" doc:"The memo of each draft." example:"Office rent accrual"`
	Lines          []JournalLineDraft `json:"lines" doc:"The lines of each draft."`
	Reverses       bool               `json:"reverses" doc:"Each draft reverses by itself on the first day of the next month." example:"true"`
	Debits         []Money            `json:"debits" nullable:"false" doc:"The total of its debit lines, per currency."`
	Version        int                `json:"version" doc:"The version of the recurring journal; it makes the ETag." example:"3"`
	ContentVersion int                `json:"content_version" doc:"The version of its content, which drafts name; it changes when the memo, lines or reversal change." example:"2"`
	EditedBy       *Person            `json:"edited_by,omitempty" doc:"Who wrote the current content: they do not approve the posting of its drafts (SOD-3)."`
	EditedAt       time.Time          `json:"edited_at" doc:"When the current content was written." example:"2026-10-21T09:30:00Z"`
	Checks         []JournalCheck     `json:"checks" nullable:"false" doc:"Its statuses (Appendix C.1 #28, #29; ENG-20, ENG-21): every problem the posting rules find in its content, as Templates incomplete (✖); a warning when an occurrence before today has no draft yet; or OK, Paused or Ended."`
}

// RecurringJournalPage is a page of recurring journals, by name.
type RecurringJournalPage struct {
	Items      []RecurringJournal `json:"items" nullable:"false"`
	NextCursor string             `json:"next_cursor,omitempty" doc:"Pass it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6Ik9mZmljZSByZW50fDAxOTIifQ"`
}

// RecurringJournalPath names a recurring journal.
type RecurringJournalPath struct {
	RecurringJournalID string `path:"recurring_journal_id" format:"uuid" doc:"The recurring journal's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e10"`
}

type recurringListInput struct {
	PageQuery
}

type recurringCreateInput struct {
	KeyHeader
	PreferHeader
	Body RecurringJournalInput
}

type recurringReplaceInput struct {
	RecurringJournalPath
	IfMatchHeader
	PreferHeader
	Body RecurringJournalInput
}

type recurringStateInput struct {
	RecurringJournalPath
	KeyHeader
	IfMatchHeader
	PreferHeader
}

type recurringListOutput struct{ Body RecurringJournalPage }

type recurringReadOutput struct {
	ETag string `header:"ETag" doc:"The recurring journal's version, for If-Match."`
	Body RecurringJournal
}

type recurringWriteOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The recurring journal's version, for If-Match."`
	Location string `header:"Location" doc:"The recurring journal's URL."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     RecurringJournal
}

// RecurringJournalCodes are the rules a recurring journal's changes may answer.
var RecurringJournalCodes = []string{journal.CodeRecurringSchedule, journal.CodeRecurringPaused, storage.CodeVersion,
	money.CodeNotDecimal, money.CodePrecision, money.CodeRange, money.CodeCurrency, money.CodeRate, storage.CodeRetry, storage.CodeLockTimeout}

func optDate(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.DateOnly)
}

// recurringOf is a recurring journal as the API shows it, with its statuses.
func recurringOf(r storage.RecurringJournal, st []journal.Status) (RecurringJournal, error) {
	saved, reverses, err := journal.ReadContent(r.Content)
	if err != nil {
		return RecurringJournal{}, err
	}
	out := RecurringJournal{ID: r.ID.String(), Name: r.Name, Status: string(r.Status), NextOn: optDate(r.NextOn), LastOn: optDate(r.LastOn),
		Schedule: RecurringSchedule{EveryMonths: r.Schedule.EveryMonths, DayOfMonth: r.Schedule.DayOfMonth,
			StartsOn: r.Schedule.StartsOn.Format(time.DateOnly), EndsOn: optDate(r.Schedule.EndsOn)},
		Memo: saved.Memo, Lines: linesOut(saved.Lines), Reverses: reverses, Version: r.Version, ContentVersion: r.ContentVersion,
		EditedBy: personOf(r.EditedBy), EditedAt: r.EditedAt.UTC(), Checks: checksOf(st)}
	if r.NextOn == nil {
		out.Status = "ended"
	}
	if r.LastJournalID != nil {
		out.LastJournalID = r.LastJournalID.String()
	}
	out.Debits, err = debitTotals(saved.Lines)
	return out, err
}

// recurringInputOf reads a recurring journal from the request, with every invalid field at once.
func recurringInputOf(in RecurringJournalInput, base money.Currency) (journal.RecurringInput, error) {
	out := journal.RecurringInput{Name: strings.TrimSpace(in.Name)}
	var bad []FieldProblem
	if out.Name == "" {
		bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.name", Message: "is empty", Fix: "Name the recurring journal"})
	}
	starts, err := time.Parse(time.DateOnly, in.Schedule.StartsOn)
	if err != nil {
		bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.schedule.starts_on", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"})
	}
	out.Schedule = storage.Schedule{EveryMonths: in.Schedule.EveryMonths, DayOfMonth: in.Schedule.DayOfMonth, StartsOn: starts}
	if in.Schedule.EndsOn != "" {
		ends, err := time.Parse(time.DateOnly, in.Schedule.EndsOn)
		switch {
		case err != nil:
			bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.schedule.ends_on", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"})
		case ends.Before(starts):
			bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.schedule.ends_on", Message: "is before the start",
				Fix: "End it on or after " + in.Schedule.StartsOn + ", or leave the end out"})
		default:
			out.Schedule.EndsOn = &ends
		}
	}
	d, err := draftOf(JournalDraft{Memo: in.Memo, Lines: in.Lines}, base)
	if p, ok := err.(*Problem); ok {
		bad = append(bad, p.Errors...)
	} else if err != nil {
		return out, err
	}
	if len(bad) > 0 {
		return out, invalid(bad)
	}
	out.Content = journal.RecurringContent{Memo: d.Memo, Lines: d.Lines, Reverses: in.Reverses}
	return out, nil
}

// recurringCursor reads a "name|id" cursor; the name may itself hold a "|".
func recurringCursor(after string) (string, *uuid.UUID, error) {
	if after == "" {
		return "", nil, nil
	}
	i := strings.LastIndex(after, "|")
	if i < 0 {
		return "", nil, badCursor()
	}
	id, err := uuid.Parse(after[i+1:])
	if err != nil {
		return "", nil, badCursor()
	}
	return after[:i], &id, nil
}

// viewRecurring is a recurring journal with its statuses, and the name of who wrote its content.
func (a *API) viewRecurring(ctx context.Context, u storage.UnitOfWork, rs ...storage.RecurringJournal) ([]RecurringJournal, error) {
	today, err := orgToday(ctx, a, u)
	if err != nil {
		return nil, err
	}
	st, err := journal.RecurringStatuses(ctx, u, rs, today)
	if err != nil {
		return nil, err
	}
	out := make([]RecurringJournal, 0, len(rs))
	var people []*Person
	for _, r := range rs {
		v, err := recurringOf(r, st[r.ID])
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	for i := range out {
		people = append(people, out[i].EditedBy)
	}
	return out, a.name(ctx, u, people...)
}

func registerRecurringJournals(a *API) {
	register(a, authz.ListRecurringJournals, huma.Operation{
		OperationID: "listRecurringJournals", Method: http.MethodGet, Path: "/v1/recurring-journals", Tags: []string{"Journals"},
		Summary: "List the recurring journals",
		Description: "The organisation's recurring journals by name, a page at a time (FR-GL-02; PRD-001 rev 0.60): each one's schedule, " +
			"next occurrence, content and statuses. Read-only; no side effects.",
	}, []string{CodeCursor}, func(ctx context.Context, in *recurringListInput) (*recurringListOutput, error) {
		after, err := decodeCursor(in.Cursor)
		if err != nil {
			return nil, err
		}
		afterName, afterID, err := recurringCursor(after)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		out := recurringListOutput{Body: RecurringJournalPage{Items: []RecurringJournal{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			page, err := u.RecurringJournals().Page(ctx, afterName, afterID, in.PageSize+1)
			if err != nil {
				return err
			}
			if len(page) > in.PageSize {
				page = page[:in.PageSize]
				last := page[len(page)-1]
				out.Body.NextCursor = encodeCursor(last.Name + "|" + last.ID.String())
			}
			out.Body.Items, err = a.viewRecurring(ctx, u, page...)
			return err
		})
		return &out, err
	})

	register(a, authz.CreateRecurringJournal, huma.Operation{
		OperationID: "createRecurringJournal", Method: http.MethodPost, Path: "/v1/recurring-journals", Tags: []string{"Journals"},
		DefaultStatus: http.StatusCreated, Summary: "Add a recurring journal",
		Description: "Adds a recurring journal, active: a journal's memo and lines, whether each draft reverses on the first day of the " +
			"next month, and a schedule (every 1, 3, 6 or 12 months on a day of the month, from a start, until an optional end; " +
			"FR-GL-02, FR-AR-07; PRD-001 rev 0.60). The scheduler makes the draft journal of each occurrence, dated on it, which a " +
			"person posts under the usual approvals: nothing recurring posts by itself, and whoever wrote the content does not " +
			"approve its drafts (SOD-3). Its content may be incomplete: its statuses say what the posting rules would refuse. Side " +
			"effects: the recurring journal, its first content version and its audit event. Idempotent with Idempotency-Key; Prefer: " +
			"return=preview checks it and saves nothing.",
	}, append([]string{CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		CodeInvalid}, RecurringJournalCodes...),
		func(ctx context.Context, in *recurringCreateInput) (*recurringWriteOutput, error) {
			p, _ := authn.From(ctx)
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (RecurringJournal, error) {
				base, err := baseOf(ctx, u)
				if err != nil {
					return RecurringJournal{}, err
				}
				input, err := recurringInputOf(in.Body, base)
				if err != nil {
					return RecurringJournal{}, err
				}
				r, err := a.journals.CreateRecurring(ctx, p.Organisation, input)
				if err != nil {
					return RecurringJournal{}, err
				}
				v, err := a.viewRecurring(ctx, u, r)
				if err != nil {
					return RecurringJournal{}, err
				}
				return v[0], nil
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &recurringWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &recurringWriteOutput{Status: w.Status, ETag: etag(w.Body.Version), Location: "/v1/recurring-journals/" + w.Body.ID,
				Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})

	register(a, authz.GetRecurringJournal, huma.Operation{
		OperationID: "getRecurringJournal", Method: http.MethodGet, Path: "/v1/recurring-journals/{recurring_journal_id}", Tags: []string{"Journals"},
		Summary:     "Get a recurring journal",
		Description: "Returns a recurring journal with its statuses and its ETag (FR-GL-02; PRD-001 rev 0.60). Read-only; no side effects.",
	}, []string{CodeNotFound}, func(ctx context.Context, in *RecurringJournalPath) (*recurringReadOutput, error) {
		id, err := parseID("recurring_journal_id", in.RecurringJournalID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out recurringReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			r, err := u.RecurringJournals().Get(ctx, id)
			if err != nil {
				return err
			}
			v, err := a.viewRecurring(ctx, u, r)
			if err != nil {
				return err
			}
			out.Body, out.ETag = v[0], etag(r.Version)
			return nil
		})
		return &out, err
	})

	register(a, authz.ReplaceRecurringJournal, huma.Operation{
		OperationID: "replaceRecurringJournal", Method: http.MethodPut, Path: "/v1/recurring-journals/{recurring_journal_id}", Tags: []string{"Journals"},
		Summary: "Change a recurring journal",
		Description: "Replaces a recurring journal's name, schedule and content (FR-GL-02; PRD-001 rev 0.60). A changed memo, lines or " +
			"reversal is a new content version, written by the caller; drafts already made keep theirs. A changed schedule takes " +
			"effect after the last occurrence made. Side effects: the next version and its audit event. Needs If-Match with the " +
			"recurring journal's ETag. Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid}, RecurringJournalCodes...),
		func(ctx context.Context, in *recurringReplaceInput) (*recurringWriteOutput, error) {
			id, err := parseID("recurring_journal_id", in.RecurringJournalID)
			if err != nil {
				return nil, err
			}
			p, _ := authn.From(ctx)
			w, err := write(ctx, a, "", in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (RecurringJournal, error) {
				cur, err := u.RecurringJournals().Get(ctx, id)
				if err != nil {
					return RecurringJournal{}, err
				}
				if err := checkIfMatch(in.IfMatch, etag(cur.Version)); err != nil {
					return RecurringJournal{}, err
				}
				base, err := baseOf(ctx, u)
				if err != nil {
					return RecurringJournal{}, err
				}
				input, err := recurringInputOf(in.Body, base)
				if err != nil {
					return RecurringJournal{}, err
				}
				r, err := a.journals.UpdateRecurring(ctx, p.Organisation, id, cur.Version, input)
				if err != nil {
					return RecurringJournal{}, err
				}
				v, err := a.viewRecurring(ctx, u, r)
				if err != nil {
					return RecurringJournal{}, err
				}
				return v[0], nil
			})
			if err != nil {
				return nil, err
			}
			return &recurringWriteOutput{Status: w.Status, ETag: etag(w.Body.Version), Applied: applied(w), Body: w.Body}, nil
		})

	state := func(policy authz.Policy, op, path, summary, description string, to storage.RecurringStatus) {
		register(a, policy, huma.Operation{
			OperationID: op, Method: http.MethodPost, Path: "/v1/recurring-journals/{recurring_journal_id}/" + path, Tags: []string{"Journals"},
			Summary: summary, Description: description,
		}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld}, RecurringJournalCodes...),
			func(ctx context.Context, in *recurringStateInput) (*recurringWriteOutput, error) {
				id, err := parseID("recurring_journal_id", in.RecurringJournalID)
				if err != nil {
					return nil, err
				}
				p, _ := authn.From(ctx)
				w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (RecurringJournal, error) {
					cur, err := u.RecurringJournals().Get(ctx, id)
					if err != nil {
						return RecurringJournal{}, err
					}
					if err := checkIfMatch(in.IfMatch, etag(cur.Version)); err != nil {
						return RecurringJournal{}, err
					}
					var r storage.RecurringJournal
					if to == storage.RecurringPaused {
						r, err = a.journals.PauseRecurring(ctx, p.Organisation, id, cur.Version)
					} else {
						today, terr := orgToday(ctx, a, u)
						if terr != nil {
							return RecurringJournal{}, terr
						}
						r, err = a.journals.ResumeRecurring(ctx, p.Organisation, id, cur.Version, today)
					}
					if err != nil {
						return RecurringJournal{}, err
					}
					v, err := a.viewRecurring(ctx, u, r)
					if err != nil {
						return RecurringJournal{}, err
					}
					return v[0], nil
				})
				if err != nil {
					return nil, err
				}
				return &recurringWriteOutput{Status: w.Status, ETag: etag(w.Body.Version), Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
			})
	}
	state(authz.PauseRecurringJournal, "pauseRecurringJournal", "pause", "Pause a recurring journal",
		"Stops an active recurring journal making drafts until it is resumed (FR-GL-02; PRD-001 rev 0.60). Drafts already made stay. "+
			"Side effects: the next version and its audit event. Needs If-Match; idempotent with Idempotency-Key; Prefer: "+
			"return=preview checks it and saves nothing.", storage.RecurringPaused)
	state(authz.ResumeRecurringJournal, "resumeRecurringJournal", "resume", "Resume a recurring journal",
		"Starts a paused recurring journal again (FR-GL-02; PRD-001 rev 0.60): its next draft is for its first occurrence on or after "+
			"today, so the occurrences it missed while paused are skipped, not made late. Side effects: the next version and its audit "+
			"event. Needs If-Match; idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.", storage.RecurringActive)
}
