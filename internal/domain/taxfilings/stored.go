package taxfilings

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The stored working paper (FR-TAX-06, p2-tax-filings task 2.3, rev 0.608): the adjustments the tax agent enters for a financial year, with their reasons, kept as a document
// of their own so evidence can attach to it (the evidence table holds files of documents only, FR-DOC-05): drafted, changed while a draft, then recorded once its evidence is
// attached, from when it changes no more and its files are committed to the retention-locked store (FR-DOC-13). A later recorded paper of the same year supersedes it.

// The stored paper's codes.
const (
	CodePaper      = "TAXPAPER-01" // a paper that does not fit: its year, scheme or an adjustment
	CodeNoEvidence = "TAXPAPER-02" // a paper with adjustments recorded without evidence
	CodeRecorded   = "TAXPAPER-03" // a paper recorded already changes no more
)

// The longest texts and the most adjustments a paper keeps.
const (
	MaxReason      = 500
	MaxAdjustments = 100
)

// Error is a refusal in plain words (ENG-23).
type Error struct{ Code, Message, Fix string }

func (e *Error) Error() string { return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Fix) }

// Stored is a financial year's paper as a person saved it.
type Stored struct {
	Date              time.Time // the day it was drafted
	FinancialYearEnd  time.Time
	Scheme            string
	CashGrantEligible bool
	Adjustments       []Adjustment
	Figures           json.RawMessage // what the paper worked out when it was recorded; empty while a draft
}

type adjustmentPayload struct {
	Kind   string `json:"kind"`
	Amount string `json:"amount"`
	Reason string `json:"reason"`
}

type storedPayload struct {
	Date              string              `json:"date"`
	FinancialYearEnd  string              `json:"financial_year_end"`
	Scheme            string              `json:"scheme"`
	CashGrantEligible bool                `json:"cash_grant_eligible,omitempty"`
	Adjustments       []adjustmentPayload `json:"adjustments"`
	Figures           json.RawMessage     `json:"figures,omitempty"`
}

// Encode is a paper as a document version keeps it.
func Encode(s Stored) ([]byte, error) {
	p := storedPayload{Date: s.Date.Format(time.DateOnly), FinancialYearEnd: s.FinancialYearEnd.Format(time.DateOnly), Scheme: s.Scheme, CashGrantEligible: s.CashGrantEligible,
		Adjustments: []adjustmentPayload{}, Figures: s.Figures}
	for _, a := range s.Adjustments {
		p.Adjustments = append(p.Adjustments, adjustmentPayload{a.Kind, a.Amount, a.Reason})
	}
	return json.Marshal(p)
}

// Read reads a paper's version as it was saved.
func Read(doc storage.Document) (Stored, error) {
	var p storedPayload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return Stored{}, err
	}
	out := Stored{Scheme: p.Scheme, CashGrantEligible: p.CashGrantEligible, Figures: p.Figures}
	var err error
	if out.FinancialYearEnd, err = time.Parse(time.DateOnly, p.FinancialYearEnd); err != nil {
		return out, err
	}
	if out.Date, err = time.Parse(time.DateOnly, p.Date); err != nil {
		return out, err
	}
	for _, a := range p.Adjustments {
		out.Adjustments = append(out.Adjustments, Adjustment{Kind: a.Kind, Amount: a.Amount, Reason: a.Reason})
	}
	return out, nil
}

// Check is what is wrong with a paper on its own, every problem at once (ENG-22): no year end, a scheme that is not one of the two, more than 100 adjustments, and for each one an
// unknown kind, an amount that is not a decimal in dollars with at most two decimals, or a reason blank or too long.
func Check(s Stored) ledger.Problems {
	var probs ledger.Problems
	add := func(field, msg, fix string) {
		probs = append(probs, ledger.Problem{Code: CodePaper, Field: field, Message: msg, Fix: fix})
	}
	if s.FinancialYearEnd.IsZero() {
		add("financial_year_end", "the financial year's last day is missing", "Enter the last day of the financial year, such as 2026-12-31")
	}
	if s.Scheme != pack.SchemeStartUp && s.Scheme != pack.SchemePartial {
		add("scheme", "the scheme is not the start-up exemption (sute) or the partial exemption (pte)", "Choose sute or pte")
	}
	if len(s.Adjustments) > MaxAdjustments {
		add("adjustments", fmt.Sprintf("there are %d adjustments; the most is %d", len(s.Adjustments), MaxAdjustments), "Combine adjustments of one kind")
	}
	for i, a := range s.Adjustments {
		f := fmt.Sprintf("adjustments[%d]", i)
		if !slices.Contains([]string{NonDeductible, CapitalAllowance, OtherAdjustment}, a.Kind) {
			add(f+".kind", fmt.Sprintf("%q is not a kind of adjustment", a.Kind), "Choose non_deductible, capital_allowance_difference or other")
		}
		if d, _, err := apd.NewFromString(strings.TrimSpace(a.Amount)); err != nil || d.Form != apd.Finite || d.Exponent < -2 {
			add(f+".amount", fmt.Sprintf("%q is not an amount in Singapore dollars with at most two decimals", a.Amount), "Enter it as 15000.00; negative takes off")
		}
		switch n := utf8.RuneCountInString(strings.TrimSpace(a.Reason)); {
		case n == 0:
			add(f+".reason", "an adjustment says why: the tax agent signs the working", "Say what it is, such as client entertainment")
		case n > MaxReason:
			add(f+".reason", fmt.Sprintf("the reason is longer than %d characters", MaxReason), "Shorten it; attach the detail as evidence")
		}
	}
	if len(probs) > 0 {
		return probs
	}
	return nil
}

