package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/period"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// HoldKind is what a retention hold names.
type HoldKind string

// Schema documents the kinds.
func (HoldKind) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Enum: []any{"record", "contact", "financial_year"}, Examples: []any{"record"},
		Description: "record: one source document, with its events, ledger entries and evidence files; contact: a contact, every document that " +
			"names it, and their events, entries and files; financial_year: every ledger entry dated in the year, every event that occurred in it and " +
			"the evidence of documents dated in it."}
}

// RetentionHoldView is a legal hold on records (AUD-11, rev 0.617): while it stands the purge after retention skips what it
// covers, the database refuses to delete it, and the evidence files it covers keep a storage hold.
type RetentionHoldView struct {
	HoldID         string   `json:"hold_id" format:"uuid" doc:"The hold." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	Kind           HoldKind `json:"kind" doc:"What it names."`
	DocumentID     string   `json:"document_id,omitempty" format:"uuid" doc:"A record's source document." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e11"`
	DocumentType   string   `json:"document_type,omitempty" doc:"The document's type." example:"invoice"`
	DocumentNumber string   `json:"document_number,omitempty" doc:"The document's number, once it has one." example:"INV-0042"`
	ContactID      string   `json:"contact_id,omitempty" format:"uuid" doc:"A contact." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e12"`
	ContactName    string   `json:"contact_name,omitempty" doc:"The contact's legal name." example:"Acme Pte Ltd"`
	YearNumber     int      `json:"year_number,omitempty" doc:"A financial year's number in the organisation's calendar, 1 for the first." example:"1"`
	YearStartsOn   string   `json:"year_starts_on,omitempty" format:"date" doc:"The year's first day." example:"2026-01-01"`
	YearEndsOn     string   `json:"year_ends_on,omitempty" format:"date" doc:"The year's last day." example:"2026-12-31"`
	Reason         string   `json:"reason" doc:"Why it was placed." example:"A dispute with the supplier is open"`
	PlacedBy       string   `json:"placed_by" doc:"Who placed it, as records name actors." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	PlacedAt       string   `json:"placed_at" format:"date-time" doc:"When." example:"2026-11-09T02:00:00Z"`
	InForce        bool     `json:"in_force" doc:"true while it stands." example:"true"`
	LiftedBy       string   `json:"lifted_by,omitempty" doc:"Who lifted it." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	LiftedAt       string   `json:"lifted_at,omitempty" format:"date-time" doc:"When." example:"2026-12-01T02:00:00Z"`
	LiftReason     string   `json:"lift_reason,omitempty" doc:"Why it was lifted." example:"The dispute is settled"`
}

// RetentionHoldList is the organisation's holds, the newest placed first.
type RetentionHoldList struct {
	Items   []RetentionHoldView `json:"items" nullable:"false" doc:"The holds, the newest placed first."`
	InForce int                 `json:"in_force" doc:"How many stand." example:"1"`
}

// RetentionHoldInput places a hold: its kind, what it names (the field of that kind only) and why.
type RetentionHoldInput struct {
	Kind          HoldKind `json:"kind" doc:"What the hold names."`
	DocumentID    string   `json:"document_id,omitempty" format:"uuid" doc:"For a record: its source document." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e11"`
	ContactID     string   `json:"contact_id,omitempty" format:"uuid" doc:"For a contact." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e12"`
	FinancialYear int      `json:"financial_year,omitempty" minimum:"1" maximum:"200" doc:"For a financial year: its number in the organisation's calendar, 1 for the first." example:"1"`
	Reason        string   `json:"reason" minLength:"3" maxLength:"500" doc:"Why: the dispute, claim, audit or inquiry the records are held for." example:"A dispute with the supplier is open"`
}

// LiftRetentionHoldInput lifts a hold, with its reason.
type LiftRetentionHoldInput struct {
	Reason string `json:"reason" minLength:"3" maxLength:"500" doc:"Why it is lifted." example:"The dispute is settled"`
}

