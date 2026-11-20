package sharepay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// More stable codes (ENG-23).
const (
	CodeAccounts = "SBP-03" // the expense or reserve account is not one the journal can post to
	CodeMonth    = "SBP-04" // the month is not one the grant's expense runs over, or has not begun
	CodeMonthRun = "SBP-05" // the month's expense was proposed already
	CodeArchived = "SBP-06" // the grant is archived
)

// Grant is a grant of shares for services (FR-EQ-13): the recipient, the fair value per share at the grant date with its valuation noted `[verify valuation]`, the service start, the tranches
// and the two accounts its monthly journal posts to. It is saved as a document of type share_grant, a new version at each change.
type Grant struct {
	Recipient      string        `json:"recipient"`
	FairValue      string        `json:"fair_value"`
	Valuation      string        `json:"valuation,omitempty"`
	ServiceStart   string        `json:"service_start"`
	Tranches       []TrancheSpec `json:"tranches"`
	ExpenseAccount uuid.UUID     `json:"expense_account"`
	ReserveAccount uuid.UUID     `json:"reserve_account"`
	Archived       bool          `json:"archived,omitempty"`
}

// TrancheSpec is a tranche as saved.
type TrancheSpec struct {
	Shares int64  `json:"shares"`
	VestOn string `json:"vest_on"`
}

// Read is the grant a document holds.
func Read(doc storage.Document) (Grant, error) {
	if doc.Type != storage.DocumentShareGrant {
		return Grant{}, ports.ErrNotFound
	}
	var g Grant
	if err := json.Unmarshal(doc.Payload, &g); err != nil {
		return Grant{}, fmt.Errorf("the saved share grant cannot be read: %w", err)
	}
	return g, nil
}

func problem(code, field, message, fix string) ledger.Problem {
	return ledger.Problem{Code: code, Field: field, Message: message, Fix: fix}
}

// ScheduleOf is the schedule the grant's figures work out to, in the base currency.
func ScheduleOf(g Grant, base money.Currency) (Expense, error) {
	fv, err := money.ParseUnitPrice(strings.TrimSpace(g.FairValue), base, "fair_value")
	if err != nil {
		return Expense{}, err
	}
	start, err := time.Parse(time.DateOnly, g.ServiceStart)
	if err != nil {
		return Expense{}, &Error{CodeGrant, "service_start", "the service start is not a date", "Enter the first day of a month, such as 2026-01-01"}
	}
	var ts []Tranche
	for i, t := range g.Tranches {
		d, err := time.Parse(time.DateOnly, t.VestOn)
		if err != nil {
			return Expense{}, &Error{CodeTranche, fmt.Sprintf("tranche %d", i+1), fmt.Sprintf("tranche %d's vest day is not a date", i+1), "Enter the day the shares vest, such as 2026-12-31"}
		}
		ts = append(ts, Tranche{Shares: t.Shares, VestOn: d})
	}
	return Work(fv, start, ts)
}

// Check is every problem with a grant (ENG-22): the recipient, the schedule's figures and the two accounts, which are an active expense account and an active equity account that is not
// share capital (share capital takes no manual journal, INV-13: the reserve moves to it when the shares are issued).
func Check(ctx context.Context, u storage.UnitOfWork, g Grant) (ledger.Problems, error) {
	var out ledger.Problems
	if strings.TrimSpace(g.Recipient) == "" || len(g.Recipient) > 120 {
		out = append(out, problem(CodeGrant, "recipient", "the recipient is empty or longer than 120 characters", "Name who the shares are granted to"))
	}
	org, err := u.Organisation(ctx)
	if err != nil {
		return nil, err
	}
	base, err := money.Lookup(org.BaseCurrency)
	if err != nil {
		return nil, err
	}
	if _, err := ScheduleOf(g, base); err != nil {
		var e *Error
		var me *money.Error
		switch {
		case errors.As(err, &e):
			out = append(out, problem(e.Code, e.Field, e.Message, e.Fix))
		case errors.As(err, &me):
			out = append(out, problem(CodeGrant, "fair_value", me.Message, me.Fix))
		default:
			return nil, err
		}
	}
	account := func(id uuid.UUID, field string, typ storage.AccountType, what string) error {
		a, err := u.Accounts().Get(ctx, id)
		switch {
		case errors.Is(err, ports.ErrNotFound) || (err == nil && (a.Type != typ || a.Group || !a.Active || a.Role == storage.RoleShareCapital)):
			out = append(out, problem(CodeAccounts, field, "that is not "+what, "Choose an account of the chart that takes postings"))
		case err != nil:
			return err
		}
		return nil
	}
	if err := account(g.ExpenseAccount, "expense_account", storage.Expense, "an active expense account"); err != nil {
		return nil, err
	}
	if err := account(g.ReserveAccount, "reserve_account", storage.Equity, "an active equity account that is not share capital"); err != nil {
		return nil, err
	}
	return out, nil
}

