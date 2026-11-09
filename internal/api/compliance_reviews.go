package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/compliance"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	controls "github.com/cognizhi/fyniq/internal/domain/compliance"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ComplianceReview is a recurring review people sign off, as it stands for the organisation (CMP-03, design D6).
type ComplianceReview struct {
	Kind        string             `json:"kind" enum:"review-log,access-review,accountant-review,close-signoff,break-glass-review,policy-acknowledgement,risk-review,data-inventory" doc:"The review." example:"access-review"`
	Title       string             `json:"title" doc:"What it is, in a sentence." example:"An Owner attests the access review (each quarter)"`
	Owner       string             `json:"owner" enum:"owner,controller,accountant,dpo,member" doc:"The role that signs it off; member when each member signs for themselves." example:"owner"`
	Cadence     string             `json:"cadence" enum:"weekly,quarterly,monthly,each use,each version,by review date" doc:"How often it falls due." example:"quarterly"`
	Status      string             `json:"status" enum:"signed,due,overdue,nothing_due,not_applicable" doc:"overdue: something waits past its time; due: something waits within it; signed: nothing waits and it was signed off before; nothing_due: nothing waits and nothing was ever signed off; not_applicable: the team mode does not call for it." example:"due"`
	Period      string             `json:"period,omitempty" doc:"The period that waits, as people say it." example:"Q3 2026"`
	DueOn       string             `json:"due_on,omitempty" format:"date" doc:"When the earliest waiting item falls due; for the risk reviews, the next review date." example:"2026-10-14"`
	Waiting     int                `json:"waiting" doc:"How many items wait for their sign-off." example:"1"`
	Overdue     int                `json:"overdue" doc:"How many of them are past their time." example:"0"`
	LastSignOff *ComplianceSignOff `json:"last_sign_off,omitempty" doc:"The latest sign-off; none before the first."`
	Detail      string             `json:"detail" doc:"What waits, or that nothing does, in a sentence." example:"The access review of Q3 2026 waits for an Owner's attestation, due by 14 Oct 2026."`
	Where       string             `json:"where" doc:"The web app's page where it is signed off." example:"/settings/access-review"`
	Evidence    string             `json:"evidence" doc:"What each sign-off keeps." example:"Each quarter's attestation, with the report attested and its SHA-256, and a CSV of it"`
	Controls    []ControlRef       `json:"controls" nullable:"false" doc:"The controls whose attestation check reads it."`
}

// ControlRef names a control of the registry.
type ControlRef string

// Schema implements huma.SchemaProvider: a control's ID, with an example.
func (ControlRef) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Pattern: "^CTL-[A-Z]+-[0-9]{2}$", Description: "A control's ID, as listControls names it.", Examples: []any{"CTL-IAM-02"}}
}

// ComplianceSignOff is a review's latest sign-off.
type ComplianceSignOff struct {
	By     string `json:"by" doc:"Who signed off, as records name actors." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	ByName string `json:"by_name,omitempty" doc:"Their name, while they are a member." example:"Ada Tan"`
	At     string `json:"at" format:"date-time" doc:"When." example:"2026-10-03T02:00:00Z"`
}

// ComplianceReviews are the organisation's recurring reviews.
type ComplianceReviews struct {
	Items []ComplianceReview `json:"items" nullable:"false" doc:"The recurring reviews, in the Compliance Center's order."`
}

type complianceReviewsOutput struct{ Body ComplianceReviews }

func registerComplianceReviews(a *API) {
	register(a, authz.ListComplianceReviews, exempt(huma.Operation{
		OperationID: "listComplianceReviews", Method: http.MethodGet, Path: "/v1/compliance/reviews", Tags: []string{"Compliance"},
		Summary: "List the recurring reviews and attestations",
		Description: "Every recurring review people sign off (CMP-03): the review log (weekly, Founder mode), the access review (each " +
			"quarter), the accountant's review (each quarter, Founder mode), the close's sign-off (monthly), each break-glass's " +
			"post-review, the policy acknowledgements, the risk reviews and the personal-data inventory's quarterly review. Each " +
			"comes with its owner role, what waits and when it falls due, how much is overdue under its own rule, its latest sign-off, " +
			"its evidence, the page where it is signed off, and the controls whose attestation check reads it. Read-only; no side effects.",
	}, "cursor", "The recurring reviews, eight of them, read whole: one summary per kind, each worked out from its own records."),
		[]string{storage.CodeRetry, storage.CodeLockTimeout}, func(ctx context.Context, _ *struct{}) (*complianceReviewsOutput, error) {
			p, _ := authn.From(ctx)
			now := a.deps.Clock.Now()
			var reviews []controls.Review
			var names map[uuid.UUID]string
			err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				var err error
				if reviews, err = controls.Reviews(ctx, u, now); err != nil {
					return err
				}
				names, err = memberNames(ctx, u, now)
				return err
			})
			if err != nil {
				return nil, err
			}
			by, err := attestedBy()
			if err != nil {
				return nil, err
			}
			out := &complianceReviewsOutput{Body: ComplianceReviews{Items: make([]ComplianceReview, 0, len(reviews))}}
			for _, r := range reviews {
				v := ComplianceReview{Kind: r.Kind, Title: compliance.Attestations[r.Kind], Owner: r.Owner, Cadence: r.Cadence, Status: r.Status,
					Period: r.Period, Waiting: r.Waiting, Overdue: r.Overdue, Detail: r.Detail, Where: r.Where, Evidence: r.Evidence,
					Controls: by[r.Kind]}
				if v.Controls == nil {
					v.Controls = []ControlRef{}
				}
				if r.DueOn != nil {
					v.DueOn = r.DueOn.Format(time.DateOnly)
				}
				if r.LastAt != nil {
					v.LastSignOff = &ComplianceSignOff{By: r.LastBy, At: r.LastAt.UTC().Format(time.RFC3339)}
					if id, err := uuid.Parse(strings.TrimPrefix(r.LastBy, "user:")); err == nil {
						v.LastSignOff.ByName = names[id]
					}
				}
				out.Body.Items = append(out.Body.Items, v)
			}
			return out, nil
		})
}

// attestedBy are the controls whose attestation checks read each review, by kind.
func attestedBy() (map[string][]ControlRef, error) {
	registry, err := compliance.Load()
	if err != nil {
		return nil, err
	}
	out := map[string][]ControlRef{}
	for _, c := range registry {
		for _, ch := range c.Checks {
			if kind, name := ch.Kind(); kind == compliance.KindAttestation && ch.Status == "running" {
				out[name] = append(out[name], ControlRef(c.ID))
			}
		}
	}
	return out, nil
}
