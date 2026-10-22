package journal

import (
	"context"
	"errors"
	"fmt"

	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// rated gives each foreign-currency line of j without a rate of its own the rate the cascade finds for the journal's
// date (ENG-40; PRD-001 rev 0.71), with its source: a typed rate is the document's own and stays. A line the cascade
// finds no rate for keeps none and comes back as FX-01's problem. A journal without a date takes no rate yet.
func rated(ctx context.Context, u storage.UnitOfWork, j *ledger.Journal, base money.Currency) (ledger.Problems, error) {
	if j.Date.IsZero() {
		return nil, nil
	}
	var missing ledger.Problems
	for i := range j.Lines {
		l := &j.Lines[i]
		cur := l.Amount.Currency()
		if l.Rate != nil || cur.Code == "" || cur == base {
			continue
		}
		f, err := fx.Lookup(ctx, u, base, cur, j.Date)
		var fe *fx.Error
		if errors.As(err, &fe) {
			missing = append(missing, ledger.Problem{Code: fe.Code, Field: fmt.Sprintf("lines[%d].fx_rate", i+1), Message: fe.Message, Fix: fe.Fix})
			continue
		}
		if err != nil {
			return nil, err
		}
		r := f.Rate
		l.Rate = &r
	}
	return missing, nil
}

// postRated runs the posting rules over j once the cascade has given its lines their rates. For a line it found no
// rate for, its FX-01 takes the place of the rules' "needs a rate", in the rules' order.
func postRated(ctx context.Context, u storage.UnitOfWork, j ledger.Journal, settings ledger.Settings) (ledger.Entry, error) {
	missing, err := rated(ctx, u, &j, settings.Base)
	if err != nil {
		return ledger.Entry{}, err
	}
	entry, err := ledger.PostJournal(j, settings)
	return entry, withMissingRates(err, missing)
}

// withMissingRates puts each FX-01 of missing in place of the rules' missing-rate problem on the same line.
func withMissingRates(err error, missing ledger.Problems) error {
	var problems ledger.Problems
	if len(missing) == 0 || !errors.As(err, &problems) {
		return err
	}
	byField := make(map[string]ledger.Problem, len(missing))
	for _, m := range missing {
		byField[m.Field] = m
	}
	out := make(ledger.Problems, 0, len(problems))
	for _, p := range problems {
		if m, ok := byField[p.Field]; ok && p.Code == ledger.CodeMissingRate {
			p = m
		}
		out = append(out, p)
	}
	return out
}

// ratedAs gives each foreign-currency line of j without a rate of its own the rate the line of e at its place took:
// the rate the cascade found when the journal posted, which the entry keeps (ENG-14). Replay reads the rate from there,
// as the cascade's tables may have changed since.
func ratedAs(j *ledger.Journal, e ledger.Entry, base money.Currency) {
	for i := range j.Lines {
		l := &j.Lines[i]
		if l.Rate != nil || l.Amount.Currency() == base || i >= len(e.Lines) {
			continue
		}
		if r := e.Lines[i].Rate; r.From == l.Amount.Currency() {
			l.Rate = &r
		}
	}
}
