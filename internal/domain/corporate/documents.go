package corporate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"
	"text/template"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/equity"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/render"
)

// Corporate documents (FR-CS-05; p1-company-secretary task 4.1, design D6): a directors' resolution, a members' written
// resolution, a notice of the annual general meeting, its minutes, or the notice that the statements were sent without
// one, drafted from one of Fyniq's templates (templates/<kind>.json) and filled from the records on the day it is
// drafted. A draft is a paper to review: Fyniq never signs or files it. A person downloads it as PDF and has it signed
// outside Fyniq. The templates are Fyniq's, versioned here and pinned in templates/lock.json, and carry "counsel to
// confirm" until a lawyer reads them (rev 0.283). The Act's notice period (s177), the minute book's month (s188) and the
// written resolutions' majorities (s184A–G) are not checked until sg-acra@2026.2 holds them.

// CodeDocument is a corporate document that does not fit: its kind, its fields, or the records it needs.
const CodeDocument = "CORP-05"

//go:embed templates/*.json
var templateFiles embed.FS

// The signers a template's paper has a line for.
const (
	SignersDirectors = "directors" // each director in office on the day
	SignersMembers   = "members"   // each member holding shares on the day
	SignersSecretary = "secretary" // the secretary in office, by order of the board
	SignersChair     = "chair"     // whoever chaired the meeting, its chair field
)

// The types of a template's field, and how each is written on the paper.
const (
	FieldDate    = "date"    // a day, 2026-12-31, written 31 December 2026
	FieldTime    = "time"    // a time of day, 24-hour, 14:30
	FieldText    = "text"    // a line of text
	FieldLines   = "lines"   // lines, the numbered items of a section that names it
	FieldChoice  = "choice"  // one of its choices, written as the choice's words
	FieldCount   = "count"   // a whole number above zero, written with commas
	FieldAmount  = "amount"  // a decimal, written in the base currency
	FieldOfficer = "officer" // an officer in office, by their first version's ID: their name, and their office as <name>_role
)

// The bounds of a document's fields.
const (
	MaxFieldText = 500
	MaxLine      = 1000
	MaxLines     = 20
)

var (
	timeFormat   = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)
	countFormat  = regexp.MustCompile(`^[1-9][0-9]{0,14}$`)
	amountFormat = regexp.MustCompile(`^[0-9]{1,12}(\.[0-9]{1,4})?$`)
)

// TemplateField is a field a template asks for.
type TemplateField struct {
	Name     string            `json:"name"`
	Type     string            `json:"type"`
	Label    string            `json:"label"`
	Optional bool              `json:"optional,omitempty"`
	Choices  map[string]string `json:"choices,omitempty"` // a choice's values and their words
	Roles    []string          `json:"roles,omitempty"`   // the offices an officer field accepts
}

// TemplateSection is a part of a template's paper: a heading, paragraphs and items, each a text/template over the
// company and the fields, then the lines of a lines field as more items. A section left with nothing is left out.
type TemplateSection struct {
	Heading    string   `json:"heading,omitempty"`
	Paragraphs []string `json:"paragraphs,omitempty"`
	Items      []string `json:"items,omitempty"`
	ItemsFrom  string   `json:"items_from,omitempty"`
}

// Template is one of Fyniq's corporate templates: a kind of document, its version, its title and summary, who signs it,
// the fields it asks for and its sections.
type Template struct {
	Kind     string            `json:"kind"`
	Version  int               `json:"version"`
	Title    string            `json:"title"`
	Summary  string            `json:"summary"`
	Signers  string            `json:"signers"`
	Fields   []TemplateField   `json:"fields"`
	Sections []TemplateSection `json:"sections"`
	SHA256   string            `json:"-"` // of its file, as templates/lock.json pins it
}

// ID is the kind and its version, as a draft names its template: directors.approve_statements@1.
func (t Template) ID() string { return fmt.Sprintf("%s@%d", t.Kind, t.Version) }

// Field is the template's field named name.
func (t Template) Field(name string) (TemplateField, bool) {
	i := slices.IndexFunc(t.Fields, func(f TemplateField) bool { return f.Name == name })
	if i < 0 {
		return TemplateField{}, false
	}
	return t.Fields[i], true
}

