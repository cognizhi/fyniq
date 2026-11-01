package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/kpi"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/period"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------- the KPIs (FR-KPI-01, FR-KPI-02, FR-KPI-03)

// KPIFigure is a KPI's value for a month: its figure, or words that stand for it, or why it has none.
type KPIFigure struct {
	Value     string `json:"value,omitempty" doc:"The figure, rounded by RND-KPI: an amount in the base currency as a decimal string, a percentage, ratio, number of months or days to one decimal, or a whole count. Left out when the KPI has words or no value." example:"40.0"`
	Words     string `json:"words,omitempty" doc:"Words that stand for the figure, such as Not burning for the runway when the average net burn is zero or less." example:"Not burning"`
	Undefined string `json:"undefined,omitempty" doc:"Why it has no value, in words; never shown as 0 (FR-KPI-03)." example:"no subscription data: enter the month's customer counts"`
	Entered   bool   `json:"entered,omitempty" doc:"true when it used the month's entered customer counts or headcount (rev 0.161)." example:"false"`
}

// KPIItem is one KPI: its definition, its value for the month and for the month before.
type KPIItem struct {
	Key       string     `json:"key" doc:"The KPI." example:"runway"`
	Name      string     `json:"name" doc:"Its name." example:"Runway"`
	Version   string     `json:"version" doc:"The definition's version: a change of formula is a new one (FR-KPI-03)." example:"runway@1"`
	Unit      string     `json:"unit" enum:"amount,percent,ratio,months,days,count" doc:"What its value measures, which decides its decimals (RND-KPI)." example:"months"`
	Source    string     `json:"source" doc:"Where it comes from: ledger (the balance service), entered (counts a person entered), ledger and entered, or subscription data (P2)." example:"ledger"`
	Formula   string     `json:"formula" doc:"How it is worked out, in words, as its tile shows on hover." example:"Cash at month end ÷ the average net burn of the last three months of the financial year"`
	DateBasis string     `json:"date_basis" doc:"The dates it reads." example:"the month end and the last three months"`
	Month     KPIFigure  `json:"month" doc:"Its value for the month."`
	Before    *KPIFigure `json:"before,omitempty" doc:"Its value for the month before, when the books have it."`
}

// KPICountsOut are a month's entered counts.
type KPICountsOut struct {
	Month           string  `json:"month" pattern:"^[0-9]{4}-[0-9]{2}$" doc:"The month, YYYY-MM." example:"2026-10"`
	PayingCustomers *int64  `json:"paying_customers,omitempty" doc:"Paying customers at the month's end." example:"10"`
	NewCustomers    *int64  `json:"new_customers,omitempty" doc:"Customers who started paying in the month." example:"2"`
	LostCustomers   *int64  `json:"lost_customers,omitempty" doc:"Customers who stopped paying in the month." example:"1"`
	Headcount       *int64  `json:"headcount,omitempty" doc:"People employed in the month." example:"8"`
	EnteredBy       *Person `json:"entered_by,omitempty" doc:"Who entered them last."`
	EnteredAt       string  `json:"entered_at" format:"date-time" doc:"When." example:"2026-11-03T02:00:00Z"`
	Version         int     `json:"version" doc:"The counts' version: If-Match takes it as the ETag." example:"1"`
}

// KPIs are a month's KPIs, read at one ledger revision.
type KPIs struct {
	Month           string        `json:"month" pattern:"^[0-9]{4}-[0-9]{2}$" doc:"The month, YYYY-MM." example:"2026-10"`
	From            string        `json:"from" format:"date" doc:"The month's first day." example:"2026-10-01"`
	To              string        `json:"to" format:"date" doc:"Its last day." example:"2026-10-31"`
	Currency        string        `json:"currency" doc:"The base currency every amount is in." example:"SGD"`
	Revision        int64         `json:"ledger_revision" doc:"The ledger revision every ledger figure is read as at (FR-RPT-08)." example:"42"`
	PlatformVersion string        `json:"platform_version" doc:"The platform version that worked them out (REL-05)." example:"0.1.0"`
	KPIs            []KPIItem     `json:"kpis" nullable:"false" doc:"Every KPI, in the KPI page's order (FR-KPI-02)."`
	Counts          *KPICountsOut `json:"counts,omitempty" doc:"The month's entered counts, when any were entered (rev 0.161)."`
}