// Create saves a grant as a document, its first version.
func Create(ctx context.Context, u storage.UnitOfWork, g Grant) (storage.Document, error) {
	g = tidy(g)
	if probs, err := Check(ctx, u, g); err != nil || len(probs) > 0 {
		if err != nil {
			return storage.Document{}, err
		}
		return storage.Document{}, probs
	}
	b, err := json.Marshal(g)
	if err != nil {
		return storage.Document{}, err
	}
	return u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentShareGrant, Payload: b})
}

// Revise saves the next version of a grant. A grant with a month proposed keeps its figures (what was expensed stays what it was): only its recipient, valuation note and archive can change.
func Revise(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, g Grant) (storage.Document, error) {
	doc, err := u.Documents().Lock(ctx, id)
	if err != nil {
		return doc, err
	}
	was, err := Read(doc)
	if err != nil {
		return doc, err
	}
	g = tidy(g)
	runs, err := Months(ctx, u, id)
	if err != nil {
		return doc, err
	}
	if len(runs) > 0 {
		keep := was
		keep.Recipient, keep.Valuation, keep.Archived = g.Recipient, g.Valuation, g.Archived
		if a, b := mustJSON(keep), mustJSON(g); a != b {
			return doc, &Error{CodeMonth, "grant", "a month of its expense is proposed already, so its figures cannot change", "Only the recipient, the valuation note and archiving can change; record a new grant for other figures"}
		}
	}
	if !g.Archived {
		if probs, err := Check(ctx, u, g); err != nil || len(probs) > 0 {
			if err != nil {
				return doc, err
			}
			return doc, probs
		}
	}
	b, err := json.Marshal(g)
	if err != nil {
		return doc, err
	}
	return u.Documents().Revise(ctx, id, b)
}

func mustJSON(g Grant) string { b, _ := json.Marshal(g); return string(b) }

func tidy(g Grant) Grant {
	g.Recipient, g.FairValue, g.Valuation = strings.TrimSpace(g.Recipient), strings.TrimSpace(g.FairValue), strings.TrimSpace(g.Valuation)
	return g
}

// MonthRun is a month of a grant's expense proposed as a draft journal.
type MonthRun struct {
	DocumentID uuid.UUID
	GrantID    uuid.UUID
	Month      time.Time
	JournalID  uuid.UUID
	Expense    string
}

type monthPayload struct {
	Date      string    `json:"date"`
	GrantID   uuid.UUID `json:"grant_id"`
	GrantVer  int       `json:"grant_version"`
	Month     string    `json:"month"`
	JournalID uuid.UUID `json:"journal_id"`
	Expense   string    `json:"expense"`
}

// Months are a grant's months proposed so far, earliest first.
func Months(ctx context.Context, u storage.UnitOfWork, grant uuid.UUID) ([]MonthRun, error) {
	docs, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: storage.DocumentShareGrantMonth, Limit: 1000})
	if err != nil {
		return nil, err
	}
	var out []MonthRun
	for _, d := range docs {
		var p monthPayload
		if json.Unmarshal(d.Payload, &p) != nil || p.GrantID != grant {
			continue
		}
		m, _ := time.Parse("2006-01", p.Month)
		out = append(out, MonthRun{DocumentID: d.ID, GrantID: p.GrantID, Month: m, JournalID: p.JournalID, Expense: p.Expense})
	}
	slices.SortFunc(out, func(a, b MonthRun) int { return a.Month.Compare(b.Month) })
	return out, nil
}

