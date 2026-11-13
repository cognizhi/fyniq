package migration

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	billdoc "github.com/cognizhi/fyniq/internal/domain/bill"
	invoicedoc "github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The history's kinds (design D5).
const (
	KindOpeningBalances = "opening_balances"
	KindInvoice         = "invoice"
	KindCreditNote      = "credit_note"
	KindBill            = "bill"
	KindVendorCredit    = "vendor_credit"
	KindJournal         = "journal"
)

// credit are the workbook's account types whose balances it gives on the credit side.
var credit = map[string]bool{"Liability": true, "Equity": true, "Revenue": true, "Other Income": true}

// dated is a history record and when it posts, so the plan imports them in date order.
type dated struct {
	Record
	on    time.Time
	order int // on one day: invoices and quotes, bills, journals, the money that moved, assets, then depreciation
}

// history is the current financial year's records (design D5): the opening balances at the books start with the
// opening open items, then the invoices, bills and journals in date order. Rows the workbook did not post (✖) were
// left out when it was read.
func (w *Workbook) history(m Mapping) []Record {
	var out []Record
	if r, ok := w.openingBalances(m); ok {
		out = append(out, r)
	}
	var items, rest []dated
	for _, d := range w.Invoices {
		x := dated{Record: w.invoiceRecord(m, d), on: d.Date, order: 1}
		if d.OpeningItem {
			items = append(items, x)
		} else {
			rest = append(rest, x)
		}
	}
	for _, d := range w.Bills {
		x := dated{Record: w.billRecord(m, d), on: d.Date, order: 2}
		if d.OpeningItem {
			items = append(items, x)
		} else {
			rest = append(rest, x)
		}
	}
	for _, j := range w.journals() {
		rest = append(rest, dated{Record: w.journalRecord(m, j), on: j[0].Date, order: 3})
	}
	statements, rows := w.transactions(m)
	out = append(out, statements...)
	rest = append(rest, rows...)
	rest = append(rest, w.payrollRecords(m)...)
	for _, q := range w.QuotesOrders {
		if x, ok := w.quoteRecord(m, q); ok {
			rest = append(rest, x)
		}
	}
	start := w.BooksStart(storage.PathHistory)
	for _, a := range w.Assets {
		rest = append(rest, w.assetRecord(m, a, start, false))
	}
	if len(w.Assets) > 0 {
		rest = append(rest, w.depreciationRecord())
	}
	byDate := func(a, b dated) int {
		return cmp.Or(a.on.Compare(b.on), cmp.Compare(a.order, b.order), cmp.Compare(a.Row, b.Row))
	}
	slices.SortStableFunc(items, byDate)
	slices.SortStableFunc(rest, byDate)
	for _, x := range append(items, rest...) {
		out = append(out, x.Record)
	}
	return out
}

// baseOf is the organisation's base currency.
func baseOf(ctx context.Context, u storage.UnitOfWork) (money.Currency, error) {
	o, err := u.Organisation(ctx)
	if err != nil {
		return money.Currency{}, err
	}
	return money.Lookup(o.BaseCurrency)
}

// amount is a decimal of the workbook as an amount in cur, negated when neg.
func amount(d *apd.Decimal, cur money.Currency, neg bool, field string) (money.Money, error) {
	x := new(apd.Decimal)
	if d != nil {
		x.Set(d)
	}
	if neg {
		x.Neg(x)
	}
	m, err := money.ParseField(x.Text('f'), cur, field)
	if err != nil {
		return m, Refused{{Code: CodeValue, Field: field, Message: fmt.Sprintf("%s is not an amount in %s", x.Text('f'), cur.Code),
			Fix: fmt.Sprintf("Enter it with at most %d decimals in the workbook", cur.MinorUnits)}}
	}
	return m, nil
}

