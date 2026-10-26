package claim

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The expense policy's refusals at submission (✖, ENG-21; FR-AP-15). CLAIM-20 to CLAIM-23 are a category's.
const (
	CodeReceiptRequired = "CLAIM-24" // a line above its category's receipt threshold with no receipt attached
	CodeLateReason      = "CLAIM-25" // a line past the claim deadline, and the claim does not say why
	CodeDuplicate       = "CLAIM-26" // a receipt whose file is on another record, and the line does not say why it is not a duplicate
	CodeReceiptFile     = "CLAIM-27" // a line names a receipt file that is not attached to its claim
)

// The expense policy's warnings (⚠, ENG-21): the claim posts, and its approvers see them.
const (
	WarnAttendees = "CLAIM-ATTENDEES"     // a line above its category's attendee threshold names no one
	WarnLate      = "CLAIM-PAST-DEADLINE" // a line older than the claim deadline: finance approves too
	WarnDuplicate = "CLAIM-DUPLICATE"     // a receipt whose file is on another record, with the reason given
	WarnLookalike = "CLAIM-LOOKALIKE"     // another claim has a receipt of the same date, merchant and amount
)

// DeadlineSetting is the setting that holds the claim deadline in days (FR-AP-15).
const DeadlineSetting = "claims.deadline_days"

// Outcome is what the expense policy makes of a claim as it would be submitted on a day: its warnings, the problems
// that stop its submission, and whether finance approves it too (FR-AP-17: a line above its limit, or past the deadline).
type Outcome struct {
	Warnings []Warning
	Problems ledger.Problems
	Finance  bool
}

// Policy runs the expense policy over a claim prepared from doc as it would be submitted on today (FR-AP-15; design
// D6): each line's date against the claim deadline in force, a receipt line's receipt and attendees against its
// category's thresholds, its receipt's file against the organisation's other records (UC-EXP-01), and look-alike
// receipts on other claims. A line above its category's limit is the preparation's (Prepared.OverLimit).
func Policy(ctx context.Context, u storage.UnitOfWork, doc storage.Document, p Prepared, base money.Currency, today time.Time) (Outcome, error) {
	out := Outcome{Finance: p.OverLimit}
	add := func(code, field, msg, fix string) {
		out.Problems = append(out.Problems, ledger.Problem{Code: code, Field: field, Message: msg, Fix: fix})
	}
	warn := func(code, field, msg, fix string) {
		out.Warnings = append(out.Warnings, Warning{code, field, msg, fix})
	}
	days, err := Deadline(ctx, u, today)
	if err != nil {
		return Outcome{}, err
	}
	d := p.Draft
	late := false
	for i, l := range d.Lines {
		f := func(name string) string { return fmt.Sprintf("lines[%d].%s", i+1, name) }
		if !l.Date.IsZero() && l.Date.AddDate(0, 0, days).Before(today) {
			late = true
			warn(WarnLate, f("date"), fmt.Sprintf("it is dated %s, more than the claim deadline of %d days before %s", day(l.Date), days, day(today)),
				"It can still be claimed: say why it is late, and finance approves it too")
		}
		if l.Kind == LineMileage || i >= len(p.Categories) || i >= len(p.Claim.Lines) {
			continue
		}
		cat := p.Categories[i]
		owed, ok := lineOwed(p.Claim.Lines[i], base)
		above := func(threshold *storage.Decimal) bool {
			if threshold == nil || !ok {
				return false
			}
			t, err := money.ParseStored(string(*threshold), base)
			over, cmpErr := owed.Cmp(t)
			return err == nil && cmpErr == nil && over > 0
		}
		var receipt *storage.Evidence
		if l.ReceiptID != nil {
			e, err := u.Evidence().Get(ctx, *l.ReceiptID)
			switch {
			case errors.Is(err, ports.ErrNotFound) || err == nil && (e.DocumentID != doc.ID || e.State == storage.EvidenceRemoved):
				add(CodeReceiptFile, f("receipt_id"), "the receipt file is not attached to this claim", "Attach the receipt to the claim, then pick it on the line")
			case err != nil:
				return Outcome{}, err
			default:
				receipt = &e
			}
		}
		if receipt == nil && l.ReceiptID == nil && above(cat.ReceiptAbove) {
			limit, _ := money.ParseStored(string(*cat.ReceiptAbove), base)
			add(CodeReceiptRequired, f("receipt_id"), fmt.Sprintf("a %s line above %s %s needs its receipt", cat.Name, limit, base),
				"Attach the receipt: paste it, drop it, or forward it to your capture address")
		}
		if strings.TrimSpace(l.Attendees) == "" && above(cat.AttendeesAbove) {
			limit, _ := money.ParseStored(string(*cat.AttendeesAbove), base)
			warn(WarnAttendees, f("attendees"), fmt.Sprintf("a %s line above %s %s names who attended", cat.Name, limit, base),
				"Name who attended; the approver may return the claim to ask")
		}
		if receipt != nil {
			if err := duplicates(ctx, u, doc, *receipt, l, f, add, warn); err != nil {
				return Outcome{}, err
			}
		}
		if err := lookalikes(ctx, u, doc, l, base, days, f, warn); err != nil {
			return Outcome{}, err
		}
	}
	if late {
		out.Finance = true
		if strings.TrimSpace(d.LateReason) == "" {
			add(CodeLateReason, "late_reason", fmt.Sprintf("a line is past the claim deadline of %d days, and the claim does not say why", days),
				"Say why it is claimed late")
		}
	}
	return out, nil
}

