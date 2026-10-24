//go:build local

package postgres_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/statement"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// FR-AR-08 (p1-sales task 7.2): a statement made is a document of its own, numbered ST-0001, saying what it said; it
// posts nothing, so no entry names one (FY204), and it is never voided, whoever tries (FY204).
// req: FR-AR-08
func TestAStatementStaysAsItWasMade(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "clerk"}})
	s := store(t, "fyniq_api")
	b := newSalesBooks(t, ctx, s, "Statements Pte Ltd")
	sgd := money.MustLookup("SGD")
	api := journal.Invoices{Store: s}
	inv, err := api.Create(ctx, b.Org, invoice.Draft{CustomerID: b.Customer.ID, Date: porttest.Day(10, 1), Currency: sgd,
		Lines: []invoice.Line{invoiceLine(t, b, "1", "100.00", "", "SR", "SGD")}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := api.Post(ctx, b.Org, inv.ID); err != nil {
		t.Fatal(err)
	}
	var doc storage.Document
	var st statement.Statement
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		doc, st, err = statement.Make(ctx, u, sgd, b.Customer.ID, sgd, porttest.Day(10, 1), porttest.Day(10, 31), porttest.Day(11, 1))
		return err
	}); err != nil || doc.Number != "ST-0001" || doc.Status != storage.DocumentPosted || !st.AddsUp() || len(st.Lines) != 1 {
		t.Fatalf("a statement made: %+v %+v %v", doc, st, err)
	}
	sent, err := statement.Read(doc)
	if err != nil || !sent.Closing.Equal(st.Closing) || len(sent.Lines) != 1 || sent.Lines[0].Number != "INV-0001" {
		t.Fatalf("as it was made: %+v %v", sent, err)
	}

	err = s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		e := porttest.Sale("statement-entry", porttest.Day(10, 20), b.Books, "1.00")
		e.Source = storage.Source{Type: "statement", ID: doc.ID.String(), Version: 1}
		_, err := u.Ledger().Append(ctx, e)
		return err
	})
	if storage.CodeOf(err) != storage.CodeStatement {
		t.Errorf("an entry naming a statement: %v", err)
	}
	for _, role := range []string{"fyniq_api", "fyniq_migrator"} {
		err := asOrg(ctx, conn(t, role), b.Org, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "UPDATE fyniq.document SET status = 'reversed' WHERE id = $1", doc.ID)
			return err
		})
		wantState(t, role+": a statement voided", err, "FY204")
	}
}
