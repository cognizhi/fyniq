// Package salesmail emails quotes and invoices to customers (FR-AR-06; p1-sales tasks 5.2 and 5.3), for the API's
// emailInvoice and emailQuote, its reminders (FR-AR-08, task 7.1) and the scheduler's recurring invoices set to issue
// and send (task 6.1): the
// document's current version rendered with the template that applies to its customer, its PDF kept as the document's
// evidence, an online link, the email from the organisation's invoices sender, and the send. A sending is prepared
// (read and rendered, outside any unit of work, since rendering is a call to the render service), its PDF staged, and
// recorded in one unit of work.
package salesmail

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/doctemplate"
	"github.com/cognizhi/fyniq/internal/domain/evidence"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/quote"
	"github.com/cognizhi/fyniq/internal/domain/salesdoc"
	"github.com/cognizhi/fyniq/internal/domain/salesreminder"
	"github.com/cognizhi/fyniq/internal/email"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/render"
)

// The refusals a caller words for its own audience.
var (
	// ErrNoRenderer is a deployment with no render service: nothing is sent.
	ErrNoRenderer = errors.New("salesmail: no render service is configured")
	// ErrChanged is a document changed between its rendering and its send.
	ErrChanged = errors.New("salesmail: the document changed while it was being sent")
)

// MissingParticulars is a tax invoice whose PDF lacks particulars IRAS requires (FR-AR-05): it is not sent.
type MissingParticulars struct{ Missing []string }

func (m *MissingParticulars) Error() string {
	return "salesmail: the tax invoice lacks " + strings.Join(m.Missing, ", ")
}

// Render renders a document with a template, finding its particulars, as issued at created.
type Render func(ctx context.Context, documentType string, c doctemplate.Content, d render.Data, created time.Time) (render.Response, error)

// Sender emails documents of one deployment.
type Sender struct {
	Store   storage.Store
	Render  Render // nil: nothing renders, so nothing is sent
	Staging ports.BlobStore
	Outbox  outbox.Dispatcher
	Origin  string // the web app's origin, for the online link; empty: the email carries none
	Clock   ports.Clock
}

// Sending is what emailing a document takes, read and rendered before its unit of work.
type Sending struct {
	DocType  string // the template's document type: invoice or quote
	Purpose  string // the email's
	Kind     string // in words: invoice, quote or sales order
	Version  int
	To       string
	Subject  string
	Body     string
	FileName string
	Applied  doctemplate.Applied
	PDF      []byte
	SHA      string
	Link     string // the online link's token's SHA-256 (task 5.3); empty when the deployment has no web origin

	// A reminder (task 7.1) sends again the PDF already sent of the version, this evidence, when there is one; it renders
	// one otherwise. Evidence is uuid.Nil when the sending carries a PDF of its own.
	Evidence uuid.UUID
	Reminder *salesreminder.Reminder
}

// Prepare reads document id of org for emailing to to (empty: the customer's billing email): an issued invoice
// (invoiceSend) or a sent quote or order, at its current version; the email's subject and body from the email template
// that applies to the customer, with the online link; and, when rendering, its PDF from the document template that
// applies, which a tax invoice must show every particular in. inUnit, when set, runs in the read's unit of work with the
// document, such as for an ETag.
func (s Sender) Prepare(ctx context.Context, org, id uuid.UUID, invoiceSend bool, to string, rendering bool,
	inUnit func(ctx context.Context, u storage.UnitOfWork, doc storage.Document) error) (Sending, error) {
	return s.prepare(ctx, org, id, invoiceSend, to, rendering, inUnit, nil)
}

// PrepareReminder is Prepare for reminder r (FR-AR-08): an email of purpose reminder, from the reminders sender, its
// subject and body from the reminder email template once the invoice is overdue, and from the invoice's or quote's own
// before; the PDF of the version already sent, if one was, else rendered.
func (s Sender) PrepareReminder(ctx context.Context, org uuid.UUID, r salesreminder.Reminder, to string, rendering bool,
	inUnit func(ctx context.Context, u storage.UnitOfWork, doc storage.Document) error) (Sending, error) {
	return s.prepare(ctx, org, r.DocumentID, r.Kind == salesreminder.KindInvoice, to, rendering, inUnit, &r)
}

