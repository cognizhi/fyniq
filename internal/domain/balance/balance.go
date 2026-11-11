// Package balance is the balance service (ENG-04): the one place ledger figures come from. It reads
// the materialised balances, which the database keeps from the journal lines in the posting
// transaction and proves equal to them (INV-08). Nothing else sums journal lines; an architecture
// test keeps every other package away from the balances.
package balance

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Row is one account's base-currency total.
type Row struct {
	AccountID uuid.UUID
	Base      money.Money
}

// TrialBalance is every account's base-currency total over some periods, and their sum, which is
// zero when the books balance (INV-09).
type TrialBalance struct {
	Rows  []Row
	Total money.Money
}

// Snapshot is how the balance service reads the ledger (FR-RPT-08, ENG-46): Current reads the materialised balances as
// they stand; At reads the ledger as at a revision, so that a report re-run at the revision it recorded gives the same
// figures whatever was posted since: a period's cached balances when nothing later is in it (INV-08 keeps them its
// lines' sums; NFR-PERF-02), else its lines up to the revision. A check's read sums the lines (storage.WithLines).
type Snapshot struct {
	at       bool
	revision int64
	// only, when set, keeps the lines whose dimension values it accepts (FR-DIM-02): a department's P&L reads the lines
	// with that department's value; nil reads every line.
	only func(values []uuid.UUID) bool
}

// Current reads the balances as they stand.
var Current = Snapshot{}

// At reads the ledger as at a revision.
func At(revision int64) Snapshot { return Snapshot{at: true, revision: revision} }

// Revision is the revision s reads as at, and whether it reads at one.
func (s Snapshot) Revision() (int64, bool) { return s.revision, s.at }

// Only is s reading only the lines whose dimension values keep accepts (FR-DIM-02), as a set's values are held: once
// each, in ascending order, nil for none.
func (s Snapshot) Only(keep func(values []uuid.UUID) bool) Snapshot {
	s.only = keep
	return s
}

func (s Snapshot) forPeriod(ctx context.Context, u storage.UnitOfWork, period uuid.UUID) ([]storage.Balance, error) {
	if s.only != nil {
		var sets []storage.SetBalance
		var err error
		if s.at {
			sets, err = u.Balances().ForPeriodBySetAt(ctx, period, s.revision)
		} else {
			sets, err = u.Balances().ForPeriodBySet(ctx, period)
		}
		if err != nil {
			return nil, err
		}
		var out []storage.Balance
		for _, b := range sets {
			if s.only(b.Values) {
				out = append(out, b.Balance)
			}
		}
		return out, nil
	}
	if s.at {
		return u.Balances().ForPeriodAt(ctx, period, s.revision)
	}
	return u.Balances().ForPeriod(ctx, period)
}

// Trial returns the trial balance of the given periods, in the organisation's base currency.
func Trial(ctx context.Context, u storage.UnitOfWork, base money.Currency, periods ...uuid.UUID) (TrialBalance, error) {
	return TrialAt(ctx, u, base, Current, periods...)
}

// TrialAt is Trial read through a snapshot. A period before the ledger's latest purge boundary, or a snapshot as at a
// revision before its purge, is refused (Purged, AUD-10).
func TrialAt(ctx context.Context, u storage.UnitOfWork, base money.Currency, s Snapshot, periods ...uuid.UUID) (TrialBalance, error) {
	if err := s.check(ctx, u); err != nil {
		return TrialBalance{}, err
	}
	for _, id := range periods {
		p, err := u.Periods().Get(ctx, id)
		if err != nil {
			return TrialBalance{}, err
		}
		if err := CheckDay(ctx, u, p.EndsOn); err != nil {
			return TrialBalance{}, err
		}
	}
	return trialAt(ctx, u, base, s, periods...)
}

// trialAt is TrialAt with no refusal: what a position from the boundary on reads of the purged periods.
func trialAt(ctx context.Context, u storage.UnitOfWork, base money.Currency, s Snapshot, periods ...uuid.UUID) (TrialBalance, error) {
	totals := map[uuid.UUID]money.Money{}
	for _, p := range periods {
		rows, err := s.forPeriod(ctx, u, p)
		if err != nil {
			return TrialBalance{}, err
		}
		for _, r := range rows {
			amount, err := money.ParseStored(string(r.BaseAmount), base)
			if err != nil {
				return TrialBalance{}, err
			}
			cur, ok := totals[r.AccountID]
			if !ok {
				cur = money.Zero(base)
			}
			if totals[r.AccountID], err = cur.Add(amount); err != nil {
				return TrialBalance{}, err
			}
		}
	}
	tb := TrialBalance{Total: money.Zero(base)}
	for id, amount := range totals {
		tb.Rows = append(tb.Rows, Row{AccountID: id, Base: amount})
		var err error
		if tb.Total, err = tb.Total.Add(amount); err != nil {
			return TrialBalance{}, err
		}
	}
	sortRows(tb.Rows)
	return tb, nil
}

