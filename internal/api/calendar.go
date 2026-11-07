package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/deadlines"
	"github.com/cognizhi/fyniq/internal/domain/equity"
	"github.com/cognizhi/fyniq/internal/domain/evidence"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the statutory calendar (FR-TAX-07, CMP-02)
//
// Every filing and payment deadline of the organisation, generated each time from the sg-deadlines pack and the
// organisation's facts (p1-gst design D5): its financial years, its GST return periods, whether it pays staff, its AGM
// route. Fyniq files nothing (golden rule 5): a person records what was filed, with its acknowledgement attached, or
// that an item is not required, with the reason (design D6). A record is withdrawn with a reason, never edited.

// CalendarCodes are the codes a filing record answers besides the common ones.
var CalendarCodes = []string{deadlines.CodeFiling, deadlines.CodeRecorded, deadlines.CodeWithdrawn, storage.CodeStatutoryFiling, evidence.CodeMissing}

// StatutoryItem is one deadline of the calendar.
type StatutoryItem struct {
	Key        string               `json:"key" doc:"The item: its obligation and its anchor day. Record it with this key." example:"eci:2026-12-31"`
	Obligation string               `json:"obligation" enum:"gst_f5,cpf,ir8a_ais,eci,form_cs,annual_return,agm,statements_to_members,gst_register_retrospective,gst_register_prospective,register_controller_entry,register_controller_central,register_controller_notice,register_share_lodgement,financial_year,sg-deadlines" doc:"What is due: the register_ ones are a register's own, done and checked on their record." example:"eci"`
	Area       string               `json:"area,omitempty" enum:"tax,payroll,corporate" doc:"Its area, to filter by." example:"tax"`
	Title      string               `json:"title" doc:"What is due, in plain words." example:"Estimated chargeable income for the year ended 31 Dec 2026"`
	Anchor     string               `json:"anchor,omitempty" format:"date" doc:"The day it counts from: a period's or a year's end, or a forecast's day." example:"2026-12-31"`
	DueOn      string               `json:"due_on,omitempty" format:"date" doc:"Its due day; absent while not assessed." example:"2027-03-31"`
	Why        string               `json:"why,omitempty" doc:"The rule's words, from the sg-deadlines pack." example:"Estimated chargeable income: within 3 months from the end of the financial year, unless waived (revenue of $5 million or below and ECI nil)"`
	Source     string               `json:"source,omitempty" doc:"The pack's sources of the rule, by id." example:"iras-eci"`
	Extended   bool                 `json:"extended,omitempty" doc:"true when a one-off extension moved its day." example:"false"`
	Rolled     bool                 `json:"rolled,omitempty" doc:"true when its day moved to the next working day." example:"false"`
	Status     string               `json:"status" enum:"open,due_soon,overdue,filed,not_required,not_assessed" doc:"open; due_soon: within 14 days; overdue; filed or not_required: recorded; not_assessed: an input is missing, which reason names." example:"open"`
	Reason     string               `json:"reason,omitempty" doc:"Why it is not assessed." example:"The books start and the first financial year's end are not set: set them in Settings › Organisation"`
	Filing     *StatutoryFilingView `json:"filing,omitempty" doc:"The live record, once the item is recorded."`
	Register   bool                 `json:"register,omitempty" doc:"true for a register's own deadline (a controller, a share lodgement): done on its record, which link opens, and never recorded here." example:"false"`
	Link       string               `json:"link,omitempty" doc:"Where in the web app a register's deadline is done." example:"/equity/holders/01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c11"`
}

// StatutoryCalendarView is the calendar from one day to another.
type StatutoryCalendarView struct {
	From      string          `json:"from" format:"date" doc:"The window's first day." example:"2026-11-01"`
	To        string          `json:"to" format:"date" doc:"The window's last day." example:"2027-10-31"`
	Today     string          `json:"today" format:"date" doc:"The organisation's today, which decides each status." example:"2026-11-20"`
	Pack      string          `json:"pack,omitempty" doc:"The sg-deadlines version the days come from." example:"sg-deadlines@2026.1"`
	Starts    string          `json:"starts,omitempty" format:"date" doc:"The day its rules start: nothing due before it is in the calendar." example:"2026-11-07"`
	Deadlines []StatutoryItem `json:"deadlines" nullable:"false" doc:"Every item due in the window, with each earlier one still overdue, by due day; the items not assessed first."`
}