// contactOf is the contact a workbook name became: a Contacts row, or one of the mapping's people.
func contactOf(e *Env, name string) (uuid.UUID, error) {
	key := strings.ToLower(strings.TrimSpace(name))
	if id, ok := e.Made("contact:" + key); ok {
		return id, nil
	}
	if id, ok := e.Made("person:" + strings.ToLower(personName(name))); ok {
		return id, nil
	}
	return uuid.Nil, Refused{{Code: CodeImport, Field: "contact", Message: fmt.Sprintf("names %q, who is neither a row of Contacts nor one of the mapping's people", name),
		Fix: "Add the contact to the workbook's Contacts sheet, or correct the name"}}
}

// projectOf is the project dimension's value a workbook project became, or none for no project.
func projectOf(ctx context.Context, u storage.UnitOfWork, e *Env, m Mapping, name string) ([]uuid.UUID, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil
	}
	i := slices.IndexFunc(m.Projects, func(v ValueMap) bool { return v.Workbook == name })
	if i < 0 {
		return nil, Refused{{Code: CodeMapping, Field: "project", Message: fmt.Sprintf("names the project %q, which the mapping does not have", name),
			Fix: "Validate the workbook again: its mapping lists every project"}}
	}
	code := m.Projects[i].Code
	if id, ok := e.Made(KindProject + ":" + code); ok {
		return []uuid.UUID{id}, nil
	}
	d, err := dimensionOf(ctx, u, storage.DimensionProject)
	if err != nil {
		return nil, err
	}
	values, err := u.Dimensions().Values(ctx, &d.ID)
	if err != nil {
		return nil, err
	}
	for _, v := range values {
		if v.Code == code {
			return []uuid.UUID{v.ID}, nil
		}
	}
	return nil, Refused{{Code: CodeMapping, Field: "project", Message: fmt.Sprintf("the project %q maps onto %s, which the organisation does not have", name, code),
		Fix: "Map the project onto a value the organisation has, or create it"}}
}

// rateOf is the rate a document in another currency posts at: the one the workbook used, so each amount is the
// workbook's (ENG-40); none for the base currency.
func rateOf(d Document, cur, base money.Currency) (*money.Rate, error) {
	if cur == base {
		return nil, nil
	}
	used := d.RateUsed
	if used == nil { // Carry Forward keeps the rate as typed
		used = d.Rate
	}
	if used == nil {
		return nil, Refused{{Code: CodeValue, Field: "fx_rate", Message: "the workbook used no rate for it", Fix: "Recalculate the workbook and save it again"}}
	}
	r, err := money.ParseRate(used.Text('f'), cur, base, "document")
	if err != nil {
		return nil, Refused{{Code: CodeValue, Field: "fx_rate", Message: err.Error(), Fix: "Enter the rate in Lists › FX rates, or on the row"}}
	}
	return &r, nil
}

// origin is what a director account's line enters as where its amount came from (FR-EQ-01): a credit is money lent,
// or pay deferred on deferred remuneration; a debit is money paid out to the person.
func origin(role storage.AccountRole, amt money.Money) string {
	switch {
	case amt.Sign() < 0 && string(role) == ledger.RoleDeferredRemuneration:
		return ledger.OriginPay
	case amt.Sign() < 0:
		return ledger.OriginLent
	}
	return ledger.OriginPaidOut
}

// openingBalances is the opening balances at the books start (FR-GL-06): each account's balance on the Accounts sheet,
// the receivables and payables the opening items carry on 3900 Opening balance clearing instead of 1100 and 2010
// (rev 0.257), and a director's account's balance in its person's name (B.7). None when no account has one.
func (w *Workbook) openingBalances(m Mapping) (Record, bool) {
	var rows []Account
	for _, a := range w.Accounts {
		if a.Opening != nil && !a.Opening.IsZero() {
			rows = append(rows, a)
		}
	}
	return w.balancesRecord(m, rows, "Accounts")
}

