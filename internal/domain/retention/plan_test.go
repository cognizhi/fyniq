package retention

import (
	"testing"
	"time"
)

func d(s string) time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return t
}

// The year that may go (AUD-10): the oldest year still kept, once its records' retention has passed (the end of the
// year plus 7 years, or more if the setting lengthened it), every period it holds is closed and no record of it is on
// legal hold. With books from April to March, a first line on 15 May 2020 is in the year from 1 April 2020; it may go
// from 1 April 2028, and not a day before; not while a period of it is open; not when retention is 10 years until
// 1 April 2031; not while a record of 31 March 2021 is held, though a hold on 1 April 2021 is the next year's.
// req: AUD-10
func TestTheNextYearToPurge(t *testing.T) {
	sg := time.UTC
	closed := []Period{{StartsOn: d("2020-04-01"), EndsOn: d("2021-03-31")}, {StartsOn: d("2021-04-01"), EndsOn: d("2021-04-30"), Open: true}}
	y, ok := NextYear(4, 7, d("2020-05-15"), closed, nil, d("2028-04-01"), sg)
	if !ok || !y.StartsOn.Equal(d("2020-04-01")) || !y.Boundary.Equal(d("2021-04-01")) {
		t.Errorf("on 1 April 2028: %+v %v", y, ok)
	}
	if _, ok := NextYear(4, 7, d("2020-05-15"), closed, nil, d("2028-03-31"), sg); ok {
		t.Error("a day early")
	}
	if _, ok := NextYear(4, 7, d("2020-05-15"), append(closed, Period{StartsOn: d("2020-09-01"), EndsOn: d("2020-09-30"), Open: true}), nil, d("2030-01-01"), sg); ok {
		t.Error("a period of the year is open")
	}
	if _, ok := NextYear(4, 10, d("2020-05-15"), closed, nil, d("2031-03-31"), sg); ok {
		t.Error("retention of 10 years, a day early")
	}
	if _, ok := NextYear(4, 3, d("2020-05-15"), closed, nil, d("2025-01-01"), sg); ok {
		t.Error("a retention below 7 years is 7")
	}
	if _, ok := NextYear(4, 7, time.Time{}, closed, nil, d("2040-01-01"), sg); ok {
		t.Error("no line at all")
	}
	if _, ok := NextYear(4, 7, d("2020-05-15"), closed, []time.Time{d("2021-03-31")}, d("2030-01-01"), sg); ok {
		t.Error("a record of the year is on legal hold")
	}
	if _, ok := NextYear(4, 7, d("2020-05-15"), closed, []time.Time{d("2021-04-01")}, d("2030-01-01"), sg); !ok {
		t.Error("a hold on a record of the next year keeps this one")
	}
}

// The kept set (AUD-10, rev 0.238): every entry from the boundary on, those a register or the bank's reconciliation
// names, and every entry linked by a reversal to a kept one, however far the chain of reversals goes.
// req: AUD-10
func TestTheEntriesThePurgeKeeps(t *testing.T) {
	entries := []EntryRef{
		{ID: "a", Date: d("2020-05-01")},                // purged
		{ID: "b", Date: d("2020-06-01")},                // named: a share issue's
		{ID: "c", Date: d("2020-07-01"), Reverses: "b"}, // reverses a kept entry
		{ID: "d", Date: d("2020-08-01")},                // reversed after the boundary
		{ID: "e", Date: d("2021-05-01"), Reverses: "d"},
		{ID: "f", Date: d("2020-09-01")}, // reversed by g, which is reversed by h after the boundary? no: a chain f←g, g kept by h
		{ID: "g", Date: d("2020-10-01"), Reverses: "f"},
		{ID: "h", Date: d("2021-06-01"), Reverses: "g"},
		{ID: "i", Date: d("2020-11-01"), Reverses: "a2"},
		{ID: "a2", Date: d("2020-10-15")},
	}
	kept := Kept(entries, map[string]bool{"b": true}, d("2021-04-01"))
	for id, want := range map[string]bool{"a": false, "b": true, "c": true, "d": true, "e": true, "f": true, "g": true, "h": true, "i": false, "a2": false} {
		if kept[id] != want {
			t.Errorf("%s kept %v, want %v", id, kept[id], want)
		}
	}
}
