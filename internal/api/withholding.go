package api

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// WithholdingDecisionDraft is the tax agent's decision to record for a non-resident supplier and a nature of payment
// (FR-AP-19).
type WithholdingDecisionDraft struct {
	Nature      string `json:"nature_of_payment" enum:"goods,software_use,software_rights,services_in_singapore,services_outside_singapore,interest,movable_rent,other" doc:"The nature of payment it is for." example:"services_in_singapore"`
	Outcome     string `json:"outcome" enum:"not_applicable,withhold,treaty" doc:"not_applicable: nothing is withheld (ordinary remote SaaS, for example); withhold: at the rate the tax agent advised; treaty: a reduced treaty rate, with the certificate of residence attached to the bill." example:"withhold"`
	RatePercent string `json:"rate_percent,omitempty" pattern:"^(100(\\.0{1,2})?|[0-9]{1,2}(\\.[0-9]{1,2})?)$" doc:"For withhold and treaty: the rate the tax agent advised, as a percentage of the payment before GST, above 0 and up to 100. Left out for not_applicable." example:"15"`
	TaxAgent    string `json:"tax_agent" minLength:"1" maxLength:"200" doc:"The tax agent who gave the decision." example:"Grace Ho, Ho & Partners"`
	Note        string `json:"note,omitempty" maxLength:"2000" doc:"Why, in the tax agent's words." example:"Non-resident professional working on site in Singapore: 15% of gross"`
	BillID      string `json:"bill_id,omitempty" format:"uuid" doc:"The supplier's bill it is recorded from: attach the tax agent's advice (and a certificate of residence) to it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e30"`
}

// WithholdingDecisionPage is a page of a supplier's decisions, newest first.
type WithholdingDecisionPage struct {
	Items      []WithholdingDecision `json:"items" nullable:"false" doc:"The decisions, newest first: the newest for a nature is in force."`
	NextCursor string                `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjAxOTI4ZjZlIn0"`
}

type withholdingListInput struct {
	ContactPath
	PageQuery
}

type withholdingListOutput struct {
	Body WithholdingDecisionPage
}

type withholdingRecordInput struct {
	ContactPath
	IfMatchHeader
	KeyHeader
	PreferHeader
	Body WithholdingDecisionDraft
}

type withholdingRecordOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     WithholdingDecision
}

func registerWithholding(a *API) {
	codes := []string{storage.CodeRetry, storage.CodeLockTimeout, CodeNotFound}
	// named fills in who recorded each decision.
	named := func(ctx context.Context, u storage.UnitOfWork, ds []WithholdingDecision) error {
		people := make([]*Person, 0, len(ds))
		for i := range ds {
			people = append(people, ds[i].RecordedBy)
		}
		return a.name(ctx, u, people...)
	}

	register(a, authz.ListWithholdingDecisions, huma.Operation{
		OperationID: "listWithholdingDecisions", Method: http.MethodGet, Path: "/v1/contacts/{contact_id}/withholding-decisions", Tags: []string{"Purchases"},
		Summary:     "List a supplier's withholding decisions",
		Description: "The tax agent's decisions on whether tax is withheld from a non-resident supplier's payments, one per nature of payment in force at a time, newest first, a page at a time (FR-AP-19). A bill of the supplier posts with the decision in force for its nature when it posts. Read-only; no side effects.",
	}, append([]string{CodeCursor}, codes...), func(ctx context.Context, in *withholdingListInput) (*withholdingListOutput, error) {
		after := ""
		if in.Cursor != "" {
			var err error
			if after, err = decodeCursor(in.Cursor); err != nil {
				return nil, err
			}
		}
		id, err := parseID("contact_id", in.ContactID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		out := withholdingListOutput{Body: WithholdingDecisionPage{Items: []WithholdingDecision{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			if _, err := u.Contacts().Get(ctx, id); err != nil {
				return err
			}
			ds, err := u.WithholdingDecisions().Of(ctx, id)
			if err != nil {
				return err
			}
			if after != "" { // the page after the decision the cursor names, in the same order
				i := slices.IndexFunc(ds, func(d storage.WithholdingDecision) bool { return d.ID.String() == after })
				if i < 0 {
					return badCursor()
				}
				ds = ds[i+1:]
			}
			if len(ds) > in.PageSize {
				ds = ds[:in.PageSize]
				out.Body.NextCursor = encodeCursor(ds[len(ds)-1].ID.String())
			}
			for _, d := range ds {
				out.Body.Items = append(out.Body.Items, withholdingDecisionOf(d))
			}
			return named(ctx, u, out.Body.Items)
		})
		return &out, err
	})

	register(a, authz.RecordWithholdingDecision, huma.Operation{
		OperationID: "recordWithholdingDecision", Method: http.MethodPost, Path: "/v1/contacts/{contact_id}/withholding-decisions", Tags: []string{"Purchases"},
		DefaultStatus: http.StatusCreated, Summary: "Record the tax agent's withholding decision",
		Description: "Records the tax agent's decision for a supplier resident outside Singapore and a nature of payment (FR-AP-19): not " +
			"applicable, withhold at the rate they advised, or a reduced treaty rate with a certificate of residence. It replaces the " +
			"supplier's earlier decision for the nature from now: bills posted before keep theirs, and a posted bill whose rate it " +
			"changes waits for an edit before it is paid (BILLPAY-11). Attach the tax agent's advice to the bill named. A decision is " +
			"never changed or removed (FY174). If-Match is the supplier's ETag, so the decision is recorded for the tax residence " +
			"that was read. Side effects: the decision and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview " +
			"checks it and saves nothing.",
	}, append([]string{CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		bill.CodeWithholding}, codes...),
		func(ctx context.Context, in *withholdingRecordInput) (*withholdingRecordOutput, error) {
			id, err := parseID("contact_id", in.ContactID)
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (WithholdingDecision, error) {
				c, err := u.Contacts().Get(ctx, id)
				if err != nil {
					return WithholdingDecision{}, err
				}
				if err := checkIfMatch(in.IfMatch, etag(c.Version)); err != nil {
					return WithholdingDecision{}, err
				}
				b := in.Body
				d := storage.NewWithholdingDecision{SupplierID: id, Nature: b.Nature, Outcome: b.Outcome, RatePercent: strings.TrimSpace(b.RatePercent),
					TaxAgent: b.TaxAgent, Note: b.Note}
				if b.BillID != "" {
					bid, err := uuid.Parse(b.BillID)
					if err != nil {
						return WithholdingDecision{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.bill_id", Message: "is not an ID", Fix: "Send the bill's ID"}})
					}
					d.BillID = &bid
				}
				o, err := u.Organisation(ctx)
				if err != nil {
					return WithholdingDecision{}, err
				}
				base, err := money.Lookup(o.BaseCurrency)
				if err != nil {
					return WithholdingDecision{}, err
				}
				rec, err := bill.RecordWithholding(ctx, u, d, base)
				if err != nil {
					return WithholdingDecision{}, err
				}
				out := []WithholdingDecision{withholdingDecisionOf(rec)}
				return out[0], named(ctx, u, out)
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &withholdingRecordOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &withholdingRecordOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})
}
