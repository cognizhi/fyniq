package taxfilings_test

import (
	"testing"

	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/domain/taxfilings"
	"github.com/cognizhi/fyniq/packs"
)

func cit(t *testing.T) pack.Version {
	t.Helper()
	v, err := pack.Load(packs.FS, "sg-cit/2026.1")
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// Each figure worked by hand from IRAS's rules (the sg-cit pack's sources), YA 2026, 17%, a 50% rebate capped at S$40,000 less the S$2,000 cash grant for a company that
// qualifies for it:
//   - S$300,000, partial exemption: 75% of 10,000 and 50% of 190,000 is 7,500 + 95,000 = 102,500 exempt; 197,500 taxable; tax 33,575.00; rebate 16,787.50; with the grant
//     the rebate is 14,787.50;
//   - S$250,000, start-up exemption: 75% of 100,000 and 50% of 100,000 is 125,000 exempt; 125,000 taxable; tax 21,250.00; rebate 10,625.00;
//   - S$120,000 with 15,000 added back and 5,000 of allowances taken off: 130,000 chargeable; partial exemption 7,500 + 60,000 = 67,500; 62,500 taxable; tax 10,625.00;
//     rebate 5,312.50;
//   - S$1,000,000, partial exemption: 102,500 exempt, 897,500 taxable, tax 152,575.00, the rebate 76,287.50 capped at 40,000.
//
// req: FR-TAX-06
func TestTheWorkingPaperIsWorkedByHand(t *testing.T) {
	v := cit(t)
	for name, tc := range map[string]struct {
		in                                                         taxfilings.Input
		chargeable, exempt, taxable, tax, rebate, payable, rounded string
	}{
		"partial exemption, no grant":  {taxfilings.Input{YA: 2026, ProfitBeforeTax: "300000", Scheme: pack.SchemePartial}, "300000.00", "102500.00", "197500.00", "33575.00", "16787.50", "16787.50", "16787.50"},
		"partial exemption, the grant": {taxfilings.Input{YA: 2026, ProfitBeforeTax: "300000", Scheme: pack.SchemePartial, CashGrantEligible: true}, "300000.00", "102500.00", "197500.00", "33575.00", "14787.50", "18787.50", "18787.50"},
		"start-up exemption":           {taxfilings.Input{YA: 2026, ProfitBeforeTax: "250000", Scheme: pack.SchemeStartUp}, "250000.00", "125000.00", "125000.00", "21250.00", "10625.00", "10625.00", "10625.00"},
		"adjustments": {taxfilings.Input{YA: 2026, ProfitBeforeTax: "120000", Scheme: pack.SchemePartial, Adjustments: []taxfilings.Adjustment{
			{Kind: taxfilings.NonDeductible, Amount: "15000", Reason: "Client entertainment", Evidence: true},
			{Kind: taxfilings.CapitalAllowance, Amount: "-5000", Reason: "Allowances over depreciation", Evidence: true}}}, "130000.00", "67500.00", "62500.00", "10625.00", "5312.50", "5312.50", "5312.50"},
		"the rebate cap": {taxfilings.Input{YA: 2026, ProfitBeforeTax: "1000000", Scheme: pack.SchemePartial}, "1000000.00", "102500.00", "897500.00", "152575.00", "40000.00", "112575.00", "112575.00"},
	} {
		t.Run(name, func(t *testing.T) {
			p, bad := taxfilings.Work(v, tc.in)
			if len(bad) > 0 {
				t.Fatal(bad)
			}
			if p.ChargeableIncome != tc.chargeable || p.Exempt != tc.exempt || p.NormalChargeable != tc.taxable || p.TaxBeforeRebate != tc.tax || p.Rebate != tc.rebate ||
				p.TaxPayable != tc.payable || p.TaxPayableRounded != tc.rounded || p.Rate != "0.17" || p.PackVersion != "sg-cit@2026.1" {
				t.Errorf("the paper is %+v", p)
			}
		})
	}
}

// A loss pays no tax and is carried: the adjusted profit below zero leaves chargeable income at zero and the loss forward, and a tax payable of nothing; and a figure with more
// than two decimals is taxed exactly and rounded once, at the end.
// req: FR-TAX-06
func TestALossIsCarriedAndTheTaxIsRoundedOnce(t *testing.T) {
	v := cit(t)
	p, bad := taxfilings.Work(v, taxfilings.Input{YA: 2026, ProfitBeforeTax: "-50000", Scheme: pack.SchemePartial,
		Adjustments: []taxfilings.Adjustment{{Kind: taxfilings.NonDeductible, Amount: "10000", Reason: "Fines"}}})
	if len(bad) > 0 || p.ChargeableIncome != "0.00" || p.LossForward != "40000.00" || p.TaxBeforeRebate != "0.00" || p.TaxPayableRounded != "0.00" {
		t.Fatalf("%+v %v", p, bad)
	}
	// a profit of 114,845.67 under the partial exemption: 7,500 + 50% of 104,845.67 = 59,922.835 exempt, 54,922.835 taxable, tax 9,336.88195, rebate 4,668.440975
	p, bad = taxfilings.Work(v, taxfilings.Input{YA: 2026, ProfitBeforeTax: "114845.67", Scheme: pack.SchemePartial})
	if len(bad) > 0 || p.Exempt != "59922.835" || p.NormalChargeable != "54922.835" || p.TaxBeforeRebate != "9336.88195" || p.Rebate != "4668.440975" || p.TaxPayable != "4668.440975" || p.TaxPayableRounded != "4668.44" {
		t.Fatalf("%+v %v", p, bad)
	}
}

// An adjustment with no reason, one of a kind that is not offered, an amount that is not a number, a scheme that is neither and a Year of Assessment the pack has no rate for
// are refused with their field.
// req: FR-TAX-06
func TestTheWorkingPaperRefusesWhatItCannotWork(t *testing.T) {
	v := cit(t)
	good := taxfilings.Input{YA: 2026, ProfitBeforeTax: "100000", Scheme: pack.SchemePartial}
	for name, mut := range map[string]func(*taxfilings.Input){
		"no reason": func(i *taxfilings.Input) {
			i.Adjustments = []taxfilings.Adjustment{{Kind: taxfilings.NonDeductible, Amount: "1"}}
		},
		"a kind of its own": func(i *taxfilings.Input) {
			i.Adjustments = []taxfilings.Adjustment{{Kind: "magic", Amount: "1", Reason: "x"}}
		},
		"not a number":         func(i *taxfilings.Input) { i.ProfitBeforeTax = "a lot" },
		"a scheme of its own":  func(i *taxfilings.Input) { i.Scheme = "none" },
		"a YA before the pack": func(i *taxfilings.Input) { i.YA = 2015 },
	} {
		in := good
		mut(&in)
		if _, bad := taxfilings.Work(v, in); len(bad) == 0 {
			t.Errorf("%s was worked", name)
		}
	}
}
