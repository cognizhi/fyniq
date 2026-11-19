// Package differential runs a fyniq-payroll-run@1 document (test/differential/payroll-run.schema.json) through Nemura's payroll
// engine and writes the result in the shape the independent oracle writes (QA-ENG-02; p1-nemura-payroll 1.5): each person's
// employee-month, the run's totals and B.3's entry by account. It is test tooling and holds no rule of its own: the figures are the
// engine's.
package differential

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/domain/pack"
	nemurapacks "github.com/cognizhi/fyniq/internal/nemura/packs"
	"github.com/cognizhi/fyniq/internal/nemura/payroll"
)

// Schema is the run document's schema name.
const Schema = "fyniq-payroll-run@1"

// ResultSchema is the result's.
const ResultSchema = "fyniq-payroll-run-result@1"

// InvalidError is a document that is not a valid run (the runner exits with status 2).
type InvalidError struct{ Reason string }

func (e *InvalidError) Error() string { return e.Reason }

func invalid(format string, a ...any) error { return &InvalidError{Reason: fmt.Sprintf(format, a...)} }

type itemDoc struct {
	Key       string `json:"key"`
	Name      string `json:"name"`
	Amount    string `json:"amount"`
	CPFWages  bool   `json:"cpf_wages"`
	ForMonth  string `json:"for_month"`
	PayableOn string `json:"payable_on"`
	Suggested string `json:"suggested"`
}

type ytdDoc struct {
	OW        string `json:"ow"`
	AWSubject string `json:"aw_subject"`
	AWPaid    string `json:"aw_paid"`
}

type deductionDoc struct {
	Key        string `json:"key"`
	Kind       string `json:"kind"`
	Amount     string `json:"amount"`
	Consent    bool   `json:"consent"`
	Instalment int    `json:"instalment"`
	IsAdvance  bool   `json:"is_advance"`
}

type factsDoc struct {
	Items          []itemDoc      `json:"items"`
	YTD            *ytdDoc        `json:"ytd"`
	LastMonth      bool           `json:"last_month"`
	Reimbursements []string       `json:"reimbursements"`
	Deductions     []deductionDoc `json:"deductions"`
	Final          bool           `json:"final"`
	Held           string         `json:"held"`
}

type personDoc struct {
	Employee      *bool  `json:"employee"`
	Residency     string `json:"residency"`
	PRStart       string `json:"pr_start"`
	BirthDate     string `json:"birth_date"`
	JointElection bool   `json:"joint_election"`
	SHGFund       string `json:"shg_fund"`
	SHGOptOut     bool   `json:"shg_opt_out"`
}

type entryDoc struct {
	ID     string    `json:"id"`
	Person personDoc `json:"person"`
	factsDoc
	Posted *factsDoc `json:"posted"`
}

// RunDoc is a fyniq-payroll-run@1 document.
type RunDoc struct {
	Schema string            `json:"schema"`
	Name   string            `json:"name"`
	Month  string            `json:"month"`
	PayDay string            `json:"pay_day"`
	Packs  map[string]string `json:"packs"`
	People []entryDoc        `json:"people"`
}

// Parse reads a run document.
func Parse(b []byte) (RunDoc, error) {
	var d RunDoc
	if err := json.Unmarshal(b, &d); err != nil {
		return d, invalid("not JSON: %v", err)
	}
	if d.Schema != Schema {
		return d, invalid("schema is %q, want %s", d.Schema, Schema)
	}
	return d, nil
}

func date(s string) (time.Time, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return t, invalid("%q is not a date", s)
	}
	return t, nil
}

func money(s string) (*apd.Decimal, error) {
	d, _, err := apd.NewFromString(s)
	if err != nil {
		return nil, invalid("%q is not an amount", s)
	}
	return d, nil
}

func loadPacks(ids map[string]string) (payroll.Packs, error) {
	want := map[string]string{"cpf": "sg-cpf/2026.2", "sdl": "sg-sdl/2026.1", "shg": "sg-shg/2026.1"}
	for k, v := range ids {
		if _, ok := want[k]; !ok {
			return payroll.Packs{}, invalid("packs: %q is not cpf, sdl or shg", k)
		}
		pk, ver, found := cut(v, '@')
		if !found {
			return payroll.Packs{}, invalid("packs: %q is not pack@version", v)
		}
		want[k] = pk + "/" + ver
	}
	var out payroll.Packs
	for key, dst := range map[string]*pack.Version{"cpf": &out.CPF, "sdl": &out.SDL, "shg": &out.SHG} {
		v, err := pack.Load(nemurapacks.StatutoryFiles(), want[key])
		if err != nil {
			return out, invalid("pack %s: %v", want[key], err)
		}
		*dst = v
	}
	return out, nil
}

