package journal

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/banktransaction"
	"github.com/cognizhi/fyniq/internal/domain/billpayment"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/receipt"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Matching a bank line to what it settles (FR-BNK-07, FR-BNK-08, FR-AR-18; p1-bank-import-reconcile task 4.3, design
// D5). Accepting a match to invoices records a receipt from the line, and to bills a bill payment, through their own
// commands in the line's unit of work: the same validations, settlement tolerances, realised FX and postings as the
// hand path, one document or many, part payments, and what is left over kept as a credit with the contact. A receipt,
// payment, deposit or refund already recorded by hand for that money is matched as it is and posts nothing new; making
// a second one beside it is refused. The database checks every match posts the line's amount through its account
// (FY228), and that no matched document is reversed until its match is undone (FY229).

// The stable codes of matching (ENG-23).
const (
	CodeDirection = "BANKTXN-10" // money in matched to bills, or money out to invoices
	CodeRecorded  = "BANKTXN-11" // the money was recorded by hand already: match that instead of recording it twice
	CodeNotMoving = "BANKTXN-12" // the document recorded by hand does not move the line's amount through its account
	CodeOverParts = "BANKTXN-13" // the parts settle more than the line brought in or paid out
)

// RecordedWindow is how many days either side of a line's booking date a document recorded by hand may be dated and
// still be the same money (design D5).
const RecordedWindow = 7

// InvoiceMatch is a line of money in matched to a customer's invoices: the parts in the invoices' currency (zero: the
// line's), and what is left over as a credit with the customer (nil: what the parts leave of the line when the
// invoices are in its currency, else nothing).
type InvoiceMatch struct {
	LineID     uuid.UUID
	CustomerID uuid.UUID
	Currency   money.Currency
	Invoices   []receipt.Part
	Unapplied  *money.Money
	Basis      string
	RuleID     *uuid.UUID
	At         time.Time
}

// BillMatch is a line of money out matched to a supplier's bills, as InvoiceMatch is for invoices.
type BillMatch struct {
	LineID     uuid.UUID
	SupplierID uuid.UUID
	Currency   money.Currency
	Bills      []billpayment.Part
	Unapplied  *money.Money
	Basis      string
	RuleID     *uuid.UUID
	At         time.Time
}

// Matched is a matched line: the document it settles or became, the entry made from the line (none for a document
// recorded by hand), the match and the line as it now is.
type Matched struct {
	Document storage.Document
	Entry    *storage.Entry
	Match    storage.BankMatch
	Line     storage.BankLine
}

// matchable is a line waiting to be matched, with its money account and its signed amount: unreconciled, or in
// suspense, whose posting is reversed first in this unit of work.
func (s BankTransactions) matchable(ctx context.Context, u storage.UnitOfWork, lineID uuid.UUID, at time.Time) (storage.BankLine, storage.MoneyAccount, money.Money, error) {
	line, err := u.Banking().Line(ctx, lineID)
	if errors.Is(err, ports.ErrNotFound) {
		return line, storage.MoneyAccount{}, money.Money{}, &banktransaction.Error{Code: banktransaction.CodeLine,
			Message: "the bank line does not exist in this organisation", Fix: "Choose a line from the Categorise queue"}
	}
	if err != nil {
		return line, storage.MoneyAccount{}, money.Money{}, err
	}
	switch line.Status {
	case storage.LineUnreconciled:
	case storage.LineSuspense:
		if err := s.reverse(ctx, u, line, "Matched from suspense", at); err != nil {
			return line, storage.MoneyAccount{}, money.Money{}, err
		}
	default:
		return line, storage.MoneyAccount{}, money.Money{}, &banktransaction.Error{Code: CodeLineDone,
			Message: fmt.Sprintf("the line is %s, not waiting to be matched", line.Status), Fix: "Undo what it is matched to first, or resolve the possible duplicate"}
	}
	acct, err := u.Banking().MoneyAccount(ctx, line.MoneyAccountID)
	if err != nil {
		return line, acct, money.Money{}, err
	}
	cur, err := money.Lookup(line.Currency)
	if err != nil {
		return line, acct, money.Money{}, err
	}
	amount, err := money.ParseStored(string(line.Amount), cur)
	return line, acct, amount, err
}

