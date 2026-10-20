package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/domain/team"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ journal approvals (FR-GL-01, FM-1, FM-2, SOD-3; rev 0.53)
//
// A journal whose debits in the base currency are above the self-approval threshold (an FM-2 setting, S$2,000 by
// default) posts only on another person's approval, in any team mode; at or below it, a Founder-mode post is logged as
// self-approved for the weekly review (FM-3). Whoever prepared the journal never approves it (SOD-3). In Founder mode,
// voiding a posted journal always needs a second person (FM-1).

// The codes of journal approvals.
const (
	CodeNeedsApproval = "JOURNAL-07" // the journal is above the self-approval threshold: ask for approval
	CodeVoidApproval  = "JOURNAL-08" // in Founder mode a void needs a second person: ask for approval
	CodeOwnJournal    = "JOURNAL-09" // the one who prepared a journal never approves it (SOD-3)
)

// JournalApprovalCodes are the codes an approval of a journal's post or void may answer: SOD-3, and the posting's own.
func JournalApprovalCodes() []string {
	return append([]string{CodeOwnJournal, journal.CodeNotDraft, journal.CodeNotPosted}, postingCodes()...)
}

// selfApprovalThreshold is the threshold in force today.
func selfApprovalThreshold(ctx context.Context, a *API, u storage.UnitOfWork, base money.Currency) (money.Money, error) {
	today, err := orgToday(ctx, a, u)
	if err != nil {
		return money.Money{}, err
	}
	d, _ := settings.Lookup("approvals.self_approval_threshold")
	changes, err := u.Settings().Changes(ctx, d.Key)
	if err != nil {
		return money.Money{}, err
	}
	return money.Parse(settings.On(d, changes, today).Value, base)
}

// gatePosting refuses a post above the threshold, and says whether a post at or below it is a Founder-mode
// self-approval to log.
func (a *API) gatePosting(ctx context.Context, u storage.UnitOfWork, p authn.Principal, id uuid.UUID) (selfApproved bool, debits money.Money, err error) {
	if debits, err = a.journals.Debits(ctx, p.Organisation, id); err != nil {
		return false, debits, err
	}
	threshold, err := selfApprovalThreshold(ctx, a, u, debits.Currency())
	if err != nil {
		return false, debits, err
	}
	if over, err := debits.Cmp(threshold); err != nil {
		return false, debits, err
	} else if over > 0 {
		return false, debits, problem(CodeNeedsApproval,
			fmt.Sprintf("The journal's debits are %s %s, above the self-approval threshold of %s.", debits, debits.Currency().Code, threshold),
			"Ask for another person's approval to post it")
	}
	mode, _, _, err := teamModeOf(ctx, u)
	return mode == team.Founder, debits, err
}

// logSelfApproval records a Founder-mode post at or below the threshold for the weekly review (FM-2, FM-3).
func (a *API) logSelfApproval(ctx context.Context, u storage.UnitOfWork, p authn.Principal, id uuid.UUID, debits money.Money) error {
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return err
	}
	var actor *uuid.UUID
	if p.UserID != uuid.Nil {
		actor = &p.UserID
	}
	name := doc.Number
	if name == "" {
		name = "a journal"
	}
	_, err = u.ReviewLog().Log(ctx, storage.NewReviewLogEntry{Kind: storage.ReviewSelfApproved, ObjectType: approval.ObjectJournal, ObjectKey: id.String(),
		Summary: fmt.Sprintf("Posted %s of %s %s alone, at or below the self-approval threshold", name, debits, debits.Currency().Code),
		Actor:   actor, LoggedAt: a.deps.Clock.Now()})
	return err
}

// gateVoid refuses the void of a posted journal in Founder mode: it needs a second person (FM-1). A journal that is not
// posted is left to the reversal's own refusal.
func gateVoid(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) error {
	doc, err := u.Documents().Get(ctx, id)
	if err != nil || doc.Status != storage.DocumentPosted {
		return err
	}
	mode, _, _, err := teamModeOf(ctx, u)
	if err != nil {
		return err
	}
	if mode == team.Founder {
		return problem(CodeVoidApproval, "In Founder mode, reversing a posted journal needs a second person (FM-1).",
			"Ask for another person's approval to reverse it")
	}
	return nil
}

