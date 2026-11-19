package mcpsurface

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/aitool"
	"github.com/cognizhi/fyniq/internal/neverlist"
	"github.com/cognizhi/fyniq/internal/proposal"
)

func proposalTools(t *testing.T) []Definition {
	t.Helper()
	m, _ := Default()
	var out []Definition
	for _, d := range m.Tools {
		if IsProposalTool(d.Name) {
			out = append(out, d)
		}
	}
	return out
}

// The one thing an MCP client may write is a proposal from the catalogue, which waits for a person (AI-16, AI-42, AI-34): a
// propose tool for each type the map names and no other, each needing its own scope and nothing else, never destructive,
// idempotent or open to the world, with the catalogue's own text; the two reads need the proposals' read scope. No tool anywhere
// approves, rejects, executes, withdraws or pays.
// req: AI-16
// req: AI-42
// req: AI-34
// req: CTL-MCP-01
// req: CTL-MCP-02
// asvs: V13.1.1, V8.1.1
func TestTheOnlyWriteAClientHasIsAProposalThatWaitsForAPerson(t *testing.T) {
	s := scopes(t)
	cat := proposal.Default()
	var proposes []string
	for _, d := range proposalTools(t) {
		switch {
		case d.Name == ToolProposalsList || d.Name == ToolProposalsGet:
			if d.Annotations.ReadOnly == nil || !*d.Annotations.ReadOnly || !slices.Equal(d.Scopes, []string{ProposalsReadScope}) {
				t.Errorf("%s: %+v", d.Name, d)
			}
		default:
			id := s.TaskTypeOf(d.Name)
			tt, ok := cat.Get(id)
			if id == "" || !ok {
				t.Fatalf("%s is no task type the map exposes", d.Name)
			}
			proposes = append(proposes, id)
			a := d.Annotations
			if a.ReadOnly == nil || *a.ReadOnly || a.Destructive == nil || *a.Destructive || a.Idempotent == nil || *a.Idempotent || a.OpenWorld == nil || *a.OpenWorld {
				t.Errorf("%s: a proposal is no read and no destructive, idempotent or open-world act: %+v", d.Name, a)
			}
			if !slices.Equal(d.Scopes, []string{"fyniq.propose." + id}) || !ProposeScope.MatchString(d.Scopes[0]) {
				t.Errorf("%s needs %v, want only fyniq.propose.%s", d.Name, d.Scopes, id)
			}
			// The surface reads the catalogue's files itself, so the MCP service carries none of the proposal service's code: what a
			// client is shown must still be exactly the text and the schema the model is offered.
			mt := tt.ModelTool()
			var got, want any
			_ = json.Unmarshal(d.InputSchema, &got)
			_ = json.Unmarshal(mt.Schema, &want)
			if d.Description != mt.Description || d.Title != tt.Name || d.Version != tt.Version || d.Name != mt.Name || !reflect.DeepEqual(got, want) {
				t.Errorf("%s: its text or schema is not the catalogue's", d.Name)
			}
			if what := neverlist.Refuse(tt.ID, tt.Command); what != "" {
				t.Errorf("%s is on the never-list: %s", id, what)
			}
		}
	}
	if !slices.Equal(proposes, s.Propose) {
		t.Errorf("the manifest proposes %v, the map names %v", proposes, s.Propose)
	}
	// Nothing in the manifest has a verb that decides, executes or pays, whatever its annotations say.
	m, _ := Default()
	for _, d := range m.Tools {
		verb := d.Name[strings.LastIndex(d.Name, ".")+1:]
		for _, bad := range []string{"approve", "reject", "execute", "withdraw", "sign", "pay", "post", "export", "delete", "revoke", "grant"} {
			if strings.Contains(verb, bad) {
				t.Errorf("%s: its name says it %ss", d.Name, bad)
			}
		}
	}
}

// What a call of a proposal tool needs (AI-37): the scope of its task type alone, so a client allowed to propose one type cannot
// propose another, and the read scope alone does not let it propose; a type the map does not name is not a tool.
// req: AI-37
// req: AI-42
// asvs: V8.1.1
func TestAProposalToolNeedsTheScopeOfItsTaskTypeAlone(t *testing.T) {
	s := scopes(t)
	got, err := s.Needs(ProposeToolOf("sec.create_task"), map[string]any{})
	if err != nil || !slices.Equal(got, []string{"fyniq.propose.sec.create_task"}) {
		t.Errorf("propose_sec_create_task: %v %v", got, err)
	}
	if got, err := s.Needs(ToolProposalsList, nil); err != nil || !slices.Equal(got, []string{"fyniq.read.proposals"}) {
		t.Errorf("proposals.list: %v %v", got, err)
	}
	for _, tool := range []string{"propose_sec_approve", "propose_", "propose_made_up", "proposals.approve", "proposals.execute", "proposals.withdraw", "approve_proposal"} {
		if _, err := s.Needs(tool, nil); err == nil {
			t.Errorf("%s is a tool the server exposes", tool)
		}
	}
}

// A task type is exposed over MCP only when a reviewer names it in the map; a name that is not in the catalogue, one listed twice
// or out of order, or a type that may never be proposed fails the build (default deny, CTL-MCP-02).
// req: AI-40
// req: CTL-MCP-02
// asvs: V13.1.1
func TestATaskTypeIsExposedOnlyWhenAReviewerNamesIt(t *testing.T) {
	base := scopes(t)
	tools := aitool.Default().Tools()
	none := base
	none.Propose = nil
	m, err := Build(tools, none)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range m.Tools {
		if strings.HasPrefix(d.Name, proposeToolPrefix) {
			t.Errorf("%s is exposed with no type named", d.Name)
		}
	}
	bad := base
	bad.Propose = []string{"sec.not_a_type"}
	if _, err := Build(tools, bad); err == nil {
		t.Error("a task type the catalogue lacks was accepted")
	}
	if err := checkPropose([]string{"sec.create_task", "sec.create_task"}); err == nil {
		t.Error("a type listed twice passed the map's check")
	}
	if err := checkPropose([]string{"sec.draft_reminder", "sec.create_task"}); err == nil {
		t.Error("an unsorted list passed")
	}
	if err := checkPropose([]string{"Sec.Create"}); err == nil {
		t.Error("a name that is not a scope shape passed")
	}
}
