package paymentfile_test

import (
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/domain/paymentfile"
)

// The files a money account may give (rev 0.157; p1-payment-files design D1, D2, D5): the payment list for any bank, by
// default; Wise's batch payment file only for a Wise account, and DBS IDEAL's UFF only for a DBS account, each refused
// until it is built; an Airwallex account and another bank give the payment list alone. The bank's own check comes before
// "not built", so the refusal for the wrong bank stays true once the file is built.
// req: FR-BNK-12
// req: banking/bank-transactions The file is a setting of the money account (rev 0.157)
// req: banking/bank-transactions Airwallex pays through its API (rev 0.157)
func TestEachBankTakesItsOwnFile(t *testing.T) {
	built := paymentfile.Built()
	offered := func(provider string) string {
		var out []string
		for _, c := range built.Choices(provider) {
			if c.Offered != (c.Reason == "") {
				t.Errorf("%s for %q: offered %v with the reason %q", c.Key, provider, c.Offered, c.Reason)
			}
			if c.Offered {
				out = append(out, c.Key)
			}
		}
		return strings.Join(out, ",")
	}
	for _, provider := range []string{"", "dbs", "wise", "airwallex", "ocbc"} {
		if got := offered(provider); got != "payment-list" {
			t.Errorf("offered to %q: %s, want the payment list alone while no bank's file is built", provider, got)
		}
	}
	for _, c := range []struct{ file, provider, code, words string }{
		{"wise-batch", "dbs", paymentfile.CodeProvider, "only for a Wise account"},
		{"wise-batch", "", paymentfile.CodeProvider, "only for a Wise account"},
		{"dbs-uff", "wise", paymentfile.CodeProvider, "only for a DBS account"},
		{"dbs-uff", "airwallex", paymentfile.CodeProvider, "only for a DBS account"},
		{"wise-batch", "wise", paymentfile.CodeNotBuilt, "Wise's template for paying SGD to bank accounts"},
		{"dbs-uff", "dbs", paymentfile.CodeNotBuilt, "DBS UFF Version 2.2"},
		{"swift-mt101", "dbs", paymentfile.CodeProvider, "not a payment file Fyniq writes"},
	} {
		err := built.Check(c.file, c.provider)
		if err == nil || err.Code != c.code || !strings.Contains(err.Message, c.words) || !strings.HasPrefix(err.Message, "Payment file:") || err.Fix == "" {
			t.Errorf("%s at %q: %+v, want %s saying %q", c.file, c.provider, err, c.code, c.words)
		}
	}
	if w, err := built.Writer("", "ocbc"); err != nil || w.Name() != "payment-list@1" {
		t.Errorf("an account never set gives %v, %v; want the payment list", w, err)
	}

	// A bank's file, once built, is offered to its own bank's accounts alone.
	withWise := paymentfile.Writers{"payment-list": paymentfile.PaymentList{}, "wise-batch": paymentfile.PaymentList{}}
	if got := strings.Join([]string{offeredBy(withWise, "wise"), offeredBy(withWise, "dbs")}, " | "); got != "payment-list,wise-batch | payment-list" {
		t.Errorf("with Wise's file built: %s", got)
	}
	if _, err := withWise.Writer("wise-batch", "dbs"); err == nil || err.Code != paymentfile.CodeProvider {
		t.Errorf("Wise's file for a DBS account: %v", err)
	}
	if len(paymentfile.Formats()) != 3 {
		t.Errorf("the files: %v", paymentfile.Formats())
	}
	if f, ok := paymentfile.Lookup("dbs-uff"); !ok || f.Provider != "dbs" {
		t.Errorf("DBS's file: %+v %v", f, ok)
	}
}

func offeredBy(w paymentfile.Writers, provider string) string {
	var out []string
	for _, c := range w.Choices(provider) {
		if c.Offered {
			out = append(out, c.Key)
		}
	}
	return strings.Join(out, ",")
}
