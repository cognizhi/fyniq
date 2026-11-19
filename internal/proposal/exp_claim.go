package proposal

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/claim"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// exp.draft_claim (PRD-001 Appendix E, FR-DOC-12; p1-ai-proposals task 1.2): a draft expense claim of the requester's receipts, made through the same
// function a person's draft and a captured receipt's draft use (journal.CreateClaimIn). It is tier 1 and the requester's own: the payload has
// no claimant, and the claim is made for the person who asked, so it cannot be made for another person or approved by one (the executor
// refuses when the approver is not the requester). A draft posts nothing, and the claim follows its approval policy when the claimant
// submits it (FR-AP-17). The input-tax facts are the receipt's own, for the approver to confirm (FR-AP-20, G4-029).

// MaxClaimLines is how many receipt lines one draft claim proposal holds.
const MaxClaimLines = 30

// ClaimLine is one receipt line of a draft claim: its category by name, what was bought and what the receipt states.
type ClaimLine struct {
	Date        string `json:"date"`
	Merchant    string `json:"merchant"`
	Category    string `json:"category"`
	Description string `json:"description"`
	Amount      string `json:"amount"`
	GSTCode     string `json:"gst_code"`
	GST         string `json:"gst"`
	Document    string `json:"document"`
}

// ClaimDraftPayload is exp.draft_claim's payload.
type ClaimDraftPayload struct {
	Title    string      `json:"title"`
	Date     string      `json:"date"`
	Currency string      `json:"currency"`
	Lines    []ClaimLine `json:"lines"`
	Notes    string      `json:"notes"`
}

// ClaimDraftOf reads a payload against the records into the draft the claimant would have typed, refusing with the field and the fix.
func ClaimDraftOf(ctx context.Context, u storage.UnitOfWork, d ClaimDraftPayload, claimant uuid.UUID, today time.Time) (claim.Draft, error) {
	out := claim.Draft{ClaimantID: claimant, Title: strings.TrimSpace(d.Title), Notes: d.Notes}
	var err error
	out.Date = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	if d.Date != "" {
		if out.Date, err = time.Parse(time.DateOnly, d.Date); err != nil {
			return out, refusal("payload.date is not a date", "Send the claim's date as YYYY-MM-DD, or leave it out for today")
		}
	}
	cur, err := money.Lookup(d.Currency)
	if err != nil {
		return out, refusal("payload.currency is not a currency Fyniq knows", "Send an ISO 4217 code such as SGD")
	}
	if len(d.Lines) == 0 || len(d.Lines) > MaxClaimLines {
		return out, refusal(fmt.Sprintf("payload.lines holds %d lines", len(d.Lines)), fmt.Sprintf("Send 1 to %d receipt lines", MaxClaimLines))
	}
	categories, err := u.ClaimCategories().List(ctx)
	if err != nil {
		return out, err
	}
	codes, err := bill.PurchaseCodes(ctx, u, out.Date)
	if err != nil {
		return out, err
	}
	for i, l := range d.Lines {
		at := fmt.Sprintf("payload.lines[%d].", i)
		ci := slices.IndexFunc(categories, func(c storage.ClaimCategory) bool {
			return c.Active && !c.Mileage && strings.EqualFold(c.Name, strings.TrimSpace(l.Category))
		})
		if ci < 0 {
			return out, refusal(at+"category is not the name of an active claim category", "Use a category name as the person's claim categories list it; mileage is claimed by the person")
		}
		line := claim.Line{Kind: claim.LineReceipt, Merchant: strings.TrimSpace(l.Merchant), CategoryID: categories[ci].ID, Description: l.Description, GSTCode: l.GSTCode}
		if line.Date, err = time.Parse(time.DateOnly, l.Date); err != nil {
			return out, refusal(at+"date is not a date", "Send the receipt's date as YYYY-MM-DD")
		}
		if line.Amount, err = money.Parse(l.Amount, cur); err != nil {
			return out, refusal(at+"amount is not an amount in "+cur.Code, "Send what the receipt shows was paid as a decimal string such as 12.50")
		}
		if line.GST, err = money.Parse(l.GST, cur); err != nil {
			return out, refusal(at+"gst is not an amount in "+cur.Code, "Send the GST the receipt states as a decimal string, 0.00 for none")
		}
		if len(codes.Codes) > 0 && !slices.ContainsFunc(codes.Codes, func(c pack.GSTCode) bool { return c.Code == l.GSTCode }) {
			return out, refusal(at+"gst_code is not a purchase GST code of the organisation", "Use one of the purchase GST codes, such as TX or NR")
		}
		switch l.Document {
		case "", bill.DocNone:
			line.Document = bill.DocNone // an unknown fact claims no input tax (G4-029)
		case bill.DocTaxInvoice, bill.DocSimplified:
			line.Document = l.Document
		default:
			return out, refusal(at+"document is not tax_invoice, simplified or none", "Say what the receipt states itself to be, or leave it out when you are not sure")
		}
		out.Lines = append(out.Lines, line)
	}
	return out, nil
}

