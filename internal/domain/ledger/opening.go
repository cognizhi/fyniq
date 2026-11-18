package ledger

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
)

// An opening item (FR-GL-06; PRD-001 rev 0.257, Appendix B.2 "Opening open items"; p1-workbook-migration design D6)
// [accountant to confirm] is an invoice issued or a bill received before the books start and still unpaid then. It
// posts at the books start under opening_invoice@1 or opening_bill@1: an invoice debits accounts receivable, and a bill credits accounts
// payable, with its total in its currency at its rate, against the opening balance clearing account in the base
// currency. It posts no revenue, cost or GST: those were the books before the books start. The opening balances carry
// the receivables and payables on the clearing account instead, so the clearing nets to zero once both have posted, and
// every receivable and payable is a document that can be settled, aged and reminded.
//
// (rev 0.261) A credit note to a customer, or a supplier's credit note, issued before the books start and still open then
// is an opening item too: it posts the legs an opening invoice or bill of its content would, the other way, under
// opening_credit_note@1 or opening_vendor_credit@1, so a credit open at the cut-over is a document that can be applied
// or refunded.
const (
	RuleOpeningInvoice      = "opening_invoice@1"
	RuleOpeningBill         = "opening_bill@1"
	RuleOpeningCreditNote   = "opening_credit_note@1"
	RuleOpeningVendorCredit = "opening_vendor_credit@1"
	RoleOpeningClearing     = "opening-balance-clearing"
	CodeOpeningItem         = "LEDGER-50" // an opening item dated on or after the books start, or with what posts after it
)

// openingChecks are what an opening item is refused for, beside the rules of its kind.
func openingChecks(kind string, date time.Time, opening *time.Time, spreads, deposits bool, add func(code, field, msg, fix string)) {
	switch {
	case opening == nil:
		add(CodeOpeningItem, "opening_item", fmt.Sprintf("the %s posts as an opening item without the books start", kind),
			"Set the organisation's books start in its profile")
	case !date.IsZero() && !date.Before(*opening):
		add(CodeOpeningItem, "date", fmt.Sprintf("an opening item is dated before the books start, %s; this %s is dated %s", opening.Format(time.DateOnly), kind,
			date.Format(time.DateOnly)), fmt.Sprintf("Enter it as an ordinary %s, or correct its date", kind))
	}
	if spreads {
		add(CodeOpeningItem, "lines", "an opening item spreads nothing: its revenue or cost was in the books before", "Remove the spread")
	}
	if deposits {
		add(CodeOpeningItem, "deposits", "an opening item deducts no deposit", "Remove the deposits; settle it with a receipt")
	}
}

// openingEntry is an opening item's entry: its total on accounts receivable or payable (sign 1 or -1) in its currency at
// its rate under RND-FX, and the same base amount the other way on the clearing account.
func openingEntry(sign int, total money.Money, rate money.Rate, side Account, s Settings, memo string, add func(code, field, msg, fix string)) []Line {
	clearing, ok := s.byRole(RoleOpeningClearing)
	if !ok {
		add(CodeSystemRole, "lines", "the chart has no opening balance clearing account", "Load the standard chart's 3900 Opening balance clearing, or give an equity account the role")
		return nil
	}
	base, err := rounding.FX(total, rate)
	if err != nil {
		add(CodeBaseRange, "lines", fmt.Sprintf("at this rate the total does not fit: %v", err), "Check the amounts and the rate")
		return nil
	}
	amount := total
	if sign < 0 {
		amount, base = total.Neg(), base.Neg()
	}
	return []Line{{AccountID: side.ID, Amount: amount, Rate: rate, Base: base, Description: memo},
		{AccountID: clearing.ID, Amount: base.Neg(), Rate: money.Identity(s.Base), Base: base.Neg(), Description: "Opening item " + memo}}
}

// PostOpeningInvoice turns one version of an opening invoice into its entry (opening_invoice@1): every rule of invoice@1
// holds, then Dr 1100 Accounts receivable with its total at its rate under RND-FX, Cr 3900 Opening balance clearing with
// that base amount, at the books start.
func PostOpeningInvoice(i Invoice, s Settings) (Entry, error) {
	var probs Problems
	add := func(code, field, msg, fix string) { probs = append(probs, Problem{code, field, msg, fix}) }
	spreads := false
	for _, l := range i.Lines {
		spreads = spreads || l.Spread != nil
	}
	openingChecks("invoice", i.Date, i.Opening, spreads, len(i.Deposits) > 0, add)
	plain := i
	plain.Opening = nil
	if _, err := PostInvoice(plain, s); err != nil {
		var ps Problems
		if !errors.As(err, &ps) {
			return Entry{}, err
		}
		probs = append(probs, ps...)
	}
	if len(probs) > 0 {
		return Entry{}, probs
	}
	totals, err := TotalsOf(i)
	if err != nil {
		return Entry{}, Problems{{CodeGSTRate, "lines", err.Error(), "Choose each line's GST code again: its rate comes from the sg-gst pack"}}
	}
	rate := money.Identity(s.Base)
	if i.Rate != nil {
		rate = *i.Rate
	}
	ar, _ := s.byRole(RoleAccountsReceivable)
	source := Source{Type: SourceInvoice, ID: i.ID.String(), Version: i.Version}
	identity := InvoiceIdentity(i.ID, i.Version, RuleOpeningInvoice)
	lines := openingEntry(1, totals.Total, rate, ar, s, i.Memo, add)
	if len(probs) > 0 {
		return Entry{}, probs
	}
	y, m, d := i.Opening.Date()
	return Entry{PostingDate: time.Date(y, m, d, 0, 0, 0, 0, time.UTC), Source: source, RuleVersion: RuleOpeningInvoice, PostingIdentity: identity, Memo: i.Memo,
		Lines: lines}, nil
}

