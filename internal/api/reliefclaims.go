package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/reliefclaim"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ claims of GST bad debt relief (FR-AR-11; p2-tax-filings task 3.2)

// BadDebtReliefClaimRequest asks to claim GST bad debt relief for a write-off.
type BadDebtReliefClaimRequest struct {
	WriteOffID  string `json:"write_off_id" format:"uuid" doc:"The posted bad-debt write-off to claim relief for; each is claimed once." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c8a01"`
	Date        string `json:"date" format:"date" doc:"The day it is claimed: within the pack's dates (twelve months from the supply, five years at most) and not after today." example:"2027-03-10"`
	F5PeriodEnd string `json:"f5_period_end,omitempty" format:"date" doc:"The last day of the GST return period the claim goes in; on or after the claim's date." example:"2027-03-31"`
	Note        string `json:"note,omitempty" maxLength:"1000" doc:"The steps taken to recover the debt, or the debtor's insolvency, in plain words; files attach to the claim as evidence." example:"Three reminders and a letter of demand"`
}

// BadDebtReliefClaim is a claim of GST bad debt relief.
type BadDebtReliefClaim struct {
	ID           string  `json:"id" format:"uuid" doc:"The claim's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c8b01"`
	Status       string  `json:"status" enum:"posted,reversed" doc:"posted, or reversed by a void." example:"posted"`
	Version      int     `json:"version" doc:"The version of its content (always 1: a claim is never edited); with the status it makes the ETag." example:"1"`
	WriteOffID   string  `json:"write_off_id" format:"uuid" doc:"The write-off claimed for." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c8a01"`
	InvoiceID    string  `json:"invoice_id" format:"uuid" doc:"Its invoice." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c8a02"`
	Date         string  `json:"date" format:"date" doc:"The day it was claimed." example:"2027-03-10"`
	F5PeriodEnd  string  `json:"f5_period_end,omitempty" format:"date" doc:"The last day of the F5 period it goes in." example:"2027-03-31"`
	Note         string  `json:"note,omitempty" doc:"The note it was made with." example:"Three reminders and a letter of demand"`
	Written      Money   `json:"written_off" doc:"What was written off, in the invoice's currency."`
	GST          Money   `json:"gst" doc:"The GST claimed, in the base currency: RND-BDR of the invoice's output tax in the part written off [accountant to confirm]."`
	Pack         string  `json:"pack" doc:"The sg-tax-filings version whose dates allowed it." example:"sg-tax-filings@2026.1"`
	EligibleFrom string  `json:"eligible_from" format:"date" doc:"The first day a claim could be made." example:"2027-01-15"`
	ClaimBy      string  `json:"claim_by" format:"date" doc:"The last day to claim." example:"2031-01-14"`
	PostedBy     *Person `json:"posted_by,omitempty" doc:"Who claimed it."`
}

// BadDebtReliefClaimPosting is a claim with the entry it posted or reversed.
type BadDebtReliefClaimPosting struct {
	Claim BadDebtReliefClaim `json:"claim" doc:"The claim after the command."`
	Entry Entry              `json:"entry" doc:"The entry the claim posted, or the reversing entry a void posted."`
}

// BadDebtReliefClaimPath names a claim in the path.
type BadDebtReliefClaimPath struct {
	ClaimID string `path:"claim_id" format:"uuid" doc:"The claim's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c8b01"`
}

type (
	claimReliefInput struct {
		KeyHeader
		PreferHeader
		Body BadDebtReliefClaimRequest
	}
	claimReliefOutput struct {
		Status   int
		ETag     string `header:"ETag" doc:"The claim's version and status, for If-Match."`
		Location string `header:"Location" doc:"The claim's URL."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved, and the entry shows what the claim would post (FR-INT-14)."`
		Body     BadDebtReliefClaimPosting
	}
	getReliefClaimOutput struct {
		ETag string `header:"ETag" doc:"The claim's version and status, for If-Match."`
		Body BadDebtReliefClaim
	}
	voidReliefClaimInput struct {
		BadDebtReliefClaimPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body Reversal
	}
)

func reliefClaimETag(c BadDebtReliefClaim) string { return etag(c.Version, c.Status) }

func reliefClaimOf(doc storage.Document) (BadDebtReliefClaim, error) {
	c, err := reliefclaim.Read(doc)
	if err != nil {
		return BadDebtReliefClaim{}, err
	}
	out := BadDebtReliefClaim{ID: doc.ID.String(), Status: string(doc.Status), Version: doc.Version, WriteOffID: c.WriteOffID.String(), InvoiceID: c.InvoiceID.String(),
		Date: dateOf(c.Date), Note: c.Note, Written: moneyOut(c.Written), GST: moneyOut(c.GST), Pack: c.Pack, EligibleFrom: dateOf(c.EligibleFrom), ClaimBy: dateOf(c.ClaimBy),
		PostedBy: personOf(doc.PostedBy)}
	if !c.F5PeriodEnd.IsZero() {
		out.F5PeriodEnd = dateOf(c.F5PeriodEnd)
	}
	return out, nil
}

