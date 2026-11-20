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
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/sharepay"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ shares for services (FR-EQ-13; p2-reporting-planning task 8.3, design D15)
//
// A grant of shares for services is an equity-settled share-based payment (SFRS(I) 2 / IFRS 2): its fair value per share at the grant date, a service start and tranches of whole shares each with its
// vest day. Each tranche's expense is its shares times the fair value, rounded once, spread over its whole service months by ALLOC-EVEN; each month's expense is proposed as a draft journal, Dr share-based
// payment expense · Cr share-based payment reserve, that an approver posts like any other. The reserve moves to share capital when the shares are issued (an equity transaction: not built here).

// ShareGrantTranche is some of a grant's shares and the day they vest.
type ShareGrantTranche struct {
	Shares int64  `json:"shares" minimum:"1" doc:"Whole shares." example:"12000"`
	VestOn string `json:"vest_on" format:"date" doc:"The day they vest: the grant's expense for them runs to the end of that day's month." example:"2026-12-31"`
}

// ShareGrantInput is what a person saves of a grant.
type ShareGrantInput struct {
	Recipient      string              `json:"recipient" minLength:"1" maxLength:"120" doc:"Who the shares are granted to." example:"Wei Jie Ong"`
	FairValue      string              `json:"fair_value" pattern:"^[0-9]+(\\.[0-9]{1,6})?$" doc:"The fair value of a share at the grant date, in the base currency, at most six decimals: measured once, never remeasured." example:"0.50"`
	Valuation      string              `json:"valuation,omitempty" maxLength:"500" doc:"Where the fair value comes from, such as the valuation report and its date [verify valuation]." example:"Independent valuation of 30 Sep 2026"`
	ServiceStart   string              `json:"service_start" format:"date" doc:"The first day of the month the service starts." example:"2026-01-01"`
	Tranches       []ShareGrantTranche `json:"tranches" nullable:"false" minItems:"1" maxItems:"20" doc:"The tranches: each has its own service period, from the service start to its vest day's month."`
	ExpenseAccount string              `json:"expense_account" format:"uuid" doc:"The expense account the monthly expense is debited to." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c1a01"`
	ReserveAccount string              `json:"reserve_account" format:"uuid" doc:"The equity account (the share-based payment reserve) it is credited to; never share capital." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c1a02"`
	Archived       bool                `json:"archived,omitempty" doc:"true to archive the grant: no month is proposed for it again." example:"false"`
}

// ShareGrant is a saved grant.
type ShareGrant struct {
	ID      string `json:"id" format:"uuid" doc:"The grant's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c9a01"`
	Version int    `json:"version" doc:"The version of its content; it makes the ETag." example:"1"`
	ShareGrantInput
}

// ShareGrantPage is a page of grants.
type ShareGrantPage struct {
	Items      []ShareGrant `json:"items" nullable:"false" doc:"The grants, latest first."`
	NextCursor string       `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjEifQ"`
}

// ShareGrantTrancheExpense is a tranche's schedule.
type ShareGrantTrancheExpense struct {
	Months int      `json:"months" doc:"Its service months." example:"12"`
	Total  string   `json:"total" doc:"Its shares times the fair value, rounded once (RND-LINE)." example:"6000.00"`
	Parts  []string `json:"parts" nullable:"false" doc:"Each month's part, by ALLOC-EVEN: they add to the total."`
}

// ShareGrantMonthExpense is a month's expense and whether it is proposed.
type ShareGrantMonthExpense struct {
	Month     string `json:"month" pattern:"^[0-9]{4}-[0-9]{2}$" doc:"The month." example:"2026-01"`
	Expense   string `json:"expense" doc:"The tranches' parts for it." example:"500.00"`
	JournalID string `json:"journal_id,omitempty" format:"uuid" doc:"The draft journal, once proposed." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c2a01"`
}