// Templates are the release's templates by kind, and Kinds their kinds in order.
var (
	Templates map[string]Template
	Kinds     []string
)

func init() {
	var err error
	if Templates, err = LoadTemplates(templateFiles); err != nil {
		panic(err)
	}
	Kinds = slices.Sorted(maps.Keys(Templates))
}

// LoadTemplates reads every templates/<kind>.json in fsys and checks each: its kind is its file's name, a version, a
// title and a summary, known signers, fields of known types each named once (a choice with its choices, an officer's
// offices known), every section with something in it, items only from a lines field, and every text a template that
// parses.
func LoadTemplates(fsys fs.FS) (map[string]Template, error) {
	paths, err := fs.Glob(fsys, "templates/*.json")
	if err != nil {
		return nil, err
	}
	out := map[string]Template{}
	for _, p := range paths {
		raw, err := fs.ReadFile(fsys, p)
		if err != nil {
			return nil, err
		}
		var t Template
		d := json.NewDecoder(bytes.NewReader(raw))
		d.DisallowUnknownFields()
		if err := d.Decode(&t); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		t.SHA256 = sha256Hex(raw)
		if err := t.check(strings.TrimSuffix(path.Base(p), ".json")); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		out[t.Kind] = t
	}
	return out, nil
}

func (t Template) check(name string) error {
	switch {
	case t.Kind != name:
		return fmt.Errorf("its kind %q is not its file's name", t.Kind)
	case t.Version < 1:
		return fmt.Errorf("version %d: a template's version starts at 1", t.Version)
	case strings.TrimSpace(t.Title) == "" || strings.TrimSpace(t.Summary) == "":
		return fmt.Errorf("it has no title or summary")
	case !slices.Contains([]string{SignersDirectors, SignersMembers, SignersSecretary, SignersChair}, t.Signers):
		return fmt.Errorf("signers %q is not directors, members, secretary or chair", t.Signers)
	case len(t.Sections) == 0:
		return fmt.Errorf("it has no section")
	}
	seen := map[string]bool{}
	for _, f := range t.Fields {
		switch {
		case f.Name == "" || seen[f.Name]:
			return fmt.Errorf("field %q: every field has a name of its own", f.Name)
		case !slices.Contains([]string{FieldDate, FieldTime, FieldText, FieldLines, FieldChoice, FieldCount, FieldAmount, FieldOfficer}, f.Type):
			return fmt.Errorf("field %s: %q is not a field type", f.Name, f.Type)
		case strings.TrimSpace(f.Label) == "":
			return fmt.Errorf("field %s has no label", f.Name)
		case (f.Type == FieldChoice) != (len(f.Choices) > 0):
			return fmt.Errorf("field %s: a choice, and only a choice, has choices", f.Name)
		case (f.Type == FieldOfficer) != (len(f.Roles) > 0):
			return fmt.Errorf("field %s: an officer, and only an officer, names the offices it accepts", f.Name)
		}
		for _, r := range f.Roles {
			if !slices.Contains(Roles, r) {
				return fmt.Errorf("field %s: %q is not an office", f.Name, r)
			}
		}
		seen[f.Name] = true
	}
	if t.Signers == SignersChair {
		if f, ok := t.Field("chair"); !ok || f.Type != FieldText || f.Optional {
			return fmt.Errorf("a paper the chair signs asks for the chair, a text")
		}
	}
	for i, s := range t.Sections {
		if len(s.Paragraphs) == 0 && len(s.Items) == 0 && s.ItemsFrom == "" {
			return fmt.Errorf("section %d has nothing in it", i+1)
		}
		if s.ItemsFrom != "" {
			if f, ok := t.Field(s.ItemsFrom); !ok || f.Type != FieldLines {
				return fmt.Errorf("section %d takes its items from %q, not a lines field", i+1, s.ItemsFrom)
			}
		}
		for _, text := range slices.Concat([]string{s.Heading}, s.Paragraphs, s.Items) {
			if _, err := parse(text); err != nil {
				return fmt.Errorf("section %d: %w", i+1, err)
			}
		}
	}
	return nil
}

