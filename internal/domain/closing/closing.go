// Package closing is the period close (PRD-001 FR-GL-07, FR-GL-08, FM-4, AUD-04, ENG-46, ENG-47; rev 0.50,
// provisional): two acts by two people. The preparer runs the close checks, which record their result and the ledger
// revision they saw. Another person attests and closes: in one unit of work, under the ledger lock, the close refuses
// checks that an entry dated in or before the period has overtaken (G4-053), closes the period, and records the
// attestation with the trial balance at the period end, as canonical JSON with its SHA-256 (AUD-04).
package closing

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/payables"
	"github.com/cognizhi/fyniq/internal/domain/receivables"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The stable error codes of the close (ENG-23).
const (
	CodeNotOpen    = "CLOSE-01" // the period is not open
	CodeStale      = "CLOSE-02" // an entry dated in or before the period posted after the checks ran
	CodeOrder      = "CLOSE-03" // an earlier period is open: periods close in order
	CodeNotClean   = "CLOSE-04" // the checks refused the close
	CodeSelfAttest = "CLOSE-05" // the one who ran the checks cannot attest them (FM-4)
	CodeWrongCheck = "CLOSE-06" // the checks are of another period
	CodeNotClosed  = "CLOSE-07" // the period is not closed, so it does not reopen
	CodeLater      = "CLOSE-08" // a later period is closed: only the latest closed period reopens
)

// Error is a refusal with a stable code.
type Error struct {
	Code    string
	Message string
	Fix     string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Fix) }

// Severity is what a check means for the close: ok, warning (⚠, the close may go on) or error (✖, it may not).
type Severity string

// The severities (ENG-21).
const (
	OK      Severity = "ok"
	Warning Severity = "warning"
	Failed  Severity = "error"
)

// Check is one close check's outcome.
type Check struct {
	Key      string   `json:"key"`
	Severity Severity `json:"severity"`
	Title    string   `json:"title"`
	Detail   string   `json:"detail,omitempty"`
}

// Service runs the close commands against a store.
type Service struct {
	Store storage.Store
	// Clock gives the day the checks run on, in the organisation's time zone, for the checks that depend on it (a
	// recurring journal's occurrence the scheduler failed to make, C.1 #28). Without it they are not run.
	Clock ports.Clock
}

// earlier are the periods before p, earliest first.
func earlier(all []storage.Period, p storage.Period) []storage.Period {
	var out []storage.Period
	for _, x := range all {
		if x.EndsOn.Before(p.StartsOn) {
			out = append(out, x)
		}
	}
	return out
}

func base(ctx context.Context, u storage.UnitOfWork) (money.Currency, storage.Organisation, error) {
	o, err := u.Organisation(ctx)
	if err != nil {
		return money.Currency{}, o, err
	}
	b, err := money.Lookup(o.BaseCurrency)
	return b, o, err
}

