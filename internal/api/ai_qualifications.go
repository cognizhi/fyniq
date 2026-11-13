package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/evals"
	"github.com/cognizhi/fyniq/internal/ai"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/aibudget"
	"github.com/cognizhi/fyniq/internal/domain/aiqual"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ qualification before use (AI-78, AI-87, CMP-AI-03)
//
// A model serves an agent only after passing that agent's evals on its connection: the agent's suite of synthetic
// cases, run by the eval runner against the model, each call budgeted and recorded, the suite's threshold deciding the
// result exactly. The latest run counts; another version answering, or a moving alias's week passing, puts a pass out
// of date, and the daily job runs those again.

// AIQualification is a run of an agent's evals against a model on a connection.
type AIQualification struct {
	ID           string     `json:"id" format:"uuid" example:"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a31"`
	Agent        string     `json:"agent" doc:"The agent whose evals it ran." example:"chat"`
	Standing     string     `json:"standing" enum:"qualified,out_of_date,unqualified" doc:"qualified: it serves the agent; out_of_date: another version answered since, or a moving alias's week passed, so it carries no personal data or payroll and shows ⚠ until it is qualified again (AI-87); unqualified: its latest run failed, so it serves the agent nothing." example:"qualified"`
	Result       string     `json:"result" enum:"pass,fail" example:"pass"`
	Suite        string     `json:"suite" doc:"The eval suite, as evals/<suite>/suite.json names it." example:"probe"`
	SuiteVersion int        `json:"suite_version" example:"1"`
	Threshold    string     `json:"threshold" doc:"The share of cases that must pass, as an exact decimal." example:"1"`
	Cases        int        `json:"cases" example:"3"`
	Passed       int        `json:"passed" example:"3"`
	Version      string     `json:"version,omitempty" doc:"The model version the provider reported while it ran; absent when it reported none (AI-87)." example:"2026-10-01"`
	Run          string     `json:"run" format:"uuid" doc:"The eval run: its calls are in the AI activity log under it." example:"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a32"`
	RanBy        string     `json:"ran_by" doc:"Who ran it: a person, or the daily job." example:"system:ai.qualify"`
	RanAt        time.Time  `json:"ran_at" example:"2026-11-14T06:30:00Z"`
	OutOfDateAt  *time.Time `json:"out_of_date_at,omitempty" example:"2026-11-15T02:00:00Z"`
	OutOfDateWhy string     `json:"out_of_date_why,omitempty" example:"kimi-latest answered as version 2026-11-15, not 2026-11-01, which passed the evals"`
}

// EvalCaseID names a case of an eval suite.
type EvalCaseID string

// Schema implements huma.SchemaProvider.
func (EvalCaseID) Schema(huma.Registry) *huma.Schema {
	most := 60
	return &huma.Schema{Type: "string", MaxLength: &most, Description: "A case of the suite, by its ID.", Examples: []any{"arithmetic"}}
}

// AIQualificationRun is a run as it was just made, with the cases it failed.
type AIQualificationRun struct {
	AIQualification
	Failed []EvalCaseID `json:"failed" nullable:"false" doc:"The cases it failed, by ID."`
}

// AIModelQualify names a listed model and the agent whose evals it runs.
type AIModelQualify struct {
	Model string `json:"model" minLength:"1" maxLength:"200" doc:"The provider's ID for it, as the connection's list shows it." example:"claude-opus-4-6"`
	Agent string `json:"agent" minLength:"2" maxLength:"60" doc:"The agent, as GET /v1/ai/routes lists it: one whose eval suite has arrived." example:"probe"`
}

type (
	aiModelQualifyInput struct {
		AIConnectionPath
		Body AIModelQualify
	}
	aiModelQualifyOutput struct{ Body AIQualificationRun }
)

func qualificationOut(q storage.AIQualification, standing string) AIQualification {
	out := AIQualification{ID: q.ID.String(), Agent: q.Agent, Standing: standing, Result: q.Result, Suite: q.Suite, SuiteVersion: q.SuiteVersion,
		Threshold: q.Threshold, Cases: q.Cases, Passed: q.Passed, Version: q.Version, Run: q.Run.String(), RanBy: q.RanBy, RanAt: q.RanAt.UTC(),
		OutOfDateWhy: q.OutOfDateWhy}
	if q.OutOfDateAt != nil {
		at := q.OutOfDateAt.UTC()
		out.OutOfDateAt = &at
	}
	return out
}

