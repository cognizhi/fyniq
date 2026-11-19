package knowledge

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// GuidePrefix is where the raw sources live in the repository; a page cites a source by that path.
const GuidePrefix = "docs/user-guide/"

// Guide is one raw source, a page of the user guide: the text the knowledge base is compiled from, and the audience its
// front matter names (FR-UX-32).
type Guide struct {
	Path   string // docs/user-guide/<file>
	Key    string // the guide page's own key, such as equity.founders-money; empty for a page with none
	Title  string
	Roles  []string // the roles it is for, as the guide names them; ["all"] for every role
	Status string   // published or draft
	Body   string   // front matter aside
	Full   string   // the file as written, which a page's hash and citations are checked against
	SHA256 string   // of the whole file, as a page's sources line records it
}

// Corpus is a release's knowledge: its pages and the guide they are compiled from.
type Corpus struct {
	Pages []Page
	Guide []Guide
	Index string // kb/index.md as shipped
	// PackVersions are the statutory pack versions that exist, by pack, in order (LoadPacks); nil until loaded, when the lint
	// leaves the packs a page declares unchecked.
	PackVersions map[string][]PackVersion
}

// PackVersion is a statutory pack's version, "<year>.<n>".
type PackVersion struct{ Year, N int }

func (v PackVersion) String() string { return fmt.Sprintf("%d.%d", v.Year, v.N) }
func (v PackVersion) less(w PackVersion) bool {
	return v.Year < w.Year || v.Year == w.Year && v.N < w.N
}

var packVersionDir = regexp.MustCompile(`^(\d{4})\.(\d+)$`)

// LoadPacks reads which versions of each statutory pack exist, from the packs directory (packs/<pack>/<year>.<n>/). A
// directory that is not there means no pack exists.
func (c *Corpus) LoadPacks(packs fs.FS) error {
	c.PackVersions = map[string][]PackVersion{}
	entries, err := fs.ReadDir(packs, ".")
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read the packs: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		versions, err := fs.ReadDir(packs, e.Name())
		if err != nil {
			return fmt.Errorf("read the pack %s: %w", e.Name(), err)
		}
		for _, v := range versions {
			if m := packVersionDir.FindStringSubmatch(v.Name()); m != nil && v.IsDir() {
				y, _ := strconv.Atoi(m[1])
				n, _ := strconv.Atoi(m[2])
				c.PackVersions[e.Name()] = append(c.PackVersions[e.Name()], PackVersion{y, n})
			}
		}
		sort.Slice(c.PackVersions[e.Name()], func(i, j int) bool { return c.PackVersions[e.Name()][i].less(c.PackVersions[e.Name()][j]) })
	}
	return nil
}

// roleNames are the roles as the guide pages name them, where a page's name differs from the role's key
// (apps/web/src/help/guide.ts keeps the same list; a test reads every page through both).
var roleNames = map[string]string{"admin": "administrator", "ar": "ar_specialist", "ap": "ap_specialist"}

// ParseGuide reads one guide page. Its path is relative to the guide's directory, so a page of Nemura's guide is
// "nemura/leave-apply.md".
func ParseGuide(rel, text string) Guide {
	g := Guide{Path: GuidePrefix + rel, Roles: []string{"all"}, Status: "published", Body: text, Full: text}
	sum := sha256.Sum256([]byte(text))
	g.SHA256 = hex.EncodeToString(sum[:])
	fm, body, ok, err := parseFrontMatter(text)
	if !ok || err != nil {
		return g
	}
	g.Body = body
	g.Key = fm.scalars["key"]
	g.Title = strings.Trim(fm.scalars["title"], `"'`)
	if g.Title == "" {
		g.Title = g.Key
	}
	if s := fm.scalars["status"]; s != "" {
		g.Status = s
	}
	if r, ok := fm.scalars["roles"]; ok {
		var roles []string
		for _, role := range strings.Split(strings.Trim(r, "[]"), ",") {
			if role = strings.TrimSpace(role); role != "" {
				if n, ok := roleNames[role]; ok {
					role = n
				}
				roles = append(roles, role)
			}
		}
		if len(roles) > 0 {
			g.Roles = roles
		}
	}
	return g
}

// Load reads a release's knowledge. kb is the kb/ directory (pages/ and index.md) and guide the docs/user-guide/
// directory, including Nemura's pages in nemura/; the README is not a source.
func Load(kb, guide fs.FS) (Corpus, error) {
	var c Corpus
	err := fs.WalkDir(kb, "pages", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if path.Ext(p) != ".md" {
			c.Pages = append(c.Pages, Page{File: "kb/" + p, err: []string{"only Markdown files belong in kb/pages"}})
			return nil
		}
		b, err := fs.ReadFile(kb, p)
		if err != nil {
			return err
		}
		c.Pages = append(c.Pages, ParsePage("kb/"+p, string(b)))
		return nil
	})
	if err != nil {
		return c, fmt.Errorf("read the pages: %w", err)
	}
	err = fs.WalkDir(guide, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if path.Ext(p) != ".md" || p == "README.md" {
			return nil
		}
		b, err := fs.ReadFile(guide, p)
		if err != nil {
			return err
		}
		c.Guide = append(c.Guide, ParseGuide(p, string(b)))
		return nil
	})
	if err != nil {
		return c, fmt.Errorf("read the guide: %w", err)
	}
	if b, err := fs.ReadFile(kb, "index.md"); err == nil {
		c.Index = string(b)
	}
	sort.Slice(c.Pages, func(i, j int) bool { return c.Pages[i].File < c.Pages[j].File })
	sort.Slice(c.Guide, func(i, j int) bool { return c.Guide[i].Path < c.Guide[j].Path })
	return c, nil
}

// GuideAt is the guide page at a source path.
func (c Corpus) GuideAt(p string) (Guide, bool) {
	for _, g := range c.Guide {
		if g.Path == p {
			return g, true
		}
	}
	return Guide{}, false
}
