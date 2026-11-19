// Package kbcompile is the knowledge-base compiler (PRD-001 AI-52, AI-57; p1-knowledge-base design D7): the job that turns an
// organisation's notes into page drafts. It has no database and no publish: everything it does is one of the five calls of Client,
// which the API serves under the compiler's own service identity, and the interface has no call that publishes, reads the ledger or
// runs a domain command, so the compiler cannot do what AI-57 says it cannot. It reads a note's text as data (the API gives it to the
// model, never the job), asks the model for a page, checks the page with the same lint a publication is checked by, and submits it
// as a draft, or raises a finding when the model cannot make a page that passes. A page a person wrote is never overwritten: the
// recompile is a draft beside it, with a finding for the person to resolve.
package kbcompile

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/knowledge"
)

// Item is a note that needs compiling: its key, and what the organisation's page for it is now.
type Item struct {
	NoteKey     string   `json:"note_key"`
	NoteVersion int      `json:"note_version"`
	Title       string   `json:"title"`
	Text        string   `json:"text"` // the note as the lint reads it: its title and body
	Latest      *Latest  `json:"latest,omitempty"`
	Keys        []string `json:"keys"` // the pages a [[link]] may name
}

// Latest is the newest version of the note's page.
type Latest struct {
	Number      int    `json:"number"`
	Origin      string `json:"origin"`
	HumanEdited bool   `json:"human_edited"`
	NoteVersion int    `json:"note_version"` // the version of the note it was compiled from; 0 when no note's
}

// Draft is a page as the model made it.
type Draft struct {
	Title   string   `json:"title"`
	Summary string   `json:"summary"`
	Aliases []string `json:"aliases"`
	Body    string   `json:"body"`
	Model   string   `json:"model"`
}

// Finding is what the compiler raises when it cannot make a page that passes.
type Finding struct {
	Kind    string `json:"kind"`
	PageKey string `json:"page_key"`
	Version int    `json:"version"`
	Detail  string `json:"detail"`
}

// Client is everything the compiler can do, and all of it: the API refuses it anything else. There is no call that publishes.
type Client interface {
	// Work is the notes of the organisation that changed since their page was compiled.
	Work(ctx context.Context, org uuid.UUID) ([]Item, error)
	// Compile asks the model for the page of a note, with what an earlier attempt's lint found.
	Compile(ctx context.Context, org uuid.UUID, note string, version int, feedback []string) (Draft, error)
	// SubmitDraft stores the page as a draft version of the note's page, authored by the compiler.
	SubmitDraft(ctx context.Context, org uuid.UUID, note string, version int, d Draft) error
	// RaiseFinding records a finding for a person to resolve.
	RaiseFinding(ctx context.Context, org uuid.UUID, f Finding) error
	// Lint has the API run the weekly deterministic lint of the whole knowledge base and raise its findings (task 6.3).
	Lint(ctx context.Context, org uuid.UUID) (found, raised int, err error)
}

// Report is what a run did.
type Report struct {
	Considered int
	Drafts     int
	Findings   int
	Failed     []string // notes whose compile failed on the model or the API, with why
}

// PageKey is the key of the page made from a note.
func PageKey(note string) string { return "org." + note }

// FindingKind is the finding a lint rule is raised as.
func FindingKind(rule string) string {
	switch rule {
	case knowledge.OrgRuleNumber:
		return "number_not_in_source"
	case knowledge.OrgRuleSection, knowledge.OrgRuleCiteSource, knowledge.OrgRuleSourceGone:
		return "citation"
	case knowledge.OrgRuleLink:
		return "missing_cross_reference"
	}
	return "front_matter"
}

// RunLint is the weekly pass: the deterministic lint across the organisation's whole knowledge base, its findings raised for the Owner and
// the Administrator. A model's contradiction pass is the other half of it `[live-verify]`.
func RunLint(ctx context.Context, c Client, org uuid.UUID) (found, raised int, err error) {
	return c.Lint(ctx, org)
}

// Run compiles every note of the organisation that changed.
func Run(ctx context.Context, c Client, org uuid.UUID) (Report, error) {
	var rep Report
	items, err := c.Work(ctx, org)
	if err != nil {
		return rep, fmt.Errorf("list the work: %w", err)
	}
	for _, it := range items {
		rep.Considered++
		if err := compileOne(ctx, c, org, it, &rep); err != nil {
			rep.Failed = append(rep.Failed, it.NoteKey+": "+err.Error())
		}
	}
	return rep, nil
}

// lintDraft is the findings of the lint a publication is checked by, for a draft of the note's page.
func lintDraft(it Item, d Draft) []knowledge.Finding {
	ref := "note:" + it.NoteKey
	keys := map[string]bool{}
	for _, k := range it.Keys {
		keys[k] = true
	}
	return knowledge.LintOrgPage(knowledge.OrgPage{Key: PageKey(it.NoteKey), Summary: d.Summary, Body: d.Body, Sources: []string{ref}},
		func(r string) (string, bool) { return it.Text, r == ref }, func(k string) bool { return keys[k] })
}

func compileOne(ctx context.Context, c Client, org uuid.UUID, it Item, rep *Report) error {
	if it.Latest != nil && it.Latest.NoteVersion >= it.NoteVersion {
		return nil // already compiled from this version of the note
	}
	var feedback []string
	var d Draft
	var found []knowledge.Finding
	for attempt := 0; attempt < 2; attempt++ { // one more try, told what the lint found
		var err error
		if d, err = c.Compile(ctx, org, it.NoteKey, it.NoteVersion, feedback); err != nil {
			return fmt.Errorf("the model: %w", err)
		}
		if found = lintDraft(it, d); len(found) == 0 {
			break
		}
		feedback = feedback[:0]
		for _, f := range found {
			feedback = append(feedback, f.Detail)
		}
	}
	if len(found) > 0 { // no draft: a finding for a person, with what the lint said of the last attempt
		seen := map[string]bool{}
		for _, f := range found {
			kind := FindingKind(f.Rule)
			detail := "the page compiled from the note could not be made to pass: " + f.Detail
			if seen[kind+detail] {
				continue
			}
			seen[kind+detail] = true
			if err := c.RaiseFinding(ctx, org, Finding{Kind: kind, PageKey: PageKey(it.NoteKey), Detail: detail}); err != nil {
				return err
			}
			rep.Findings++
		}
		return nil
	}
	if err := c.SubmitDraft(ctx, org, it.NoteKey, it.NoteVersion, d); err != nil {
		return fmt.Errorf("submit the draft: %w", err)
	}
	rep.Drafts++
	if it.Latest != nil && it.Latest.HumanEdited { // a person's version stands; theirs to compare and decide
		if err := c.RaiseFinding(ctx, org, Finding{Kind: "human_edit_conflict", PageKey: PageKey(it.NoteKey), Version: it.Latest.Number,
			Detail: "the note changed since you edited this page: a recompiled draft waits beside your version, which stays as it is until you decide"}); err != nil {
			return err
		}
		rep.Findings++
	}
	return nil
}

// Instruction is what a retry tells the model: the lint's findings, to fix exactly those.
func Instruction(feedback []string) string {
	if len(feedback) == 0 {
		return ""
	}
	return "Your earlier page did not pass these checks: " + strings.Join(feedback, "; ") + ". Make the page again, fixing exactly these."
}