func cut(s string, sep byte) (string, string, bool) {
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			return s[:i], s[i+1:], true
		}
	}
	return s, "", false
}

func person(p personDoc) (payroll.Person, error) {
	out := payroll.Person{Employee: p.Employee == nil || *p.Employee, Residency: p.Residency, JointElection: p.JointElection, SHGFund: p.SHGFund, SHGOptOut: p.SHGOptOut}
	b, err := date(p.BirthDate)
	if err != nil {
		return out, err
	}
	out.BirthDate = b
	if p.PRStart != "" {
		s, err := date(p.PRStart)
		if err != nil {
			return out, err
		}
		out.PRStart = &s
	}
	return out, nil
}

func month(first, payDay time.Time, p payroll.Person, f factsDoc) (payroll.Month, error) {
	m := payroll.Month{Month: first, PayDay: payDay, Person: p, LastMonth: f.LastMonth, Final: f.Final}
	for _, it := range f.Items {
		amt, err := money(it.Amount)
		if err != nil {
			return m, err
		}
		item := payroll.Item{Key: it.Key, Name: it.Name, Amount: amt, CPFWages: it.CPFWages, Suggested: it.Suggested}
		if it.ForMonth != "" {
			if item.ForMonth, err = date(it.ForMonth); err != nil {
				return m, err
			}
		}
		if it.PayableOn != "" {
			if item.PayableOn, err = date(it.PayableOn); err != nil {
				return m, err
			}
		}
		m.Items = append(m.Items, item)
	}
	if f.YTD != nil {
		ow, err1 := money(f.YTD.OW)
		aws, err2 := money(f.YTD.AWSubject)
		awp, err3 := money(f.YTD.AWPaid)
		if err := errors.Join(err1, err2, err3); err != nil {
			return m, err
		}
		m.YTD = payroll.YearToDate{Known: true, OW: ow, AWSubject: aws, AWPaid: awp}
	}
	for _, r := range f.Reimbursements {
		d, err := money(r)
		if err != nil {
			return m, err
		}
		m.Reimbursements = append(m.Reimbursements, d)
	}
	for _, d := range f.Deductions {
		amt, err := money(d.Amount)
		if err != nil {
			return m, err
		}
		m.Deductions = append(m.Deductions, payroll.Deduction{Key: d.Key, Kind: d.Kind, Amount: amt, Consent: d.Consent, Instalment: d.Instalment, IsAdvance: d.IsAdvance})
	}
	if f.Held != "" {
		h, err := money(f.Held)
		if err != nil {
			return m, err
		}
		m.Held = h
	}
	return m, nil
}

func text(d *apd.Decimal) string {
	if d == nil {
		return "0"
	}
	return d.Text('f')
}

func figures(r payroll.Result) map[string]any {
	out := map[string]any{
		"gross": text(r.Gross), "ow_subject": text(r.OWSubject), "aw_subject": text(r.AWSubject),
		"cpf_total": text(r.CPF.Total), "cpf_employee": text(r.CPF.Employee), "cpf_employer": text(r.CPF.Employer),
		"sdl": text(r.SDL), "shg": text(r.SHG), "deductions": text(r.Deductions), "reimbursements": text(r.Reimbursements),
		"held": text(r.Held), "net": text(r.Net), "disburse": text(r.Disburse), "employer_cost": text(r.EmployerCost),
		"refund_wages": "0", "refund_total": "0", "refund_employee": "0",
	}
	if r.Refund != nil {
		out["refund_wages"], out["refund_total"], out["refund_employee"] = text(r.Refund.Wages), text(r.Refund.Total), text(r.Refund.Employee)
	}
	vs := []map[string]string{}
	for _, v := range r.DeductionViolations {
		vs = append(vs, map[string]string{"key": v.Key, "rule": v.Rule})
	}
	out["violations"] = vs
	return out
}

func errorCode(err error) string {
	if errors.Is(err, payroll.ErrNeedsYearToDate) {
		return "needs_year_to_date"
	}
	return "invalid: " + err.Error()
}

