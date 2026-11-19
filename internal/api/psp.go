package api

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/banking"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/pspnet"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The stable codes of the payment providers' setup (ENG-23).
const (
	CodePSPNotProvider = "PSP-01" // the money account is not a payment provider's: its source is not a connector that reads one
	CodePSPPayout      = "PSP-02" // the account payouts land in cannot take them
	CodePSPFee         = "PSP-03" // the account fees expense to cannot take them
	CodePSPDisputes    = "PSP-04" // the account disputes wait in cannot hold them
)

// PaymentProviderAccount is a payment provider's money account as set up (FR-PSP-04..06): the connector that feeds it, where
// its payouts land and what its fees and disputes post to, how far the sweep has made it complete, and what the books and
// the provider each say it holds.
type PaymentProviderAccount struct {
	MoneyAccountID    string `json:"money_account_id" format:"uuid" doc:"The provider's clearing money account." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b10"`
	MoneyAccountName  string `json:"money_account_name" doc:"Its name." example:"Stripe SGD"`
	Currency          string `json:"currency" pattern:"^[A-Z]{3}$" doc:"Its currency, the provider balance's." example:"SGD"`
	ConnectorID       string `json:"connector_id" format:"uuid" doc:"The organisation connector that feeds it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b40"`
	Provider          string `json:"provider" doc:"The connector's manifest ID." example:"stripe"`
	PayoutAccountID   string `json:"payout_account_id,omitempty" format:"uuid" doc:"The money account payouts land in; none: a payout waits for a person." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b11"`
	PayoutAccountName string `json:"payout_account_name,omitempty" doc:"Its name." example:"DBS Operating"`
	FeeAccountID      string `json:"fee_account_id" format:"uuid" doc:"The chart account fees expense to." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b12"`
	FeeAccountName    string `json:"fee_account_name" doc:"Its code and name." example:"5040 Payment processing fees"`
	DisputesAccountID string `json:"disputes_account_id,omitempty" format:"uuid" doc:"The chart account a disputed amount waits in; none: a dispute waits for a person." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b13"`
	DisputesName      string `json:"disputes_account_name,omitempty" doc:"Its code and name." example:"1055 Payment disputes clearing"`
	SweptThrough      string `json:"swept_through,omitempty" format:"date" doc:"The day the daily sweep has made the account complete to (FR-PSP-01)." example:"2026-11-18"`
	SweepError        string `json:"sweep_error,omitempty" doc:"Why the last sweep could not close a gap (C.2 #43)." example:"the provider answered 503"`
	ProviderBalance   *Money `json:"provider_balance,omitempty" doc:"What the provider said it holds for the organisation and has not paid out, at the last sweep."`
	BookBalance       *Money `json:"book_balance,omitempty" doc:"What the books hold in the clearing account."`
	Version           int    `json:"version" doc:"Its version, for If-Match." example:"1"`
}

// PaymentProviderAccountDraft is a provider account's setup as written.
type PaymentProviderAccountDraft struct {
	PayoutAccountID   string `json:"payout_account_id,omitempty" format:"uuid" doc:"The money account payouts land in, in the same currency; left out, a payout waits for a person." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b11"`
	FeeAccountID      string `json:"fee_account_id" format:"uuid" doc:"The expense chart account fees post to, such as 5040." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b12"`
	DisputesAccountID string `json:"disputes_account_id,omitempty" format:"uuid" doc:"A chart account, not a money account, a disputed amount waits in, such as 1055; left out, a dispute waits for a person." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b13"`
}

// PaymentProviderTxn is a balance transaction a payout settles, and whether its lines have posted.
type PaymentProviderTxn struct {
	TxnID    string `json:"txn_id" doc:"The provider's ID, the reference of its statement line." example:"txn_1Q3"`
	Category string `json:"category" enum:"charge,refund,dispute,dispute_reversal,fee,payout,other" doc:"How Fyniq reads it." example:"charge"`
	Gross    Money  `json:"gross" doc:"Signed: money in positive."`
	Fee      Money  `json:"fee" doc:"What the provider keeps."`
	Net      Money  `json:"net" doc:"Gross less fee."`
	Posted   bool   `json:"posted" doc:"Whether its line, and its fee's line when it has one, have posted." example:"true"`
}