// journalAsked is the pending request to post or reverse a journal, if one waits.
func (a *API) journalAsked(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (string, error) {
	pending, err := u.Approvals().List(ctx, string(approval.Pending))
	if err != nil {
		return "", err
	}
	now := a.deps.Clock.Now()
	for _, r := range pending {
		if r.ObjectType == approval.ObjectJournal && r.ObjectKey == id.String() && approval.StatusAt(requestOf(r), now) == approval.Pending {
			return r.ID.String(), nil
		}
	}
	return "", nil
}

// journalDebits are a journal's debits in the base currency: as the rules would post its draft, or as its entry posted.
func (a *API) journalDebits(ctx context.Context, u storage.UnitOfWork, org uuid.UUID, doc storage.Document) (string, error) {
	if doc.Status == storage.DocumentDraft {
		d, err := a.journals.Debits(ctx, org, doc.ID)
		return d.String(), err
	}
	e, err := u.Ledger().ByIdentity(ctx, ledger.JournalIdentity(doc.ID, doc.Version, ledger.RuleManualJournal))
	if err != nil {
		return "", err
	}
	o, err := u.Organisation(ctx)
	if err != nil {
		return "", err
	}
	base, err := money.Lookup(o.BaseCurrency)
	if err != nil {
		return "", err
	}
	total := money.Zero(base)
	for _, l := range e.Lines {
		b, err := money.ParseStored(string(l.BaseAmount), base)
		if err != nil {
			return "", err
		}
		if b.Sign() > 0 {
			if total, err = total.Add(b); err != nil {
				return "", err
			}
		}
	}
	return total.String(), nil
}

type journalRequestInput struct {
	JournalPath
	KeyHeader
	IfMatchHeader
	PreferHeader
}

type journalReversalRequestInput struct {
	JournalPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body Reversal
}

type journalRequestOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     Approval
}

// requestFor asks for approval of a change to a journal at the version read.
func (a *API) requestFor(ctx context.Context, rawID, key, ifMatch, prefer string, request any, status storage.DocumentStatus,
	change func(doc storage.Document, j Journal, debits string) pendingChange) (*journalRequestOutput, error) {
	id, err := parseID("journal_id", rawID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(ifMatch) == "" {
		return nil, checkIfMatch(ifMatch, "")
	}
	p, _ := authn.From(ctx)
	w, err := write(ctx, a, key, prefer, request, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
		doc, err := u.Documents().Lock(ctx, id)
		if err != nil {
			return Approval{}, err
		}
		j, err := journalOf(doc)
		if err != nil {
			return Approval{}, err
		}
		if err := checkIfMatch(ifMatch, journalETag(j)); err != nil {
			return Approval{}, err
		}
		if doc.Status != status {
			code := journal.CodeNotDraft
			if status == storage.DocumentPosted {
				code = journal.CodeNotPosted
			}
			return Approval{}, problem(code, fmt.Sprintf("The journal is %s.", doc.Status), "Ask for approval of a journal in the state it needs")
		}
		debits, err := a.journalDebits(ctx, u, p.Organisation, doc)
		if err != nil {
			return Approval{}, err
		}
		return a.requestApproval(ctx, u, change(doc, j, debits))
	})
	if err != nil {
		return nil, err
	}
	return &journalRequestOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
}

func journalName(doc storage.Document, j Journal) string {
	name := doc.Number
	if name == "" {
		name = "the draft journal"
	}
	if j.Memo != "" {
		name += " (" + j.Memo + ")"
	}
	return name
}

