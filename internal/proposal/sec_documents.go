package proposal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/corporate"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/render"
)

// documentDraft is sec.draft_document's payload: the kind of document, the fields its template asks for, and a note.
type documentDraft struct {
	Kind   string           `json:"kind"`
	Fields corporate.Values `json:"fields"`
	Note   string           `json:"note"`
}

// documentText is the base key of the paper's text: the dry run's, which a person approved.
const documentText = "corporate_document_text"

// textOf fingerprints a paper's text as a person reads it, its title, company, lines, sections and signatures, and not
// its banner, whose day is the day it is drafted: the base version the executor checks (AI-P5).
func textOf(p render.Paper) int64 {
	b, _ := json.Marshal([]any{p.Title, p.Author, p.Lines, p.Sections, p.Signatures})
	h := fnv.New64a()
	_, _ = h.Write(b)
	return int64(h.Sum64() >> 1)
}

// problemsError is a document that does not fit, as a proposal's refusal: each problem with its field in the payload.
func problemsError(err error) error {
	var probs ledger.Problems
	if !errors.As(err, &probs) || len(probs) == 0 {
		return err
	}
	msgs := make([]string, len(probs))
	for i, p := range probs {
		msgs[i] = "payload." + p.Field + ": " + p.Message
	}
	return &storage.Error{Code: storage.CodeInvalid, Message: strings.Join(msgs, "; "), Hint: probs[0].Fix}
}

// DraftCorporateDocument is sec.draft_document's command (Appendix E; p1-company-secretary task 4.2, design D6): a
// corporate document drafted from one of Fyniq's templates and filled from the records on the day, as a person drafts it
// with createCorporateDocument. The dry run shows its text. Executed as the approving person, it keeps the draft, naming
// the proposal it came from: never signed, filed or recorded in the minute book, which a person does with the signed
// copy. When the records change its text since the dry run, it is stale and drafts nothing (AI-P5).
type DraftCorporateDocument struct{}

// Policy implements Command: drafting a corporate document (C on "Corporate profile and registers"; the Owner too,
// as they approve it under Appendix E).
func (DraftCorporateDocument) Policy() authz.Policy { return authz.CreateCorporateDocument }

// Measure implements Command: one document, no amount.
func (DraftCorporateDocument) Measure(payload json.RawMessage) (Measure, error) {
	var d documentDraft
	if err := json.Unmarshal(payload, &d); err != nil {
		return Measure{}, err
	}
	return Measure{Items: 1}, nil
}

// DryRun implements Command: the document as it would be drafted from the records on today, its text in full; refused,
// naming each field, when it does not fit or no one in the records could sign it.
func (DraftCorporateDocument) DryRun(ctx context.Context, u storage.UnitOfWork, payload json.RawMessage, as Actor) (Impact, error) {
	var d documentDraft
	if err := json.Unmarshal(payload, &d); err != nil {
		return Impact{}, err
	}
	doc, err := corporate.FillDocument(ctx, u, d.Kind, d.Fields, strings.TrimSpace(d.Note), as.Today)
	if err != nil {
		return Impact{}, problemsError(err)
	}
	changes, err := json.Marshal(map[string]any{"corporate_documents": []map[string]any{{"change": "draft", "kind": doc.Kind, "template": doc.Template,
		"title": doc.Paper.Title, "paper": doc.Paper}}})
	if err != nil {
		return Impact{}, err
	}
	title := strings.ToLower(doc.Paper.Title[:1]) + doc.Paper.Title[1:]
	return Impact{Summary: fmt.Sprintf("Drafts the %s (%s) from the records, for the directors and the company secretary to review; nothing is signed or filed",
		title, doc.Kind), Changes: changes, Base: map[string]int64{documentText: textOf(doc.Paper)}}, nil
}

// Execute implements Command: the draft, kept as the approving person with the proposal it came from, unless the
// records changed its text since the dry run.
func (DraftCorporateDocument) Execute(ctx context.Context, u storage.UnitOfWork, payload json.RawMessage, base map[string]int64, as Actor) (Execution, error) {
	var d documentDraft
	if err := json.Unmarshal(payload, &d); err != nil {
		return Execution{}, err
	}
	want, ok := base[documentText]
	if !ok {
		return Execution{}, fmt.Errorf("the dry run did not read %s", documentText)
	}
	doc, err := corporate.FillDocument(ctx, u, d.Kind, d.Fields, strings.TrimSpace(d.Note), as.Today)
	if err != nil {
		return Execution{}, problemsError(err)
	}
	if textOf(doc.Paper) != want {
		return Execution{}, ErrStale // a director, the secretary, a member or the company's details changed since the dry run
	}
	proposal := as.Proposal
	made, err := corporate.DraftDocumentIn(ctx, u, d.Kind, d.Fields, strings.TrimSpace(d.Note), &proposal, as.Today)
	if err != nil {
		return Execution{}, problemsError(err)
	}
	return Execution{Records: []string{"corporate_document:" + made.ID.String()}, Entries: []string{}}, nil
}
