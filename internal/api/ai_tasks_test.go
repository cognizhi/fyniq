//go:build local

package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/proposal"
)

// fixtureCatalogue is a catalogue of test-only task types, one per ID.
func fixtureCatalogue(t *testing.T, ids ...string) *proposal.Catalogue {
	t.Helper()
	fsys := fstest.MapFS{}
	for _, id := range ids {
		b, _ := json.Marshal(map[string]any{
			"id": id, "version": 1, "name": "Annotate a note", "description": "Adds a note to a record. A fixture of the tests.",
			"inputSchema": map[string]any{"type": "object", "additionalProperties": false, "required": []any{"note"},
				"properties": map[string]any{"note": map[string]any{"type": "string", "maxLength": 200, "description": "The note."}}},
			"command": "AnnotateNote", "tier": 1, "limits": map[string]any{"items": 5}, "approvers": []any{"owner"}, "expiry_hours": 72,
		})
		fsys["tasks/"+id+".json"] = &fstest.MapFile{Data: b}
	}
	c, err := proposal.Load(fsys)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// Every task type is off until the Owner turns it on with a second approver (AI-32); a type the release's catalogue
// lacks is refused; taking types off, or all of them, is a kill switch that takes effect at once and voids a waiting
// request to change them; a change that adds any, even while taking another off, still waits for the second approver.
// req: AI-32
// req: AI-13
// req: ai/task-catalogue Enabled per organisation, off by default
// req: CTL-AI-04
// asvs: V8.1.4
func TestTaskTypesAreOffUntilEnabledUnderDualControl(t *testing.T) {
	e := newEnv(t)
	f := foundersIn(e)
	long := []string{}
	for _, c := range "abcdefghij" {
		long = append(long, "test.annotate_a_rather_longer_note_"+string(c))
	}
	e.api.deps.Catalogue = fixtureCatalogue(t, append(long, "test.annotate_note", "test.tag_note")...)
	ahkow, weiling := f.as(f.ahkow), f.as(f.weiling)
	const path = "/v1/organisation/settings/ai.task_types"
	get := func() response { return e.call(http.MethodGet, path, ahkow, nil) }
	set := func(value string) response {
		return e.call(http.MethodPatch, path, ahkow, map[string]any{"value": value}, "If-Match", get().Header.Get("ETag"))
	}

	if g := get(); g.Status != 200 || g.str("value") != "" || g.str("kind") != "task_types" || g.Body["off_at_once"] != true ||
		g.Body["needs_second_approver"] != true {
		t.Fatalf("off by default: %s", g.Raw)
	}
	e.problemIs(set("test.missing"), "updateSetting", 422, CodeInvalid)
	e.problemIs(set("Test.annotate_note"), "updateSetting", 422, CodeInvalid)

	// Adding waits for a second approver.
	asked := set("test.tag_note, test.annotate_note")
	if asked.Status != http.StatusAccepted || get().str("value") != "" {
		t.Fatalf("asking to enable two: %d %s", asked.Status, asked.Raw)
	}
	if d := f.decide(weiling, asked.str("id"), "approve", hashOf(asked)); d.Status != 200 {
		t.Fatalf("approving: %d %s", d.Status, d.Raw)
	}
	if g := get(); g.str("value") != "test.annotate_note,test.tag_note" {
		t.Fatalf("once approved: %s", g.Raw)
	}

	// Taking one off takes effect at once; swapping one for another still waits.
	if r := set("test.annotate_note"); r.Status != 200 || r.str("value") != "test.annotate_note" {
		t.Fatalf("taking one off: %d %s", r.Status, r.Raw)
	}
	waiting := set("test.tag_note")
	if waiting.Status != http.StatusAccepted {
		t.Fatalf("swapping one for another: %d %s", waiting.Status, waiting.Raw)
	}

	// Emptying it takes effect at once and voids the waiting requests.
	if r := set(""); r.Status != 200 || r.str("value") != "" {
		t.Fatalf("taking all off: %d %s", r.Status, r.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/approvals/"+waiting.str("id"), weiling, nil); g.str("status") != "void" {
		t.Fatalf("the waiting request after all were taken off: %s", g.Raw)
	}
	e.problemIs(f.decide(weiling, waiting.str("id"), "approve", hashOf(waiting)), "approveApproval", 409, approval.CodeChanged)
	if g := get(); g.str("value") != "" {
		t.Fatalf("a voided request enabled a type: %s", g.Raw)
	}

	// Every type of a long catalogue fits, past the 200 characters other settings hold.
	asked = set(strings.Join(long, ","))
	if asked.Status != http.StatusAccepted {
		t.Fatalf("asking to enable ten: %d %s", asked.Status, asked.Raw)
	}
	if d := f.decide(weiling, asked.str("id"), "approve", hashOf(asked)); d.Status != 200 || get().str("value") != strings.Join(long, ",") {
		t.Fatalf("enabling ten: %d %s", d.Status, d.Raw)
	}
}

// A list setting can be emptied again: the fields the AI never reads, once added, are taken off with a second approver,
// which stored an empty value the database refused (regression, p1-ai-proposals-core task 1.2).
// req: AI-02
func TestTheAIExclusionsCanBeEmptiedAgain(t *testing.T) {
	e := newEnv(t)
	f := foundersIn(e)
	ahkow, weiling := f.as(f.ahkow), f.as(f.weiling)
	const path = "/v1/organisation/settings/ai.exclusions"
	for _, value := range []string{"notes", ""} {
		asked := e.call(http.MethodPatch, path, ahkow, map[string]any{"value": value}, "If-Match", e.call(http.MethodGet, path, ahkow, nil).Header.Get("ETag"))
		if asked.Status != http.StatusAccepted {
			t.Fatalf("asking for %q: %d %s", value, asked.Status, asked.Raw)
		}
		if d := f.decide(weiling, asked.str("id"), "approve", hashOf(asked)); d.Status != 200 {
			t.Fatalf("approving %q: %d %s", value, d.Status, d.Raw)
		}
		if g := e.call(http.MethodGet, path, ahkow, nil); g.str("value") != value {
			t.Fatalf("after approving %q: %s", value, g.Raw)
		}
	}
}
