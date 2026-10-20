package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/period"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ periods and financial years (FR-ORG-02, FR-GL-08)

// PeriodCodes are the codes of the period rules.
var PeriodCodes = []string{period.CodeNoYear, period.CodeTooFar, period.CodeNotNext}

// Period is an accounting period: a month, or part of one at the books start.
type Period struct {
	ID             string `json:"id" doc:"The period's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d61"`
	StartsOn       string `json:"starts_on" format:"date" doc:"Its first day." example:"2026-10-01"`
	EndsOn         string `json:"ends_on" format:"date" doc:"Its last day." example:"2026-10-31"`
	Status         string `json:"status" enum:"open,soft_locked,closed" doc:"open takes postings; closed is the hard lock of INV-03, which the database enforces; soft_locked arrives with FR-GL-09 (P2)." example:"open"`
	FinancialYear  int    `json:"financial_year,omitempty" doc:"Which financial year holds it, 1 for the first; left out when the books start is not set." example:"1"`
	ClosedRevision *int64 `json:"closed_revision,omitempty" doc:"The ledger revision at the close (ENG-46), while it is closed." example:"412"`
	Version        int    `json:"version" doc:"The period's version." example:"1"`
}

// FinancialYear is a financial year and its periods.
type FinancialYear struct {
	Number   int    `json:"number" doc:"1 for the first financial year." example:"1"`
	StartsOn string `json:"starts_on" format:"date" doc:"Its first day: the books start for the first year." example:"2026-03-15"`
	EndsOn   string `json:"ends_on" format:"date" doc:"Its last day." example:"2027-03-31"`
	Periods  int    `json:"periods" doc:"How many periods it has: its months, the first one short when the books start mid-month." example:"13"`
	MayOpen  bool   `json:"may_open" doc:"false while it starts more than a year after the year that holds today." example:"true"`
}

// PeriodPage is a page of periods, earliest first.
type PeriodPage struct {
	Items      []Period       `json:"items" nullable:"false"`
	NextCursor string         `json:"next_cursor,omitempty" doc:"Pass it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjIwMjYtMTAtMDEifQ"`
	NextYear   *FinancialYear `json:"next_year,omitempty" doc:"The financial year that opens next with openFinancialYear; absent until the books start and the first year end are set."`
}

// FinancialYearOpened is what opening a financial year did.
type FinancialYearOpened struct {
	Year    FinancialYear `json:"year"`
	Periods []Period      `json:"periods" nullable:"false"`
}

type periodListInput struct {
	PageQuery
}

type periodListOutput struct {
	Body PeriodPage
}

type PeriodPath struct {
	PeriodID string `path:"period_id" doc:"The period's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d61"`
}

type periodReadOutput struct {
	ETag string `header:"ETag" doc:"The period's version, for If-Match."`
	Body Period
}

// OpenYearRequest names the financial year to open.
type OpenYearRequest struct {
	StartsOn string `json:"starts_on" format:"date" doc:"The first day of the year to open: next_year.starts_on from listPeriods. A year that is not the next one is refused, so a repeated request never opens a second year." example:"2026-03-15"`
}

type openYearInput struct {
	KeyHeader
	PreferHeader
	Body OpenYearRequest
}