// run runs the checks of a period as they stand in u. Until p1-checks, the close's own checks are these:
// ✖ every earlier period is closed; ✖ the balances agree with the lines (INV-08); ✖ the trial balance at the period end
// nets to zero (INV-01, INV-09); ⚠ draft journals dated in the period; ⚠ the opening balances not posted, in the period
// the books start in; (rev 0.58) ⚠ automatic reversals that have not posted: dated on or before the period's end, so
// closing it stops them, or of a journal dated in the period and waiting for a period that is not open; (rev 0.60)
// recurring journals with an occurrence on or before the period's end and no draft yet, ⚠, or ✖ when the occurrence is
// more than a day before today, so the hourly scheduler failed (Appendix C.1 #28); ⚠ active recurring journals whose
// content the posting rules refuse (C.1 #29; the founders' decision of rev 0.61: it warns, as the close is about the
// period's postings and their drafts show their own problems); (rev 0.65) ✖ an income or expense account's variance
// against the previous month above both thresholds with no explanation of its totals as they are (FR-GL-14, Appendix C.2
// #57); (rev 0.68) ✖ a task of the month's close checklist neither done nor skipped with a reason (FR-GL-07, C.2 #58);
// (rev 0.78) ✖ the AP sub-ledger differs from 2010 by anything no source explains, ⚠ amounts on 2010 from opening
// balances or manual journals (INV-07, C.2 #2); (rev 0.85) the same two checks of the recurring bills as of the
// recurring journals (C.1 #28, #29). Reconciliations join with p1-bank-import-reconcile.
func run(ctx context.Context, u storage.UnitOfWork, p storage.Period, today time.Time) ([]Check, error) {
	cur, org, err := base(ctx, u)
	if err != nil {
		return nil, err
	}
	all, err := u.Periods().List(ctx)
	if err != nil {
		return nil, err
	}
	var out []Check
	open := 0
	for _, x := range earlier(all, p) {
		if x.Status != storage.PeriodClosed {
			open++
		}
	}
	if open > 0 {
		out = append(out, Check{"earlier", Failed, "Earlier periods are open", fmt.Sprintf("%d earlier period(s) are not closed: periods close in order", open)})
	} else {
		out = append(out, Check{"earlier", OK, "Every earlier period is closed", ""})
	}
	diffs, err := balance.Differences(ctx, u)
	if err != nil {
		return nil, err
	}
	if diffs > 0 {
		out = append(out, Check{"balances", Failed, "The balances do not agree with the ledger", fmt.Sprintf("%d balance(s) differ from their lines: report it, never rebuild them unseen", diffs)})
	} else {
		out = append(out, Check{"balances", OK, "The balances agree with the ledger", ""})
	}
	tb, err := balance.Position(ctx, u, cur, all, p)
	if err != nil {
		return nil, err
	}
	if !tb.Total.IsZero() {
		out = append(out, Check{"trial-balance", Failed, "The trial balance does not net to zero", fmt.Sprintf("It is off by %s %s", tb.Total, cur.Code)})
	} else {
		out = append(out, Check{"trial-balance", OK, "The trial balance nets to zero", ""})
	}
	drafts, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: storage.DocumentJournal, Status: storage.DocumentDraft,
		From: p.StartsOn.Format(time.DateOnly), To: p.EndsOn.Format(time.DateOnly), Limit: 101})
	if err != nil {
		return nil, err
	}
	if n := len(drafts); n > 0 {
		count := fmt.Sprint(n)
		if n > 100 {
			count = "More than 100"
		}
		out = append(out, Check{"drafts", Warning, "Draft journals are dated in the period", count + " draft journal(s): once the period closes they cannot post on those dates"})
	} else {
		out = append(out, Check{"drafts", OK, "No draft journal is dated in the period", ""})
	}
	due, err := u.Documents().DueReversals(ctx, p.EndsOn.Format(time.DateOnly), 101)
	if err != nil {
		return nil, err
	}
	waiting, err := u.Documents().WaitingReversals(ctx, "", nil, 1000)
	if err != nil {
		return nil, err
	}
	dueIDs := map[uuid.UUID]bool{}
	for _, id := range due {
		dueIDs[id] = true
	}
	held := 0
	for _, d := range waiting {
		if !dueIDs[d.ID] && d.Date >= p.StartsOn.Format(time.DateOnly) && d.Date <= p.EndsOn.Format(time.DateOnly) {
			held++
		}
	}
	if len(due) > 0 || held > 0 {
		var parts []string
		if n := len(due); n > 0 {
			count := fmt.Sprint(n)
			if n > 100 {
				count = "More than 100"
			}
			parts = append(parts, count+" journal(s) reverse by themselves on or before "+p.EndsOn.Format(time.DateOnly)+": once the period closes, their reversals cannot post")
		}
		if held > 0 {
			parts = append(parts, fmt.Sprint(held)+" journal(s) dated in the period wait to reverse in a period that is not open: open it so they can")
		}
		out = append(out, Check{"reversals", Warning, "Automatic reversals have not posted", strings.Join(parts, "; ")})
	} else {
		out = append(out, Check{"reversals", OK, "No automatic reversal is waiting", ""})
	}
	reval, err := revaluationCheck(ctx, u, p)
	if err != nil {
		return nil, err
	}
	out = append(out, reval)
	payables, err := payablesChecks(ctx, u, cur, p)
	if err != nil {
		return nil, err
	}
	out = append(out, payables...)
	receivables, err := receivablesChecks(ctx, u, cur, p)
	if err != nil {
		return nil, err
	}
	out = append(out, receivables...)
	recurring, err := recurringChecks(ctx, u, p, today)
	if err != nil {
		return nil, err
	}
	out = append(out, recurring...)
	recurringBills, err := recurringBillChecks(ctx, u, p, today)
	if err != nil {
		return nil, err
	}
	out = append(out, recurringBills...)
	flux, err := compare(ctx, u, p, all, cur, cmpDay(today, p.EndsOn))
	if err != nil {
		return nil, err
	}
	out = append(out, fluxCheck(flux))
	list, err := checklistOf(ctx, u, p)
	if err != nil {
		return nil, err
	}
	out = append(out, checklistCheck(list))
	if bs := org.Profile.BooksStart; bs != nil && !bs.Before(p.StartsOn) && !bs.After(p.EndsOn) {
		ob, err := u.Documents().OpeningBalance(ctx)
		switch {
		case errors.Is(err, ports.ErrNotFound) || (err == nil && ob.Status == storage.DocumentDraft):
			out = append(out, Check{"opening", Warning, "The opening balances are not posted", "The books start in this period"})
		case err != nil:
			return nil, err
		default:
			out = append(out, Check{"opening", OK, "The opening balances are posted", ""})
		}
	}
	return out, nil
}