// DraftClaim is exp.draft_claim's command: a draft claim of the requester's receipts. The dry run makes it in the unit of work that is rolled
// back and shows its total and what would stop it being submitted; executed, it refuses unless the requester approved it themselves.
type DraftClaim struct{}

// Policy implements Command: the right to claim one's own expenses.
func (DraftClaim) Policy() authz.Policy { return authz.CreateExpenseClaim }

// Measure implements Command: one draft, no amount (a draft moves no money).
func (DraftClaim) Measure(payload json.RawMessage) (Measure, error) {
	var d ClaimDraftPayload
	if err := json.Unmarshal(payload, &d); err != nil {
		return Measure{}, err
	}
	return Measure{Items: 1}, nil
}

// DryRun implements Command.
func (DraftClaim) DryRun(ctx context.Context, u storage.UnitOfWork, payload json.RawMessage, as Actor) (Impact, error) {
	var d ClaimDraftPayload
	if err := json.Unmarshal(payload, &d); err != nil {
		return Impact{}, err
	}
	draft, err := ClaimDraftOf(ctx, u, d, as.User, as.Today)
	if err != nil {
		return Impact{}, err
	}
	doc, err := journal.CreateClaimIn(ctx, u, draft)
	if err != nil {
		return Impact{}, claimRefusal(err)
	}
	checked, err := journal.CheckClaim(ctx, u, doc)
	if err != nil {
		return Impact{}, err
	}
	lines := make([]map[string]any, 0, len(draft.Lines))
	total := money.Zero(draft.Lines[0].Amount.Currency())
	for _, l := range draft.Lines {
		lines = append(lines, map[string]any{"date": l.Date.Format(time.DateOnly), "merchant": l.Merchant, "description": l.Description, "amount": l.Amount.String(), "gst": l.GST.String()})
		if total, err = total.Add(l.Amount); err != nil {
			return Impact{}, err
		}
	}
	var stops, tax []string
	for _, p := range checked.Problems {
		stops = append(stops, p.Field+": "+p.Message)
	}
	for _, x := range checked.Prepared.InputTax {
		tax = append(tax, x.Text)
	}
	changes, err := json.Marshal(map[string]any{"claims": []map[string]any{{"change": "draft", "title": draft.Title, "lines": lines, "total": total.String(), "input_tax": tax, "stops_submitting": stops}},
		"ledger": "A draft posts nothing: the claim is submitted by its claimant and approved by another person"})
	if err != nil {
		return Impact{}, err
	}
	return Impact{Summary: fmt.Sprintf("Drafts your expense claim %q of %d receipts, %s %s in all; nothing is submitted or posted", draft.Title, len(draft.Lines), total.Currency().Code, total.String()),
		Changes: changes}, nil
}

// Execute implements Command.
func (DraftClaim) Execute(ctx context.Context, u storage.UnitOfWork, payload json.RawMessage, base map[string]int64, as Actor) (Execution, error) {
	if as.Requester != uuid.Nil && as.Requester != as.User {
		return Execution{}, refusal("A claim is the claimant's own: only the person who asked can approve this one", "Ask them to approve it from their Inbox")
	}
	var d ClaimDraftPayload
	if err := json.Unmarshal(payload, &d); err != nil {
		return Execution{}, err
	}
	draft, err := ClaimDraftOf(ctx, u, d, as.User, as.Today)
	if err != nil {
		return Execution{}, err
	}
	doc, err := journal.CreateClaimIn(ctx, u, draft)
	if err != nil {
		return Execution{}, claimRefusal(err)
	}
	return Execution{Records: []string{"expense_claim:" + doc.ID.String()}, Entries: []string{}}, nil
}

// claimRefusal is a claim's own refusal in the proposal's words.
func claimRefusal(err error) error {
	if ce, ok := err.(*claim.Error); ok {
		return refusal(ce.Message, ce.Fix)
	}
	return err
}
