package closing

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/banking"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The close's bank checks (FR-GL-07, FR-BNK-10, FR-BNK-11, FR-BNK-16; p1-bank-import-reconcile task 6.2, design D9),
// over the money accounts set up in Banking:
//   - ✖ a money account with activity in the period has no reconciliation for it signed off by a second person, or one
//     prepared before the last posting to it dated in the period (it saw another ledger revision);
//   - ⚠ the suspense account is not at nothing at the period's end (Appendix C.1 #30);
//   - ⚠ a bank or cash account is below zero at the period's end (C.1 #31);
//   - ⚠ a signed-off reconciliation leaves a difference (C.1 #32);
//   - ⚠ an account fed by email or a connector has had nothing for more than 3 days (C.2 #8; C.2 #43, a sync that
//     failed, comes with the connectors);
//   - ⚠ lines unreconciled for more than 30 days (C.2 #9);
//   - ⚠ lines marked possible duplicates not yet resolved (C.2 #51).
//
// Nothing is said when the organisation has no money account in Banking.

// The ages the checks allow (Appendix C.2 #8, #9).
const (
	staleFeedDays      = 3
	unreconciledDays   = 30
	maxLinesInDetail   = 5
	activityLinesLimit = 1_000_000
)

func listed(xs []string) string {
	if len(xs) > maxLinesInDetail {
		return strings.Join(xs[:maxLinesInDetail], ", ") + fmt.Sprintf(" and %d more", len(xs)-maxLinesInDetail)
	}
	return strings.Join(xs, ", ")
}