// carriedBalances is a clean start's opening balances at the cut-over (§17.1 M3): the Carry Forward sheet's balances,
// on each account's side as the Accounts sheet gives them.
func (w *Workbook) carriedBalances(m Mapping) (Record, bool) {
	accounts := map[string]Account{}
	for _, a := range w.Accounts {
		accounts[a.Code] = a
	}
	var rows []Account
	for _, b := range w.CarryForward.Balances {
		if b.Balance == nil || b.Balance.IsZero() {
			continue
		}
		a := accounts[b.Code]
		a.Code, a.Origin, a.Opening = b.Code, b.Origin, b.Balance
		rows = append(rows, a)
	}
	return w.balancesRecord(m, rows, "Carry Forward")
}

// balancesRecord posts rows' balances as the opening balances at the books start.
func (w *Workbook) balancesRecord(m Mapping, rows []Account, sheet string) (Record, bool) {
	if len(rows) == 0 {
		return Record{}, false
	}
	persons := map[string]string{}
	for _, a := range m.Accounts {
		persons[a.Workbook] = a.OpeningPerson
	}
	return Record{Key: "opening-balances", Kind: KindOpeningBalances, Origin: Origin{Sheet: sheet},
		make: func(ctx context.Context, u storage.UnitOfWork, e *Env) (Made, error) {
			base, err := baseOf(ctx, u)
			if err != nil {
				return Made{}, err
			}
			accounts, err := u.Accounts().List(ctx)
			if err != nil {
				return Made{}, err
			}
			byCode, byRole := map[string]storage.Account{}, map[storage.AccountRole]storage.Account{}
			for _, a := range accounts {
				byCode[a.Code] = a
				if a.Role != "" {
					byRole[a.Role] = a
				}
			}
			var lines []ledger.JournalLine
			for _, a := range rows {
				acc, ok := byCode[m.accountOf(a.Code)]
				if !ok {
					return Made{}, Refused{{Code: CodeMapping, Field: "account", Message: fmt.Sprintf("account %s maps onto %s, which the organisation does not have", a.Code, m.accountOf(a.Code)),
						Fix: "Map it onto an account of the chart, and sign the mapping again"}}
				}
				amt, err := amount(a.Opening, base, credit[a.Type], "opening_balance")
				if err != nil {
					return Made{}, err
				}
				line := ledger.JournalLine{AccountID: acc.ID, Amount: amt, Description: "Opening balance of " + a.Code + " " + a.Name}
				switch string(acc.Role) {
				case ledger.RoleAccountsReceivable, ledger.RoleAccountsPayable: // carried by the opening items (rev 0.257)
					clearing, ok := byRole[storage.AccountRole(ledger.RoleOpeningClearing)]
					if !ok {
						return Made{}, Refused{{Code: CodeMapping, Field: "account", Message: "the chart has no opening balance clearing account (3900) to carry the receivables and payables",
							Fix: "Add 3900 Opening balance clearing, with its role, to the chart"}}
					}
					line.AccountID, line.Description = clearing.ID, "Opening "+a.Name+", carried by the opening items"
				case ledger.RoleConvertibleInstruments: // a line per SAFE or note of the Funding sheet, naming it (FR-EQ-18; rev 0.262)
					split, err := w.byInstrument(e, line, amt)
					if err != nil {
						return Made{}, err
					}
					lines = append(lines, split...)
					continue
				case ledger.RoleDirectorLoan, ledger.RoleDeferredRemuneration, ledger.RoleShareApplicationMonies:
					person, err := contactOf(e, persons[a.Code])
					if err != nil {
						return Made{}, err
					}
					line.Party = person
					if string(acc.Role) != ledger.RoleShareApplicationMonies {
						line.Origin = origin(acc.Role, amt)
					}
				}
				lines = append(lines, line)
			}
			svc := journal.Service{Store: e.Store}
			doc, err := svc.SaveOpening(ctx, e.Org, "The opening balances of the workbook (FR-MIG-01)", lines)
			if err != nil {
				return Made{}, err
			}
			_, err = svc.PostOpening(ctx, e.Org)
			return Made{Type: "document", ID: doc.ID}, err
		}}, true
}

