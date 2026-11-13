package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ai"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/aiconnection"
	"github.com/cognizhi/fyniq/internal/domain/aimodel"
	"github.com/cognizhi/fyniq/internal/domain/aipolicy"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ connections to AI model providers (AI-72, AI-73, AI-79)
//
// A connection is one of the release's model-provider connectors (anthropic, openai, gemini, openai-compatible), from
// one of its presets or its own endpoint (p1-ai-providers design D2). It starts off. Creating it, changing it, replacing
// its credential and enabling it are each an approval request of the object ai_connection, asked for by a person with C
// on "AI providers and model routes" and approved by the Owner, who holds its A (golden rule 6, FM-1); switching it off
// is never held back. A credential is stored in the secret store as soon as it is given, never shown again, and
// becomes the connection's only when its request is approved; a refused one is destroyed.

// AIConnectionQuota is a subscription's quota (AI-73): tokens or requests per window, for a fee.
type AIConnectionQuota struct {
	Amount   int64  `json:"amount" minimum:"1" doc:"The tokens or requests the plan allows each window." example:"5000000"`
	Unit     string `json:"unit" enum:"tokens,requests" doc:"What the quota counts." example:"tokens"`
	Window   string `json:"window" enum:"day,month" doc:"When the quota resets." example:"month"`
	Fee      string `json:"fee" pattern:"^[0-9]+(\\.[0-9]{1,4})?$" doc:"The fee for each window, a decimal string." example:"30.00"`
	Currency string `json:"currency" pattern:"^[A-Z]{3}$" doc:"The fee's currency." example:"USD"`
}

// AIConnectionBilling is how a connection is billed (AI-73).
type AIConnectionBilling struct {
	Mode  string             `json:"mode" enum:"usage,subscription" doc:"usage: charged per token from the provider's own usage fields; subscription: a fixed fee with a quota. A person's own plan is not offered (AI-73 (c), P2)." example:"usage"`
	Quota *AIConnectionQuota `json:"quota,omitempty" doc:"A subscription's quota; none for usage-based billing."`
}

// AIConnectionCredentialState says whether a connection has a credential: never the credential.
type AIConnectionCredentialState struct {
	Set   bool       `json:"set" doc:"Whether the connection has a current credential. Its value is never shown again (AI-79)." example:"true"`
	SetAt *time.Time `json:"set_at,omitempty" doc:"When the current credential was made current." example:"2026-11-14T09:00:00Z"`
}

// AIConnection is a connection to an AI model provider.
type AIConnection struct {
	ID            string                      `json:"id" format:"uuid" example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d40"`
	Name          string                      `json:"name" doc:"Its name in the organisation." example:"Claude in Singapore"`
	Connector     string                      `json:"connector" doc:"The model provider's connector, from listConnectorCatalogue." example:"anthropic"`
	Preset        string                      `json:"preset,omitempty" doc:"The preset it started from." example:"kimi"`
	Auth          string                      `json:"auth" enum:"api-key,workload-identity,none" doc:"How it authenticates: an API key, the service's own cloud account, or none for a self-hosted server on a registered private endpoint." example:"workload-identity"`
	Configuration map[string]any              `json:"configuration" doc:"Its configuration, as the provider's manifest describes it." example:"{\"endpoint\":\"agent-platform\",\"project\":\"fyniq-prod\",\"location\":\"asia-southeast1\"}"`
	Host          string                      `json:"host" doc:"The one host its calls go to (SEC-36)." example:"asia-southeast1-aiplatform.googleapis.com"`
	SelfHosted    bool                        `json:"self_hosted" doc:"A server the organisation runs, reached only as a registered private endpoint (SEC-37)." example:"false"`
	Credential    AIConnectionCredentialState `json:"credential"`
	Billing       AIConnectionBilling         `json:"billing"`
	DataHandling  AIDataHandling              `json:"data_handling" doc:"Where its prompts are processed and kept, per model, endpoint and feature, with evidence (AI-75, AI-88)."`
	AutoQualify   bool                        `json:"auto_qualify" doc:"Whether newly listed models are queued for the evals of every agent whose route uses this connection (AI-81); off by default." example:"false"`
	State         string                      `json:"state" enum:"disabled,enabled" doc:"disabled until an approved request switches it on." example:"disabled"`
	Version       int                         `json:"version" example:"1"`
	CreatedBy     string                      `json:"created_by" example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	CreatedAt     time.Time                   `json:"created_at" example:"2026-11-14T09:00:00Z"`
	ChangedBy     string                      `json:"changed_by" example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	ChangedAt     time.Time                   `json:"changed_at" example:"2026-11-14T09:00:00Z"`
}

