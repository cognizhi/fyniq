package journal

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/claim"
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The nightly replay (ENG-06, QA-ENG-06; PRD-001 rev 0.67, provisional): every stored entry is posted again under the
// rule version that posted it, from the document version it names, and must come out the same in every stored field.
// A reversal must be the exact negation of the entry it reverses, under that entry's source and rule. A mismatch is
// recorded in the organisation's audit trail as a failed check, and the job fails, so its absence alert pages the
// platform owner: a rule's output changed under an old version, or the ledger holds an entry no rule would post.

// Mismatch is a stored entry the replay does not reproduce, and why.
type Mismatch struct {
	EntryID         uuid.UUID
	PostingIdentity string
	Why             string
}

// Replayed is one organisation's replay: how many entries it checked and the ones that did not reproduce.
type Replayed struct {
	Organisation uuid.UUID
	Entries      int
	Mismatches   []Mismatch
}

// replayPage is how many entries the replay reads at a time.
const replayPage = 500

// Replay re-runs every entry of u's organisation (ENG-06). It reads only: nothing is posted.
func Replay(ctx context.Context, u storage.UnitOfWork) (Replayed, error) {
	var out Replayed
	settings, err := loadSettings(ctx, u)
	if err != nil {
		return out, err
	}
	// The rules check that an account is active; one deactivated since a posting does not change what it posted.
	for id, a := range settings.Accounts {
		a.Active = true
		settings.Accounts[id] = a
	}
	o, err := u.Organisation(ctx)
	if err != nil {
		return out, err
	}
	out.Organisation = o.ID
	var after int64
	for {
		entries, err := u.Ledger().Entries(ctx, after, replayPage)
		if err != nil {
			return out, err
		}
		for _, e := range entries {
			out.Entries++
			why, err := replayOne(ctx, u, settings, o, e)
			if err != nil {
				return out, err
			}
			if why != "" {
				out.Mismatches = append(out.Mismatches, Mismatch{EntryID: e.ID, PostingIdentity: e.PostingIdentity, Why: why})
			}
			after = e.LedgerRevision
		}
		if len(entries) < replayPage {
			return out, nil
		}
	}
}

