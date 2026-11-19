// Package letters is the rules of Nemura's letters (PRD-002 NM-LTR-01, NM-LTR-02, NM-LTR-03, NM-LTR-07): the catalogue of letter types and of merge fields, templates of
// text blocks with typed fields and no free code, filling a template from a person's facts, and the reference and hash an issued letter carries. It holds no database
// and no clock; the adapter keeps the records and the renderer port draws the bytes.
package letters

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Codes of the refusals of this package.
const (
	CodeInvalid  = "NM_LETTER_INVALID"
	CodeNotFound = "NM_NOT_FOUND"
	CodeConflict = "NM_LETTER_CONFLICT"
	CodeRole     = "NM_ROLE_REQUIRED"
	CodeStepUp   = "NM_STEP_UP_REQUIRED"
)

// Refusal is a plain refusal with a stable code.
type Refusal struct{ Code, Message string }

func (r *Refusal) Error() string { return r.Code + ": " + r.Message }

func invalid(format string, a ...any) *Refusal {
	return &Refusal{CodeInvalid, fmt.Sprintf(format, a...)}
}

// Field is a merge field of the catalogue: where its value comes from and how it is written.
type Field struct {
	Key    string // person.legal_name
	Label  string // what a template author sees
	Format string // text or date
	// Restricted fields (salary) are shown only by a type that says so and only when the person asked for it.
	Restricted bool
}

// Fields is the typed catalogue of merge fields. A template names these and nothing else: there is no free code.
var Fields = []Field{
	{Key: "person.legal_name", Label: "Legal name", Format: "text"},
	{Key: "person.preferred_name", Label: "Preferred name", Format: "text"},
	{Key: "job.title", Label: "Job title", Format: "text"},
	{Key: "job.department", Label: "Department", Format: "text"},
	{Key: "employment.type", Label: "Kind of employment", Format: "text"},
	{Key: "employment.start", Label: "Start date", Format: "date"},
	{Key: "employment.end", Label: "Last day of employment", Format: "date"},
	{Key: "employment.probation_end", Label: "Probation end", Format: "date"},
	{Key: "company.name", Label: "Company name", Format: "text"},
	{Key: "company.uen", Label: "Company UEN", Format: "text"},
	{Key: "company.address", Label: "Company address", Format: "text"},
	{Key: "letter.date", Label: "Date of the letter", Format: "date"},
	{Key: "letter.reference", Label: "Reference of the letter", Format: "text"},
	{Key: "request.purpose", Label: "What the letter is for", Format: "text"},
}

// FieldOf is the catalogue's field for a key.
func FieldOf(key string) (Field, bool) {
	for _, f := range Fields {
		if f.Key == key {
			return f, true
		}
	}
	return Field{}, false
}

// Type is a kind of letter (NM-LTR-01): what it may show, who signs and how fast it is due.
type Type struct {
	Key, Title string
	FieldKeys  []string // the fields a template of this type may use; no others
	DueDays    int      // working days to issue, by default
	Salary     bool     // the type shows salary when the person asks for it: not built until the pay structure exists
	AutoIssue  bool     // the type may be issued automatically under a signatory's delegation (NM-LTR-04)
	Needs      string   // what must exist for the type to be offered: empty, or "salary", "terms"
	Statement  string   // a plain sentence of what the letter says, for the type's picker
}

var commonFields = []string{"person.legal_name", "job.title", "employment.start", "company.name", "company.uen", "company.address", "letter.date", "letter.reference", "request.purpose"}

// Types is the catalogue of letter types of NM-LTR-01. The ones that show salary or need the person's terms wait for the pay structure and the KETs (Needs).
var Types = []Type{
	{Key: "employment_confirmation", Title: "Employment confirmation", FieldKeys: commonFields, DueDays: 3, AutoIssue: true, Statement: "Confirms that the person works for the company, in what role and since when, for a bank, a landlord or a visa."},
	{Key: "certificate_of_employment", Title: "Certificate of employment", FieldKeys: append(slices.Clone(commonFields), "employment.end", "job.department", "employment.type"), DueDays: 5, Statement: "Certifies the period and the role of a person who worked for the company."},
	{Key: "service_letter", Title: "Service or experience letter", FieldKeys: append(slices.Clone(commonFields), "employment.end", "job.department", "employment.type"), DueDays: 5, Statement: "States the facts of a person's service: dates, role and department, with no evaluation."},
	{Key: "release_letter", Title: "Release letter", FieldKeys: append(slices.Clone(commonFields), "employment.end"), DueDays: 5, Statement: "Releases a person from their obligations to the company on their last day."},
	{Key: "probation_confirmation", Title: "Probation confirmation", FieldKeys: append(slices.Clone(commonFields), "employment.probation_end"), DueDays: 5, Statement: "Confirms that a person has completed their probation."},
	{Key: "offer_letter", Title: "Offer letter", FieldKeys: append(slices.Clone(commonFields), "job.department", "employment.type"), DueDays: 5, Statement: "Offers a person a role, a start date and a kind of employment."},
	{Key: "salary_certificate", Title: "Salary certificate", FieldKeys: append(slices.Clone(commonFields), "pay.basic_salary"), DueDays: 3, Salary: true, Needs: "salary", Statement: "States the person's salary, only when they ask for it."},
	{Key: "pay_change_letter", Title: "Promotion or pay-change letter", FieldKeys: append(slices.Clone(commonFields), "pay.basic_salary"), DueDays: 5, Salary: true, Needs: "salary", Statement: "Tells a person about a promotion or a change of pay."},
	{Key: "kets", Title: "Key employment terms", FieldKeys: commonFields, DueDays: 14, Needs: "terms", Statement: "The key employment terms the Employment Act asks for, within 14 days of the start."},
}

