package api

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	controls "github.com/cognizhi/fyniq/internal/domain/compliance"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// BreachView is a personal-data breach with its PDPA timers and steps (CMP-PD-05, rev 0.244).
type BreachView struct {
	ID              string           `json:"id" format:"uuid" doc:"The breach." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	Title           string           `json:"title" doc:"What happened, in a few words." example:"A payroll file emailed to the wrong person"`
	Description     string           `json:"description" doc:"What happened." example:"The October payroll file went to a supplier's address."`
	AwareAt         string           `json:"aware_at" format:"date-time" doc:"When the company became aware of it: the assessment's 30 days run from it." example:"2026-11-09T02:00:00Z"`
	Individuals     int              `json:"individuals" doc:"How many people, as best known." example:"12"`
	Classes         []BreachClass    `json:"classes" nullable:"false" doc:"The kinds of data involved."`
	Prescribed      bool             `json:"prescribed" doc:"true when the data is prescribed: a name or identification number with pay, card or bank account numbers, or an account's identifier with its password. Such a breach is deemed to cause significant harm, so it is notifiable." example:"true"`
	AssessmentDueOn string           `json:"assessment_due_on" format:"date" doc:"The last day to assess it: 30 days after awareness, in Singapore." example:"2026-12-09"`
	AssessmentLate  bool             `json:"assessment_overdue" doc:"true when it is not assessed and that day has passed." example:"false"`
	Notifiable      *bool            `json:"notifiable,omitempty" doc:"Whether it was decided notifiable; absent before the decision." example:"true"`
	PDPCDueOn       string           `json:"pdpc_due_on,omitempty" format:"date" doc:"Once decided notifiable: the last day to notify the PDPC, 3 calendar days after the decision." example:"2026-11-12"`
	PDPCLate        bool             `json:"pdpc_overdue" doc:"true when the PDPC is not notified and that day has passed." example:"false"`
	Status          string           `json:"status" enum:"recorded,assessed,decided,pdpc_notified,individuals_notified,contained,closed" doc:"Its latest step." example:"decided"`
	Steps           []BreachStepView `json:"steps" nullable:"false" doc:"Its steps, oldest first."`
}

// BreachStepView is a breach's step.
type BreachStepView struct {
	Step       string `json:"step" enum:"assessed,decided,pdpc_notified,individuals_notified,contained,closed" doc:"The step." example:"decided"`
	At         string `json:"at" format:"date-time" doc:"When." example:"2026-11-10T02:00:00Z"`
	By         string `json:"by" doc:"Who, as records name actors." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	Note       string `json:"note,omitempty" doc:"What was found or done." example:"Significant harm likely: pay and names."`
	Notifiable *bool  `json:"notifiable,omitempty" doc:"decided's: whether it is notifiable." example:"true"`
	Reference  string `json:"reference,omitempty" doc:"pdpc_notified's: the PDPC's reference." example:"DBN-2026-0123"`
}

// BreachInput records a breach.
type BreachInput struct {
	Title       string        `json:"title" minLength:"1" maxLength:"200" doc:"What happened, in a few words." example:"A payroll file emailed to the wrong person"`
	Description string        `json:"description" minLength:"1" maxLength:"8000" doc:"What happened, as known now." example:"The October payroll file went to a supplier's address."`
	AwareAt     string        `json:"aware_at,omitempty" format:"date-time" doc:"When the company became aware of it; left out, now. Never later than now." example:"2026-11-09T02:00:00Z"`
	Individuals int           `json:"individuals" minimum:"0" doc:"How many people, as best known." example:"12"`
	Classes     []BreachClass `json:"classes" minItems:"1" doc:"The kinds of data involved."`
}

// BreachStepInput adds a breach's step.
type BreachStepInput struct {
	Step       string `json:"step" enum:"assessed,decided,pdpc_notified,individuals_notified,contained,closed" doc:"The step." example:"decided"`
	Note       string `json:"note,omitempty" maxLength:"4000" doc:"What was found or done." example:"Significant harm likely."`
	Notifiable *bool  `json:"notifiable,omitempty" doc:"With decided: whether it is notifiable." example:"true"`
	Reference  string `json:"reference,omitempty" maxLength:"200" doc:"With pdpc_notified: the PDPC's reference." example:"DBN-2026-0123"`
}

// BreachExerciseView is a tabletop exercise of the breach runbook (CTL-DP-06).
type BreachExerciseView struct {
	ID         string `json:"id" format:"uuid" doc:"The exercise." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	HeldOn     string `json:"held_on" format:"date" doc:"When it was held." example:"2026-11-09"`
	Scenario   string `json:"scenario" doc:"The breach it rehearsed." example:"A laptop with the payroll export is stolen."`
	Findings   string `json:"findings" doc:"What it found and what changed." example:"The runbook now names who calls the PDPC."`
	RecordedBy string `json:"recorded_by" doc:"Who recorded it, as records name actors." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
}