func sortRows(rows []Row) {
	sort.Slice(rows, func(i, j int) bool { return rows[i].AccountID.String() < rows[j].AccountID.String() })
}

// Differences counts the balances that differ from a recomputation from the journal lines (INV-08), in u's
// organisation: 0 when the cache agrees with the ledger.
func Differences(ctx context.Context, u storage.UnitOfWork) (int, error) {
	d, err := u.Balances().Differences(ctx)
	return len(d), err
}

// ActionCheckFailed is the audit action of a control check that failed (PRD-001 Appendix F).
const ActionCheckFailed = "control.check.failed"

// Verification is one organisation's nightly comparison of its balances with its journal lines.
type Verification struct {
	Organisation uuid.UUID
	Differences  int
}

// VerifyAll recomputes every organisation's balances from its journal lines and compares them with
// the cache (INV-08; the nightly check of CTL-FIN-01). A difference is recorded in that organisation's
// audit trail as control.check.failed, and the run fails: the ledger is then an incident to
// investigate (fyniq-bug-fix), never to "fix" by rebuilding the cache unseen.
func VerifyAll(ctx context.Context, store storage.Store) ([]Verification, error) {
	orgs, err := store.Organisations(ctx)
	if err != nil {
		return nil, err
	}
	var out []Verification
	failed := 0
	for _, org := range orgs {
		v := Verification{Organisation: org}
		err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			diffs, err := u.Balances().Differences(ctx)
			if err != nil || len(diffs) == 0 {
				return err
			}
			v.Differences = len(diffs)
			return u.Audit().Record(ctx, storage.AuditRecord{Action: ActionCheckFailed, ObjectType: "organisation", ObjectID: org,
				Changes: map[string][2]any{"control": {nil, "CTL-FIN-01"}, "check": {nil, "ledger.verify"}, "balance_differences": {nil, len(diffs)}}})
		})
		if err != nil {
			return out, fmt.Errorf("organisation %s: %w", org, err)
		}
		out = append(out, v)
		if v.Differences > 0 {
			failed++
		}
	}
	if failed > 0 {
		return out, fmt.Errorf("%d organisation(s) whose balances differ from their journal lines (INV-08)", failed)
	}
	return out, nil
}

// MaxLedgerLines is how many lines of one account the general ledger report shows at once: beyond it, a narrower range
// is asked for (PRD-001 rev 0.52, provisional). The export has no limit: it walks the lines a page at a time (rev 0.250).
const MaxLedgerLines = 5000

// CodeTooManyLines refuses a general ledger report of more than MaxLedgerLines lines for an account.
const CodeTooManyLines = "REPORT-01"

// TooMany is the refusal of a range with more lines than one report shows.
type TooMany struct {
	AccountID uuid.UUID
}

func (e *TooMany) Error() string {
	return fmt.Sprintf("%s: account %s has more than %d lines in the range", CodeTooManyLines, e.AccountID, MaxLedgerLines)
}

// LedgerLine is one line of the general ledger report: what posted, from where, and the account's balance after it.
type LedgerLine struct {
	storage.AccountLine
	Amount  money.Money // in the line's currency
	Base    money.Money
	Balance money.Money // the running balance in the base currency
}

// AccountLedger is one account's general ledger for a range: its balance before the range, each line with the running
// balance, the debits and credits, and its balance at the end.
type AccountLedger struct {
	AccountID uuid.UUID
	Opening   money.Money
	Lines     []LedgerLine
	Debits    money.Money
	Credits   money.Money // as a positive amount
	Closing   money.Money
}

// GeneralLedger is the general ledger report of an account over from..to, in the base currency (FR-GL-11): the
// balance before from, then each line dated in the range in posting order with the running balance. With year-end
// (FR-GL-10), a range within one financial year: one that crosses a year's start is refused (CrossesYear).
func GeneralLedger(ctx context.Context, u storage.UnitOfWork, base money.Currency, account uuid.UUID, from, to time.Time) (AccountLedger, error) {
	return GeneralLedgerOnly(ctx, u, base, account, from, to, nil)
}

