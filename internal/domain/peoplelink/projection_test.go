package peoplelink

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

var (
	weiJie  = uuid.MustParse("0192a000-0000-7000-8000-0000000000e1")
	oldMgr  = uuid.MustParse("0192a000-0000-7000-8000-0000000000e2")
	newMgr  = uuid.MustParse("0192a000-0000-7000-8000-0000000000e3")
	holdID  = uuid.MustParse("0192a000-0000-7000-8000-0000000000e4")
	bankID  = uuid.MustParse("0192a000-0000-7000-8000-0000000000e5")
	someDay = time.Date(2026, time.November, 2, 3, 0, 0, 0, time.UTC)
)

func upsert(manager uuid.UUID) PersonUpserted {
	return PersonUpserted{PersonID: weiJie, DisplayName: "Wei Jie Ong", WorkEmail: "weijie@example.com", Status: "active", StartedOn: "2026-01-05", LineManager: &manager}
}

// A late older version of a person's record is ignored and does not bring back the old manager (FR-PPL-15, NM-INT-10).
func TestALateOlderPersonVersionDoesNotSetTheManagerBack(t *testing.T) {
	var cur Person
	cur, ignored, err := ApplyPerson(cur, upsert(oldMgr), 1, someDay)
	if err != nil || ignored || cur.LineManager != oldMgr {
		t.Fatalf("version 1: %+v %v %v", cur, ignored, err)
	}
	cur = Existing(cur)
	cur, ignored, err = ApplyPerson(cur, upsert(newMgr), 2, someDay)
	if err != nil || ignored || cur.LineManager != newMgr || cur.Version != 2 {
		t.Fatalf("version 2: %+v %v %v", cur, ignored, err)
	}
	cur = Existing(cur)
	late, ignored, err := ApplyPerson(cur, upsert(oldMgr), 1, someDay)
	if err != nil || !ignored || late.LineManager != newMgr || late.Version != 2 {
		t.Fatalf("late version 1: %+v %v %v", late, ignored, err)
	}
	if _, ignored, _ := ApplyPerson(cur, upsert(oldMgr), 2, someDay); !ignored { // the same version again, from another event
		t.Error("an equal version is not applied twice")
	}
}

func TestATombstoneKeepsThePersonAndMarksTheRemoval(t *testing.T) {
	cur, _, _ := ApplyPerson(Person{}, upsert(oldMgr), 1, someDay)
	cur = Existing(cur)
	gone, ignored, err := ApplyPerson(cur, PersonUpserted{PersonID: weiJie, Deleted: true}, 2, someDay)
	if err != nil || ignored || !gone.Removed || gone.DisplayName != "Wei Jie Ong" || gone.Version != 2 {
		t.Fatalf("%+v %v %v", gone, ignored, err)
	}
	back, _, err := ApplyPerson(Existing(gone), upsert(oldMgr), 3, someDay)
	if err != nil || back.Removed {
		t.Errorf("a later record brings the person back: %+v %v", back, err)
	}
}

func TestAPersonEventThatIsNotWellFormedIsRefused(t *testing.T) {
	self := upsert(weiJie)
	bad := []PersonUpserted{{PersonID: weiJie}, {DisplayName: "x"}, self, func() PersonUpserted { e := upsert(oldMgr); e.StartedOn = "05/01/2026"; return e }()}
	for i, e := range bad {
		var r *Refusal
		if _, _, err := ApplyPerson(Person{}, e, 1, someDay); !errors.As(err, &r) || r.Code != CodeMalformed {
			t.Errorf("case %d: %v", i, err)
		}
	}
}

// The hold's version and the bank details' version move on their own: an older one is ignored, a release needs evidence (FR-PPL-13, FR-PPL-14).
func TestHoldAndBankVersionsMoveApart(t *testing.T) {
	cur, _, _ := ApplyPerson(Person{}, upsert(oldMgr), 5, someDay)
	cur = Existing(cur)
	held, ignored, err := ApplyTaxHold(cur, TaxHoldChanged{PersonID: weiJie, HoldID: holdID, EffectiveFrom: "2026-10-01", Status: "held"}, 1, someDay)
	if err != nil || ignored || !held.Held || held.HoldVersion != 1 || held.Version != 5 {
		t.Fatalf("%+v %v %v", held, ignored, err)
	}
	held = Existing(held)
	var r *Refusal
	if _, _, err := ApplyTaxHold(held, TaxHoldChanged{PersonID: weiJie, HoldID: holdID, EffectiveFrom: "2026-10-01", Status: "released"}, 2, someDay); !errors.As(err, &r) {
		t.Errorf("a release without evidence: %v", err)
	}
	freed, _, err := ApplyTaxHold(held, TaxHoldChanged{PersonID: weiJie, HoldID: holdID, EffectiveFrom: "2026-10-01", Status: "released", EvidenceRef: "evidence-1"}, 2, someDay)
	if err != nil || freed.Held || freed.HoldVersion != 2 {
		t.Fatalf("%+v %v", freed, err)
	}
	freed = Existing(freed)
	if again, ignored, _ := ApplyTaxHold(freed, TaxHoldChanged{PersonID: weiJie, HoldID: holdID, EffectiveFrom: "2026-10-01", Status: "held"}, 1, someDay); !ignored || again.Held {
		t.Errorf("a late older hold must not re-hold a released person: %+v", again)
	}
	b, ignored, err := ApplyBankDetails(freed, BankDetailsChanged{PersonID: weiJie, RecordID: bankID}, 3, someDay)
	if err != nil || ignored || b.BankVersion != 3 || b.BankRecordID != bankID || b.HoldVersion != 2 {
		t.Fatalf("%+v %v %v", b, ignored, err)
	}
	if _, ignored, _ := ApplyBankDetails(Existing(b), BankDetailsChanged{PersonID: weiJie, RecordID: bankID}, 2, someDay); !ignored {
		t.Error("an older bank-details version is ignored")
	}
}
