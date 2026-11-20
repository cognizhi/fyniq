package proposal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"slices"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// recur.create_template (PRD-001 Appendix E; p1-ai-proposals task 4.1, design D8; FR-GL-02, FR-AUT-01): a recurring journal, through the function a person's
// Add a recurring journal runs (journal.CreateRecurringIn). v1 is the journal template alone (an accrual or a prepayment that repeats): a recurring bill
// or invoice is a later version. Its drafts are made by the scheduler for a person to post under the usual approvals: nothing recurring posts by itself
// (rev 0.58), so the template posts nothing. The lines keep to ordinary accounts, in the base currency (journal_lines.go), the schedule is the
// API's (every 1, 3, 6 or 12 months), and its monthly equivalent, the debits over the months between occurrences, stays within the task type's limit.
// The undo pauses the template, which is a person's own action on it (a template is never deleted), while nobody has changed it since: drafts the
// scheduler has made stay as drafts for a person to post or ignore.

// RecurringTemplatePayload is recur.create_template's payload.
type RecurringTemplatePayload struct {
	Name        string          `json:"name"`
	Memo        string          `json:"memo"`
	Lines       []JournalLineIn `json:"lines"`
	Reverses    bool            `json:"reverses"`
	EveryMonths int             `json:"every_months"`
	DayOfMonth  int             `json:"day_of_month"`
	StartsOn    string          `json:"starts_on"`
	EndsOn      string          `json:"ends_on"`
}

func recurringKey(id uuid.UUID) string { return "recurring_journal:" + id.String() }

func (p RecurringTemplatePayload) input(ctx context.Context, u storage.UnitOfWork) (journal.RecurringInput, *apd.Decimal, int64, error) {
	var out journal.RecurringInput
	name := strings.TrimSpace(p.Name)
	if name == "" || len(name) > 120 {
		return out, nil, 0, refusal("payload.name is empty or over 120 characters", "Name the recurring journal, such as Monthly rent accrual")
	}
	if !slices.Contains([]int{1, 3, 6, 12}, p.EveryMonths) {
		return out, nil, 0, refusal("payload.every_months is not 1, 3, 6 or 12", "Repeat it every 1, 3, 6 or 12 months")
	}
	if p.DayOfMonth < 1 || p.DayOfMonth > 31 {
		return out, nil, 0, refusal("payload.day_of_month is not 1 to 31", "Give the day of the month; 29 to 31 mean the last day of a shorter month")
	}
	starts, err := time.Parse(time.DateOnly, p.StartsOn)
	if err != nil {
		return out, nil, 0, refusal("payload.starts_on is not a date", "Send the first day it may repeat from as YYYY-MM-DD")
	}
	out.Name = name
	out.Schedule = storage.Schedule{EveryMonths: p.EveryMonths, DayOfMonth: p.DayOfMonth, StartsOn: starts}
	if p.EndsOn != "" {
		ends, err := time.Parse(time.DateOnly, p.EndsOn)
		if err != nil || ends.Before(starts) {
			return out, nil, 0, refusal("payload.ends_on is not a date on or after the start", "Send the last day as YYYY-MM-DD, or leave it out")
		}
		out.Schedule.EndsOn = &ends
	}
	if journal.FirstOn(out.Schedule, starts) == nil {
		return out, nil, 0, refusal("the schedule ends before its first occurrence", "Set the end on or after the first occurrence, or leave it out")
	}
	lines, debits, err := JournalLinesOf(ctx, u, "payload.lines", p.Lines)
	if err != nil {
		return out, nil, 0, err
	}
	out.Content = journal.RecurringContent{Memo: strings.TrimSpace(p.Memo), Lines: lines, Reverses: p.Reverses}
	total, err := decimalOf(debits)
	if err != nil {
		return out, nil, 0, err
	}
	monthly := new(apd.Decimal)
	if _, err := apd.BaseContext.WithPrecision(34).Quo(monthly, total, apd.New(int64(p.EveryMonths), 0)); err != nil {
		return out, nil, 0, err
	}
	h := fnv.New64a()
	for _, l := range lines {
		a, err := u.Accounts().Get(ctx, l.AccountID)
		if err != nil {
			return out, nil, 0, err
		}
		fmt.Fprintf(h, "%s:%d:%t;", a.ID, a.Version, a.Active)
	}
	return out, monthly, int64(h.Sum64() >> 1), nil // #nosec G115 -- 63 bits
}

// CreateRecurring is recur.create_template's command.
type CreateRecurring struct{}

// Policy implements Command: the right to add a recurring journal.
func (CreateRecurring) Policy() authz.Policy { return authz.CreateRecurringJournal }

// Measure implements Command: one template, its monthly equivalent as its amount (read in the dry run's checks: a payload that does not read has none).
func (CreateRecurring) Measure(payload json.RawMessage) (Measure, error) {
	var p RecurringTemplatePayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return Measure{}, err
	}
	total := new(apd.Decimal)
	for _, l := range p.Lines {
		d, _, err := apd.NewFromString(l.Amount)
		if err != nil || d.Form != apd.Finite {
			return Measure{}, refusal("a line's amount is not a decimal", "Send each amount as a decimal string such as 250.00")
		}
		if d.Sign() > 0 {
			if _, err := apd.BaseContext.WithPrecision(34).Add(total, total, d); err != nil {
				return Measure{}, err
			}
		}
	}
	if !slices.Contains([]int{1, 3, 6, 12}, p.EveryMonths) {
		return Measure{}, refusal("payload.every_months is not 1, 3, 6 or 12", "Repeat it every 1, 3, 6 or 12 months")
	}
	monthly := new(apd.Decimal)
	if _, err := apd.BaseContext.WithPrecision(34).Quo(monthly, total, apd.New(int64(p.EveryMonths), 0)); err != nil {
		return Measure{}, err
	}
	return Measure{Items: 1, Amounts: []string{monthly.Text('f')}}, nil
}

