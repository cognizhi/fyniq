package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/auditsample"
	"github.com/cognizhi/fyniq/internal/domain/team"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The Auditor's engagement (PRD-001 FR-UX-42, the owner's decision of 23 Nov 2026; p2-reporting-planning design D13). An Owner opens a time-boxed engagement naming the auditor (a member
// holding the Auditor role) and a second Owner confirms it (the same Owner in Founder mode, as a publication is, FM-3). While it is active, and between its two dates, the auditor asks for items
// (each answered by a named person with evidence), draws samples from posted ledger lines by a rule, and accepts answers; the auditor writes nothing else. The ledger is only read.

// Stable codes of the engagement (ENG-23).
const (
	CodeAuditWindow  = "AUDENG-01" // the engagement is not active, or today is outside its dates
	CodeAuditAuditor = "AUDENG-02" // only the engagement's own auditor asks, draws and accepts
	CodeAuditAnswer  = "AUDENG-03" // only the person the request names, or an Owner, answers it
	CodeAuditConfirm = "AUDENG-04" // in Team mode a second Owner confirms an engagement
	CodeAuditOpen    = "AUDENG-05" // the engagement's auditor or assignee is not a member with the right role
)

// MaxSampleScan is how many qualifying lines a sample's rule may meet before it is asked to be narrowed.
const maxSampleScan = 500000

// EvidenceFileID is an evidence file's ID, as getEvidence gives it.
type EvidenceFileID string

// Schema implements huma.SchemaProvider: a UUID string, with an example.
func (EvidenceFileID) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Format: "uuid", Description: "An evidence file's ID.", Examples: []any{"01928f6e-7a3b-7c41-9d2e-5f8a1b3c9b12"}}
}

// AuditEngagement is an engagement as an Owner opened it.
type AuditEngagement struct {
	ID          string       `json:"id" format:"uuid" doc:"The engagement's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c9b10"`
	Name        string       `json:"name" doc:"What it is called." example:"FY2026 statutory audit"`
	Firm        string       `json:"firm,omitempty" doc:"The auditing firm." example:"Example & Co LLP"`
	AuditorID   MemberUserID `json:"auditor_user_id" doc:"The member, holding the Auditor role, who asks and draws samples."`
	PeriodFrom  string       `json:"period_from" format:"date" doc:"The first day of the period under audit." example:"2026-01-01"`
	PeriodTo    string       `json:"period_to" format:"date" doc:"The last day of the period under audit." example:"2026-12-31"`
	OpensOn     string       `json:"opens_on" format:"date" doc:"The first day requests and samples may be added." example:"2027-01-04"`
	ClosesOn    string       `json:"closes_on" format:"date" doc:"The last day they may be added; at most 366 days after it opens." example:"2027-03-31"`
	Status      string       `json:"status" enum:"pending,active,closed" doc:"pending until a second Owner confirms it; active while requests and samples may be added; closed." example:"active"`
	OpenedAt    time.Time    `json:"opened_at" doc:"When an Owner opened it." example:"2026-12-01T03:00:00Z"`
	ConfirmedAt *time.Time   `json:"confirmed_at,omitempty" doc:"When it was confirmed." example:"2026-12-01T04:00:00Z"`
	ClosedAt    *time.Time   `json:"closed_at,omitempty" doc:"When it was closed." example:"2027-03-31T04:00:00Z"`
}

// AuditRequestBody is an item the auditor asked for.
type AuditRequestBody struct {
	ID          string           `json:"id" format:"uuid" doc:"The request's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c9b11"`
	Engagement  string           `json:"engagement_id" format:"uuid" doc:"Its engagement." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c9b10"`
	Number      int              `json:"number" doc:"Its number within the engagement." example:"1"`
	Title       string           `json:"title" doc:"What is asked for." example:"Bank confirmation at 31 December"`
	Detail      string           `json:"detail,omitempty" doc:"More about it." example:"The statement of the main account"`
	DueOn       string           `json:"due_on,omitempty" format:"date" doc:"When the auditor wants it." example:"2027-01-20"`
	Assignee    MemberUserID     `json:"assignee_user_id" doc:"The member who answers it."`
	Status      string           `json:"status" enum:"open,answered,closed" doc:"open until answered; answered until the auditor accepts it; closed." example:"open"`
	RequestedAt time.Time        `json:"requested_at" doc:"When it was asked." example:"2027-01-05T03:00:00Z"`
	Answer      string           `json:"answer,omitempty" doc:"The answer." example:"Attached"`
	EvidenceIDs []EvidenceFileID `json:"evidence_ids" nullable:"false" doc:"The evidence files attached to the answer."`
	AnsweredAt  *time.Time       `json:"answered_at,omitempty" doc:"When it was answered." example:"2027-01-10T03:00:00Z"`
	ClosedAt    *time.Time       `json:"closed_at,omitempty" doc:"When the auditor accepted it." example:"2027-01-12T03:00:00Z"`
}

