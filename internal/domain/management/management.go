// Package management lays out the management reports from the workbook that the documents and the balance service
// give (FR-RPT-03; p1-reports task 4.1, design D6): sales by customer and by item, spend by supplier, and the top
// expenses, over a run of whole months, every figure in the base currency and read at one ledger revision (FR-RPT-08).
//
// Sales are what the invoices and credit notes posted in the run earned, net of GST: the lines of their entries on any
// account but the receivables, GST, deposits and FX controls, revenue spread over months included in full when it is
// invoiced. Spend is the same of the bills and suppliers' credit notes. A void or an edit posts a reversal, which counts
// in the month it posts, as in the P&L. Money received or paid at the bank without an invoice or bill, and expense
// claims, are in the P&L and the top expenses, not in these. [accountant to confirm]
package management

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/financials"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Kind is a management report.
type Kind string

// The management reports.
const (
	SalesByCustomer Kind = "sales_by_customer"
	SalesByItem     Kind = "sales_by_item"
	SpendBySupplier Kind = "spend_by_supplier"
	TopExpenses     Kind = "top_expenses"
)

// Top is how many expense accounts the top expenses name; the rest are one row.
const Top = 10

// Row is one customer's, item's, supplier's or expense account's figure. A row with no ID is the rest: the lines
// without an item, or the expenses after the top ten.
type Row struct {
	ID        uuid.UUID
	Code      string // an item's or account's code
	Name      string
	Amount    money.Money
	Share     string // its % of the total (RND-PERCENT); "" when the total is zero
	Quantity  string // by item: the quantity sold, net of credits
	Documents int    // the invoices, bills and credit notes behind it; 0 for the top expenses
}

// Report is a management report: its rows, largest first, and their total.
type Report struct {
	Kind     Kind
	Rows     []Row
	Total    money.Money
	Revision int64
}

// Request is a report over the periods All[First..Last], read as at Revision.
type Request struct {
	Kind        Kind
	All         []storage.Period
	First, Last int
	Revision    int64
}

// controls are the roles whose lines are not what a document earned or spent: the receivable or payable, the GST, the
// deposits it deducts, the tax withheld and the FX and rounding differences.
var controls = []string{ledger.RoleAccountsReceivable, ledger.RoleAccountsPayable, ledger.RoleGSTControl, ledger.RoleCustomerDeposits,
	ledger.RoleWithholdingTax, ledger.RoleFXRounding}

// Lay reads and lays out a management report in u (FR-RPT-03).
func Lay(ctx context.Context, u storage.UnitOfWork, base money.Currency, r Request) (Report, error) {
	out := Report{Kind: r.Kind, Revision: r.Revision, Total: money.Zero(base), Rows: []Row{}}
	var err error
	switch r.Kind {
	case TopExpenses:
		out.Rows, err = topExpenses(ctx, u, base, r)
	case SalesByCustomer, SalesByItem, SpendBySupplier:
		out.Rows, err = documents(ctx, u, base, r)
	default:
		return out, errors.New("management: no such report " + string(r.Kind))
	}
	if err != nil {
		return out, err
	}
	for _, x := range out.Rows {
		if out.Total, err = out.Total.Add(x.Amount); err != nil {
			return out, err
		}
	}
	for i := range out.Rows {
		if !out.Total.IsZero() {
			if out.Rows[i].Share, err = rounding.Percent(out.Rows[i].Amount, out.Total); err != nil {
				return out, err
			}
		}
	}
	return out, nil
}

// figure is one row as it adds up.
type figure struct {
	row  Row
	qty  money.Quantity
	docs map[uuid.UUID]bool
}

// documents adds up the entries of the invoices and credit notes, or of the bills and suppliers' credit notes, posted in
// the run as at the revision, by customer, item or supplier.
func documents(ctx context.Context, u storage.UnitOfWork, base money.Currency, r Request) ([]Row, error) {
	from, to := r.All[r.First].StartsOn, r.All[r.Last].EndsOn
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return nil, err
	}
	control := map[uuid.UUID]bool{}
	for _, a := range accounts {
		control[a.ID] = slices.Contains(controls, string(a.Role))
	}
	sales := r.Kind != SpendBySupplier
	sources := []string{string(storage.DocumentBill), string(storage.DocumentVendorCredit)}
	if sales {
		sources = []string{string(storage.DocumentInvoice), string(storage.DocumentCreditNote)}
	}
	by := map[uuid.UUID]*figure{}
	at := func(id uuid.UUID) *figure {
		f, ok := by[id]
		if !ok {
			f = &figure{row: Row{ID: id, Amount: money.Zero(base)}, docs: map[uuid.UUID]bool{}}
			by[id] = f
		}
		return f
	}
	err = balance.EachEntry(ctx, u, r.Revision, func(e storage.Entry) error {
		if e.PostingDate.Before(from) || e.PostingDate.After(to) || !slices.Contains(sources, e.Source.Type) {
			return nil
		}
		docID, err := uuid.Parse(e.Source.ID)
		if err != nil {
			return err
		}
		doc, err := u.Documents().Version(ctx, docID, e.Source.Version)
		if err != nil {
			return err
		}
		// What the document earned (a credit, so its sign turned) or spent (a debit), line by line.
		var earned []money.Money
		for _, l := range e.Lines {
			if control[l.AccountID] {
				continue
			}
			m, err := money.ParseStored(string(l.BaseAmount), base)
			if err != nil {
				return err
			}
			if sales {
				m = m.Neg()
			}
			earned = append(earned, m)
		}
		switch r.Kind {
		case SalesByItem:
			return byItem(doc, base, e, earned, at)
		case SalesByCustomer:
			d, err := invoice.Read(doc, base)
			if err != nil {
				return err
			}
			return add(at(d.CustomerID), docID, earned...)
		default:
			d, err := bill.Read(doc, base)
			if err != nil {
				return err
			}
			return add(at(d.SupplierID), docID, earned...)
		}
	})
	if err != nil {
		return nil, err
	}
	rows := make([]Row, 0, len(by))
	for id, f := range by {
		f.row.Documents = len(f.docs)
		if r.Kind == SalesByItem && id != uuid.Nil {
			f.row.Quantity = f.qty.String()
		}
		if err := name(ctx, u, r.Kind, &f.row); err != nil {
			return nil, err
		}
		rows = append(rows, f.row)
	}
	order(rows)
	return rows, nil
}

