package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/contactsummary"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ a contact's balances and activity (FR-CT-02, rev 0.185)

// ContactEvent is one of a contact's documents.
type ContactEvent struct {
	Type   string `json:"type" doc:"The document's type, such as invoice, customer_receipt, bill or bill_payment." example:"invoice"`
	ID     string `json:"id" format:"uuid" doc:"The document." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d90"`
	Number string `json:"number,omitempty" doc:"Its number, once it has one." example:"INV-0012"`
	Date   string `json:"date,omitempty" format:"date" doc:"Its date." example:"2026-10-20"`
	Status string `json:"status" doc:"Its status, such as draft, posted or reversed." example:"posted"`
	Amount *Money `json:"amount,omitempty" doc:"What it moved on the receivables or payables account, in its currency, as owed: positive when the customer owes more (an invoice) or the company owes the supplier more (a bill), negative for what settles or credits it. Absent for a draft."`
}

// ContactYear is the financial year a summary's totals cover.
type ContactYear struct {
	From string `json:"from" format:"date" doc:"Its first day." example:"2026-01-01"`
	To   string `json:"to" format:"date" doc:"Its last day." example:"2026-12-31"`
}

// ContactSummary is what a contact stands at today (FR-CT-02).
type ContactSummary struct {
	AsAt       string           `json:"as_at" format:"date" doc:"The day: the organisation's today." example:"2026-11-06"`
	Receivable []ReceivablesRow `json:"receivable" nullable:"false" doc:"What the customer owes the company, by currency and days past due, with its unapplied credits; empty when nothing is open, or when you do not view invoices."`
	Payable    []PayablesRow    `json:"payable" nullable:"false" doc:"What the company owes the supplier, by currency and days past due, with its unapplied credits; empty when nothing is open, or when you do not view bills."`
	Year       *ContactYear     `json:"year,omitempty" doc:"The financial year holding the day; absent when the years are not set."`
	Invoiced   *Money           `json:"invoiced,omitempty" doc:"Invoiced to the customer in the year, less credit notes, in the base currency, GST included; absent when you do not view invoices."`
	Billed     *Money           `json:"billed,omitempty" doc:"Billed by the supplier in the year, less vendor credits, in the base currency, GST included; absent when you do not view bills."`
	Timeline   []ContactEvent   `json:"timeline" nullable:"false" doc:"Their latest documents you may see, newest first: at most 50."`
}

type contactSummaryOutput struct{ Body ContactSummary }

// views reports whether p views row's records, all of them: a right without a qualifier, or with one that limits only
// what they do there, such as the Accountant's posting limit on journals (authz.WholeRow).
func views(p authn.Principal, row authz.Capability) bool {
	return authz.Decide(authz.Policy{Operation: "view", Capability: row, Need: authz.View, Unqualified: true, Qualified: authz.WholeRow},
		p.Roles, p.AI()).Allowed
}

func registerContactSummary(a *API) {
	register(a, authz.GetContactSummary, huma.Operation{
		OperationID: "getContactSummary", Method: http.MethodGet, Path: "/v1/contacts/{contact_id}/summary", Tags: []string{"Contacts"},
		Summary: "Get a contact's balances and activity",
		Description: "What a customer or supplier stands at today (FR-CT-02; PRD-001 rev 0.185): what they owe the company and what it owes " +
			"them, by currency and days past due, from the AR and AP sub-ledgers (INV-07); what was invoiced to them and billed by them " +
			"in the financial year, in the base currency; and their latest documents with what each moved. The receivable side shows " +
			"to whoever views invoices, the payable side to whoever views bills. Read-only.",
	}, []string{CodeNotFound}, func(ctx context.Context, in *ContactPath) (*contactSummaryOutput, error) {
		id, err := parseID("contact_id", in.ContactID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		sales, purchases := views(p, authz.Invoices), views(p, authz.Bills)
		out := &contactSummaryOutput{Body: ContactSummary{Receivable: []ReceivablesRow{}, Payable: []PayablesRow{}, Timeline: []ContactEvent{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			if _, err := u.Contacts().Get(ctx, id); err != nil {
				return err
			}
			o, err := u.Organisation(ctx)
			if err != nil {
				return err
			}
			base, err := money.Lookup(o.BaseCurrency)
			if err != nil {
				return err
			}
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return err
			}
			s, err := contactsummary.Of(ctx, u, base, id, today)
			if err != nil {
				return err
			}
			out.Body.AsAt = dateOf(today)
			if !s.Year.StartsOn.IsZero() {
				out.Body.Year = &ContactYear{From: s.Year.StartsOn.Format(time.DateOnly), To: s.Year.EndsOn.Format(time.DateOnly)}
			}
			if sales {
				for _, r := range s.Receivable {
					out.Body.Receivable = append(out.Body.Receivable, receivablesRowOf(r))
				}
				v := moneyOut(s.Invoiced)
				out.Body.Invoiced = &v
			}
			if purchases {
				for _, r := range s.Payable {
					out.Body.Payable = append(out.Body.Payable, payablesRowOf(r))
				}
				v := moneyOut(s.Billed)
				out.Body.Billed = &v
			}
			for _, ev := range s.Timeline {
				var side struct {
					Customer string `json:"customer_id"`
					Supplier string `json:"supplier_id"`
				}
				_ = json.Unmarshal(ev.Document.Payload, &side)
				if (side.Customer != "" && !sales) || (side.Supplier != "" && !purchases) {
					continue
				}
				e := ContactEvent{Type: string(ev.Document.Type), ID: ev.Document.ID.String(), Number: ev.Document.Number, Date: ev.Document.Date,
					Status: string(ev.Document.Status)}
				if ev.Amount != nil {
					v := moneyOut(*ev.Amount)
					e.Amount = &v
				}
				out.Body.Timeline = append(out.Body.Timeline, e)
			}
			return nil
		})
		return out, err
	})
}