// AIConnectionList is a page of the organisation's AI connections.
type AIConnectionList struct {
	Items      []AIConnection `json:"items" nullable:"false" doc:"The connections, by name."`
	NextCursor string         `json:"next_cursor,omitempty" doc:"The cursor of the next page; none on the last." example:"eyJhZnRlciI6IjEwMDAifQ"`
}

// AIConnectionDefinition is what a person asks a connection to be.
type AIConnectionDefinition struct {
	Name          string              `json:"name" minLength:"1" maxLength:"80" doc:"Its name in the organisation." example:"Claude in Singapore"`
	Preset        string              `json:"preset,omitempty" maxLength:"40" doc:"One of the provider's presets, from listConnectorCatalogue." example:"kimi"`
	Auth          string              `json:"auth" enum:"api-key,workload-identity,none" doc:"How it authenticates, one the provider and its preset offer." example:"workload-identity"`
	Configuration map[string]any      `json:"configuration,omitempty" doc:"Its configuration, as the provider's manifest describes it." example:"{\"endpoint\":\"agent-platform\",\"project\":\"fyniq-prod\"}"`
	Billing       AIConnectionBilling `json:"billing"`
	AutoQualify   bool                `json:"auto_qualify,omitempty" doc:"Queue newly listed models for the agents' evals (AI-81); off by default. Adding a model to the allow-list stays a person's decision." example:"false"`
	DataHandling  *AIDataHandling     `json:"data_handling,omitempty" doc:"Its data-handling declaration (AI-75, AI-88); switching it on needs one, with its data inventory row and, when it processes outside Singapore, its transfer assessment."`
}

// AIConnectionCreate asks for a new connection.
type AIConnectionCreate struct {
	AIConnectionDefinition
	Connector  string `json:"connector" maxLength:"40" doc:"The model provider's connector, from listConnectorCatalogue." example:"anthropic"`
	Credential string `json:"credential,omitempty" maxLength:"4096" writeOnly:"true" doc:"The provider's API key, for an api-key connection: stored at once, never shown again, and the connection's when the request is approved (AI-79). A consumer plan's token, a session or a cookie is refused (AI-73)." example:"sk-proj-0123456789abcdef"`
}

// AIConnectionCredential replaces a connection's credential.
type AIConnectionCredential struct {
	Value string `json:"value" minLength:"1" maxLength:"4096" writeOnly:"true" doc:"The new API key: stored at once, never shown again, and the connection's when the request is approved; the one it replaces stops after the provider's overlap (AI-79)." example:"sk-proj-0123456789abcdef"`
}

