package migration

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/bankfile"
	"github.com/cognizhi/fyniq/internal/domain/banking"
	"github.com/cognizhi/fyniq/internal/domain/banktransaction"
	billdoc "github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/billpayment"
	invoicedoc "github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/receipt"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The Transactions sheet's kinds (design D5).
const (
	KindStatement       = "statement"
	KindReceipt         = "receipt"
	KindBillPayment     = "bill_payment"
	KindTransfer        = "transfer"
	KindBankTransaction = "bank_transaction"
)

// lineRef is the reference a Transactions row's line takes in its money account's statement: its row, so a row finds
// its line and no two rows are taken for duplicates. A transfer's other side, which the workbook keeps on the same row,
// is the row's with "-to" or "-from".
func lineRef(row int, side string) string {
	return fmt.Sprintf("WB-%d%s", row, side)
}

// moneyOf says whether the workbook keeps an account as a money account.
func (w *Workbook) moneyOf(code string) bool {
	for _, a := range w.Accounts {
		if a.Code == code {
			return a.Money
		}
	}
	return false
}

// role is the system role of the Fyniq account a workbook account maps onto, as the mapping read it.
func (m Mapping) role(code string) string {
	for _, a := range m.Accounts {
		if a.Workbook == codeOf(code) {
			return a.Role
		}
	}
	return ""
}

// signed is a Transactions row's amount, money in less money out.
func signed(x Transaction) *apd.Decimal {
	out := new(apd.Decimal)
	if x.In != nil {
		out.Set(x.In)
	}
	if x.Out != nil {
		if _, err := apd.BaseContext.WithPrecision(40).Sub(out, out, x.Out); err != nil {
			panic(err)
		}
	}
	return out
}

// equity says whether a row goes to share capital or to convertible instruments: what the Funding sheet's share issues
// and SAFEs record (task 4.6).
func (m Mapping) equity(code string) bool {
	r := m.role(code)
	return r == string(storage.RoleShareCapital) || r == ledger.RoleConvertibleInstruments
}

// transactions are the Transactions sheet's records (design D5): first a statement of each money account's lines, a
// row each, and for a transfer between two of them a line on each side; then, in date order with the rest of the
// history, each row as what it was: a customer's payment of the invoice it names, a payment of a supplier's bill, a
// transfer, a subscription for shares, or a categorised bank transaction, a SAFE's money naming its instrument (rev
// 0.262).
func (w *Workbook) transactions(m Mapping) ([]Record, []dated) {
	type side struct {
		Transaction
		amount *apd.Decimal
		ref    string
	}
	lines := map[string][]side{} // by the workbook's money account
	var accounts []string
	add := func(account string, x Transaction, amount *apd.Decimal, ref string) {
		if _, ok := lines[account]; !ok {
			accounts = append(accounts, account)
		}
		lines[account] = append(lines[account], side{x, amount, ref})
	}
	var rows []dated
	for _, x := range w.Transactions {
		amt := signed(x)
		add(x.Bank, x, amt, lineRef(x.Row, ""))
		switch {
		case w.moneyOf(x.Category):
			other := "-to"
			if amt.Sign() > 0 {
				other = "-from"
			}
			add(x.Category, x, new(apd.Decimal).Neg(amt), lineRef(x.Row, other))
			rows = append(rows, dated{Record: w.transferRecord(m, x, amt.Sign() < 0), on: x.Date, order: 4})
		case m.role(x.Category) == string(storage.RoleShareCapital):
			rows = append(rows, dated{Record: w.subscriptionRecord(m, x), on: x.Date, order: 4})
		case m.role(x.Category) == ledger.RoleAccountsReceivable:
			rows = append(rows, dated{Record: w.receiptRecord(m, x), on: x.Date, order: 4})
		case m.role(x.Category) == ledger.RoleAccountsPayable:
			rows = append(rows, dated{Record: w.paymentRecord(m, x), on: x.Date, order: 4})
		default:
			rows = append(rows, dated{Record: w.categorisedRecord(m, x), on: x.Date, order: 4})
		}
	}
	var statements []Record
	for _, account := range accounts {
		ls := lines[account]
		slices.SortStableFunc(ls, func(a, b side) int { return cmp.Or(a.Date.Compare(b.Date), cmp.Compare(a.Row, b.Row)) })
		code := m.accountOf(account)
		statements = append(statements, Record{Key: "statement:" + code, Kind: KindStatement, Origin: ls[0].Origin,
			make: func(ctx context.Context, u storage.UnitOfWork, e *Env) (Made, error) {
				ma, err := moneyAccountOf(ctx, u, code)
				if err != nil {
					return Made{}, err
				}
				cur, err := money.Lookup(ma.Currency)
				if err != nil {
					return Made{}, err
				}
				st := bankfile.Statement{Currency: cur, From: ls[0].Date, To: ls[len(ls)-1].Date}
				for i, l := range ls {
					amt, err := amount(l.amount, cur, false, "amount")
					if err != nil {
						return Made{}, err
					}
					st.Lines = append(st.Lines, bankfile.Line{Position: i + 1, Booked: l.Date, Amount: amt, Reference: l.ref, Counterparty: l.Contact,
						Description: cmp.Or(l.Description, "Transactions row "+fmt.Sprint(l.Row))})
				}
				got, err := banking.ImportStatement(ctx, u, banking.ImportRequest{Format: bankfile.FormatXLSX, Account: &ma.ID, Source: storage.MoneySourceFile,
					FileRef: "workbook:transactions", At: e.Now}, st)
				if err != nil {
					return Made{}, err
				}
				// A look-alike of another row is the workbook's own row: kept, not a duplicate (FR-BNK-16).
				for _, l := range got.Lines {
					if l.Outcome == banking.OutcomeFlag && l.ID != nil {
						x, err := u.Banking().Line(ctx, *l.ID)
						if err != nil {
							return Made{}, err
						}
						if _, err := banking.Keep(ctx, u, x.ID, x.Version, e.Now); err != nil {
							return Made{}, err
						}
					}
				}
				if got.Statement == nil {
					return Made{Type: "money_account", ID: ma.ID}, nil
				}
				return Made{Type: "bank_statement", ID: got.Statement.ID}, nil
			}})
	}
	return statements, rows
}

