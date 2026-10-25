package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/banking"
	"github.com/cognizhi/fyniq/internal/domain/banktransaction"
	"github.com/cognizhi/fyniq/internal/domain/billpayment"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/receipt"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The bank line actions and transfers (FR-BNK-05…09, FR-BNK-14; p1-bank-import-reconcile task 9.1, design D14): a line
// categorised, put in suspense, matched to invoices, bills or money recorded by hand, kept or excluded, and put back in
// the queue; transfers between money accounts recorded, completed and undone. Each posts through the same commands the
// rules and auto-reconcile use, in the request's unit of work.

// BankPartDraft is one part of a line as a person codes it (FR-BNK-05).
type BankPartDraft struct {
	AccountID    string `json:"account_id" format:"uuid" doc:"The account it posts to: an active posting account, not a money account, receivables or payables." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d30"`
	Amount       *Money `json:"amount,omitempty" doc:"Its signed amount in the line's currency, money in positive. Left out, the rest of the line, for one part at most: a whole line is one part without it."`
	GSTCode      string `json:"gst_code,omitempty" maxLength:"16" doc:"Its GST code from the sg-gst pack on the line's date; left out, the account's default." example:"TX"`
	SpreadMonths int    `json:"spread_months,omitempty" minimum:"0" maximum:"60" doc:"Months it spreads over from the line's month, as a prepayment or deferred income released monthly (FR-GL-03); 0 for none." example:"12"`
	Description  string `json:"description,omitempty" maxLength:"200" doc:"What it was." example:"AWS October"`
}

// BankCategorisation is how a person codes a line: the contact and reference it names, and its parts.
type BankCategorisation struct {
	ContactID string          `json:"contact_id,omitempty" format:"uuid" doc:"The customer or supplier it was with." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e20"`
	Reference string          `json:"reference,omitempty" maxLength:"140" doc:"A reference kept with it." example:"Invoice 88213"`
	Parts     []BankPartDraft `json:"parts" minItems:"1" maxItems:"50" doc:"Its parts: together they make the line's amount."`
}

// BankInvoicesMatch matches money in to a customer's invoices (FR-BNK-07, FR-BNK-08).
type BankInvoicesMatch struct {
	CustomerID string                `json:"customer_id" format:"uuid" doc:"The customer who paid." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f20"`
	Currency   string                `json:"currency,omitempty" pattern:"^[A-Z]{3}$" doc:"The invoices' currency; left out, the line's." example:"SGD"`
	Invoices   []CustomerReceiptPart `json:"invoices" minItems:"1" maxItems:"100" doc:"What the line settles of each invoice."`
	Unapplied  *Money                `json:"unapplied,omitempty" doc:"What it received beyond the invoices, kept as a credit with the customer (FR-AR-18). Left out, what the parts leave of the line when the invoices are in its currency, else nothing."`
}

// BankBillsMatch matches money out to a supplier's bills, as BankInvoicesMatch does invoices.
type BankBillsMatch struct {
	SupplierID string            `json:"supplier_id" format:"uuid" doc:"The supplier paid." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e20"`
	Currency   string            `json:"currency,omitempty" pattern:"^[A-Z]{3}$" doc:"The bills' currency; left out, the line's." example:"SGD"`
	Bills      []BillPaymentPart `json:"bills" minItems:"1" maxItems:"100" doc:"What the line settles of each bill."`
	Unapplied  *Money            `json:"unapplied,omitempty" doc:"What it paid beyond the bills, kept as a credit with the supplier. Left out, what the parts leave of the line when the bills are in its currency, else nothing."`
}

// BankLineDone is a line as an action left it, with what the action made.
type BankLineDone struct {
	Line       BankLine `json:"line" doc:"The line as it is now, with its live match."`
	DocumentID string   `json:"document_id,omitempty" format:"uuid" doc:"The bank transaction, receipt or bill payment made from the line, or the document recorded by hand it is matched to." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b80"`
	TransferID string   `json:"transfer_id,omitempty" format:"uuid" doc:"The transfer the line is a side of." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b90"`
	Entry      *Entry   `json:"entry,omitempty" doc:"The entry it posted; absent when it posted nothing."`
}

// BankTransferDraft records a transfer between money accounts (FR-BNK-09): the lines known, one or both, and for a
// side whose line has not arrived, its account and amount.
type BankTransferDraft struct {
	FromLineID         string `json:"from_line_id,omitempty" format:"uuid" doc:"The paying account's line: money out." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b70"`
	ToLineID           string `json:"to_line_id,omitempty" format:"uuid" doc:"The receiving account's line: money in." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b71"`
	FromMoneyAccountID string `json:"from_money_account_id,omitempty" format:"uuid" doc:"Without the paying line: the account it left." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b10"`
	ToMoneyAccountID   string `json:"to_money_account_id,omitempty" format:"uuid" doc:"Without the receiving line: the account it reaches." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b11"`
	Paid               *Money `json:"paid,omitempty" doc:"Without the paying line: what left, as the provider reports it."`
	Received           *Money `json:"received,omitempty" doc:"Without the receiving line: what arrives, as the provider reports it."`
	Fee                *Money `json:"fee,omitempty" doc:"The fee the provider reports, in the paying currency, part of what was paid: it posts to the bank charges account (bank.charges_account)."`
	Date               string `json:"date,omitempty" format:"date" doc:"Without a line: the day it left. A line gives its own date." example:"2026-10-20"`
}