// PaymentProviderPayout is a payout, what it settles and whether it nets to zero in clearing (check C.2 #42).
type PaymentProviderPayout struct {
	ID               string               `json:"id" format:"uuid" doc:"The payout's ID in Fyniq." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b60"`
	PayoutID         string               `json:"payout_id" doc:"The provider's ID." example:"po_1Q3"`
	MoneyAccountID   string               `json:"money_account_id" format:"uuid" doc:"The provider's clearing money account." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b10"`
	Amount           Money                `json:"amount" doc:"Paid out, as the provider reports it."`
	Status           string               `json:"status" doc:"The provider's status." example:"paid"`
	ArrivalDate      string               `json:"arrival_date,omitempty" format:"date" doc:"When it was due in the bank." example:"2026-11-12"`
	Automatic        bool                 `json:"automatic" doc:"Whether it is an automatic payout, which Stripe itemises (FR-PSP-06)." example:"true"`
	Transactions     []PaymentProviderTxn `json:"transactions" nullable:"false" doc:"What it settles."`
	PayoutLinePosted bool                 `json:"payout_line_posted" doc:"Whether the payout's own statement line has posted, as a transfer to the bank." example:"true"`
	NetsToZero       *bool                `json:"nets_to_zero,omitempty" doc:"Whether the payout and what it settles come to nothing in clearing; absent until the payout's own line has posted." example:"true"`
	Difference       *Money               `json:"difference,omitempty" doc:"What they come to, when not nothing."`
	Missing          []string             `json:"missing,omitempty" doc:"References of lines that are not imported or not posted."`
}

// PaymentProviderEvent is a webhook event as received, without its payload.
type PaymentProviderEvent struct {
	ID            string `json:"id" format:"uuid" doc:"The event's ID in Fyniq." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b70"`
	ConnectorID   string `json:"connector_id" format:"uuid" doc:"The connector it came through." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b40"`
	Provider      string `json:"provider" doc:"The connector's manifest ID." example:"stripe"`
	EventID       string `json:"event_id" doc:"The provider's event ID." example:"evt_1Q3"`
	EventType     string `json:"event_type" doc:"Its type." example:"payout.paid"`
	APIVersion    string `json:"api_version,omitempty" doc:"The provider's API version." example:"2025-09-30.clover"`
	ParserVersion string `json:"parser_version" doc:"The parser's version." example:"stripe@1"`
	ObjectKind    string `json:"object_kind,omitempty" doc:"What the event names." example:"payout"`
	ObjectID      string `json:"object_id,omitempty" doc:"The object's provider ID." example:"po_1Q3"`
	Status        string `json:"status" enum:"received,processed,parked,failed" doc:"Where it stands: parked waits for a person (an event type Fyniq does not know, or an object it cannot post)." example:"parked"`
	ParkedReason  string `json:"parked_reason,omitempty" doc:"Why it is parked." example:"Fyniq does not know this event type (radar.early_fraud_warning.created): a person reviews it, and it posts nothing"`
	LastError     string `json:"last_error,omitempty" doc:"Why its last attempt failed."`
	ReceivedAt    string `json:"received_at" format:"date-time" doc:"When it was received." example:"2026-11-12T10:00:00Z"`
}

type pspAccountsOutput struct {
	Body struct {
		Items      []PaymentProviderAccount `json:"items" nullable:"false" doc:"The provider accounts, by money account name."`
		NextCursor string                   `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjAxOTIifQ"`
	}
}

type setPSPAccountInput struct {
	MoneyAccountPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body PaymentProviderAccountDraft
}

type pspAccountOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The provider account's version, for If-Match."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     PaymentProviderAccount
}

type pspPayoutsInput struct {
	PageQuery
	MoneyAccountID string `query:"money_account_id" format:"uuid" doc:"Only this provider account's payouts." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b10"`
}

type pspPayoutsOutput struct {
	Body struct {
		Items      []PaymentProviderPayout `json:"items" nullable:"false" doc:"The payouts, newest first."`
		NextCursor string                  `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjAxOTIifQ"`
	}
}

type pspEventsInput struct {
	PageQuery
	Status string `query:"status" enum:"received,processed,parked,failed" doc:"Only events in this state; parked is what a person reviews." example:"parked"`
}