// moneyAccountOf is the organisation's money account on the account with a Fyniq code.
func moneyAccountOf(ctx context.Context, u storage.UnitOfWork, code string) (storage.MoneyAccount, error) {
	acc, err := account(ctx, u, "bank", code)
	if err != nil {
		return storage.MoneyAccount{}, err
	}
	all, err := u.Banking().MoneyAccounts(ctx)
	if err != nil {
		return storage.MoneyAccount{}, err
	}
	for _, x := range all {
		if x.AccountID == acc.ID {
			return x, nil
		}
	}
	return storage.MoneyAccount{}, Refused{{Code: CodeMapping, Field: "bank", Message: fmt.Sprintf("account %s is no money account in Banking", code),
		Fix: "Map the workbook's bank or cash account onto a money account of the chart"}}
}

// lineOf is a Transactions row's line in its money account's statement.
func lineOf(ctx context.Context, u storage.UnitOfWork, m Mapping, bank string, ref string) (storage.BankLine, error) {
	ma, err := moneyAccountOf(ctx, u, m.accountOf(bank))
	if err != nil {
		return storage.BankLine{}, err
	}
	lines, err := u.Banking().Lines(ctx, storage.BankLineFilter{MoneyAccountID: &ma.ID})
	if err != nil {
		return storage.BankLine{}, err
	}
	for _, l := range lines {
		if l.Reference == ref {
			return l, nil
		}
	}
	return storage.BankLine{}, Refused{{Code: CodeImport, Field: "line", Message: fmt.Sprintf("no line of %s is the row's (%s)", bank, ref),
		Fix: "Import the money account's statement first"}}
}

// documentOf is the invoice or bill a row names, as an earlier record imported it.
func documentOf(ctx context.Context, u storage.UnitOfWork, e *Env, keys ...string) (storage.Document, error) {
	for _, k := range keys {
		if id, ok := e.Made(k); ok {
			return u.Documents().Get(ctx, id)
		}
	}
	return storage.Document{}, Refused{{Code: CodeImport, Field: "reference", Message: fmt.Sprintf("names %s, which the workbook has no row of", strings.TrimPrefix(keys[0], "invoice:")),
		Fix: "Give the invoice or bill's number as the row's reference, as the workbook matches them"}}
}