// StatutoryFilingDraft is a record as drafted.
type StatutoryFilingDraft struct {
	Key       string `json:"key" maxLength:"60" doc:"The item, as the calendar gives its key." example:"eci:2026-12-31"`
	Outcome   string `json:"outcome" enum:"filed,not_required" doc:"filed, with its day and reference; or not_required, with the reason." example:"filed"`
	FiledOn   string `json:"filed_on,omitempty" format:"date" doc:"For filed: the day it was filed or paid, today or earlier." example:"2027-03-20"`
	Reference string `json:"reference,omitempty" maxLength:"100" doc:"For filed: the acknowledgement or transaction number." example:"ECI-2027-000123"`
	Reason    string `json:"reason,omitempty" maxLength:"500" doc:"For not_required: why, such as the ECI waiver's conditions." example:"Revenue under S$5 million and ECI nil: waived (IRAS)"`
	Note      string `json:"note,omitempty" maxLength:"1000" doc:"A note." example:"Filed by the tax agent"`
}

// StatutoryFilingView is a record, drafted, recorded, withdrawn, or a withdrawal.
type StatutoryFilingView struct {
	ID         string      `json:"id" format:"uuid" doc:"The record's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c9a01"`
	Status     string      `json:"status" enum:"draft,recorded,withdrawn,withdrawal" doc:"draft: waiting for its evidence and to be recorded; recorded: live; withdrawn: a later withdrawal took it back; withdrawal: the record of taking one back." example:"recorded"`
	Version    int         `json:"version" doc:"The version of its content; with its status it makes the ETag." example:"1"`
	Key        string      `json:"key" doc:"The item." example:"eci:2026-12-31"`
	Outcome    string      `json:"outcome" enum:"filed,not_required,withdrawn" doc:"What was recorded." example:"filed"`
	FiledOn    string      `json:"filed_on,omitempty" format:"date" doc:"The day it was filed." example:"2027-03-20"`
	Reference  string      `json:"reference,omitempty" doc:"Its acknowledgement or transaction number." example:"ECI-2027-000123"`
	Reason     string      `json:"reason,omitempty" doc:"Why it is not required, or why it was withdrawn." example:"Revenue under S$5 million and ECI nil: waived (IRAS)"`
	Note       string      `json:"note,omitempty" doc:"A note." example:"Filed by the tax agent"`
	Withdraws  string      `json:"withdraws,omitempty" format:"uuid" doc:"A withdrawal's record." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c9a02"`
	Evidence   int         `json:"evidence" doc:"How many files are attached and not removed." example:"1"`
	RecordedBy *Person     `json:"recorded_by,omitempty" doc:"Who recorded it."`
	RecordedAt *time.Time  `json:"recorded_at,omitempty" doc:"When it was recorded." example:"2027-03-20T09:30:00Z"`
	Checks     []BillCheck `json:"checks" nullable:"false" doc:"A draft's status: ✖ while a filed record's required acknowledgement is not attached."`
}

// StatutoryFilingPage is a page of records, the latest first.
type StatutoryFilingPage struct {
	Items      []StatutoryFilingView `json:"items" nullable:"false"`
	NextCursor string                `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjIwMjYifQ"`
}

// StatutoryWithdrawal is why a record is withdrawn.
type StatutoryWithdrawal struct {
	Reason string `json:"reason" minLength:"1" maxLength:"500" doc:"Why the record was wrong." example:"Recorded against the wrong year"`
}

