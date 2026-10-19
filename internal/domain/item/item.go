// Package item holds the rules an item must meet before it is saved (FR-ITM-01): every rule that fails is
// reported at once, with its field, a stable code and how to fix it (ENG-23, FR-INT-07). The database's checks
// (migration 000027) are the backstop.
package item

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The codes of an item's rules.
const (
	CodeCode     = "ITM-01" // not an item code
	CodeName     = "ITM-02" // the name is blank or too long
	CodeType     = "ITM-03" // neither a product nor a service
	CodeText     = "ITM-04" // the description or unit is only spaces or too long
	CodeCurrency = "ITM-05" // a price's currency is unknown, or priced twice
	CodePrice    = "ITM-06" // a unit price is not a decimal, is below zero or does not fit
	CodeGSTCode  = "ITM-07" // not the form of a GST code
	CodeAccount  = "ITM-08" // the revenue account is not an active revenue account that takes postings
)

// Problem is one rule that failed.
type Problem struct {
	Code, Field, Message, Fix string
}

var (
	// code is what an item's code may hold: 1 to 30 letters, digits, dots, underscores, slashes or hyphens,
	// starting with a letter or digit, such as CONSULT-HR or SKU/1042.
	code    = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z._/-]{0,29}$`)
	gstCode = regexp.MustCompile(`^[A-Z]{2,4}$`)
)

// CheckCode returns the problem with a new item's code, or nil.
func CheckCode(c string) []Problem {
	if !code.MatchString(c) {
		return []Problem{{CodeCode, "code", fmt.Sprintf("%q is not an item code", c),
			"Use 1 to 30 letters, digits, dots, underscores, slashes or hyphens, starting with a letter or digit, such as CONSULT-HR"}}
	}
	return nil
}

// Check returns every rule d breaks, or nil. Prices come back in d as the column holds them only after they
// are saved; Check reads them as entered.
func Check(d storage.ItemDetails) []Problem {
	var out []Problem
	add := func(code, field, message, fix string) { out = append(out, Problem{code, field, message, fix}) }
	if strings.TrimSpace(d.Name) == "" || len([]rune(d.Name)) > 200 {
		add(CodeName, "name", "is blank or longer than 200 characters", "Enter the item's name, such as HR consulting")
	}
	if d.Type != storage.ItemProduct && d.Type != storage.ItemService {
		add(CodeType, "type", fmt.Sprintf("%q is not an item type", d.Type), "Use product or service")
	}
	for _, c := range []struct {
		field, v string
		max      int
	}{{"description", d.Description, 2000}, {"unit", d.Unit, 20}} {
		if len([]rune(c.v)) > c.max || (c.v != "" && strings.TrimSpace(c.v) == "") {
			add(CodeText, c.field, fmt.Sprintf("is only spaces or longer than %d characters", c.max), "Shorten it, or leave it empty")
		}
	}
	if d.GSTCode != "" && !gstCode.MatchString(d.GSTCode) {
		add(CodeGSTCode, "gst_code", fmt.Sprintf("%q is not a GST code", d.GSTCode), "Use the GST code, such as SR or ZR")
	}
	seen := map[string]bool{}
	for i, p := range d.Prices {
		field := fmt.Sprintf("prices[%d]", i)
		cur, err := money.Lookup(p.Currency)
		switch {
		case err != nil:
			add(CodeCurrency, field+".currency", fmt.Sprintf("%q is not a currency Fyniq knows", p.Currency), "Use an ISO 4217 code, such as SGD or USD")
			continue
		case seen[p.Currency]:
			add(CodeCurrency, field+".currency", fmt.Sprintf("%s is priced twice", p.Currency), "Give each currency one price")
			continue
		}
		seen[p.Currency] = true
		price, err := money.ParseUnitPrice(string(p.UnitPrice), cur, field+".unit_price")
		switch {
		case err != nil:
			add(CodePrice, field+".unit_price", fmt.Sprintf("%q is not a unit price in %s", p.UnitPrice, p.Currency),
				fmt.Sprintf("Enter the price as a plain decimal with at most %d decimals and %d digits before the point, such as 120.50",
					money.UnitPriceScale, money.UnitPriceIntegerDigits))
		case price.Sign() < 0:
			add(CodePrice, field+".unit_price", fmt.Sprintf("%s %s is below zero", p.Currency, p.UnitPrice),
				"Enter a price of zero or more; a discount goes on the document's line")
		}
	}
	return out
}

// CheckAccount returns the problem with a revenue account that is not an active revenue account that takes
// postings, or nil.
func CheckAccount(a storage.Account) []Problem {
	if a.Group || !a.Active || a.Type != storage.Revenue {
		return []Problem{{CodeAccount, "revenue_account_id", fmt.Sprintf("account %s is a group, inactive or not a revenue account", a.Code),
			"Choose an active revenue account that takes postings, such as 4000 Sales"}}
	}
	return nil
}