type (
	// AIConnectionPath names an AI connection.
	AIConnectionPath struct {
		ConnectionID string `path:"connection_id" doc:"The connection's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d40"`
	}
	aiConnectionListOutput struct{ Body AIConnectionList }
	aiConnectionOutput     struct {
		ETag string `header:"ETag" doc:"The connection's version, for If-Match."`
		Body AIConnection
	}
	aiConnectionAskOutput struct {
		Status   int
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     Approval
	}
	aiConnectionCreateInput struct {
		KeyHeader
		PreferHeader
		Body AIConnectionCreate
	}
	aiConnectionChangeInput struct {
		AIConnectionPath
		KeyHeader
		PreferHeader
		IfMatchHeader
		Body AIConnectionDefinition
	}
	aiConnectionCredentialInput struct {
		AIConnectionPath
		KeyHeader
		PreferHeader
		IfMatchHeader
		Body AIConnectionCredential
	}
	aiConnectionStepInput struct {
		AIConnectionPath
		KeyHeader
		PreferHeader
		IfMatchHeader
	}
	aiConnectionDisableInput struct {
		AIConnectionPath
		KeyHeader
		PreferHeader
		IfMatchHeader
	}
	aiConnectionDisableOutput struct {
		ETag     string `header:"ETag" doc:"The connection's version, for If-Match."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     AIConnection
	}
)

// AIConnectionCodes are the refusals of an AI connection's definition and steps.
var AIConnectionCodes = []string{aiconnection.CodeProvider, aiconnection.CodeAuth, aiconnection.CodeConfig, aiconnection.CodeCredential,
	aiconnection.CodeEgress, aiconnection.CodeBilling, aiconnection.CodeState, aiconnection.CodeName, aipolicy.CodeDeclaration, aimodel.CodeListUnavailable,
	storage.CodeAIConnection, storage.CodeDuplicate}

func aiConnectionOut(c storage.AIConnection) AIConnection {
	out := AIConnection{ID: c.ID.String(), Name: c.Name, Connector: c.Connector, Preset: c.Preset, Auth: c.Auth, Configuration: map[string]any{},
		Host: c.Host, SelfHosted: c.SelfHosted, Credential: AIConnectionCredentialState{Set: c.CredentialRef != ""},
		Billing: AIConnectionBilling{Mode: c.Billing}, State: c.State, Version: c.Version, CreatedBy: c.CreatedBy, CreatedAt: c.CreatedAt.UTC(),
		ChangedBy: c.ChangedBy, ChangedAt: c.ChangedAt.UTC()}
	if c.CredentialSetAt != nil {
		at := c.CredentialSetAt.UTC()
		out.Credential.SetAt = &at
	}
	_ = json.Unmarshal(c.Configuration, &out.Configuration)
	out.DataHandling, out.AutoQualify = dataHandlingOut(c.DataHandling), c.AutoQualify
	if len(c.Quota) > 0 {
		var q AIConnectionQuota
		if json.Unmarshal(c.Quota, &q) == nil {
			out.Billing.Quota = &q
		}
	}
	return out
}

// aiProblem is a refusal of the connection's rules, with the field it is about.
func aiProblem(err error) error {
	var e *aiconnection.Error
	if errors.As(err, &e) {
		return problem(e.Code, e.Field+" "+e.Message+".", e.Fix, FieldProblem{Code: e.Code, Field: "body." + e.Field, Message: e.Message, Fix: e.Fix})
	}
	return err
}

func (d AIConnectionDefinition) domain(connector string) aiconnection.Definition {
	out := aiconnection.Definition{Name: d.Name, Connector: connector, Preset: d.Preset, Auth: d.Auth, Configuration: d.Configuration, Billing: d.Billing.Mode}
	if out.Configuration == nil {
		out.Configuration = map[string]any{}
	}
	if q := d.Billing.Quota; q != nil {
		out.Quota = &aiconnection.Quota{Amount: q.Amount, Unit: q.Unit, Window: q.Window, Fee: q.Fee, Currency: q.Currency}
	}
	return out
}

// definitionChange is a definition as an approval request carries it: strings, booleans and integers (FR-APR-10).
func definitionChange(d aiconnection.Definition, t aiconnection.Target, handling *AIDataHandling, autoQualify bool) (map[string]any, error) {
	config, err := json.Marshal(d.Configuration)
	if err != nil {
		return nil, err
	}
	declaration := []byte(`{}`)
	if handling != nil {
		if declaration, err = json.Marshal(handling.domain()); err != nil {
			return nil, err
		}
	}
	quota := ""
	if d.Quota != nil {
		b, err := json.Marshal(d.Quota)
		if err != nil {
			return nil, err
		}
		quota = string(b)
	}
	return map[string]any{"name": d.Name, "connector": d.Connector, "preset": d.Preset, "auth": d.Auth, "configuration": string(config),
		"host": t.Host, "self_hosted": t.SelfHosted, "billing_mode": d.Billing, "quota": quota, "data_handling": string(declaration),
		"auto_qualify": autoQualify}, nil
}

// definitionDiff is what an approver reads of a definition.
func definitionDiff(before *storage.AIConnection, d aiconnection.Definition, t aiconnection.Target, change map[string]any) []ApprovalDiff {
	diff := []ApprovalDiff{}
	add := func(field string, was, now any) {
		if before == nil || fmt.Sprint(was) != fmt.Sprint(now) {
			d := ApprovalDiff{Field: field, After: fmt.Sprint(now)}
			if before != nil {
				d.Before = fmt.Sprint(was)
			}
			diff = append(diff, d)
		}
	}
	var bName, bPreset, bAuth, bConfig, bHost, bBilling, bQuota any
	if before != nil {
		bName, bPreset, bAuth, bConfig, bHost, bBilling, bQuota = before.Name, before.Preset, before.Auth, string(before.Configuration), before.Host,
			before.Billing, string(before.Quota)
	}
	add("name", bName, d.Name)
	if before == nil {
		add("connector", nil, d.Connector)
	}
	add("preset", bPreset, d.Preset)
	add("auth", bAuth, d.Auth)
	add("configuration", bConfig, change["configuration"])
	add("host", bHost, t.Host)
	add("billing_mode", bBilling, d.Billing)
	add("quota", bQuota, change["quota"])
	var bHandling any
	if before != nil {
		bHandling = string(before.DataHandling)
	}
	add("data_handling", bHandling, change["data_handling"])
	return diff
}

// secretName is the secret that holds an AI connection's credential, beside the organisation's connector secrets.
func secretName(org, connection uuid.UUID) string {
	return "org-" + org.String() + "-ai-" + connection.String() + "-api_key"
}

// putAICredential stores a credential as a new version of the connection's secret: destroyed again if the request is
// not saved; refused or expired, the connector secrets' sweep destroys it (AI-79).
func (a *API) putAICredential(ctx context.Context, u storage.UnitOfWork, org, connection uuid.UUID, connector, value string) (ports.SecretRef, error) {
	if err := aiconnection.CheckCredential(connector, value); err != nil {
		return "", aiProblem(err)
	}
	if a.deps.Connectors.Secrets == nil {
		return "", problem(CodeUnavailable, "The secret store is not configured.", "Try again later.")
	}
	ref, err := a.deps.Connectors.Secrets.Put(ctx, secretName(org, connection), value)
	if err != nil {
		return "", err
	}
	u.AfterRollback(func(ctx context.Context) { _ = a.deps.Connectors.Secrets.Destroy(ctx, ref) })
	return ref, nil
}

// credentialDigest stands in for a credential in a request's idempotency hash, so the hash never holds the value.
func credentialDigest(value string) string {
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// aiConnectionDependencies are an AI connection's version and state: "new" before it exists (AI-72).
func aiConnectionDependencies(ctx context.Context, u storage.UnitOfWork, key string) (int, map[string]any, error) {
	id, err := uuid.Parse(key)
	if err != nil {
		return 0, nil, err
	}
	c, err := u.AIConnections().Get(ctx, id)
	if errors.Is(err, ports.ErrNotFound) {
		return 0, map[string]any{"status": "new"}, nil
	}
	if err != nil {
		return 0, nil, err
	}
	return c.Version, map[string]any{"status": c.State}, nil
}

func (a *API) aiConnection(ctx context.Context, u storage.UnitOfWork, raw, ifMatch string) (storage.AIConnection, error) {
	id, err := parseID("connection_id", raw)
	if err != nil {
		return storage.AIConnection{}, err
	}
	c, err := u.AIConnections().Get(ctx, id)
	if err != nil {
		return storage.AIConnection{}, err
	}
	if err := checkIfMatch(ifMatch, etag(c.Version, c.State)); err != nil {
		return storage.AIConnection{}, err
	}
	return c, nil
}

func registerAIConnections(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	asks := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeSessionRequired, CodeStepUpRequired, CodeInvalid,
		CodeUnavailable, approval.CodePending}, locks...)
	versioned := []string{CodeIfMatchRequired, CodeStale, storage.CodeVersion}
	note := " It is dual controlled (golden rule 6, FM-1): the answer is the approval request, which the Owner, who holds A on \"AI providers " +
		"and model routes\", approves; nothing changes until then. Side effects: the approval request and its audit event. Idempotent with " +
		"Idempotency-Key; Prefer: return=preview checks it and asks nothing. On the never-list for the AI and MCP (§9.6)."

	register(a, authz.ListAIConnections, huma.Operation{
		OperationID: "listAIConnections", Method: http.MethodGet, Path: "/v1/ai/connections", Tags: []string{"AI"},
		Summary: "List the organisation's AI connections",
		Description: "The organisation's connections to AI model providers (AI-72), by name, a page at a time (FR-INT-10): each one's provider, preset, " +
			"authentication, configuration, the host its calls go to, whether it has a credential (never the credential), its billing and its " +
			"state. Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *PageQuery) (*aiConnectionListOutput, error) {
		out := AIConnectionList{Items: []AIConnection{}}
		p, _ := authn.From(ctx)
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			all, err := u.AIConnections().List(ctx)
			if err != nil {
				return err
			}
			page, next, err := pageOf(all, func(c storage.AIConnection) string { return c.ID.String() }, *in)
			for _, c := range page {
				out.Items = append(out.Items, aiConnectionOut(c))
			}
			out.NextCursor = next
			return err
		})
		return &aiConnectionListOutput{Body: out}, err
	})

	register(a, authz.GetAIConnection, huma.Operation{
		OperationID: "getAIConnection", Method: http.MethodGet, Path: "/v1/ai/connections/{connection_id}", Tags: []string{"AI"},
		Summary: "Get an AI connection",
		Description: "One connection to an AI model provider (AI-72) with its ETag for the commands that change it; never its credential (AI-79). " +
			"Read-only; no side effects.",
	}, append([]string{CodeNotFound}, locks...), func(ctx context.Context, in *AIConnectionPath) (*aiConnectionOutput, error) {
		var out storage.AIConnection
		p, _ := authn.From(ctx)
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			id, err := parseID("connection_id", in.ConnectionID)
			if err != nil {
				return err
			}
			out, err = u.AIConnections().Get(ctx, id)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &aiConnectionOutput{ETag: etag(out.Version, out.State), Body: aiConnectionOut(out)}, nil
	})

	register(a, authz.CreateAIConnection, huma.Operation{
		OperationID: "createAIConnection", Method: http.MethodPost, Path: "/v1/ai/connections", Tags: []string{"AI"},
		DefaultStatus: http.StatusAccepted, Summary: "Ask to connect an AI model provider",
		Description: "Asks to connect one of the release's model providers (AI-71, AI-72): from one of its presets or its own endpoint, with an " +
			"authentication it offers, its configuration, and how it is billed (AI-73). Its calls go to one https host; a self-hosted server only as " +
			"a registered private endpoint (SEC-37). A credential given now is stored at once, never shown again, and refused when it is a consumer " +
			"plan's token, a session or a cookie (AI-73, AI-79). The connection is created switched off; enabling it is a request of its own." + note,
	}, append(append([]string{}, asks...), AIConnectionCodes...), func(ctx context.Context, in *aiConnectionCreateInput) (*aiConnectionAskOutput, error) {
		d := in.Body.AIConnectionDefinition.domain(in.Body.Connector)
		target, err := aiconnection.Check(a.deps.Connectors.Catalogue, d)
		if err != nil {
			return nil, aiProblem(err)
		}
		if h := in.Body.DataHandling; h != nil {
			if err := aipolicy.Check(h.domain(), a.deps.Clock.Now()); err != nil {
				return nil, policyProblem(err)
			}
		}
		if in.Body.Credential != "" && d.Auth != "api-key" {
			return nil, problem(aiconnection.CodeCredential, "credential is given for a connection that takes no API key.", "Leave the credential out",
				FieldProblem{Code: aiconnection.CodeCredential, Field: "body.credential", Message: "is given for auth " + d.Auth, Fix: "Leave it out"})
		}
		p, _ := authn.From(ctx)
		id := uuid.Must(uuid.NewV7())
		request := struct {
			Definition AIConnectionDefinition
			Connector  string
			Credential string
		}{in.Body.AIConnectionDefinition, in.Body.Connector, credentialDigest(in.Body.Credential)}
		resp, err := write(ctx, a, in.IdempotencyKey, in.Prefer, request, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
			change, err := definitionChange(d, target, in.Body.DataHandling, in.Body.AutoQualify)
			if err != nil {
				return Approval{}, err
			}
			change["id"] = id.String()
			diff := definitionDiff(nil, d, target, change)
			if in.Body.Credential != "" {
				ref, err := a.putAICredential(ctx, u, p.Organisation, id, d.Connector, in.Body.Credential)
				if err != nil {
					return Approval{}, err
				}
				change["ref"] = string(ref)
				diff = append(diff, ApprovalDiff{Field: "credential", After: "set"})
			}
			return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectAIConnection, ObjectKey: id.String(),
				Operation: approval.OpAIConnectionCreate, Summary: "Connect the AI provider " + d.Name, Execute: change, Diff: diff})
		})
		if err != nil {
			return nil, err
		}
		return &aiConnectionAskOutput{Status: resp.Status, Replayed: replayedHeader(resp.Replayed), Applied: applied(resp), Body: resp.Body}, nil
	})

	register(a, authz.ChangeAIConnection, huma.Operation{
		OperationID: "changeAIConnection", Method: http.MethodPut, Path: "/v1/ai/connections/{connection_id}", Tags: []string{"AI"},
		DefaultStatus: http.StatusAccepted, Summary: "Ask to change an AI connection",
		Description: "Asks to replace a connection's name, preset, authentication, configuration and billing (AI-72, AI-73); its provider stays. " +
			"Checked as a new connection is. Needs If-Match with its ETag." + note,
	}, append(append(append([]string{CodeNotFound}, asks...), versioned...), AIConnectionCodes...), func(ctx context.Context, in *aiConnectionChangeInput) (*aiConnectionAskOutput, error) {
		resp, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
			c, err := a.aiConnection(ctx, u, in.ConnectionID, in.IfMatch)
			if err != nil {
				return Approval{}, err
			}
			d := in.Body.domain(c.Connector)
			target, err := aiconnection.Check(a.deps.Connectors.Catalogue, d)
			if err != nil {
				return Approval{}, aiProblem(err)
			}
			if d.Auth != "api-key" && c.CredentialRef != "" || d.Auth == "api-key" && c.Auth != "api-key" {
				return Approval{}, problem(aiconnection.CodeAuth, "auth changes between an API key and none or the service's own account.",
					"Make a new connection for the other authentication",
					FieldProblem{Code: aiconnection.CodeAuth, Field: "body.auth", Message: "changes the kind of credential", Fix: "Keep " + c.Auth})
			}
			if h := in.Body.DataHandling; h != nil {
				if err := aipolicy.Check(h.domain(), a.deps.Clock.Now()); err != nil {
					return Approval{}, policyProblem(err)
				}
			} else if len(c.DataHandling) > 2 {
				kept := dataHandlingOut(c.DataHandling) // a change that leaves it out keeps the declaration
				in.Body.DataHandling = &kept
			}
			change, err := definitionChange(d, target, in.Body.DataHandling, in.Body.AutoQualify)
			if err != nil {
				return Approval{}, err
			}
			change["id"] = c.ID.String()
			return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectAIConnection, ObjectKey: c.ID.String(),
				Operation: approval.OpAIConnectionChange, Summary: "Change the AI connection " + c.Name, Execute: change,
				Diff: definitionDiff(&c, d, target, change)})
		})
		if err != nil {
			return nil, err
		}
		return &aiConnectionAskOutput{Status: resp.Status, Replayed: replayedHeader(resp.Replayed), Applied: applied(resp), Body: resp.Body}, nil
	})

	register(a, authz.SetAIConnectionCredential, huma.Operation{
		OperationID: "setAIConnectionCredential", Method: http.MethodPut, Path: "/v1/ai/connections/{connection_id}/credential", Tags: []string{"AI"},
		DefaultStatus: http.StatusAccepted, Summary: "Ask to replace an AI connection's API key",
		Description: "Stores the new API key in the secret store at once, as a new version never shown again, and asks to make it the " +
			"connection's (AI-79): approved, the key it replaces stops after the provider's overlap; refused or expired, the new key is destroyed. " +
			"A consumer plan's token, a session or a cookie is refused (AI-73). Needs If-Match with its ETag." + note,
	}, append(append(append([]string{CodeNotFound}, asks...), versioned...), AIConnectionCodes...), func(ctx context.Context, in *aiConnectionCredentialInput) (*aiConnectionAskOutput, error) {
		p, _ := authn.From(ctx)
		request := struct {
			ID, IfMatch, Credential string
		}{in.ConnectionID, in.IfMatch, credentialDigest(in.Body.Value)}
		resp, err := write(ctx, a, in.IdempotencyKey, in.Prefer, request, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
			c, err := a.aiConnection(ctx, u, in.ConnectionID, in.IfMatch)
			if err != nil {
				return Approval{}, err
			}
			if c.Auth != "api-key" {
				return Approval{}, problem(aiconnection.CodeState, "The connection takes no API key: it authenticates with "+c.Auth+".",
					"Change its authentication first, or leave it as it is")
			}
			ref, err := a.putAICredential(ctx, u, p.Organisation, c.ID, c.Connector, in.Body.Value)
			if err != nil {
				return Approval{}, err
			}
			return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectAIConnection, ObjectKey: c.ID.String(),
				Operation: approval.OpAIConnectionCredential, Summary: "Replace the API key of the AI connection " + c.Name,
				Execute: map[string]any{"id": c.ID.String(), "ref": string(ref)},
				Diff:    []ApprovalDiff{{Field: "credential", Before: map[bool]string{true: "set", false: "none"}[c.CredentialRef != ""], After: "replaced"}}})
		})
		if err != nil {
			return nil, err
		}
		return &aiConnectionAskOutput{Status: resp.Status, Replayed: replayedHeader(resp.Replayed), Applied: applied(resp), Body: resp.Body}, nil
	})

	register(a, authz.EnableAIConnection, huma.Operation{
		OperationID: "enableAIConnection", Method: http.MethodPost, Path: "/v1/ai/connections/{connection_id}/enable", Tags: []string{"AI"},
		DefaultStatus: http.StatusAccepted, Summary: "Ask to switch an AI connection on",
		Description: "Asks to switch a connection on (AI-72): an API-key connection needs its credential, a self-hosted one a private endpoint " +
			"registered for it (SEC-37). A model serves no agent until its qualification passes (AI-78). Needs If-Match with its ETag." + note,
	}, append(append(append([]string{CodeNotFound}, asks...), versioned...), AIConnectionCodes...), func(ctx context.Context, in *aiConnectionStepInput) (*aiConnectionAskOutput, error) {
		resp, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
			c, err := a.aiConnection(ctx, u, in.ConnectionID, in.IfMatch)
			if err != nil {
				return Approval{}, err
			}
			if err := a.canEnable(ctx, u, c); err != nil {
				return Approval{}, err
			}
			return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectAIConnection, ObjectKey: c.ID.String(),
				Operation: approval.OpAIConnectionEnable, Summary: "Switch on the AI connection " + c.Name, Execute: map[string]any{"id": c.ID.String()},
				Diff: []ApprovalDiff{{Field: "state", Before: c.State, After: aiconnection.StateEnabled}}})
		})
		if err != nil {
			return nil, err
		}
		return &aiConnectionAskOutput{Status: resp.Status, Replayed: replayedHeader(resp.Replayed), Applied: applied(resp), Body: resp.Body}, nil
	})

	register(a, authz.DisableAIConnection, huma.Operation{
		OperationID: "disableAIConnection", Method: http.MethodPost, Path: "/v1/ai/connections/{connection_id}/disable", Tags: []string{"AI"},
		Summary: "Switch an AI connection off",
		Description: "Switches a connection off at once (AI-72): never held back for a second person. No new call goes to it; routes fall back to " +
			"their next step or stop with a plain message (AI-76). Side effects: its new version and the audit event ai_connection.disabled. " +
			"Needs If-Match with its ETag; idempotent with Idempotency-Key; Prefer: return=preview shows the result and saves nothing. On the " +
			"never-list for the AI and MCP (§9.6).",
	}, append(append([]string{CodeNotFound, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, aiconnection.CodeState,
		storage.CodeAIConnection}, versioned...), locks...), func(ctx context.Context, in *aiConnectionDisableInput) (*aiConnectionDisableOutput, error) {
		resp, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (storage.AIConnection, error) {
			c, err := a.aiConnection(ctx, u, in.ConnectionID, in.IfMatch)
			if err != nil {
				return storage.AIConnection{}, err
			}
			if c.State != aiconnection.StateEnabled {
				return storage.AIConnection{}, problem(aiconnection.CodeState, "The connection is off already.", "Nothing to do")
			}
			c.State, c.ChangedBy, c.ChangedAt = aiconnection.StateDisabled, uploader(ctx), a.deps.Clock.Now()
			return u.AIConnections().Step(ctx, c, storage.AIConnectionDisabled, nil)
		})
		if err != nil {
			return nil, err
		}
		return &aiConnectionDisableOutput{ETag: etag(resp.Body.Version, resp.Body.State), Replayed: replayedHeader(resp.Replayed), Applied: applied(resp),
			Body: aiConnectionOut(resp.Body)}, nil
	})
}

// canEnable is whether c may be switched on: off now, with its credential if it takes one, and a self-hosted one with
// a private endpoint registered for it (SEC-37).
func (a *API) canEnable(ctx context.Context, u storage.UnitOfWork, c storage.AIConnection) error {
	switch {
	case c.State != aiconnection.StateDisabled:
		return problem(aiconnection.CodeState, "The connection is on already.", "Nothing to do")
	case c.Auth == "api-key" && c.CredentialRef == "":
		return problem(aiconnection.CodeState, "The connection has no API key yet.", "Set its API key with setAIConnectionCredential, then ask again")
	}
	if err := listedWhole(ctx, u, c); err != nil {
		return err
	}
	if c.SelfHosted {
		endpoints, err := u.PrivateEndpoints().For(ctx, storage.PrivateEndpointAIModel, c.ID)
		if err != nil {
			return err
		}
		if len(endpoints) == 0 {
			return problem(aiconnection.CodeEgress, "The self-hosted server has no private endpoint registered for this connection.",
				"Ask a Super admin to register the server's private endpoint for it (SEC-37), then ask again")
		}
	}
	return recorded(ctx, u, declared(c))
}

// executeAIConnection runs an approved request on an AI connection (AI-72, AI-79), as its requester asked, after
// checking it once more against the catalogue and the connection as it is now.
func (a *API) executeAIConnection(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest, s stored) error {
	str := func(k string) string { v, _ := s.Execute[k].(string); return v }
	by, now := "user:"+r.RequestedBy.String(), a.deps.Clock.Now()
	id, err := uuid.Parse(str("id"))
	if err != nil {
		return err
	}
	definition := func(connector string) (aiconnection.Definition, aiconnection.Target, error) {
		d := aiconnection.Definition{Name: str("name"), Connector: connector, Preset: str("preset"), Auth: str("auth"), Billing: str("billing_mode")}
		if err := json.Unmarshal([]byte(str("configuration")), &d.Configuration); err != nil {
			return d, aiconnection.Target{}, err
		}
		if q := str("quota"); q != "" {
			d.Quota = &aiconnection.Quota{}
			if err := json.Unmarshal([]byte(q), d.Quota); err != nil {
				return d, aiconnection.Target{}, err
			}
		}
		t, err := aiconnection.Check(a.deps.Connectors.Catalogue, d)
		return d, t, aiProblem(err)
	}
	apply := func(c *storage.AIConnection, d aiconnection.Definition, t aiconnection.Target) error {
		config, err := json.Marshal(d.Configuration)
		if err != nil {
			return err
		}
		c.Name, c.Preset, c.Auth, c.Configuration, c.Host, c.SelfHosted, c.Billing = d.Name, d.Preset, d.Auth, config, t.Host, t.SelfHosted, d.Billing
		c.DataHandling = []byte(str("data_handling"))
		c.AutoQualify, _ = s.Execute["auto_qualify"].(bool)
		if len(c.DataHandling) == 0 {
			c.DataHandling = []byte(`{}`)
		}
		c.Quota = nil
		if d.Quota != nil {
			if c.Quota, err = json.Marshal(d.Quota); err != nil {
				return err
			}
		}
		return nil
	}
	if r.Operation == approval.OpAIConnectionCreate {
		d, t, err := definition(str("connector"))
		if err != nil {
			return err
		}
		c := storage.AIConnection{ID: id, Connector: d.Connector, CreatedBy: by, CreatedAt: now}
		if err := apply(&c, d, t); err != nil {
			return err
		}
		if ref := str("ref"); ref != "" {
			c.CredentialRef, c.CredentialSetAt = ref, &now
		}
		if _, err = u.AIConnections().Create(ctx, c, r.ID); err != nil {
			return err
		}
		if c.CredentialRef != "" || c.Auth != "api-key" {
			return a.queueModelRead(ctx, u, c.ID, r.ID) // AI-80: the list read once there is a credential
		}
		return nil
	}
	c, err := u.AIConnections().Get(ctx, id)
	if err != nil {
		return err
	}
	c.ChangedBy, c.ChangedAt = by, now
	step := ""
	switch r.Operation {
	case approval.OpAIConnectionChange:
		d, t, err := definition(c.Connector)
		if err != nil {
			return err
		}
		if err := apply(&c, d, t); err != nil {
			return err
		}
		step = storage.AIConnectionChanged
	case approval.OpAIConnectionCredential:
		c.CredentialRef, c.CredentialSetAt, step = str("ref"), &now, storage.AIConnectionCredential
		if err := a.queueModelRead(ctx, u, c.ID, r.ID); err != nil { // AI-80: the list read when a key is stored
			return err
		}
	case approval.OpAIConnectionEnable:
		if err := a.canEnable(ctx, u, c); err != nil {
			return err
		}
		c.State, step = aiconnection.StateEnabled, storage.AIConnectionEnabled
	case approval.OpAIConnectionAllowModel:
		model := str("model")
		if err := allowable(ctx, u, c.ID, model, now); err != nil {
			return err
		}
		_, err := u.AIModels().Allow(ctx, c.ID, model, true, &r.ID)
		return err
	case approval.OpAIConnectionPrice:
		return a.executeAIPrice(ctx, u, c, r, str)
	default:
		return fmt.Errorf("ai connection: no operation %q", r.Operation)
	}
	_, err = u.AIConnections().Step(ctx, c, step, &r.ID)
	return err
}

// queueModelRead has the worker read a connection's model list after this change commits (AI-80).
func (a *API) queueModelRead(ctx context.Context, u storage.UnitOfWork, id, change uuid.UUID) error {
	item, err := ai.ReadItem(id, aimodel.ByCredential, change.String())
	if err != nil {
		return err
	}
	if a.deps.Outbox != nil {
		p, _ := authn.From(ctx)
		_, err = a.deps.Outbox.Enqueue(ctx, u, p.Organisation, item)
		return err
	}
	_, err = u.Outbox().Enqueue(ctx, item) // no dispatcher here: the tick sends it
	return err
}
