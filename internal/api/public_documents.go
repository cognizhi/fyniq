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
	"github.com/cognizhi/fyniq/internal/domain/doctemplate"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/quote"
	"github.com/cognizhi/fyniq/internal/domain/salesdoc"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------- the online link to a quote or invoice (FR-AR-06, FR-AR-16; p1-sales task 5.3)
//
// Emailing a document makes a link for its version: a random 256-bit token in the URL's fragment, <origin>/d/#<token>,
// kept only as its SHA-256. The web app's public page reads the fragment and calls these operations with the token in
// the Fyniq-Link header, so the token never appears in a path or query and no access log records it. With no session,
// the API finds the token's organisation through the registry, then reads the link and its document in that
// organisation's unit of work. A link shows its document and its PDF and nothing else; a quote's link also takes the
// customer's answer. It lasts 180 days, until revoked, voided or cancelled, or an invoice is paid in full. Every link
// that opens nothing answers the same 404. The threat model: docs/security/threat-models/document-links.md.

// The requests a link may take a minute (the threat model's T6): the edge limits by address besides.
const (
	linkReads   = 30
	linkAnswers = 5
)

// LinkHeader carries a link's token.
type LinkHeader struct {
	Token string `header:"Fyniq-Link" required:"true" maxLength:"64" doc:"The link's token, from the fragment of the link the email carried (after #/d/#). Never put it in a URL." example:"q7Kx3m9VbN2pR8tY1wE5uI0oP4aS6dF3gH7jK9lZ2xC"`
}

// PublicLine is a line of a document opened through its link.
type PublicLine struct {
	Description string `json:"description" doc:"What the line is for." example:"Analytics platform, October"`
	Quantity    string `json:"quantity,omitempty" doc:"How many." example:"2"`
	UnitPrice   string `json:"unit_price,omitempty" doc:"The price of one before GST." example:"1500.00"`
	GSTRate     string `json:"gst_rate,omitempty" doc:"Its GST: a rate such as 9%, or a code such as ES; none before the supplier is GST-registered." example:"9%"`
	Amount      Money  `json:"amount" doc:"Its amount before GST."`
}

// PublicAnswer is a quote's answer, as its link shows it.
type PublicAnswer struct {
	Outcome string `json:"outcome" enum:"accepted,declined,expired" doc:"What happened." example:"accepted"`
	Date    string `json:"date" format:"date" doc:"The day." example:"2026-10-30"`
	Name    string `json:"name,omitempty" doc:"Who accepted it." example:"Jane Tan"`
}

// PublicDocument is a quote or invoice opened through its link: what its PDF shows, and nothing else.
type PublicDocument struct {
	Kind          string        `json:"kind" enum:"invoice,quote,sales_order" doc:"What it is." example:"invoice"`
	Supplier      string        `json:"supplier" doc:"Who issued it." example:"Northwind Analytics Pte. Ltd."`
	Customer      string        `json:"customer" doc:"Whom it is for." example:"Acme Trading Pte Ltd"`
	Number        string        `json:"number" doc:"Its number." example:"INV-0042"`
	Date          string        `json:"date" format:"date" doc:"Its date." example:"2026-10-23"`
	DueDate       string        `json:"due_date,omitempty" format:"date" doc:"An invoice's due date." example:"2026-11-22"`
	ValidUntil    string        `json:"valid_until,omitempty" format:"date" doc:"A quote's last valid day." example:"2026-11-22"`
	Reference     string        `json:"reference,omitempty" doc:"The customer's reference." example:"PO-4411"`
	TaxInvoice    bool          `json:"tax_invoice" doc:"Whether it is a tax invoice: the supplier is GST-registered on its date." example:"true"`
	Lines         []PublicLine  `json:"lines" nullable:"false" doc:"Its lines."`
	Subtotal      Money         `json:"subtotal" doc:"Its total before GST."`
	GST           Money         `json:"gst" doc:"Its GST."`
	Total         Money         `json:"total" doc:"Its total with GST."`
	AmountDue     *Money        `json:"amount_due,omitempty" doc:"An invoice's amount still due."`
	Status        string        `json:"status,omitempty" enum:"sent,accepted,declined,expired,invoiced" doc:"A quote's status." example:"sent"`
	Answer        *PublicAnswer `json:"answer,omitempty" doc:"A quote's answer, once given."`
	CanAnswer     bool          `json:"can_answer" doc:"Whether the customer can accept or decline it here: a sent quote, still valid, with no answer." example:"true"`
	LinkExpiresAt time.Time     `json:"link_expires_at" doc:"When the link stops working, at the latest." example:"2027-04-22T09:30:00Z"`
}

