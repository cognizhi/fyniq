package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/brand"
	"github.com/cognizhi/fyniq/internal/domain/doctemplate"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/portal"
	"github.com/cognizhi/fyniq/internal/domain/salesdoc"
	"github.com/cognizhi/fyniq/internal/domain/statement"
	"github.com/cognizhi/fyniq/internal/email"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the customer portal (FR-AR-14; p2-customer-portal-subscriptions)
//
// A person with the right to post on Invoices makes a portal link for a customer: a random 256-bit token shown once, in the URL's
// fragment (<origin>/portal/#<token>), kept only as its SHA-256. The portal's public page reads the fragment and calls these
// operations with the token in the Fyniq-Portal header, so the token never appears in a path or query and no access log records it.
// With no session, the API finds the token's organisation through the registry, then reads the link and the customer's documents in
// that organisation's unit of work, filtered by the link's customer. The portal reads and shows; it posts nothing and asks no provider
// for anything. Every link that opens nothing, and every document that is not the customer's, answers the same 404. The threat model:
// docs/security/threat-models/customer-portal.md.

// The requests a portal link may take a minute (threat model T6); the per-address limit of /v1/public/ applies besides.
const portalReads = 60

// PortalHeader carries a portal link's token.
type PortalHeader struct {
	Token string `header:"Fyniq-Portal" required:"true" maxLength:"64" doc:"The portal link's token, from the fragment of the link (after #/portal/#). Never put it in a URL." example:"q7Kx3m9VbN2pR8tY1wE5uI0oP4aS6dF3gH7jK9lZ2xC"`
}

// PortalInvoice is one open invoice of the customer.
type PortalInvoice struct {
	ID         string `json:"id" format:"uuid" doc:"The invoice's ID, which downloads its PDF." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f30"`
	Number     string `json:"number" doc:"Its number." example:"INV-0150"`
	Date       string `json:"date" format:"date" doc:"Its date." example:"2026-10-23"`
	DueDate    string `json:"due_date,omitempty" format:"date" doc:"Its due date." example:"2026-11-22"`
	Total      Money  `json:"total" doc:"Its total with GST."`
	Open       Money  `json:"open" doc:"What is still due on it."`
	Overdue    bool   `json:"overdue" doc:"Whether it is past its due date." example:"false"`
	PaymentURL string `json:"payment_url,omitempty" doc:"The payment provider's page where it can be paid, when the company made one for it." example:"https://securecheckout.hit-pay.com/payment-request/@lumen/9b2c"`
}

// PortalCurrency is a currency the customer has a statement in.
type PortalCurrency struct {
	Currency string `json:"currency" pattern:"^[A-Z]{3}$" doc:"The ISO 4217 currency code." example:"SGD"`
}

// PortalSummary is what a portal link opens first.
type PortalSummary struct {
	Supplier      string           `json:"supplier" doc:"The company that issued the invoices." example:"Northwind Analytics Pte. Ltd."`
	Customer      string           `json:"customer" doc:"The customer this portal is for." example:"Acme Trading Pte Ltd"`
	Invoices      []PortalInvoice  `json:"invoices" nullable:"false" doc:"The invoices still due, earliest due date first."`
	Currencies    []PortalCurrency `json:"statement_currencies" nullable:"false" maxItems:"50" doc:"The currencies the customer has a statement in."`
	Today         string           `json:"today" format:"date" doc:"The company's date today." example:"2026-11-20"`
	LinkExpiresAt time.Time        `json:"link_expires_at" doc:"When the link stops working at the latest." example:"2026-12-20T09:30:00Z"`
	Logo          []byte           `json:"logo,omitempty" doc:"The company's logo as its documents show it: a PNG, base64. Absent when it has none." example:"iVBORw0KGgo="`
}

// PortalStatementLine is a movement of what the customer owes.
type PortalStatementLine struct {
	Date        string `json:"date" format:"date" doc:"The day." example:"2026-10-23"`
	Description string `json:"description" doc:"What it was." example:"Invoice INV-0150, due 22 Nov 2026"`
	Amount      Money  `json:"amount" doc:"Positive when the customer owes more."`
	Balance     Money  `json:"balance" doc:"What they owe after it."`
}

