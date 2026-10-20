package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/team"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ edits of posted journals (FR-GL-12, FM-1; rev 0.54)
//
// A posted journal in an open period is corrected by an edit: its next version, whose post reverses the entry it had on
// that entry's date and posts the new one, in one transaction. An edit needs the approval a void needs: in Founder mode
// always a second person (FM-1); in any mode, a new version whose debits are above the self-approval threshold (FM-2).

// CodeEditApproval is the refusal of an edit that needs a second person in Founder mode.
const CodeEditApproval = "JOURNAL-10"

// JournalEdit is the correction of a posted journal: its new content, and why (FR-GL-12).
type JournalEdit struct {
	Date      string             `json:"date" format:"date" doc:"The date of the new version; its period must be open, as must the period of the entry it replaces." example:"2026-10-31"`
	Memo      string             `json:"memo,omitempty" maxLength:"1000" doc:"What the journal is for." example:"October rent accrual"`
	Lines     []JournalLineDraft `json:"lines" maxItems:"1000" doc:"Debits positive, credits negative."`
	ReverseOn string             `json:"reverse_on,omitempty" format:"date" doc:"The date the new version reverses by itself, if it does (FR-GL-02); after its date." example:"2026-11-01"`
	Reason    string             `json:"reason" minLength:"1" maxLength:"500" doc:"Why it is edited: the audit trail and the reversal's memo keep it (FR-GL-12)." example:"The rent is 1,250.00, not 1,200.00"`
}

func (e JournalEdit) draft() JournalDraft {
	return JournalDraft{Date: e.Date, Memo: e.Memo, Lines: e.Lines, ReverseOn: e.ReverseOn}
}

// JournalEdited is what an edit posted.
type JournalEdited struct {
	Journal  Journal `json:"journal" doc:"The journal at its new version, still posted, with whoever posted the edit."`
	Reversal Entry   `json:"reversal" doc:"The entry that reverses the one the journal had, dated on that entry's date."`
	Entry    Entry   `json:"entry" doc:"The entry of the new version, on its date."`
}

type editJournalInput struct {
	JournalPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body JournalEdit
}

type editJournalOutput struct {
	ETag     string `header:"ETag" doc:"The journal's new version and status, for If-Match."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved, and the entries show what the edit would post (FR-INT-14)."`
	Body     JournalEdited
}

// gateEdit refuses an edit in Founder mode (FM-1), and in any mode an edit whose new version's debits are above the
// self-approval threshold (FM-2). A journal that is not posted is left to the edit's own refusal.
func (a *API) gateEdit(ctx context.Context, u storage.UnitOfWork, p authn.Principal, doc storage.Document, d journal.Draft) error {
	if doc.Status != storage.DocumentPosted {
		return nil
	}
	mode, _, _, err := teamModeOf(ctx, u)
	if err != nil {
		return err
	}
	if mode == team.Founder {
		return problem(CodeEditApproval, "In Founder mode, editing a posted journal needs a second person (FM-1).",
			"Ask for another person's approval to edit it")
	}
	debits, err := a.journals.EditDebits(ctx, p.Organisation, doc.ID, d)
	if err != nil {
		return err
	}
	threshold, err := selfApprovalThreshold(ctx, a, u, debits.Currency())
	if err != nil {
		return err
	}
	if over, err := debits.Cmp(threshold); err != nil {
		return err
	} else if over > 0 {
		return problem(CodeNeedsApproval,
			fmt.Sprintf("The edited journal's debits are %s %s, above the self-approval threshold of %s.", debits, debits.Currency().Code, threshold),
			"Ask for another person's approval to edit it")
	}
	return nil
}

// baseOf is the organisation's base currency.
func baseOf(ctx context.Context, u storage.UnitOfWork) (money.Currency, error) {
	o, err := u.Organisation(ctx)
	if err != nil {
		return money.Currency{}, err
	}
	return money.Lookup(o.BaseCurrency)
}

// contentOf is an edit's content as the request keeps it, canonical, and its SHA-256, which the envelope holds.
func contentOf(d JournalDraft) ([]byte, string, error) {
	raw, err := json.Marshal(d)
	if err != nil {
		return nil, "", err
	}
	return canonicalOf(raw)
}

// canonicalOf is JSON in its canonical form and that form's SHA-256: the database keeps a request's content as jsonb,
// which rewrites its spacing and key order, so the hash is always taken over the canonical form.
func canonicalOf(raw []byte) ([]byte, string, error) {
	var v any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return nil, "", err
	}
	canonical, err := approval.Canonical(v)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(canonical)
	return canonical, hex.EncodeToString(sum[:]), nil
}

