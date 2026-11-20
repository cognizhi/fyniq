//go:build local

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// InvoiceNow's records keep their rules whoever writes them (FR-TAX-10, FR-TAX-11; FYI01 to FYI04): a submission moves only
// queued, sent, acknowledged (with IRAS's acknowledgement ID) or to failed and back, an acknowledged one never moves, a later
// version keeps the first's UUID; a document has one live exclusion, withdrawn once; a contingency is never approved by its
// requester; the worker role moves a submission but cannot queue one; and another organisation sees none of it.
// req: FR-TAX-10
// req: FR-TAX-11
// req: AUD-01
// asvs: V8.2.1
// asvs: V8.3.1
func TestAttackInvoiceNowKeepsItsRules(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Attack InvoiceNow Pte Ltd")
	other := books(t, "Attack InvoiceNow Other Pte Ltd")
	api := store(t, "fyniq_api")
	worker := store(t, "fyniq_worker")
	now := time.Date(2027, 2, 3, 9, 0, 0, 0, time.UTC)
	var doc uuid.UUID
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		d, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentStatutoryFiling, Payload: []byte(`{"date":"2027-02-03","key":"eci:2026-12-31","outcome":"filed","filed_on":"2027-02-03","reference":"R"}`)})
		doc = d.ID
		return err
	}); err != nil {
		t.Fatal(err)
	}
	sum := make([]byte, 32)
	var first storage.InvoiceSubmission
	do := func(f func(u storage.InvoiceNow) error) error {
		return api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error { return f(u.InvoiceNow()) })
	}
	move := func(id uuid.UUID, to, ack string) error {
		return do(func(n storage.InvoiceNow) error {
			_, err := n.Move(ctx, id, storage.InvoiceSubmissionMove{Status: to, IRASAck: ack, At: now})
			return err
		})
	}
	if err := do(func(n storage.InvoiceNow) (err error) {
		first, err = n.Queue(ctx, storage.NewInvoiceSubmission{DocumentID: doc, Kind: "peppol", Direction: "sale", Recipient: "0195:SGUEN201912345A", PayloadSHA256: sum, At: now})
		return err
	}); err != nil || first.Status != "queued" || first.Version != 1 {
		t.Fatalf("queued: %+v, %v", first, err)
	}
	// a peppol submission names its recipient, and a recipient is a participant ID
	for name, n := range map[string]storage.NewInvoiceSubmission{
		"a Peppol submission with no recipient": {DocumentID: doc, Kind: "peppol", Direction: "sale", At: now},
		"a recipient that is not an ID":         {DocumentID: doc, Kind: "peppol", Direction: "sale", Recipient: "not an id", At: now},
		"a purchase sent as a sale":             {DocumentID: doc, Kind: "extracted_purchase", Direction: "sale", At: now},
		"an unknown kind":                       {DocumentID: doc, Kind: "carrier_pigeon", Direction: "sale", At: now},
	} {
		if err := do(func(i storage.InvoiceNow) error { _, err := i.Queue(ctx, n); return err }); err == nil {
			t.Errorf("%s was queued", name)
		}
	}
	// the legal path only
	if err := move(first.ID, "acknowledged", "IRAS-1"); storage.CodeOf(err) != storage.CodeInvoiceNow {
		t.Errorf("queued straight to acknowledged: %v", err)
	}
	if err := move(first.ID, "sent", ""); err != nil {
		t.Fatalf("queued to sent: %v", err)
	}
	if err := move(first.ID, "queued", ""); storage.CodeOf(err) != storage.CodeInvoiceNow {
		t.Errorf("sent back to queued: %v", err)
	}
	if err := move(first.ID, "acknowledged", ""); err == nil {
		t.Error("acknowledged with no acknowledgement ID")
	}
	// the worker role moves a submission along, and cannot queue one
	if err := worker.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.InvoiceNow().Move(ctx, first.ID, storage.InvoiceSubmissionMove{Status: "acknowledged", IRASAck: "IRAS-1", At: now})
		return err
	}); err != nil {
		t.Fatalf("the worker moves it to acknowledged: %v", err)
	}
	if err := worker.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.InvoiceNow().Queue(ctx, storage.NewInvoiceSubmission{DocumentID: doc, Kind: "extracted_sale", Direction: "sale", At: now})
		return err
	}); err == nil {
		t.Error("the worker role queued a submission")
	}
	// an acknowledged submission is final
	for _, to := range []string{"queued", "sent", "failed"} {
		if err := move(first.ID, to, ""); storage.CodeOf(err) != storage.CodeInvoiceNow {
			t.Errorf("an acknowledged submission moved to %s: %v", to, err)
		}
	}
	// a correction is the next version, with the document's UUID
	var second storage.InvoiceSubmission
	if err := do(func(n storage.InvoiceNow) (err error) {
		second, err = n.Queue(ctx, storage.NewInvoiceSubmission{DocumentID: doc, Kind: "peppol", Direction: "sale", Recipient: "0195:SGUEN201912345A", PayloadSHA256: sum, At: now})
		return err
	}); err != nil || second.Version != 2 || second.DocumentUUID != first.DocumentUUID {
		t.Fatalf("the next version: %+v, %v", second, err)
	}
	if err := do(func(n storage.InvoiceNow) error {
		ev, err := n.Events(ctx, first.ID)
		if err == nil && len(ev) != 3 { // queued, sent, acknowledged
			t.Errorf("the history holds %d entries, want 3", len(ev))
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// one live exclusion, withdrawn once
	var ex storage.InvoiceExclusion
	if err := do(func(n storage.InvoiceNow) (err error) {
		ex, err = n.Exclude(ctx, doc, "import_permit", "", now)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := do(func(n storage.InvoiceNow) error { _, err := n.Exclude(ctx, doc, "reverse_charge", "", now); return err }); storage.CodeOf(err) != storage.CodeInvoiceNow {
		t.Errorf("a second live exclusion: %v", err)
	}
	if err := do(func(n storage.InvoiceNow) error { _, err := n.Exclude(ctx, uuid.New(), "other", "", now); return err }); err == nil {
		t.Error("an exclusion of another reason with no words, of a document that does not exist")
	}
	if err := do(func(n storage.InvoiceNow) error { _, err := n.WithdrawExclusion(ctx, ex.ID, now); return err }); err != nil {
		t.Fatal(err)
	}
	if err := do(func(n storage.InvoiceNow) error { _, err := n.WithdrawExclusion(ctx, ex.ID, now); return err }); storage.CodeOf(err) != storage.CodeVersion {
		t.Errorf("a second withdrawal: %v", err)
	}
	if err := do(func(n storage.InvoiceNow) error { _, err := n.Exclude(ctx, doc, "reverse_charge", "", now); return err }); err != nil {
		t.Errorf("a new exclusion after the withdrawal: %v", err)
	}

	// a contingency is approved by someone other than its requester: the same actor (the fixture) is refused
	var c storage.InvoiceContingency
	if err := do(func(n storage.InvoiceNow) (err error) {
		c, err = n.RequestContingency(ctx, storage.NewInvoiceContingency{PeriodFrom: now, PeriodTo: now.AddDate(0, 3, 0), Reason: "The access point was down for two days",
			Evidence: "The provider's notice of the outage", At: now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := do(func(n storage.InvoiceNow) error { _, err := n.ApproveContingency(ctx, c.ID, now); return err }); storage.CodeOf(err) != storage.CodeInvoiceNow {
		t.Errorf("a contingency approved by its requester: %v", err)
	}
	if err := do(func(n storage.InvoiceNow) error {
		_, err := n.RequestContingency(ctx, storage.NewInvoiceContingency{PeriodFrom: now, PeriodTo: now.AddDate(0, -1, 0), Reason: "An inverted period is refused", Evidence: "Evidence of the outage", At: now})
		return err
	}); err == nil {
		t.Error("a contingency for a period that ends before it starts")
	}

	// another organisation sees none of it
	if err := api.Do(ctx, other.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		if _, err := u.InvoiceNow().Submission(ctx, first.ID); !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("another organisation reads the submission: %v", err)
		}
		if _, err := u.InvoiceNow().Contingency(ctx, c.ID); !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("another organisation reads the contingency: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