// PortalStatement is the customer's statement of account.
type PortalStatement struct {
	Customer string                `json:"customer" doc:"The customer." example:"Acme Trading Pte Ltd"`
	Currency string                `json:"currency" doc:"The statement's currency." example:"SGD"`
	From     string                `json:"from" format:"date" doc:"The first day." example:"2026-10-01"`
	To       string                `json:"to" format:"date" doc:"The last day." example:"2026-11-20"`
	Opening  Money                 `json:"opening" doc:"What the customer owed before the first day."`
	Lines    []PortalStatementLine `json:"lines" nullable:"false" doc:"The movements, oldest first."`
	Closing  Money                 `json:"closing" doc:"What the customer owes at the end of the last day."`
	Overdue  Money                 `json:"overdue" doc:"What of it is past due."`
}

type portalSummaryOutput struct {
	CacheControl   string `header:"Cache-Control" doc:"no-store: never cached."`
	Robots         string `header:"X-Robots-Tag" doc:"noindex, nofollow: never indexed."`
	ReferrerPolicy string `header:"Referrer-Policy" doc:"no-referrer: the page never leaks its address."`
	Body           PortalSummary
}

type portalPDFInput struct {
	PortalHeader
	InvoiceID string `path:"invoice_id" format:"uuid" doc:"The invoice's ID, from the portal's list." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f30"`
}

type portalPDFOutput struct {
	CacheControl   string `header:"Cache-Control" doc:"no-store: never cached."`
	Robots         string `header:"X-Robots-Tag" doc:"noindex, nofollow: never indexed."`
	ReferrerPolicy string `header:"Referrer-Policy" doc:"no-referrer: the page never leaks its address."`
	Body           PublicPDF
}

type portalStatementInput struct {
	PortalHeader
	Currency string `query:"currency" pattern:"^[A-Z]{3}$" doc:"The statement's currency; the company's base currency when left out." example:"SGD"`
	From     string `query:"from" format:"date" doc:"The first day; 30 days before today when left out." example:"2026-10-01"`
	To       string `query:"to" format:"date" doc:"The last day; today when left out." example:"2026-11-20"`
}

type portalStatementOutput struct {
	CacheControl   string `header:"Cache-Control" doc:"no-store: never cached."`
	Robots         string `header:"X-Robots-Tag" doc:"noindex, nofollow: never indexed."`
	ReferrerPolicy string `header:"Referrer-Policy" doc:"no-referrer: the page never leaks its address."`
	Body           PortalStatement
}

// notPortal is the one answer to a link that opens nothing, and to a document that is not the link's customer's (T9).
func notPortal() error {
	return problem(portal.CodeLink, "This link does not open anything: it may have expired or been withdrawn.", "Ask the company for a new link")
}

// openedPortal is what a portal link opens: the organisation's unit of work already runs as the customer (the link), at now.
type openedPortal struct {
	scope portal.Scope
	now   time.Time
	today time.Time
}

// resolvePortal finds the organisation and link token opens, before any tenant is set, and makes the link the actor of what
// follows. Anything that opens nothing is notPortal.
func (a *API) resolvePortal(ctx context.Context, token string) (context.Context, uuid.UUID, uuid.UUID, error) {
	hash, ok := salesdoc.TokenHash(strings.TrimSpace(token))
	if !ok {
		return ctx, uuid.Nil, uuid.Nil, notPortal()
	}
	routes, ok := a.deps.Store.(storage.PortalRoutes)
	if !ok {
		return ctx, uuid.Nil, uuid.Nil, notPortal()
	}
	org, linkID, err := routes.PortalRoute(ctx, hash)
	if errors.Is(err, ports.ErrNotFound) {
		return ctx, uuid.Nil, uuid.Nil, notPortal()
	}
	if err != nil {
		return ctx, uuid.Nil, uuid.Nil, err
	}
	audit := storage.AuditFrom(ctx)
	audit.Actor = &storage.Actor{Type: "customer", ID: linkID.String()} // whoever holds the link
	return storage.WithAudit(ctx, audit), org, linkID, nil
}

