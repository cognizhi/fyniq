package migration

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	billdoc "github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/budget"
	"github.com/cognizhi/fyniq/internal/domain/gst"
	invoicedoc "github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/domain/period"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The kinds of what both paths import after the history or the clean start (design D5, task 4.6; rev 0.263).
const (
	KindRecurringInvoice = "recurring_invoice"
	KindRecurringBill    = "recurring_bill"
	KindRecurringJournal = "recurring_journal"
	KindBudget           = "budget"
)

// The Recurring sheet's kinds.
const (
	recurringTransaction = "transaction"
	recurringInvoice     = "invoice"
	recurringBill        = "bill"
	recurringJournal     = "journal"
)

// maxRecurringName is the longest name a recurring invoice, bill or journal keeps.
const maxRecurringName = 120

// scheduleOf is a Recurring row's schedule in Fyniq (FR-AR-07, FR-GL-02): every 1, 3, 6 or 12 months on its first
// date's day of the month, or every 1 or 2 weeks for an invoice, until its last date; its first occurrence the first
// after the workbook's "as of", as the workbook made every one up to it (rev 0.263). False for a frequency Fyniq does
// not schedule, or a row whose last occurrence is on or before the "as of".
func (w *Workbook) scheduleOf(r Recurring) (storage.Schedule, bool) {
	s := storage.Schedule{DayOfMonth: r.First.Day(), StartsOn: r.First}
	switch strings.ToLower(strings.TrimSpace(r.Every)) {
	case "monthly":
		s.EveryMonths = 1
	case "quarterly":
		s.EveryMonths = 3
	case "half-yearly", "half yearly", "semi-annually", "every 6 months":
		s.EveryMonths = 6
	case "yearly", "annually", "annual":
		s.EveryMonths = 12
	case "weekly":
		s.EveryWeeks = 1
	case "fortnightly", "every 2 weeks":
		s.EveryWeeks = 2
	default:
		return s, false
	}
	if s.EveryWeeks > 0 && strings.ToLower(r.Kind) != recurringInvoice {
		return s, false
	}
	if !r.Last.IsZero() {
		last := r.Last
		s.EndsOn = &last
	}
	if r.First.IsZero() {
		return s, false
	}
	next := journal.FirstOn(s, w.Settings.AsOf.AddDate(0, 0, 1))
	if next == nil {
		return s, false
	}
	s.StartsOn = *next // an occurrence, so the ones after it fall as the row's do
	return s, true
}

// recurringName is a recurring invoice's, bill's or journal's name: the row's description, with its contact.
func recurringName(r Recurring) string {
	name := strings.TrimSpace(r.Description)
	if c := personName(r.Contact); c != "" {
		name = strings.TrimSpace(c + " – " + name)
	}
	name = cmp.Or(name, fmt.Sprintf("Recurring row %d", r.Row))
	if runes := []rune(name); len(runes) > maxRecurringName {
		name = string(runes[:maxRecurringName])
	}
	return name
}