// AuditSampleLine is a line a sample holds, with where to open its source read-only.
type AuditSampleLine struct {
	EntryID     string `json:"entry_id" format:"uuid" doc:"The journal entry." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c9b13"`
	LineNo      int    `json:"line_no" doc:"The line within it." example:"1"`
	PostingDate string `json:"posting_date" format:"date" doc:"The posting date." example:"2026-06-15"`
	Account     string `json:"account" doc:"The account's code." example:"6120"`
	AccountName string `json:"account_name" doc:"The account's name." example:"Software"`
	BaseAmount  string `json:"base_amount" doc:"The line in the base currency, a decimal string." example:"-1500.00"`
	Memo        string `json:"memo,omitempty" doc:"The entry's memo." example:"Hosting"`
	SourceType  string `json:"source_type" doc:"The kind of document it posted from, which opens read-only." example:"bill"`
	SourceID    string `json:"source_id,omitempty" doc:"That document's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c9b14"`
}

// AuditSampleRule is the auditor's rule.
type AuditSampleRule struct {
	From      string `json:"from" format:"date" doc:"The first posting date." example:"2026-01-01"`
	To        string `json:"to" format:"date" doc:"The last posting date." example:"2026-12-31"`
	Account   string `json:"account,omitempty" doc:"An account's code; left out, any account." example:"6120"`
	MinAmount string `json:"min_amount,omitempty" doc:"The smallest line, as an absolute decimal amount in the base currency." example:"500.00"`
	Count     int    `json:"count" minimum:"1" maximum:"200" doc:"How many lines." example:"25"`
}

// AuditSampleBody is a drawn sample.
type AuditSampleBody struct {
	ID         string            `json:"id" format:"uuid" doc:"The sample's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c9b15"`
	Engagement string            `json:"engagement_id" format:"uuid" doc:"Its engagement." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c9b10"`
	Rule       AuditSampleRule   `json:"rule" doc:"The rule it was drawn by."`
	Seed       string            `json:"seed" doc:"The seed: the same seed and rule over the same ledger draw the same lines." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c9b16"`
	Population int               `json:"population" doc:"How many lines the rule qualified." example:"312"`
	Items      []AuditSampleLine `json:"items" nullable:"false" doc:"The sampled lines, in the order the seed ranks them."`
	DrawnAt    time.Time         `json:"drawn_at" doc:"When it was drawn." example:"2027-01-06T03:00:00Z"`
}

func engDayStr(t time.Time) string { return t.UTC().Format("2006-01-02") }

func engagementOf(e storage.AuditEngagement) AuditEngagement {
	return AuditEngagement{ID: e.ID.String(), Name: e.Name, Firm: e.Firm, AuditorID: MemberUserID(e.AuditorUser.String()), PeriodFrom: engDayStr(e.PeriodFrom), PeriodTo: engDayStr(e.PeriodTo), OpensOn: engDayStr(e.OpensOn),
		ClosesOn: engDayStr(e.ClosesOn), Status: e.Status, OpenedAt: e.OpenedAt.UTC(), ConfirmedAt: e.ConfirmedAt, ClosedAt: e.ClosedAt}
}

func auditRequestOf(r storage.AuditRequest) AuditRequestBody {
	out := AuditRequestBody{ID: r.ID.String(), Engagement: r.Engagement.String(), Number: r.Number, Title: r.Title, Detail: r.Detail, Assignee: MemberUserID(r.Assignee.String()), Status: r.Status,
		RequestedAt: r.RequestedAt.UTC(), Answer: r.Answer, EvidenceIDs: []EvidenceFileID{}, AnsweredAt: r.AnsweredAt, ClosedAt: r.ClosedAt}
	if r.DueOn != nil {
		out.DueOn = engDayStr(*r.DueOn)
	}
	for _, id := range r.EvidenceIDs {
		out.EvidenceIDs = append(out.EvidenceIDs, EvidenceFileID(id.String()))
	}
	return out
}

func auditSampleOf(s storage.AuditSample) AuditSampleBody {
	out := AuditSampleBody{ID: s.ID.String(), Engagement: s.Engagement.String(), Seed: s.Seed, Population: s.Population, Items: []AuditSampleLine{}, DrawnAt: s.DrawnAt.UTC()}
	var r auditsample.Rule
	_ = json.Unmarshal(s.Rule, &r)
	out.Rule = AuditSampleRule{From: r.From, To: r.To, Account: r.Account, MinAmount: r.MinAmount, Count: r.Count}
	var items []auditsample.Line
	_ = json.Unmarshal(s.Items, &items)
	for _, l := range items {
		out.Items = append(out.Items, AuditSampleLine{EntryID: l.EntryID, LineNo: l.LineNo, PostingDate: l.PostingDate, Account: l.Account, AccountName: l.AccountName, BaseAmount: l.BaseAmount,
			Memo: l.Memo, SourceType: l.SourceType, SourceID: l.SourceID})
	}
	return out
}