// clip is s cut to at most n bytes, at a rune boundary.
func clip(s string, n int) string {
	for len(s) > n {
		_, k := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-k]
	}
	return s
}

// leftOver is what a line leaves over after its parts, in the documents' currency: as given, else the line less the
// parts when they are in its currency, else nothing. Parts that settle more than the line refuse it.
func leftOver(line money.Money, cur money.Currency, parts []money.Money, given *money.Money) (money.Money, error) {
	if given != nil {
		return *given, nil
	}
	if cur != line.Currency() {
		return money.Zero(cur), nil
	}
	rest := line
	for _, p := range parts {
		if p.Currency() == cur {
			rest, _ = rest.Sub(p)
		}
	}
	if rest.Sign() < 0 {
		return money.Money{}, &banktransaction.Error{Code: CodeOverParts, Message: fmt.Sprintf("the parts settle %s %s more than the line's %s %s",
			rest.Neg(), cur.Code, line, cur.Code), Fix: "Settle no more than the line, and leave what is short open"}
	}
	return rest, nil
}

// MatchInvoices matches a line of money in to a customer's invoices by recording a receipt from it, in one transaction
// with the match and the line's status: the receipt is dated on the line's booking date into its money account, with
// the bank's reference. When the money was recorded by hand already, it is refused: match that receipt instead.
func (s BankTransactions) MatchInvoices(ctx context.Context, org uuid.UUID, m InvoiceMatch) (Matched, error) {
	var out Matched
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		line, acct, amount, err := s.matchable(ctx, u, m.LineID, m.At)
		if err != nil {
			return err
		}
		if amount.Sign() <= 0 {
			return &banktransaction.Error{Code: CodeDirection, Message: "the line is money out, which pays bills, not invoices", Fix: "Match it to bills, or categorise it"}
		}
		if err := notRecorded(ctx, u, line); err != nil {
			return err
		}
		cur := m.Currency
		if cur.Code == "" {
			cur = amount.Currency()
		}
		var parts []money.Money
		for _, p := range m.Invoices {
			parts = append(parts, p.Amount)
		}
		rest, err := leftOver(amount, cur, parts, m.Unapplied)
		if err != nil {
			return err
		}
		d := receipt.Draft{CustomerID: m.CustomerID, Date: line.BookingDate, AccountID: acct.AccountID, Currency: cur, Reference: clip(line.Reference, receipt.MaxReference),
			Invoices: m.Invoices, Unapplied: rest}
		if cur != amount.Currency() {
			d.Received = &amount // a base-currency account received this for invoices in another currency
		}
		rec, err := CustomerReceipts{Store: s.Store, AfterPosting: s.AfterPosting}.RecordIn(ctx, u, d)
		if err != nil {
			return err
		}
		if out, err = s.matched(ctx, u, line, storage.MatchReceipt, rec.Receipt, &rec.Entry, m.Basis, m.RuleID, m.At); err != nil {
			return err
		}
		var invoices []uuid.UUID
		for _, p := range m.Invoices {
			invoices = append(invoices, p.InvoiceID)
		}
		return closeSignals(ctx, u, invoices, line.ID, m.At)
	})
	return out, err
}

// MatchBills matches a line of money out to a supplier's bills by recording a bill payment from it, as MatchInvoices
// does a receipt.
func (s BankTransactions) MatchBills(ctx context.Context, org uuid.UUID, m BillMatch) (Matched, error) {
	var out Matched
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		line, acct, amount, err := s.matchable(ctx, u, m.LineID, m.At)
		if err != nil {
			return err
		}
		if amount.Sign() >= 0 {
			return &banktransaction.Error{Code: CodeDirection, Message: "the line is money in, which settles invoices, not bills", Fix: "Match it to invoices, or categorise it"}
		}
		if err := notRecorded(ctx, u, line); err != nil {
			return err
		}
		paid := amount.Neg()
		cur := m.Currency
		if cur.Code == "" {
			cur = paid.Currency()
		}
		var parts []money.Money
		for _, p := range m.Bills {
			parts = append(parts, p.Amount)
		}
		rest, err := leftOver(paid, cur, parts, m.Unapplied)
		if err != nil {
			return err
		}
		d := billpayment.Draft{SupplierID: m.SupplierID, Date: line.BookingDate, AccountID: acct.AccountID, Currency: cur,
			Reference: clip(line.Reference, billpayment.MaxReference), Bills: m.Bills, Unapplied: rest}
		if cur != paid.Currency() {
			d.Paid = &paid // a base-currency account paid this for bills in another currency
		}
		pay, err := BillPayments{Store: s.Store, AfterPosting: s.AfterPosting}.RecordIn(ctx, u, d)
		if err != nil {
			return err
		}
		out, err = s.matched(ctx, u, line, storage.MatchBillPayment, pay.Payment, &pay.Entry, m.Basis, m.RuleID, m.At)
		return err
	})
	return out, err
}