type pspEventsOutput struct {
	Body struct {
		Items      []PaymentProviderEvent `json:"items" nullable:"false" doc:"The events, newest first, without their payloads."`
		NextCursor string                 `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjAxOTIifQ"`
	}
}

// pspConnectorOf is the payment-provider connector a money account's source in force names, and its manifest ID.
func pspConnectorOf(ctx context.Context, a *API, u storage.UnitOfWork, m storage.MoneyAccount) (uuid.UUID, string, error) {
	notProvider := problem(CodePSPNotProvider, "The money account is not a payment provider's.",
		"Choose a payment gateway money account whose data source is a Stripe or HitPay connector, set from Banking › the account's source")
	if m.Kind != storage.MoneyPaymentGateway {
		return uuid.Nil, "", notProvider
	}
	sources, err := u.Banking().Sources(ctx, m.ID)
	if err != nil {
		return uuid.Nil, "", err
	}
	if len(sources) == 0 || sources[len(sources)-1].Kind != storage.MoneySourceConnector || sources[len(sources)-1].ConnectorID == nil {
		return uuid.Nil, "", notProvider
	}
	cid := *sources[len(sources)-1].ConnectorID
	all, err := u.Connectors().Organisation(ctx)
	if err != nil {
		return uuid.Nil, "", err
	}
	for _, c := range all {
		if c.ID == cid {
			if b, ok := a.deps.Connectors.Catalogue.Get(c.Connector); ok && b.PaymentProvider != nil {
				return cid, c.Connector, nil
			}
		}
	}
	return uuid.Nil, "", notProvider
}

func pspAccountOf(ctx context.Context, a *API, u storage.UnitOfWork, x storage.PSPAccount, base money.Currency) (PaymentProviderAccount, error) {
	m, err := u.Banking().MoneyAccount(ctx, x.MoneyAccountID)
	if err != nil {
		return PaymentProviderAccount{}, err
	}
	out := PaymentProviderAccount{MoneyAccountID: m.ID.String(), MoneyAccountName: m.Name, Currency: m.Currency, ConnectorID: x.ConnectorID.String(),
		FeeAccountID: x.FeeAccountID.String(), SweepError: x.SweepError, Version: x.Version}
	all, err := u.Connectors().Organisation(ctx)
	if err != nil {
		return out, err
	}
	for _, c := range all {
		if c.ID == x.ConnectorID {
			out.Provider = c.Connector
		}
	}
	name := func(id uuid.UUID) (string, error) {
		acc, err := u.Accounts().Get(ctx, id)
		return strings.TrimSpace(acc.Code + " " + acc.Name), err
	}
	if out.FeeAccountName, err = name(x.FeeAccountID); err != nil {
		return out, err
	}
	if x.PayoutAccountID != nil {
		pa, err := u.Banking().MoneyAccount(ctx, *x.PayoutAccountID)
		if err != nil {
			return out, err
		}
		out.PayoutAccountID, out.PayoutAccountName = pa.ID.String(), pa.Name
	}
	if x.DisputesAccountID != nil {
		out.DisputesAccountID = x.DisputesAccountID.String()
		if out.DisputesName, err = name(*x.DisputesAccountID); err != nil {
			return out, err
		}
	}
	if x.SweptThrough != nil {
		out.SweptThrough = dateOf(*x.SweptThrough)
	}
	cur, err := money.Lookup(m.Currency)
	if err != nil {
		return out, err
	}
	if pb, err := u.PSP().LatestBalance(ctx, x.ConnectorID, m.Currency); err == nil {
		v := moneyOf(pb.Amount, m.Currency)
		out.ProviderBalance = &v
	} else if !errors.Is(err, ports.ErrNotFound) {
		return out, err
	}
	book, _, err := banking.BookBalance(ctx, u, m.AccountID, cur, base, a.deps.Clock.Now())
	if err != nil {
		return out, err
	}
	out.BookBalance = &Money{Amount: book.String(), Currency: m.Currency}
	return out, nil
}