// replayOne says why an entry does not reproduce, or nothing when it does.
func replayOne(ctx context.Context, u storage.UnitOfWork, s ledger.Settings, o storage.Organisation, e storage.Entry) (string, error) {
	stored, err := fromStorage(e, s.Base)
	if err != nil {
		return "", err
	}
	if e.ReversesEntryID != nil {
		original, err := u.Ledger().Entry(ctx, *e.ReversesEntryID)
		if err != nil {
			return "", err
		}
		was, err := fromStorage(original, s.Base)
		if err != nil {
			return "", err
		}
		want, err := ledger.Reverse(was, stored.PostingDate, "")
		if err != nil {
			return err.Error(), nil
		}
		want.Memo = stored.Memo // a reversal's memo carries its reason, which nothing else keeps
		return strings.Join(ledger.Differences(want, stored), "; "), nil
	}
	if e.Source.Type == ledger.SourceBankTransfer { // a transfer between money accounts, with the rates its entry kept (FR-BNK-09)
		want, err := replayBankTransfer(ctx, u, s, e)
		if err != nil {
			return err.Error(), nil
		}
		return strings.Join(ledger.Differences(want, stored), "; "), nil
	}
	id, err := uuid.Parse(e.Source.ID)
	if err != nil {
		return fmt.Sprintf("its source %s %q is not a document", e.Source.Type, e.Source.ID), nil
	}
	doc, err := u.Documents().Version(ctx, id, e.Source.Version)
	if errors.Is(err, ports.ErrNotFound) {
		return fmt.Sprintf("its source %s %s has no version %d", e.Source.Type, e.Source.ID, e.Source.Version), nil
	}
	if err != nil {
		return "", err
	}
	if ledger.IsRelease(e.Source.Type) { // a month of a spread, from its document version and entry (FR-GL-03)
		want, err := replayRelease(ctx, u, doc, s, e)
		if err != nil {
			return err.Error(), nil
		}
		return strings.Join(ledger.Differences(want, stored), "; "), nil
	}
	if doc.Type == fx.DocumentFXRevaluation { // the month-end revaluation, from what its run recorded (FR-GL-04)
		r, rs, err := fx.ReadRevaluation(ctx, u, doc)
		if err != nil {
			return err.Error(), nil
		}
		want, err := ledger.ReplayRevaluation(e.RuleVersion, r, rs)
		if err != nil {
			return err.Error(), nil
		}
		return strings.Join(ledger.Differences(want, stored), "; "), nil
	}
	if doc.Type == storage.DocumentSupplierRefund { // money a supplier paid back, with the rates and carrying amount it kept (FR-AP-03)
		want, err := replaySupplierRefund(ctx, u, doc, s, e)
		if err != nil {
			return err.Error(), nil
		}
		return strings.Join(ledger.Differences(want, stored), "; "), nil
	}
	if doc.Type == storage.DocumentCreditApplication { // a credit applied to a bill, with the rates and amounts it cleared (FR-AP-03)
		want, err := replayCreditApplication(ctx, u, doc, s, e)
		if err != nil {
			return err.Error(), nil
		}
		return strings.Join(ledger.Differences(want, stored), "; "), nil
	}
	if doc.Type == storage.DocumentBillPayment { // a payment of bills, with the rates and carrying amounts it kept (FR-AP-02)
		want, err := replayBillPayment(ctx, u, doc, s, e)
		if err != nil {
			return err.Error(), nil
		}
		return strings.Join(ledger.Differences(want, stored), "; "), nil
	}
	if doc.Type == storage.DocumentCustomerCreditApplication { // a credit applied to an invoice, with the rates and amounts it cleared (FR-AR-04)
		want, err := replayCustomerCreditApplication(ctx, u, doc, s, e)
		if err != nil {
			return err.Error(), nil
		}
		return strings.Join(ledger.Differences(want, stored), "; "), nil
	}
	if doc.Type == storage.DocumentCustomerRefund { // money paid back to a customer, with the rates and carrying amount it kept (FR-AR-04)
		want, err := replayCustomerRefund(ctx, u, doc, s, e)
		if err != nil {
			return err.Error(), nil
		}
		return strings.Join(ledger.Differences(want, stored), "; "), nil
	}
	if doc.Type == storage.DocumentCustomerDeposit { // money a customer paid ahead, with the rate and the GST its entry kept (FR-AR-09)
		want, err := replayCustomerDeposit(ctx, u, doc, s, e)
		if err != nil {
			return err.Error(), nil
		}
		return strings.Join(ledger.Differences(want, stored), "; "), nil
	}
	if doc.Type == storage.DocumentBankTransaction { // a categorised bank line, with the rates its version kept (FR-BNK-06)
		want, err := replayBankTransaction(doc, s, e)
		if err != nil {
			return err.Error(), nil
		}
		return strings.Join(ledger.Differences(want, stored), "; "), nil
	}
	if doc.Type == storage.DocumentWriteOff { // what is left of an invoice written off, with the rate and carrying amount it kept (FR-AR-17)
		want, err := replayWriteOff(ctx, u, doc, s, e)
		if err != nil {
			return err.Error(), nil
		}
		return strings.Join(ledger.Differences(want, stored), "; "), nil
	}
	if doc.Type == storage.DocumentCustomerReceipt { // money a customer paid, with the rates and carrying amounts it kept (FR-AR-18)
		want, err := replayCustomerReceipt(ctx, u, doc, s, e)
		if err != nil {
			return err.Error(), nil
		}
		return strings.Join(ledger.Differences(want, stored), "; "), nil
	}
	if invoice.IsInvoice(doc) { // a customer's invoice, with the rate and the GST its entry kept (FR-AR-03)
		want, err := replayInvoice(ctx, u, doc, s, e)
		if err != nil {
			return err.Error(), nil
		}
		return strings.Join(ledger.Differences(want, stored), "; "), nil
	}
	if doc.Type == storage.DocumentClaimReimbursement { // money paid back to a claimant, settling their claims (FR-AP-07)
		want, err := replayClaimReimbursement(ctx, u, doc, s, e)
		if err != nil {
			return err.Error(), nil
		}
		return strings.Join(ledger.Differences(want, stored), "; "), nil
	}
	if claim.IsClaim(doc) { // a member's expense claim, with the accounts, rates and input tax its entry kept (FR-AP-07)
		want, err := replayExpenseClaim(ctx, u, doc, s, e)
		if err != nil {
			return err.Error(), nil
		}
		return strings.Join(ledger.Differences(want, stored), "; "), nil
	}
	if bill.IsBill(doc) { // a supplier's bill or credit note, with the rate and input tax its entry kept (FR-AP-02, FR-AP-03)
		want, err := replayBill(ctx, u, doc, s, e)
		if err != nil {
			return err.Error(), nil
		}
		return strings.Join(ledger.Differences(want, stored), "; "), nil
	}
	j, err := decode(doc, s.Base)
	if err != nil {
		return err.Error(), nil
	}
	if doc.Type == storage.DocumentOpeningBalance && o.Profile.BooksStart != nil {
		j.Date = *o.Profile.BooksStart // as PostOpening dates them: the books start, fixed once a period exists
	}
	ratedAs(&j, stored, s.Base) // the cascade's rates, as the entry keeps them (ENG-14, ENG-40)
	want, err := ledger.Replay(e.RuleVersion, j, s)
	if err != nil {
		return err.Error(), nil
	}
	return strings.Join(ledger.Differences(want, stored), "; "), nil
}

