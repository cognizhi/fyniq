package invoicenow

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ErrNotASale is Enqueue's answer for a document that is not a posted invoice or credit note: bills reach IRAS from the
// customer's side (FR-TAX-10: solution-extracted purchases), which is not built yet (p2-invoicenow, Left).
var ErrNotASale = errors.New("only a posted invoice or credit note can be queued for InvoiceNow here")

// Enqueue queues the next version of a posted sale's submission. When the customer's Peppol ID is registered on the
// network it is a Peppol submission to them; with none, or one the directory does not know, it is a solution-extracted sale
// that goes to IRAS alone (FR-TAX-10). The document is built and checked first, so one the rules refuse is never queued
// (a *RefusedError says which rule).
func Enqueue(ctx context.Context, u storage.UnitOfWork, ap ports.AccessPoint, documentID uuid.UUID, base money.Currency, now time.Time) (storage.InvoiceSubmission, error) {
	doc, err := u.Documents().Get(ctx, documentID)
	if err != nil {
		return storage.InvoiceSubmission{}, err
	}
	if doc.Status != storage.DocumentPosted || !(invoice.IsInvoice(doc) || invoice.IsCreditNote(doc)) {
		return storage.InvoiceSubmission{}, ErrNotASale
	}
	// the document's UUID is the first version's, kept for every one after it
	docUUID := uuid.New()
	if prior, err := u.InvoiceNow().Submissions(ctx, storage.InvoiceSubmissionFilter{DocumentID: &documentID, Limit: 1}); err == nil && len(prior) > 0 {
		docUUID = prior[0].DocumentUUID
	}
	d, err := FromInvoice(ctx, u, doc, docUUID, base)
	if err != nil {
		return storage.InvoiceSubmission{}, err
	}
	b, err := Build(d)
	if err != nil {
		return storage.InvoiceSubmission{}, err
	}
	kind, recipient := "extracted_sale", ""
	if id := d.Buyer.ParticipantID(); id != "" && d.Buyer.EndpointID != "" {
		p, err := ap.Lookup(ctx, id)
		if err != nil {
			return storage.InvoiceSubmission{}, err
		}
		if p.Registered {
			kind, recipient = "peppol", id
		}
	}
	sum := sha256.Sum256(b)
	return u.InvoiceNow().Queue(ctx, storage.NewInvoiceSubmission{DocumentID: documentID, DocumentUUID: docUUID, Kind: kind, Direction: "sale", Recipient: recipient,
		PayloadSHA256: sum[:], At: now})
}

// Send hands a queued submission to the access point. The document is built again from the posted document and must be the
// bytes that were queued; a document that changed since is failed with that reason, never sent. An outage leaves the
// submission queued and returns ports.ErrAccessPointUnavailable for the job to retry (G4-033).
func Send(ctx context.Context, u storage.UnitOfWork, ap ports.AccessPoint, sub storage.InvoiceSubmission, base money.Currency, now time.Time) (storage.InvoiceSubmission, error) {
	if sub.Status != string(Queued) {
		return sub, nil
	}
	fail := func(reason string) (storage.InvoiceSubmission, error) {
		return u.InvoiceNow().Move(ctx, sub.ID, storage.InvoiceSubmissionMove{Status: string(Failed), Error: reason, At: now})
	}
	doc, err := u.Documents().Get(ctx, sub.DocumentID)
	if err != nil {
		return sub, err
	}
	d, err := FromInvoice(ctx, u, doc, sub.DocumentUUID, base)
	var refused *RefusedError
	if errors.As(err, &refused) {
		return fail(refused.Error())
	}
	if err != nil {
		return sub, err
	}
	b, err := Build(d)
	if errors.As(err, &refused) {
		return fail(refused.Error())
	}
	if err != nil {
		return sub, err
	}
	if sum := sha256.Sum256(b); string(sum[:]) != string(sub.PayloadSHA256) {
		return fail("the document changed after it was queued: queue it again")
	}
	id, err := ap.Send(ctx, ports.AccessPointSend{To: sub.Recipient, DocumentUUID: sub.DocumentUUID.String(), Version: sub.Version, Kind: string(d.Kind), Payload: b,
		Key: fmt.Sprintf("%s:%d", sub.DocumentUUID, sub.Version)})
	if err != nil {
		return sub, err
	}
	return u.InvoiceNow().Move(ctx, sub.ID, storage.InvoiceSubmissionMove{Status: string(Sent), TransmissionID: id, At: now})
}

