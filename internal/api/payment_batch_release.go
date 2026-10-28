package api

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/billpayment"
	"github.com/cognizhi/fyniq/internal/domain/claim"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/payee"
	"github.com/cognizhi/fyniq/internal/domain/paymentbatch"
	"github.com/cognizhi/fyniq/internal/domain/paymentfile"
	"github.com/cognizhi/fyniq/internal/domain/team"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ approving, releasing and cancelling a payment batch
//
// A batch is approved through the approval engine by someone who holds A on "Payment batches", never its preparer, its
// envelope holding its version, status, total and each payee's account in force (FR-APR-05, FR-APR-10, G4-055); approval
// numbers it. It is released once by the Payment releaser, a person stepped up, under its latest approval, whose accounts
// and total must still be the batch's (else it is approved again, PB-17): the release posts one bill payment per supplier
// and one reimbursement per claimant as recording them by hand does, writes the payment list sealed with its SHA-256, and
// links them (design D3, D4, D5, D7). In Standard mode the releaser neither prepared nor approved the batch (SOD-2) nor
// the account change of a payee it pays (SOD-1); in Founder mode they may have, and each such release is logged for the
// weekly review (FM-3).

// The codes of approving and releasing a batch.
const (
	CodeBatchReapprove  = "PB-17" // a payee's account or the total changed since the batch was approved: approve it again
	CodeBatchReleased   = "PB-18" // the batch is released already
	CodeBatchSeparation = "PB-19" // in Standard mode, the releaser prepared or approved the batch or a payee's account change
	CodeBatchNotOpen    = "PB-20" // the batch is not approved, or not released, as the command needs
	CodeBatchOwn        = "PB-21" // whoever prepared the batch never approves it
	CodeBatchReason     = "PB-22" // cancelling needs a reason
)

// The writer of released batches' files until the bank is chosen (rev 0.156).
var batchWriter paymentfile.Writer = paymentfile.PaymentList{}

// PaymentBatchRelease is a batch's release as the API shows it: never the file's content.
type PaymentBatchRelease struct {
	ReleasedBy Person   `json:"released_by" doc:"Who released it."`
	ReleasedAt string   `json:"released_at" format:"date-time" doc:"When it was released." example:"2026-10-31T09:30:00+08:00"`
	ApprovalID string   `json:"approval_id" format:"uuid" doc:"The approval it was released under." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5c20"`
	Writer     string   `json:"writer" doc:"The file's format and version: payment-list@1 is the payment list keyed into the bank (rev 0.156)." example:"payment-list@1"`
	FileName   string   `json:"file_name" doc:"The file's name." example:"PB-0003-payment-list.csv"`
	SHA256     string   `json:"sha256" doc:"The SHA-256 of the file as written, in hexadecimal: a downloaded copy can be checked against it." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	Payments   []string `json:"payment_ids" nullable:"false" doc:"The bill payments and reimbursements it posted, one per payee." example:"[\"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5d10\"]"`
}

// PaymentBatchFile is a released batch's file: its content in base64. It holds full account numbers.
type PaymentBatchFile struct {
	FileName  string `json:"file_name" doc:"The file's name." example:"PB-0003-payment-list.csv"`
	MediaType string `json:"media_type" doc:"Its type." example:"text/csv"`
	SHA256    string `json:"sha256" doc:"The SHA-256 of the content, in hexadecimal: the release's." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	Size      int    `json:"size_bytes" doc:"Its size, in bytes." example:"512"`
	Content   string `json:"content_base64" doc:"The file, in base64. It holds full account numbers: keep it only as long as it takes to pay." example:"UGF5ZWUsUGF5ZWUgdHlwZQ=="`
}

// PaymentBatchCancel is why an approved batch is cancelled.
type PaymentBatchCancel struct {
	Reason string `json:"reason" minLength:"1" maxLength:"500" doc:"Why it is cancelled: its bills and claims are free for another batch, or to pay by hand." example:"The supplier asked to be paid by cheque"`
}

type batchCommandInput struct {
	PaymentBatchPath
	KeyHeader
	IfMatchHeader
	PreferHeader
}

type batchCancelInput struct {
	PaymentBatchPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body PaymentBatchCancel
}

type batchRequestOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was asked (FR-INT-14)."`
	Body     Approval
}

type batchReleaseOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The batch's version and status, for If-Match."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was released (FR-INT-14)."`
	Body     PaymentBatch
}

type batchFileOutput struct{ Body PaymentBatchFile }

// batchAAD binds a release's sealed file to its batch in the organisation: copied to another, it does not open.
func batchAAD(org, batch uuid.UUID) []byte {
	return []byte("payment_batch_release:" + org.String() + ":" + batch.String())
}

// batchDependencies are a batch's version and status, its total in SGD, and each payee's account in force (G4-055).
func (a *API) batchDependencies(ctx context.Context, u storage.UnitOfWork, key string) (int, map[string]any, error) {
	id, err := uuid.Parse(key)
	if err != nil {
		return 0, nil, err
	}
	doc, err := batchDoc(ctx, u, id, false)
	if err != nil {
		return 0, nil, err
	}
	v, err := a.batchView(ctx, u)
	if err != nil {
		return 0, nil, err
	}
	b, err := paymentbatch.Evaluate(ctx, u, doc, v.settings, v.approved, v.now)
	if err != nil {
		return 0, nil, err
	}
	return doc.Version, map[string]any{"status": string(doc.Status), "total": b.Total.String(), "accounts": b.Accounts()}, nil
}

// batchExecutable refuses a signer who prepared the batch (SOD-2, FM-1); the requester is refused by the engine.
func batchExecutable(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest, signer uuid.UUID) error {
	id, err := uuid.Parse(r.ObjectKey)
	if err != nil {
		return err
	}
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return err
	}
	if doc.SavedBy == (storage.Actor{Type: "user", ID: signer.String()}).String() {
		return problem(CodeBatchOwn, "You prepared this batch, so another person approves it (FM-1).", "Ask another person who holds the approval right on payment batches")
	}
	return nil
}

// executeBatchApproval approves a batch on its approval: its checks again, then its number, and the batch posted with no
// entry (the database refuses it without a second person's approval, FY242). A re-approval changes nothing but the
// approval the release checks.
func (a *API) executeBatchApproval(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest) error {
	id, err := uuid.Parse(r.ObjectKey)
	if err != nil {
		return err
	}
	doc, err := batchDoc(ctx, u, id, true)
	if err != nil {
		return err
	}
	v, err := a.batchView(ctx, u)
	if err != nil {
		return err
	}
	b, err := paymentbatch.Evaluate(ctx, u, doc, v.settings, v.approved, v.now)
	if err != nil {
		return err
	}
	if len(b.Errors) > 0 {
		return batchProblems(b.Errors)
	}
	if r.Operation == approval.OpBatchReapprove {
		rel, err := u.PaymentBatches().ReleaseOf(ctx, id)
		if err != nil {
			return err
		}
		if doc.Status != storage.DocumentPosted || rel != nil {
			return problem(CodeBatchNotOpen, "The batch is not approved and waiting for release, so it is not approved again.", "Nothing to approve")
		}
		return nil
	}
	if doc.Status != storage.DocumentDraft {
		return problem(paymentbatch.CodeNotDraft, "The batch is approved already.", "Release it, or cancel it")
	}
	if _, err := u.Sequences().Number(ctx, id, storage.DocumentPaymentBatch); err != nil {
		return err
	}
	return u.Documents().SetStatus(ctx, id, storage.DocumentPosted)
}

