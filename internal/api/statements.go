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
	"github.com/cognizhi/fyniq/internal/domain/doctemplate"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/salesdoc"
	"github.com/cognizhi/fyniq/internal/domain/statement"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/salesmail"
)

// ------------------------------------------------------------- statements of account (FR-AR-08; p1-sales task 7.2)
//
// A customer's statement in one currency from one day to another: the balance brought forward, each document that moved
// what it owes with a running balance, the closing balance from the AR sub-ledger, and its ageing by days past due,
// which adds up to it. Worked out on request; made, it is kept as a statement document (ST-) that says what it said, and
// emailed with its PDF as an invoice is.

// StatementLine is a movement of what the customer owes.
type StatementLine struct {
	Date        string `json:"date" format:"date" doc:"The document's date, or the void's." example:"2026-10-14"`
	Kind        string `json:"kind" enum:"invoice,credit_note,receipt,refund,write_off,void" doc:"What moved the balance: an invoice, a credit note, a payment received, a refund of a credit, a write-off, or the void of one of them." example:"invoice"`
	DocumentID  string `json:"document_id" format:"uuid" doc:"The document." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f30"`
	Number      string `json:"number,omitempty" doc:"Its number." example:"INV-0012"`
	Description string `json:"description" doc:"The line as the statement prints it." example:"Invoice INV-0012, due 13 Nov 2026"`
	Amount      Money  `json:"amount" doc:"What it moved: positive when the customer owes more."`
	Balance     Money  `json:"balance" doc:"The running balance after it."`
}

// StatementAgeing is the closing balance by days past due on the statement's last day; with the credits, it adds up to it.
type StatementAgeing struct {
	NotYetDue Money `json:"not_yet_due" doc:"Not yet due, or due on the last day."`
	Days1To30 Money `json:"overdue_1_30" doc:"1 to 30 days past due."`
	Days31To  Money `json:"overdue_31_60" doc:"31 to 60 days past due."`
	Days61To  Money `json:"overdue_61_90" doc:"61 to 90 days past due."`
	Over90    Money `json:"overdue_over_90" doc:"More than 90 days past due."`
	Credits   Money `json:"unapplied_credits" doc:"Credit notes and payments not yet applied to an invoice: negative."`
}

// StatementCustomer is the customer a statement is of.
type StatementCustomer struct {
	ID   string `json:"id" format:"uuid" doc:"The contact's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f20"`
	Name string `json:"name" doc:"The contact's legal name." example:"Acme Analytics Pte Ltd"`
}

// Statement is a customer's statement of account in one currency: worked out, or made and kept as it was.
type Statement struct {
	ID       string            `json:"id,omitempty" format:"uuid" doc:"A made statement's ID; absent when worked out only." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f60"`
	Number   string            `json:"number,omitempty" doc:"A made statement's number." example:"ST-0001"`
	Date     string            `json:"date,omitempty" format:"date" doc:"The day it was made." example:"2026-10-31"`
	Customer StatementCustomer `json:"customer" doc:"Its customer."`
	Currency string            `json:"currency" doc:"Its currency, ISO 4217: a statement each currency the customer trades in." example:"SGD"`
	From     string            `json:"from" format:"date" doc:"Its first day." example:"2026-10-01"`
	To       string            `json:"to" format:"date" doc:"Its last day." example:"2026-10-31"`
	Opening  Money             `json:"opening_balance" doc:"Brought forward: what the customer owed at the end of the day before from."`
	Lines    []StatementLine   `json:"lines" nullable:"false" doc:"What moved the balance, by date."`
	Closing  Money             `json:"balance_due" doc:"What the customer owes at the end of to, from the AR sub-ledger."`
	Overdue  Money             `json:"overdue" doc:"What of the balance due is past due on to."`
	Ageing   StatementAgeing   `json:"ageing" doc:"The balance due by days past due."`
	AddsUp   bool              `json:"adds_up" doc:"The balance brought forward and the lines come to the balance due. A statement that does not is never made or sent." example:"true"`
	Version  int               `json:"version,omitempty" doc:"A made statement's version; it makes the ETag." example:"1"`

	Difference *Money            `json:"difference,omitempty" doc:"When it does not add up: the balance due less the balance brought forward and the lines."`
	Currencies []CurrencyCode    `json:"currencies,omitempty" doc:"Worked out: the currencies the customer's documents and balances are in over the period, a statement each." example:"[\"SGD\",\"USD\"]"`
	Sends      []DocumentSendOut `json:"sends,omitempty" doc:"A made statement's emails, newest first."`
}

