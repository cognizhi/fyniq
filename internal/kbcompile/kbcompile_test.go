package kbcompile_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/kbcompile"
)

// fake is a compiler's client with a scripted model. It records everything the job asks for: there is nothing else it could ask.
type fake struct {
	items    []kbcompile.Item
	drafts   map[int][]kbcompile.Draft // the model's attempts, by call number
	calls    int
	feedback [][]string
	submit   []string
	findings []kbcompile.Finding
	failCall bool
}

func (f *fake) Work(context.Context, uuid.UUID) ([]kbcompile.Item, error) { return f.items, nil }
func (f *fake) Compile(_ context.Context, _ uuid.UUID, note string, _ int, fb []string) (kbcompile.Draft, error) {
	f.calls++
	f.feedback = append(f.feedback, fb)
	if f.failCall {
		return kbcompile.Draft{}, errors.New("the model did not answer")
	}
	ds := f.drafts[f.calls]
	if len(ds) == 0 {
		return kbcompile.Draft{}, errors.New("no scripted draft")
	}
	return ds[0], nil
}
func (f *fake) SubmitDraft(_ context.Context, _ uuid.UUID, note string, v int, d kbcompile.Draft) error {
	f.submit = append(f.submit, note+" "+d.Title)
	return nil
}
func (f *fake) RaiseFinding(_ context.Context, _ uuid.UUID, fi kbcompile.Finding) error {
	f.findings = append(f.findings, fi)
	return nil
}

const good = "# Expense policy\n\n## Claims\n\nClaims are made within 14 days. (src: note:expense-policy#claims)\n"

var item = kbcompile.Item{NoteKey: "expense-policy", NoteVersion: 2, Title: "Expense policy", Text: "Expense policy\nClaims are made within 14 days.", Keys: []string{"org.other"}}

func draft(body string) kbcompile.Draft {
	return kbcompile.Draft{Title: "Expense policy", Summary: "How claims are made.", Body: body, Model: "m"}
}

// The compiler turns a changed note into a draft page, checks it with the lint a publication is checked by, tries once more when the
// lint finds something, and raises a finding when the second try fails too: it publishes nothing, because it has no call that does
// (AI-52, AI-57). A note already compiled from its current version is left alone, and a page a person wrote is never overwritten: the
// draft waits beside it with a finding for the person.
func TestTheCompilerMakesDraftsAndRaisesFindingsAndNothingElse(t *testing.T) {
	ctx := context.Background()
	org := uuid.New()
	// A good first draft.
	f := &fake{items: []kbcompile.Item{item}, drafts: map[int][]kbcompile.Draft{1: {draft(good)}}}
	rep, err := kbcompile.Run(ctx, f, org)
	if err != nil || rep.Drafts != 1 || rep.Findings != 0 || len(f.submit) != 1 || f.calls != 1 {
		t.Fatalf("a good draft: %+v %v %+v", rep, err, f)
	}
	// A draft that invents a number is tried again with what the lint found, and the second one passes.
	bad := draft(strings.Replace(good, "14 days", "30 days", 1))
	f = &fake{items: []kbcompile.Item{item}, drafts: map[int][]kbcompile.Draft{1: {bad}, 2: {draft(good)}}}
	rep, err = kbcompile.Run(ctx, f, org)
	if err != nil || rep.Drafts != 1 || f.calls != 2 || len(f.feedback[1]) == 0 || !strings.Contains(f.feedback[1][0], "30") {
		t.Fatalf("a second try: %+v %v calls %d feedback %v", rep, err, f.calls, f.feedback)
	}
	// Two failures: a finding and no draft.
	f = &fake{items: []kbcompile.Item{item}, drafts: map[int][]kbcompile.Draft{1: {bad}, 2: {bad}}}
	rep, _ = kbcompile.Run(ctx, f, org)
	if rep.Drafts != 0 || rep.Findings != 1 || len(f.submit) != 0 || f.findings[0].Kind != "number_not_in_source" || f.findings[0].PageKey != "org.expense-policy" {
		t.Fatalf("two failures: %+v %+v", rep, f.findings)
	}
	// Already compiled from this version of the note: nothing is asked of the model.
	done := item
	done.Latest = &kbcompile.Latest{Number: 3, Origin: "compiled", NoteVersion: 2}
	f = &fake{items: []kbcompile.Item{done}, drafts: map[int][]kbcompile.Draft{1: {draft(good)}}}
	if rep, _ = kbcompile.Run(ctx, f, org); rep.Considered != 1 || f.calls != 0 || len(f.submit) != 0 {
		t.Errorf("an up-to-date page was compiled again: %+v %d", rep, f.calls)
	}
	// A person's version: the draft goes in beside it, with a finding; the person's version is never overwritten.
	edited := item
	edited.Latest = &kbcompile.Latest{Number: 3, Origin: "human_edited", HumanEdited: true, NoteVersion: 1}
	f = &fake{items: []kbcompile.Item{edited}, drafts: map[int][]kbcompile.Draft{1: {draft(good)}}}
	rep, _ = kbcompile.Run(ctx, f, org)
	if rep.Drafts != 1 || rep.Findings != 1 || f.findings[0].Kind != "human_edit_conflict" || f.findings[0].Version != 3 {
		t.Fatalf("a person's version: %+v %+v", rep, f.findings)
	}
	// A model that fails fails that note alone and the run goes on.
	f = &fake{items: []kbcompile.Item{item, item}, failCall: true}
	rep, err = kbcompile.Run(ctx, f, org)
	if err != nil || len(rep.Failed) != 2 || rep.Drafts != 0 {
		t.Errorf("a failing model: %+v %v", rep, err)
	}
}
