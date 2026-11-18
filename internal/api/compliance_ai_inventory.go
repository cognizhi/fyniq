package api

import (
	"context"
	"net/http"
	"slices"
	"sort"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the AI inventory (CMP-AI-02)
//
// What the AI uses, beside the AI assistant's impact assessment in the Compliance Center: each connection, where it
// sends prompts and where they are processed, its allowed models and who asked for it; each agent's route and the data
// classes it carries; the prompts and tool versions as p1-ai-assistant adds them; and whether the assessment was
// approved after the AI's last change. It is read from the configuration itself, so it is always current.

// AIModelID is a model's ID, as its provider names it.
type AIModelID string

// Schema implements huma.SchemaProvider.
func (AIModelID) Schema(huma.Registry) *huma.Schema {
	most := 200
	return &huma.Schema{Type: "string", MaxLength: &most, Description: "A model's ID, as its provider names it.", Examples: []any{"claude-opus-4-6"}}
}

// AIInventoryConnection is a connection to a model provider, as the inventory lists it.
type AIInventoryConnection struct {
	ID          string        `json:"id" format:"uuid" example:"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2b"`
	Name        string        `json:"name" example:"Claude in Singapore"`
	Connector   string        `json:"connector" example:"anthropic"`
	Endpoint    string        `json:"endpoint" doc:"The one host its calls go to (SEC-36)." example:"asia-southeast1-aiplatform.googleapis.com"`
	State       string        `json:"state" enum:"disabled,enabled" doc:"enabled: in use; disabled: waiting for approval, or switched off." example:"enabled"`
	ProcessedIn []CountryCode `json:"processed_in" nullable:"false" doc:"Where its prompts are processed, from its data-handling declaration (AI-88)."`
	Training    string        `json:"training,omitempty" enum:"no,yes,unknown" doc:"Whether the provider trains on them, the least favourable its declaration says." example:"no"`
	Models      []AIModelID   `json:"models" nullable:"false" doc:"The models on its allow-list."`
	Classes     []RouteClass  `json:"classes" nullable:"false" doc:"The data classes the routes send it."`
	AskedBy     string        `json:"asked_by" doc:"Who asked for it." example:"user:0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2d"`
	ChangedAt   time.Time     `json:"changed_at" example:"2026-11-14T09:00:00Z"`
}

// AIInventoryStep is a step of a route: a model on a connection.
type AIInventoryStep struct {
	Model      string `json:"model" example:"claude-opus-4-6"`
	Connection string `json:"connection" doc:"The connection's name." example:"Claude in Singapore"`
}

// AIInventoryRoute is an agent's route in force.
type AIInventoryRoute struct {
	Agent   string            `json:"agent" example:"chat"`
	Version int               `json:"version" example:"2"`
	Classes []RouteClass      `json:"classes" nullable:"false" doc:"The data classes it carries."`
	Steps   []AIInventoryStep `json:"steps" nullable:"false"`
	MadeBy  string            `json:"made_by" example:"user:0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2d"`
	MadeAt  time.Time         `json:"made_at" example:"2026-11-14T09:00:00Z"`
}

// AIInventoryVersion is a prompt's or the tool registry's version.
type AIInventoryVersion struct {
	Name    string `json:"name" example:"assistant"`
	Version string `json:"version" example:"14"`
}

// AIInventoryDPIA is the AI assistant's impact assessment as the inventory stands against it.
type AIInventoryDPIA struct {
	Key      string           `json:"key" example:"ai-assistant"`
	Required bool             `json:"required" doc:"The AI assistant is switched on, so it must be approved (CMP-PD-08)." example:"true"`
	Approved *DPIAApprovalRef `json:"approved,omitempty" doc:"Its latest approval."`
	Current  bool             `json:"current" doc:"Approved after the AI's last change of a connection or route; otherwise it needs reviewing (CMP-AI-02)." example:"true"`
	Why      string           `json:"why" doc:"Why it is current, or what to do." example:"Approved after the last change of the AI's connections and routes."`
}

// AIInventory is what the AI uses, beside its impact assessment (CMP-AI-02).
type AIInventory struct {
	Owner       string                  `json:"owner,omitempty" doc:"The Data Protection Officer the profile names, who keeps it." example:"Arjun Rao"`
	Connections []AIInventoryConnection `json:"connections" nullable:"false" doc:"Each connection, in use or waiting, by name."`
	Routes      []AIInventoryRoute      `json:"routes" nullable:"false" doc:"Each agent's route in force."`
	Classes     []RouteClass            `json:"classes" nullable:"false" doc:"The data categories the AI processes: every class a route carries."`
	Prompts     []AIInventoryVersion    `json:"prompts" nullable:"false" doc:"The prompts' versions: none until p1-ai-assistant adds them."`
	Tools       []AIInventoryVersion    `json:"tools" nullable:"false" doc:"The tool registry's version: none until p1-ai-assistant adds it."`
	DPIA        AIInventoryDPIA         `json:"dpia"`
}

type aiInventoryOutput struct{ Body AIInventory }

// aiInventory reads the inventory from the AI's configuration.
func (a *API) aiInventory(ctx context.Context, u storage.UnitOfWork) (AIInventory, error) {
	out := AIInventory{Connections: []AIInventoryConnection{}, Routes: []AIInventoryRoute{}, Classes: []RouteClass{}, Prompts: []AIInventoryVersion{},
		Tools: []AIInventoryVersion{}}
	o, err := u.Organisation(ctx)
	if err != nil {
		return out, err
	}
	out.Owner = o.Profile.DPOName
	connections, err := u.AIConnections().List(ctx)
	if err != nil {
		return out, err
	}
	routes, err := u.AIRoutes().Current(ctx)
	if err != nil {
		return out, err
	}
	var last time.Time // the AI's last change of a connection or route
	names := map[string]string{}
	carried := map[string][]string{}
	for _, r := range routes {
		ir := AIInventoryRoute{Agent: r.Agent, Version: r.Version, Classes: []RouteClass{}, Steps: []AIInventoryStep{}, MadeBy: r.MadeBy, MadeAt: r.MadeAt.UTC()}
		for _, c := range r.Classes {
			ir.Classes = append(ir.Classes, RouteClass(c))
			if !slices.Contains(out.Classes, RouteClass(c)) {
				out.Classes = append(out.Classes, RouteClass(c))
			}
		}
		for _, st := range r.Steps {
			for _, c := range r.Classes {
				if !slices.Contains(carried[st.ConnectionID.String()], c) {
					carried[st.ConnectionID.String()] = append(carried[st.ConnectionID.String()], c)
				}
			}
		}
		if r.MadeAt.After(last) {
			last = r.MadeAt
		}
		out.Routes = append(out.Routes, ir)
	}
	sort.Slice(out.Classes, func(i, j int) bool { return out.Classes[i] < out.Classes[j] })
	for _, c := range connections {
		names[c.ID.String()] = c.Name
		ic := AIInventoryConnection{ID: c.ID.String(), Name: c.Name, Connector: c.Connector, Endpoint: c.Host, State: c.State, ProcessedIn: []CountryCode{},
			Models: []AIModelID{}, Classes: []RouteClass{}, AskedBy: c.CreatedBy, ChangedAt: c.ChangedAt.UTC()}
		if ic.ChangedAt.IsZero() {
			ic.ChangedAt = c.CreatedAt.UTC()
		}
		rank := map[string]int{"no": 0, "unknown": 1, "yes": 2}
		for _, h := range declared(c).Handling {
			for _, country := range h.ProcessedIn {
				if !slices.Contains(ic.ProcessedIn, CountryCode(country)) {
					ic.ProcessedIn = append(ic.ProcessedIn, CountryCode(country))
				}
			}
			if ic.Training == "" || rank[h.Training] > rank[ic.Training] {
				ic.Training = h.Training
			}
		}
		models, err := u.AIModels().Of(ctx, c.ID)
		if err != nil {
			return out, err
		}
		for _, m := range models {
			if m.Allowed {
				ic.Models = append(ic.Models, AIModelID(m.Model))
			}
		}
		for _, k := range carried[c.ID.String()] {
			ic.Classes = append(ic.Classes, RouteClass(k))
		}
		if ic.ChangedAt.After(last) {
			last = ic.ChangedAt
		}
		out.Connections = append(out.Connections, ic)
	}
	sort.Slice(out.Connections, func(i, j int) bool { return out.Connections[i].Name < out.Connections[j].Name })
	for i := range out.Routes {
		for _, st := range routes[i].Steps {
			out.Routes[i].Steps = append(out.Routes[i].Steps, AIInventoryStep{Model: st.Model, Connection: names[st.ConnectionID.String()]})
		}
	}

	views, err := a.dpiaViews(ctx, u, false)
	if err != nil {
		return out, err
	}
	out.DPIA = AIInventoryDPIA{Key: "ai-assistant", Why: "Not approved yet: the Compliance officer approves it before the AI assistant is switched on"}
	for _, v := range views {
		if v.Key != out.DPIA.Key {
			continue
		}
		out.DPIA.Required, out.DPIA.Approved = v.Required, v.Approved
		if v.Approved == nil || v.Unapproved {
			break
		}
		at, err := time.Parse(time.RFC3339, v.Approved.At)
		if err != nil {
			return out, err
		}
		if at.Before(last) {
			out.DPIA.Why = "The AI's connections or routes changed after its approval: review it, and approve it again"
			break
		}
		out.DPIA.Current, out.DPIA.Why = true, "Approved after the last change of the AI's connections and routes"
	}
	return out, nil
}

func registerAIInventory(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	register(a, authz.GetAIInventory, huma.Operation{
		OperationID: "getAIInventory", Method: http.MethodGet, Path: "/v1/compliance/ai-inventory", Tags: []string{"Compliance"},
		Summary: "Get the AI inventory",
		Description: "What the AI uses, beside the AI assistant's impact assessment (CMP-AI-02): each connection, in use or waiting, with the " +
			"endpoint its calls go to, where prompts are processed, whether the provider trains on them, its allowed models, the data classes " +
			"the routes send it and who asked for it; each agent's route in force; the data categories the AI processes; the prompts' and " +
			"the tool registry's versions, none until p1-ai-assistant adds them; and whether the assessment was approved after the AI's " +
			"last change. It is read from the configuration, so it is always current. Read-only; no side effects.",
	}, locks, func(ctx context.Context, _ *struct{}) (*aiInventoryOutput, error) {
		p, _ := authn.From(ctx)
		var out AIInventory
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			out, err = a.aiInventory(ctx, u)
			return err
		})
		return &aiInventoryOutput{Body: out}, err
	})
}
