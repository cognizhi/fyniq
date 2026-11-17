package proposal_test

import (
	"encoding/json"
	"maps"
	"slices"
	"testing"

	"github.com/cognizhi/fyniq/internal/domain/corporate"
	"github.com/cognizhi/fyniq/internal/proposal"
)

// sec.draft_document asks for what Fyniq's templates do: its kinds are the templates' kinds, and its fields are every
// field a template asks for, lines as arrays and the rest as text, and no other; it is tier 1, one document a proposal,
// approved by the Owner or the Company secretary, and maps to DraftCorporateDocument.
// req: ai/task-catalogue sec.draft_document Draft a corporate document
// req: ai/task-catalogue sec.draft_document Tier 1
// req: ai/task-catalogue sec.draft_document Approver is Owner or Company secretary
// req: FR-CS-05
func TestTheDraftDocumentTypeAsksForTheTemplatesFields(t *testing.T) {
	tt, ok := proposal.Default().Get("sec.draft_document")
	if !ok {
		t.Fatal("sec.draft_document is not in the release's catalogue")
	}
	if tt.Tier != proposal.Tier1 || tt.Limits.Items != 1 || tt.Command != "DraftCorporateDocument" || proposal.Release[tt.Command] == nil ||
		len(tt.Approvers) != 2 || tt.Approvers[0] != "owner" || tt.Approvers[1] != "company_secretary" {
		t.Errorf("the task type: %+v", tt)
	}
	var schema struct {
		Properties struct {
			Kind struct {
				Enum []string `json:"enum"`
			} `json:"kind"`
			Fields struct {
				Properties map[string]struct {
					Type string `json:"type"`
				} `json:"properties"`
			} `json:"fields"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(tt.InputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(schema.Properties.Kind.Enum, corporate.Kinds) {
		t.Errorf("its kinds %v are not the templates' %v", schema.Properties.Kind.Enum, corporate.Kinds)
	}
	asked := map[string]string{}
	for _, k := range corporate.Kinds {
		for _, f := range corporate.Templates[k].Fields {
			asked[f.Name] = map[bool]string{true: "array", false: "string"}[f.Type == corporate.FieldLines]
		}
	}
	have := map[string]string{}
	for name, p := range schema.Properties.Fields.Properties {
		have[name] = p.Type
	}
	if !maps.Equal(asked, have) {
		t.Errorf("its fields %v are not the templates' %v", have, asked)
	}
}