func registerJournalApprovals(a *API) {
	codes := []string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeSessionRequired,
		CodeStepUpRequired, approval.CodePending, storage.CodeApproval, journal.CodeNotJournal, journal.CodeNotDraft, journal.CodeNotPosted,
		journal.CodeBadDraft, storage.CodeRetry, storage.CodeLockTimeout}
	codes = append(codes, LedgerCodes...)

	register(a, authz.RequestJournalPost, huma.Operation{
		OperationID: "requestJournalPost", Method: http.MethodPost, Path: "/v1/journals/{journal_id}/post-approval", Tags: []string{"Journals"},
		Summary: "Ask for approval to post a journal", DefaultStatus: http.StatusAccepted,
		Description: "Asks for another person's approval to post a draft journal at the version read (FR-GL-01, FM-2, SOD-3; PRD-001 rev " +
			"0.53, provisional): what postJournal asks for when the journal's debits are above the self-approval threshold " +
			"(JOURNAL-07). The answer is 202 with an approval request, which a person who holds A on Journals (the Controller), did " +
			"not ask and did not prepare the journal approves, stepped up; on the approval the journal posts, as the approver. " +
			"The request holds the journal's version and status and its debits, so a change to the draft voids it. Asking needs a " +
			"person signed in who passed step-up, and If-Match with the journal's ETag. Idempotent with Idempotency-Key; Prefer: " +
			"return=preview checks it and asks nothing.",
	}, codes, func(ctx context.Context, in *journalRequestInput) (*journalRequestOutput, error) {
		return a.requestFor(ctx, in.JournalID, in.IdempotencyKey, in.IfMatch, in.Prefer, in, storage.DocumentDraft, func(doc storage.Document, j Journal, debits string) pendingChange {
			return pendingChange{ObjectType: approval.ObjectJournal, ObjectKey: doc.ID.String(), Operation: approval.OpJournalPost,
				Summary: fmt.Sprintf("Post %s, debits %s", journalName(doc, j), debits),
				Execute: map[string]any{"journal_id": doc.ID.String(), "version": fmt.Sprint(doc.Version)},
				Diff:    []ApprovalDiff{{Field: "status", Before: "draft", After: "posted"}, {Field: "debits", After: debits}}}
		})
	})

	register(a, authz.RequestJournalReversal, huma.Operation{
		OperationID: "requestJournalReversal", Method: http.MethodPost, Path: "/v1/journals/{journal_id}/reverse-approval", Tags: []string{"Journals"},
		Summary: "Ask for approval to reverse a journal", DefaultStatus: http.StatusAccepted,
		Description: "Asks for another person's approval to void a posted journal with a reversing entry on the date given, with a " +
			"reason (FR-GL-12, FM-1; PRD-001 rev 0.53, provisional): what reverseJournal asks for in Founder mode (JOURNAL-08). The " +
			"answer is 202 with an approval request, which a person who holds A on Journals and did not ask approves, stepped up; " +
			"on the approval the reversal posts. Asking needs a person signed in who passed step-up, and If-Match with the " +
			"journal's ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing.",
	}, append([]string{CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid}, codes...), func(ctx context.Context, in *journalReversalRequestInput) (*journalRequestOutput, error) {
		if _, err := time.Parse(time.DateOnly, in.Body.Date); err != nil {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.date", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"}})
		}
		reason := strings.TrimSpace(in.Body.Reason)
		return a.requestFor(ctx, in.JournalID, in.IdempotencyKey, in.IfMatch, in.Prefer, in, storage.DocumentPosted, func(doc storage.Document, j Journal, debits string) pendingChange {
			return pendingChange{ObjectType: approval.ObjectJournal, ObjectKey: doc.ID.String(), Operation: approval.OpJournalReverse,
				Summary: fmt.Sprintf("Reverse %s on %s: %s", journalName(doc, j), in.Body.Date, reason),
				Execute: map[string]any{"journal_id": doc.ID.String(), "version": fmt.Sprint(doc.Version), "date": in.Body.Date, "reason": reason},
				Diff:    []ApprovalDiff{{Field: "status", Before: "posted", After: "reversed"}, {Field: "reversal_date", After: in.Body.Date}}}
		})
	})
}