// MatchRecorded matches a line to a receipt, payment, deposit or refund recorded by hand for that money: the document
// must move the line's amount through its money account, and nothing new is posted.
func (s BankTransactions) MatchRecorded(ctx context.Context, org, lineID, documentID uuid.UUID, basis string, at time.Time) (Matched, error) {
	var out Matched
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		line, acct, _, err := s.matchable(ctx, u, lineID, at)
		if err != nil {
			return err
		}
		doc, err := u.Documents().Lock(ctx, documentID)
		if err != nil {
			return err
		}
		if ok, err := moves(ctx, u, doc, line, acct); err != nil {
			return err
		} else if !ok {
			return &banktransaction.Error{Code: CodeNotMoving, Message: fmt.Sprintf("%s does not move %s %s through the line's money account",
				firstOf(doc.Number, string(doc.Type)), line.Amount, line.Currency), Fix: "Choose the receipt, payment, deposit or refund recorded for this money"}
		}
		if out, err = s.matched(ctx, u, line, storage.MatchRecorded, doc, nil, basis, nil, at); err != nil {
			return err
		}
		if doc.Type != storage.DocumentCustomerReceipt {
			return nil
		}
		org, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		base, err := money.Lookup(org.BaseCurrency)
		if err != nil {
			return err
		}
		d, err := receipt.Read(doc, base)
		if err != nil {
			return err
		}
		var invoices []uuid.UUID
		for _, p := range d.Invoices {
			invoices = append(invoices, p.InvoiceID)
		}
		return closeSignals(ctx, u, invoices, line.ID, at)
	})
	return out, err
}

// closeSignals closes the pending incoming-funds signals pointing to invoices, as the statement line matched to them
// (FR-BNK-16, UC-BNK-02): in the unit of work of the match, so the invoice stops showing "likely paid" as it is paid.
func closeSignals(ctx context.Context, u storage.UnitOfWork, invoices []uuid.UUID, line uuid.UUID, at time.Time) error {
	if len(invoices) == 0 {
		return nil
	}
	pending, err := u.BankMatching().PendingSignals(ctx)
	if err != nil {
		return err
	}
	for _, sig := range pending {
		if sig.InvoiceID == nil || !slices.Contains(invoices, *sig.InvoiceID) {
			continue
		}
		if _, err := u.BankMatching().CloseSignal(ctx, sig.ID, storage.SignalClosed, &line, at); err != nil {
			return err
		}
	}
	return nil
}

func (s BankTransactions) matched(ctx context.Context, u storage.UnitOfWork, line storage.BankLine, kind string, doc storage.Document, e *storage.Entry,
	basis string, rule *uuid.UUID, at time.Time) (Matched, error) {
	if basis == "" {
		basis = storage.BasisPerson
	}
	out := Matched{Document: doc, Entry: e}
	n := storage.NewBankMatch{LineID: line.ID, Kind: kind, DocumentID: &doc.ID, Basis: basis, RuleID: rule, At: at}
	if e != nil {
		n.EntryID = &e.ID
	}
	var err error
	if out.Match, err = u.BankMatching().MakeMatch(ctx, n); err != nil {
		return Matched{}, err
	}
	if out.Line, err = u.Banking().SetLineStatus(ctx, line.ID, line.Version, storage.LineMatched, at); err != nil {
		return Matched{}, err
	}
	return out, nil
}

