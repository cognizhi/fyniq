package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/evidence"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the evidence rules table (FR-DOC-06)
//
// PRD-001 §8.16's table of required and recommended evidence per activity, with each configurable activity's level in
// force today, for the Evidence panel's "Rules for this activity". Read-only; it holds no figure and no record.

// EvidenceKind is what a file is, as an uploader names it (an evidence type).
type EvidenceKind string

// Schema implements huma.SchemaProvider.
func (EvidenceKind) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Description: "What a file is, such as bank_letter or receipt.", Examples: []any{"bank_letter"}}
}

// EvidenceRuleItem is a piece of evidence a rule names.
type EvidenceRuleItem struct {
	Label string         `json:"label" doc:"In the table's words." example:"Receipt or invoice"`
	Types []EvidenceKind `json:"types" nullable:"false" doc:"Evidence types that meet it, as a file names what it is; none means any file does."`
}

// EvidenceRule is one row of the table.
type EvidenceRule struct {
	Key         string             `json:"key" doc:"The activity." example:"bill"`
	Area        string             `json:"area" enum:"purchases,journals,filings,equity,governance" doc:"The group of activities it belongs to." example:"purchases"`
	Label       string             `json:"label" doc:"The activity in a sentence." example:"Bill"`
	Level       string             `json:"level" enum:"required,recommended,optional" doc:"How its evidence counts today: the organisation's setting where there is one, else the table's default." example:"required"`
	Setting     string             `json:"setting,omitempty" doc:"The organisation setting that holds its level (FR-ORG-08), changed under dual control; none when its module fixes it." example:"evidence.bill"`
	Built       bool               `json:"built" doc:"Whether its module exists, so the rule is enforced now; otherwise it starts to apply with its module." example:"true"`
	Required    []EvidenceRuleItem `json:"required" nullable:"false" doc:"What it needs; a missing item blocks completing it while its level is required."`
	Recommended []EvidenceRuleItem `json:"recommended" nullable:"false" doc:"What is recommended; a missing item is a warning."`
}

type getEvidenceRulesOutput struct {
	Body struct {
		Rules []EvidenceRule `json:"rules" nullable:"false" doc:"The table's rows, in its order."`
	}
}

func registerEvidenceRulesList(a *API) {
	register(a, authz.GetEvidenceRules, huma.Operation{
		OperationID: "getEvidenceRules", Method: http.MethodGet, Path: "/v1/evidence-rules", Tags: []string{"Evidence"},
		Summary: "List the evidence rules",
		Description: "The required and recommended evidence of each kind of activity (FR-DOC-06; PRD-001 §8.16), with each " +
			"configurable activity's level in force today: required blocks completing it, recommended warns (C.2 #30), optional " +
			"asks for nothing. Read-only; any member may read it.",
	}, nil, func(ctx context.Context, _ *struct{}) (*getEvidenceRulesOutput, error) {
		p, _ := authn.From(ctx)
		out := &getEvidenceRulesOutput{}
		out.Body.Rules = []EvidenceRule{}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			for _, r := range evidence.Catalogue {
				level := r.Level
				if r.Setting != "" {
					v, err := settingToday(ctx, a, u, r.Setting)
					if err != nil {
						return err
					}
					level = v
				}
				out.Body.Rules = append(out.Body.Rules, EvidenceRule{Key: r.Key, Area: r.Area, Label: r.Label, Level: level, Setting: r.Setting,
					Built: r.Built, Required: ruleItems(r.Required), Recommended: ruleItems(r.Recommended)})
			}
			return nil
		})
		return out, err
	})
}

func ruleItems(in []evidence.Item) []EvidenceRuleItem {
	out := make([]EvidenceRuleItem, len(in))
	for i, x := range in {
		out[i] = EvidenceRuleItem{Label: x.Label, Types: make([]EvidenceKind, len(x.Types))}
		for j, t := range x.Types {
			out[i].Types[j] = EvidenceKind(t)
		}
	}
	return out
}