// BreachExerciseInput records a tabletop exercise.
type BreachExerciseInput struct {
	HeldOn   string `json:"held_on" format:"date" doc:"When it was held, today or earlier." example:"2026-11-09"`
	Scenario string `json:"scenario" minLength:"1" maxLength:"4000" doc:"The breach it rehearsed." example:"A laptop with the payroll export is stolen."`
	Findings string `json:"findings" minLength:"1" maxLength:"8000" doc:"What it found and what changed." example:"The runbook now names who calls the PDPC."`
}

type (
	breachesOutput struct {
		Body struct {
			Items []BreachView `json:"items" nullable:"false" doc:"The breaches, oldest first."`
		}
	}
	// BreachPath names a breach (exported: huma reads only an exported embedded struct).
	BreachPath struct {
		BreachID string `path:"breach_id" format:"uuid" doc:"The breach." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	}
	breachCreateInput struct {
		KeyHeader
		PreferHeader
		Body BreachInput
	}
	breachStepInput struct {
		BreachPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body BreachStepInput
	}
	breachOutput struct {
		Status   int
		ETag     string `header:"ETag" doc:"The breach's steps, for If-Match."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     BreachView
	}
	exercisesOutput struct {
		Body struct {
			Items []BreachExerciseView `json:"items" nullable:"false" doc:"The tabletop exercises, oldest first."`
		}
	}
	exerciseInput struct {
		KeyHeader
		PreferHeader
		Body BreachExerciseInput
	}
	exerciseOutput struct {
		Status   int
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     BreachExerciseView
	}
)

// breachClasses are the kinds of data a breach may involve.
var breachClasses = []string{"names", "identification_numbers", "pay", "bank_accounts", "card_numbers", "credentials", "contact_details", "other"}

// BreachClass is a kind of data a breach involves.
type BreachClass string

// Schema implements huma.SchemaProvider: one of breachClasses, with an example.
func (BreachClass) Schema(huma.Registry) *huma.Schema {
	var e []any
	for _, c := range breachClasses {
		e = append(e, c)
	}
	return &huma.Schema{Type: "string", Enum: e, Description: "A kind of data: a name or identification number with pay, card or bank account numbers, or credentials, is prescribed.",
		Examples: []any{"pay"}}
}

func classesOut(cs []string) []BreachClass {
	out := make([]BreachClass, 0, len(cs))
	for _, c := range cs {
		out = append(out, BreachClass(c))
	}
	return out
}

func breachETag(v BreachView) string { return etag("s" + strconv.Itoa(len(v.Steps))) }

func breachView(b storage.Breach, today time.Time) BreachView {
	v := BreachView{ID: b.ID.String(), Title: b.Title, Description: b.Description, AwareAt: b.AwareAt.UTC().Format(time.RFC3339), Individuals: b.Individuals,
		Classes: classesOut(b.Classes), Prescribed: b.Prescribed, AssessmentDueOn: b.AssessmentDueOn.Format(time.DateOnly), Status: "recorded", Steps: []BreachStepView{}}
	for _, s := range b.Steps {
		v.Steps = append(v.Steps, BreachStepView{Step: s.Step, At: s.At.UTC().Format(time.RFC3339), By: s.By, Note: s.Note, Notifiable: s.Notifiable, Reference: s.Reference})
		v.Status = s.Step
		if s.Step == "decided" {
			v.Notifiable = s.Notifiable
			if s.PDPCDueOn != nil {
				v.PDPCDueOn = s.PDPCDueOn.Format(time.DateOnly)
			}
		}
	}
	assessment, pdpc := controls.BreachTimers(b)
	v.AssessmentLate = assessment != nil && today.After(*assessment)
	v.PDPCLate = pdpc != nil && today.After(*pdpc)
	return v
}

func (a *API) oneBreach(ctx context.Context, u storage.UnitOfWork, id string) (storage.Breach, error) {
	all, err := u.Compliance().Breaches(ctx)
	if err != nil {
		return storage.Breach{}, err
	}
	i := slices.IndexFunc(all, func(b storage.Breach) bool { return b.ID.String() == id })
	if i < 0 {
		return storage.Breach{}, problem(CodeNotFound, "There is no breach "+id+".", "Use an ID from listBreaches.")
	}
	return all[i], nil
}

