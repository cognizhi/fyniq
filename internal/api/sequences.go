package api

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/organisation"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ document number sequences (FR-ORG-04)

// Sequence is a document type's number sequence.
type Sequence struct {
	DocumentType string `json:"document_type" enum:"journal,bill" doc:"The documents it numbers." example:"journal"`
	Prefix       string `json:"prefix" doc:"What every number starts with." example:"JNL-"`
	NextNumber   int64  `json:"next_number" doc:"The number the next document takes." example:"42"`
	Digits       int    `json:"digits" doc:"The number is padded with zeros to this many digits." example:"4"`
	GapFree      bool   `json:"gap_free" doc:"true: a document is numbered when it posts, in the same transaction, so a failed posting takes no number. false: it is numbered when it is saved, so an abandoned draft leaves a gap." example:"true"`
	Example      string `json:"example" doc:"The number the next document takes, as printed." example:"JNL-0042"`
	Version      int    `json:"version" doc:"The sequence's version (0 until it is set up); it also makes the ETag." example:"1"`
}

// SequenceChanges change a sequence: a field left out keeps its value.
type SequenceChanges struct {
	Prefix     *string `json:"prefix,omitempty" maxLength:"12" doc:"1 to 12 capital letters, digits, slashes or hyphens." example:"JV/"`
	NextNumber *int64  `json:"next_number,omitempty" doc:"The next number; it may move forward, never back." example:"100"`
	Digits     *int    `json:"digits,omitempty" doc:"1 to 12." example:"5"`
	GapFree    *bool   `json:"gap_free,omitempty" doc:"Whether documents are numbered when they post (true) or when they are saved." example:"true"`
}

// SequencePage is a page of sequences, by document type.
type SequencePage struct {
	Items      []Sequence `json:"items" doc:"One sequence per document type that is numbered, set up or with its defaults."`
	NextCursor string     `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6ImpvdXJuYWwifQ"`
}

// SequencePath names a document type's sequence.
type SequencePath struct {
	DocumentType string `path:"document_type" enum:"journal,bill" doc:"The document type." example:"journal"`
}

type sequenceListInput struct {
	PageQuery
}

type sequenceUpdateInput struct {
	SequencePath
	IfMatchHeader
	PreferHeader
	Body SequenceChanges
}

type sequenceListOutput struct{ Body SequencePage }

type sequenceReadOutput struct {
	ETag string `header:"ETag" doc:"The sequence's version, for If-Match; \"0\" until it is set up."`
	Body Sequence
}

type sequenceWriteOutput struct {
	ETag    string `header:"ETag" doc:"The sequence's version, for If-Match."`
	Applied string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body    Sequence
}

// SequenceCodes are a sequence's rules.
var SequenceCodes = []string{organisation.CodePrefix, organisation.CodeNext, organisation.CodeDigits}

func sequenceOf(s storage.Sequence) Sequence {
	return Sequence{DocumentType: string(s.DocumentType), Prefix: s.Prefix, NextNumber: s.NextNumber, Digits: s.Digits, GapFree: s.GapFree,
		Example: storage.FormatNumber(s.Prefix, s.NextNumber, s.Digits), Version: s.Version}
}

func registerSequences(a *API) {
	register(a, authz.ListSequences, huma.Operation{
		OperationID: "listSequences", Method: http.MethodGet, Path: "/v1/organisation/sequences", Tags: []string{"Organisation"},
		Summary:     "List the document number sequences",
		Description: "One number sequence per document type that is numbered, as set up or with its defaults (FR-ORG-04). Read-only; no side effects.",
	}, []string{CodeCursor}, func(ctx context.Context, in *sequenceListInput) (*sequenceListOutput, error) {
		p, _ := authn.From(ctx)
		var all []Sequence
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			types := make([]string, 0, len(storage.DefaultSequences))
			for t := range storage.DefaultSequences {
				types = append(types, string(t))
			}
			slices.Sort(types)
			for _, t := range types {
				s, err := u.Sequences().Get(ctx, storage.DocumentType(t))
				if err != nil {
					return err
				}
				all = append(all, sequenceOf(s))
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		items, next, err := pageOf(all, func(s Sequence) string { return s.DocumentType }, in.PageQuery)
		if err != nil {
			return nil, err
		}
		return &sequenceListOutput{Body: SequencePage{Items: items, NextCursor: next}}, nil
	})

	register(a, authz.GetSequence, huma.Operation{
		OperationID: "getSequence", Method: http.MethodGet, Path: "/v1/organisation/sequences/{document_type}", Tags: []string{"Organisation"},
		Summary:     "Get a document number sequence",
		Description: "A document type's number sequence with its ETag; \"0\" while it still has its defaults (FR-ORG-04). Read-only; no side effects.",
	}, nil, func(ctx context.Context, in *SequencePath) (*sequenceReadOutput, error) {
		p, _ := authn.From(ctx)
		var out sequenceReadOutput
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			s, err := u.Sequences().Get(ctx, storage.DocumentType(in.DocumentType))
			out.Body, out.ETag = sequenceOf(s), etag(s.Version)
			return err
		})
		return &out, err
	})

	register(a, authz.UpdateSequence, huma.Operation{
		OperationID: "updateSequence", Method: http.MethodPatch, Path: "/v1/organisation/sequences/{document_type}", Tags: []string{"Organisation"},
		Summary: "Change a document number sequence",
		Description: "Sets up or changes a document type's number sequence: its prefix, next number (forward only, so a number is never " +
			"issued twice), padding and whether it is gap-free (FR-ORG-04). Numbers already given never change. Side effects: the " +
			"sequence's next version and its audit event. Needs If-Match with its ETag (\"0\" while it has its defaults). Approval " +
			"policy: the Owner approves once the approval engine arrives (P1). Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, storage.CodeVersion, storage.CodeSequence,
		storage.CodeRetry, storage.CodeLockTimeout}, SequenceCodes...),
		func(ctx context.Context, in *sequenceUpdateInput) (*sequenceWriteOutput, error) {
			t := storage.DocumentType(in.DocumentType)
			w, err := write(ctx, a, "", in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (Sequence, error) {
				cur, err := u.Sequences().Get(ctx, t)
				if err != nil {
					return Sequence{}, err
				}
				if err := checkIfMatch(in.IfMatch, etag(cur.Version)); err != nil {
					return Sequence{}, err
				}
				was := storage.SequenceSettings{Prefix: cur.Prefix, NextNumber: cur.NextNumber, Digits: cur.Digits, GapFree: cur.GapFree}
				next := was
				if in.Body.Prefix != nil {
					next.Prefix = strings.TrimSpace(*in.Body.Prefix)
				}
				if in.Body.NextNumber != nil {
					next.NextNumber = *in.Body.NextNumber
				}
				if in.Body.Digits != nil {
					next.Digits = *in.Body.Digits
				}
				if in.Body.GapFree != nil {
					next.GapFree = *in.Body.GapFree
				}
				var bad []FieldProblem
				for _, x := range organisation.CheckSequence(was, next) {
					bad = append(bad, FieldProblem{Code: x.Code, Field: "body." + x.Field, Message: x.Message, Fix: x.Fix})
				}
				if len(bad) > 0 {
					return Sequence{}, invalid(bad)
				}
				s, err := u.Sequences().Update(ctx, t, cur.Version, next)
				return sequenceOf(s), err
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &sequenceWriteOutput{Applied: applied(w), Body: w.Body}, nil
			}
			return &sequenceWriteOutput{ETag: etag(w.Body.Version), Body: w.Body}, nil
		})
}
