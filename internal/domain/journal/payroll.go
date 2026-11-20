package journal

import (
	"context"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// PostPayrollRunIn posts one verified version of a pay run in u (payroll_run@1, FR-PAY-07, FR-PPL-04, FR-PPL-11): its difference from what the run's
// earlier versions posted, as an entry under the rule, the period locks and the database's own checks like any posting. A version that changes
// nothing posts nothing: the returned stored entry has no ID. The caller verified the run's authority first (FR-PPL-09) and keeps the receipt in
// the same unit of work, without which the database refuses the entry.
func PostPayrollRunIn(ctx context.Context, u storage.UnitOfWork, run ledger.PayrollRun, prior ledger.PayrollPosted) (ledger.Entry, storage.Entry, error) {
	settings, err := loadSettings(ctx, u)
	if err != nil {
		return ledger.Entry{}, storage.Entry{}, err
	}
	entry, err := ledger.PostPayrollRun(run, prior, settings)
	if err != nil || len(entry.Lines) == 0 {
		return entry, storage.Entry{}, err
	}
	stored, err := appendEntry(ctx, u, toStorage(entry, nil))
	return entry, stored, err
}

// PayrollNetPay is what a run's posted totals owe on 2100 Salaries payable (FR-PPL-12): the most that payment lines for it may pay.
func PayrollNetPay(ctx context.Context, u storage.UnitOfWork, posted ledger.PayrollPosted, base money.Currency) (money.Money, error) {
	settings, err := loadSettings(ctx, u)
	if err != nil {
		return money.Money{}, err
	}
	return posted.NetPay(settings, base)
}
