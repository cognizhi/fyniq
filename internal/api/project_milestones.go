package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/projectbilling"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ milestones and retainer periods (FR-PRJ-10; p1-projects task 5.3)

// ProjectMilestone is a milestone of a fixed-fee project, or a period of a retainer.
type ProjectMilestone struct {
	ID         string `json:"id" format:"uuid" doc:"The milestone." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e91"`
	ProjectID  string `json:"project_id" format:"uuid" doc:"Its project." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e31"`
	Kind       string `json:"kind" enum:"milestone,retainer" doc:"milestone: a part of a fixed fee, invoiced once the customer has accepted it; retainer: a period of a retainer, invoiced without acceptance." example:"milestone"`
	Name       string `json:"name" doc:"What it is." example:"Design sign-off"`
	Amount     Money  `json:"amount" doc:"What it bills, in the project's currency."`
	DueOn      string `json:"due_on,omitempty" format:"date" doc:"When it is due." example:"2026-12-15"`
	AcceptedOn string `json:"accepted_on,omitempty" format:"date" doc:"The day the customer accepted it." example:"2026-12-10"`
	Version    int    `json:"version" doc:"For If-Match." example:"1"`
	InvoiceID  string `json:"invoice_id,omitempty" format:"uuid" doc:"The posted invoice that billed it; absent until one does, and again if that invoice is voided." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4ea1"`
	CanInvoice bool   `json:"can_invoice" doc:"true when an invoice can bill it now: accepted (a retainer period needs no acceptance) and not billed." example:"true"`
}

// RetainerBalance is where a retainer stands.
type RetainerBalance struct {
	Billed   Money `json:"billed" doc:"The retainer periods on posted invoices."`
	Drawn    Money `json:"drawn" doc:"The value of all the project's approved billable time at billing rates (RND-TIME per entry)."`
	Left     Money `json:"left" doc:"Billed less drawn; negative when time was worked beyond what was billed."`
	Unpriced int   `json:"unpriced_entries" doc:"Approved billable entries no billing rate covers; they are not in drawn." example:"0"`
}

// ProjectMilestones are a project's milestones, with the retainer's balance for a retainer.
type ProjectMilestones struct {
	Milestones []ProjectMilestone `json:"milestones" nullable:"false" doc:"Oldest first."`
	Retainer   *RetainerBalance   `json:"retainer,omitempty" doc:"For a retainer project: what it has billed, drawn and has left."`
	NextCursor string             `json:"next_cursor,omitempty" doc:"Pass it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjEwMCJ9"`
}

type milestoneListInput struct {
	ProjectPath
	PageQuery
}

type projectMilestonesOutput struct{ Body ProjectMilestones }

// ProjectMilestoneDraft is a milestone as entered.
type ProjectMilestoneDraft struct {
	ProjectID string `json:"project_id" format:"uuid" doc:"The fixed-fee project, or the retainer." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e31"`
	Name      string `json:"name" minLength:"1" maxLength:"100" doc:"What it is, such as Design sign-off, or the retainer's month." example:"Design sign-off"`
	Amount    string `json:"amount" pattern:"^(0|[1-9][0-9]{0,15})(\\.[0-9]{1,4})?$" doc:"What it bills, as a decimal in the project's currency, above zero." example:"15000.00"`
	DueOn     string `json:"due_on,omitempty" format:"date" doc:"When it is due." example:"2026-12-15"`
}

type createProjectMilestoneInput struct {
	KeyHeader
	PreferHeader
	Body ProjectMilestoneDraft
}

// ProjectMilestoneAcceptance is the customer's acceptance of a milestone.
type ProjectMilestoneAcceptance struct {
	AcceptedOn string `json:"accepted_on" format:"date" doc:"The day the customer accepted it, not in the future." example:"2026-12-10"`
}

type acceptProjectMilestoneInput struct {
	MilestonePath
	IfMatchHeader
	KeyHeader
	PreferHeader
	Body ProjectMilestoneAcceptance
}

// MilestonePath is a milestone's path.
type MilestonePath struct {
	MilestoneID string `path:"milestone_id" format:"uuid" doc:"The milestone's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e91"`
}

type projectMilestoneOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The milestone's version, for If-Match."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     ProjectMilestone
}

