// Package payrollimport reads a payroll provider's journal export, or a file import, into the draft journal of a pay period (PRD-001 FR-PPL-05, FR-PAY-15; PRD-002 NM-EXT-02, NM-EXT-03; p1-payroll-link
// design D12), and says what a later version of the period changes. It also holds the payroll go-live gate: which of a month's statutory arrangements are still missing. The external system's
// approval cannot be verified, so what is read here is only ever a draft that a Controller or Accountant approves under Journals; no per-person pay is in it (FR-PAY-13): lines are by account and
// department.
package payrollimport

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The stable codes of an import's refusals (ENG-23).
const (
	CodeFile      = "IMP-01" // the file is not a readable CSV, or has no header or no row
	CodeColumn    = "IMP-02" // a column the mapping names is not in the file
	CodeAccount   = "IMP-03" // a row's account is not an account of the chart
	CodeAmount    = "IMP-04" // a row's amount is not dollars and cents, or has a debit and a credit
	CodeUnbalance = "IMP-05" // the file's debits and credits are not equal
	CodeDept      = "IMP-06" // a row's department is not a value of the Department dimension
	CodeTooMany   = "IMP-07" // more rows than an import holds
	CodePerson    = "IMP-08" // the file names a person: pay is kept by account and department (FR-PAY-13)
)

// MaxRows is how many rows one import holds.
const MaxRows = 5000

// Mapping names the file's columns by their header, as the provider writes them: what Fyniq reads of each. Department and Memo are optional.
type Mapping struct {
	Account    string `json:"account"`
	Department string `json:"department,omitempty"`
	Debit      string `json:"debit"`
	Credit     string `json:"credit"`
	Memo       string `json:"memo,omitempty"`
}

// Default is the mapping of a file written in Fyniq's own column names.
var Default = Mapping{Account: "account", Department: "department", Debit: "debit", Credit: "credit", Memo: "memo"}

// Resolver turns what a file names into what the chart holds.
type Resolver interface {
	Account(code string) (uuid.UUID, bool)
	Department(code string) (uuid.UUID, bool)
}

// Problem is one rule a row breaks, with its row (1 is the first row after the header) and no value from the file.
type Problem struct {
	Code, Message, Fix string
	Row                int
}

func (p Problem) Error() string { return fmt.Sprintf("%s row %d: %s", p.Code, p.Row, p.Message) }

// Line is one account and department's amount: signed, debit positive.
type Line struct {
	Account uuid.UUID
	Values  []uuid.UUID
	Amount  money.Money
	Memo    string
}

// Key is a line's account and values as the totals keep them.
func (l Line) Key() string {
	vs := make([]string, len(l.Values))
	for i, v := range l.Values {
		vs[i] = v.String()
	}
	slices.Sort(vs)
	return l.Account.String() + "|" + strings.Join(vs, ",")
}

// Result is a file read: its lines summed by account and department, the SHA-256 of the file and how many rows it had.
type Result struct {
	SHA256 string
	Rows   int
	Lines  []Line
}

