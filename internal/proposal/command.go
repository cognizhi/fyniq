package proposal

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
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
}

// Commands are the release's commands, by the name the catalogue gives them.
type Commands map[string]Command

// Measure is what a payload holds, for the limits: its items (1 when it is not a batch) and each item's amount.
type Measure struct {
	Items   int
	Amounts []string
}

// Actor is the person a command runs for: the person the AI proposed for in the dry run, the approving person when it
// executes (AI-11).
type Actor struct {
	User  uuid.UUID
	Roles []authz.Role
	Today time.Time
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
