package api

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/payables"
	"github.com/cognizhi/fyniq/internal/domain/payee"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// BillPlanDraft is a bill's planned payment date (FR-AP-13).
type BillPlanDraft struct {
	PlannedOn string `json:"planned_on,omitempty" format:"date" doc:"The date to pay it, on or after the bill's date. Left out, the bill is unscheduled." example:"2026-11-10"`
}

// BillReminder is a bill to pay soon (FR-AP-13; rev 0.93).
type BillReminder struct {
	BillID    string       `json:"bill_id" format:"uuid" doc:"The bill." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e30"`
	Number    string       `json:"number,omitempty" doc:"Its number." example:"BILL-0007"`
	Supplier  BillSupplier `json:"supplier" doc:"Its supplier."`
	DueDate   string       `json:"due_date,omitempty" format:"date" doc:"When it falls due." example:"2026-11-13"`
	PlannedOn string       `json:"planned_payment_on,omitempty" format:"date" doc:"When its payment is planned for, if it is." example:"2026-11-10"`
	Open      Money        `json:"open" doc:"What is left to pay, in its currency."`
	Why       string       `json:"why" enum:"overdue,due,planned" doc:"overdue: past its due date; due: it falls due within the organisation's reminder days; planned: its planned payment date is within them." example:"due"`
}

// BillReminderPage is the bills to pay as at the organisation's today, earliest first.
type BillReminderPage struct {
	Days       int            `json:"days" doc:"The organisation's reminder days (the setting purchases.reminder_days)." example:"3"`
	Items      []BillReminder `json:"items" nullable:"false" doc:"The bills to pay, earliest date first: overdue, or due or planned within the days."`
	NextCursor string         `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjAxOTI4ZjZlIn0"`
}

type billRemindersInput struct {
	PageQuery
}

type billPlanInput struct {
	BillPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body *BillPlanDraft `required:"false"`
}

type billPlanOutput struct {
	ETag     string `header:"ETag" doc:"The bill's version and status, for If-Match: planning changes neither."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     Bill
}

type billRemindersOutput struct {
	Body BillReminderPage
}

func registerBillPlans(a *API) {
	register(a, authz.PlanBillPayment, huma.Operation{
		OperationID: "planBillPayment", Method: http.MethodPost, Path: "/v1/bills/{bill_id}/payment-plan", Tags: []string{"Purchases"},
		Summary: "Plan a bill's payment date",
		Description: "Plans the date a posted bill is to be paid (FR-AP-13), or unschedules it when planned_on is left out. A plan is " +
			"kept beside the bill: it posts nothing and changes no version of the bill, and a later plan replaces it (FY175). The " +
			"bill must have something open, and the date be on or after the bill's (BILL-18). While a change of the supplier's bank " +
			"details waits for approval, payments to it are frozen and no date is planned (PAYEE-12; FR-CT-03). The bills board and the reminders " +
			"read it; recording the payment settles the bill. Side effects: the plan and an audit event on the bill. Needs " +
			"If-Match with the bill's ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, []string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeInvalid, CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused,
		CodeIdempotencyBusy, CodeIdempotencyOld, bill.CodeNotPosted, bill.CodePlan, payee.CodeFrozen, storage.CodeRetry, storage.CodeLockTimeout},
		func(ctx context.Context, in *billPlanInput) (*billPlanOutput, error) {
			id, err := parseID("bill_id", in.BillID)
			if err != nil {
				return nil, err
			}
			var on *time.Time
			if in.Body != nil && in.Body.PlannedOn != "" {
				d, err := time.Parse(time.DateOnly, in.Body.PlannedOn)
				if err != nil {
					return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.planned_on", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"}})
				}
				on = &d
			}
			p, _ := authn.From(ctx)
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (Bill, error) {
				doc, err := u.Documents().Lock(ctx, id)
				if err != nil {
					return Bill{}, err
				}
				v, err := a.billView(ctx, u)
				if err != nil {
					return Bill{}, err
				}
				before, err := a.billOf(ctx, u, v, doc, false)
				if err != nil {
					return Bill{}, err
				}
				if err := checkIfMatch(in.IfMatch, billETag(before)); err != nil {
					return Bill{}, err
				}
				if on != nil { // FR-CT-03: no payment is planned to a vendor whose bank details wait for approval
					if supplier, err := uuid.Parse(before.Supplier.ID); err == nil {
						if err := payee.Frozen(ctx, u, supplier, a.deps.Clock.Now()); err != nil {
							return Bill{}, err
						}
					}
				}
				if _, err := a.bills.PlanPayment(ctx, p.Organisation, id, on); err != nil {
					return Bill{}, err
				}
				return a.billOf(ctx, u, v, doc, false)
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &billPlanOutput{Applied: applied(w), Body: w.Body}, nil
			}
			return &billPlanOutput{ETag: billETag(w.Body), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})

	register(a, authz.ListBillReminders, huma.Operation{
		OperationID: "listBillReminders", Method: http.MethodGet, Path: "/v1/bill-reminders", Tags: []string{"Purchases"},
		Summary: "List the bills to pay soon",
		Description: "The posted bills with something left to pay that are overdue, or fall due or are planned to be paid within the " +
			"organisation's reminder days (the setting purchases.reminder_days, 3 by default), as at its today, earliest first, a page at a time (FR-AP-13). " +
			"The Inbox shows them to the members who record bill payments. Read-only; no side effects.",
	}, []string{CodeCursor, storage.CodeRetry, storage.CodeLockTimeout}, func(ctx context.Context, in *billRemindersInput) (*billRemindersOutput, error) {
		after := ""
		if in.Cursor != "" {
			var err error
			if after, err = decodeCursor(in.Cursor); err != nil {
				return nil, err
			}
		}
		p, _ := authn.From(ctx)
		out := billRemindersOutput{Body: BillReminderPage{Items: []BillReminder{}}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			v, err := a.billView(ctx, u)
			if err != nil {
				return err
			}
			def, _ := settings.Lookup("purchases.reminder_days")
			changes, err := u.Settings().Changes(ctx, def.Key)
			if err != nil {
				return err
			}
			if out.Body.Days, err = strconv.Atoi(settings.On(def, changes, v.today).Value); err != nil {
				return err
			}
			if v.payables == uuid.Nil {
				return nil // no accounts payable account: no bill has posted
			}
			rs, err := payables.Reminders(ctx, u, v.base, v.payables, v.today, out.Body.Days)
			if err != nil {
				return err
			}
			if after != "" { // the page after the bill the cursor names, in the same order
				i := slices.IndexFunc(rs, func(r payables.Reminder) bool { return r.Item.DocumentID.String() == after })
				if i < 0 {
					return badCursor()
				}
				rs = rs[i+1:]
			}
			if len(rs) > in.PageSize {
				rs = rs[:in.PageSize]
				out.Body.NextCursor = encodeCursor(rs[len(rs)-1].Item.DocumentID.String())
			}
			for _, r := range rs {
				x := BillReminder{BillID: r.Item.DocumentID.String(), Number: r.Item.Number, Supplier: BillSupplier{ID: r.Supplier.ID.String(), Name: r.Supplier.Name},
					DueDate: dayText(r.Item.DueDate), Open: moneyOut(r.Item.Open), Why: r.Why}
				if r.PlannedOn != nil {
					x.PlannedOn = dayText(*r.PlannedOn)
				}
				out.Body.Items = append(out.Body.Items, x)
			}
			return nil
		})
		return &out, err
	})
}
