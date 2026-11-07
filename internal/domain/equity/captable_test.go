package equity

import (
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"
)

// The cap table from movements alone, each figure worked by hand (FR-EQ-20, Appendix G.3).
// req: FR-EQ-20
// req: RND-DISPLAY
func TestTheCapTableComesFromTheMovements(t *testing.T) {
	founder, investor, safe := uuid.MustParse("00000000-0000-7000-8000-00000000000f"), uuid.MustParse("00000000-0000-7000-8000-00000000001a"),
		uuid.MustParse("00000000-0000-7000-8000-00000000002b")
	ordAccount, prefAccount := uuid.MustParse("00000000-0000-7000-8000-000000003010"), uuid.MustParse("00000000-0000-7000-8000-000000003020")
	ordinary, preference := uuid.MustParse("00000000-0000-7000-8000-0000000000c1"), uuid.MustParse("00000000-0000-7000-8000-0000000000c2")
	day := func(s string) time.Time { d, _ := time.Parse(time.DateOnly, s); return d }
	move := func(holder, account, class uuid.UUID, shares, on string) Move {
		m := Move{ID: uuid.New(), Key: HoldingKey{Holder: holder, Account: account, Class: class}, On: day(on)}
		if _, _, err := m.Shares.SetString(shares); err != nil {
			t.Fatal(err)
		}
		return m
	}
	votes := func(k HoldingKey) (apd.Decimal, bool, error) {
		var v apd.Decimal
		switch k.Class {
		case ordinary:
			v.SetInt64(1)
		case preference:
			v.SetInt64(0)
		default:
			v.SetInt64(1)
			return v, true, nil
		}
		return v, false, nil
	}
	type line struct{ issued, votes, ownership, voting string }
	check := func(name string, table CapTable, want map[HoldingKey]line, holders map[uuid.UUID]line) {
		t.Helper()
		if len(table.Lines) != len(want) {
			t.Fatalf("%s: %d lines, want %d: %+v", name, len(table.Lines), len(want), table.Lines)
		}
		for _, l := range table.Lines {
			w, ok := want[l.Key]
			got := line{l.Issued.Text('f'), l.Votes.Text('f'), l.Ownership, l.Voting}
			if !ok || got != w {
				t.Errorf("%s: line %v = %+v, want %+v", name, l.Key, got, w)
			}
			if l.FullyDiluted.Cmp(&l.Issued) != 0 || l.Diluted != l.Ownership {
				t.Errorf("%s: fully diluted %s (%s) differs from issued with nothing dilutive", name, l.FullyDiluted.Text('f'), l.Diluted)
			}
		}
		for _, h := range table.Holders {
			got := line{h.Issued.Text('f'), h.Votes.Text('f'), h.Ownership, h.Voting}
			if w, ok := holders[h.Holder]; !ok || got != w {
				t.Errorf("%s: holder %v = %+v, want %+v", name, h.Holder, got, w)
			}
		}
		if len(table.Holders) != len(holders) {
			t.Errorf("%s: %d holders, want %d", name, len(table.Holders), len(holders))
		}
	}

	// G.3's round: 1,000,000 founder shares; the investor's 200,000 and the SAFE's 52,083, both effective 12 Mar.
	round := []Move{
		move(founder, ordAccount, ordinary, "1000000", "2026-01-05"),
		move(investor, ordAccount, ordinary, "200000", "2026-03-12"),
		move(safe, ordAccount, ordinary, "52083", "2026-03-12"),
	}
	before, err := TableAt(day("2026-03-11"), round, votes)
	if err != nil {
		t.Fatal(err)
	}
	f := HoldingKey{Holder: founder, Account: ordAccount, Class: ordinary}
	check("the day before", before, map[HoldingKey]line{f: {"1000000", "1000000", "100.00", "100.00"}},
		map[uuid.UUID]line{founder: {"1000000", "1000000", "100.00", "100.00"}})
	after, err := TableAt(day("2026-03-12"), round, votes)
	if err != nil {
		t.Fatal(err)
	}
	i, s := HoldingKey{Holder: investor, Account: ordAccount, Class: ordinary}, HoldingKey{Holder: safe, Account: ordAccount, Class: ordinary}
	check("the round", after, map[HoldingKey]line{
		f: {"1000000", "1000000", "79.87", "79.87"}, i: {"200000", "200000", "15.97", "15.97"}, s: {"52083", "52083", "4.16", "4.16"},
	}, map[uuid.UUID]line{founder: {"1000000", "1000000", "79.87", "79.87"}, investor: {"200000", "200000", "15.97", "15.97"},
		safe: {"52083", "52083", "4.16", "4.16"}})
	if after.Issued.Text('f') != "1252083" || after.Votes.Text('f') != "1252083" {
		t.Errorf("totals %s issued, %s votes; want 1252083", after.Issued.Text('f'), after.Votes.Text('f'))
	}

	// Preference shares without votes, then a transfer: 950,000 of 1,200,000 is 79.1666…; the investor's 50,000 and
	// 200,000 are 4.1666… and 16.6666…, so the lines show 100.01 and are not forced to 100.00; their holder total,
	// 250,000, is 20.8333…. The 1,000,000 votes are all ordinary.
	pref := []Move{
		move(founder, ordAccount, ordinary, "1000000", "2026-01-05"),
		move(investor, prefAccount, preference, "200000", "2026-03-12"),
		move(founder, ordAccount, ordinary, "-50000", "2026-04-01"),
		move(investor, ordAccount, ordinary, "50000", "2026-04-01"),
	}
	moved, err := TableAt(day("2026-04-01"), pref, votes)
	if err != nil {
		t.Fatal(err)
	}
	p := HoldingKey{Holder: investor, Account: prefAccount, Class: preference}
	check("a transfer", moved, map[HoldingKey]line{
		f: {"950000", "950000", "79.17", "95.00"}, i: {"50000", "50000", "4.17", "5.00"}, p: {"200000", "0", "16.67", "0.00"},
	}, map[uuid.UUID]line{founder: {"950000", "950000", "79.17", "95.00"}, investor: {"250000", "50000", "20.83", "5.00"}})
	for _, l := range moved.Lines {
		if l.Key == f && len(l.Moves) != 2 {
			t.Errorf("the founder's line lists %d movements, want 2", len(l.Moves))
		}
	}

	// Shares transferred away leave no line; shares of an account with no recorded class count one vote a share, assumed.
	gone, err := TableAt(day("2026-05-01"), append(round, move(safe, ordAccount, ordinary, "-52083", "2026-05-01"),
		move(investor, ordAccount, ordinary, "52083", "2026-05-01"), move(investor, prefAccount, uuid.Nil, "10", "2026-05-01")), votes)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range gone.Lines {
		if l.Key.Holder == safe {
			t.Errorf("a holder with no shares left keeps a line: %+v", l)
		}
		if l.Key.Class == uuid.Nil && (!l.VotesAssumed || l.Votes.Text('f') != "10") {
			t.Errorf("a class-less holding's votes = %s, assumed %v; want 10, assumed", l.Votes.Text('f'), l.VotesAssumed)
		}
	}

	// Nothing held: no line, no percentage.
	empty, err := TableAt(day("2025-12-31"), round, votes)
	if err != nil || len(empty.Lines) != 0 || len(empty.Holders) != 0 || !empty.Issued.IsZero() {
		t.Errorf("before any shares: %+v, %v", empty, err)
	}
}

