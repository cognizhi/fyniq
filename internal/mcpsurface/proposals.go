package mcpsurface

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strings"

	contracts "github.com/cognizhi/fyniq/contracts/ai"
	"github.com/cognizhi/fyniq/internal/aitool"
	"github.com/cognizhi/fyniq/internal/neverlist"
)

// The proposal tools (AI-16, AI-42, AI-34; p1-mcp-first-party tasks 7.1 to 7.3): the one write an MCP client has, and it is not a
// write: a proposal from the catalogue, which waits for a person to approve it in Fyniq. They are not tools of the registry
// (which holds bounded reads); they are generated from the catalogue and the reviewed list in mcp-scopes.json, so a task type is
// invisible to MCP until a reviewer names it there. A client gets no approve, reject, execute or withdraw: those are never tools.
const (
	// ToolProposalsList and ToolProposalsGet read the proposals the app made for the person, the person's own only (AI-42).
	ToolProposalsList = "proposals.list"
	ToolProposalsGet  = "proposals.get"
	// ProposalsReadScope is what the two reads need.
	ProposalsReadScope = "fyniq.read.proposals"
	proposeToolPrefix  = "propose_"
)

// ProposeScope is the scope shape of a proposal tool: fyniq.propose.<task type> (AI-37).
var ProposeScope = regexp.MustCompile(`^fyniq\.propose\.[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$`)

// IsProposalTool reports whether a tool name is one of the proposal tools.
func IsProposalTool(name string) bool {
	return name == ToolProposalsList || name == ToolProposalsGet || strings.HasPrefix(name, proposeToolPrefix)
}

// ProposeToolOf is the name of the tool that proposes a task type over MCP: the name the model is offered, too.
func ProposeToolOf(taskType string) string {
	return proposeToolPrefix + strings.ReplaceAll(taskType, ".", "_")
}

// TaskTypeOf is the task type a propose tool is for, or "" when the name is not that of a task type the map exposes.
func (s Scopes) TaskTypeOf(tool string) string {
	for _, id := range s.Propose {
		if ProposeToolOf(id) == tool {
			return id
		}
	}
	return ""
}

func boolp(b bool) *bool { return &b }