// latestApproval is a batch's latest approved request, approval or re-approval, with its envelope's dependencies.
func latestApproval(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (*storage.ApprovalRequest, map[string]any, error) {
	approved, err := u.Approvals().List(ctx, string(approval.Approved))
	if err != nil {
		return nil, nil, err
	}
	var latest *storage.ApprovalRequest
	for _, r := range approved {
		if r.ObjectType == approval.ObjectPaymentBatch && r.ObjectKey == id.String() &&
			(latest == nil || r.DecidedAt != nil && latest.DecidedAt != nil && !r.DecidedAt.Before(*latest.DecidedAt)) {
			x := r
			latest = &x
		}
	}
	if latest == nil {
		return nil, nil, nil
	}
	var env struct {
		Dependencies map[string]any `json:"dependencies"`
	}
	if err := json.Unmarshal([]byte(latest.Envelope), &env); err != nil {
		return nil, nil, err
	}
	return latest, env.Dependencies, nil
}

// sameAccounts reports whether an envelope's accounts are the batch's now.
func sameAccounts(signed any, now map[string]any) bool {
	m, ok := signed.(map[string]any)
	if !ok || len(m) != len(now) {
		return false
	}
	for k, v := range now {
		if fmt.Sprint(m[k]) != fmt.Sprint(v) {
			return false
		}
	}
	return true
}

// actorIDs are the users behind a change's preparer and its approval's requester and signers.
func actorIDs(ctx context.Context, u storage.UnitOfWork, savedBy string, r *storage.ApprovalRequest) ([]uuid.UUID, error) {
	var out []uuid.UUID
	if id, ok := strings.CutPrefix(savedBy, "user:"); ok {
		if x, err := uuid.Parse(id); err == nil {
			out = append(out, x)
		}
	}
	if r == nil {
		return out, nil
	}
	out = append(out, r.RequestedBy)
	sigs, err := u.Approvals().Signatures(ctx, r.ID)
	if err != nil {
		return nil, err
	}
	for _, s := range sigs {
		out = append(out, s.Signer)
	}
	return out, nil
}

// changeApproval is the approved request that put a change of bank details in force.
func changeApproval(ctx context.Context, u storage.UnitOfWork, change uuid.UUID) (*storage.ApprovalRequest, error) {
	approved, err := u.Approvals().List(ctx, string(approval.Approved))
	if err != nil {
		return nil, err
	}
	for _, r := range approved {
		if (r.ObjectType == approval.ObjectPayeeBankChange || r.ObjectType == approval.ObjectClaimantBankChange) && r.ObjectKey == change.String() {
			x := r
			return &x, nil
		}
	}
	return nil, nil
}

// conflicts are the separation rules a release by releaser breaks: SOD-2, preparing, asking for or approving the batch;
// SOD-1, preparing, asking for or approving the account change of a payee it pays.
func conflicts(ctx context.Context, u storage.UnitOfWork, doc storage.Document, b paymentbatch.Batch, r *storage.ApprovalRequest,
	releaser uuid.UUID) ([]string, error) {
	var out []string
	batchPeople, err := actorIDs(ctx, u, doc.SavedBy, r)
	if err != nil {
		return nil, err
	}
	if slices.Contains(batchPeople, releaser) {
		out = append(out, "SOD-2: the releaser prepared, asked for or approved the batch")
	}
	for _, p := range b.Payments {
		if p.Account == nil {
			continue
		}
		change, err := u.Documents().Get(ctx, p.Account.ChangeID)
		if err != nil {
			return nil, err
		}
		cr, err := changeApproval(ctx, u, change.ID)
		if err != nil {
			return nil, err
		}
		people, err := actorIDs(ctx, u, change.SavedBy, cr)
		if err != nil {
			return nil, err
		}
		if slices.Contains(people, releaser) {
			out = append(out, fmt.Sprintf("SOD-1: the releaser prepared, asked for or approved the bank account it pays %s", paymentbatch.Key(p.Payee)))
		}
	}
	return out, nil
}

// releaseBatch releases an approved batch as p, in u: every check again, the separation rules, the payment list sealed,
// the release, one payment per payee posted as by hand, and each linked (design D4, D5, D7).
func (a *API) releaseBatch(ctx context.Context, u storage.UnitOfWork, p authn.Principal, id uuid.UUID, ifMatch string) (PaymentBatch, error) {
	doc, err := batchDoc(ctx, u, id, true)
	if err != nil {
		return PaymentBatch{}, err
	}
	if err := checkIfMatch(ifMatch, batchETag(doc)); err != nil {
		return PaymentBatch{}, err
	}
	if doc.Status != storage.DocumentPosted {
		return PaymentBatch{}, problem(CodeBatchNotOpen, "The batch is not approved, so it is not released.", "Ask for its approval: another person approves it first")
	}
	if rel, err := u.PaymentBatches().ReleaseOf(ctx, id); err != nil {
		return PaymentBatch{}, err
	} else if rel != nil {
		return PaymentBatch{}, problem(CodeBatchReleased, "The batch is released already: its payments are posted.", "Download its payment list, or match its payments")
	}
	v, err := a.batchView(ctx, u)
	if err != nil {
		return PaymentBatch{}, err
	}
	b, err := paymentbatch.Evaluate(ctx, u, doc, v.settings, v.approved, v.now)
	if err != nil {
		return PaymentBatch{}, err
	}
	if len(b.Errors) > 0 {
		return PaymentBatch{}, batchProblems(b.Errors)
	}
	r, deps, err := latestApproval(ctx, u, id)
	if err != nil {
		return PaymentBatch{}, err
	}
	if r == nil || !sameAccounts(deps["accounts"], b.Accounts()) || fmt.Sprint(deps["total"]) != b.Total.String() {
		return PaymentBatch{}, problem(CodeBatchReapprove, "A payee's bank account, or what is open on an item, changed since the batch was approved.",
			"Ask for its approval again: the approver sees the accounts and total it pays now (G4-055)")
	}
	found, err := conflicts(ctx, u, doc, b, r, p.UserID)
	if err != nil {
		return PaymentBatch{}, err
	}
	mode, _, _, err := teamModeOf(ctx, u)
	if err != nil {
		return PaymentBatch{}, err
	}
	if len(found) > 0 && team.Blocks(mode) {
		return PaymentBatch{}, problem(CodeBatchSeparation, "You cannot release this batch: "+strings.Join(found, "; ")+".",
			"Ask another Payment releaser, who neither prepared nor approved it, to release it (SOD-1, SOD-2)")
	}

	// The payment list: each payee's number opened here, and the file sealed before the transaction ends.
	f := paymentfile.File{Batch: doc.Number, PaymentDate: b.Date, Method: b.Stored.Method, Debit: paymentfile.Debit{Name: b.Account.Name, Identifier: b.Account.Identifier}}
	names, err := memberNames(ctx, u, v.now)
	if err != nil {
		return PaymentBatch{}, err
	}
	for _, pay := range b.Payments {
		number, err := payee.Open(ctx, a.deps.Fields, p.Organisation, *pay.Account)
		if err != nil {
			return PaymentBatch{}, err
		}
		item := paymentfile.Payment{BIC: pay.Bank.BIC, AccountName: pay.Bank.AccountName, Number: number, Amount: pay.Amount, Reference: batchReference(doc.Number, pay)}
		if pay.Payee.IsPerson() {
			item.Payee, item.Kind = names[pay.Payee.PersonID], "claimant"
		} else {
			c, err := u.Contacts().Get(ctx, pay.Payee.ContactID)
			if err != nil {
				return PaymentBatch{}, err
			}
			item.Payee, item.Kind = c.LegalName, "supplier"
		}
		f.Payments = append(f.Payments, item)
	}
	out, err := batchWriter.Write(f)
	if err != nil {
		return PaymentBatch{}, err
	}
	sum := sha256.Sum256(out.Data)
	if a.deps.Fields == nil {
		return PaymentBatch{}, payee.NoKeys()
	}
	sealed, err := a.deps.Fields.Seal(ctx, out.Data, batchAAD(p.Organisation, id))
	if err != nil {
		return PaymentBatch{}, err
	}
	release, err := u.PaymentBatches().Release(ctx, storage.PaymentBatchRelease{BatchID: id, BatchVersion: doc.Version, ApprovalID: r.ID, ReleasedBy: p.UserID,
		ReleasedAt: v.now, Writer: batchWriter.Name(), FileName: out.Name, MediaType: out.MediaType, Sealed: sealed, SHA256: sum[:],
		Payments: len(b.Payments), Currency: paymentbatch.SGD.Code, Total: storage.Decimal(b.Total.String())})
	if err != nil {
		return PaymentBatch{}, err
	}

	// One payment per payee, posted as recording it by hand does (design D5), and linked to the release.
	for _, pay := range b.Payments {
		var paymentID uuid.UUID
		if pay.Payee.IsPerson() {
			var parts []claim.Part
			for _, l := range pay.Lines {
				parts = append(parts, claim.Part{ClaimID: l.DocumentID, Amount: l.Open})
			}
			rec, err := a.reimbursements.RecordIn(ctx, u, claim.Reimbursement{ClaimantID: pay.Payee.PersonID, Date: b.Date, AccountID: b.Stored.MoneyAccountID,
				Reference: doc.Number, Claims: parts, Notes: "Paid in " + doc.Number})
			if err != nil {
				return PaymentBatch{}, err
			}
			paymentID = rec.Reimbursement.ID
		} else {
			var parts []billpayment.Part
			for _, l := range pay.Lines {
				parts = append(parts, billpayment.Part{BillID: l.DocumentID, Amount: l.Open, InFull: true})
			}
			rec, err := a.payments.RecordIn(ctx, u, billpayment.Draft{SupplierID: pay.Payee.ContactID, Date: b.Date, AccountID: b.Stored.MoneyAccountID,
				Currency: paymentbatch.SGD, Reference: doc.Number, Bills: parts, Unapplied: money.Zero(paymentbatch.SGD), Notes: "Paid in " + doc.Number})
			if err != nil {
				return PaymentBatch{}, err
			}
			paymentID = rec.Payment.ID
		}
		if err := u.PaymentBatches().Link(ctx, release.ID, paymentID); err != nil {
			return PaymentBatch{}, err
		}
	}

	// In Founder mode a conflict is allowed, with the approval as its control, and logged for the weekly review (FM-3).
	for _, c := range found {
		actor := p.UserID
		if _, err := u.ReviewLog().Log(ctx, storage.NewReviewLogEntry{Kind: storage.ReviewReleaseConflict, ObjectType: approval.ObjectPaymentBatch,
			ObjectKey: id.String(), Summary: fmt.Sprintf("%s released %s, %s SGD, approved under request %s: %s", names[p.UserID], doc.Number, b.Total.String(), r.ID, c),
			Actor: &actor, LoggedAt: v.now}); err != nil {
			return PaymentBatch{}, err
		}
	}
	if doc, err = u.Documents().Get(ctx, id); err != nil {
		return PaymentBatch{}, err
	}
	return a.batchOf(ctx, u, v, doc, false)
}

// batchReference is what a payee's statement shows: the batch's number and the documents it pays, at most 140 characters.
func batchReference(batch string, p paymentbatch.Payment) string {
	ref := batch
	for _, l := range p.Lines {
		next := ref + " " + l.Number
		if len(next) > 140 {
			break
		}
		ref = next
	}
	return ref
}

// releaseOf is a batch's release as the API shows it, nil while it has none.
func (a *API) releaseOf(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (*PaymentBatchRelease, error) {
	rel, err := u.PaymentBatches().ReleaseOf(ctx, id)
	if err != nil || rel == nil {
		return nil, err
	}
	ids, err := u.PaymentBatches().Payments(ctx, rel.ID)
	if err != nil {
		return nil, err
	}
	who := Person{Kind: "user", ID: rel.ReleasedBy.String()}
	if err := a.name(ctx, u, &who); err != nil {
		return nil, err
	}
	out := &PaymentBatchRelease{ReleasedBy: who, ReleasedAt: rel.ReleasedAt.UTC().Format(time.RFC3339), ApprovalID: rel.ApprovalID.String(), Writer: rel.Writer,
		FileName: rel.FileName, SHA256: hex.EncodeToString(rel.SHA256), Payments: []string{}}
	for _, x := range ids {
		out.Payments = append(out.Payments, x.String())
	}
	return out, nil
}

// BatchApprovalCodes are the codes an approval of a batch may answer: FM-1, and the batch's own checks.
func BatchApprovalCodes() []string {
	return append([]string{CodeBatchOwn, CodeBatchNotOpen, paymentbatch.CodeCurrency, paymentbatch.CodeNothingOpen, paymentbatch.CodeInAnother,
		paymentbatch.CodeNoAccount, paymentbatch.CodeWithholding, payee.CodeFrozen}, BatchCodes...)
}

func registerPaymentBatchRelease(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	checkCodes := []string{paymentbatch.CodeCurrency, paymentbatch.CodeNothingOpen, paymentbatch.CodeInAnother, paymentbatch.CodeNoAccount,
		paymentbatch.CodeWithholding, payee.CodeFrozen}

	ask := func(op string) func(ctx context.Context, in *batchCommandInput) (*batchRequestOutput, error) {
		return func(ctx context.Context, in *batchCommandInput) (*batchRequestOutput, error) {
			id, err := parseID("batch_id", in.BatchID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
				doc, err := batchDoc(ctx, u, id, true)
				if err != nil {
					return Approval{}, err
				}
				if err := checkIfMatch(in.IfMatch, batchETag(doc)); err != nil {
					return Approval{}, err
				}
				rel, err := u.PaymentBatches().ReleaseOf(ctx, id)
				if err != nil {
					return Approval{}, err
				}
				switch {
				case op == approval.OpBatchApprove && doc.Status != storage.DocumentDraft:
					return Approval{}, problem(paymentbatch.CodeNotDraft, "The batch is approved already.", "Release it, or ask for its approval again after a payee's change")
				case op == approval.OpBatchReapprove && (doc.Status != storage.DocumentPosted || rel != nil):
					return Approval{}, problem(CodeBatchNotOpen, "Only an approved batch waiting for release is approved again.", "Ask for its first approval instead")
				}
				v, err := a.batchView(ctx, u)
				if err != nil {
					return Approval{}, err
				}
				b, err := paymentbatch.Evaluate(ctx, u, doc, v.settings, v.approved, v.now)
				if err != nil {
					return Approval{}, err
				}
				if len(b.Errors) > 0 {
					return Approval{}, batchProblems(b.Errors)
				}
				shown, err := a.batchOf(ctx, u, v, doc, false)
				if err != nil {
					return Approval{}, err
				}
				var diff []ApprovalDiff
				for _, pay := range shown.Payments {
					into := "no account"
					if pay.Account != nil {
						into = accountWords(*pay.Account)
					}
					diff = append(diff, ApprovalDiff{Field: pay.Payee.Name, After: fmt.Sprintf("%s SGD into %s", pay.Amount.Amount, into)})
				}
				name := doc.Number
				if name == "" {
					name = "a payment batch"
				}
				verb := "Pay"
				if op == approval.OpBatchReapprove {
					verb = "Approve again, after a payee's change:"
				}
				summary := fmt.Sprintf("%s %s: %d payments, %s SGD, on %s by %s", verb, name, len(b.Payments), b.Total.String(), b.Stored.PaymentDate,
					strings.ToUpper(b.Stored.Method))
				return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectPaymentBatch, ObjectKey: id.String(), Operation: op, Summary: summary,
					Execute: map[string]any{"batch_id": id.String(), "version": fmt.Sprint(doc.Version)}, Diff: diff,
					Amounts: []approval.Amount{{Amount: b.Total.String(), Currency: paymentbatch.SGD.Code}}})
			})
			if err != nil {
				return nil, err
			}
			return &batchRequestOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		}
	}
	askCodes := append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeSessionRequired,
		CodeStepUpRequired, approval.CodePending, storage.CodeApproval, CodeBatchNotOpen}, append(append(locks, checkCodes...), BatchCodes...)...)

	register(a, authz.RequestPaymentBatchApproval, huma.Operation{
		OperationID: "requestPaymentBatchApproval", Method: http.MethodPost, Path: "/v1/payment-batches/{batch_id}/approval", Tags: []string{"Purchases"},
		Summary: "Ask for approval of a payment batch", DefaultStatus: http.StatusAccepted,
		Description: "Asks for another person's approval of a draft batch at the version read (FR-BNK-12, FM-1), in every team mode, refused " +
			"while any check stops it (every problem at once). The answer is 202 with the request, which a person who holds A on \"Payment " +
			"batches\", neither prepared the batch nor asked for it, and passed step-up approves; its envelope holds the batch's total and each " +
			"payee's account in force, so an account changed later stops the release (G4-055). Approval numbers the batch (PB-). Asking " +
			"needs a person signed in who passed step-up, and If-Match with the batch's ETag. Idempotent with Idempotency-Key; Prefer: " +
			"return=preview checks it and asks nothing.",
	}, askCodes, ask(approval.OpBatchApprove))

	register(a, authz.RequestPaymentBatchReapproval, huma.Operation{
		OperationID: "requestPaymentBatchReapproval", Method: http.MethodPost, Path: "/v1/payment-batches/{batch_id}/reapproval", Tags: []string{"Purchases"},
		Summary: "Ask for a payment batch's approval again", DefaultStatus: http.StatusAccepted,
		Description: "Asks for another approval of an approved batch waiting for release, when a payee's account or what is open on an item " +
			"changed since it was approved (G4-055): the approver sees the accounts and total it pays now, and the release checks this " +
			"approval instead. Nothing else changes. Needs a person signed in who passed step-up, and If-Match with the batch's ETag. " +
			"Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing.",
	}, askCodes, ask(approval.OpBatchReapprove))

	register(a, authz.ReleasePaymentBatch, huma.Operation{
		OperationID: "releasePaymentBatch", Method: http.MethodPost, Path: "/v1/payment-batches/{batch_id}/release", Tags: []string{"Purchases"},
		Summary: "Release a payment batch",
		Description: "Releases an approved batch (FR-BNK-12, design D4, D5): the Payment releaser, a person stepped up in the last 5 minutes. " +
			"Every check runs again, and the batch's latest approval must still hold its payees' accounts and total (else PB-17: ask for " +
			"its approval again). In Standard mode the releaser neither prepared, asked for nor approved the batch (SOD-2), nor the account " +
			"change of a payee it pays (SOD-1); in Founder mode they may have, and each such release is logged for the weekly review (FM-3). " +
			"Side effects, in one transaction: one bill payment per supplier and one reimbursement per claimant, posted on the batch's " +
			"payment date as recording them by hand does (Dr 2010 or 2150, Cr the money account) and referenced with the batch's number; " +
			"the payment list (payment-list@1), sealed (SEC-11) with the SHA-256 of the file; the release and the links, audited with the " +
			"file's name and hash only. Download the list with getPaymentBatchFile and key it into the bank. Needs If-Match with the " +
			"batch's ETag. Idempotent with Idempotency-Key; Prefer: return=preview runs every check and releases nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeSessionRequired,
		CodeStepUpRequired, CodeBatchNotOpen, CodeBatchReleased, CodeBatchReapprove, CodeBatchSeparation, payee.CodeNoKeys, storage.CodeBatch,
		storage.CodeBatchHeld}, append(append(append(locks, checkCodes...), BatchCodes...), postingCodes()...)...),
		func(ctx context.Context, in *batchCommandInput) (*batchReleaseOutput, error) {
			id, err := parseID("batch_id", in.BatchID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			p, err := a.steppedUpPerson(ctx, "Releasing a payment batch")
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (PaymentBatch, error) {
				return a.releaseBatch(ctx, u, p, id, in.IfMatch)
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &batchReleaseOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &batchReleaseOutput{Status: w.Status, ETag: etag(w.Body.Version, string(storage.DocumentPosted)), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})

	register(a, authz.GetPaymentBatchFile, huma.Operation{
		OperationID: "getPaymentBatchFile", Method: http.MethodGet, Path: "/v1/payment-batches/{batch_id}/file", Tags: []string{"Purchases"},
		Summary: "Download a released batch's payment list",
		Description: "Returns the file a release wrote, as it was written, in base64, with its SHA-256 (FR-BNK-12, design D7): for the Payment " +
			"releaser, a person stepped up in the last 5 minutes. It holds full account numbers: key it into the bank, then delete it. " +
			"Side effects: an audit event of the download, with the file's hash (SEC-11). Never cached.",
	},
		[]string{CodeNotFound, CodeSessionRequired, CodeStepUpRequired, CodeBatchNotOpen, paymentbatch.CodeNotBatch, payee.CodeNoKeys},
		func(ctx context.Context, in *PaymentBatchPath) (*batchFileOutput, error) {
			id, err := parseID("batch_id", in.BatchID)
			if err != nil {
				return nil, err
			}
			p, err := a.steppedUpPerson(ctx, "Downloading a payment list")
			if err != nil {
				return nil, err
			}
			var out batchFileOutput
			err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				if _, err := batchDoc(ctx, u, id, false); err != nil {
					return err
				}
				rel, err := u.PaymentBatches().ReleaseOf(ctx, id)
				if err != nil {
					return err
				}
				if rel == nil {
					return problem(CodeBatchNotOpen, "The batch is not released, so it has no file yet.", "Release it first")
				}
				if a.deps.Fields == nil {
					return payee.NoKeys()
				}
				data, err := a.deps.Fields.Open(ctx, rel.Sealed, batchAAD(p.Organisation, id))
				if err != nil {
					return err
				}
				if sum := sha256.Sum256(data); !slices.Equal(sum[:], rel.SHA256) {
					return fmt.Errorf("payment batch %s: the file does not match its hash", id)
				}
				if err := u.Audit().Record(ctx, storage.AuditRecord{Action: "payment_batch.file_downloaded", ObjectType: "document", ObjectID: id,
					Changes: map[string][2]any{"file": {nil, map[string]any{"name": rel.FileName, "sha256": hex.EncodeToString(rel.SHA256)}}}}); err != nil {
					return err
				}
				out.Body = PaymentBatchFile{FileName: rel.FileName, MediaType: rel.MediaType, SHA256: hex.EncodeToString(rel.SHA256), Size: len(data),
					Content: base64.StdEncoding.EncodeToString(data)}
				return nil
			})
			return &out, err
		})

	register(a, authz.CancelPaymentBatch, huma.Operation{
		OperationID: "cancelPaymentBatch", Method: http.MethodPost, Path: "/v1/payment-batches/{batch_id}/cancel", Tags: []string{"Purchases"},
		Summary: "Cancel an approved payment batch",
		Description: "Cancels an approved batch that is not released, with a reason (FR-BNK-12): it pays nothing, and its bills and claims are " +
			"free for another batch or to pay by hand. A draft is not cancelled: change it, or leave it. A released batch is never cancelled: " +
			"void a payment the bank rejected on its own. Side effects: the batch cancelled (posting nothing) and its audit event with the " +
			"reason. Needs If-Match with the batch's ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeInvalid,
		CodeMalformed, CodeTooLarge, CodeMediaType, CodeBatchNotOpen, CodeBatchReleased, CodeBatchReason, paymentbatch.CodeNotBatch, storage.CodeBatch}, locks...),
		func(ctx context.Context, in *batchCancelInput) (*batchWriteOutput, error) {
			id, err := parseID("batch_id", in.BatchID)
			if err != nil {
				return nil, err
			}
			reason := strings.TrimSpace(in.Body.Reason)
			if reason == "" {
				return nil, problem(CodeBatchReason, "Cancelling a batch needs a reason.", "Say why it is cancelled")
			}
			ctx = storage.WithReason(ctx, reason)
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (PaymentBatch, error) {
				doc, err := batchDoc(ctx, u, id, true)
				if err != nil {
					return PaymentBatch{}, err
				}
				if err := checkIfMatch(in.IfMatch, batchETag(doc)); err != nil {
					return PaymentBatch{}, err
				}
				rel, err := u.PaymentBatches().ReleaseOf(ctx, id)
				if err != nil {
					return PaymentBatch{}, err
				}
				switch {
				case rel != nil:
					return PaymentBatch{}, problem(CodeBatchReleased, "The batch is released, so it is never cancelled.", "Void a payment the bank rejected on its own")
				case doc.Status != storage.DocumentPosted:
					return PaymentBatch{}, problem(CodeBatchNotOpen, "Only an approved batch is cancelled; a draft is changed, or left.", "Change the draft instead")
				}
				if err := u.Documents().SetStatus(ctx, id, storage.DocumentReversed); err != nil {
					return PaymentBatch{}, err
				}
				if doc, err = u.Documents().Get(ctx, id); err != nil {
					return PaymentBatch{}, err
				}
				v, err := a.batchView(ctx, u)
				if err != nil {
					return PaymentBatch{}, err
				}
				return a.batchOf(ctx, u, v, doc, false)
			})
			if err != nil {
				return nil, err
			}
			return &batchWriteOutput{Status: w.Status, ETag: etag(w.Body.Version, string(storage.DocumentReversed)), Replayed: replayedHeader(w.Replayed),
				Applied: applied(w), Body: w.Body}, nil
		})
}
