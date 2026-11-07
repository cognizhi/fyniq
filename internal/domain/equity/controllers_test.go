package equity

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The controller test by hand (FR-EQ-25, design D10, G4-101). Of 10,000 ordinary shares, Bee holds 2,000 (20%) from 1
// Mar 2027; a transfer of 501 on 8 Mar takes her to 2,501, 25.01%: a controller from 8 Mar, where 2,500 (exactly 25%)
// would not be. She sells 101 on 15 Mar (24.00%) and buys 200 back on 22 Mar (26.00%): her run starts again on 22 Mar.
// Cat holds 1,000 preference shares of 10 votes each, 9.09% of the shares but 50.00% of the 20,000 votes: a controller
// on the votes alone. Dan holds 500 shares and a right to appoint a majority of the board: named for a person to weigh,
// not by the cap table. Eve has consent rights and no shares: named too.
// req: FR-EQ-25
// req: G4-101
func TestTheControllerTestByHand(t *testing.T) {
	ann, bee, cat, dan, eve := uuid.MustParse("00000000-0000-7000-8000-0000000000a1"), uuid.MustParse("00000000-0000-7000-8000-0000000000b2"),
		uuid.MustParse("00000000-0000-7000-8000-0000000000c3"), uuid.MustParse("00000000-0000-7000-8000-0000000000d4"),
		uuid.MustParse("00000000-0000-7000-8000-0000000000e5")
	ordinary, preference := uuid.MustParse("00000000-0000-7000-8000-000000003010"), uuid.MustParse("00000000-0000-7000-8000-000000003020")
	prefClass := uuid.MustParse("00000000-0000-7000-8000-0000000000f2")
	day := func(s string) time.Time { d, _ := time.Parse(time.DateOnly, s); return d }
	move := func(holder, account, class uuid.UUID, shares int64, on string) Move {
		m := Move{ID: uuid.New(), Key: HoldingKey{Holder: holder, Account: account, Class: class}, On: day(on)}
		m.Shares.SetInt64(shares)
		return m
	}
	moves := []Move{
		move(ann, ordinary, uuid.Nil, 7500, "2027-03-01"), move(bee, ordinary, uuid.Nil, 2000, "2027-03-01"), move(dan, ordinary, uuid.Nil, 500, "2027-03-01"),
		move(ann, ordinary, uuid.Nil, -501, "2027-03-08"), move(bee, ordinary, uuid.Nil, 501, "2027-03-08"),
		move(bee, ordinary, uuid.Nil, -101, "2027-03-15"), move(ann, ordinary, uuid.Nil, 101, "2027-03-15"),
		move(ann, ordinary, uuid.Nil, -200, "2027-03-22"), move(bee, ordinary, uuid.Nil, 200, "2027-03-22"),
	}
	r := Reader{Moves: moves}
	rights := map[uuid.UUID]InvestorRights{dan: {BoardMajority: true}, eve: {Consents: []string{"Issuing new shares"}}, ann: {BoardSeats: 1}}

	on8 := func(d string) map[uuid.UUID]ControllerFinding {
		t.Helper()
		got, err := ControllerTest(day(d), r, rights)
		if err != nil {
			t.Fatal(err)
		}
		out := map[uuid.UUID]ControllerFinding{}
		for _, f := range got {
			out[f.HolderID] = f
		}
		return out
	}
	if f := on8("2027-03-07"); f[bee].Over() || !f[ann].Shares || !f[ann].Since.Equal(day("2027-03-01")) {
		t.Errorf("at 20%% Bee is not a controller, Ann at 75%% is since 1 Mar: %+v", f)
	}
	f := on8("2027-03-10")
	if b := f[bee]; !b.Shares || !b.Since.Equal(day("2027-03-08")) || b.Ownership != "25.01" || b.Rights {
		t.Errorf("Bee at 2,501 of 10,000: %+v", b)
	}
	if d := f[dan]; d.Over() || !d.Rights {
		t.Errorf("Dan's board majority names him for a person to weigh: %+v", d)
	}
	if e, ok := f[eve]; !ok || e.Over() || !e.Rights {
		t.Errorf("Eve's consent rights name her with no shares: %+v", e)
	}
	if a := f[ann]; a.Rights {
		t.Errorf("one board seat is not a board majority: %+v", a)
	}
	if b, ok := on8("2027-03-16")[bee]; ok {
		t.Errorf("at 24.00%% Bee is no longer a controller: %+v", b)
	}
	if b := on8("2027-03-31")[bee]; !b.Shares || !b.Since.Equal(day("2027-03-22")) || b.Ownership != "26.00" {
		t.Errorf("back over 25%% on 22 Mar: %+v", b)
	}

	exactly := Reader{Moves: []Move{move(ann, ordinary, uuid.Nil, 7500, "2027-03-01"), move(bee, ordinary, uuid.Nil, 2500, "2027-03-01")}}
	got, err := ControllerTest(day("2027-03-02"), exactly, nil)
	if err != nil || len(got) != 1 || got[0].HolderID != ann {
		t.Errorf("exactly 25%% is not more than 25%%: %+v %v", got, err)
	}

	pref := Recorded[ShareClass]{Doc: storage.Document{ID: prefClass, Status: storage.DocumentPosted}, Value: ShareClass{VotesPerShare: "10"},
		RecordedAt: day("2027-01-01")}
	voting := Reader{Moves: []Move{move(ann, ordinary, uuid.Nil, 10000, "2027-03-01"), move(cat, preference, prefClass, 1000, "2027-03-01")},
		classes: map[uuid.UUID][]Recorded[ShareClass]{prefClass: {pref}}}
	got, err = ControllerTest(day("2027-03-02"), voting, nil)
	if err != nil || len(got) != 2 {
		t.Fatalf("Ann and Cat: %+v %v", got, err)
	}
	for _, x := range got {
		if x.HolderID == cat && (x.Shares || !x.Votes || x.Ownership != "9.09" || x.Voting != "50.00") {
			t.Errorf("Cat controls on the votes alone: %+v", x)
		}
	}
}

