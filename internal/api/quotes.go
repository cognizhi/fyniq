package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/quote"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------- quotes and sales orders (FR-AR-01; p1-sales)
//
// A quote to a customer, or a sales order a customer placed, has the lines an invoice would have and a validity date,
// and posts nothing: it is drafted, checked as an invoice of the same content is, sent (numbered QT- or SO-, posted with
// no entry) and cancelled (reversed, with no entry). The customer's answer (accepted, declined) is recorded once, and a
// quote past its validity date with no answer is expired by the scheduler. The pipeline adds up what the open ones
// come to in the base currency. Conversion to an invoice follows.

// QuoteDraft is a quote or a sales order as entered.
type QuoteDraft struct {
	Kind       string             `json:"kind,omitempty" enum:"quote,sales_order" doc:"quote, the default, or sales_order: an order the customer placed. Fixed once saved." example:"quote"`
	CustomerID string             `json:"customer_id,omitempty" format:"uuid" doc:"The customer: a contact marked as a customer." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f20"`
	Reference  string             `json:"reference,omitempty" maxLength:"60" doc:"The customer's reference for it." example:"RFQ-2291"`
	Date       string             `json:"date,omitempty" format:"date" doc:"The quote's date." example:"2026-10-24"`
	ValidUntil string             `json:"valid_until,omitempty" format:"date" doc:"The last day the quote is valid: required for a quote, on or after its date (QUOTE-03); an order may name none." example:"2026-11-23"`
	Currency   string             `json:"currency" pattern:"^[A-Z]{3}$" doc:"The quote's currency." example:"SGD"`
	FXRate     string             `json:"fx_rate,omitempty" doc:"For a quote in another currency: base currency per one unit. Left out, the cascade's rate for its date (ENG-40)." example:"1.34"`
	Lines      []InvoiceLineDraft `json:"lines" maxItems:"200" doc:"Its lines, as an invoice's."`
	Notes      string             `json:"notes,omitempty" maxLength:"2000" doc:"Notes kept with it." example:"Prices valid for 30 days"`
}

// Quote is a quote or a sales order: its current version, its status, and what it comes to.
type Quote struct {
	ID         string           `json:"id" format:"uuid" doc:"The quote's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4fb0"`
	Kind       string           `json:"kind" enum:"quote,sales_order" doc:"A quote, or a sales order." example:"quote"`
	Number     string           `json:"number,omitempty" doc:"Its number from its sequence, given when it is sent (QT- or SO-); a draft has none." example:"QT-0007"`
	Status     string           `json:"status" enum:"draft,sent,accepted,declined,expired,cancelled" doc:"draft (✎); sent; accepted or declined by the customer; expired, past its validity date with no answer; or cancelled (FR-AR-01). Invoicing follows." example:"sent"`
	Version    int              `json:"version" doc:"The version of its content; with the status it makes the ETag." example:"1"`
	Customer   *InvoiceCustomer `json:"customer,omitempty" doc:"The customer, with its name."`
	Reference  string           `json:"reference,omitempty" doc:"The customer's reference for it." example:"RFQ-2291"`
	Date       string           `json:"date,omitempty" format:"date" doc:"Its date." example:"2026-10-24"`
	ValidUntil string           `json:"valid_until,omitempty" format:"date" doc:"The last day it is valid." example:"2026-11-23"`
	Currency   string           `json:"currency" doc:"Its currency." example:"SGD"`
	FXRate     string           `json:"fx_rate,omitempty" doc:"The rate typed for it, if one was." example:"1.34"`
	Lines      []InvoiceLine    `json:"lines" nullable:"false" doc:"Its lines, each with its net."`
	Notes      string           `json:"notes,omitempty" doc:"Its notes." example:"Prices valid for 30 days"`
	GSTCharged bool             `json:"gst_charged" doc:"Whether it charges GST: the organisation is GST-registered on its date." example:"true"`
	GST        []InvoiceGST     `json:"gst_subtotals" nullable:"false" doc:"Its GST by GST code under RND-GST-EXCL."`
	Net        *Money           `json:"net,omitempty" doc:"Every line's net, in its currency."`
	GSTTotal   *Money           `json:"gst,omitempty" doc:"Its GST, in its currency."`
	Total      *Money           `json:"total,omitempty" doc:"Its net and GST, in its currency; absent while a line cannot be read."`
	Base       *Money           `json:"base,omitempty" doc:"Its total in the base currency at its rate: what it adds to the pipeline while it is open."`
	Checks     []InvoiceCheck   `json:"checks" nullable:"false" doc:"getQuote only: what stops a draft being sent (✖)."`
	PreparedBy *Person          `json:"prepared_by,omitempty" doc:"Who saved the current version."`
	SentBy     *Person          `json:"sent_by,omitempty" doc:"Who sent it."`
	SentAt     *time.Time       `json:"sent_at,omitempty" doc:"When it was sent." example:"2026-10-24T09:30:00Z"`
	Answer     *QuoteAnswered   `json:"answer,omitempty" doc:"The customer's answer, or its expiry, once recorded."`
}