// AuditEngagementPath names an engagement in the path.
type AuditEngagementPath struct {
	ID string `path:"id" format:"uuid" doc:"The engagement's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c9b10"`
}

// AuditRequestPath names a request in the path.
type AuditRequestPath struct {
	ID string `path:"id" format:"uuid" doc:"The request's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c9b11"`
}

type auditEngagementOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     AuditEngagement
}

type auditRequestOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     AuditRequestBody
}

type auditSampleOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     AuditSampleBody
}

type openAuditEngagementInput struct {
	KeyHeader
	PreferHeader
	Body struct {
		Name       string       `json:"name" minLength:"2" maxLength:"120" doc:"What the engagement is called." example:"FY2026 statutory audit"`
		Firm       string       `json:"firm,omitempty" maxLength:"120" doc:"The auditing firm." example:"Example & Co LLP"`
		AuditorID  MemberUserID `json:"auditor_user_id" doc:"A member holding the Auditor role."`
		PeriodFrom string       `json:"period_from" format:"date" doc:"The first day of the period under audit." example:"2026-01-01"`
		PeriodTo   string       `json:"period_to" format:"date" doc:"The last day of the period under audit." example:"2026-12-31"`
		OpensOn    string       `json:"opens_on" format:"date" doc:"The first day requests and samples may be added." example:"2027-01-04"`
		ClosesOn   string       `json:"closes_on" format:"date" doc:"The last day; at most 366 days after it opens." example:"2027-03-31"`
	}
}

type auditEngagementActionInput struct {
	AuditEngagementPath
	KeyHeader
	PreferHeader
}

type askAuditRequestInput struct {
	AuditEngagementPath
	KeyHeader
	PreferHeader
	Body struct {
		Title    string       `json:"title" minLength:"2" maxLength:"200" doc:"What is asked for." example:"Bank confirmation at 31 December"`
		Detail   string       `json:"detail,omitempty" maxLength:"2000" doc:"More about it." example:"The statement of the main account"`
		DueOn    string       `json:"due_on,omitempty" format:"date" doc:"When it is wanted." example:"2027-01-20"`
		Assignee MemberUserID `json:"assignee_user_id" doc:"The member who answers it: an Owner, Controller or Accountant."`
	}
}

type answerAuditRequestInput struct {
	AuditRequestPath
	KeyHeader
	PreferHeader
	Body struct {
		Answer      string           `json:"answer" minLength:"1" maxLength:"4000" doc:"The answer." example:"Attached"`
		EvidenceIDs []EvidenceFileID `json:"evidence_ids,omitempty" maxItems:"20" doc:"Evidence files already added in the organisation, as getEvidence gives their IDs."`
	}
}

type auditRequestActionInput struct {
	AuditRequestPath
	KeyHeader
	PreferHeader
}

type drawAuditSampleInput struct {
	AuditEngagementPath
	KeyHeader
	PreferHeader
	Body struct {
		Rule AuditSampleRule `json:"rule" doc:"The rule: a period inside the engagement's, an account, a smallest line and a count."`
		Seed string          `json:"seed,omitempty" maxLength:"64" doc:"A seed to re-draw an earlier sample; left out, Fyniq makes one and keeps it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c9b16"`
	}
}

type auditListInput struct {
	AuditEngagementPath
	PageQuery
}

func engDay(field, s string) (time.Time, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return t, invalid([]FieldProblem{{Code: CodeInvalid, Field: field, Message: "is not a date", Fix: "Send a date as YYYY-MM-DD"}})
	}
	return t, nil
}

// engagementFor reads an engagement, as a Not found problem when there is none.
func engagementFor(ctx context.Context, u storage.UnitOfWork, id string) (storage.AuditEngagement, error) {
	eid, err := parseID("id", id)
	if err != nil {
		return storage.AuditEngagement{}, err
	}
	e, err := u.AuditEngagements().Get(ctx, eid)
	if errors.Is(err, ports.ErrNotFound) {
		return e, problem(CodeNotFound, "No engagement with that ID exists.", "List the engagements")
	}
	return e, err
}

