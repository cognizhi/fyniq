// Package remitmail emails a supplier its remittance advice (FR-BNK-12, FR-AP-13; PRD-001 rev 0.154; p1-payments-out
// task 2.7, design D8): when bank import matches a supplier payment that a payment batch released to its statement line,
// the match writes the job, once per payment however often the match is undone and done again; the worker renders the
// remittance template that applies to the supplier to PDF, keeps it as the payment's evidence, emails it to the
// supplier's remittance address from the organisation's invoices sender, and records the send. A payment voided since,
// a supplier with no remittance address, or one already sent, sends nothing; a claimant's reimbursement never sends. A
// job that gave up is sent again only by a person (Again; rev 0.160).
package remitmail

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/billpayment"
	"github.com/cognizhi/fyniq/internal/domain/doctemplate"
	"github.com/cognizhi/fyniq/internal/domain/evidence"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/paymentbatch"
	"github.com/cognizhi/fyniq/internal/domain/salesdoc"
	"github.com/cognizhi/fyniq/internal/email"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/render"
	"github.com/cognizhi/fyniq/internal/salesmail"
)

// Kind is the outbox kind of a remittance advice, run by the worker class.
const Kind = "payments.remittance"

// ErrNoRenderer is a deployment with no render service: nothing is sent, and the job is retried until it has one.
var ErrNoRenderer = errors.New("remitmail: no render service is configured")

type job struct {
	PaymentID uuid.UUID `json:"payment_id"`
}

// Key is the business key of a payment's remittance job: one per payment.
func Key(paymentID uuid.UUID) string { return "remittance:" + paymentID.String() }

// againKey is the business key of the nth time a person sent a payment's failed advice again (rev 0.160).
func againKey(paymentID uuid.UUID, n int) string { return fmt.Sprintf("%s:again:%d", Key(paymentID), n) }

// Latest is a payment's latest remittance job, nil when none was queued, and how many times a person sent it again:
// the job the match queued, then each one Again queued after it.
func Latest(ctx context.Context, u storage.UnitOfWork, paymentID uuid.UUID) (*storage.OutboxRow, int, error) {
	row, err := u.Outbox().ByKey(ctx, Kind, Key(paymentID))
	if err != nil || row == nil {
		return row, 0, err
	}
	n := 0
	for {
		next, err := u.Outbox().ByKey(ctx, Kind, againKey(paymentID, n+1))
		if err != nil || next == nil {
			return row, n, err
		}
		row, n = next, n+1
	}
}

// Again queues a payment's failed advice once more, under a key of its own, for a person who sent it again (FR-BNK-12,
// rev 0.160): the sender's checks hold, so a payment voided or sent since sends nothing, and it sends at most once.
func Again(ctx context.Context, d outbox.Dispatcher, u storage.UnitOfWork, org, paymentID uuid.UUID) error {
	_, n, err := Latest(ctx, u, paymentID)
	if err != nil {
		return err
	}
	b, err := json.Marshal(job{PaymentID: paymentID})
	if err != nil {
		return err
	}
	_, err = d.Enqueue(ctx, u, org, storage.NewOutboxItem{Class: storage.ClassWorker, Kind: Kind, Payload: b, BusinessKey: againKey(paymentID, n+1)})
	return err
}

// Queue writes the remittance job of doc, a document just matched to a statement line, in u: only for a supplier payment
// that a batch released, and only once.
func Queue(ctx context.Context, d outbox.Dispatcher, u storage.UnitOfWork, org uuid.UUID, doc storage.Document) error {
	if doc.Type != storage.DocumentBillPayment {
		return nil
	}
	rel, err := u.PaymentBatches().ReleaseOfPayment(ctx, doc.ID)
	if err != nil || rel == nil {
		return err
	}
	if row, err := u.Outbox().ByKey(ctx, Kind, Key(doc.ID)); err != nil || row != nil {
		return err
	}
	b, err := json.Marshal(job{PaymentID: doc.ID})
	if err != nil {
		return err
	}
	_, err = d.Enqueue(ctx, u, org, storage.NewOutboxItem{Class: storage.ClassWorker, Kind: Kind, Payload: b, BusinessKey: Key(doc.ID)})
	return err
}

// Sender sends remittance advice for one deployment.
type Sender struct {
	Store   storage.Store
	Render  salesmail.Render // nil: nothing renders, so nothing is sent
	Staging ports.BlobStore
	Outbox  outbox.Dispatcher
}

// Actor is who sends remittance advice in the audit trail.
var Actor = storage.Actor{Type: "system", ID: "remittance"}

// prepared is a remittance read and laid out before it is rendered.
type prepared struct {
	skip              bool
	doc               storage.Document
	to, subject, body string
	applied           doctemplate.Applied
	data              salesdoc.Invoice
}

// methods name a batch's method on the advice.
var methods = map[string]string{paymentbatch.FAST: "FAST bank transfer", paymentbatch.GIRO: "GIRO bank transfer"}

