package banking_test

import (
	"testing"

	"github.com/cognizhi/fyniq/internal/domain/banking"
)

// Which bank an account is at, from its institution (p1-payment-files design D2, decided in task 1.1): the name the
// screens list, or another name it goes by, in any case and without "Bank", "Ltd" or "Singapore", so text typed before
// the list maps as a picked name does; a name on no list is another bank. Each listed name maps to its own key.
// req: FR-BNK-12
// req: banking/bank-transactions The file is a setting of the money account (rev 0.157)
func TestAnAccountsBankIsRecognisedByName(t *testing.T) {
	for in, want := range map[string]string{
		"DBS": "dbs", "dbs bank ltd": "dbs", "DBS Bank Ltd.": "dbs", "POSB": "dbs", "DBS/POSB": "dbs", "  Dbs  ": "dbs",
		"OCBC Bank": "ocbc", "UOB": "uob", "United Overseas Bank Limited": "uob", "Standard Chartered Bank (Singapore) Limited": "standard-chartered",
		"Standard Chartered Bank": "standard-chartered", "Wise": "wise", "TransferWise": "wise", "Wise Asia-Pacific Pte Ltd": "wise",
		"Airwallex": "airwallex", "airwallex (singapore) pte ltd": "airwallex", "GXS": "gxs", "Bank of China": "bank-of-china",
		"Bank": "", "": "", "DBS Vickers": "", "Revolut": "", "Bank of Singapore": "",
	} {
		if got := banking.ProviderOf(in); got != want {
			t.Errorf("ProviderOf(%q) = %q, want %q", in, got, want)
		}
	}
	seen := map[string]bool{}
	for _, i := range banking.Institutions {
		if seen[i.Key] {
			t.Errorf("%s is listed twice", i.Key)
		}
		seen[i.Key] = true
		if got := banking.ProviderOf(i.Name); got != i.Key {
			t.Errorf("the listed name %q maps to %q, not %q", i.Name, got, i.Key)
		}
		for _, a := range i.Also {
			if got := banking.ProviderOf(a); got != i.Key {
				t.Errorf("%q, another name of %s, maps to %q", a, i.Key, got)
			}
		}
		if banking.InstitutionName(i.Key) != i.Name {
			t.Errorf("the name of %s: %q", i.Key, banking.InstitutionName(i.Key))
		}
	}
	if banking.InstitutionName("revolut") != "" {
		t.Error("a key on no list has a name")
	}
}
