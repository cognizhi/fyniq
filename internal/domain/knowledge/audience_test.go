package knowledge_test

import (
	"encoding/json"
	"flag"
	"os"
	"strings"
	"testing"

	userguide "github.com/cognizhi/fyniq/docs/user-guide"
	"github.com/cognizhi/fyniq/internal/domain/knowledge"
	"github.com/cognizhi/fyniq/kb"
)

var update = flag.Bool("update", false, "rewrite test/kb/guide-audience.json from the guide as it is")

// The audience of every guide page, as the server reads it, is the audience the web app's guide reads (AI-54; design D2): the
// knowledge base's reader filter is built on the guide pages' `roles`, which apps/web/src/help/guide.ts parses on its own for
// what it shows a person. This file is what the Go parser makes of the guide; the web app's Vitest suite
// (apps/web/src/help/guide-audience.test.ts) checks its parser against the same file, so the two cannot drift.
// req: AI-54
func TestTheGuideAudienceIsTheSameInGoAndTheWebApp(t *testing.T) {
	c, err := knowledge.Load(kb.FS, userguide.FS)
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		File   string   `json:"file"`
		Key    string   `json:"key"`
		Roles  []string `json:"roles"`
		Status string   `json:"status"`
	}
	var rows []row
	for _, g := range c.Guide {
		if g.Key == "" || strings.HasPrefix(g.Path, knowledge.GuidePrefix+"nemura/") {
			continue // a page with no key is not part of the guide, and Nemura's guide is Nemura's own app's (guide.ts)
		}
		rows = append(rows, row{File: g.Path[len(knowledge.GuidePrefix):], Key: g.Key, Roles: g.Roles, Status: g.Status})
	}
	got, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	const path = "../../../test/kb/guide-audience.json"
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil || string(want) != string(got) {
		t.Errorf("test/kb/guide-audience.json is not what the guide says (err %v); run go test ./internal/domain/knowledge -update and review the diff", err)
	}
}