func parse(text string) (*template.Template, error) {
	return template.New("").Option("missingkey=error").Parse(text)
}

// Values are a document's fields as it was asked for: a text for each field, or lines for a lines field.
type Values map[string]any

// text is the field's text, and whether it is one.
func (v Values) text(name string) (string, bool) {
	s, ok := v[name].(string)
	return strings.TrimSpace(s), ok
}

// lines are the field's lines, and whether they are lines.
func (v Values) lines(name string) ([]string, bool) {
	switch x := v[name].(type) {
	case []string:
		return x, true
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			s, ok := e.(string)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	}
	return nil, false
}

// Records are what a template is filled from on a day: the company's name and registered office as the profile has
// them in force (else as the organisation's settings do), its UEN and base currency, the directors and the secretary in
// office, the members holding shares, and every officer in office by their first version.
type Records struct {
	On        time.Time
	Name, UEN string
	Office    string
	Currency  string
	Directors []string
	Secretary string
	Members   []string
	Officers  map[uuid.UUID]Officer
}

// RecordsOn reads the records a template is filled from on day.
func RecordsOn(ctx context.Context, u storage.UnitOfWork, day time.Time) (Records, error) {
	r := Records{On: day, Officers: map[uuid.UUID]Officer{}}
	o, err := u.Organisation(ctx)
	if err != nil {
		return r, err
	}
	r.Name, r.UEN, r.Office, r.Currency = o.Profile.LegalName, o.Profile.UEN, o.Profile.RegisteredAddress, o.BaseCurrency
	if r.Name == "" {
		r.Name = o.Name
	}
	details, err := Details(ctx, u)
	if err != nil {
		return r, err
	}
	if d := DetailOn(details, day); d != nil {
		r.Name, r.Office = d.Value.Name, d.Value.Office
	}
	officers, err := Officers(ctx, u)
	if err != nil {
		return r, err
	}
	for _, x := range OfficersOn(officers, day) {
		r.Officers[Root(x.Doc, x.Value)] = x.Value
		switch x.Value.Role {
		case RoleDirector:
			r.Directors = append(r.Directors, x.Value.Name)
		case RoleSecretary:
			if r.Secretary == "" {
				r.Secretary = x.Value.Name
			}
		}
	}
	reader, err := equity.ReaderIn(ctx, u)
	if err != nil {
		return r, err
	}
	table, err := reader.At(day)
	if err != nil {
		return r, err
	}
	for _, h := range table.Holders {
		if h.Issued.Sign() <= 0 {
			continue
		}
		c, err := u.Contacts().Get(ctx, h.Holder)
		if err != nil {
			return r, err
		}
		r.Members = append(r.Members, c.LegalName)
	}
	slices.Sort(r.Members)
	return r, nil
}

// roleWords are an office in words, as a paper writes it.
var roleWords = map[string]string{RoleDirector: "director", RoleSecretary: "secretary", RoleCEO: "chief executive officer", RoleAuditor: "auditor"}