// byInstrument splits an opening balance of convertible instruments into a line for each SAFE or note of the Funding
// sheet, each its amount, naming the instrument its record made (FR-EQ-18): refused when they do not add up to it.
func (w *Workbook) byInstrument(e *Env, line ledger.JournalLine, total money.Money) ([]ledger.JournalLine, error) {
	var out []ledger.JournalLine
	sum := money.Zero(total.Currency())
	for _, x := range w.Instruments {
		id, ok := e.Made(fmt.Sprintf("instrument:%d", x.Row))
		if !ok {
			continue
		}
		amt, err := amount(x.Amount, total.Currency(), total.Sign() < 0, "amount")
		if err != nil {
			return nil, err
		}
		if sum, err = sum.Add(amt); err != nil {
			return nil, err
		}
		l := line
		l.Amount, l.Instrument, l.Description = amt, id, line.Description+", "+personName(x.Investor)
		out = append(out, l)
	}
	if c, err := sum.Cmp(total); err != nil || c != 0 {
		if total.Sign() < 0 {
			sum, total = sum.Neg(), total.Neg()
		}
		return nil, Refused{{Code: CodeImport, Field: "opening_balance", Message: fmt.Sprintf("the SAFEs and notes of the Funding sheet add up to %s, not the %s the account carries",
			sum, total), Fix: "Enter every SAFE and note the account holds on the Funding sheet, each with its amount"}}
	}
	return out, nil
}

// docLines are the Doc Lines of a document's number; none for no number.
func (w *Workbook) docLines(number string) []DocLine {
	if strings.TrimSpace(number) == "" {
		return nil
	}
	var out []DocLine
	for _, l := range w.DocLines {
		if strings.EqualFold(strings.TrimSpace(l.Document), strings.TrimSpace(number)) {
			out = append(out, l)
		}
	}
	return out
}

// salesLines are an invoice's, quote's or order's lines (FR-AR-01, FR-AR-03): its Doc Lines, each with the workbook's
// resolved description, quantity and price, its discount, and its item's revenue account; or one line of its amount
// before GST. spread, when given, spreads each line as the document says.
func (w *Workbook) salesLines(ctx context.Context, u storage.UnitOfWork, e *Env, number string, net *apd.Decimal, description string, accountID uuid.UUID,
	gst string, values []uuid.UUID, cur money.Currency, spread func(*invoicedoc.Line)) ([]invoicedoc.Line, error) {
	if spread == nil {
		spread = func(*invoicedoc.Line) {}
	}
	lines := w.docLines(number)
	if len(lines) == 0 {
		price, err := money.ParseUnitPrice(textOf(net), cur, "amount")
		if err != nil {
			return nil, Refused{{Code: CodeValue, Field: "amount", Message: err.Error(), Fix: "Enter the amount before GST in the workbook"}}
		}
		l := invoicedoc.Line{Description: description, Quantity: mustQuantity("1"), UnitPrice: price, AccountID: accountID, GSTCode: gst, Values: values}
		spread(&l)
		if l.GSTCode == "" {
			if l.GSTCode, err = defaultGST(ctx, u, l.AccountID); err != nil {
				return nil, err
			}
		}
		return []invoicedoc.Line{l}, nil
	}
	var out []invoicedoc.Line
	for _, x := range lines {
		l := invoicedoc.Line{Description: cmp.Or(x.DescriptionUsed, x.Description, description), Discount: percent(x.Discount), AccountID: accountID,
			GSTCode: gst, Values: values}
		if x.Item != "" {
			id, ok := e.Made("item:" + x.Item)
			if !ok {
				return nil, Refused{{Code: CodeImport, Field: "item", Message: fmt.Sprintf("Doc Lines row %d names the item %s, which is not imported", x.Row, x.Item),
					Fix: "Add the item to the Items sheet"}}
			}
			l.ItemID = &id
			if it, err := u.Items().Get(ctx, id); err == nil {
				if it.RevenueAccountID != nil {
					l.AccountID = *it.RevenueAccountID
				}
				l.GSTCode = cmp.Or(l.GSTCode, it.GSTCode)
			}
		}
		var err error
		if l.GSTCode == "" {
			if l.GSTCode, err = defaultGST(ctx, u, l.AccountID); err != nil {
				return nil, err
			}
		}
		if l.Quantity, err = money.ParseQuantity(textOf(cmp.Or(x.QuantityUsed, x.Quantity, apd.New(1, 0))), "quantity"); err != nil {
			return nil, Refused{{Code: CodeValue, Field: "quantity", Message: fmt.Sprintf("Doc Lines row %d: %v", x.Row, err), Fix: "Enter the quantity in the workbook"}}
		}
		if l.UnitPrice, err = money.ParseUnitPrice(textOf(cmp.Or(x.PriceUsed, x.Price)), cur, "unit_price"); err != nil {
			return nil, Refused{{Code: CodeValue, Field: "unit_price", Message: fmt.Sprintf("Doc Lines row %d: %v", x.Row, err), Fix: "Enter the unit price in the workbook"}}
		}
		spread(&l)
		out = append(out, l)
	}
	return out, nil
}

