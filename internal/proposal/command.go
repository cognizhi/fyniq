package proposal

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Command is the domain command a task type maps to (design D3, D6): what a person needs to do it themselves, a
// payload's items and amounts for the task type's limits, and its dry run. The core ships none: each task type's
// change registers its own, with what it acts on.
type Command interface {
	// Policy is the operation a person needs the right to, to do it themselves (Appendix D): the person the AI proposes
	// for must hold it, as must the person who approves.
	Policy() authz.Policy
	// Measure is the payload's items, and each item's amount in the limits' currency where it moves money.
	Measure(payload json.RawMessage) (Measure, error)
	// DryRun does it in u, as as, in a unit of work that is rolled back: what it would change, in platform words, and
	// the version of every record it read or changed. A refusal names the field and the fix, as the API's own do.
	DryRun(ctx context.Context, u storage.UnitOfWork, payload json.RawMessage, as Actor) (Impact, error)
	// Execute does it in u, as as (the approving person, AI-11), only while every record the dry run read is at its base
	// version: ErrStale when one moved (AI-P5). It runs in the executor, under the executor's role, which holds the
	// grants its task types need and no more; locks, policies and validations hold as for any write.
	Execute(ctx context.Context, u storage.UnitOfWork, payload json.RawMessage, base map[string]int64, as Actor) (Execution, error)
}

// Undoer is the optional side of a command that can be undone (PRD-001 AI-31, AI-91): a command that posts or changes master data reverses what it did
// through the domain's own function, as the person who undoes it, only while the records are as the execution left them and, for ledger entries,
// the period is open; it never updates or deletes posted history. A command that does not implement it is not undoable: a draft posts nothing and is
// never deleted, and a message sent cannot be unsent.
type Undoer interface {
	// Undo reverses what an execution made, as as (the person undoing it), in u, and says what it reversed. A refusal names the field and the fix, as the
	// API's own do; ErrStale when a record moved since the execution.
	Undo(ctx context.Context, u storage.UnitOfWork, done Execution, as Actor) (Execution, error)
}

// Undoable is whether cmd can be undone.
func Undoable(cmd Command) bool {
	_, ok := cmd.(Undoer)
	return ok
}

// ErrStale is a command's refusal to execute because a record it read moved since the dry run (AI-P5).
var ErrStale = errors.New("a record the proposal relied on changed since its dry run")

// Execution is what a command did: the records it made or changed and the ledger entries it posted.
type Execution struct {
	Records []string `json:"records"`
	Entries []string `json:"entries"`
	// Undo is what the command's own Undo needs to reverse it, written as it executes: the values it replaced and the version it left a record at
	// (AI-31, AI-91). Opaque to everything else, kept with the proposal's execution record, never shown as a figure, and never carrying a person's
	// sealed personal data: a command that changes such a record refuses instead.
	Undo json.RawMessage `json:"undo,omitempty"`
}

// Commands are the release's commands, by the name the catalogue gives them.
type Commands map[string]Command

// Release are the release's commands. Each task type's change adds its own with what it acts on.
var Release = Commands{"CreateComplianceTask": CreateComplianceTask{}, "DraftReminder": DraftReminder{}, "DraftCorporateDocument": DraftCorporateDocument{}, "DraftInvoice": DraftInvoice{}, "DraftBill": DraftBill{}, "DraftClaim": DraftClaim{}, "UpdateContact": UpdateContact{}, "UpsertBankRule": UpsertBankRule{}, "CreateRecurring": CreateRecurring{}, "CreateBudgetVersion": CreateBudgetVersion{}, "ProposePage": ProposePage{}}

// Measure is what a payload holds, for the limits: its items (1 when it is not a batch) and each item's amount.
type Measure struct {
	Items   int
	Amounts []string
}

// Actor is the person a command runs for: the person the AI proposed for in the dry run, the approving person when it
// executes (AI-11).
type Actor struct {
	User uuid.UUID
	// Requester is the person the AI proposed for: the same as User in the dry run, and the person who asked (who may not be the approver)
	// when it executes. A command that acts for one person, such as a claim, uses it and refuses when the approver is another.
	Requester uuid.UUID
	Roles     []authz.Role
	Today     time.Time
	// Proposal is the proposal it executes: Nil in the dry run.
	Proposal uuid.UUID
	// Outbox is where the command's side work goes as it executes, such as an email's send (outbox.Dispatcher): the
	// executor's own; zero in the dry run, which sends nothing.
	Outbox outbox.Dispatcher
}