// recurring are the Recurring sheet's active rows from the cut-over on (design D5, task 4.6; rev 0.263): an invoice
// row a recurring invoice making drafts, a bill row a recurring bill, a journal row a recurring journal, each first
// occurring after the workbook's "as of" with its contact, account, amount, GST code and project. The scheduler makes
// each occurrence's draft, which a person issues or posts (FR-AUT-02). A transaction row, which a bank feed brings, an
// inactive row and one Fyniq cannot schedule stay in the archive (the validation report warns of each).
func (w *Workbook) recurring(m Mapping) []Record {
	var out []Record
	for _, r := range w.Recurring {
		s, ok := w.scheduleOf(r)
		if !r.Active || !ok {
			continue
		}
		key := fmt.Sprintf("recurring:%d", r.Row)
		switch strings.ToLower(strings.TrimSpace(r.Kind)) {
		case recurringInvoice:
			out = append(out, Record{Key: key, Kind: KindRecurringInvoice, Origin: r.Origin,
				make: func(ctx context.Context, u storage.UnitOfWork, e *Env) (Made, error) {
					customer, err := contactOf(e, r.Contact)
					if err != nil {
						return Made{}, err
					}
					base, err := baseOf(ctx, u)
					if err != nil {
						return Made{}, err
					}
					cur, err := currencyOf(ctx, u, customer, base)
					if err != nil {
						return Made{}, err
					}
					acc, err := account(ctx, u, "category", m.accountOf(r.Account))
					if err != nil {
						return Made{}, err
					}
					values, err := projectOf(ctx, u, e, m, r.Project)
					if err != nil {
						return Made{}, err
					}
					price, err := money.ParseUnitPrice(textOf(r.Amount), cur, "amount")
					if err != nil {
						return Made{}, Refused{{Code: CodeValue, Field: "amount", Message: err.Error(), Fix: "Enter the amount before GST in the workbook"}}
					}
					l := invoicedoc.Line{Description: cmp.Or(strings.TrimSpace(r.Description), "Recurring invoice"), Quantity: mustQuantity("1"), UnitPrice: price,
						AccountID: acc.ID, GSTCode: m.salesGST(r.GST), Values: values}
					if l.GSTCode == "" {
						if l.GSTCode, err = defaultGST(ctx, u, acc.ID); err != nil {
							return Made{}, err
						}
					}
					x, err := journal.Invoices{Store: e.Store, Clock: e.Clock}.CreateRecurring(ctx, e.Org, journal.RecurringInvoiceInput{Name: recurringName(r), Schedule: s,
						Content: invoicedoc.Draft{CustomerID: customer, Currency: cur, Lines: []invoicedoc.Line{l}}})
					return Made{Type: "recurring_invoice", ID: x.ID}, err
				}})
		case recurringBill:
			out = append(out, Record{Key: key, Kind: KindRecurringBill, Origin: r.Origin,
				make: func(ctx context.Context, u storage.UnitOfWork, e *Env) (Made, error) {
					supplier, err := contactOf(e, r.Contact)
					if err != nil {
						return Made{}, err
					}
					base, err := baseOf(ctx, u)
					if err != nil {
						return Made{}, err
					}
					cur, err := currencyOf(ctx, u, supplier, base)
					if err != nil {
						return Made{}, err
					}
					acc, err := account(ctx, u, "category", m.accountOf(r.Account))
					if err != nil {
						return Made{}, err
					}
					values, err := projectOf(ctx, u, e, m, r.Project)
					if err != nil {
						return Made{}, err
					}
					net, err := amount(r.Amount, cur, false, "amount")
					if err != nil {
						return Made{}, err
					}
					code := m.purchaseGST(r.GST)
					if code == "" {
						if code, err = defaultGST(ctx, u, acc.ID); err != nil {
							return Made{}, err
						}
					}
					tax, err := supplierGST(ctx, u, code, net, s.StartsOn)
					if err != nil {
						return Made{}, err
					}
					l := billdoc.Line{AccountID: acc.ID, Description: cmp.Or(strings.TrimSpace(r.Description), "Recurring bill"), Net: net, GSTCode: code, GST: tax,
						Values: values}
					x, err := journal.Bills{Store: e.Store, Clock: e.Clock}.CreateRecurring(ctx, e.Org, journal.RecurringBillInput{Name: recurringName(r), Schedule: s,
						Content: billdoc.Draft{SupplierID: supplier, Currency: cur, Lines: []billdoc.Line{l}}})
					return Made{Type: "recurring_bill", ID: x.ID}, err
				}})
		case recurringJournal:
			out = append(out, Record{Key: key, Kind: KindRecurringJournal, Origin: r.Origin,
				make: func(ctx context.Context, u storage.UnitOfWork, e *Env) (Made, error) {
					base, err := baseOf(ctx, u)
					if err != nil {
						return Made{}, err
					}
					debit, err := account(ctx, u, "account", m.accountOf(r.Account))
					if err != nil {
						return Made{}, err
					}
					credit, err := account(ctx, u, "counter_account", m.accountOf(r.Counter))
					if err != nil {
						return Made{}, err
					}
					values, err := projectOf(ctx, u, e, m, r.Project)
					if err != nil {
						return Made{}, err
					}
					amt, err := amount(r.Amount, base, false, "amount")
					if err != nil {
						return Made{}, err
					}
					memo := cmp.Or(strings.TrimSpace(r.Description), "Recurring journal")
					x, err := journal.Service{Store: e.Store}.CreateRecurring(ctx, e.Org, journal.RecurringInput{Name: recurringName(r), Schedule: s,
						Content: journal.RecurringContent{Memo: memo, Lines: []ledger.JournalLine{
							{AccountID: debit.ID, Amount: amt, Description: memo, Values: values},
							{AccountID: credit.ID, Amount: amt.Neg(), Description: memo, Values: values}}}})
					return Made{Type: "recurring_journal", ID: x.ID}, err
				}})
		}
	}
	return out
}

