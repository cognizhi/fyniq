package api

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/director"
	"github.com/cognizhi/fyniq/internal/domain/equity"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ equity transactions (FR-EQ-17; p1-equity-register slice 2)
//
// A cash issue to one subscriber, a transfer between two holders, a conversion of one holder's shares between classes,
// or the reversal of one of these (design D3 to D5). The Accountant drafts it and attaches its evidence (the resolutions,
// the instrument of transfer, the ACRA extract); another person who holds A on the cap table approves it, never a holder
// it moves shares of or for, with the Accountant too when every available approver benefits (FM-1, FM-9); and an Owner
// or the Controller records the day ACRA's register of members shows it, which writes its holding movements, and for a
// cash issue its share issue and entry from share application monies (B.7), in one transaction. Nothing is edited once
// approved: a reversal corrects it.

// EquityTransactionsPath names an equity transaction.
type EquityTransactionPath struct {
	TransactionID string `path:"transaction_id" format:"uuid" doc:"The equity transaction's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6a01"`
}

// EquityTransactionDraft is an equity transaction as drafted. Which fields it takes depends on its kind.
type EquityTransactionDraft struct {
	Kind             string `json:"kind" enum:"issue_cash,transfer,class_conversion,reversal" doc:"issue_cash: shares allotted to one subscriber for cash; transfer: shares from one holder to another; class_conversion: one holder's shares into another class; reversal: undoes an effective transaction." example:"issue_cash"`
	HolderID         string `json:"holder_id,omitempty" format:"uuid" doc:"issue_cash: the subscriber, a contact marked shareholder or investor; class_conversion: the holder whose shares convert." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c11"`
	SellerID         string `json:"seller_id,omitempty" format:"uuid" doc:"transfer: the holder the shares come from." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c11"`
	BuyerID          string `json:"buyer_id,omitempty" format:"uuid" doc:"transfer: the holder they go to, a contact marked shareholder or investor." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c12"`
	ClassAccountID   string `json:"class_account_id,omitempty" format:"uuid" doc:"The class's share-capital account (3010 ordinary, 3020 preference); a conversion's class it converts from." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d30"`
	ClassID          string `json:"class_id,omitempty" format:"uuid" doc:"The share class, a recorded class of that account (FR-EQ-16). Left out: the account's first recorded class, if it has one." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e01"`
	ToClassAccountID string `json:"to_class_account_id,omitempty" format:"uuid" doc:"class_conversion: the share-capital account of the class the shares convert into." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d31"`
	ToClassID        string `json:"to_class_id,omitempty" format:"uuid" doc:"class_conversion: the class they convert into." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e02"`
	Shares           string `json:"shares,omitempty" doc:"The number of shares, a whole number above zero; a conversion's shares out." example:"52083"`
	ToShares         string `json:"to_shares,omitempty" doc:"class_conversion: the number of shares in, a whole number above zero." example:"52083"`
	Price            string `json:"price,omitempty" doc:"issue_cash: the price per share in the base currency, above zero, at most 6 decimals; transfer: the price agreed per share, for information (share duty is the parties' matter)." example:"2.40"`
	Amount           string `json:"amount,omitempty" doc:"class_conversion between accounts: the carrying amount of the shares converted, in the base currency [accountant to confirm the basis]. Shown for issue_cash: shares × price under RND-LINE." example:"125000.00"`
	ResolutionRefs   string `json:"resolution_refs,omitempty" maxLength:"500" doc:"The shareholders' approval and the board resolution, by reference: needed for an issue and a conversion." example:"Board resolution 5 Mar 2027; shareholders' resolution 5 Mar 2027"`
	ReversesID       string `json:"reverses_id,omitempty" format:"uuid" doc:"reversal: the effective transaction it undoes." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6a01"`
	Reason           string `json:"reason,omitempty" maxLength:"500" doc:"reversal: what was wrong, such as the ACRA extract shows another number of shares (FR-EQ-26)." example:"ACRA's register shows 50,000 shares, not 52,083"`
	Note             string `json:"note,omitempty" maxLength:"1000" doc:"A note for the register." example:"Seed round, second close"`
}

// EquityParty is a holder a transaction moves shares of or for.
type EquityParty struct {
	ContactID string `json:"contact_id" format:"uuid" doc:"The holder's contact ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c11"`
	Name      string `json:"name" doc:"Their name." example:"Seedling Ventures Pte. Ltd."`
	Role      string `json:"role" enum:"subscriber,seller,buyer,holder" doc:"What they are in the transaction." example:"subscriber"`
}

// EquityMovement is shares moving into or out of one holding, once a transaction is effective.
type EquityMovement struct {
	ID          string `json:"id" format:"uuid" doc:"The movement's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6b01"`
	Kind        string `json:"kind" enum:"issue,transfer,class_conversion,reversal" doc:"What moved the shares." example:"issue"`
	HolderID    string `json:"holder_id" format:"uuid" doc:"The holder." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c11"`
	HolderName  string `json:"holder_name" doc:"Their name." example:"Seedling Ventures Pte. Ltd."`
	ClassCode   string `json:"class_code" doc:"The class's account code." example:"3010"`
	ClassID     string `json:"class_id,omitempty" format:"uuid" doc:"The share class, when it names one." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e01"`
	Shares      string `json:"shares" pattern:"^-?[1-9][0-9]*$" doc:"Shares in (positive) or out (negative)." example:"52083"`
	EffectiveOn string `json:"effective_on" format:"date" doc:"The day ACRA's register of members shows it." example:"2027-03-12"`
}

// EquityStepView is one ACRA step of an equity transaction, as recorded.
type EquityStepView struct {
	ID         string `json:"id" format:"uuid" doc:"The step's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5f41"`
	Kind       string `json:"kind" enum:"resolved,approval_lodged,agm_due,allotment_lodged,instrument_signed,transfer_lodged,conversion_lodged" doc:"Which step: a cash issue's resolved, approval_lodged, agm_due and allotment_lodged; a transfer's instrument_signed and transfer_lodged; a conversion's resolved and conversion_lodged." example:"approval_lodged"`
	On         string `json:"on" format:"date" doc:"Its day." example:"2027-03-08"`
	Reference  string `json:"reference,omitempty" doc:"ACRA's transaction number, or what it rests on." example:"T27ACRA00456"`
	RecordedAt string `json:"recorded_at" format:"date-time" doc:"When it was recorded." example:"2027-03-08T09:30:00Z"`
}