// revaluationCheck refuses the close of a period with foreign-currency balances on accounts marked revalue and no
// revaluation run since the last posting dated in or before it (FR-GL-04; rev 0.73), naming a currency with no rate.
func revaluationCheck(ctx context.Context, u storage.UnitOfWork, p storage.Period) (Check, error) {
	plan, err := fx.Prepare(ctx, u, p.ID, journal.SubLedgers())
	if err != nil {
		return Check{}, err
	}
	switch {
	case len(plan.Items) == 0:
		return Check{"revaluation", OK, "No foreign-currency balance to revalue", ""}, nil
	case !plan.Due():
		return Check{"revaluation", OK, "Foreign-currency balances are revalued", ""}, nil
	}
	detail := fmt.Sprintf("%d balance(s) in foreign currencies: revalue them in Accounting › Currencies", len(plan.Items))
	if plan.Latest != nil {
		detail = "A posting dated in or before the period came after the revaluation: revalue it again in Accounting › Currencies"
	}
	for _, m := range plan.Missing {
		detail += "; " + strings.TrimPrefix(m.Message, "✖ ")
	}
	return Check{"revaluation", Failed, "Foreign-currency balances are not revalued", detail}, nil
}

// explainedWords names a source that posts to accounts payable or receivable other than the sub-ledger's documents.
var explainedWords = map[string]string{ledger.SourceOpeningBalance: "opening balances", ledger.SourceManualJournal: "manual journals"}