// openPortal finds what token opens and runs fn in its organisation's unit of work: the link must be live, is counted against its
// limit, and has its first opening recorded (audited once). A link that is unknown, revoked or expired, or whose customer is no
// longer an active customer, is notPortal.
func (a *API) openPortal(ctx context.Context, token string, fn func(ctx context.Context, u storage.UnitOfWork, o openedPortal) error) error {
	ctx, org, linkID, err := a.resolvePortal(ctx, token)
	if err != nil {
		return err
	}
	return a.deps.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		now := a.deps.Clock.Now()
		l, err := u.PortalLinks().Get(ctx, linkID)
		if errors.Is(err, ports.ErrNotFound) {
			return notPortal()
		}
		if err != nil {
			return err
		}
		if !portal.Live(l, now) {
			return notPortal()
		}
		start := now.UTC().Truncate(rateWindow)
		used, err := u.RateLimits().Count(ctx, "portal "+linkID.String(), storage.RateRead, start)
		if err != nil {
			return err
		}
		if used > portalReads {
			reset := int(start.Add(rateWindow).Sub(now).Seconds()) + 1
			return problem(CodeRateLimited, fmt.Sprintf("This link was opened more than %d times in this minute (%s).", portalReads, readsPolicy),
				fmt.Sprintf("Wait %d seconds, then try again.", reset)).withHeader("Retry-After", fmt.Sprint(reset))
		}
		if _, _, err := u.PortalLinks().Opened(ctx, linkID, now); err != nil {
			return err
		}
		today, err := orgToday(ctx, a, u)
		if err != nil {
			return err
		}
		err = fn(ctx, u, openedPortal{scope: portal.Scope{Org: org, Link: l}, now: now, today: today})
		if errors.Is(err, ports.ErrNotFound) {
			return notPortal() // a document that is not the customer's is not found, as one that does not exist is
		}
		return err
	})
}

// PortalLinkOut is a portal link as the staff see it: never its token.
type PortalLinkOut struct {
	ID            string     `json:"id" format:"uuid" doc:"The link's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e91"`
	ContactID     string     `json:"contact_id" format:"uuid" doc:"The customer it opens." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4a01"`
	Label         string     `json:"label,omitempty" doc:"A note for the staff, such as who it was sent to." example:"Sent to ap@acme.example"`
	Status        string     `json:"status" enum:"live,revoked,expired" doc:"live; revoked by a person; or expired." example:"live"`
	CreatedAt     time.Time  `json:"created_at" doc:"When it was made." example:"2026-11-20T09:30:00Z"`
	ExpiresAt     time.Time  `json:"expires_at" doc:"When it stops working." example:"2026-12-20T09:30:00Z"`
	FirstOpenedAt *time.Time `json:"first_opened_at,omitempty" doc:"When the customer first opened it; never their address." example:"2026-11-21T02:10:00Z"`
	RevokedAt     *time.Time `json:"revoked_at,omitempty" doc:"When it was revoked." example:"2026-11-22T09:00:00Z"`
}

// PortalLinkMade is a new link: the only time its token is shown.
type PortalLinkMade struct {
	PortalLinkOut
	Token string `json:"token" doc:"The link's token, shown this once: Fyniq keeps only its hash. The customer's address is the portal page's, then #, then this." example:"q7Kx3m9VbN2pR8tY1wE5uI0oP4aS6dF3gH7jK9lZ2xC"`
	Path  string `json:"path" doc:"The path of the portal page with the token in its fragment, to put after the web app's origin." example:"/portal/#q7Kx3m9VbN2pR8tY1wE5uI0oP4aS6dF3gH7jK9lZ2xC"`
	// EmailedTo is the address the link was emailed to when send_email was asked for.
	EmailedTo string `json:"emailed_to,omitempty" doc:"The customer's billing email address the link was queued to, when send_email was true." example:"ap@acme.example"`
}