type kpisInput struct {
	Month    string `query:"month" pattern:"^[0-9]{4}-[0-9]{2}$" doc:"The month, YYYY-MM; left out, the month of the organisation's today." example:"2026-10"`
	Revision int64  `query:"as_at_revision" minimum:"1" doc:"Read as at this ledger revision (FR-RPT-08); left out, the ledger as it stands." example:"42"`
}

type kpisOutput struct{ Body KPIs }

func figureOf(v kpi.Value) KPIFigure {
	return KPIFigure{Value: v.Figure, Words: v.Words, Undefined: v.Undefined, Entered: v.Entered}
}

func (a *API) countsOut(ctx context.Context, u storage.UnitOfWork, x storage.KPIInput) (KPICountsOut, error) {
	out := KPICountsOut{Month: x.Month.Format(statementMonthForm), PayingCustomers: x.Counts.Paying, NewCustomers: x.Counts.New, LostCustomers: x.Counts.Lost,
		Headcount: x.Counts.Headcount, EnteredAt: x.EnteredAt.UTC().Format(time.RFC3339), Version: x.Version}
	if kind, id, ok := strings.Cut(x.EnteredBy, ":"); ok {
		out.EnteredBy = &Person{Kind: kind, ID: id}
		if err := a.name(ctx, u, out.EnteredBy); err != nil {
			return out, err
		}
	}
	return out, nil
}

// yearStart is the first day of the financial year that holds day; the books' first day when the year cannot be told.
func (a *API) yearStart(ctx context.Context, u storage.UnitOfWork, day time.Time) (time.Time, error) {
	o, err := u.Organisation(ctx)
	if err != nil {
		return time.Time{}, err
	}
	cal, err := period.CalendarOf(o.Profile)
	if err != nil {
		return time.Time{}, nil // no financial years set: the last three months, whatever their year
	}
	return cal.Year(max(cal.Number(day), 1)).StartsOn, nil
}

// kpis works out a month's KPIs in u (FR-KPI-02, FR-KPI-03, design D2): the month's and the month before's.
func (a *API) kpis(ctx context.Context, u storage.UnitOfWork, in *kpisInput) (out KPIs, err error) {
	org, err := u.Organisation(ctx)
	if err != nil {
		return out, err
	}
	base, err := money.Lookup(org.BaseCurrency)
	if err != nil {
		return out, err
	}
	r, err := a.monthsOf(ctx, u, "-", in.Month, in.Revision)
	if err != nil {
		return out, err
	}
	last := r.last
	start, err := a.yearStart(ctx, u, r.all[last].StartsOn)
	if err != nil {
		return out, err
	}
	months, before, err := kpi.Read(ctx, u, base, r.all, last, start, r.revision)
	if err != nil {
		return out, err
	}
	now := kpi.Compute(months, before, base)
	var then []kpi.Value
	if last > 0 {
		startBefore, err := a.yearStart(ctx, u, r.all[last-1].StartsOn)
		if err != nil {
			return out, err
		}
		m, b, err := kpi.Read(ctx, u, base, r.all, last-1, startBefore, r.revision)
		if err != nil {
			return out, err
		}
		then = kpi.Compute(m, b, base)
	}
	out = KPIs{Month: r.all[last].StartsOn.Format(statementMonthForm), From: dateOf(r.all[last].StartsOn), To: dateOf(r.all[last].EndsOn), Currency: base.Code,
		Revision: r.revision, PlatformVersion: a.deps.Version, KPIs: make([]KPIItem, 0, len(now))}
	for i, v := range now {
		item := KPIItem{Key: v.Key, Name: v.Name, Version: v.Version, Unit: string(v.Unit), Source: v.Source, Formula: v.Formula, DateBasis: v.DateBasis,
			Month: figureOf(v)}
		if then != nil {
			f := figureOf(then[i])
			item.Before = &f
		}
		out.KPIs = append(out.KPIs, item)
	}
	x, err := u.KPIInputs().Get(ctx, r.all[last].StartsOn)
	switch {
	case errors.Is(err, ports.ErrNotFound):
	case err != nil:
		return out, err
	default:
		c, err := a.countsOut(ctx, u, x)
		if err != nil {
			return out, err
		}
		out.Counts = &c
	}
	return out, nil
}

