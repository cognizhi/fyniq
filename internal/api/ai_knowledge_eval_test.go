//go:build local

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The knowledge evals' retrieval half (QA-AI-06; p1-knowledge-base design D10): at least 100 golden help and policy questions
// in the categories the requirement names, each with the page that answers it, asked of the knowledge base the release ships
// through the tools an assistant calls, as the person who would ask. It measures retrieval recall per category, that every
// citation resolves to a guide page the reader can open and none to a compiled page, how many misses the guide's own text
// still recovers (the fallback of AI-51), and that a reader whose access was revoked finds nothing of a page: not in the
// index, not in the search, not in a read. A category below its threshold fails; a category below it for two releases needs
// a named owner and a fix plan, which its entry in the suite carries.
//
// What this does not prove, and is `[live-verify]`: that a model reads the index, reads the page and answers from it, and,
// for a stale guide against the pack in force, that its answer comes from packs.get and says the guide may be out of date.
// Those need a model's recorded runs (evals/cassettes), so QA-AI-06 as a whole is not ticked by this test.
type knowledgeSuite struct {
	Suite      string                       `json:"suite"`
	Version    int                          `json:"version"`
	Top        int                          `json:"top"`
	Thresholds map[string]float64           `json:"thresholds"`
	Owners     map[string]map[string]string `json:"owners"`
	Cases      []knowledgeCase              `json:"cases"`
}

type knowledgeCase struct {
	ID       string   `json:"id"`
	Category string   `json:"category"`
	Question string   `json:"question"`
	Expect   []string `json:"expect"`
	Reader   string   `json:"reader,omitempty"`
	Live     string   `json:"live,omitempty"`
}

