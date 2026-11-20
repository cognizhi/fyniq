package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/invoicenow"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ InvoiceNow (FR-AR-12, FR-AP-05, FR-TAX-10, FR-TAX-11)
//
// A posted invoice or credit note is queued, checked against PINT-SG's rules first; the access point sends it and IRAS's
// acknowledgement ID comes back through it. The gate lists the documents of a return period that have neither an
// acknowledgement nor an exclusion; a contingency, requested by one person and approved by another, lets a return be filed
// with the gap (p2-invoicenow design D4 and D5). The operations use the GST return capability (design D7).

// InvoiceNowCodes are the codes the InvoiceNow operations answer besides the common ones.
var InvoiceNowCodes = []string{storage.CodeInvoiceNow, CodeUnavailable, CodeNotFound, CodeInvalid}

// InvoiceNowSubmission is a document's submission.
type InvoiceNowSubmission struct {
	ID             string `json:"id" format:"uuid" doc:"The submission's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c8a01"`
	DocumentID     string `json:"document_id" format:"uuid" doc:"The invoice or credit note it is for." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c8a02"`
	DocumentUUID   string `json:"document_uuid" format:"uuid" doc:"The document's unique identifier on the network (BT-SG-003): the same for every version." example:"0a1b2c3d-1111-4222-8333-444455556666"`
	Version        int    `json:"version" doc:"1, then one more for each correction sent." example:"1"`
	Kind           string `json:"kind" enum:"peppol,extracted_sale,extracted_purchase,aggregated" doc:"peppol: to a customer on the network; extracted_sale: to IRAS alone, the customer not being on the network." example:"peppol"`
	Direction      string `json:"direction" enum:"sale,purchase" doc:"The direction." example:"sale"`
	Status         string `json:"status" enum:"queued,sent,acknowledged,failed" doc:"acknowledged only with IRAS's acknowledgement ID." example:"sent"`
	Recipient      string `json:"recipient,omitempty" doc:"The customer's Peppol participant ID, for a Peppol submission." example:"0195:SGUEN201912345A"`
	TransmissionID string `json:"transmission_id,omitempty" doc:"The access point's ID for the transmission." example:"tx-3f2c1a9d4b7e"`
	IRASAck        string `json:"iras_ack,omitempty" doc:"IRAS's acknowledgement ID." example:"1500003994"`
	LastError      string `json:"last_error,omitempty" doc:"Why it failed." example:"the document changed after it was queued: queue it again"`
	CreatedAt      string `json:"created_at" format:"date-time" doc:"The created at." example:"2027-02-03T09:00:00Z"`
	UpdatedAt      string `json:"updated_at" format:"date-time" doc:"The updated at." example:"2027-02-03T09:05:00Z"`
}

// InvoiceNowSubmissionPage is a page of submissions.
type InvoiceNowSubmissionPage struct {
	Items      []InvoiceNowSubmission `json:"items"`
	NextCursor string                 `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjIwMjYifQ"`
}

// InvoiceNowExclusion is a document left out of the submissions.
type InvoiceNowExclusion struct {
	ID         string `json:"id" format:"uuid" doc:"The id." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c8a01"`
	DocumentID string `json:"document_id" format:"uuid" doc:"The document id." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c8a02"`
	Reason     string `json:"reason" enum:"reverse_charge,import_permit,deemed_supply,financial_services,unregistered_supplier,blocked_input_tax,out_of_scope,other" doc:"Why no submission is expected." example:"reverse_charge"`
	Detail     string `json:"detail,omitempty" maxLength:"500" doc:"What the person says when the reason is other." example:"Supplied under a customer accounting arrangement"`
	RecordedBy string `json:"recorded_by" doc:"The recorded by." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c8a03"`
	RecordedAt string `json:"recorded_at" format:"date-time" doc:"The recorded at." example:"2027-02-03T09:00:00Z"`
	Withdrawn  bool   `json:"withdrawn" doc:"true once ended: the document is expected to be submitted again." example:"false"`
}

// InvoiceNowContingency is the decision to file a return with a gap.
type InvoiceNowContingency struct {
	ID          string `json:"id" format:"uuid" doc:"The id." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c8a01"`
	PeriodFrom  string `json:"period_from" format:"date" doc:"The period from." example:"2027-01-01"`
	PeriodTo    string `json:"period_to" format:"date" doc:"The period to." example:"2027-03-31"`
	Reason      string `json:"reason" doc:"The reason." example:"The access point was down on 30 and 31 March"`
	Evidence    string `json:"evidence" doc:"The outage and any IRAS correspondence, as recorded." example:"The provider's incident notice of 31 March, reference INC-2027-0331"`
	RequestedBy string `json:"requested_by" doc:"The requested by." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c8a03"`
	RequestedAt string `json:"requested_at" format:"date-time" doc:"The requested at." example:"2027-04-02T09:00:00Z"`
	ApprovedBy  string `json:"approved_by,omitempty" doc:"The approved by." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c8a04"`
	ApprovedAt  string `json:"approved_at,omitempty" format:"date-time" doc:"The approved at." example:"2027-04-02T10:00:00Z"`
}