// Fill fills template t with values from records r into the paper to render, or says every problem at once (ENG-22):
// a field missing, of the wrong form, unknown to the template or showing an NRIC or FIN in full; an officer not in office
// in an office it accepts; or no one in the records to sign it.
func Fill(t Template, v Values, r Records) (render.Paper, ledger.Problems) {
	p := &problems{code: CodeDocument}
	written := map[string]string{}
	lines := map[string][]string{}
	for _, name := range slices.Sorted(maps.Keys(v)) {
		if _, ok := t.Field(name); !ok {
			p.add("fields."+name, fmt.Sprintf("%s is not a field of %s", name, t.Kind), "Leave it out, or choose the kind of document that asks for it")
		}
	}
	for _, f := range t.Fields {
		field := "fields." + f.Name
		if f.Type == FieldLines {
			xs, ok := v.lines(f.Name)
			switch {
			case v[f.Name] != nil && !ok:
				p.add(field, f.Label+" are not lines of text", "Send a list of lines")
			case len(xs) == 0 && !f.Optional:
				p.add(field, f.Label+" are missing", "Enter at least one line")
			case len(xs) > MaxLines:
				p.add(field, fmt.Sprintf("%s are more than %d lines", f.Label, MaxLines), "Keep the paper to the lines it needs")
			}
			for i, x := range xs {
				x = strings.TrimSpace(x)
				switch {
				case x == "":
					p.add(fmt.Sprintf("%s[%d]", field, i), "the line is empty", "Remove it, or write it")
				case utf8.RuneCountInString(x) > MaxLine:
					p.add(fmt.Sprintf("%s[%d]", field, i), fmt.Sprintf("the line is longer than %d characters", MaxLine), "Shorten it")
				case fullNRIC.MatchString(x):
					p.add(fmt.Sprintf("%s[%d]", field, i), "it shows an NRIC or FIN in full", "Leave identifiers out of the paper")
				}
				lines[f.Name] = append(lines[f.Name], x)
			}
			continue
		}
		s, ok := v.text(f.Name)
		switch {
		case v[f.Name] != nil && !ok:
			p.add(field, f.Label+" is not a text", "Send it as a text")
			continue
		case s == "" && f.Optional:
			written[f.Name] = ""
			continue
		case s == "":
			p.add(field, f.Label+" is missing", "Enter it")
			continue
		case utf8.RuneCountInString(s) > MaxFieldText:
			p.add(field, fmt.Sprintf("%s is longer than %d characters", f.Label, MaxFieldText), "Shorten it")
			continue
		case fullNRIC.MatchString(s):
			p.add(field, "it shows an NRIC or FIN in full", "Leave identifiers out of the paper")
			continue
		}
		switch f.Type {
		case FieldDate:
			d, err := time.Parse(time.DateOnly, s)
			if err != nil {
				p.add(field, f.Label+" is not a date", "Enter it as 2026-12-31")
				continue
			}
			written[f.Name] = d.Format("2 January 2006")
		case FieldTime:
			if !timeFormat.MatchString(s) {
				p.add(field, f.Label+" is not a time of day", "Enter it as 14:30")
				continue
			}
			written[f.Name] = s
		case FieldText:
			written[f.Name] = s
		case FieldChoice:
			words, ok := f.Choices[s]
			if !ok {
				p.add(field, fmt.Sprintf("%q is not one of %s", s, strings.Join(slices.Sorted(maps.Keys(f.Choices)), ", ")), "Choose one of them")
				continue
			}
			written[f.Name] = words
		case FieldCount:
			if !countFormat.MatchString(s) {
				p.add(field, f.Label+" is not a whole number above zero", "Enter it as 10000, with no commas")
				continue
			}
			written[f.Name] = grouped(s)
		case FieldAmount:
			if !amountFormat.MatchString(s) {
				p.add(field, f.Label+" is not an amount", "Enter it as 1.00, with at most 4 decimals and no commas")
				continue
			}
			written[f.Name] = r.Currency + " " + amountWords(s)
		case FieldOfficer:
			id, err := uuid.Parse(s)
			o, ok := r.Officers[id]
			if err != nil || !ok || !slices.Contains(f.Roles, o.Role) {
				offices := make([]string, len(f.Roles))
				for i, x := range f.Roles {
					offices[i] = roleWords[x]
				}
				p.add(field, fmt.Sprintf("%s is not an officer in office on %s as %s", f.Label, r.On.Format("2 January 2006"), strings.Join(offices, " or ")),
					"Choose one from the corporate profile's officers")
				continue
			}
			written[f.Name], written[f.Name+"_role"] = o.Name, roleWords[o.Role]
		}
	}
	var signers []render.PaperSignature
	sign := func(names []string, as string) {
		for _, n := range names {
			signers = append(signers, render.PaperSignature{Name: n, Role: as})
		}
	}
	switch t.Signers {
	case SignersDirectors:
		if len(r.Directors) == 0 {
			p.add("kind", "no director is in office on "+r.On.Format("2 January 2006")+", so no one can sign it", "Record the directors in the corporate profile")
		}
		sign(r.Directors, "Director")
	case SignersMembers:
		if len(r.Members) == 0 {
			p.add("kind", "no member holds shares on "+r.On.Format("2 January 2006")+", so no one can sign it", "Record the shares issued in the equity register")
		}
		sign(r.Members, "Member")
	case SignersSecretary:
		if r.Secretary == "" {
			p.add("kind", "no secretary is in office on "+r.On.Format("2 January 2006")+" to give it by order of the board",
				"Record the company secretary among the officers")
		}
		sign([]string{r.Secretary}, "Secretary")
	case SignersChair:
		sign([]string{written["chair"]}, "Chair")
	}
	if len(p.out) > 0 {
		return render.Paper{}, p.out
	}

	data := map[string]any{"Company": map[string]string{"Name": r.Name, "UEN": r.UEN, "Office": r.Office}, "F": written}
	out := render.Paper{Title: t.Title, Author: r.Name, Footer: t.ID(),
		Banner: "Draft by Fyniq from the company's records on " + r.On.Format("2 January 2006") + ", for the directors and the company secretary to " +
			"review before anyone signs it. Fyniq does not sign or file it. Fyniq's template, which a lawyer has yet to confirm."}
	if r.UEN != "" {
		out.Lines = append(out.Lines, "UEN "+r.UEN)
	}
	if r.Office != "" {
		out.Lines = append(out.Lines, "Registered office: "+r.Office)
	}
	execute := func(text string) string {
		var b strings.Builder
		if err := template.Must(parse(text)).Execute(&b, data); err != nil {
			panic(fmt.Sprintf("corporate: template %s: %v", t.ID(), err)) // a template's own mistake: TestEveryTemplateFills finds it
		}
		return strings.Join(strings.Fields(b.String()), " ")
	}
	for _, s := range t.Sections {
		x := render.PaperSection{Heading: execute(s.Heading)}
		for _, para := range s.Paragraphs {
			x.Paragraphs = append(x.Paragraphs, execute(para))
		}
		for _, item := range s.Items {
			x.Items = append(x.Items, execute(item))
		}
		x.Items = append(x.Items, lines[s.ItemsFrom]...)
		if len(x.Paragraphs) > 0 || len(x.Items) > 0 {
			out.Sections = append(out.Sections, x)
		}
	}
	out.Signatures = signers
	return out, nil
}