// payablesChecks reconcile the AP sub-ledger with the accounts-payable account at the period's end (INV-07; the owner's
// decision of 23 Oct 2026): ✖ when they differ by anything no source explains, which is a fault; ⚠ when the account
// holds amounts from opening balances or manual journals that no bill supports (question 55 lets a journal post there);
// ✔ otherwise. The revaluation's base-only lines are explained and need no warning. Nothing is said when the chart has
// no accounts-payable account, or it and the sub-ledger are empty.
func payablesChecks(ctx context.Context, u storage.UnitOfWork, cur money.Currency, p storage.Period) ([]Check, error) {
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return nil, err
	}
	var ap *storage.Account
	for i := range accounts {
		if string(accounts[i].Role) == ledger.RoleAccountsPayable {
			ap = &accounts[i]
		}
	}
	if ap == nil {
		return nil, nil
	}
	r, l, err := payables.Reconcile(ctx, u, cur, ap.ID, p.EndsOn)
	if err != nil {
		return nil, err
	}
	if len(r.Currencies) == 0 && len(l.Suppliers) == 0 {
		return nil, nil
	}
	var out []Check
	if r.Unexplained() {
		var parts []string
		for _, c := range r.Currencies {
			if !c.Unexplained.IsZero() {
				parts = append(parts, fmt.Sprintf("in %s, %s says %s is owed and the open bills and credits %s", c.Currency, ap.Code, c.Control, c.SubLedger))
			}
		}
		if b := r.Base; !b.Unexplained.IsZero() {
			parts = append(parts, fmt.Sprintf("in carrying amounts (%s), %s says %s and the open bills and credits %s", b.Currency, ap.Code, b.Control, b.SubLedger))
		}
		out = append(out, Check{"payables", Failed, "The AP sub-ledger does not agree with accounts payable",
			strings.Join(parts, "; ") + ": a posting and its document disagree; report it, never adjust it by journal"})
	} else {
		items := 0
		for _, s := range l.Suppliers {
			items += len(s.Items)
		}
		out = append(out, Check{"payables", OK, "The AP sub-ledger agrees with accounts payable", fmt.Sprintf("%d open bill(s) and supplier credit(s)", items)})
	}
	var held []string
	for _, c := range r.Currencies {
		for _, e := range c.Explained {
			if w, ok := explainedWords[e.SourceType]; ok && !e.Amount.IsZero() {
				held = append(held, fmt.Sprintf("%s %s from %s", e.Amount, c.Currency, w))
			}
		}
	}
	if len(held) > 0 {
		out = append(out, Check{"payables-unsupported", Warning, "Accounts payable holds amounts no bill supports",
			fmt.Sprintf("%s holds %s: support them with the suppliers' statements, or enter the bills", ap.Code, strings.Join(held, ", "))})
	}
	return out, nil
}

// receivablesChecks reconcile the AR sub-ledger with the accounts-receivable account at the period's end (INV-07;
// p1-sales task 1.6), as payablesChecks do for accounts payable: ✖ when they differ by anything no source explains,
// which is a fault; ⚠ when the account holds amounts from opening balances or manual journals that no invoice supports;
// ✔ otherwise. Nothing is said when the chart has no accounts-receivable account, or it and the sub-ledger are empty.
func receivablesChecks(ctx context.Context, u storage.UnitOfWork, cur money.Currency, p storage.Period) ([]Check, error) {
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return nil, err
	}
	var ar *storage.Account
	for i := range accounts {
		if string(accounts[i].Role) == ledger.RoleAccountsReceivable {
			ar = &accounts[i]
		}
	}
	if ar == nil {
		return nil, nil
	}
	r, l, err := receivables.Reconcile(ctx, u, cur, ar.ID, p.EndsOn)
	if err != nil {
		return nil, err
	}
	if len(r.Currencies) == 0 && len(l.Customers) == 0 {
		return nil, nil
	}
	var out []Check
	if r.Unexplained() {
		var parts []string
		for _, c := range r.Currencies {
			if !c.Unexplained.IsZero() {
				parts = append(parts, fmt.Sprintf("in %s, %s says %s is owed and the open invoices and credits %s", c.Currency, ar.Code, c.Control, c.SubLedger))
			}
		}
		if b := r.Base; !b.Unexplained.IsZero() {
			parts = append(parts, fmt.Sprintf("in carrying amounts (%s), %s says %s and the open invoices and credits %s", b.Currency, ar.Code, b.Control, b.SubLedger))
		}
		out = append(out, Check{"receivables", Failed, "The AR sub-ledger does not agree with accounts receivable",
			strings.Join(parts, "; ") + ": a posting and its document disagree; report it, never adjust it by journal"})
	} else {
		items := 0
		for _, c := range l.Customers {
			items += len(c.Items)
		}
		out = append(out, Check{"receivables", OK, "The AR sub-ledger agrees with accounts receivable", fmt.Sprintf("%d open invoice(s) and customer credit(s)", items)})
	}
	var held []string
	for _, c := range r.Currencies {
		for _, e := range c.Explained {
			if w, ok := explainedWords[e.SourceType]; ok && !e.Amount.IsZero() {
				held = append(held, fmt.Sprintf("%s %s from %s", e.Amount, c.Currency, w))
			}
		}
	}
	if len(held) > 0 {
		out = append(out, Check{"receivables-unsupported", Warning, "Accounts receivable holds amounts no invoice supports",
			fmt.Sprintf("%s holds %s: support them with the customers' statements, or enter the invoices", ar.Code, strings.Join(held, ", "))})
	}
	return out, nil
}