// Run works a run document out through the engine.
func Run(d RunDoc) (map[string]any, error) {
	first, err := date(d.Month)
	if err != nil {
		return nil, err
	}
	payDay := time.Date(first.Year(), first.Month()+1, 0, 0, 0, 0, 0, time.UTC)
	if d.PayDay != "" {
		if payDay, err = date(d.PayDay); err != nil {
			return nil, err
		}
	}
	packs, err := loadPacks(d.Packs)
	if err != nil {
		return nil, err
	}
	var people []map[string]any
	var counted []payroll.Result
	for _, e := range d.People {
		p, err := person(e.Person)
		if err != nil {
			return nil, err
		}
		m, err := month(first, payDay, p, e.factsDoc)
		if err != nil {
			return nil, err
		}
		res, err := payroll.Calculate(m, packs)
		if err != nil {
			people = append(people, map[string]any{"id": e.ID, "error": errorCode(err)})
			continue
		}
		counted = append(counted, res)
		shown := figures(res)
		shown["id"] = e.ID
		if e.Posted != nil {
			pm, err := month(first, payDay, p, *e.Posted)
			if err != nil {
				return nil, err
			}
			if before, err := payroll.Calculate(pm, packs); err == nil {
				diff := payroll.Diff(res, &before)
				shown["difference"] = map[string]any{
					"gross": text(diff.Gross), "cpf_employee": text(diff.EmployeeCPF), "cpf_employer": text(diff.EmployerCPF), "sdl": text(diff.SDL),
					"shg": text(diff.SHG), "deductions": text(diff.Deductions), "reimbursements": text(diff.Reimbursements), "net": text(diff.Net),
					"disburse": text(diff.Disburse),
				}
			}
		}
		people = append(people, shown)
	}
	t := payroll.Total(counted)
	lines := payroll.Postings(t)
	return map[string]any{
		"schema": ResultSchema,
		"people": people,
		"totals": map[string]any{
			"gross": text(t.Gross), "employee_cpf": text(t.EmployeeCPF), "employer_cpf": text(t.EmployerCPF), "sdl_employees": text(t.SDLEmployees),
			"sdl_payable": text(t.SDLPayable), "shg": text(t.SHG), "deductions": text(t.Deductions), "reimbursements": text(t.Reimbursements),
			"held": text(t.Held), "net": text(t.Net), "disburse": text(t.Disburse), "employer_cost": text(t.EmployerCost),
		},
		"postings": entry(lines),
		"lines":    rawLines(lines),
		"balanced": payroll.Balanced(lines),
	}, nil
}

func zero() *apd.Decimal { return apd.New(0, 0) }

func sum(a, b *apd.Decimal) *apd.Decimal {
	out := new(apd.Decimal)
	if _, err := apd.BaseContext.WithPrecision(60).Add(out, a, b); err != nil {
		panic(err)
	}
	return out
}

// entry groups B.3's lines the way the oracle does: the six accounts B.3 names are shown by code; every other debit is the salary
// expense (by department in Fyniq) and every other credit a deduction's own account.
func entry(lines []payroll.Line) map[string]any {
	known := map[string]bool{"6020": true, "6030": true, "2150": true, "1250": true, "2100": true, "2110": true}
	type side struct{ debit, credit *apd.Decimal }
	acc := map[string]*side{}
	for _, name := range []string{"salary", "6020", "6030", "2150", "1250", "deductions", "2100", "2110"} {
		acc[name] = &side{zero(), zero()}
	}
	for _, l := range lines {
		name := l.Account
		if !known[name] {
			if l.Debit != nil && l.Debit.Sign() != 0 {
				name = "salary"
			} else {
				name = "deductions"
			}
		}
		s := acc[name]
		if l.Debit != nil {
			s.debit = sum(s.debit, l.Debit)
		}
		if l.Credit != nil {
			s.credit = sum(s.credit, l.Credit)
		}
	}
	names := make([]string, 0, len(acc))
	for n := range acc {
		names = append(names, n)
	}
	sort.Strings(names)
	out := map[string]any{}
	for _, n := range names {
		out[n] = map[string]any{"debit": text(acc[n].debit), "credit": text(acc[n].credit)}
	}
	return out
}

// rawLines are the entry's lines as the engine writes them, for a person reading a disagreement (the comparison drops them).
func rawLines(lines []payroll.Line) []map[string]string {
	out := []map[string]string{}
	for _, l := range lines {
		out = append(out, map[string]string{"account": l.Account, "debit": text(l.Debit), "credit": text(l.Credit), "why": l.Why})
	}
	return out
}