// DryRun implements Command: the template is made in the rolled-back unit of work, and what its checks find is shown; a template whose content the
// posting rules would refuse is refused here, so the approver never approves one that makes unpostable drafts.
func (CreateRecurring) DryRun(ctx context.Context, u storage.UnitOfWork, payload json.RawMessage, as Actor) (Impact, error) {
	r, impact, err := createRecurring(ctx, u, payload, as.Today)
	if err != nil {
		return Impact{}, err
	}
	_ = r
	return impact, nil
}

func createRecurring(ctx context.Context, u storage.UnitOfWork, payload json.RawMessage, today time.Time) (storage.RecurringJournal, Impact, error) {
	var p RecurringTemplatePayload
	if err := decodeStrict(payload, &p); err != nil {
		return storage.RecurringJournal{}, Impact{}, err
	}
	in, monthly, fingerprint, err := p.input(ctx, u)
	if err != nil {
		return storage.RecurringJournal{}, Impact{}, err
	}
	r, err := journal.CreateRecurringIn(ctx, u, in)
	if err != nil {
		return r, Impact{}, recurringRefusal(err)
	}
	statuses, err := journal.RecurringStatuses(ctx, u, []storage.RecurringJournal{r}, today)
	if err != nil {
		return r, Impact{}, err
	}
	var stops []string
	for _, s := range statuses[r.ID] {
		if s.Severity == journal.SeverityError {
			stops = append(stops, s.Title)
		}
	}
	if len(stops) > 0 {
		return r, Impact{}, refusal("the template's drafts would not post: "+strings.Join(stops, "; "), "Correct the lines so each occurrence's draft can be posted")
	}
	lines := make([]map[string]any, 0, len(p.Lines))
	for _, l := range p.Lines {
		lines = append(lines, map[string]any{"account": l.Account, "amount": l.Amount, "description": l.Description})
	}
	next := ""
	if r.NextOn != nil {
		next = r.NextOn.Format(time.DateOnly)
	}
	changes, err := json.Marshal(map[string]any{"recurring_journals": []map[string]any{{"change": "create", "name": in.Name, "every_months": p.EveryMonths,
		"day_of_month": p.DayOfMonth, "starts_on": p.StartsOn, "ends_on": nilIfBlank(p.EndsOn), "first_draft_on": next, "reverses_next_month": p.Reverses,
		"monthly_equivalent": monthly.Text('f'), "lines": lines}},
		"ledger": "Nothing is posted. The scheduler makes a draft journal on each occurrence, which a person posts under the usual approvals. The undo pauses the template"})
	if err != nil {
		return r, Impact{}, err
	}
	return r, Impact{Summary: fmt.Sprintf("Adds the recurring journal %q every %d month(s) on day %d from %s, about %s a month; it makes drafts and posts nothing", in.Name, p.EveryMonths,
		p.DayOfMonth, p.StartsOn, monthly.Text('f')), Changes: changes, Base: map[string]int64{"accounts": fingerprint}}, nil
}

func recurringRefusal(err error) error {
	var je *journal.Error
	if errors.As(err, &je) {
		return refusal(je.Message, je.Fix)
	}
	return err
}

// Execute implements Command: only while the accounts it names are as the dry run read them (AI-P5).
func (CreateRecurring) Execute(ctx context.Context, u storage.UnitOfWork, payload json.RawMessage, base map[string]int64, as Actor) (Execution, error) {
	var p RecurringTemplatePayload
	if err := decodeStrict(payload, &p); err != nil {
		return Execution{}, err
	}
	in, _, fingerprint, err := p.input(ctx, u)
	if err != nil {
		return Execution{}, err
	}
	if want, ok := base["accounts"]; !ok || want != fingerprint {
		return Execution{}, ErrStale
	}
	r, err := journal.CreateRecurringIn(ctx, u, in)
	if err != nil {
		return Execution{}, recurringRefusal(err)
	}
	undo, err := json.Marshal(map[string]any{"recurring_journal": r.ID.String(), "version": r.Version})
	if err != nil {
		return Execution{}, err
	}
	return Execution{Records: []string{recurringKey(r.ID)}, Entries: []string{}, Undo: undo}, nil
}

// Undo implements Undoer: the template is paused, as a person's own Pause does, while it is at the version the execution left it.
func (CreateRecurring) Undo(ctx context.Context, u storage.UnitOfWork, done Execution, as Actor) (Execution, error) {
	var st struct {
		ID      string `json:"recurring_journal"`
		Version int    `json:"version"`
	}
	if err := json.Unmarshal(done.Undo, &st); err != nil || st.ID == "" {
		return Execution{}, refusal("the execution kept nothing to undo it with", "Pause the recurring journal by hand")
	}
	id, err := uuid.Parse(st.ID)
	if err != nil {
		return Execution{}, err
	}
	r, err := journal.PauseRecurringIn(ctx, u, id, st.Version)
	switch {
	case storage.CodeOf(err) == storage.CodeVersion:
		return Execution{}, ErrStale
	case err != nil:
		return Execution{}, recurringRefusal(err)
	}
	return Execution{Records: []string{recurringKey(r.ID)}, Entries: []string{}}, nil
}

var _ Undoer = CreateRecurring{}