// qualifier is the API's eval runner: the API's router, the release's suites.
func (a *API) qualifier() ai.Qualifier {
	q := ai.Qualifier{Router: a.router(), Suites: evals.Suites}
	if a.deps.EvalSuites != nil {
		q.Suites = a.deps.EvalSuites
	}
	return q
}

func registerAIQualifications(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	register(a, authz.QualifyAIConnectionModel, exempt(exempt(huma.Operation{
		OperationID: "qualifyAIConnectionModel", Method: http.MethodPost, Path: "/v1/ai/connections/{connection_id}/models/qualify", Tags: []string{"AI"},
		Summary: "Qualify a listed model for an agent",
		Description: "Runs an agent's eval suite against a model the connection's provider lists (AI-78, CMP-AI-03): synthetic cases only, " +
			"nothing of the organisation's, each call reserved against the budgets and recorded under the run as the eval runner's (AI-86). " +
			"The suite's threshold decides pass or fail, and the run is recorded with the version the provider reported. A model serves an " +
			"agent only on a pass that is not out of date, and a route change that widens where data goes needs one for every step. A case the " +
			"provider does not answer stops the run and records nothing. Side effects: the run and its audit event ai_qualification.recorded. " +
			"On the never-list for the AI and MCP (§9.6).",
	}, "Idempotency-Key", "A run asks the provider again; repeating it runs the evals again and records that run, which is then the latest."),
		"Prefer", "A run asks the provider: a preview would be the run itself."),
		append([]string{CodeNotFound, CodeInvalid, aibudget.CodeSpent, aiqual.CodeNoSuite, aiqual.CodeUnanswered}, locks...),
		func(ctx context.Context, in *aiModelQualifyInput) (*aiModelQualifyOutput, error) {
			p, _ := authn.From(ctx)
			id, err := parseID("connection_id", in.ConnectionID)
			if err != nil {
				return nil, err
			}
			var provider ports.ModelProvider
			var built error
			var c storage.AIConnection
			listed := false
			if err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
				if c, err = u.AIConnections().Get(ctx, id); err != nil {
					return err
				}
				if _, err := agentOf(ctx, u, in.Body.Agent); err != nil {
					return err
				}
				all, err := u.AIModels().Of(ctx, id)
				if err != nil {
					return err
				}
				for _, m := range all {
					listed = listed || m.Model == in.Body.Model
				}
				provider, built = a.lister().Provider(ctx, u, c)
				return nil
			}); err != nil {
				return nil, err
			}
			if !listed {
				return nil, problem(CodeNotFound, in.Body.Model+" is not a model this connection's provider lists.", "Refresh its models and choose one it lists")
			}
			q := a.qualifier()
			suites, err := aiqual.Load(q.Suites)
			if err != nil {
				return nil, err
			}
			if _, ok := suites[in.Body.Agent]; !ok {
				e := aiqual.NoSuite(in.Body.Agent).(*aiqual.Error)
				return nil, problem(e.Code, e.Message+".", e.Fix, FieldProblem{Code: e.Code, Field: "body.agent", Message: e.Message, Fix: e.Fix})
			}
			if built != nil {
				return nil, problem(aiqual.CodeUnanswered, "The connection could not be reached: "+built.Error()+".", "Check its credential, then try again")
			}
			run, err := q.Qualify(ctx, p.Organisation, c, provider, in.Body.Model, in.Body.Agent, uploader(ctx))
			var spent *aibudget.Error
			var qe *aiqual.Error
			switch {
			case errors.As(err, &spent):
				return nil, problem(spent.Code, spent.Message+".", spent.Fix)
			case errors.As(err, &qe):
				return nil, problem(qe.Code, qe.Message+".", qe.Fix, FieldProblem{Code: qe.Code, Field: "body." + qe.Field, Message: qe.Message, Fix: qe.Fix})
			case err != nil:
				return nil, err
			}
			var alias bool
			if err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				all, err := u.AIModels().Of(ctx, id)
				for _, m := range all {
					alias = alias || (m.Model == in.Body.Model && m.AliasOf != "")
				}
				return err
			}); err != nil {
				return nil, err
			}
			out := AIQualificationRun{AIQualification: qualificationOut(run.AIQualification, ai.Standing(&run.AIQualification, alias, a.deps.Clock.Now())),
				Failed: []EvalCaseID{}}
			for _, f := range run.Failed {
				out.Failed = append(out.Failed, EvalCaseID(f))
			}
			return &aiModelQualifyOutput{Body: out}, nil
		})
}