// GeneralLedgerOnly is GeneralLedger of the lines whose dimension values only accepts (FR-DIM-02), such as Engineering's:
// its opening balance and its lines are of those lines alone. nil reads every line.
func GeneralLedgerOnly(ctx context.Context, u storage.UnitOfWork, base money.Currency, account uuid.UUID, from, to time.Time,
	only func(values []uuid.UUID) bool) (AccountLedger, error) {
	out, err := opening(ctx, u, base, account, from, to, only)
	if err != nil {
		return AccountLedger{}, err
	}
	lines, err := u.Ledger().AccountLines(ctx, account, from, to, MaxLedgerLines+1)
	if err != nil {
		return AccountLedger{}, err
	}
	if len(lines) > MaxLedgerLines {
		return AccountLedger{}, &TooMany{AccountID: account}
	}
	out.Lines = make([]LedgerLine, 0, len(lines))
	for _, l := range lines {
		if only != nil && !only(l.DimensionValues) {
			continue
		}
		line, err := out.add(l, base)
		if err != nil {
			return AccountLedger{}, err
		}
		out.Lines = append(out.Lines, line)
	}
	return out, nil
}

// LedgerPage is how many lines the general ledger export reads at a time (NFR-PERF-05).
var LedgerPage = 5000

// Opening is an account's general ledger over from..to before its lines, as the export starts it (NFR-PERF-05, rev
// 0.250): its balance before from, with no lines and nothing added yet. A range that crosses a financial year's start is
// refused, as GeneralLedger's is (CrossesYear).
func Opening(ctx context.Context, u storage.UnitOfWork, base money.Currency, account uuid.UUID, from, to time.Time) (AccountLedger, error) {
	return opening(ctx, u, base, account, from, to, nil)
}

// Walk reads the account's lines dated from..to in posting order, a page of LedgerPage at a time and with no limit of
// lines, adding each to gl and giving it to each with the balance after it, so gl ends with the range's debits,
// credits and closing balance and holds no lines (NFR-PERF-05, rev 0.250). base is the organisation's currency.
func (gl *AccountLedger) Walk(ctx context.Context, u storage.UnitOfWork, base money.Currency, from, to time.Time, each func(LedgerLine) error) error {
	var after *storage.LineKey
	for {
		lines, err := u.Ledger().AccountLinesAfter(ctx, gl.AccountID, from, to, after, LedgerPage)
		if err != nil {
			return err
		}
		for _, l := range lines {
			line, err := gl.add(l, base)
			if err != nil {
				return err
			}
			if err := each(line); err != nil {
				return err
			}
		}
		if len(lines) < LedgerPage {
			return nil
		}
		k := lines[len(lines)-1].Key()
		after = &k
	}
}

// opening is the account's ledger over from..to with its opening balance and nothing added yet.
func opening(ctx context.Context, u storage.UnitOfWork, base money.Currency, account uuid.UUID, from, to time.Time,
	only func(values []uuid.UUID) bool) (AccountLedger, error) {
	if err := CheckDay(ctx, u, from); err != nil {
		return AccountLedger{}, err
	}
	y, err := yearEndOf(ctx, u)
	if err != nil {
		return AccountLedger{}, err
	}
	if ys := y.start(to); !ys.IsZero() && from.Before(ys) && !y.start(from).IsZero() {
		return AccountLedger{}, &CrossesYear{YearStart: ys}
	}
	o, err := before(ctx, u, base, y, account, from, only)
	if err != nil {
		return AccountLedger{}, err
	}
	return AccountLedger{AccountID: account, Opening: o, Debits: money.Zero(base), Credits: money.Zero(base), Closing: o}, nil
}

// add adds a line to the ledger's totals and closing balance, and is the line with the balance after it.
func (gl *AccountLedger) add(l storage.AccountLine, base money.Currency) (LedgerLine, error) {
	cur, err := money.Lookup(l.Currency)
	if err != nil {
		return LedgerLine{}, err
	}
	amount, err := money.ParseStored(string(l.Amount), cur)
	if err != nil {
		return LedgerLine{}, err
	}
	b, err := money.ParseStored(string(l.BaseAmount), base)
	if err != nil {
		return LedgerLine{}, err
	}
	if gl.Closing, err = gl.Closing.Add(b); err != nil {
		return LedgerLine{}, err
	}
	if b.Sign() > 0 {
		gl.Debits, err = gl.Debits.Add(b)
	} else {
		gl.Credits, err = gl.Credits.Add(b.Neg())
	}
	if err != nil {
		return LedgerLine{}, err
	}
	return LedgerLine{AccountLine: l, Amount: amount, Base: b, Balance: gl.Closing}, nil
}