func (s Sender) prepare(ctx context.Context, org, id uuid.UUID, invoiceSend bool, to string, rendering bool,
	inUnit func(ctx context.Context, u storage.UnitOfWork, doc storage.Document) error, reminder *salesreminder.Reminder) (Sending, error) {
	out := Sending{Reminder: reminder}
	var data salesdoc.Invoice
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Get(ctx, id)
		if err != nil {
			return err
		}
		settings, err := journal.LoadSettings(ctx, u)
		if err != nil {
			return err
		}
		var customer uuid.UUID
		switch {
		case invoiceSend && doc.Type == storage.DocumentInvoice:
			out.DocType, out.Purpose, out.Kind = doctemplate.Invoice, email.PurposeInvoice, "invoice"
		case !invoiceSend && quote.IsQuote(doc):
			out.DocType, out.Purpose, out.Kind = doctemplate.Quote, email.PurposeQuote, quote.Kind(doc)
		case invoiceSend:
			return &invoice.Error{Code: invoice.CodeNotInvoice, Message: fmt.Sprintf("document %s is not an invoice", doc.ID), Fix: "Email an invoice"}
		default:
			return &quote.Error{Code: quote.CodeNotQuote, Message: fmt.Sprintf("document %s is not a quote or a sales order", doc.ID), Fix: "Email a quote"}
		}
		if doc.Status != storage.DocumentPosted {
			what := map[storage.DocumentStatus]string{storage.DocumentDraft: "a draft", storage.DocumentReversed: "voided"}[doc.Status]
			if !invoiceSend && doc.Status == storage.DocumentReversed {
				what = "cancelled"
			}
			return &salesdoc.Error{Code: salesdoc.CodeNotSent, Message: fmt.Sprintf("the %s is %s", out.Kind, what),
				Fix: "Email an issued invoice or a sent quote; a draft is issued or sent first"}
		}
		if invoiceSend {
			if data, err = salesdoc.InvoiceData(ctx, u, doc, settings); err != nil {
				return err
			}
			d, err := invoice.Read(doc, settings.Base)
			if err != nil {
				return err
			}
			customer = d.CustomerID
		} else {
			if data, err = salesdoc.QuoteData(ctx, u, doc, settings); err != nil {
				return err
			}
			d, err := quote.Read(doc, settings.Base)
			if err != nil {
				return err
			}
			customer = d.Content.CustomerID
		}
		if out.To = strings.TrimSpace(to); out.To == "" {
			out.To = strings.TrimSpace(data.Data.Fields["customer.email"].Text)
		}
		if out.To == "" {
			return &salesdoc.Error{Code: salesdoc.CodeNoRecipient, Message: "the customer has no billing email",
				Fix: "Give the address to send it to, or add the customer's billing email"}
		}
		if out.Applied, err = doctemplate.Applying(ctx, u, doctemplate.KindDocument, out.DocType, &customer); err != nil {
			return err
		}
		mailType := out.DocType
		if reminder != nil {
			out.Purpose = email.PurposeReminder
			if reminder.Overdue() {
				mailType = doctemplate.Reminder
			}
		}
		mail, err := doctemplate.Applying(ctx, u, doctemplate.KindEmail, mailType, &customer)
		if err != nil {
			return err
		}
		// The email carries the online link, its token in the URL's fragment, never sent to a server (task 5.3); the PDF
		// does not.
		emailed := data.Data
		emailed.Fields = maps.Clone(data.Data.Fields)
		if origin := strings.TrimRight(s.Origin, "/"); origin != "" {
			token, hash, err := salesdoc.NewToken()
			if err != nil {
				return err
			}
			emailed.Fields["document.link"] = render.Value{Text: origin + "/d/#" + token}
			out.Link = hash
		}
		if reminder != nil {
			reminderFields(emailed.Fields, *reminder)
			if out.Evidence, err = sentPDF(ctx, u, &out, doc); err != nil {
				return err
			}
		}
		if out.Subject, out.Body, err = render.Email(mail.Content, emailed); err != nil {
			return err
		}
		if utf8.RuneCountInString(out.Subject) > 200 {
			out.Subject = string([]rune(out.Subject)[:199]) + "…"
		}
		out.Version, out.FileName = doc.Version, doc.Number+".pdf"
		if inUnit != nil {
			return inUnit(ctx, u, doc)
		}
		return nil
	})
	if err != nil || !rendering || out.Evidence != uuid.Nil {
		return out, err
	}
	if s.Render == nil {
		return out, ErrNoRenderer
	}
	res, err := s.Render(ctx, out.DocType, out.Applied.Content, data.Data, data.Created)
	if err != nil {
		return out, err
	}
	if len(res.Missing) > 0 {
		return out, &MissingParticulars{Missing: res.Missing}
	}
	out.PDF, out.SHA = res.PDF, res.SHA256
	return out, nil
}

