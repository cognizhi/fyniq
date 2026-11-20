//go:build local

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A payment link (FR-AR-13, FR-PSP-02; p2-bank-feeds-payments): the API alone records one and closes it, the database holds one
// live link per invoice and lets a link only close, once; the worker alone records the payments a provider's charges made under
// it, once each, and can change nothing else; a payment is never changed or deleted; and another organisation sees none of it.
// req: FR-AR-13
// req: FR-PSP-02
// asvs: V4.2.1
func TestAPaymentLinkIsRecordedOnceLiveAndItsPaymentsByTheWorkerOnly(t *testing.T) {
	ctx := context.Background()
	api, worker := store(t, "fyniq_api"), store(t, "fyniq_worker")
	books := porttest.NewBooks(t, api, "Payment link Pte Ltd")
	other := porttest.NewBooks(t, api, "Another link Pte Ltd")
	conn := pspConnector(t, books.Org)
	at := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	var invoice uuid.UUID
	if err := api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		d, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentInvoice, Payload: []byte(`{}`)})
		invoice = d.ID
		return err
	}); err != nil {
		t.Fatal(err)
	}
	n := storage.NewPaymentLink{ConnectorID: conn, InvoiceID: invoice, ProviderRequestID: "pr_1", URL: "https://pay.example.com/pr_1", Reference: "INV-1",
		Currency: "SGD", Amount: "109.00", At: at}

	// The worker records no link, and a link needs an https URL.
	if err := worker.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.PaymentLinks().Create(ctx, n)
		return err
	}); err == nil {
		t.Fatal("the worker recorded a payment link")
	}
	bad := n
	bad.URL = "http://insecure.example.com/pr_1"
	if err := api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.PaymentLinks().Create(ctx, bad)
		return err
	}); err == nil {
		t.Fatal("a link with no https URL was recorded")
	}
	var link storage.PaymentLink
	if err := api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		link, err = u.PaymentLinks().Create(ctx, n)
		return err
	}); err != nil || link.Status != storage.LinkLive || link.Amount != "109.0000" {
		t.Fatalf("the link: %+v, %v", link, err)
	}
	// One live link per invoice, in the database.
	second := n
	second.ProviderRequestID = "pr_2"
	err := api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.PaymentLinks().Create(ctx, second)
		return err
	})
	if err == nil {
		t.Fatal("a second live link was recorded")
	}
	// The worker records the charge made under it, once; the API cannot.
	p := storage.NewPaymentLinkPayment{LinkID: link.ID, ConnectorID: conn, ProviderPaymentID: "ch_1", Currency: "SGD", Amount: "109.00", PaidAt: at, SeenAt: at}
	if err := api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, _, err := u.PaymentLinks().RecordPayment(ctx, p)
		return err
	}); err == nil {
		t.Fatal("the API recorded a payment under a link")
	}
	if err := worker.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		if _, isNew, err := u.PaymentLinks().RecordPayment(ctx, p); err != nil || !isNew {
			return errors.Join(err, errors.New("the first record was not new"))
		}
		if _, isNew, err := u.PaymentLinks().RecordPayment(ctx, p); err != nil || isNew {
			return errors.Join(err, errors.New("a repeat recorded again"))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := worker.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.PaymentLinks().Close(ctx, link.ID, storage.LinkPaid, "", at, link.Version)
		return err
	}); err == nil {
		t.Fatal("the worker closed a link")
	}
	// A link closes once, with its reason; a closed link never reopens or closes again, and the invoice can have a new one.
	var closed storage.PaymentLink
	if err := api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		closed, err = u.PaymentLinks().Close(ctx, link.ID, storage.LinkCancelled, "The customer asked for a new link", at, link.Version)
		return err
	}); err != nil || closed.Status != storage.LinkCancelled || closed.ClosedAt == nil || closed.Version != 2 {
		t.Fatalf("closing: %+v, %v", closed, err)
	}
	if err := api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.PaymentLinks().Close(ctx, link.ID, storage.LinkPaid, "", at, closed.Version)
		return err
	}); err == nil {
		t.Fatal("a closed link was closed again")
	}
	if err := api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.PaymentLinks().Create(ctx, second)
		return err
	}); err != nil {
		t.Fatalf("a new link after the cancellation: %v", err)
	}
	if err := api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		pay, err := u.PaymentLinks().PaymentsOf(ctx, link.ID)
		if err == nil && (len(pay) != 1 || pay[0].ProviderPaymentID != "ch_1") {
			t.Errorf("the payments: %+v", pay)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// Another organisation sees none of it.
	if err := api.Do(ctx, other.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		if _, err := u.PaymentLinks().Get(ctx, link.ID); !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("another organisation read the link: %v", err)
		}
		if _, err := u.PaymentLinks().PaymentByCharge(ctx, conn, "ch_1"); !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("another organisation read the payment: %v", err)
		}
		ls, err := u.PaymentLinks().OfInvoice(ctx, invoice)
		if err != nil || len(ls) != 0 {
			t.Errorf("another organisation lists %d links of the invoice: %v", len(ls), err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