// ShareGrantSchedule is a grant's expense over its months.
type ShareGrantSchedule struct {
	GrantID  string                     `json:"grant_id" format:"uuid" doc:"The grant." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c9a01"`
	Total    string                     `json:"total" doc:"The grant's total expense." example:"6000.00"`
	Tranches []ShareGrantTrancheExpense `json:"tranches" nullable:"false" doc:"Each tranche's total and parts."`
	Months   []ShareGrantMonthExpense   `json:"months" nullable:"false" doc:"Each month's expense, with the journal proposed for it."`
	Note     string                     `json:"note" doc:"What it is." example:"No tax deduction is claimed for newly issued shares; the reserve moves to share capital when the shares are issued."`
}

// ShareGrantMonthInput asks for a month's expense as a draft journal.
type ShareGrantMonthInput struct {
	GrantID string `json:"grant_id" format:"uuid" doc:"The grant." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c9a01"`
	Month   string `json:"month" pattern:"^[0-9]{4}-[0-9]{2}$" doc:"The month, one the schedule runs over that has begun." example:"2026-01"`
}

// ShareGrantMonth is a month proposed.
type ShareGrantMonth struct {
	GrantID   string `json:"grant_id" format:"uuid" doc:"The grant." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c9a01"`
	Month     string `json:"month" pattern:"^[0-9]{4}-[0-9]{2}$" doc:"The month." example:"2026-01"`
	Expense   string `json:"expense" doc:"The month's expense, debited to the expense account and credited to the reserve." example:"500.00"`
	JournalID string `json:"journal_id" format:"uuid" doc:"The draft journal for an approver." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c2a01"`
}

// ShareGrantMonthPage is a grant's months proposed.
type ShareGrantMonthPage struct {
	Items      []ShareGrantMonth `json:"items" nullable:"false" doc:"The months proposed, earliest first."`
	NextCursor string            `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjEifQ"`
}

type (
	// ShareGrantPath names a grant.
	ShareGrantPath struct {
		GrantID string `path:"grant_id" format:"uuid" doc:"The grant." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c9a01"`
	}
	shareGrantListInput  struct{ PageQuery }
	shareGrantListOutput struct{ Body ShareGrantPage }
	shareGrantSaveInput  struct {
		KeyHeader
		PreferHeader
		Body ShareGrantInput
	}
	shareGrantReviseInput struct {
		ShareGrantPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body ShareGrantInput
	}
	shareGrantWriteOutput struct {
		Status   int
		ETag     string `header:"ETag" doc:"The grant's version, for If-Match."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     ShareGrant
	}
	shareGrantReadOutput struct {
		ETag string `header:"ETag" doc:"The grant's version, for If-Match."`
		Body ShareGrant
	}
	shareGrantScheduleOutput struct{ Body ShareGrantSchedule }
	shareGrantMonthSave      struct {
		KeyHeader
		PreferHeader
		Body ShareGrantMonthInput
	}
	shareGrantMonthOutput struct {
		Status   int
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     ShareGrantMonth
	}
	shareGrantMonthsInput struct {
		GrantID string `query:"grant_id" format:"uuid" required:"true" doc:"The grant whose months are listed." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c9a01"`
		PageQuery
	}
	shareGrantMonthsOutput struct{ Body ShareGrantMonthPage }
)

func shareGrantOf(doc storage.Document) (ShareGrant, error) {
	g, err := sharepay.Read(doc)
	if err != nil {
		return ShareGrant{}, err
	}
	out := ShareGrant{ID: doc.ID.String(), Version: doc.Version, ShareGrantInput: ShareGrantInput{Recipient: g.Recipient, FairValue: g.FairValue, Valuation: g.Valuation,
		ServiceStart: g.ServiceStart, ExpenseAccount: g.ExpenseAccount.String(), ReserveAccount: g.ReserveAccount.String(), Archived: g.Archived, Tranches: []ShareGrantTranche{}}}
	for _, t := range g.Tranches {
		out.Tranches = append(out.Tranches, ShareGrantTranche{Shares: t.Shares, VestOn: t.VestOn})
	}
	return out, nil
}

