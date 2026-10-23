// Package pack reads, validates and selects the statutory parameter packs (PRD-001 §10.6): every
// statutory value is data in an effective-dated, versioned pack, packs/<pack>/<year>.<n>/, with its
// official sources, its verifier, its review date and its hand-pinned tests (ENG-30, FR-ORG-05). A
// version that lacks any of them, or still marks a value [verify], is refused, so an unconfirmed rate
// cannot reach a posting. Selection (At), the rate and the codes in force, and the tax point are pure
// functions: no clock, no I/O (ENG-01, ENG-31, ENG-45).
package pack

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/url"
	"path"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// The codes of a pack version's rules.
const (
	CodeID       = "PACK-01" // the id is not <pack>@<year>.<n>, or does not match its directory
	CodeSource   = "PACK-02" // a source without an https URL, section, kind or retrieval date; a row citing no declared source
	CodeVerifier = "PACK-03" // no verifier
	CodeReview   = "PACK-04" // no review date
	CodeVerify   = "PACK-05" // a value still marked [verify]
	CodeDates    = "PACK-06" // effective dates missing, out of order, overlapping or with a gap
	CodeTable    = "PACK-07" // a table or pinned file missing or malformed, or a pack Fyniq cannot read
	CodeValue    = "PACK-08" // a value that is not what its column holds (a rate that is not an exact decimal fraction, …)
	CodePinned   = "PACK-09" // a pinned statutory test fails
	CodeApproval = "PACK-10" // activation without a second approver through the approval engine (SOD-10)
	CodeNone     = "PACK-11" // no version of the pack in force on the date
	CodeRule     = "PACK-12" // a tax-point rule Fyniq does not know
	CodeActive   = "PACK-13" // the organisation has activated that version already
)

// Kinds are the kinds of source ENG-30 names.
var Kinds = []string{"law", "regulation", "official guidance", "contract term", "product policy"}

// Problem is one rule that failed.
type Problem struct {
	Code, Field, Message, Fix string
}

// Problems are every rule that failed.
type Problems []Problem

func (p Problems) Error() string {
	parts := make([]string, len(p))
	for i, x := range p {
		parts[i] = fmt.Sprintf("%s %s: %s (%s)", x.Code, x.Field, x.Message, x.Fix)
	}
	return strings.Join(parts, "; ")
}

// Source is an official page a value comes from (ENG-30).
type Source struct {
	ID        string `toml:"id"`
	Title     string `toml:"title"`
	URL       string `toml:"url"`
	Section   string `toml:"section"`
	Kind      string `toml:"kind"`
	Retrieved string `toml:"retrieved"`
	Quote     string `toml:"quote"`
}

// Table is a CSV table of a version: a data table, or the pinned cases of one.
type Table struct {
	Name, File, Description string
	Columns                 []string
	Rows                    [][]string
}

// Col returns the index of column name, or -1.
func (t Table) Col(name string) int { return slices.Index(t.Columns, name) }

// Version is one version of a pack, read and validated.
type Version struct {
	ID, Pack, Title string
	Year, N         int
	EffectiveFrom   time.Time
	EffectiveTo     *time.Time // the last day in force; nil while no later version ends it
	Supersedes      string
	ReviewDate      time.Time
	Verifier        string
	Conditions      string
	TaxPointRule    string
	Sources         []Source
	Tables          map[string]Table
	Pinned          map[string]Table
	Digest          string // SHA-256 of the version's files, so a loaded version is never changed
}

// Schema is what Fyniq reads from one pack: its tables and their pinned cases, with their columns in order.
type Schema struct {
	Tables, Pinned map[string][]string
	// Since is the first version, as {year, n}, that holds a table and its pinned cases, for a table Fyniq began to read
	// after the pack's first version: an earlier version, written before it, holds neither.
	Since map[string][2]int
}

