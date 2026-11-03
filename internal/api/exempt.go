package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/director"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ exempt-private-company status (FR-EQ-11; p1-founders-money)
//
// Until the corporate profile exists (FR-CS-01, p1-company-secretary), the organisation's exempt-private-company status
// is an effective-dated fact of its own (the owner's decision of 3 Nov 2026, rev 0.167): drafted by an Owner or the
// Controller (A on "Director accounts"), with the ACRA business profile attached, and recorded, append-only, posting no
// entry. A director's account in debit is warned of by it: allowed but disclosed for an exempt private company; the
// Companies Act s162 ban and its exception for one that is not, or whose status nobody recorded (design D9).

// ExemptCodes are the codes an exempt status's own checks answer.
var ExemptCodes = []string{director.CodeExemptStatus, director.CodeNotExemptStatus}

// ExemptStatusDraft is a status as drafted.
type ExemptStatusDraft struct {
	From   string `json:"from" format:"date" doc:"The day it applies from: the day the company became, or stopped being, exempt." example:"2025-03-14"`
	Exempt bool   `json:"exempt" doc:"An exempt private company: no corporate shareholder and at most 20 members. False once, for example, a corporate investor joins." example:"true"`
	Note   string `json:"note,omitempty" maxLength:"1000" doc:"What shows it." example:"ACRA business profile of 14 Mar 2025"`
}

// ExemptStatusRecord is a status, drafted or recorded.
type ExemptStatusRecord struct {
	ID         string      `json:"id" format:"uuid" doc:"Its ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4fe1"`
	Status     string      `json:"status" enum:"draft,recorded" doc:"draft: waiting for its evidence to be attached and recorded; recorded: in force from its day until a later one." example:"recorded"`
	Version    int         `json:"version" doc:"The version of its content; with its status it makes the ETag." example:"1"`
	From       string      `json:"from" format:"date" doc:"The day it applies from." example:"2025-03-14"`
	Exempt     bool        `json:"exempt" doc:"An exempt private company from that day." example:"true"`
	Note       string      `json:"note,omitempty" doc:"Its note." example:"ACRA business profile of 14 Mar 2025"`
	Evidence   int         `json:"evidence" doc:"How many files are attached and not removed: recording needs one, such as the ACRA business profile." example:"1"`
	RecordedBy *Person     `json:"recorded_by,omitempty" doc:"Who recorded it."`
	RecordedAt *time.Time  `json:"recorded_at,omitempty" doc:"When it was recorded." example:"2025-03-15T09:30:00Z"`
	Checks     []BillCheck `json:"checks" nullable:"false" doc:"A draft's status: ✖ while no evidence is attached."`
}

// ExemptStatusInForce is the status in force on a day.
type ExemptStatusInForce struct {
	Status     string     `json:"status" enum:"exempt,not_exempt,not_recorded" doc:"exempt; not_exempt; or not_recorded, which is warned of as not exempt (rev 0.167)." example:"exempt"`
	From       string     `json:"from,omitempty" format:"date" doc:"The day the recorded status applies from." example:"2025-03-14"`
	StatusID   string     `json:"status_id,omitempty" format:"uuid" doc:"The recorded status." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4fe1"`
	RecordedAt *time.Time `json:"recorded_at,omitempty" doc:"When it was recorded." example:"2025-03-15T09:30:00Z"`
}

// ExemptStatusPage is a page of statuses, the latest day first.
type ExemptStatusPage struct {
	Items      []ExemptStatusRecord `json:"items" nullable:"false"`
	NextCursor string               `json:"next_cursor,omitempty" doc:"Pass it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjIwMjUtMDMtMTR8MDE5MiJ9"`
}

// ExemptStatusPath names a status in the path.
type ExemptStatusPath struct {
	StatusID string `path:"status_id" format:"uuid" doc:"The status's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4fe1"`
}

type exemptListInput struct {
	PageQuery
	Status string `query:"status" enum:"draft,recorded" doc:"Only the statuses with this status." example:"recorded"`
}

