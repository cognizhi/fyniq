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
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ recurring invoices (FR-AR-07, FR-AUT-01; rev 0.135)
//
// A recurring invoice is an invoice's content and a schedule, kept as a recurring invoice is. The scheduler makes the
// draft invoice of each occurrence, which a person issues under the usual approvals (the owner's decision of 24 Oct
// 2026: making a draft is the default); issuing and sending by the scheduler follows.

// InvoiceSchedule is when a recurring invoice recurs (FR-AR-07: weekly to yearly): every 1 or 2 weeks from its start's
// weekday, or every 1, 3, 6 or 12 months on a day of the month.
type InvoiceSchedule struct {
	EveryWeeks  int    `json:"every_weeks,omitempty" enum:"1,2" doc:"Every week or every two weeks, on the start's weekday; leave every_months and day_of_month out." example:"1"`
	EveryMonths int    `json:"every_months,omitempty" enum:"1,3,6,12" doc:"Every 1, 3, 6 or 12 months: monthly, quarterly, half-yearly or yearly, with day_of_month; leave every_weeks out." example:"1"`
	DayOfMonth  int    `json:"day_of_month,omitempty" minimum:"1" maximum:"31" doc:"With every_months: the day of the month of each occurrence; the 29th to the 31st are the last day of a shorter month." example:"1"`
	StartsOn    string `json:"starts_on" format:"date" doc:"The first occurrence is the first on or after this date; one in the past is made by the next run." example:"2026-11-01"`
	EndsOn      string `json:"ends_on,omitempty" format:"date" doc:"No occurrence after this date; leave it out for no end." example:"2027-10-31"`
}

// RecurringInvoiceInput is what a person sets on a recurring invoice.
type RecurringInvoiceInput struct {
	Name       string             `json:"name" minLength:"1" maxLength:"120" doc:"What it is, as the list shows it." example:"Analytics subscription, Acme"`
	Schedule   InvoiceSchedule    `json:"schedule"`
	CustomerID string             `json:"customer_id,omitempty" format:"uuid" doc:"The customer of each draft: a contact marked as a customer." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f20"`
	Reference  string             `json:"reference,omitempty" maxLength:"60" doc:"The customer's reference on each draft, such as their purchase order." example:"PO-4411"`
	Currency   string             `json:"currency" pattern:"^[A-Z]{3}$" doc:"The currency of each draft. One in another currency takes the cascade's rate for its date when it is issued (ENG-40)." example:"SGD"`
	Lines      []InvoiceLineDraft `json:"lines" maxItems:"200" doc:"The lines of each draft, as on an invoice; a line never names a quote."`
	Notes      string             `json:"notes,omitempty" maxLength:"2000" doc:"The notes of each draft." example:"Monthly subscription"`
}

// RecurringInvoice is a recurring invoice at its version (FR-AR-07; PRD-001 rev 0.135).
type RecurringInvoice struct {
	ID             string           `json:"id" format:"uuid" doc:"The recurring invoice's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e19"`
	Name           string           `json:"name" doc:"What it is." example:"Analytics subscription, Acme"`
	Status         string           `json:"status" enum:"active,paused,ended" doc:"active makes its drafts; paused makes none until it is resumed; ended has no occurrence left." example:"active"`
	Schedule       InvoiceSchedule  `json:"schedule"`
	NextOn         string           `json:"next_on,omitempty" format:"date" doc:"The next occurrence the scheduler makes a draft invoice for, dated that day; absent once it has ended." example:"2026-12-01"`
	LastOn         string           `json:"last_on,omitempty" format:"date" doc:"The last occurrence a draft was made for." example:"2026-11-01"`
	LastInvoiceID  string           `json:"last_invoice_id,omitempty" format:"uuid" doc:"That draft invoice." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d72"`
	Customer       *InvoiceCustomer `json:"customer,omitempty" doc:"The customer of each draft, with its name."`
	Reference      string           `json:"reference,omitempty" doc:"The customer's reference on each draft." example:"PO-4411"`
	Currency       string           `json:"currency" doc:"The currency of each draft." example:"SGD"`
	Lines          []InvoiceLine    `json:"lines" nullable:"false" doc:"The lines of each draft, each with its net."`
	Notes          string           `json:"notes,omitempty" doc:"The notes of each draft." example:"Monthly subscription"`
	Net            Money            `json:"net" doc:"Each draft's lines' net, before GST: the GST is worked out on each draft's date."`
	Version        int              `json:"version" doc:"The version of the recurring invoice; it makes the ETag." example:"3"`
	ContentVersion int              `json:"content_version" doc:"The version of its content, which drafts name; it changes when the customer, reference, currency, lines or notes change." example:"2"`
	EditedBy       *Person          `json:"edited_by,omitempty" doc:"Who wrote the current content: they do not approve the issue of its drafts (SOD-3)."`
	EditedAt       time.Time        `json:"edited_at" doc:"When the current content was written." example:"2026-10-23T09:30:00Z"`
	Checks         []JournalCheck   `json:"checks" nullable:"false" doc:"Its statuses (Appendix C.1 #28, #29): every problem the invoice rules find in its content (a missing rate aside, which a draft takes when it is issued), as Templates incomplete (✖); a warning when an occurrence before today has no draft yet; or OK, Paused or Ended."`
}

