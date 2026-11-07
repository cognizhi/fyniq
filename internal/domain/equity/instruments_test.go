package equity

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
)

// An instrument's terms (FR-EQ-18, design D6): G4-044's post-money SAFE fits; each term outside its rules is named, all
// at once, and a template's form must be the instrument's.
// req: FR-EQ-18
func TestAnInstrumentKeepsItsTerms(t *testing.T) {
	sgd, err := money.Lookup("SGD")
	if err != nil {
		t.Fatal(err)
	}
	usd, err := money.Lookup("USD")
	if err != nil {
		t.Fatal(err)
	}
	capital, sales := uuid.New(), uuid.New()
	s := ledger.Settings{Base: sgd, Accounts: map[uuid.UUID]ledger.Account{capital: {ID: capital, Code: "3010", Active: true, ShareCapital: true},
		sales: {ID: sales, Code: "4000", Active: true}}}
	amount := func(text string, c money.Currency) money.Money {
		m, err := money.Parse(text, c)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	signed := time.Date(2026, 6, 18, 0, 0, 0, 0, time.UTC)
	safe := Instrument{Form: FormPostMoneySAFE, HolderID: uuid.New(), Amount: amount("125000.00", sgd), SignedOn: signed, ValuationCap: "2500000",
		Trigger: "priced_round", ClassAccountID: capital, Denominator: "post_money_capitalisation", PriceDecimals: 4, Template: "yc-sg-post-money-cap@1"}
	if p := CheckInstrument(safe, s); len(p) > 0 {
		t.Fatalf("G4-044's SAFE: %v", p)
	}
	maturity := signed.AddDate(0, 0, -1)
	for name, c := range map[string]struct {
		change func(i *Instrument)
		fields string
	}{
		"a capped SAFE with no cap":  {func(i *Instrument) { i.ValuationCap = "" }, "valuation_cap"},
		"a discount of 100":          {func(i *Instrument) { i.Form, i.Template, i.ValuationCap, i.Discount = FormDiscountSAFE, "", "", "100" }, "discount"},
		"an MFN SAFE with no clause": {func(i *Instrument) { i.Form, i.Template, i.ValuationCap = FormMFNSAFE, "", "" }, "mfn"},
		"a note with no interest or maturity": {func(i *Instrument) { i.Form, i.Template, i.ValuationCap = FormConvertibleNote, "", "" },
			"interest_rate,maturity_on"},
		"interest on a SAFE": {func(i *Instrument) { i.InterestRate = "8" }, "interest_rate"},
		"a maturity before it was signed": {func(i *Instrument) {
			i.Form, i.Template, i.InterestRate, i.MaturityOn = FormConvertibleNote, "", "8", &maturity
		}, "maturity_on"},
		"another currency and nothing": {func(i *Instrument) { i.Amount = amount("0.00", usd) }, "amount"},
		"a class on sales":             {func(i *Instrument) { i.ClassAccountID = sales }, "class_account_id"},
		"the terms' count undescribed": {func(i *Instrument) { i.Denominator = "as_terms" }, "denominator_note"},
		"a precision of 9 decimals":    {func(i *Instrument) { i.PriceDecimals = 9 }, "price_decimals"},
		"a template of another form":   {func(i *Instrument) { i.Form, i.ValuationCap = FormPreMoneySAFE, "2500000" }, "template"},
		"an unknown template":          {func(i *Instrument) { i.Template = "seed-note@1" }, "template"},
		"nothing at all": {func(i *Instrument) { *i = Instrument{} },
			"form,holder_id,amount,signed_on,trigger,class_account_id,denominator"},
	} {
		i := safe
		c.change(&i)
		if got := fields(CheckInstrument(i, s)); got != c.fields {
			t.Errorf("%s: fields %q, want %q", name, got, c.fields)
		}
	}
	if TemplateOf("yc-sg-post-money-cap@1") == nil || TemplateOf("seed-note@1") != nil {
		t.Error("the known templates")
	}
}
