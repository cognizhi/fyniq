// Package proposal is the AI's one write path (PRD-001 §9.3, §9.5, §9.6, AI-P3 to AI-P5; p1-ai-proposals-core design
// D1 to D6): the task catalogue of contracts/ai/tasks, checked as it loads; the proposal's lifecycle; and what proposing,
// approving and executing a task type need of it. The AI proposes, a named person approves the exact payload, and the
// executor runs it as that person. Nothing on the never-list can ever be proposed, whatever a catalogue file says.
package proposal

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/cockroachdb/apd/v3"

	contracts "github.com/cognizhi/fyniq/contracts/ai"
	"github.com/cognizhi/fyniq/internal/aitool"
	"github.com/cognizhi/fyniq/internal/authz"
)

// The risk tiers (§9.6): 1 routine and reversible, 2 posts or changes master data, 3 reserved and never allowed today.
const (
	Tier1 = 1
	Tier2 = 2
)

// The most a task type may allow.
const (
	MaxItems       = 100
	MaxExpiryHours = 72
	// MaxIDLength keeps the tool the model is offered, propose_ and the ID, within the 64 characters providers allow.
	MaxIDLength = 56
)

// Limits are what one proposal of a task type may hold: items, and amounts per item and in total in a currency.
type Limits struct {
	Items         int    `json:"items"`
	AmountPerItem string `json:"amount_per_item,omitempty"`
	AmountTotal   string `json:"amount_total,omitempty"`
	Currency      string `json:"currency,omitempty"`
}

