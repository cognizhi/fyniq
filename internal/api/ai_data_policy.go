package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/aipolicy"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ where the AI may send which data (AI-75, AI-83, AI-88, CMP-PD-06)
//
// Each AI connection declares its data handling as part of its definition; the organisation's data-class policy maps
// each class to the connections allowed beyond the defaults (p1-ai-providers design D5). By default personal data and
// payroll go only where Singapore processing is evidenced, restricted data nowhere, and know-how and business figures to
// any enabled connection. Changing the policy is one person's request the Owner approves (golden rule 6); widening a
// sensitive class overseas needs the connection's transfer recorded (CMP-PD-06).

// AIDataEvidence is where a data-handling statement comes from.
type AIDataEvidence struct {
	Kind       string `json:"kind" enum:"source,contract" doc:"source: the provider's official page, read on a day; contract: a contract term kept as evidence (FR-DOC-05). A probe never proves geography (AI-88)." example:"source"`
	URL        string `json:"url,omitempty" pattern:"^https://" doc:"The official page, for a source." example:"https://cloud.google.com/vertex-ai/generative-ai/docs/learn/locations"`
	Title      string `json:"title" minLength:"1" maxLength:"300" doc:"The page's title, or the contract and its clause." example:"Generative AI on Vertex AI locations"`
	CheckedOn  string `json:"checked_on" format:"date" doc:"The day it was read." example:"2026-11-14"`
	EvidenceID string `json:"evidence_id,omitempty" doc:"The contract's evidence file, for a contract term." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d60"`
}

// CountryCode is an ISO 3166 country code.
type CountryCode string

// Schema implements huma.SchemaProvider.
func (CountryCode) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Pattern: "^[A-Z]{2}$", Description: "An ISO 3166 country code.", Examples: []any{"SG"}}
}

// ProcessorName names a sub-processor or a gateway.
type ProcessorName string

// Schema implements huma.SchemaProvider.
func (ProcessorName) Schema(huma.Registry) *huma.Schema {
	one, most := 1, 200
	return &huma.Schema{Type: "string", MinLength: &one, MaxLength: &most, Description: "A sub-processor's or gateway's name.", Examples: []any{"Google Cloud"}}
}

func asCountry(s string) CountryCode     { return CountryCode(s) }
func asProcessor(s string) ProcessorName { return ProcessorName(s) }

func plain[T ~string](xs []T) []string {
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		out = append(out, string(x))
	}
	return out
}

// AIDataHandlingEntry is a connection's data handling for one model, endpoint and feature.
type AIDataHandlingEntry struct {
	Model         string          `json:"model" maxLength:"128" doc:"The model, or * for every model." example:"claude-opus-4-6"`
	Endpoint      string          `json:"endpoint" maxLength:"253" doc:"The endpoint's host." example:"asia-southeast1-aiplatform.googleapis.com"`
	Feature       string          `json:"feature" maxLength:"128" doc:"The feature, such as messages, batch or caching, or * for every feature." example:"*"`
	ProcessedIn   []CountryCode   `json:"processed_in" minItems:"1" doc:"Where prompts are processed, as ISO 3166 codes: distinct from where they are stored (AI-88)."`
	StoredIn      []CountryCode   `json:"stored_in,omitempty" doc:"Where they are kept at rest."`
	RetentionDays *int            `json:"retention_days,omitempty" minimum:"0" maximum:"3650" doc:"How long the provider keeps prompts: 0 for zero data retention; none when not known." example:"0"`
	Training      string          `json:"training" enum:"no,yes,unknown" doc:"Whether the provider trains models on them." example:"no"`
	SubProcessors []ProcessorName `json:"sub_processors,omitempty" doc:"The provider's sub-processors for it."`
	Gateways      []ProcessorName `json:"gateways,omitempty" doc:"Any gateway in between, with its own handling."`
	Unclear       bool            `json:"unclear,omitempty" doc:"The evidence is unclear or conflicting: then it never counts as Singapore (AI-88)." example:"false"`
	Evidence      AIDataEvidence  `json:"evidence"`
}