// RecurringInvoicePage is a page of recurring invoices, by name.
type RecurringInvoicePage struct {
	Items      []RecurringInvoice `json:"items" nullable:"false"`
	NextCursor string             `json:"next_cursor,omitempty" doc:"Pass it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6Ik9mZmljZSByZW50fDAxOTIifQ"`
}

// RecurringInvoicePath names a recurring invoice.
type RecurringInvoicePath struct {
	RecurringInvoiceID string `path:"recurring_invoice_id" format:"uuid" doc:"The recurring invoice's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e19"`
}

type recurringInvoiceCreateInput struct {
	KeyHeader
	PreferHeader
	Body RecurringInvoiceInput
}

type recurringInvoiceReplaceInput struct {
	RecurringInvoicePath
	IfMatchHeader
	PreferHeader
	Body RecurringInvoiceInput
}

type recurringInvoiceStateInput struct {
	RecurringInvoicePath
	KeyHeader
	IfMatchHeader
	PreferHeader
}

type recurringInvoiceListOutput struct{ Body RecurringInvoicePage }

type recurringInvoiceReadOutput struct {
	ETag string `header:"ETag" doc:"The recurring invoice's version, for If-Match."`
	Body RecurringInvoice
}

type recurringInvoiceWriteOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The recurring invoice's version, for If-Match."`
	Location string `header:"Location" doc:"The recurring invoice's URL."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     RecurringInvoice
}

// recurringInvoiceOf is a recurring invoice as the API shows it, with its statuses.
func recurringInvoiceOf(ctx context.Context, u storage.UnitOfWork, base money.Currency, r storage.RecurringInvoice, st []journal.Status) (RecurringInvoice, error) {
	d, err := journal.ReadInvoiceContent(r.Content, base)
	if err != nil {
		return RecurringInvoice{}, err
	}
	net := money.Zero(d.Currency)
	for _, l := range d.Lines {
		if n, err := l.Net(); err == nil {
			net, _ = net.Add(n)
		}
	}
	out := RecurringInvoice{ID: r.ID.String(), Name: r.Name, Status: string(r.Status), NextOn: optDate(r.NextOn), LastOn: optDate(r.LastOn),
		Schedule: InvoiceSchedule{EveryWeeks: r.Schedule.EveryWeeks, EveryMonths: r.Schedule.EveryMonths, DayOfMonth: r.Schedule.DayOfMonth,
			StartsOn: r.Schedule.StartsOn.Format(time.DateOnly), EndsOn: optDate(r.Schedule.EndsOn)},
		Reference: d.Reference, Currency: d.Currency.Code, Lines: invoiceLinesOut(d), Notes: d.Notes, Net: moneyOut(net), Version: r.Version,
		ContentVersion: r.ContentVersion, EditedBy: personOf(r.EditedBy), EditedAt: r.EditedAt.UTC(), Checks: checksOf(st)}
	if r.NextOn == nil {
		out.Status = "ended"
	}
	if r.LastInvoiceID != nil {
		out.LastInvoiceID = r.LastInvoiceID.String()
	}
	if d.CustomerID != uuid.Nil {
		out.Customer = &InvoiceCustomer{ID: d.CustomerID.String()}
		if c, err := u.Contacts().Get(ctx, d.CustomerID); err == nil {
			out.Customer.Name = c.LegalName
		}
	}
	return out, nil
}

// recurringInvoiceInputOf reads a recurring invoice from the request, with every invalid field at once.
func recurringInvoiceInputOf(in RecurringInvoiceInput, base money.Currency) (journal.RecurringInvoiceInput, error) {
	out := journal.RecurringInvoiceInput{Name: strings.TrimSpace(in.Name)}
	var bad []FieldProblem
	if out.Name == "" {
		bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.name", Message: "is empty", Fix: "Name the recurring invoice"})
	}
	switch sc := in.Schedule; {
	case (sc.EveryWeeks == 0) == (sc.EveryMonths == 0):
		bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.schedule", Message: "names neither every_weeks nor every_months, or both",
			Fix: "Send every_weeks (1 or 2), or every_months (1, 3, 6 or 12) with day_of_month"})
	case sc.EveryMonths != 0 && sc.DayOfMonth == 0:
		bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.schedule.day_of_month", Message: "is missing", Fix: "Send the day of the month, 1 to 31"})
	case sc.EveryWeeks != 0 && sc.DayOfMonth != 0:
		bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.schedule.day_of_month", Message: "is for a monthly schedule",
			Fix: "Leave it out: a weekly schedule falls on its start's weekday"})
	}
	starts, err := time.Parse(time.DateOnly, in.Schedule.StartsOn)
	if err != nil {
		bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.schedule.starts_on", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"})
	}
	out.Schedule = storage.Schedule{EveryWeeks: in.Schedule.EveryWeeks, EveryMonths: in.Schedule.EveryMonths, DayOfMonth: in.Schedule.DayOfMonth, StartsOn: starts}
	if in.Schedule.EndsOn != "" {
		ends, err := time.Parse(time.DateOnly, in.Schedule.EndsOn)
		switch {
		case err != nil:
			bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.schedule.ends_on", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"})
		case ends.Before(starts):
			bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.schedule.ends_on", Message: "is before the start",
				Fix: "End it on or after " + in.Schedule.StartsOn + ", or leave the end out"})
		default:
			out.Schedule.EndsOn = &ends
		}
	}
	d, err := invoiceDraftOf(InvoiceDraft{CustomerID: in.CustomerID, Reference: in.Reference, Currency: in.Currency, Lines: in.Lines, Notes: in.Notes}, base)
	if p, ok := err.(*Problem); ok {
		bad = append(bad, p.Errors...)
	} else if err != nil {
		return out, err
	}
	if len(bad) > 0 {
		return out, invalid(bad)
	}
	out.Content = d
	return out, nil
}

// viewRecurringInvoices are recurring invoices with their statuses, and the names of who wrote their content.
func (a *API) viewRecurringInvoices(ctx context.Context, u storage.UnitOfWork, rs ...storage.RecurringInvoice) ([]RecurringInvoice, error) {
	today, err := orgToday(ctx, a, u)
	if err != nil {
		return nil, err
	}
	base, err := baseOf(ctx, u)
	if err != nil {
		return nil, err
	}
	st, err := journal.RecurringInvoiceStatuses(ctx, u, rs, today)
	if err != nil {
		return nil, err
	}
	out := make([]RecurringInvoice, 0, len(rs))
	for _, r := range rs {
		v, err := recurringInvoiceOf(ctx, u, base, r, st[r.ID])
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	people := make([]*Person, 0, len(out))
	for i := range out {
		people = append(people, out[i].EditedBy)
	}
	return out, a.name(ctx, u, people...)
}

// RecurringInvoiceCodes are the rules a recurring invoice's changes may answer.
var RecurringInvoiceCodes = []string{journal.CodeRecurringSchedule, journal.CodeRecurringPaused, storage.CodeVersion, invoice.CodeLine, invoice.CodeTooLong,
	money.CodeNotDecimal, money.CodePrecision, money.CodeRange, money.CodeCurrency, money.CodeRate, storage.CodeRetry, storage.CodeLockTimeout}

func registerRecurringInvoices(a *API) {
	register(a, authz.ListRecurringInvoices, huma.Operation{
		OperationID: "listRecurringInvoices", Method: http.MethodGet, Path: "/v1/recurring-invoices", Tags: []string{"Sales"},
		Summary: "List the recurring invoices",
		Description: "The organisation's recurring invoices by name, a page at a time (FR-AR-07; PRD-001 rev 0.135): each one's schedule, next " +
			"occurrence, content and statuses. Read-only; no side effects.",
	}, []string{CodeCursor}, func(ctx context.Context, in *recurringListInput) (*recurringInvoiceListOutput, error) {
		after, err := decodeCursor(in.Cursor)
		if err != nil {
			return nil, err
		}
		afterName, afterID, err := recurringCursor(after)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		out := recurringInvoiceListOutput{Body: RecurringInvoicePage{Items: []RecurringInvoice{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			page, err := u.RecurringInvoices().Page(ctx, afterName, afterID, in.PageSize+1)
			if err != nil {
				return err
			}
			if len(page) > in.PageSize {
				page = page[:in.PageSize]
				last := page[len(page)-1]
				out.Body.NextCursor = encodeCursor(last.Name + "|" + last.ID.String())
			}
			out.Body.Items, err = a.viewRecurringInvoices(ctx, u, page...)
			return err
		})
		return &out, err
	})

	register(a, authz.CreateRecurringInvoice, huma.Operation{
		OperationID: "createRecurringInvoice", Method: http.MethodPost, Path: "/v1/recurring-invoices", Tags: []string{"Sales"},
		DefaultStatus: http.StatusCreated, Summary: "Add a recurring invoice",
		Description: "Adds a recurring invoice, active: an invoice's customer, reference, currency, lines and notes, and a schedule " +
			"(every 1 or 2 weeks from the start's weekday, or every 1, 3, 6 or 12 months on a day of the month, from a start, until an " +
			"optional end; FR-AR-07; PRD-001 rev 0.135). The scheduler makes the draft invoice of each occurrence, dated on it, as " +
			"scheduler:invoices.recurring; a person issues it under the usual approvals, and whoever wrote the content does not approve " +
			"its drafts (SOD-3). A line naming a quote is refused (INVOICE-09). Its content may be incomplete: its statuses say what the " +
			"invoice rules would refuse. Side effects: the recurring invoice, its first content version and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview " +
			"checks it and saves nothing.",
	}, append([]string{CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		CodeInvalid}, RecurringInvoiceCodes...),
		func(ctx context.Context, in *recurringInvoiceCreateInput) (*recurringInvoiceWriteOutput, error) {
			p, _ := authn.From(ctx)
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (RecurringInvoice, error) {
				base, err := baseOf(ctx, u)
				if err != nil {
					return RecurringInvoice{}, err
				}
				input, err := recurringInvoiceInputOf(in.Body, base)
				if err != nil {
					return RecurringInvoice{}, err
				}
				r, err := a.invoices.CreateRecurring(ctx, p.Organisation, input)
				if err != nil {
					return RecurringInvoice{}, err
				}
				v, err := a.viewRecurringInvoices(ctx, u, r)
				if err != nil {
					return RecurringInvoice{}, err
				}
				return v[0], nil
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &recurringInvoiceWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &recurringInvoiceWriteOutput{Status: w.Status, ETag: etag(w.Body.Version), Location: "/v1/recurring-invoices/" + w.Body.ID,
				Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})

	register(a, authz.GetRecurringInvoice, huma.Operation{
		OperationID: "getRecurringInvoice", Method: http.MethodGet, Path: "/v1/recurring-invoices/{recurring_invoice_id}", Tags: []string{"Sales"},
		Summary:     "Get a recurring invoice",
		Description: "Returns a recurring invoice with its statuses and its ETag (FR-AR-07; PRD-001 rev 0.135). Read-only; no side effects.",
	}, []string{CodeNotFound}, func(ctx context.Context, in *RecurringInvoicePath) (*recurringInvoiceReadOutput, error) {
		id, err := parseID("recurring_invoice_id", in.RecurringInvoiceID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out recurringInvoiceReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			r, err := u.RecurringInvoices().Get(ctx, id)
			if err != nil {
				return err
			}
			v, err := a.viewRecurringInvoices(ctx, u, r)
			if err != nil {
				return err
			}
			out.Body, out.ETag = v[0], etag(r.Version)
			return nil
		})
		return &out, err
	})

	register(a, authz.ReplaceRecurringInvoice, huma.Operation{
		OperationID: "replaceRecurringInvoice", Method: http.MethodPut, Path: "/v1/recurring-invoices/{recurring_invoice_id}", Tags: []string{"Sales"},
		Summary: "Change a recurring invoice",
		Description: "Replaces a recurring invoice's name, schedule and content (FR-AR-07; PRD-001 rev 0.135). A changed customer, reference, currency, " +
			"lines or notes is a new content version, written by the caller; drafts already made keep theirs. A changed schedule " +
			"takes effect after the last occurrence made. Side effects: the next version and its audit event. Needs If-Match with the " +
			"recurring invoice's ETag. Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid}, RecurringInvoiceCodes...),
		func(ctx context.Context, in *recurringInvoiceReplaceInput) (*recurringInvoiceWriteOutput, error) {
			id, err := parseID("recurring_invoice_id", in.RecurringInvoiceID)
			if err != nil {
				return nil, err
			}
			p, _ := authn.From(ctx)
			w, err := write(ctx, a, "", in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (RecurringInvoice, error) {
				cur, err := u.RecurringInvoices().Get(ctx, id)
				if err != nil {
					return RecurringInvoice{}, err
				}
				if err := checkIfMatch(in.IfMatch, etag(cur.Version)); err != nil {
					return RecurringInvoice{}, err
				}
				base, err := baseOf(ctx, u)
				if err != nil {
					return RecurringInvoice{}, err
				}
				input, err := recurringInvoiceInputOf(in.Body, base)
				if err != nil {
					return RecurringInvoice{}, err
				}
				r, err := a.invoices.UpdateRecurring(ctx, p.Organisation, id, cur.Version, input)
				if err != nil {
					return RecurringInvoice{}, err
				}
				v, err := a.viewRecurringInvoices(ctx, u, r)
				if err != nil {
					return RecurringInvoice{}, err
				}
				return v[0], nil
			})
			if err != nil {
				return nil, err
			}
			return &recurringInvoiceWriteOutput{Status: w.Status, ETag: etag(w.Body.Version), Applied: applied(w), Body: w.Body}, nil
		})

	state := func(policy authz.Policy, op, path, summary, description string, to storage.RecurringStatus) {
		register(a, policy, huma.Operation{
			OperationID: op, Method: http.MethodPost, Path: "/v1/recurring-invoices/{recurring_invoice_id}/" + path, Tags: []string{"Sales"},
			Summary: summary, Description: description,
		}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld}, RecurringInvoiceCodes...),
			func(ctx context.Context, in *recurringInvoiceStateInput) (*recurringInvoiceWriteOutput, error) {
				id, err := parseID("recurring_invoice_id", in.RecurringInvoiceID)
				if err != nil {
					return nil, err
				}
				p, _ := authn.From(ctx)
				w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (RecurringInvoice, error) {
					cur, err := u.RecurringInvoices().Get(ctx, id)
					if err != nil {
						return RecurringInvoice{}, err
					}
					if err := checkIfMatch(in.IfMatch, etag(cur.Version)); err != nil {
						return RecurringInvoice{}, err
					}
					var r storage.RecurringInvoice
					if to == storage.RecurringPaused {
						r, err = a.invoices.PauseRecurring(ctx, p.Organisation, id, cur.Version)
					} else {
						today, terr := orgToday(ctx, a, u)
						if terr != nil {
							return RecurringInvoice{}, terr
						}
						r, err = a.invoices.ResumeRecurring(ctx, p.Organisation, id, cur.Version, today)
					}
					if err != nil {
						return RecurringInvoice{}, err
					}
					v, err := a.viewRecurringInvoices(ctx, u, r)
					if err != nil {
						return RecurringInvoice{}, err
					}
					return v[0], nil
				})
				if err != nil {
					return nil, err
				}
				return &recurringInvoiceWriteOutput{Status: w.Status, ETag: etag(w.Body.Version), Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
			})
	}
	state(authz.PauseRecurringInvoice, "pauseRecurringInvoice", "pause", "Pause a recurring invoice",
		"Stops an active recurring invoice making drafts until it is resumed (FR-AR-07; PRD-001 rev 0.135). Draft invoices already made stay. "+
			"Side effects: the next version and its audit event. Needs If-Match; idempotent with Idempotency-Key; Prefer: "+
			"return=preview checks it and saves nothing.", storage.RecurringPaused)
	state(authz.ResumeRecurringInvoice, "resumeRecurringInvoice", "resume", "Resume a recurring invoice",
		"Starts a paused recurring invoice again (FR-AR-07; PRD-001 rev 0.135): its next draft is for its first occurrence on or after "+
			"today, so the occurrences it missed while paused are skipped, not made late. Side effects: the next version and its audit "+
			"event. Needs If-Match; idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.", storage.RecurringActive)
}
