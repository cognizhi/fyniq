package proposal

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// kb.propose_page (PRD-001 AI-59, Appendix E; p2-ai-expansion design D2): from a conversation the AI may propose a know-how page, which an Owner or an
// Administrator approves. An answer never becomes a page by itself (AI-59): it carries one person's context and the organisation's figures. The proposal
// makes a knowledge-base NOTE, a source, under the key "proposed.<key>"; it is not a page, and a page is compiled from it and published only by the
// knowledge base's own steps, which a person approves (AI-56). Its text holds no figure (NoFigures), a rule the proposal's dry run applies whatever the model wrote.

// KBPagePayload is kb.propose_page's payload.
type KBPagePayload struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

// ProposedKeyPrefix begins the key of every note the AI proposes: the database holds the executor to it.
const ProposedKeyPrefix = "proposed."

var (
	pageKey = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,60}$`)
	// money: a currency sign or code beside a number, either way round.
	moneyShape = regexp.MustCompile(`(?i)(?:S\$|US\$|\$|€|£|¥|\b(?:SGD|USD|EUR|GBP|JPY|MYR|IDR|AUD|HKD)\b)\s?\d|\d\s?(?:S\$|\$|€|£|¥|\b(?:SGD|USD|EUR|GBP|JPY|MYR|IDR|AUD|HKD)\b)`)
	percent    = regexp.MustCompile(`\d\s?(?:%|per ?cent\b)`)
	decimal    = regexp.MustCompile(`\d[\d,]*\.\d`)
	number     = regexp.MustCompile(`\d[\d,]*`)
	identifier = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b|\b[STFGM]\d{7}[A-Z]\b|\b\d{9,}\b`)
)

// NoFigures is why a text holds a figure, or "" when it holds none: an amount of money, a percentage, a decimal, an identifier (a UUID, an identity or
// account number), or a whole number of a thousand or more that is not a year (1900 to 2100). Small numbers in prose, such as "within 14 days" or "step 3",
// and years are not figures. It is deliberately strict: a page the AI proposes is know-how, and a figure is read from a tool at the time it is asked for.
func NoFigures(text string) string {
	switch {
	case moneyShape.MatchString(text):
		return "it holds an amount of money"
	case percent.MatchString(text):
		return "it holds a percentage"
	case decimal.MatchString(text):
		return "it holds a decimal figure"
	case identifier.MatchString(text):
		return "it holds an identifier, such as an ID, an identity number or an account number"
	}
	for _, m := range number.FindAllString(text, -1) {
		n, err := strconv.Atoi(strings.ReplaceAll(m, ",", ""))
		if err == nil && n >= 1000 && !(n >= 1900 && n <= 2100 && !strings.Contains(m, ",")) {
			return "it holds a figure of a thousand or more"
		}
		if err != nil {
			return "it holds a figure"
		}
	}
	return ""
}

func (p KBPagePayload) check() error {
	key := strings.TrimSpace(p.Key)
	switch {
	case !pageKey.MatchString(key):
		return refusal("payload.key is not a key", "Use lower-case letters, digits, dots and hyphens, starting with a letter or digit, up to 61 characters")
	case strings.HasPrefix(key, ProposedKeyPrefix):
		return refusal("payload.key already starts with proposed.", "Send the key without the prefix; Fyniq adds it")
	case strings.TrimSpace(p.Title) == "" || len([]rune(p.Title)) > 120:
		return refusal("payload.title is empty or longer than 120 characters", "Give the page a short title")
	case strings.TrimSpace(p.Body) == "" || len([]rune(p.Body)) > 6000:
		return refusal("payload.body is empty or longer than 6,000 characters", "Write the know-how in up to 6,000 characters")
	}
	if why := NoFigures(p.Title + "\n" + p.Body); why != "" {
		return refusal("a page the AI proposes holds no figure: "+why, "Take the figure out and say where to read it, such as \"see the cash KPI\"; figures come from the tools when they are asked for")
	}
	return nil
}

// ProposePage is kb.propose_page's command.
type ProposePage struct{}

// Policy implements Command: writing a knowledge-base note (C on "Knowledge base"): the Owner and the Administrator.
func (ProposePage) Policy() authz.Policy { return authz.CreateKBNote }

// Measure implements Command: one page, no amount.
func (ProposePage) Measure(payload json.RawMessage) (Measure, error) {
	var p KBPagePayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return Measure{}, err
	}
	return Measure{Items: 1}, nil
}

const noteBase = "kb_note"

func noteKey(p KBPagePayload) string { return ProposedKeyPrefix + strings.TrimSpace(p.Key) }

// DryRun implements Command: the note as it would be written, its text in full, refused when it holds a figure or its key is taken.
func (ProposePage) DryRun(ctx context.Context, u storage.UnitOfWork, payload json.RawMessage, as Actor) (Impact, error) {
	var p KBPagePayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return Impact{}, err
	}
	if err := p.check(); err != nil {
		return Impact{}, err
	}
	if _, err := u.OrganisationKnowledge().Note(ctx, noteKey(p)); err == nil {
		return Impact{}, refusal("payload.key is taken by a note that exists already", "Choose another key")
	}
	changes, err := json.Marshal(map[string]any{"kb_notes": []map[string]any{{"change": "add", "key": noteKey(p), "kind": "note", "title": strings.TrimSpace(p.Title), "body": strings.TrimSpace(p.Body)}}})
	if err != nil {
		return Impact{}, err
	}
	return Impact{Summary: fmt.Sprintf("Adds the knowledge-base note %q for a person to turn into a page; nothing is published", strings.TrimSpace(p.Title)), Changes: changes,
		Base: map[string]int64{noteBase: 0}}, nil
}

// Execute implements Command: the note, written as the approving person, unless the key was taken since the dry run (stale).
func (ProposePage) Execute(ctx context.Context, u storage.UnitOfWork, payload json.RawMessage, base map[string]int64, as Actor) (Execution, error) {
	var p KBPagePayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return Execution{}, err
	}
	if err := p.check(); err != nil {
		return Execution{}, err
	}
	if _, ok := base[noteBase]; !ok {
		return Execution{}, fmt.Errorf("the dry run did not read %s", noteBase)
	}
	if _, err := u.OrganisationKnowledge().Note(ctx, noteKey(p)); err == nil {
		return Execution{}, ErrStale // a note took the key since the dry run
	}
	n, err := u.OrganisationKnowledge().SaveNote(ctx, storage.KBNote{Key: noteKey(p), Kind: "note", Title: strings.TrimSpace(p.Title), Body: strings.TrimSpace(p.Body),
		DataClass: "general", UpdatedBy: as.User, UpdatedAt: as.Today}, 0)
	if err != nil {
		return Execution{}, err
	}
	return Execution{Records: []string{"kb_note:" + n.ID.String()}, Entries: []string{}}, nil
}
