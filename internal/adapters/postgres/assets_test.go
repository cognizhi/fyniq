//go:build local

package postgres_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// FR-FA-01 and QA-ENG-05 (p1-fixed-assets task 1.1): the database keeps an asset's source to one line of a posted bill
// or bank line, on the line's own asset account, which its entry debits, once (FY235, fixed_asset_source_line_once);
// it refuses a second asset on a line, a line of another account, a line of a draft or voided bill, an account outside
// the register's groups, and any change or deletion of a source.
// req: FR-FA-01
// req: QA-ENG-05
// asvs: V2.2.1
func TestAssetSourcesKeepTheirLine(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "accountant"}})
	s := store(t, "fyniq_api")
	b := newPurchasesBooks(t, ctx, s, "Assets Pte Ltd")
	var equipment, advances storage.Account
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if equipment, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "1510", Name: "Computer equipment", Type: storage.Asset,
			AccountAttributes: storage.AccountAttributes{ReportGroup: "Property, plant & equipment"}}); err != nil {
			return err
		}
		advances, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "1250", Name: "Staff advances", Type: storage.Asset,
			AccountAttributes: storage.AccountAttributes{ReportGroup: "Other current assets"}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	bills := journal.Bills{Store: s}
	draft := func(ref string, on storage.Account) bill.Draft {
		return bill.Draft{SupplierID: b.Supplier.ID, SupplierRef: ref, Date: porttest.Day(10, 14), Currency: money.MustLookup("SGD"), Lines: []bill.Line{
			{AccountID: on.ID, Description: "MacBook Pro 14", Net: sgdOf(t, "1800.00"), GSTCode: "ZP", GST: sgdOf(t, "0.00")},
			{AccountID: b.Rent.ID, Description: "Desk rent", Net: sgdOf(t, "100.00"), GSTCode: "ZP", GST: sgdOf(t, "0.00")},
		}}
	}
	posted := func(ref string, on storage.Account) storage.Document {
		t.Helper()
		doc, err := bills.Create(ctx, b.Org, draft(ref, on))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := bills.Post(ctx, b.Org, doc.ID); err != nil {
			t.Fatal(err)
		}
		return doc
	}
	laptop := posted("LP-1", equipment)
	assets := journal.FixedAssets{Store: s}
	register := func(source uuid.UUID, line int) (storage.Document, error) {
		var doc storage.Document
		err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			doc, err = assets.Register(ctx, u, journal.AssetDraft{SourceID: &source, SourceLine: line, LifeMonths: 36})
			return err
		})
		return doc, err
	}
	first, err := register(laptop.ID, 1)
	if err != nil {
		t.Fatalf("register the laptop: %v", err)
	}

	// Straight at the store, as a bug in the application might: each refused by the database.
	add := func(src storage.AssetSource) error {
		return s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentFixedAsset, Payload: []byte(`{}`)})
			if err != nil {
				return err
			}
			src.AssetID = doc.ID
			return u.AssetSources().Add(ctx, src)
		})
	}
	draftBill, err := bills.Create(ctx, b.Org, draft("LP-2", equipment))
	if err != nil {
		t.Fatal(err)
	}
	misfiled := posted("LP-3", advances)
	for _, c := range []struct {
		what string
		src  storage.AssetSource
		code string
	}{
		{"a second asset on the line", storage.AssetSource{SourceID: laptop.ID, SourceVersion: 1, Line: 1, AccountID: equipment.ID}, storage.CodeDuplicate},
		{"the rent line, named as on the asset account", storage.AssetSource{SourceID: laptop.ID, SourceVersion: 1, Line: 2, AccountID: equipment.ID}, storage.CodeAsset},
		{"the rent line on its expense account", storage.AssetSource{SourceID: laptop.ID, SourceVersion: 1, Line: 2, AccountID: b.Rent.ID}, storage.CodeAsset},
		{"a line beyond the bill's", storage.AssetSource{SourceID: laptop.ID, SourceVersion: 1, Line: 3, AccountID: equipment.ID}, storage.CodeAsset},
		{"a draft bill", storage.AssetSource{SourceID: draftBill.ID, SourceVersion: 1, Line: 1, AccountID: equipment.ID}, storage.CodeAsset},
		{"an account outside the register", storage.AssetSource{SourceID: misfiled.ID, SourceVersion: 1, Line: 1, AccountID: advances.ID}, storage.CodeAsset},
	} {
		if err := add(c.src); storage.CodeOf(err) != c.code {
			t.Errorf("%s: %v, want %s", c.what, err, c.code)
		}
	}
	// A document that is not an asset takes no source.
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		return u.AssetSources().Add(ctx, storage.AssetSource{AssetID: misfiled.ID, SourceID: laptop.ID, SourceVersion: 1, Line: 1, AccountID: equipment.ID})
	}); storage.CodeOf(err) != storage.CodeAsset {
		t.Errorf("a bill as an asset: %v", err)
	}
	// A voided bill's line is registered no more.
	voided := posted("LP-4", equipment)
	if _, err := bills.Void(ctx, b.Org, voided.ID, porttest.Day(10, 20), "Wrong supplier"); err != nil {
		t.Fatal(err)
	}
	if err := add(storage.AssetSource{SourceID: voided.ID, SourceVersion: 1, Line: 1, AccountID: equipment.ID}); storage.CodeOf(err) != storage.CodeAsset {
		t.Errorf("a voided bill: %v", err)
	}

	// The source never changes or goes: no role may, and the guard refuses even an owner of the table.
	err = asOrg(ctx, conn(t, "fyniq_api"), b.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE fyniq.fixed_asset_source SET line = 2 WHERE asset_id = $1", first.ID)
		return err
	})
	wantState(t, "the API changes a source", err, "42501")
	err = asOrg(ctx, conn(t, "fyniq"), b.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "DELETE FROM fyniq.fixed_asset_source WHERE asset_id = $1", first.ID)
		return err
	})
	wantState(t, "the owner deletes a source", err, "FY235")

	// What was registered stands: one asset, from line 1, on 1510.
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		got, err := u.AssetSources().OfDocument(ctx, laptop.ID)
		if err != nil {
			return err
		}
		all, err := u.AssetSources().List(ctx)
		if err != nil {
			return err
		}
		of, err := u.AssetSources().Of(ctx, first.ID)
		if len(got) != 1 || len(all) != 1 || got[0] != of || of.Line != 1 || of.AccountID != equipment.ID || of.SourceVersion != 1 {
			t.Errorf("sources: %+v %+v %+v", got, all, of)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