// QuoteAnswered is the customer's answer to a quote, or its expiry, as recorded.
type QuoteAnswered struct {
	Outcome    string    `json:"outcome" enum:"accepted,declined,expired" doc:"What happened." example:"accepted"`
	Date       string    `json:"date" format:"date" doc:"The day the customer answered, or it expired." example:"2026-10-30"`
	Name       string    `json:"name,omitempty" doc:"Who accepted it on the customer's side, as recorded." example:"Jane Tan"`
	Note       string    `json:"note,omitempty" doc:"The note kept with it." example:"Confirmed by email"`
	RecordedBy *Person   `json:"recorded_by,omitempty" doc:"Who recorded it: a person, or the scheduler for an expiry."`
	RecordedAt time.Time `json:"recorded_at" doc:"When it was recorded." example:"2026-10-30T09:30:00Z"`
}

// QuoteAnswer is the customer's answer, as a person records it.
type QuoteAnswer struct {
	Date string `json:"date,omitempty" format:"date" doc:"The day the customer answered; left out, the organisation's today." example:"2026-10-30"`
	Name string `json:"name,omitempty" maxLength:"200" doc:"Who answered on the customer's side." example:"Jane Tan"`
	Note string `json:"note,omitempty" maxLength:"2000" doc:"A note, such as how they answered." example:"Confirmed by email"`
}

// QuotePage is a page of quotes, newest date first.
type QuotePage struct {
	Items      []Quote `json:"items" nullable:"false"`
	NextCursor string  `json:"next_cursor,omitempty" doc:"Pass it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjIwMjYtMTAtMzF8MDE5MiJ9"`
}

// QuotePath names a quote in the path.
type QuotePath struct {
	QuoteID string `path:"quote_id" format:"uuid" doc:"The quote's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4fb0"`
}

// QuoteCancel is why a sent quote or order is cancelled.
type QuoteCancel struct {
	Reason string `json:"reason" minLength:"1" maxLength:"500" doc:"Why it is cancelled, kept in the audit trail." example:"The customer chose another supplier"`
}

// PipelineLine is what one kind of open document adds to the pipeline.
type PipelineLine struct {
	Count int   `json:"count" doc:"How many are open." example:"3"`
	Total Money `json:"total" doc:"What they come to in the base currency, each at its rate."`
}

// Pipeline is the sales pipeline (FR-AR-01): what the open quotes and sales orders come to in the base currency.
type Pipeline struct {
	AsAt   string       `json:"as_at" format:"date" doc:"The day it is read for." example:"2026-10-24"`
	Quotes PipelineLine `json:"quotes" doc:"The quotes sent or accepted and still valid on the day."`
	Orders PipelineLine `json:"orders" doc:"The sales orders sent or accepted, still valid on the day when they name a validity date."`
	Total  Money        `json:"total" doc:"Both together."`
}