// AIDataHandling is a connection's data-handling declaration (AI-75, AI-88).
type AIDataHandling struct {
	Handling    []AIDataHandlingEntry `json:"handling" nullable:"false" doc:"Every model, endpoint and feature the connection is used for."`
	InventoryID string                `json:"inventory_id,omitempty" doc:"Its row in the data inventory (SEC-13), which switching it on needs." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d61"`
	TransferID  string                `json:"transfer_id,omitempty" doc:"Its recorded transfer assessment (CMP-PD-06), which switching on needs when it processes outside Singapore." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d62"`
}

func (h AIDataHandling) domain() aipolicy.Declaration {
	d := aipolicy.Declaration{Inventory: h.InventoryID, Transfer: h.TransferID, Handling: []aipolicy.Handling{}}
	for _, e := range h.Handling {
		d.Handling = append(d.Handling, aipolicy.Handling{Model: e.Model, Endpoint: e.Endpoint, Feature: e.Feature, ProcessedIn: plain(e.ProcessedIn),
			StoredIn: plain(e.StoredIn), RetentionDays: e.RetentionDays, Training: e.Training, SubProcessors: plain(e.SubProcessors), Gateways: plain(e.Gateways),
			Unclear: e.Unclear, Evidence: aipolicy.Evidence{Kind: e.Evidence.Kind, URL: e.Evidence.URL, Title: e.Evidence.Title,
				CheckedOn: e.Evidence.CheckedOn, File: e.Evidence.EvidenceID}})
	}
	return d
}

func dataHandlingOut(raw json.RawMessage) AIDataHandling {
	var d aipolicy.Declaration
	out := AIDataHandling{Handling: []AIDataHandlingEntry{}}
	if json.Unmarshal(raw, &d) != nil {
		return out
	}
	out.InventoryID, out.TransferID = d.Inventory, d.Transfer
	for _, h := range d.Handling {
		out.Handling = append(out.Handling, AIDataHandlingEntry{Model: h.Model, Endpoint: h.Endpoint, Feature: h.Feature,
			ProcessedIn: named(h.ProcessedIn, asCountry), StoredIn: named(h.StoredIn, asCountry), RetentionDays: h.RetentionDays, Training: h.Training,
			SubProcessors: named(h.SubProcessors, asProcessor), Gateways: named(h.Gateways, asProcessor),
			Unclear: h.Unclear, Evidence: AIDataEvidence{Kind: h.Evidence.Kind, URL: h.Evidence.URL, Title: h.Evidence.Title,
				CheckedOn: h.Evidence.CheckedOn, EvidenceID: h.Evidence.File}})
	}
	return out
}

// policyProblem is a refusal of the data-class rules, with the field it is about.
func policyProblem(err error) error {
	var e *aipolicy.Error
	if errors.As(err, &e) {
		return problem(e.Code, e.Field+" "+e.Message+".", e.Fix, FieldProblem{Code: e.Code, Field: "body." + e.Field, Message: e.Message, Fix: e.Fix})
	}
	return err
}

// declared reads a connection's declaration.
func declared(c storage.AIConnection) aipolicy.Declaration {
	var d aipolicy.Declaration
	_ = json.Unmarshal(c.DataHandling, &d)
	return d
}

// recorded checks a connection's declaration against the Compliance Center before it is switched on: a declaration,
// its data inventory row open, and its transfer assessment open when it processes outside Singapore (SEC-13,
// CMP-PD-06).
func recorded(ctx context.Context, u storage.UnitOfWork, d aipolicy.Declaration) error {
	if len(d.Handling) == 0 {
		return problem(aipolicy.CodeDeclaration, "The connection declares no data handling.", "Declare where each model's prompts are processed, with evidence, then ask again")
	}
	entries, err := u.Compliance().InventoryEntries(ctx)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(entries, func(e storage.InventoryEntry) bool { return e.EntryID.String() == d.Inventory && e.State == "open" }) {
		return problem(aipolicy.CodeDeclaration, "The connection names no open row of the data inventory (SEC-13).",
			"Add its row to the data inventory in the Compliance Center and name it in the connection's data handling")
	}
	if d.Overseas() {
		transfers, err := u.Compliance().Transfers(ctx)
		if err != nil {
			return err
		}
		if !slices.ContainsFunc(transfers, func(t storage.Transfer) bool { return t.TransferID.String() == d.Transfer && t.State == "open" }) {
			return problem(aipolicy.CodeDeclaration, "The connection processes outside Singapore and names no open transfer assessment (CMP-PD-06).",
				"Record the transfer in the Compliance Center and name it in the connection's data handling")
		}
	}
	return nil
}