// defaultGST is the GST code a line on an account starts with, as the workbook falls back to its category's.
func defaultGST(ctx context.Context, u storage.UnitOfWork, account uuid.UUID) (string, error) {
	if account == uuid.Nil {
		return "", nil
	}
	a, err := u.Accounts().Get(ctx, account)
	return a.DefaultGSTCode, err
}

// percent is a workbook discount, a fraction such as 0.1, as the percentage a line keeps ("10").
func percent(d *apd.Decimal) string {
	if d == nil || d.IsZero() {
		return ""
	}
	x := new(apd.Decimal)
	if _, err := apd.BaseContext.WithPrecision(30).Mul(x, d, apd.New(100, 0)); err != nil {
		return ""
	}
	x.Reduce(x)
	return x.Text('f')
}

// invoiceRecord imports an invoice (FR-AR-03): its Doc Lines, or one line of its amount; its spread; the rate and the
// due date the workbook used; its project. It keeps the workbook's number (rev 0.259) and posts on its date, or at the
// books start as an opening item (rev 0.257); a draft stays a draft.
func (w *Workbook) invoiceRecord(m Mapping, d Document) Record {
	// A row of a negative amount is a credit note to the customer (FR-AR-04), entered with positive amounts.
	creditNote := d.Net != nil && d.Net.Sign() < 0
	kind, typ := KindInvoice, storage.DocumentInvoice
	if creditNote {
		kind, typ = KindCreditNote, storage.DocumentCreditNote
	}
	return Record{Key: kind + ":" + strings.TrimSpace(d.Number), Kind: kind, Origin: d.Origin,
		make: func(ctx context.Context, u storage.UnitOfWork, e *Env) (Made, error) {
			base, err := baseOf(ctx, u)
			if err != nil {
				return Made{}, err
			}
			cur := base
			if d.Currency != "" {
				if cur, err = money.Lookup(d.Currency); err != nil {
					return Made{}, err
				}
			}
			customer, err := contactOf(e, d.Contact)
			if err != nil {
				return Made{}, err
			}
			rate, err := rateOf(d, cur, base)
			if err != nil {
				return Made{}, err
			}
			values, err := projectOf(ctx, u, e, m, d.Project)
			if err != nil {
				return Made{}, err
			}
			category, err := account(ctx, u, "revenue_category", m.accountOf(d.Category))
			if err != nil {
				return Made{}, err
			}
			gst := m.salesGST(cmp.Or(d.GSTUsed, d.GST))
			draft := invoicedoc.Draft{CustomerID: customer, Date: d.Date, DueDate: d.Due, Currency: cur, Rate: rate, Notes: d.Notes, OpeningItem: d.OpeningItem}
			spread := func(l *invoicedoc.Line) {
				if d.ServiceMonths != nil {
					if n, err := d.ServiceMonths.Int64(); err == nil && n > 1 {
						l.SpreadMonths, l.SpreadFrom = int(n), d.ServiceStart
					}
				}
			}
			net := d.Net
			if creditNote {
				net = new(apd.Decimal).Neg(d.Net)
			}
			itemised := d.Number // a balance the Carry Forward sheet brings forward is one line of its amount, never the invoice's Doc Lines
			if d.Sheet == "Carry Forward" {
				itemised = ""
			}
			if draft.Lines, err = w.salesLines(ctx, u, e, itemised, net, cmp.Or(d.Description, "Invoice "+d.Number), category.ID, gst, values, cur, spread); err != nil {
				return Made{}, err
			}
			svc := journal.Invoices{Store: e.Store, Clock: e.Clock}
			create := svc.Create
			if creditNote {
				create, draft.DueDate = svc.CreateCredit, time.Time{} // a credit note falls due on nothing
			}
			doc, err := create(ctx, e.Org, draft)
			if err != nil {
				return Made{}, err
			}
			if _, err := u.Sequences().Keep(ctx, doc.ID, typ, strings.TrimSpace(d.Number)); err != nil {
				return Made{}, err
			}
			if !d.Draft {
				if _, err := svc.Post(ctx, e.Org, doc.ID); err != nil {
					return Made{}, err
				}
			}
			return Made{Type: "document", ID: doc.ID}, nil
		}}
}