func clean(checks []Check) bool {
	for _, c := range checks {
		if c.Severity == Failed {
			return false
		}
	}
	return true
}

// RunChecks runs a period's close checks and records them, with the ledger revision they saw, by the unit of work's
// actor: the preparer.
func (s Service) RunChecks(ctx context.Context, org, periodID uuid.UUID) (storage.CloseCheck, error) {
	var out storage.CloseCheck
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		p, err := u.Periods().Get(ctx, periodID)
		if err != nil {
			return err
		}
		if p.Status != storage.PeriodOpen {
			return &Error{CodeNotOpen, fmt.Sprintf("the period %s to %s is %s", p.StartsOn.Format(time.DateOnly), p.EndsOn.Format(time.DateOnly), p.Status),
				"Only an open period is checked and closed"}
		}
		rev, err := u.Ledger().Revision(ctx)
		if err != nil {
			return err
		}
		var today time.Time
		if s.Clock != nil {
			o, err := u.Organisation(ctx)
			if err != nil {
				return err
			}
			loc, err := time.LoadLocation(o.Profile.TimeZone)
			if err != nil || o.Profile.TimeZone == "" {
				loc = time.UTC
			}
			y, m, d := s.Clock.Now().In(loc).Date()
			today = time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
		}
		checks, err := run(ctx, u, p, today)
		if err != nil {
			return err
		}
		result, err := json.Marshal(checks)
		if err != nil {
			return err
		}
		out, err = u.Closes().RecordCheck(ctx, storage.NewCloseCheck{PeriodID: p.ID, LedgerRevision: rev, Result: result, Clean: clean(checks)})
		return err
	})
	return out, err
}

// Snapshot is the trial balance at a period's end, as the close records it (AUD-04).
type Snapshot struct {
	PeriodID string            `json:"period_id"`
	StartsOn string            `json:"starts_on"`
	EndsOn   string            `json:"ends_on"`
	Revision int64             `json:"ledger_revision"`
	Currency string            `json:"currency"`
	Accounts []SnapshotAccount `json:"accounts"`
	Total    string            `json:"total"`
}

// SnapshotAccount is one account's base-currency balance at the period end.
type SnapshotAccount struct {
	AccountID string `json:"account_id"`
	Code      string `json:"code"`
	Balance   string `json:"balance"`
}

// snapshot builds the period's snapshot as canonical JSON (RFC 8785) and its SHA-256.
func snapshot(ctx context.Context, u storage.UnitOfWork, p storage.Period, all []storage.Period, rev int64) ([]byte, []byte, error) {
	cur, _, err := base(ctx, u)
	if err != nil {
		return nil, nil, err
	}
	tb, err := balance.Position(ctx, u, cur, all, p)
	if err != nil {
		return nil, nil, err
	}
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return nil, nil, err
	}
	codes := make(map[uuid.UUID]string, len(accounts))
	for _, a := range accounts {
		codes[a.ID] = a.Code
	}
	s := Snapshot{PeriodID: p.ID.String(), StartsOn: p.StartsOn.Format(time.DateOnly), EndsOn: p.EndsOn.Format(time.DateOnly), Revision: rev,
		Currency: cur.Code, Accounts: []SnapshotAccount{}, Total: tb.Total.String()}
	for _, r := range tb.Rows {
		s.Accounts = append(s.Accounts, SnapshotAccount{AccountID: r.AccountID.String(), Code: codes[r.AccountID], Balance: r.Base.String()})
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return nil, nil, err
	}
	// Numbers come back as json.Number, which the canonicaliser takes as integers; it refuses a float (ENG-10).
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var generic any
	if err := dec.Decode(&generic); err != nil {
		return nil, nil, err
	}
	canonical, err := approval.Canonical(generic)
	if err != nil {
		return nil, nil, err
	}
	return canonical, approval.Hash(canonical), nil
}

