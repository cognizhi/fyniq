package erasure

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/nemura/datacat"
)

func day(s string) time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return t
}

// The first day an erasure may be asked for is the later of the employment records' period from leaving and the payroll records' five years from the end of the year of the last pay; a company
// that keeps employment records longer keeps them longer, never shorter.
// part of NM-SEC-08
func TestEligibilityIsTheLaterOfTheEmploymentAndPayrollPeriods(t *testing.T) {
	left := day("2090-01-31")
	if got := EligibleOn(left, nil, 0); !got.Equal(day("2091-01-31")) {
		t.Errorf("never paid: one year after leaving: %v", got)
	}
	last := day("2089-12-01")
	if got := EligibleOn(left, &last, 0); !got.Equal(day("2095-01-01")) { // the pay of 2089 is kept to the end of 2094
		t.Errorf("paid: five years from the end of the year of the last pay: %v", got)
	}
	if got := EligibleOn(left, nil, 36); !got.Equal(day("2093-01-31")) {
		t.Errorf("a company's longer period: %v", got)
	}
	if got := EligibleOn(left, nil, 3); !got.Equal(day("2091-01-31")) {
		t.Errorf("a shorter company period never shortens the minimum: %v", got)
	}
	var r *Refusal
	if err := Check(nil, day("2099-01-01"), day("2099-01-01")); !errors.As(err, &r) || r.Code != CodeNotLeft {
		t.Errorf("not left: %v", err)
	}
	if err := Check(&left, day("2095-01-01"), day("2094-12-31")); !errors.As(err, &r) || r.Code != CodeNotYet {
		t.Errorf("too early: %v", err)
	}
	if err := Check(&left, day("2095-01-01"), day("2095-01-01")); err != nil {
		t.Errorf("on the day: %v", err)
	}
}

// A register entry is applied only if its signature verifies and it names the organisation and the place it is filed under; one changed after it was signed is not.
// part of NM-SEC-08
// asvs: V11.4.1, V16.2.5
func TestARegisterEntryIsAppliedOnlyWhenItVerifiesAndIsInItsPlace(t *testing.T) {
	org, person := uuid.New(), uuid.New()
	e := Entry{Schema: Schema, Organisation: org, Person: person, Order: uuid.New(), ErasedOn: "2099-03-01", EligibleOn: "2091-01-31"}
	msg, _ := e.Canonical()
	v := fakeVerifier{msg: msg}
	good := signedJSON(e, "k1", []byte("sig"))
	if _, err := Open(good, v, org, Key(org, person)); err != nil {
		t.Fatalf("a good entry: %v", err)
	}
	for name, c := range map[string]struct {
		raw []byte
		org uuid.UUID
		key string
	}{
		"another organisation": {good, uuid.New(), Key(org, person)},
		"another place":        {good, org, Key(org, uuid.New())},
		"a changed person":     {signedJSON(Entry{Schema: Schema, Organisation: org, Person: uuid.New(), ErasedOn: "2099-03-01"}, "k1", []byte("sig")), org, Key(org, person)},
		"not an entry":         {[]byte("{"), org, Key(org, person)},
	} {
		if _, err := Open(c.raw, v, c.org, c.key); err == nil {
			t.Errorf("%s was applied", name)
		}
	}
}

// Every class of the data inventory has a bucket, no bucket names a class that is not there, and what an erasure cannot reach is listed so it is not forgotten: the owner decides how those are shredded.
// part of NM-SEC-08
func TestEveryClassOfThePersonalDataInventoryHasAnErasureBucket(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range datacat.Categories {
		seen[c.Key] = true
		if _, ok := Classes[c.Key]; !ok {
			t.Errorf("the class %q has no erasure bucket: add it to erasure.Classes", c.Key)
		}
	}
	for k := range Classes {
		if !seen[k] {
			t.Errorf("erasure.Classes names %q, which is no class of the inventory", k)
		}
	}
}
