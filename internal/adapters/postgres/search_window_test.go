//go:build local

package postgres_test

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A search for text alone reads the newest documents first (p1-operations-dr task 13.13): a page of them answers it, and
// text found only in documents older than the 1,000 newest of its types is found by the trigram indexes, with the same
// rule. Of 1,001 journals the two oldest say "Harbourfront deposit", and the rest "Office supplies".
// req: FR-UX-02
// req: NFR-PERF-01
func TestASearchBeyondTheNewestDocumentsFindsTheOlder(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "accountant"}})
	s := store(t, "fyniq_api")
	b := porttest.NewBooks(t, s, "Search window Pte Ltd")
	var old []uuid.UUID
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		for i := range 1001 {
			memo, date := fmt.Sprintf("Office supplies %d", i), "2026-10-31"
			if i < 2 {
				memo, date = fmt.Sprintf("Harbourfront deposit %d", i), fmt.Sprintf("2026-10-0%d", i+1)
			}
			doc, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentJournal,
				Payload: fmt.Appendf(nil, `{"date":%q,"memo":%q,"lines":[]}`, date, memo)})
			if err != nil {
				return err
			}
			if i < 2 {
				old = append(old, doc.ID)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	search := func(text string) []storage.DocumentHit {
		t.Helper()
		var hits []storage.DocumentHit
		if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			hits, err = u.Documents().Search(ctx, storage.DocumentSearch{Types: []storage.DocumentType{storage.DocumentJournal}, Text: text, Limit: 5})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return hits
	}
	var got []uuid.UUID
	for _, h := range search("harbourfront DEPOSIT") {
		got = append(got, h.ID)
	}
	if !slices.Equal(got, []uuid.UUID{old[1], old[0]}) {
		t.Errorf("the older journals: %v; want %v, newest first", got, []uuid.UUID{old[1], old[0]})
	}
	page := search("office supplies")
	if len(page) != 5 {
		t.Fatalf("a page of the newest: %d hits", len(page))
	}
	for _, h := range page {
		if h.Date != "2026-10-31" {
			t.Errorf("%s dated %s in the newest page", h.ID, h.Date)
		}
	}
}
