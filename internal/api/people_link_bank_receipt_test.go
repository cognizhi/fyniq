package api

import (
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/peoplelink"
)

// Fyniq answers a bank-details event it ignored as no newer than what it holds with the version and the record it holds, so Nemura can tell a restore that lost a confirmed change from a repeat or a late
// older event: it carries an ID and a number, never an account, and only for a bank-details event whose answer is ignored_older (FR-PPL-19; PRD-002 NM-INT-13).
// part of FR-PPL-19 (the checkpoint and the privacy deletions are tested elsewhere)
// asvs: V8.2.1, V13.2.1
func TestAnIgnoredBankDetailsEventIsAnsweredWithTheVersionAndRecordFyniqHolds(t *testing.T) {
	w := payrollWorld(t)
	person, first, second := uuid.NewString(), uuid.NewString(), uuid.NewString()
	says := func(version int, record string) response {
		return w.nemuraSays(peoplelink.TypeBankDetailsChange, "bank_details", person, version, map[string]any{"person_id": person, "bank_detail_id": record}, nil)
	}
	if r := says(2, first); r.Status != 200 || r.str("outcome") != "applied" || r.Body["held_bank_version"] != nil || r.Body["held_bank_detail_id"] != nil {
		t.Fatalf("an applied version says nothing of what Fyniq holds: %d %s", r.Status, r.Raw)
	}
	// A restore: Nemura numbers another record 2 and sends it. Fyniq holds version 2 for the first, and says so.
	r := says(2, second)
	if r.Status != 200 || r.str("outcome") != "ignored_older" || r.Body["held_bank_version"] != float64(2) || r.str("held_bank_detail_id") != first {
		t.Fatalf("an ignored version names the version and record Fyniq holds: %d %s", r.Status, r.Raw)
	}
	// A late version 1 also names what is held now, which is Nemura's own record at 2 when it is not a restore.
	if r := says(1, uuid.NewString()); r.str("outcome") != "ignored_older" || r.Body["held_bank_version"] != float64(2) || r.str("held_bank_detail_id") != first {
		t.Errorf("a late older version: %s", r.Raw)
	}
	// A person's event that is ignored does not carry it: it is about bank details alone.
	if r := w.nemuraSays(peoplelink.TypePersonUpserted, "person", person, 3, map[string]any{"person_id": person, "display_name": "Wei", "status": "active", "started_on": "2026-01-05", "deleted": false}, nil); r.str("outcome") != "applied" {
		t.Fatalf("a person event: %s", r.Raw)
	}
	if r := w.nemuraSays(peoplelink.TypePersonUpserted, "person", person, 2, map[string]any{"person_id": person, "display_name": "Wei", "status": "active", "started_on": "2026-01-05", "deleted": false}, nil); r.str("outcome") != "ignored_older" || r.Body["held_bank_version"] != nil || r.Body["held_bank_detail_id"] != nil {
		t.Errorf("an ignored person event carries bank detail: %s", r.Raw)
	}
}
