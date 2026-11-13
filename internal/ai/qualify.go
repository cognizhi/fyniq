package ai

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/aibudget"
	"github.com/cognizhi/fyniq/internal/domain/aiqual"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// QualifyJob runs again, daily at 06:30 Singapore time after the model lists and prices, the qualifications that are
// due (AI-78, AI-87): a moving alias's before its week ends, and every one put out of date by another version.
const QualifyJob = "ai.qualify"

// QualifyKind is the outbox's work of qualifying a newly listed model for one agent, on a connection whose automatic
// qualification is on (AI-81).
const QualifyKind = "ai.qualify"

// requalifyAfter is how old a moving alias's pass may be before the daily job runs it again: a day inside its week, so
// it does not lapse between runs.
const requalifyAfter = aiqual.AliasWeek - 24*time.Hour

// Qualifier runs an agent's eval suite against a model on a connection, as the eval runner, and records the result
// (AI-78, CMP-AI-03): the cases are synthetic text, each call reserves against the budgets and is recorded under the
// run, and the suite's threshold decides the result exactly.
type Qualifier struct {
	Router Router
	// Suites are the eval suites, evals/<suite>/suite.json (the release's embedded evals.Suites).
	Suites fs.FS
}

// Qualification is a recorded run and the cases it failed.
type Qualification struct {
	storage.AIQualification
	Failed []string
}

// Qualify runs agent's suite against model on c and records the result. A case the provider does not answer stops the
// run without recording it: a busy or failing provider is not the model failing its evals.
func (q Qualifier) Qualify(ctx context.Context, org uuid.UUID, c storage.AIConnection, provider ports.ModelProvider, model, agent,
	actor string) (Qualification, error) {
	suites, err := aiqual.Load(q.Suites)
	if err != nil {
		return Qualification{}, err
	}
	suite, ok := suites[agent]
	if !ok {
		return Qualification{}, aiqual.NoSuite(agent)
	}
	run, err := uuid.NewV7()
	if err != nil {
		return Qualification{}, err
	}
	out := Qualification{AIQualification: storage.AIQualification{Agent: agent, ConnectionID: c.ID, Model: model, Suite: suite.ID,
		SuiteVersion: suite.Version, Threshold: suite.Threshold, Cases: len(suite.Cases), Run: run, RanBy: actor}}
	for _, k := range suite.Cases {
		resp, err := q.Router.Direct(ctx, org, run, EvalAgent, c, provider, k.Request(model), actor)
		if err != nil {
			return Qualification{}, aiqual.Unanswered(k.ID, err)
		}
		out.Version = cmp.Or(out.Version, resp.Version)
		if k.Expect.Met(answerText(resp)) {
			out.Passed++
		} else {
			out.Failed = append(out.Failed, k.ID)
		}
	}
	out.Result = aiqual.Fail
	if suite.Passes(out.Passed) {
		out.Result = aiqual.Pass
	}
	out.RanAt = q.Router.Clock.Now()
	err = q.Router.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		out.AIQualification, err = u.AIQualifications().Record(ctx, out.AIQualification)
		return err
	})
	return out, err
}