type quoteListInput struct {
	PageQuery
	Kind   string `query:"kind" enum:"quote,sales_order" default:"quote" doc:"Quotes, the default, or sales orders." example:"quote"`
	Status string `query:"status" enum:"draft,sent,cancelled" doc:"Only those with this status of their document: draft, sent (whatever the customer answered, or expired) or cancelled." example:"sent"`
	From   string `query:"from" format:"date" doc:"Only those dated on or after this day." example:"2026-10-01"`
	To     string `query:"to" format:"date" doc:"Only those dated on or before this day." example:"2026-10-31"`
	Q      string `query:"q" maxLength:"100" doc:"Only those whose number contains this text, ignoring case." example:"QT-0007"`
}

type quoteListOutput struct{ Body QuotePage }

type createQuoteInput struct {
	KeyHeader
	PreferHeader
	Body QuoteDraft
}

type replaceQuoteInput struct {
	QuotePath
	IfMatchHeader
	PreferHeader
	Body QuoteDraft
}

type quoteCommandInput struct {
	QuotePath
	KeyHeader
	IfMatchHeader
	PreferHeader
}

type quoteAnswerInput struct {
	QuotePath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body QuoteAnswer
}

type quoteCancelInput struct {
	QuotePath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body QuoteCancel
}

type quoteOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The quote's version and status, for If-Match."`
	Location string `header:"Location" doc:"The quote's URL."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     Quote
}

type quoteReadOutput struct {
	ETag string `header:"ETag" doc:"The quote's version and status, for If-Match."`
	Body Quote
}

type pipelineInput struct {
	AsAt string `query:"as_at" format:"date" doc:"The day to read it for; left out, the organisation's today." example:"2026-10-24"`
}

type pipelineOutput struct{ Body Pipeline }

func quoteETag(q Quote) string { return etag(q.Version, q.Status) }

func quoteKind(doc storage.Document) string {
	if doc.Type == storage.DocumentSalesOrder {
		return "sales_order"
	}
	return "quote"
}

func quoteType(kind string) storage.DocumentType {
	if kind == "sales_order" {
		return storage.DocumentSalesOrder
	}
	return storage.DocumentQuote
}

// quoteDraftOf reads a quote from the request, with every invalid field at once, through an invoice's content.
func quoteDraftOf(in QuoteDraft, base money.Currency) (quote.Draft, error) {
	c, err := invoiceDraftOf(InvoiceDraft{CustomerID: in.CustomerID, Reference: in.Reference, Date: in.Date, Currency: in.Currency, FXRate: in.FXRate,
		Lines: in.Lines, Notes: in.Notes}, base)
	d := quote.Draft{Content: c}
	var fields []FieldProblem
	if in.ValidUntil != "" {
		t, perr := time.Parse(time.DateOnly, in.ValidUntil)
		if perr != nil {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.valid_until", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"})
		}
		d.ValidUntil = t
	}
	if err != nil {
		return d, err
	}
	if len(fields) > 0 {
		return d, invalid(fields)
	}
	return d, nil
}

// quoteOf is a quote as the API shows it, from its document. withChecks fills the checks of a draft (getQuote).
func (a *API) quoteOf(ctx context.Context, u storage.UnitOfWork, base money.Currency, doc storage.Document, withChecks bool) (Quote, error) {
	d, err := quote.Read(doc, base)
	if err != nil {
		return Quote{}, err
	}
	c := d.Content
	outcomes, err := u.QuoteOutcomes().Of(ctx, []uuid.UUID{doc.ID})
	if err != nil {
		return Quote{}, err
	}
	var outcome *storage.QuoteOutcome
	if o, ok := outcomes[doc.ID]; ok {
		outcome = &o
	}
	out := Quote{ID: doc.ID.String(), Kind: quoteKind(doc), Number: doc.Number, Status: quote.Status(doc, outcome), Version: doc.Version, Reference: c.Reference,
		Date: dayText(c.Date), ValidUntil: dayText(d.ValidUntil), Currency: c.Currency.Code, Lines: invoiceLinesOut(c), Notes: c.Notes, GST: []InvoiceGST{},
		Checks: []InvoiceCheck{}, PreparedBy: personOf(doc.SavedBy), SentBy: personOf(doc.PostedBy), SentAt: utc(doc.PostedAt)}
	if c.Rate != nil {
		out.FXRate = c.Rate.String()
	}
	if c.CustomerID != uuid.Nil {
		out.Customer = &InvoiceCustomer{ID: c.CustomerID.String()}
		if x, err := u.Contacts().Get(ctx, c.CustomerID); err == nil {
			out.Customer.Name = x.LegalName
		}
	}
	if outcome != nil {
		out.Answer = &QuoteAnswered{Outcome: outcome.Outcome, Date: dayText(outcome.On), Name: outcome.Name, Note: outcome.Note,
			RecordedBy: personOf(outcome.RecordedBy), RecordedAt: outcome.RecordedAt.UTC()}
	}
	p, probs, err := journal.CheckQuote(ctx, u, doc)
	if err != nil {
		return Quote{}, err
	}
	out.GSTCharged = p.Invoice.Registered
	if !probs.Has(ledger.CodeGSTRate) && len(p.Invoice.Invoice.Lines) == len(c.Lines) {
		t := p.Invoice.Totals
		net, gst, total := moneyOut(t.Net), moneyOut(t.GST), moneyOut(t.Total)
		out.Net, out.GSTTotal, out.Total = &net, &gst, &total
		for _, s := range t.Subtotals {
			out.GST = append(out.GST, InvoiceGST{GSTCode: s.Code, Rate: s.Rate, Net: moneyOut(s.Net), GST: moneyOut(s.GST)})
		}
		if !p.Base.IsZero() {
			b := moneyOut(p.Base)
			out.Base = &b
		}
	}
	if withChecks && doc.Status == storage.DocumentDraft {
		for _, x := range probs {
			out.Checks = append(out.Checks, InvoiceCheck{Severity: "error", Code: x.Code, Field: journalField(x.Field), Message: x.Message, Fix: x.Fix})
		}
	}
	var answeredBy *Person
	if out.Answer != nil {
		answeredBy = out.Answer.RecordedBy
	}
	return out, a.name(ctx, u, out.PreparedBy, out.SentBy, answeredBy)
}

// quoteProblems turns the checks' problems into the API's, naming the quote.
func quoteProblems(err error) error {
	probs, ok := err.(ledger.Problems)
	if !ok || len(probs) == 0 {
		return err
	}
	fields := make([]FieldProblem, 0, len(probs))
	for _, x := range probs {
		fields = append(fields, FieldProblem{Code: x.Code, Field: journalField(x.Field), Message: x.Message, Fix: x.Fix})
	}
	return problem(probs[0].Code, fmt.Sprintf("The quote cannot be sent: %d problem(s), each listed in errors.", len(probs)),
		"Correct the quote and send it again.", fields...)
}

func registerQuotes(a *API) {
	formCodes := []string{CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, money.CodeNotDecimal, money.CodePrecision, money.CodeRange,
		money.CodeCurrency, money.CodeRate, invoice.CodeTooLong, invoice.CodeLine, quote.CodeContent, storage.CodeRetry, storage.CodeLockTimeout}
	readCodes := []string{CodeNotFound, quote.CodeNotQuote, quote.CodeBadDraft, invoice.CodeBadDraft}

	register(a, authz.ListQuotes, huma.Operation{
		OperationID: "listQuotes", Method: http.MethodGet, Path: "/v1/quotes", Tags: []string{"Sales"},
		Summary: "List the quotes or sales orders",
		Description: "The organisation's quotes, or its sales orders, at their current version, newest date first (a draft without a date " +
			"last), a page at a time, narrowed by status, dates and text in the number (FR-AR-01). Each has its customer, totals, " +
			"validity date and status. Read-only; no side effects.",
	}, append([]string{CodeCursor, CodeInvalid}, readCodes...), func(ctx context.Context, in *quoteListInput) (*quoteListOutput, error) {
		after, err := decodeCursor(in.Cursor)
		if err != nil {
			return nil, err
		}
		afterDate, afterID, err := journalCursor(after)
		if err != nil {
			return nil, err
		}
		status := map[string]storage.DocumentStatus{"draft": storage.DocumentDraft, "sent": storage.DocumentPosted, "cancelled": storage.DocumentReversed}[in.Status]
		p, _ := authn.From(ctx)
		out := quoteListOutput{Body: QuotePage{Items: []Quote{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			docs, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: quoteType(in.Kind), Status: status, From: in.From, To: in.To,
				Search: strings.TrimSpace(in.Q), AfterDate: afterDate, AfterID: afterID, Limit: in.PageSize + 1})
			if err != nil {
				return err
			}
			if len(docs) > in.PageSize {
				docs = docs[:in.PageSize]
				last := docs[len(docs)-1]
				out.Body.NextCursor = encodeCursor(last.Date + "|" + last.ID.String())
			}
			base, err := baseOf(ctx, u)
			if err != nil {
				return err
			}
			for _, d := range docs {
				q, err := a.quoteOf(ctx, u, base, d, false)
				if err != nil {
					return err
				}
				out.Body.Items = append(out.Body.Items, q)
			}
			return nil
		})
		return &out, err
	})

	register(a, authz.GetSalesPipeline, huma.Operation{
		OperationID: "getSalesPipeline", Method: http.MethodGet, Path: "/v1/quotes/pipeline", Tags: []string{"Sales"},
		Summary: "Get the sales pipeline",
		Description: "What the open quotes and sales orders come to in the base currency on a day (FR-AR-01): those sent or accepted, " +
			"not declined, expired or cancelled, and still valid on it when they name a validity date, each total at its rate. Read-only; no " +
			"side effects.",
	}, []string{CodeInvalid}, func(ctx context.Context, in *pipelineInput) (*pipelineOutput, error) {
		p, _ := authn.From(ctx)
		var out Pipeline
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			base, err := baseOf(ctx, u)
			if err != nil {
				return err
			}
			day := in.AsAt
			if day == "" {
				t, err := orgToday(ctx, a, u)
				if err != nil {
					return err
				}
				day = dateOf(t)
			}
			out = Pipeline{AsAt: day, Quotes: PipelineLine{Total: moneyOut(money.Zero(base))}, Orders: PipelineLine{Total: moneyOut(money.Zero(base))}}
			totals := map[storage.DocumentType]money.Money{storage.DocumentQuote: money.Zero(base), storage.DocumentSalesOrder: money.Zero(base)}
			counts := map[storage.DocumentType]int{}
			for _, t := range []storage.DocumentType{storage.DocumentQuote, storage.DocumentSalesOrder} {
				f := storage.DocumentFilter{Type: t, Status: storage.DocumentPosted, To: day, Limit: 500}
				for {
					docs, err := u.Documents().Page(ctx, f)
					if err != nil {
						return err
					}
					ids := make([]uuid.UUID, 0, len(docs))
					for _, doc := range docs {
						ids = append(ids, doc.ID)
					}
					answered, err := u.QuoteOutcomes().Of(ctx, ids)
					if err != nil {
						return err
					}
					on, err := time.Parse(time.DateOnly, day)
					if err != nil {
						return invalid([]FieldProblem{{Code: CodeInvalid, Field: "as_at", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"}})
					}
					for _, doc := range docs {
						qp, probs, err := journal.CheckQuote(ctx, u, doc)
						if err != nil {
							return err
						}
						var o *storage.QuoteOutcome
						if x, ok := answered[doc.ID]; ok {
							o = &x
						}
						if !quote.Open(doc, o, qp.Draft.ValidUntil, on) || len(probs) > 0 && qp.Base.IsZero() {
							continue // declined, expired, past its validity date, or what it comes to cannot be read
						}
						totals[t], _ = totals[t].Add(qp.Base)
						counts[t]++
					}
					if len(docs) < f.Limit {
						break
					}
					last := docs[len(docs)-1]
					f.AfterDate, f.AfterID = last.Date, &last.ID
				}
			}
			out.Quotes = PipelineLine{Count: counts[storage.DocumentQuote], Total: moneyOut(totals[storage.DocumentQuote])}
			out.Orders = PipelineLine{Count: counts[storage.DocumentSalesOrder], Total: moneyOut(totals[storage.DocumentSalesOrder])}
			all, _ := totals[storage.DocumentQuote].Add(totals[storage.DocumentSalesOrder])
			out.Total = moneyOut(all)
			return nil
		})
		return &pipelineOutput{Body: out}, err
	})

	register(a, authz.GetQuote, huma.Operation{
		OperationID: "getQuote", Method: http.MethodGet, Path: "/v1/quotes/{quote_id}", Tags: []string{"Sales"},
		Summary: "Get a quote or sales order",
		Description: "Returns a quote or sales order with its ETag: its customer, lines, totals, what it comes to in the base currency, " +
			"validity date, status, and for a draft what stops it being sent. Read-only; no side effects.",
	}, readCodes, func(ctx context.Context, in *QuotePath) (*quoteReadOutput, error) {
		id, err := parseID("quote_id", in.QuoteID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out Quote
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := u.Documents().Get(ctx, id)
			if err != nil {
				return err
			}
			base, err := baseOf(ctx, u)
			if err != nil {
				return err
			}
			out, err = a.quoteOf(ctx, u, base, doc, true)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &quoteReadOutput{ETag: quoteETag(out), Body: out}, nil
	})

	register(a, authz.CreateQuote, huma.Operation{
		OperationID: "createQuote", Method: http.MethodPost, Path: "/v1/quotes", Tags: []string{"Sales"}, DefaultStatus: http.StatusCreated,
		Summary: "Save a draft quote or sales order",
		Description: "Saves a quote to a customer, or with kind sales_order an order the customer placed, as a draft (✎) at version 1 " +
			"(FR-AR-01), without a number: it takes the next from its sequence when it is sent. Its lines are an invoice's; a quote " +
			"names the last day it is valid. It posts nothing. Side effects: a draft document and its audit event. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld}, formCodes...),
		func(ctx context.Context, in *createQuoteInput) (*quoteOutput, error) {
			p, _ := authn.From(ctx)
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (Quote, error) {
				base, err := baseOf(ctx, u)
				if err != nil {
					return Quote{}, err
				}
				d, err := quoteDraftOf(in.Body, base)
				if err != nil {
					return Quote{}, err
				}
				doc, err := a.quotes.Create(ctx, p.Organisation, d, in.Body.Kind == "sales_order")
				if err != nil {
					return Quote{}, err
				}
				return a.quoteOf(ctx, u, base, doc, false)
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &quoteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &quoteOutput{Status: w.Status, ETag: quoteETag(w.Body), Location: "/v1/quotes/" + w.Body.ID, Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})

	register(a, authz.ReplaceQuote, huma.Operation{
		OperationID: "replaceQuote", Method: http.MethodPut, Path: "/v1/quotes/{quote_id}", Tags: []string{"Sales"},
		Summary: "Replace a draft quote or sales order",
		Description: "Saves a draft's new content as its next version (FR-AR-01); its kind never changes. A sent quote never changes: " +
			"cancel it and send another (QUOTE-04). Needs If-Match with the draft's ETag. Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, quote.CodeNotDraft, quote.CodeNotQuote}, formCodes...),
		func(ctx context.Context, in *replaceQuoteInput) (*quoteOutput, error) {
			id, err := parseID("quote_id", in.QuoteID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			p, _ := authn.From(ctx)
			w, err := write(ctx, a, "", in.Prefer, in.Body, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (Quote, error) {
				doc, err := u.Documents().Lock(ctx, id)
				if err != nil {
					return Quote{}, err
				}
				base, err := baseOf(ctx, u)
				if err != nil {
					return Quote{}, err
				}
				before, err := a.quoteOf(ctx, u, base, doc, false)
				if err != nil {
					return Quote{}, err
				}
				if err := checkIfMatch(in.IfMatch, quoteETag(before)); err != nil {
					return Quote{}, err
				}
				if in.Body.Kind != "" && in.Body.Kind != before.Kind {
					return Quote{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.kind", Message: "is " + before.Kind + " and never changes",
						Fix: "Leave it out, or save a new document of the other kind"}})
				}
				d, err := quoteDraftOf(in.Body, base)
				if err != nil {
					return Quote{}, err
				}
				after, err := a.quotes.Revise(ctx, p.Organisation, id, d)
				if err != nil {
					return Quote{}, err
				}
				return a.quoteOf(ctx, u, base, after, false)
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &quoteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &quoteOutput{Status: w.Status, ETag: quoteETag(w.Body), Body: w.Body}, nil
		})

	command := func(ctx context.Context, rawID, key, ifMatch, prefer string, request any,
		run func(ctx context.Context, p authn.Principal, id uuid.UUID) (storage.Document, error)) (*quoteOutput, error) {
		id, err := parseID("quote_id", rawID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(ifMatch) == "" {
			return nil, checkIfMatch(ifMatch, "")
		}
		p, _ := authn.From(ctx)
		w, err := write(ctx, a, key, prefer, request, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (Quote, error) {
			doc, err := u.Documents().Lock(ctx, id)
			if err != nil {
				return Quote{}, err
			}
			base, err := baseOf(ctx, u)
			if err != nil {
				return Quote{}, err
			}
			before, err := a.quoteOf(ctx, u, base, doc, false)
			if err != nil {
				return Quote{}, err
			}
			if err := checkIfMatch(ifMatch, quoteETag(before)); err != nil {
				return Quote{}, err
			}
			after, err := run(ctx, p, id)
			if err != nil {
				return Quote{}, quoteProblems(err)
			}
			if after, err = u.Documents().Get(ctx, after.ID); err != nil {
				return Quote{}, err
			}
			return a.quoteOf(ctx, u, base, after, false)
		})
		if err != nil {
			return nil, err
		}
		if w.Previewed {
			return &quoteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
		}
		return &quoteOutput{Status: w.Status, ETag: quoteETag(w.Body), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	}
	commandCodes := append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		quote.CodeNotQuote, quote.CodeBadDraft, invoice.CodeBadDraft, storage.CodeRetry, storage.CodeLockTimeout}, append(InvoiceCodes, quote.CodeValidity)...)

	register(a, authz.SendQuote, huma.Operation{
		OperationID: "sendQuote", Method: http.MethodPost, Path: "/v1/quotes/{quote_id}/send", Tags: []string{"Sales"},
		Summary: "Send a quote or sales order",
		Description: "Sends a draft (FR-AR-01): it takes its number (QT- or SO-) and its status becomes sent; it posts nothing. It is checked " +
			"as an invoice of the same content is, and a quote needs a validity date on or after its date (QUOTE-03); every problem " +
			"comes back together, and a refused quote takes no number. Emailing it with its PDF follows (FR-AR-06). Side effects: the " +
			"document's change and its audit event. Needs If-Match with the draft's ETag. Idempotent with Idempotency-Key.",
	}, append([]string{quote.CodeNotDraft}, commandCodes...), func(ctx context.Context, in *quoteCommandInput) (*quoteOutput, error) {
		return command(ctx, in.QuoteID, in.IdempotencyKey, in.IfMatch, in.Prefer, in, func(ctx context.Context, p authn.Principal, id uuid.UUID) (storage.Document, error) {
			return a.quotes.Send(ctx, p.Organisation, id)
		})
	})

	answerCodes := append([]string{CodeInvalid, quote.CodeNotOpen, quote.CodeAnswered, quote.CodeAnswer, storage.CodeQuoteOutcome}, commandCodes...)
	answer := func(accept bool) func(ctx context.Context, in *quoteAnswerInput) (*quoteOutput, error) {
		return func(ctx context.Context, in *quoteAnswerInput) (*quoteOutput, error) {
			var on time.Time
			if in.Body.Date != "" {
				t, err := time.Parse(time.DateOnly, in.Body.Date)
				if err != nil {
					return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.date", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"}})
				}
				on = t
			}
			return command(ctx, in.QuoteID, in.IdempotencyKey, in.IfMatch, in.Prefer, in, func(ctx context.Context, p authn.Principal, id uuid.UUID) (storage.Document, error) {
				if on.IsZero() {
					if err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
						on, err = orgToday(ctx, a, u)
						return err
					}); err != nil {
						return storage.Document{}, err
					}
				}
				run := a.quotes.Decline
				if accept {
					run = a.quotes.Accept
				}
				if _, err := run(ctx, p.Organisation, id, journal.QuoteAnswer{On: on, Name: in.Body.Name, Note: in.Body.Note}); err != nil {
					return storage.Document{}, err
				}
				return storage.Document{ID: id}, nil
			})
		}
	}

	register(a, authz.AcceptQuote, huma.Operation{
		OperationID: "acceptQuote", Method: http.MethodPost, Path: "/v1/quotes/{quote_id}/accept", Tags: []string{"Sales"},
		Summary: "Record that the customer accepted a quote",
		Description: "Records that the customer accepted a sent quote or order (FR-AR-01), on the day given or the organisation's today, " +
			"with who accepted it and a note: its status becomes accepted, and it stays in the pipeline. A quote has one answer, which " +
			"never changes, and one that expired takes none (QUOTE-07). The customer's own acceptance through the online link follows " +
			"(FR-AR-16). Side effects: the answer and its audit event. Needs If-Match with the quote's ETag. Idempotent with Idempotency-Key.",
	}, answerCodes, answer(true))

	register(a, authz.DeclineQuote, huma.Operation{
		OperationID: "declineQuote", Method: http.MethodPost, Path: "/v1/quotes/{quote_id}/decline", Tags: []string{"Sales"},
		Summary: "Record that the customer declined a quote",
		Description: "Records that the customer declined a sent quote or order (FR-AR-01), on the day given or the organisation's today, " +
			"with a note: its status becomes declined, and it leaves the pipeline. A quote has one answer, which never changes " +
			"(QUOTE-07). Side effects: the answer and its audit event. Needs If-Match with the quote's ETag. Idempotent with Idempotency-Key.",
	}, answerCodes, answer(false))

	register(a, authz.CancelQuote, huma.Operation{
		OperationID: "cancelQuote", Method: http.MethodPost, Path: "/v1/quotes/{quote_id}/cancel", Tags: []string{"Sales"},
		Summary: "Cancel a quote or sales order",
		Description: "Cancels a sent quote or order, with a reason (FR-AR-01): its status becomes cancelled and it leaves the pipeline; it " +
			"posts nothing. A draft is simply not sent (QUOTE-05). Side effects: the document's change and its audit event with the " +
			"reason. Needs If-Match with the quote's ETag. Idempotent with Idempotency-Key.",
	}, append([]string{CodeInvalid, quote.CodeNotOpen, quote.CodeAnswered}, commandCodes...), func(ctx context.Context, in *quoteCancelInput) (*quoteOutput, error) {
		return command(ctx, in.QuoteID, in.IdempotencyKey, in.IfMatch, in.Prefer, in, func(ctx context.Context, p authn.Principal, id uuid.UUID) (storage.Document, error) {
			return a.quotes.Cancel(ctx, p.Organisation, id, strings.TrimSpace(in.Body.Reason))
		})
	})
}