// Read reads a CSV (UTF-8, comma, a header row) with the mapping. It refuses a file that names a person's column (FR-PAY-13), a row it cannot place, and a file whose debits and credits differ; every
// problem is returned at once (ENG-21), none carrying a value of the file.
func Read(data []byte, m Mapping, in Resolver, base money.Currency) (Result, []Problem) {
	sum := sha256.Sum256(data)
	out := Result{SHA256: hex.EncodeToString(sum[:])}
	r := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))))
	r.FieldsPerRecord = -1
	r.LazyQuotes = false
	head, err := r.Read()
	if err != nil || len(head) == 0 {
		return out, []Problem{{CodeFile, "the file has no header row", "Send a CSV with a header row", 0}}
	}
	col := map[string]int{}
	for i, h := range head {
		col[strings.ToLower(strings.TrimSpace(h))] = i
	}
	var probs []Problem
	for _, h := range head {
		switch strings.ToLower(strings.TrimSpace(h)) {
		case "employee", "employee name", "name", "nric", "fin", "employee id", "employee_id", "person":
			probs = append(probs, Problem{CodePerson, "the file has a column naming a person: send the journal by account and department, never per person", "Export the journal summary, not the payslips", 0})
		}
	}
	idx := func(name string, required bool) int {
		if name == "" {
			return -1
		}
		i, ok := col[strings.ToLower(strings.TrimSpace(name))]
		if !ok && required {
			probs = append(probs, Problem{CodeColumn, fmt.Sprintf("the file has no column %q", name), "Name the file's columns in the mapping", 0})
			return -1
		}
		if !ok {
			return -1
		}
		return i
	}
	ia, id, ic, idpt, imemo := idx(m.Account, true), idx(m.Debit, true), idx(m.Credit, true), idx(m.Department, false), idx(m.Memo, false)
	if len(probs) > 0 {
		return out, probs
	}
	sums := map[string]*Line{}
	var order []string
	debits, credits := money.Zero(base), money.Zero(base)
	for row := 1; ; row++ {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return out, append(probs, Problem{CodeFile, "the file is not a readable CSV", "Save it as CSV (UTF-8, comma separated)", row})
		}
		if row > MaxRows {
			return out, append(probs, Problem{CodeTooMany, fmt.Sprintf("more than %d rows", MaxRows), "Split the period's file, or send its summary", row})
		}
		out.Rows++
		cell := func(i int) string {
			if i < 0 || i >= len(rec) {
				return ""
			}
			return strings.TrimSpace(rec[i])
		}
		if strings.Join(rec, "") == "" {
			continue
		}
		acc, ok := in.Account(cell(ia))
		if !ok {
			probs = append(probs, Problem{CodeAccount, "the row's account is not an account of the chart", "Use the account's code from the chart", row})
			continue
		}
		var values []uuid.UUID
		if d := cell(idpt); d != "" {
			v, ok := in.Department(d)
			if !ok {
				probs = append(probs, Problem{CodeDept, "the row's department is not a value of the Department dimension", "Use a department's code, or leave it empty", row})
				continue
			}
			values = []uuid.UUID{v}
		}
		parse := func(s string) (money.Money, bool) {
			if s == "" {
				return money.Zero(base), true
			}
			v, err := money.Parse(strings.ReplaceAll(s, ",", ""), base)
			return v, err == nil && v.Sign() >= 0
		}
		dr, ok1 := parse(cell(id))
		cr, ok2 := parse(cell(ic))
		if !ok1 || !ok2 || (dr.Sign() != 0 && cr.Sign() != 0) || (dr.Sign() == 0 && cr.Sign() == 0) {
			probs = append(probs, Problem{CodeAmount, "the row's debit or credit is not dollars and cents, or the row has both or neither", "Give each row one amount, a debit or a credit, in dollars and cents", row})
			continue
		}
		amount := dr
		if cr.Sign() != 0 {
			amount = cr.Neg()
		}
		debits, credits = mustAdd(debits, dr), mustAdd(credits, cr)
		l := Line{Account: acc, Values: values, Amount: amount, Memo: cell(imemo)}
		k := l.Key()
		if cur, ok := sums[k]; ok {
			cur.Amount = mustAdd(cur.Amount, amount)
		} else {
			sums[k] = &l
			order = append(order, k)
		}
	}
	if out.Rows == 0 {
		probs = append(probs, Problem{CodeFile, "the file has no row", "Send the period's journal", 0})
	}
	if same, err := debits.Cmp(credits); err == nil && same != 0 && len(probs) == 0 {
		probs = append(probs, Problem{CodeUnbalance, "the file's debits and credits are not equal", "Check the export: a journal balances", 0})
	}
	sort.Strings(order)
	for _, k := range order {
		if sums[k].Amount.Sign() != 0 {
			out.Lines = append(out.Lines, *sums[k])
		}
	}
	return out, probs
}

func mustAdd(a, b money.Money) money.Money {
	s, err := a.Add(b)
	if err != nil {
		return a
	}
	return s
}

// Totals are lines as the period's totals keep them: signed amounts by account and department.
func Totals(lines []Line) map[string]string {
	out := map[string]string{}
	for _, l := range lines {
		out[l.Key()] = l.Amount.String()
	}
	return out
}

// Difference is what a version changes over the totals the period had after the version before: a line for each account and department whose total moved, by the amount it moved. A first version is
// its own lines. The lines are returned in a stable order.
func Difference(prior map[string]string, now []Line, base money.Currency) ([]Line, error) {
	var out []Line
	seen := map[string]bool{}
	for _, l := range now {
		k := l.Key()
		seen[k] = true
		was := money.Zero(base)
		if p, ok := prior[k]; ok {
			var err error
			if was, err = money.Parse(p, base); err != nil {
				return nil, err
			}
		}
		d, err := l.Amount.Sub(was)
		if err != nil {
			return nil, err
		}
		if d.Sign() != 0 {
			l.Amount = d
			out = append(out, l)
		}
	}
	for k, p := range prior {
		if seen[k] {
			continue
		}
		was, err := money.Parse(p, base)
		if err != nil {
			return nil, err
		}
		acc, vals, _ := strings.Cut(k, "|")
		id, err := uuid.Parse(acc)
		if err != nil {
			return nil, err
		}
		l := Line{Account: id, Amount: was.Neg()}
		for _, v := range strings.Split(vals, ",") {
			if v != "" {
				u, err := uuid.Parse(v)
				if err != nil {
					return nil, err
				}
				l.Values = append(l.Values, u)
			}
		}
		if l.Amount.Sign() != 0 {
			out = append(out, l)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key() < out[j].Key() })
	return out, nil
}

// The gate (FR-PAY-15, G4-009): the items that must each have an arrangement in force for a wage month before a pay run that pays a person is approved for payment.
var GateItems = []string{storage.ArrangePayslips, storage.ArrangeKETs, storage.ArrangeCPF, storage.ArrangeAIS, storage.ArrangeIR21}

// Missing are the items of the gate with no arrangement in force: each in GateItems that is absent from have.
func Missing(have []storage.PayrollArrangement) []string {
	got := map[string]bool{}
	for _, a := range have {
		got[a.Item] = true
	}
	var out []string
	for _, item := range GateItems {
		if !got[item] {
			out = append(out, item)
		}
	}
	return out
}

// Words are an item as a person reads it.
var Words = map[string]string{storage.ArrangePayslips: "itemised payslips", storage.ArrangeKETs: "key employment terms", storage.ArrangeCPF: "CPF, SDL and SHG submission and payment",
	storage.ArrangeAIS: "the Auto-Inclusion Scheme", storage.ArrangeIR21: "IR21 clearance"}