// claimsByWriteOff is the live claims, by the write-off each is for.
func claimsByWriteOff(ctx context.Context, u storage.UnitOfWork) (map[string]storage.Document, error) {
	out := map[string]storage.Document{}
	filter := storage.DocumentFilter{Type: storage.DocumentBadDebtRelief, Status: storage.DocumentPosted, Limit: 200}
	for {
		page, err := u.Documents().Page(ctx, filter)
		if err != nil {
			return nil, err
		}
		for _, doc := range page {
			c, err := reliefclaim.Read(doc)
			if err != nil {
				return nil, err
			}
			out[c.WriteOffID.String()] = doc
		}
		if len(page) < filter.Limit {
			return out, nil
		}
		last := page[len(page)-1]
		filter.AfterDate, filter.AfterID = last.Date, &last.ID
	}
}

func reliefRequestOf(in BadDebtReliefClaimRequest) (reliefclaim.Request, error) {
	id, err := parseID("body.write_off_id", in.WriteOffID)
	if err != nil {
		return reliefclaim.Request{}, err
	}
	date, err := time.Parse(time.DateOnly, in.Date)
	if err != nil {
		return reliefclaim.Request{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.date", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"}})
	}
	r := reliefclaim.Request{WriteOffID: id, Date: date, Note: in.Note}
	if in.F5PeriodEnd != "" {
		if r.F5PeriodEnd, err = time.Parse(time.DateOnly, in.F5PeriodEnd); err != nil {
			return reliefclaim.Request{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.f5_period_end", Message: "is not a date", Fix: "Send the period's last day as YYYY-MM-DD"}})
		}
	}
	return r, nil
}