func portalLinkOf(l storage.PortalLink, now time.Time) PortalLinkOut {
	out := PortalLinkOut{ID: l.ID.String(), ContactID: l.ContactID.String(), Label: l.Label, Status: "live", CreatedAt: l.CreatedAt.UTC(), ExpiresAt: l.ExpiresAt.UTC(),
		FirstOpenedAt: utc(l.FirstOpenedAt), RevokedAt: utc(l.RevokedAt)}
	switch {
	case l.RevokedAt != nil:
		out.Status = "revoked"
	case !now.Before(l.ExpiresAt):
		out.Status = "expired"
	}
	return out
}

type createPortalLinkInput struct {
	ContactPath
	Body struct {
		Label string `json:"label,omitempty" maxLength:"200" doc:"A note for the staff, such as who the link is sent to. Never shown to the customer." example:"Sent to ap@acme.example"`
		Days  int    `json:"days,omitempty" minimum:"1" maximum:"90" doc:"How many days the link works: 30 when left out, at most 90." example:"30"`
		// SendEmail also emails the link to the customer's billing address from the statements sender, in the same step: the link is made only if the email is queued.
		SendEmail bool `json:"send_email,omitempty" doc:"Also email the link to the customer's billing email address, from the statements sender; nothing is made if it cannot be queued. The address is the contact's own and cannot be chosen here." example:"true"`
	}
}

type portalLinkMadeOutput struct {
	Status int
	Body   PortalLinkMade
}

type contactPortalLinksOutput struct {
	Body struct {
		Links []PortalLinkOut `json:"links" nullable:"false" doc:"The customer's portal links, newest first."`
	}
}

type revokePortalLinkInput struct {
	PortalLinkPath
	KeyHeader
	PreferHeader
}

// PortalLinkPath names a portal link in the path.
type PortalLinkPath struct {
	LinkID string `path:"link_id" format:"uuid" doc:"The link's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e91"`
}

type revokePortalLinkOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     PortalLinkOut
}

