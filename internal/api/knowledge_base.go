package api

import (
	"context"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The organisation's knowledge base, administered under Settings → AI → Knowledge base (PRD-001 AI-55, AI-56; p1-knowledge-base
// design D9): the Owner and the Administrator write the notes, policies and decisions its pages are compiled from, and the
// Auditor reads them. These operations are not offered to the assistant, which reads pages only through the knowledge-base
// tools, and are on the never-list for MCP and agent clients (§9.6).

// KBNoteInput is a note, policy or decision.
type KBNoteInput struct {
	Key       string `json:"key,omitempty" pattern:"^[a-z0-9][a-z0-9.-]{0,99}$" doc:"Its key, once and for good: lower-case words, dots and hyphens." example:"expense-policy"`
	Kind      string `json:"kind" enum:"note,policy,decision" doc:"What it is." example:"policy"`
	Title     string `json:"title" minLength:"1" maxLength:"200" doc:"Its title." example:"Expense policy"`
	Body      string `json:"body" minLength:"1" maxLength:"20000" doc:"What it says, in words the assistant may explain to colleagues. No secrets, bank account numbers or identity numbers." example:"Claims are made within 30 days of the receipt date."`
	DataClass string `json:"data_class,omitempty" enum:"general,personal,payroll,equity" doc:"The strictest class of data it holds: a page made from it is shown only to those who may read that class (AI-54); left out, general." example:"general"`
}

// KBNote is a note as saved.
type KBNote struct {
	Key       string `json:"key" example:"expense-policy"`
	Kind      string `json:"kind" enum:"note,policy,decision" example:"policy"`
	Title     string `json:"title" example:"Expense policy"`
	Body      string `json:"body" example:"Claims are made within 30 days of the receipt date."`
	DataClass string `json:"data_class" enum:"general,personal,payroll,equity" example:"general"`
	Version   int    `json:"version" doc:"Its version; the ETag carries it for If-Match." example:"2"`
	SavedBy   string `json:"saved_by" doc:"Who last saved it: user:<id>." example:"user:0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2d"`
	SavedAt   string `json:"saved_at" format:"date-time" example:"2026-11-19T08:00:00Z"`
}

// KBNotes are the notes.
type KBNotes struct {
	Items []KBNote `json:"items" nullable:"false"`
}

type (
	// KBNotePath is a note's path parameter (exported: huma reads only an exported embedded struct).
	KBNotePath struct {
		Key string `path:"key" pattern:"^[a-z0-9][a-z0-9.-]{0,99}$" doc:"The note's key." example:"expense-policy"`
	}
	kbNotesOutput  struct{ Body KBNotes }
	kbNoteCreateIn struct {
		KeyHeader
		PreferHeader
		Body KBNoteInput
	}
	kbNoteReviseIn struct {
		KBNotePath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body KBNoteInput
	}
	kbNoteOut struct {
		Status   int
		ETag     string `header:"ETag" doc:"Its version, for If-Match."`
		Location string `header:"Location" doc:"Its URL."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     KBNote
	}
)

func kbNoteETag(n KBNote) string { return etag("v" + strconv.Itoa(n.Version)) }

func kbNoteOf(n storage.KBNote) KBNote {
	return KBNote{Key: n.Key, Kind: n.Kind, Title: n.Title, Body: n.Body, DataClass: n.DataClass, Version: n.Version,
		SavedBy: "user:" + n.UpdatedBy.String(), SavedAt: n.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00")}
}

// secretShape is what a note may never hold: a full NRIC or FIN, a long run of digits such as an account number, or a
// credential-looking token (SEC-15, AI-02). The model sees the pages made from notes, so this is the first place to stop it.
var secretShape = regexp.MustCompile(`(?i)\b[STFGM]\d{7}[A-Z]\b|\b\d{9,}\b|\b(?:sk|pk|api|key|token|secret)[-_](?:[A-Za-z]+[-_])*[A-Za-z0-9]{16,}\b`)

func kbNoteFields(in KBNoteInput) ([]FieldProblem, storage.KBNote) {
	var problems []FieldProblem
	n := storage.KBNote{Key: in.Key, Kind: in.Kind, Title: strings.TrimSpace(in.Title), Body: strings.TrimSpace(in.Body), DataClass: in.DataClass}
	if n.Title == "" {
		problems = append(problems, FieldProblem{Code: CodeInvalid, Field: "body.title", Message: "is empty", Fix: "Give the note a title"})
	}
	if n.Body == "" {
		problems = append(problems, FieldProblem{Code: CodeInvalid, Field: "body.body", Message: "is empty", Fix: "Write what the note says"})
	}
	if m := secretShape.FindString(n.Body + " " + n.Title); m != "" {
		problems = append(problems, FieldProblem{Code: CodeInvalid, Field: "body.body", Message: "holds what looks like an identity number, a bank account number or a secret",
			Fix: "Take it out: the assistant reads the pages made from notes, and these never reach it (AI-02)"})
	}
	return problems, n
}

func registerKnowledgeBase(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid,
		storage.CodeKnowledge, storage.CodeVersion}, locks...)
	person := func(ctx context.Context) (authn.Principal, error) {
		p, _ := authn.From(ctx)
		if p.Class != authn.ClassUser || p.UserID == uuid.Nil {
			return p, problem(CodeSessionRequired, "A person signed in writes the organisation's notes.", "Sign in to Fyniq")
		}
		return p, nil
	}
	register(a, authz.ListKBNotes, exempt(huma.Operation{
		OperationID: "listKBNotes", Method: http.MethodGet, Path: "/v1/knowledge-base/notes", Tags: []string{"Knowledge base"},
		Summary: "List the organisation's knowledge-base notes",
		Description: "Every note, policy and decision the organisation's pages are compiled from (AI-50), by key, with their text and version. " +
			"Read-only; no side effects. On the never-list for MCP and agent clients (§9.6).",
	}, "cursor", "An organisation's notes are a few dozen, read whole: the page sorts and filters them itself."),
		append([]string{CodeInvalid}, locks...), func(ctx context.Context, _ *struct{}) (*kbNotesOutput, error) {
			p, _ := authn.From(ctx)
			out := &kbNotesOutput{Body: KBNotes{Items: []KBNote{}}}
			err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				all, err := u.OrganisationKnowledge().Notes(ctx)
				for _, n := range all {
					out.Body.Items = append(out.Body.Items, kbNoteOf(n))
				}
				return err
			})
			return out, err
		})

	answer := func(w written[KBNote], location string) *kbNoteOut {
		if w.Previewed {
			return &kbNoteOut{Status: w.Status, Applied: applied(w), Body: w.Body}
		}
		return &kbNoteOut{Status: w.Status, ETag: kbNoteETag(w.Body), Location: location, Replayed: replayedHeader(w.Replayed), Body: w.Body}
	}
	register(a, authz.CreateKBNote, huma.Operation{
		OperationID: "createKBNote", Method: http.MethodPost, Path: "/v1/knowledge-base/notes", Tags: []string{"Knowledge base"}, DefaultStatus: http.StatusCreated,
		Summary: "Write a knowledge-base note",
		Description: "Writes a note, policy or decision at version 1 (AI-50). It is a source: nothing the assistant says changes until a page compiled " +
			"from it is published, which a person approves (AI-56). A note holding what looks like an identity number, a bank account number or a " +
			"secret is refused (AI-02). Side effects: the note, the audit event kb_note.created. Idempotent with Idempotency-Key; Prefer: " +
			"return=preview saves nothing. On the never-list for MCP and agent clients (§9.6).",
	}, append([]string{CodeSessionRequired}, writes...), func(ctx context.Context, in *kbNoteCreateIn) (*kbNoteOut, error) {
		p, err := person(ctx)
		if err != nil {
			return nil, err
		}
		problems, n := kbNoteFields(in.Body)
		if in.Body.Key == "" {
			problems = append(problems, FieldProblem{Code: CodeInvalid, Field: "body.key", Message: "is missing", Fix: "Give the note a key, such as expense-policy"})
		}
		if len(problems) > 0 {
			return nil, invalid(problems)
		}
		now := a.deps.Clock.Now()
		n.UpdatedBy, n.UpdatedAt = p.UserID, now
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (KBNote, error) {
			if _, err := u.OrganisationKnowledge().Note(ctx, n.Key); err == nil {
				return KBNote{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.key", Message: "is taken by another note", Fix: "Choose another key, or change that note"}})
			} else if err != ports.ErrNotFound {
				return KBNote{}, err
			}
			saved, err := u.OrganisationKnowledge().SaveNote(ctx, n, 0)
			return kbNoteOf(saved), err
		})
		if err != nil {
			return nil, err
		}
		return answer(w, "/v1/knowledge-base/notes/"+n.Key), nil
	})

	register(a, authz.ReviseKBNote, huma.Operation{
		OperationID: "reviseKBNote", Method: http.MethodPut, Path: "/v1/knowledge-base/notes/{key}", Tags: []string{"Knowledge base"},
		Summary: "Change a knowledge-base note",
		Description: "Saves the note's next version (AI-50): its title, text, kind or data class; its key never changes. The pages made from it " +
			"are recompiled and published only with a person's approval (AI-56). Side effects: the new version, the audit event kb_note.changed. " +
			"Needs If-Match with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing. On the never-list for MCP and agent clients (§9.6).",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeSessionRequired}, writes...), func(ctx context.Context, in *kbNoteReviseIn) (*kbNoteOut, error) {
		p, err := person(ctx)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		problems, n := kbNoteFields(in.Body)
		if len(problems) > 0 {
			return nil, invalid(problems)
		}
		n.Key = in.Key
		now := a.deps.Clock.Now()
		n.UpdatedBy, n.UpdatedAt = p.UserID, now
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (KBNote, error) {
			old, err := u.OrganisationKnowledge().Note(ctx, in.Key)
			if err != nil {
				return KBNote{}, err
			}
			if err := checkIfMatch(in.IfMatch, kbNoteETag(kbNoteOf(old))); err != nil {
				return KBNote{}, err
			}
			saved, err := u.OrganisationKnowledge().SaveNote(ctx, n, old.Version)
			return kbNoteOf(saved), err
		})
		if err != nil {
			return nil, err
		}
		return answer(w, "/v1/knowledge-base/notes/"+in.Key), nil
	})
}