// InvoiceNowMissing is a document the gate waits for.
type InvoiceNowMissing struct {
	DocumentID  string `json:"document_id" format:"uuid" doc:"The document id." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c8a02"`
	Number      string `json:"number" doc:"The number." example:"INV-2027-0007"`
	Kind        string `json:"kind" enum:"sale,purchase,credit_note" doc:"What kind of document it is." example:"sale"`
	Date        string `json:"date" format:"date" doc:"The date." example:"2027-02-03"`
	State       string `json:"state" enum:"none,queued,sent,failed,acknowledged" doc:"Where its submission stands: none when it has not been queued." example:"queued"`
	NeedsChoice bool   `json:"needs_choice" doc:"It has an exempt line and the tax agent has not chosen ES33 or ESN33 (setting invoicenow.es_category)." example:"false"`
}

// InvoiceNowGate is the filing gate for a return period (FR-TAX-11).
type InvoiceNowGate struct {
	From         string                 `json:"from" format:"date" doc:"The from." example:"2027-01-01"`
	To           string                 `json:"to" format:"date" doc:"The to." example:"2027-03-31"`
	State        string                 `json:"state" enum:"not_assessed,ready,blocked" doc:"not_assessed: the requirement is not marked as applying, or no sg-invoicenow version is active; ready: nothing is missing, or an approved contingency covers it; blocked: the return waits." example:"blocked"`
	Reason       string                 `json:"reason,omitempty" doc:"Why the gate is not assessed." example:"The InvoiceNow requirement is not marked as applying (setting invoicenow.applies)."`
	Documents    int                    `json:"documents" doc:"The documents of the period the requirement covers." example:"12"`
	Acknowledged int                    `json:"acknowledged" doc:"The acknowledged." example:"9"`
	Excluded     int                    `json:"excluded" doc:"The excluded." example:"2"`
	Missing      []InvoiceNowMissing    `json:"missing" doc:"What the gate waits for, in date order."`
	Contingency  *InvoiceNowContingency `json:"contingency,omitempty" doc:"The approved contingency that lets the return be filed."`
}

type (
	invoiceNowGateInput struct {
		From string `query:"from" format:"date" required:"true" doc:"The return period's first day." example:"2027-01-01"`
		To   string `query:"to" format:"date" required:"true" doc:"The return period's last day." example:"2027-03-31"`
	}
	invoiceNowGateOutput struct{ Body InvoiceNowGate }
	invoiceNowListInput  struct {
		PageQuery
		Status     string `query:"status" enum:"queued,sent,acknowledged,failed" doc:"Only submissions in this status." example:"failed"`
		DocumentID string `query:"document_id" format:"uuid" doc:"Only this document's submissions." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c8a01"`
	}
	invoiceNowListOutput struct{ Body InvoiceNowSubmissionPage }
	invoiceNowQueueBody  struct {
		DocumentID string `json:"document_id" format:"uuid" doc:"A posted invoice or credit note." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c8a02"`
	}
	invoiceNowQueueInput struct {
		KeyHeader
		PreferHeader
		Body invoiceNowQueueBody
	}
	InvoiceNowSubmissionPath struct {
		SubmissionID string `path:"submission_id" format:"uuid" doc:"The submission's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c8a01"`
	}
	invoiceNowSubmissionInput struct {
		InvoiceNowSubmissionPath
		KeyHeader
		PreferHeader
	}
	invoiceNowSubmissionOutput struct {
		Status   int
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Body     InvoiceNowSubmission
	}
	invoiceNowExcludeBody struct {
		DocumentID string `json:"document_id" format:"uuid" doc:"The document to leave out." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c8a02"`
		Reason     string `json:"reason" enum:"reverse_charge,import_permit,deemed_supply,financial_services,unregistered_supplier,blocked_input_tax,out_of_scope,other" doc:"The reason." example:"The InvoiceNow requirement is not marked as applying (setting invoicenow.applies)."`
		Detail     string `json:"detail,omitempty" maxLength:"500" doc:"Required when the reason is other." example:"Supplied under a customer accounting arrangement"`
	}
	invoiceNowExcludeInput struct {
		KeyHeader
		PreferHeader
		Body invoiceNowExcludeBody
	}
	InvoiceNowExclusionPath struct {
		ExclusionID string `path:"exclusion_id" format:"uuid" doc:"The exclusion's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c8a01"`
	}
	invoiceNowWithdrawInput struct {
		InvoiceNowExclusionPath
		KeyHeader
		PreferHeader
	}
	invoiceNowExclusionOutput struct {
		Status   int
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Body     InvoiceNowExclusion
	}
	invoiceNowContingencyBody struct {
		PeriodFrom string `json:"period_from" format:"date" doc:"The return period's first day." example:"2027-01-01"`
		PeriodTo   string `json:"period_to" format:"date" doc:"The return period's last day." example:"2027-03-31"`
		Reason     string `json:"reason" minLength:"10" maxLength:"1000" doc:"Why the return cannot wait for the missing submissions." example:"The InvoiceNow requirement is not marked as applying (setting invoicenow.applies)."`
		Evidence   string `json:"evidence" minLength:"10" maxLength:"4000" doc:"The outage of the access point or IRAS, and any IRAS correspondence." example:"The provider's incident notice of 31 March, reference INC-2027-0331"`
	}
	invoiceNowContingencyInput struct {
		KeyHeader
		PreferHeader
		Body invoiceNowContingencyBody
	}
	InvoiceNowContingencyPath struct {
		ContingencyID string `path:"contingency_id" format:"uuid" doc:"The contingency's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c8a01"`
	}
	invoiceNowApproveInput struct {
		InvoiceNowContingencyPath
		KeyHeader
		PreferHeader
	}
	invoiceNowContingencyOutput struct {
		Status   int
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Body     InvoiceNowContingency
	}
)

