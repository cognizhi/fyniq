package proposal

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The journal lines of an AI proposal (recur.create_template, gl.post_journal; p1-ai-proposals tasks 4.1, 4.2): in the organisation's base
// currency only, by account code, each amount signed as the API's journals are (a debit above zero, a credit below). The AI's journals keep to
// ordinary accounts: not a group heading, an inactive account, a sub-ledger control account, a bank or cash account, an equity account, nor
// an account the rules treat specially (a share capital, director's, GST, withholding tax or payroll account), so an accrual, a prepayment, a
// reclassification or a correction stays among income, costs, prepayments, deferred revenue and depreciation, and cash, shares, tax and people never
// move through a journal the AI proposed.

// MaxJournalLines is how many lines one journal an AI proposal holds.
const MaxJournalLines = 20

// JournalLineIn is a line of a proposed journal.
type JournalLineIn struct {
	Account     string `json:"account"`
	Amount      string `json:"amount"`
	Description string `json:"description"`
}

// journalRoles are the account roles an AI journal may use.
var journalRoles = []storage.AccountRole{"prepayments", "deferred-revenue", "depreciation", "accumulated-depreciation"}

// JournalLinesOf reads the lines against the chart in the base currency: the journal lines a person would have entered, and the sum of the debits,
// refusing with the field and the fix. The lines must balance, as the posting rules need them to.
func JournalLinesOf(ctx context.Context, u storage.UnitOfWork, at string, in []JournalLineIn) ([]ledger.JournalLine, money.Money, error) {
	var zero money.Money
	o, err := u.Organisation(ctx)
	if err != nil {
		return nil, zero, err
	}
	base, err := money.Lookup(o.BaseCurrency)
	if err != nil {
		return nil, zero, err
	}
	zero = money.Zero(base)
	if len(in) < 2 || len(in) > MaxJournalLines {
		return nil, zero, refusal(fmt.Sprintf("%s holds %d lines", at, len(in)), fmt.Sprintf("Send 2 to %d lines that balance", MaxJournalLines))
	}
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return nil, zero, err
	}
	var out []ledger.JournalLine
	sum, debits := zero, zero
	for i, l := range in {
		where := fmt.Sprintf("%s[%d].", at, i)
		ai := slices.IndexFunc(accounts, func(a storage.Account) bool { return a.Code == strings.TrimSpace(l.Account) })
		if ai < 0 {
			return nil, zero, refusal(where+"account is not the code of an account of this organisation", "Use the code of an account as records_search returned it")
		}
		a := accounts[ai]
		switch {
		case a.Group || !a.Active:
			return nil, zero, refusal(where+"account is a heading or is inactive", "Use an active account that takes postings")
		case a.Control || a.MoneyAccount || a.Type == storage.Equity || (a.Role != "" && !slices.Contains(journalRoles, a.Role)):
			return nil, zero, refusal(where+"account is a bank, sub-ledger, equity or system account, which a person's journal reaches and the AI's does not",
				"Propose the journal between income, cost, prepayment, deferred revenue and depreciation accounts, or ask a person to post it")
		}
		amt, err := money.ParseField(l.Amount, base, where+"amount")
		if err != nil {
			return nil, zero, refusal(where+"amount is not a signed amount in "+base.Code, "Send it as a decimal string such as 250.00 for a debit or -250.00 for a credit")
		}
		if amt.IsZero() {
			return nil, zero, refusal(where+"amount is zero", "A line moves something: leave it out")
		}
		if sum, err = sum.Add(amt); err != nil {
			return nil, zero, err
		}
		if amt.Sign() > 0 {
			if debits, err = debits.Add(amt); err != nil {
				return nil, zero, err
			}
		}
		out = append(out, ledger.JournalLine{AccountID: a.ID, Amount: amt, Description: strings.TrimSpace(l.Description)})
	}
	if !sum.IsZero() {
		return nil, zero, refusal(at+" does not balance: its debits and credits differ by "+sum.String()+" "+base.Code, "Make the debits and the credits equal")
	}
	return out, debits, nil
}

// decimalOf is an exact decimal of a money amount, for a limit's comparison.
func decimalOf(m money.Money) (*apd.Decimal, error) {
	d, _, err := apd.NewFromString(m.String())
	return d, err
}
