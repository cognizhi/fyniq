package equity

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
)

// The ACRA reconciliation by hand (FR-EQ-26, design D11). The register holds Ann 700,000 ordinary and Bee 300,000
// ordinary and 50,000 preference. An extract that types the same in matches. One that leaves Bee's preference out, puts
// Ann's shares in the preference class, and gives Bee 299,000 ordinary differs on each: Ann 700,000 ordinary against
// none and none against 700,000 preference, Bee 300,000 against 299,000, and 50,000 preference against none. Every
// problem of a bad extract comes back together.
// req: FR-EQ-26
func TestTheAcraReconciliationByHand(t *testing.T) {
	ann, bee := uuid.MustParse("00000000-0000-7000-8000-0000000000a1"), uuid.MustParse("00000000-0000-7000-8000-0000000000b2")
	ordinary, preference := uuid.MustParse("00000000-0000-7000-8000-000000003010"), uuid.MustParse("00000000-0000-7000-8000-000000003020")
	day := func(s string) time.Time { d, _ := time.Parse(time.DateOnly, s); return d }
	move := func(holder, account uuid.UUID, shares int64) Move {
		m := Move{ID: uuid.New(), Key: HoldingKey{Holder: holder, Account: account}, On: day("2025-01-02")}
		m.Shares.SetInt64(shares)
		return m
	}
	table, err := Reader{Moves: []Move{move(ann, ordinary, 700000), move(bee, ordinary, 300000), move(bee, preference, 50000)}}.At(day("2026-10-01"))
	if err != nil {
		t.Fatal(err)
	}
	same := AcraExtract{ProfileOn: day("2026-10-01"), Holdings: []AcraHolding{{ann, ordinary, "700000"}, {bee, ordinary, "300000"}, {bee, preference, "50000"}}}
	if d, err := Reconcile(same, table); err != nil || len(d) != 0 {
		t.Errorf("a matching extract: %+v %v", d, err)
	}
	off := AcraExtract{ProfileOn: day("2026-10-01"), Holdings: []AcraHolding{{ann, preference, "700000"}, {bee, ordinary, "299000"}}}
	d, err := Reconcile(off, table)
	if err != nil {
		t.Fatal(err)
	}
	want := []AcraDifference{{ann, ordinary, "700000", "0"}, {ann, preference, "0", "700000"}, {bee, ordinary, "300000", "299000"}, {bee, preference, "50000", "0"}}
	if len(d) != len(want) {
		t.Fatalf("differences: %+v", d)
	}
	for i := range want {
		if d[i] != want[i] {
			t.Errorf("difference %d: %+v, want %+v", i, d[i], want[i])
		}
	}

	s := ledger.Settings{Accounts: map[uuid.UUID]ledger.Account{ordinary: {ShareCapital: true, Active: true}}}
	bad := AcraExtract{ProfileOn: day("2099-01-01"), Holdings: []AcraHolding{{ann, ordinary, "1.5"}, {ann, ordinary, "10"}, {uuid.Nil, preference, "10"}}}
	fields := map[string]bool{}
	for _, p := range CheckAcra(bad, s, day("2026-10-15")) {
		fields[p.Field] = p.Code == CodeAcra
	}
	for _, f := range []string{"profile_on", "holdings[0].shares", "holdings[1]", "holdings[2].holder_id", "holdings[2].class_account_id"} {
		if !fields[f] {
			t.Errorf("no %s problem: %v", f, fields)
		}
	}
	if probs := CheckAcra(AcraExtract{ProfileOn: day("2026-10-01")}, s, day("2026-10-15")); len(probs) != 1 || probs[0].Field != "holdings" {
		t.Errorf("an extract with no holdings: %+v", probs)
	}
}