// Close attests a period's close on a run of its checks and closes it, in one unit of work (FR-GL-07, FM-4). The
// request's actor must not be the one who ran the checks, which the database also refuses (FY153); the checks must be
// clean, of this period, and current: under the ledger lock (ENG-47 step 2) no entry dated on or before the period
// end may have a revision later than they saw.
func (s Service) Close(ctx context.Context, org, periodID, checkID uuid.UUID, statement string) (storage.PeriodClose, storage.Period, error) {
	var out storage.PeriodClose
	var closed storage.Period
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		rev, err := u.Ledger().Lock(ctx) // ENG-47 step 2: nothing posts until this commits
		if err != nil {
			return err
		}
		p, err := u.Periods().Get(ctx, periodID)
		if err != nil {
			return err
		}
		if p.Status != storage.PeriodOpen {
			return &Error{CodeNotOpen, fmt.Sprintf("the period %s to %s is %s", p.StartsOn.Format(time.DateOnly), p.EndsOn.Format(time.DateOnly), p.Status),
				"Only an open period is closed"}
		}
		c, err := u.Closes().Check(ctx, checkID)
		if errors.Is(err, ports.ErrNotFound) || (err == nil && c.PeriodID != p.ID) {
			return &Error{CodeWrongCheck, "those checks are not of this period", "Run the period's checks and attest them"}
		}
		if err != nil {
			return err
		}
		if !c.Clean {
			return &Error{CodeNotClean, "the checks refused the close", "Fix what they found, run them again, then close"}
		}
		if a := storage.AuditFrom(ctx).Actor; a != nil && c.CheckedBy == a.String() {
			return &Error{CodeSelfAttest, "you ran these checks, so another founder attests them", "Ask another founder who may close periods to attest the close (FM-4)"}
		}
		all, err := u.Periods().List(ctx)
		if err != nil {
			return err
		}
		for _, x := range earlier(all, p) {
			if x.Status != storage.PeriodClosed {
				return &Error{CodeOrder, fmt.Sprintf("the period %s to %s is open", x.StartsOn.Format(time.DateOnly), x.EndsOn.Format(time.DateOnly)),
					"Close the earlier periods first: periods close in order"}
			}
		}
		stale, err := u.Closes().ChangedSince(ctx, p.EndsOn, c.LedgerRevision)
		if err != nil {
			return err
		}
		if stale {
			return &Error{CodeStale, "an entry dated in or before the period posted after the checks ran", "Run the checks again, then close"}
		}
		snap, hash, err := snapshot(ctx, u, p, all, rev)
		if err != nil {
			return err
		}
		if closed, err = u.Periods().Close(ctx, p.ID); err != nil {
			return err
		}
		// The close holds the ledger lock it took first, so the revision at the close is the one the snapshot read; a
		// difference would be a lock-order fault, and nothing is closed on it.
		if closed.ClosedRevision == nil || *closed.ClosedRevision != rev {
			return fmt.Errorf("closing: the period closed at revision %v, not the %d the snapshot read", closed.ClosedRevision, rev)
		}
		out, err = u.Closes().Record(ctx, storage.NewClose{PeriodID: p.ID, CheckID: c.ID, Statement: statement, ClosedRevision: *closed.ClosedRevision,
			Snapshot: snap, SnapshotHash: hash})
		return err
	})
	return out, closed, err
}

// Read reads checks recorded as JSON.
func Read(result []byte) ([]Check, error) {
	var out []Check
	err := json.Unmarshal(result, &out)
	return out, err
}