// The check a failed replay records in the audit trail.
const replayCheck = "ledger.replay"

// ReplayOrganisation replays one organisation's entries in store. A mismatch is recorded in its audit trail as
// control.check.failed with the first entry and why; the stored entries are never touched.
func ReplayOrganisation(ctx context.Context, store storage.Store, org uuid.UUID) (Replayed, error) {
	var r Replayed
	err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		if r, err = Replay(ctx, u); err != nil || len(r.Mismatches) == 0 {
			return err
		}
		first := r.Mismatches[0]
		return u.Audit().Record(ctx, storage.AuditRecord{Action: balance.ActionCheckFailed, ObjectType: "organisation", ObjectID: org,
			Changes: map[string][2]any{"requirement": {nil, "QA-ENG-06"}, "check": {nil, replayCheck}, "entries": {nil, r.Entries},
				"mismatches": {nil, len(r.Mismatches)}, "first_entry": {nil, first.EntryID.String()}, "first_reason": {nil, first.Why}}})
	})
	return r, err
}

// ReplayAll replays every organisation's entries (the nightly job ledger.replay). A mismatch fails the run, so the
// job's absence alert pages the platform owner: never "fixed" by changing a stored entry, only by restoring the rule
// version that posted it (ENG-06).
func ReplayAll(ctx context.Context, store storage.Store) ([]Replayed, error) {
	orgs, err := store.Organisations(ctx)
	if err != nil {
		return nil, err
	}
	var out []Replayed
	failed := 0
	for _, org := range orgs {
		r, err := ReplayOrganisation(ctx, store, org)
		if err != nil {
			return out, fmt.Errorf("organisation %s: %w", org, err)
		}
		out = append(out, r)
		if len(r.Mismatches) > 0 {
			failed++
		}
	}
	if failed > 0 {
		return out, fmt.Errorf("%d organisation(s) with entries their rule versions no longer reproduce (ENG-06, QA-ENG-06)", failed)
	}
	return out, nil
}
