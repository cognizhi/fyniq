package api

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/billpayment"
	"github.com/cognizhi/fyniq/internal/domain/claim"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/paymentbatch"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/remitmail"
)

// CodeBatchNoRemittance is a supplier in a batch with no remittance email (⚠): it is paid, and sent no advice (design D8).
const CodeBatchNoRemittance = "PB-23"

// releasedPayments are a released batch's payments as the release posted them, in its order: each payee, what it paid of
// each item, whether bank import matched it to its statement line (paid, design D8) or it was voided (rejected), and a
// supplier's remittance advice; with the total, and whether every payment is paid.
func (a *API) releasedPayments(ctx context.Context, u storage.UnitOfWork, posted []PaymentBatchPosted) ([]PaymentBatchPayment, Money, bool, error) {
	out := []PaymentBatchPayment{}
	total := money.Zero(paymentbatch.SGD)
	all := len(posted) > 0
	for _, x := range posted {
		s := x.PaymentID
		id, err := uuid.Parse(s)
		if err != nil {
			return nil, Money{}, false, err
		}
		doc, err := u.Documents().Get(ctx, id)
		if err != nil {
			return nil, Money{}, false, err
		}
		matches, err := u.BankMatching().Matches(ctx, storage.BankMatchFilter{DocumentID: &id})
		if err != nil {
			return nil, Money{}, false, err
		}
		pay := PaymentBatchPayment{PaymentID: s, Items: []PaymentBatchItem{}, Rejected: doc.Status == storage.DocumentReversed}
		for _, m := range matches {
			pay.Paid = pay.Paid || m.UndoneAt == nil
		}
		amount := money.Zero(paymentbatch.SGD)
		add := func(kind string, docID uuid.UUID, part money.Money) error {
			d, err := u.Documents().Get(ctx, docID)
			if err != nil {
				return err
			}
			pay.Items = append(pay.Items, PaymentBatchItem{Kind: kind, DocumentID: docID.String(), Number: d.Number, Date: d.Date, Open: moneyOut(part),
				Checks: []BillCheck{}})
			amount, err = amount.Add(part)
			return err
		}
		switch doc.Type {
		case storage.DocumentBillPayment:
			d, err := billpayment.Read(doc, paymentbatch.SGD)
			if err != nil {
				return nil, Money{}, false, err
			}
			c, err := u.Contacts().Get(ctx, d.SupplierID)
			if err != nil {
				return nil, Money{}, false, err
			}
			pay.Payee = PaymentBatchPayee{Kind: "vendor", ID: d.SupplierID.String(), Name: c.LegalName}
			for _, x := range d.Bills {
				if err := add(paymentbatch.KindBill, x.BillID, x.Amount); err != nil {
					return nil, Money{}, false, err
				}
			}
			sends, err := u.DocumentSends().OfDocument(ctx, id)
			if err != nil {
				return nil, Money{}, false, err
			}
			queued, err := u.Outbox().ByKey(ctx, remitmail.Kind, remitmail.Key(id))
			if err != nil {
				return nil, Money{}, false, err
			}
			switch {
			case len(sends) > 0:
				pay.Remittance = "sent"
			case strings.TrimSpace(c.RemittanceEmail) == "":
				pay.Remittance = "no_email"
			case queued != nil && queued.State == storage.OutboxDead:
				pay.Remittance = "failed" // its attempts ran out; nothing queues it again
			case queued != nil:
				pay.Remittance = "queued"
			default:
				pay.Remittance = "waiting"
			}
		case storage.DocumentClaimReimbursement:
			r, err := claim.ReadReimbursement(doc)
			if err != nil {
				return nil, Money{}, false, err
			}
			person := Person{Kind: "user", ID: r.ClaimantID.String()}
			if err := a.name(ctx, u, &person); err != nil {
				return nil, Money{}, false, err
			}
			pay.Payee = PaymentBatchPayee{Kind: "person", ID: person.ID, Name: person.Name}
			for _, x := range r.Claims {
				if err := add(paymentbatch.KindClaim, x.ClaimID, x.Amount); err != nil {
					return nil, Money{}, false, err
				}
			}
		}
		pay.Amount = moneyOut(amount)
		if total, err = total.Add(amount); err != nil {
			return nil, Money{}, false, err
		}
		all = all && pay.Paid
		out = append(out, pay)
	}
	return out, moneyOut(total), all, nil
}