// EquityACRA is an equity transaction's ACRA steps and what they raise (FR-EQ-09).
type EquityACRA struct {
	AGMDueOn string           `json:"agm_due_on,omitempty" format:"date" doc:"A cash issue: the date by which the next AGM must be held, when its shareholders' approval lapses: as recorded, or worked out from the financial years [verify]." example:"2027-06-30"`
	Steps    []EquityStepView `json:"steps" nullable:"false" doc:"Every step recorded, earliest first: a later one of a kind supersedes the earlier."`
	Flags    []ConversionFlag `json:"flags" nullable:"false" doc:"What the steps raise today: ⏰ due, or ⚠ warning. Empty once effective."`
}

// EquityStepRequest is an ACRA step as recorded.
type EquityStepRequest struct {
	Kind      string `json:"kind" enum:"resolved,approval_lodged,agm_due,allotment_lodged,instrument_signed,transfer_lodged,conversion_lodged" doc:"Which step, of those the transaction's kind takes." example:"approval_lodged"`
	On        string `json:"on" format:"date" doc:"Its day: no later than today, except the AGM date." example:"2027-03-08"`
	Reference string `json:"reference,omitempty" maxLength:"200" doc:"ACRA's transaction number, or what it rests on." example:"T27ACRA00456"`
}

// EquityEntry is an entry an equity transaction posted.
type EquityEntry struct {
	ID string `json:"id" format:"uuid" doc:"The entry's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5f20"`
}

// EquityTransaction is an equity transaction: what was drafted, where it stands, and what it moved.
type EquityTransaction struct {
	ID          string                 `json:"id" format:"uuid" doc:"The transaction's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6a01"`
	Number      string                 `json:"number,omitempty" doc:"Its number, EQ-, given on its approval (FR-ORG-04)." example:"EQ-0001"`
	Version     int                    `json:"version" doc:"Its version; it makes the ETag." example:"1"`
	Status      string                 `json:"status" enum:"draft,approved,effective,reversed" doc:"Draft (✎); approved, waiting for ACRA's date; effective, its shares moved; or reversed." example:"approved"`
	Details     EquityTransactionDraft `json:"details" doc:"What was drafted, as saved."`
	Parties     []EquityParty          `json:"parties" nullable:"false" doc:"The holders it moves shares of or for: they never approve it (FM-9)."`
	ClassCode   string                 `json:"class_code,omitempty" doc:"The class's account code." example:"3010"`
	Amount      *Money                 `json:"amount,omitempty" doc:"A cash issue's amount, or a conversion's carrying amount."`
	Paid        *Money                 `json:"paid,omitempty" doc:"A cash issue not yet effective: what its subscriber's share application monies hold today (B.7); it becomes effective once they cover the amount."`
	ApprovedOn  string                 `json:"approved_on,omitempty" format:"date" doc:"The day it was approved: it is never effective earlier." example:"2027-03-05"`
	EffectiveOn string                 `json:"effective_on,omitempty" format:"date" doc:"The day ACRA's register of members shows it." example:"2027-03-12"`
	ReversedBy  string                 `json:"reversed_by,omitempty" format:"uuid" doc:"The reversal that undid it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6a09"`
	ApprovalID  string                 `json:"approval_id,omitempty" format:"uuid" doc:"The approval request waiting, if any." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f41"`
	Evidence    int                    `json:"evidence" doc:"How many files are attached and not removed." example:"2"`
	Checks      []BillCheck            `json:"checks" nullable:"false" doc:"What stops it moving on now, in plain words."`
	Movements   []EquityMovement       `json:"movements" nullable:"false" doc:"The holding movements it wrote once effective."`
	ShareIssue  string                 `json:"share_issue_id,omitempty" format:"uuid" doc:"A cash issue's row in the register of share issues." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5f10"`
	Entries     []EquityEntry          `json:"entries" nullable:"false" doc:"The entries it posted: a cash issue's or a conversion's, or the reversals of them."`
	ACRA        *EquityACRA            `json:"acra,omitempty" doc:"Its ACRA steps and what they raise, once approved; a reversal has none (FR-EQ-09)."`
}

// EquityTransactionPage is a page of equity transactions, the latest first.
type EquityTransactionPage struct {
	Items      []EquityTransaction `json:"items" nullable:"false" doc:"The transactions on this page, the latest drafted first."`
	NextCursor string              `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjAxOTIifQ"`
}

// EquityEffectiveRequest is the day ACRA's register of members shows an approved transaction.
type EquityEffectiveRequest struct {
	EffectiveOn  string `json:"effective_on" format:"date" doc:"The day ACRA's register of members shows it: no later than today, no earlier than its approval." example:"2027-03-12"`
	LodgementRef string `json:"lodgement_ref,omitempty" maxLength:"100" doc:"ACRA's lodgement reference, such as the return of allotment's transaction number." example:"AA2027031200456"`
}

