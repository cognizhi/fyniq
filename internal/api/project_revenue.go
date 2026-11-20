package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/projectrevenue"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ revenue over time for fixed-fee projects (FR-PRJ-11; p2-customer-portal-subscriptions task 4.1)

// ProjectRevenueElection is the accountant's choice for a fixed-fee project.
type ProjectRevenueElection struct {
	Method        string `json:"method" enum:"milestone,over_time" doc:"milestone: revenue is recognised as each milestone is invoiced (the default); over_time: by cost-to-cost progress, through a monthly journal proposed for approval." example:"over_time"`
	EstimatedCost *Money `json:"estimated_cost,omitempty" doc:"The estimated total cost of the project, in its currency; set for over_time."`
	ExcludedCost  *Money `json:"excluded_cost,omitempty" doc:"The running total of the project's posted costs left out of its progress because they do not show it (SFRS(I) 15 B19), with its reason; absent when none."`
	ExcludedWhy   string `json:"excluded_reason,omitempty" doc:"Why they are left out." example:"Rework of a drawing the customer had already approved"`
	Version       int    `json:"version" doc:"The election's version, for If-Match; 0 when none was made." example:"1"`
}

// ProjectRevenueRun is a month's proposed journal.
type ProjectRevenueRun struct {
	Month      string `json:"month" format:"date" doc:"The month's first day." example:"2026-10-01"`
	JournalID  string `json:"journal_id" format:"uuid" doc:"The draft journal proposed for approval; open it under Journals." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5b01"`
	Adjustment Money  `json:"adjustment" doc:"The revenue it books; negative when invoiced ahead of progress."`
	Earned     Money  `json:"earned" doc:"The revenue earned to date after it."`
}

// ProjectRevenue is a project's revenue over time for a month.
type ProjectRevenue struct {
	Election ProjectRevenueElection  `json:"election"`
	Month    string                  `json:"month,omitempty" format:"date" doc:"The month the figures are for." example:"2026-10-01"`
	Position *ProjectRevenuePosition `json:"position,omitempty" doc:"The figures as the month's journal would book them; absent when the project is not elected or cannot be worked out (see problem)."`
	Problem  string                  `json:"problem,omitempty" doc:"Why the figures are not shown, in plain words." example:"The project has no milestones, so it has no fee to recognise."`
	Runs     []ProjectRevenueRun     `json:"runs" nullable:"false" doc:"The months already proposed, oldest first."`
}

// ProjectRevenuePosition is the working of one month.
type ProjectRevenuePosition struct {
	Fee         Money  `json:"fee" doc:"The fixed fee: the total of the project's milestones."`
	Estimate    Money  `json:"estimated_cost" doc:"The estimated total cost."`
	CostToDate  Money  `json:"cost_to_date" doc:"What progress is measured by: the project's costs posted to the end of the month (direct and payroll-tagged) less those left out as not showing progress."`
	CostPosted  Money  `json:"costs_posted" doc:"The project's costs posted to the end of the month, before any are left out."`
	CostLeftOut Money  `json:"costs_left_out" doc:"The part of them the accountant left out of progress, with the reason on the election."`
	Progress    string `json:"progress" doc:"Cost to date over the estimate as a percentage, at most 100.0 (RND-PERCENT)." example:"50.0"`
	Overrun     bool   `json:"overrun" doc:"The cost has reached the estimate: the whole fee is earned and the estimate needs revisiting." example:"false"`
	Earned      Money  `json:"earned" doc:"The revenue earned to date: RND-RECOGNISE of the fee by progress."`
	Invoiced    Money  `json:"invoiced" doc:"The revenue invoiced to the project to date, net of GST: the revenue the ledger holds less the earlier journals' adjustments."`
	Held        Money  `json:"held" doc:"The revenue the ledger holds for the project to date."`
	Adjustment  Money  `json:"adjustment" doc:"The revenue this month's journal books: earned less held; negative when invoiced ahead of progress."`
	Asset       Money  `json:"contract_asset" doc:"The contract asset after the journal: earned and not yet invoiced."`
	Liability   Money  `json:"contract_liability" doc:"The contract liability after the journal: invoiced and not yet earned."`
	NothingToDo bool   `json:"nothing_to_propose" doc:"The journal would move nothing." example:"false"`
}

func revMoney(m money.Money) Money { return Money{Amount: m.String(), Currency: m.Currency().Code} }

func electionOf(e storage.ProjectRevenueElection, currency string) ProjectRevenueElection {
	out := ProjectRevenueElection{Method: e.Method, Version: e.Version}
	if e.EstimatedCost != nil {
		m := moneyOf(*e.EstimatedCost, currency)
		out.EstimatedCost = &m
	}
	if string(e.ExcludedCost) != "" && !isZeroDecimal(e.ExcludedCost) {
		m := moneyOf(e.ExcludedCost, currency)
		out.ExcludedCost, out.ExcludedWhy = &m, e.ExcludedReason
	}
	return out
}