// KPICountsIn are a month's counts as a person enters them.
type KPICountsIn struct {
	PayingCustomers *int64 `json:"paying_customers,omitempty" minimum:"0" maximum:"10000000" doc:"Paying customers at the month's end; with new_customers and lost_customers, or none of the three." example:"10"`
	NewCustomers    *int64 `json:"new_customers,omitempty" minimum:"0" maximum:"10000000" doc:"Customers who started paying in the month." example:"2"`
	LostCustomers   *int64 `json:"lost_customers,omitempty" minimum:"0" maximum:"10000000" doc:"Customers who stopped paying in the month." example:"1"`
	Headcount       *int64 `json:"headcount,omitempty" minimum:"0" maximum:"1000000" doc:"People employed in the month." example:"8"`
}

type setKpiInputInput struct {
	Month string `path:"month" pattern:"^[0-9]{4}-[0-9]{2}$" doc:"The month, YYYY-MM." example:"2026-10"`
	IfMatchHeader
	PreferHeader
	Body KPICountsIn
}

type kpiInputOutput struct {
	ETag    string `header:"ETag" doc:"The counts' version, for If-Match."`
	Applied string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body    KPICountsOut
}

type kpiInputsInput struct {
	PageQuery
	From string `query:"from" pattern:"^[0-9]{4}-[0-9]{2}$" doc:"The first month, YYYY-MM; left out, twelve months before to." example:"2026-01"`
	To   string `query:"to" pattern:"^[0-9]{4}-[0-9]{2}$" doc:"The last month, YYYY-MM; left out, the month of the organisation's today." example:"2026-10"`
}

// KPICountsList is a page of months' entered counts, earliest first.
type KPICountsList struct {
	Items      []KPICountsOut `json:"items" nullable:"false" doc:"The months with counts entered, earliest first."`
	NextCursor string         `json:"next_cursor,omitempty" doc:"The cursor of the next page; none on the last." example:"eyJhZnRlciI6IjIwMjYtMDUifQ"`
}

type kpiInputsOutput struct{ Body KPICountsList }

func kpiETag(version int) string { return `"` + strconv.Itoa(version) + `"` }

