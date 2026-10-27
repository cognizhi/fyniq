package asset

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

var (
	sgd       = money.MustLookup("SGD")
	usd       = money.MustLookup("USD")
	equipment = storage.Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000001510"), Code: "1510", Type: storage.Asset, Active: true,
		AccountAttributes: storage.AccountAttributes{ReportGroup: "Property, plant & equipment"}}
	booksStart = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
)

func amount(t testing.TB, s string, cur money.Currency) money.Money {
	t.Helper()
	m, err := money.Parse(s, cur)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// laptop is a S$3,600 laptop bought on 14 March 2026 from a bill's first line, over 36 months from March.
func laptop(t testing.TB) Asset {
	march := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	return Asset{Name: "MacBook Pro 14", AccountID: equipment.ID, Source: &Source{DocumentID: uuid.New(), Version: 1, Line: 1},
		PurchasedOn: time.Date(2026, 3, 14, 0, 0, 0, 0, time.UTC), Cost: amount(t, "3600.00", sgd), Residual: money.Zero(sgd), LifeMonths: 36,
		Method: StraightLine, Start: march, Carried: money.Zero(sgd), From: march}
}

func codes(p ledger.Problems) []string {
	var out []string
	for _, x := range p {
		out = append(out, x.Field+":"+x.Code)
	}
	return out
}

// An asset version keeps what it was saved with, and reads back as it was.
// req: FR-FA-01
func TestAnAssetReadsBackAsSaved(t *testing.T) {
	a := laptop(t)
	a.Notes = "Serial C02XG0FDH7JY"
	d := uuid.New()
	a.DisposalID = &d
	b, err := Encode(a)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Read(storage.Document{ID: uuid.New(), Type: storage.DocumentFixedAsset, Payload: b})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != a.Name || *got.Source != *a.Source || !got.PurchasedOn.Equal(a.PurchasedOn) || !got.Cost.Equal(a.Cost) || !got.Residual.Equal(a.Residual) ||
		got.LifeMonths != 36 || got.Method != StraightLine || !got.Start.Equal(a.Start) || !got.Carried.Equal(a.Carried) || !got.From.Equal(a.From) ||
		got.Notes != a.Notes || *got.DisposalID != d || got.Disposed {
		t.Errorf("read back %+v, saved %+v", got, a)
	}
	if !strings.Contains(string(b), `"date":"2026-03-14"`) {
		t.Errorf("a list sorts by the purchase date: %s", b)
	}
	if !got.End().Equal(time.Date(2029, 2, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("36 months from March 2026 end in %v, not February 2029", got.End())
	}
	// An opening asset has no source.
	a.Source = nil
	b, _ = Encode(a)
	if got, _ := Read(storage.Document{Type: storage.DocumentFixedAsset, Payload: b}); got.Source != nil {
		t.Errorf("an opening asset read back with a source")
	}
	// Another document, or one that cannot be read, is refused with its code.
	for _, c := range []struct {
		doc  storage.Document
		code string
	}{
		{storage.Document{Type: storage.DocumentBill, Payload: b}, CodeNotAsset},
		{storage.Document{Type: storage.DocumentFixedAsset, Payload: []byte(`{`)}, CodeBadContent},
		{storage.Document{Type: storage.DocumentFixedAsset, Payload: []byte(`{"name":"x"}`)}, CodeBadContent},
		{storage.Document{Type: storage.DocumentFixedAsset, Payload: []byte(strings.Replace(string(b), `"purchased_on":"2026-03-14"`, `"purchased_on":"14/3"`, 1))}, CodeBadContent},
		{storage.Document{Type: storage.DocumentFixedAsset, Payload: []byte(strings.Replace(string(b), `"start":"2026-03"`, `"start":"March"`, 1))}, CodeBadContent},
		{storage.Document{Type: storage.DocumentFixedAsset, Payload: []byte(strings.Replace(string(b), `"from":"2026-03"`, `"from":""`, 1))}, CodeBadContent},
	} {
		_, err := Read(c.doc)
		if e, ok := err.(*Error); !ok || e.Code != c.code || e.Error() == "" {
			t.Errorf("%s: %v, want %s", c.doc.Payload, err, c.code)
		}
	}
}

// An asset's rules, every problem at once (ENG-23): a life of zero, a residual above the cost and a start before the
// purchase month are refused; an asset bought before the books start needs them, and carries at most its cost less its
// residual; only an active asset account of the register takes an asset.
// req: FR-FA-01
// req: FR-FA-02
func TestAnAssetMeetsItsRules(t *testing.T) {
	if p := Check(laptop(t), &equipment, sgd, booksStart); len(p) != 0 {
		t.Fatalf("a laptop: %v", p)
	}
	a := laptop(t)
	a.Name, a.LifeMonths, a.Residual, a.Start, a.Method = " ", 0, amount(t, "3600.01", sgd), time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), "reducing_balance"
	want := []string{"name:" + CodeTooLong, "life_months:" + CodeLife, "method:" + CodeMethod, "residual:" + CodeResidual, "start:" + CodeStart}
	if got := codes(Check(a, &equipment, sgd, booksStart)); !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	a = laptop(t)
	a.Name, a.Notes, a.LifeMonths, a.Residual = strings.Repeat("x", MaxName+1), strings.Repeat("x", MaxNotes+1), MaxLifeMonths+1, amount(t, "-1.00", sgd)
	want = []string{"name:" + CodeTooLong, "notes:" + CodeTooLong, "life_months:" + CodeLife, "residual:" + CodeResidual}
	if got := codes(Check(a, &equipment, sgd, booksStart)); !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	// The edges are allowed: a residual of the whole cost, a life of the most months, a start later than the purchase.
	a = laptop(t)
	a.Name, a.Notes, a.Residual, a.LifeMonths, a.Start = strings.Repeat("x", MaxName), strings.Repeat("x", MaxNotes), a.Cost, MaxLifeMonths,
		time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	if p := Check(a, &equipment, sgd, booksStart); len(p) != 0 {
		t.Errorf("the edges: %v", p)
	}
	a.LifeMonths = 1
	if p := Check(a, &equipment, sgd, booksStart); len(p) != 0 {
		t.Errorf("a life of one month: %v", p)
	}

	// The account: none, an expense account, an inactive one, a group heading.
	for _, acc := range []*storage.Account{nil, {Code: "6100", Active: true}, {Code: "1510", Active: false, AccountAttributes: equipment.AccountAttributes},
		{Code: "1500", Active: true, Group: true, AccountAttributes: equipment.AccountAttributes}} {
		if got := codes(Check(laptop(t), acc, sgd, booksStart)); !slices.Equal(got, []string{"account_id:" + CodeAccount}) {
			t.Errorf("account %+v: %v", acc, got)
		}
	}
	intangible := storage.Account{Code: "1540", Active: true, AccountAttributes: storage.AccountAttributes{ReportGroup: "Intangible assets"}}
	if !InRegister(intangible) || InRegister(storage.Account{Code: "1250", Active: true, AccountAttributes: storage.AccountAttributes{ReportGroup: "Other current assets"}}) {
		t.Errorf("the register's groups: Property, plant & equipment and Intangible assets")
	}

	// Amounts in another currency, and a cost of nothing.
	a = laptop(t)
	a.Residual = amount(t, "1.00", usd)
	if got := codes(Check(a, &equipment, sgd, booksStart)); !slices.Equal(got, []string{"residual:" + CodeResidual}) {
		t.Errorf("a residual in USD: %v", got)
	}
	a = laptop(t)
	a.Cost = money.Zero(sgd)
	if got := codes(Check(a, &equipment, sgd, booksStart)); !slices.Equal(got, []string{"cost:" + CodeOpening}) {
		t.Errorf("a cost of nothing: %v", got)
	}
}

// An asset bought before the books start carries its opening accumulated depreciation from the books start (FR-FA-02):
// it needs a books start, was bought before it, and carries at most its cost less its residual value.
// req: FR-FA-02
func TestAnOpeningAssetCarriesItsAccumulatedDepreciation(t *testing.T) {
	opening := func(bought string, carried string) Asset {
		a := laptop(t)
		a.Source = nil
		a.PurchasedOn, _ = time.Parse(time.DateOnly, bought)
		a.Start = Month(a.PurchasedOn)
		a.From = booksStart
		a.Carried = amount(t, carried, sgd)
		a.Residual = amount(t, "600.00", sgd)
		return a
	}
	if p := Check(opening("2025-03-14", "1000.00"), &equipment, sgd, booksStart); len(p) != 0 {
		t.Errorf("an opening asset: %v", p)
	}
	if p := Check(opening("2025-03-14", "3000.00"), &equipment, sgd, booksStart); len(p) != 0 {
		t.Errorf("carrying all but the residual: %v", p)
	}
	for _, c := range []struct {
		a     Asset
		start time.Time
		want  []string
	}{
		{opening("2025-03-14", "1000.00"), time.Time{}, []string{"purchased_on:" + CodeOpening}},
		{opening("2026-01-01", "0.00"), booksStart, []string{"purchased_on:" + CodeOpening}},
		{opening("2025-03-14", "3000.01"), booksStart, []string{"opening_accumulated:" + CodeOpening}},
		{opening("2025-03-14", "-0.01"), booksStart, []string{"opening_accumulated:" + CodeOpening}},
	} {
		if got := codes(Check(c.a, &equipment, sgd, c.start)); !slices.Equal(got, c.want) {
			t.Errorf("%s carrying %s: %v, want %v", c.a.PurchasedOn.Format(time.DateOnly), c.a.Carried, got, c.want)
		}
	}
	// A sourced asset carries nothing of the opening balances' rules.
	a := laptop(t)
	if p := Check(a, &equipment, sgd, time.Time{}); len(p) != 0 {
		t.Errorf("a sourced asset without a books start: %v", p)
	}
}