func submissionOut(s storage.InvoiceSubmission) InvoiceNowSubmission {
	return InvoiceNowSubmission{ID: s.ID.String(), DocumentID: s.DocumentID.String(), DocumentUUID: s.DocumentUUID.String(), Version: s.Version, Kind: s.Kind,
		Direction: s.Direction, Status: s.Status, Recipient: s.Recipient, TransmissionID: s.TransmissionID, IRASAck: s.IRASAck, LastError: s.LastError,
		CreatedAt: s.CreatedAt.UTC().Format(time.RFC3339), UpdatedAt: s.UpdatedAt.UTC().Format(time.RFC3339)}
}

func exclusionOut(x storage.InvoiceExclusion) InvoiceNowExclusion {
	return InvoiceNowExclusion{ID: x.ID.String(), DocumentID: x.DocumentID.String(), Reason: x.Reason, Detail: x.Detail, RecordedBy: x.RecordedBy,
		RecordedAt: x.RecordedAt.UTC().Format(time.RFC3339), Withdrawn: x.WithdrawnAt != nil}
}

func contingencyOut(c storage.InvoiceContingency) InvoiceNowContingency {
	out := InvoiceNowContingency{ID: c.ID.String(), PeriodFrom: c.PeriodFrom.Format(time.DateOnly), PeriodTo: c.PeriodTo.Format(time.DateOnly), Reason: c.Reason,
		Evidence: c.Evidence, RequestedBy: c.RequestedBy, RequestedAt: c.RequestedAt.UTC().Format(time.RFC3339), ApprovedBy: c.ApprovedBy}
	if c.ApprovedAt != nil {
		out.ApprovedAt = c.ApprovedAt.UTC().Format(time.RFC3339)
	}
	return out
}

// invoiceNowRefusal turns the rules a document breaks into the fields it is told to fix (ENG-23).
func invoiceNowRefusal(err error) error {
	var r *invoicenow.RefusedError
	if !errors.As(err, &r) {
		return err
	}
	fields := make([]FieldProblem, 0, len(r.Findings))
	for _, f := range r.Findings {
		fields = append(fields, FieldProblem{Code: CodeInvalid, Field: f.Field, Message: f.Rule + ": " + f.Message, Fix: "Correct the document, or the contact or organisation details it takes, then queue it again"})
	}
	return invalid(fields)
}

func (a *API) accessPoint() (ports.AccessPoint, error) {
	if a.deps.AccessPoint == nil {
		return nil, problem(CodeUnavailable, "No InvoiceNow access point is connected to this installation.", "Connect an access point (question 2) before sending; local mode has a stand-in.")
	}
	return a.deps.AccessPoint, nil
}