func add(f *figure, doc uuid.UUID, ms ...money.Money) error {
	for _, m := range ms {
		var err error
		if f.row.Amount, err = f.row.Amount.Add(m); err != nil {
			return err
		}
	}
	f.docs[doc] = true
	return nil
}

// byItem gives each line of an invoice's or credit note's entry to the item of its line on the document: the rule
// posts one line per document line, in order, beside the controls. An entry whose lines do not match its document's
// counts as lines without an item, so the total still agrees with the entries.
func byItem(doc storage.Document, base money.Currency, e storage.Entry, earned []money.Money, at func(uuid.UUID) *figure) error {
	d, err := invoice.Read(doc, base)
	if err != nil {
		return err
	}
	if len(d.Lines) != len(earned) {
		return add(at(uuid.Nil), doc.ID, earned...)
	}
	// A credit note takes back what it credits; so does a reversal. The quantity turns with them.
	turn := invoice.IsCreditNote(doc) != (e.ReversesEntryID != nil)
	for i, l := range d.Lines {
		id := uuid.Nil
		if l.ItemID != nil {
			id = *l.ItemID
		}
		f := at(id)
		if err := add(f, doc.ID, earned[i]); err != nil {
			return err
		}
		if turn {
			f.qty = f.qty.Sub(l.Quantity)
		} else {
			f.qty = f.qty.Add(l.Quantity)
		}
	}
	return nil
}

// name gives a row its customer's, supplier's or item's name: the rest has none.
func name(ctx context.Context, u storage.UnitOfWork, k Kind, r *Row) error {
	if r.ID == uuid.Nil {
		r.Name = "Lines without an item"
		return nil
	}
	if k == SalesByItem {
		it, err := u.Items().Get(ctx, r.ID)
		if errors.Is(err, ports.ErrNotFound) {
			r.Name = "An item no longer on file"
			return nil
		}
		r.Code, r.Name = it.Code, it.Name
		return err
	}
	c, err := u.Contacts().Get(ctx, r.ID)
	if errors.Is(err, ports.ErrNotFound) {
		r.Name = "A contact no longer on file"
		return nil
	}
	r.Name = c.LegalName
	return err
}

// order puts the largest figure first, then by name and code: the same rows always in the same order.
func order(rows []Row) {
	slices.SortFunc(rows, func(a, b Row) int {
		if c, err := b.Amount.Cmp(a.Amount); err == nil && c != 0 { // one currency: the base
			return c
		}
		return cmp.Or(strings.Compare(a.Name, b.Name), strings.Compare(a.Code, b.Code), strings.Compare(a.ID.String(), b.ID.String()))
	})
}

// topExpenses are the P&L's expense accounts over the run, largest first: the top ten, and the rest as one row.
func topExpenses(ctx context.Context, u storage.UnitOfWork, base money.Currency, r Request) ([]Row, error) {
	s, err := financials.Lay(ctx, u, base, financials.Request{Kind: financials.ProfitAndLoss, All: r.All, First: r.First, Last: r.Last,
		Revision: r.Revision})
	if err != nil {
		return nil, err
	}
	var rows []Row
	for _, b := range s.Blocks {
		for _, g := range b.Groups {
			for _, l := range g.Lines {
				if l.Account.Type != storage.Expense || l.Amounts[0].IsZero() {
					continue
				}
				rows = append(rows, Row{ID: l.Account.ID, Code: l.Account.Code, Name: l.Account.Name, Amount: l.Amounts[0]})
			}
		}
	}
	return top(rows, base)
}

// top orders rows, largest first, and keeps the first Top, the rest added up as one row.
func top(rows []Row, base money.Currency) ([]Row, error) {
	order(rows)
	if len(rows) <= Top {
		return rows, nil
	}
	rest := Row{Name: "All other expenses", Amount: money.Zero(base)}
	for _, x := range rows[Top:] {
		var err error
		if rest.Amount, err = rest.Amount.Add(x.Amount); err != nil {
			return nil, err
		}
	}
	return append(rows[:Top:Top], rest), nil
}