func tidy(s Stored) Stored {
	for i := range s.Adjustments {
		s.Adjustments[i].Amount = strings.TrimSpace(s.Adjustments[i].Amount)
		s.Adjustments[i].Reason = strings.TrimSpace(s.Adjustments[i].Reason)
		s.Adjustments[i].Evidence = false
	}
	return s
}

// DraftIn saves a paper to record in u: a draft, to attach its evidence to.
func DraftIn(ctx context.Context, u storage.UnitOfWork, s Stored, today time.Time) (storage.Document, error) {
	s = tidy(s)
	s.Date, s.Figures = today, nil
	if probs := Check(s); probs != nil {
		return storage.Document{}, probs
	}
	b, err := Encode(s)
	if err != nil {
		return storage.Document{}, err
	}
	return u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentTaxWorkingPaper, Payload: b})
}

// Drafted reads a paper not recorded yet, its row locked: not found for any other document.
func Drafted(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (storage.Document, error) {
	doc, err := u.Documents().Lock(ctx, id)
	if err != nil {
		return doc, err
	}
	if doc.Type != storage.DocumentTaxWorkingPaper {
		return doc, ports.ErrNotFound
	}
	if doc.Status != storage.DocumentDraft {
		return doc, &Error{CodeRecorded, "the paper is recorded, so it changes no more", "Draft a new paper for the year; the latest recorded is the one in force"}
	}
	return doc, nil
}

// ReviseIn saves a new version of a paper not recorded yet, in u; the day it was drafted stays.
func ReviseIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, s Stored) (storage.Document, error) {
	doc, err := Drafted(ctx, u, id)
	if err != nil {
		return doc, err
	}
	was, err := Read(doc)
	if err != nil {
		return doc, err
	}
	s = tidy(s)
	s.Date, s.Figures = was.Date, nil
	if probs := Check(s); probs != nil {
		return doc, probs
	}
	b, err := Encode(s)
	if err != nil {
		return doc, err
	}
	return u.Documents().Revise(ctx, id, b)
}

// RecordIn records a drafted paper in u: checked again, with evidence attached when it has adjustments, the figures it worked out kept with it as it was signed. From then on it
// changes no more.
func RecordIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, figures json.RawMessage) (storage.Document, error) {
	doc, err := Drafted(ctx, u, id)
	if err != nil {
		return doc, err
	}
	s, err := Read(doc)
	if err != nil {
		return doc, err
	}
	if probs := Check(s); probs != nil {
		return doc, probs
	}
	if len(s.Adjustments) > 0 {
		n, err := attached(ctx, u, id)
		if err != nil {
			return doc, err
		}
		if n == 0 {
			return doc, &Error{CodeNoEvidence, "the paper has adjustments and no evidence attached",
				"Attach what supports them, such as the invoices of the non-deductible expenses or the capital allowance schedule, then record it"}
		}
	}
	s.Figures = figures
	b, err := Encode(s)
	if err != nil {
		return doc, err
	}
	if _, err := u.Documents().Revise(ctx, id, b); err != nil {
		return doc, err
	}
	if err := u.Documents().SetStatus(ctx, id, storage.DocumentPosted); err != nil {
		return doc, err
	}
	return u.Documents().Get(ctx, id)
}

// attached counts the files attached to a document and not removed.
func attached(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (int, error) {
	n, after := 0, uuid.Nil
	for {
		page, err := u.Evidence().OfDocument(ctx, id, after, 200)
		if err != nil {
			return 0, err
		}
		for _, e := range page {
			if e.State != storage.EvidenceRemoved {
				n++
			}
			after = e.ID
		}
		if len(page) < 200 {
			return n, nil
		}
	}
}

// Recorded is a stored paper with its document.
type Recorded struct {
	Doc storage.Document
	Stored
}

// Papers lists the papers of a financial year (every year when end is the zero time), the drafts and the recorded ones, the latest recorded first and then the drafts.
func Papers(ctx context.Context, u storage.UnitOfWork, end time.Time) ([]Recorded, error) {
	var recorded, drafts []Recorded
	for _, status := range []storage.DocumentStatus{storage.DocumentPosted, storage.DocumentDraft} {
		f := storage.DocumentFilter{Type: storage.DocumentTaxWorkingPaper, Status: status, Limit: 200}
		for {
			page, err := u.Documents().Page(ctx, f)
			if err != nil {
				return nil, err
			}
			for _, doc := range page {
				x, err := Read(doc)
				if err != nil {
					return nil, err
				}
				if !end.IsZero() && !x.FinancialYearEnd.Equal(end) {
					continue
				}
				if status == storage.DocumentPosted {
					recorded = append(recorded, Recorded{doc, x})
				} else {
					drafts = append(drafts, Recorded{doc, x})
				}
			}
			if len(page) < f.Limit {
				break
			}
			last := page[len(page)-1]
			f.AfterDate, f.AfterID = last.Date, &last.ID
		}
	}
	at := func(r Recorded) time.Time {
		if r.Doc.PostedAt != nil {
			return *r.Doc.PostedAt
		}
		return r.Date
	}
	slices.SortStableFunc(recorded, func(a, b Recorded) int { return at(b).Compare(at(a)) })
	return append(recorded, drafts...), nil
}