// PublicPDF is a document's PDF opened through its link.
type PublicPDF struct {
	PDF    []byte `json:"pdf" doc:"The document, as PDF/A-2a and PDF/UA-1, base64." example:"JVBERi0xLjc="`
	SHA256 string `json:"sha256" doc:"The PDF's SHA-256, in hex." example:"a18760db1dd480808a9d4b2fd9b460608c4312c8998317e5ef444d83c0061bda"`
}

// PublicAnswerRequest is the customer's answer to a quote through its link.
type PublicAnswerRequest struct {
	Name    string `json:"name,omitempty" maxLength:"200" doc:"Who answers, as they want it recorded: required to accept." example:"Jane Tan"`
	Comment string `json:"comment,omitempty" maxLength:"2000" doc:"A comment, kept with the answer." example:"Please start on 1 November"`
}

// What every public answer carries (the threat model's T4): never cached, never indexed, no referrer.
const (
	noStore    = "no-store"
	noIndex    = "noindex, nofollow"
	noReferrer = "no-referrer"
)

type publicDocumentOutput struct {
	CacheControl   string `header:"Cache-Control" doc:"no-store: never cached."`
	Robots         string `header:"X-Robots-Tag" doc:"noindex, nofollow: never indexed."`
	ReferrerPolicy string `header:"Referrer-Policy" doc:"no-referrer: the page never leaks its address."`
	Body           PublicDocument
}

type publicPDFOutput struct {
	CacheControl   string `header:"Cache-Control" doc:"no-store: never cached."`
	Robots         string `header:"X-Robots-Tag" doc:"noindex, nofollow: never indexed."`
	ReferrerPolicy string `header:"Referrer-Policy" doc:"no-referrer: the page never leaks its address."`
	Body           PublicPDF
}

type publicAnswerInput struct {
	LinkHeader
	KeyHeader
	PreferHeader
	Body PublicAnswerRequest
}

type publicAnswerOutput struct {
	Status         int
	CacheControl   string `header:"Cache-Control" doc:"no-store: never cached."`
	Robots         string `header:"X-Robots-Tag" doc:"noindex, nofollow: never indexed."`
	ReferrerPolicy string `header:"Referrer-Policy" doc:"no-referrer: the page never leaks its address."`
	Replayed       string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied        string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body           PublicDocument
}

// DocumentLinkOut is an online link as the document's members see it: never its token.
type DocumentLinkOut struct {
	ID            string     `json:"id" format:"uuid" doc:"The link's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e90"`
	Version       int        `json:"version" doc:"The document's version it opens." example:"1"`
	Status        string     `json:"status" enum:"live,revoked,ended" doc:"live; revoked by a person; or ended: past its expiry, its document voided, cancelled or changed, or an invoice paid in full." example:"live"`
	CreatedAt     time.Time  `json:"created_at" doc:"When it was made, with the email that carried it." example:"2026-10-24T09:30:00Z"`
	ExpiresAt     time.Time  `json:"expires_at" doc:"When it stops working at the latest: 180 days after." example:"2027-04-22T09:30:00Z"`
	FirstViewedAt *time.Time `json:"first_viewed_at,omitempty" doc:"When the customer first opened it; never their address." example:"2026-10-25T02:10:00Z"`
	RevokedAt     *time.Time `json:"revoked_at,omitempty" doc:"When it was revoked." example:"2026-10-26T09:00:00Z"`
}

// DocumentLinkPath names an online link in the path.
type DocumentLinkPath struct {
	LinkID string `path:"link_id" format:"uuid" doc:"The link's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e90"`
}

type revokeLinkInput struct {
	DocumentLinkPath
	KeyHeader
	PreferHeader
}

type revokeLinkOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     DocumentLinkOut
}