// PostOpeningBill turns one version of an opening bill into its entry (opening_bill@1): every rule of bill@1 holds, then
// Cr 2010 Accounts payable with its total, net plus the GST its supplier stated, at its rate under RND-FX, Dr 3900
// Opening balance clearing with that base amount, at the books start. It withholds no tax: what was withheld was in the
// books before.
func PostOpeningBill(b Bill, s Settings) (Entry, error) {
	var probs Problems
	add := func(code, field, msg, fix string) { probs = append(probs, Problem{code, field, msg, fix}) }
	spreads := false
	for _, l := range b.Lines {
		spreads = spreads || l.Spread != nil
	}
	openingChecks("bill", b.Date, b.Opening, spreads, false, add)
	if b.Withholding != nil {
		add(CodeOpeningItem, "withholding", "an opening item withholds no tax: what was withheld was in the books before", "Remove the withholding")
	}
	plain := b
	plain.Opening, plain.Withholding = nil, nil
	if _, err := PostBill(plain, s); err != nil {
		var ps Problems
		if !errors.As(err, &ps) {
			return Entry{}, err
		}
		probs = append(probs, ps...)
	}
	if len(probs) > 0 {
		return Entry{}, probs
	}
	total := money.Zero(b.Currency)
	for _, l := range b.Lines {
		var err error
		if total, err = total.Add(l.Net); err == nil {
			total, err = total.Add(l.GST)
		}
		if err != nil {
			return Entry{}, Problems{{CodeBaseRange, "lines", err.Error(), "Check the amounts"}}
		}
	}
	rate := money.Identity(s.Base)
	if b.Rate != nil {
		rate = *b.Rate
	}
	ap, _ := s.byRole(RoleAccountsPayable)
	source := Source{Type: SourceBill, ID: b.ID.String(), Version: b.Version}
	identity := BillIdentity(b.ID, b.Version, RuleOpeningBill)
	lines := openingEntry(-1, total, rate, ap, s, b.Memo, add)
	if len(probs) > 0 {
		return Entry{}, probs
	}
	y, m, d := b.Opening.Date()
	return Entry{PostingDate: time.Date(y, m, d, 0, 0, 0, 0, time.UTC), Source: source, RuleVersion: RuleOpeningBill, PostingIdentity: identity, Memo: b.Memo,
		Lines: lines}, nil
}

// opened negates an opening item's entry into a credit's, of its source and rule, its problems worded for the credit.
func opened(e Entry, err error, words *strings.Replacer, source Source, rule, identity string) (Entry, error) {
	if probs, ok := err.(Problems); ok {
		for k := range probs {
			probs[k].Message, probs[k].Fix = words.Replace(probs[k].Message), words.Replace(probs[k].Fix)
		}
		return Entry{}, probs
	}
	if err != nil {
		return Entry{}, err
	}
	for k := range e.Lines {
		e.Lines[k].Amount, e.Lines[k].Base = e.Lines[k].Amount.Neg(), e.Lines[k].Base.Neg()
	}
	e.Source, e.RuleVersion, e.PostingIdentity = source, rule, identity
	return e, nil
}

// PostOpeningCreditNote turns one version of an opening credit note to a customer into its entry
// (opening_credit_note@1; rev 0.261): the legs an opening invoice of its content posts, the other way: Cr 1100 Accounts
// receivable with its total at its rate under RND-FX, Dr 3900 Opening balance clearing with that base amount, at the
// books start. Every refusal is the opening invoice's, worded for a credit note.
func PostOpeningCreditNote(i Invoice, s Settings) (Entry, error) {
	e, err := PostOpeningInvoice(i, s)
	return opened(e, err, asCreditNote, Source{Type: SourceCreditNote, ID: i.ID.String(), Version: i.Version}, RuleOpeningCreditNote,
		CreditNoteIdentity(i.ID, i.Version, RuleOpeningCreditNote))
}

// PostOpeningVendorCredit turns one version of a supplier's opening credit note into its entry
// (opening_vendor_credit@1; rev 0.261): the legs an opening bill of its content posts, the other way: Dr 2010 Accounts
// payable with its total at its rate under RND-FX, Cr 3900 Opening balance clearing with that base amount, at the
// books start. Every refusal is the opening bill's, worded for a credit note.
func PostOpeningVendorCredit(b Bill, s Settings) (Entry, error) {
	e, err := PostOpeningBill(b, s)
	return opened(e, err, asCredit, Source{Type: SourceVendorCredit, ID: b.ID.String(), Version: b.Version}, RuleOpeningVendorCredit,
		VendorCreditIdentity(b.ID, b.Version, RuleOpeningVendorCredit))
}