func registerPaymentProviders(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid,
		CodeNotFound, CodeStale, CodeIfMatchRequired, storage.CodeVersion, CodePSPNotProvider, CodePSPPayout, CodePSPFee, CodePSPDisputes}, locks...)

	register(a, authz.ListPaymentProviderAccounts, huma.Operation{
		OperationID: "listPaymentProviderAccounts", Method: http.MethodGet, Path: "/v1/payment-providers/accounts", Tags: []string{"Banking"},
		Summary: "List the payment providers' accounts",
		Description: "Each payment provider's money account that has been set up (FR-PSP-04..06): its connector, where its payouts land, " +
			"the accounts its fees and disputes post to, how far the daily sweep has made it complete to, and what the books and the " +
			"provider each say it holds (check C.2 #50). Read-only.",
	}, []string{CodeCursor}, func(ctx context.Context, in *PageQuery) (*pspAccountsOutput, error) {
		p, _ := authn.From(ctx)
		out := &pspAccountsOutput{}
		out.Body.Items = []PaymentProviderAccount{}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			o, err := u.Organisation(ctx)
			if err != nil {
				return err
			}
			base, err := money.Lookup(o.BaseCurrency)
			if err != nil {
				return err
			}
			all, err := u.PSP().Accounts(ctx)
			if err != nil {
				return err
			}
			for _, x := range all {
				item, err := pspAccountOf(ctx, a, u, x, base)
				if err != nil {
					return err
				}
				out.Body.Items = append(out.Body.Items, item)
			}
			slices.SortFunc(out.Body.Items, func(x, y PaymentProviderAccount) int { return strings.Compare(x.MoneyAccountName, y.MoneyAccountName) })
			if out.Body.Items, out.Body.NextCursor, err = page(out.Body.Items, func(x PaymentProviderAccount) string { return x.MoneyAccountID }, *in); err != nil {
				return err
			}
			return nil
		})
		return out, err
	})

	register(a, authz.SetPaymentProviderAccount, huma.Operation{
		OperationID: "setPaymentProviderAccount", Method: http.MethodPut, Path: "/v1/payment-providers/accounts/{money_account_id}", Tags: []string{"Banking"},
		Summary: "Set up a payment provider's money account",
		Description: "Sets up, or changes, a payment gateway money account fed by a Stripe or HitPay connector (FR-PSP-04..06): the money " +
			"account its payouts land in, the expense account its fees post to and the account a disputed amount waits in. Once it is set " +
			"up, Reconcile now posts the provider's own lines from it, within the caps of auto-reconcile: a fee, a dispute's money and a payout " +
			"as a transfer; the worker posts nothing. Needs a right on \"Bank data… connections\", as setting a connector as a source does. A " +
			"first setup needs no If-Match; a change needs the ETag. Side effects: the setup and its audit event with the old and new " +
			"accounts. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, writes, func(ctx context.Context, in *setPSPAccountInput) (*pspAccountOutput, error) {
		id, err := parseID("money_account_id", in.MoneyAccountID)
		if err != nil {
			return nil, err
		}
		payout, bad := optionalID("body.payout_account_id", in.Body.PayoutAccountID)
		disputes, bad2 := optionalID("body.disputes_account_id", in.Body.DisputesAccountID)
		fee, bad3 := optionalID("body.fee_account_id", in.Body.FeeAccountID)
		if bad != nil || bad2 != nil || bad3 != nil || fee == nil {
			problems := fieldProblems(bad, bad2, bad3)
			if fee == nil && bad3 == nil {
				problems = append(problems, FieldProblem{Code: CodeInvalid, Field: "body.fee_account_id", Message: "is missing", Fix: "Choose the expense account fees post to, such as 5040"})
			}
			return nil, invalid(problems)
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (PaymentProviderAccount, error) {
			m, err := u.Banking().MoneyAccount(ctx, id)
			if err != nil {
				return PaymentProviderAccount{}, err
			}
			connector, _, err := pspConnectorOf(ctx, a, u, m)
			if err != nil {
				return PaymentProviderAccount{}, err
			}
			if payout != nil {
				pa, err := u.Banking().MoneyAccount(ctx, *payout)
				if errors.Is(err, ports.ErrNotFound) || (err == nil && (pa.Currency != m.Currency || pa.ID == m.ID || pa.Kind == storage.MoneyPaymentGateway || pa.ClosedOn != nil)) {
					return PaymentProviderAccount{}, problem(CodePSPPayout, "The account payouts land in cannot take them.",
						"Choose an open bank, card or wallet money account in "+m.Currency, FieldProblem{Code: CodePSPPayout, Field: "body.payout_account_id"})
				} else if err != nil {
					return PaymentProviderAccount{}, err
				}
			}
			facc, err := u.Accounts().Get(ctx, *fee)
			if errors.Is(err, ports.ErrNotFound) || (err == nil && (facc.Type != storage.Expense || facc.Group || !facc.Active || facc.MoneyAccount)) {
				return PaymentProviderAccount{}, problem(CodePSPFee, "The account fees post to is not an expense account that takes postings.",
					"Choose an active expense account, such as 5040 Payment processing fees", FieldProblem{Code: CodePSPFee, Field: "body.fee_account_id"})
			} else if err != nil {
				return PaymentProviderAccount{}, err
			}
			if disputes != nil {
				dacc, err := u.Accounts().Get(ctx, *disputes)
				if errors.Is(err, ports.ErrNotFound) || (err == nil && (dacc.Group || !dacc.Active || dacc.MoneyAccount || (dacc.Type != storage.Asset && dacc.Type != storage.Liability))) {
					return PaymentProviderAccount{}, problem(CodePSPDisputes, "The account a dispute waits in cannot hold it.",
						"Choose an active asset or liability account that is not a money account, such as 1055 Payment disputes clearing",
						FieldProblem{Code: CodePSPDisputes, Field: "body.disputes_account_id"})
				} else if err != nil {
					return PaymentProviderAccount{}, err
				}
			}
			version := 0
			if cur, err := u.PSP().Account(ctx, id); err == nil {
				version = cur.Version
				if err := checkIfMatch(in.IfMatch, etag(cur.Version)); err != nil {
					return PaymentProviderAccount{}, err
				}
			} else if !errors.Is(err, ports.ErrNotFound) {
				return PaymentProviderAccount{}, err
			}
			saved, err := u.PSP().SaveAccount(ctx, storage.NewPSPAccount{MoneyAccountID: id, ConnectorID: connector, PayoutAccountID: payout, FeeAccountID: *fee,
				DisputesAccountID: disputes, At: a.deps.Clock.Now()}, version)
			if err != nil {
				return PaymentProviderAccount{}, err
			}
			o, err := u.Organisation(ctx)
			if err != nil {
				return PaymentProviderAccount{}, err
			}
			base, err := money.Lookup(o.BaseCurrency)
			if err != nil {
				return PaymentProviderAccount{}, err
			}
			return pspAccountOf(ctx, a, u, saved, base)
		})
		if err != nil {
			return nil, err
		}
		out := &pspAccountOutput{Status: w.Status, Applied: applied(w), Replayed: replayedHeader(w.Replayed), Body: w.Body}
		if !w.Previewed {
			out.ETag = etag(w.Body.Version)
		}
		return out, nil
	})

	register(a, authz.ListPaymentProviderPayouts, huma.Operation{
		OperationID: "listPaymentProviderPayouts", Method: http.MethodGet, Path: "/v1/payment-providers/payouts", Tags: []string{"Banking"},
		Summary: "List the payment providers' payouts",
		Description: "Each payout a provider's connector has reported, newest first, with the balance transactions it settles and whether " +
			"their lines have posted (FR-PSP-06). A payout whose own line has posted says whether it and what it settles come to nothing in " +
			"the provider's clearing account, the same check as C.2 #42. Read-only.",
	}, []string{CodeCursor, CodeNotFound}, func(ctx context.Context, in *pspPayoutsInput) (*pspPayoutsOutput, error) {
		p, _ := authn.From(ctx)
		out := &pspPayoutsOutput{}
		out.Body.Items = []PaymentProviderPayout{}
		only, err := optionalID("money_account_id", in.MoneyAccountID)
		if err != nil {
			return nil, invalid(fieldProblems(err))
		}
		e := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			accounts, err := u.PSP().Accounts(ctx)
			if err != nil {
				return err
			}
			var items []PaymentProviderPayout
			for _, x := range accounts {
				if only != nil && x.MoneyAccountID != *only {
					continue
				}
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
				payouts, err := u.PSP().Payouts(ctx, x.ConnectorID, 500)
				if err != nil {
					return err
				}
				for _, po := range payouts {
					if po.Currency != m.Currency {
						continue
					}
					r, err := pspnet.Of(ctx, u, x, cur, po, byRef)
					if err != nil {
						return err
					}
					items = append(items, pspPayoutOf(m, cur, r))
				}
			}
			slices.SortStableFunc(items, func(x, y PaymentProviderPayout) int {
				return strings.Compare(y.ArrivalDate+y.PayoutID, x.ArrivalDate+x.PayoutID)
			})
			var next string
			if items, next, err = page(items, func(x PaymentProviderPayout) string { return x.ID }, in.PageQuery); err != nil {
				return err
			}
			out.Body.Items, out.Body.NextCursor = items, next
			if out.Body.Items == nil {
				out.Body.Items = []PaymentProviderPayout{}
			}
			return nil
		})
		return out, e
	})

	register(a, authz.ListPaymentProviderEvents, huma.Operation{
		OperationID: "listPaymentProviderEvents", Method: http.MethodGet, Path: "/v1/payment-providers/events", Tags: []string{"Banking"},
		Summary: "List the payment providers' webhook events",
		Description: "The events the providers' signed webhooks brought, newest first, without their payloads (which are sealed): their " +
			"type, the object they name, and where each stands. A parked event is one Fyniq does not know or could not post, for a person to " +
			"review (UC-PSP-01); it posts nothing. Read-only.",
	}, []string{CodeCursor}, func(ctx context.Context, in *pspEventsInput) (*pspEventsOutput, error) {
		p, _ := authn.From(ctx)
		out := &pspEventsOutput{}
		out.Body.Items = []PaymentProviderEvent{}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			evs, err := u.PSPEvents().List(ctx, storage.PSPEventFilter{Status: in.Status, Limit: 1000})
			if err != nil {
				return err
			}
			var next string
			if evs, next, err = page(evs, func(e storage.PSPEvent) string { return e.ID.String() }, in.PageQuery); err != nil {
				return err
			}
			out.Body.NextCursor = next
			for _, e := range evs {
				ev := PaymentProviderEvent{ID: e.ID.String(), ConnectorID: e.ConnectorID.String(), Provider: e.Provider, EventID: e.EventID, EventType: e.EventType,
					APIVersion: e.APIVersion, ParserVersion: e.ParserVersion, ObjectKind: e.ObjectKind, ObjectID: e.ObjectID, Status: e.Status,
					ParkedReason: e.ParkedReason, LastError: e.LastError, ReceivedAt: e.ReceivedAt.UTC().Format("2006-01-02T15:04:05Z")}
				out.Body.Items = append(out.Body.Items, ev)
			}
			return nil
		})
		return out, err
	})
}