func grantFromInput(in ShareGrantInput) (sharepay.Grant, error) {
	g := sharepay.Grant{Recipient: in.Recipient, FairValue: in.FairValue, Valuation: in.Valuation, ServiceStart: in.ServiceStart, Archived: in.Archived}
	var fields []FieldProblem
	for field, s := range map[string]string{"expense_account": in.ExpenseAccount, "reserve_account": in.ReserveAccount} {
		id, err := uuid.Parse(s)
		if err != nil {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body." + field, Message: "is not an ID", Fix: "Send the ID of an account"})
			continue
		}
		if field == "expense_account" {
			g.ExpenseAccount = id
		} else {
			g.ReserveAccount = id
		}
	}
	for _, t := range in.Tranches {
		g.Tranches = append(g.Tranches, sharepay.TrancheSpec{Shares: t.Shares, VestOn: t.VestOn})
	}
	if len(fields) > 0 {
		return g, invalid(fields)
	}
	return g, nil
}

func shareGrantAnswer(w written[ShareGrant]) *shareGrantWriteOutput {
	if w.Previewed {
		return &shareGrantWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}
	}
	return &shareGrantWriteOutput{Status: w.Status, ETag: etag(w.Body.Version, "draft"), Replayed: replayedHeader(w.Replayed), Body: w.Body}
}

