//go:build local

package postgres_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// pspConnector installs an organisation connector in org, which the provider records belong to.
func pspConnector(t *testing.T, org uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := store(t, "fyniq_api").Do(context.Background(), org, func(ctx context.Context, u storage.UnitOfWork) error {
		c, err := u.Connectors().InstallOrganisation(ctx, storage.NewConnector{Connector: "stripe", ManifestVersion: "1.0.0", ManifestHash: strings.Repeat("a", 64),
			SecretNames: []string{"api_key"}, Position: 1, At: time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)})
		id = c.ID
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return id
}

// A provider's balance transaction is recorded once whatever the sweep or the event repeats; it is kept as the provider
// reported it and only the payout that settles it is added, once; a payout is kept as reported but for its standing; a
// balance reading is never changed; and another organisation sees none of it.
// req: banking/payment-providers Provider transactions stored
// asvs: V13.2.2
func TestAProvidersRecordsAreKeptAsTheProviderReportedThem(t *testing.T) {
	ctx := context.Background()
	books := porttest.NewBooks(t, store(t, "fyniq_api"), "PSP ledger Pte Ltd")
	conn := pspConnector(t, books.Org)
	worker := store(t, "fyniq_worker")
	at := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	txn := storage.NewPSPTxn{ConnectorID: conn, TxnID: "txn_1", Type: "charge", Category: storage.PSPCatCharge, SourceKind: "charge", SourceID: "ch_1", Currency: "SGD",
		Gross: "100.00", Fee: "3.90", Net: "96.10", ProviderAt: at, SeenAt: at}
	if err := worker.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		if _, isNew, err := u.PSP().RecordTxn(ctx, txn); err != nil || !isNew {
			t.Fatalf("the first record: %v %v", isNew, err)
		}
		if _, isNew, err := u.PSP().RecordTxn(ctx, txn); err != nil || isNew {
			t.Fatalf("a repeat recorded again: %v %v", isNew, err)
		}
		if ok, err := u.PSP().SettleTxn(ctx, conn, "txn_1", "po_1"); err != nil || !ok {
			t.Fatalf("settling: %v %v", ok, err)
		}
		if ok, err := u.PSP().SettleTxn(ctx, conn, "txn_1", "po_1"); err != nil || ok {
			t.Fatalf("settling twice: %v %v", ok, err)
		}
		if ok, err := u.PSP().SettleTxn(ctx, conn, "txn_1", "po_2"); err != nil || ok {
			t.Fatalf("another payout settled it too: %v %v", ok, err)
		}
		po := storage.NewPSPPayout{ConnectorID: conn, PayoutID: "po_1", Currency: "SGD", Amount: "96.10", Status: "pending", Automatic: true, ProviderAt: at, SeenAt: at}
		if _, changed, err := u.PSP().RecordPayout(ctx, po); err != nil || !changed {
			t.Fatalf("the payout: %v %v", changed, err)
		}
		if _, changed, err := u.PSP().RecordPayout(ctx, po); err != nil || changed {
			t.Fatalf("the same payout again: %v %v", changed, err)
		}
		po.Status = "paid"
		if p, changed, err := u.PSP().RecordPayout(ctx, po); err != nil || !changed || p.Status != "paid" {
			t.Fatalf("its standing: %+v %v %v", p, changed, err)
		}
		in, err := u.PSP().TxnsOfPayout(ctx, conn, "po_1")
		if err != nil || len(in) != 1 || in[0].TxnID != "txn_1" || in[0].Net != "96.1000" {
			t.Fatalf("the payout settles %+v, %v", in, err)
		}
		bal, err := u.PSP().RecordBalance(ctx, conn, "SGD", "31.10", at)
		if err != nil || bal.Amount != "31.1000" {
			t.Fatalf("balance: %+v %v", bal, err)
		}
		if _, err := u.PSP().Txn(ctx, conn, "txn_nope"); !errors.Is(err, ports.ErrNotFound) {
			t.Fatalf("an unknown transaction: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// What the provider reported is not rewritten, by anyone: the database refuses it, even to the worker's own statements.
	for name, sql := range map[string]string{
		"a changed amount":      `UPDATE fyniq.psp_txn SET gross = 200, net = 196.10`,
		"a deleted transaction": `DELETE FROM fyniq.psp_txn`,
		"a changed payout":      `UPDATE fyniq.psp_payout SET amount = 1`,
		"a changed balance":     `UPDATE fyniq.psp_balance SET amount = 1`,
	} {
		c := shortConn(t, "fyniq")
		tx, err := c.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `SELECT set_config('app.org_id', $1, true)`, books.Org.String()); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, sql); err == nil {
			t.Errorf("%s was allowed", name)
		}
		_ = tx.Rollback(ctx)
		_ = c.Close(ctx)
	}
	// Another organisation sees none of it.
	other := porttest.NewBooks(t, store(t, "fyniq_api"), "PSP ledger other Pte Ltd")
	if err := worker.Do(ctx, other.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		if _, err := u.PSP().Txn(ctx, conn, "txn_1"); !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("another organisation reads the transaction: %v", err)
		}
		if ps, err := u.PSP().Payouts(ctx, conn, 10); err != nil || len(ps) != 0 {
			t.Errorf("another organisation reads payouts: %v %v", ps, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