// ProposeMonth makes a month of the grant's expense as a draft journal for a person to approve, and records it so it is never changed: Dr the expense account and Cr the reserve with the month's
// expense (the tranches' parts for it). The month is one the schedule runs over that has begun (today's month included), and each month is proposed once. Nothing is posted here.
func ProposeMonth(ctx context.Context, u storage.UnitOfWork, doc storage.Document, month, today time.Time) (Month, storage.Document, error) {
	g, err := Read(doc)
	if err != nil {
		return Month{}, storage.Document{}, err
	}
	if g.Archived {
		return Month{}, storage.Document{}, &Error{CodeArchived, "grant", "the grant is archived", "Save it again without archiving it"}
	}
	if month.Day() != 1 || month.After(time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)) {
		return Month{}, storage.Document{}, &Error{CodeMonth, "month", "the month is the first day of a month that has begun", "Choose a month up to this one"}
	}
	org, err := u.Organisation(ctx)
	if err != nil {
		return Month{}, storage.Document{}, err
	}
	base, err := money.Lookup(org.BaseCurrency)
	if err != nil {
		return Month{}, storage.Document{}, err
	}
	sched, err := ScheduleOf(g, base)
	if err != nil {
		return Month{}, storage.Document{}, err
	}
	var this *Month
	for i := range sched.Months {
		if sched.Months[i].Month.Equal(month) {
			this = &sched.Months[i]
		}
	}
	if this == nil {
		return Month{}, storage.Document{}, &Error{CodeMonth, "month", "the grant's expense does not run over " + month.Format("January 2006"), "Choose a month between the service start and the last vest day"}
	}
	runs, err := Months(ctx, u, doc.ID)
	if err != nil {
		return Month{}, storage.Document{}, err
	}
	for _, r := range runs {
		if r.Month.Equal(month) {
			return Month{}, storage.Document{}, &Error{CodeMonthRun, "month", "the expense of " + month.Format("January 2006") + " is proposed already", "Open its journal; reverse it if it was wrong"}
		}
	}
	if probs, err := Check(ctx, u, g); err != nil || len(probs) > 0 {
		if err != nil {
			return Month{}, storage.Document{}, err
		}
		return Month{}, storage.Document{}, probs
	}
	if this.Expense.IsZero() {
		return Month{}, storage.Document{}, &Error{CodeMonth, "month", "the grant's expense is nothing in " + month.Format("January 2006"), "Choose a month with an expense"}
	}
	end := month.AddDate(0, 1, -1)
	memo := "Shares for services to " + g.Recipient + ", " + month.Format("January 2006") + ": expense of the grant's fair value over its service months (SFRS(I) 2)"
	j, err := journal.CreateIn(ctx, u, journal.Draft{Date: end, Memo: memo, Lines: []ledger.JournalLine{
		{AccountID: g.ExpenseAccount, Amount: this.Expense, Description: "Share-based payment expense"},
		{AccountID: g.ReserveAccount, Amount: this.Expense.Neg(), Description: "Share-based payment reserve"}}})
	if err != nil {
		return Month{}, storage.Document{}, err
	}
	b, err := json.Marshal(monthPayload{Date: end.Format(time.DateOnly), GrantID: doc.ID, GrantVer: doc.Version, Month: month.Format("2006-01"), JournalID: j.ID, Expense: this.Expense.String()})
	if err != nil {
		return Month{}, storage.Document{}, err
	}
	run, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentShareGrantMonth, Payload: b})
	if err != nil {
		return Month{}, storage.Document{}, err
	}
	if err := u.Documents().SetStatus(ctx, run.ID, storage.DocumentPosted); err != nil {
		return Month{}, storage.Document{}, err
	}
	return *this, j, nil
}