func registerKPIs(a *API) {
	register(a, authz.GetKpis, huma.Operation{
		OperationID: "getKpis", Method: http.MethodGet, Path: "/v1/kpis", Tags: []string{"Reports"},
		Summary: "Get the KPIs of a month",
		Description: "The workbook's KPI set for a month (FR-KPI-02), each with its definition, version, source, formula and date basis " +
			"(FR-KPI-03), its value for the month and the month before: booked and contracted MRR and ARR, revenue growth, margins, spend " +
			"by function, net burn, runway, ARPA, churn, CAC, LTV and its ratio, CAC payback, the burn multiple, the current and quick " +
			"ratios, working capital, debt/equity, DSO, DPO, headcount and revenue per employee. Ledger figures are read through the " +
			"statements at one ledger revision, which it returns (FR-RPT-08); customer counts and headcount are those a person " +
			"entered for the month (rev 0.161), and the KPIs that use them say so. Each value is rounded by RND-KPI; a KPI that " +
			"cannot be worked out has no value and says why, never 0. A month the books have no period for is REPORT-03, a later " +
			"revision REPORT-04. Read-only; no side effects.",
	}, []string{CodeInvalid, CodeNoPeriod, CodeLaterRevision}, func(ctx context.Context, in *kpisInput) (*kpisOutput, error) {
		p, _ := authn.From(ctx)
		var out KPIs
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			out, err = a.kpis(ctx, u, in)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &kpisOutput{Body: out}, nil
	})

	register(a, authz.ListKpiInputs, huma.Operation{
		OperationID: "listKpiInputs", Method: http.MethodGet, Path: "/v1/kpi-inputs", Tags: []string{"Reports"},
		Summary:     "List the KPIs' entered counts",
		Description: "The months' customer counts and headcount a person entered for the KPIs (FR-KPI-02, rev 0.161), earliest first, with who entered them and their version, a page at a time (FR-INT-10). Read-only; no side effects.",
	}, []string{CodeCursor, CodeInvalid}, func(ctx context.Context, in *kpiInputsInput) (*kpiInputsOutput, error) {
		p, _ := authn.From(ctx)
		out := KPICountsList{Items: []KPICountsOut{}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			to := in.To
			if to == "" {
				today, err := orgToday(ctx, a, u)
				if err != nil {
					return err
				}
				to = today.Format(statementMonthForm)
			}
			last, err := time.Parse(statementMonthForm, to)
			if err != nil {
				return invalid([]FieldProblem{{Code: CodeInvalid, Field: "to", Message: "is not a month", Fix: "Send the month as YYYY-MM"}})
			}
			first := last.AddDate(0, -12, 0)
			if in.From != "" {
				if first, err = time.Parse(statementMonthForm, in.From); err != nil || first.After(last) {
					return invalid([]FieldProblem{{Code: CodeInvalid, Field: "from", Message: "is not a month on or before to", Fix: "Send the first month as YYYY-MM"}})
				}
			}
			xs, err := u.KPIInputs().Range(ctx, first, last)
			if err != nil {
				return err
			}
			page, next, err := pageOf(xs, func(x storage.KPIInput) string { return x.Month.Format(statementMonthForm) }, in.PageQuery)
			if err != nil {
				return err
			}
			for _, x := range page {
				c, err := a.countsOut(ctx, u, x)
				if err != nil {
					return err
				}
				out.Items = append(out.Items, c)
			}
			out.NextCursor = next
			return nil
		})
		return &kpiInputsOutput{Body: out}, err
	})

	register(a, authz.SetKpiInput, huma.Operation{
		OperationID: "setKpiInput", Method: http.MethodPut, Path: "/v1/kpi-inputs/{month}", Tags: []string{"Reports"},
		Summary: "Enter a month's KPI counts",
		Description: "Enters a month's paying, new and lost customers (the three together, or none of them) and headcount, each a whole " +
			"number from zero, for the KPIs that need them until subscription data (FR-KPI-04) and the payroll link exist (FR-KPI-02; the " +
			"owner's decision of 29 Oct 2026, rev 0.161). A closed month's counts are corrected the same way: they are not ledger " +
			"figures. Side effects: the month's counts at their next version and its audit event, with the counts before and after. " +
			"Needs If-Match with the counts' ETag (\"0\" for a month without counts). Prefer: return=preview checks it and saves nothing.",
	}, []string{CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, storage.CodeVersion, storage.CodeKPIInput,
		storage.CodeRetry, storage.CodeLockTimeout},
		func(ctx context.Context, in *setKpiInputInput) (*kpiInputOutput, error) {
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			month, err := time.Parse(statementMonthForm, in.Month)
			if err != nil {
				return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "month", Message: "is not a month", Fix: "Send the month as YYYY-MM"}})
			}
			b := in.Body
			customers := []*int64{b.PayingCustomers, b.NewCustomers, b.LostCustomers}
			given := 0
			for _, c := range customers {
				if c != nil {
					given++
				}
			}
			var bad []FieldProblem
			if given != 0 && given != 3 {
				bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.paying_customers", Message: "comes with new_customers and lost_customers, or none of the three",
					Fix: "Send all three customer counts, or none of them"})
			}
			if given == 0 && b.Headcount == nil {
				bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body", Message: "has no counts", Fix: "Send the customer counts, the headcount, or both"})
			}
			if len(bad) > 0 {
				return nil, invalid(bad)
			}
			w, err := write(ctx, a, "", in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (KPICountsOut, error) {
				version := 0
				x, err := u.KPIInputs().Get(ctx, month)
				switch {
				case errors.Is(err, ports.ErrNotFound):
				case err != nil:
					return KPICountsOut{}, err
				default:
					version = x.Version
				}
				if err := checkIfMatch(in.IfMatch, kpiETag(version)); err != nil {
					return KPICountsOut{}, err
				}
				saved, err := u.KPIInputs().Set(ctx, month, storage.KPICounts{Paying: b.PayingCustomers, New: b.NewCustomers, Lost: b.LostCustomers, Headcount: b.Headcount},
					uploader(ctx), a.deps.Clock.Now(), version)
				if err != nil {
					return KPICountsOut{}, err
				}
				return a.countsOut(ctx, u, saved)
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &kpiInputOutput{Applied: applied(w), Body: w.Body}, nil
			}
			return &kpiInputOutput{ETag: kpiETag(w.Body.Version), Body: w.Body}, nil
		})
}
