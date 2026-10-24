package api

import (
	"context"
	"fmt"
	"net/http"
	netmail "net/mail"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/doctemplate"
	"github.com/cognizhi/fyniq/internal/domain/salesdoc"
	"github.com/cognizhi/fyniq/internal/domain/salesreminder"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/salesmail"
)

// ------------------------------------------------------------- reminders to customers (FR-AR-08; p1-sales task 7.1)
//
// A customer group names a reminder schedule: the days, before (negative) or after an invoice's due date, on which an
// invoice still open is due a reminder; a customer in no group follows the standard schedule. A sent quote or order is
// reminded of 3 days before its validity date. While receipts are recorded by hand (the owner's decision of 24 Oct
// 2026), nothing goes by itself: each due reminder waits in the Inbox for a person to send or skip, once.

// ReminderDay is a day of a reminder schedule: days from an invoice's due date, negative before it.
type ReminderDay int

// Schema implements huma.SchemaProvider: an integer from -30 to 365, with an example.
func (ReminderDay) Schema(huma.Registry) *huma.Schema {
	lo, hi := float64(salesreminder.MinDay), float64(salesreminder.MaxDay)
	return &huma.Schema{Type: "integer", Format: "int32", Minimum: &lo, Maximum: &hi,
		Description: "Days from an invoice's due date, negative before it.", Examples: []any{7}}
}

// reminderDays are a schedule's days as the API writes them.
func reminderDays(days []int) []ReminderDay {
	out := make([]ReminderDay, 0, len(days))
	for _, d := range days {
		out = append(out, ReminderDay(d))
	}
	return out
}

// ints are a schedule's days as the domain reads them.
func (in CustomerGroupInput) days() []int {
	out := make([]int, 0, len(in.ReminderDays))
	for _, d := range in.ReminderDays {
		out = append(out, int(d))
	}
	return out
}

// CustomerGroup is a group of customers and the reminder schedule they follow.
type CustomerGroup struct {
	ID           string        `json:"id" format:"uuid" doc:"The group's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f10"`
	Name         string        `json:"name" doc:"Its name." example:"Retainer clients"`
	ReminderDays []ReminderDay `json:"reminder_days" nullable:"false" doc:"The days, before (negative) or after an invoice's due date, on which an invoice still open is due a reminder, earliest first; none sends none." example:"[-3,0,7,30]"`
	Active       bool          `json:"active" doc:"An inactive group keeps its customers and their schedule, and is given to no other." example:"true"`
	Version      int           `json:"version" doc:"The group's version; it makes the ETag." example:"1"`
}

// CustomerGroupInput is a customer group as written.
type CustomerGroupInput struct {
	Name         string        `json:"name" minLength:"1" maxLength:"80" doc:"Its name, unique in the organisation." example:"Retainer clients"`
	ReminderDays []ReminderDay `json:"reminder_days" maxItems:"6" nullable:"false" doc:"At most 6 days from -30 to 365, earliest first, each once; [] sends no reminders." example:"[-3,0,7,30]"`
	Active       *bool         `json:"active,omitempty" doc:"false makes it inactive; a new group is active." example:"true"`
}

// CustomerGroupList is every customer group, and the standard schedule of customers in none.
type CustomerGroupList struct {
	Standard []ReminderDay   `json:"standard_reminder_days" nullable:"false" doc:"The schedule of a customer in no group: 3 days before the due date, on it, and 7 and 30 days after." example:"[-3,0,7,30]"`
	Items    []CustomerGroup `json:"items" nullable:"false" doc:"The groups, by name."`

	NextCursor string `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjAxOTI4ZjZlIn0"`
}

// CustomerGroupPath names a customer group.
type CustomerGroupPath struct {
	CustomerGroupID string `path:"customer_group_id" format:"uuid" doc:"The customer group's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f10"`
}

type customerGroupCreateInput struct {
	KeyHeader
	PreferHeader
	Body CustomerGroupInput
}

type customerGroupReplaceInput struct {
	CustomerGroupPath
	IfMatchHeader
	PreferHeader
	Body CustomerGroupInput
}

type customerGroupListInput struct {
	PageQuery
}

type customerGroupListOutput struct{ Body CustomerGroupList }

type customerGroupReadOutput struct {
	ETag string `header:"ETag" doc:"The customer group's version, for If-Match."`
	Body CustomerGroup
}

type customerGroupWriteOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The customer group's version, for If-Match."`
	Location string `header:"Location" doc:"The customer group's URL."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     CustomerGroup
}

func customerGroupOf(g storage.CustomerGroup) CustomerGroup {
	return CustomerGroup{ID: g.ID.String(), Name: g.Name, ReminderDays: reminderDays(g.ReminderDays), Active: g.Active, Version: g.Version}
}

// checkCustomerGroup runs a group's rules on its input.
func checkCustomerGroup(in CustomerGroupInput) error {
	var bad []FieldProblem
	if err := salesreminder.CheckName(in.Name); err != nil {
		e := err.(*salesreminder.Error)
		bad = append(bad, FieldProblem{Code: e.Code, Field: "body.name", Message: e.Message, Fix: e.Fix})
	}
	if err := salesreminder.CheckDays(in.days()); err != nil {
		e := err.(*salesreminder.Error)
		bad = append(bad, FieldProblem{Code: e.Code, Field: "body.reminder_days", Message: e.Message, Fix: e.Fix})
	}
	if len(bad) > 0 {
		return invalid(bad)
	}
	return nil
}

// SalesReminderCustomer is the customer a reminder goes to.
type SalesReminderCustomer struct {
	ID   string `json:"id" format:"uuid" doc:"The contact's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f20"`
	Name string `json:"name" doc:"The contact's legal name." example:"Acme Analytics Pte Ltd"`
}

// SalesReminder is a reminder due to a customer, waiting for a person to send or skip it.
type SalesReminder struct {
	Kind          string                `json:"kind" enum:"invoice,quote" doc:"invoice: an issued invoice still open; quote: a sent quote or sales order about to expire." example:"invoice"`
	DocumentID    string                `json:"document_id" format:"uuid" doc:"The invoice, quote or order." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f30"`
	Number        string                `json:"number" doc:"Its number." example:"INV-0012"`
	Customer      SalesReminderCustomer `json:"customer" doc:"Its customer."`
	Recipient     string                `json:"recipient,omitempty" doc:"The customer's billing email, where it goes unless another address is given; absent when it has none." example:"accounts@acme.example"`
	DueOn         string                `json:"due_on" format:"date" doc:"The invoice's due date, or the quote's validity date." example:"2026-11-01"`
	Step          int                   `json:"step" doc:"The reminder's day in the schedule: days from due_on, negative before it. Send or skip it by this step." example:"7"`
	DaysOverdue   int                   `json:"days_overdue" doc:"Days past an invoice's due date; 0 before it, and for a quote." example:"7"`
	Amount        Money                 `json:"amount" doc:"What is still open on the invoice, or the quote's total, in its currency."`
	EmailTemplate string                `json:"email_template" enum:"invoice,quote,reminder" doc:"The email template it is worded with: the payment reminder once the invoice is overdue, else the invoice's or quote's own." example:"reminder"`
}

// SalesReminderPage is the reminders due as at the organisation's today, earliest first.
type SalesReminderPage struct {
	AsAt       string          `json:"as_at" format:"date" doc:"The organisation's today." example:"2026-11-08"`
	Items      []SalesReminder `json:"items" nullable:"false" doc:"The reminders due, earliest first."`
	NextCursor string          `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjAxOTI4ZjZlIn0"`
}

// SalesReminderDecision is a reminder sent or skipped.
type SalesReminderDecision struct {
	ID         string    `json:"id" format:"uuid" doc:"The decision's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f40"`
	DocumentID string    `json:"document_id" format:"uuid" doc:"The invoice, quote or order." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f30"`
	Step       int       `json:"step" doc:"The reminder's day in the schedule." example:"7"`
	Outcome    string    `json:"outcome" enum:"sent,skipped" doc:"sent, with the send that carried it; or skipped." example:"sent"`
	SendID     string    `json:"send_id,omitempty" format:"uuid" doc:"The send of the document that carried the reminder: its email, recipient and PDF." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f50"`
	Reason     string    `json:"reason,omitempty" doc:"Why it was skipped, if given." example:"Paid by cheque on 6 Nov; receipt to record"`
	DecidedBy  *Person   `json:"decided_by,omitempty" doc:"Who sent or skipped it."`
	DecidedAt  time.Time `json:"decided_at" doc:"When." example:"2026-11-08T09:30:00Z"`
}