// AIDataPolicy is the organisation's data-class policy.
type AIDataPolicy struct {
	Version  int                 `json:"version" doc:"Its version; 0 before any is approved, when only the defaults apply." example:"1"`
	Allow    map[string][]string `json:"allow" doc:"Per data class, the connections it may also go to: personal-data and payroll only to a connection whose transfer is recorded; restricted never." example:"{\"business-figures\":[\"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d40\"]}"`
	Exclude  map[string][]string `json:"exclude" doc:"Per data class, the connections it may not go to although the defaults allow it." example:"{}"`
	Defaults string              `json:"defaults" doc:"The defaults that apply beyond it." example:"personal-data and payroll only where Singapore processing is evidenced; restricted nowhere; know-how and business-figures to any enabled connection"`
}

// AIDataPolicyChange asks for the policy's next version.
type AIDataPolicyChange struct {
	Allow   map[string][]string `json:"allow,omitempty" doc:"Per data class, the connections it may also go to." example:"{}"`
	Exclude map[string][]string `json:"exclude,omitempty" doc:"Per data class, the connections it may not go to." example:"{}"`
}

const policyDefaults = "personal-data and payroll only where Singapore processing is evidenced; restricted nowhere; know-how and business-figures to any enabled connection"

type (
	aiDataPolicyOutput struct {
		ETag string `header:"ETag" doc:"The policy's version, for If-Match."`
		Body AIDataPolicy
	}
	aiDataPolicyChangeInput struct {
		KeyHeader
		PreferHeader
		IfMatchHeader
		Body AIDataPolicyChange
	}
)

func policyOut(p storage.AIDataPolicy) AIDataPolicy {
	var dp aipolicy.Policy
	_ = json.Unmarshal(p.Policy, &dp)
	out := AIDataPolicy{Version: p.Version, Allow: map[string][]string{}, Exclude: map[string][]string{}, Defaults: policyDefaults}
	for k, v := range dp.Allow {
		out.Allow[k] = v
	}
	for k, v := range dp.Exclude {
		out.Exclude[k] = v
	}
	return out
}

// policyConnections are the organisation's connections as the policy sees them.
func policyConnections(ctx context.Context, u storage.UnitOfWork) (map[string]aipolicy.Connection, error) {
	all, err := u.AIConnections().List(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]aipolicy.Connection{}
	for _, c := range all {
		out[c.ID.String()] = aipolicy.Connection{ID: c.ID.String(), Enabled: c.State == "enabled", Declaration: declared(c)}
	}
	return out, nil
}

// aiDataPolicyDependencies are the policy's version (AI-75).
func aiDataPolicyDependencies(ctx context.Context, u storage.UnitOfWork) (int, map[string]any, error) {
	p, err := u.AIDataPolicies().Latest(ctx)
	if err != nil {
		return 0, nil, err
	}
	return p.Version, map[string]any{"version": p.Version}, nil
}