func isZeroDecimal(d storage.Decimal) bool {
	v, _, err := apd.NewFromString(string(d))
	return err == nil && v.IsZero()
}

func revenueRunsOf(rs []storage.ProjectRevenueRun, currency string) []ProjectRevenueRun {
	out := []ProjectRevenueRun{}
	for _, r := range rs {
		out = append(out, ProjectRevenueRun{Month: dateOf(r.Month), JournalID: r.JournalID.String(), Adjustment: moneyOf(r.Adjustment, currency), Earned: moneyOf(r.Earned, currency)})
	}
	return out
}

type getProjectRevenueInput struct {
	ProjectPath
	Month string `query:"month" format:"date" doc:"The month's first day; this month when left out." example:"2026-10-01"`
}

type projectRevenueOutput struct {
	Status int
	ETag   string `header:"ETag" doc:"The election's version, for If-Match."`
	Body   ProjectRevenue
}

type setProjectRevenueInput struct {
	ProjectPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body struct {
		Method        string `json:"method" enum:"milestone,over_time" doc:"How the project recognises its revenue." example:"over_time"`
		EstimatedCost string `json:"estimated_cost,omitempty" pattern:"^(0|[1-9][0-9]{0,15})(\\.[0-9]{1,4})?$" doc:"The estimated total cost in the project's currency, above nothing; needed for over_time." example:"6000.00"`
		ExcludedCost  string `json:"excluded_cost,omitempty" pattern:"^(0|[1-9][0-9]{0,15})(\\.[0-9]{1,4})?$" doc:"The running total of the project's posted costs to leave out of its progress because they do not show it (waste, rework, a cost out of proportion to the work: SFRS(I) 15 B19); over_time only, with excluded_reason; left out for none." example:"250.00"`
		ExcludedWhy   string `json:"excluded_reason,omitempty" maxLength:"500" doc:"Why those costs do not show progress; needed with excluded_cost above nothing." example:"Rework of a drawing the customer had already approved"`
	}
}

type proposeProjectRevenueInput struct {
	ProjectPath
	KeyHeader
	PreferHeader
	Body struct {
		Month string `json:"month" format:"date" doc:"The month's first day: after the last month proposed, up to this month." example:"2026-10-01"`
	}
}

type projectRevenueRunOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     struct {
		Run     ProjectRevenueRun      `json:"run"`
		Figures ProjectRevenuePosition `json:"figures"`
	}
}

func positionOf(r projectrevenue.Report) *ProjectRevenuePosition {
	p := r.Position
	return &ProjectRevenuePosition{Fee: revMoney(r.Fee), Estimate: revMoney(r.Estimate), CostToDate: revMoney(r.CostToDate), CostPosted: revMoney(r.CostPosted), CostLeftOut: revMoney(r.Excluded), Progress: p.Progress, Overrun: p.Overrun, Earned: revMoney(p.Earned),
		Invoiced: revMoney(r.Billed), Held: revMoney(r.Held), Adjustment: revMoney(p.Adjustment), Asset: revMoney(p.Asset), Liability: revMoney(p.Liability),
		NothingToDo: p.AssetChange.IsZero() && p.LiabilityChange.IsZero()}
}