func registerReliefClaims(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeNotFound, CodeStale,
		CodeIfMatchRequired, storage.CodeDuplicate, storage.CodePeriodClosed, storage.CodeNoPeriod, storage.CodeInvalid}, locks...)
	claimCodes := append([]string{reliefclaim.CodeWriteOff, reliefclaim.CodeClaimed, reliefclaim.CodeCredited, reliefclaim.CodeNoPack, reliefclaim.CodeNotYet, reliefclaim.CodeExpired,
		reliefclaim.CodeFuture, reliefclaim.CodePeriod, reliefclaim.CodeNote, ledger.CodeNoRelief, ledger.CodePaymentAmount, ledger.CodeSystemRole, ledger.CodeRuleAccount}, LedgerCodes...)

	register(a, authz.ClaimBadDebtRelief, huma.Operation{
		OperationID: "claimBadDebtRelief", Method: http.MethodPost, Path: "/v1/tax/bad-debt-relief/claims", Tags: []string{"Compliance"}, DefaultStatus: http.StatusCreated,
		Summary: "Claim GST bad debt relief for a write-off",
		Description: "Claims the GST bad debt relief of a posted bad-debt write-off (FR-AR-11) and posts it at once: Dr the GST control account, Cr the account the write-off took " +
			"the debt to, in the base currency, for the invoice's output tax in the part written off (RND-BDR, half away from zero, capped at what earlier claims of the invoice " +
			"left [accountant to confirm]; the posting rule bad_debt_relief@1). Refused unless the date is within the sg-tax-filings pack's dates and not after today, the write-off " +
			"is a posted bad debt and not claimed already, and no credit note credits its invoice. The conditions of the claim that are not dates (reasonable steps to recover the debt, " +
			"open market value) are the claimer's declaration: say how in note, and attach files as evidence. Side effects: the claim, its entry, a new ledger revision, balances and " +
			"audit events. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append(claimCodes, writes...), func(ctx context.Context, in *claimReliefInput) (*claimReliefOutput, error) {
		req, err := reliefRequestOf(in.Body)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (BadDebtReliefClaimPosting, error) {
			if err := checkConflicts(ctx, u, p, "Posting"); err != nil {
				return BadDebtReliefClaimPosting{}, err
			}
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return BadDebtReliefClaimPosting{}, err
			}
			r, err := a.reliefClaims.Claim(ctx, p.Organisation, req, today)
			if err != nil {
				return BadDebtReliefClaimPosting{}, reliefProblems(err)
			}
			c, err := reliefClaimOf(r.Claim)
			return BadDebtReliefClaimPosting{Claim: c, Entry: entryOf(r.Entry)}, err
		})
		if err != nil {
			return nil, err
		}
		if w.Previewed {
			return &claimReliefOutput{Status: http.StatusOK, Applied: applied(w), Body: w.Body}, nil
		}
		return &claimReliefOutput{Status: w.Status, ETag: reliefClaimETag(w.Body.Claim), Location: "/v1/tax/bad-debt-relief/claims/" + w.Body.Claim.ID,
			Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})

	register(a, authz.GetBadDebtReliefClaim, huma.Operation{
		OperationID: "getBadDebtReliefClaim", Method: http.MethodGet, Path: "/v1/tax/bad-debt-relief/claims/{claim_id}", Tags: []string{"Compliance"},
		Summary:     "Get a claim of GST bad debt relief",
		Description: "Returns a claim with its ETag: the write-off, the GST claimed, the F5 period, the dates it was allowed by and its status (FR-AR-11). Read-only; no side effects.",
	}, []string{CodeNotFound, reliefclaim.CodeNotClaim, reliefclaim.CodeBadClaim}, func(ctx context.Context, in *BadDebtReliefClaimPath) (*getReliefClaimOutput, error) {
		id, err := parseID("claim_id", in.ClaimID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out BadDebtReliefClaim
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := u.Documents().Get(ctx, id)
			if err != nil {
				return err
			}
			if out, err = reliefClaimOf(doc); err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		return &getReliefClaimOutput{ETag: reliefClaimETag(out), Body: out}, nil
	})

	register(a, authz.VoidBadDebtReliefClaim, huma.Operation{
		OperationID: "voidBadDebtReliefClaim", Method: http.MethodPost, Path: "/v1/tax/bad-debt-relief/claims/{claim_id}/void", Tags: []string{"Compliance"},
		Summary: "Void a claim of GST bad debt relief",
		Description: "Voids a posted claim with a reversing entry on the date given, with a reason (FR-GL-12); posted history never changes. The write-off can then be claimed again " +
			"or voided. Side effects: the reversing entry, a new ledger revision, balances and audit events with the reason. Needs If-Match with the claim's ETag. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview voids nothing.",
	}, append([]string{reliefclaim.CodeNotClaim, reliefclaim.CodeBadClaim, reliefclaim.CodeNotPosted, reliefclaim.CodeVoidDate}, append(writes, LedgerCodes...)...),
		func(ctx context.Context, in *voidReliefClaimInput) (*claimReliefOutput, error) {
			id, err := parseID("claim_id", in.ClaimID)
			if err != nil {
				return nil, err
			}
			date, err := time.Parse(time.DateOnly, in.Body.Date)
			if err != nil {
				return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.date", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"}})
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			p, _ := authn.From(ctx)
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (BadDebtReliefClaimPosting, error) {
				doc, err := u.Documents().Lock(ctx, id)
				if err != nil {
					return BadDebtReliefClaimPosting{}, err
				}
				cur, err := reliefClaimOf(doc)
				if err != nil {
					return BadDebtReliefClaimPosting{}, err
				}
				if err := checkIfMatch(in.IfMatch, reliefClaimETag(cur)); err != nil {
					return BadDebtReliefClaimPosting{}, err
				}
				if err := checkConflicts(ctx, u, p, "Posting"); err != nil {
					return BadDebtReliefClaimPosting{}, err
				}
				r, err := a.reliefClaims.Void(ctx, p.Organisation, id, date, strings.TrimSpace(in.Body.Reason))
				if err != nil {
					return BadDebtReliefClaimPosting{}, reliefProblems(err)
				}
				c, err := reliefClaimOf(r.Claim)
				return BadDebtReliefClaimPosting{Claim: c, Entry: entryOf(r.Entry)}, err
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &claimReliefOutput{Status: http.StatusOK, Applied: applied(w), Body: w.Body}, nil
			}
			return &claimReliefOutput{Status: http.StatusOK, ETag: reliefClaimETag(w.Body.Claim), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})
}

// reliefProblems turns the posting rule's problems into the API's, naming the claim.
func reliefProblems(err error) error {
	var lp ledger.Problems
	if !errors.As(err, &lp) || len(lp) == 0 {
		return err
	}
	fields := make([]FieldProblem, 0, len(lp))
	for _, x := range lp {
		fields = append(fields, FieldProblem{Code: x.Code, Field: x.Field, Message: x.Message, Fix: x.Fix})
	}
	return problem(lp[0].Code, fmt.Sprintf("The claim cannot post: %d problem(s), each listed in errors.", len(lp)), "Correct the claim and ask again.", fields...)
}