// TaskType is a catalogue entry (§9.6, "Catalogue entry attributes").
type TaskType struct {
	ID          string          `json:"id"`
	Version     int             `json:"version"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	Command     string          `json:"command"`
	Tier        int             `json:"tier"`
	Limits      Limits          `json:"limits"`
	Approvers   []authz.Role    `json:"approvers"`
	ExpiryHours int             `json:"expiry_hours"`
	// SHA256 is the hash of the definition's canonical JSON.
	SHA256 string `json:"-"`
}

// Bulk is whether an approver may approve all of a proposal's items in one action: tier 1 only (AI-32).
func (t TaskType) Bulk() bool { return t.Tier == Tier1 }

// Expiry is how long a proposal of the type waits for its decision.
func (t TaskType) Expiry() time.Duration { return time.Duration(t.ExpiryHours) * time.Hour }

// ModelName is the tool the model is offered to propose it (design D7).
func (t TaskType) ModelName() string { return "propose_" + strings.ReplaceAll(t.ID, ".", "_") }

// Catalogue is the release's task types.
type Catalogue struct {
	types []TaskType
	byID  map[string]int
}

// Types are the catalogue's task types, by ID.
func (c *Catalogue) Types() []TaskType { return slices.Clone(c.types) }

// Get is the task type id, if the catalogue has it.
func (c *Catalogue) Get(id string) (TaskType, bool) {
	i, ok := c.byID[id]
	if !ok {
		return TaskType{}, false
	}
	return c.types[i], true
}

var defaultCatalogue = sync.OnceValues(func() (*Catalogue, error) { return Load(contracts.Tasks) })

// Default is the catalogue the release ships, from contracts/ai/tasks. The tests check it, so a broken one is a build
// that never passed: it panics.
func Default() *Catalogue {
	c, err := defaultCatalogue()
	if err != nil {
		panic("the proposal catalogue: " + err.Error())
	}
	return c
}

// Load reads and checks the task types in fsys's tasks directory. A catalogue with none is a catalogue: nothing can be
// proposed.
func Load(fsys fs.FS) (*Catalogue, error) {
	paths, err := fs.Glob(fsys, "tasks/*.json")
	if err != nil {
		return nil, err
	}
	slices.Sort(paths)
	c := &Catalogue{byID: map[string]int{}}
	models := map[string]bool{}
	for _, p := range paths {
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return nil, err
		}
		t, err := parse(b)
		if err == nil && path.Base(p) != t.ID+".json" {
			err = fmt.Errorf("is %s: a task type's file is its ID and .json", t.ID)
		}
		if err == nil {
			err = check(t)
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		if models[t.ModelName()] {
			return nil, fmt.Errorf("%s: another task type has the tool name %s", p, t.ModelName())
		}
		models[t.ModelName()] = true
		c.byID[t.ID] = len(c.types)
		c.types = append(c.types, t)
	}
	return c, nil
}

// parse reads a definition strictly and hashes its canonical JSON.
func parse(b []byte) (TaskType, error) {
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	var t TaskType
	if err := d.Decode(&t); err != nil {
		return TaskType{}, err
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return TaskType{}, err
	}
	canonical, err := json.Marshal(v) // object keys sorted
	if err != nil {
		return TaskType{}, err
	}
	sum := sha256.Sum256(canonical)
	t.SHA256 = hex.EncodeToString(sum[:])
	compact := bytes.Buffer{}
	if err := json.Compact(&compact, t.InputSchema); err != nil {
		return TaskType{}, err
	}
	t.InputSchema = compact.Bytes()
	return t, nil
}

var (
	taskID      = regexp.MustCompile(`^[a-z]+\.[a-z]+(_[a-z]+)*$`)
	commandName = regexp.MustCompile(`^[A-Z][a-z]+([A-Z][a-z]+)+$`)
	currency    = regexp.MustCompile(`^[A-Z]{3}$`)
)

// check refuses a definition that is not a bounded, catalogued change a person may approve: the rules of design D1.
func check(t TaskType) error {
	f := func(ok bool, format string, args ...any) error {
		if ok {
			return nil
		}
		return fmt.Errorf(format, args...)
	}
	return first(
		f(taskID.MatchString(t.ID) && len(t.ID) <= MaxIDLength, "the ID %q is not an area, a dot and lower-case words joined by underscores, in at most %d characters", t.ID, MaxIDLength),
		f(Refuse(t.ID, t.Command) == "", "%s is on the never-list: %s (§9.6)", t.ID, Refuse(t.ID, t.Command)),
		f(t.Version >= 1, "the version is %d, not 1 or more", t.Version),
		f(t.Name != "" && len(t.Name) <= 60, "the name is empty or over 60 characters"),
		f(t.Description != "" && len(t.Description) <= 1000, "the description is empty or over 1,000 characters"),
		f(commandName.MatchString(t.Command), "the command %q is not a domain command's name, such as CreateComplianceTask", t.Command),
		f(t.Tier == Tier1 || t.Tier == Tier2, "the tier is %d: 1 or 2; tier 3 is reserved and never allowed today", t.Tier),
		f(t.Limits.Items >= 1 && t.Limits.Items <= MaxItems, "limits.items is %d, not 1 to %d", t.Limits.Items, MaxItems),
		checkAmounts(t.Limits),
		f(len(t.Approvers) > 0, "no approver roles: a person approves every proposal (AI-P4)"),
		checkApprovers(t.Approvers),
		f(t.ExpiryHours >= 1 && t.ExpiryHours <= MaxExpiryHours, "expiry_hours is %d, not 1 to %d", t.ExpiryHours, MaxExpiryHours),
		aitool.CheckInput(t.InputSchema),
	)
}

func checkAmounts(l Limits) error {
	if l.AmountPerItem == "" && l.AmountTotal == "" {
		if l.Currency != "" {
			return fmt.Errorf("limits.currency without an amount")
		}
		return nil
	}
	if !currency.MatchString(l.Currency) {
		return fmt.Errorf("limits.currency %q is not a currency's code: an amount needs its currency", l.Currency)
	}
	for _, a := range []string{l.AmountPerItem, l.AmountTotal} {
		if a == "" {
			continue
		}
		if d, _, err := apd.NewFromString(a); err != nil || d.Form != apd.Finite || d.Sign() <= 0 {
			return fmt.Errorf("a limit's amount %q is not an exact decimal above zero", a)
		}
	}
	return nil
}

func checkApprovers(roles []authz.Role) error {
	for _, r := range roles {
		if _, ok := authz.Roles[r]; !ok || r == authz.AI {
			return fmt.Errorf("the approver role %q is not a person's role", r)
		}
	}
	return nil
}

func first(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// never is the never-list (§9.6, "Never in the catalogue"): words, or runs of words, a task type's ID or command may
// not hold, with what they name. The proposal service refuses them as the catalogue loads and again as each proposal is made, so adding one
// to the catalogue proposes nothing.
var never = []struct {
	words []string
	what  string
}{
	{[]string{"approve", "approval", "approvals", "reject", "sign", "signature"}, "approving or rejecting anything"},
	{[]string{"release", "pay", "payment", "initiate"}, "releasing or initiating payments"},
	{[]string{"payee", "payees", "bank details", "bank account"}, "creating or changing payee bank details"},
	{[]string{"user", "users", "role", "roles", "member", "members", "team mode", "policy", "policies", "catalogue", "setting", "settings"},
		"changing users, roles, team mode, approval policies, the AI catalogue or AI settings"},
	{[]string{"statutory", "pack", "packs", "parameter", "parameters"}, "changing statutory parameters"},
	{[]string{"reopen", "unlock"}, "reopening periods"},
	{[]string{"void", "delete", "remove", "purge"}, "voiding or deleting posted records"},
	{[]string{"lodge", "file", "filing", "submit"}, "filing or lodging with an authority"},
	{[]string{"export"}, "exporting the organisation's data"},
	{[]string{"audit"}, "the audit trail"},
}

// Refuse is what on the never-list a task type would do, or "" when it does none of it: by the words of its ID and of
// its command.
func Refuse(id, command string) string {
	for _, words := range [][]string{splitWords(id), splitWords(command)} {
		for _, n := range never {
			for _, w := range n.words {
				if holds(words, strings.Fields(w)) {
					return n.what
				}
			}
		}
	}
	return ""
}

// holds is whether words hold run, one word after another.
func holds(words, run []string) bool {
	for i := 0; i+len(run) <= len(words); i++ {
		if slices.Equal(words[i:i+len(run)], run) {
			return true
		}
	}
	return false
}

// splitWords are an ID's or a command's words, lower-case: sec.create_task → sec create task; CreateComplianceTask →
// create compliance task.
func splitWords(s string) []string {
	var out []string
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			out = append(out, b.String())
			b.Reset()
		}
	}
	for _, r := range s {
		switch {
		case r == '.' || r == '_' || r == '-' || r == ' ':
			flush()
		case unicode.IsUpper(r):
			flush()
			b.WriteRune(unicode.ToLower(r))
		default:
			b.WriteRune(r)
		}
	}
	flush()
	return out
}