// BankTransfer is a transfer between money accounts and where it stands.
type BankTransfer struct {
	ID                 string `json:"id" format:"uuid" doc:"The transfer's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b90"`
	FromMoneyAccountID string `json:"from_money_account_id" format:"uuid" doc:"The paying account." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b10"`
	ToMoneyAccountID   string `json:"to_money_account_id" format:"uuid" doc:"The receiving account." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b11"`
	FromLineID         string `json:"from_line_id,omitempty" format:"uuid" doc:"The paying line; absent while in transit." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b70"`
	ToLineID           string `json:"to_line_id,omitempty" format:"uuid" doc:"The receiving line; absent while in transit." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b71"`
	Date               string `json:"date" format:"date" doc:"The day it left." example:"2026-10-20"`
	Paid               Money  `json:"paid" doc:"What left the paying account."`
	Received           Money  `json:"received" doc:"What reached the receiving account."`
	Fee                *Money `json:"fee,omitempty" doc:"The fee the provider reported."`
	EntryID            string `json:"entry_id,omitempty" format:"uuid" doc:"Its entry: each leg at its own currency's rate for the day (ENG-40)." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d80"`
	Status             string `json:"status" enum:"in_transit,complete,undone" doc:"In transit while a side's line has not arrived." example:"complete"`
	MadeBy             string `json:"made_by" doc:"Who recorded it: a person, or the system." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c4a01"`
	UndoReason         string `json:"undo_reason,omitempty" doc:"Why it was undone." example:"Two payments, not a transfer"`
	Version            int    `json:"version" doc:"Its version, as its ETag." example:"2"`
}

// BankTransferDone is a transfer as an action left it, its entry, and its lines.
type BankTransferDone struct {
	Transfer BankTransfer `json:"transfer" doc:"The transfer."`
	Entry    *Entry       `json:"entry,omitempty" doc:"The entry it posted, or the reversal an undo posted."`
	Lines    []BankLine   `json:"lines" nullable:"false" doc:"Its lines as they are now."`
}

type LinePath struct {
	LineID string `path:"line_id" format:"uuid" doc:"The line's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b70"`
}

type TransferPath struct {
	TransferID string `path:"transfer_id" format:"uuid" doc:"The transfer's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b90"`
}

// ReasonBody is a reason an action keeps in the audit trail.
type ReasonBody struct {
	Reason string `json:"reason" minLength:"1" maxLength:"500" doc:"Why, kept in the audit trail and on the reversal." example:"Matched to the wrong invoice"`
}

type categoriseInput struct {
	LinePath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body BankCategorisation
}

type lineActionInput struct {
	LinePath
	KeyHeader
	IfMatchHeader
	PreferHeader
}

type lineReasonInput struct {
	LinePath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body ReasonBody
}

type matchInvoicesInput struct {
	LinePath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body BankInvoicesMatch
}

type matchBillsInput struct {
	LinePath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body BankBillsMatch
}

type matchRecordedInput struct {
	LinePath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body struct {
		DocumentID string `json:"document_id" format:"uuid" doc:"The receipt, bill payment, deposit or refund recorded by hand that moved the line's money: one the line's suggestions name." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b80"`
	}
}

type lineDoneOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The line's version, for If-Match."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     BankLineDone
}

type reconcileNowInput struct {
	PreferHeader
	Body struct {
		MoneyAccountID string `json:"money_account_id,omitempty" format:"uuid" doc:"One money account's waiting lines; left out, every account's." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b10"`
	}
}

type reconcileNowOutput struct {
	Status  int
	Applied string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was posted (FR-INT-14)."`
	Body    AutoReconciled
}

type recordTransferInput struct {
	KeyHeader
	PreferHeader
	Body BankTransferDraft
}

type completeTransferInput struct {
	TransferPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body struct {
		LineID string `json:"line_id" format:"uuid" doc:"The side's line that arrived: the paying account's money out or the receiving account's money in, of the amount recorded." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b71"`
	}
}

type undoTransferInput struct {
	TransferPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body ReasonBody
}

type transferDoneOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The transfer's version, for If-Match."`
	Location string `header:"Location" doc:"The transfer's URL."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     BankTransferDone
}

type transfersInput struct {
	PageQuery
}

type transfersOutput struct {
	Body struct {
		Items      []BankTransfer `json:"items" nullable:"false" doc:"The transfers in transit, latest first."`
		NextCursor string         `json:"next_cursor,omitempty" doc:"The next page's cursor; absent on the last page." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b90"`
	}
}

type transferOutput struct {
	ETag string `header:"ETag" doc:"The transfer's version, for If-Match."`
	Body BankTransfer
}

// lineWithMatch is a line with its live match, as an answer shows it.
func lineWithMatch(ctx context.Context, u storage.UnitOfWork, l storage.BankLine) (BankLine, error) {
	out := bankLineOf(l)
	matches, err := u.BankMatching().Matches(ctx, storage.BankMatchFilter{LineID: &l.ID})
	if err != nil || len(matches) == 0 {
		return out, err
	}
	m := matches[0]
	out.Match = &BankMatch{Kind: m.Kind, DocumentID: idText(m.DocumentID), EntryID: idText(m.EntryID), TransferID: idText(m.TransferID),
		Basis: m.Basis, RuleID: idText(m.RuleID), MadeBy: m.MadeBy, MadeAt: m.MadeAt.UTC().Format(time.RFC3339)}
	return out, nil
}

