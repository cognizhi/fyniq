package api

import (
	"cmp"
	"context"
	"net/http"
	"slices"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------ prepayment and deferral schedules (FR-GL-03, INV-07)

// ScheduleMonth is one month of a schedule as at the day.
type ScheduleMonth struct {
	Month   string `json:"month" doc:"The month, as YYYY-MM." example:"2026-10"`
	Date    string `json:"date" format:"date" doc:"The release's date: the month's first day, or the document's date for a month before it." example:"2026-10-01"`
	Amount  Money  `json:"amount" doc:"What the month releases, in the base currency: the months are rounded on the running total, so they add up to the schedule's total exactly (ALLOC-EVEN)."`
	Status  string `json:"status" enum:"posted,reversed,waiting,projected" doc:"As at the day: posted, in the ledger; reversed, posted and then reversed; waiting, dated by the day and not in the ledger yet (the hourly job posts it, or waits for its period to open); projected, dated after the day, not in the ledger (ENG-05)." example:"posted"`
	EntryID string `json:"entry_id,omitempty" format:"uuid" doc:"Its entry, once posted." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d80"`
}

// Schedule is one spread line of a posted document as at the day.
type Schedule struct {
	Kind         string          `json:"kind" enum:"prepayment,deferred_revenue" doc:"prepayment: a cost on 1210 Prepayments, expensed month by month; deferred_revenue: income on 2400 Deferred revenue, earned month by month." example:"prepayment"`
	SourceType   string          `json:"source_type" enum:"bill,invoice,bank_transaction" doc:"The document whose line is spread." example:"bill"`
	SourceID     string          `json:"source_id" format:"uuid" doc:"The document, to open it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d70"`
	Number       string          `json:"number,omitempty" doc:"The document's number, once it has one." example:"BILL-0866"`
	Line         int             `json:"line" doc:"The document's line that is spread, from 1." example:"1"`
	Description  string          `json:"description,omitempty" doc:"The line's description." example:"Directors' and officers' insurance"`
	AccountID    string          `json:"account_id" format:"uuid" doc:"The account each month posts to: an expense account for a prepayment, a revenue account for deferred revenue." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d60"`
	AccountCode  string          `json:"account_code" doc:"Its code." example:"6410"`
	AccountName  string          `json:"account_name" doc:"Its name." example:"Insurance"`
	Total        Money           `json:"total" doc:"What the line put on the holding account, in the base currency."`
	Recognised   Money           `json:"recognised" doc:"What its months posted by the day took off it."`
	Remaining    Money           `json:"remaining" doc:"What is left on the holding account: the total less what is recognised."`
	MonthsPosted int             `json:"months_posted" doc:"How many of its months are posted by the day." example:"6"`
	Months       []ScheduleMonth `json:"months" nullable:"false" doc:"Each month with something to release, in order."`
}

// ScheduleExplained is what a source other than the schedules' own documents put on a holding account.
type ScheduleExplained struct {
	SourceType string `json:"source_type" doc:"The source: opening_balance or manual_journal, or another." example:"manual_journal"`
	Amount     Money  `json:"amount" doc:"What it put on the account, in the base currency, as the holding account reads it."`
}

// ScheduleHolding is a holding account reconciled with its schedules (INV-07).
type ScheduleHolding struct {
	Kind        string              `json:"kind" enum:"prepayment,deferred_revenue" doc:"prepayment: 1210 Prepayments; deferred_revenue: 2400 Deferred revenue." example:"prepayment"`
	AccountID   string              `json:"account_id" format:"uuid" doc:"The account with the role." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d61"`
	AccountCode string              `json:"account_code" doc:"Its code." example:"1210"`
	AccountName string              `json:"account_name" doc:"Its name." example:"Prepayments"`
	Balance     Money               `json:"balance" doc:"What the account holds at the day, in the base currency, from the balance service (ENG-04): a debit for prepayments, a credit for deferred revenue, each read as positive."`
	Schedules   Money               `json:"schedules" doc:"What its open schedules still hold."`
	Direct      Money               `json:"direct" doc:"What bills and bank lines put on it directly, which no schedule releases."`
	Explained   []ScheduleExplained `json:"explained" nullable:"false" doc:"What opening balances, manual journals and other sources put on it."`
	Unexplained Money               `json:"unexplained" doc:"What is left: a posting and its document disagree. Zero when it ties."`
	Status      string              `json:"status" enum:"agrees,explained,differs" doc:"agrees: the schedules and the documents' direct amounts are all it holds; explained: it also holds amounts from other sources; differs: something is unexplained, which the close refuses." example:"agrees"`
	Open        int                 `json:"open" doc:"Its schedules with something remaining." example:"3"`
}

// Schedules are the prepayment and deferral schedules as at a day.
type Schedules struct {
	AsAt      string            `json:"as_at" format:"date" doc:"The day." example:"2026-09-30"`
	Currency  string            `json:"currency" doc:"The base currency every amount is in." example:"SGD"`
	Holdings  []ScheduleHolding `json:"holdings" nullable:"false" doc:"1210 Prepayments and 2400 Deferred revenue, each that the chart has, reconciled with their schedules."`
	Schedules []Schedule        `json:"schedules" nullable:"false" doc:"The schedules, prepayments first, by the date of their first month, then the document's number and line."`
}

type schedulesInput struct {
	AsAt   string `query:"as_at" format:"date" doc:"The day; left out, the organisation's today." example:"2026-09-30"`
	Status string `query:"status" enum:"open,all" default:"open" doc:"open: the schedules with something remaining at the day; all: those finished too." example:"open"`
}

type schedulesOutput struct{ Body Schedules }

// scheduleKind is the API's name of a holding account's role.
func scheduleKind(role string) string {
	if role == ledger.RoleDeferredRevenue {
		return "deferred_revenue"
	}
	return "prepayment"
}

func registerSchedules(a *API) {
	register(a, authz.GetSchedules, huma.Operation{
		OperationID: "getSchedules", Method: http.MethodGet, Path: "/v1/reports/schedules", Tags: []string{"Reports"},
		Summary: "Get the prepayment and deferral schedules",
		Description: "Every cost and income spread over months (FR-GL-03) as at a day: each spread line of a posted bill, invoice or " +
			"bank line, with what it put on 1210 Prepayments or 2400 Deferred revenue, what its months posted by the day took off " +
			"it, what remains, and each month's state. Months post only once they are reached (ENG-05): later ones are " +
			"projections, never in the ledger. Each holding account is reconciled with its schedules (INV-07): its balance from " +
			"the balance service against what the schedules and the documents' direct amounts hold, with what opening balances " +
			"and manual journals put on it named, and anything else unexplained, which the period close refuses. Read-only; " +
			"no side effects.",
	}, []string{CodeInvalid}, func(ctx context.Context, in *schedulesInput) (*schedulesOutput, error) {
		p, _ := authn.From(ctx)
		var out Schedules
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			o, err := u.Organisation(ctx)
			if err != nil {
				return err
			}
			base, err := money.Lookup(o.BaseCurrency)
			if err != nil {
				return err
			}
			asAt, err := orgToday(ctx, a, u)
			if err != nil {
				return err
			}
			if in.AsAt != "" {
				if asAt, err = time.Parse(time.DateOnly, in.AsAt); err != nil {
					return invalid([]FieldProblem{{Code: CodeInvalid, Field: "as_at", Message: "is not a date", Fix: "Send the day as YYYY-MM-DD"}})
				}
			}
			d, err := journal.DeferralsAt(ctx, u, asAt)
			if err != nil {
				return err
			}
			accounts, err := u.Accounts().List(ctx)
			if err != nil {
				return err
			}
			byID := map[string]storage.Account{}
			for _, acc := range accounts {
				byID[acc.ID.String()] = acc
			}
			out = Schedules{AsAt: dateOf(asAt), Currency: base.Code, Holdings: []ScheduleHolding{}, Schedules: []Schedule{}}
			for _, h := range d.Holdings {
				r := h.Reconciliation.Base
				direct := money.Zero(base)
				for _, it := range h.Direct {
					if direct, err = direct.Add(it.Carrying); err != nil {
						return err
					}
				}
				scheduled, err := r.SubLedger.Sub(direct)
				if err != nil {
					return err
				}
				x := ScheduleHolding{Kind: scheduleKind(h.Role), AccountID: h.Account.ID.String(), AccountCode: h.Account.Code, AccountName: h.Account.Name,
					Balance: moneyOut(r.Control), Schedules: moneyOut(scheduled), Direct: moneyOut(direct), Explained: []ScheduleExplained{},
					Unexplained: moneyOut(r.Unexplained), Status: "agrees", Open: h.Open}
				for _, e := range r.Explained {
					if !e.Amount.IsZero() {
						x.Explained = append(x.Explained, ScheduleExplained{SourceType: e.SourceType, Amount: moneyOut(e.Amount)})
						x.Status = "explained"
					}
				}
				if h.Reconciliation.Unexplained() {
					x.Status = "differs"
				}
				out.Holdings = append(out.Holdings, x)
			}
			for _, sc := range d.Schedules {
				if in.Status != "all" && sc.Remaining.IsZero() {
					continue
				}
				acc := byID[sc.AccountID.String()]
				x := Schedule{Kind: scheduleKind(sc.Holding), SourceType: string(sc.Document.Type), SourceID: sc.Document.ID.String(), Number: sc.Document.Number,
					Line: sc.Line, Description: sc.Description, AccountID: sc.AccountID.String(), AccountCode: acc.Code, AccountName: acc.Name,
					Total: moneyOut(sc.Total), Recognised: moneyOut(sc.Recognised), Remaining: moneyOut(sc.Remaining), Months: []ScheduleMonth{}}
				for _, m := range sc.Months {
					y := ScheduleMonth{Month: m.Month.Format("2006-01"), Date: dateOf(m.Date), Amount: moneyOut(m.Amount), Status: m.Status}
					if m.Status == journal.ReleasePosted || m.Status == journal.ReleaseReversed {
						y.EntryID = m.EntryID.String()
					}
					if m.Status == journal.ReleasePosted {
						x.MonthsPosted++
					}
					x.Months = append(x.Months, y)
				}
				out.Schedules = append(out.Schedules, x)
			}
			slices.SortStableFunc(out.Schedules, func(a, b Schedule) int {
				return cmp.Or(cmp.Compare(kindOrder(a.Kind), kindOrder(b.Kind)), cmp.Compare(firstMonth(a), firstMonth(b)), cmp.Compare(a.Number, b.Number),
					cmp.Compare(a.Line, b.Line))
			})
			return nil
		})
		if err != nil {
			return nil, err
		}
		return &schedulesOutput{Body: out}, nil
	})
}

// kindOrder puts prepayments before deferred revenue.
func kindOrder(kind string) int {
	if kind == "prepayment" {
		return 0
	}
	return 1
}

// firstMonth is a schedule's first release date, to order schedules by when they start.
func firstMonth(s Schedule) string {
	if len(s.Months) == 0 {
		return ""
	}
	return s.Months[0].Date
}
