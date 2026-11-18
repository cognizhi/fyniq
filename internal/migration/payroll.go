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

	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// KindPayroll is a month's pay run as one summary journal (design D7, rev 0.254).
const KindPayroll = "payroll"

// The Settings sheet's names of the system accounts payroll posts to.
const (
	sysEmployerCPF = "Employer CPF expense"
	sysSDL         = "SDL expense"
	sysSalaries    = "Salaries payable"
	sysCPF         = "CPF & SDL payable"
)

// valueOf is the value of the department or project dimension a workbook name maps onto, by its code: one the import
// created, or one the organisation had.
func valueOf(ctx context.Context, u storage.UnitOfWork, e *Env, kind, value string, maps []ValueMap, name string) ([]uuid.UUID, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil
	}
	i := slices.IndexFunc(maps, func(v ValueMap) bool { return v.Workbook == name })
	if i < 0 {
		return nil, Refused{{Code: CodeMapping, Field: kind, Message: fmt.Sprintf("names the %s %q, which the mapping does not have", kind, name),
			Fix: "Validate the workbook again: its mapping lists every " + kind}}
	}
	code := maps[i].Code
	if id, ok := e.Made(value + ":" + code); ok {
		return []uuid.UUID{id}, nil
	}
	d, err := dimensionOf(ctx, u, kind)
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
	return nil, Refused{{Code: CodeMapping, Field: kind, Message: fmt.Sprintf("the %s %q maps onto %s, which the organisation does not have", kind, name, code),
		Fix: "Map it onto a value the organisation has, or create it"}}
}

// payrollRecords are the Payroll sheet's months, each one summary journal on the month's last day (design D7): from
// the amounts the workbook calculated and paid, gross pay to each employee's salary account, employer CPF and SDL to
// their expense accounts, each by the employee's department, net pay to salaries payable, and employee and employer CPF,
// SDL and SHG to the CPF payable. The per-employee rows stay in the archived workbook (rev 0.254).
func (w *Workbook) payrollRecords(m Mapping) []dated {
	months := map[time.Time][]PayRow{}
	var order []time.Time
	for _, p := range w.Payroll {
		end := p.MonthEnd
		if end.IsZero() {
			end = time.Date(p.Month.Year(), p.Month.Month()+1, 0, 0, 0, 0, 0, time.UTC)
		}
		if _, ok := months[end]; !ok {
			order = append(order, end)
		}
		months[end] = append(months[end], p)
	}
	department := map[string]string{}
	for _, x := range w.Employees {
		department[strings.TrimSpace(x.Name)] = x.Department
	}
	sys := w.Settings.SystemAccounts
	var out []dated
	for _, end := range order {
		rows := months[end]
		out = append(out, dated{on: end, order: 3, Record: Record{Key: "payroll:" + end.Format("2006-01"), Kind: KindPayroll, Origin: rows[0].Origin,
			make: func(ctx context.Context, u storage.UnitOfWork, e *Env) (Made, error) {
				base, err := baseOf(ctx, u)
				if err != nil {
					return Made{}, err
				}
				type key struct{ account, department string }
				sums := map[key]*apd.Decimal{}
				var keys []key
				add := func(k key, x *apd.Decimal, neg bool) {
					if x == nil || x.IsZero() {
						return
					}
					if _, ok := sums[k]; !ok {
						sums[k] = new(apd.Decimal)
						keys = append(keys, k)
					}
					y := new(apd.Decimal).Set(x)
					if neg {
						y.Neg(y)
					}
					if _, err := apd.BaseContext.WithPrecision(40).Add(sums[k], sums[k], y); err != nil {
						panic(err)
					}
				}
				for _, p := range rows {
					dept := department[strings.TrimSpace(p.Employee)]
					add(key{p.SalaryAccount, dept}, p.Gross, false)
					add(key{sys[sysEmployerCPF], dept}, p.EmployerCPF, false)
					add(key{sys[sysSDL], dept}, p.SDL, false)
					add(key{sys[sysSalaries], ""}, p.Net, true)
					for _, x := range []*apd.Decimal{p.EmployeeCPF, p.EmployerCPF, p.SDL, p.SHG} {
						add(key{sys[sysCPF], ""}, x, true)
					}
				}
				slices.SortFunc(keys, func(a, b key) int {
					return cmp.Or(cmp.Compare(a.account, b.account), cmp.Compare(a.department, b.department))
				})
				d := journal.Draft{Date: end, Memo: "Payroll " + end.Format("January 2006") + " (imported from the workbook)"}
				for _, k := range keys {
					if k.account == "" {
						return Made{}, Refused{{Code: CodeImport, Field: "account", Message: "the workbook's Settings name no account for part of the pay run",
							Fix: "Fill in the system accounts on the Settings sheet"}}
					}
					acc, err := account(ctx, u, "account", m.accountOf(k.account))
					if err != nil {
						return Made{}, err
					}
					amt, err := amount(sums[k], base, false, "amount")
					if err != nil {
						return Made{}, err
					}
					values, err := valueOf(ctx, u, e, storage.DimensionDepartment, KindDepartment, m.Departments, k.department)
					if err != nil {
						return Made{}, err
					}
					d.Lines = append(d.Lines, ledger.JournalLine{AccountID: acc.ID, Amount: amt, Values: values,
						Description: strings.TrimSpace("Payroll " + end.Format("Jan 2006") + " " + k.department)})
				}
				svc := journal.Service{Store: e.Store}
				doc, err := svc.Create(ctx, e.Org, d)
				if err != nil {
					return Made{}, err
				}
				_, err = svc.Post(ctx, e.Org, doc.ID)
				return Made{Type: "document", ID: doc.ID}, err
			}}})
	}
	return out
}
