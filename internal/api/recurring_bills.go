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
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ recurring bills (FR-AP-09, FR-AUT-01; rev 0.84)
//
// A recurring bill is a bill's content and a schedule, kept as a recurring journal is. The scheduler makes the draft
// bill of each occurrence, which a person completes with the supplier's number and posts under the usual approvals
// (the owner's decision of 22 Oct 2026): nothing recurring posts by itself.

// RecurringBillInput is what a person sets on a recurring bill.
type RecurringBillInput struct {
	Name       string            `json:"name" minLength:"1" maxLength:"120" doc:"What it is, as the list shows it." example:"Office rent"`
	Schedule   RecurringSchedule `json:"schedule"`
	SupplierID string            `json:"supplier_id,omitempty" format:"uuid" doc:"The supplier of each draft: a contact marked as a vendor." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e20"`
	Currency   string            `json:"currency" pattern:"^[A-Z]{3}$" doc:"The currency of each draft. One in another currency takes the cascade's rate for its date when it posts (ENG-40)." example:"SGD"`
	Lines      []BillLineDraft   `json:"lines" maxItems:"200" doc:"The lines of each draft, as on a bill."`
	Notes      string            `json:"notes,omitempty" maxLength:"2000" doc:"The notes of each draft." example:"Monthly rent, unit 05-12"`
	Nature     string            `json:"nature_of_payment,omitempty" enum:"goods,software_use,software_rights,services_in_singapore,services_outside_singapore,interest,movable_rent,other" doc:"For a supplier resident outside Singapore: what each draft's payment is for (FR-AP-19), as on a bill." example:"software_use"`
}

// RecurringBill is a recurring bill at its version (FR-AP-09; PRD-001 rev 0.84).
type RecurringBill struct {
	ID             string            `json:"id" format:"uuid" doc:"The recurring bill's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e18"`
	Name           string            `json:"name" doc:"What it is." example:"Office rent"`
	Status         string            `json:"status" enum:"active,paused,ended" doc:"active makes its drafts; paused makes none until it is resumed; ended has no occurrence left." example:"active"`
	Schedule       RecurringSchedule `json:"schedule"`
	NextOn         string            `json:"next_on,omitempty" format:"date" doc:"The next occurrence the scheduler makes a draft bill for, dated that day; absent once it has ended." example:"2026-12-01"`
	LastOn         string            `json:"last_on,omitempty" format:"date" doc:"The last occurrence a draft was made for." example:"2026-11-01"`
	LastBillID     string            `json:"last_bill_id,omitempty" format:"uuid" doc:"That draft bill." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d71"`
	Supplier       *BillSupplier     `json:"supplier,omitempty" doc:"The supplier of each draft, with its name."`
	Currency       string            `json:"currency" doc:"The currency of each draft." example:"SGD"`
	Lines          []BillLineDraft   `json:"lines" nullable:"false" doc:"The lines of each draft."`
	Notes          string            `json:"notes,omitempty" doc:"The notes of each draft." example:"Monthly rent, unit 05-12"`
	Nature         string            `json:"nature_of_payment,omitempty" doc:"What each draft's payment is for, for a supplier resident outside Singapore (FR-AP-19)." example:"software_use"`
	Total          Money             `json:"total" doc:"Each draft's total: every line's net and GST, in its currency."`
	Version        int               `json:"version" doc:"The version of the recurring bill; it makes the ETag." example:"3"`
	ContentVersion int               `json:"content_version" doc:"The version of its content, which drafts name; it changes when the supplier, currency, lines, notes or nature of payment change." example:"2"`
	EditedBy       *Person           `json:"edited_by,omitempty" doc:"Who wrote the current content: they do not approve the posting of its drafts (SOD-3)."`
	EditedAt       time.Time         `json:"edited_at" doc:"When the current content was written." example:"2026-10-23T09:30:00Z"`
	Checks         []JournalCheck    `json:"checks" nullable:"false" doc:"Its statuses (Appendix C.1 #28, #29): every problem the bill rules find in its content (a missing rate aside, which a draft takes when it posts), as Templates incomplete (✖); a warning when an occurrence before today has no draft yet; or OK, Paused or Ended."`
}

// RecurringBillPage is a page of recurring bills, by name.
type RecurringBillPage struct {
	Items      []RecurringBill `json:"items" nullable:"false"`
	NextCursor string          `json:"next_cursor,omitempty" doc:"Pass it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6Ik9mZmljZSByZW50fDAxOTIifQ"`
}