// Deadline is the claim deadline in days in force on a day (the setting claims.deadline_days, FR-AP-15).
func Deadline(ctx context.Context, u storage.UnitOfWork, on time.Time) (int, error) {
	changes, err := u.Settings().Changes(ctx, DeadlineSetting)
	if err != nil {
		return 0, err
	}
	def, _ := settings.Lookup(DeadlineSetting)
	return strconv.Atoi(settings.On(def, changes, on).Value)
}

// WarnAdvanceOpen is an advance still open past the claim deadline after its date (⚠, FR-AP-17, design D8; the check
// line C.2 #39 arrives with p1-checks).
const WarnAdvanceOpen = "CLAIM-ADVANCE-OPEN"

// AdvanceWarnings are an advance's warnings on today: something still open on it more than the claim deadline after
// the day it was paid.
func AdvanceWarnings(ctx context.Context, u storage.UnitOfWork, a Advance, b AdvanceBalance, today time.Time) ([]Warning, error) {
	if a.Refunds != nil || b.Open.Sign() <= 0 || a.Date.IsZero() {
		return nil, nil
	}
	days, err := Deadline(ctx, u, today)
	if err != nil {
		return nil, err
	}
	if !a.Date.AddDate(0, 0, days).Before(today) {
		return nil, nil
	}
	return []Warning{{WarnAdvanceOpen, "open", fmt.Sprintf("%s %s is still open on an advance paid on %s, more than the claim deadline of %d days ago",
		b.Open, b.Open.Currency(), day(a.Date), days), "Ask for the claims that settle it, or for what is left to be refunded"}}, nil
}

// lineOwed is what the company owes for a receipt line in the base currency: its amount, what was paid for it, or its
// amount at its rate; false when it cannot tell.
func lineOwed(l ledger.ClaimLine, base money.Currency) (money.Money, bool) {
	switch {
	case l.Amount.Currency() == base:
		return l.Amount, true
	case l.Paid != nil && l.Paid.Currency() == base:
		return *l.Paid, true
	case l.Rate != nil:
		x, err := rounding.FX(l.Amount, *l.Rate)
		return x, err == nil
	}
	return money.Money{}, false
}

// duplicates finds the receipt's file on the organisation's other records (FR-AP-15, UC-EXP-01): any one blocks the
// claim until the line says why it is not a duplicate, and shows either way.
func duplicates(ctx context.Context, u storage.UnitOfWork, doc storage.Document, receipt storage.Evidence, l Line, f func(string) string,
	add, warn func(code, field, msg, fix string)) error {
	same, err := u.Evidence().ByHash(ctx, receipt.OriginalSHA256)
	if err != nil {
		return err
	}
	var on []string
	for _, e := range same {
		if e.DocumentID == doc.ID {
			continue
		}
		other, err := u.Documents().Get(ctx, e.DocumentID)
		if err != nil && !errors.Is(err, ports.ErrNotFound) {
			return err
		}
		on = append(on, strings.TrimSpace(strings.ReplaceAll(string(other.Type), "_", " ")+" "+other.Number))
	}
	if len(on) == 0 {
		return nil
	}
	if strings.TrimSpace(l.DuplicateReason) == "" {
		add(CodeDuplicate, f("duplicate_reason"), "the same receipt file is on "+strings.Join(on, ", "),
			"Say why this is not a duplicate, or remove the line")
		return nil
	}
	warn(WarnDuplicate, f("receipt_id"), "the same receipt file is on "+strings.Join(on, ", ")+": "+l.DuplicateReason,
		"Check the reason before approving")
	return nil
}

// lookalikes finds receipts of the same date, merchant and amount on the organisation's other claims dated within the
// claim deadline of it (UC-EXP-01): they show, and block nothing.
func lookalikes(ctx context.Context, u storage.UnitOfWork, doc storage.Document, l Line, base money.Currency, days int, f func(string) string,
	warn func(code, field, msg, fix string)) error {
	merchant := strings.ToLower(strings.TrimSpace(l.Merchant))
	if l.Date.IsZero() || merchant == "" {
		return nil
	}
	var afterDate string
	var afterID *uuid.UUID
	for {
		docs, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: storage.DocumentExpenseClaim, From: day(l.Date),
			To: day(l.Date.AddDate(0, 0, days+1)), AfterDate: afterDate, AfterID: afterID, Limit: 200})
		if err != nil {
			return err
		}
		for _, other := range docs {
			afterDate, afterID = other.Date, &other.ID
			if other.ID == doc.ID || other.Status == storage.DocumentReversed {
				continue
			}
			o, err := Read(other, base)
			if err != nil {
				continue
			}
			for _, x := range o.Lines {
				if x.Kind == LineReceipt && x.Date.Equal(l.Date) && strings.ToLower(strings.TrimSpace(x.Merchant)) == merchant && x.Amount.Equal(l.Amount) {
					warn(WarnLookalike, f("merchant"), fmt.Sprintf("%s has a receipt of %s from %s for %s %s too", cmpOr(other.Number, "another claim"),
						day(l.Date), l.Merchant, l.Amount, l.Amount.Currency()), "Check it is not claimed twice")
					return nil
				}
			}
		}
		if len(docs) < 200 {
			return nil
		}
	}
}
