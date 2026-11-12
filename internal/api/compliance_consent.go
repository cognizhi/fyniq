package api

import (
	"context"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ConsentPurposeView is a purpose of the organisation's, with whether it needs consent (CMP-PD-03, rev 0.244).
type ConsentPurposeView struct {
	ID           string `json:"id" format:"uuid" doc:"The purpose's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	Name         string `json:"name" doc:"What the data is used for, in a few words." example:"Newsletter"`
	Description  string `json:"description" doc:"What it means for the person." example:"A monthly email about our products, to customers' contacts who ask for it."`
	Audience     string `json:"audience" enum:"members,contacts" doc:"Whose data it uses: the organisation's members, or its contacts." example:"contacts"`
	NeedsConsent bool   `json:"needs_consent" doc:"Whether the purpose needs each person's consent; one that does is used only for those whose consent stands." example:"true"`
	CreatedBy    string `json:"created_by" doc:"Who added it, as records name actors." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	CreatedAt    string `json:"created_at" format:"date-time" doc:"When." example:"2026-11-09T02:00:00Z"`
	Given        int    `json:"given" doc:"How many people's consent stands for it now." example:"12"`
}

// ConsentPurposeInput adds a purpose.
type ConsentPurposeInput struct {
	Name         string `json:"name" minLength:"1" maxLength:"200" doc:"What the data is used for, in a few words." example:"Newsletter"`
	Description  string `json:"description" minLength:"1" maxLength:"2000" doc:"What it means for the person." example:"A monthly email about our products, to customers' contacts who ask for it."`
	Audience     string `json:"audience" enum:"members,contacts" doc:"Whose data it uses." example:"contacts"`
	NeedsConsent bool   `json:"needs_consent" doc:"Whether it needs each person's consent." example:"true"`
}

// ConsentView is a consent given or withdrawn.
type ConsentView struct {
	ID         string `json:"id" format:"uuid" doc:"The record's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e11"`
	PurposeID  string `json:"purpose_id" format:"uuid" doc:"The purpose." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	Person     string `json:"person" doc:"Whose consent: member:<user id> or contact:<contact id>." example:"contact:01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e12"`
	Event      string `json:"event" enum:"given,withdrawn" doc:"Given, or withdrawn." example:"given"`
	At         string `json:"at" format:"date-time" doc:"When the person gave or withdrew it, as recorded." example:"2026-11-09T02:00:00Z"`
	RecordedBy string `json:"recorded_by" doc:"Who recorded it, as records name actors." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	Note       string `json:"note,omitempty" doc:"How it was given or withdrawn, such as the email it came in." example:"Asked by email on 8 Nov 2026."`
}

// ConsentInput records a consent given or withdrawn.
type ConsentInput struct {
	PurposeID string `json:"purpose_id" format:"uuid" doc:"The purpose, from listConsentPurposes." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	Person    string `json:"person" pattern:"^(member|contact):[0-9a-f-]{36}$" doc:"Whose consent: member:<user id> for a purpose for members, contact:<contact id> for one for contacts." example:"contact:01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e12"`
	Event     string `json:"event" enum:"given,withdrawn" doc:"given: a consent the person gave; withdrawn: one they withdrew." example:"given"`
	Note      string `json:"note,omitempty" maxLength:"2000" doc:"How it was given or withdrawn." example:"Asked by email on 8 Nov 2026."`
}

type (
	consentPurposesOutput struct {
		Body struct {
			Items []ConsentPurposeView `json:"items" nullable:"false" doc:"The organisation's purposes, oldest first. None is defined by default."`
		}
	}
	consentPurposeInput struct {
		KeyHeader
		PreferHeader
		Body ConsentPurposeInput
	}
	consentPurposeOutput struct {
		Status   int
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     ConsentPurposeView
	}
	consentsQuery struct {
		PurposeID string `query:"purpose_id" doc:"Only this purpose's records." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
		Person    string `query:"person" doc:"Only this person's records." example:"contact:01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e12"`
	}
	consentsOutput struct {
		Body struct {
			Items []ConsentView `json:"items" nullable:"false" doc:"Every consent given or withdrawn, oldest first."`
		}
	}
	consentInput struct {
		KeyHeader
		PreferHeader
		Body ConsentInput
	}
	consentOutput struct {
		Status   int
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     ConsentView
	}
)

var personRef = regexp.MustCompile(`^(member|contact):[0-9a-f-]{36}$`)

func consentPurposeView(p storage.ConsentPurpose, records []storage.ConsentRecord) ConsentPurposeView {
	last := map[string]string{}
	for _, r := range records { // oldest first: the last stands
		if r.PurposeID == p.ID {
			last[r.Person] = r.Event
		}
	}
	given := 0
	for _, e := range last {
		if e == "given" {
			given++
		}
	}
	return ConsentPurposeView{ID: p.ID.String(), Name: p.Name, Description: p.Description, Audience: p.Audience, NeedsConsent: p.NeedsConsent,
		CreatedBy: p.CreatedBy, CreatedAt: p.CreatedAt.UTC().Format(time.RFC3339), Given: given}
}

func consentView(r storage.ConsentRecord) ConsentView {
	return ConsentView{ID: r.ID.String(), PurposeID: r.PurposeID.String(), Person: r.Person, Event: r.Event, At: r.At.UTC().Format(time.RFC3339),
		RecordedBy: r.RecordedBy, Note: r.Note}
}