// TypeOf is the catalogue's type for a key.
func TypeOf(key string) (Type, bool) {
	for _, t := range Types {
		if t.Key == key {
			return t, true
		}
	}
	return Type{}, false
}

// Block kinds of a template.
const (
	Letterhead = "letterhead"
	Heading    = "heading"
	Paragraph  = "paragraph"
	Signature  = "signature"
)

// Block is one block of a template: a kind and its text, which may hold {{field}} placeholders.
type Block struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

var placeholder = regexp.MustCompile(`\{\{\s*([a-z_]+\.[a-z_]+)\s*\}\}`)

// maxBlocks and maxText bound a template.
const (
	MaxBlocks = 40
	MaxText   = 2000
)

// Problem is one thing wrong with a template, with where.
type Problem struct {
	Block   int
	Message string
}

// Check validates a template for a type: the kinds, the lengths, and that every placeholder is a field the type may use (never a salary field on a type that does not
// show it, never an unknown key). It returns every problem at once.
func Check(t Type, blocks []Block) []Problem {
	var out []Problem
	if len(blocks) == 0 || len(blocks) > MaxBlocks {
		return []Problem{{0, fmt.Sprintf("A template has between 1 and %d blocks: add a block, or remove some.", MaxBlocks)}}
	}
	hasSignature := false
	for i, b := range blocks {
		switch b.Kind {
		case Letterhead, Heading, Paragraph, Signature:
		default:
			out = append(out, Problem{i + 1, fmt.Sprintf("Block %d is a %q, which is not a block: use a letterhead, heading, paragraph or signature.", i+1, b.Kind)})
		}
		hasSignature = hasSignature || b.Kind == Signature
		if utf8.RuneCountInString(b.Text) > MaxText {
			out = append(out, Problem{i + 1, fmt.Sprintf("Block %d is longer than %d characters: shorten it or split it.", i+1, MaxText)})
		}
		if strings.Contains(b.Text, "{{") {
			rest := placeholder.ReplaceAllString(b.Text, "")
			if strings.Contains(rest, "{{") || strings.Contains(rest, "}}") {
				out = append(out, Problem{i + 1, fmt.Sprintf("Block %d has a field that is not written as {{group.name}}: correct it.", i+1)})
			}
		}
		for _, m := range placeholder.FindAllStringSubmatch(b.Text, -1) {
			key := m[1]
			if !slices.Contains(t.FieldKeys, key) {
				if _, known := FieldOf(key); known || strings.HasPrefix(key, "pay.") {
					out = append(out, Problem{i + 1, fmt.Sprintf("Block %d uses {{%s}}, which a %s letter may not show: remove it.", i+1, key, strings.ToLower(t.Title))})
				} else {
					out = append(out, Problem{i + 1, fmt.Sprintf("Block %d uses {{%s}}, which is not a merge field: choose one from the catalogue.", i+1, key)})
				}
			}
		}
	}
	if !hasSignature {
		out = append(out, Problem{0, "A template ends with a signature block: add one."})
	}
	return out
}

// FieldsUsed lists the distinct fields a template uses, in the order they first appear.
func FieldsUsed(blocks []Block) []string {
	var out []string
	for _, b := range blocks {
		for _, m := range placeholder.FindAllStringSubmatch(b.Text, -1) {
			if !slices.Contains(out, m[1]) {
				out = append(out, m[1])
			}
		}
	}
	return out
}

// Values are the facts a letter is filled from, by field key, already written as text; a date is held as 2006-01-02 and written by the format.
type Values map[string]string

// Filled is a template with its fields replaced, ready for the renderer.
type Filled struct{ Blocks []Block }

// Fill replaces each field of a template with its value, written by the field's format. A field with no value is refused and named, so a letter never goes out with a
// blank where a fact belongs.
func Fill(blocks []Block, v Values) (Filled, error) {
	var missing []string
	out := make([]Block, len(blocks))
	for i, b := range blocks {
		text := placeholder.ReplaceAllStringFunc(b.Text, func(m string) string {
			key := placeholder.FindStringSubmatch(m)[1]
			val, ok := v[key]
			if !ok || strings.TrimSpace(val) == "" {
				if !slices.Contains(missing, key) {
					missing = append(missing, key)
				}
				return m
			}
			f, _ := FieldOf(key)
			return write(f, val)
		})
		out[i] = Block{Kind: b.Kind, Text: text}
	}
	if len(missing) > 0 {
		var names []string
		for _, k := range missing {
			f, _ := FieldOf(k)
			names = append(names, f.Label)
		}
		return Filled{}, invalid("The letter needs a value for %s, which the record does not hold for this person: record it first.", strings.Join(names, ", "))
	}
	return Filled{Blocks: out}, nil
}

func write(f Field, v string) string {
	if f.Format == "date" {
		if t, err := time.Parse(time.DateOnly, v); err == nil {
			return fmt.Sprintf("%d %s %d", t.Day(), t.Month(), t.Year())
		}
	}
	return v
}

// Reference is the reference a letter carries: the year, the type's short code and a number within the organisation and year, so a letter can be told apart and found.
func Reference(year int, typeKey string, n int) string {
	short := map[string]string{"employment_confirmation": "EC", "certificate_of_employment": "CE", "service_letter": "SL", "release_letter": "RL", "probation_confirmation": "PC",
		"offer_letter": "OL", "salary_certificate": "SC", "pay_change_letter": "PA", "kets": "KE"}[typeKey]
	if short == "" {
		short = "LT"
	}
	return fmt.Sprintf("%s-%d-%04d", short, year, n)
}