// SendSalesReminder sends a reminder due.
type SendSalesReminder struct {
	Step int    `json:"step" minimum:"-30" maximum:"365" doc:"The reminder's step, as the list gives it." example:"7"`
	To   string `json:"to,omitempty" maxLength:"254" doc:"The address to send it to; left out, the customer's billing email." example:"accounts@acme.example"`
}

// SkipSalesReminder skips a reminder due.
type SkipSalesReminder struct {
	Step   int    `json:"step" minimum:"-30" maximum:"365" doc:"The reminder's step, as the list gives it." example:"7"`
	Reason string `json:"reason,omitempty" maxLength:"500" doc:"Why, for whoever reads it later." example:"Paid by cheque on 6 Nov; receipt to record"`
}

// SalesReminderPath names the document a reminder is of.
type SalesReminderPath struct {
	DocumentID string `path:"document_id" format:"uuid" doc:"The invoice, quote or order the reminder is of." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f30"`
}

type salesRemindersInput struct {
	PageQuery
}

type salesRemindersOutput struct{ Body SalesReminderPage }

type sendSalesReminderInput struct {
	SalesReminderPath
	KeyHeader
	PreferHeader
	Body SendSalesReminder
}

type skipSalesReminderInput struct {
	SalesReminderPath
	KeyHeader
	PreferHeader
	Body SkipSalesReminder
}

type salesReminderDecisionOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     SalesReminderDecision
}

func salesReminderOf(r salesreminder.Reminder) SalesReminder {
	tpl := doctemplate.Invoice
	switch {
	case r.Kind == salesreminder.KindQuote:
		tpl = doctemplate.Quote
	case r.Overdue():
		tpl = doctemplate.Reminder
	}
	return SalesReminder{Kind: r.Kind, DocumentID: r.DocumentID.String(), Number: r.Number,
		Customer: SalesReminderCustomer{ID: r.CustomerID.String(), Name: r.CustomerName}, Recipient: r.Recipient, DueOn: dateOf(r.DueOn),
		Step: r.Step, DaysOverdue: r.DaysOverdue, Amount: moneyOut(r.Amount), EmailTemplate: tpl}
}

func salesReminderDecisionOf(d storage.SalesReminder) SalesReminderDecision {
	out := SalesReminderDecision{ID: d.ID.String(), DocumentID: d.DocumentID.String(), Step: d.Step, Outcome: d.Outcome, Reason: d.Reason,
		DecidedBy: personOf(d.DecidedBy), DecidedAt: d.DecidedAt.UTC()}
	if d.SendID != nil {
		out.SendID = d.SendID.String()
	}
	return out
}

// dueReminder is the reminder due on document id at step as at the organisation's today: REMINDER-02 when none is.
func (a *API) dueReminder(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, step int) (salesreminder.Reminder, error) {
	base, err := baseOf(ctx, u)
	if err != nil {
		return salesreminder.Reminder{}, err
	}
	today, err := orgToday(ctx, a, u)
	if err != nil {
		return salesreminder.Reminder{}, err
	}
	due, err := salesreminder.Due(ctx, u, base, today)
	if err != nil {
		return salesreminder.Reminder{}, err
	}
	return salesreminder.Find(due, id, step)
}

// decisionOn is the decision recorded on document id at step.
func decisionOn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, step int) (SalesReminderDecision, error) {
	ds, err := u.SalesReminders().Of(ctx, []uuid.UUID{id})
	if err != nil {
		return SalesReminderDecision{}, err
	}
	for _, d := range ds[id] {
		if d.Step == step {
			return salesReminderDecisionOf(d), nil
		}
	}
	return SalesReminderDecision{}, fmt.Errorf("the reminder of %s at step %d was not recorded", id, step)
}