// grouped writes a whole number with commas: 1,000,000.
func grouped(digits string) string {
	var b strings.Builder
	for i, c := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String()
}

// amountWords writes a decimal with commas and at least 2 decimals: 1250 is 1,250.00 and 0.0001 stays 0.0001.
func amountWords(s string) string {
	whole, frac, _ := strings.Cut(s, ".")
	if whole = strings.TrimLeft(whole, "0"); whole == "" {
		whole = "0"
	}
	for len(frac) < 2 {
		frac += "0"
	}
	return grouped(whole) + "." + frac
}

func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// Document is a corporate document drafted from a template: the day it was drafted, the template's kind and version,
// the fields as asked, the paper as filled from that day's records, the day it was signed once someone records it
// (task 4.2), the sec.draft_document proposal it came from if it did, and a note.
type Document struct {
	Date     time.Time
	Kind     string
	Template string
	Values   Values
	Paper    render.Paper
	SignedOn *time.Time
	Proposal *uuid.UUID
	Note     string
}

type documentPayload struct {
	Date     string       `json:"date"`
	Kind     string       `json:"kind"`
	Template string       `json:"template"`
	Fields   Values       `json:"fields"`
	Paper    render.Paper `json:"paper"`
	SignedOn string       `json:"signed_on,omitempty"`
	Proposal string       `json:"proposal_id,omitempty"`
	Note     string       `json:"note,omitempty"`
}

// EncodeDocument is a corporate document as a document version keeps it.
func EncodeDocument(d Document) ([]byte, error) {
	fields := d.Values
	if fields == nil {
		fields = Values{}
	}
	return json.Marshal(documentPayload{Date: day(d.Date), Kind: d.Kind, Template: d.Template, Fields: fields, Paper: d.Paper, SignedOn: optional(d.SignedOn),
		Proposal: idText(d.Proposal), Note: d.Note})
}