// Unmatch undoes a line's match with a reason, while its period is open: a receipt or bill payment made from the line
// is voided on its own date, which reopens what it settled; a document recorded by hand stays as it is; the line goes
// back to the queue unreconciled.
func (s BankTransactions) Unmatch(ctx context.Context, org, lineID uuid.UUID, reason string, at time.Time) (storage.BankLine, error) {
	if reason != "" {
		ctx = storage.WithReason(ctx, reason)
	}
	var out storage.BankLine
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		line, err := u.Banking().Line(ctx, lineID)
		if err != nil {
			return err
		}
		matches, err := u.BankMatching().Matches(ctx, storage.BankMatchFilter{LineID: &line.ID})
		if err != nil {
			return err
		}
		var m *storage.BankMatch
		for i := range matches {
			switch matches[i].Kind {
			case storage.MatchReceipt, storage.MatchBillPayment, storage.MatchRecorded:
				m = &matches[i]
			}
		}
		if m == nil {
			return &banktransaction.Error{Code: CodeNotCategory, Message: fmt.Sprintf("the line is %s, not matched to a document", line.Status),
				Fix: "Undo a categorisation from the line itself, or a transfer from the transfer"}
		}
		if _, err := u.BankMatching().UndoMatch(ctx, m.ID, reason, at); err != nil { // before its document is voided (FY229)
			return err
		}
		switch m.Kind {
		case storage.MatchReceipt:
			if _, err := (CustomerReceipts{Store: s.Store, AfterPosting: s.AfterPosting}).VoidIn(ctx, u, *m.DocumentID, line.BookingDate, reason); err != nil {
				return err
			}
		case storage.MatchBillPayment:
			if _, err := (BillPayments{Store: s.Store, AfterPosting: s.AfterPosting}).VoidIn(ctx, u, *m.DocumentID, line.BookingDate, reason); err != nil {
				return err
			}
		}
		out, err = u.Banking().SetLineStatus(ctx, line.ID, line.Version, storage.LineUnreconciled, at)
		return err
	})
	return out, err
}

// recordedTypes are the documents recorded by hand that move money through a money account: in for money in, out for
// money out. A cash advance is both: an advance goes out, its refund comes in; the amount's sign tells which.
var recordedTypes = map[bool][]storage.DocumentType{
	true:  {storage.DocumentCustomerReceipt, storage.DocumentCustomerDeposit, storage.DocumentSupplierRefund, storage.DocumentCashAdvance},
	false: {storage.DocumentBillPayment, storage.DocumentCustomerRefund, storage.DocumentClaimReimbursement, storage.DocumentCashAdvance},
}