// Schemas are the tables Fyniq reads from each pack; a pack or table not listed here is refused, as no code could
// read it.
var Schemas = map[string]Schema{
	"sg-gst": {
		Tables: map[string][]string{
			"rates":  {"effective_from", "effective_to", "rate", "source"},
			"codes":  {"code", "direction", "standard_rated", "value_box", "gst_box", "description", "source"},
			"limits": {"limit", "effective_from", "effective_to", "amount", "currency", "source"},
		},
		Pinned: map[string][]string{
			"rates":  {"case", "date", "want_rate", "why", "source"},
			"codes":  {"case", "code", "want_direction", "want_value_box", "want_gst_box", "why", "source"},
			"limits": {"case", "limit", "date", "want", "why", "source"},
		},
		Since: map[string][2]int{"limits": {2026, 2}}, // the simplified-invoice limit (FR-AP-20; rev 0.89)
	},
}

// holds reports whether version v of a pack holds the table name of s.
func (s Schema) holds(v Version, name string) bool {
	since, ok := s.Since[name]
	return !ok || v.Year > since[0] || (v.Year == since[0] && v.N >= since[1])
}

type fileRef struct {
	Name        string `toml:"name"`
	File        string `toml:"file"`
	Description string `toml:"description"`
}

type manifest struct {
	ID            string    `toml:"id"`
	Title         string    `toml:"title"`
	EffectiveFrom string    `toml:"effective_from"`
	EffectiveTo   string    `toml:"effective_to"`
	Supersedes    string    `toml:"supersedes"`
	ReviewDate    string    `toml:"review_date"`
	Verifier      string    `toml:"verifier"`
	Conditions    string    `toml:"conditions"`
	TaxPointRule  string    `toml:"tax_point_rule"`
	Sources       []Source  `toml:"sources"`
	Tables        []fileRef `toml:"tables"`
	Pinned        []fileRef `toml:"pinned"`
}

var (
	idPattern  = regexp.MustCompile(`^([a-z][a-z0-9-]{1,30})@(20[0-9]{2})\.([1-9][0-9]{0,2})$`)
	dirPattern = regexp.MustCompile(`^(20[0-9]{2})\.([1-9][0-9]{0,2})$`)
	verifyTag  = regexp.MustCompile(`(?i)\[\s*verify`)
)

// ParseDate reads a date as YYYY-MM-DD, at midnight UTC.
func ParseDate(s string) (time.Time, error) { return time.ParseInLocation(time.DateOnly, s, time.UTC) }