// Stage keeps a sending's PDF in the staging store until its evidence row is committed, and returns where; if the unit
// of work that records it rolls back, the sweeper deletes the file (FR-DOC-13). A sending that sends again a PDF
// already kept stages nothing.
func (s Sender) Stage(ctx context.Context, org uuid.UUID, x Sending) (string, error) {
	if x.Evidence != uuid.Nil {
		return "", nil
	}
	key := evidence.StagingKey(org, uuid.Must(uuid.NewV7()))
	return key, s.Staging.Put(ctx, key, bytes.NewReader(x.PDF))
}

// Record keeps a sending of document id in u, as the unit of work's actor: the PDF staged at key as the document's
// evidence (or the one already kept), the email with it attached from the invoices or reminders sender, the online
// link, the send, and for a reminder the decision; the evidence is committed to the locked store by the worker. The
// document must still be posted at the version rendered (ErrChanged).
func (s Sender) Record(ctx context.Context, u storage.UnitOfWork, org, id uuid.UUID, x Sending, key string) error {
	doc, err := u.Documents().Lock(ctx, id)
	if err != nil {
		return err
	}
	if doc.Status != storage.DocumentPosted || doc.Version != x.Version {
		return ErrChanged
	}
	by := "system:salesmail"
	if a := storage.AuditFrom(ctx).Actor; a != nil {
		by = a.String()
	}
	evidenceID := x.Evidence
	if evidenceID == uuid.Nil {
		e, err := u.Evidence().Attach(ctx, storage.NewEvidence{DocumentID: id, EvidenceType: strings.ToUpper(x.Kind[:1]) + x.Kind[1:] + " as sent",
			FileName: x.FileName, MediaType: "application/pdf", Size: int64(len(x.PDF)), SHA256: x.SHA, OriginalSHA256: x.SHA,
			Source: storage.SourceGenerated, DataClass: storage.ClassGeneral, StagingKey: key, UploadedBy: by})
		if err != nil {
			return err
		}
		evidenceID = e.ID
	}
	m, err := email.QueueWith(ctx, s.Outbox, u, org, x.Purpose, x.To, x.Subject, x.Body, []uuid.UUID{evidenceID})
	if err != nil {
		return err
	}
	if x.Link != "" {
		if _, err := u.DocumentLinks().Create(ctx, storage.NewDocumentLink{DocumentID: id, DocumentVersion: x.Version, MessageID: m.ID, TokenHash: x.Link,
			ExpiresAt: s.Clock.Now().Add(salesdoc.LinkDays * 24 * time.Hour)}); err != nil {
			return err
		}
	}
	send, err := u.DocumentSends().Record(ctx, storage.NewDocumentSend{DocumentID: id, DocumentVersion: x.Version, MessageID: m.ID, EvidenceID: evidenceID,
		Recipient: x.To, TemplateSource: x.Applied.Source, TemplateID: x.Applied.TemplateID, TemplateVersion: x.Applied.Version,
		TemplateHash: fmt.Sprintf("%x", x.Applied.Hash), PDFSHA256: x.SHA})
	if err != nil {
		return err
	}
	if x.Reminder != nil {
		if _, err := u.SalesReminders().Record(ctx, storage.NewSalesReminder{DocumentID: id, Step: x.Reminder.Step, Outcome: storage.ReminderSent,
			SendID: &send.ID}); err != nil {
			return err
		}
	}
	if x.Evidence != uuid.Nil {
		return nil // the PDF was committed with its first send
	}
	_, err = s.Outbox.Enqueue(ctx, u, org, storage.NewOutboxItem{Class: storage.ClassWorker, Kind: evidence.CommitKind, Payload: []byte(`{}`)})
	return err
}