// tellBreach sends the critical breach notice to those who read the PDPA registers: the DPO, the Owner and the Auditor.
func (a *API) tellBreach(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, title string) error {
	if a.deps.Outbox == nil {
		return nil
	}
	p, _ := authn.From(ctx)
	_, err := notify.Emit(ctx, *a.deps.Outbox, u, p.Organisation, notify.Event{Type: notify.BreachType, RecordType: "pd_breach", RecordID: &id, Title: title,
		Link: "/compliance/data-protection", Audience: notify.Audience{Capability: string(authz.DataProtection), Need: "V"}, At: a.deps.Clock.Now()})
	return err
}

func registerBreaches(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid,
		storage.CodeBreach}, locks...)
	answer := func(w written[BreachView]) *breachOutput {
		if w.Previewed {
			return &breachOutput{Status: w.Status, Applied: applied(w), Body: w.Body}
		}
		return &breachOutput{Status: w.Status, ETag: breachETag(w.Body), Replayed: replayedHeader(w.Replayed), Body: w.Body}
	}

	register(a, authz.ListBreaches, exempt(huma.Operation{
		OperationID: "listBreaches", Method: http.MethodGet, Path: "/v1/compliance/breaches", Tags: []string{"Compliance"},
		Summary: "List the breach register",
		Description: "Every personal-data breach (CMP-PD-05) with its PDPA timers: the assessment, due 30 days after awareness, and, " +
			"once decided notifiable, the PDPC's notification, due 3 calendar days after the decision; whether its data is prescribed; " +
			"and its steps. Read-only; no side effects.",
	}, "cursor", "A company records few breaches, read whole as one register."), locks,
		func(ctx context.Context, _ *struct{}) (*breachesOutput, error) {
			p, _ := authn.From(ctx)
			out := &breachesOutput{}
			out.Body.Items = []BreachView{}
			err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				today, err := orgToday(ctx, a, u)
				if err != nil {
					return err
				}
				all, err := u.Compliance().Breaches(ctx)
				for _, b := range all {
					out.Body.Items = append(out.Body.Items, breachView(b, today))
				}
				return err
			})
			return out, err
		})

	register(a, authz.RecordBreach, huma.Operation{
		OperationID: "recordBreach", Method: http.MethodPost, Path: "/v1/compliance/breaches", Tags: []string{"Compliance"},
		Summary:     "Record a personal-data breach",
		Description: "Records a breach, or a suspected one, as soon as the company becomes aware of it (CMP-PD-05): its assessment is due 30 days on. Fyniq works out whether its data is prescribed, which makes it notifiable. Side effects: the breach, the audit event breach.recorded, a critical notice to the Compliance officer, the Owner and the Auditor. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, writes, func(ctx context.Context, in *breachCreateInput) (*breachOutput, error) {
		now := a.deps.Clock.Now()
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (BreachView, error) {
			aware := now
			var bad []FieldProblem
			if in.Body.AwareAt != "" {
				t, err := time.Parse(time.RFC3339, in.Body.AwareAt)
				if err != nil || t.After(now) {
					bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.aware_at", Message: "is not a time up to now", Fix: "Send when the company became aware of it, RFC 3339"})
				}
				aware = t
			}
			var given []string
			for _, c := range in.Body.Classes {
				given = append(given, string(c))
			}
			classes := slices.Compact(slices.Sorted(slices.Values(given)))
			for _, c := range classes {
				if !slices.Contains(breachClasses, c) {
					bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.classes", Message: fmt.Sprintf("%q is not a kind of data", c),
						Fix: "Use " + strings.Join(breachClasses, ", ")})
				}
			}
			if len(bad) > 0 {
				return BreachView{}, invalid(bad)
			}
			b, err := u.Compliance().RecordBreach(ctx, storage.Breach{Title: strings.TrimSpace(in.Body.Title), Description: strings.TrimSpace(in.Body.Description),
				AwareAt: aware, Individuals: in.Body.Individuals, Classes: classes, RecordedBy: uploader(ctx), RecordedAt: now})
			if err != nil {
				return BreachView{}, err
			}
			if err := a.tellBreach(ctx, u, b.ID, fmt.Sprintf("A personal-data breach was recorded: %s. Assess it by %s", b.Title, b.AssessmentDueOn.Format("2 Jan 2006"))); err != nil {
				return BreachView{}, err
			}
			today, err := orgToday(ctx, a, u)
			return breachView(b, today), err
		})
		if err != nil {
			return nil, err
		}
		return answer(w), nil
	})

	register(a, authz.StepBreach, huma.Operation{
		OperationID: "stepBreach", Method: http.MethodPost, Path: "/v1/compliance/breaches/{breach_id}/steps", Tags: []string{"Compliance"},
		Summary:     "Add a breach's next step",
		Description: "Records that a breach was assessed, decided notifiable or not (a breach of prescribed data, or of 500 or more people, is notifiable), reported to the PDPC with its reference, told to the people affected, contained, or closed (CMP-PD-05). Deciding it notifiable starts the PDPC's 3 calendar days and sends a critical notice. Side effects: the step, the audit event breach.<step>. Needs If-Match with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired}, writes...), func(ctx context.Context, in *breachStepInput) (*breachOutput, error) {
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		now := a.deps.Clock.Now()
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (BreachView, error) {
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return BreachView{}, err
			}
			b, err := a.oneBreach(ctx, u, in.BreachID)
			if err != nil {
				return BreachView{}, err
			}
			if err := checkIfMatch(in.IfMatch, breachETag(breachView(b, today))); err != nil {
				return BreachView{}, err
			}
			var bad []FieldProblem
			if (in.Body.Step == "decided") != (in.Body.Notifiable != nil) {
				bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.notifiable", Message: "goes with a decision, and only with one", Fix: "Send notifiable with decided"})
			}
			if (in.Body.Step == "pdpc_notified") != (strings.TrimSpace(in.Body.Reference) != "") {
				bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.reference", Message: "goes with the PDPC's notification, and only with it",
					Fix: "Send the PDPC's reference with pdpc_notified"})
			}
			if len(bad) > 0 {
				return BreachView{}, invalid(bad)
			}
			if err := u.Compliance().StepBreach(ctx, b.ID, storage.BreachStep{Step: in.Body.Step, At: now, By: uploader(ctx), Note: strings.TrimSpace(in.Body.Note),
				Notifiable: in.Body.Notifiable, Reference: strings.TrimSpace(in.Body.Reference)}); err != nil {
				return BreachView{}, err
			}
			after, err := a.oneBreach(ctx, u, in.BreachID)
			if err != nil {
				return BreachView{}, err
			}
			v := breachView(after, today)
			if in.Body.Step == "decided" && *in.Body.Notifiable {
				due, _ := time.Parse(time.DateOnly, v.PDPCDueOn)
				if err := a.tellBreach(ctx, u, b.ID, fmt.Sprintf("The breach %q is notifiable: notify the PDPC by %s", b.Title, due.Format("2 Jan 2006"))); err != nil {
					return BreachView{}, err
				}
			}
			return v, nil
		})
		if err != nil {
			return nil, err
		}
		return answer(w), nil
	})

	register(a, authz.ListBreachExercises, exempt(huma.Operation{
		OperationID: "listBreachExercises", Method: http.MethodGet, Path: "/v1/compliance/breach-exercises", Tags: []string{"Compliance"},
		Summary:     "List the tabletop exercises",
		Description: "The tabletop exercises of the breach runbook (CTL-DP-06), held twice a year. Read-only; no side effects.",
	}, "cursor", "Two exercises a year, read whole as one register."), locks,
		func(ctx context.Context, _ *struct{}) (*exercisesOutput, error) {
			p, _ := authn.From(ctx)
			out := &exercisesOutput{}
			out.Body.Items = []BreachExerciseView{}
			err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				all, err := u.Compliance().BreachExercises(ctx)
				for _, e := range all {
					out.Body.Items = append(out.Body.Items, BreachExerciseView{ID: e.ID.String(), HeldOn: e.HeldOn.Format(time.DateOnly), Scenario: e.Scenario,
						Findings: e.Findings, RecordedBy: e.RecordedBy})
				}
				return err
			})
			return out, err
		})

	register(a, authz.RecordBreachExercise, huma.Operation{
		OperationID: "recordBreachExercise", Method: http.MethodPost, Path: "/v1/compliance/breach-exercises", Tags: []string{"Compliance"},
		Summary:     "Record a tabletop exercise",
		Description: "Records a tabletop exercise of the breach runbook, with its scenario and findings (CTL-DP-06): the next is due 6 months on. Side effects: the exercise, the audit event breach_exercise.recorded. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, writes, func(ctx context.Context, in *exerciseInput) (*exerciseOutput, error) {
		now := a.deps.Clock.Now()
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (BreachExerciseView, error) {
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return BreachExerciseView{}, err
			}
			held, err := time.Parse(time.DateOnly, in.Body.HeldOn)
			if err != nil || held.After(today) {
				return BreachExerciseView{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.held_on", Message: "is not a day up to today", Fix: "Send the day it was held, YYYY-MM-DD"}})
			}
			e, err := u.Compliance().RecordBreachExercise(ctx, storage.BreachExercise{HeldOn: held, Scenario: strings.TrimSpace(in.Body.Scenario),
				Findings: strings.TrimSpace(in.Body.Findings), RecordedBy: uploader(ctx), RecordedAt: now})
			return BreachExerciseView{ID: e.ID.String(), HeldOn: e.HeldOn.Format(time.DateOnly), Scenario: e.Scenario, Findings: e.Findings, RecordedBy: e.RecordedBy}, err
		})
		if err != nil {
			return nil, err
		}
		return &exerciseOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})
}
