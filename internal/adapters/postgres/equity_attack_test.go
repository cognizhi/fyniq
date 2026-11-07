//go:build local

package postgres_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The equity register's records keep their rules whoever writes them (FR-EQ-15, FR-EQ-16, CMP-PD-07; FY285 to FY287): a
// holder record never holds more than an individual's last 4 identifier characters, a key of its own or a full NRIC; a
// share class belongs to an active share-capital account; and once an account has a recorded class, a share issue of it
// names one of its own classes.
// req: FR-EQ-15
// req: FR-EQ-16
// req: CMP-PD-07
// req: QA-ENG-05
// asvs: V8.2.2
func TestAttackEquityRecordsKeepTheirRules(t *testing.T) {
	ctx := context.Background()
	d := newDirectorBooks(t, "Attack equity records Pte Ltd")
	class := shareCapital(t, d)
	api := store(t, "fyniq_api")
	create := func(typ storage.DocumentType, payload string) error {
		return api.Do(ctx, d.org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Documents().Create(ctx, storage.NewDocument{Type: typ, Payload: []byte(payload)})
			return err
		})
	}
	holder := fmt.Sprintf(`{"date":"2026-11-07","contact_id":%q,"kind":"individual"`, d.director.ID)
	for name, payload := range map[string]string{
		"a full NRIC as the identifier": holder + `,"id_type":"nric","id_last4":"S1234567D"}`,
		"a full NRIC in the address":    holder + `,"address":"NRIC S1234567D, 1 Raffles Place"}`,
		"a key of its own":              holder + `,"id_number":"S1234567D"}`,
		"an entity's last 4":            fmt.Sprintf(`{"date":"2026-11-07","contact_id":%q,"kind":"entity","id_last4":"123A"}`, d.director.ID),
	} {
		if err := create(storage.DocumentHolderRecord, payload); storage.CodeOf(err) != storage.CodeEquityRecord {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := create(storage.DocumentHolderRecord, holder+`,"id_type":"nric","id_last4":"567D"}`); err != nil {
		t.Fatalf("a record with the last 4 characters: %v", err)
	}
	classOf := func(account fmt.Stringer) string {
		return fmt.Sprintf(`{"date":"2026-11-07","name":"Ordinary","kind":"ordinary","account_id":%q,"votes_per_share":"1","liquidation":{},"dividend":{"kind":"none"},"anti_dilution":"none"}`, account)
	}
	if err := create(storage.DocumentShareClass, classOf(d.loan)); storage.CodeOf(err) != storage.CodeEquityRecord {
		t.Errorf("a class on the director's loan account: %v", err)
	}
	if err := api.Do(ctx, d.org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentShareClass, Payload: []byte(classOf(class))})
		if err != nil {
			return err
		}
		return u.Documents().SetStatus(ctx, doc.ID, storage.DocumentPosted)
	}); err != nil {
		t.Fatalf("recording a class on 3010: %v", err)
	}
	wantState(t, "an issue of 3010 naming no class once 3010 has one", rawShareIssue(t, d, class, "100", "100", true), "FY287")
}
