package corporate

import (
	"encoding/json"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/render"
)

// Each template is pinned in templates.lock.json by its version and its file's hash: a changed template is a new
// version, reviewed (design D6). The release has the nine kinds FR-CS-05 names.
// req: governance/company-secretary FR-CS-05 Documents drafted
func TestEveryTemplateIsPinned(t *testing.T) {
	raw, err := os.ReadFile("templates.lock.json")
	if err != nil {
		t.Fatal(err)
	}
	var lock map[string]struct {
		Version int    `json:"version"`
		SHA256  string `json:"sha256"`
	}
	if err := json.Unmarshal(raw, &lock); err != nil {
		t.Fatal(err)
	}
	want := []string{"agm.dispensation", "agm.minutes", "agm.notice", "directors.allot_shares", "directors.appoint_officer", "directors.approve_statements",
		"directors.change_office", "directors.remove_officer", "members.written_resolution"}
	if !slices.Equal(Kinds, want) {
		t.Errorf("the kinds: %v", Kinds)
	}
	for _, k := range Kinds {
		tpl, pin := Templates[k], lock[k]
		if pin.Version != tpl.Version || pin.SHA256 != tpl.SHA256 {
			t.Errorf("%s: version %d with hash %s, pinned as version %d with %s: a changed template is a new version, pinned again", k, tpl.Version, tpl.SHA256,
				pin.Version, pin.SHA256)
		}
	}
	if len(lock) != len(Kinds) {
		t.Errorf("the lock pins %d templates, the release has %d", len(lock), len(Kinds))
	}
}

// sampleRecords are a company with two directors, a secretary, a CEO and two members on 16 Nov 2026.
func sampleRecords() (Records, uuid.UUID, uuid.UUID) {
	secretary, director := uuid.New(), uuid.New()
	return Records{On: on("2026-11-16"), Name: "Cognizhi Pte. Ltd.", UEN: "202612345K", Office: "1 North Bridge Road, #08-08, Singapore 179094",
		Currency: "SGD", Directors: []string{"Tan Mei Lin", "Ah Kow"}, Secretary: "Rachel Teo", Members: []string{"Ah Kow", "Lion Rock Fund I"},
		Officers: map[uuid.UUID]Officer{secretary: {Role: RoleSecretary, Name: "Rachel Teo"}, director: {Role: RoleDirector, Name: "Tan Mei Lin"}}}, secretary, director
}

// sampleValues are fields that fit each template's: a day, a time, a text, lines, its first choice, a count, an amount
// or the secretary.
func sampleValues(tpl Template, officer uuid.UUID) Values {
	v := Values{}
	for _, f := range tpl.Fields {
		switch f.Type {
		case FieldDate:
			v[f.Name] = "2026-12-31"
		case FieldTime:
			v[f.Name] = "10:30"
		case FieldText:
			v[f.Name] = "Sample " + f.Name
		case FieldLines:
			v[f.Name] = []any{"First " + f.Name, "Second " + f.Name}
		case FieldChoice:
			v[f.Name] = slices.Sorted(maps.Keys(f.Choices))[0]
		case FieldCount:
			v[f.Name] = "1250000"
		case FieldAmount:
			v[f.Name] = "1.5"
		case FieldOfficer:
			v[f.Name] = officer.String()
		}
	}
	return v
}

// Every template fills from the records into a paper the render service lays out: the company at its head with its UEN
// and office, each field written in words, the lines as numbered items, an optional section left out when empty, and a
// line to sign for each director, member, the secretary or the chair, under a banner saying it is Fyniq's draft, never
// signed or filed by Fyniq.
// req: governance/company-secretary FR-CS-05 Documents drafted
// req: FR-CS-05
func TestEveryTemplateFills(t *testing.T) {
	r, secretary, _ := sampleRecords()
	for _, k := range Kinds {
		tpl := Templates[k]
		paper, probs := Fill(tpl, sampleValues(tpl, secretary), r)
		if len(probs) > 0 {
			t.Errorf("%s: %v", k, probs)
			continue
		}
		if err := paper.Check(); err != nil {
			t.Errorf("%s: %v", k, err)
		}
		raw, _ := json.Marshal(paper)
		text := string(raw)
		for _, want := range []string{"Cognizhi Pte. Ltd.", "UEN 202612345K", "Registered office: 1 North Bridge Road", "Fyniq does not sign or file it", k + "@1"} {
			if !strings.Contains(text, want) {
				t.Errorf("%s: the paper lacks %q: %s", k, want, text)
			}
		}
		for _, never := range []string{"<no value>", "{{", "2026-12-31"} {
			if strings.Contains(text, never) {
				t.Errorf("%s: the paper shows %q: %s", k, never, text)
			}
		}
		if len(paper.Signatures) == 0 {
			t.Errorf("%s: no one signs it", k)
		}
		for _, f := range tpl.Fields {
			if f.Type == FieldLines && !strings.Contains(text, "Second "+f.Name) {
				t.Errorf("%s: the lines of %s are not on the paper", k, f.Name)
			}
		}
	}

	notice := Templates["agm.notice"]
	v := sampleValues(notice, secretary)
	delete(v, "other_business")
	paper, probs := Fill(notice, v, r)
	if len(probs) > 0 || slices.ContainsFunc(paper.Sections, func(s render.PaperSection) bool { return s.Heading == "Other business" }) {
		t.Errorf("a notice with no other business: %v %+v", probs, paper.Sections)
	}
	if paper.Signatures[0].Name != "Rachel Teo" || paper.Signatures[0].Role != "Secretary" {
		t.Errorf("the notice is given by the secretary: %+v", paper.Signatures)
	}
	allot := Templates["directors.allot_shares"]
	paper, _ = Fill(allot, sampleValues(allot, secretary), r)
	raw, _ := json.Marshal(paper)
	if !strings.Contains(string(raw), "That 1,250,000 Sample share_class shares in the company be allotted and issued to Sample allottee at SGD 1.50 each") ||
		len(paper.Signatures) != 2 || paper.Signatures[1].Name != "Ah Kow" {
		t.Errorf("the allotment: %s", raw)
	}
	remove := Templates["directors.remove_officer"]
	paper, _ = Fill(remove, sampleValues(remove, secretary), r)
	if raw, _ := json.Marshal(paper); !strings.Contains(string(raw), "That Rachel Teo cease to be the company's secretary with effect from 31 December 2026") {
		t.Errorf("the removal: %s", raw)
	}
	members := Templates["members.written_resolution"]
	paper, _ = Fill(members, sampleValues(members, secretary), r)
	if len(paper.Signatures) != 2 || paper.Signatures[1].Name != "Lion Rock Fund I" || paper.Signatures[1].Role != "Member" {
		t.Errorf("the members sign their resolution: %+v", paper.Signatures)
	}
}