// InvoiceNowInbound is a document received through InvoiceNow (FR-AP-05).
type InvoiceNowInbound struct {
	ID         string `json:"id" format:"uuid" doc:"The received document's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c8a01"`
	MessageID  string `json:"message_id" doc:"The access point's ID of the message." example:"msg-1"`
	Sender     string `json:"sender" doc:"The sender's Peppol participant ID." example:"0195:SGUEN202012345K"`
	Number     string `json:"number,omitempty" doc:"The invoice number it states." example:"INV-2027-0007"`
	ReceivedAt string `json:"received_at" format:"date-time" doc:"When it was received." example:"2027-02-03T09:00:00Z"`
	Status     string `json:"status" enum:"received,drafted,refused" doc:"received: kept, no draft yet (see note); drafted: a draft bill was made; refused: it is not read, as note says." example:"drafted"`
	BillID     string `json:"bill_id,omitempty" format:"uuid" doc:"The draft bill made from it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c8a09"`
	Note       string `json:"note,omitempty" doc:"Why it waits or was refused." example:"no active supplier has that Peppol ID: add the supplier, then draft the bill"`
}

// InvoiceNowInboundPage is a page of received documents.
type InvoiceNowInboundPage struct {
	Items      []InvoiceNowInbound `json:"items"`
	NextCursor string              `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjIwMjYifQ"`
}

type (
	invoiceNowInboundListInput  struct{ PageQuery }
	invoiceNowInboundListOutput struct{ Body InvoiceNowInboundPage }
	invoiceNowPollInput         struct {
		KeyHeader
		PreferHeader
	}
	invoiceNowPollOutput struct {
		Status   int
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Body     InvoiceNowInboundPage
	}
	InvoiceNowInboundPath struct {
		InboundID string `path:"inbound_id" format:"uuid" doc:"The received document's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c8a01"`
	}
	invoiceNowDraftInput struct {
		InvoiceNowInboundPath
		KeyHeader
		PreferHeader
	}
	invoiceNowDraftOutput struct {
		Status   int
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Body     InvoiceNowInbound
	}
)

func inboundOut(x storage.InvoiceInbound) InvoiceNowInbound {
	out := InvoiceNowInbound{ID: x.ID.String(), MessageID: x.MessageID, Sender: x.Sender, Number: x.Number, ReceivedAt: x.ReceivedAt.UTC().Format(time.RFC3339), Status: x.Status, Note: x.Note}
	if x.BillID != nil {
		out.BillID = x.BillID.String()
	}
	return out
}