func registerPortal(a *API) {
	readCodes := []string{portal.CodeLink, statement.CodeRange, storage.CodeRetry, storage.CodeLockTimeout, CodeRateLimited}

	register(a, authz.GetPortal, huma.Operation{
		OperationID: "getPortal", Method: http.MethodGet, Path: "/v1/public/portal", Tags: []string{"Sales"},
		Summary: "Open a customer's portal through its link",
		Description: "Public: no session. What a portal link's token (in the Fyniq-Portal header, from the link's fragment) opens first (FR-AR-14): the company, the " +
			"customer, the customer's invoices still due with where each can be paid, and the currencies of their statements. Only that customer's: another " +
			"customer's or organisation's data is never read, and an unknown, expired or revoked token answers one 404 (PORTAL-01). The first opening of a link is " +
			"recorded. At most 60 a minute per link. Never cached or indexed.",
	}, readCodes, func(ctx context.Context, in *PortalHeader) (*portalSummaryOutput, error) {
		var out PortalSummary
		var org uuid.UUID
		var issued storage.BrandVersion
		err := a.openPortal(ctx, in.Token, func(ctx context.Context, u storage.UnitOfWork, o openedPortal) error {
			s, err := portal.Read(ctx, u, o.scope, o.today)
			if err != nil {
				return err
			}
			org = o.scope.Org
			org2, err := u.Organisation(ctx)
			if err != nil {
				return err
			}
			out = PortalSummary{Supplier: org2.Profile.LegalName, Customer: s.Customer, Invoices: []PortalInvoice{}, Currencies: []PortalCurrency{}, Today: dateOf(s.Today),
				LinkExpiresAt: o.scope.Link.ExpiresAt.UTC()}
			if out.Supplier == "" {
				out.Supplier = org2.Name
			}
			for _, i := range s.Invoices {
				x := PortalInvoice{ID: i.ID.String(), Number: i.Number, Date: dateOf(i.Date), Total: moneyOut(i.Total), Open: moneyOut(i.Open), Overdue: i.Overdue, PaymentURL: i.PaymentURL}
				if !i.DueDate.IsZero() {
					x.DueDate = dateOf(i.DueDate)
				}
				out.Invoices = append(out.Invoices, x)
			}
			for _, c := range s.Currencies {
				out.Currencies = append(out.Currencies, PortalCurrency{Currency: c.Code})
			}
			issued, err = brand.At(ctx, u, o.now)
			return err
		})
		if err != nil {
			return nil, err
		}
		if out.Logo, err = brand.PrintLogo(ctx, a.deps.Staging, org, issued); err != nil {
			logFailure(ctx, err) // the page shows without its logo rather than not at all
		}
		return &portalSummaryOutput{CacheControl: noStore, Robots: noIndex, ReferrerPolicy: noReferrer, Body: out}, nil
	})

	register(a, authz.GetPortalInvoicePDF, huma.Operation{
		OperationID: "getPortalInvoicePDF", Method: http.MethodGet, Path: "/v1/public/portal/invoices/{invoice_id}/pdf", Tags: []string{"Sales"},
		Summary: "Get the PDF of one of the customer's invoices through the portal",
		Description: "Public: no session. The PDF of an issued invoice of the customer the portal link opens (FR-AR-14), rendered as emailing it renders it. An invoice that " +
			"is a draft, voided, a credit note, another customer's or does not exist answers the same 404 (PORTAL-01). At most 60 a minute per link. Never cached or indexed.",
	}, append([]string{doctemplate.CodeRenderer}, readCodes...), func(ctx context.Context, in *portalPDFInput) (*portalPDFOutput, error) {
		id, err := parseID("invoice_id", in.InvoiceID)
		if err != nil {
			return nil, err
		}
		var data salesdoc.Invoice
		var applied doctemplate.Applied
		var org uuid.UUID
		var issued storage.BrandVersion
		err = a.openPortal(ctx, in.Token, func(ctx context.Context, u storage.UnitOfWork, o openedPortal) error {
			doc, d, err := portal.InvoiceOf(ctx, u, o.scope, id)
			if err != nil {
				return err
			}
			s, err := journal.LoadSettings(ctx, u)
			if err != nil {
				return err
			}
			if data, err = salesdoc.InvoiceData(ctx, u, doc, s); err != nil {
				return err
			}
			org = o.scope.Org
			if applied, err = doctemplate.Applying(ctx, u, doctemplate.KindDocument, doctemplate.Invoice, &d.CustomerID); err != nil {
				return err
			}
			if issued, err = brand.At(ctx, u, data.Created); err != nil {
				return err
			}
			applied.Content = brand.Themed(applied.Content, applied.Source, issued)
			return nil
		})
		if err != nil {
			return nil, err
		}
		if a.deps.Renderer == nil {
			return nil, problem(doctemplate.CodeRenderer, "The document cannot be rendered just now.", "Try again later")
		}
		logo, err := brand.PrintLogo(ctx, a.deps.Staging, org, issued)
		if err != nil {
			return nil, err
		}
		res, err := a.renderDocumentAt(ctx, doctemplate.Invoice, applied.Content, data.Data, data.Created, logo)
		if err != nil {
			return nil, err
		}
		return &portalPDFOutput{CacheControl: noStore, Robots: noIndex, ReferrerPolicy: noReferrer, Body: PublicPDF{PDF: res.PDF, SHA256: res.SHA256}}, nil
	})

	register(a, authz.GetPortalStatement, huma.Operation{
		OperationID: "getPortalStatement", Method: http.MethodGet, Path: "/v1/public/portal/statement", Tags: []string{"Sales"},
		Summary: "Get the customer's statement of account through the portal",
		Description: "Public: no session. The statement of account of the customer the portal link opens, in one currency over a period of at most two years ending " +
			"today or earlier (FR-AR-14, FR-AR-08): what they owed before it, each invoice, credit note and payment, and what they owe at its end. Only that customer's. " +
			"At most 60 a minute per link. Never cached or indexed.",
	}, readCodes, func(ctx context.Context, in *portalStatementInput) (*portalStatementOutput, error) {
		var out PortalStatement
		err := a.openPortal(ctx, in.Token, func(ctx context.Context, u storage.UnitOfWork, o openedPortal) error {
			org, err := u.Organisation(ctx)
			if err != nil {
				return err
			}
			cur := in.Currency
			if cur == "" {
				cur = org.BaseCurrency
			}
			to, from := o.today, o.today.AddDate(0, 0, -30)
			if in.To != "" {
				if to, err = time.Parse(time.DateOnly, in.To); err != nil {
					return invalid([]FieldProblem{{Code: CodeInvalid, Field: "to", Message: "is not a date", Fix: "Use a date such as 2026-11-20"}})
				}
			}
			if in.From != "" {
				if from, err = time.Parse(time.DateOnly, in.From); err != nil {
					return invalid([]FieldProblem{{Code: CodeInvalid, Field: "from", Message: "is not a date", Fix: "Use a date such as 2026-10-01"}})
				}
			} else if in.To != "" {
				from = to.AddDate(0, 0, -30)
			}
			st, err := portal.Statement(ctx, u, o.scope, cur, from, to, o.today)
			if err != nil {
				return err
			}
			out = PortalStatement{Customer: st.CustomerName, Currency: st.Currency.Code, From: dateOf(st.From), To: dateOf(st.To), Opening: moneyOut(st.Opening),
				Closing: moneyOut(st.Closing), Overdue: moneyOut(st.Overdue), Lines: []PortalStatementLine{}}
			for _, l := range st.Lines {
				out.Lines = append(out.Lines, PortalStatementLine{Date: dateOf(l.Date), Description: l.Description(), Amount: moneyOut(l.Amount), Balance: moneyOut(l.Balance)})
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		return &portalStatementOutput{CacheControl: noStore, Robots: noIndex, ReferrerPolicy: noReferrer, Body: out}, nil
	})

	register(a, authz.CreatePortalLink, oneTimeSecret(exempt(huma.Operation{
		OperationID: "createPortalLink", Method: http.MethodPost, Path: "/v1/contacts/{contact_id}/portal-links", Tags: []string{"Sales"},
		Summary: "Make a customer's portal link", DefaultStatus: http.StatusCreated,
		Description: "Makes a link that opens one customer's portal: their invoices still due, where to pay, their invoice PDFs and statements (FR-AR-14). The answer " +
			"carries the token once and Fyniq keeps only its hash, so it cannot be shown again; a link works 30 days by default and at most 90, and is withdrawn with " +
			"revokePortalLink. The contact must be an active customer. With send_email the link is also queued to the customer's billing email address from the statements sender (the address cannot be chosen here), in the same step. A person's alone: the AI and MCP have no tool for it. Side effects: the link, the queued email if asked for, and their audit events.",
	}, "If-Match", "A portal link is a record of its own and changes nothing of the contact (FR-AR-14).")),
		[]string{storage.CodeInvalid, CodeNotFound, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, storage.CodeRetry, storage.CodeLockTimeout},
		func(ctx context.Context, in *createPortalLinkInput) (*portalLinkMadeOutput, error) {
			contact, err := parseID("contact_id", in.ContactID)
			if err != nil {
				return nil, err
			}
			days := in.Body.Days
			if days == 0 {
				days = portal.DefaultDays
			}
			if days < 1 || days > portal.MaxDays {
				return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.days", Message: "is not from 1 to 90", Fix: "Choose from 1 to 90 days"}})
			}
			token, hash, err := salesdoc.NewToken()
			if err != nil {
				return nil, err
			}
			p, _ := authn.From(ctx)
			var made PortalLinkMade
			err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				c, err := u.Contacts().Get(ctx, contact)
				if err != nil {
					return err
				}
				if !c.Customer || !c.Active {
					return invalid([]FieldProblem{{Code: CodeInvalid, Field: "contact_id", Message: "is not an active customer", Fix: "Choose an active customer"}})
				}
				now := a.deps.Clock.Now()
				l, err := u.PortalLinks().Create(ctx, storage.NewPortalLink{ContactID: contact, Label: strings.TrimSpace(in.Body.Label), TokenHash: hash,
					ExpiresAt: now.AddDate(0, 0, days), At: now})
				if err != nil {
					return err
				}
				made = PortalLinkMade{PortalLinkOut: portalLinkOf(l, now), Token: token, Path: "/portal/#" + token}
				if !in.Body.SendEmail {
					return nil
				}
				// The email carries the link with its token in the fragment, which a browser never sends to a server, as an invoice's online link does; only the hash is kept.
				to := strings.TrimSpace(c.BillingEmail)
				switch {
				case a.deps.Outbox == nil || a.deps.Origin == "":
					return problem(CodeUnavailable, "Email cannot be sent from this deployment.", "Make the link without send_email and share it yourself, or ask the operator to configure email and the web address")
				case to == "":
					return invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.send_email", Message: "needs the customer's billing email address", Fix: "Set the contact's billing email first, or make the link without send_email"}})
				}
				org, err := u.Organisation(ctx)
				if err != nil {
					return err
				}
				text := "Hello,\n\n" + org.Profile.LegalName + " has set up a page for you where you can see what you owe, pay online where that is offered, and download your invoices and statements:\n\n" +
					a.deps.Origin + made.Path + "\n\nThe link works until " + l.ExpiresAt.UTC().Format("2 January 2006") + ". Please keep it private: anyone who has it can see your invoices.\n\nIf you did not expect this email, you can ignore it."
				if _, err := email.QueueWith(ctx, *a.deps.Outbox, u, p.Organisation, email.PurposeStatement, to, "Your account with "+org.Profile.LegalName, text, nil); err != nil {
					return err
				}
				made.EmailedTo = to
				return nil
			})
			if err != nil {
				return nil, err
			}
			return &portalLinkMadeOutput{Status: http.StatusCreated, Body: made}, nil
		})

	register(a, authz.GetContactPortalLinks, huma.Operation{
		OperationID: "getContactPortalLinks", Method: http.MethodGet, Path: "/v1/contacts/{contact_id}/portal-links", Tags: []string{"Sales"},
		Summary: "Read a customer's portal links",
		Description: "The customer's portal links, newest first: when each was made, when it expires, when the customer first opened it, and whether it was revoked. " +
			"Never a token. Read-only.",
	}, []string{CodeNotFound}, func(ctx context.Context, in *ContactPath) (*contactPortalLinksOutput, error) {
		contact, err := parseID("contact_id", in.ContactID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out contactPortalLinksOutput
		out.Body.Links = []PortalLinkOut{}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			if _, err := u.Contacts().Get(ctx, contact); err != nil {
				return err
			}
			ls, err := u.PortalLinks().OfContact(ctx, contact)
			now := a.deps.Clock.Now()
			for _, l := range ls {
				out.Body.Links = append(out.Body.Links, portalLinkOf(l, now))
			}
			return err
		})
		if err != nil {
			return nil, err
		}
		return &out, nil
	})

	register(a, authz.RevokePortalLink, huma.Operation{
		OperationID: "revokePortalLink", Method: http.MethodPost, Path: "/v1/portal-links/{link_id}/revoke", Tags: []string{"Sales"},
		Summary: "Revoke a customer's portal link",
		Description: "Stops a portal link working at once (FR-AR-14): whoever holds it then gets PORTAL-01. A link is revoked once; to give the customer the portal " +
			"again, make a new link. Side effects: the revocation and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview revokes nothing.",
	}, []string{CodeNotFound, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, storage.CodeLink, storage.CodeRetry, storage.CodeLockTimeout},
		func(ctx context.Context, in *revokePortalLinkInput) (*revokePortalLinkOutput, error) {
			id, err := parseID("link_id", in.LinkID)
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (PortalLinkOut, error) {
				l, err := u.PortalLinks().Revoke(ctx, id)
				if err != nil {
					return PortalLinkOut{}, err
				}
				return portalLinkOf(l, a.deps.Clock.Now()), nil
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &revokePortalLinkOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &revokePortalLinkOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})
}
