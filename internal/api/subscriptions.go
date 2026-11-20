package api

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/subscription"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ subscription contracts and their amendments (FR-AR-15, FR-KPI-02)
//
// A contract and its dated amendments are records, not postings: the schedule and the contracted MRR are derived from them on every read.

// SubscriptionAmendment is a dated change of a contract's monthly value.
type SubscriptionAmendment struct {
	ID          string    `json:"id" format:"uuid" doc:"The amendment's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5a02"`
	Kind        string    `json:"kind" enum:"upgrade,downgrade,cancel,renew" doc:"What changed: the monthly value rose or fell, the subscription was cancelled, or it started again after it ended." example:"upgrade"`
	EffectiveOn string    `json:"effective_on" format:"date" doc:"The first day of the new monthly value; for a cancellation, the first day with no subscription." example:"2026-04-01"`
	MRR         Money     `json:"mrr" doc:"The monthly value from that day; zero for a cancellation."`
	Reason      string    `json:"reason" doc:"Why it changed." example:"The customer moved to the larger plan"`
	CreatedAt   time.Time `json:"created_at" doc:"When it was recorded." example:"2026-03-20T09:30:00Z"`
}

// SubscriptionMonth is one month of a contract's schedule.
type SubscriptionMonth struct {
	Month     string                 `json:"month" format:"date" doc:"The month's first day." example:"2026-04-01"`
	Status    string                 `json:"status" enum:"not_started,active,ended" doc:"What the contract is on the month's last day." example:"active"`
	MRR       Money                  `json:"mrr" doc:"The monthly value in force on the month's last day: the contract's contracted MRR at month end (FR-KPI-02)."`
	Amendment *SubscriptionAmendment `json:"amendment,omitempty" doc:"The amendment that took effect in the month, the last if several."`
}

// SubscriptionContract is a customer's subscription.
type SubscriptionContract struct {
	ID         string `json:"id" format:"uuid" doc:"The contract's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5a01"`
	ContactID  string `json:"contact_id" format:"uuid" doc:"The customer." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4db0"`
	Name       string `json:"name" doc:"What the customer subscribes to." example:"Fyniq Growth plan"`
	StartsOn   string `json:"starts_on" format:"date" doc:"The first day." example:"2026-01-15"`
	EndsOn     string `json:"ends_on,omitempty" format:"date" doc:"The agreed last day, when there is one; a renewal continues past it." example:"2026-12-31"`
	InitialMRR Money  `json:"initial_mrr" doc:"The monthly value at the start."`
	Status     string `json:"status" enum:"not_started,active,ended" doc:"What it is today." example:"active"`
	MRR        Money  `json:"mrr" doc:"The monthly value in force today; zero unless active."`
	Version    int    `json:"version" doc:"The contract's version: it moves with each amendment, and makes the ETag." example:"1"`
}

// SubscriptionContractDetail is a contract with its amendments and its schedule.
type SubscriptionContractDetail struct {
	SubscriptionContract
	Amendments []SubscriptionAmendment `json:"amendments" nullable:"false" doc:"Its amendments, by effective day."`
	Schedule   []SubscriptionMonth     `json:"schedule" nullable:"false" doc:"Month by month from the month it starts through the month of the later of today and its last change (at most 120 months)."`
}

// SubscriptionContractPage is a page of contracts.
type SubscriptionContractPage struct {
	Items      []SubscriptionContract `json:"items" nullable:"false"`
	NextCursor string                 `json:"next_cursor,omitempty" doc:"Pass it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjEwMCJ9"`
}

// ContractedMRR is the monthly value of the active contracts on a day, in one currency.
type ContractedMRR struct {
	Currency string `json:"currency" doc:"The currency: totals are not converted." example:"SGD"`
	MRR      Money  `json:"mrr" doc:"The monthly value of the contracts active that day."`
	Active   int    `json:"active" doc:"How many contracts are active." example:"12"`
}

func subscriptionAmendmentOf(a storage.SubscriptionAmendment, currency string) SubscriptionAmendment {
	return SubscriptionAmendment{ID: a.ID.String(), Kind: a.Kind, EffectiveOn: dateOf(a.EffectiveOn), MRR: moneyOf(a.MRR, currency), Reason: a.Reason, CreatedAt: a.CreatedAt.UTC()}
}

func subscriptionOf(c storage.SubscriptionContract, ams []storage.SubscriptionAmendment, today time.Time) (SubscriptionContract, error) {
	s, err := subscription.On(c, ams, today)
	if err != nil {
		return SubscriptionContract{}, err
	}
	out := SubscriptionContract{ID: c.ID.String(), ContactID: c.ContactID.String(), Name: c.Name, StartsOn: dateOf(c.StartsOn), InitialMRR: moneyOf(c.MRR, c.Currency),
		Status: s.Status, MRR: Money{Amount: s.MRR.String(), Currency: c.Currency}, Version: c.Version}
	if c.EndsOn != nil {
		out.EndsOn = dateOf(*c.EndsOn)
	}
	return out, nil
}