// firstOf is the first day of t's month.
func firstOf(t time.Time) time.Time { return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC) }

// currencyOf is a contact's currency, or the base currency for one with none.
func currencyOf(ctx context.Context, u storage.UnitOfWork, contact uuid.UUID, base money.Currency) (money.Currency, error) {
	c, err := u.Contacts().Get(ctx, contact)
	if err != nil || c.Currency == "" {
		return base, err
	}
	return money.Lookup(c.Currency)
}

// supplierGST is the GST a supplier charges on net under a GST code on a day, as the workbook works it out: the
// standard rate of the sg-gst pack for a standard-rated code, else none, under RND-GST-EXCL. Each occurrence's draft
// keeps it until a person enters the tax invoice's own [accountant to confirm].
func supplierGST(ctx context.Context, u storage.UnitOfWork, code string, net money.Money, on time.Time) (money.Money, error) {
	v, err := gst.Version(ctx, u, on)
	if err != nil {
		return money.Money{}, err
	}
	c, ok := pack.Code(v, code)
	rate, known := pack.RateOn(v, on)
	if !ok || !c.StandardRated || !known {
		return money.Zero(net.Currency()), nil
	}
	sub, err := rounding.GSTExcl([]rounding.GSTNet{{Code: code, Rate: rate, Net: net}}, rounding.GSTPerLine)
	if err != nil {
		return money.Money{}, err
	}
	return sub[0].GST, nil
}

// budgetMonth is an account's month of the Budget sheet, by its first day.
type budgetMonth struct {
	account string
	month   time.Time
}

