package migration

import (
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The parity (design D8) finds nothing when Fyniq's books are the workbook's own Trial Balance, and every figure that
// differs once they are not: a depreciation month a cent out proposed as a cent allocation (a), a GST control month as a
// rule change (b), an ordinary account's month with no class; and the month's closing and the ones after it with it.
// The checks compare by number: the worst of Fyniq's statuses against the workbook's, one not assessed never the same.
// req: FR-MIG-02
// req: QA-MIG-01
// req: data/migration Parity check (M4)
func TestTheParityFindsEveryFigureThatDiffers(t *testing.T) {
	w, err := Read(sample(t))
	if err != nil {
		t.Fatal(err)
	}
	m := w.Propose(standardTarget(t), nil)
	// Books as Fyniq's would be: each month's movement the change in the workbook's closing, which RND-IMPORT rounds once
	// each, so a month of the depreciation the workbook works in fractions of a cent moves a cent less than its rounded
	// movement (616.66 against 616.67), as the asset register posts it.
	books := Books{Opening: map[string]*apd.Decimal{}, Movement: map[string]map[time.Time]*apd.Decimal{}, Roles: map[string]string{}, Names: map[string]string{}}
	for _, a := range m.Accounts {
		books.Roles[a.Fyniq] = a.Role
	}
	for _, r := range w.TrialBalance {
		code := m.accountOf(r.Code)
		books.Opening[code] = add(books.Opening[code], r.Opening)
		if books.Movement[code] == nil {
			books.Movement[code] = map[time.Time]*apd.Decimal{}
		}
		before := r.Opening
		for i, end := range r.Months {
			if i < len(r.Closing) {
				books.Movement[code][firstOf(end)] = add(books.Movement[code][firstOf(end)], sub(r.Closing[i], before))
				before = r.Closing[i]
			}
		}
	}
	// The open items as the AR & AP sheet gives them, each contact by the ID the import gave it; the pay runs' lines as
	// the summary journals post them (design D7).
	books.Receivable, books.Payable, books.Contacts, books.ContactNames = map[string]*apd.Decimal{}, map[string]*apd.Decimal{}, map[string]string{}, map[string]string{}
	for _, side := range []struct {
		rows []OpenBalance
		to   map[string]*apd.Decimal
	}{{w.Receivables, books.Receivable}, {w.Payables, books.Payable}} {
		for _, o := range side.rows {
			id := "id-" + strings.ToLower(o.Contact)
			books.Contacts["contact:"+strings.ToLower(o.Contact)], books.ContactNames[id] = id, o.Contact
			side.to[id] = add(side.to[id], o.Total)
		}
	}
	sys := w.Settings.SystemAccounts
	salaries, cpf := m.accountOf(sys[sysSalaries]), m.accountOf(sys[sysCPF])
	books.PayrollLines = map[time.Time]map[string]*apd.Decimal{}
	for _, r := range w.Payroll {
		mo := firstOf(r.MonthEnd)
		if books.PayrollLines[mo] == nil {
			books.PayrollLines[mo] = map[string]*apd.Decimal{}
		}
		l := books.PayrollLines[mo]
		l[salaries] = sub(l[salaries], r.Net)
		l[cpf] = sub(sub(sub(sub(l[cpf], r.EmployeeCPF), r.EmployerCPF), r.SDL), r.SHG)
		l["6110"] = add(add(add(l["6110"], r.Gross), r.EmployerCPF), r.SDL)
	}
	p := w.Reconcile(m, storage.PathHistory, books)
	cents := map[string]string{}
	for _, d := range p.Differences {
		cents[d.Key()] = d.Difference.Text('f') + " " + d.Proposed
	}
	depreciation, accumulated := m.accountOf("6610"), m.accountOf("1590")
	if len(p.Differences) != 4 || cents[depreciation+".2026-06.movement"] != "-0.01 a" || cents[accumulated+".2026-09.movement"] != "0.01 a" ||
		p.Accounts == 0 || p.Figures != p.Accounts*(1+2*9)+len(w.Receivables)+len(w.Payables)+3*len(books.PayrollLines) ||
		!p.Through.Equal(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("the workbook against books like Fyniq's: %v, %d accounts, %d figures, through %s", cents, p.Accounts, p.Figures, p.Through)
	}
	march := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	nudge := func(code, by string) {
		d, _, _ := apd.NewFromString(by)
		books.Movement[code][march] = add(books.Movement[code][march], d)
	}
	gst, rent := m.accountOf("2100"), m.accountOf("6310")
	books.Roles[gst] = ledger.RoleGSTControl
	nudge(accumulated, "-0.01")
	nudge(gst, "-3.00")
	nudge(rent, "50.00")
	// A customer owing 100.00 more, and March's net pay 1.00 less than the Payroll sheet's.
	harbour := "id-" + strings.ToLower(w.Receivables[0].Contact)
	books.Receivable[harbour] = add(books.Receivable[harbour], apd.New(100, 0))
	books.PayrollLines[march][salaries] = add(books.PayrollLines[march][salaries], apd.New(1, 0))
	p = w.Reconcile(m, storage.PathHistory, books)
	got := map[string]string{}
	for _, d := range p.Differences {
		got[d.Key()] = d.Difference.Text('f') + " " + d.Proposed
	}
	// Each nudged account's March movement and its closings from March to September, 3 × (1 + 7), the 4 cents before, the
	// customer's open total and March's net pay.
	if len(p.Differences) != 30 || got["ar."+harbour] != "100.00 " || got["payroll.2026-03.net"] != "-1.00 " || got[accumulated+".2026-03.movement"] != "-0.01 a" || got[accumulated+".2026-09.closing"] != "-0.01 a" ||
		got[gst+".2026-03.movement"] != "-3.00 b" || got[rent+".2026-03.movement"] != "50.00 " || got[rent+".2026-02.closing"] != "" {
		t.Errorf("the differences: %v", got)
	}
	// The checks: the worst of Fyniq's against the workbook's; not assessed never the same; none of the number missing.
	cs := w.CompareChecks([]FyniqCheck{{Number: 1, Key: "books.a", Status: "ok"}, {Number: 2, Key: "books.b", Status: "ok"}, {Number: 2, Key: "books.c", Status: "error"},
		{Number: 3, Key: "books.d", Status: "not_assessed"}})
	for _, c := range cs {
		want := map[int]bool{1: true, 2: false, 3: false}[c.Number] || c.Number > 3
		if c.Same != want || c.Missing != (c.Number > 3) {
			t.Errorf("check %d: %+v", c.Number, c)
		}
	}
}