type (
	StatutoryFilingPath struct {
		FilingID string `path:"filing_id" format:"uuid" doc:"The filing record." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c9a01"`
	}
	StatutoryFilingEvidencePath struct {
		StatutoryFilingPath
		EvidenceID string `path:"evidence_id" format:"uuid" doc:"The file's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e21"`
	}
	calendarInput struct {
		From string `query:"from" format:"date" doc:"The window's first day; left out, the first day of today's month." example:"2026-11-01"`
		To   string `query:"to" format:"date" doc:"The window's last day, at most 18 months after from; left out, 12 months after from, less a day." example:"2027-10-31"`
	}
	calendarOutput  struct{ Body StatutoryCalendarView }
	filingListInput struct {
		Key string `query:"key" maxLength:"60" doc:"Only an item's records, by its key." example:"eci:2026-12-31"`
		PageQuery
	}
	filingListOutput struct{ Body StatutoryFilingPage }
	filingDraftInput struct {
		KeyHeader
		PreferHeader
		Body StatutoryFilingDraft
	}
	filingReviseInput struct {
		StatutoryFilingPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body StatutoryFilingDraft
	}
	filingRecordInput struct {
		StatutoryFilingPath
		KeyHeader
		IfMatchHeader
		PreferHeader
	}
	filingWithdrawInput struct {
		StatutoryFilingPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body StatutoryWithdrawal
	}
	filingReadOutput struct {
		ETag string `header:"ETag" doc:"Its version and status, for If-Match."`
		Body StatutoryFilingView
	}
	filingWriteOutput struct {
		Status   int
		ETag     string `header:"ETag" doc:"Its version and status, for If-Match."`
		Location string `header:"Location" doc:"Its URL."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     StatutoryFilingView
	}
	addFilingEvidenceInput struct {
		StatutoryFilingPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		RawBody huma.MultipartFormFiles[EvidenceUpload]
	}
	listFilingEvidenceInput struct {
		StatutoryFilingPath
		PageQuery
	}
	removeFilingEvidenceInput struct {
		StatutoryFilingEvidencePath
		IfMatchHeader
		PreferHeader
	}
)

// filingIn reads a record's fields, or what of them is wrong.
func filingIn(d StatutoryFilingDraft) (deadlines.Filing, error) {
	f := deadlines.Filing{Key: d.Key, Outcome: d.Outcome, Reference: d.Reference, Reason: d.Reason, Note: d.Note}
	var fields []FieldProblem
	f.FiledOn = dayIn("body.filed_on", d.FiledOn, &fields)
	if len(fields) > 0 {
		return f, invalid(fields)
	}
	return f, nil
}

// filingStatus is a record's status as the API names it.
func filingStatus(doc storage.Document, f deadlines.Filing) string {
	switch {
	case doc.Status == storage.DocumentDraft:
		return "draft"
	case f.Outcome == deadlines.OutcomeWithdrawn:
		return "withdrawal"
	case doc.Status == storage.DocumentReversed:
		return "withdrawn"
	}
	return "recorded"
}

func (a *API) filingOf(ctx context.Context, u storage.UnitOfWork, doc storage.Document) (StatutoryFilingView, error) {
	f, err := deadlines.ReadFiling(doc)
	if err != nil {
		return StatutoryFilingView{}, err
	}
	out := StatutoryFilingView{ID: doc.ID.String(), Status: filingStatus(doc, f), Version: doc.Version, Key: f.Key, Outcome: f.Outcome,
		FiledOn: dateOut(f.FiledOn), Reference: f.Reference, Reason: f.Reason, Note: f.Note}
	if f.Withdraws != nil {
		out.Withdraws = f.Withdraws.String()
	}
	out.Evidence, _, out.RecordedBy, out.RecordedAt, out.Checks, err = a.recordCommon(ctx, u, doc, nil)
	if err != nil {
		return out, err
	}
	out.Checks = []BillCheck{}
	if doc.Status == storage.DocumentDraft && f.Outcome == deadlines.OutcomeFiled && out.Evidence == 0 {
		level, err := settingToday(ctx, a, u, evidence.StatutoryFiling.Setting)
		if err != nil {
			return out, err
		}
		if blocks, _ := evidence.Needs(level, 0); blocks {
			out.Checks = append(out.Checks, BillCheck{Severity: "error", Code: evidence.CodeMissing, Field: "evidence",
				Message: "it has no acknowledgement attached, so it cannot be recorded as filed", Fix: "Attach the submission or payment acknowledgement"})
		}
	}
	return out, nil
}

// shownFiling is a record by ID, as the caller reads it: not found for anything else.
func (a *API) shownFiling(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (StatutoryFilingView, error) {
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return StatutoryFilingView{}, err
	}
	if doc.Type != storage.DocumentStatutoryFiling {
		return StatutoryFilingView{}, ports.ErrNotFound
	}
	return a.filingOf(ctx, u, doc)
}

func filingETag(v StatutoryFilingView) string {
	status := "posted"
	switch v.Status {
	case "draft":
		status = "draft"
	case "withdrawn":
		status = "reversed"
	}
	return etag(v.Version, status)
}

func filingAnswer(w written[StatutoryFilingView]) *filingWriteOutput {
	if w.Previewed {
		return &filingWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}
	}
	return &filingWriteOutput{Status: w.Status, ETag: filingETag(w.Body), Location: "/v1/compliance/filings/" + w.Body.ID,
		Replayed: replayedHeader(w.Replayed), Body: w.Body}
}

// itemOut is an item as the API shows it.
func (a *API) itemOut(ctx context.Context, u storage.UnitOfWork, it deadlines.Item) (StatutoryItem, error) {
	out := StatutoryItem{Key: it.Key, Obligation: it.Obligation, Area: it.Area, Title: it.Title, Why: it.Why, Source: it.Source,
		Extended: it.Extended, Rolled: it.Rolled, Status: string(it.Status), Reason: it.Reason, Register: it.Register, Link: it.Link}
	if !it.Anchor.IsZero() {
		out.Anchor = it.Anchor.Format(time.DateOnly)
	}
	if !it.Due.IsZero() {
		out.DueOn = it.Due.Format(time.DateOnly)
	}
	if it.Filing != nil {
		v, err := a.shownFiling(ctx, u, it.Filing.DocumentID)
		if err != nil {
			return out, err
		}
		out.Filing = &v
	}
	return out, nil
}

func registerCalendar(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid}, locks...)

	register(a, authz.GetStatutoryCalendar, huma.Operation{
		OperationID: "getStatutoryCalendar", Method: http.MethodGet, Path: "/v1/compliance/calendar", Tags: []string{"Compliance"},
		Summary: "Get the statutory calendar",
		Description: "Every filing and payment deadline due in a window of at most 18 months, with each earlier one still overdue (FR-TAX-07, " +
			"CMP-02, design D5): GST F5 for each return period, CPF and IR8A/AIS while the organisation pays staff, ECI, Form C-S/C, the " +
			"annual return and the AGM or the financial statements to members for each financial year, and applying to register for GST " +
			"while the monitor finds a test met. Each day comes from the sg-deadlines pack, with the rule's words and sources, from the " +
			"organisation's years, registrations and settings. An item whose input is missing is not assessed and says which. Read-only; " +
			"no side effects.",
	}, append([]string{deadlines.CodeWindow, CodeInvalid}, locks...), func(ctx context.Context, in *calendarInput) (*calendarOutput, error) {
		p, _ := authn.From(ctx)
		var out calendarOutput
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return err
			}
			var fields []FieldProblem
			from := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)
			if d := dayIn("query.from", in.From, &fields); d != nil {
				from = *d
			}
			to := from.AddDate(1, 0, -1)
			if d := dayIn("query.to", in.To, &fields); d != nil {
				to = *d
			}
			if len(fields) > 0 {
				return invalid(fields)
			}
			c, err := deadlines.Read(ctx, u, from, to, today)
			if errors.Is(err, deadlines.ErrWindow) {
				return problem(deadlines.CodeWindow, "The window ends before it starts, or is longer than 18 months.", "Ask for at most 18 months from from.")
			}
			if err != nil {
				return err
			}
			out.Body = StatutoryCalendarView{From: c.From.Format(time.DateOnly), To: c.To.Format(time.DateOnly), Today: c.Today.Format(time.DateOnly),
				Pack: c.Pack, Deadlines: []StatutoryItem{}}
			if !c.Starts.IsZero() {
				out.Body.Starts = c.Starts.Format(time.DateOnly)
			}
			for _, it := range c.Items {
				v, err := a.itemOut(ctx, u, it)
				if err != nil {
					return err
				}
				out.Body.Deadlines = append(out.Body.Deadlines, v)
			}
			return nil
		})
		return &out, err
	})

	register(a, authz.ListStatutoryFilings, huma.Operation{
		OperationID: "listStatutoryFilings", Method: http.MethodGet, Path: "/v1/compliance/filings", Tags: []string{"Compliance"},
		Summary:     "List the filing records",
		Description: "The records of the calendar's items, drafts, recorded, withdrawn and withdrawals, the latest first, a page at a time, or one item's history by its key (FR-TAX-07). Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *filingListInput) (*filingListOutput, error) {
		p, _ := authn.From(ctx)
		out := filingListOutput{Body: StatutoryFilingPage{Items: []StatutoryFilingView{}}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			var all []StatutoryFilingView
			for _, status := range []storage.DocumentStatus{storage.DocumentDraft, storage.DocumentPosted, storage.DocumentReversed} {
				f := storage.DocumentFilter{Type: storage.DocumentStatutoryFiling, Status: status, Limit: 200}
				for {
					page, err := u.Documents().Page(ctx, f)
					if err != nil {
						return err
					}
					for _, doc := range page {
						v, err := a.filingOf(ctx, u, doc)
						if err != nil {
							return err
						}
						if in.Key == "" || v.Key == strings.TrimSpace(in.Key) {
							all = append(all, v)
						}
					}
					if len(page) < f.Limit {
						break
					}
					last := page[len(page)-1]
					f.AfterDate, f.AfterID = last.Date, &last.ID
				}
			}
			var err error
			out.Body.Items, out.Body.NextCursor, err = pageOf(all, func(x StatutoryFilingView) string { return x.ID }, in.PageQuery)
			if out.Body.Items == nil {
				out.Body.Items = []StatutoryFilingView{}
			}
			return err
		})
		return &out, err
	})

	register(a, authz.CreateStatutoryFiling, huma.Operation{
		OperationID: "createStatutoryFiling", Method: http.MethodPost, Path: "/v1/compliance/filings", Tags: []string{"Compliance"},
		DefaultStatus: http.StatusCreated, Summary: "Draft a filing record",
		Description: "Drafts what was done about an item of the calendar, by its key (FR-TAX-07, design D6): filed on a day with its " +
			"reference, or not required with the reason, such as the ECI waiver. Fyniq files nothing itself. For filed, attach the " +
			"acknowledgement with addStatutoryFilingEvidence (the evidence rule evidence.statutory_filing, FR-DOC-06), then record it " +
			"with recordStatutoryFiling. Side effects: the draft, audit events. Every problem comes back together. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{}, writes...), CalendarCodes...), func(ctx context.Context, in *filingDraftInput) (*filingWriteOutput, error) {
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (StatutoryFilingView, error) {
			f, err := filingIn(in.Body)
			if err != nil {
				return StatutoryFilingView{}, err
			}
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return StatutoryFilingView{}, err
			}
			doc, err := deadlines.DraftIn(ctx, u, f, today)
			if err != nil {
				return StatutoryFilingView{}, equityProblems(err)
			}
			return a.shownFiling(ctx, u, doc.ID)
		})
		if err != nil {
			return nil, err
		}
		return filingAnswer(w), nil
	})

	register(a, authz.GetStatutoryFiling, huma.Operation{
		OperationID: "getStatutoryFiling", Method: http.MethodGet, Path: "/v1/compliance/filings/{filing_id}", Tags: []string{"Compliance"},
		Summary:     "Get a filing record",
		Description: "A record, drafted, recorded, withdrawn, or a withdrawal, with its files and who recorded it (FR-TAX-07). Read-only; no side effects.",
	}, append([]string{CodeNotFound}, locks...), func(ctx context.Context, in *StatutoryFilingPath) (*filingReadOutput, error) {
		id, err := parseID("filing_id", in.FilingID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out filingReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			v, err := a.shownFiling(ctx, u, id)
			out.Body, out.ETag = v, filingETag(v)
			return err
		})
		return &out, err
	})

	register(a, authz.ReplaceStatutoryFiling, huma.Operation{
		OperationID: "replaceStatutoryFiling", Method: http.MethodPut, Path: "/v1/compliance/filings/{filing_id}", Tags: []string{"Compliance"},
		Summary: "Change a drafted filing record",
		Description: "Saves a new version of a record not recorded yet (FR-TAX-07); a recorded one changes no more: withdraw it instead. Side " +
			"effects: the new version, audit events. Needs If-Match with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired}, writes...), CalendarCodes...),
		func(ctx context.Context, in *filingReviseInput) (*filingWriteOutput, error) {
			id, err := parseID("filing_id", in.FilingID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (StatutoryFilingView, error) {
				doc, err := deadlines.Drafted(ctx, u, id)
				if err != nil {
					return StatutoryFilingView{}, err
				}
				if err := checkIfMatch(in.IfMatch, recordETag(doc)); err != nil {
					return StatutoryFilingView{}, err
				}
				f, err := filingIn(in.Body)
				if err != nil {
					return StatutoryFilingView{}, err
				}
				today, err := orgToday(ctx, a, u)
				if err != nil {
					return StatutoryFilingView{}, err
				}
				if _, err := deadlines.ReviseIn(ctx, u, id, f, today); err != nil {
					return StatutoryFilingView{}, equityProblems(err)
				}
				return a.shownFiling(ctx, u, id)
			})
			if err != nil {
				return nil, err
			}
			return filingAnswer(w), nil
		})

	register(a, authz.RecordStatutoryFiling, huma.Operation{
		OperationID: "recordStatutoryFiling", Method: http.MethodPost, Path: "/v1/compliance/filings/{filing_id}/record", Tags: []string{"Compliance"},
		Summary: "Record a filing record",
		Description: "Records a drafted record (FR-TAX-07, design D6): from then on its item is filed or not required, and leaves Checks and " +
			"the reminders. A filed record needs its acknowledgement attached while the evidence rule requires it (DOC-10). An item has at " +
			"most one live record (CAL-02; the database refuses a second, FY299). It posts nothing. Side effects: the record, its files " +
			"committed, audit events. Needs If-Match with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired}, writes...), CalendarCodes...),
		func(ctx context.Context, in *filingRecordInput) (*filingWriteOutput, error) {
			id, err := parseID("filing_id", in.FilingID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (StatutoryFilingView, error) {
				doc, err := deadlines.Drafted(ctx, u, id)
				if err != nil {
					return StatutoryFilingView{}, err
				}
				if err := checkIfMatch(in.IfMatch, recordETag(doc)); err != nil {
					return StatutoryFilingView{}, err
				}
				f, err := deadlines.ReadFiling(doc)
				if err != nil {
					return StatutoryFilingView{}, err
				}
				if f.Outcome == deadlines.OutcomeFiled {
					if err := a.requireEvidence(ctx, u, evidence.StatutoryFiling, id); err != nil {
						return StatutoryFilingView{}, err
					}
				}
				today, err := orgToday(ctx, a, u)
				if err != nil {
					return StatutoryFilingView{}, err
				}
				if _, err := deadlines.RecordIn(ctx, u, id, today); err != nil {
					return StatutoryFilingView{}, equityProblems(err)
				}
				if a.deps.Outbox != nil { // its evidence committed now, not at the next sweep (FR-DOC-13)
					p, _ := authn.From(ctx)
					if _, err := a.deps.Outbox.Enqueue(ctx, u, p.Organisation, storage.NewOutboxItem{Class: storage.ClassWorker,
						Kind: EvidenceCommitKind, Payload: []byte(`{}`)}); err != nil {
						return StatutoryFilingView{}, err
					}
				}
				return a.shownFiling(ctx, u, id)
			})
			if err != nil {
				return nil, err
			}
			return filingAnswer(w), nil
		})

	register(a, authz.WithdrawStatutoryFiling, huma.Operation{
		OperationID: "withdrawStatutoryFiling", Method: http.MethodPost, Path: "/v1/compliance/filings/{filing_id}/withdraw", Tags: []string{"Compliance"},
		Summary: "Withdraw a filing record",
		Description: "Takes back a live record, with the reason (FR-TAX-07, design D6): it is withdrawn and a withdrawal recorded beside it, " +
			"so its item is open again and both stay in its history. Side effects: the withdrawal, audit events. Needs If-Match with the " +
			"record's ETag. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired}, writes...), CalendarCodes...),
		func(ctx context.Context, in *filingWithdrawInput) (*filingWriteOutput, error) {
			id, err := parseID("filing_id", in.FilingID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (StatutoryFilingView, error) {
				cur, err := a.shownFiling(ctx, u, id)
				if err != nil {
					return StatutoryFilingView{}, err
				}
				if err := checkIfMatch(in.IfMatch, filingETag(cur)); err != nil {
					return StatutoryFilingView{}, err
				}
				today, err := orgToday(ctx, a, u)
				if err != nil {
					return StatutoryFilingView{}, err
				}
				doc, err := deadlines.WithdrawIn(ctx, u, id, in.Body.Reason, today)
				if err != nil {
					return StatutoryFilingView{}, equityProblems(err)
				}
				return a.shownFiling(ctx, u, doc.ID)
			})
			if err != nil {
				return nil, err
			}
			return filingAnswer(w), nil
		})

	addCodes := append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused,
		CodeIdempotencyBusy, CodeIdempotencyOld, CodeUnavailable, equity.CodeRecorded}, append(locks, EvidenceCodes...)...)
	removeCodes := append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, storage.CodeEvidence, storage.CodeVersion, equity.CodeRecorded}, locks...)
	register(a, authz.AddStatutoryFilingEvidence, huma.Operation{
		OperationID: "addStatutoryFilingEvidence", Method: http.MethodPost, Path: "/v1/compliance/filings/{filing_id}/evidence", Tags: []string{"Evidence"},
		DefaultStatus: http.StatusCreated, Summary: "Attach a filing's acknowledgement",
		Description: "Attaches a file to a drafted filing record (FR-TAX-07, FR-DOC-05), as multipart/form-data: IRAS's, CPF Board's or " +
			"ACRA's acknowledgement, or an AGM's minutes. The file's type is decided from its content; it is scanned for malware and its " +
			"image metadata is stripped before it is stored (FR-DOC-08). Side effects: the file in the staging store, its evidence row and " +
			"its audit event; once recorded its files are committed to the retention-locked store (FR-DOC-13). Needs If-Match with the " +
			"record's ETag. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, addCodes, func(ctx context.Context, in *addFilingEvidenceInput) (*equityEvidenceWriteOutput, error) {
		return a.attachEquityEvidence(ctx, storage.DocumentStatutoryFiling, "/v1/compliance/filings", in.FilingID, in.IdempotencyKey, in.IfMatch, in.Prefer, in.RawBody.Data())
	})
	register(a, authz.ListStatutoryFilingEvidence, huma.Operation{
		OperationID: "listStatutoryFilingEvidence", Method: http.MethodGet, Path: "/v1/compliance/filings/{filing_id}/evidence", Tags: []string{"Evidence"},
		Summary:     "List a filing record's files",
		Description: "The files attached to a filing record, oldest first, removed ones included, a page at a time (FR-DOC-05). Read-only; no side effects.",
	}, []string{CodeNotFound, CodeCursor}, func(ctx context.Context, in *listFilingEvidenceInput) (*equityEvidenceListOutput, error) {
		return a.listEquityEvidence(ctx, storage.DocumentStatutoryFiling, "filing_id", in.FilingID, in.PageQuery)
	})
	register(a, authz.GetStatutoryFilingEvidence, huma.Operation{
		OperationID: "getStatutoryFilingEvidence", Method: http.MethodGet, Path: "/v1/compliance/filings/{filing_id}/evidence/{evidence_id}", Tags: []string{"Evidence"},
		Summary:     "Get a filing record's file",
		Description: "Returns a file's details, hashes, state and retain-until, with its ETag (FR-DOC-05, FR-DOC-07). Read-only; no side effects.",
	}, []string{CodeNotFound}, func(ctx context.Context, in *StatutoryFilingEvidencePath) (*equityEvidenceReadOutput, error) {
		return a.getEquityEvidence(ctx, storage.DocumentStatutoryFiling, "filing_id", in.FilingID, in.EvidenceID)
	})
	register(a, authz.RemoveStatutoryFilingEvidence, huma.Operation{
		OperationID: "removeStatutoryFilingEvidence", Method: http.MethodDelete, Path: "/v1/compliance/filings/{filing_id}/evidence/{evidence_id}", Tags: []string{"Evidence"},
		Summary: "Remove a file from a drafted filing record",
		Description: "Takes a file off a filing record not recorded yet (FR-DOC-07). Once recorded its evidence is append-only, and the database " +
			"refuses. The row stays, marked removed, for the audit trail. Side effects: the removal and its audit event. Needs If-Match " +
			"with the file's ETag; Prefer: return=preview saves nothing.",
	}, removeCodes, func(ctx context.Context, in *removeFilingEvidenceInput) (*equityEvidenceWriteOutput, error) {
		return a.removeEquityEvidence(ctx, storage.DocumentStatutoryFiling, "filing_id", in.FilingID, in.EvidenceID, in.IfMatch, in.Prefer, in)
	})
	if op := a.OpenAPI().Paths["/v1/compliance/filings/{filing_id}/evidence"].Post; op != nil && op.RequestBody != nil {
		if mt := op.RequestBody.Content["multipart/form-data"]; mt != nil && mt.Schema != nil && mt.Schema.Properties["file"] != nil {
			mt.Schema.Properties["file"].Examples = []any{"%PDF-1.7 … (the file's bytes)"}
		}
	}
}
