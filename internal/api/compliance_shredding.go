package api

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ShreddingRunView is a financial year's person keys to shred once its retention has ended (§11.8, rev 0.244).
type ShreddingRunView struct {
	ID         string `json:"id" format:"uuid" doc:"The run." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	Cohort     string `json:"cohort" format:"date" doc:"The last day of the financial year whose personal values it shreds." example:"2019-12-31"`
	People     int    `json:"people" doc:"How many people's keys of that year it shreds: one key per person." example:"14"`
	ListedAt   string `json:"listed_at" format:"date-time" doc:"When the job listed it, the year's retention having ended." example:"2027-01-02T17:50:00Z"`
	Status     string `json:"status" enum:"listed,approved,done" doc:"listed: waiting for the DPO; approved: shredded on the next daily run, register first; done: the keys are gone." example:"listed"`
	ApprovedBy string `json:"approved_by,omitempty" doc:"The DPO who approved it, as records name actors." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	ApprovedAt string `json:"approved_at,omitempty" format:"date-time" doc:"When." example:"2027-01-03T02:00:00Z"`
	Register   string `json:"register,omitempty" doc:"The signed register entry, written before the keys went." example:"pdpa-shredding/01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10/01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e11.json"`
	DoneAt     string `json:"done_at,omitempty" format:"date-time" doc:"When its keys went." example:"2027-01-03T17:50:00Z"`
	Reapplied  bool   `json:"reapplied" doc:"true when a restore recreated it from the register." example:"false"`
	Version    int    `json:"version" doc:"Its version; the ETag carries it for If-Match." example:"1"`
}

// AnonymisationView is a contact's personal details replaced (§11.9, rev 0.244).
type AnonymisationView struct {
	ID           string `json:"id" format:"uuid" doc:"The anonymisation." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	ContactID    string `json:"contact_id" format:"uuid" doc:"The contact." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e12"`
	ContactName  string `json:"contact_name" doc:"Their legal name, which the records keep." example:"Ada Tan"`
	RequestID    string `json:"request_id,omitempty" format:"uuid" doc:"The request it honours, if any." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e13"`
	Reason       string `json:"reason" doc:"Why." example:"Their purpose ended: no business since 2022."`
	AnonymisedBy string `json:"anonymised_by" doc:"Who, as records name actors." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	AnonymisedAt string `json:"anonymised_at" format:"date-time" doc:"When." example:"2026-11-10T02:00:00Z"`
	Reapplied    bool   `json:"reapplied" doc:"true when a restore applied it again from the register." example:"false"`
}

type (
	shreddingRunsOutput struct {
		Body struct {
			Items []ShreddingRunView `json:"items" nullable:"false" doc:"The runs, by financial year."`
		}
	}
	// ShreddingRunPath names a run (exported: huma reads only an exported embedded struct).
	ShreddingRunPath struct {
		RunID string `path:"run_id" format:"uuid" doc:"The run." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	}
	shreddingApproveInput struct {
		ShreddingRunPath
		KeyHeader
		IfMatchHeader
		PreferHeader
	}
	shreddingRunOutput struct {
		Status   int
		ETag     string `header:"ETag" doc:"The run's version, for If-Match."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     ShreddingRunView
	}
	anonymisationsOutput struct {
		Body struct {
			Items []AnonymisationView `json:"items" nullable:"false" doc:"The anonymisations, oldest first."`
		}
	}
	anonymiseInput struct {
		KeyHeader
		PreferHeader
		Body struct {
			ContactID string `json:"contact_id" format:"uuid" doc:"The contact: a director, or one marked as a person." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e12"`
			RequestID string `json:"request_id,omitempty" format:"uuid" doc:"The access or correction request it honours, if any." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e13"`
			Reason    string `json:"reason" minLength:"1" maxLength:"1000" doc:"Why: the request honoured, or the purpose that ended." example:"Their purpose ended: no business since 2022."`
		}
	}
	anonymisationOutput struct {
		Status   int
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     AnonymisationView
	}
)

