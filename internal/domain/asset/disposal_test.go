package asset

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A disposal keeps what it decided, and reads back as it was; another document, or one that cannot be read, is refused.
// req: FR-FA-03
func TestADisposalReadsBackAsSaved(t *testing.T) {
	bank, inv, clearing := uuid.New(), uuid.New(), uuid.New()
	gross, net := amount(t, "1090.00", sgd), amount(t, "1500.00", sgd)
	d := Disposal{AssetID: uuid.New(), AssetVersion: 3, Date: time.Date(2027, 3, 15, 0, 0, 0, 0, time.UTC), Cost: amount(t, "1800.00", sgd),
		Accumulated: amount(t, "600.00", sgd), Residual: amount(t, "0.00", sgd), Before: amount(t, "1200.00", sgd), Proceeds: ProceedsMoney,
		MoneyAccountID: &bank, Gross: &gross, GSTCode: "SR", GSTRate: "0.09", InvoiceID: &inv, InvoiceLine: 2, InvoiceNet: &net, ClearingAccountID: &clearing,
		AssetAccountID: equipment.ID, DisposalAccountID: uuid.New(), Notes: "Sold to a staff member"}
	b, err := EncodeDisposal(d)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ReadDisposal(storage.Document{Type: storage.DocumentAssetDisposal, Payload: b})
	if err != nil {
		t.Fatal(err)
	}
	if got.AssetID != d.AssetID || got.AssetVersion != 3 || !got.Date.Equal(d.Date) || got.Whole || !got.Cost.Equal(d.Cost) || !got.Accumulated.Equal(d.Accumulated) ||
		!got.Residual.Equal(d.Residual) || !got.Before.Equal(d.Before) || got.Proceeds != ProceedsMoney || *got.MoneyAccountID != bank || !got.Gross.Equal(gross) ||
		got.GSTCode != "SR" || got.GSTRate != "0.09" || *got.InvoiceID != inv || got.InvoiceLine != 2 || !got.InvoiceNet.Equal(net) || *got.ClearingAccountID != clearing ||
		got.AssetAccountID != d.AssetAccountID || got.DisposalAccountID != d.DisposalAccountID || got.Notes != d.Notes {
		t.Errorf("read back %+v, saved %+v", got, d)
	}
	d.Notes = strings.Repeat("x", MaxNotes+1)
	if _, err := EncodeDisposal(d); err == nil {
		t.Errorf("notes too long")
	}
	for _, doc := range []storage.Document{{Type: storage.DocumentFixedAsset, Payload: b}, {Type: storage.DocumentAssetDisposal, Payload: []byte(`{`)},
		{Type: storage.DocumentAssetDisposal, Payload: []byte(`{"date":"2027-03-15"}`)},
		{Type: storage.DocumentAssetDisposal, Payload: []byte(strings.Replace(string(b), `"date":"2027-03-15"`, `"date":"15 March"`, 1))}} {
		if _, err := ReadDisposal(doc); err == nil {
			t.Errorf("%s read", doc.Payload)
		}
	}
}

// A share of an asset's cost takes its accumulated depreciation and residual value pro rata (ALLOC-PRORATA); the whole
// cost takes all of both. Worked by hand: half of a S$3,600 laptop carrying S$1,200 takes S$600; a third of S$1,000.00
// carrying S$100.00 with a residual of S$100.00 takes S$33.33 and S$33.33 (333.33 of 1,000.00).
// req: FR-FA-03
// req: ALLOC-PRORATA
func TestAShareTakesItsPart(t *testing.T) {
	a := laptop(t)
	acc, res, err := Share(a, amount(t, "1800.00", sgd), amount(t, "1200.00", sgd))
	if err != nil || acc.String() != "600.00" || res.String() != "0.00" {
		t.Errorf("half: %s %s %v", acc, res, err)
	}
	if acc, res, err := Share(a, a.Cost, amount(t, "1200.00", sgd)); err != nil || acc.String() != "1200.00" || !res.Equal(a.Residual) {
		t.Errorf("the whole: %s %s %v", acc, res, err)
	}
	a.Cost, a.Residual = amount(t, "1000.00", sgd), amount(t, "100.00", sgd)
	if acc, res, err := Share(a, amount(t, "333.33", sgd), amount(t, "100.00", sgd)); err != nil || acc.String() != "33.33" || res.String() != "33.33" {
		t.Errorf("a third: %s %s %v", acc, res, err)
	}
	for _, cost := range []string{"0.00", "1000.01"} {
		if _, _, err := Share(a, amount(t, cost, sgd), amount(t, "100.00", sgd)); err == nil {
			t.Errorf("a share of %s", cost)
		}
	}
}

// The rule reads the disposal with its number and the asset's name, and only the proceeds it names.
// req: FR-FA-03
func TestADisposalsRule(t *testing.T) {
	bank, clearing := uuid.New(), uuid.New()
	gross, net := amount(t, "2000.00", sgd), amount(t, "1500.00", sgd)
	d := Disposal{Date: time.Date(2027, 10, 15, 0, 0, 0, 0, time.UTC), Whole: true, Cost: amount(t, "3600.00", sgd), Accumulated: amount(t, "1200.00", sgd),
		Proceeds: ProceedsMoney, MoneyAccountID: &bank, Gross: &gross, ClearingAccountID: &clearing, InvoiceNet: &net, AssetAccountID: equipment.ID}
	doc := storage.Document{ID: uuid.New(), Version: 1, Number: "DSP-0001"}
	r := d.Rule(doc, "FA-0001 MacBook Pro 14", sgd)
	if r.Memo != "DSP-0001: FA-0001 MacBook Pro 14, sold" || r.MoneyAccountID == nil || !r.Gross.Equal(gross) || r.ClearingAccountID != nil ||
		!r.InvoiceNet.IsZero() || r.ID != doc.ID || r.Version != 1 {
		t.Errorf("a sale: %+v", r)
	}
	d.Proceeds, d.Whole = ProceedsInvoice, false
	if r := d.Rule(doc, "FA-0001", sgd); r.Memo != "DSP-0001: FA-0001, part sold" || r.MoneyAccountID != nil || r.ClearingAccountID == nil || !r.InvoiceNet.Equal(net) ||
		!r.Gross.IsZero() {
		t.Errorf("a part invoiced: %+v", r)
	}
	d.Proceeds, d.Whole = ProceedsNone, true
	if r := d.Rule(doc, "FA-0001", sgd); r.Memo != "DSP-0001: FA-0001, written off" || r.MoneyAccountID != nil || r.ClearingAccountID != nil {
		t.Errorf("a write-off: %+v", r)
	}
}
