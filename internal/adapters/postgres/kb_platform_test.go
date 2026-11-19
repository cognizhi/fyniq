//go:build local

package postgres_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"slices"
	"testing"
	"testing/fstest"

	"github.com/google/uuid"

	userguide "github.com/cognizhi/fyniq/docs/user-guide"
	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/domain/knowledge"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/kb"
)

// kbRelease is a small knowledge base to read through the reader filter: a page for everyone, a page whose guide page is
// for Owners, a page compiled from both, a payroll page, and a page from a guide page still a draft.
func kbRelease(t *testing.T) knowledge.Release {
	t.Helper()
	guides := map[string]string{
		"open.md":       "---\nkey: g.open\ntitle: Open\nroles: [all]\n---\n\n# Open\n\n## Terms\n\nPay within 30 days. Ask about the director loan.\n",
		"owners.md":     "---\nkey: g.owners\ntitle: Owners only\nroles: [owner]\n---\n\n# Owners only\n\n## Terms\n\nThe secret handshake is a plain hand.\n",
		"draft.md":      "---\nkey: g.draft\ntitle: A draft\nstatus: draft\n---\n\n# A draft\n\n## Terms\n\nSubmit within 14 days.\n",
		"accountant.md": "---\nkey: g.acc\ntitle: For accountants\nroles: [accountant]\n---\n\n# For accountants\n\n## Terms\n\nAccountants post journals.\n",
	}
	page := func(key, class, status string, aliases []string, srcs ...string) string {
		out := "---\nkey: " + key + "\ntype: task\ntitle: Page " + key + "\nsummary: About " + key + ".\n"
		if aliases != nil {
			out += "aliases:\n"
			for _, a := range aliases {
				out += "  - " + a + "\n"
			}
		}
		out += "sources:\n"
		body := "# Page " + key + "\n\n## Terms\n\n"
		for _, s := range srcs {
			sum := sha256.Sum256([]byte(guides[s]))
			out += "  - docs/user-guide/" + s + " sha256:" + hex.EncodeToString(sum[:]) + "\n"
			body += "- A fact. (src: docs/user-guide/" + s + "#terms)\n"
		}
		return out + "data_class: " + class + "\ntaint: trusted\nstatus: " + status + "\nhuman_edited: false\ncompiled_by: test\nschema: kb-schema@1\ncompiled_at: 2026-11-18\n---\n" + body +
			"- See [[p.open]], [[p.owner]], [[p.both]], [[p.payroll]], [[p.draft]], [[p.acc]].\n"
	}
	pages := map[string]string{
		"p.open":    page("p.open", "general", "published", []string{"director loan"}, "open.md"),
		"p.owner":   page("p.owner", "general", "published", nil, "owners.md"),
		"p.both":    page("p.both", "general", "published", nil, "open.md", "owners.md"),
		"p.payroll": page("p.payroll", "payroll", "published", nil, "open.md"),
		"p.draft":   page("p.draft", "general", "draft", nil, "draft.md"),
		"p.acc":     page("p.acc", "general", "published", nil, "accountant.md"),
	}
	files := func(index string) (fstest.MapFS, fstest.MapFS) {
		k, g := fstest.MapFS{}, fstest.MapFS{}
		for key, text := range pages {
			k["pages/task/"+key+".md"] = &fstest.MapFile{Data: []byte(text)}
		}
		for name, text := range guides {
			g[name] = &fstest.MapFile{Data: []byte(text)}
		}
		k["index.md"] = &fstest.MapFile{Data: []byte(index)}
		return k, g
	}
	k, g := files("")
	c, err := knowledge.Load(k, g)
	if err != nil {
		t.Fatal(err)
	}
	k, g = files(knowledge.RenderIndex(c.Pages))
	if c, err = knowledge.Load(k, g); err != nil {
		t.Fatal(err)
	}
	rel, err := knowledge.NewRelease("v-test", c)
	if err != nil {
		t.Fatal(err)
	}
	return rel
}

func loadKB(t *testing.T, rel knowledge.Release) {
	t.Helper()
	if err := postgres.LoadPlatformKB(context.Background(), db.URL("fyniq_migrator"), rel, io.Discard); err != nil {
		t.Fatal(err)
	}
}