func shreddingRunView(r storage.ShreddingRun) ShreddingRunView {
	v := ShreddingRunView{ID: r.ID.String(), Cohort: r.Cohort.Format(time.DateOnly), People: len(r.Keys), ListedAt: r.ListedAt.UTC().Format(time.RFC3339),
		Status: r.Status, ApprovedBy: r.ApprovedBy, Register: r.Register, Reapplied: r.Reapplied, Version: r.Version}
	if r.ApprovedAt != nil {
		v.ApprovedAt = r.ApprovedAt.UTC().Format(time.RFC3339)
	}
	if r.DoneAt != nil {
		v.DoneAt = r.DoneAt.UTC().Format(time.RFC3339)
	}
	return v
}

func shreddingETag(version int) string { return etag("v" + strconv.Itoa(version)) }

func registerShredding(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid,
		storage.CodeShredding}, locks...)
	register(a, authz.ListShreddingRuns, exempt(huma.Operation{
		OperationID: "listShreddingRuns", Method: http.MethodGet, Path: "/v1/compliance/shredding-runs", Tags: []string{"Compliance"},
		Summary: "List the shredding runs",
		Description: "The shredding of personal data at the end of retention (§11.8): each financial year whose records' retention " +
			"has ended, with how many people's keys it shreds, and whether it waits for the DPO, is approved, or is done, with its " +
			"signed register entry. Read-only; no side effects.",
	}, "cursor", "One run per financial year: a handful over the life of an organisation, read whole."), locks,
		func(ctx context.Context, _ *struct{}) (*shreddingRunsOutput, error) {
			p, _ := authn.From(ctx)
			out := &shreddingRunsOutput{}
			out.Body.Items = []ShreddingRunView{}
			err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				runs, err := u.Compliance().ShreddingRuns(ctx)
				for _, r := range runs {
					out.Body.Items = append(out.Body.Items, shreddingRunView(r))
				}
				return err
			})
			return out, err
		})

	register(a, authz.ApproveShreddingRun, huma.Operation{
		OperationID: "approveShreddingRun", Method: http.MethodPost, Path: "/v1/compliance/shredding-runs/{run_id}/approval", Tags: []string{"Compliance"},
		Summary:     "Approve a shredding run",
		Description: "The DPO approves the shredding of a financial year's person keys, its retention having ended, stepped up (§11.8). The next daily run writes the signed register entry, then deletes the keys: the values they sealed in the audit trail no longer open, and the events and the chain stay. It cannot be undone. Side effects: the approval, the audit event pdpa.shredding_approved. Needs If-Match with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeSessionRequired, CodeStepUpRequired}, writes...),
		func(ctx context.Context, in *shreddingApproveInput) (*shreddingRunOutput, error) {
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			if _, err := a.steppedUpPerson(ctx, "Approving a shredding run"); err != nil {
				return nil, err
			}
			now := a.deps.Clock.Now()
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (ShreddingRunView, error) {
				runs, err := u.Compliance().ShreddingRuns(ctx)
				if err != nil {
					return ShreddingRunView{}, err
				}
				i := slices.IndexFunc(runs, func(r storage.ShreddingRun) bool { return r.ID.String() == in.RunID })
				if i < 0 {
					return ShreddingRunView{}, problem(CodeNotFound, "There is no shredding run "+in.RunID+".", "Use an id from listShreddingRuns.")
				}
				if err := checkIfMatch(in.IfMatch, shreddingETag(runs[i].Version)); err != nil {
					return ShreddingRunView{}, err
				}
				r, err := u.Compliance().ApproveShreddingRun(ctx, runs[i].ID, runs[i].Version, uploader(ctx), now)
				return shreddingRunView(r), err
			})
			if err != nil {
				return nil, err
			}
			return &shreddingRunOutput{Status: w.Status, ETag: shreddingETag(w.Body.Version), Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})

	register(a, authz.ListAnonymisations, exempt(huma.Operation{
		OperationID: "listAnonymisations", Method: http.MethodGet, Path: "/v1/compliance/anonymisations", Tags: []string{"Compliance"},
		Summary:     "List the anonymised contacts",
		Description: "Each contact whose personal details were replaced (§11.9), with why, by whom and when. Read-only; no side effects.",
	}, "cursor", "Anonymisations are few, a register read whole beside the shredding runs."), locks,
		func(ctx context.Context, _ *struct{}) (*anonymisationsOutput, error) {
			p, _ := authn.From(ctx)
			out := &anonymisationsOutput{}
			out.Body.Items = []AnonymisationView{}
			err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				as, err := u.Compliance().Anonymisations(ctx)
				if err != nil {
					return err
				}
				for _, x := range as {
					c, err := u.Contacts().Get(ctx, x.ContactID)
					if err != nil {
						return err
					}
					out.Body.Items = append(out.Body.Items, anonymisationView(x, c.LegalName))
				}
				return nil
			})
			return out, err
		})

	register(a, authz.AnonymiseContact, huma.Operation{
		OperationID: "anonymiseContact", Method: http.MethodPost, Path: "/v1/compliance/anonymisations", Tags: []string{"Compliance"},
		Summary:     "Anonymise a contact",
		Description: "The DPO replaces a person's contact details the records do not need, stepped up, when a request is honoured or their purpose ends (§11.9): their emails and phone go and their address reads Anonymised; their legal name and every issued document stay. The signed register entry is written first, so a restore anonymises them again. It cannot be undone. Side effects: the anonymisation, the audit events pd_anonymisation.recorded and contact.anonymised. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append([]string{CodeNotFound, CodeSessionRequired, CodeStepUpRequired, CodeUnavailable}, writes...),
		func(ctx context.Context, in *anonymiseInput) (*anonymisationOutput, error) {
			if _, err := a.steppedUpPerson(ctx, "Anonymising a contact"); err != nil {
				return nil, err
			}
			if a.deps.Anchors == nil || a.deps.Signer == nil {
				return nil, problem(CodeUnavailable, "The anonymisation register is not available.", "Try again later; the deployment's register bucket is down.")
			}
			p, _ := authn.From(ctx)
			now := a.deps.Clock.Now()
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (AnonymisationView, error) {
				id, err := parseID("body.contact_id", in.Body.ContactID)
				if err != nil {
					return AnonymisationView{}, err
				}
				var request *uuid.UUID
				if in.Body.RequestID != "" {
					r, err := parseID("body.request_id", in.Body.RequestID)
					if err != nil {
						return AnonymisationView{}, err
					}
					request = &r
				}
				c, err := u.Contacts().Get(ctx, id)
				if err != nil {
					return AnonymisationView{}, err
				}
				if !c.Individual && !c.Director {
					return AnonymisationView{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.contact_id", Message: "is a company, not a person",
						Fix: "Anonymise a director, or a contact marked as a person"}})
				}
				reason := strings.TrimSpace(in.Body.Reason)
				if reason == "" {
					return AnonymisationView{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.reason", Message: "is empty", Fix: "Say why: the request honoured, or the purpose that ended"}})
				}
				register := audit.AnonymisationKey(p.Organisation, uuid.Nil) // a preview writes no entry
				if !wantsPreview(in.Prefer) {
					// The register first: a restore re-applies it, so an anonymisation is never lost to a backup.
					entry, err := uuid.NewV7()
					if err != nil {
						return AnonymisationView{}, err
					}
					register, err = audit.RecordAnonymisation(ctx, a.deps.Anchors, a.deps.Signer, audit.Anonymisation{OrganisationID: p.Organisation,
						EntryID: entry, ContactID: id, RequestID: request, AnonymisedBy: uploader(ctx), AnonymisedAt: now.UTC().Format(time.RFC3339Nano),
						WrittenAt: now.UTC().Format(time.RFC3339Nano)})
					if err != nil {
						return AnonymisationView{}, err
					}
				}
				x, err := u.Compliance().Anonymise(ctx, storage.Anonymisation{ContactID: id, RequestID: request, Reason: reason, Register: register,
					AnonymisedBy: uploader(ctx), AnonymisedAt: now})
				return anonymisationView(x, c.LegalName), err
			})
			if err != nil {
				return nil, err
			}
			return &anonymisationOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})
}

func anonymisationView(x storage.Anonymisation, name string) AnonymisationView {
	v := AnonymisationView{ID: x.ID.String(), ContactID: x.ContactID.String(), ContactName: name, Reason: x.Reason, AnonymisedBy: x.AnonymisedBy,
		AnonymisedAt: x.AnonymisedAt.UTC().Format(time.RFC3339), Reapplied: x.Reapplied}
	if x.RequestID != nil {
		v.RequestID = x.RequestID.String()
	}
	return v
}
