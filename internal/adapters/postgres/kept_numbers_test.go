//go:build local

package postgres_test

import (
	"context"
	"testing"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A number kept from the workbook (FR-MIG-01, rev 0.259; FR-ORG-04): a document keeps the number it is given, audited;
// one of the sequence's own form moves the sequence past it, so it never issues that number again; one of another form
// leaves the sequence as it was; a number is kept once in its document type; a document numbered already keeps its own.
// req: FR-MIG-01
// req: FR-ORG-04
func TestANumberKeptFromTheWorkbookIsNeverIssuedAgain(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "importer"}})
	b := books(t, "Kept Numbers Pte Ltd")
	s := store(t, "fyniq_api")
	doc := func(u storage.UnitOfWork) storage.Document {
		d, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentJournal, Payload: []byte(`{"memo":"x"}`)})
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		a, other, c := doc(u), doc(u), doc(u)
		if n, err := u.Sequences().Keep(ctx, a.ID, storage.DocumentJournal, "JNL-0107"); err != nil || n != "JNL-0107" {
			t.Fatalf("keeping JNL-0107: %q %v", n, err)
		}
		if n, err := u.Sequences().Keep(ctx, other.ID, storage.DocumentJournal, "J-0001"); err != nil || n != "J-0001" {
			t.Fatalf("keeping J-0001: %q %v", n, err)
		}
		if n, err := u.Sequences().Keep(ctx, a.ID, storage.DocumentJournal, "JNL-0200"); err != nil || n != "JNL-0107" {
			t.Errorf("a numbered document keeps its number: %q %v", n, err)
		}
		if n, err := u.Sequences().Number(ctx, c.ID, storage.DocumentJournal); err != nil || n != "JNL-0108" {
			t.Errorf("the next number issued: %q %v, not JNL-0108", n, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Sequences().Keep(ctx, doc(u).ID, storage.DocumentJournal, "J-0001")
		return err
	})
	if !storageCode(err, storage.CodeSequence) {
		t.Errorf("a number kept twice: %v", err)
	}
}