// before is an account's base-currency balance before a day: the cached balances of every period that ends before it
// (INV-08), and the lines of the period that holds it dated before it. Year-end applies (FR-GL-10): a revenue or expense
// account counts only the periods of the financial year that holds the day, and the retained-earnings account also
// counts the profit or loss of every earlier year.
func before(ctx context.Context, u storage.UnitOfWork, base money.Currency, y yearEnd, account uuid.UUID, day time.Time,
	only func(values []uuid.UUID) bool) (money.Money, error) {
	ys := y.start(day)
	total := money.Zero(base)
	periods, err := u.Periods().List(ctx)
	if err != nil {
		return total, err
	}
	add := func(s storage.Decimal) error {
		m, err := money.ParseStored(string(s), base)
		if err == nil {
			total, err = total.Add(m)
		}
		return err
	}
	for _, p := range periods {
		switch {
		case p.EndsOn.Before(day):
			rows, err := Current.Only(only).forPeriod(ctx, u, p.ID)
			if err != nil {
				return total, err
			}
			past := earlier(p, ys)
			for _, r := range rows {
				own := r.AccountID == account && !(past && y.pl[account])
				folded := past && account == y.re && y.pl[r.AccountID]
				if own || folded {
					if err := add(r.BaseAmount); err != nil {
						return total, err
					}
				}
			}
		case p.StartsOn.Before(day): // the period holds the day: its lines before it
			lines, err := u.Ledger().AccountLines(ctx, account, p.StartsOn, day.AddDate(0, 0, -1), 100000)
			if err != nil {
				return total, err
			}
			for _, l := range lines {
				if only != nil && !only(l.DimensionValues) {
					continue
				}
				if err := add(l.BaseAmount); err != nil {
					return total, err
				}
			}
		}
	}
	return total, nil
}

// EntriesOfType are the entries of a source type posted up to a revision, with their lines, in revision order: the few,
// such as the opening balances, that a statement reads apart from the balances (NFR-PERF-02).
func EntriesOfType(ctx context.Context, u storage.UnitOfWork, sourceType string, revision int64) ([]storage.Entry, error) {
	all, err := u.Ledger().OfSourceType(ctx, sourceType)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(all, func(e storage.Entry) bool { return e.LedgerRevision > revision }), nil
}

// EachAccountLine gives each line of an account dated from..to and posted up to a revision, in posting order, a page at
// a time and with no limit of lines: the equity accounts' few lines a statement reads apart from the balances
// (NFR-PERF-02).
func EachAccountLine(ctx context.Context, u storage.UnitOfWork, account uuid.UUID, from, to time.Time, revision int64,
	each func(storage.AccountLine) error) error {
	var after *storage.LineKey
	for {
		lines, err := u.Ledger().AccountLinesAfter(ctx, account, from, to, after, LedgerPage)
		if err != nil {
			return err
		}
		for _, l := range lines {
			if l.LedgerRevision > revision {
				continue
			}
			if err := each(l); err != nil {
				return err
			}
		}
		if len(lines) < LedgerPage {
			return nil
		}
		k := lines[len(lines)-1].Key()
		after = &k
	}
}

// EachEntryIn calls each with every entry of the periods up to a revision, with its lines: period by period, as given,
// and each period's in revision order. A statement that needs entries for some months, such as a month's cash flow the
// checks lay for every open month, reads those months alone, so it keeps its time as the ledger grows (NFR-PERF-02,
// INV-11; p1-operations-dr task 13.13).
func EachEntryIn(ctx context.Context, u storage.UnitOfWork, periods []uuid.UUID, revision int64, each func(storage.Entry) error) error {
	if err := CheckRevision(ctx, u, revision); err != nil {
		return err
	}
	const page = 500
	for _, p := range periods {
		after := int64(0)
		for more := true; more; {
			entries, err := u.Ledger().EntriesOfPeriod(ctx, p, after, page)
			if err != nil {
				return err
			}
			more = len(entries) == page
			for _, e := range entries {
				if e.LedgerRevision > revision {
					more = false
					break
				}
				if err := each(e); err != nil {
					return err
				}
				after = e.LedgerRevision
			}
		}
	}
	return nil
}

// EachEntry calls each with every entry the ledger holds up to a revision, in revision order, with its lines: what a
// statement that needs entries rather than balances reads, such as the cash flow (FR-RPT-01, INV-11), at the revision
// it reads every other figure at.
func EachEntry(ctx context.Context, u storage.UnitOfWork, revision int64, each func(storage.Entry) error) error {
	if err := CheckRevision(ctx, u, revision); err != nil {
		return err
	}
	const page = 500
	after := int64(0)
	for {
		entries, err := u.Ledger().Entries(ctx, after, page)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if e.LedgerRevision > revision {
				return nil
			}
			if err := each(e); err != nil {
				return err
			}
			after = e.LedgerRevision
		}
		if len(entries) < page {
			return nil
		}
	}
}