func registerConsent(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid,
		storage.CodeConsent}, locks...)
	register(a, authz.ListConsentPurposes, exempt(huma.Operation{
		OperationID: "listConsentPurposes", Method: http.MethodGet, Path: "/v1/compliance/consent-purposes", Tags: []string{"Compliance"},
		Summary: "List the consent purposes",
		Description: "The purposes the organisation defined for members' or contacts' data, each saying whether it needs consent and how " +
			"many people's consent stands for it (CMP-PD-03). None is defined by default: Fyniq uses the data for the work, the " +
			"business relationship and the law's records. Read-only; no side effects.",
	}, "cursor", "An organisation defines a handful of purposes at most, read whole."), locks,
		func(ctx context.Context, _ *struct{}) (*consentPurposesOutput, error) {
			p, _ := authn.From(ctx)
			out := &consentPurposesOutput{}
			out.Body.Items = []ConsentPurposeView{}
			err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				ps, err := u.Compliance().ConsentPurposes(ctx)
				if err != nil {
					return err
				}
				rs, err := u.Compliance().Consents(ctx)
				for _, x := range ps {
					out.Body.Items = append(out.Body.Items, consentPurposeView(x, rs))
				}
				return err
			})
			return out, err
		})

	register(a, authz.CreateConsentPurpose, huma.Operation{
		OperationID: "createConsentPurpose", Method: http.MethodPost, Path: "/v1/compliance/consent-purposes", Tags: []string{"Compliance"},
		Summary:     "Add a consent purpose",
		Description: "Adds a purpose for members' or contacts' data, saying whether it needs consent (CMP-PD-03). A purpose never changes; add another instead. Side effects: the purpose, the audit event consent_purpose.created. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, writes, func(ctx context.Context, in *consentPurposeInput) (*consentPurposeOutput, error) {
		now := a.deps.Clock.Now()
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (ConsentPurposeView, error) {
			name, desc := strings.TrimSpace(in.Body.Name), strings.TrimSpace(in.Body.Description)
			var bad []FieldProblem
			if name == "" {
				bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.name", Message: "is empty", Fix: "Say what the data is used for"})
			}
			if desc == "" {
				bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.description", Message: "is empty", Fix: "Say what it means for the person"})
			}
			if len(bad) > 0 {
				return ConsentPurposeView{}, invalid(bad)
			}
			p, err := u.Compliance().CreateConsentPurpose(ctx, storage.ConsentPurpose{Name: name, Description: desc, Audience: in.Body.Audience,
				NeedsConsent: in.Body.NeedsConsent, CreatedBy: uploader(ctx), CreatedAt: now})
			return consentPurposeView(p, nil), err
		})
		if err != nil {
			return nil, err
		}
		return &consentPurposeOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})

	register(a, authz.ListConsents, exempt(huma.Operation{
		OperationID: "listConsents", Method: http.MethodGet, Path: "/v1/compliance/consents", Tags: []string{"Compliance"},
		Summary:     "List the consents given and withdrawn",
		Description: "Every consent given or withdrawn for the organisation's purposes, oldest first, by purpose or person (CMP-PD-03). A person's last record says whether their consent stands. Read-only; no side effects.",
	}, "cursor", "Consents exist only for the few purposes that need them, read whole for a purpose or a person."), append([]string{CodeInvalid}, locks...),
		func(ctx context.Context, in *consentsQuery) (*consentsOutput, error) {
			p, _ := authn.From(ctx)
			out := &consentsOutput{}
			out.Body.Items = []ConsentView{}
			err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				rs, err := u.Compliance().Consents(ctx)
				for _, r := range rs {
					if (in.PurposeID == "" || r.PurposeID.String() == in.PurposeID) && (in.Person == "" || r.Person == in.Person) {
						out.Body.Items = append(out.Body.Items, consentView(r))
					}
				}
				return err
			})
			return out, err
		})

	register(a, authz.RecordConsent, huma.Operation{
		OperationID: "recordConsent", Method: http.MethodPost, Path: "/v1/compliance/consents", Tags: []string{"Compliance"},
		Summary:     "Record a consent given or withdrawn",
		Description: "Records that a person gave or withdrew their consent for a purpose that needs it (CMP-PD-03). A consent is given only when none stands for that person and purpose, and withdrawn only when one does; withdrawal stops further use. Side effects: the record, the audit event consent.given or consent.withdrawn. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, writes, func(ctx context.Context, in *consentInput) (*consentOutput, error) {
		now := a.deps.Clock.Now()
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (ConsentView, error) {
			ps, err := u.Compliance().ConsentPurposes(ctx)
			if err != nil {
				return ConsentView{}, err
			}
			i := slices.IndexFunc(ps, func(p storage.ConsentPurpose) bool { return p.ID.String() == in.Body.PurposeID })
			var bad []FieldProblem
			switch {
			case i < 0:
				bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.purpose_id", Message: "is not a purpose of the organisation", Fix: "Use an ID from listConsentPurposes"})
			case !ps[i].NeedsConsent:
				bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.purpose_id", Message: "does not need consent", Fix: "Record consent only for a purpose that needs it"})
			case !personRef.MatchString(in.Body.Person) || strings.HasPrefix(in.Body.Person, "member:") != (ps[i].Audience == "members"):
				bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.person", Message: "is not someone the purpose is for",
					Fix: "Name a member:<user id> for a purpose for members, a contact:<contact id> for one for contacts"})
			}
			if len(bad) > 0 {
				return ConsentView{}, invalid(bad)
			}
			r, err := u.Compliance().RecordConsent(ctx, storage.ConsentRecord{PurposeID: ps[i].ID, Person: in.Body.Person, Event: in.Body.Event, At: now,
				RecordedBy: uploader(ctx), Note: strings.TrimSpace(in.Body.Note)})
			return consentView(r), err
		})
		if err != nil {
			return nil, err
		}
		return &consentOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})
}
