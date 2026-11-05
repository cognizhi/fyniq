package checks

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/closing"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Each check the close runs itself takes the close's findings of its own keys for the month to close next (design D2):
// ✖ when one refuses the close, ⚠ when one warns, OK when they pass, with an item for each finding naming the month and
// opening its close. The close's own tests trigger each finding; here each check is shown to read its keys, and only
// them, and the month-end ones to wait for the month to end.
// req: C.1 #22
// req: C.1 #28
// req: C.1 #29
// req: C.1 #30
// req: C.1 #31
// req: C.1 #32
// req: C.2 #2
// req: C.2 #8
// req: C.2 #9
// req: C.2 #51
// req: C.2 #56
// req: C.2 #57
// req: C.2 #58
// req: C.2 #59
// req: C.2 #60
func TestEachCloseBackedCheckReadsItsOwnFindings(t *testing.T) {
	sgd, _ := money.Lookup("SGD")
	oct := storage.Period{ID: uuid.New(), StartsOn: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), EndsOn: time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC),
		Status: storage.PeriodOpen}
	env := func(today time.Time, found []closing.Check) *Env {
		return &Env{Base: sgd, Today: today, Periods: []storage.Period{oct}, close: &closeRun{done: true, period: &oct, checks: found}}
	}
	after := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	for _, d := range Catalogue() {
		if len(d.CloseKeys) == 0 || slicesHas([]string{"C.1 #1", "C.1 #3", "C.1 #5", "C.1 #23", "C.2 #1", "C.2 #23", "C.2 #49"}, d.Ref) {
			continue // the books' checks read the statements and the balance service (slice 1); C.2 #49 reads the write-offs too;
			// C.2 #23 reads the confirmations at the latest year end itself, as the close does (founders_test.go)
		}
		t.Run(d.Ref, func(t *testing.T) {
			var bad, fine []closing.Check
			for _, k := range d.CloseKeys {
				sev := closing.Warning
				if d.Worst == Error && k == d.CloseKeys[0] {
					sev = closing.Failed
				}
				bad = append(bad, closing.Check{Key: k, Severity: sev, Title: "Found " + k, Detail: "what " + k + " found"})
				fine = append(fine, closing.Check{Key: k, Severity: closing.OK, Title: k + " is fine"})
			}
			others := append([]closing.Check{{Key: "someone-elses", Severity: closing.Failed, Title: "Not mine", Detail: "x"}}, bad...)
			r, err := d.Run(context.Background(), nil, env(after, others))
			if err != nil {
				t.Fatal(err)
			}
			if r.Status != d.Worst || len(r.Items) != len(d.CloseKeys) || r.Items[0].Kind != "close" || r.Items[0].ID != oct.ID.String()+"/"+d.CloseKeys[0] ||
				r.Items[0].Label != "October 2026" {
				t.Errorf("its findings: %+v", r)
			}
			if r, err := d.Run(context.Background(), nil, env(after, fine)); err != nil || r.Status != OK {
				t.Errorf("its keys passing: %+v, %v", r, err)
			}
			if r, err := d.Run(context.Background(), nil, &Env{Base: sgd, Today: after}); err != nil || (r.Status != NotAssessed && r.Status != OK) {
				t.Errorf("no month to close: %+v, %v", r, err)
			}
			if slicesHas([]string{"C.2 #56", "C.2 #57", "C.2 #58", "C.2 #59"}, d.Ref) {
				during := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
				if r, err := d.Run(context.Background(), nil, env(during, bad)); err != nil || r.Status != OK || r.Detail != "October 2026 has not ended: this is checked once it has" {
					t.Errorf("the month not ended: %+v, %v", r, err)
				}
			}
		})
	}
}

func slicesHas(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}