// editDiff is what an edit changes, line by line, for the approver (FR-APR-02).
func editDiff(ctx context.Context, u storage.UnitOfWork, before Journal, after JournalDraft, debitsBefore, debitsAfter string) ([]ApprovalDiff, error) {
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	for _, ac := range accounts {
		names[ac.ID.String()] = ac.Code + " " + ac.Name
	}
	line := func(l JournalLineDraft) string {
		s := fmt.Sprintf("%s: %s %s", names[l.AccountID], l.Amount.Amount, l.Amount.Currency)
		if l.FXRate != "" {
			s += " at " + l.FXRate
		}
		if l.Description != "" {
			s += " (" + l.Description + ")"
		}
		return s
	}
	var out []ApprovalDiff
	if before.Date != after.Date {
		out = append(out, ApprovalDiff{Field: "date", Before: before.Date, After: after.Date})
	}
	if before.Memo != after.Memo {
		out = append(out, ApprovalDiff{Field: "memo", Before: before.Memo, After: after.Memo})
	}
	if before.ReverseOn != after.ReverseOn {
		out = append(out, ApprovalDiff{Field: "reverse_on", Before: before.ReverseOn, After: after.ReverseOn})
	}
	out = append(out, ApprovalDiff{Field: "debits", Before: debitsBefore, After: debitsAfter})
	for i := 0; i < max(len(before.Lines), len(after.Lines)); i++ {
		var b, f string
		if i < len(before.Lines) {
			b = line(before.Lines[i])
		}
		if i < len(after.Lines) {
			f = line(after.Lines[i])
		}
		if b != f {
			out = append(out, ApprovalDiff{Field: fmt.Sprintf("line %d", i+1), Before: b, After: f})
		}
	}
	return out, nil
}

// executeEdit runs an approved edit: the content the request kept, checked against the hash its envelope holds, posted
// as the approver, with the requester as the one who prepared the new version.
func (a *API) executeEdit(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest, s stored) error {
	text := func(k string) string { v, _ := s.Execute[k].(string); return v }
	content, sum, err := canonicalOf(s.Content)
	if err != nil {
		return err
	}
	if sum != text("content_sha256") {
		return fmt.Errorf("approval: the content of request %s does not match its envelope", r.ID)
	}
	var body JournalDraft
	if err := json.Unmarshal(content, &body); err != nil {
		return err
	}
	id, err := uuid.Parse(r.ObjectKey)
	if err != nil {
		return err
	}
	base, err := baseOf(ctx, u)
	if err != nil {
		return err
	}
	d, err := draftOf(body, base)
	if err != nil {
		return err
	}
	o, err := u.Organisation(ctx)
	if err != nil {
		return err
	}
	_, err = a.journals.Edit(ctx, o.ID, id, d, text("reason"), text("prepared_by"))
	return err
}