func registerAIDataPolicy(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	register(a, authz.GetAIDataPolicy, huma.Operation{
		OperationID: "getAIDataPolicy", Method: http.MethodGet, Path: "/v1/ai/data-policy", Tags: []string{"AI"},
		Summary: "Get the AI's data-class policy",
		Description: "Which AI connections each data class may go to (AI-75): the policy in force, with its ETag, and the defaults that apply " +
			"beyond it. Read-only; no side effects.",
	}, locks, func(ctx context.Context, _ *struct{}) (*aiDataPolicyOutput, error) {
		var out storage.AIDataPolicy
		p, _ := authn.From(ctx)
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			out, err = u.AIDataPolicies().Latest(ctx)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &aiDataPolicyOutput{ETag: etag(out.Version), Body: policyOut(out)}, nil
	})

	register(a, authz.ChangeAIDataPolicy, huma.Operation{
		OperationID: "changeAIDataPolicy", Method: http.MethodPut, Path: "/v1/ai/data-policy", Tags: []string{"AI"},
		DefaultStatus: http.StatusAccepted, Summary: "Ask to change the AI's data-class policy",
		Description: "Asks for the policy's next version (AI-75): per data class, the connections it may also go to and those it may not. " +
			"Restricted data never goes to an AI provider; personal data and payroll go overseas only on a connection whose transfer assessment " +
			"is recorded (CMP-PD-06). It is dual controlled (golden rule 6, FM-1): the answer is the approval request, which the Owner approves. " +
			"Needs If-Match with its ETag; idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing. On the never-list " +
			"for the AI and MCP (§9.6).",
	}, append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeSessionRequired, CodeStepUpRequired, CodeIfMatchRequired, CodeStale,
		storage.CodeVersion, approval.CodePending, aipolicy.CodePolicy}, locks...), func(ctx context.Context, in *aiDataPolicyChangeInput) (*aiConnectionAskOutput, error) {
		resp, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
			current, err := u.AIDataPolicies().Latest(ctx)
			if err != nil {
				return Approval{}, err
			}
			if err := checkIfMatch(in.IfMatch, etag(current.Version)); err != nil {
				return Approval{}, err
			}
			next := aipolicy.Policy{Allow: in.Body.Allow, Exclude: in.Body.Exclude}
			connections, err := policyConnections(ctx, u)
			if err != nil {
				return Approval{}, err
			}
			if err := aipolicy.CheckPolicy(next, connections); err != nil {
				return Approval{}, policyProblem(err)
			}
			raw, err := json.Marshal(next)
			if err != nil {
				return Approval{}, err
			}
			return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectAIDataPolicy, ObjectKey: approval.AIDataPolicyKey,
				Operation: approval.OpAIDataPolicyChange, Summary: "Change where the AI may send each kind of data",
				Execute: map[string]any{"policy": string(raw), "version": current.Version + 1},
				Diff:    []ApprovalDiff{{Field: "policy", Before: string(current.Policy), After: string(raw)}}})
		})
		if err != nil {
			return nil, err
		}
		return &aiConnectionAskOutput{Status: resp.Status, Replayed: replayedHeader(resp.Replayed), Applied: applied(resp), Body: resp.Body}, nil
	})
}

// executeAIDataPolicy makes an approved policy the organisation's next version, after checking it once more against
// the connections as they are now (AI-75, CMP-PD-06).
func (a *API) executeAIDataPolicy(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest, s stored) error {
	raw, _ := s.Execute["policy"].(string)
	var next aipolicy.Policy
	if err := json.Unmarshal([]byte(raw), &next); err != nil {
		return err
	}
	connections, err := policyConnections(ctx, u)
	if err != nil {
		return err
	}
	if err := aipolicy.CheckPolicy(next, connections); err != nil {
		return policyProblem(err)
	}
	version, err := strconv.Atoi(numberText(s.Execute["version"]))
	if err != nil {
		return err
	}
	_, err = u.AIDataPolicies().Save(ctx, storage.AIDataPolicy{Version: version, Policy: []byte(raw), Approval: r.ID,
		MadeBy: "user:" + r.RequestedBy.String(), MadeAt: a.deps.Clock.Now()})
	return err
}

// numberText is an Execute number as text: JSON numbers come back as float64.
func numberText(v any) string {
	switch n := v.(type) {
	case float64:
		return strconv.FormatInt(int64(n), 10)
	case int:
		return strconv.Itoa(n)
	case json.Number:
		return n.String()
	}
	return ""
}