type SubscriptionContractPath struct {
	ContractID string `path:"contract_id" format:"uuid" doc:"The contract's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5a01"`
}

type createSubscriptionInput struct {
	KeyHeader
	PreferHeader
	Body struct {
		ContactID string `json:"contact_id" format:"uuid" doc:"The customer: an active contact marked as a customer." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4db0"`
		Name      string `json:"name" minLength:"1" maxLength:"140" doc:"What the customer subscribes to." example:"Fyniq Growth plan"`
		Currency  string `json:"currency" pattern:"^[A-Z]{3}$" doc:"The contract's currency." example:"SGD"`
		StartsOn  string `json:"starts_on" format:"date" doc:"The first day." example:"2026-01-15"`
		EndsOn    string `json:"ends_on,omitempty" format:"date" doc:"The agreed last day, if any." example:"2026-12-31"`
		MRR       string `json:"mrr" pattern:"^(0|[1-9][0-9]{0,15})(\\.[0-9]{1,4})?$" doc:"The monthly value at the start, a decimal string." example:"100.00"`
	}
}

type subscriptionOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The contract's version, for If-Match."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     SubscriptionContractDetail
}

type listSubscriptionsInput struct {
	PageQuery
	ContactID string `query:"contact_id" format:"uuid" doc:"Only this customer's contracts." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4db0"`
}

type listSubscriptionsOutput struct{ Body SubscriptionContractPage }

type getSubscriptionOutput struct {
	ETag string `header:"ETag" doc:"The contract's version, for If-Match."`
	Body SubscriptionContractDetail
}

type amendSubscriptionInput struct {
	SubscriptionContractPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body struct {
		Kind        string `json:"kind" enum:"upgrade,downgrade,cancel,renew" doc:"What changes." example:"upgrade"`
		EffectiveOn string `json:"effective_on" format:"date" doc:"The first day of the change: after the contract's last amendment and in an open month." example:"2026-04-01"`
		MRR         string `json:"mrr,omitempty" pattern:"^(0|[1-9][0-9]{0,15})(\\.[0-9]{1,4})?$" doc:"The new monthly value from that day, above nothing; left out for a cancellation." example:"150.00"`
		Reason      string `json:"reason" minLength:"1" maxLength:"500" doc:"Why it changes." example:"The customer moved to the larger plan"`
	}
}

type contractedMRRInput struct {
	AsAt string `query:"as_at" format:"date" doc:"The day; today when left out." example:"2026-10-31"`
}

type contractedMRROutput struct {
	Body struct {
		AsAt   string          `json:"as_at" format:"date" doc:"The day." example:"2026-10-31"`
		Totals []ContractedMRR `json:"totals" nullable:"false" doc:"The monthly value of the contracts active that day, by currency (contracted MRR, FR-KPI-02); nothing is converted."`
	}
}

// contractDetailOf is a contract with its amendments and its schedule through the later of today and its last change.
func contractDetailOf(ctx context.Context, u storage.UnitOfWork, c storage.SubscriptionContract, today time.Time) (SubscriptionContractDetail, error) {
	ams, err := u.Subscriptions().Amendments(ctx, c.ID)
	if err != nil {
		return SubscriptionContractDetail{}, err
	}
	head, err := subscriptionOf(c, ams, today)
	if err != nil {
		return SubscriptionContractDetail{}, err
	}
	out := SubscriptionContractDetail{SubscriptionContract: head, Amendments: []SubscriptionAmendment{}, Schedule: []SubscriptionMonth{}}
	through := today
	for _, a := range ams {
		out.Amendments = append(out.Amendments, subscriptionAmendmentOf(a, c.Currency))
		if a.EffectiveOn.After(through) {
			through = a.EffectiveOn
		}
	}
	if c.EndsOn != nil && c.EndsOn.After(through) {
		through = *c.EndsOn
	}
	if limit := c.StartsOn.AddDate(10, 0, 0); through.After(limit) {
		through = limit
	}
	months, err := subscription.Schedule(c, ams, through)
	if err != nil {
		return SubscriptionContractDetail{}, err
	}
	for _, m := range months {
		row := SubscriptionMonth{Month: dateOf(m.Month), Status: m.Status, MRR: Money{Amount: m.MRR.String(), Currency: c.Currency}}
		if m.Amendment != nil {
			am := subscriptionAmendmentOf(*m.Amendment, c.Currency)
			row.Amendment = &am
		}
		out.Schedule = append(out.Schedule, row)
	}
	return out, nil
}