// linksOut are a document's links with their status at now; settled is an invoice paid in full.
func linksOut(links []storage.DocumentLink, doc storage.Document, settled bool, now time.Time) []DocumentLinkOut {
	out := []DocumentLinkOut{}
	for _, l := range links {
		x := DocumentLinkOut{ID: l.ID.String(), Version: l.DocumentVersion, Status: "ended", CreatedAt: l.CreatedAt.UTC(), ExpiresAt: l.ExpiresAt.UTC(),
			FirstViewedAt: utc(l.FirstViewedAt), RevokedAt: utc(l.RevokedAt)}
		switch {
		case l.RevokedAt != nil:
			x.Status = "revoked"
		case salesdoc.Live(l, doc, settled, now):
			x.Status = "live"
		}
		out = append(out, x)
	}
	return out
}

// viewedBy says, per email message, whether the link it carried was opened.
func viewedBy(links []storage.DocumentLink) map[uuid.UUID]bool {
	out := map[uuid.UUID]bool{}
	for _, l := range links {
		if l.FirstViewedAt != nil {
			out[l.MessageID] = true
		}
	}
	return out
}

// notALink is the one answer to a link that opens nothing (T9).
func notALink() error {
	return problem(salesdoc.CodeLink, "This link does not open a document: it may have expired or been withdrawn.",
		"Ask the sender for the document again")
}

// opened is what a link opens, read in its organisation's unit of work.
type opened struct {
	org      uuid.UUID
	link     storage.DocumentLink
	doc      storage.Document
	invoice  bool
	customer uuid.UUID
	data     salesdoc.Invoice
	quote    quote.Draft
	outcome  *storage.QuoteOutcome
	status   string
	today    time.Time
}

// resolveLink finds the organisation and link token opens, before any tenant is set, and makes the customer holding it
// the actor of what follows (T5). Anything that opens nothing is notALink.
func (a *API) resolveLink(ctx context.Context, token string) (context.Context, uuid.UUID, uuid.UUID, error) {
	hash, ok := salesdoc.TokenHash(strings.TrimSpace(token))
	if !ok {
		return ctx, uuid.Nil, uuid.Nil, notALink()
	}
	routes, ok := a.deps.Store.(storage.LinkRoutes)
	if !ok {
		return ctx, uuid.Nil, uuid.Nil, notALink()
	}
	org, linkID, err := routes.LinkRoute(ctx, hash)
	if errors.Is(err, ports.ErrNotFound) {
		return ctx, uuid.Nil, uuid.Nil, notALink()
	}
	if err != nil {
		return ctx, uuid.Nil, uuid.Nil, err
	}
	audit := storage.AuditFrom(ctx)
	audit.Actor = &storage.Actor{Type: "customer", ID: linkID.String()} // whoever holds the link
	return storage.WithAudit(ctx, audit), org, linkID, nil
}

// loadLink reads, in u, what a link opens: the link, its live document and its data; counted against the link's limit,
// answers when answer. Anything that opens nothing is notALink.
func (a *API) loadLink(ctx context.Context, u storage.UnitOfWork, org, linkID uuid.UUID, answer bool) (*opened, error) {
	now := a.deps.Clock.Now()
	o := &opened{org: org}
	var err error
	if o.link, err = u.DocumentLinks().Get(ctx, linkID); err != nil {
		return nil, err
	}
	if o.doc, err = u.Documents().Get(ctx, o.link.DocumentID); err != nil {
		return nil, err
	}
	if !salesdoc.Live(o.link, o.doc, false, now) {
		return nil, notALink()
	}
	kind, quota, name := storage.RateRead, linkReads, readsPolicy
	if answer {
		kind, quota, name = storage.RateWrite, linkAnswers, writesPolicy
	}
	start := now.UTC().Truncate(rateWindow)
	used, err := u.RateLimits().Count(ctx, "link "+linkID.String(), kind, start)
	if err != nil {
		return nil, err
	}
	if used > quota {
		reset := int(start.Add(rateWindow).Sub(now).Seconds()) + 1
		return nil, problem(CodeRateLimited, fmt.Sprintf("This link was opened more than %d times in this minute (%s).", quota, name),
			fmt.Sprintf("Wait %d seconds, then try again.", reset)).withHeader("Retry-After", fmt.Sprint(reset))
	}
	s, err := journal.LoadSettings(ctx, u)
	if err != nil {
		return nil, err
	}
	if o.today, err = orgToday(ctx, a, u); err != nil {
		return nil, err
	}
	o.invoice = o.doc.Type == storage.DocumentInvoice
	if o.invoice {
		if o.data, err = salesdoc.InvoiceData(ctx, u, o.doc, s); err != nil {
			return nil, err
		}
		d, err := invoice.Read(o.doc, s.Base)
		if err != nil {
			return nil, err
		}
		o.customer = d.CustomerID
		if due := o.data.Data.Fields["document.amount_due"].Amount; due != nil && strings.Trim(due.Amount, "0.") == "" {
			return nil, notALink() // paid in full
		}
		return o, nil
	}
	if o.data, err = salesdoc.QuoteData(ctx, u, o.doc, s); err != nil {
		return nil, err
	}
	if o.quote, err = quote.Read(o.doc, s.Base); err != nil {
		return nil, err
	}
	o.customer = o.quote.Content.CustomerID
	outcomes, err := u.QuoteOutcomes().Of(ctx, []uuid.UUID{o.doc.ID})
	if err != nil {
		return nil, err
	}
	if x, ok := outcomes[o.doc.ID]; ok {
		o.outcome = &x
	}
	states, _, err := quote.Invoicing(ctx, u, o.doc.ID, o.quote, s.Base, uuid.Nil)
	if err != nil {
		return nil, err
	}
	o.status = quote.StatusOf(o.doc, o.outcome, states)
	return o, nil
}