// The cap table fully diluted with the option pool (FR-EQ-19, FR-EQ-20, design D9), worked by hand: 1,000,000 founder
// shares, a pool of 100,000 from 2 Jan 2025 and Priya's grant of 48,000 options on 15 Oct 2025. On 15 Oct 2025 Priya
// has a line of her own with no shares; the pool leaves 52,000, and fully diluted the company is 1,100,000: the founder
// 90.91%, Priya 4.36%. A day before the grant counts the whole pool as unallocated.
// req: FR-EQ-19
// req: FR-EQ-20
func TestTheCapTableCountsThePool(t *testing.T) {
	founder, priya := uuid.MustParse("00000000-0000-7000-8000-00000000000f"), uuid.MustParse("00000000-0000-7000-8000-00000000003c")
	account, class := uuid.MustParse("00000000-0000-7000-8000-000000003010"), uuid.MustParse("00000000-0000-7000-8000-0000000000c1")
	day := func(s string) time.Time { d, _ := time.Parse(time.DateOnly, s); return d }
	shares := Move{ID: uuid.New(), Key: HoldingKey{Holder: founder, Account: account, Class: class}, On: day("2025-01-02")}
	shares.Shares.SetInt64(1000000)
	book := OptionBook{
		Pools:  []PoolResolution{{Size: "100000", ClassAccountID: account, ResolvedOn: day("2025-01-02")}},
		Grants: []Grant{{HolderID: priya, Number: "48000", GrantedOn: day("2025-10-15"), ClassAccountID: account}},
	}
	grant := Option{Key: HoldingKey{Holder: priya, Account: account, Class: class}, On: day("2025-10-15")}
	grant.Number.SetInt64(48000)
	d := Dilution{Options: []Option{grant}, Pool: func(on time.Time) (apd.Decimal, error) {
		p, _, err := book.At(on)
		return p.Unallocated, err
	}}
	one := func(HoldingKey) (apd.Decimal, bool, error) { return *apd.New(1, 0), false, nil }

	got, err := DilutedAt(day("2025-10-15"), []Move{shares}, one, d)
	if err != nil {
		t.Fatal(err)
	}
	if got.Issued.Text('f') != "1000000" || got.Options.Text('f') != "48000" || got.Unallocated.Text('f') != "52000" ||
		got.FullyDiluted.Text('f') != "1100000" || len(got.Lines) != 2 || len(got.Holders) != 2 {
		t.Fatalf("on the grant date: %+v", got)
	}
	for _, l := range got.Lines {
		want := map[uuid.UUID][4]string{founder: {"1000000", "1000000", "100.00", "90.91"}, priya: {"0", "48000", "0.00", "4.36"}}[l.Key.Holder]
		if g := [4]string{l.Issued.Text('f'), l.FullyDiluted.Text('f'), l.Ownership, l.Diluted}; g != want {
			t.Errorf("line of %v: %v, want %v", l.Key.Holder, g, want)
		}
	}
	before, err := DilutedAt(day("2025-10-14"), []Move{shares}, one, d)
	if err != nil {
		t.Fatal(err)
	}
	if before.Unallocated.Text('f') != "100000" || before.FullyDiluted.Text('f') != "1100000" || len(before.Lines) != 1 || before.Lines[0].Diluted != "90.91" {
		t.Errorf("the day before the grant: %+v", before)
	}
	short, err := (OptionBook{Pools: book.Pools, Grants: append(book.Grants, Grant{Number: "60000", GrantedOn: day("2025-06-01")})}).ShortFrom(day("2025-06-01"))
	if err != nil || short == nil || !short.Day.Equal(day("2025-10-15")) || short.Granted.Text('f') != "108000" {
		t.Errorf("an earlier grant of 60,000 shorts 15 Oct: %+v %v", short, err)
	}
}