type (
	equityTransactionListInput struct {
		PageQuery
		Kind string `query:"kind" enum:"issue_cash,transfer,class_conversion,reversal" doc:"Only transactions of this kind." example:"transfer"`
	}
	equityTransactionListOutput  struct{ Body EquityTransactionPage }
	equityTransactionCreateInput struct {
		KeyHeader
		PreferHeader
		Body EquityTransactionDraft
	}
	equityTransactionReviseInput struct {
		EquityTransactionPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body EquityTransactionDraft
	}
	equityTransactionAskInput struct {
		EquityTransactionPath
		KeyHeader
		IfMatchHeader
		PreferHeader
	}
	equityTransactionEffectiveInput struct {
		EquityTransactionPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body EquityEffectiveRequest
	}
	equityStepInput struct {
		EquityTransactionPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body EquityStepRequest
	}
	equityTransactionReadOutput struct {
		ETag string `header:"ETag" doc:"Its version and status, for If-Match."`
		Body EquityTransaction
	}
	equityTransactionWriteOutput struct {
		Status   int
		ETag     string `header:"ETag" doc:"Its version and status, for If-Match."`
		Location string `header:"Location" doc:"Its URL."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     EquityTransaction
	}
)

// EquityTransactionCodes are the codes an equity transaction's writes answer with.
var EquityTransactionCodes = []string{equity.CodeNotRecord, equity.CodeTransaction, equity.CodeRecorded, equity.CodeEffective, equity.CodeNoMonies,
	equity.CodeNoEvidence, storage.CodeHolding, storage.CodeShareIssue, storage.CodeParty, storage.CodePeriodClosed, ledger.CodeEquity, ledger.CodeParty,
	ledger.CodeNoDate, ledger.CodeReversalDate}

// transactionIn is a draft as the domain reads it: an ID that is not one is left as none, for the checks to name.
func transactionIn(d EquityTransactionDraft, today time.Time) equity.Transaction {
	id := func(s string) uuid.UUID {
		x, _ := uuid.Parse(s)
		return x
	}
	opt := func(s string) *uuid.UUID {
		if x, err := uuid.Parse(s); err == nil {
			return &x
		}
		return nil
	}
	return equity.Transaction{Kind: d.Kind, HolderID: id(d.HolderID), SellerID: id(d.SellerID), BuyerID: id(d.BuyerID), ClassAccountID: id(d.ClassAccountID),
		ClassID: opt(d.ClassID), ToAccountID: id(d.ToClassAccountID), ToClassID: opt(d.ToClassID), Shares: strings.TrimSpace(d.Shares),
		ToShares: strings.TrimSpace(d.ToShares), Price: strings.TrimSpace(d.Price), Amount: strings.TrimSpace(d.Amount),
		ResolutionRefs: strings.TrimSpace(d.ResolutionRefs), ReversesID: id(d.ReversesID), Reason: strings.TrimSpace(d.Reason), Note: strings.TrimSpace(d.Note),
		Date: today}
}

// transactionDraftOf is a transaction as the API shows what was drafted.
func transactionDraftOf(t equity.Transaction) EquityTransactionDraft {
	id := func(x uuid.UUID) string {
		if x == uuid.Nil {
			return ""
		}
		return x.String()
	}
	opt := func(x *uuid.UUID) string {
		if x == nil {
			return ""
		}
		return x.String()
	}
	return EquityTransactionDraft{Kind: t.Kind, HolderID: id(t.HolderID), SellerID: id(t.SellerID), BuyerID: id(t.BuyerID), ClassAccountID: id(t.ClassAccountID),
		ClassID: opt(t.ClassID), ToClassAccountID: id(t.ToAccountID), ToClassID: opt(t.ToClassID), Shares: t.Shares, ToShares: t.ToShares, Price: t.Price,
		Amount: t.Amount, ResolutionRefs: t.ResolutionRefs, ReversesID: id(t.ReversesID), Reason: t.Reason, Note: t.Note}
}

// equityView is what showing transactions reads once: names, account codes, approvals waiting, movements by
// transaction, the reversal of each, the share issues and today.
type equityView struct {
	names     map[uuid.UUID]string
	codes     map[uuid.UUID]string
	asked     map[string]string
	moved     map[uuid.UUID][]storage.HoldingMovement
	reversals map[uuid.UUID]uuid.UUID // a reversed transaction → its reversal
	issues    map[uuid.UUID]storage.ShareIssue
	settings  ledger.Settings
	today     time.Time
}

func (a *API) equityViewOf(ctx context.Context, u storage.UnitOfWork) (equityView, error) {
	v := equityView{names: map[uuid.UUID]string{}, codes: map[uuid.UUID]string{}, moved: map[uuid.UUID][]storage.HoldingMovement{},
		reversals: map[uuid.UUID]uuid.UUID{}, issues: map[uuid.UUID]storage.ShareIssue{}}
	var err error
	if v.settings, err = journal.LoadSettings(ctx, u); err != nil {
		return v, err
	}
	for id, acc := range v.settings.Accounts {
		v.codes[id] = acc.Code
	}
	if v.today, err = orgToday(ctx, a, u); err != nil {
		return v, err
	}
	if v.asked, err = a.waiting(ctx, u, approval.ObjectEquityTransaction); err != nil {
		return v, err
	}
	moves, err := u.Holdings().List(ctx)
	if err != nil {
		return v, err
	}
	byID := map[uuid.UUID]storage.HoldingMovement{}
	for _, m := range moves {
		byID[m.ID] = m
		if m.TransactionID != nil {
			v.moved[*m.TransactionID] = append(v.moved[*m.TransactionID], m)
		}
	}
	for _, m := range moves {
		if m.ReversesID != nil && m.TransactionID != nil {
			if was := byID[*m.ReversesID]; was.TransactionID != nil {
				v.reversals[*was.TransactionID] = *m.TransactionID
			}
		}
	}
	issues, err := u.ShareIssues().List(ctx)
	if err != nil {
		return v, err
	}
	for _, s := range issues {
		v.issues[s.ID] = s
	}
	return v, nil
}

func (v equityView) name(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (string, error) {
	if n, ok := v.names[id]; ok || id == uuid.Nil {
		return n, nil
	}
	c, err := u.Contacts().Get(ctx, id)
	if err != nil {
		return "", err
	}
	v.names[id] = c.LegalName
	return c.LegalName, nil
}

// equityTransactionStatus is where a transaction stands.
func (v equityView) status(doc storage.Document) string {
	switch {
	case doc.Status == storage.DocumentDraft:
		return "draft"
	case v.reversals[doc.ID] != uuid.Nil:
		return "reversed"
	case len(v.moved[doc.ID]) > 0:
		return "effective"
	}
	return "approved"
}

// equityTransactionOf is a transaction as the API shows it.
func (a *API) equityTransactionOf(ctx context.Context, u storage.UnitOfWork, doc storage.Document, v equityView) (EquityTransaction, error) {
	t, err := equity.ReadTransaction(doc)
	if err != nil {
		return EquityTransaction{}, err
	}
	out := EquityTransaction{ID: doc.ID.String(), Number: doc.Number, Version: doc.Version, Status: v.status(doc), Details: transactionDraftOf(t),
		Parties: []EquityParty{}, Checks: []BillCheck{}, Movements: []EquityMovement{}, Entries: []EquityEntry{}, ClassCode: v.codes[t.ClassAccountID],
		ApprovalID: v.asked[doc.ID.String()]}
	for _, p := range []struct {
		id   uuid.UUID
		role string
	}{{t.HolderID, map[bool]string{true: "subscriber", false: "holder"}[t.Kind == equity.KindIssueCash]}, {t.SellerID, "seller"}, {t.BuyerID, "buyer"}} {
		if p.id == uuid.Nil {
			continue
		}
		n, err := v.name(ctx, u, p.id)
		if err != nil {
			return out, err
		}
		out.Parties = append(out.Parties, EquityParty{ContactID: p.id.String(), Name: n, Role: p.role})
	}
	if t.Amount != "" {
		if m, err := money.ParseStored(t.Amount, v.settings.Base); err == nil {
			mo := moneyOut(m)
			out.Amount = &mo
		}
	}
	if t.ApprovedOn != nil {
		out.ApprovedOn = t.ApprovedOn.Format(time.DateOnly)
	}
	if r := v.reversals[doc.ID]; r != uuid.Nil {
		out.ReversedBy = r.String()
	}
	if out.Evidence, err = attachedFiles(ctx, u, doc.ID); err != nil {
		return out, err
	}
	for _, m := range v.moved[doc.ID] {
		n, err := v.name(ctx, u, m.HolderID)
		if err != nil {
			return out, err
		}
		em := EquityMovement{ID: m.ID.String(), Kind: m.Kind, HolderID: m.HolderID.String(), HolderName: n, ClassCode: v.codes[m.ClassAccountID],
			Shares: string(m.Shares), EffectiveOn: m.EffectiveOn.Format(time.DateOnly)}
		if m.ClassID != nil {
			em.ClassID = m.ClassID.String()
		}
		out.Movements = append(out.Movements, em)
		out.EffectiveOn = em.EffectiveOn
		if m.ShareIssueID != nil {
			out.ShareIssue = m.ShareIssueID.String()
		}
	}
	if out.Entries, err = a.equityEntries(ctx, u, doc, t, v); err != nil {
		return out, err
	}
	if doc.Status == storage.DocumentPosted && t.Kind != equity.KindReversal && t.ApprovedOn != nil {
		if out.ACRA, err = a.equityACRA(ctx, u, doc, t, v, out.Status == "approved"); err != nil {
			return out, err
		}
	}
	switch out.Status {
	case "draft":
		if out.Evidence == 0 {
			out.Checks = append(out.Checks, BillCheck{Severity: "error", Code: equity.CodeNoEvidence, Field: "evidence",
				Message: "it has no evidence attached, so it cannot be asked for approval", Fix: "Attach the resolutions, the instrument of transfer or the ACRA extract"})
		}
		if _, err := equity.CheckTransaction(ctx, u, t, v.settings); err != nil {
			if probs, ok := err.(ledger.Problems); ok {
				for _, p := range probs {
					out.Checks = append(out.Checks, BillCheck{Severity: "error", Code: p.Code, Field: p.Field, Message: p.Message, Fix: p.Fix})
				}
			} else {
				return out, err
			}
		}
	case "approved":
		if t.Kind == equity.KindIssueCash && out.Amount != nil {
			if out.Paid, err = a.paidIn(ctx, u, v, t); err != nil {
				return out, err
			}
			if out.Paid == nil {
				out.Checks = append(out.Checks, BillCheck{Severity: "error", Code: equity.CodeNoMonies, Field: "paid",
					Message: "the chart of accounts has no share application monies account", Fix: "Add 2040 Share application monies, then match the subscriber's payment to it"})
				break
			}
			paid, err := money.ParseStored(out.Paid.Amount, v.settings.Base)
			if err != nil {
				return out, err
			}
			due, err := money.ParseStored(out.Amount.Amount, v.settings.Base)
			if err != nil {
				return out, err
			}
			if c, err := paid.Cmp(due); err != nil {
				return out, err
			} else if c < 0 {
				out.Checks = append(out.Checks, BillCheck{Severity: "error", Code: equity.CodeEffective, Field: "paid",
					Message: fmt.Sprintf("%s %s of the %s %s due is paid", paid, paid.Currency().Code, due, due.Currency().Code),
					Fix:     "Match the subscriber's payment to share application monies, then record the effective date"})
			}
		}
	}
	return out, nil
}

// equityACRA is an approved transaction's ACRA steps, and while it waits for its effective date what they raise.
func (a *API) equityACRA(ctx context.Context, u storage.UnitOfWork, doc storage.Document, t equity.Transaction, v equityView, waiting bool) (*EquityACRA, error) {
	all, err := u.EquitySteps().Of(ctx, doc.ID)
	if err != nil {
		return nil, err
	}
	out := &EquityACRA{Steps: []EquityStepView{}, Flags: []ConversionFlag{}}
	for _, x := range all {
		out.Steps = append(out.Steps, EquityStepView{ID: x.ID.String(), Kind: x.Kind, On: dateOf(x.On), Reference: x.Reference,
			RecordedAt: x.RecordedAt.UTC().Format(time.RFC3339)})
	}
	steps := equity.StepsOf(all)
	var lapses time.Time
	if t.Kind == equity.KindIssueCash {
		o, err := u.Organisation(ctx)
		if err != nil {
			return nil, err
		}
		if s, ok := steps[storage.StepAGMDue]; ok {
			lapses = s.On
		} else if on, ok := director.LapsesOn(o.Profile, *t.ApprovedOn); ok {
			lapses = on
		}
		if !lapses.IsZero() {
			out.AGMDueOn = dateOf(lapses)
		}
	}
	if !waiting {
		return out, nil
	}
	days, err := director.RegistrationDays(ctx, u, v.today)
	if err != nil {
		return nil, err
	}
	for _, f := range equity.Flags(t.Kind, steps, *t.ApprovedOn, lapses, v.today, days) {
		x := ConversionFlag{Code: f.Code, Kind: f.Kind, Message: f.Message, Fix: f.Fix}
		if f.DueOn != nil {
			x.DueOn = dateOf(*f.DueOn)
		}
		out.Flags = append(out.Flags, x)
	}
	return out, nil
}

// equityEntries are the entries an effective transaction posted: a cash issue's from the register, a conversion's between
// accounts, or a reversal's of those of the transaction it reverses.
func (a *API) equityEntries(ctx context.Context, u storage.UnitOfWork, doc storage.Document, t equity.Transaction, v equityView) ([]EquityEntry, error) {
	out := []EquityEntry{}
	if len(v.moved[doc.ID]) == 0 {
		return out, nil
	}
	if t.Kind == equity.KindReversal {
		was, err := u.Documents().Get(ctx, t.ReversesID)
		if err != nil {
			return nil, err
		}
		wasT, err := equity.ReadTransaction(was)
		if err != nil {
			return nil, err
		}
		reversed, err := a.equityEntries(ctx, u, was, wasT, v)
		if err != nil {
			return nil, err
		}
		for _, x := range reversed {
			e, err := u.Ledger().Entry(ctx, uuid.MustParse(x.ID))
			if err != nil {
				return nil, err
			}
			if r, err := u.Ledger().ByIdentity(ctx, ledger.ReversalIdentity(e.PostingIdentity)); err == nil {
				out = append(out, EquityEntry{ID: r.ID.String()})
			}
		}
		return out, nil
	}
	for _, m := range v.moved[doc.ID] {
		if m.ShareIssueID == nil {
			continue
		}
		if s, ok := v.issues[*m.ShareIssueID]; ok && s.EntryID != nil {
			out = append(out, EquityEntry{ID: s.EntryID.String()})
		}
	}
	if e, err := u.Ledger().ByIdentity(ctx, ledger.ClassConversionIdentity(doc.ID, doc.Version, ledger.RuleClassConversion)); err == nil {
		out = append(out, EquityEntry{ID: e.ID.String()})
	}
	return out, nil
}

// paidIn is what a cash issue's subscriber's share application monies hold today, nil when the chart has no account.
func (a *API) paidIn(ctx context.Context, u storage.UnitOfWork, v equityView, t equity.Transaction) (*Money, error) {
	for id, acc := range v.settings.Accounts {
		if acc.SubscriberAccount() && acc.Active && !acc.Group {
			held, err := balance.ApplicationMonies(ctx, u, v.settings.Base, id, t.HolderID, v.today)
			if err != nil {
				return nil, err
			}
			m := moneyOut(held)
			return &m, nil
		}
	}
	return nil, nil
}

func (a *API) shownEquityTransaction(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (EquityTransaction, storage.Document, error) {
	doc, err := equityRecordOfType(ctx, u, storage.DocumentEquityTransaction, id)
	if err != nil {
		return EquityTransaction{}, doc, err
	}
	v, err := a.equityViewOf(ctx, u)
	if err != nil {
		return EquityTransaction{}, doc, err
	}
	out, err := a.equityTransactionOf(ctx, u, doc, v)
	return out, doc, err
}

// equityTransactionDependencies are a transaction's envelope values: its status and the holders it moves shares of or for
// (FR-APR-05).
func equityTransactionDependencies(ctx context.Context, u storage.UnitOfWork, key string) (int, map[string]string, error) {
	id, err := uuid.Parse(key)
	if err != nil {
		return 0, nil, err
	}
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return 0, nil, err
	}
	t, err := equity.ReadTransaction(doc)
	if err != nil {
		return 0, nil, err
	}
	parties, err := equity.PartiesOf(ctx, u, t)
	if err != nil {
		return 0, nil, err
	}
	ids := make([]string, 0, len(parties))
	for _, p := range parties {
		ids = append(ids, p.String())
	}
	sort.Strings(ids)
	return doc.Version, map[string]string{"status": string(doc.Status), "parties": strings.Join(ids, ",")}, nil
}

// equityBeneficiary refuses a member who signs in as a holder the transaction moves shares of or for (FM-1, FM-9).
func equityBeneficiary(ctx context.Context, u storage.UnitOfWork, id, signer uuid.UUID, what string) error {
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return err
	}
	t, err := equity.ReadTransaction(doc)
	if err != nil {
		return err
	}
	parties, err := equity.PartiesOf(ctx, u, t)
	if err != nil {
		return err
	}
	for _, p := range parties {
		c, err := u.Contacts().Get(ctx, p)
		if err != nil {
			return err
		}
		if c.MemberUserID != nil && *c.MemberUserID == signer {
			return problem(approval.CodeBeneficiary, "The transaction moves your own shares, so another person "+what+" (FM-9).",
				"Ask another person who holds the approval right on the cap table")
		}
	}
	return nil
}

// equityTransactionExecutable refuses a holder the transaction is for: never their own approval (FM-9).
func equityTransactionExecutable(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest, signer uuid.UUID) error {
	id, err := uuid.Parse(r.ObjectKey)
	if err != nil {
		return err
	}
	return equityBeneficiary(ctx, u, id, signer, "approves it")
}

// everyEquityApproverBenefits says whether each member who could approve a transaction for parties asked by requester (an
// Owner or the Controller), other than the requester and the parties' own members, is a holder with an equity
// transaction of their own waiting or approved in the last 30 days (FM-9): then the Accountant reviews it too
// [accountant to confirm]. With nobody else who could sign, it is false.
func (a *API) everyEquityApproverBenefits(ctx context.Context, u storage.UnitOfWork, asked map[string]string, requester uuid.UUID, parties []uuid.UUID) (bool, error) {
	theirs := map[uuid.UUID]bool{}
	for _, p := range parties {
		c, err := u.Contacts().Get(ctx, p)
		if err != nil {
			return false, err
		}
		if c.MemberUserID != nil {
			theirs[*c.MemberUserID] = true
		}
	}
	members, err := u.Members().List(ctx, a.deps.Clock.Now())
	if err != nil {
		return false, err
	}
	waiting := map[string]bool{}
	for k := range asked {
		waiting[k] = true
	}
	benefits, err := equity.PartiesSince(ctx, u, waiting, a.deps.Clock.Now().AddDate(0, 0, -30))
	if err != nil {
		return false, err
	}
	var candidates []uuid.UUID
	for _, m := range members {
		if m.UserID == requester || theirs[m.UserID] {
			continue
		}
		if slices.ContainsFunc(m.Roles, func(r storage.RoleAssignment) bool {
			return r.Role == string(authz.Owner) || r.Role == string(authz.Controller)
		}) {
			candidates = append(candidates, m.UserID)
		}
	}
	contactOf := func(user uuid.UUID) (uuid.UUID, bool) {
		c, err := u.Contacts().OfMember(ctx, user)
		return c.ID, err == nil
	}
	return director.EveryApproverBenefits(candidates, contactOf, benefits), nil
}

func equityTransactionETag(doc storage.Document) string { return recordETag(doc) }

func registerEquityTransactions(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid}, locks...)
	answer := func(w written[EquityTransaction], doc storage.Document) *equityTransactionWriteOutput {
		if w.Previewed {
			return &equityTransactionWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}
		}
		status := "draft"
		if w.Body.Status != "draft" {
			status = "posted"
		}
		return &equityTransactionWriteOutput{Status: w.Status, ETag: etag(w.Body.Version, status), Location: "/v1/equity-transactions/" + w.Body.ID,
			Replayed: replayedHeader(w.Replayed), Body: w.Body}
	}

	register(a, authz.ListEquityTransactions, huma.Operation{
		OperationID: "listEquityTransactions", Method: http.MethodGet, Path: "/v1/equity-transactions", Tags: []string{"Equity"},
		Summary: "List equity transactions",
		Description: "The equity transactions, drafts included, the latest drafted first, a page at a time, narrowed by kind (FR-EQ-17): cash " +
			"issues, transfers, conversions between classes and reversals, each with where it stands (draft, approved, effective or " +
			"reversed), the holders it is for, and the holding movements it wrote. Read-only; no side effects.",
	}, []string{CodeCursor, CodeInvalid}, func(ctx context.Context, in *equityTransactionListInput) (*equityTransactionListOutput, error) {
		p, _ := authn.From(ctx)
		out := equityTransactionListOutput{Body: EquityTransactionPage{Items: []EquityTransaction{}}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			docs, err := equity.Transactions(ctx, u)
			if err != nil {
				return err
			}
			v, err := a.equityViewOf(ctx, u)
			if err != nil {
				return err
			}
			var all []EquityTransaction
			for i := len(docs) - 1; i >= 0; i-- {
				t, err := a.equityTransactionOf(ctx, u, docs[i], v)
				if err != nil {
					return err
				}
				if in.Kind == "" || t.Details.Kind == in.Kind {
					all = append(all, t)
				}
			}
			sort.SliceStable(all, func(i, j int) bool { return all[i].ID > all[j].ID })
			page, next, err := pageOf(all, func(x EquityTransaction) string { return x.ID }, in.PageQuery)
			if page != nil {
				out.Body.Items = page
			}
			out.Body.NextCursor = next
			return err
		})
		return &out, err
	})

	register(a, authz.CreateEquityTransaction, huma.Operation{
		OperationID: "createEquityTransaction", Method: http.MethodPost, Path: "/v1/equity-transactions", Tags: []string{"Equity"},
		DefaultStatus: http.StatusCreated, Summary: "Draft an equity transaction",
		Description: "Drafts an equity transaction (FR-EQ-17): issue_cash allots shares to one subscriber for cash at a price per share (its " +
			"amount shares × price under RND-LINE); transfer moves shares from one holder to another; class_conversion moves one holder's " +
			"shares into another class, with the carrying amount when the classes' accounts differ [accountant to confirm the basis]; " +
			"reversal undoes an effective transaction, citing why. Attach its evidence with addEquityTransactionEvidence, then ask for " +
			"approval with requestEquityTransactionApproval; once approved, recordEquityTransactionEffective records the day ACRA's register " +
			"shows it. Nothing moves until then. Side effects: the draft, audit events. Every problem comes back together. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{CodeNotFound}, writes...), EquityTransactionCodes...), func(ctx context.Context, in *equityTransactionCreateInput) (*equityTransactionWriteOutput, error) {
		var doc storage.Document
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (EquityTransaction, error) {
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return EquityTransaction{}, err
			}
			settings, err := journal.LoadSettings(ctx, u)
			if err != nil {
				return EquityTransaction{}, err
			}
			if doc, _, err = equity.DraftTransactionIn(ctx, u, transactionIn(in.Body, today), settings); err != nil {
				return EquityTransaction{}, equityProblems(err)
			}
			out, d, err := a.shownEquityTransaction(ctx, u, doc.ID)
			doc = d
			return out, err
		})
		if err != nil {
			return nil, err
		}
		return answer(w, doc), nil
	})

	register(a, authz.GetEquityTransaction, huma.Operation{
		OperationID: "getEquityTransaction", Method: http.MethodGet, Path: "/v1/equity-transactions/{transaction_id}", Tags: []string{"Equity"},
		Summary: "Get an equity transaction",
		Description: "Returns an equity transaction with its ETag (FR-EQ-17): what was drafted, where it stands, the holders it is for, how " +
			"many files are attached, the approval waiting, what stops it moving on, a cash issue's application monies paid in, and once " +
			"effective its movements and entries. Read-only; no side effects.",
	}, append([]string{CodeNotFound}, locks...), func(ctx context.Context, in *EquityTransactionPath) (*equityTransactionReadOutput, error) {
		id, err := parseID("transaction_id", in.TransactionID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out equityTransactionReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			body, doc, err := a.shownEquityTransaction(ctx, u, id)
			out.Body, out.ETag = body, equityTransactionETag(doc)
			return err
		})
		return &out, err
	})

	register(a, authz.ReplaceEquityTransaction, huma.Operation{
		OperationID: "replaceEquityTransaction", Method: http.MethodPut, Path: "/v1/equity-transactions/{transaction_id}", Tags: []string{"Equity"},
		Summary: "Change a drafted equity transaction",
		Description: "Saves a new version of an equity transaction not approved yet (FR-EQ-17). An approved one changes no more (EQUITY-05): " +
			"once effective, a reversal corrects it. Side effects: the new version, audit events. Needs If-Match with its ETag. Idempotent " +
			"with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired}, writes...), EquityTransactionCodes...), func(ctx context.Context, in *equityTransactionReviseInput) (*equityTransactionWriteOutput, error) {
		id, err := parseID("transaction_id", in.TransactionID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		var doc storage.Document
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (EquityTransaction, error) {
			cur, _, err := equity.DraftedTransaction(ctx, u, id)
			if err != nil {
				return EquityTransaction{}, err
			}
			if err := checkIfMatch(in.IfMatch, equityTransactionETag(cur)); err != nil {
				return EquityTransaction{}, err
			}
			settings, err := journal.LoadSettings(ctx, u)
			if err != nil {
				return EquityTransaction{}, err
			}
			if _, _, err := equity.ReviseTransactionIn(ctx, u, id, transactionIn(in.Body, time.Time{}), settings); err != nil {
				return EquityTransaction{}, equityProblems(err)
			}
			out, d, err := a.shownEquityTransaction(ctx, u, id)
			doc = d
			return out, err
		})
		if err != nil {
			return nil, err
		}
		return answer(w, doc), nil
	})

	register(a, authz.RequestEquityTransactionApproval, huma.Operation{
		OperationID: "requestEquityTransactionApproval", Method: http.MethodPost, Path: "/v1/equity-transactions/{transaction_id}/approval", Tags: []string{"Equity"},
		DefaultStatus: http.StatusAccepted, Summary: "Ask for approval of an equity transaction",
		Description: "Asks another person to approve a drafted equity transaction at the version read (FR-EQ-17, FM-1): it needs its " +
			"evidence attached (EQUITY-06) and passes its checks again. The answer is 202 with an approval request, which a person who " +
			"holds A on \"Cap table, holders, investors, equity transactions\" approves, stepped up: never the one who asked, nor a holder " +
			"it moves shares of or for (FM-9); when every available approver has an equity transaction of their own waiting or approved in " +
			"the last 30 days, a final step the Accountant signs records their review [accountant to confirm]. On the approval it is " +
			"numbered EQ- and waits for its effective date; nothing moves yet. The request holds the transaction's version, status and " +
			"holders, so a change voids it. Needs If-Match with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it " +
			"and asks nothing.",
	}, append(append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeSessionRequired, CodeStepUpRequired}, writes...), EquityTransactionCodes...),
		func(ctx context.Context, in *equityTransactionAskInput) (*equityAskOutput, error) {
			id, err := parseID("transaction_id", in.TransactionID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
				doc, t, err := equity.DraftedTransaction(ctx, u, id)
				if err != nil {
					return Approval{}, err
				}
				if err := checkIfMatch(in.IfMatch, equityTransactionETag(doc)); err != nil {
					return Approval{}, err
				}
				if n, err := attachedFiles(ctx, u, id); err != nil {
					return Approval{}, err
				} else if n == 0 {
					return Approval{}, &equity.Error{Code: equity.CodeNoEvidence, Message: "the transaction has no evidence attached",
						Fix: "Attach the resolutions, the instrument of transfer or the ACRA extract, then ask again"}
				}
				v, err := a.equityViewOf(ctx, u)
				if err != nil {
					return Approval{}, err
				}
				if t, err = equity.CheckTransaction(ctx, u, t, v.settings); err != nil {
					return Approval{}, equityProblems(err)
				}
				parties, err := equity.PartiesOf(ctx, u, t)
				if err != nil {
					return Approval{}, err
				}
				p, _ := authn.From(ctx)
				review, err := a.everyEquityApproverBenefits(ctx, u, v.asked, p.UserID, parties)
				if err != nil {
					return Approval{}, err
				}
				var then []approval.Step
				if review { // FM-9: every available approver benefits, so the Accountant reviews it too [accountant to confirm]
					then = []approval.Step{{Quorum: 1, Roles: []string{string(authz.Accountant)}}}
				}
				shown, err := a.equityTransactionOf(ctx, u, doc, v)
				if err != nil {
					return Approval{}, err
				}
				names := make([]string, 0, len(shown.Parties))
				for _, x := range shown.Parties {
					names = append(names, x.Name+" ("+x.Role+")")
				}
				diff := []ApprovalDiff{{Field: "status", Before: "draft", After: "approved"}, {Field: "kind", After: t.Kind},
					{Field: "holders", After: strings.Join(names, ", ")}, {Field: "accountant_review", After: strconv.FormatBool(review)}}
				if t.Shares != "" {
					diff = append(diff, ApprovalDiff{Field: "shares", After: t.Shares + " of " + v.codes[t.ClassAccountID]})
				}
				var amounts []approval.Amount
				if shown.Amount != nil {
					diff = append(diff, ApprovalDiff{Field: "amount", After: shown.Amount.Amount + " " + shown.Amount.Currency})
					amounts = append(amounts, approval.Amount{Amount: shown.Amount.Amount, Currency: shown.Amount.Currency})
				}
				switch t.Kind {
				case equity.KindIssueCash: // the entry it posts when effective, from share application monies (B.7)
					if shown.Amount == nil {
						break
					}
					diff = append(diff, ApprovalDiff{Field: "debit 2040", After: shown.Amount.Amount + " " + shown.Amount.Currency},
						ApprovalDiff{Field: "credit " + v.codes[t.ClassAccountID], After: shown.Amount.Amount + " " + shown.Amount.Currency})
				case equity.KindClassConversion:
					diff = append(diff, ApprovalDiff{Field: "to", After: t.ToShares + " of " + v.codes[t.ToAccountID]})
				case equity.KindReversal:
					diff = append(diff, ApprovalDiff{Field: "reverses", After: t.ReversesID.String()}, ApprovalDiff{Field: "reason", After: t.Reason})
				}
				summary := map[string]string{equity.KindIssueCash: "Issue %s shares for cash to %s", equity.KindTransfer: "Transfer %s shares: %s",
					equity.KindClassConversion: "Convert %s shares of %s", equity.KindReversal: "Reverse an equity transaction%s for %s"}[t.Kind]
				return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectEquityTransaction, ObjectKey: doc.ID.String(),
					Operation: approval.OpEquityTransactionApprove, Summary: fmt.Sprintf(summary, t.Shares, strings.Join(names, ", ")),
					Execute: map[string]any{"transaction_id": doc.ID.String(), "version": strconv.Itoa(doc.Version), "kind": t.Kind},
					Diff:    diff, Amounts: amounts, Then: then})
			})
			if err != nil {
				return nil, err
			}
			return &equityAskOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})

	register(a, authz.RecordEquityStep, huma.Operation{
		OperationID: "recordEquityStep", Method: http.MethodPost, Path: "/v1/equity-transactions/{transaction_id}/acra-steps", Tags: []string{"Equity"},
		DefaultStatus: http.StatusCreated, Summary: "Record an ACRA step of an equity transaction",
		Description: "Records one ACRA step of an approved equity transaction not yet effective (FR-EQ-09, FR-EQ-17): a cash issue's day the " +
			"shareholders resolved, the day their approval was lodged (due within 14 days of the resolution, s161(5), s186; ⏰ until it " +
			"is), the date by which the next AGM must be held when it differs from the one worked out [verify], and the day the return of " +
			"allotment was lodged; a transfer's day the instrument of transfer was signed and the day the notice of transfer was lodged " +
			"(s126; ACRA's guidance expects it within 14 days [verify]); a conversion between classes' day the shareholders resolved and the " +
			"day it was lodged. Attach ACRA's acknowledgement as evidence. A later step of a kind supersedes the earlier, which stays. " +
			"Needs a person signed in who passed step-up, and If-Match with the transaction's ETag. Side effects: the step, append-only, " +
			"and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, []string{equity.CodeStep, equity.CodeEffective, storage.CodeEquityStep, CodeNotFound, CodeStale, CodeIfMatchRequired, CodeSessionRequired,
		CodeStepUpRequired, CodeInvalid, CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		storage.CodeRetry, storage.CodeLockTimeout},
		func(ctx context.Context, in *equityStepInput) (*equityTransactionWriteOutput, error) {
			id, err := parseID("transaction_id", in.TransactionID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			on, err := time.Parse(time.DateOnly, in.Body.On)
			if err != nil {
				return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.on", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"}})
			}
			p, err := a.steppedUpPerson(ctx, "Recording an equity transaction's ACRA step")
			if err != nil {
				return nil, err
			}
			var doc storage.Document
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (EquityTransaction, error) {
				cur, err := u.Documents().Lock(ctx, id)
				if err != nil {
					return EquityTransaction{}, err
				}
				if cur.Type != storage.DocumentEquityTransaction {
					return EquityTransaction{}, problem(CodeNotFound, "There is no such equity transaction.", "List the equity transactions to find it.")
				}
				if err := checkIfMatch(in.IfMatch, equityTransactionETag(cur)); err != nil {
					return EquityTransaction{}, err
				}
				t, err := equity.ReadTransaction(cur)
				if err != nil {
					return EquityTransaction{}, err
				}
				moved, err := u.Holdings().OfTransaction(ctx, id)
				if err != nil {
					return EquityTransaction{}, err
				}
				if cur.Status != storage.DocumentPosted || len(moved) > 0 {
					return EquityTransaction{}, &equity.Error{Code: equity.CodeEffective, Message: "only an approved transaction waiting for ACRA records its steps",
						Fix: "Record the steps between the transaction's approval and its effective date"}
				}
				today, err := orgToday(ctx, a, u)
				if err != nil {
					return EquityTransaction{}, err
				}
				ref := strings.TrimSpace(in.Body.Reference)
				if err := equity.CheckStep(t.Kind, in.Body.Kind, on, ref, today); err != nil {
					return EquityTransaction{}, err
				}
				if _, err := u.EquitySteps().Record(ctx, storage.EquityStep{TransactionID: id, Kind: in.Body.Kind, On: on, Reference: ref,
					RecordedBy: p.UserID, RecordedAt: a.deps.Clock.Now()}); err != nil {
					return EquityTransaction{}, err
				}
				out, d, err := a.shownEquityTransaction(ctx, u, id)
				doc = d
				return out, err
			})
			if err != nil {
				return nil, err
			}
			return answer(w, doc), nil
		})

	register(a, authz.RecordEquityTransactionEffective, huma.Operation{
		OperationID: "recordEquityTransactionEffective", Method: http.MethodPost, Path: "/v1/equity-transactions/{transaction_id}/effective", Tags: []string{"Equity"},
		Summary: "Record the day an equity transaction takes effect",
		Description: "Records the day ACRA's register of members shows an approved equity transaction, and ACRA's lodgement reference " +
			"(FR-EQ-17, FR-EQ-09): no later than today, no earlier than its approval, in an open period (reopen it rather than choose a " +
			"later day). It writes everything the transaction moves in one transaction: a cash issue, once its subscriber's share " +
			"application monies on that day cover its amount (B.7), marks them shareholder, posts share_issue_cash@1 (Dr 2040 naming them, " +
			"Cr the class) and records the issue in the register; a transfer, once the seller holds the shares, moves them to the buyer " +
			"with no entry; a conversion moves the shares between classes and, when the accounts differ, posts its carrying amount " +
			"between them; a reversal undoes every movement and entry of the transaction it names, on its day. The database refuses any " +
			"movement that does not fit (FY288). Needs a person signed in who passed step-up, never a holder it moves shares of or for, " +
			"and If-Match with its ETag. Side effects: the movements, share issue and entries, and their audit events. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append(append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeSessionRequired, CodeStepUpRequired, approval.CodeBeneficiary}, writes...),
		EquityTransactionCodes...),
		func(ctx context.Context, in *equityTransactionEffectiveInput) (*equityTransactionWriteOutput, error) {
			id, err := parseID("transaction_id", in.TransactionID)
			if err != nil {
				return nil, err
			}
			on, err := time.Parse(time.DateOnly, in.Body.EffectiveOn)
			if err != nil {
				return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.effective_on", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"}})
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			p, err := a.steppedUpPerson(ctx, "Recording an equity transaction's effective date")
			if err != nil {
				return nil, err
			}
			var doc storage.Document
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (EquityTransaction, error) {
				cur, err := equityRecordOfType(ctx, u, storage.DocumentEquityTransaction, id)
				if err != nil {
					return EquityTransaction{}, err
				}
				if err := checkIfMatch(in.IfMatch, equityTransactionETag(cur)); err != nil {
					return EquityTransaction{}, err
				}
				if err := equityBeneficiary(ctx, u, id, p.UserID, "records it"); err != nil {
					return EquityTransaction{}, err
				}
				today, err := orgToday(ctx, a, u)
				if err != nil {
					return EquityTransaction{}, err
				}
				if _, err := a.equities.EffectiveIn(ctx, u, id, journal.EquityEffective{EffectiveOn: on, LodgementRef: strings.TrimSpace(in.Body.LodgementRef),
					Today: today, RecordedBy: p.UserID}); err != nil {
					return EquityTransaction{}, equityProblems(err)
				}
				out, d, err := a.shownEquityTransaction(ctx, u, id)
				doc = d
				return out, err
			})
			if err != nil {
				return nil, err
			}
			return answer(w, doc), nil
		})

}

// afterEquity runs once an equity transaction is effective: its files go to the locked store (FR-DOC-13), and when it
// posted an entry those who approve postings hear of it (FR-NTF-01).
func (a *API) afterEquity(ctx context.Context, u storage.UnitOfWork, doc storage.Document, entry storage.Entry) error {
	if entry.ID == uuid.Nil {
		return a.commitEvidence(ctx, u, doc, entry)
	}
	return a.afterPosting(ctx, u, doc, entry)
}