// proposalDefinitions are the pinned definitions of the proposal tools: the two reads the map names, then one propose tool for each
// task type the map names, in the map's order. A name that is not a type of the catalogue, or one that may never be proposed,
// fails the build.
func proposalDefinitions(s Scopes, cat map[string]task) ([]Definition, error) {
	var out []Definition
	add := func(d Definition) error {
		var err error
		if d.SHA256, err = d.hash(); err != nil {
			return err
		}
		out = append(out, d)
		return nil
	}
	reads := aitool.Annotations{ReadOnly: boolp(true), Destructive: boolp(false), Idempotent: boolp(true), OpenWorld: boolp(false)}
	for _, d := range []Definition{
		{Name: ToolProposalsList, Title: "List the changes you proposed",
			Description: "Lists the changes this app proposed for you, newest first, with each one's status: waiting for a person to approve it, approved, rejected, expired, executed or not. Only your own proposals are listed.",
			InputSchema: json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"status":{"type":"string","enum":["pending","approved","rejected","expired","superseded","executed","stale","failed","invalid"],"description":"Only proposals in this status."},"limit":{"type":"integer","minimum":1,"maximum":50,"description":"How many to return, at most 50."}}}`)},
		{Name: ToolProposalsGet, Title: "See one change you proposed",
			Description: "Shows one change this app proposed for you: what it would do, in Fyniq's words, and its status. Only your own proposals can be seen.",
			InputSchema: json.RawMessage(`{"type":"object","additionalProperties":false,"required":["id"],"properties":{"id":{"type":"string","maxLength":36,"pattern":"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$","description":"The proposal's ID, from proposals.list or the answer to a proposal."}}}`)},
	} {
		r, named := s.Tools[d.Name]
		if !named { // default deny: a reviewer maps a read before it is exposed
			continue
		}
		d.Annotations, d.Scopes, d.Version = reads, scopesOf(r), 1
		if err := add(d); err != nil {
			return nil, err
		}
	}
	for _, id := range s.Propose {
		tt, ok := cat[id]
		if !ok {
			return nil, fmt.Errorf("mcp-scopes.json proposes %s, which is not a task type of the catalogue", id)
		}
		if why := neverlist.Refuse(tt.ID, tt.Command); why != "" {
			return nil, fmt.Errorf("%s may not be proposed over MCP: it is %s", id, why)
		}
		// A proposal is not a write: it changes nothing until a person approves it in Fyniq, and it is never destructive, idempotent
		// or open to the world (AI-34).
		if err := add(Definition{Name: ProposeToolOf(id), Title: tt.Name, Description: tt.Description + " A person approves it in Fyniq before anything changes.", InputSchema: proposeSchema(tt.Name, tt.InputSchema),
			Annotations: aitool.Annotations{ReadOnly: boolp(false), Destructive: boolp(false), Idempotent: boolp(false), OpenWorld: boolp(false)},
			Scopes:      []string{"fyniq.propose." + id}, Version: tt.Version}); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// checkPropose is what the map's reviewed list of task types must hold: unique, each a scope shape.
func checkPropose(ids []string) error {
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] || !ProposeScope.MatchString("fyniq.propose."+id) {
			return fmt.Errorf("mcp-scopes.json: propose names %q twice or not as a task type", id)
		}
		seen[id] = true
	}
	if !slices.IsSorted(ids) {
		return fmt.Errorf("mcp-scopes.json: propose is kept sorted, so a change shows in review")
	}
	return nil
}

// task is the part of a catalogue entry (contracts/ai/tasks) the MCP surface needs. The catalogue's own loader (internal/proposal)
// validates the whole entry; this reads the same files for the text a client is shown, so the MCP service carries none of the
// proposal service's code. A test keeps the two the same text.
type task struct {
	ID          string          `json:"id"`
	Version     int             `json:"version"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	Command     string          `json:"command"`
}

// loadTasks reads the catalogue's entries from the contracts the release embeds.
func loadTasks() (map[string]task, error) {
	paths, err := fs.Glob(contracts.Tasks, "tasks/*.json")
	if err != nil {
		return nil, err
	}
	out := map[string]task{}
	for _, p := range paths {
		b, err := fs.ReadFile(contracts.Tasks, p)
		if err != nil {
			return nil, err
		}
		var t task
		if err := json.Unmarshal(b, &t); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		if path.Base(p) != t.ID+".json" {
			return nil, fmt.Errorf("%s: a task type's file is its ID and .json", p)
		}
		out[t.ID] = t
	}
	return out, nil
}

// proposeSchema is the input of a propose tool: the payload by the task type's own schema, the reasons for the approver, the records
// relied on, and a proposal of the app's still waiting that this one replaces. It is the model tool's schema, word for word.
func proposeSchema(name string, payload json.RawMessage) json.RawMessage {
	var in map[string]any
	_ = json.Unmarshal(payload, &in)
	if _, ok := in["description"]; !ok {
		in["description"] = "What to do: " + name + "."
	}
	p, _ := json.Marshal(in)
	return json.RawMessage(`{"type":"object","additionalProperties":false,"required":["payload","rationale"],"properties":{"payload":` + string(p) +
		`,"rationale":{"type":"string","maxLength":4000,"description":"Why you propose it, for the person who approves it."},` +
		`"evidence":{"type":"array","maxItems":50,"description":"The records and documents you relied on.","items":{"type":"object",` +
		`"additionalProperties":false,"required":["kind","id"],"properties":{"kind":{"type":"string","maxLength":40,` +
		`"description":"The kind of record, such as task or invoice."},"id":{"type":"string","maxLength":100,"description":"Its ID."},` +
		`"title":{"type":"string","maxLength":200,"description":"What it is, in a few words."}}}},` +
		`"revises":{"type":"string","maxLength":36,"pattern":"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$","description":"To change a proposal of yours that still waits, its ID: this one ` +
		`replaces it, and the person approves this one instead."}}}`)
}