// inWindow is why an engagement takes no requests or samples today, or nil.
func (a *API) inWindow(e storage.AuditEngagement) error {
	today, _ := time.Parse("2006-01-02", a.deps.Clock.Now().UTC().Format("2006-01-02"))
	switch {
	case e.Status != storage.EngagementActive:
		return problem(CodeAuditWindow, "The engagement is "+e.Status+", so it takes no requests or samples.", "An Owner confirms a pending engagement; a closed one is not reopened: open a new one")
	case today.Before(e.OpensOn) || today.After(e.ClosesOn):
		return problem(CodeAuditWindow, "Today is outside the engagement's dates ("+engDayStr(e.OpensOn)+" to "+engDayStr(e.ClosesOn)+").", "Requests and samples are added between those dates; an Owner opens a new engagement to go on")
	}
	return nil
}

func (a *API) onlyTheAuditor(ctx context.Context, e storage.AuditEngagement) error {
	if p, _ := authn.From(ctx); p.Class != authn.ClassUser || p.UserID != e.AuditorUser {
		return problem(CodeAuditAuditor, "Only the engagement's own auditor does that.", "Ask the auditor named on the engagement")
	}
	return nil
}

func (a *API) memberHolds(ctx context.Context, u storage.UnitOfWork, user uuid.UUID, roles ...authz.Role) (bool, error) {
	rs, err := u.Members().Roles(ctx, user, a.deps.Clock.Now())
	if err != nil {
		return false, err
	}
	for _, r := range rs {
		if slices.Contains(roles, authz.Role(r.Role)) {
			return true, nil
		}
	}
	return false, nil
}