// answerText is an answer's text parts, joined.
func answerText(r ports.ModelResponse) string {
	var b strings.Builder
	for _, p := range r.Parts {
		if p.Kind == ports.PartText {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

// due is a qualification to run again, and what it runs on.
type due struct {
	q        storage.AIQualification
	c        storage.AIConnection
	provider ports.ModelProvider
}

// Daily runs again, in every organisation, each passing qualification that is due on a connection that is on: a moving
// alias's before its week ends, and every one out of date (AI-78, AI-87). An agent whose suite has not arrived is left
// as it is; a run that fails does not stop the others.
func (q Qualifier) Daily(ctx context.Context) error {
	orgs, err := q.Router.Store.Organisations(ctx)
	if err != nil {
		return err
	}
	suites, err := aiqual.Load(q.Suites)
	if err != nil {
		return err
	}
	now := q.Router.Clock.Now()
	var failed []error
	for _, org := range orgs {
		var runs []due
		if err := q.Router.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			all, err := u.AIQualifications().Due(ctx, now.Add(-requalifyAfter))
			if err != nil {
				return err
			}
			for _, x := range all {
				if _, ok := suites[x.Agent]; !ok {
					continue
				}
				c, err := u.AIConnections().Get(ctx, x.ConnectionID)
				if err != nil {
					return err
				}
				if c.State != "enabled" {
					continue
				}
				models, err := u.AIModels().Of(ctx, c.ID)
				if err != nil {
					return err
				}
				alias := false
				for _, m := range models {
					alias = alias || (m.Model == x.Model && m.AliasOf != "")
				}
				if x.OutOfDateAt == nil && !alias {
					continue // a pinned version stays qualified until another version answers
				}
				provider, err := q.Router.Provider(ctx, u, c)
				if err != nil {
					failed = append(failed, fmt.Errorf("%s %s: %w", c.ID, x.Model, err))
					continue
				}
				runs = append(runs, due{q: x, c: c, provider: provider})
			}
			return nil
		}); err != nil {
			failed = append(failed, err)
			continue
		}
		for _, d := range runs {
			if _, err := q.Qualify(ctx, org, d.c, d.provider, d.q.Model, d.q.Agent, "system:"+QualifyJob); err != nil {
				failed = append(failed, fmt.Errorf("%s %s for %s: %w", d.c.ID, d.q.Model, d.q.Agent, err))
			}
		}
	}
	return errors.Join(failed...)
}

// AutoQualifies is whether a connection qualifies its newly listed models automatically: its configuration's
// auto_qualify, off by default (AI-81).
func AutoQualifies(c storage.AIConnection) bool {
	var config struct {
		Auto bool `json:"auto_qualify"`
	}
	return json.Unmarshal(c.Configuration, &config) == nil && config.Auto
}

// QualifyItem is the outbox item that has the worker qualify model for agent on connection, once for the read that
// first listed it.
func QualifyItem(connection uuid.UUID, model, agent string, read uuid.UUID) (storage.NewOutboxItem, error) {
	payload, err := json.Marshal(map[string]string{"connection_id": connection.String(), "model": model, "agent": agent})
	return storage.NewOutboxItem{Class: storage.ClassWorker, Kind: QualifyKind, Payload: payload,
		BusinessKey: "ai-qualify:" + connection.String() + ":" + model + ":" + agent + ":" + read.String()}, err
}

// newlyListed queues, on a connection whose automatic qualification is on, each model a read listed for the first time
// for the evals of every agent whose route in force uses that connection (AI-81). Adding it to the allow-list or a
// route stays a person's decision.
func newlyListed(ctx context.Context, d *outbox.Dispatcher, u storage.UnitOfWork, org uuid.UUID, c storage.AIConnection, read uuid.UUID,
	kept []storage.AIModel, models []storage.AIModel) error {
	if !AutoQualifies(c) {
		return nil
	}
	routes, err := u.AIRoutes().Current(ctx)
	if err != nil {
		return err
	}
	var agents []string
	for _, r := range routes {
		if slices.ContainsFunc(r.Steps, func(s storage.AIRouteStep) bool { return s.ConnectionID == c.ID }) && !slices.Contains(agents, r.Agent) {
			agents = append(agents, r.Agent)
		}
	}
	slices.Sort(agents)
	for _, m := range models {
		if slices.ContainsFunc(kept, func(k storage.AIModel) bool { return k.Model == m.Model }) {
			continue
		}
		for _, agent := range agents {
			item, err := QualifyItem(c.ID, m.Model, agent, read)
			if err != nil {
				return err
			}
			if d != nil {
				_, err = d.Enqueue(ctx, u, org, item)
			} else {
				_, err = u.Outbox().Enqueue(ctx, item)
			}
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// Work is the outbox's work of QualifyKind: run the agent's evals against the model as the system, on a connection that
// is on and still qualifies automatically (AI-81). An agent whose suite has not arrived, or a spent budget, ends it
// with the model as it was, which a person may still qualify; the stopped call is in the activity log.
func (q Qualifier) Work() outbox.Work {
	return func(ctx context.Context, org uuid.UUID, row storage.OutboxRow) error {
		var p struct {
			Connection uuid.UUID `json:"connection_id"`
			Model      string    `json:"model"`
			Agent      string    `json:"agent"`
		}
		if err := json.Unmarshal(row.Payload, &p); err != nil {
			return fmt.Errorf("%s: %w", QualifyKind, err)
		}
		var c storage.AIConnection
		var provider ports.ModelProvider
		var built error
		skip := false
		if err := q.Router.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			if c, err = u.AIConnections().Get(ctx, p.Connection); err != nil {
				return err
			}
			if skip = c.State != "enabled" || !AutoQualifies(c); skip {
				return nil
			}
			provider, built = q.Router.Provider(ctx, u, c)
			return nil
		}); err != nil || skip {
			return err
		}
		if built != nil {
			return built
		}
		_, err := q.Qualify(ctx, org, c, provider, p.Model, p.Agent, "system:"+QualifyJob)
		var spent *aibudget.Error
		var qe *aiqual.Error
		switch {
		case errors.As(err, &spent), errors.As(err, &qe) && qe.Code == aiqual.CodeNoSuite:
			return nil
		}
		return err
	}
}