func transferOf(t storage.BankTransfer) BankTransfer {
	out := BankTransfer{ID: t.ID.String(), FromMoneyAccountID: t.FromAccountID.String(), ToMoneyAccountID: t.ToAccountID.String(),
		FromLineID: idText(t.FromLineID), ToLineID: idText(t.ToLineID), Date: dateOf(t.OnDate), Paid: moneyOf(t.FromAmount, t.FromCurrency),
		Received: moneyOf(t.ToAmount, t.ToCurrency), EntryID: idText(t.EntryID), MadeBy: t.MadeBy, UndoReason: t.UndoReason, Version: t.Version,
		Status: "complete"}
	if t.Fee != nil {
		m := moneyOf(*t.Fee, t.FromCurrency)
		out.Fee = &m
	}
	switch {
	case t.UndoneAt != nil:
		out.Status = "undone"
	case t.FromLineID == nil || t.ToLineID == nil:
		out.Status = "in_transit"
	}
	return out
}

// bankProblems names the line's action in the posting rule's problems, each field as it is in the request.
func bankProblems(what string, err error) error {
	var lp ledger.Problems
	if !errors.As(err, &lp) || len(lp) == 0 {
		return err
	}
	fields := make([]FieldProblem, 0, len(lp))
	for _, x := range lp {
		fields = append(fields, FieldProblem{Code: x.Code, Field: "body." + x.Field, Message: x.Message, Fix: x.Fix})
	}
	return problem(lp[0].Code, fmt.Sprintf("%s: %d problem(s), each listed in errors.", what, len(lp)), "Correct the request and send it again.", fields...)
}