// ReadDocument reads a corporate document's version as it was saved.
func ReadDocument(doc storage.Document) (Document, error) {
	var p documentPayload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return Document{}, err
	}
	out := Document{Kind: p.Kind, Template: p.Template, Values: p.Fields, Paper: p.Paper, Note: p.Note}
	var err error
	if out.Date, err = time.Parse(time.DateOnly, p.Date); err != nil {
		return out, err
	}
	if out.SignedOn, err = readDay(p.SignedOn); err != nil {
		return out, err
	}
	out.Proposal, err = readID(p.Proposal)
	return out, err
}

// ErrNoTemplate is a kind of document no template drafts.
var ErrNoTemplate = errors.New("corporate: no template drafts that kind of document")

// drafted fills kind's template with v from the records on today, as a document to save, or says what is wrong.
func drafted(ctx context.Context, u storage.UnitOfWork, kind string, v Values, note string, today time.Time) (Document, error) {
	t, ok := Templates[kind]
	if !ok {
		return Document{}, ledger.Problems{{Code: CodeDocument, Field: "kind", Message: fmt.Sprintf("%q is not a kind of document Fyniq drafts", kind),
			Fix: "Choose one of " + strings.Join(Kinds, ", ")}}
	}
	r, err := RecordsOn(ctx, u, today)
	if err != nil {
		return Document{}, err
	}
	paper, probs := Fill(t, v, r)
	p := &problems{code: CodeDocument, out: probs}
	p.note(note)
	if fullNRIC.MatchString(note) {
		p.add("note", "it shows an NRIC or FIN in full", "Keep the last 4 characters only")
	}
	if len(p.out) > 0 {
		return Document{}, p.out
	}
	return Document{Date: today, Kind: kind, Template: t.ID(), Values: v, Paper: paper, Note: note}, nil
}

// DraftDocumentIn drafts a corporate document of kind in u from its template, filled from the records on today with
// the fields v (DraftCorporateDocument): a draft to review, download and have signed, never signed or filed by Fyniq.
// proposal is the sec.draft_document proposal it comes from, if it does.
func DraftDocumentIn(ctx context.Context, u storage.UnitOfWork, kind string, v Values, note string, proposal *uuid.UUID, today time.Time) (storage.Document, error) {
	d, err := drafted(ctx, u, kind, v, note, today)
	if err != nil {
		return storage.Document{}, err
	}
	d.Proposal = proposal
	b, err := EncodeDocument(d)
	if err != nil {
		return storage.Document{}, err
	}
	return u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentCorporateDocument, Payload: b})
}

// ReviseDocumentIn saves a new version of a corporate document not recorded yet, in u: its fields filled again from the
// records on today; its kind and the proposal it came from stay.
func ReviseDocumentIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, v Values, note string, today time.Time) (storage.Document, error) {
	doc, err := equity.Drafted(ctx, u, storage.DocumentCorporateDocument, id)
	if err != nil {
		return doc, err
	}
	was, err := ReadDocument(doc)
	if err != nil {
		return doc, err
	}
	d, err := drafted(ctx, u, was.Kind, v, note, today)
	if err != nil {
		return doc, err
	}
	d.Proposal, d.SignedOn = was.Proposal, was.SignedOn
	b, err := EncodeDocument(d)
	if err != nil {
		return doc, err
	}
	return u.Documents().Revise(ctx, id, b)
}

// Documents are every corporate document, drafts and recorded, the latest drafted first.
func Documents(ctx context.Context, u storage.UnitOfWork) ([]equity.Recorded[Document], error) {
	docs, err := all(ctx, u, storage.DocumentCorporateDocument)
	if err != nil {
		return nil, err
	}
	var out []equity.Recorded[Document]
	for _, d := range docs {
		v, err := ReadDocument(d)
		if err != nil {
			return nil, err
		}
		out = append(out, recorded(d, v))
	}
	slices.SortStableFunc(out, func(a, b equity.Recorded[Document]) int {
		if c := b.Value.Date.Compare(a.Value.Date); c != 0 {
			return c
		}
		return strings.Compare(b.Doc.ID.String(), a.Doc.ID.String())
	})
	return out, nil
}
