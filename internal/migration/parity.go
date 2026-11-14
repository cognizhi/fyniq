package migration

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The kinds of a parity figure (design D8): an account's opening at the books start, its movement in a month, and its
// closing at the month's end.
const (
	FigureOpening  = "opening"
	FigureMovement = "movement"
	FigureClosing  = "closing"
)

// CodeRegister is a decision or a sign-off the register refuses: a line the report does not have, a workbook defect
// without a note, or a sign-off with a line undecided, Fyniq's checks never run, or the report signed already.
const CodeRegister = "MIGRATION-15"

// The classes of a difference (FR-MIG-02, design D8): a, a cent allocation; b, a deliberate rule change; c, a defect of
// the workbook.
const (
	ClassAllocation = "a"
	ClassRule       = "b"
	ClassDefect     = "c"
)

// Books are Fyniq's figures for the parity, each account's by its Fyniq code in the base currency, debit positive: its
// opening at the books start (what the opening balances and the opening items post), and its movement in each month
// from the books start, the opening left out.
type Books struct {
	Opening  map[string]*apd.Decimal
	Movement map[string]map[time.Time]*apd.Decimal // by the month's first day
	Roles    map[string]string                     // each account's role
	Names    map[string]string
}

// Figure is one figure of the parity: Fyniq's and the workbook's, and their difference (Fyniq less the workbook).
type Figure struct {
	Account    string
	Name       string
	Kind       string
	Month      time.Time // the month's first day; zero for the opening
	Fyniq      *apd.Decimal
	Workbook   *apd.Decimal
	Difference *apd.Decimal
	Proposed   string // the class proposed for a difference, "" when none fits
}

// Key is the figure's key in the difference register, which names it in a path: its account, month and kind, such as
// 6610.2026-06.movement, or its account and opening.
func (f Figure) Key() string {
	if f.Kind == FigureOpening {
		return f.Account + ".opening"
	}
	return f.Account + "." + f.Month.Format("2006-01") + "." + f.Kind
}

// CheckParity is one of the workbook's checks against Fyniq's of its Appendix C.1 number.
type CheckParity struct {
	Origin
	Number   int
	Name     string
	Workbook Status
	Fyniq    []string // the statuses of Fyniq's checks of the number, by key: "key=status"
	Same     bool     // false when they differ: a register line or a fix
	Missing  bool     // Fyniq has no check of the number
}

// Parity is the reconciliation against the workbook's own engine (FR-MIG-02, QA-MIG-01, design D8): every figure
// compared, and the differences among them.
type Parity struct {
	From, Through time.Time // the books start and the last month compared
	Accounts      int
	Figures       int
	Differences   []Figure
	Checks        []CheckParity
}

// add returns a + b.
func add(a, b *apd.Decimal) *apd.Decimal {
	out := new(apd.Decimal)
	x, y := cmp.Or(a, new(apd.Decimal)), cmp.Or(b, new(apd.Decimal))
	if _, err := apd.BaseContext.WithPrecision(40).Add(out, x, y); err != nil {
		panic(err)
	}
	return out
}

func sub(a, b *apd.Decimal) *apd.Decimal {
	return add(a, new(apd.Decimal).Neg(cmp.Or(b, new(apd.Decimal))))
}

