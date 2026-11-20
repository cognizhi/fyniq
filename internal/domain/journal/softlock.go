package journal

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Soft locks (FR-GL-09; p2-mobile-approvals-automation design D6).
//
// The hard close is the database's (INV-03) and is not touched here. A soft lock is the domain's: a posting into a soft-locked period, and a posting of a locked module
// into an open period, is refused unless the person who posts holds a live pass of that period, made by an Accountant with a reason (storage.SoftLocks). Every posting made
// under a pass is kept on the exceptions report in the same unit of work. appendEntry is the only place the domain writes an entry (an architecture test holds that), so
// a posting source added later cannot skip the lock. It is closed by default: the worker, the proposal executor, an integration client and an agent act as a service or an
// integration, never as a person with a pass, so every one of them is refused.

// ModuleOf is the module a posting source belongs to for the per-module soft locks: ar, ap, banking or payroll; "" for a source only the whole-period lock stops.
// A test holds that every ledger.Source* constant is listed here, either way.
func ModuleOf(sourceType string) string {
	switch sourceType {
	case ledger.SourceInvoice, ledger.SourceInvoiceRelease, ledger.SourceCreditNote, ledger.SourceCustomerReceipt, ledger.SourceCustomerRefund,
		ledger.SourceCustomerDeposit, ledger.SourceCustomerCreditApplication, ledger.SourceBadDebtRelief, ledger.SourceWriteOff:
		return storage.ModuleAR
	case ledger.SourceBill, ledger.SourceBillRelease, ledger.SourceBillPayment, ledger.SourceVendorCredit, ledger.SourceCreditApplication, ledger.SourceSupplierRefund,
		ledger.SourceExpenseClaim, ledger.SourceClaimReimbursement, ledger.SourceCashAdvance:
		return storage.ModuleAP
	case ledger.SourceBankTransaction, ledger.SourceBankTransfer, ledger.SourceBankInRelease, ledger.SourceBankOutRelease:
		return storage.ModuleBanking
	case ledger.SourcePayrollRun:
		return storage.ModulePayroll
	}
	return ""
}

// NoModule lists the sources that belong to no module: only a period's own soft lock stops them.
var NoModule = []string{ledger.SourceManualJournal, ledger.SourceOpeningBalance, ledger.SourceAccountMerge, ledger.SourceAssetDepreciation, ledger.SourceAssetDisposal,
	ledger.SourceDirectorFee, ledger.SourceDirectorRepayment, ledger.SourceDirectorSettlement, ledger.SourceEquityTransaction, ledger.SourceFXRevaluation,
	ledger.SourcePreIncorporationCost, ledger.SourceShareIssue}

// appendEntry writes an entry as storage.Ledger.Append does, once the soft locks allow it.
func appendEntry(ctx context.Context, u storage.UnitOfWork, e storage.NewEntry) (storage.Entry, error) {
	// ENG-47: the ledger lock first, so a soft lock being set and this posting never interleave.
	if _, err := u.Ledger().Lock(ctx); err != nil {
		return storage.Entry{}, err
	}
	locks := u.SoftLocks()
	p, found, err := locks.OfDate(ctx, e.PostingDate)
	if err != nil {
		return storage.Entry{}, err
	}
	scope := ""
	if found {
		switch p.Status {
		case storage.PeriodSoftLocked:
			scope = storage.ScopePeriod
		case storage.PeriodOpen:
			if m := ModuleOf(e.Source.Type); m != "" {
				locked, err := locks.LockedModules(ctx, p.ID)
				if err != nil {
					return storage.Entry{}, err
				}
				for _, l := range locked {
					if l == m {
						scope = m
					}
				}
			}
		}
	}
	if scope == "" {
		return u.Ledger().Append(ctx, e)
	}
	what := fmt.Sprintf("The period %s to %s is soft-locked", p.StartsOn.Format("2 Jan"), p.EndsOn.Format("2 Jan 2006"))
	if scope != storage.ScopePeriod {
		what = fmt.Sprintf("The %s module is locked for %s to %s", scope, p.StartsOn.Format("2 Jan"), p.EndsOn.Format("2 Jan 2006"))
	}
	refuse := &storage.Error{Code: storage.CodeSoftLocked, Message: what + ", so this cannot post on " + e.PostingDate.Format("2 Jan 2006") + " without an exception",
		Hint: "Ask an Accountant to make an exception with a reason, or ask for the lock to be released (FR-GL-09)"}
	from := storage.AuditFrom(ctx)
	who := from.Actor
	// A job, an integration and an agent never hold a pass, and nor does the proposal executor, which acts as the approving person on behalf of another and carries the
	// AI's proposal: a pass is for a person's own posting, never for the AI's (AI-10, golden rule 5).
	if who == nil || who.Type != "user" || from.OnBehalfOf != nil || from.Originator != "" {
		return storage.Entry{}, refuse
	}
	user, err := uuid.Parse(who.ID)
	if err != nil {
		return storage.Entry{}, refuse
	}
	pass, ok, err := locks.LivePass(ctx, p.ID, user)
	if err != nil {
		return storage.Entry{}, err
	}
	if !ok {
		return storage.Entry{}, refuse
	}
	posted, err := u.Ledger().Append(ctx, e)
	if err != nil {
		return storage.Entry{}, err
	}
	_, err = locks.RecordException(ctx, storage.SoftLockException{PassID: pass.ID, PeriodID: p.ID, EntryID: posted.ID, UserID: user, Scope: scope, SourceType: e.Source.Type,
		PostingDate: e.PostingDate, Reason: pass.Reason})
	return posted, err
}