type openYearOutput struct {
	Status   int
	Location string `header:"Location" doc:"The periods."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     FinancialYearOpened
}

func periodOf(p storage.Period, cal *period.Calendar) Period {
	out := Period{ID: p.ID.String(), StartsOn: p.StartsOn.Format(time.DateOnly), EndsOn: p.EndsOn.Format(time.DateOnly),
		Status: string(p.Status), ClosedRevision: p.ClosedRevision, Version: p.Version}
	if cal != nil {
		out.FinancialYear = cal.Number(p.StartsOn)
	}
	return out
}

func periodETag(p Period) string { return etag(p.Version, p.Status) }

func yearOf(y period.Year, mayOpen bool) FinancialYear {
	return FinancialYear{Number: y.Number, StartsOn: y.StartsOn.Format(time.DateOnly), EndsOn: y.EndsOn.Format(time.DateOnly),
		Periods: len(y.Months), MayOpen: mayOpen}
}

// calendar is the organisation's calendar, or nil while its years are not set.
func calendar(ctx context.Context, u storage.UnitOfWork) (*period.Calendar, error) {
	o, err := u.Organisation(ctx)
	if err != nil {
		return nil, err
	}
	if o.Profile.BooksStart == nil || o.Profile.FirstYearEnd == nil {
		return nil, nil // the years are not set yet
	}
	c, err := period.CalendarOf(o.Profile)
	return &c, err
}

func registerPeriods(a *API) {
	register(a, authz.ListPeriods, huma.Operation{
		OperationID: "listPeriods", Method: http.MethodGet, Path: "/v1/periods", Tags: []string{"Periods"},
		Summary: "List the periods",
		Description: "The organisation's accounting periods, earliest first, a page at a time, each with its status and financial year " +
			"(FR-ORG-02, FR-GL-08), and the financial year that opens next. A posting's period is the month of its posting date " +
			"(Appendix B); a date no period covers cannot be posted. Read-only; no side effects.",
	}, []string{CodeCursor}, func(ctx context.Context, in *periodListInput) (*periodListOutput, error) {
		p, _ := authn.From(ctx)
		out := periodListOutput{Body: PeriodPage{Items: []Period{}}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			all, err := u.Periods().List(ctx)
			if err != nil {
				return err
			}
			cal, err := calendar(ctx, u)
			if err != nil {
				return err
			}
			page, next, err := pageOf(all, func(p storage.Period) string { return p.StartsOn.Format(time.DateOnly) }, in.PageQuery)
			if err != nil {
				return err
			}
			out.Body.NextCursor = next
			for _, x := range page {
				out.Body.Items = append(out.Body.Items, periodOf(x, cal))
			}
			if cal != nil {
				today, err := orgToday(ctx, a, u)
				if err != nil {
					return err
				}
				y := cal.Next(all)
				fy := yearOf(y, cal.MayOpen(y, today) == nil)
				out.Body.NextYear = &fy
			}
			return nil
		})
		return &out, err
	})

	register(a, authz.GetPeriod, huma.Operation{
		OperationID: "getPeriod", Method: http.MethodGet, Path: "/v1/periods/{period_id}", Tags: []string{"Periods"},
		Summary:     "Get a period",
		Description: "A period with its status and its ETag. Read-only; no side effects.",
	}, []string{CodeNotFound}, func(ctx context.Context, in *PeriodPath) (*periodReadOutput, error) {
		id, err := parseID("period_id", in.PeriodID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out periodReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			x, err := u.Periods().Get(ctx, id)
			if err != nil {
				return err
			}
			cal, err := calendar(ctx, u)
			out.Body = periodOf(x, cal)
			out.ETag = periodETag(out.Body)
			return err
		})
		return &out, err
	})

	register(a, authz.OpenFinancialYear, huma.Operation{
		OperationID: "openFinancialYear", Method: http.MethodPost, Path: "/v1/periods:open-year", Tags: []string{"Periods"},
		DefaultStatus: http.StatusCreated, Summary: "Open the next financial year",
		Description: "Creates the periods of the next financial year: the first year runs from the books start to the first year end, " +
			"each later one the twelve months after; its periods are calendar months, the first one short when the books start " +
			"mid-month (FR-ORG-02; PRD-001 rev 0.47, provisional). The body names the year's first day, as listPeriods' next_year " +
			"gives it, and any other year is refused, as is a year that starts more than a year after the one that holds today. " +
			"Once a period exists, when the books start and how the years run no longer change. Side effects: the periods and an " +
			"audit event for each. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		storage.CodePeriodDates, storage.CodeRetry, storage.CodeLockTimeout}, PeriodCodes...),
		func(ctx context.Context, in *openYearInput) (*openYearOutput, error) {
			starts, err := time.Parse(time.DateOnly, strings.TrimSpace(in.Body.StartsOn))
			if err != nil {
				return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.starts_on", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"}})
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (FinancialYearOpened, error) {
				o, err := u.Organisation(ctx)
				if err != nil {
					return FinancialYearOpened{}, err
				}
				cal, err := period.CalendarOf(o.Profile)
				if err != nil {
					return FinancialYearOpened{}, err
				}
				all, err := u.Periods().List(ctx)
				if err != nil {
					return FinancialYearOpened{}, err
				}
				y := cal.Next(all)
				if !y.StartsOn.Equal(starts) {
					return FinancialYearOpened{}, problem(period.CodeNotNext,
						fmt.Sprintf("The year starting %s is not the next one to open; the next starts %s.", starts.Format(time.DateOnly), y.StartsOn.Format(time.DateOnly)),
						"List the periods again and open the year their next_year names.")
				}
				today, err := orgToday(ctx, a, u)
				if err != nil {
					return FinancialYearOpened{}, err
				}
				if err := cal.MayOpen(y, today); err != nil {
					return FinancialYearOpened{}, err
				}
				out := FinancialYearOpened{Year: yearOf(y, true), Periods: []Period{}}
				for _, m := range y.Months {
					p, err := u.Periods().Create(ctx, storage.NewPeriod{StartsOn: m.StartsOn, EndsOn: m.EndsOn})
					if err != nil {
						return FinancialYearOpened{}, err
					}
					out.Periods = append(out.Periods, periodOf(p, &cal))
				}
				return out, nil
			})
			if err != nil {
				return nil, err
			}
			out := &openYearOutput{Status: w.Status, Applied: applied(w), Body: w.Body}
			if !w.Previewed {
				out.Location, out.Replayed = "/v1/periods", replayedHeader(w.Replayed)
			}
			return out, nil
		})
}