// CurrencyCode is an ISO 4217 currency code.
type CurrencyCode string

// Schema implements huma.SchemaProvider: three capital letters, with an example.
func (CurrencyCode) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Pattern: "^[A-Z]{3}$", Description: "An ISO 4217 currency code.", Examples: []any{"SGD"}}
}

// StatementPage is a page of the statements made, newest first.
type StatementPage struct {
	Items      []Statement `json:"items" nullable:"false" doc:"The statements made."`
	NextCursor string      `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjAxOTI4ZjZlIn0"`
}

// StatementRequest names a statement: its customer, currency and period.
type StatementRequest struct {
	CustomerID string `json:"customer_id" format:"uuid" doc:"The customer." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f20"`
	Currency   string `json:"currency,omitempty" doc:"Its currency, ISO 4217; left out, the customer's own, else the organisation's base currency." example:"SGD"`
	From       string `json:"from" format:"date" doc:"Its first day." example:"2026-10-01"`
	To         string `json:"to" format:"date" doc:"Its last day, today at the latest; at most two years after from." example:"2026-10-31"`
}

// StatementPath names a statement made.
type StatementPath struct {
	StatementID string `path:"statement_id" format:"uuid" doc:"The statement's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f60"`
}

type customerStatementInput struct {
	ContactPath
	Currency string `query:"currency" doc:"Its currency, ISO 4217; left out, the customer's own, else the organisation's base currency." example:"SGD"`
	From     string `query:"from" required:"true" format:"date" doc:"Its first day." example:"2026-10-01"`
	To       string `query:"to" required:"true" format:"date" doc:"Its last day, today at the latest." example:"2026-10-31"`
}

type statementListInput struct {
	PageQuery
	CustomerID string `query:"customer_id" format:"uuid" doc:"Only this customer's statements." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f20"`
}

type statementCreateInput struct {
	KeyHeader
	PreferHeader
	Body StatementRequest
}

type emailStatementInput struct {
	StatementPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body *EmailDocument `required:"false"`
}

type statementReadOutput struct {
	ETag string `header:"ETag" doc:"The statement's version, for If-Match."`
	Body Statement
}

type statementWriteOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The statement's version, for If-Match."`
	Location string `header:"Location" doc:"The statement's URL."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     Statement
}

type statementListOutput struct{ Body StatementPage }

type customerStatementOutput struct{ Body Statement }

// ageingOut is an ageing's buckets and credits as the API shows them.
func ageingOut(buckets [5]money.Money, credits money.Money) StatementAgeing {
	return StatementAgeing{NotYetDue: moneyOut(buckets[0]), Days1To30: moneyOut(buckets[1]), Days31To: moneyOut(buckets[2]), Days61To: moneyOut(buckets[3]),
		Over90: moneyOut(buckets[4]), Credits: moneyOut(credits)}
}

// statementOut is a statement worked out.
func statementOut(s statement.Statement) Statement {
	out := Statement{Customer: StatementCustomer{ID: s.CustomerID.String(), Name: s.CustomerName}, Currency: s.Currency.Code, From: dateOf(s.From),
		To: dateOf(s.To), Opening: moneyOut(s.Opening), Lines: []StatementLine{}, Closing: moneyOut(s.Closing), Overdue: moneyOut(s.Overdue),
		Ageing: ageingOut(s.Ageing.Buckets, s.Ageing.Credits), AddsUp: s.AddsUp()}
	for _, l := range s.Lines {
		out.Lines = append(out.Lines, StatementLine{Date: dateOf(l.Date), Kind: l.Kind, DocumentID: l.DocumentID.String(), Number: l.Number,
			Description: l.Description(), Amount: moneyOut(l.Amount), Balance: moneyOut(l.Balance)})
	}
	if !s.AddsUp() {
		d := moneyOut(s.Difference)
		out.Difference = &d
	}
	return out
}

// madeStatementOf is a statement document as it was made, with its sends.
func madeStatementOf(ctx context.Context, u storage.UnitOfWork, doc storage.Document) (Statement, error) {
	s, err := statement.Read(doc)
	if err != nil {
		return Statement{}, err
	}
	c, err := u.Contacts().Get(ctx, s.CustomerID)
	if err != nil {
		return Statement{}, err
	}
	out := Statement{ID: doc.ID.String(), Number: doc.Number, Date: dateOf(s.Date), Customer: StatementCustomer{ID: c.ID.String(), Name: c.LegalName},
		Currency: s.Currency.Code, From: dateOf(s.From), To: dateOf(s.To), Opening: moneyOut(s.Opening), Lines: []StatementLine{},
		Closing: moneyOut(s.Closing), Overdue: moneyOut(s.Overdue), Ageing: ageingOut(s.Ageing, s.Credits), AddsUp: true, Version: doc.Version}
	for _, l := range s.Lines {
		out.Lines = append(out.Lines, StatementLine{Date: dateOf(l.Date), Kind: l.Kind, DocumentID: l.DocumentID.String(), Number: l.Number,
			Description: l.Description, Amount: moneyOut(l.Amount), Balance: moneyOut(l.Balance)})
	}
	sends, err := u.DocumentSends().OfDocument(ctx, doc.ID)
	if err != nil {
		return Statement{}, err
	}
	out.Sends = sendsOut(sends, nil)
	return out, nil
}

// statementPeriod reads a statement's request: its customer, currency (the customer's own, else base), and days.
func statementPeriod(ctx context.Context, u storage.UnitOfWork, base money.Currency, customer, currency, from, to, field string) (uuid.UUID,
	money.Currency, time.Time, time.Time, error) {
	var bad []FieldProblem
	id, err := uuid.Parse(strings.TrimSpace(customer))
	if err != nil {
		bad = append(bad, FieldProblem{Code: CodeInvalid, Field: field + "customer_id", Message: "is not a contact ID", Fix: "Send the customer's ID"})
	}
	f, errF := time.Parse(time.DateOnly, from)
	if errF != nil {
		bad = append(bad, FieldProblem{Code: CodeInvalid, Field: field + "from", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"})
	}
	t, errT := time.Parse(time.DateOnly, to)
	if errT != nil {
		bad = append(bad, FieldProblem{Code: CodeInvalid, Field: field + "to", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"})
	}
	if len(bad) > 0 {
		return uuid.Nil, money.Currency{}, time.Time{}, time.Time{}, invalid(bad)
	}
	cur := base
	code := strings.TrimSpace(currency)
	if code == "" {
		c, err := u.Contacts().Get(ctx, id)
		if err != nil {
			return uuid.Nil, money.Currency{}, time.Time{}, time.Time{}, err
		}
		code = c.Currency
	}
	if code != "" {
		if cur, err = money.Lookup(code); err != nil {
			return uuid.Nil, money.Currency{}, time.Time{}, time.Time{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: field + "currency",
				Message: "is not an ISO 4217 currency", Fix: "Send a currency code such as SGD"}})
		}
	}
	return id, cur, f, t, nil
}

func registerStatements(a *API) {
	codes := []string{CodeInvalid, statement.CodeRange, statement.CodeCustomer, storage.CodeRetry, storage.CodeLockTimeout}
	register(a, authz.GetCustomerStatement, huma.Operation{
		OperationID: "getCustomerStatement", Method: http.MethodGet, Path: "/v1/contacts/{contact_id}/statement", Tags: []string{"Sales"},
		Summary: "Work out a customer's statement",
		Description: "Works out a customer's statement of account in one currency from one day to another (FR-AR-08; PRD-001 rev 0.138): the " +
			"balance brought forward, each invoice, credit note, payment received, refund of a credit, write-off and void in the period with " +
			"a running balance, the balance due from the AR sub-ledger, what of it is overdue, and its ageing by days past due, which adds " +
			"up to it; adds_up says whether the lines come to the balance due, and currencies lists the customer's currencies in the " +
			"period. Nothing is kept: createStatement makes one to send. Read-only; no side effects.",
	}, append([]string{CodeNotFound}, codes...), func(ctx context.Context, in *customerStatementInput) (*customerStatementOutput, error) {
		p, _ := authn.From(ctx)
		var out customerStatementOutput
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			base, err := baseOf(ctx, u)
			if err != nil {
				return err
			}
			id, cur, from, to, err := statementPeriod(ctx, u, base, in.ContactID, in.Currency, in.From, in.To, "")
			if err != nil {
				return err
			}
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return err
			}
			if err := statement.CheckPeriod(from, to, today); err != nil {
				return err
			}
			s, err := statement.Of(ctx, u, base, id, cur, from, to)
			if err != nil {
				return err
			}
			out.Body = statementOut(s)
			cs, err := statement.Currencies(ctx, u, base, id, from, to)
			for _, c := range cs {
				out.Body.Currencies = append(out.Body.Currencies, CurrencyCode(c.Code))
			}
			return err
		})
		return &out, err
	})

	register(a, authz.CreateStatement, huma.Operation{
		OperationID: "createStatement", Method: http.MethodPost, Path: "/v1/statements", Tags: []string{"Sales"}, DefaultStatus: http.StatusCreated,
		Summary: "Make a statement to send",
		Description: "Makes a customer's statement of account in one currency for a period ending today at the latest, as getCustomerStatement " +
			"works it out, and keeps it as a statement document numbered from its sequence (ST-), saying what it said, never changed or " +
			"voided (FR-AR-08; PRD-001 rev 0.138); emailStatement sends it. One that does not add up (STATEMENT-03), or of a currency in which " +
			"the customer has nothing in the period (STATEMENT-04), is refused. Side effects: the statement and its audit events; it posts " +
			"nothing. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		statement.CodeAddsUp, statement.CodeCurrency}, codes...),
		func(ctx context.Context, in *statementCreateInput) (*statementWriteOutput, error) {
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (Statement, error) {
				base, err := baseOf(ctx, u)
				if err != nil {
					return Statement{}, err
				}
				b := in.Body
				id, cur, from, to, err := statementPeriod(ctx, u, base, b.CustomerID, b.Currency, b.From, b.To, "body.")
				if err != nil {
					return Statement{}, err
				}
				today, err := orgToday(ctx, a, u)
				if err != nil {
					return Statement{}, err
				}
				doc, _, err := statement.Make(ctx, u, base, id, cur, from, to, today)
				if err != nil {
					return Statement{}, err
				}
				return madeStatementOf(ctx, u, doc)
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &statementWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &statementWriteOutput{Status: w.Status, ETag: etag(w.Body.Version), Location: "/v1/statements/" + w.Body.ID,
				Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})

	register(a, authz.ListStatements, huma.Operation{
		OperationID: "listStatements", Method: http.MethodGet, Path: "/v1/statements", Tags: []string{"Sales"},
		Summary: "List the statements made",
		Description: "The statements of account made, newest first, a page at a time, of one customer when customer_id is given, each as it " +
			"was made, with its emails (FR-AR-08; PRD-001 rev 0.138). Read-only; no side effects.",
	}, []string{CodeCursor, CodeInvalid}, func(ctx context.Context, in *statementListInput) (*statementListOutput, error) {
		after, err := decodeCursor(in.Cursor)
		if err != nil {
			return nil, err
		}
		afterDate, afterID, err := journalCursor(after)
		if err != nil {
			return nil, err
		}
		var customer uuid.UUID
		if in.CustomerID != "" {
			if customer, err = parseID("customer_id", in.CustomerID); err != nil {
				return nil, err
			}
		}
		p, _ := authn.From(ctx)
		out := statementListOutput{Body: StatementPage{Items: []Statement{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			f := storage.DocumentFilter{Type: storage.DocumentStatement, Status: storage.DocumentPosted, AfterDate: afterDate, AfterID: afterID, Limit: 500}
			for len(out.Body.Items) <= in.PageSize {
				docs, err := u.Documents().Page(ctx, f)
				if err != nil {
					return err
				}
				for _, doc := range docs {
					s, err := statement.Read(doc)
					if err != nil {
						return err
					}
					if customer != uuid.Nil && s.CustomerID != customer {
						continue
					}
					if len(out.Body.Items) == in.PageSize { // one more than the page: there is a next page
						last := out.Body.Items[len(out.Body.Items)-1]
						out.Body.NextCursor = encodeCursor(last.Date + "|" + last.ID)
						return nil
					}
					m, err := madeStatementOf(ctx, u, doc)
					if err != nil {
						return err
					}
					out.Body.Items = append(out.Body.Items, m)
				}
				if len(docs) < f.Limit {
					return nil
				}
				last := docs[len(docs)-1]
				f.AfterDate, f.AfterID = last.Date, &last.ID
			}
			return nil
		})
		return &out, err
	})

	register(a, authz.GetStatement, huma.Operation{
		OperationID: "getStatement", Method: http.MethodGet, Path: "/v1/statements/{statement_id}", Tags: []string{"Sales"},
		Summary:     "Get a statement made",
		Description: "Returns a statement of account as it was made, with its emails and its ETag (FR-AR-08; PRD-001 rev 0.138). Read-only; no side effects.",
	}, []string{CodeNotFound, statement.CodeNotStatement}, func(ctx context.Context, in *StatementPath) (*statementReadOutput, error) {
		id, err := parseID("statement_id", in.StatementID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out statementReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := u.Documents().Get(ctx, id)
			if err != nil {
				return err
			}
			out.Body, err = madeStatementOf(ctx, u, doc)
			out.ETag = etag(doc.Version)
			return err
		})
		return &out, err
	})

	sendCodes := []string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeInvalid, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		CodeUnavailable, statement.CodeNotStatement, salesdoc.CodeNotSent, salesdoc.CodeNoRecipient, doctemplate.CodeRenderer, storage.CodeRetry, storage.CodeLockTimeout}
	register(a, authz.EmailStatement, huma.Operation{
		OperationID: "emailStatement", Method: http.MethodPost, Path: "/v1/statements/{statement_id}/email", Tags: []string{"Sales"},
		Summary: "Email a statement to the customer",
		Description: "Emails a statement of account made (FR-AR-08; PRD-001 rev 0.138) to the customer's billing email, or the address given, " +
			"from the organisation's invoices sender, with the subject and body of the statement email template that applies to the customer " +
			"and the PDF of it attached, rendered with the statement document template that applies (FR-TPL-05) as of the day it was made. " +
			"Sending again sends the same statement again. Side effects: the PDF in the statement's evidence (source generated), the email " +
			"message in the delivery log, the send, and their audit events. Needs If-Match with the statement's ETag. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview checks it and sends nothing.",
	}, sendCodes, func(ctx context.Context, in *emailStatementInput) (*statementWriteOutput, error) {
		id, err := parseID("statement_id", in.StatementID)
		if err != nil {
			return nil, err
		}
		w, err := emailDocument(ctx, a, id, salesmail.Statement, emailDocumentInput{in.IdempotencyKey, in.IfMatch, in.Prefer, in.Body}, in,
			func(ctx context.Context, u storage.UnitOfWork, doc storage.Document) (string, error) {
				return etag(doc.Version), nil
			},
			func(ctx context.Context, u storage.UnitOfWork) (Statement, error) {
				doc, err := u.Documents().Get(ctx, id)
				if err != nil {
					return Statement{}, err
				}
				return madeStatementOf(ctx, u, doc)
			})
		if err != nil {
			return nil, err
		}
		if w.Previewed {
			return &statementWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
		}
		return &statementWriteOutput{Status: w.Status, ETag: etag(w.Body.Version), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})
}