// A document that does not fit says every problem at once, each naming its field (ENG-22): a field missing, unknown to
// its kind, not a date, a time, a choice, a whole number or an amount, too many lines, an NRIC in full, an officer not in
// office in an office the field accepts, or no one in the records to sign it.
// req: FR-CS-05
func TestADocumentThatDoesNotFit(t *testing.T) {
	r, secretary, director := sampleRecords()
	minutes := Templates["agm.minutes"]
	v := sampleValues(minutes, secretary)
	delete(v, "chair")
	v["meeting_on"], v["time"], v["extra"] = "31/12/2026", "25:00", "x"
	v["present"] = []any{"Tan Mei Lin S1234567D"}
	v["resolutions"] = make([]any, MaxLines+1)
	for i := range v["resolutions"].([]any) {
		v["resolutions"].([]any)[i] = "A resolution"
	}
	_, probs := Fill(minutes, v, r)
	fields := map[string]bool{}
	for _, p := range probs {
		if p.Code != CodeDocument {
			t.Errorf("a problem's code: %+v", p)
		}
		fields[p.Field] = true
	}
	for _, want := range []string{"fields.chair", "fields.meeting_on", "fields.time", "fields.extra", "fields.present[0]", "fields.resolutions"} {
		if !fields[want] {
			t.Errorf("no problem names %s: %v", want, probs)
		}
	}

	allot := Templates["directors.allot_shares"]
	v = sampleValues(allot, secretary)
	v["shares"], v["price"] = "1,000", "1.23456"
	_, probs = Fill(allot, v, Records{On: r.On, Name: r.Name, Currency: "SGD"})
	fields = map[string]bool{}
	for _, p := range probs {
		fields[p.Field] = true
	}
	if !fields["fields.shares"] || !fields["fields.price"] || !fields["kind"] {
		t.Errorf("a count with commas, an amount with 5 decimals and no director to sign: %v", probs)
	}

	remove := Templates["directors.remove_officer"]
	for name, id := range map[string]string{"a director": director.String(), "no officer": uuid.NewString(), "not an ID": "x"} {
		v = sampleValues(remove, secretary)
		v["officer_id"] = id
		if _, probs := Fill(remove, v, r); len(probs) != 1 || probs[0].Field != "fields.officer_id" {
			t.Errorf("removing %s: %v", name, probs)
		}
	}
	appoint := Templates["directors.appoint_officer"]
	v = sampleValues(appoint, secretary)
	v["role"] = "chairman"
	if _, probs := Fill(appoint, v, r); len(probs) != 1 || probs[0].Field != "fields.role" || !strings.Contains(probs[0].Message, "auditor, ceo, director, secretary") {
		t.Errorf("an office that is not one: %v", probs)
	}
	notice := Templates["agm.notice"]
	if _, probs := Fill(notice, sampleValues(notice, secretary), Records{On: r.On, Name: r.Name, Directors: r.Directors}); len(probs) != 1 || probs[0].Field != "kind" {
		t.Errorf("a notice with no secretary to give it: %v", probs)
	}
}

// A corporate document's version keeps what it was drafted from: its kind, template, fields, paper, the day it was
// signed and the proposal it came from.
// req: FR-CS-05
func TestADocumentReadsAsItWasSaved(t *testing.T) {
	r, _, _ := sampleRecords()
	tpl := Templates["directors.approve_statements"]
	paper, _ := Fill(tpl, Values{"fy_end": "2026-12-31"}, r)
	proposal, signed := uuid.New(), on("2027-01-10")
	d := Document{Date: on("2027-01-05"), Kind: tpl.Kind, Template: tpl.ID(), Values: Values{"fy_end": "2026-12-31"}, Paper: paper, SignedOn: &signed,
		Proposal: &proposal, Note: "For the AGM"}
	b, err := EncodeDocument(d)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ReadDocument(storage.Document{Payload: b})
	if err != nil || got.Kind != d.Kind || got.Template != "directors.approve_statements@1" || got.Values["fy_end"] != "2026-12-31" || got.Paper.Title != paper.Title ||
		!got.SignedOn.Equal(signed) || *got.Proposal != proposal || got.Note != d.Note || !got.Date.Equal(d.Date) {
		t.Errorf("read back: %+v %v", got, err)
	}
}