func registerShareGrants(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	domain := []string{sharepay.CodeGrant, sharepay.CodeTranche, sharepay.CodeAccounts, sharepay.CodeMonth, sharepay.CodeMonthRun, sharepay.CodeArchived}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeNotFound}, locks...)
	tags := []string{"Equity"}

	register(a, authz.ListShareGrants, huma.Operation{
		OperationID: "listShareGrants", Method: http.MethodGet, Path: "/v1/share-grants", Tags: tags,
		Summary:     "List the grants of shares for services",
		Description: "Every grant, the latest first, a page at a time (FR-EQ-13). A reader who holds only the masked view gets the figures and names masked. Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *shareGrantListInput) (*shareGrantListOutput, error) {
		p, _ := authn.From(ctx)
		out := shareGrantListOutput{Body: ShareGrantPage{Items: []ShareGrant{}}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			var all []ShareGrant
			f := storage.DocumentFilter{Type: storage.DocumentShareGrant, Limit: 200}
			for {
				docs, err := u.Documents().Page(ctx, f)
				if err != nil {
					return err
				}
				for _, d := range docs {
					v, err := shareGrantOf(d)
					if err != nil {
						return err
					}
					all = append(all, v)
				}
				if len(docs) < f.Limit {
					break
				}
				f.AfterDate, f.AfterID = docs[len(docs)-1].Date, &docs[len(docs)-1].ID
			}
			var err error
			out.Body.Items, out.Body.NextCursor, err = pageOf(all, func(x ShareGrant) string { return x.ID }, in.PageQuery)
			if out.Body.Items == nil {
				out.Body.Items = []ShareGrant{}
			}
			return err
		})
		return &out, err
	})

	register(a, authz.CreateShareGrant, huma.Operation{
		OperationID: "createShareGrant", Method: http.MethodPost, Path: "/v1/share-grants", Tags: tags, DefaultStatus: http.StatusCreated,
		Summary: "Record a grant of shares for services",
		Description: "Records a grant (FR-EQ-13): the fair value per share at the grant date, the service start, the tranches and the two accounts its monthly journal posts to (an expense account and an equity " +
			"account that is not share capital). It posts nothing: getShareGrantSchedule shows its expense and proposeShareGrantMonth proposes each month as a draft journal. Every problem comes back together. " +
			"Side effects: the grant and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(domain, writes...), func(ctx context.Context, in *shareGrantSaveInput) (*shareGrantWriteOutput, error) {
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (ShareGrant, error) {
			g, err := grantFromInput(in.Body)
			if err != nil {
				return ShareGrant{}, err
			}
			doc, err := sharepay.Create(ctx, u, g)
			if err != nil {
				return ShareGrant{}, equityProblems(err)
			}
			return shareGrantOf(doc)
		})
		if err != nil {
			return nil, err
		}
		return shareGrantAnswer(w), nil
	})

	register(a, authz.GetShareGrant, huma.Operation{
		OperationID: "getShareGrant", Method: http.MethodGet, Path: "/v1/share-grants/{grant_id}", Tags: tags,
		Summary: "Get a grant of shares for services", Description: "A grant as recorded, with its ETag (FR-EQ-13). A reader who holds only the masked view gets the figures and names masked. Read-only; no side effects.",
	}, append([]string{CodeNotFound}, locks...), func(ctx context.Context, in *ShareGrantPath) (*shareGrantReadOutput, error) {
		id, err := parseID("grant_id", in.GrantID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out shareGrantReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := u.Documents().Get(ctx, id)
			if err != nil {
				return err
			}
			if out.Body, err = shareGrantOf(doc); err != nil {
				return err
			}
			out.ETag = recordETag(doc)
			return nil
		})
		return &out, err
	})

	register(a, authz.ReplaceShareGrant, huma.Operation{
		OperationID: "replaceShareGrant", Method: http.MethodPut, Path: "/v1/share-grants/{grant_id}", Tags: tags,
		Summary: "Change a grant of shares for services",
		Description: "Saves the grant's next version (FR-EQ-13). Once a month of its expense is proposed its figures stay as they were: only the recipient, the valuation note and archiving can change (SBP-04). " +
			"Needs If-Match with its ETag. Side effects: the new version and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append([]string{CodeStale, CodeIfMatchRequired}, append(domain, writes...)...), func(ctx context.Context, in *shareGrantReviseInput) (*shareGrantWriteOutput, error) {
		id, err := parseID("grant_id", in.GrantID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (ShareGrant, error) {
			doc, err := u.Documents().Lock(ctx, id)
			if err != nil {
				return ShareGrant{}, err
			}
			if err := checkIfMatch(in.IfMatch, recordETag(doc)); err != nil {
				return ShareGrant{}, err
			}
			g, err := grantFromInput(in.Body)
			if err != nil {
				return ShareGrant{}, err
			}
			if doc, err = sharepay.Revise(ctx, u, id, g); err != nil {
				return ShareGrant{}, equityProblems(err)
			}
			return shareGrantOf(doc)
		})
		if err != nil {
			return nil, err
		}
		return shareGrantAnswer(w), nil
	})

	register(a, authz.GetShareGrantSchedule, huma.Operation{
		OperationID: "getShareGrantSchedule", Method: http.MethodGet, Path: "/v1/share-grants/{grant_id}/schedule", Tags: tags,
		Summary: "Work a grant's expense out",
		Description: "Each tranche's total (its shares times the fair value, rounded once) and its part of each service month by ALLOC-EVEN, and each month's expense with the journal proposed for it (FR-EQ-13). " +
			"Worked out on every read in exact decimals. A reader who holds only the masked view gets the figures and names masked. Read-only; no side effects.",
	}, append([]string{CodeNotFound}, append(domain, locks...)...), func(ctx context.Context, in *ShareGrantPath) (*shareGrantScheduleOutput, error) {
		id, err := parseID("grant_id", in.GrantID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out shareGrantScheduleOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := u.Documents().Get(ctx, id)
			if err != nil {
				return err
			}
			g, err := sharepay.Read(doc)
			if err != nil {
				return err
			}
			org, err := u.Organisation(ctx)
			if err != nil {
				return err
			}
			base, err := money.Lookup(org.BaseCurrency)
			if err != nil {
				return err
			}
			s, err := sharepay.ScheduleOf(g, base)
			if err != nil {
				return err
			}
			runs, err := sharepay.Months(ctx, u, id)
			if err != nil {
				return err
			}
			out.Body = ShareGrantSchedule{GrantID: id.String(), Total: s.Total.String(), Tranches: []ShareGrantTrancheExpense{}, Months: []ShareGrantMonthExpense{},
				Note: "No tax deduction is claimed for newly issued shares; the reserve moves to share capital when the shares are issued (an equity transaction)."}
			for _, t := range s.Tranches {
				parts := make([]string, len(t.Parts))
				for i, x := range t.Parts {
					parts[i] = x.String()
				}
				out.Body.Tranches = append(out.Body.Tranches, ShareGrantTrancheExpense{Months: t.Months, Total: t.Total.String(), Parts: parts})
			}
			for _, m := range s.Months {
				row := ShareGrantMonthExpense{Month: m.Month.Format("2006-01"), Expense: m.Expense.String()}
				for _, r := range runs {
					if r.Month.Equal(m.Month) {
						row.JournalID = r.JournalID.String()
					}
				}
				out.Body.Months = append(out.Body.Months, row)
			}
			return nil
		})
		return &out, err
	})

	register(a, authz.ProposeShareGrantMonth, huma.Operation{
		OperationID: "proposeShareGrantMonth", Method: http.MethodPost, Path: "/v1/share-grant-months", Tags: tags, DefaultStatus: http.StatusCreated,
		Summary: "Propose a month of a grant's expense as a draft journal",
		Description: "Makes the month's expense as a draft journal for an approver (FR-EQ-13): Dr the grant's expense account and Cr its reserve with the tranches' parts for the month. The month is one the schedule " +
			"runs over that has begun, and each month is proposed once (SBP-05). The month is recorded, never changed and never reversed (FYR14). Side effects: the draft journal, the recorded month and their " +
			"audit events. Nothing is posted. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(domain, writes...), func(ctx context.Context, in *shareGrantMonthSave) (*shareGrantMonthOutput, error) {
		id, err := parseID("body.grant_id", in.Body.GrantID)
		if err != nil {
			return nil, err
		}
		month, err := time.Parse("2006-01", in.Body.Month)
		if err != nil {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.month", Message: "is not a month", Fix: "Send a month as YYYY-MM"}})
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (ShareGrantMonth, error) {
			doc, err := u.Documents().Lock(ctx, id)
			if err != nil {
				return ShareGrantMonth{}, err
			}
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return ShareGrantMonth{}, err
			}
			m, j, err := sharepay.ProposeMonth(ctx, u, doc, month, today)
			if err != nil {
				return ShareGrantMonth{}, equityProblems(err)
			}
			return ShareGrantMonth{GrantID: id.String(), Month: m.Month.Format("2006-01"), Expense: m.Expense.String(), JournalID: j.ID.String()}, nil
		})
		if err != nil {
			return nil, err
		}
		return &shareGrantMonthOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})

	register(a, authz.ListShareGrantMonths, huma.Operation{
		OperationID: "listShareGrantMonths", Method: http.MethodGet, Path: "/v1/share-grant-months", Tags: tags,
		Summary: "List a grant's months proposed", Description: "The months of a grant's expense proposed so far, earliest first, a page at a time (FR-EQ-13). A reader who holds only the masked view gets the figures and names masked. Read-only; no side effects.",
	}, append([]string{CodeNotFound, CodeCursor}, locks...), func(ctx context.Context, in *shareGrantMonthsInput) (*shareGrantMonthsOutput, error) {
		id, err := parseID("grant_id", in.GrantID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		out := shareGrantMonthsOutput{Body: ShareGrantMonthPage{Items: []ShareGrantMonth{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := u.Documents().Get(ctx, id)
			if err != nil {
				return err
			}
			if _, err := sharepay.Read(doc); err != nil {
				return err
			}
			runs, err := sharepay.Months(ctx, u, id)
			if err != nil {
				return err
			}
			var all []ShareGrantMonth
			for _, r := range runs {
				all = append(all, ShareGrantMonth{GrantID: id.String(), Month: r.Month.Format("2006-01"), Expense: r.Expense, JournalID: r.JournalID.String()})
			}
			out.Body.Items, out.Body.NextCursor, err = pageOf(all, func(x ShareGrantMonth) string { return x.Month }, in.PageQuery)
			if out.Body.Items == nil {
				out.Body.Items = []ShareGrantMonth{}
			}
			return err
		})
		return &out, err
	})
}

// TransformSchema implements huma.SchemaTransformer: an example for each part (NFR-API-01).
func (ShareGrantTrancheExpense) TransformSchema(_ huma.Registry, s *huma.Schema) *huma.Schema {
	itemExamples(s, map[string]any{"parts": "500.00"})
	return s
}