// Reopenable says whether a period can reopen now (FR-GL-08; rev 0.51): it is closed, no later period is, and it has a
// close; it answers that close.
func Reopenable(ctx context.Context, u storage.UnitOfWork, periodID uuid.UUID) (storage.Period, storage.PeriodClose, error) {
	p, err := u.Periods().Get(ctx, periodID)
	if err != nil {
		return p, storage.PeriodClose{}, err
	}
	if p.Status != storage.PeriodClosed {
		return p, storage.PeriodClose{}, &Error{CodeNotClosed, fmt.Sprintf("the period %s to %s is %s", p.StartsOn.Format(time.DateOnly), p.EndsOn.Format(time.DateOnly), p.Status),
			"Only a closed period reopens"}
	}
	all, err := u.Periods().List(ctx)
	if err != nil {
		return p, storage.PeriodClose{}, err
	}
	for _, x := range all {
		if x.StartsOn.After(p.EndsOn) && x.Status == storage.PeriodClosed {
			return p, storage.PeriodClose{}, &Error{CodeLater, fmt.Sprintf("the later period %s to %s is closed", x.StartsOn.Format(time.DateOnly), x.EndsOn.Format(time.DateOnly)),
				"Reopen the latest closed period first: periods reopen in reverse order"}
		}
	}
	c, err := u.Closes().Latest(ctx, p.ID)
	if errors.Is(err, ports.ErrNotFound) {
		return p, c, &Error{CodeNotClosed, "the period has no close to undo", "Only a period closed through its close reopens here"}
	}
	return p, c, err
}

// Reopen reopens a closed period on an approved request, in the approving unit of work (FR-GL-08, FM-1): under the
// ledger lock (ENG-47) it checks the period still reopens, takes the snapshot after (the trial balance at the period
// end now), reopens it and records the reopening with the close's snapshot as the one before.
func Reopen(ctx context.Context, u storage.UnitOfWork, periodID, approvalID uuid.UUID, reason string) (storage.PeriodReopen, storage.Period, error) {
	rev, err := u.Ledger().Lock(ctx)
	if err != nil {
		return storage.PeriodReopen{}, storage.Period{}, err
	}
	p, c, err := Reopenable(ctx, u, periodID)
	if err != nil {
		return storage.PeriodReopen{}, p, err
	}
	all, err := u.Periods().List(ctx)
	if err != nil {
		return storage.PeriodReopen{}, p, err
	}
	snap, hash, err := snapshot(ctx, u, p, all, rev)
	if err != nil {
		return storage.PeriodReopen{}, p, err
	}
	opened, err := u.Periods().Reopen(ctx, p.ID)
	if err != nil {
		return storage.PeriodReopen{}, p, err
	}
	r, err := u.Closes().RecordReopen(ctx, storage.NewReopen{PeriodID: p.ID, CloseID: c.ID, ApprovalID: approvalID, Reason: reason, ReopenedRevision: rev,
		BeforeHash: c.SnapshotHash, AfterSnapshot: snap, AfterHash: hash})
	return r, opened, err
}

// recurringChecks are the close's checks of the recurring journals (FR-GL-02; Appendix C.1 #28, #29; rev 0.60).
func recurringChecks(ctx context.Context, u storage.UnitOfWork, p storage.Period, today time.Time) ([]Check, error) {
	var out []Check
	due, err := u.RecurringJournals().Due(ctx, p.EndsOn.Format(time.DateOnly), 101)
	if err != nil {
		return nil, err
	}
	failed := 0
	for _, id := range due {
		r, err := u.RecurringJournals().Get(ctx, id)
		if err != nil {
			return nil, err
		}
		if !today.IsZero() && r.NextOn != nil && r.NextOn.Before(today.AddDate(0, 0, -1)) {
			failed++
		}
	}
	count := func(n int) string {
		if n > 100 {
			return "More than 100"
		}
		return fmt.Sprint(n)
	}
	switch {
	case failed > 0:
		out = append(out, Check{"recurring", Failed, "The scheduler did not make recurring drafts",
			count(failed) + " recurring journal(s) have an occurrence more than a day ago with no draft: the scheduler may have failed. Tell an administrator, then run the checks again"})
	case len(due) > 0:
		out = append(out, Check{"recurring", Warning, "Recurring journals due in the period have no draft",
			count(len(due)) + " recurring journal(s) have an occurrence on or before " + p.EndsOn.Format(time.DateOnly) + " with no draft yet: the scheduler makes them within the hour"})
	default:
		out = append(out, Check{"recurring", OK, "Every recurring journal due in the period has its draft", ""})
	}
	var all []storage.RecurringJournal
	var afterName string
	var afterID *uuid.UUID
	for {
		page, err := u.RecurringJournals().Page(ctx, afterName, afterID, 1000)
		if err != nil {
			return nil, err
		}
		for _, r := range page {
			if r.Status == storage.RecurringActive && r.NextOn != nil {
				all = append(all, r)
			}
		}
		if len(page) < 1000 {
			break
		}
		last := page[len(page)-1]
		afterName, afterID = last.Name, &last.ID
	}
	statuses, err := journal.RecurringStatuses(ctx, u, all, cmpDay(today, p.EndsOn))
	if err != nil {
		return nil, err
	}
	incomplete := 0
	for _, st := range statuses {
		if len(st) > 0 && st[0].Severity == journal.SeverityError {
			incomplete++
		}
	}
	if incomplete > 0 {
		out = append(out, Check{"recurring-content", Warning, "Recurring journals are incomplete",
			count(incomplete) + " active recurring journal(s) have content the posting rules refuse, so their drafts could not post: fix or pause them"})
	} else {
		out = append(out, Check{"recurring-content", OK, "Every active recurring journal is complete", ""})
	}
	return out, nil
}

