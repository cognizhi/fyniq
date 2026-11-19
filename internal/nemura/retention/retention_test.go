package retention

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func d(y int, m time.Month, day int) time.Time { return time.Date(y, m, day, 0, 0, 0, 0, time.UTC) }

// A company's period may be longer than the statutory minimum and never shorter; a record younger than its period is never due, and a
// company policy cannot shorten the law (NM-SEC-04, NM-PACK-04).
// req: NM-SEC-04
func TestARetentionPeriodNeverGoesBelowTheMinimum(t *testing.T) {
	p := Policy{RecordType: "employment-records", MinMonths: 24, KeepMonths: 12}
	var r *Refusal
	if err := p.Check(); !errors.As(err, &r) || r.Code != CodeBelowMinimum {
		t.Fatalf("a company period of 12 months over a 24-month minimum: %v", err)
	}
	if _, err := p.Plan(d(2099, 1, 1), nil, nil); err == nil {
		t.Error("a plan was made under a policy below the minimum")
	}
	longer := Policy{RecordType: "employment-records", MinMonths: 24, KeepMonths: 36}
	if got := longer.Cutoff(d(2099, 6, 1)); !got.Equal(d(2096, 6, 1)) {
		t.Errorf("a 36-month period cuts off at %s", got)
	}
	if got := (Policy{RecordType: "x", MinMonths: 24}).Cutoff(d(2099, 6, 1)); !got.Equal(d(2097, 6, 1)) {
		t.Errorf("with no company period the minimum applies: %s", got)
	}
	young := Record{ID: uuid.New(), From: d(2098, 1, 1)}
	old := Record{ID: uuid.New(), From: d(2090, 1, 1)}
	plan, err := longer.Plan(d(2099, 6, 1), []Record{young, old}, nil)
	if err != nil || len(plan.Due) != 1 || plan.Due[0].ID != old.ID || len(plan.Held) != 0 {
		t.Errorf("a record inside its period is not due: %+v, %v", plan, err)
	}
}

// A legal hold keeps a record: one person's, or every record of the type, from its day until it is released; the plan lists what it keeps.
// req: NM-SEC-05
func TestALegalHoldKeepsWhatItNames(t *testing.T) {
	p := Policy{RecordType: "medical", MinMonths: 12}
	alice, bob := uuid.New(), uuid.New()
	records := []Record{{ID: uuid.New(), Subject: alice, From: d(2090, 1, 1)}, {ID: uuid.New(), Subject: bob, From: d(2090, 1, 1)}}
	now := d(2099, 1, 1)
	held := func(holds ...Hold) (due, kept int) {
		plan, err := p.Plan(now, records, holds)
		if err != nil {
			t.Fatal(err)
		}
		return len(plan.Due), len(plan.Held)
	}
	if due, kept := held(); due != 2 || kept != 0 {
		t.Errorf("no hold: %d due, %d held", due, kept)
	}
	if due, kept := held(Hold{RecordType: "medical", Subject: &alice, From: d(2098, 1, 1)}); due != 1 || kept != 1 {
		t.Errorf("a hold on Alice: %d due, %d held", due, kept)
	}
	if due, kept := held(Hold{RecordType: "medical", From: d(2098, 1, 1)}); due != 0 || kept != 2 {
		t.Errorf("a hold on the type: %d due, %d held", due, kept)
	}
	released := d(2098, 6, 1)
	if due, kept := held(Hold{RecordType: "medical", From: d(2098, 1, 1), Until: &released}); due != 2 || kept != 0 {
		t.Errorf("a released hold: %d due, %d held", due, kept)
	}
	if due, kept := held(Hold{RecordType: "disciplinary", From: d(2098, 1, 1)}); due != 2 || kept != 0 {
		t.Errorf("a hold on another type: %d due, %d held", due, kept)
	}
}

// A report not reviewed by its review date is overdue (✖), so "reviewed" never means kept for ever; the proof of a deletion is a
// fingerprint that differs by record and content and never holds the content (NM-SEC-05).
// req: NM-SEC-05
func TestAnUnreviewedReportGoesOverdueAndProofIsAFingerprint(t *testing.T) {
	made := d(2099, 3, 1)
	by := ReviewBy(made)
	if !by.Equal(d(2099, 3, 31)) {
		t.Errorf("the review date is %s", by)
	}
	if Overdue(by, d(2099, 3, 31), false) || !Overdue(by, d(2099, 4, 1), false) || Overdue(by, d(2099, 6, 1), true) {
		t.Error("overdue is: not reviewed, and past its date")
	}
	id := uuid.New()
	a, b := Fingerprint("medical", id, []byte("a note")), Fingerprint("medical", id, []byte("another note"))
	if len(a) != 32 || string(a) == string(b) || string(a) == string(Fingerprint("disciplinary", id, []byte("a note"))) {
		t.Error("the fingerprint is not a SHA-256 that depends on the type, id and content")
	}
}