type exemptListOutput struct{ Body ExemptStatusPage }

type createExemptInput struct {
	KeyHeader
	PreferHeader
	Body ExemptStatusDraft
}

type replaceExemptInput struct {
	ExemptStatusPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body ExemptStatusDraft
}

type recordExemptInput struct {
	ExemptStatusPath
	KeyHeader
	IfMatchHeader
	PreferHeader
}

type exemptOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"Its version and status, for If-Match."`
	Location string `header:"Location" doc:"Its URL."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     ExemptStatusRecord
}

type exemptReadOutput struct {
	ETag string `header:"ETag" doc:"Its version and status, for If-Match."`
	Body ExemptStatusRecord
}

func exemptETag(doc storage.Document) string { return etag(doc.Version, string(doc.Status)) }

// exemptIn reads a status as sent.
func exemptIn(d ExemptStatusDraft) (director.ExemptStatus, error) {
	from, err := time.Parse(time.DateOnly, d.From)
	if err != nil {
		return director.ExemptStatus{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.from", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"}})
	}
	return director.ExemptStatus{From: from, Exempt: d.Exempt, Note: strings.TrimSpace(d.Note)}, nil
}

// exemptOf is a status as the API shows it.
func (a *API) exemptOf(ctx context.Context, u storage.UnitOfWork, doc storage.Document) (ExemptStatusRecord, error) {
	x, err := director.ReadExemptStatus(doc)
	if err != nil {
		return ExemptStatusRecord{}, err
	}
	out := ExemptStatusRecord{ID: doc.ID.String(), Status: "draft", Version: doc.Version, From: dateOf(x.From), Exempt: x.Exempt, Note: x.Note, Checks: []BillCheck{}}
	if out.Evidence, err = director.Attached(ctx, u, doc.ID); err != nil {
		return ExemptStatusRecord{}, err
	}
	if doc.Status == storage.DocumentPosted {
		out.Status, out.RecordedBy, out.RecordedAt = "recorded", personOf(doc.PostedBy), doc.PostedAt
		return out, a.name(ctx, u, out.RecordedBy)
	}
	if out.Evidence == 0 {
		out.Checks = append(out.Checks, BillCheck{Severity: "error", Code: director.CodeNoProfile, Field: "evidence",
			Message: "it has no evidence attached, so it cannot be recorded", Fix: "Attach the ACRA business profile that shows it"})
	}
	return out, nil
}

// exemptInForce is the status in force on a day, from every recorded status.
func exemptInForce(statuses []director.RecordedStatus, day time.Time) (ExemptStatusInForce, *director.RecordedStatus) {
	s := director.ExemptOn(statuses, day)
	if s == nil {
		return ExemptStatusInForce{Status: "not_recorded"}, nil
	}
	at := s.RecordedAt
	out := ExemptStatusInForce{Status: "not_exempt", From: dateOf(s.Status.From), StatusID: s.ID.String(), RecordedAt: &at}
	if s.Status.Exempt {
		out.Status = "exempt"
	}
	return out, s
}

// readableExemptStatus is a status the caller may read: not found for one whose right on "Director accounts" is
// qualified.
func readableExemptStatus(ctx context.Context, u storage.UnitOfWork, p authn.Principal, id uuid.UUID) (storage.Document, error) {
	if !authz.Decide(authz.GetExemptStatus, p.Roles, p.AI()).Allowed {
		return storage.Document{}, ports.ErrNotFound
	}
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return doc, err
	}
	if !director.IsExemptStatus(doc) {
		return doc, ports.ErrNotFound
	}
	return doc, nil
}