func registerProjectRevenue(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeNotFound, CodeProjectsOff}, locks...)
	revCodes := []string{projectrevenue.CodeProject, projectrevenue.CodeEstimate, projectrevenue.CodeMonth, projectrevenue.CodeAccounts, projectrevenue.CodeNothing}

	register(a, authz.GetProjectRevenue, huma.Operation{
		OperationID: "getProjectRevenue", Method: http.MethodGet, Path: "/v1/projects/{project_id}/revenue", Tags: []string{"Projects"},
		Summary: "Read a fixed-fee project's revenue over time for a month",
		Description: "The project's election (revenue as each milestone is invoiced, or over time) and, for an over-time project, the month's figures as the journal would book them " +
			"(FR-PRJ-11): the fee (the milestones' total), the cost to date, progress by cost-to-cost, the revenue earned (RND-RECOGNISE), what the ledger holds, the adjustment and the " +
			"contract asset or liability. The figures are read from the posted ledger; why they cannot be shown is in `problem`. Read-only.",
	}, []string{CodeNotFound, CodeProjectsOff, CodeInvalid}, func(ctx context.Context, in *getProjectRevenueInput) (*projectRevenueOutput, error) {
		pid, err := parseID("project_id", in.ProjectID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out projectRevenueOutput
		out.Status = http.StatusOK
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			if err := requireProjects(ctx, a, u); err != nil {
				return err
			}
			pr, err := a.visibleProject(ctx, u, p, pid)
			if err != nil {
				return err
			}
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return err
			}
			month := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)
			if in.Month != "" {
				if month, err = time.Parse(time.DateOnly, in.Month); err != nil {
					return invalid([]FieldProblem{{Code: CodeInvalid, Field: "month", Message: "is not a date", Fix: "Use the first day of a month, such as 2026-10-01"}})
				}
			}
			out.Body = ProjectRevenue{Election: ProjectRevenueElection{Method: storage.RevenueMilestone}, Runs: []ProjectRevenueRun{}}
			el, err := u.ProjectRevenue().Election(ctx, pid)
			if err != nil && err != ports.ErrNotFound {
				return err
			}
			if err == nil {
				out.Body.Election = electionOf(el, pr.Currency)
				out.ETag = etag(el.Version)
			}
			runs, err := u.ProjectRevenue().Runs(ctx, pid)
			if err != nil {
				return err
			}
			out.Body.Runs = revenueRunsOf(runs, pr.Currency)
			if out.Body.Election.Method != storage.RevenueOverTime {
				return nil
			}
			r, err := projectrevenue.Of(ctx, u, pid, month, today)
			var pe *projectrevenue.Error
			if asProjectRevenueError(err, &pe) {
				out.Body.Problem = pe.Message + " " + pe.Fix + "."
				return nil
			}
			if err != nil {
				return err
			}
			out.Body.Month, out.Body.Position = dateOf(month), positionOf(r)
			return nil
		})
		return &out, err
	})

	register(a, authz.SetProjectRevenueElection, huma.Operation{
		OperationID: "setProjectRevenueElection", Method: http.MethodPut, Path: "/v1/projects/{project_id}/revenue-election", Tags: []string{"Projects"},
		Summary: "Elect a fixed-fee project's revenue over time, or back to milestones",
		Description: "The accountant's choice for a fixed-fee project in the base currency (FR-PRJ-11): over_time with the estimated total cost above nothing, or milestone, which is the default. " +
			"Changing it needs If-Match with the election's ETag; the first election needs none. It posts nothing; the month's journal is proposed with proposeProjectRevenueJournal. " +
			"Side effects: the election and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append(append([]string{projectrevenue.CodeProject, projectrevenue.CodeEstimate, CodeStale}, writes...), CodeIfMatchRequired),
		func(ctx context.Context, in *setProjectRevenueInput) (*projectRevenueOutput, error) {
			pid, err := parseID("project_id", in.ProjectID)
			if err != nil {
				return nil, err
			}
			p, _ := authn.From(ctx)
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (ProjectRevenue, error) {
				if err := requireProjects(ctx, a, u); err != nil {
					return ProjectRevenue{}, err
				}
				pr, err := a.visibleProject(ctx, u, p, pid)
				if err != nil {
					return ProjectRevenue{}, err
				}
				org, err := u.Organisation(ctx)
				if err != nil {
					return ProjectRevenue{}, err
				}
				if pr.BillingMethod != storage.BillingFixedFee || pr.Currency != org.BaseCurrency {
					return ProjectRevenue{}, problem(projectrevenue.CodeProject, "Revenue over time is for a fixed-fee project in the organisation's base currency "+org.BaseCurrency+".",
						"Bill the project as a fixed fee in "+org.BaseCurrency+" first")
				}
				var est *storage.Decimal
				switch in.Body.Method {
				case storage.RevenueOverTime:
					cur, err := money.Lookup(pr.Currency)
					if err != nil {
						return ProjectRevenue{}, err
					}
					m, err := money.Parse(strings.TrimSpace(in.Body.EstimatedCost), cur)
					if err != nil || m.Sign() <= 0 {
						return ProjectRevenue{}, invalid([]FieldProblem{{Code: projectrevenue.CodeEstimate, Field: "body.estimated_cost", Message: "is not an amount above nothing in " + cur.Code,
							Fix: "Estimate the project's total cost, such as 6000.00"}})
					}
					d := storage.Decimal(m.String())
					est = &d
				case storage.RevenueMilestone:
					if strings.TrimSpace(in.Body.EstimatedCost) != "" || strings.TrimSpace(in.Body.ExcludedCost) != "" {
						return ProjectRevenue{}, invalid([]FieldProblem{{Code: projectrevenue.CodeEstimate, Field: "body.estimated_cost", Message: "belongs to revenue over time", Fix: "Leave it out for milestone"}})
					}
				default:
					return ProjectRevenue{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.method", Message: "is milestone or over_time", Fix: "Choose one of them"}})
				}
				excluded, why := storage.Decimal("0"), ""
				if txt := strings.TrimSpace(in.Body.ExcludedCost); txt != "" && in.Body.Method == storage.RevenueOverTime {
					cur, err := money.Lookup(pr.Currency)
					if err != nil {
						return ProjectRevenue{}, err
					}
					m, err := money.Parse(txt, cur)
					if err != nil || m.Sign() < 0 {
						return ProjectRevenue{}, invalid([]FieldProblem{{Code: projectrevenue.CodeEstimate, Field: "body.excluded_cost", Message: "is not an amount of zero or more in " + cur.Code, Fix: "Enter what to leave out, such as 250.00, or leave it out"}})
					}
					excluded = storage.Decimal(m.String())
					if !m.IsZero() {
						if why = strings.TrimSpace(in.Body.ExcludedWhy); why == "" {
							return ProjectRevenue{}, invalid([]FieldProblem{{Code: projectrevenue.CodeEstimate, Field: "body.excluded_reason", Message: "is needed with costs left out of progress", Fix: "Say why those costs do not show progress"}})
						}
					}
				}
				version := 0
				if cur, err := u.ProjectRevenue().Election(ctx, pid); err == nil {
					if err := checkIfMatch(in.IfMatch, etag(cur.Version)); err != nil {
						return ProjectRevenue{}, err
					}
					version = cur.Version
				} else if err != ports.ErrNotFound {
					return ProjectRevenue{}, err
				}
				el, err := u.ProjectRevenue().Elect(ctx, pid, version, in.Body.Method, est, excluded, why, a.deps.Clock.Now())
				if err != nil {
					return ProjectRevenue{}, err
				}
				runs, err := u.ProjectRevenue().Runs(ctx, pid)
				return ProjectRevenue{Election: electionOf(el, pr.Currency), Runs: revenueRunsOf(runs, pr.Currency)}, err
			})
			if err != nil {
				return nil, err
			}
			return &projectRevenueOutput{Status: w.Status, ETag: etag(w.Body.Election.Version), Body: w.Body}, nil
		})

	register(a, authz.ProposeProjectRevenueJournal, exempt(huma.Operation{
		OperationID: "proposeProjectRevenueJournal", Method: http.MethodPost, Path: "/v1/projects/{project_id}/revenue-journals", Tags: []string{"Projects"}, DefaultStatus: http.StatusCreated,
		Summary: "Propose a month's revenue journal for a project elected for revenue over time",
		Description: "Makes the month's journal as a draft for a person to approve and post (FR-PRJ-11): Dr or Cr the contract asset and the contract liability by what the position moves them by " +
			"and the opposite on the revenue account, every line carrying the project; the accounts are set under organisation settings. The month is the first day of a month up to this one and " +
			"after the last month proposed, each earlier journal being posted; a month is proposed once and a journal that would move nothing is refused. The draft is approved and posted " +
			"like any journal. Side effects: the draft journal, the run recording it, audit events. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, "If-Match", "It makes a new journal and a run of its own and changes nothing of the project; a month is proposed once (FR-PRJ-11)."), append(append([]string{}, revCodes...), writes...), func(ctx context.Context, in *proposeProjectRevenueInput) (*projectRevenueRunOutput, error) {
		pid, err := parseID("project_id", in.ProjectID)
		if err != nil {
			return nil, err
		}
		month, err := time.Parse(time.DateOnly, in.Body.Month)
		if err != nil {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.month", Message: "is not a date", Fix: "Use the first day of a month, such as 2026-10-01"}})
		}
		p, _ := authn.From(ctx)
		type made struct {
			Run     ProjectRevenueRun
			Figures ProjectRevenuePosition
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (made, error) {
			if err := requireProjects(ctx, a, u); err != nil {
				return made{}, err
			}
			if _, err := a.visibleProject(ctx, u, p, pid); err != nil {
				return made{}, err
			}
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return made{}, err
			}
			r, doc, err := projectrevenue.Propose(ctx, u, pid, month, today, a.deps.Clock.Now())
			if err != nil {
				return made{}, err
			}
			return made{Run: ProjectRevenueRun{Month: dateOf(month), JournalID: doc.ID.String(), Adjustment: revMoney(r.Position.Adjustment), Earned: revMoney(r.Position.Earned)}, Figures: *positionOf(r)}, nil
		})
		if err != nil {
			return nil, err
		}
		out := &projectRevenueRunOutput{Status: w.Status, Applied: applied(w), Replayed: replayedHeader(w.Replayed)}
		out.Body.Run, out.Body.Figures = w.Body.Run, w.Body.Figures
		return out, nil
	})
}

func asProjectRevenueError(err error, target **projectrevenue.Error) bool {
	pe, ok := err.(*projectrevenue.Error)
	if ok {
		*target = pe
	}
	return ok
}