// recurringBillChecks are the close's checks of the recurring bills (FR-AP-09; Appendix C.1 #28, #29; rev 0.85), as
// the recurring journals': ⚠ an occurrence on or before the period's end with no draft yet, ✖ one more than a day before
// today (the hourly scheduler failed), and ⚠ an active one whose content the bill rules refuse.
func recurringBillChecks(ctx context.Context, u storage.UnitOfWork, p storage.Period, today time.Time) ([]Check, error) {
	var out []Check
	due, err := u.RecurringBills().Due(ctx, p.EndsOn.Format(time.DateOnly), 101)
	if err != nil {
		return nil, err
	}
	failed := 0
	for _, id := range due {
		r, err := u.RecurringBills().Get(ctx, id)
		if err != nil {
			return nil, err
		}
		if !today.IsZero() && r.NextOn != nil && r.NextOn.Before(today.AddDate(0, 0, -1)) {
			failed++
		}
	}
	count := func(n int) string {
		if n > 100 {
			return "More than 100"
		}
		return fmt.Sprint(n)
	}
	switch {
	case failed > 0:
		out = append(out, Check{"recurring-bills", Failed, "The scheduler did not make recurring bills' drafts",
			count(failed) + " recurring bill(s) have an occurrence more than a day ago with no draft: the scheduler may have failed. Tell an administrator, then run the checks again"})
	case len(due) > 0:
		out = append(out, Check{"recurring-bills", Warning, "Recurring bills due in the period have no draft",
			count(len(due)) + " recurring bill(s) have an occurrence on or before " + p.EndsOn.Format(time.DateOnly) + " with no draft yet: the scheduler makes them within the hour"})
	default:
		out = append(out, Check{"recurring-bills", OK, "Every recurring bill due in the period has its draft", ""})
	}
	var all []storage.RecurringBill
	var afterName string
	var afterID *uuid.UUID
	for {
		page, err := u.RecurringBills().Page(ctx, afterName, afterID, 1000)
		if err != nil {
			return nil, err
		}
		for _, r := range page {
			if r.Status == storage.RecurringActive && r.NextOn != nil {
				all = append(all, r)
			}
		}
		if len(page) < 1000 {
			break
		}
		last := page[len(page)-1]
		afterName, afterID = last.Name, &last.ID
	}
	statuses, err := journal.RecurringBillStatuses(ctx, u, all, cmpDay(today, p.EndsOn))
	if err != nil {
		return nil, err
	}
	incomplete := 0
	for _, st := range statuses {
		if len(st) > 0 && st[0].Severity == journal.SeverityError {
			incomplete++
		}
	}
	if incomplete > 0 {
		out = append(out, Check{"recurring-bills-content", Warning, "Recurring bills are incomplete",
			count(incomplete) + " active recurring bill(s) have lines a bill would refuse, so their drafts could not post: fix or pause them"})
	} else {
		out = append(out, Check{"recurring-bills-content", OK, "Every active recurring bill is complete", ""})
	}
	return out, nil
}

// cmpDay is today, or day when today is unknown.
func cmpDay(today, day time.Time) time.Time {
	if today.IsZero() {
		return day
	}
	return today
}