// Reconcile compares Fyniq's books with the workbook's Trial Balance sheet (design D8), to the cent after RND-IMPORT:
// on the history, each account's opening at the books start and each month's movement and closing through the
// workbook's "as of"; on a clean start, each account's opening against the Carry Forward sheet's balance. Workbook accounts that
// map onto one Fyniq account are added. Each difference gets a proposed class: a cent allocation (a) on a spread or
// depreciation account within a cent a month, a rule change (b) on a revaluation or statutory account, else none, for
// the Accountant to decide.
func (w *Workbook) Reconcile(m Mapping, path string, b Books) Parity {
	start := w.BooksStart(path)
	p := Parity{From: start}
	type row struct {
		opening, atStart *apd.Decimal
		movement         map[time.Time]*apd.Decimal
		closing          map[time.Time]*apd.Decimal
	}
	rows := map[string]*row{}
	var months []time.Time
	seenMonth := map[time.Time]bool{}
	for _, t := range w.TrialBalance {
		code := m.accountOf(t.Code)
		r := rows[code]
		if r == nil {
			r = &row{movement: map[time.Time]*apd.Decimal{}, closing: map[time.Time]*apd.Decimal{}}
			rows[code] = r
		}
		r.opening = add(r.opening, t.Opening)
		for i, end := range t.Months {
			month := firstOf(end)
			if end.After(w.Settings.AsOf) {
				continue
			}
			if i < len(t.Movements) {
				r.movement[month] = add(r.movement[month], t.Movements[i])
			}
			if i < len(t.Closing) {
				r.closing[month] = add(r.closing[month], t.Closing[i])
			}
			if !seenMonth[month] && !month.Before(firstOf(start)) {
				seenMonth[month] = true
				months = append(months, month)
			}
		}
	}
	slices.SortFunc(months, func(a, b time.Time) int { return a.Compare(b) })
	// A clean start opens at the Carry Forward sheet's balances, its P&L closed into retained earnings, each on its
	// account's side as the Accounts sheet gives it.
	carried := map[string]*apd.Decimal{}
	if path == storage.PathCleanStart {
		kind := map[string]string{}
		for _, a := range w.Accounts {
			kind[a.Code] = a.Type
		}
		for _, c := range w.CarryForward.Balances {
			x := cmp.Or(c.Balance, new(apd.Decimal))
			if credit[kind[c.Code]] {
				x = new(apd.Decimal).Neg(x)
			}
			code := m.accountOf(c.Code)
			carried[code] = add(carried[code], x)
		}
	}
	codes := map[string]bool{}
	for c := range rows {
		codes[c] = true
	}
	for c := range carried {
		codes[c] = true
	}
	for c := range b.Opening {
		codes[c] = true
	}
	for c := range b.Movement {
		codes[c] = true
	}
	statutory := map[string]bool{}
	for name, code := range w.Settings.SystemAccounts {
		n := strings.ToLower(name)
		if strings.Contains(n, "cpf") || strings.Contains(n, "tax") || strings.Contains(n, "gst") {
			statutory[m.accountOf(code)] = true
		}
	}
	compare := func(code, kind string, month time.Time, fyniq, workbook *apd.Decimal, monthsIn int) {
		p.Figures++
		fyniq, workbook = cmp.Or(fyniq, new(apd.Decimal)), cmp.Or(workbook, new(apd.Decimal))
		diff := sub(fyniq, workbook)
		if diff.IsZero() {
			return
		}
		f := Figure{Account: code, Name: b.Names[code], Kind: kind, Month: month, Fyniq: fyniq, Workbook: workbook, Difference: diff}
		role := b.Roles[code]
		cents := apd.New(int64(max(monthsIn, 1)), -2)
		switch {
		case role == ledger.RolePrepayments || role == ledger.RoleDeferredRevenue || role == ledger.RoleAccumulatedDepreciation ||
			role == ledger.RoleDepreciation:
			if new(apd.Decimal).Abs(diff).Cmp(cents) <= 0 {
				f.Proposed = ClassAllocation
			}
		case role == string(storage.RoleUnrealisedFX) || role == ledger.RoleFXRounding || role == ledger.RoleGSTControl ||
			role == ledger.RoleWithholdingTax || statutory[code]:
			f.Proposed = ClassRule
		}
		p.Differences = append(p.Differences, f)
	}
	ordered := make([]string, 0, len(codes))
	for c := range codes {
		ordered = append(ordered, c)
	}
	slices.Sort(ordered)
	for _, code := range ordered {
		r := rows[code]
		if r == nil {
			r = &row{movement: map[time.Time]*apd.Decimal{}, closing: map[time.Time]*apd.Decimal{}}
		}
		p.Accounts++
		if path == storage.PathCleanStart {
			compare(code, FigureOpening, time.Time{}, b.Opening[code], carried[code], 0)
			continue
		}
		compare(code, FigureOpening, time.Time{}, b.Opening[code], r.opening, 0)
		closing := cmp.Or(b.Opening[code], new(apd.Decimal))
		for i, month := range months {
			moved := b.Movement[code][month]
			closing = add(closing, moved)
			compare(code, FigureMovement, month, moved, r.movement[month], 1)
			compare(code, FigureClosing, month, closing, r.closing[month], i+1)
		}
	}
	if len(months) > 0 {
		p.Through = months[len(months)-1]
	}
	if path == storage.PathCleanStart {
		p.Through = firstOf(w.Settings.AsOf)
	}
	return p
}

// CompareChecks compares the workbook's Checks rows with Fyniq's checks of their Appendix C.1 number (design D8): each
// status as the workbook writes it (✔, ⚠, ✖) against the worst of Fyniq's. fyniq gives each check's C.1 number, key and
// status (ok, warning, error, due, not_assessed or failed): a check not assessed, or failed, is never the same.
func (w *Workbook) CompareChecks(fyniq []FyniqCheck) []CheckParity {
	var out []CheckParity
	for _, c := range w.Checks {
		x := CheckParity{Origin: c.Origin, Number: c.Number, Name: c.Name, Workbook: c.Status}
		worst, assessed := Fine, true
		for _, f := range fyniq {
			if f.Number != c.Number {
				continue
			}
			x.Fyniq = append(x.Fyniq, f.Key+"="+f.Status)
			assessed = assessed && f.Status != "not_assessed" && f.Status != "failed"
			if s := statusOfCheck(f.Status); rank(s) > rank(worst) {
				worst = s
			}
		}
		x.Missing = len(x.Fyniq) == 0
		x.Same = x.Missing || worst == c.Status && assessed
		out = append(out, x)
	}
	return out
}

// FyniqCheck is one of Fyniq's checks as the comparison reads it.
type FyniqCheck struct {
	Number      int
	Key, Status string
}

// statusOfCheck is a Fyniq check's status as the workbook writes it: an error ✖, a warning or a deadline due ⚠.
func statusOfCheck(s string) Status {
	switch s {
	case "error":
		return Blocked
	case "warning", "due":
		return Review
	}
	return Fine
}

func rank(s Status) int {
	switch s {
	case Blocked:
		return 2
	case Review:
		return 1
	}
	return 0
}