// reminderFields are the reminder email template's fields for r (doctemplate.Reminder): what is due, what of it is
// overdue, since when, and by how many days.
func reminderFields(f map[string]render.Value, r salesreminder.Reminder) {
	f["document.amount_due"] = render.Value{Amount: &render.Amount{Amount: r.Amount.String(), Currency: r.Amount.Currency().Code}}
	overdue := money.Zero(r.Amount.Currency())
	if r.Overdue() {
		overdue = r.Amount
	}
	f["document.overdue"] = render.Value{Amount: &render.Amount{Amount: overdue.String(), Currency: overdue.Currency().Code}}
	f["document.oldest_due_date"] = render.Value{Date: r.DueOn.Format(time.DateOnly)}
	days := int64(r.DaysOverdue)
	f["document.days_overdue"] = render.Value{Number: &days}
}

// sentPDF is the evidence of the PDF last sent of doc's current version, with the template that rendered it, written
// into x: what a reminder sends again. uuid.Nil when no send carried that version.
func sentPDF(ctx context.Context, u storage.UnitOfWork, x *Sending, doc storage.Document) (uuid.UUID, error) {
	sends, err := u.DocumentSends().OfDocument(ctx, doc.ID)
	if err != nil {
		return uuid.Nil, err
	}
	for _, s := range sends { // newest first
		if s.DocumentVersion != doc.Version {
			continue
		}
		hash, err := hex.DecodeString(s.TemplateHash)
		if err != nil {
			return uuid.Nil, err
		}
		x.Applied = doctemplate.Applied{Source: s.TemplateSource, TemplateID: s.TemplateID, Version: s.TemplateVersion, Hash: hash}
		x.SHA = s.PDFSHA256
		return s.EvidenceID, nil
	}
	return uuid.Nil, nil
}

// SendScheduled emails, as the scheduler, the invoices of org it issued from recurring invoices and has not sent yet
// (FR-AR-07): each prepared and rendered, its PDF staged, and recorded, which the database allows the scheduler only for
// such an invoice (FY201, FY202). One that cannot go now, for want of a billing email, a particular IRAS requires or a
// render service, is left for a later run. It answers how many it sent.
func (s Sender) SendScheduled(ctx context.Context, org uuid.UUID) (int, error) {
	ctx = storage.WithAudit(ctx, storage.AuditContext{Actor: &journal.RecurringInvoiceMaker, Reason: journal.RecurringInvoiceReason})
	var ids []uuid.UUID
	if err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		ids, err = u.RecurringInvoices().Unsent(ctx, maxScheduledSends)
		return err
	}); err != nil {
		return 0, err
	}
	n := 0
	var errs []error
	for _, id := range ids {
		x, err := s.Prepare(ctx, org, id, true, "", true, nil)
		var missing *MissingParticulars
		var se *salesdoc.Error
		switch {
		case errors.Is(err, ErrNoRenderer) || errors.As(err, &missing) || errors.As(err, &se):
			continue // not now: a person sees it unsent on the invoice
		case err != nil:
			errs = append(errs, fmt.Errorf("invoice %s: %w", id, err))
			continue
		}
		key, err := s.Stage(ctx, org, x)
		if err != nil {
			errs = append(errs, fmt.Errorf("invoice %s: %w", id, err))
			continue
		}
		if err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			return s.Record(ctx, u, org, id, x, key)
		}); err != nil {
			_ = s.Staging.Delete(context.WithoutCancel(ctx), key) // no row uses it
			errs = append(errs, fmt.Errorf("invoice %s: %w", id, err))
			continue
		}
		n++
	}
	return n, errors.Join(errs...)
}

// maxScheduledSends is how many of an organisation's issued invoices a run sends at most; the next run sends the rest.
const maxScheduledSends = 100

// Scheduled runs the recurring invoices (FR-AR-07): the drafts of each occurrence, the invoices the scheduler issues of
// those set to issue and send, and their emails. It is the platform job invoices.recurring.
type Scheduled struct {
	Invoices journal.Invoices
	Sender   Sender
}

// RunAll makes, issues and sends up to each organisation's own today at now. An organisation that fails does not stop
// the others; their errors come back together.
func (x Scheduled) RunAll(ctx context.Context, now time.Time) error {
	_, err := x.Invoices.MakeDueAll(ctx, now)
	errs := []error{err}
	orgs, err := x.Invoices.Store.Organisations(ctx)
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	for _, org := range orgs {
		if _, err := x.Sender.SendScheduled(ctx, org); err != nil {
			errs = append(errs, fmt.Errorf("organisation %s: %w", org, err))
		}
	}
	return errors.Join(errs...)
}