// LoadAll reads every version under fsys: one directory per pack, one per version inside it
// (packs/<pack>/<year>.<n>/). It returns the versions sorted by id, or every problem found.
func LoadAll(fsys fs.FS) ([]Version, error) {
	packs, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	var out []Version
	var problems Problems
	for _, p := range packs {
		if !p.IsDir() {
			continue
		}
		versions, err := fs.ReadDir(fsys, p.Name())
		if err != nil {
			return nil, err
		}
		for _, v := range versions {
			if !v.IsDir() {
				continue
			}
			got, err := Load(fsys, path.Join(p.Name(), v.Name()))
			var ps Problems
			switch {
			case err == nil:
				out = append(out, got)
			case asProblems(err, &ps):
				problems = append(problems, ps...)
			default:
				return nil, err
			}
		}
	}
	if len(problems) > 0 {
		return nil, problems
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func asProblems(err error, out *Problems) bool {
	p, ok := err.(Problems)
	if ok {
		*out = p
	}
	return ok
}

// Load reads and validates the version in dir (<pack>/<year>.<n>). A version is refused, with every
// problem found, when its id does not match its directory, when it lacks a source URL, a verifier or a
// review date, when any value is still marked [verify], or when a table breaks its pack's rules (ENG-30).
func Load(fsys fs.FS, dir string) (Version, error) {
	var out Problems
	add := func(code, field, message, fix string) {
		out = append(out, Problem{code, dir + ": " + field, message, fix})
	}
	files := map[string][]byte{}
	read := func(name string) ([]byte, bool) {
		b, err := fs.ReadFile(fsys, path.Join(dir, name))
		if err != nil {
			add(CodeTable, name, "is missing", "Add the file the pack.toml names, or remove its entry")
			return nil, false
		}
		files[name] = b
		if verifyTag.Match(b) {
			add(CodeVerify, name, "still marks a value [verify]",
				"Confirm the value from its official source and remove the tag; until then the version cannot be loaded or activated")
		}
		return b, true
	}
	raw, ok := read("pack.toml")
	if !ok {
		return Version{}, Problems(out)
	}
	var m manifest
	md, err := toml.Decode(string(raw), &m)
	if err != nil {
		add(CodeTable, "pack.toml", fmt.Sprintf("does not parse: %v", err), "Write valid TOML")
		return Version{}, Problems(out)
	}
	for _, k := range md.Undecoded() {
		add(CodeTable, "pack.toml", fmt.Sprintf("has the unknown key %q", k.String()), "Remove it or correct its spelling")
	}

	v := Version{ID: m.ID, Title: strings.TrimSpace(m.Title), Supersedes: m.Supersedes, Verifier: strings.TrimSpace(m.Verifier),
		Conditions: strings.TrimSpace(m.Conditions), TaxPointRule: m.TaxPointRule, Sources: m.Sources,
		Tables: map[string]Table{}, Pinned: map[string]Table{}}
	parts := idPattern.FindStringSubmatch(m.ID)
	dirParts := strings.Split(dir, "/")
	if parts == nil || len(dirParts) != 2 || !dirPattern.MatchString(dirParts[1]) ||
		parts[1] != dirParts[0] || parts[2]+"."+parts[3] != dirParts[1] {
		add(CodeID, "id", fmt.Sprintf("%q does not match its directory %s", m.ID, dir),
			"Name the version <pack>@<year>.<n> and keep it in packs/<pack>/<year>.<n>/")
	} else {
		v.Pack = parts[1]
		v.Year, _ = strconv.Atoi(parts[2])
		v.N, _ = strconv.Atoi(parts[3])
	}
	if v.Title == "" {
		add(CodeTable, "title", "is blank", "Say what the version holds")
	}
	if v.Conditions == "" {
		add(CodeSource, "conditions", "are blank", "State the conditions under which the rules apply (ENG-30)")
	}
	if d, err := ParseDate(m.EffectiveFrom); err != nil {
		add(CodeDates, "effective_from", fmt.Sprintf("%q is not a date (YYYY-MM-DD)", m.EffectiveFrom), "Enter the first day the version is in force")
	} else {
		v.EffectiveFrom = d
	}
	if m.EffectiveTo != "" {
		d, err := ParseDate(m.EffectiveTo)
		if err != nil || d.Before(v.EffectiveFrom) {
			add(CodeDates, "effective_to", fmt.Sprintf("%q is not a date on or after effective_from", m.EffectiveTo),
				"Enter the last day the version is in force, or leave it out")
		} else {
			v.EffectiveTo = &d
		}
	}
	if d, err := ParseDate(m.ReviewDate); err != nil {
		add(CodeReview, "review_date", fmt.Sprintf("%q is not a date (YYYY-MM-DD)", m.ReviewDate),
			"Enter the date by which the sources must be checked again (C.2 #16)")
	} else {
		v.ReviewDate = d
	}
	if v.Verifier == "" {
		add(CodeVerifier, "verifier", "is blank", "Name the person who checked every value against its official source (ENG-30)")
	}
	if m.Supersedes != "" && !idPattern.MatchString(m.Supersedes) {
		add(CodeID, "supersedes", fmt.Sprintf("%q is not a pack version", m.Supersedes), "Name the version as <pack>@<year>.<n>")
	}
	if v.Pack == "sg-gst" && !slices.Contains(Rules, v.TaxPointRule) {
		add(CodeRule, "tax_point_rule", fmt.Sprintf("%q is not a rule Fyniq knows", v.TaxPointRule),
			"Name one of "+strings.Join(Rules, ", ")+", or add the rule to internal/domain/pack with its tests")
	}

	sources := map[string]bool{}
	if len(m.Sources) == 0 {
		add(CodeSource, "sources", "are missing", "Cite the official page of every value (ENG-30)")
	}
	for i, s := range m.Sources {
		field := fmt.Sprintf("sources[%d]", i)
		if s.ID == "" || sources[s.ID] {
			add(CodeSource, field+".id", fmt.Sprintf("%q is blank or repeated", s.ID), "Give every source its own id")
		}
		sources[s.ID] = true
		if u, err := url.Parse(s.URL); err != nil || u.Scheme != "https" || u.Host == "" {
			add(CodeSource, field+".url", fmt.Sprintf("%q is not an https URL", s.URL), "Enter the official page's address")
		}
		if strings.TrimSpace(s.Section) == "" {
			add(CodeSource, field+".section", "is blank", "Name the section of the page the values come from (ENG-30)")
		}
		if !slices.Contains(Kinds, s.Kind) {
			add(CodeSource, field+".kind", fmt.Sprintf("%q is not a kind of source", s.Kind), "Use one of: "+strings.Join(Kinds, ", "))
		}
		if _, err := ParseDate(s.Retrieved); err != nil {
			add(CodeSource, field+".retrieved", fmt.Sprintf("%q is not a date", s.Retrieved), "Enter the day the page was read (YYYY-MM-DD)")
		}
	}

	schema, known := Schemas[v.Pack]
	if v.Pack != "" && !known {
		add(CodeTable, "id", fmt.Sprintf("Fyniq has no reader for the pack %s", v.Pack), "Add its tables to pack.Schemas with the code that reads them")
	}
	readTables := func(refs []fileRef, want map[string][]string, into map[string]Table, kind string) {
		for _, ref := range refs {
			cols, ok := want[ref.Name]
			if !ok || !schema.holds(v, ref.Name) {
				add(CodeTable, kind+"."+ref.Name, "is not a table of this version of the pack", "Remove it, or add it to pack.Schemas")
				continue
			}
			if _, dup := into[ref.Name]; dup {
				add(CodeTable, kind+"."+ref.Name, "is listed twice", "List each table once")
				continue
			}
			b, ok := read(ref.File)
			if !ok {
				continue
			}
			t, err := readCSV(b)
			if err != nil {
				add(CodeTable, ref.File, fmt.Sprintf("does not parse: %v", err), "Write it as CSV with a header row")
				continue
			}
			t.Name, t.File, t.Description = ref.Name, ref.File, ref.Description
			if !slices.Equal(t.Columns, cols) {
				add(CodeTable, ref.File, fmt.Sprintf("has the columns %v", t.Columns), fmt.Sprintf("Use the columns %v, in that order", cols))
				continue
			}
			src := t.Col("source")
			for i, row := range t.Rows {
				ids := strings.Fields(row[src])
				if len(ids) == 0 {
					add(CodeSource, fmt.Sprintf("%s row %d", ref.File, i+2), "cites no source", "Name the id of the source the value comes from")
				}
				for _, id := range ids {
					if !sources[id] {
						add(CodeSource, fmt.Sprintf("%s row %d", ref.File, i+2), fmt.Sprintf("cites %q, which pack.toml does not declare", id),
							"Declare the source in pack.toml, or correct the id")
					}
				}
			}
			into[ref.Name] = t
		}
		for name := range want {
			if _, ok := into[name]; !ok && schema.holds(v, name) {
				add(CodeTable, kind+"."+name, "is missing", "Add the table: every pack version holds all of its tables and their pinned tests")
			}
		}
	}
	if known {
		readTables(m.Tables, schema.Tables, v.Tables, "tables")
		readTables(m.Pinned, schema.Pinned, v.Pinned, "pinned")
	}
	if v.Pack == "sg-gst" {
		out = append(out, checkGST(dir, v)...)
	}
	if len(out) > 0 {
		return Version{}, out
	}
	v.Digest = digest(files)
	return v, nil
}

func readCSV(b []byte) (Table, error) {
	r := csv.NewReader(bytes.NewReader(b))
	r.ReuseRecord = false
	all, err := r.ReadAll()
	if err != nil {
		return Table{}, err
	}
	if len(all) == 0 {
		return Table{}, fmt.Errorf("no header row")
	}
	return Table{Columns: all[0], Rows: all[1:]}, nil
}

// digest hashes the files by name, so the same files always give the same digest.
func digest(files map[string][]byte) string {
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, n := range names {
		fmt.Fprintf(h, "%s\x00%d\x00", n, len(files[n]))
		h.Write(files[n])
	}
	return hex.EncodeToString(h.Sum(nil))
}