// Impact is a dry run's: its summary, from platform code and never the model (AI-07, design D5); what the review shows,
// such as ledger lines, balances before and after, checks that change and documents made or changed; and the version
// of every record it read or changed (base versions, for the executor's check).
type Impact struct {
	Summary string           `json:"summary"`
	Changes json.RawMessage  `json:"changes,omitempty"`
	Base    map[string]int64 `json:"-"`
}

// Within is why a payload's measure breaks the task type's limits, or "" when it keeps to them.
func (t TaskType) Within(m Measure) string {
	items := max(m.Items, 1)
	if items > t.Limits.Items {
		return fmt.Sprintf("it holds %d items: one proposal of %s holds at most %d", items, t.ID, t.Limits.Items)
	}
	if len(m.Amounts) == 0 {
		return ""
	}
	if t.Limits.AmountPerItem == "" && t.Limits.AmountTotal == "" {
		return fmt.Sprintf("it moves money, which %s never does", t.ID)
	}
	ctx := apd.BaseContext.WithPrecision(34)
	total := new(apd.Decimal)
	for _, a := range m.Amounts {
		d, _, err := apd.NewFromString(a)
		if err != nil || d.Form != apd.Finite || d.Sign() < 0 {
			return fmt.Sprintf("the amount %q is not an exact decimal of zero or more", a)
		}
		if t.Limits.AmountPerItem != "" && exceeds(d, t.Limits.AmountPerItem) {
			return fmt.Sprintf("an item of %s %s is more than the %s %s one item may hold", t.Limits.Currency, a, t.Limits.Currency, t.Limits.AmountPerItem)
		}
		if _, err := ctx.Add(total, total, d); err != nil {
			return "the amounts cannot be added up exactly"
		}
	}
	if t.Limits.AmountTotal != "" && exceeds(total, t.Limits.AmountTotal) {
		return fmt.Sprintf("its items come to %s %s, more than the %s %s one proposal may hold", t.Limits.Currency, total.Text('f'),
			t.Limits.Currency, t.Limits.AmountTotal)
	}
	return ""
}

func exceeds(d *apd.Decimal, limit string) bool {
	l, _, err := apd.NewFromString(limit)
	return err != nil || d.Cmp(l) > 0
}

// Canonical is a payload as it is kept, hashed and executed: its JSON with each object's keys sorted and no spaces,
// numbers as they were sent. Its SHA-256, in hex, is what a person approves.
func Canonical(raw json.RawMessage) (payload, sha string, err error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var v map[string]any
	if err := d.Decode(&v); err != nil {
		return "", "", fmt.Errorf("the payload is not a JSON object: %w", err)
	}
	if v == nil {
		return "", "", fmt.Errorf("the payload is not a JSON object")
	}
	b, err := json.Marshal(v) // map keys sorted, json.Number kept as sent
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256(b)
	return string(b), hex.EncodeToString(sum[:]), nil
}

// Subset is a batch payload cut to the items an approver chose (AI-32), from 1, in the payload's order: what executes,
// as Canonical keeps it, and its SHA-256. Choosing every item is the payload itself.
func Subset(payload string, items []int) (subset, sha string, err error) {
	var v map[string]json.RawMessage
	if err := json.Unmarshal([]byte(payload), &v); err != nil {
		return "", "", err
	}
	var all []json.RawMessage
	if err := json.Unmarshal(v["items"], &all); err != nil || len(all) == 0 {
		return "", "", fmt.Errorf("the proposal is not a batch of items")
	}
	chosen := make([]bool, len(all))
	for _, n := range items {
		if n < 1 || n > len(all) || chosen[n-1] {
			return "", "", fmt.Errorf("item %d is not one of the proposal's %d, or is chosen twice", n, len(all))
		}
		chosen[n-1] = true
	}
	if len(items) == 0 {
		return "", "", fmt.Errorf("no item is chosen")
	}
	kept := make([]json.RawMessage, 0, len(items))
	for i, it := range all {
		if chosen[i] {
			kept = append(kept, it)
		}
	}
	b, err := json.Marshal(kept)
	if err != nil {
		return "", "", err
	}
	v["items"] = b
	whole, err := json.Marshal(v)
	if err != nil {
		return "", "", err
	}
	return Canonical(whole)
}

// decodeStrict reads a payload into v, refusing a field the task type does not name.
func decodeStrict(payload json.RawMessage, v any) error {
	d := json.NewDecoder(bytes.NewReader(payload))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return refusal("the payload holds a field the task type does not name: "+err.Error(), "Send only the fields the task type's schema lists")
	}
	return nil
}