func registerInvoiceNow(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid}, locks...)

	register(a, authz.GetInvoiceNowGate, huma.Operation{
		OperationID: "getInvoiceNowGate", Method: http.MethodGet, Path: "/v1/compliance/invoicenow-gate", Tags: []string{"Compliance"},
		Summary: "Get the InvoiceNow filing gate for a return period",
		Description: "Which documents of the period the GST InvoiceNow requirement covers still have neither IRAS's acknowledgement ID nor a recorded " +
			"exclusion (FR-TAX-10, FR-TAX-11): the return waits for them, unless an approved contingency covers the period. Not assessed " +
			"while the setting invoicenow.applies is off or no sg-invoicenow version is active. Read-only; no side effects.",
	}, append(append([]string{}, locks...), CodeInvalid), func(ctx context.Context, in *invoiceNowGateInput) (*invoiceNowGateOutput, error) {
		from, err := time.Parse(time.DateOnly, in.From)
		to, err2 := time.Parse(time.DateOnly, in.To)
		if err != nil || err2 != nil || to.Before(from) {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "query.to", Message: "must be a day on or after from", Fix: "Send the return period's first and last day"}})
		}
		p, _ := authn.From(ctx)
		var out invoiceNowGateOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			base, err := baseOf(ctx, u)
			if err != nil {
				return err
			}
			g, err := invoicenow.Gate(ctx, u, base, from, to)
			if err != nil {
				return err
			}
			out.Body = InvoiceNowGate{From: in.From, To: in.To, Missing: []InvoiceNowMissing{}, Documents: len(g.Entries), Acknowledged: g.Acknowledged, Excluded: g.Excluded}
			switch {
			case g.NoPack:
				out.Body.State, out.Body.Reason = "not_assessed", "No sg-invoicenow version is active: activate it under Settings › Statutory packs."
			case !g.Applies:
				out.Body.State, out.Body.Reason = "not_assessed", "The InvoiceNow requirement is not marked as applying (setting invoicenow.applies): the tax agent confirms it (question 2)."
			case g.Ready:
				out.Body.State = "ready"
			default:
				out.Body.State = "blocked"
			}
			needs := map[string]bool{}
			for _, e := range g.Entries {
				needs[e.DocumentID] = e.NeedsChoice
			}
			for _, m := range g.Missing {
				out.Body.Missing = append(out.Body.Missing, InvoiceNowMissing{DocumentID: m.DocumentID, Number: m.Number, Kind: m.Kind, Date: m.Date.Format(time.DateOnly),
					State: m.State, NeedsChoice: needs[m.DocumentID]})
			}
			if g.Contingency != nil {
				cs, err := u.InvoiceNow().Contingencies(ctx, from, to)
				if err != nil {
					return err
				}
				for _, c := range cs {
					if c.ApprovedBy != "" && c.ApprovedBy == g.Contingency.ApprovedBy && c.RequestedBy == g.Contingency.RequestedBy {
						v := contingencyOut(c)
						out.Body.Contingency = &v
						break
					}
				}
			}
			return nil
		})
		return &out, err
	})

	register(a, authz.ListInvoiceNowSubmissions, huma.Operation{
		OperationID: "listInvoiceNowSubmissions", Method: http.MethodGet, Path: "/v1/invoicenow/submissions", Tags: []string{"Compliance"},
		Summary:     "List the InvoiceNow submissions",
		Description: "Submissions of invoices and credit notes through InvoiceNow, newest first, a page at a time (FR-TAX-10). Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *invoiceNowListInput) (*invoiceNowListOutput, error) {
		p, _ := authn.From(ctx)
		out := invoiceNowListOutput{Body: InvoiceNowSubmissionPage{Items: []InvoiceNowSubmission{}}}
		var doc *uuid.UUID
		if in.DocumentID != "" {
			id, err := parseID("document_id", in.DocumentID)
			if err != nil {
				return nil, err
			}
			doc = &id
		}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			all, err := u.InvoiceNow().Submissions(ctx, storage.InvoiceSubmissionFilter{Status: in.Status, DocumentID: doc, Limit: 500})
			if err != nil {
				return err
			}
			views := make([]InvoiceNowSubmission, 0, len(all))
			for _, s := range all {
				views = append(views, submissionOut(s))
			}
			out.Body.Items, out.Body.NextCursor, err = pageOf(views, func(x InvoiceNowSubmission) string { return x.ID }, in.PageQuery)
			return err
		})
		return &out, err
	})

	register(a, authz.QueueInvoiceNowSubmission, huma.Operation{
		OperationID: "queueInvoiceNowSubmission", Method: http.MethodPost, Path: "/v1/invoicenow/submissions", Tags: []string{"Compliance"},
		DefaultStatus: http.StatusCreated, Summary: "Queue an invoice or credit note for InvoiceNow",
		Description: "Builds the PINT-SG document of a posted invoice or credit note and checks it against the published GST rules: one that breaks a rule is " +
			"refused with each rule and field, and never queued. A customer on the network gets a Peppol submission; with no Peppol ID, or one the " +
			"directory does not know, it goes to IRAS alone as a solution-extracted sale (FR-TAX-10). A correction queues the next version of the " +
			"same document UUID. Side effects: the submission, audit events. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{}, writes...), InvoiceNowCodes...), func(ctx context.Context, in *invoiceNowQueueInput) (*invoiceNowSubmissionOutput, error) {
		id, err := parseID("document_id", in.Body.DocumentID)
		if err != nil {
			return nil, err
		}
		ap, err := a.accessPoint()
		if err != nil {
			return nil, err
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (InvoiceNowSubmission, error) {
			base, err := baseOf(ctx, u)
			if err != nil {
				return InvoiceNowSubmission{}, err
			}
			s, err := invoicenow.Enqueue(ctx, u, ap, id, base, a.deps.Clock.Now())
			if errors.Is(err, invoicenow.ErrNotASale) {
				return InvoiceNowSubmission{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.document_id", Message: err.Error(), Fix: "Send the ID of a posted invoice or credit note"}})
			}
			if errors.Is(err, invoicenow.ErrNoPack) {
				return InvoiceNowSubmission{}, problem(CodeUnavailable, err.Error(), "Activate sg-invoicenow under Settings › Statutory packs.")
			}
			if errors.Is(err, ports.ErrAccessPointUnavailable) {
				return InvoiceNowSubmission{}, problem(CodeUnavailable, "The InvoiceNow access point cannot be reached.", "Try again later: nothing was queued.")
			}
			if err != nil {
				return InvoiceNowSubmission{}, invoiceNowRefusal(err)
			}
			return submissionOut(s), nil
		})
		if err != nil {
			return nil, err
		}
		return &invoiceNowSubmissionOutput{Status: w.Status, Applied: applied(w), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})

	register(a, authz.SendInvoiceNowSubmission, huma.Operation{
		OperationID: "sendInvoiceNowSubmission", Method: http.MethodPost, Path: "/v1/invoicenow/submissions/{submission_id}/send", Tags: []string{"Compliance"},
		Summary: "Send a queued submission and read its status",
		Description: "Hands a queued submission to the access point, or reads a sent one's status up to IRAS's acknowledgement ID. The document is built again and must " +
			"be the one queued, else the submission fails with that reason. During an outage the submission stays queued and the call answers 503: call again " +
			"later (nothing is lost, G4-033). Side effects: the status, its history and audit events. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{}, writes...), InvoiceNowCodes...), func(ctx context.Context, in *invoiceNowSubmissionInput) (*invoiceNowSubmissionOutput, error) {
		id, err := parseID("submission_id", in.SubmissionID)
		if err != nil {
			return nil, err
		}
		ap, err := a.accessPoint()
		if err != nil {
			return nil, err
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, id, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (InvoiceNowSubmission, error) {
			base, err := baseOf(ctx, u)
			if err != nil {
				return InvoiceNowSubmission{}, err
			}
			s, err := u.InvoiceNow().Submission(ctx, id)
			if err != nil {
				return InvoiceNowSubmission{}, err
			}
			now := a.deps.Clock.Now()
			if wantsPreview(in.Prefer) {
				return submissionOut(s), nil // a preview never reaches the access point: it answers what the submission is now
			}
			switch s.Status {
			case string(invoicenow.Queued):
				s, err = invoicenow.Send(ctx, u, ap, s, base, now)
			case string(invoicenow.Sent):
				s, err = invoicenow.Refresh(ctx, u, ap, s, now)
			}
			if errors.Is(err, ports.ErrAccessPointUnavailable) {
				return InvoiceNowSubmission{}, problem(CodeUnavailable, "The InvoiceNow access point cannot be reached.", "Try again later: the submission stays queued and nothing was sent.")
			}
			if err != nil {
				return InvoiceNowSubmission{}, err
			}
			return submissionOut(s), nil
		})
		if err != nil {
			return nil, err
		}
		return &invoiceNowSubmissionOutput{Status: w.Status, Applied: applied(w), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})

	register(a, authz.RetryInvoiceNowSubmission, huma.Operation{
		OperationID: "retryInvoiceNowSubmission", Method: http.MethodPost, Path: "/v1/invoicenow/submissions/{submission_id}/retry", Tags: []string{"Compliance"},
		Summary:     "Queue a failed submission again",
		Description: "Moves a failed submission back to queued so it is sent again, with the same version and UUID (a changed document is a new version: queue it instead). Side effects: the status and audit events. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{}, writes...), InvoiceNowCodes...), func(ctx context.Context, in *invoiceNowSubmissionInput) (*invoiceNowSubmissionOutput, error) {
		id, err := parseID("submission_id", in.SubmissionID)
		if err != nil {
			return nil, err
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, id, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (InvoiceNowSubmission, error) {
			s, err := u.InvoiceNow().Move(ctx, id, storage.InvoiceSubmissionMove{Status: string(invoicenow.Queued), At: a.deps.Clock.Now()})
			if err != nil {
				return InvoiceNowSubmission{}, err
			}
			return submissionOut(s), nil
		})
		if err != nil {
			return nil, err
		}
		return &invoiceNowSubmissionOutput{Status: w.Status, Applied: applied(w), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})

	register(a, authz.CreateInvoiceNowExclusion, huma.Operation{
		OperationID: "createInvoiceNowExclusion", Method: http.MethodPost, Path: "/v1/invoicenow/exclusions", Tags: []string{"Compliance"},
		DefaultStatus: http.StatusCreated, Summary: "Leave a document out of the InvoiceNow submissions",
		Description: "Records why a document of the return needs no submission, such as a reverse charge supply or an import permit (FR-TAX-10, G4-034). One live exclusion for a " +
			"document: withdraw it to record another. Side effects: the exclusion, audit events. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{}, writes...), InvoiceNowCodes...), func(ctx context.Context, in *invoiceNowExcludeInput) (*invoiceNowExclusionOutput, error) {
		id, err := parseID("document_id", in.Body.DocumentID)
		if err != nil {
			return nil, err
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (InvoiceNowExclusion, error) {
			if in.Body.Reason == "other" && len(in.Body.Detail) < 3 {
				return InvoiceNowExclusion{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.detail", Message: "says why when the reason is other", Fix: "Write the reason in a few words"}})
			}
			if _, err := u.Documents().Get(ctx, id); err != nil {
				return InvoiceNowExclusion{}, err
			}
			x, err := u.InvoiceNow().Exclude(ctx, id, in.Body.Reason, in.Body.Detail, a.deps.Clock.Now())
			if err != nil {
				return InvoiceNowExclusion{}, err
			}
			return exclusionOut(x), nil
		})
		if err != nil {
			return nil, err
		}
		return &invoiceNowExclusionOutput{Status: w.Status, Applied: applied(w), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})

	register(a, authz.WithdrawInvoiceNowExclusion, huma.Operation{
		OperationID: "withdrawInvoiceNowExclusion", Method: http.MethodPost, Path: "/v1/invoicenow/exclusions/{exclusion_id}/withdraw", Tags: []string{"Compliance"},
		Summary:     "Withdraw an exclusion",
		Description: "Ends an exclusion, once: the document is expected to be submitted again. Side effects: the exclusion's end and audit events. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{}, writes...), InvoiceNowCodes...), func(ctx context.Context, in *invoiceNowWithdrawInput) (*invoiceNowExclusionOutput, error) {
		id, err := parseID("exclusion_id", in.ExclusionID)
		if err != nil {
			return nil, err
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, id, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (InvoiceNowExclusion, error) {
			x, err := u.InvoiceNow().WithdrawExclusion(ctx, id, a.deps.Clock.Now())
			if err != nil {
				return InvoiceNowExclusion{}, err
			}
			return exclusionOut(x), nil
		})
		if err != nil {
			return nil, err
		}
		return &invoiceNowExclusionOutput{Status: w.Status, Applied: applied(w), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})

	register(a, authz.RequestInvoiceNowContingency, huma.Operation{
		OperationID: "requestInvoiceNowContingency", Method: http.MethodPost, Path: "/v1/invoicenow/contingencies", Tags: []string{"Compliance"},
		DefaultStatus: http.StatusCreated, Summary: "Ask to file a return with a gap in its InvoiceNow submissions",
		Description: "When an access point or IRAS outage would make a return late, a person asks to file it with the gap recorded, with the reason and the evidence of the outage " +
			"(FR-TAX-11, G4-033). It lets nothing through until someone else approves it. Side effects: the request, audit events. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{}, writes...), InvoiceNowCodes...), func(ctx context.Context, in *invoiceNowContingencyInput) (*invoiceNowContingencyOutput, error) {
		from, err := time.Parse(time.DateOnly, in.Body.PeriodFrom)
		to, err2 := time.Parse(time.DateOnly, in.Body.PeriodTo)
		if err != nil || err2 != nil || to.Before(from) {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.period_to", Message: "must be a day on or after period_from", Fix: "Send the return period's first and last day"}})
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (InvoiceNowContingency, error) {
			c, err := u.InvoiceNow().RequestContingency(ctx, storage.NewInvoiceContingency{PeriodFrom: from, PeriodTo: to, Reason: in.Body.Reason, Evidence: in.Body.Evidence,
				At: a.deps.Clock.Now()})
			if err != nil {
				return InvoiceNowContingency{}, err
			}
			return contingencyOut(c), nil
		})
		if err != nil {
			return nil, err
		}
		return &invoiceNowContingencyOutput{Status: w.Status, Applied: applied(w), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})

	register(a, authz.ApproveInvoiceNowContingency, huma.Operation{
		OperationID: "approveInvoiceNowContingency", Method: http.MethodPost, Path: "/v1/invoicenow/contingencies/{contingency_id}/approve", Tags: []string{"Compliance"},
		Summary: "Approve a contingency",
		Description: "Approves a request someone else made, once: the return may then be filed with the gap, and the unsent items stay queued and are sent when service returns " +
			"(FR-TAX-11). The person who requested it cannot approve it. Side effects: the approval and audit events. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{}, writes...), InvoiceNowCodes...), func(ctx context.Context, in *invoiceNowApproveInput) (*invoiceNowContingencyOutput, error) {
		id, err := parseID("contingency_id", in.ContingencyID)
		if err != nil {
			return nil, err
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, id, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (InvoiceNowContingency, error) {
			// the database refuses the requester as the approver (FYI04)
			c, err := u.InvoiceNow().ApproveContingency(ctx, id, a.deps.Clock.Now())
			if err != nil {
				return InvoiceNowContingency{}, err
			}
			return contingencyOut(c), nil
		})
		if err != nil {
			return nil, err
		}
		return &invoiceNowContingencyOutput{Status: w.Status, Applied: applied(w), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})

	register(a, authz.ListInvoiceNowInbound, huma.Operation{
		OperationID: "listInvoiceNowInbound", Method: http.MethodGet, Path: "/v1/invoicenow/inbound", Tags: []string{"Purchases"},
		Summary:     "List the invoices received through InvoiceNow",
		Description: "The documents suppliers sent through InvoiceNow, newest first, a page at a time, each with its status and the draft bill made from it (FR-AP-05). Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *invoiceNowInboundListInput) (*invoiceNowInboundListOutput, error) {
		p, _ := authn.From(ctx)
		out := invoiceNowInboundListOutput{Body: InvoiceNowInboundPage{Items: []InvoiceNowInbound{}}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			all, err := u.InvoiceNow().InboundPage(ctx, nil, 500)
			if err != nil {
				return err
			}
			views := make([]InvoiceNowInbound, 0, len(all))
			for _, x := range all {
				views = append(views, inboundOut(x))
			}
			out.Body.Items, out.Body.NextCursor, err = pageOf(views, func(x InvoiceNowInbound) string { return x.ID }, in.PageQuery)
			return err
		})
		return &out, err
	})

	register(a, authz.PollInvoiceNowInbound, huma.Operation{
		OperationID: "pollInvoiceNowInbound", Method: http.MethodPost, Path: "/v1/invoicenow/inbound/poll", Tags: []string{"Purchases"},
		DefaultStatus: http.StatusOK, Summary: "Receive the invoices suppliers sent through InvoiceNow",
		Description: "Reads what the access point holds for the organisation since the last document kept, keeps each as received and makes a draft bill of each whose supplier is a contact (the Peppol ID or the UEN " +
			"matches); a document that is not PINT-SG, or that the sender sent before under the same number, is refused with why, and one with no known supplier waits for it (FR-AP-05). A draft posts nothing. " +
			"During an outage it answers 503 and keeps nothing. Side effects: the received documents, the draft bills, audit events. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{}, writes...), InvoiceNowCodes...), func(ctx context.Context, in *invoiceNowPollInput) (*invoiceNowPollOutput, error) {
		ap, err := a.accessPoint()
		if err != nil {
			return nil, err
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, "poll", http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (InvoiceNowInboundPage, error) {
			got, err := invoicenow.Receive(ctx, u, ap, a.deps.Clock.Now())
			if errors.Is(err, ports.ErrAccessPointUnavailable) {
				return InvoiceNowInboundPage{}, problem(CodeUnavailable, "The InvoiceNow access point cannot be reached.", "Try again later: nothing was received or kept.")
			}
			if err != nil {
				return InvoiceNowInboundPage{}, err
			}
			page := InvoiceNowInboundPage{Items: []InvoiceNowInbound{}}
			for _, x := range got {
				page.Items = append(page.Items, inboundOut(x))
			}
			return page, nil
		})
		if err != nil {
			return nil, err
		}
		return &invoiceNowPollOutput{Status: w.Status, Applied: applied(w), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})

	register(a, authz.DraftInvoiceNowInbound, huma.Operation{
		OperationID: "draftInvoiceNowInbound", Method: http.MethodPost, Path: "/v1/invoicenow/inbound/{inbound_id}/draft", Tags: []string{"Purchases"},
		Summary: "Make the draft bill of a received document",
		Description: "Tries again to make the draft bill of a received document that waited, such as for its supplier to be added (FR-AP-05). One already drafted or refused answers 412. A draft posts nothing. " +
			"Side effects: the draft bill, the document's status, audit events. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{}, writes...), InvoiceNowCodes...), func(ctx context.Context, in *invoiceNowDraftInput) (*invoiceNowDraftOutput, error) {
		id, err := parseID("inbound_id", in.InboundID)
		if err != nil {
			return nil, err
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, id, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (InvoiceNowInbound, error) {
			x, err := u.InvoiceNow().Inbound(ctx, id)
			if err != nil {
				return InvoiceNowInbound{}, err
			}
			if x.Status != "received" {
				return InvoiceNowInbound{}, &storage.Error{Code: storage.CodeVersion, Message: "the received document was drafted or refused already"}
			}
			x, err = invoicenow.DraftInbound(ctx, u, id)
			if err != nil {
				return InvoiceNowInbound{}, err
			}
			return inboundOut(x), nil
		})
		if err != nil {
			return nil, err
		}
		return &invoiceNowDraftOutput{Status: w.Status, Applied: applied(w), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})
}