// billRecord imports a bill (FR-AP-02): one line of its amount and the GST the workbook worked out, its spread, the rate
// and the due date the workbook used; the workbook's number as the supplier's reference (rev 0.259). It posts on its date,
// or at the books start as an opening item; a draft stays a draft.
func (w *Workbook) billRecord(m Mapping, d Document) Record {
	// A row of a negative amount is the supplier's credit note (FR-AP-03), entered with positive amounts.
	credit := d.Net != nil && d.Net.Sign() < 0
	kind := KindBill
	if credit {
		kind = KindVendorCredit
	}
	return Record{Key: kind + ":" + strings.ToLower(strings.TrimSpace(d.Contact)) + ":" + strings.TrimSpace(d.Number), Kind: kind, Origin: d.Origin,
		make: func(ctx context.Context, u storage.UnitOfWork, e *Env) (Made, error) {
			base, err := baseOf(ctx, u)
			if err != nil {
				return Made{}, err
			}
			cur := base
			if d.Currency != "" {
				if cur, err = money.Lookup(d.Currency); err != nil {
					return Made{}, err
				}
			}
			supplier, err := contactOf(e, d.Contact)
			if err != nil {
				return Made{}, err
			}
			rate, err := rateOf(d, cur, base)
			if err != nil {
				return Made{}, err
			}
			values, err := projectOf(ctx, u, e, m, d.Project)
			if err != nil {
				return Made{}, err
			}
			category, err := account(ctx, u, "category", m.accountOf(d.Category))
			if err != nil {
				return Made{}, err
			}
			net, err := amount(d.Net, cur, credit, "amount")
			if err != nil {
				return Made{}, err
			}
			gst := money.Zero(cur)
			if d.Total != nil {
				total, err := amount(d.Total, cur, credit, "total")
				if err != nil {
					return Made{}, err
				}
				if gst, err = total.Sub(net); err != nil {
					return Made{}, err
				}
			}
			l := billdoc.Line{AccountID: category.ID, Description: cmp.Or(d.Description, "Bill "+d.Number), Net: net, GSTCode: m.purchaseGST(cmp.Or(d.GSTUsed, d.GST)),
				GST: gst, Values: values}
			if d.ServiceMonths != nil {
				if n, err := d.ServiceMonths.Int64(); err == nil && n > 1 {
					l.SpreadMonths, l.SpreadFrom = int(n), d.ServiceStart
				}
			}
			draft := billdoc.Draft{SupplierID: supplier, SupplierRef: strings.TrimSpace(d.Number), Date: d.Date, DueDate: d.Due, Currency: cur, Rate: rate,
				Lines: []billdoc.Line{l}, Notes: d.Notes, OpeningItem: d.OpeningItem}
			if c, err := u.Contacts().Get(ctx, supplier); err != nil {
				return Made{}, err
			} else if billdoc.NonResident(c) { // the Accountant's choice in the mapping (FR-AP-19)
				for _, n := range m.Natures {
					if strings.EqualFold(n.Contact, strings.TrimSpace(d.Contact)) {
						draft.Nature = n.Nature
					}
				}
			}
			svc := journal.Bills{Store: e.Store, Clock: e.Clock}
			create := svc.Create
			if credit {
				create, draft.DueDate = svc.CreateCredit, time.Time{} // a credit falls due on nothing
			}
			doc, err := create(ctx, e.Org, draft)
			if err != nil {
				return Made{}, err
			}
			if !d.Draft {
				if _, err := svc.Post(ctx, e.Org, doc.ID); err != nil {
					return Made{}, err
				}
			}
			return Made{Type: "document", ID: doc.ID}, nil
		}}
}