// actOnLine runs an action on a line at the If-Match version in the request's unit of work, and answers with the line
// as it is then. An action that posts (what names it) is refused where the caller's roles hold a conflict their team
// mode blocks.
func (a *API) actOnLine(ctx context.Context, what, idText, ifMatch, key, prefer string, request any,
	act func(ctx context.Context, u storage.UnitOfWork, org uuid.UUID, l storage.BankLine) (BankLineDone, error)) (*lineDoneOutput, error) {
	id, err := parseID("line_id", idText)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(ifMatch) == "" {
		return nil, checkIfMatch(ifMatch, "")
	}
	p, _ := authn.From(ctx)
	w, err := write(ctx, a, key, prefer, request, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (BankLineDone, error) {
		l, err := u.Banking().Line(ctx, id)
		if err != nil {
			return BankLineDone{}, err
		}
		if err := checkIfMatch(ifMatch, etag(l.Version)); err != nil {
			return BankLineDone{}, err
		}
		if what != "" {
			if err := checkConflicts(ctx, u, p, what); err != nil {
				return BankLineDone{}, err
			}
		}
		out, err := act(ctx, u, p.Organisation, l)
		if err != nil {
			return out, err
		}
		if l, err = u.Banking().Line(ctx, id); err != nil {
			return out, err
		}
		out.Line, err = lineWithMatch(ctx, u, l)
		return out, err
	})
	if err != nil {
		return nil, err
	}
	return &lineDoneOutput{Status: w.Status, ETag: etag(w.Body.Line.Version), Applied: applied(w), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
}

func registerBankLines(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid,
		CodeNotFound, CodeStale, CodeIfMatchRequired, storage.CodeVersion, storage.CodeBanking}, locks...)
	posting := append(append([]string{banktransaction.CodeLine, journal.CodeLineDone, ledger.CodeBankPart, ledger.CodeSystemRole}, postingCodes()...), writes...)

	register(a, authz.CategoriseBankLine, huma.Operation{
		OperationID: "categoriseBankLine", Method: http.MethodPost, Path: "/v1/bank-lines/{line_id}/categorise", Tags: []string{"Banking"},
		Summary: "Categorise a bank line",
		Description: "Codes a waiting line to accounts and posts it (FR-BNK-05, FR-BNK-06): one part or several, each with its GST code and " +
			"an optional spread over months (bank_transaction@1). A line in suspense is classified: its suspense posting is reversed and the " +
			"new one posted together. Side effects: a bank transaction and its entry, the line's match and status, each audited. Needs " +
			"If-Match with the line's ETag. Idempotent with Idempotency-Key; Prefer: return=preview returns the entry and saves nothing.",
	}, append([]string{banktransaction.CodeGSTCode, banktransaction.CodeContact, banktransaction.CodeTooLong, banktransaction.CodeRest}, posting...),
		func(ctx context.Context, in *categoriseInput) (*lineDoneOutput, error) {
			return a.actOnLine(ctx, "Posting", in.LineID, in.IfMatch, in.IdempotencyKey, in.Prefer, in, func(ctx context.Context, u storage.UnitOfWork, org uuid.UUID, l storage.BankLine) (BankLineDone, error) {
				d := banktransaction.Draft{LineID: l.ID, Reference: strings.TrimSpace(in.Body.Reference)}
				var fields []FieldProblem
				if in.Body.ContactID != "" {
					c, err := uuid.Parse(in.Body.ContactID)
					if err != nil {
						fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.contact_id", Message: "is not an ID", Fix: "Send the contact's ID"})
					}
					d.ContactID = &c
				}
				for i, x := range in.Body.Parts {
					at := func(f string) string { return "body.parts[" + strconv.Itoa(i) + "]." + f }
					acc, err := uuid.Parse(x.AccountID)
					if err != nil {
						fields = append(fields, FieldProblem{Code: CodeInvalid, Field: at("account_id"), Message: "is not an ID", Fix: "Send the account's ID"})
					}
					part := banktransaction.Part{AccountID: acc, GSTCode: x.GSTCode, Months: x.SpreadMonths, Description: strings.TrimSpace(x.Description)}
					if x.Amount != nil {
						m, bad := moneyIn(*x.Amount, at("amount"))
						if bad != nil {
							fields = append(fields, *bad)
						}
						part.Amount = &m
					}
					d.Parts = append(d.Parts, part)
				}
				if len(fields) > 0 {
					return BankLineDone{}, invalid(fields)
				}
				c, err := a.bank.Categorise(ctx, org, journal.Categorisation{Draft: d, Basis: storage.BasisPerson, At: a.deps.Clock.Now()})
				if err != nil {
					return BankLineDone{}, bankProblems("The line cannot be categorised", err)
				}
				e := entryOf(c.Entry)
				return BankLineDone{DocumentID: c.Transaction.ID.String(), Entry: &e}, nil
			})
		})

	register(a, authz.SuspendBankLine, huma.Operation{
		OperationID: "suspendBankLine", Method: http.MethodPost, Path: "/v1/bank-lines/{line_id}/suspend", Tags: []string{"Banking"},
		Summary: "Put a bank line in suspense",
		Description: "Posts a waiting line whose purpose is not known yet to the suspense account (2999), so the books show the money moved " +
			"(FR-BNK-06). It stays in the queue as suspense until it is categorised or matched, which reverses the suspense posting; the " +
			"close warns while suspense is not at nothing. Needs If-Match with the line's ETag. Idempotent with Idempotency-Key; Prefer: " +
			"return=preview saves nothing.",
	}, posting, func(ctx context.Context, in *lineActionInput) (*lineDoneOutput, error) {
		return a.actOnLine(ctx, "Posting", in.LineID, in.IfMatch, in.IdempotencyKey, in.Prefer, in, func(ctx context.Context, u storage.UnitOfWork, org uuid.UUID, l storage.BankLine) (BankLineDone, error) {
			c, err := a.bank.Suspend(ctx, org, l.ID, a.deps.Clock.Now())
			if err != nil {
				return BankLineDone{}, bankProblems("The line cannot go to suspense", err)
			}
			e := entryOf(c.Entry)
			return BankLineDone{DocumentID: c.Transaction.ID.String(), Entry: &e}, nil
		})
	})

	matching := append([]string{journal.CodeDirection, journal.CodeRecorded, journal.CodeOverParts}, posting...)
	matched := func(m journal.Matched) BankLineDone {
		out := BankLineDone{DocumentID: m.Document.ID.String()}
		if m.Entry != nil {
			e := entryOf(*m.Entry)
			out.Entry = &e
		}
		return out
	}
	register(a, authz.MatchBankLineInvoices, huma.Operation{
		OperationID: "matchBankLineInvoices", Method: http.MethodPost, Path: "/v1/bank-lines/{line_id}/match-invoices", Tags: []string{"Banking"},
		Summary: "Match money in to invoices",
		Description: "Records a receipt from a line of money in that settles a customer's invoices (FR-BNK-07, FR-BNK-08): one or many, in " +
			"part or in full within the settlement tolerances, with realised FX at the line's day's rate and what is left over kept as a " +
			"credit. The receipt runs the same checks as one recorded by hand; money recorded by hand already is matched instead " +
			"(BANKTXN-11). Needs If-Match with the line's ETag. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(ReceiptCodes, matching...),
		func(ctx context.Context, in *matchInvoicesInput) (*lineDoneOutput, error) {
			return a.actOnLine(ctx, "Posting", in.LineID, in.IfMatch, in.IdempotencyKey, in.Prefer, in, func(ctx context.Context, u storage.UnitOfWork, org uuid.UUID, l storage.BankLine) (BankLineDone, error) {
				m := journal.InvoiceMatch{LineID: l.ID, Basis: storage.BasisPerson, At: a.deps.Clock.Now()}
				var fields []FieldProblem
				if id, err := uuid.Parse(in.Body.CustomerID); err != nil {
					fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.customer_id", Message: "is not an ID", Fix: "Send the customer's ID"})
				} else {
					m.CustomerID = id
				}
				cur, unapplied, more := matchMoney(l, in.Body.Currency, in.Body.Unapplied)
				fields = append(fields, more...)
				m.Currency, m.Unapplied = cur, unapplied
				for i, x := range in.Body.Invoices {
					at := func(f string) string { return "body.invoices[" + strconv.Itoa(i) + "]." + f }
					id, err := uuid.Parse(x.InvoiceID)
					if err != nil {
						fields = append(fields, FieldProblem{Code: CodeInvalid, Field: at("invoice_id"), Message: "is not an ID", Fix: "Send the invoice's ID"})
					}
					amount, bad := moneyIn(x.Amount, at("amount"))
					if bad != nil {
						fields = append(fields, *bad)
					}
					m.Invoices = append(m.Invoices, receipt.Part{InvoiceID: id, Amount: amount, InFull: x.InFull})
				}
				if len(fields) > 0 {
					return BankLineDone{}, invalid(fields)
				}
				r, err := a.bank.MatchInvoices(ctx, org, m)
				if err != nil {
					return BankLineDone{}, bankProblems("The receipt cannot be recorded from the line", err)
				}
				return matched(r), nil
			})
		})

	register(a, authz.MatchBankLineBills, huma.Operation{
		OperationID: "matchBankLineBills", Method: http.MethodPost, Path: "/v1/bank-lines/{line_id}/match-bills", Tags: []string{"Banking"},
		Summary: "Match money out to bills",
		Description: "Records a bill payment from a line of money out that settles a supplier's bills, as matching invoices records a receipt " +
			"(FR-BNK-07, FR-BNK-08). Needs If-Match with the line's ETag. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(PaymentCodes, matching...),
		func(ctx context.Context, in *matchBillsInput) (*lineDoneOutput, error) {
			return a.actOnLine(ctx, "Posting", in.LineID, in.IfMatch, in.IdempotencyKey, in.Prefer, in, func(ctx context.Context, u storage.UnitOfWork, org uuid.UUID, l storage.BankLine) (BankLineDone, error) {
				m := journal.BillMatch{LineID: l.ID, Basis: storage.BasisPerson, At: a.deps.Clock.Now()}
				var fields []FieldProblem
				if id, err := uuid.Parse(in.Body.SupplierID); err != nil {
					fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.supplier_id", Message: "is not an ID", Fix: "Send the supplier's ID"})
				} else {
					m.SupplierID = id
				}
				cur, unapplied, more := matchMoney(l, in.Body.Currency, in.Body.Unapplied)
				fields = append(fields, more...)
				m.Currency, m.Unapplied = cur, unapplied
				for i, x := range in.Body.Bills {
					at := func(f string) string { return "body.bills[" + strconv.Itoa(i) + "]." + f }
					id, err := uuid.Parse(x.BillID)
					if err != nil {
						fields = append(fields, FieldProblem{Code: CodeInvalid, Field: at("bill_id"), Message: "is not an ID", Fix: "Send the bill's ID"})
					}
					amount, bad := moneyIn(x.Amount, at("amount"))
					if bad != nil {
						fields = append(fields, *bad)
					}
					m.Bills = append(m.Bills, billpayment.Part{BillID: id, Amount: amount, InFull: x.InFull})
				}
				if len(fields) > 0 {
					return BankLineDone{}, invalid(fields)
				}
				r, err := a.bank.MatchBills(ctx, org, m)
				if err != nil {
					return BankLineDone{}, bankProblems("The bill payment cannot be recorded from the line", err)
				}
				return matched(r), nil
			})
		})

	register(a, authz.MatchBankLineRecorded, huma.Operation{
		OperationID: "matchBankLineRecorded", Method: http.MethodPost, Path: "/v1/bank-lines/{line_id}/match-recorded", Tags: []string{"Banking"},
		Summary: "Match a bank line to money recorded by hand",
		Description: "Matches a line to a receipt, bill payment, deposit or refund recorded by hand that moved its amount through its account " +
			"within 7 days (FR-BNK-07). Nothing new posts. Needs If-Match with the line's ETag. Idempotent with Idempotency-Key; Prefer: " +
			"return=preview saves nothing.",
	}, append([]string{journal.CodeNotMoving}, posting...), func(ctx context.Context, in *matchRecordedInput) (*lineDoneOutput, error) {
		return a.actOnLine(ctx, "Posting", in.LineID, in.IfMatch, in.IdempotencyKey, in.Prefer, in, func(ctx context.Context, u storage.UnitOfWork, org uuid.UUID, l storage.BankLine) (BankLineDone, error) {
			doc, err := parseID("body.document_id", in.Body.DocumentID)
			if err != nil {
				return BankLineDone{}, err
			}
			r, err := a.bank.MatchRecorded(ctx, org, l.ID, doc, storage.BasisPerson, a.deps.Clock.Now())
			if err != nil {
				return BankLineDone{}, err
			}
			return matched(r), nil
		})
	})

	register(a, authz.KeepBankLine, huma.Operation{
		OperationID: "keepBankLine", Method: http.MethodPost, Path: "/v1/bank-lines/{line_id}/keep", Tags: []string{"Banking"},
		Summary: "Keep a possible duplicate",
		Description: "Keeps a line flagged as a possible duplicate as a line of its own: it joins the queue (FR-BNK-04, C.2 #51). Posts " +
			"nothing. Needs If-Match with the line's ETag. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append([]string{banking.CodeLineState}, writes...), func(ctx context.Context, in *lineActionInput) (*lineDoneOutput, error) {
		return a.actOnLine(ctx, "", in.LineID, in.IfMatch, in.IdempotencyKey, in.Prefer, in, func(ctx context.Context, u storage.UnitOfWork, org uuid.UUID, l storage.BankLine) (BankLineDone, error) {
			_, err := banking.Keep(ctx, u, l.ID, l.Version, a.deps.Clock.Now())
			return BankLineDone{}, err
		})
	})

	register(a, authz.ExcludeBankLine, huma.Operation{
		OperationID: "excludeBankLine", Method: http.MethodPost, Path: "/v1/bank-lines/{line_id}/exclude", Tags: []string{"Banking"},
		Summary: "Exclude a bank line",
		Description: "Takes a waiting line out of the queue with its reason, posting nothing: a possible duplicate confirmed, or a line of a " +
			"statement imported in error (FR-BNK-04). Undo puts it back while its period is open. Needs If-Match with the line's ETag. " +
			"Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append([]string{banking.CodeLineState, banking.CodeRequest}, writes...), func(ctx context.Context, in *lineReasonInput) (*lineDoneOutput, error) {
		return a.actOnLine(ctx, "", in.LineID, in.IfMatch, in.IdempotencyKey, in.Prefer, in, func(ctx context.Context, u storage.UnitOfWork, org uuid.UUID, l storage.BankLine) (BankLineDone, error) {
			_, err := banking.Exclude(ctx, u, l.ID, l.Version, in.Body.Reason, a.deps.Clock.Now())
			return BankLineDone{}, err
		})
	})

	register(a, authz.UnreconcileBankLine, huma.Operation{
		OperationID: "unreconcileBankLine", Method: http.MethodPost, Path: "/v1/bank-lines/{line_id}/unreconcile", Tags: []string{"Banking"},
		Summary: "Put a bank line back in the queue",
		Description: "Undoes what a line was reconciled to, with a reason, while its period is open (FR-GL-12, FR-BNK-14): a categorisation " +
			"is reversed with the releases it posted, a receipt or bill payment made from the line voided, a match to money recorded by " +
			"hand undone, a transfer undone with its other side, an excluded line included again. A line in suspense is categorised instead " +
			"(BANKTXN-09). Needs If-Match with the line's ETag. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append([]string{banking.CodeLineState, banking.CodeRequest, journal.CodeNotCategory, journal.CodeTransferUndone}, posting...),
		func(ctx context.Context, in *lineReasonInput) (*lineDoneOutput, error) {
			return a.actOnLine(ctx, "Posting", in.LineID, in.IfMatch, in.IdempotencyKey, in.Prefer, in, func(ctx context.Context, u storage.UnitOfWork, org uuid.UUID, l storage.BankLine) (BankLineDone, error) {
				_, err := banking.Unreconcile(ctx, a.bank, org, l.ID, in.Body.Reason, a.deps.Clock.Now(), false)
				return BankLineDone{}, err
			})
		})

	// Each line reconciles in its own unit of work, so the run is not one write to replay: a repeat finds the lines it
	// reconciled no longer waiting.
	register(a, authz.AutoReconcileBankLines, exempt(huma.Operation{
		OperationID: "autoReconcileBankLines", Method: http.MethodPost, Path: "/v1/bank-lines:auto-reconcile", Tags: []string{"Banking"},
		Summary: "Reconcile now",
		Description: "Runs auto-reconcile over the waiting lines, one money account's or every account's, as the system principal and within the " +
			"same limits as after an upload (FR-BNK-14; the owner's decision of 26 Oct 2026, rev 0.143): a transfer's missing side, the one open " +
			"document a line names exactly, and approved auto-apply rules. Lines that arrive by feed in P1 wait for it. Each line posts in its own " +
			"transaction, so one refusal never stops the others; what is left stays in the queue, counted by why. The person who asked is " +
			"recorded as the one it acted for. Prefer: return=preview says what it would do and posts nothing.",
	}, "Idempotency-Key", "Each line reconciles in its own transaction and a repeat finds those lines no longer waiting, so nothing posts twice (FR-BNK-14)."),
		[]string{CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeNotFound, storage.CodeRetry, storage.CodeLockTimeout},
		func(ctx context.Context, in *reconcileNowInput) (*reconcileNowOutput, error) {
			f := storage.BankLineFilter{Statuses: []string{storage.LineUnreconciled}}
			if in.Body.MoneyAccountID != "" {
				id, err := parseID("body.money_account_id", in.Body.MoneyAccountID)
				if err != nil {
					return nil, err
				}
				f.MoneyAccountID = &id
			}
			p, _ := authn.From(ctx)
			now := a.deps.Clock.Now()
			var ids []uuid.UUID
			var planned banking.AutoRun
			err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				if f.MoneyAccountID != nil {
					if _, err := u.Banking().MoneyAccount(ctx, *f.MoneyAccountID); err != nil {
						return err
					}
				}
				lines, err := u.Banking().Lines(ctx, f)
				if err != nil {
					return err
				}
				ids = make([]uuid.UUID, 0, len(lines))
				for _, l := range lines {
					ids = append(ids, l.ID)
				}
				if wantsPreview(in.Prefer) {
					planned, err = banking.Plan(ctx, u, ids, now)
				}
				return err
			})
			if err != nil {
				return nil, err
			}
			out := &reconcileNowOutput{Status: http.StatusOK}
			run := planned
			if wantsPreview(in.Prefer) {
				out.Applied = previewApplied
			} else if run, err = banking.AutoReconcile(ctx, a.deps.Store, p.Organisation, ids, now); err != nil {
				return nil, err
			}
			out.Body = AutoReconciled{Reconciled: len(run.Reconciled), Left: map[string]int{}}
			for _, l := range run.Left {
				out.Body.Left[l.Why]++
			}
			return out, nil
		})

	transferring := append([]string{journal.CodeTransferLine, ledger.CodeTransfer, ledger.CodeNotMoneyAccount}, posting...)
	transferDone := func(ctx context.Context, u storage.UnitOfWork, r journal.Transferred) (BankTransferDone, error) {
		out := BankTransferDone{Transfer: transferOf(r.Transfer), Lines: []BankLine{}}
		if r.Entry != nil {
			e := entryOf(*r.Entry)
			out.Entry = &e
		}
		for _, id := range []*uuid.UUID{r.Transfer.FromLineID, r.Transfer.ToLineID} {
			if id == nil {
				continue
			}
			l, err := u.Banking().Line(ctx, *id)
			if err != nil {
				return out, err
			}
			x, err := lineWithMatch(ctx, u, l)
			if err != nil {
				return out, err
			}
			out.Lines = append(out.Lines, x)
		}
		return out, nil
	}
	register(a, authz.RecordBankTransfer, huma.Operation{
		OperationID: "recordBankTransfer", Method: http.MethodPost, Path: "/v1/bank-transfers", Tags: []string{"Banking"},
		Summary: "Record a transfer between money accounts", DefaultStatus: http.StatusCreated,
		Description: "Records money moved between two of the organisation's money accounts and posts it (FR-BNK-09, bank_transfer@1): each " +
			"leg at its own currency's rate for the day, a reported fee to the bank charges account, and the rest of a conversion to 7030. " +
			"Name the line of one side at least; a side whose line has not arrived waits in transit with its account and amount. Side " +
			"effects: the transfer, its entry, its lines' matches. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, transferring, func(ctx context.Context, in *recordTransferInput) (*transferDoneOutput, error) {
		r := journal.TransferRequest{Basis: storage.BasisPerson, At: a.deps.Clock.Now()}
		var fields []FieldProblem
		for _, x := range []struct {
			text, field string
			to          **uuid.UUID
		}{{in.Body.FromLineID, "body.from_line_id", &r.FromLineID}, {in.Body.ToLineID, "body.to_line_id", &r.ToLineID}} {
			if x.text == "" {
				continue
			}
			id, err := uuid.Parse(x.text)
			if err != nil {
				fields = append(fields, FieldProblem{Code: CodeInvalid, Field: x.field, Message: "is not an ID", Fix: "Send the line's ID"})
			}
			*x.to = &id
		}
		for _, x := range []struct {
			text, field string
			to          *uuid.UUID
		}{{in.Body.FromMoneyAccountID, "body.from_money_account_id", &r.FromAccountID}, {in.Body.ToMoneyAccountID, "body.to_money_account_id", &r.ToAccountID}} {
			if x.text == "" {
				continue
			}
			id, err := uuid.Parse(x.text)
			if err != nil {
				fields = append(fields, FieldProblem{Code: CodeInvalid, Field: x.field, Message: "is not an ID", Fix: "Send the money account's ID"})
			}
			*x.to = id
		}
		for _, x := range []struct {
			in    *Money
			field string
			to    **money.Money
		}{{in.Body.Paid, "body.paid", &r.Paid}, {in.Body.Received, "body.received", &r.Received}, {in.Body.Fee, "body.fee", &r.Fee}} {
			if x.in == nil {
				continue
			}
			m, bad := moneyIn(*x.in, x.field)
			if bad != nil {
				fields = append(fields, *bad)
				continue
			}
			*x.to = &m
		}
		if d, bad := optionalDate("body.date", in.Body.Date); bad != nil {
			fields = append(fields, *bad)
		} else if d != nil {
			r.Date = *d
		}
		if len(fields) > 0 {
			return nil, invalid(fields)
		}
		p, _ := authn.From(ctx)
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (BankTransferDone, error) {
			if err := checkConflicts(ctx, u, p, "Posting"); err != nil {
				return BankTransferDone{}, err
			}
			t, err := a.bank.RecordTransfer(ctx, p.Organisation, r)
			if err != nil {
				return BankTransferDone{}, bankProblems("The transfer cannot be recorded", err)
			}
			return transferDone(ctx, u, t)
		})
		if err != nil {
			return nil, err
		}
		out := &transferDoneOutput{Status: w.Status, Applied: applied(w), Replayed: replayedHeader(w.Replayed), Body: w.Body}
		if !w.Previewed {
			out.ETag, out.Location = etag(w.Body.Transfer.Version), "/v1/bank-transfers/"+w.Body.Transfer.ID
		}
		return out, nil
	})

	register(a, authz.ListBankTransfers, huma.Operation{
		OperationID: "listBankTransfers", Method: http.MethodGet, Path: "/v1/bank-transfers", Tags: []string{"Banking"},
		Summary: "List the transfers in transit",
		Description: "The transfers with a side whose line has not arrived, latest first, a page at a time (FR-BNK-09): an import completes " +
			"one when the line comes, or a person does. Read-only.",
	}, []string{CodeCursor}, func(ctx context.Context, in *transfersInput) (*transfersOutput, error) {
		p, _ := authn.From(ctx)
		out := &transfersOutput{}
		out.Body.Items = []BankTransfer{}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			all, err := u.BankMatching().InTransit(ctx)
			if err != nil {
				return err
			}
			if all, out.Body.NextCursor, err = page(all, func(t storage.BankTransfer) string { return t.ID.String() }, in.PageQuery); err != nil {
				return err
			}
			for _, t := range all {
				out.Body.Items = append(out.Body.Items, transferOf(t))
			}
			return nil
		})
		return out, err
	})

	register(a, authz.GetBankTransfer, huma.Operation{
		OperationID: "getBankTransfer", Method: http.MethodGet, Path: "/v1/bank-transfers/{transfer_id}", Tags: []string{"Banking"},
		Summary: "Get a transfer", Description: "Returns a transfer between money accounts and where it stands, with its ETag. Read-only.",
	}, []string{CodeNotFound}, func(ctx context.Context, in *TransferPath) (*transferOutput, error) {
		id, err := parseID("transfer_id", in.TransferID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out BankTransfer
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			t, err := u.BankMatching().Transfer(ctx, id)
			out = transferOf(t)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &transferOutput{ETag: etag(out.Version), Body: out}, nil
	})

	onTransfer := func(ctx context.Context, idText, ifMatch, key, prefer string, request any,
		act func(ctx context.Context, org uuid.UUID, t storage.BankTransfer) (journal.Transferred, error)) (*transferDoneOutput, error) {
		id, err := parseID("transfer_id", idText)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(ifMatch) == "" {
			return nil, checkIfMatch(ifMatch, "")
		}
		p, _ := authn.From(ctx)
		w, err := write(ctx, a, key, prefer, request, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (BankTransferDone, error) {
			t, err := u.BankMatching().Transfer(ctx, id)
			if err != nil {
				return BankTransferDone{}, err
			}
			if err := checkIfMatch(ifMatch, etag(t.Version)); err != nil {
				return BankTransferDone{}, err
			}
			if err := checkConflicts(ctx, u, p, "Posting"); err != nil {
				return BankTransferDone{}, err
			}
			r, err := act(ctx, p.Organisation, t)
			if err != nil {
				return BankTransferDone{}, err
			}
			return transferDone(ctx, u, r)
		})
		if err != nil {
			return nil, err
		}
		return &transferDoneOutput{Status: w.Status, ETag: etag(w.Body.Transfer.Version), Applied: applied(w), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	}

	register(a, authz.CompleteBankTransfer, huma.Operation{
		OperationID: "completeBankTransfer", Method: http.MethodPost, Path: "/v1/bank-transfers/{transfer_id}/complete", Tags: []string{"Banking"},
		Summary: "Complete a transfer in transit",
		Description: "Matches a transfer in transit to its other side's line, once it arrives: the paying account's money out or the " +
			"receiving account's money in, of the amount recorded (FR-BNK-09). Nothing new posts. Needs If-Match with the transfer's ETag. " +
			"Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append([]string{journal.CodeTransferUndone}, transferring...), func(ctx context.Context, in *completeTransferInput) (*transferDoneOutput, error) {
		return onTransfer(ctx, in.TransferID, in.IfMatch, in.IdempotencyKey, in.Prefer, in, func(ctx context.Context, org uuid.UUID, t storage.BankTransfer) (journal.Transferred, error) {
			line, err := parseID("body.line_id", in.Body.LineID)
			if err != nil {
				return journal.Transferred{}, err
			}
			return a.bank.CompleteTransfer(ctx, org, t.ID, line, storage.BasisPerson, a.deps.Clock.Now())
		})
	})

	register(a, authz.UndoBankTransfer, huma.Operation{
		OperationID: "undoBankTransfer", Method: http.MethodPost, Path: "/v1/bank-transfers/{transfer_id}/undo", Tags: []string{"Banking"},
		Summary: "Undo a transfer",
		Description: "Undoes a transfer with a reason while its period is open: its entry is reversed, its lines go back to the queue, and it " +
			"is marked undone (FR-BNK-09, FR-GL-12). Needs If-Match with the transfer's ETag. Idempotent with Idempotency-Key; Prefer: " +
			"return=preview saves nothing.",
	}, append([]string{journal.CodeTransferUndone}, posting...), func(ctx context.Context, in *undoTransferInput) (*transferDoneOutput, error) {
		return onTransfer(ctx, in.TransferID, in.IfMatch, in.IdempotencyKey, in.Prefer, in, func(ctx context.Context, org uuid.UUID, t storage.BankTransfer) (journal.Transferred, error) {
			return a.bank.UndoTransfer(ctx, org, t.ID, strings.TrimSpace(in.Body.Reason), a.deps.Clock.Now())
		})
	})
}

// matchMoney reads a match's currency (the line's when left out) and what is left over (nil when left out).
func matchMoney(l storage.BankLine, currency string, unapplied *Money) (money.Currency, *money.Money, []FieldProblem) {
	var fields []FieldProblem
	cur, _ := money.Lookup(l.Currency)
	if currency != "" {
		c, err := money.Lookup(currency)
		if err != nil {
			fields = append(fields, FieldProblem{Code: money.CodeCurrency, Field: "body.currency", Message: "is not a currency Fyniq knows", Fix: "Send an ISO 4217 code such as SGD"})
		}
		cur = c
	}
	var left *money.Money
	if unapplied != nil {
		m, bad := moneyIn(*unapplied, "body.unapplied")
		if bad != nil {
			fields = append(fields, *bad)
		} else {
			left = &m
		}
	}
	return cur, left, fields
}