// RecordedFor are the documents recorded by hand for a line's money (design D5): posted receipts, payments, deposits
// and refunds dated within RecordedWindow days of its booking date whose entry, of their current version and not
// reversed, moves the line's amount through its money account, and that no line is matched to yet.
func RecordedFor(ctx context.Context, u storage.UnitOfWork, line storage.BankLine) ([]storage.Document, error) {
	acct, err := u.Banking().MoneyAccount(ctx, line.MoneyAccountID)
	if err != nil {
		return nil, err
	}
	cur, err := money.Lookup(line.Currency)
	if err != nil {
		return nil, err
	}
	amount, err := money.ParseStored(string(line.Amount), cur)
	if err != nil {
		return nil, err
	}
	kinds := recordedTypes[amount.Sign() > 0]
	lines, err := u.Ledger().AccountLines(ctx, acct.AccountID, line.BookingDate.AddDate(0, 0, -RecordedWindow), line.BookingDate.AddDate(0, 0, RecordedWindow), 1000)
	if err != nil {
		return nil, err
	}
	var out []storage.Document
	seen := map[string]bool{}
	for _, l := range lines {
		if l.ReversesEntryID != nil || l.Currency != line.Currency || seen[l.Source.ID] {
			continue
		}
		if x, err := money.ParseStored(string(l.Amount), cur); err != nil || !x.Equal(amount) {
			continue
		}
		id, err := uuid.Parse(l.Source.ID)
		if err != nil {
			continue
		}
		doc, err := u.Documents().Get(ctx, id)
		if errors.Is(err, ports.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		known := false
		for _, k := range kinds {
			known = known || doc.Type == k
		}
		if !known {
			continue
		}
		seen[l.Source.ID] = true
		ok, err := moves(ctx, u, doc, line, acct)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		live, err := u.BankMatching().Matches(ctx, storage.BankMatchFilter{DocumentID: &doc.ID})
		if err != nil {
			return nil, err
		}
		if len(live) == 0 {
			out = append(out, doc)
		}
	}
	return out, nil
}

// moves says whether a posted document of a kind recorded by hand moves the line's amount through its money account
// in an entry of its current version that nothing reversed (what FY228 checks).
func moves(ctx context.Context, u storage.UnitOfWork, doc storage.Document, line storage.BankLine, acct storage.MoneyAccount) (bool, error) {
	if doc.Status != storage.DocumentPosted {
		return false, nil
	}
	var identities []string
	switch doc.Type {
	case storage.DocumentCustomerReceipt:
		identities = []string{ledger.CustomerReceiptIdentity(doc.ID, doc.Version, ledger.RuleCustomerReceipt)}
	case storage.DocumentBillPayment:
		identities = []string{ledger.BillPaymentIdentity(doc.ID, doc.Version, ledger.RuleBillPayment)}
	case storage.DocumentCustomerDeposit:
		identities = []string{ledger.CustomerDepositIdentity(doc.ID, doc.Version, ledger.RuleCustomerDeposit)}
	case storage.DocumentCustomerRefund: // of a credit, or of a deposit
		identities = []string{ledger.CustomerRefundIdentity(doc.ID, doc.Version, ledger.RuleCustomerRefund),
			ledger.CustomerRefundIdentity(doc.ID, doc.Version, ledger.RuleDepositRefund)}
	case storage.DocumentSupplierRefund:
		identities = []string{ledger.SupplierRefundIdentity(doc.ID, doc.Version, ledger.RuleSupplierRefund)}
	case storage.DocumentClaimReimbursement: // money paid back to a claimant (FR-AP-07)
		identities = []string{ledger.ClaimReimbursementIdentity(doc.ID, doc.Version, ledger.RuleClaimReimbursement)}
	case storage.DocumentCashAdvance: // money advanced to a member, or its refund: the amount's sign tells which (FR-AP-17)
		identities = []string{ledger.CashAdvanceIdentity(doc.ID, doc.Version, ledger.RuleCashAdvance)}
	}
	var e storage.Entry
	found := false
	for _, id := range identities {
		x, err := u.Ledger().ByIdentity(ctx, id)
		if errors.Is(err, ports.ErrNotFound) {
			continue
		}
		if err != nil {
			return false, err
		}
		e, found = x, true
		break
	}
	if !found {
		return false, nil
	}
	if _, err := u.Ledger().ByIdentity(ctx, ledger.ReversalIdentity(e.PostingIdentity)); err == nil {
		return false, nil
	} else if !errors.Is(err, ports.ErrNotFound) {
		return false, err
	}
	cur, err := money.Lookup(line.Currency)
	if err != nil {
		return false, err
	}
	amount, err := money.ParseStored(string(line.Amount), cur)
	if err != nil {
		return false, err
	}
	for _, l := range e.Lines {
		if l.AccountID != acct.AccountID || l.Currency != line.Currency {
			continue
		}
		if x, err := money.ParseStored(string(l.Amount), cur); err == nil && x.Equal(amount) {
			return true, nil
		}
	}
	return false, nil
}

// notRecorded refuses to record the line's money again when a document recorded by hand for it waits to be matched.
func notRecorded(ctx context.Context, u storage.UnitOfWork, line storage.BankLine) error {
	docs, err := RecordedFor(ctx, u, line)
	if err != nil || len(docs) == 0 {
		return err
	}
	return &banktransaction.Error{Code: CodeRecorded, Message: fmt.Sprintf("%s records this money already", firstOf(docs[0].Number, string(docs[0].Type))),
		Fix: "Match the line to it instead: recording the money twice would post it twice"}
}

func firstOf(s, otherwise string) string {
	if s == "" {
		return otherwise
	}
	return s
}