// journals are the Journal sheet's entries: its lines by entry, in the order of their first row.
func (w *Workbook) journals() [][]JournalLine {
	var out [][]JournalLine
	at := map[string]int{}
	for _, l := range w.Journal {
		key := strings.TrimSpace(l.Entry)
		i, ok := at[key]
		if !ok {
			i = len(out)
			at[key] = i
			out = append(out, nil)
		}
		out[i] = append(out[i], l)
	}
	return out
}

// journalRecord imports a journal entry (FR-GL-01) on its first line's date: each line's account, amount, description
// and project, a director's account's line in its person's name; it keeps the workbook's number (rev 0.259) and posts.
func (w *Workbook) journalRecord(m Mapping, lines []JournalLine) Record {
	first := lines[0]
	return Record{Key: "journal:" + strings.TrimSpace(first.Entry), Kind: KindJournal, Origin: first.Origin,
		make: func(ctx context.Context, u storage.UnitOfWork, e *Env) (Made, error) {
			base, err := baseOf(ctx, u)
			if err != nil {
				return Made{}, err
			}
			d := journal.Draft{Date: first.Date, Memo: cmp.Or(first.Description, "Journal "+first.Entry)}
			for _, l := range lines {
				acc, err := account(ctx, u, "account", m.accountOf(l.Account))
				if err != nil {
					return Made{}, err
				}
				dr, err := amount(l.Debit, base, false, "debit")
				if err != nil {
					return Made{}, err
				}
				cr, err := amount(l.Credit, base, false, "credit")
				if err != nil {
					return Made{}, err
				}
				amt, err := dr.Sub(cr)
				if err != nil {
					return Made{}, err
				}
				values, err := projectOf(ctx, u, e, m, l.Project)
				if err != nil {
					return Made{}, err
				}
				line := ledger.JournalLine{AccountID: acc.ID, Amount: amt, Description: l.Description, Values: values}
				if personal(string(acc.Role)) {
					if line.Party, err = contactOf(e, l.Contact); err != nil {
						return Made{}, err
					}
					if string(acc.Role) != ledger.RoleShareApplicationMonies {
						line.Origin = origin(acc.Role, amt)
					}
				}
				d.Lines = append(d.Lines, line)
			}
			svc := journal.Service{Store: e.Store}
			doc, err := svc.Create(ctx, e.Org, d)
			if err != nil {
				return Made{}, err
			}
			if _, err := u.Sequences().Keep(ctx, doc.ID, storage.DocumentJournal, strings.TrimSpace(first.Entry)); err != nil {
				return Made{}, err
			}
			_, err = svc.Post(ctx, e.Org, doc.ID)
			return Made{Type: "document", ID: doc.ID}, err
		}}
}

// textOf is a decimal's text; "0" for none.
func textOf(d *apd.Decimal) string {
	if d == nil {
		return "0"
	}
	return d.Text('f')
}

func mustQuantity(s string) money.Quantity {
	q, err := money.ParseQuantity(s, "quantity")
	if err != nil {
		panic(err)
	}
	return q
}
