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
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/pspnet"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// More stable codes of the payment providers (ENG-23).
const (
	CodePSPDisputeState = "PSP-05" // the dispute is not one whose money waits in disputes clearing and is posted, or no accounts are set
	CodePSPDisputeLimit = "PSP-06" // above the expense limit, written off without a reason and the acknowledgement
	CodePSPEventState   = "PSP-07" // the event is not parked or failed
)

// PaymentProviderDispute is a dispute's money in the provider's disputes clearing account.
type PaymentProviderDispute struct {
	DisputeID      string `json:"dispute_id" doc:"The provider's ID." example:"dp_1Q3"`
	MoneyAccountID string `json:"money_account_id" format:"uuid" doc:"The provider's clearing money account." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b10"`
	Held           Money  `json:"held" doc:"What its posted lines have moved into disputes clearing and the provider has not given back."`
	Status         string `json:"status" enum:"open,won,written_off" doc:"open: its money waits; won: the provider gave it all back; written_off: a draft journal was prepared to write it off." example:"open"`
	AllPosted      bool   `json:"all_posted" doc:"Whether every statement line of the dispute has posted; a draft is prepared only when they all have." example:"true"`
	OverLimit      bool   `json:"over_limit" doc:"Whether it is above the limit up to which a lost dispute is written off as an expense without a reason (psp.dispute_expense_limit)." example:"false"`
	DocumentID     string `json:"loss_document_id,omitempty" format:"uuid" doc:"The draft journal prepared to write it off." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b80"`
}

type pspDisputesOutput struct {
	Body struct {
		Items      []PaymentProviderDispute `json:"items" nullable:"false" doc:"The disputes, oldest first."`
		NextCursor string                   `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjAxOTIifQ"`
	}
}

type PSPDisputePath struct {
	DisputeID string `path:"dispute_id" minLength:"1" maxLength:"255" doc:"The provider's ID of the dispute." example:"dp_1Q3"`
}

// DisputeLossDraft asks for the draft journal that writes a lost dispute off.
type DisputeLossDraft struct {
	MoneyAccountID string `json:"money_account_id" format:"uuid" doc:"The provider's clearing money account the dispute is on." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b10"`
	Date           string `json:"date,omitempty" format:"date" doc:"The journal's date; today when left out." example:"2026-11-19"`
	Accepted       bool   `json:"accept_overstated_revenue,omitempty" doc:"Above the limit, confirms the loss is written off as an expense although the invoice's revenue stays as invoiced until it is credited." example:"false"`
	Reason         string `json:"reason,omitempty" maxLength:"500" doc:"Above the limit, why it is written off without crediting the invoice." example:"The customer cannot be found: the charge was a one-off"`
}

type prepareDisputeLossInput struct {
	PSPDisputePath
	KeyHeader
	PreferHeader
	Body DisputeLossDraft
}

// DisputeLossPrepared is the draft journal prepared for a lost dispute.
type DisputeLossPrepared struct {
	DisputeID  string `json:"dispute_id" doc:"The provider's ID." example:"dp_1Q3"`
	DocumentID string `json:"document_id" format:"uuid" doc:"The draft journal: Dr the dispute loss account, Cr disputes clearing. Post it under Journals, with its approval." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b80"`
	Amount     Money  `json:"amount" doc:"What it writes off."`
	OverLimit  bool   `json:"over_limit" doc:"Whether it was above the expense limit." example:"false"`
}

type disputeLossOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     DisputeLossPrepared
}

type PSPEventPath struct {
	EventID string `path:"event_id" format:"uuid" doc:"The event's ID in Fyniq." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b70"`
}

type resolveEventInput struct {
	PSPEventPath
	KeyHeader
	PreferHeader
	Body struct {
		Reason string `json:"reason" minLength:"1" maxLength:"500" doc:"What the person did about it, or why nothing is needed." example:"A fraud warning for a charge we refunded: nothing to post"`
	}
}

type resolveEventOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     PaymentProviderEvent
}

func pspEventOf(e storage.PSPEvent) PaymentProviderEvent {
	return PaymentProviderEvent{ID: e.ID.String(), ConnectorID: e.ConnectorID.String(), Provider: e.Provider, EventID: e.EventID, EventType: e.EventType,
		APIVersion: e.APIVersion, ParserVersion: e.ParserVersion, ObjectKind: e.ObjectKind, ObjectID: e.ObjectID, Status: e.Status,
		ParkedReason: e.ParkedReason, LastError: e.LastError, ReceivedAt: e.ReceivedAt.UTC().Format("2006-01-02T15:04:05Z")}
}

// disputeLimit is the organisation's limit on a day, in the base currency (psp.dispute_expense_limit).
func disputeLimit(ctx context.Context, u storage.UnitOfWork, base money.Currency, on time.Time) (money.Money, error) {
	def, _ := settings.Lookup("psp.dispute_expense_limit")
	changes, err := u.Settings().Changes(ctx, def.Key)
	if err != nil {
		return money.Money{}, err
	}
	return money.ParseStored(settings.On(def, changes, on).Value, base)
}

func registerPaymentProviderDisputes(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeNotFound}, locks...)

	register(a, authz.ListPaymentProviderDisputes, huma.Operation{
		OperationID: "listPaymentProviderDisputes", Method: http.MethodGet, Path: "/v1/payment-providers/disputes", Tags: []string{"Banking"},
		Summary: "List the payment providers' disputes",
		Description: "Each dispute whose lines are on a provider's clearing account (FR-PSP-04): what its posted lines hold in disputes clearing, whether " +
			"it is open, won or written off, and whether it is above the limit up to which a lost dispute is written off as an expense. Read-only.",
	}, []string{CodeCursor}, func(ctx context.Context, in *PageQuery) (*pspDisputesOutput, error) {
		p, _ := authn.From(ctx)
		out := &pspDisputesOutput{}
		out.Body.Items = []PaymentProviderDispute{}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			o, err := u.Organisation(ctx)
			if err != nil {
				return err
			}
			base, err := money.Lookup(o.BaseCurrency)
			if err != nil {
				return err
			}
			limit, err := disputeLimit(ctx, u, base, a.deps.Clock.Now())
			if err != nil {
				return err
			}
			accounts, err := u.PSP().Accounts(ctx)
			if err != nil {
				return err
			}
			var items []PaymentProviderDispute
			for _, x := range accounts {
				m, err := u.Banking().MoneyAccount(ctx, x.MoneyAccountID)
				if err != nil {
					return err
				}
				cur, err := money.Lookup(m.Currency)
				if err != nil {
					return err
				}
				byRef, err := pspnet.Lines(ctx, u, m)
				if err != nil {
					return err
				}
				ds, err := pspnet.Disputes(ctx, u, x, cur, byRef)
				if err != nil {
					return err
				}
				for _, d := range ds {
					item := PaymentProviderDispute{DisputeID: d.DisputeID, MoneyAccountID: m.ID.String(), Held: Money{Amount: d.Held.String(), Currency: d.Currency},
						Status: d.Status, AllPosted: d.Posted, OverLimit: cur == base && mustCmp(d.Held, limit) > 0}
					if d.LossDocument != nil {
						item.DocumentID = d.LossDocument.DocumentID.String()
					}
					items = append(items, item)
				}
			}
			var next string
			if items, next, err = page(items, func(x PaymentProviderDispute) string { return x.MoneyAccountID + "/" + x.DisputeID }, *in); err != nil {
				return err
			}
			out.Body.Items, out.Body.NextCursor = items, next
			if out.Body.Items == nil {
				out.Body.Items = []PaymentProviderDispute{}
			}
			return nil
		})
		return out, err
	})

	register(a, authz.PrepareDisputeLoss, huma.Operation{
		OperationID: "prepareDisputeLoss", Method: http.MethodPost, Path: "/v1/payment-providers/disputes/{dispute_id}/loss", Tags: []string{"Banking"},
		Summary: "Prepare the journal that writes a lost dispute off", DefaultStatus: http.StatusCreated,
		Description: "Prepares a draft journal for a dispute the provider has lost for the organisation (FR-PSP-04): Dr the provider account's dispute loss " +
			"account, Cr disputes clearing, for what the dispute holds. It posts nothing: the draft is posted under Journals with that row's own approval. " +
			"Up to the limit psp.dispute_expense_limit (S$100 by default) nothing more is needed; above it the request must confirm that the invoice's " +
			"revenue stays as invoiced and give a reason. Once per dispute; every line of the dispute must have posted first. Needs a right on Journals. " +
			"Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append([]string{CodePSPDisputeState, CodePSPDisputeLimit, storage.CodeDuplicate}, writes...), func(ctx context.Context, in *prepareDisputeLossInput) (*disputeLossOutput, error) {
		account, err := parseID("body.money_account_id", in.Body.MoneyAccountID)
		if err != nil {
			return nil, err
		}
		date, bad := optionalDate("body.date", in.Body.Date)
		if bad != nil {
			return nil, invalid(fieldProblems(bad))
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (DisputeLossPrepared, error) {
			refuse := func(detail, fix string) (DisputeLossPrepared, error) {
				return DisputeLossPrepared{}, problem(CodePSPDisputeState, detail, fix)
			}
			x, err := u.PSP().Account(ctx, account)
			if err != nil {
				return DisputeLossPrepared{}, err
			}
			if x.DisputesAccountID == nil || x.DisputeLossAccountID == nil {
				return refuse("The provider account names no disputes account or no dispute loss account.", "Set both on the provider account first")
			}
			m, err := u.Banking().MoneyAccount(ctx, account)
			if err != nil {
				return DisputeLossPrepared{}, err
			}
			cur, err := money.Lookup(m.Currency)
			if err != nil {
				return DisputeLossPrepared{}, err
			}
			o, err := u.Organisation(ctx)
			if err != nil {
				return DisputeLossPrepared{}, err
			}
			base, err := money.Lookup(o.BaseCurrency)
			if err != nil {
				return DisputeLossPrepared{}, err
			}
			if cur != base {
				return refuse("A dispute in "+cur.Code+" is written off by a person with a journal of their own.", "Prepare the journal under Journals")
			}
			byRef, err := pspnet.Lines(ctx, u, m)
			if err != nil {
				return DisputeLossPrepared{}, err
			}
			ds, err := pspnet.Disputes(ctx, u, x, cur, byRef)
			if err != nil {
				return DisputeLossPrepared{}, err
			}
			var d *pspnet.Dispute
			for i := range ds {
				if ds[i].DisputeID == in.DisputeID {
					d = &ds[i]
				}
			}
			switch {
			case d == nil:
				return DisputeLossPrepared{}, problem(CodeNotFound, "The provider account has no such dispute.", "Choose a dispute from the list")
			case d.Status == pspnet.DisputeWrittenOff:
				return DisputeLossPrepared{}, problem(storage.CodeDuplicate, "A draft journal was prepared for this dispute already.", "Post it under Journals")
			case d.Status != pspnet.DisputeOpen:
				return refuse("The dispute holds nothing in disputes clearing.", "A dispute the provider gave back needs nothing")
			case !d.Posted:
				return refuse("Some statement lines of the dispute have not posted.", "Reconcile now, then prepare the journal")
			}
			now := a.deps.Clock.Now()
			limit, err := disputeLimit(ctx, u, base, now)
			if err != nil {
				return DisputeLossPrepared{}, err
			}
			over := mustCmp(d.Held, limit) > 0
			reason := strings.TrimSpace(in.Body.Reason)
			if over && (!in.Body.Accepted || reason == "") {
				return DisputeLossPrepared{}, problem(CodePSPDisputeLimit, "The dispute is above the limit of "+limit.String()+" "+cur.Code+" for writing a lost dispute off as an expense.",
					"Confirm that the invoice's revenue stays as invoiced and say why, or credit the invoice instead", FieldProblem{Code: CodePSPDisputeLimit, Field: "body.accept_overstated_revenue"})
			}
			day := date
			if day == nil {
				t := dayOnly(now)
				day = &t
			}
			memo := "Lost dispute " + in.DisputeID + " written off from disputes clearing (FR-PSP-04)"
			doc, err := journal.CreateIn(ctx, u, journal.Draft{Date: *day, Memo: memo, Lines: []ledger.JournalLine{
				{AccountID: *x.DisputeLossAccountID, Amount: d.Held, Description: memo},
				{AccountID: *x.DisputesAccountID, Amount: d.Held.Neg(), Description: memo}}})
			if err != nil {
				return DisputeLossPrepared{}, err
			}
			if _, err := u.PSP().RecordDisputeLoss(ctx, storage.NewPSPDisputeLoss{ConnectorID: x.ConnectorID, DisputeID: in.DisputeID, DocumentID: doc.ID,
				Currency: cur.Code, Amount: storage.Decimal(d.Held.String()), AcceptedOverstatedRevenue: over, Reason: reason, At: now}); err != nil {
				return DisputeLossPrepared{}, err
			}
			return DisputeLossPrepared{DisputeID: in.DisputeID, DocumentID: doc.ID.String(), Amount: Money{Amount: d.Held.String(), Currency: cur.Code}, OverLimit: over}, nil
		})
		if err != nil {
			return nil, err
		}
		return &disputeLossOutput{Status: w.Status, Applied: applied(w), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})

	register(a, authz.ResolvePaymentProviderEvent, huma.Operation{
		OperationID: "resolvePaymentProviderEvent", Method: http.MethodPost, Path: "/v1/payment-providers/events/{event_id}/resolution", Tags: []string{"Banking"},
		Summary: "Mark a parked provider event as dealt with",
		Description: "Records that a person reviewed an event that was parked or failed (UC-PSP-01) and dealt with it, with what they did or why nothing is needed. " +
			"It posts nothing and fetches nothing: the books follow the provider's statement lines and its sweep. The event keeps its payload, type and object; only " +
			"its standing becomes processed. Side effects: the audit event with the reason. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append([]string{CodePSPEventState}, writes...), func(ctx context.Context, in *resolveEventInput) (*resolveEventOutput, error) {
		id, err := uuid.Parse(in.EventID)
		if err != nil {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "event_id", Message: "is not an ID", Fix: "Send the event's ID"}})
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (PaymentProviderEvent, error) {
			ev, err := u.PSPEvents().Get(ctx, id)
			if err != nil {
				return PaymentProviderEvent{}, err
			}
			if ev.Status != storage.PSPParked && ev.Status != storage.PSPFailed {
				return PaymentProviderEvent{}, problem(CodePSPEventState, "The event is not waiting for a person: it is "+ev.Status+".", "Only a parked or failed event is resolved")
			}
			ctx = storage.WithReason(ctx, strings.TrimSpace(in.Body.Reason))
			done, err := u.PSPEvents().Settle(ctx, id, storage.PSPProcessed, "", a.deps.Clock.Now())
			if err != nil {
				return PaymentProviderEvent{}, err
			}
			return pspEventOf(done), nil
		})
		if err != nil {
			return nil, err
		}
		return &resolveEventOutput{Status: w.Status, Applied: applied(w), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})
}

func mustCmp(a, b money.Money) int {
	c, err := a.Cmp(b)
	if err != nil {
		return 0
	}
	return c
}

func dayOnly(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