// openLink finds what token opens and runs fn on it in its organisation's unit of work, as the customer.
func (a *API) openLink(ctx context.Context, token string, fn func(ctx context.Context, u storage.UnitOfWork, o *opened) error) error {
	ctx, org, linkID, err := a.resolveLink(ctx, token)
	if err != nil {
		return err
	}
	return a.deps.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		o, err := a.loadLink(ctx, u, org, linkID, false)
		if err != nil {
			return err
		}
		return fn(ctx, u, o)
	})
}

// canAnswer says whether a quote opened through its link takes an answer: sent, with none, and valid today.
func (o *opened) canAnswer() bool {
	return !o.invoice && o.status == quote.StatusSent && (o.quote.ValidUntil.IsZero() || !o.quote.ValidUntil.Before(o.today))
}

// publicOf is what a link shows of its document: what its PDF shows.
func publicOf(o *opened) PublicDocument {
	f := o.data.Data.Fields
	text := func(p string) string { return f[p].Text }
	day := func(p string) string { return f[p].Date }
	amount := func(p string) Money {
		if a := f[p].Amount; a != nil {
			return Money{Amount: a.Amount, Currency: a.Currency}
		}
		return Money{}
	}
	out := PublicDocument{Kind: string(o.doc.Type), Supplier: text("organisation.name"), Customer: text("customer.name"), Number: o.doc.Number,
		Date: day("document.date"), DueDate: day("document.due_date"), ValidUntil: day("document.valid_until"), Reference: text("document.reference"),
		TaxInvoice: o.invoice && o.data.Data.GSTRegistered, Lines: []PublicLine{}, Subtotal: amount("document.subtotal"), GST: amount("document.gst_total"),
		Total: amount("document.total"), CanAnswer: o.canAnswer(), LinkExpiresAt: o.link.ExpiresAt.UTC()}
	for _, l := range o.data.Data.Lines {
		out.Lines = append(out.Lines, PublicLine{Description: l.Description, Quantity: l.Quantity, UnitPrice: l.UnitPrice, GSTRate: l.GSTRate,
			Amount: Money{Amount: l.Amount.Amount, Currency: l.Amount.Currency}})
	}
	if o.invoice {
		due := amount("document.amount_due")
		out.AmountDue = &due
	} else {
		out.Status = o.status
		if o.outcome != nil {
			out.Answer = &PublicAnswer{Outcome: o.outcome.Outcome, Date: dayText(o.outcome.On), Name: o.outcome.Name}
		}
	}
	return out
}