func milestoneOf(m storage.Milestone, p storage.Project) (ProjectMilestone, error) {
	cur, err := money.Lookup(p.Currency)
	if err != nil {
		return ProjectMilestone{}, err
	}
	amt, err := money.ParseStored(string(m.Amount), cur)
	if err != nil {
		return ProjectMilestone{}, err
	}
	o := ProjectMilestone{ID: m.ID.String(), ProjectID: m.ProjectID.String(), Kind: m.Kind, Name: m.Name, Amount: moneyOut(amt), Version: m.Version}
	if m.DueOn != nil {
		o.DueOn = m.DueOn.Format(time.DateOnly)
	}
	if m.AcceptedOn != nil {
		o.AcceptedOn = m.AcceptedOn.Format(time.DateOnly)
	}
	if m.InvoiceID != nil {
		o.InvoiceID = m.InvoiceID.String()
	}
	o.CanInvoice = m.InvoiceID == nil && (m.Kind == storage.MilestoneRetainer || m.AcceptedOn != nil)
	return o, nil
}

func registerProjectMilestones(a *API) {
	register(a, authz.ListProjectMilestones, huma.Operation{
		OperationID: "listProjectMilestones", Method: http.MethodGet, Path: "/v1/projects/{project_id}/milestones", Tags: []string{"Projects"},
		Summary: "A project's milestones or retainer periods",
		Description: "The milestones of a fixed-fee project, or the periods of a retainer, oldest first, each with whether the customer accepted it and the invoice that " +
			"billed it (FR-PRJ-10). A retainer shows what it has billed, what its approved billable time has drawn down at billing rates, and what is left. A person " +
			"who reads only the projects they manage gets 404 for another. Refused with PRJ-09 while Projects and time are switched off. Read-only; no side effects.",
	}, []string{CodeNotFound, CodeCursor, CodeProjectsOff}, func(ctx context.Context, in *milestoneListInput) (*projectMilestonesOutput, error) {
		id, err := parseID("project_id", in.ProjectID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		out := projectMilestonesOutput{Body: ProjectMilestones{Milestones: []ProjectMilestone{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			if err := requireProjects(ctx, a, u); err != nil {
				return err
			}
			pr, err := a.visibleProject(ctx, u, p, id)
			if err != nil {
				return err
			}
			ms, err := u.Billing().Milestones(ctx, id)
			if err != nil {
				return err
			}
			from, to, next, err := pageAt(len(ms), in.Cursor, in.PageSize)
			if err != nil {
				return err
			}
			out.Body.NextCursor = next
			for _, m := range ms[from:to] {
				x, err := milestoneOf(m, pr)
				if err != nil {
					return err
				}
				out.Body.Milestones = append(out.Body.Milestones, x)
			}
			if pr.BillingMethod == storage.BillingRetainer {
				r, err := projectbilling.RetainerOf(ctx, u, pr)
				if err != nil {
					return err
				}
				out.Body.Retainer = &RetainerBalance{Billed: moneyOut(r.Billed), Drawn: moneyOut(r.Drawn), Left: moneyOut(r.Left), Unpriced: r.Unpriced}
			}
			return nil
		})
		return &out, err
	})

	register(a, authz.CreateProjectMilestone, huma.Operation{
		OperationID: "createProjectMilestone", Method: http.MethodPost, Path: "/v1/project-milestones", Tags: []string{"Projects"}, DefaultStatus: http.StatusCreated,
		Summary: "Add a milestone or a retainer period",
		Description: "Adds a milestone to a fixed-fee project, or a period to a retainer: a name, an amount in the project's currency above zero and an optional due date " +
			"(FR-PRJ-10). Its amount and name never change; the customer's acceptance is recorded with acceptProjectMilestone. Refused with PRJ-15 on a project of another " +
			"billing method or a closed one, and PRJ-09 while Projects and time are switched off. Side effects: the milestone and its audit event under the project. " +
			"Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeInvalid, CodeProjectsOff, projectbilling.CodeMilestone, storage.CodeMilestone, CodeMalformed, CodeTooLarge, CodeMediaType,
		CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, storage.CodeRetry, storage.CodeLockTimeout}, money.CodeNotDecimal, money.CodePrecision, money.CodeRange),
		func(ctx context.Context, in *createProjectMilestoneInput) (*projectMilestoneOutput, error) {
			pid, err := parseID("body.project_id", in.Body.ProjectID)
			if err != nil {
				return nil, err
			}
			p, _ := authn.From(ctx)
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (ProjectMilestone, error) {
				if err := requireProjects(ctx, a, u); err != nil {
					return ProjectMilestone{}, err
				}
				pr, err := a.visibleProject(ctx, u, p, pid)
				if err != nil {
					return ProjectMilestone{}, err
				}
				kind := ""
				switch pr.BillingMethod {
				case storage.BillingFixedFee:
					kind = storage.MilestoneFixedFee
				case storage.BillingRetainer:
					kind = storage.MilestoneRetainer
				default:
					return ProjectMilestone{}, problem(projectbilling.CodeMilestone, "Only a fixed-fee project has milestones, and a retainer periods.", "Set the project's billing method first")
				}
				if pr.Status == storage.ProjectClosed {
					return ProjectMilestone{}, problem(projectbilling.CodeMilestone, "A closed project takes no new milestone.", "Reopen the project first")
				}
				cur, err := money.Lookup(pr.Currency)
				if err != nil {
					return ProjectMilestone{}, err
				}
				amt, err := money.Parse(in.Body.Amount, cur)
				if err != nil {
					return ProjectMilestone{}, invalid([]FieldProblem{fieldOf(err, "body.amount")})
				}
				if amt.Sign() <= 0 {
					return ProjectMilestone{}, invalid([]FieldProblem{{Code: projectbilling.CodeMilestone, Field: "body.amount", Message: "is not above zero", Fix: "Enter what the milestone bills"}})
				}
				name := strings.TrimSpace(in.Body.Name)
				if name == "" {
					return ProjectMilestone{}, invalid([]FieldProblem{{Code: projectbilling.CodeMilestone, Field: "body.name", Message: "is empty", Fix: "Say what the milestone is"}})
				}
				nm := storage.NewMilestone{ProjectID: pid, Kind: kind, Name: name, Amount: storage.Decimal(amt.String())}
				if in.Body.DueOn != "" {
					d, err := time.Parse(time.DateOnly, in.Body.DueOn)
					if err != nil {
						return ProjectMilestone{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.due_on", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"}})
					}
					nm.DueOn = &d
				}
				m, err := u.Billing().CreateMilestone(ctx, nm)
				if err != nil {
					return ProjectMilestone{}, err
				}
				return milestoneOf(m, pr)
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &projectMilestoneOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &projectMilestoneOutput{Status: w.Status, ETag: etag(w.Body.Version), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})

	register(a, authz.AcceptProjectMilestone, huma.Operation{
		OperationID: "acceptProjectMilestone", Method: http.MethodPost, Path: "/v1/project-milestones/{milestone_id}/accept", Tags: []string{"Projects"},
		Summary: "Record the customer's acceptance of a milestone",
		Description: "Records the day the customer accepted a fixed-fee milestone, once, which lets an invoice bill it (FR-PRJ-10). A retainer period needs no acceptance " +
			"(PRJ-15). Needs If-Match with the milestone's ETag. Side effects: its next version and its audit event under the project. Refused with PRJ-09 while Projects " +
			"and time are switched off. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, []string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeInvalid, CodeProjectsOff, projectbilling.CodeMilestone, storage.CodeMilestone, storage.CodeVersion, CodeMalformed,
		CodeTooLarge, CodeMediaType, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, storage.CodeRetry, storage.CodeLockTimeout},
		func(ctx context.Context, in *acceptProjectMilestoneInput) (*projectMilestoneOutput, error) {
			id, err := parseID("milestone_id", in.MilestoneID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			day, err := time.Parse(time.DateOnly, in.Body.AcceptedOn)
			if err != nil {
				return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.accepted_on", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"}})
			}
			p, _ := authn.From(ctx)
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (ProjectMilestone, error) {
				if err := requireProjects(ctx, a, u); err != nil {
					return ProjectMilestone{}, err
				}
				m, err := u.Billing().GetMilestone(ctx, id)
				if err != nil {
					return ProjectMilestone{}, err
				}
				pr, err := a.visibleProject(ctx, u, p, m.ProjectID)
				if err != nil {
					return ProjectMilestone{}, err
				}
				if err := checkIfMatch(in.IfMatch, etag(m.Version)); err != nil {
					return ProjectMilestone{}, err
				}
				today, err := orgToday(ctx, a, u)
				if err != nil {
					return ProjectMilestone{}, err
				}
				switch {
				case m.Kind == storage.MilestoneRetainer:
					return ProjectMilestone{}, problem(projectbilling.CodeMilestone, "A retainer period needs no acceptance.", "Draft its invoice")
				case m.AcceptedOn != nil:
					return ProjectMilestone{}, problem(projectbilling.CodeMilestone, "The milestone was accepted already.", "Draft its invoice")
				case day.After(today):
					return ProjectMilestone{}, invalid([]FieldProblem{{Code: projectbilling.CodeMilestone, Field: "body.accepted_on", Message: "is in the future", Fix: "Enter the day the customer accepted it"}})
				}
				after, err := u.Billing().AcceptMilestone(ctx, id, m.Version, day)
				if err != nil {
					return ProjectMilestone{}, err
				}
				return milestoneOf(after, pr)
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &projectMilestoneOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &projectMilestoneOutput{Status: w.Status, ETag: etag(w.Body.Version), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})
}