func registerExempt(a *API) {
	readCodes := []string{CodeNotFound, director.CodeNotExemptStatus, director.CodeExemptStatus}

	register(a, authz.ListExemptStatuses, huma.Operation{
		OperationID: "listExemptStatuses", Method: http.MethodGet, Path: "/v1/exempt-statuses", Tags: []string{"Founders"},
		Summary: "List the exempt-private-company statuses",
		Description: "The organisation's exempt-private-company statuses, drafted and recorded, the latest day first, a page at a time " +
			"(FR-EQ-11). The status in force is on the director accounts (listDirectorAccounts). Needs the right to read every " +
			"director's account. Read-only; no side effects.",
	}, append([]string{CodeCursor, CodeInvalid}, readCodes...), func(ctx context.Context, in *exemptListInput) (*exemptListOutput, error) {
		after, err := decodeCursor(in.Cursor)
		if err != nil {
			return nil, err
		}
		afterDate, afterID, err := journalCursor(after)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		out := exemptListOutput{Body: ExemptStatusPage{Items: []ExemptStatusRecord{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			status := storage.DocumentStatus("")
			switch in.Status {
			case "draft":
				status = storage.DocumentDraft
			case "recorded":
				status = storage.DocumentPosted
			}
			for {
				docs, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: storage.DocumentExemptStatus, Status: status, AfterDate: afterDate,
					AfterID: afterID, Limit: 500})
				if err != nil {
					return err
				}
				for _, doc := range docs {
					afterDate, afterID = doc.Date, &doc.ID
					if len(out.Body.Items) == in.PageSize {
						last := out.Body.Items[len(out.Body.Items)-1]
						out.Body.NextCursor = encodeCursor(last.From + "|" + last.ID)
						return nil
					}
					x, err := a.exemptOf(ctx, u, doc)
					if err != nil {
						return err
					}
					out.Body.Items = append(out.Body.Items, x)
				}
				if len(docs) < 500 {
					return nil
				}
			}
		})
		return &out, err
	})

	register(a, authz.GetExemptStatus, huma.Operation{
		OperationID: "getExemptStatus", Method: http.MethodGet, Path: "/v1/exempt-statuses/{status_id}", Tags: []string{"Founders"},
		Summary:     "Get an exempt-private-company status",
		Description: "Returns a status with its ETag: its day, whether exempt, its note, how many files are attached, and who recorded it (FR-EQ-11). Read-only; no side effects.",
	}, readCodes, func(ctx context.Context, in *ExemptStatusPath) (*exemptReadOutput, error) {
		id, err := parseID("status_id", in.StatusID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out exemptReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := readableExemptStatus(ctx, u, p, id)
			if err != nil {
				return err
			}
			out.ETag = exemptETag(doc)
			out.Body, err = a.exemptOf(ctx, u, doc)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &out, nil
	})

	writeCodes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid,
		storage.CodeRetry, storage.CodeLockTimeout}, ExemptCodes...)
	shown := func(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (ExemptStatusRecord, error) {
		doc, err := u.Documents().Get(ctx, id)
		if err != nil {
			return ExemptStatusRecord{}, err
		}
		return a.exemptOf(ctx, u, doc)
	}
	answer := func(w written[ExemptStatusRecord]) *exemptOutput {
		if w.Previewed {
			return &exemptOutput{Status: w.Status, Applied: applied(w), Body: w.Body}
		}
		status := "draft"
		if w.Body.Status == "recorded" {
			status = "posted"
		}
		return &exemptOutput{Status: w.Status, ETag: etag(w.Body.Version, status), Location: "/v1/exempt-statuses/" + w.Body.ID,
			Replayed: replayedHeader(w.Replayed), Body: w.Body}
	}

	register(a, authz.CreateExemptStatus, huma.Operation{
		OperationID: "createExemptStatus", Method: http.MethodPost, Path: "/v1/exempt-statuses", Tags: []string{"Founders"},
		DefaultStatus: http.StatusCreated, Summary: "Draft an exempt-private-company status",
		Description: "Drafts the organisation's exempt-private-company status from a day on (FR-EQ-11): exempt, or not, for example once a " +
			"corporate investor joins. Attach its evidence with addExemptStatusEvidence, then record it with recordExemptStatus. Side " +
			"effects: the draft, audit events. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, writeCodes, func(ctx context.Context, in *createExemptInput) (*exemptOutput, error) {
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (ExemptStatusRecord, error) {
			x, err := exemptIn(in.Body)
			if err != nil {
				return ExemptStatusRecord{}, err
			}
			doc, err := director.DraftExemptStatusIn(ctx, u, x)
			if err != nil {
				return ExemptStatusRecord{}, err
			}
			return shown(ctx, u, doc.ID)
		})
		if err != nil {
			return nil, err
		}
		return answer(w), nil
	})

	register(a, authz.ReplaceExemptStatus, huma.Operation{
		OperationID: "replaceExemptStatus", Method: http.MethodPut, Path: "/v1/exempt-statuses/{status_id}", Tags: []string{"Founders"},
		Summary: "Change a drafted exempt-private-company status",
		Description: "Saves a new version of a status not recorded yet (FR-EQ-11). A recorded status changes no more (FOUNDER-23): record a " +
			"new one from the day it changes. Side effects: the new version, audit events. Needs If-Match with its ETag. Idempotent " +
			"with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, director.CodeStatusRecorded}, writeCodes...), func(ctx context.Context, in *replaceExemptInput) (*exemptOutput, error) {
		id, err := parseID("status_id", in.StatusID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (ExemptStatusRecord, error) {
			doc, _, err := director.DraftedExemptStatus(ctx, u, id)
			if err != nil {
				return ExemptStatusRecord{}, err
			}
			if err := checkIfMatch(in.IfMatch, exemptETag(doc)); err != nil {
				return ExemptStatusRecord{}, err
			}
			x, err := exemptIn(in.Body)
			if err != nil {
				return ExemptStatusRecord{}, err
			}
			if _, err := director.ReviseExemptStatusIn(ctx, u, id, x); err != nil {
				return ExemptStatusRecord{}, err
			}
			return shown(ctx, u, id)
		})
		if err != nil {
			return nil, err
		}
		return answer(w), nil
	})

	register(a, authz.RecordExemptStatus, huma.Operation{
		OperationID: "recordExemptStatus", Method: http.MethodPost, Path: "/v1/exempt-statuses/{status_id}/record", Tags: []string{"Founders"},
		Summary: "Record an exempt-private-company status",
		Description: "Records a drafted status (FR-EQ-11): in force from its day until a later one, append-only, posting no entry. It needs " +
			"its evidence attached (FOUNDER-22), such as the ACRA business profile. The director accounts' warnings follow it at once. " +
			"Side effects: the status recorded, audit events. Needs If-Match with its ETag. Idempotent with Idempotency-Key; Prefer: " +
			"return=preview saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, director.CodeStatusRecorded, director.CodeNoProfile, storage.CodeDocumentStatus}, writeCodes...),
		func(ctx context.Context, in *recordExemptInput) (*exemptOutput, error) {
			id, err := parseID("status_id", in.StatusID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (ExemptStatusRecord, error) {
				doc, _, err := director.DraftedExemptStatus(ctx, u, id)
				if err != nil {
					return ExemptStatusRecord{}, err
				}
				if err := checkIfMatch(in.IfMatch, exemptETag(doc)); err != nil {
					return ExemptStatusRecord{}, err
				}
				if _, err := director.RecordExemptStatusIn(ctx, u, id); err != nil {
					return ExemptStatusRecord{}, err
				}
				if a.deps.Outbox != nil { // its evidence committed now, not at the next sweep (FR-DOC-13)
					p, _ := authn.From(ctx)
					if _, err := a.deps.Outbox.Enqueue(ctx, u, p.Organisation, storage.NewOutboxItem{Class: storage.ClassWorker,
						Kind: EvidenceCommitKind, Payload: []byte(`{}`)}); err != nil {
						return ExemptStatusRecord{}, err
					}
				}
				return shown(ctx, u, id)
			})
			if err != nil {
				return nil, err
			}
			return answer(w), nil
		})

	registerExemptEvidence(a)
}