// receiptRecord imports a customer's payment of the invoice its reference names (FR-AR-18): its line matched to the
// invoice by a receipt from the line (FR-BNK-07), settling the invoice in full when the money is in another currency, as
// the workbook did, else what the line brings, the rest a credit with the customer.
func (w *Workbook) receiptRecord(m Mapping, x Transaction) Record {
	return Record{Key: "transaction:" + fmt.Sprint(x.Row), Kind: KindReceipt, Origin: x.Origin,
		make: func(ctx context.Context, u storage.UnitOfWork, e *Env) (Made, error) {
			line, err := lineOf(ctx, u, m, x.Bank, lineRef(x.Row, ""))
			if err != nil {
				return Made{}, err
			}
			ref := strings.TrimSpace(x.Ref)
			doc, err := documentOf(ctx, u, e, "invoice:"+ref, "credit_note:"+ref)
			if err != nil {
				return Made{}, err
			}
			base, err := baseOf(ctx, u)
			if err != nil {
				return Made{}, err
			}
			d, err := invoicedoc.Read(doc, base)
			if err != nil {
				return Made{}, err
			}
			lineAmt, err := money.ParseStored(string(line.Amount), money.MustLookup(line.Currency))
			if err != nil {
				return Made{}, err
			}
			part := receipt.Part{InvoiceID: doc.ID, Amount: lineAmt, InFull: d.Currency != lineAmt.Currency()}
			if part.InFull { // the invoice's total in its currency, as the workbook settled it
				if part.Amount, err = w.totalOf(w.Invoices, ref, "", d.Currency); err != nil {
					return Made{}, err
				}
			}
			svc := journal.BankTransactions{Store: e.Store, Clock: e.Clock}
			got, err := svc.MatchInvoices(ctx, e.Org, journal.InvoiceMatch{LineID: line.ID, CustomerID: d.CustomerID, Currency: d.Currency,
				Invoices: []receipt.Part{part}, Basis: storage.BasisPerson, At: e.Now})
			return Made{Type: "document", ID: got.Document.ID}, err
		}}
}

// paymentRecord imports a payment of the supplier's bill its reference names (FR-AP-02), as receiptRecord does for
// invoices.
func (w *Workbook) paymentRecord(m Mapping, x Transaction) Record {
	return Record{Key: "transaction:" + fmt.Sprint(x.Row), Kind: KindBillPayment, Origin: x.Origin,
		make: func(ctx context.Context, u storage.UnitOfWork, e *Env) (Made, error) {
			line, err := lineOf(ctx, u, m, x.Bank, lineRef(x.Row, ""))
			if err != nil {
				return Made{}, err
			}
			ref := strings.TrimSpace(x.Ref)
			doc, err := documentOf(ctx, u, e, "bill:"+strings.ToLower(strings.TrimSpace(x.Contact))+":"+ref)
			if err != nil {
				return Made{}, err
			}
			base, err := baseOf(ctx, u)
			if err != nil {
				return Made{}, err
			}
			d, err := billdoc.Read(doc, base)
			if err != nil {
				return Made{}, err
			}
			lineAmt, err := money.ParseStored(string(line.Amount), money.MustLookup(line.Currency))
			if err != nil {
				return Made{}, err
			}
			part := billpayment.Part{BillID: doc.ID, Amount: lineAmt.Neg(), InFull: d.Currency != lineAmt.Currency()}
			if part.InFull { // the bill's total in its currency, as the workbook settled it
				if part.Amount, err = w.totalOf(w.Bills, ref, x.Contact, d.Currency); err != nil {
					return Made{}, err
				}
			}
			svc := journal.BankTransactions{Store: e.Store, Clock: e.Clock}
			got, err := svc.MatchBills(ctx, e.Org, journal.BillMatch{LineID: line.ID, SupplierID: d.SupplierID, Currency: d.Currency,
				Bills: []billpayment.Part{part}, Basis: storage.BasisPerson, At: e.Now})
			return Made{Type: "document", ID: got.Document.ID}, err
		}}
}