func pspPayoutOf(m storage.MoneyAccount, cur money.Currency, r pspnet.Result) PaymentProviderPayout {
	po := r.Payout
	out := PaymentProviderPayout{ID: po.ID.String(), PayoutID: po.PayoutID, MoneyAccountID: m.ID.String(), Amount: moneyOf(po.Amount, po.Currency), Status: po.Status,
		Automatic: po.Automatic, Transactions: []PaymentProviderTxn{}, PayoutLinePosted: r.PayoutLine != nil && pspnet.Posted(r.PayoutLine.Status), Missing: r.Missing}
	if po.ArrivalDate != nil {
		out.ArrivalDate = dateOf(*po.ArrivalDate)
	}
	for _, t := range r.Txns {
		posted := t.Gross != nil && pspnet.Posted(t.Gross.Status)
		if f, err := money.ParseStored(string(t.PSPTxn.Fee), cur); err == nil && !f.IsZero() {
			posted = posted && t.Fee != nil && pspnet.Posted(t.Fee.Status)
		}
		out.Transactions = append(out.Transactions, PaymentProviderTxn{TxnID: t.TxnID, Category: t.Category, Gross: moneyOf(t.PSPTxn.Gross, t.Currency),
			Fee: moneyOf(t.PSPTxn.Fee, t.Currency), Net: moneyOf(t.PSPTxn.Net, t.Currency), Posted: posted})
	}
	if r.Evaluated {
		nets := r.Nets()
		out.NetsToZero = &nets
		if !r.Total.IsZero() {
			d := Money{Amount: r.Total.String(), Currency: po.Currency}
			out.Difference = &d
		}
	}
	return out
}