type (
	retentionHoldListOutput struct{ Body RetentionHoldList }
	// RetentionHoldPath names a hold (exported: huma reads only an exported embedded struct).
	RetentionHoldPath struct {
		HoldID string `path:"hold_id" format:"uuid" doc:"The hold." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	}
	retentionHoldsQuery struct {
		State string `query:"state" enum:"in_force,all" default:"in_force" doc:"in_force, the default, lists the holds that stand; all adds the lifted ones." example:"in_force"`
	}
	retentionHoldPlaceInput struct {
		KeyHeader
		PreferHeader
		Body RetentionHoldInput
	}
	retentionHoldLiftInput struct {
		RetentionHoldPath
		KeyHeader
		PreferHeader
		Body LiftRetentionHoldInput
	}
	retentionHoldOutput struct {
		Status   int
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     RetentionHoldView
	}
)

// retentionHoldView reads a hold, naming its subject from the records the caller may read.
func retentionHoldView(ctx context.Context, u storage.UnitOfWork, h storage.Hold) (RetentionHoldView, error) {
	v := RetentionHoldView{HoldID: h.ID.String(), Kind: HoldKind(h.Kind), Reason: h.Reason, PlacedBy: h.PlacedBy,
		PlacedAt: h.PlacedAt.UTC().Format(time.RFC3339), InForce: h.LiftedAt == nil, LiftedBy: h.LiftedBy, LiftReason: h.LiftReason}
	if h.LiftedAt != nil {
		v.LiftedAt = h.LiftedAt.UTC().Format(time.RFC3339)
	}
	if h.Year != nil {
		v.YearNumber, v.YearStartsOn, v.YearEndsOn = h.Year.Number, h.Year.StartsOn.Format(time.DateOnly), h.Year.EndsOn.Format(time.DateOnly)
	}
	if h.DocumentID != nil {
		v.DocumentID = h.DocumentID.String()
		if d, err := u.Documents().Get(ctx, *h.DocumentID); err == nil {
			v.DocumentType, v.DocumentNumber = string(d.Type), d.Number
		}
	}
	if h.ContactID != nil {
		v.ContactID = h.ContactID.String()
		if c, err := u.Contacts().Get(ctx, *h.ContactID); err == nil {
			v.ContactName = c.LegalName
		}
	}
	return v, nil
}

// tellHold tells those who read the holds that one was placed or lifted (AUD-11): the Owners, the DPO, the Controller and the Auditor.
func (a *API) tellHold(ctx context.Context, u storage.UnitOfWork, h storage.Hold, title string) error {
	if a.deps.Outbox == nil {
		return nil
	}
	p, _ := authn.From(ctx)
	_, err := notify.Emit(ctx, *a.deps.Outbox, u, p.Organisation, notify.Event{Type: notify.HoldType, RecordType: "retention_hold", RecordID: &h.ID, Title: title,
		Link: "/compliance/data-protection", Audience: notify.Audience{Capability: string(authz.RetentionHolds), Need: "V"}, At: a.deps.Clock.Now()})
	return err
}

func registerRetentionHolds(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid,
		storage.CodeHold}, locks...)
	register(a, authz.ListRetentionHolds, exempt(huma.Operation{
		OperationID: "listRetentionHolds", Method: http.MethodGet, Path: "/v1/compliance/holds", Tags: []string{"Compliance"},
		Summary: "List the legal holds on records",
		Description: "The holds the DPO and the Owners placed on a record, a contact or a financial year (AUD-11), the ones that stand by default, " +
			"each with its subject, reason and who placed it; state=all adds the lifted ones with who lifted them and why. Read-only; no side effects.",
	}, "cursor", "Holds are few and read whole: the page lists those in force, and a lifted one is rare."), locks,
		func(ctx context.Context, in *retentionHoldsQuery) (*retentionHoldListOutput, error) {
			p, _ := authn.From(ctx)
			out := &retentionHoldListOutput{Body: RetentionHoldList{Items: []RetentionHoldView{}}}
			err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				all, err := u.Retention().Holds(ctx, in.State != "all")
				if err != nil {
					return err
				}
				for _, h := range all {
					v, err := retentionHoldView(ctx, u, h)
					if err != nil {
						return err
					}
					out.Body.Items = append(out.Body.Items, v)
					if v.InForce {
						out.Body.InForce++
					}
				}
				return nil
			})
			return out, err
		})

	register(a, authz.PlaceRetentionHold, huma.Operation{
		OperationID: "placeRetentionHold", Method: http.MethodPost, Path: "/v1/compliance/holds", Tags: []string{"Compliance"},
		Summary: "Place a legal hold on a record, a contact or a financial year",
		Description: "The DPO or an Owner places a hold (AUD-11), with a reason, on one source document, a contact or a financial year. While it stands the purge " +
			"after retention skips what it covers, the database refuses to delete it, and the evidence files it covers keep a storage-level hold (a job sets it " +
			"within a day). A subject with a hold already is refused. Side effects: the hold, the audit event retention_hold.placed, and a notice to those who read the " +
			"holds. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append([]string{CodeNotFound}, writes...), func(ctx context.Context, in *retentionHoldPlaceInput) (*retentionHoldOutput, error) {
		now := a.deps.Clock.Now()
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (RetentionHoldView, error) {
			h := storage.NewHold{Kind: string(in.Body.Kind), Reason: strings.TrimSpace(in.Body.Reason), PlacedBy: uploader(ctx), PlacedAt: now}
			if len([]rune(h.Reason)) < 3 {
				return RetentionHoldView{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.reason", Message: "is too short", Fix: "Say why the records are held, in a few words"}})
			}
			var bad []FieldProblem
			subject := func(field, value string, others ...string) uuid.UUID {
				for _, o := range others {
					if o != "" {
						bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body." + field, Message: "is not for a hold of this kind", Fix: "Name only what the kind says"})
						return uuid.Nil
					}
				}
				id, err := uuid.Parse(value)
				if err != nil {
					bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body." + field, Message: "is not an ID", Fix: "Send the " + strings.ReplaceAll(field, "_id", "") + "'s ID"})
				}
				return id
			}
			switch in.Body.Kind {
			case "record":
				if in.Body.FinancialYear != 0 {
					bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.financial_year", Message: "is not for a hold of this kind", Fix: "Name only the document"})
				}
				h.DocumentID = subject("document_id", in.Body.DocumentID, in.Body.ContactID)
			case "contact":
				if in.Body.FinancialYear != 0 {
					bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.financial_year", Message: "is not for a hold of this kind", Fix: "Name only the contact"})
				}
				h.ContactID = subject("contact_id", in.Body.ContactID, in.Body.DocumentID)
			case "financial_year":
				if in.Body.DocumentID != "" || in.Body.ContactID != "" || in.Body.FinancialYear == 0 {
					bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.financial_year", Message: "is the only subject of a hold on a year",
						Fix: "Give the year's number, 1 for the first, and no document or contact"})
				}
			default:
				bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.kind", Message: "is not a kind of hold", Fix: "Use record, contact or financial_year"})
			}
			if len(bad) > 0 {
				return RetentionHoldView{}, invalid(bad)
			}
			switch h.Kind {
			case storage.HoldRecord:
				if _, err := u.Documents().Get(ctx, h.DocumentID); err != nil {
					return RetentionHoldView{}, problem(CodeNotFound, "There is no document "+h.DocumentID.String()+".", "Use the ID of a record's source document.")
				}
			case storage.HoldContact:
				if _, err := u.Contacts().Get(ctx, h.ContactID); err != nil {
					return RetentionHoldView{}, problem(CodeNotFound, "There is no contact "+h.ContactID.String()+".", "Use a contact's ID from the contacts list.")
				}
			case storage.HoldFinancialYear:
				o, err := u.Organisation(ctx)
				if err != nil {
					return RetentionHoldView{}, err
				}
				cal, err := period.CalendarOf(o.Profile)
				if err != nil {
					return RetentionHoldView{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.financial_year", Message: "has no calendar to read it against",
						Fix: "Enter when the books start and when the first financial year ends, in Settings › Organisation"}})
				}
				y := cal.Year(in.Body.FinancialYear)
				h.Year = &storage.HoldYear{Number: y.Number, StartsOn: y.StartsOn, EndsOn: y.EndsOn}
			}
			saved, err := u.Retention().PlaceHold(ctx, h)
			if err != nil {
				return RetentionHoldView{}, err
			}
			v, err := retentionHoldView(ctx, u, saved)
			if err != nil {
				return v, err
			}
			return v, a.tellHold(ctx, u, saved, "A legal hold was placed: "+holdWords(v)+". See it on Compliance › Data protection › Holds")
		})
		if err != nil {
			return nil, err
		}
		return &retentionHoldOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})

	register(a, authz.LiftRetentionHold, huma.Operation{
		OperationID: "liftRetentionHold", Method: http.MethodPost, Path: "/v1/compliance/holds/{hold_id}/lift", Tags: []string{"Compliance"},
		Summary: "Lift a legal hold",
		Description: "The DPO or an Owner, stepped up, lifts a hold (AUD-11), with a reason; a lifted hold never stands again, and a new one is placed to hold again. The purge after " +
			"retention may then reach what it covered, and the evidence files it alone covered are freed within a day. Side effects: the audit event retention_hold.lifted and " +
			"a notice to those who read the holds. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append([]string{CodeNotFound, CodeSessionRequired, CodeStepUpRequired}, writes...), func(ctx context.Context, in *retentionHoldLiftInput) (*retentionHoldOutput, error) {
		if _, err := a.steppedUpPerson(ctx, "Lifting a legal hold"); err != nil { // a lift can lead to deletion, so the person confirms it is them (p2-security-certification D3)
			return nil, err
		}
		now := a.deps.Clock.Now()
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (RetentionHoldView, error) {
			id, err := parseID("hold_id", in.HoldID)
			if err != nil {
				return RetentionHoldView{}, err
			}
			reason := strings.TrimSpace(in.Body.Reason)
			if len([]rune(reason)) < 3 {
				return RetentionHoldView{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.reason", Message: "is too short", Fix: "Say why the hold is lifted, in a few words"}})
			}
			saved, err := u.Retention().LiftHold(ctx, id, uploader(ctx), reason, now)
			if err != nil {
				if errors.Is(err, ports.ErrNotFound) {
					return RetentionHoldView{}, problem(CodeNotFound, "There is no hold "+in.HoldID+".", "Use a hold_id from listRetentionHolds.")
				}
				return RetentionHoldView{}, err
			}
			v, err := retentionHoldView(ctx, u, saved)
			if err != nil {
				return v, err
			}
			return v, a.tellHold(ctx, u, saved, "A legal hold was lifted: "+holdWords(v)+". See it on Compliance › Data protection › Holds")
		})
		if err != nil {
			return nil, err
		}
		return &retentionHoldOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})
}

// holdWords says what a hold names, for a notice's title.
func holdWords(v RetentionHoldView) string {
	switch v.Kind {
	case "record":
		return "a " + cmpOr(v.DocumentType, "record") + " " + v.DocumentNumber
	case "contact":
		return "the contact " + v.ContactName
	}
	return "financial year " + v.YearStartsOn + " to " + v.YearEndsOn
}