func registerJournalEdits(a *API) {
	codes := append([]string{journal.CodeNotPosted, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, storage.CodeDocumentEdit},
		postingCodes()...)

	register(a, authz.EditJournal, huma.Operation{
		OperationID: "editJournal", Method: http.MethodPost, Path: "/v1/journals/{journal_id}/edit", Tags: []string{"Journals"},
		Summary: "Edit a posted journal",
		Description: "Corrects a posted journal in an open period (FR-GL-12; PRD-001 rev 0.54, provisional): saves the content sent as " +
			"its next version, reverses the entry it had on that entry's own date, and posts the new version's entry on its date, " +
			"in one transaction; the journal stays posted, keeps its number, and records who posted the edit, and the database " +
			"refuses an edit without both postings. Side effects: a new version, two entries, a new ledger revision, balances, " +
			"audit events with the reason. Needs If-Match with the journal's ETag. An edit needs the approval a void needs: in " +
			"Founder mode it is refused (JOURNAL-10), and in any mode a new version whose debits are above the self-approval " +
			"threshold is refused (JOURNAL-07); requestJournalEdit asks for the approval. Idempotent with Idempotency-Key; " +
			"Prefer: return=preview shows the two entries and saves nothing.",
	}, append([]string{CodeEditApproval, CodeNeedsApproval}, codes...), func(ctx context.Context, in *editJournalInput) (*editJournalOutput, error) {
		id, err := parseID("journal_id", in.JournalID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		reason := strings.TrimSpace(in.Body.Reason)
		if reason == "" {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.reason", Message: "is empty", Fix: "Say why the journal is edited"}})
		}
		p, _ := authn.From(ctx)
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (JournalEdited, error) {
			doc, err := u.Documents().Lock(ctx, id)
			if err != nil {
				return JournalEdited{}, err
			}
			before, err := journalOf(doc)
			if err != nil {
				return JournalEdited{}, err
			}
			if err := checkIfMatch(in.IfMatch, journalETag(before)); err != nil {
				return JournalEdited{}, err
			}
			if err := checkConflicts(ctx, u, p, "Posting"); err != nil {
				return JournalEdited{}, err
			}
			base, err := baseOf(ctx, u)
			if err != nil {
				return JournalEdited{}, err
			}
			d, err := draftOf(in.Body.draft(), base)
			if err != nil {
				return JournalEdited{}, err
			}
			if err := a.gateEdit(ctx, u, p, doc, d); err != nil {
				return JournalEdited{}, err
			}
			res, err := a.journals.Edit(ctx, p.Organisation, id, d, reason, "")
			if err != nil {
				return JournalEdited{}, err
			}
			after, err := u.Documents().Get(ctx, id)
			if err != nil {
				return JournalEdited{}, err
			}
			j, err := journalOf(after)
			if err != nil {
				return JournalEdited{}, err
			}
			return JournalEdited{Journal: j, Reversal: entryOf(res.Reversal), Entry: entryOf(res.Entry)}, a.name(ctx, u, j.PreparedBy, j.PostedBy)
		})
		if err != nil {
			return nil, err
		}
		if w.Previewed {
			return &editJournalOutput{Applied: applied(w), Body: w.Body}, nil
		}
		return &editJournalOutput{ETag: journalETag(w.Body.Journal), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})

	register(a, authz.RequestJournalEdit, huma.Operation{
		OperationID: "requestJournalEdit", Method: http.MethodPost, Path: "/v1/journals/{journal_id}/edit-approval", Tags: []string{"Journals"},
		Summary: "Ask for approval to edit a posted journal", DefaultStatus: http.StatusAccepted,
		Description: "Asks for another person's approval to edit a posted journal with the content sent (FR-GL-12, FM-1, FM-2; PRD-001 " +
			"rev 0.54, provisional): what editJournal asks for in Founder mode (JOURNAL-10) or above the self-approval threshold " +
			"(JOURNAL-07). The content must pass the posting rules. The answer is 202 with an approval request whose changes list " +
			"the date, the description, the debits and each line that changes; it keeps the content, and its envelope holds the " +
			"content's SHA-256 with the journal's version, status and debits, so any change to the journal voids it. A person who " +
			"holds A on Journals and did not ask approves it, stepped up; on the approval the edit posts as the approver, with the " +
			"requester as the one who prepared the new version. Asking needs a person signed in who passed step-up, and If-Match " +
			"with the journal's ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		CodeSessionRequired, CodeStepUpRequired, approval.CodePending, storage.CodeApproval, journal.CodeNotJournal, journal.CodeBadDraft,
		CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid}, LedgerCodes...), func(ctx context.Context, in *editJournalInput) (*journalRequestOutput, error) {
		reason := strings.TrimSpace(in.Body.Reason)
		if reason == "" {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.reason", Message: "is empty", Fix: "Say why the journal is edited"}})
		}
		p, _ := authn.From(ctx)
		return a.requestFor(ctx, in.JournalID, in.IdempotencyKey, in.IfMatch, in.Prefer, in, storage.DocumentPosted,
			func(ctx context.Context, u storage.UnitOfWork, doc storage.Document, j Journal, debits string) (pendingChange, error) {
				base, err := baseOf(ctx, u)
				if err != nil {
					return pendingChange{}, err
				}
				d, err := draftOf(in.Body.draft(), base)
				if err != nil {
					return pendingChange{}, err
				}
				after, err := a.journals.EditDebits(ctx, p.Organisation, doc.ID, d)
				if err != nil {
					return pendingChange{}, err
				}
				content, sum, err := contentOf(in.Body.draft())
				if err != nil {
					return pendingChange{}, err
				}
				diff, err := editDiff(ctx, u, j, in.Body.draft(), debits, after.String())
				if err != nil {
					return pendingChange{}, err
				}
				return pendingChange{ObjectType: approval.ObjectJournal, ObjectKey: doc.ID.String(), Operation: approval.OpJournalEdit,
					Summary: fmt.Sprintf("Edit %s: %s", journalName(doc, j), reason),
					Execute: map[string]any{"journal_id": doc.ID.String(), "version": fmt.Sprint(doc.Version), "content_sha256": sum,
						"reason": reason, "prepared_by": (storage.Actor{Type: "user", ID: p.UserID.String()}).String()},
					Content: content, Diff: diff}, nil
			})
	})
}