// req: AI-54
// req: AI-58
// asvs: V8.2.1
// (Not tagged QA-AI-06: it proves the retrieval half only, which the comment above says.)
func TestTheKnowledgeSuiteRetrievesCitesAndHidesWhatARevokedReaderMayNotOpen(t *testing.T) {
	raw, err := os.ReadFile("../../evals/knowledge/knowledge.json")
	if err != nil {
		t.Fatal(err)
	}
	var suite knowledgeSuite
	if err := json.Unmarshal(raw, &suite); err != nil {
		t.Fatal(err)
	}
	if len(suite.Cases) < 100 {
		t.Fatalf("the suite holds %d questions; QA-AI-06 asks for at least 100", len(suite.Cases))
	}
	e := newEnv(t)
	corpus := shippedKB(t)
	code := e.call(http.MethodPost, "/v1/members/invitations", e.as(authz.Owner), map[string]any{"display_name": "Staff", "roles": []string{"employee"}}).str("code")
	f := foundersIn(e)
	service, err := e.services.Token(ServiceAI)
	if err != nil {
		t.Fatal(err)
	}
	grant := func(user string) string {
		t.Helper()
		var token string
		e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
			token, err = e.api.issueAIGrant(ctx, u, e.books.Org, uuid.MustParse(user), uuid.New())
			return err
		})
		return token
	}
	tokens := map[string]string{"": grant(f.ahkow), "owner": grant(f.ahkow)}
	staff := e.call(http.MethodPost, "/v1/members", e.principal(authn.Principal{Subject: "staff-" + uuid.NewString(), Client: "web"}),
		map[string]any{"invitation_code": code, "display_name": "Staff"}).str("user_id")
	tokens["employee"] = grant(staff)
	tool := func(reader, name string, input map[string]any) response {
		return e.call(http.MethodPost, "/internal/v1/tool-calls", tokens[reader], map[string]any{"tool": name, "input": input}, ServiceTokenHeader, service)
	}
	hitKeys := func(r response) (pages, sources []string) {
		c, _ := r.Body["content"].(map[string]any)
		hits, _ := c["hits"].([]any)
		for _, h := range hits {
			m := h.(map[string]any)
			if m["kind"] == "page" {
				pages = append(pages, m["key"].(string))
			} else {
				sources = append(sources, m["key"].(string))
			}
		}
		return pages, sources
	}
	pageKeys := map[string]bool{}
	for _, p := range corpus.Pages {
		pageKeys[p.Key] = true
	}
	guideKeys := map[string]bool{}
	for _, g := range corpus.Guide {
		guideKeys[g.Key] = true
	}
	in := func(list []string, keys []string) bool {
		for _, k := range keys {
			for _, x := range list {
				if x == k {
					return true
				}
			}
		}
		return false
	}
	type tally struct{ asked, found, recovered, hidden int }
	byCategory := map[string]*tally{}
	var missed []string
	for _, c := range suite.Cases {
		for _, k := range c.Expect {
			if !pageKeys[k] {
				t.Fatalf("%s expects the page %s, which the release does not ship", c.ID, k)
			}
		}
		y := byCategory[c.Category]
		if y == nil {
			y = &tally{}
			byCategory[c.Category] = y
		}
		y.asked++
		// The page is found by the words of the question, as the person who asks may read.
		reader := "owner"
		pages, _ := hitKeys(tool(reader, "kb.search", map[string]any{"query": c.Question, "scope": "pages", "limit": suite.Top}))
		if in(pages, c.Expect) {
			y.found++
		} else {
			missed = append(missed, c.ID)
			// The fallback: the guide's own text still leads to the guide page the page was compiled from.
			_, sources := hitKeys(tool(reader, "kb.search", map[string]any{"query": c.Question, "scope": "sources", "limit": 10}))
			for _, k := range c.Expect {
				page := corpus.Pages[0]
				for _, p := range corpus.Pages {
					if p.Key == k {
						page = p
					}
				}
				for _, s := range page.Sources {
					if g, ok := corpus.GuideAt(s.Path); ok && in(sources, []string{g.Key}) {
						y.recovered++
						goto recovered
					}
				}
			}
		recovered:
		}
		if c.Reader == "employee" {
			// A reader whose access was revoked: the page is not in their index, not in a search of pages or of the guide, and
			// a read is refused as a page that is not there. Its summary and snippets are not there either.
			k := c.Expect[0]
			page := ""
			for _, p := range corpus.Pages {
				if p.Key == k {
					page = p.Summary
				}
			}
			idx, _ := tool("employee", "kb.index", map[string]any{"section": kbSection(k)}).Body["content"].(map[string]any)["pages"].([]any)
			s := tool("employee", "kb.search", map[string]any{"query": c.Question, "scope": "both", "limit": 10})
			rd := tool("employee", "kb.read", map[string]any{"key": k})
			sp, ss := hitKeys(s)
			if containsKey(idx, k) || in(sp, c.Expect) || strings.Contains(s.Raw, page) || rd.Status != http.StatusUnprocessableEntity ||
				strings.Contains(rd.Raw, page) {
				t.Errorf("%s: a reader without the access found %s: index %v, search %v %v, read %d", c.ID, k, idx, sp, ss, rd.Status)
			} else {
				y.hidden++
			}
		}
	}
	// Every page of the release reads with citations that resolve to guide pages, never to a compiled page.
	cites, bad := 0, 0
	for _, p := range corpus.Pages {
		r := tool("owner", "kb.read", map[string]any{"key": p.Key})
		if r.Status != http.StatusOK {
			t.Errorf("the Owner could not read %s: %d %s", p.Key, r.Status, r.Raw)
			continue
		}
		c, _ := r.Body["content"].(map[string]any)
		secs, _ := c["sections"].([]any)
		for _, s := range secs {
			if long, _ := c["outline"].([]any); len(long) > 0 {
				break
			}
			cs, _ := s.(map[string]any)["cites"].([]any)
			for _, ct := range cs {
				cites++
				m := ct.(map[string]any)
				if g, _ := m["guide"].(string); !guideKeys[g] || m["title"] == "" || m["anchor"] == "" {
					bad++
					t.Errorf("%s cites %v: not a guide page", p.Key, m)
				}
			}
		}
	}
	if cites == 0 || bad != 0 {
		t.Errorf("citations: %d checked, %d did not resolve to a guide page", cites, bad)
	}
	// The report, then the thresholds.
	report := map[string]any{"suite": suite.Suite, "version": suite.Version, "questions": len(suite.Cases), "top": suite.Top, "missed": missed, "citations_checked": cites}
	cats := make([]string, 0, len(byCategory))
	for c := range byCategory {
		cats = append(cats, c)
	}
	sort.Strings(cats)
	perCategory := map[string]any{}
	for _, c := range cats {
		y := byCategory[c]
		recall := float64(y.found) / float64(y.asked)
		perCategory[c] = map[string]any{"asked": y.asked, "recall_at_top": recall, "recovered_by_the_guide_text": y.recovered, "hidden_from_a_revoked_reader": y.hidden}
		t.Logf("%-22s asked %3d  recall@%d %.2f  recovered by the guide's text %d", c, y.asked, suite.Top, recall, y.recovered)
		if min, ok := suite.Thresholds[c]; !ok {
			t.Errorf("category %s has no threshold in the suite", c)
		} else if recall+1e-9 < min {
			plan := suite.Owners[c]
			t.Errorf("category %s: recall %.2f is below its threshold %.2f (owner %q, plan %q): fix it in the compile step, never with vectors (AI-58)",
				c, recall, min, plan["owner"], plan["plan"])
		}
		if c == "revoked-reader" && y.hidden != y.asked {
			t.Errorf("a revoked reader found %d of %d pages", y.asked-y.hidden, y.asked)
		}
	}
	report["categories"] = perCategory
	if out := os.Getenv("FYNIQ_EVALS_OUT"); out != "" {
		b, _ := json.MarshalIndent(report, "", "  ")
		if err := os.WriteFile(filepath.Join(out, "knowledge.json"), append(b, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_ = fmt.Sprint
}