// The exempt-private-company test by hand (FR-EQ-25, FR-EQ-11, design D10): two individuals are exempt; an entity
// joining ends it; a 21st member ends it; a member with no holder record leaves it undecided, unless something else
// already decides.
// req: FR-EQ-25
// req: FR-EQ-11
func TestTheExemptTestByHand(t *testing.T) {
	table := func(n int) (CapTable, []uuid.UUID) {
		var t CapTable
		var ids []uuid.UUID
		for i := 0; i < n; i++ {
			h := CapHolder{Holder: uuid.New()}
			h.Issued.SetInt64(100)
			t.Holders = append(t.Holders, h)
			ids = append(ids, h.Holder)
		}
		return t, ids
	}
	two, ids := table(2)
	kinds := map[uuid.UUID]string{ids[0]: "individual", ids[1]: "individual"}
	if f := ExemptTest(two, kinds); !f.Decided || !f.Exempt || f.Members != 2 {
		t.Errorf("two individuals: %+v", f)
	}
	kinds[ids[1]] = "entity"
	if f := ExemptTest(two, kinds); !f.Decided || f.Exempt || len(f.Entities) != 1 {
		t.Errorf("an entity member: %+v", f)
	}
	delete(kinds, ids[1])
	if f := ExemptTest(two, kinds); f.Decided || len(f.Unknown) != 1 {
		t.Errorf("a member with no record: %+v", f)
	}
	many, all := table(21)
	k21 := map[uuid.UUID]string{}
	for _, id := range all[:20] {
		k21[id] = "individual"
	}
	if f := ExemptTest(many, k21); !f.Decided || f.Exempt || f.Members != 21 {
		t.Errorf("a 21st member, even with no record: %+v", f)
	}
	gone, gids := table(1)
	gone.Holders[0].Issued.SetInt64(0)
	if f := ExemptTest(gone, map[uuid.UUID]string{gids[0]: "entity"}); f.Members != 0 || !f.Exempt {
		t.Errorf("a holder of no shares is no member: %+v", f)
	}
	if got := BusinessDaysAfter(time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC), 2); got.Format(time.DateOnly) != "2026-11-10" {
		t.Errorf("2 business days after Friday 6 Nov 2026: %s, want Tuesday 10 Nov", got.Format(time.DateOnly))
	}
}
