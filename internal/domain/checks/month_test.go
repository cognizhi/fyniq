package checks

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/financials"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A month check turns each open month whose statement fails its check into an item naming the month, with the
// statement's words: the movement in equity here, whose own failure the statements' tests trigger (BS_EqChk), as a sound
// ledger never shows it.
// req: C.1 #23
// req: INV-11
func TestAMonthWhoseEquityDoesNotReconcileIsNamed(t *testing.T) {
	sgd, _ := money.Lookup("SGD")
	oct := storage.Period{ID: uuid.New(), StartsOn: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), EndsOn: time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC),
		Status: storage.PeriodOpen}
	nov := storage.Period{ID: uuid.New(), StartsOn: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), EndsOn: time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC),
		Status: storage.PeriodOpen}
	e := &Env{Base: sgd, Today: time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC), Periods: []storage.Period{oct, nov}, statements: map[string]financials.Statement{
		"cf/0": {Checks: []financials.Check{{Key: "equity", Title: "The movement in equity does not reconcile",
			Detail: "Equity at the end differs from its movement by 0.01 SGD: report it"}}},
		"cf/1": {Checks: []financials.Check{{Key: "equity", OK: true}}},
	}}
	d, _ := Find("books.equity-movement-reconciles")
	r, err := d.Run(context.Background(), nil, e)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != Error || r.Count != 1 || len(r.Items) != 1 || r.Items[0].Label != "October 2026" || r.Items[0].ID != oct.ID.String() ||
		r.Items[0].Detail != "Equity at the end differs from its movement by 0.01 SGD: report it" || r.Fix == "" {
		t.Errorf("the month whose equity does not reconcile: %+v", r)
	}
	e.statements["cf/0"] = financials.Statement{Checks: []financials.Check{{Key: "equity", OK: true}}}
	if r, err := d.Run(context.Background(), nil, e); err != nil || r.Status != OK {
		t.Errorf("every month reconciles: %+v, %v", r, err)
	}
}