// RecurringBillPath names a recurring bill.
type RecurringBillPath struct {
	RecurringBillID string `path:"recurring_bill_id" format:"uuid" doc:"The recurring bill's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e18"`
}

type recurringBillCreateInput struct {
	KeyHeader
	PreferHeader
	Body RecurringBillInput
}

type recurringBillReplaceInput struct {
	RecurringBillPath
	IfMatchHeader
	PreferHeader
	Body RecurringBillInput
}

type recurringBillStateInput struct {
	RecurringBillPath
	KeyHeader
	IfMatchHeader
	PreferHeader
}

type recurringBillListOutput struct{ Body RecurringBillPage }

type recurringBillReadOutput struct {
	ETag string `header:"ETag" doc:"The recurring bill's version, for If-Match."`
	Body RecurringBill
}

type recurringBillWriteOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The recurring bill's version, for If-Match."`
	Location string `header:"Location" doc:"The recurring bill's URL."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     RecurringBill
}

// recurringBillOf is a recurring bill as the API shows it, with its statuses.
func recurringBillOf(ctx context.Context, u storage.UnitOfWork, base money.Currency, r storage.RecurringBill, st []journal.Status) (RecurringBill, error) {
	d, err := journal.ReadBillContent(r.Content, base)
	if err != nil {
		return RecurringBill{}, err
	}
	total, err := d.Total()
	if err != nil {
		return RecurringBill{}, err
	}
	out := RecurringBill{ID: r.ID.String(), Name: r.Name, Status: string(r.Status), NextOn: optDate(r.NextOn), LastOn: optDate(r.LastOn),
		Schedule: RecurringSchedule{EveryMonths: r.Schedule.EveryMonths, DayOfMonth: r.Schedule.DayOfMonth,
			StartsOn: r.Schedule.StartsOn.Format(time.DateOnly), EndsOn: optDate(r.Schedule.EndsOn)},
		Currency: d.Currency.Code, Lines: billLinesOut(d), Notes: d.Notes, Nature: d.Nature, Total: moneyOut(total), Version: r.Version,
		ContentVersion: r.ContentVersion, EditedBy: personOf(r.EditedBy), EditedAt: r.EditedAt.UTC(), Checks: checksOf(st)}
	if r.NextOn == nil {
		out.Status = "ended"
	}
	if r.LastBillID != nil {
		out.LastBillID = r.LastBillID.String()
	}
	if d.SupplierID != uuid.Nil {
		out.Supplier = &BillSupplier{ID: d.SupplierID.String()}
		if c, err := u.Contacts().Get(ctx, d.SupplierID); err == nil {
			out.Supplier.Name = c.LegalName
		}
	}
	return out, nil
}

// recurringBillInputOf reads a recurring bill from the request, with every invalid field at once.
func recurringBillInputOf(in RecurringBillInput, base money.Currency) (journal.RecurringBillInput, error) {
	out := journal.RecurringBillInput{Name: strings.TrimSpace(in.Name)}
	var bad []FieldProblem
	if out.Name == "" {
		bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.name", Message: "is empty", Fix: "Name the recurring bill"})
	}
	starts, err := time.Parse(time.DateOnly, in.Schedule.StartsOn)
	if err != nil {
		bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.schedule.starts_on", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"})
	}
	out.Schedule = storage.Schedule{EveryMonths: in.Schedule.EveryMonths, DayOfMonth: in.Schedule.DayOfMonth, StartsOn: starts}
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
	d, err := billDraftOf(BillDraft{SupplierID: in.SupplierID, Currency: in.Currency, Lines: in.Lines, Notes: in.Notes, Nature: in.Nature}, base)
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

// viewRecurringBills are recurring bills with their statuses, and the names of who wrote their content.
func (a *API) viewRecurringBills(ctx context.Context, u storage.UnitOfWork, rs ...storage.RecurringBill) ([]RecurringBill, error) {
	today, err := orgToday(ctx, a, u)
	if err != nil {
		return nil, err
	}
	base, err := baseOf(ctx, u)
	if err != nil {
		return nil, err
	}
	st, err := journal.RecurringBillStatuses(ctx, u, rs, today)
	if err != nil {
		return nil, err
	}
	out := make([]RecurringBill, 0, len(rs))
	for _, r := range rs {
		v, err := recurringBillOf(ctx, u, base, r, st[r.ID])
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

// RecurringBillCodes are the rules a recurring bill's changes may answer.
var RecurringBillCodes = []string{journal.CodeRecurringSchedule, journal.CodeRecurringPaused, storage.CodeVersion,
	money.CodeNotDecimal, money.CodePrecision, money.CodeRange, money.CodeCurrency, money.CodeRate, storage.CodeRetry, storage.CodeLockTimeout}

func registerRecurringBills(a *API) {
	register(a, authz.ListRecurringBills, huma.Operation{
		OperationID: "listRecurringBills", Method: http.MethodGet, Path: "/v1/recurring-bills", Tags: []string{"Purchases"},
		Summary: "List the recurring bills",
		Description: "The organisation's recurring bills by name, a page at a time (FR-AP-09; PRD-001 rev 0.84): each one's schedule, next " +
			"occurrence, content and statuses. Read-only; no side effects.",
	}, []string{CodeCursor}, func(ctx context.Context, in *recurringListInput) (*recurringBillListOutput, error) {
		after, err := decodeCursor(in.Cursor)
		if err != nil {
			return nil, err
		}
		afterName, afterID, err := recurringCursor(after)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		out := recurringBillListOutput{Body: RecurringBillPage{Items: []RecurringBill{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			page, err := u.RecurringBills().Page(ctx, afterName, afterID, in.PageSize+1)
			if err != nil {
				return err
			}
			if len(page) > in.PageSize {
				page = page[:in.PageSize]
				last := page[len(page)-1]
				out.Body.NextCursor = encodeCursor(last.Name + "|" + last.ID.String())
			}
			out.Body.Items, err = a.viewRecurringBills(ctx, u, page...)
			return err
		})
		return &out, err
	})

	register(a, authz.CreateRecurringBill, huma.Operation{
		OperationID: "createRecurringBill", Method: http.MethodPost, Path: "/v1/recurring-bills", Tags: []string{"Purchases"},
		DefaultStatus: http.StatusCreated, Summary: "Add a recurring bill",
		Description: "Adds a recurring bill, active: a bill's supplier, currency, lines and notes, and a schedule (every 1, 3, 6 or 12 " +
			"months on a day of the month, from a start, until an optional end; FR-AP-09, FR-AR-07; PRD-001 rev 0.84). The scheduler " +
			"makes the draft bill of each occurrence, dated on it, as scheduler:bills.recurring; a person adds the supplier's number " +
			"and posts it under the usual approvals: nothing recurring posts by itself, and whoever wrote the content does not approve " +
			"its drafts (SOD-3). Its content may be incomplete: its statuses say what the bill rules would refuse. Side effects: the " +
			"recurring bill, its first content version and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview " +
			"checks it and saves nothing.",
	}, append([]string{CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		CodeInvalid}, RecurringBillCodes...),
		func(ctx context.Context, in *recurringBillCreateInput) (*recurringBillWriteOutput, error) {
			p, _ := authn.From(ctx)
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (RecurringBill, error) {
				base, err := baseOf(ctx, u)
				if err != nil {
					return RecurringBill{}, err
				}
				input, err := recurringBillInputOf(in.Body, base)
				if err != nil {
					return RecurringBill{}, err
				}
				r, err := a.bills.CreateRecurring(ctx, p.Organisation, input)
				if err != nil {
					return RecurringBill{}, err
				}
				v, err := a.viewRecurringBills(ctx, u, r)
				if err != nil {
					return RecurringBill{}, err
				}
				return v[0], nil
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &recurringBillWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &recurringBillWriteOutput{Status: w.Status, ETag: etag(w.Body.Version), Location: "/v1/recurring-bills/" + w.Body.ID,
				Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})

	register(a, authz.GetRecurringBill, huma.Operation{
		OperationID: "getRecurringBill", Method: http.MethodGet, Path: "/v1/recurring-bills/{recurring_bill_id}", Tags: []string{"Purchases"},
		Summary:     "Get a recurring bill",
		Description: "Returns a recurring bill with its statuses and its ETag (FR-AP-09; PRD-001 rev 0.84). Read-only; no side effects.",
	}, []string{CodeNotFound}, func(ctx context.Context, in *RecurringBillPath) (*recurringBillReadOutput, error) {
		id, err := parseID("recurring_bill_id", in.RecurringBillID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out recurringBillReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			r, err := u.RecurringBills().Get(ctx, id)
			if err != nil {
				return err
			}
			v, err := a.viewRecurringBills(ctx, u, r)
			if err != nil {
				return err
			}
			out.Body, out.ETag = v[0], etag(r.Version)
			return nil
		})
		return &out, err
	})

	register(a, authz.ReplaceRecurringBill, huma.Operation{
		OperationID: "replaceRecurringBill", Method: http.MethodPut, Path: "/v1/recurring-bills/{recurring_bill_id}", Tags: []string{"Purchases"},
		Summary: "Change a recurring bill",
		Description: "Replaces a recurring bill's name, schedule and content (FR-AP-09; PRD-001 rev 0.84). A changed supplier, currency, " +
			"lines or notes is a new content version, written by the caller; drafts already made keep theirs. A changed schedule " +
			"takes effect after the last occurrence made. Side effects: the next version and its audit event. Needs If-Match with the " +
			"recurring bill's ETag. Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid}, RecurringBillCodes...),
		func(ctx context.Context, in *recurringBillReplaceInput) (*recurringBillWriteOutput, error) {
			id, err := parseID("recurring_bill_id", in.RecurringBillID)
			if err != nil {
				return nil, err
			}
			p, _ := authn.From(ctx)
			w, err := write(ctx, a, "", in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (RecurringBill, error) {
				cur, err := u.RecurringBills().Get(ctx, id)
				if err != nil {
					return RecurringBill{}, err
				}
				if err := checkIfMatch(in.IfMatch, etag(cur.Version)); err != nil {
					return RecurringBill{}, err
				}
				base, err := baseOf(ctx, u)
				if err != nil {
					return RecurringBill{}, err
				}
				input, err := recurringBillInputOf(in.Body, base)
				if err != nil {
					return RecurringBill{}, err
				}
				r, err := a.bills.UpdateRecurring(ctx, p.Organisation, id, cur.Version, input)
				if err != nil {
					return RecurringBill{}, err
				}
				v, err := a.viewRecurringBills(ctx, u, r)
				if err != nil {
					return RecurringBill{}, err
				}
				return v[0], nil
			})
			if err != nil {
				return nil, err
			}
			return &recurringBillWriteOutput{Status: w.Status, ETag: etag(w.Body.Version), Applied: applied(w), Body: w.Body}, nil
		})

	state := func(policy authz.Policy, op, path, summary, description string, to storage.RecurringStatus) {
		register(a, policy, huma.Operation{
			OperationID: op, Method: http.MethodPost, Path: "/v1/recurring-bills/{recurring_bill_id}/" + path, Tags: []string{"Purchases"},
			Summary: summary, Description: description,
		}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld}, RecurringBillCodes...),
			func(ctx context.Context, in *recurringBillStateInput) (*recurringBillWriteOutput, error) {
				id, err := parseID("recurring_bill_id", in.RecurringBillID)
				if err != nil {
					return nil, err
				}
				p, _ := authn.From(ctx)
				w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (RecurringBill, error) {
					cur, err := u.RecurringBills().Get(ctx, id)
					if err != nil {
						return RecurringBill{}, err
					}
					if err := checkIfMatch(in.IfMatch, etag(cur.Version)); err != nil {
						return RecurringBill{}, err
					}
					var r storage.RecurringBill
					if to == storage.RecurringPaused {
						r, err = a.bills.PauseRecurring(ctx, p.Organisation, id, cur.Version)
					} else {
						today, terr := orgToday(ctx, a, u)
						if terr != nil {
							return RecurringBill{}, terr
						}
						r, err = a.bills.ResumeRecurring(ctx, p.Organisation, id, cur.Version, today)
					}
					if err != nil {
						return RecurringBill{}, err
					}
					v, err := a.viewRecurringBills(ctx, u, r)
					if err != nil {
						return RecurringBill{}, err
					}
					return v[0], nil
				})
				if err != nil {
					return nil, err
				}
				return &recurringBillWriteOutput{Status: w.Status, ETag: etag(w.Body.Version), Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
			})
	}
	state(authz.PauseRecurringBill, "pauseRecurringBill", "pause", "Pause a recurring bill",
		"Stops an active recurring bill making drafts until it is resumed (FR-AP-09; PRD-001 rev 0.84). Draft bills already made stay. "+
			"Side effects: the next version and its audit event. Needs If-Match; idempotent with Idempotency-Key; Prefer: "+
			"return=preview checks it and saves nothing.", storage.RecurringPaused)
	state(authz.ResumeRecurringBill, "resumeRecurringBill", "resume", "Resume a recurring bill",
		"Starts a paused recurring bill again (FR-AP-09; PRD-001 rev 0.84): its next draft is for its first occurrence on or after "+
			"today, so the occurrences it missed while paused are skipped, not made late. Side effects: the next version and its audit "+
			"event. Needs If-Match; idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.", storage.RecurringActive)
}