func readKB[T any](t *testing.T, role string, fn func(storage.KnowledgeBase) (T, error)) T {
	t.Helper()
	var out T
	if err := store(t, role).Do(context.Background(), uuid.New(), func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		out, err = fn(u.KnowledgeBase())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

func keysOf(es []storage.KBEntry) []string {
	var out []string
	for _, e := range es {
		out = append(out, e.Key)
	}
	slices.Sort(out)
	return out
}

var allClasses = []string{"general", "personal", "payroll", "equity", "identity"}

// The reader filter is the query's (migration 000560): a page is open to a reader only when every guide page it was compiled
// from is open to one of their roles and its data class is one they may read; an Owner reads every audience; what a reader
// may not open is not in their index, their search, their page reads or their citations at all.
func TestTheReaderFilterIsTheQuerys(t *testing.T) {
	ctx := context.Background()
	loadKB(t, kbRelease(t))
	index := func(r storage.KBReader) []string {
		return keysOf(readKB(t, "fyniq_api", func(k storage.KnowledgeBase) ([]storage.KBEntry, error) { return k.PlatformIndex(ctx, r) }))
	}
	for _, c := range []struct {
		name string
		r    storage.KBReader
		want []string
	}{
		{"an owner with every class reads everything", storage.KBReader{Roles: []string{"owner"}, Classes: allClasses},
			[]string{"p.acc", "p.both", "p.draft", "p.open", "p.owner", "p.payroll"}},
		{"an owner without payroll misses the payroll page", storage.KBReader{Roles: []string{"owner"}, Classes: []string{"general"}},
			[]string{"p.acc", "p.both", "p.draft", "p.open", "p.owner"}},
		{"an employee reads only what is for all", storage.KBReader{Roles: []string{"employee"}, Classes: []string{"general"}},
			[]string{"p.draft", "p.open"}},
		{"an accountant reads theirs and what is for all", storage.KBReader{Roles: []string{"accountant"}, Classes: []string{"general"}},
			[]string{"p.acc", "p.draft", "p.open"}},
		{"a reader with no role reads what is for all", storage.KBReader{Classes: []string{"general"}}, []string{"p.draft", "p.open"}},
		{"a reader with no class reads nothing", storage.KBReader{Roles: []string{"owner"}}, nil},
	} {
		if got := index(c.r); !slices.Equal(got, c.want) {
			t.Errorf("%s: index %v; want %v", c.name, got, c.want)
		}
	}
	employee := storage.KBReader{Roles: []string{"employee"}, Classes: []string{"general"}}
	// The page that is not theirs is not there: no body, no sources, no guide references.
	for _, key := range []string{"p.owner", "p.both", "p.payroll"} {
		var p storage.KBPlatformPage
		var ok bool
		if err := store(t, "fyniq_api").Do(ctx, uuid.New(), func(ctx context.Context, u storage.UnitOfWork) (err error) {
			p, ok, err = u.KnowledgeBase().PlatformPage(ctx, key, employee)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if ok || p.Body != "" {
			t.Errorf("an employee read %s", key)
		}
		refs := readKB(t, "fyniq_api", func(k storage.KnowledgeBase) ([]storage.KBGuideRef, error) {
			return k.PlatformGuides(ctx, key, employee)
		})
		if len(refs) != 0 {
			t.Errorf("an employee got the guide references of %s: %v", key, refs)
		}
	}
	// Search never shows a page, a summary, a snippet or a raw source the reader may not open.
	hits := readKB(t, "fyniq_api", func(k storage.KnowledgeBase) ([]storage.KBHit, error) {
		return k.PlatformSearch(ctx, storage.KBSearch{Terms: []string{"secret", "handshake"}, Scope: "both", Limit: 10, Reader: employee})
	})
	if len(hits) != 0 {
		t.Errorf("an employee found the owners' page by searching its words: %v", hits)
	}
	hits = readKB(t, "fyniq_api", func(k storage.KnowledgeBase) ([]storage.KBHit, error) {
		return k.PlatformSearch(ctx, storage.KBSearch{Terms: []string{"secret", "handshake"}, Scope: "both", Limit: 10,
			Reader: storage.KBReader{Roles: []string{"owner"}, Classes: allClasses}})
	})
	if len(hits) == 0 {
		t.Error("an owner did not find the owners' page")
	}
}

// Search is PostgreSQL full-text search over the pages, their aliases and the raw guide, ranked: the users' own words find
// a page through its aliases, a typo through trigram similarity, a raw source when only it has the words, and the scope
// narrows what is searched; a draft is marked.
func TestSearchIsFullTextOverPagesAliasesAndSources(t *testing.T) {
	ctx := context.Background()
	loadKB(t, kbRelease(t))
	owner := storage.KBReader{Roles: []string{"owner"}, Classes: allClasses}
	search := func(scope string, terms ...string) []storage.KBHit {
		return readKB(t, "fyniq_api", func(k storage.KnowledgeBase) ([]storage.KBHit, error) {
			return k.PlatformSearch(ctx, storage.KBSearch{Terms: terms, Scope: scope, Limit: 10, Reader: owner})
		})
	}
	first := func(hs []storage.KBHit) string {
		if len(hs) == 0 {
			return ""
		}
		return hs[0].Kind + ":" + hs[0].Key
	}
	if h := search("pages", "director", "loan"); first(h) != "page:p.open" {
		t.Errorf("by an alias: %v", h)
	}
	if h := search("pages", "Page", "p.draft"); first(h) == "" || !slices.ContainsFunc(h, func(x storage.KBHit) bool { return x.Key == "p.draft" && x.Draft }) {
		t.Errorf("a draft page is found and marked: %v", h)
	}
	if h := search("pages", "directr", "lon"); first(h) == "" {
		t.Errorf("a typo finds nothing: %v", h)
	}
	if h := search("sources", "handshake"); first(h) != "source:g.owners" {
		t.Errorf("the raw guide: %v", h)
	}
	if h := search("pages", "handshake"); len(h) != 0 {
		t.Errorf("the pages do not hold the raw guide's words, the scope narrows: %v", h)
	}
	for _, h := range search("both", "within", "days") {
		if h.Snippet == "" || h.Key == "" {
			t.Errorf("a hit with no key or snippet: %+v", h)
		}
	}
	if h := search("pages", "the", "of"); len(h) > 10 {
		t.Errorf("more hits than the limit: %d", len(h))
	}
	err := store(t, "fyniq_api").Do(ctx, uuid.New(), func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.KnowledgeBase().PlatformSearch(ctx, storage.KBSearch{Terms: []string{"x"}, Scope: "everything", Limit: 5, Reader: owner})
		return err
	})
	wantState(t, "a scope that is not one", err, "FY560")
}

// The migrator loads the release's own knowledge base, as the release ships it, whole and once: loading it again changes
// nothing, a later release replaces it, and every page the index lists opens.
func TestTheMigratorLoadsTheShippedKnowledgeBase(t *testing.T) {
	ctx := context.Background()
	c, err := knowledge.Load(kb.FS, userguide.FS)
	if err != nil {
		t.Fatal(err)
	}
	rel, err := knowledge.NewRelease("v-shipped", c)
	if err != nil {
		t.Fatal(err)
	}
	loadKB(t, rel)
	loadKB(t, rel) // the same release again is a no-op
	info := readKB(t, "fyniq_api", func(k storage.KnowledgeBase) (storage.KBPlatformInfo, error) {
		i, ok, err := k.PlatformInfo(ctx)
		if !ok && err == nil {
			err = fmt.Errorf("no release is loaded")
		}
		return i, err
	})
	if info.Version != "v-shipped" || info.Digest != rel.Digest || info.Schema != knowledge.Schema {
		t.Errorf("loaded %+v; the release is %s %s", info, rel.Version, rel.Digest)
	}
	owner := storage.KBReader{Roles: []string{"owner"}, Classes: allClasses}
	entries := readKB(t, "fyniq_api", func(k storage.KnowledgeBase) ([]storage.KBEntry, error) { return k.PlatformIndex(ctx, owner) })
	if len(entries) != len(rel.Pages) {
		t.Fatalf("the Owner's index lists %d pages; the release ships %d", len(entries), len(rel.Pages))
	}
	// The generated index's order: by type, then key (kb/index.md).
	want := knowledge.Entries(rel.Pages)
	for i, e := range entries {
		if e.Key != want[i].Key {
			t.Fatalf("the index's order differs at %d: %s, not %s", i, e.Key, want[i].Key)
		}
	}
	// Every page opens, with the guide pages it cites; its version is the release and its file's digest.
	for _, p := range rel.Pages {
		got := readKB(t, "fyniq_api", func(k storage.KnowledgeBase) (storage.KBPlatformPage, error) {
			pg, ok, err := k.PlatformPage(ctx, p.Key, owner)
			if !ok && err == nil {
				err = fmt.Errorf("%s does not open", p.Key)
			}
			return pg, err
		})
		if got.Release != "v-shipped" || got.Digest != p.Digest || got.Body != p.Body {
			t.Errorf("%s: loaded release %s digest %s", p.Key, got.Release, got.Digest)
		}
	}
	// People's own questions find their page near the top, through the terms a question is searched by.
	for q, key := range map[string]string{
		"How do I approve a team member's expense claim?": "claims.approve-team-claims",
		"What does the company owe a founder?":            "founders.balance",
		"how do I change the theme to dark mode":          "preferences.appearance",
	} {
		hits := readKB(t, "fyniq_api", func(k storage.KnowledgeBase) ([]storage.KBHit, error) {
			return k.PlatformSearch(ctx, storage.KBSearch{Terms: knowledge.QueryTerms(q), Scope: "pages", Limit: 5, Reader: owner})
		})
		if !slices.ContainsFunc(hits, func(h storage.KBHit) bool { return h.Key == key }) {
			var got []string
			for _, h := range hits {
				got = append(got, h.Key)
			}
			t.Errorf("%q: %s is not in the top 5: %v", q, key, got)
		}
	}
	// A later release replaces the whole set.
	next := rel
	next.Version, next.Pages = "v-next", rel.Pages[:3]
	next.Digest = rel.Digest[:63] + "0"
	loadKB(t, next)
	if got := readKB(t, "fyniq_api", func(k storage.KnowledgeBase) ([]storage.KBEntry, error) { return k.PlatformIndex(ctx, owner) }); len(got) != 3 {
		t.Errorf("after the next release the index lists %d pages; want its 3", len(got))
	}
}

// req: data/data-model Global tables
// req: QA-ENG-05
// asvs: V8.2.2
// asvs: V13.2.2
// No application role reads or writes the platform knowledge base's tables, or runs its reads but the API: the reader
// filter is the only way in, and the AI's own role cannot reach it at all.
func TestAttackServiceRolesReachThePlatformKnowledgeBaseOnlyThroughTheFilter(t *testing.T) {
	ctx := context.Background()
	loadKB(t, kbRelease(t))
	for _, role := range serviceRoles {
		c := conn(t, role)
		for name, stmt := range map[string]string{
			"read the pages":        `SELECT count(*) FROM fyniq.kb_platform_page`,
			"read the sources":      `SELECT count(*) FROM fyniq.kb_platform_source`,
			"read the audiences":    `SELECT count(*) FROM fyniq.kb_platform_page_source`,
			"read the release":      `SELECT count(*) FROM fyniq.kb_platform_release`,
			"insert a page":         `INSERT INTO fyniq.kb_platform_page (key, type, title, summary, body, sources, data_class, taint, status, compiled_by, compiled_at, digest, search) VALUES ('x.y', 'task', 'x', 'x', 'x', '[{}]', 'general', 'trusted', 'published', 'x', '2026-01-01', repeat('a', 64), ''::tsvector)`,
			"change a page's class": `UPDATE fyniq.kb_platform_page SET data_class = 'general'`,
			"widen an audience":     `UPDATE fyniq.kb_platform_page_source SET roles = ARRAY['all']`,
			"delete the pages":      `DELETE FROM fyniq.kb_platform_page`,
			"truncate":              `TRUNCATE fyniq.kb_platform_source`,
		} {
			_, err := c.Exec(ctx, stmt)
			wantState(t, role+": "+name, err, "42501")
		}
		_, err := c.Exec(ctx, `SELECT * FROM fyniq.kb_platform_index(ARRAY['owner'], ARRAY['general'])`)
		if role == "fyniq_api" {
			if err != nil {
				t.Errorf("fyniq_api cannot read the index: %v", err)
			}
			continue
		}
		wantState(t, role+" runs the reader filter", err, "42501")
	}
}