// transferRecord imports money moved between two of the organisation's money accounts (FR-BNK-09), which the workbook
// keeps on one row: the row's line and its other side's, in one transfer.
func (w *Workbook) transferRecord(m Mapping, x Transaction, out bool) Record {
	return Record{Key: "transaction:" + fmt.Sprint(x.Row), Kind: KindTransfer, Origin: x.Origin,
		make: func(ctx context.Context, u storage.UnitOfWork, e *Env) (Made, error) {
			own, err := lineOf(ctx, u, m, x.Bank, lineRef(x.Row, ""))
			if err != nil {
				return Made{}, err
			}
			ref, from, to := lineRef(x.Row, "-from"), &uuid.UUID{}, &uuid.UUID{}
			if out {
				ref = lineRef(x.Row, "-to")
			}
			other, err := lineOf(ctx, u, m, x.Category, ref)
			if err != nil {
				return Made{}, err
			}
			if out {
				*from, *to = own.ID, other.ID
			} else {
				*from, *to = other.ID, own.ID
			}
			got, err := journal.BankTransactions{Store: e.Store, Clock: e.Clock}.RecordTransfer(ctx, e.Org, journal.TransferRequest{FromLineID: from, ToLineID: to,
				Date: x.Date, Basis: storage.BasisPerson, At: e.Now})
			return Made{Type: "bank_transfer", ID: got.Transfer.ID}, err
		}}
}

// categorisedRecord imports any other row as its line categorised (FR-BNK-06): its category, the GST code the workbook
// used, its spread, project and contact; a director's account's line names its person, and a SAFE's its instrument.
func (w *Workbook) categorisedRecord(m Mapping, x Transaction) Record {
	return Record{Key: "transaction:" + fmt.Sprint(x.Row), Kind: KindBankTransaction, Origin: x.Origin,
		make: func(ctx context.Context, u storage.UnitOfWork, e *Env) (Made, error) {
			line, err := lineOf(ctx, u, m, x.Bank, lineRef(x.Row, ""))
			if err != nil {
				return Made{}, err
			}
			acc, err := account(ctx, u, "category", m.accountOf(x.Category))
			if err != nil {
				return Made{}, err
			}
			values, err := projectOf(ctx, u, e, m, x.Project)
			if err != nil {
				return Made{}, err
			}
			part := banktransaction.Part{AccountID: acc.ID, GSTCode: m.gstOf(cmp.Or(x.GSTUsed, x.GST)), Description: x.Description, Values: values}
			if x.Spread != nil {
				if n, err := x.Spread.Int64(); err == nil && n > 1 {
					part.Months = int(n)
				}
			}
			d := banktransaction.Draft{LineID: line.ID, Reference: strings.TrimSpace(x.Ref), Parts: []banktransaction.Part{part}}
			if string(acc.Role) == ledger.RoleConvertibleInstruments { // the SAFE or note the money is for (FR-EQ-18)
				id, err := w.instrumentOf(e, x.Contact)
				if err != nil {
					return Made{}, err
				}
				d.InstrumentID = &id
			}
			if strings.TrimSpace(x.Contact) != "" {
				if id, err := contactOf(e, x.Contact); err == nil {
					d.ContactID = &id
				} else if personal(string(acc.Role)) {
					return Made{}, err
				}
			}
			got, err := journal.BankTransactions{Store: e.Store, Clock: e.Clock}.Categorise(ctx, e.Org, journal.Categorisation{Draft: d,
				Basis: storage.BasisPerson, At: e.Now})
			return Made{Type: "document", ID: got.Transaction.ID}, err
		}}
}

// totalOf is the total in its currency the workbook worked out for the invoice or bill numbered number (of contact,
// for a bill).
func (w *Workbook) totalOf(docs []Document, number, contact string, cur money.Currency) (money.Money, error) {
	for _, d := range docs {
		if strings.EqualFold(strings.TrimSpace(d.Number), number) && (contact == "" || strings.EqualFold(strings.TrimSpace(d.Contact), strings.TrimSpace(contact))) {
			return amount(d.Total, cur, false, "total")
		}
	}
	return money.Money{}, Refused{{Code: CodeImport, Field: "reference", Message: fmt.Sprintf("names %s, which the workbook has no row of", number),
		Fix: "Give the invoice or bill's number as the row's reference"}}
}