func registerSubscriptions(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeNotFound}, locks...)
	termCodes := []string{subscription.CodeContact, subscription.CodeTerms}
	amendCodes := []string{subscription.CodeKind, subscription.CodeDate, subscription.CodeAmount, subscription.CodeReason, CodeStale, CodeIfMatchRequired}

	register(a, authz.CreateSubscriptionContract, exempt(huma.Operation{
		OperationID: "createSubscriptionContract", Method: http.MethodPost, Path: "/v1/subscription-contracts", Tags: []string{"Sales"}, DefaultStatus: http.StatusCreated,
		Summary: "Record a customer's subscription contract",
		Description: "Records a subscription of an active customer: its name, currency, first day, an optional agreed last day and the monthly value at the start " +
			"(FR-AR-15). A contract is a record, not a posting: invoices and credit notes post through their own rules, and the schedule and the contracted MRR are " +
			"derived from it and its amendments. It never changes; a change is an amendment (amendSubscriptionContract). Idempotent with Idempotency-Key; " +
			"Prefer: return=preview records nothing.",
	}, "If-Match", "A new contract has no earlier version (FR-AR-15)."), append(termCodes, writes...),
		func(ctx context.Context, in *createSubscriptionInput) (*subscriptionOutput, error) {
			contact, err := parseID("body.contact_id", in.Body.ContactID)
			if err != nil {
				return nil, err
			}
			starts, err := time.Parse(time.DateOnly, in.Body.StartsOn)
			if err != nil {
				return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.starts_on", Message: "is not a date", Fix: "Use a date such as 2026-01-15"}})
			}
			req := subscription.Request{ContactID: contact, Name: in.Body.Name, Currency: in.Body.Currency, StartsOn: starts, MRR: in.Body.MRR}
			if in.Body.EndsOn != "" {
				ends, err := time.Parse(time.DateOnly, in.Body.EndsOn)
				if err != nil {
					return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.ends_on", Message: "is not a date", Fix: "Use a date such as 2026-12-31"}})
				}
				req.EndsOn = &ends
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (SubscriptionContractDetail, error) {
				today, err := orgToday(ctx, a, u)
				if err != nil {
					return SubscriptionContractDetail{}, err
				}
				c, err := subscription.Make(ctx, u, req, a.deps.Clock.Now())
				if err != nil {
					return SubscriptionContractDetail{}, err
				}
				return contractDetailOf(ctx, u, c, today)
			})
			if err != nil {
				return nil, err
			}
			return &subscriptionOutput{Status: w.Status, ETag: etag(w.Body.Version), Applied: applied(w), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})

	register(a, authz.ListSubscriptionContracts, huma.Operation{
		OperationID: "listSubscriptionContracts", Method: http.MethodGet, Path: "/v1/subscription-contracts", Tags: []string{"Sales"},
		Summary: "List subscription contracts",
		Description: "The organisation's subscription contracts, oldest first, a page at a time, optionally one customer's (FR-AR-15), each with its status and monthly " +
			"value today. Read-only.",
	}, []string{CodeCursor, CodeNotFound}, func(ctx context.Context, in *listSubscriptionsInput) (*listSubscriptionsOutput, error) {
		cur, err := decodeCursor(in.Cursor)
		if err != nil {
			return nil, err
		}
		after := uuid.Nil
		if cur != "" {
			if after, err = uuid.Parse(cur); err != nil {
				return nil, problem(CodeCursor, "The cursor is not one this list gave.", "Start again without a cursor.")
			}
		}
		contact := uuid.Nil
		if in.ContactID != "" {
			if contact, err = parseID("contact_id", in.ContactID); err != nil {
				return nil, err
			}
		}
		p, _ := authn.From(ctx)
		out := listSubscriptionsOutput{Body: SubscriptionContractPage{Items: []SubscriptionContract{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return err
			}
			page, err := u.Subscriptions().List(ctx, contact, after, in.PageSize+1)
			if err != nil {
				return err
			}
			if len(page) > in.PageSize {
				page = page[:in.PageSize]
				out.Body.NextCursor = encodeCursor(page[len(page)-1].ID.String())
			}
			for _, c := range page {
				ams, err := u.Subscriptions().Amendments(ctx, c.ID)
				if err != nil {
					return err
				}
				row, err := subscriptionOf(c, ams, today)
				if err != nil {
					return err
				}
				out.Body.Items = append(out.Body.Items, row)
			}
			return nil
		})
		return &out, err
	})

	register(a, authz.GetSubscriptionContract, huma.Operation{
		OperationID: "getSubscriptionContract", Method: http.MethodGet, Path: "/v1/subscription-contracts/{contract_id}", Tags: []string{"Sales"},
		Summary: "Read a subscription contract with its amendments and schedule",
		Description: "The contract, its amendments by effective day, and its schedule month by month: the monthly value in force on each month's last day, which is its " +
			"contracted MRR at month end (FR-AR-15, FR-KPI-02). Derived on every read. Read-only.",
	}, []string{CodeNotFound}, func(ctx context.Context, in *SubscriptionContractPath) (*getSubscriptionOutput, error) {
		id, err := parseID("contract_id", in.ContractID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out getSubscriptionOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return err
			}
			c, err := u.Subscriptions().Get(ctx, id)
			if err != nil {
				return err
			}
			out.Body, err = contractDetailOf(ctx, u, c, today)
			out.ETag = etag(c.Version)
			return err
		})
		return &out, err
	})

	register(a, authz.AmendSubscriptionContract, huma.Operation{
		OperationID: "amendSubscriptionContract", Method: http.MethodPost, Path: "/v1/subscription-contracts/{contract_id}/amendments", Tags: []string{"Sales"},
		DefaultStatus: http.StatusCreated, Summary: "Amend a subscription contract from a day",
		Description: "Records a dated amendment (FR-AR-15): an upgrade or downgrade sets a new monthly value from its day, a cancellation ends the subscription on its day, " +
			"and a renewal starts it again after it ended. The day is on or after the contract's start, after its last amendment, and in an open month: a closed " +
			"month's figures are never rewritten (FR-GL-12). The accounting of the invoices and credit notes it causes follows the revenue policy and is entered " +
			"as they are (FR-ORG-10). Needs If-Match with the contract's ETag. Idempotent with Idempotency-Key; Prefer: return=preview records nothing.",
	}, append(append(amendCodes, writes...), CodeStale), func(ctx context.Context, in *amendSubscriptionInput) (*subscriptionOutput, error) {
		id, err := parseID("contract_id", in.ContractID)
		if err != nil {
			return nil, err
		}
		on, err := time.Parse(time.DateOnly, in.Body.EffectiveOn)
		if err != nil {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.effective_on", Message: "is not a date", Fix: "Use a date such as 2026-04-01"}})
		}
		if err := checkIfMatch(in.IfMatch, ""); err != nil && in.IfMatch == "" {
			return nil, err
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (SubscriptionContractDetail, error) {
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return SubscriptionContractDetail{}, err
			}
			cur, err := u.Subscriptions().Lock(ctx, id)
			if err != nil {
				return SubscriptionContractDetail{}, err
			}
			if err := checkIfMatch(in.IfMatch, etag(cur.Version)); err != nil {
				return SubscriptionContractDetail{}, err
			}
			if _, err := subscription.Amend(ctx, u, id, cur.Version, subscription.Amendment{Kind: in.Body.Kind, EffectiveOn: on, MRR: in.Body.MRR, Reason: in.Body.Reason}, a.deps.Clock.Now()); err != nil {
				return SubscriptionContractDetail{}, err
			}
			c, err := u.Subscriptions().Get(ctx, id)
			if err != nil {
				return SubscriptionContractDetail{}, err
			}
			return contractDetailOf(ctx, u, c, today)
		})
		if err != nil {
			return nil, err
		}
		return &subscriptionOutput{Status: w.Status, ETag: etag(w.Body.Version), Applied: applied(w), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})

	register(a, authz.GetContractedMRR, huma.Operation{
		OperationID: "getContractedMRR", Method: http.MethodGet, Path: "/v1/subscription-mrr", Tags: []string{"Sales"},
		Summary: "Read the contracted MRR on a day",
		Description: "The monthly value of the subscription contracts active on the day, by currency (FR-AR-15; the contracted MRR of FR-KPI-02, which differs from the " +
			"booked MRR recognised in the ledger by mid-month starts, credits, unpaid or failed invoices and FX). Nothing is converted. Read-only.",
	}, []string{CodeInvalid}, func(ctx context.Context, in *contractedMRRInput) (*contractedMRROutput, error) {
		p, _ := authn.From(ctx)
		var out contractedMRROutput
		out.Body.Totals = []ContractedMRR{}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			day, err := orgToday(ctx, a, u)
			if err != nil {
				return err
			}
			if in.AsAt != "" {
				if day, err = time.Parse(time.DateOnly, in.AsAt); err != nil {
					return invalid([]FieldProblem{{Code: CodeInvalid, Field: "as_at", Message: "is not a date", Fix: "Use a date such as 2026-10-31"}})
				}
			}
			out.Body.AsAt = dateOf(day)
			totals, err := subscription.ContractedMRR(ctx, u, day)
			for _, t := range totals {
				out.Body.Totals = append(out.Body.Totals, ContractedMRR{Currency: t.Currency, MRR: Money{Amount: t.MRR.String(), Currency: t.Currency}, Active: t.Active})
			}
			return err
		})
		return &out, err
	})
}