func registerPublicDocuments(a *API) {
	readCodes := []string{salesdoc.CodeLink, salesdoc.CodeUnready, storage.CodeRetry, storage.CodeLockTimeout}
	register(a, authz.GetPublicDocument, huma.Operation{
		OperationID: "getPublicDocument", Method: http.MethodGet, Path: "/v1/public/document", Tags: []string{"Sales"},
		Summary: "Open a quote or invoice through its link",
		Description: "Public: no session. Opens the quote or invoice a link's token (in the Fyniq-Link header, from the link's fragment) was " +
			"made for (FR-AR-06, FR-AR-16): what its PDF shows, a quote's status and answer, and whether the customer can answer it here. " +
			"The first opening is recorded as the document's delivery viewed (the time only, never the reader's address). A link opens its " +
			"own document's version and nothing else, for 180 days at most, until revoked, voided or cancelled, or an invoice is paid in " +
			"full; every link that opens nothing answers the same 404 LINK-01. At most 30 a minute per link. Never cached or indexed.",
	}, readCodes, func(ctx context.Context, in *LinkHeader) (*publicDocumentOutput, error) {
		var out PublicDocument
		err := a.openLink(ctx, in.Token, func(ctx context.Context, u storage.UnitOfWork, o *opened) error {
			if _, _, err := u.DocumentLinks().Viewed(ctx, o.link.ID, a.deps.Clock.Now()); err != nil {
				return err
			}
			out = publicOf(o)
			return nil
		})
		if err != nil {
			return nil, err
		}
		return &publicDocumentOutput{CacheControl: noStore, Robots: noIndex, ReferrerPolicy: noReferrer, Body: out}, nil
	})

	register(a, authz.GetPublicDocumentPDF, huma.Operation{
		OperationID: "getPublicDocumentPDF", Method: http.MethodGet, Path: "/v1/public/document/pdf", Tags: []string{"Sales"},
		Summary: "Get the PDF of a quote or invoice through its link",
		Description: "Public: no session. The PDF of the quote or invoice a link's token opens (FR-AR-06), rendered with the template that " +
			"applies to its customer, as emailing it rendered it. Refused as getPublicDocument refuses. At most 30 a minute per link. Never " +
			"cached or indexed.",
	}, append([]string{doctemplate.CodeRenderer}, readCodes...), func(ctx context.Context, in *LinkHeader) (*publicPDFOutput, error) {
		var data salesdoc.Invoice
		var applied doctemplate.Applied
		var docType string
		err := a.openLink(ctx, in.Token, func(ctx context.Context, u storage.UnitOfWork, o *opened) error {
			data, docType = o.data, doctemplate.Quote
			if o.invoice {
				docType = doctemplate.Invoice
			}
			var err error
			applied, err = doctemplate.Applying(ctx, u, doctemplate.KindDocument, docType, &o.customer)
			return err
		})
		if err != nil {
			return nil, err
		}
		if a.deps.Renderer == nil {
			return nil, problem(doctemplate.CodeRenderer, "The document cannot be rendered just now.", "Try again later")
		}
		res, err := a.renderDocumentAt(ctx, docType, applied.Content, data.Data, data.Created)
		if err != nil {
			return nil, err
		}
		return &publicPDFOutput{CacheControl: noStore, Robots: noIndex, ReferrerPolicy: noReferrer, Body: PublicPDF{PDF: res.PDF, SHA256: res.SHA256}}, nil
	})

	answerCodes := []string{CodeInvalid, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, salesdoc.CodeLink, salesdoc.CodeUnready, quote.CodeAnswered, quote.CodeAnswer, quote.CodeValidity,
		quote.CodeNotOpen, storage.CodeQuoteOutcome, storage.CodeRetry, storage.CodeLockTimeout}
	answer := func(accept bool) func(ctx context.Context, in *publicAnswerInput) (*publicAnswerOutput, error) {
		return func(ctx context.Context, in *publicAnswerInput) (*publicAnswerOutput, error) {
			name, comment := strings.TrimSpace(in.Body.Name), strings.TrimSpace(in.Body.Comment)
			if accept && name == "" {
				return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.name", Message: "is required to accept", Fix: "Give the name of who accepts it"}})
			}
			ctx, org, linkID, err := a.resolveLink(ctx, in.Token)
			if err != nil {
				return nil, err
			}
			// The link is the caller: its answers' Idempotency-Keys are its own.
			ctx = authn.WithPrincipal(ctx, authn.Principal{Client: "link", Subject: linkID.String(), Organisation: org})
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (PublicDocument, error) {
				o, err := a.loadLink(ctx, u, org, linkID, true)
				if err != nil {
					return PublicDocument{}, err
				}
				switch {
				case o.invoice:
					return PublicDocument{}, notALink()
				case o.outcome != nil:
					return PublicDocument{}, &quote.Error{Code: quote.CodeAnswered, Message: fmt.Sprintf("the %s was %s already", quote.Kind(o.doc), o.outcome.Outcome),
						Fix: "Ask the sender for a new quote"}
				case !o.canAnswer():
					return PublicDocument{}, &quote.Error{Code: quote.CodeValidity, Message: fmt.Sprintf("the %s is no longer open to an answer", quote.Kind(o.doc)),
						Fix: "Ask the sender for a new quote"}
				}
				run := a.quotes.Decline
				if accept {
					run = a.quotes.Accept
				}
				if _, err := run(ctx, org, o.doc.ID, journal.QuoteAnswer{On: o.today, Name: name, Note: comment}); err != nil {
					return PublicDocument{}, err
				}
				outcomes, err := u.QuoteOutcomes().Of(ctx, []uuid.UUID{o.doc.ID})
				if err != nil {
					return PublicDocument{}, err
				}
				if x, ok := outcomes[o.doc.ID]; ok {
					o.outcome, o.status = &x, x.Outcome
				}
				if _, _, err := u.DocumentLinks().Viewed(ctx, o.link.ID, a.deps.Clock.Now()); err != nil {
					return PublicDocument{}, err
				}
				return publicOf(o), nil
			})
			if err != nil {
				return nil, err
			}
			out := &publicAnswerOutput{Status: w.Status, CacheControl: noStore, Robots: noIndex, ReferrerPolicy: noReferrer, Body: w.Body}
			if w.Previewed {
				out.Applied = applied(w)
			} else {
				out.Replayed = replayedHeader(w.Replayed)
			}
			return out, nil
		}
	}
	register(a, authz.AcceptQuoteByLink, huma.Operation{
		OperationID: "acceptQuoteByLink", Method: http.MethodPost, Path: "/v1/public/document/accept", Tags: []string{"Sales"},
		Summary: "Accept a quote through its link",
		Description: "Public: no session. The customer accepts the sent quote or order a link's token opens (FR-AR-16), with the name of who " +
			"accepts and a comment, kept as the quote's answer and its evidence (FR-DOC-05): recorded as the link, with the request's " +
			"address in the audit trail. Only a sent quote, still valid, with no answer: a quote has one answer, which never changes " +
			"(QUOTE-07). At most 5 answers a minute per link. Idempotent with Idempotency-Key; Prefer: return=preview checks it and records nothing.",
	}, answerCodes, answer(true))
	register(a, authz.DeclineQuoteByLink, huma.Operation{
		OperationID: "declineQuoteByLink", Method: http.MethodPost, Path: "/v1/public/document/decline", Tags: []string{"Sales"},
		Summary: "Decline a quote through its link",
		Description: "Public: no session. The customer declines the sent quote or order a link's token opens (FR-AR-16), with a comment, " +
			"kept as the quote's answer: recorded as the link, with the request's address in the audit trail. Only a sent quote, still " +
			"valid, with no answer (QUOTE-07). At most 5 answers a minute per link. Idempotent with Idempotency-Key; Prefer: return=preview " +
			"checks it and records nothing.",
	}, answerCodes, answer(false))

	register(a, authz.RevokeDocumentLink, huma.Operation{
		OperationID: "revokeDocumentLink", Method: http.MethodPost, Path: "/v1/document-links/{link_id}/revoke", Tags: []string{"Sales"},
		Summary: "Revoke a quote's or invoice's online link",
		Description: "Stops a link working at once (FR-AR-06): whoever holds it then gets LINK-01. A link is revoked once; to share the document " +
			"again, email it again, which makes a new link. Side effects: the revocation and its audit event. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview revokes nothing.",
	}, []string{CodeNotFound, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, storage.CodeLink, storage.CodeRetry, storage.CodeLockTimeout},
		func(ctx context.Context, in *revokeLinkInput) (*revokeLinkOutput, error) {
			id, err := parseID("link_id", in.LinkID)
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (DocumentLinkOut, error) {
				l, err := u.DocumentLinks().Revoke(ctx, id)
				if err != nil {
					return DocumentLinkOut{}, err
				}
				doc, err := u.Documents().Get(ctx, l.DocumentID)
				if err != nil {
					return DocumentLinkOut{}, err
				}
				return linksOut([]storage.DocumentLink{l}, doc, false, a.deps.Clock.Now())[0], nil
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &revokeLinkOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &revokeLinkOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})
}