// prepare reads payment id of org and lays it out, or says to skip it: voided, sent already, not a batch's, or a supplier
// with no remittance address.
func (s Sender) prepare(ctx context.Context, org, id uuid.UUID) (prepared, error) {
	var out prepared
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Get(ctx, id)
		if err != nil {
			return err
		}
		sends, err := u.DocumentSends().OfDocument(ctx, id)
		if err != nil {
			return err
		}
		rel, err := u.PaymentBatches().ReleaseOfPayment(ctx, id)
		if err != nil {
			return err
		}
		if doc.Type != storage.DocumentBillPayment || doc.Status != storage.DocumentPosted || len(sends) > 0 || rel == nil {
			out.skip = true
			return nil
		}
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		base, err := money.Lookup(o.BaseCurrency)
		if err != nil {
			return err
		}
		d, err := billpayment.Read(doc, base)
		if err != nil {
			return err
		}
		c, err := u.Contacts().Get(ctx, d.SupplierID)
		if err != nil {
			return err
		}
		if out.to = strings.TrimSpace(c.RemittanceEmail); out.to == "" {
			out.skip = true // the batch shows the supplier ⚠: no remittance address
			return nil
		}
		batch, err := u.Documents().Get(ctx, rel.BatchID)
		if err != nil {
			return err
		}
		b, err := paymentbatch.Read(batch)
		if err != nil {
			return err
		}
		if out.data, err = salesdoc.RemittanceData(ctx, u, doc, base, methods[b.Method]); err != nil {
			return err
		}
		if out.applied, err = doctemplate.Applying(ctx, u, doctemplate.KindDocument, doctemplate.Remittance, &d.SupplierID); err != nil {
			return err
		}
		mail, err := doctemplate.Applying(ctx, u, doctemplate.KindEmail, doctemplate.Remittance, &d.SupplierID)
		if err != nil {
			return err
		}
		if out.subject, out.body, err = render.Email(mail.Content, out.data.Data); err != nil {
			return err
		}
		if utf8.RuneCountInString(out.subject) > 200 {
			out.subject = string([]rune(out.subject)[:199]) + "…"
		}
		out.doc = doc
		return nil
	})
	return out, err
}

// Work is the worker's handler of Kind.
func (s Sender) Work() func(ctx context.Context, org uuid.UUID, row storage.OutboxRow) error {
	return func(ctx context.Context, org uuid.UUID, row storage.OutboxRow) error {
		var j job
		if err := json.Unmarshal(row.Payload, &j); err != nil {
			return err
		}
		ctx = storage.WithAudit(ctx, storage.AuditContext{Actor: &Actor})
		x, err := s.prepare(ctx, org, j.PaymentID)
		if err != nil || x.skip {
			return err
		}
		if s.Render == nil {
			return ErrNoRenderer
		}
		res, err := s.Render(ctx, doctemplate.Remittance, x.applied.Content, x.data.Data, x.data.Created)
		if err != nil {
			return err
		}
		key := evidence.StagingKey(org, uuid.Must(uuid.NewV7()))
		// Staged before the unit of work: if it rolls back, the sweeper deletes the file (FR-DOC-13).
		if err := s.Staging.Put(ctx, key, bytes.NewReader(res.PDF)); err != nil {
			return err
		}
		return s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := u.Documents().Lock(ctx, j.PaymentID)
			if err != nil {
				return err
			}
			if sends, err := u.DocumentSends().OfDocument(ctx, doc.ID); err != nil || len(sends) > 0 {
				return err
			}
			if doc.Status != storage.DocumentPosted || doc.Version != x.doc.Version {
				return nil // voided while it was laid out: nothing is sent
			}
			e, err := u.Evidence().Attach(ctx, storage.NewEvidence{DocumentID: doc.ID, EvidenceType: "Remittance advice as sent",
				FileName: doc.Number + "-remittance.pdf", MediaType: "application/pdf", Size: int64(len(res.PDF)), SHA256: res.SHA256,
				OriginalSHA256: res.SHA256, Source: storage.SourceGenerated, DataClass: storage.ClassGeneral, StagingKey: key, UploadedBy: Actor.String()})
			if err != nil {
				return err
			}
			m, err := email.QueueWith(ctx, s.Outbox, u, org, email.PurposeRemittance, x.to, x.subject, x.body, []uuid.UUID{e.ID})
			if err != nil {
				return err
			}
			if _, err := u.DocumentSends().Record(ctx, storage.NewDocumentSend{DocumentID: doc.ID, DocumentVersion: doc.Version, MessageID: m.ID,
				EvidenceID: e.ID, Recipient: x.to, TemplateSource: x.applied.Source, TemplateID: x.applied.TemplateID, TemplateVersion: x.applied.Version,
				TemplateHash: fmt.Sprintf("%x", x.applied.Hash), PDFSHA256: res.SHA256}); err != nil {
				return err
			}
			_, err = s.Outbox.Enqueue(ctx, u, org, storage.NewOutboxItem{Class: storage.ClassWorker, Kind: evidence.CommitKind, Payload: []byte(`{}`)})
			return err
		})
	}
}