func bankChecks(ctx context.Context, u storage.UnitOfWork, base money.Currency, p storage.Period, today time.Time) ([]Check, error) {
	accounts, err := u.Banking().MoneyAccounts(ctx)
	if err != nil || len(accounts) == 0 {
		return nil, err
	}
	recs, err := u.BankMatching().Reconciliations(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	byAccount := map[uuid.UUID]storage.BankReconciliation{}
	for _, r := range recs {
		byAccount[r.MoneyAccountID] = r
	}
	day := today // the checks' day; the period's end when they run without a clock
	if day.IsZero() {
		day = p.EndsOn
	}
	var missing, stale, negative, unmatched, quiet []string
	for _, a := range accounts {
		if a.ClosedOn != nil && a.ClosedOn.Before(p.StartsOn) {
			continue
		}
		cur, err := money.Lookup(a.Currency)
		if err != nil {
			return nil, err
		}
		lines, err := u.Ledger().AccountLines(ctx, a.AccountID, p.StartsOn, p.EndsOn, activityLinesLimit)
		if err != nil {
			return nil, err
		}
		bal, _, err := banking.BookBalance(ctx, u, a.AccountID, cur, base, p.EndsOn)
		if err != nil {
			return nil, err
		}
		if bal.Sign() < 0 {
			negative = append(negative, fmt.Sprintf("%s (%s %s)", a.Name, bal, cur.Code))
		}
		r, ok := byAccount[a.ID]
		if len(lines) > 0 {
			var latest int64
			for _, l := range lines {
				latest = max(latest, l.LedgerRevision)
			}
			switch {
			case !ok || r.SignedBy == "":
				missing = append(missing, a.Name)
			case latest > r.LedgerRevision:
				stale = append(stale, a.Name)
			}
		}
		if ok && r.SignedBy != "" {
			if d, err := money.ParseStored(string(r.Difference), cur); err != nil {
				return nil, err
			} else if !d.IsZero() {
				unmatched = append(unmatched, fmt.Sprintf("%s (%s %s)", a.Name, d, cur.Code))
			}
		}
		if !today.IsZero() {
			fed, err := fedBy(ctx, u, a, today)
			if err != nil {
				return nil, err
			}
			if fed {
				f, err := banking.FreshnessOf(ctx, u, a.ID)
				if err != nil {
					return nil, err
				}
				if f.LastImport == nil || today.Sub(*f.LastImport) > staleFeedDays*24*time.Hour {
					quiet = append(quiet, a.Name)
				}
			}
		}
	}
	var out []Check
	switch {
	case len(missing) > 0 || len(stale) > 0:
		var parts []string
		if len(missing) > 0 {
			parts = append(parts, "no reconciliation signed off by a second person for "+listed(missing))
		}
		if len(stale) > 0 {
			parts = append(parts, "postings since the reconciliation of "+listed(stale)+": prepare it again and have it signed off")
		}
		out = append(out, Check{"bank-reconciled", Failed, "Money accounts are not reconciled", strings.Join(parts, "; ")})
	default:
		out = append(out, Check{"bank-reconciled", OK, "Every money account with activity is reconciled and signed off", ""})
	}
	if len(unmatched) > 0 {
		out = append(out, Check{"bank-unmatched", Warning, "Reconciliations do not match", "A difference is left on " + listed(unmatched) + ": explain it, or find what is missing"})
	}
	if len(negative) > 0 {
		out = append(out, Check{"bank-negative", Warning, "A bank or cash account is below zero at the month end", listed(negative) + ": check for a missing receipt or a line coded the wrong way"})
	}
	if len(quiet) > 0 {
		out = append(out, Check{"bank-stale", Warning, "Bank data is not up to date", fmt.Sprintf("Nothing has arrived for more than %d days for %s", staleFeedDays, listed(quiet))})
	}

	// Suspense at the period's end (C.1 #30).
	all, err := u.Accounts().List(ctx)
	if err != nil {
		return nil, err
	}
	for _, a := range all {
		if string(a.Role) != ledger.RoleSuspense {
			continue
		}
		_, v, err := banking.BookBalance(ctx, u, a.ID, base, base, p.EndsOn)
		if err != nil {
			return nil, err
		}
		if !v.IsZero() {
			out = append(out, Check{"suspense", Warning, "Suspense is not at nothing", fmt.Sprintf("%s holds %s %s: classify its lines before the close", a.Code, v, base.Code)})
		}
	}

	// Lines waiting: unreconciled for more than 30 days (C.2 #9), and possible duplicates (C.2 #51).
	old := day.AddDate(0, 0, -unreconciledDays)
	if old.After(p.EndsOn) {
		old = p.EndsOn
	}
	waiting, err := u.Banking().Lines(ctx, storage.BankLineFilter{To: &old, Statuses: []string{storage.LineUnreconciled}})
	if err != nil {
		return nil, err
	}
	if n := len(waiting); n > 0 {
		out = append(out, Check{"bank-old-lines", Warning, "Bank lines have waited more than 30 days", fmt.Sprintf("%d line(s) booked on or before %s are not reconciled",
			n, old.Format(time.DateOnly))})
	}
	dupes, err := u.Banking().Lines(ctx, storage.BankLineFilter{To: &p.EndsOn, Statuses: []string{storage.LinePossibleDuplicate}})
	if err != nil {
		return nil, err
	}
	if n := len(dupes); n > 0 {
		out = append(out, Check{"bank-duplicates", Warning, "Possible duplicate bank lines are not resolved", fmt.Sprintf("%d line(s): keep or exclude each in Banking", n)})
	}
	return out, nil
}

// fedBy says whether a money account's source on a day is a feed (emailed reports or a connector), which has a schedule
// to keep, rather than files a person uploads.
func fedBy(ctx context.Context, u storage.UnitOfWork, a storage.MoneyAccount, on time.Time) (bool, error) {
	sources, err := u.Banking().Sources(ctx, a.ID)
	if err != nil {
		return false, err
	}
	kind := ""
	for _, s := range sources {
		if !s.FromDate.After(on) {
			kind = s.Kind
		}
	}
	return kind == storage.MoneySourceEmail || kind == storage.MoneySourceConnector, nil
}