func registerSalesReminders(a *API) {
	groupCodes := []string{CodeInvalid, salesreminder.CodeDays, salesreminder.CodeGroup, storage.CodeDuplicate, storage.CodeRetry, storage.CodeLockTimeout}
	register(a, authz.ListCustomerGroups, huma.Operation{
		OperationID: "listCustomerGroups", Method: http.MethodGet, Path: "/v1/customer-groups", Tags: []string{"Contacts"},
		Summary: "List the customer groups",
		Description: "The customer groups by name, a page at a time, with their reminder schedules, and the standard schedule of customers " +
			"in none (FR-AR-08; PRD-001 rev 0.137). Read-only; no side effects.",
	}, []string{CodeCursor}, func(ctx context.Context, in *customerGroupListInput) (*customerGroupListOutput, error) {
		after := ""
		if in.Cursor != "" {
			var err error
			if after, err = decodeCursor(in.Cursor); err != nil {
				return nil, err
			}
		}
		p, _ := authn.From(ctx)
		out := customerGroupListOutput{Body: CustomerGroupList{Standard: reminderDays(salesreminder.Standard), Items: []CustomerGroup{}}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			gs, err := u.CustomerGroups().List(ctx)
			if err != nil {
				return err
			}
			if after != "" { // the page after the group the cursor names, in the same order
				i := slices.IndexFunc(gs, func(g storage.CustomerGroup) bool { return g.ID.String() == after })
				if i < 0 {
					return badCursor()
				}
				gs = gs[i+1:]
			}
			if len(gs) > in.PageSize {
				gs = gs[:in.PageSize]
				out.Body.NextCursor = encodeCursor(gs[len(gs)-1].ID.String())
			}
			for _, g := range gs {
				out.Body.Items = append(out.Body.Items, customerGroupOf(g))
			}
			return nil
		})
		return &out, err
	})

	register(a, authz.CreateCustomerGroup, huma.Operation{
		OperationID: "createCustomerGroup", Method: http.MethodPost, Path: "/v1/customer-groups", Tags: []string{"Contacts"},
		DefaultStatus: http.StatusCreated, Summary: "Add a customer group",
		Description: "Adds a customer group, active, with its reminder schedule: at most 6 days from -30 to 365, before (negative) or after " +
			"an invoice's due date, earliest first; [] sends no reminders (FR-AR-08; PRD-001 rev 0.137). A customer joins it through its " +
			"contact's customer_group_id. Side effects: the group and its audit event. Idempotent with Idempotency-Key; Prefer: " +
			"return=preview checks it and saves nothing.",
	}, append([]string{CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld}, groupCodes...),
		func(ctx context.Context, in *customerGroupCreateInput) (*customerGroupWriteOutput, error) {
			if err := checkCustomerGroup(in.Body); err != nil {
				return nil, err
			}
			if in.Body.Active != nil && !*in.Body.Active {
				return nil, invalid([]FieldProblem{{Code: salesreminder.CodeGroup, Field: "body.active", Message: "is false for a new group",
					Fix: "Add the group active, or leave active out"}})
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (CustomerGroup, error) {
				g, err := u.CustomerGroups().Create(ctx, strings.TrimSpace(in.Body.Name), in.Body.days())
				return customerGroupOf(g), err
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &customerGroupWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &customerGroupWriteOutput{Status: w.Status, ETag: etag(w.Body.Version), Location: "/v1/customer-groups/" + w.Body.ID,
				Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})

	register(a, authz.GetCustomerGroup, huma.Operation{
		OperationID: "getCustomerGroup", Method: http.MethodGet, Path: "/v1/customer-groups/{customer_group_id}", Tags: []string{"Contacts"},
		Summary:     "Get a customer group",
		Description: "Returns a customer group with its reminder schedule and its ETag (FR-AR-08; PRD-001 rev 0.137). Read-only; no side effects.",
	}, []string{CodeNotFound}, func(ctx context.Context, in *CustomerGroupPath) (*customerGroupReadOutput, error) {
		id, err := parseID("customer_group_id", in.CustomerGroupID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out customerGroupReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			g, err := u.CustomerGroups().Get(ctx, id)
			out.Body, out.ETag = customerGroupOf(g), etag(g.Version)
			return err
		})
		return &out, err
	})

	register(a, authz.ReplaceCustomerGroup, huma.Operation{
		OperationID: "replaceCustomerGroup", Method: http.MethodPut, Path: "/v1/customer-groups/{customer_group_id}", Tags: []string{"Contacts"},
		Summary: "Change a customer group",
		Description: "Replaces a customer group's name, reminder schedule and whether it is active (FR-AR-08; PRD-001 rev 0.137). Its customers " +
			"follow the new schedule from now on; reminders sent or skipped stay as they were. An inactive group keeps its customers and is " +
			"given to no other. Side effects: the next version and its audit event. Needs If-Match with the group's ETag. Prefer: " +
			"return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType}, groupCodes...),
		func(ctx context.Context, in *customerGroupReplaceInput) (*customerGroupWriteOutput, error) {
			id, err := parseID("customer_group_id", in.CustomerGroupID)
			if err != nil {
				return nil, err
			}
			if err := checkCustomerGroup(in.Body); err != nil {
				return nil, err
			}
			w, err := write(ctx, a, "", in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (CustomerGroup, error) {
				cur, err := u.CustomerGroups().Get(ctx, id)
				if err != nil {
					return CustomerGroup{}, err
				}
				if err := checkIfMatch(in.IfMatch, etag(cur.Version)); err != nil {
					return CustomerGroup{}, err
				}
				active := cur.Active
				if in.Body.Active != nil {
					active = *in.Body.Active
				}
				g, err := u.CustomerGroups().Update(ctx, id, cur.Version, strings.TrimSpace(in.Body.Name), in.Body.days(), active)
				return customerGroupOf(g), err
			})
			if err != nil {
				return nil, err
			}
			return &customerGroupWriteOutput{Status: w.Status, ETag: etag(w.Body.Version), Applied: applied(w), Body: w.Body}, nil
		})

	register(a, authz.ListSalesReminders, huma.Operation{
		OperationID: "listSalesReminders", Method: http.MethodGet, Path: "/v1/sales-reminders", Tags: []string{"Sales"},
		Summary: "List the reminders due to customers",
		Description: "The reminders due as at the organisation's today, earliest first, a page at a time (FR-AR-08; PRD-001 rev 0.137): each " +
			"issued invoice with something still open whose customer's schedule (its group's, else 3 days before the due date, on it, and " +
			"7 and 30 days after) has reached a step not yet sent or skipped, and each sent quote or order not answered within 3 days of " +
			"its validity date. Only the latest step due is listed; deciding it settles the earlier ones. While receipts are recorded by " +
			"hand, nothing is sent by itself: the Inbox shows these for a person to send or skip. Read-only; no side effects.",
	}, []string{CodeCursor, storage.CodeRetry, storage.CodeLockTimeout}, func(ctx context.Context, in *salesRemindersInput) (*salesRemindersOutput, error) {
		after := ""
		if in.Cursor != "" {
			var err error
			if after, err = decodeCursor(in.Cursor); err != nil {
				return nil, err
			}
		}
		p, _ := authn.From(ctx)
		out := salesRemindersOutput{Body: SalesReminderPage{Items: []SalesReminder{}}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			base, err := baseOf(ctx, u)
			if err != nil {
				return err
			}
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return err
			}
			out.Body.AsAt = dateOf(today)
			rs, err := salesreminder.Due(ctx, u, base, today)
			if err != nil {
				return err
			}
			if after != "" { // the page after the document the cursor names, in the same order
				i := slices.IndexFunc(rs, func(r salesreminder.Reminder) bool { return r.DocumentID.String() == after })
				if i < 0 {
					return badCursor()
				}
				rs = rs[i+1:]
			}
			if len(rs) > in.PageSize {
				rs = rs[:in.PageSize]
				out.Body.NextCursor = encodeCursor(rs[len(rs)-1].DocumentID.String())
			}
			for _, r := range rs {
				out.Body.Items = append(out.Body.Items, salesReminderOf(r))
			}
			return nil
		})
		return &out, err
	})

	register(a, authz.SendSalesReminder, huma.Operation{
		OperationID: "sendSalesReminder", Method: http.MethodPost, Path: "/v1/sales-reminders/{document_id}/send", Tags: []string{"Sales"},
		Summary: "Send a reminder due",
		Description: "Sends the reminder due on an invoice, quote or order at the step the list gives (FR-AR-08; PRD-001 rev 0.137), to the " +
			"customer's billing email or the address given, from the organisation's reminders sender: worded with the payment reminder " +
			"email template once the invoice is overdue, else with the invoice's or quote's own, with the PDF already sent of its current " +
			"version (rendered when none was) and an online link. A reminder not due (settled, answered, not reached, or decided) is " +
			"refused (REMINDER-02). Side effects: the email message, the send, the link, the decision, and their audit events. Idempotent " +
			"with Idempotency-Key; Prefer: return=preview checks it and sends nothing.",
	}, []string{CodeNotFound, CodeStale, CodeInvalid, CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused, CodeIdempotencyBusy,
		CodeIdempotencyOld, CodeUnavailable, salesreminder.CodeNotDue, salesdoc.CodeNotSent, salesdoc.CodeNoRecipient, salesdoc.CodeUnready,
		salesdoc.CodeParticulars, doctemplate.CodeRenderer, storage.CodeReminder, storage.CodeRetry, storage.CodeLockTimeout},
		func(ctx context.Context, in *sendSalesReminderInput) (*salesReminderDecisionOutput, error) {
			id, err := parseID("document_id", in.DocumentID)
			if err != nil {
				return nil, err
			}
			if a.deps.Outbox == nil || a.deps.Staging == nil {
				return nil, problem(CodeUnavailable, "Email cannot be sent from this deployment.", "Ask the operator to configure the outbox and the file store")
			}
			to := strings.TrimSpace(in.Body.To)
			if to != "" {
				addr, err := netmail.ParseAddress(to)
				if err != nil || addr.Name != "" || addr.Address != to || strings.ContainsAny(to, "\r\n") {
					return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.to", Message: "is not an email address",
						Fix: "Send one address, such as accounts@acme.example"}})
				}
			}
			p, _ := authn.From(ctx)
			preview := wantsPreview(in.Prefer)
			var r salesreminder.Reminder
			if err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
				r, err = a.dueReminder(ctx, u, id, in.Body.Step)
				return err
			}); err != nil {
				return nil, err
			}
			sender := a.sender()
			s, err := sender.PrepareReminder(ctx, p.Organisation, r, to, !preview, nil)
			if err != nil {
				return nil, sendProblem(s.Kind, err)
			}
			key := ""
			if !preview {
				if key, err = sender.Stage(ctx, p.Organisation, s); err != nil {
					logFailure(ctx, err)
					return nil, problem(CodeUnavailable, "The PDF could not be stored.", "Try again in a few minutes.")
				}
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (SalesReminderDecision, error) {
				if _, err := a.dueReminder(ctx, u, id, in.Body.Step); err != nil { // still due: a receipt recorded meanwhile settles it
					return SalesReminderDecision{}, err
				}
				if preview {
					doc, err := u.Documents().Lock(ctx, id)
					if err != nil {
						return SalesReminderDecision{}, err
					}
					if doc.Status != storage.DocumentPosted || doc.Version != s.Version {
						return SalesReminderDecision{}, sendProblem(s.Kind, salesmail.ErrChanged)
					}
					return SalesReminderDecision{DocumentID: id.String(), Step: in.Body.Step, Outcome: storage.ReminderSent}, nil
				}
				if err := sender.Record(ctx, u, p.Organisation, id, s, key); err != nil {
					return SalesReminderDecision{}, sendProblem(s.Kind, err)
				}
				return decisionOn(ctx, u, id, in.Body.Step)
			})
			if (err != nil || w.Replayed) && key != "" {
				_ = a.deps.Staging.Delete(context.WithoutCancel(ctx), key) // no row uses it; the sweeper would delete it a day later
			}
			if err != nil {
				return nil, err
			}
			return &salesReminderDecisionOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})

	register(a, authz.SkipSalesReminder, huma.Operation{
		OperationID: "skipSalesReminder", Method: http.MethodPost, Path: "/v1/sales-reminders/{document_id}/skip", Tags: []string{"Sales"},
		Summary: "Skip a reminder due",
		Description: "Skips the reminder due on an invoice, quote or order at the step the list gives, with a reason if given (FR-AR-08; " +
			"PRD-001 rev 0.137): nothing is sent, and the next step in the schedule is listed when it comes. A reminder not due is refused " +
			"(REMINDER-02). Side effects: the decision and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks " +
			"it and saves nothing.",
	}, []string{CodeNotFound, CodeInvalid, CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		salesreminder.CodeNotDue, storage.CodeReminder, storage.CodeRetry, storage.CodeLockTimeout},
		func(ctx context.Context, in *skipSalesReminderInput) (*salesReminderDecisionOutput, error) {
			id, err := parseID("document_id", in.DocumentID)
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (SalesReminderDecision, error) {
				if _, err := a.dueReminder(ctx, u, id, in.Body.Step); err != nil {
					return SalesReminderDecision{}, err
				}
				d, err := u.SalesReminders().Record(ctx, storage.NewSalesReminder{DocumentID: id, Step: in.Body.Step, Outcome: storage.ReminderSkipped,
					Reason: strings.TrimSpace(in.Body.Reason)})
				return salesReminderDecisionOf(d), err
			})
			if err != nil {
				return nil, err
			}
			return &salesReminderDecisionOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})
}