// Refresh reads a sent submission's status at the access point: acknowledged keeps IRAS's acknowledgement ID, failed keeps
// the reasons; anything else leaves it as it is.
func Refresh(ctx context.Context, u storage.UnitOfWork, ap ports.AccessPoint, sub storage.InvoiceSubmission, now time.Time) (storage.InvoiceSubmission, error) {
	if sub.Status != string(Sent) || sub.TransmissionID == "" {
		return sub, nil
	}
	st, err := ap.Status(ctx, sub.TransmissionID)
	if err != nil {
		return sub, err
	}
	switch st.State {
	case ports.TransmissionAcknowledged:
		if st.IRASAck == "" {
			return sub, nil // an acknowledgement with no ID is not one
		}
		return u.InvoiceNow().Move(ctx, sub.ID, storage.InvoiceSubmissionMove{Status: string(Acknowledged), IRASAck: st.IRASAck, At: now})
	case ports.TransmissionFailed:
		return u.InvoiceNow().Move(ctx, sub.ID, storage.InvoiceSubmissionMove{Status: string(Failed), Error: strings.Join(st.Reasons, "; "), At: now})
	}
	return sub, nil
}

// DispatchJob is the platform job that hands queued submissions to the access point and reads the status of the sent ones (FR-AR-12, FR-TAX-10), every fifteen minutes. It
// runs as the worker role, which moves a submission along and cannot queue one; an outage leaves everything queued and the next run tries again (G4-033).
const DispatchJob = "invoicenow.dispatch"

// DispatchResult is what a run did.
type DispatchResult struct {
	Sent, Refreshed, Failed int
	Outage                  bool
}

// DispatchAll is the job: each organisation's queued submissions are sent and its sent ones read, a submission at a time in a unit of work of its own, so one that fails
// does not stop the others. An organisation that fails does not stop the rest.
func DispatchAll(ctx context.Context, store storage.Store, ap ports.AccessPoint, now func() time.Time) (DispatchResult, error) {
	var out DispatchResult
	if ap == nil {
		return out, nil // no access point is connected
	}
	orgs, err := store.Organisations(ctx)
	if err != nil {
		return out, err
	}
	var errs []error
	for _, org := range orgs {
		if outage, err := dispatchOrg(ctx, store, ap, org, now, &out); err != nil {
			errs = append(errs, fmt.Errorf("organisation %s: %w", org, err))
		} else if outage {
			out.Outage = true
			break // the access point is down for everyone: try again next run
		}
	}
	return out, errors.Join(errs...)
}

func dispatchOrg(ctx context.Context, store storage.Store, ap ports.AccessPoint, org uuid.UUID, now func() time.Time, out *DispatchResult) (bool, error) {
	var queued, sent []storage.InvoiceSubmission
	var base money.Currency
	if err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		if base, err = money.Lookup(o.BaseCurrency); err != nil {
			return err
		}
		if queued, err = u.InvoiceNow().Submissions(ctx, storage.InvoiceSubmissionFilter{Status: string(Queued), Limit: 200}); err != nil {
			return err
		}
		sent, err = u.InvoiceNow().Submissions(ctx, storage.InvoiceSubmissionFilter{Status: string(Sent), Limit: 200})
		return err
	}); err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			return false, nil // an organisation with no profile yet, or one that is gone, has nothing to send
		}
		return false, err
	}
	var errs []error
	for _, s := range append(queued, sent...) {
		var outage bool
		err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			var err error
			var after storage.InvoiceSubmission
			if s.Status == string(Queued) {
				after, err = Send(ctx, u, ap, s, base, now())
				if err == nil {
					if after.Status == string(Failed) {
						out.Failed++
					} else {
						out.Sent++
					}
				}
			} else {
				after, err = Refresh(ctx, u, ap, s, now())
				if err == nil && after.Status != s.Status {
					out.Refreshed++
					if after.Status == string(Failed) {
						out.Failed++
					}
				}
			}
			if errors.Is(err, ports.ErrAccessPointUnavailable) {
				outage = true
				return nil
			}
			return err
		})
		if outage {
			return true, nil
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	return false, errors.Join(errs...)
}