// budgets are the Budget sheet's plans (design D5, task 4.6; rev 0.263): for each financial year of the organisation
// that the sheet's months reach, from the books start on, an original budget recorded as the approved version. Each
// line is the account a P&L line maps onto (design D4), its amounts month by month in the base currency, signed as the
// P&L shows them: positive for income and costs, a discount negated [accountant to confirm]; the lines of one account
// added, and an account with no amount left out.
//
// A budgets file (FR-MIG-03; rev 0.272) names each line's account by its code, its amounts as the budget enters them,
// in the organisation's financial years; each year's budget is a draft, approved later as any budget is.
func (w *Workbook) budgets(m Mapping, path string) []Record {
	accountOf := map[string]string{}
	for _, l := range m.BudgetLines {
		accountOf[l.Line] = l.Account
	}
	if w.drafts {
		for _, b := range w.Budget {
			accountOf[b.Line] = codeOf(b.Line)
		}
	}
	sums := map[budgetMonth]*apd.Decimal{}
	var accounts []string
	seen := map[string]bool{}
	rowOf := map[string]int{} // each account's first row
	var origin Origin
	for _, b := range w.Budget {
		code := accountOf[b.Line]
		if code == "" || b.Year.IsZero() {
			continue
		}
		for i, x := range b.Months {
			if x == nil || x.IsZero() {
				continue
			}
			if origin.Sheet == "" {
				origin = b.Origin
			}
			if !seen[code] {
				seen[code] = true
				accounts = append(accounts, code)
				rowOf[code] = b.Row
			}
			k := budgetMonth{code, firstOf(b.Year).AddDate(0, i, 0)}
			if sums[k] == nil {
				sums[k] = new(apd.Decimal)
			}
			y := new(apd.Decimal).Set(x)
			if strings.Contains(strings.ToLower(b.Line), "discount") && !w.drafts {
				y.Neg(y)
			}
			if _, err := apd.BaseContext.WithPrecision(40).Add(sums[k], sums[k], y); err != nil {
				panic(err)
			}
		}
	}
	if len(sums) == 0 {
		return nil
	}
	cal := period.Calendar{BooksStart: w.BooksStart(path), FirstYearEnd: w.firstYearEnd(w.BooksStart(path))}
	if w.calendar != nil {
		cal = *w.calendar
	}
	years := map[int]bool{}
	var numbers []int
	var early time.Time
	for k := range sums {
		n := cal.Number(k.month.AddDate(0, 1, -1))
		if n == 0 && (early.IsZero() || k.month.Before(early)) {
			early = k.month
		}
		if n > 0 && !years[n] {
			years[n] = true
			numbers = append(numbers, n)
		}
	}
	slices.Sort(numbers)
	var out []Record
	if w.drafts && !early.IsZero() { // a file's months before the books start would be left out unsaid
		out = append(out, Record{Key: "budget:early", Kind: KindBudget, Origin: origin, make: func(context.Context, storage.UnitOfWork, *Env) (Made, error) {
			return Made{}, Refused{{Code: CodeValue, Field: "Financial year starting", Message: fmt.Sprintf("the budget's months from %s fall before the books start, %s",
				early.Format("Jan 2006"), cal.BooksStart.Format(time.DateOnly)), Fix: "Budget the financial years from the books start on"}}
		}})
	}
	for _, n := range numbers {
		y := cal.Year(n)
		out = append(out, Record{Key: "budget:" + y.StartsOn.Format(time.DateOnly), Kind: KindBudget, Origin: origin,
			make: func(ctx context.Context, u storage.UnitOfWork, e *Env) (Made, error) {
				base, err := baseOf(ctx, u)
				if err != nil {
					return Made{}, err
				}
				name := "Budget FY" + y.EndsOn.Format("2006") + " (from the workbook)"
				if w.drafts {
					name = "Budget FY" + y.EndsOn.Format("2006") + " (imported)"
				}
				b := budget.Budget{Name: name, Kind: budget.KindOriginal,
					Year: y.StartsOn.Format(time.DateOnly), Lines: []budget.BudgetLine{}}
				for _, code := range accounts {
					acc, err := account(ctx, u, "account", code)
					var refusals Refused
					if errors.As(err, &refusals) && w.drafts {
						refusals[0].Row, refusals[0].Field = rowOf[code], "P&L line"
					}
					if err != nil {
						return Made{}, err
					}
					l := budget.BudgetLine{AccountID: acc.ID.String()}
					some := false
					for _, month := range y.Months {
						amt, err := amount(sums[budgetMonth{code, firstOf(month.StartsOn)}], base, false, "amount")
						if err != nil {
							return Made{}, err
						}
						some = some || !amt.IsZero()
						l.Amounts = append(l.Amounts, amt.String())
					}
					if some {
						b.Lines = append(b.Lines, l)
					}
				}
				doc, err := budget.DraftIn(ctx, u, b)
				if err != nil || w.drafts { // a file's budget is a draft, approved later as any budget is
					return Made{Type: "document", ID: doc.ID}, err
				}
				doc, err = budget.RecordIn(ctx, u, doc.ID)
				return Made{Type: "document", ID: doc.ID}, err
			}})
	}
	return out
}