func registerAuditEngagements(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeNotFound, storage.CodeAuditEngagement, storage.CodeDuplicate}, locks...)

	register(a, authz.ListAuditEngagements, huma.Operation{
		OperationID: "listAuditEngagements", Method: http.MethodGet, Path: "/v1/audit-engagements", Tags: []string{"Audit"},
		Summary:     "List the auditor's engagements",
		Description: "The engagements an Owner opened for an auditor (FR-UX-42), newest first, a page at a time. Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *struct{ PageQuery }) (*struct {
		Body struct {
			Items      []AuditEngagement `json:"items" nullable:"false" doc:"The engagements, newest first."`
			NextCursor string            `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last." example:"eyJhZnRlciI6IjEifQ"`
		}
	}, error) {
		p, _ := authn.From(ctx)
		var all []storage.AuditEngagement
		if err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			all, err = u.AuditEngagements().List(ctx)
			return err
		}); err != nil {
			return nil, err
		}
		page, next, err := pageOf(all, func(e storage.AuditEngagement) string { return e.ID.String() }, in.PageQuery)
		if err != nil {
			return nil, err
		}
		out := &struct {
			Body struct {
				Items      []AuditEngagement `json:"items" nullable:"false" doc:"The engagements, newest first."`
				NextCursor string            `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last." example:"eyJhZnRlciI6IjEifQ"`
			}
		}{}
		out.Body.Items, out.Body.NextCursor = []AuditEngagement{}, next
		for _, e := range page {
			out.Body.Items = append(out.Body.Items, engagementOf(e))
		}
		return out, nil
	})

	register(a, authz.OpenAuditEngagement, huma.Operation{
		OperationID: "openAuditEngagement", Method: http.MethodPost, Path: "/v1/audit-engagements", Tags: []string{"Audit"}, DefaultStatus: http.StatusCreated,
		Summary: "Open an engagement for an auditor",
		Description: "An Owner opens a time-boxed engagement for a member holding the Auditor role (FR-UX-42): the period under audit and the dates between which the auditor may add requests and draw samples, at most 366 days. " +
			"It is pending until a second Owner confirms it with confirmAuditEngagement (the same Owner in Founder mode). Side effects: the engagement and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append([]string{CodeAuditOpen}, writes...), func(ctx context.Context, in *openAuditEngagementInput) (*auditEngagementOutput, error) {
		b := in.Body
		auditor, err := uuid.Parse(string(b.AuditorID))
		if err != nil {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.auditor_user_id", Message: "is not a member's user ID", Fix: "Send the user ID listMembers gives"}})
		}
		var d [4]time.Time
		for i, f := range [][2]string{{"body.period_from", b.PeriodFrom}, {"body.period_to", b.PeriodTo}, {"body.opens_on", b.OpensOn}, {"body.closes_on", b.ClosesOn}} {
			if d[i], err = engDay(f[0], f[1]); err != nil {
				return nil, err
			}
		}
		today, _ := time.Parse("2006-01-02", a.deps.Clock.Now().UTC().Format("2006-01-02"))
		switch {
		case d[0].After(d[1]):
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.period_to", Message: "is before the period's start", Fix: "Send the period's last day on or after its first"}})
		case d[2].After(d[3]) || d[3].Sub(d[2]) > 366*24*time.Hour:
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.closes_on", Message: "is before the engagement opens, or more than 366 days after", Fix: "Send closing dates up to 366 days after opens_on"}})
		case d[3].Before(today):
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.closes_on", Message: "is already past", Fix: "Send a closing date that has not passed"}})
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (AuditEngagement, error) {
			if ok, err := a.memberHolds(ctx, u, auditor, authz.Auditor); err != nil {
				return AuditEngagement{}, err
			} else if !ok {
				return AuditEngagement{}, problem(CodeAuditOpen, "That member does not hold the Auditor role.", "Invite the auditor with the Auditor role first, then open the engagement")
			}
			e, err := u.AuditEngagements().Open(ctx, storage.AuditEngagement{Name: strings.TrimSpace(b.Name), Firm: strings.TrimSpace(b.Firm), AuditorUser: auditor, PeriodFrom: d[0], PeriodTo: d[1],
				OpensOn: d[2], ClosesOn: d[3], OpenedBy: uploader(ctx), OpenedAt: a.deps.Clock.Now()})
			return engagementOf(e), err
		})
		if err != nil {
			return nil, err
		}
		return &auditEngagementOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})

	register(a, authz.ConfirmAuditEngagement, huma.Operation{
		OperationID: "confirmAuditEngagement", Method: http.MethodPost, Path: "/v1/audit-engagements/{id}/confirm", Tags: []string{"Audit"},
		Summary: "Confirm a pending engagement",
		Description: "An Owner other than the one who opened it confirms the engagement, and it becomes active (FR-UX-42; AUDENG-04). In Founder mode the same Owner may confirm it. Side effects: the status and its audit event. " +
			"Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append([]string{CodeAuditConfirm}, writes...), func(ctx context.Context, in *auditEngagementActionInput) (*auditEngagementOutput, error) {
		p, _ := authn.From(ctx)
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (AuditEngagement, error) {
			e, err := engagementFor(ctx, u, in.ID)
			if err != nil {
				return AuditEngagement{}, err
			}
			mode, _, _, err := teamModeOf(ctx, u)
			if err != nil {
				return AuditEngagement{}, err
			}
			if mode != team.Founder && e.OpenedBy == uploader(ctx) {
				return AuditEngagement{}, problem(CodeAuditConfirm, "In Team mode another Owner confirms an engagement you opened.", "Ask another Owner to confirm it")
			}
			got, err := u.AuditEngagements().Confirm(ctx, e.ID, uploader(ctx), a.deps.Clock.Now())
			_ = p
			return engagementOf(got), err
		})
		if err != nil {
			return nil, err
		}
		return &auditEngagementOutput{Status: http.StatusOK, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})

	register(a, authz.CloseAuditEngagement, huma.Operation{
		OperationID: "closeAuditEngagement", Method: http.MethodPost, Path: "/v1/audit-engagements/{id}/close", Tags: []string{"Audit"},
		Summary:     "Close an engagement",
		Description: "An Owner closes a pending or active engagement before its last day (FR-UX-42); a closed engagement takes no requests or samples and is never reopened. Side effects: the status and its audit event. Idempotent with Idempotency-Key.",
	}, writes, func(ctx context.Context, in *auditEngagementActionInput) (*auditEngagementOutput, error) {
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (AuditEngagement, error) {
			e, err := engagementFor(ctx, u, in.ID)
			if err != nil {
				return AuditEngagement{}, err
			}
			got, err := u.AuditEngagements().Close(ctx, e.ID, uploader(ctx), a.deps.Clock.Now())
			return engagementOf(got), err
		})
		if err != nil {
			return nil, err
		}
		return &auditEngagementOutput{Status: http.StatusOK, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})

	register(a, authz.ListAuditRequests, huma.Operation{
		OperationID: "listAuditRequests", Method: http.MethodGet, Path: "/v1/audit-engagements/{id}/requests", Tags: []string{"Audit"},
		Summary:     "List an engagement's requests",
		Description: "The auditor's requests, in their order (FR-UX-42), each with its answer and evidence, a page at a time. Read-only; no side effects.",
	}, append([]string{CodeCursor, CodeNotFound}, locks...), func(ctx context.Context, in *auditListInput) (*struct {
		Body struct {
			Items      []AuditRequestBody `json:"items" nullable:"false" doc:"The requests, by number."`
			NextCursor string             `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last." example:"eyJhZnRlciI6IjEifQ"`
		}
	}, error) {
		p, _ := authn.From(ctx)
		var all []storage.AuditRequest
		if err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			e, err := engagementFor(ctx, u, in.ID)
			if err != nil {
				return err
			}
			all, err = u.AuditEngagements().Requests(ctx, e.ID)
			return err
		}); err != nil {
			return nil, err
		}
		page, next, err := pageOf(all, func(r storage.AuditRequest) string { return r.ID.String() }, in.PageQuery)
		if err != nil {
			return nil, err
		}
		out := &struct {
			Body struct {
				Items      []AuditRequestBody `json:"items" nullable:"false" doc:"The requests, by number."`
				NextCursor string             `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last." example:"eyJhZnRlciI6IjEifQ"`
			}
		}{}
		out.Body.Items, out.Body.NextCursor = []AuditRequestBody{}, next
		for _, r := range page {
			out.Body.Items = append(out.Body.Items, auditRequestOf(r))
		}
		return out, nil
	})

	register(a, authz.AskAuditRequest, huma.Operation{
		OperationID: "askAuditRequest", Method: http.MethodPost, Path: "/v1/audit-engagements/{id}/requests", Tags: []string{"Audit"}, DefaultStatus: http.StatusCreated,
		Summary: "Ask for an item",
		Description: "The engagement's auditor asks for an item and names the Owner, Controller or Accountant who answers it (FR-UX-42; AUDENG-01, AUDENG-02, AUDENG-05). Only while the engagement is active and between its dates. " +
			"Side effects: the request and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append([]string{CodeAuditWindow, CodeAuditAuditor, CodeAuditOpen}, writes...), func(ctx context.Context, in *askAuditRequestInput) (*auditRequestOutput, error) {
		b := in.Body
		assignee, err := uuid.Parse(string(b.Assignee))
		if err != nil {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.assignee_user_id", Message: "is not a member's user ID", Fix: "Send the user ID listMembers gives"}})
		}
		var due *time.Time
		if b.DueOn != "" {
			d, err := engDay("body.due_on", b.DueOn)
			if err != nil {
				return nil, err
			}
			due = &d
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (AuditRequestBody, error) {
			e, err := engagementFor(ctx, u, in.ID)
			if err != nil {
				return AuditRequestBody{}, err
			}
			if err := a.onlyTheAuditor(ctx, e); err != nil {
				return AuditRequestBody{}, err
			}
			if err := a.inWindow(e); err != nil {
				return AuditRequestBody{}, err
			}
			if ok, err := a.memberHolds(ctx, u, assignee, authz.Owner, authz.Controller, authz.Accountant); err != nil {
				return AuditRequestBody{}, err
			} else if !ok {
				return AuditRequestBody{}, problem(CodeAuditOpen, "That member is not an Owner, Controller or Accountant.", "Name the person who can answer: an Owner, Controller or Accountant")
			}
			r, err := u.AuditEngagements().Ask(ctx, storage.AuditRequest{Engagement: e.ID, Title: strings.TrimSpace(b.Title), Detail: strings.TrimSpace(b.Detail), DueOn: due, Assignee: assignee,
				RequestedBy: uploader(ctx), RequestedAt: a.deps.Clock.Now()})
			return auditRequestOf(r), err
		})
		if err != nil {
			return nil, err
		}
		return &auditRequestOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})

	register(a, authz.AnswerAuditRequest, huma.Operation{
		OperationID: "answerAuditRequest", Method: http.MethodPost, Path: "/v1/audit-requests/{id}/answer", Tags: []string{"Audit"},
		Summary: "Answer an auditor's request",
		Description: "The person the request names, or an Owner, answers it with a note and evidence files already in the organisation (FR-UX-42; AUDENG-03). It can be answered again until the auditor accepts it. " +
			"Side effects: the answer and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append([]string{CodeAuditWindow, CodeAuditAnswer}, writes...), func(ctx context.Context, in *answerAuditRequestInput) (*auditRequestOutput, error) {
		p, _ := authn.From(ctx)
		var evidence []uuid.UUID
		for i, s := range in.Body.EvidenceIDs {
			id, err := uuid.Parse(string(s))
			if err != nil {
				return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.evidence_ids", Message: "item " + string(rune('1'+i)) + " is not an evidence file's ID", Fix: "Send the IDs of evidence files"}})
			}
			evidence = append(evidence, id)
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (AuditRequestBody, error) {
			rid, err := parseID("id", in.ID)
			if err != nil {
				return AuditRequestBody{}, err
			}
			r, err := u.AuditEngagements().Request(ctx, rid)
			if errors.Is(err, ports.ErrNotFound) {
				return AuditRequestBody{}, problem(CodeNotFound, "No request with that ID exists.", "List the engagement's requests")
			} else if err != nil {
				return AuditRequestBody{}, err
			}
			if p.UserID != r.Assignee && !slices.Contains(p.Roles, authz.Owner) {
				return AuditRequestBody{}, problem(CodeAuditAnswer, "Only the person the request names, or an Owner, answers it.", "Ask "+"the person named on the request")
			}
			e, err := u.AuditEngagements().Get(ctx, r.Engagement)
			if err != nil {
				return AuditRequestBody{}, err
			}
			if err := a.inWindow(e); err != nil {
				return AuditRequestBody{}, err
			}
			for _, id := range evidence {
				if _, err := u.Evidence().Get(ctx, id); errors.Is(err, ports.ErrNotFound) {
					return AuditRequestBody{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.evidence_ids", Message: "names a file that does not exist: " + id.String(), Fix: "Add the evidence to a record first, then name its ID"}})
				} else if err != nil {
					return AuditRequestBody{}, err
				}
			}
			got, err := u.AuditEngagements().Answer(ctx, r.ID, strings.TrimSpace(in.Body.Answer), evidence, uploader(ctx), a.deps.Clock.Now())
			return auditRequestOf(got), err
		})
		if err != nil {
			return nil, err
		}
		return &auditRequestOutput{Status: http.StatusOK, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})

	register(a, authz.CloseAuditRequest, huma.Operation{
		OperationID: "closeAuditRequest", Method: http.MethodPost, Path: "/v1/audit-requests/{id}/close", Tags: []string{"Audit"},
		Summary:     "Accept an answer and close the request",
		Description: "The engagement's auditor accepts an answered request and closes it (FR-UX-42; AUDENG-02). Side effects: the status and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append([]string{CodeAuditWindow, CodeAuditAuditor}, writes...), func(ctx context.Context, in *auditRequestActionInput) (*auditRequestOutput, error) {
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (AuditRequestBody, error) {
			rid, err := parseID("id", in.ID)
			if err != nil {
				return AuditRequestBody{}, err
			}
			r, err := u.AuditEngagements().Request(ctx, rid)
			if errors.Is(err, ports.ErrNotFound) {
				return AuditRequestBody{}, problem(CodeNotFound, "No request with that ID exists.", "List the engagement's requests")
			} else if err != nil {
				return AuditRequestBody{}, err
			}
			e, err := u.AuditEngagements().Get(ctx, r.Engagement)
			if err != nil {
				return AuditRequestBody{}, err
			}
			if err := a.onlyTheAuditor(ctx, e); err != nil {
				return AuditRequestBody{}, err
			}
			if err := a.inWindow(e); err != nil {
				return AuditRequestBody{}, err
			}
			got, err := u.AuditEngagements().CloseRequest(ctx, r.ID, uploader(ctx), a.deps.Clock.Now())
			return auditRequestOf(got), err
		})
		if err != nil {
			return nil, err
		}
		return &auditRequestOutput{Status: http.StatusOK, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})

	register(a, authz.ListAuditSamples, huma.Operation{
		OperationID: "listAuditSamples", Method: http.MethodGet, Path: "/v1/audit-engagements/{id}/samples", Tags: []string{"Audit"},
		Summary:     "List an engagement's samples",
		Description: "The samples the auditor drew, newest first, each with its rule, seed and lines (FR-UX-42), a page at a time. Read-only; no side effects.",
	}, append([]string{CodeCursor, CodeNotFound}, locks...), func(ctx context.Context, in *auditListInput) (*struct {
		Body struct {
			Items      []AuditSampleBody `json:"items" nullable:"false" doc:"The samples, newest first."`
			NextCursor string            `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last." example:"eyJhZnRlciI6IjEifQ"`
		}
	}, error) {
		p, _ := authn.From(ctx)
		var all []storage.AuditSample
		if err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			e, err := engagementFor(ctx, u, in.ID)
			if err != nil {
				return err
			}
			all, err = u.AuditEngagements().Samples(ctx, e.ID)
			return err
		}); err != nil {
			return nil, err
		}
		page, next, err := pageOf(all, func(s storage.AuditSample) string { return s.ID.String() }, in.PageQuery)
		if err != nil {
			return nil, err
		}
		out := &struct {
			Body struct {
				Items      []AuditSampleBody `json:"items" nullable:"false" doc:"The samples, newest first."`
				NextCursor string            `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last." example:"eyJhZnRlciI6IjEifQ"`
			}
		}{}
		out.Body.Items, out.Body.NextCursor = []AuditSampleBody{}, next
		for _, s := range page {
			out.Body.Items = append(out.Body.Items, auditSampleOf(s))
		}
		return out, nil
	})

	register(a, authz.DrawAuditSample, huma.Operation{
		OperationID: "drawAuditSample", Method: http.MethodPost, Path: "/v1/audit-engagements/{id}/samples", Tags: []string{"Audit"}, DefaultStatus: http.StatusCreated,
		Summary: "Draw a sample from the ledger",
		Description: "The engagement's auditor draws a sample of posted ledger lines by a rule: a period inside the engagement's, an account, the smallest line and a count of up to 200 (FR-UX-42; AUDENG-01, AUDENG-02). " +
			"Lines are ranked by the SHA-256 of the seed, entry and line, so the same seed and rule over the same ledger draw the same lines; the sample is kept as drawn and never changed. The ledger is only read. " +
			"Side effects: the sample and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview draws and saves nothing.",
	}, append([]string{CodeAuditWindow, CodeAuditAuditor}, writes...), func(ctx context.Context, in *drawAuditSampleInput) (*auditSampleOutput, error) {
		rule := in.Body.Rule
		if _, err := engDay("body.rule.from", rule.From); err != nil {
			return nil, err
		}
		if _, err := engDay("body.rule.to", rule.To); err != nil {
			return nil, err
		}
		seed := strings.TrimSpace(in.Body.Seed)
		if seed == "" {
			seed = uuid.NewString()
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (AuditSampleBody, error) {
			e, err := engagementFor(ctx, u, in.ID)
			if err != nil {
				return AuditSampleBody{}, err
			}
			if err := a.onlyTheAuditor(ctx, e); err != nil {
				return AuditSampleBody{}, err
			}
			if err := a.inWindow(e); err != nil {
				return AuditSampleBody{}, err
			}
			if rule.From > rule.To || rule.From < engDayStr(e.PeriodFrom) || rule.To > engDayStr(e.PeriodTo) {
				return AuditSampleBody{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.rule.from", Message: "the rule's dates are not inside the engagement's period " + engDayStr(e.PeriodFrom) + " to " + engDayStr(e.PeriodTo), Fix: "Choose dates inside the period under audit"}})
			}
			accounts, err := u.Accounts().List(ctx)
			if err != nil {
				return AuditSampleBody{}, err
			}
			byID := map[uuid.UUID]storage.Account{}
			for _, ac := range accounts {
				byID[ac.ID] = ac
			}
			if rule.Account != "" && !slices.ContainsFunc(accounts, func(ac storage.Account) bool { return ac.Code == rule.Account }) {
				return AuditSampleBody{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.rule.account", Message: "is not the code of an account of this organisation", Fix: "Use an account code from the chart, or leave it out for any account"}})
			}
			r := auditsample.Rule{From: rule.From, To: rule.To, Account: rule.Account, MinAmount: rule.MinAmount, Count: rule.Count}
			if err := r.Validate(); err != nil {
				return AuditSampleBody{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.rule", Message: err.Error(), Fix: "Ask for 1 to 200 lines and send the smallest line as a decimal string such as 500.00"}})
			}
			var lines []auditsample.Line
			var after int64
			for {
				entries, err := u.Ledger().Entries(ctx, after, 500)
				if err != nil {
					return AuditSampleBody{}, err
				}
				if len(entries) == 0 {
					break
				}
				for _, en := range entries {
					after = max(after, en.LedgerRevision)
					pd := engDayStr(en.PostingDate)
					if pd < r.From || pd > r.To {
						continue
					}
					for _, l := range en.Lines {
						ac := byID[l.AccountID]
						cand := auditsample.Line{EntryID: en.ID.String(), LineNo: l.LineNo, PostingDate: pd, Account: ac.Code, AccountName: ac.Name, BaseAmount: string(l.BaseAmount), Memo: en.Memo,
							SourceType: en.Source.Type, SourceID: en.Source.ID}
						ok, err := r.Qualifies(cand)
						if err != nil {
							return AuditSampleBody{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.rule.min_amount", Message: err.Error(), Fix: "Send the smallest line as a decimal string such as 500.00"}})
						}
						if ok {
							lines = append(lines, cand)
						}
					}
					if len(lines) > maxSampleScan {
						return AuditSampleBody{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.rule", Message: "qualifies more than 500,000 lines", Fix: "Narrow the period, name an account or raise the smallest line"}})
					}
				}
			}
			pop, picks, err := auditsample.Draw(r, seed, lines)
			if err != nil {
				return AuditSampleBody{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.rule.count", Message: err.Error(), Fix: "Ask for 1 to 200 lines"}})
			}
			rawRule, _ := json.Marshal(r)
			rawItems, _ := json.Marshal(picks)
			if picks == nil {
				rawItems = []byte("[]")
			}
			s, err := u.AuditEngagements().Draw(ctx, storage.AuditSample{Engagement: e.ID, Rule: rawRule, Seed: seed, Population: pop, Items: rawItems, DrawnBy: uploader(ctx), DrawnAt: a.deps.Clock.Now()})
			return auditSampleOf(s), err
		})
		if err != nil {
			return nil, err
		}
		return &auditSampleOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})
}
