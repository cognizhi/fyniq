package banking

import (
	"context"
	"strings"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ which bank or provider an account is at
//
// A money account's bank decides which payment files it may give (FR-BNK-12, rev 0.157; p1-payment-files design D2). It
// is its statement connector's provider when the account syncs through one (Wise, Airwallex; FR-BNK-15), and otherwise
// its institution, which the screens pick from this list for a bank account rather than have typed. Decided in task
// 1.1: the institution stays text, and Fyniq recognises it by its name or a common other name, in any case and without
// "Bank", "Ltd" or "Singapore", so the text existing accounts were given maps as a picked one does. A name on no list
// is another bank, which gives the payment list.

// Institution is a bank or provider Fyniq recognises.
type Institution struct {
	Key  string // as connectors and payment files name it
	Name string // as the screens list it
	// Also are other names it goes by.
	Also []string
}

// Institutions are the banks and providers Singapore companies commonly pay from, in the order the screens list them.
var Institutions = []Institution{
	{Key: "dbs", Name: "DBS", Also: []string{"POSB", "DBS POSB", "DBS/POSB", "Development Bank of Singapore"}},
	{Key: "ocbc", Name: "OCBC", Also: []string{"Oversea-Chinese Banking Corporation"}},
	{Key: "uob", Name: "UOB", Also: []string{"United Overseas Bank"}},
	{Key: "standard-chartered", Name: "Standard Chartered", Also: []string{"StanChart"}},
	{Key: "hsbc", Name: "HSBC"},
	{Key: "citibank", Name: "Citibank", Also: []string{"Citi"}},
	{Key: "maybank", Name: "Maybank", Also: []string{"Malayan Banking"}},
	{Key: "cimb", Name: "CIMB"},
	{Key: "bank-of-china", Name: "Bank of China"},
	{Key: "icbc", Name: "ICBC"},
	{Key: "rhb", Name: "RHB"},
	{Key: "gxs", Name: "GXS Bank"},
	{Key: "trust", Name: "Trust Bank"},
	{Key: "maribank", Name: "MariBank"},
	{Key: "anext", Name: "ANEXT Bank"},
	{Key: "aspire", Name: "Aspire"},
	{Key: "airwallex", Name: "Airwallex"},
	{Key: "wise", Name: "Wise", Also: []string{"TransferWise", "Wise Business", "Wise Asia-Pacific"}},
}

// ProviderOf is the key of the bank or provider an institution's name is, or "" when it is on no list.
func ProviderOf(institution string) string {
	n := plainName(institution)
	if n == "" {
		return ""
	}
	for _, i := range Institutions {
		if plainName(i.Name) == n {
			return i.Key
		}
		for _, a := range i.Also {
			if plainName(a) == n {
				return i.Key
			}
		}
	}
	return ""
}

// InstitutionName is a key's name as the screens list it, or "" for none.
func InstitutionName(key string) string {
	for _, i := range Institutions {
		if i.Key == key {
			return i.Name
		}
	}
	return ""
}

// plainName is a name as it is compared: lower case, letters and digits only, without the words a bank's name often
// carries ("bank", "ltd", "limited", "pte", "singapore", "berhad") at its end.
func plainName(s string) string {
	words := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9') })
	for len(words) > 1 {
		switch words[len(words)-1] {
		case "bank", "ltd", "limited", "pte", "singapore", "berhad", "sg":
			words = words[:len(words)-1]
			continue
		}
		break
	}
	return strings.Join(words, " ")
}

// ProviderOfAccount is the bank or provider money account m is at (design D2): its current source's connector's when it
// syncs through one Fyniq recognises, else its institution's; "" when neither is on the list.
func ProviderOfAccount(ctx context.Context, u storage.UnitOfWork, m storage.MoneyAccount) (string, error) {
	sources, err := u.Banking().Sources(ctx, m.ID)
	if err != nil {
		return "", err
	}
	if n := len(sources); n > 0 && sources[n-1].Kind == storage.MoneySourceConnector && sources[n-1].ConnectorID != nil {
		installed, err := u.Connectors().Organisation(ctx)
		if err != nil {
			return "", err
		}
		for _, c := range installed {
			if c.ID == *sources[n-1].ConnectorID && InstitutionName(c.Connector) != "" {
				return c.Connector, nil
			}
		}
	}
	return ProviderOf(m.Institution), nil
}
