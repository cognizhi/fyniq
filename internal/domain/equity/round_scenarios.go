package equity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Scenario is a round scenario as saved (FR-EQ-21, design D8): its name, the model's inputs, the class the shares are of
// and the resolutions the issues will name. Its outcome is computed on read and never stored; approving it (FM-1)
// drafts an issue for cash per investor and, for a top-up, a pool resolution. Modelling never posts.
type Scenario struct {
	Date           time.Time
	Name           string
	On             time.Time
	Price          string // a price per share, or
	PreMoney       string // a pre-money valuation
	PriceDecimals  int
	ClassAccountID uuid.UUID
	Investors      []ScenarioInvestor
	TopUp          string // shares added to the pool, "" for none
	TopUpBefore    bool
	Instruments    []uuid.UUID
	ResolutionRefs string
	Note           string
}

// ScenarioInvestor is an investor's money in a scenario, as typed.
type ScenarioInvestor struct {
	HolderID uuid.UUID
	Amount   string
}

type scenarioPayload struct {
	Date           string            `json:"date"`
	Name           string            `json:"name"`
	On             string            `json:"on"`
	Price          string            `json:"price,omitempty"`
	PreMoney       string            `json:"pre_money,omitempty"`
	PriceDecimals  int               `json:"price_decimals"`
	ClassAccountID string            `json:"class_account_id"`
	Investors      []investorPayload `json:"investors"`
	TopUp          string            `json:"top_up,omitempty"`
	TopUpBefore    bool              `json:"top_up_before,omitempty"`
	Instruments    []string          `json:"instruments"`
	ResolutionRefs string            `json:"resolution_refs,omitempty"`
	Note           string            `json:"note,omitempty"`
}

type investorPayload struct {
	HolderID string `json:"holder_id"`
	Amount   string `json:"amount"`
}

// MaxScenarioName is the longest name a scenario takes; MaxInvestors the most investors.
const (
	MaxScenarioName = 200
	MaxInvestors    = 50
)

var plainAmount = regexp.MustCompile(`^[0-9]{1,14}(\.[0-9]{1,2})?$`)

// EncodeScenario is a scenario as a document version keeps it.
func EncodeScenario(s Scenario) ([]byte, error) {
	p := scenarioPayload{Date: dateText(s.Date), Name: s.Name, On: dateText(s.On), Price: s.Price, PreMoney: s.PreMoney, PriceDecimals: s.PriceDecimals,
		ClassAccountID: idText(s.ClassAccountID), Investors: []investorPayload{}, TopUp: s.TopUp, TopUpBefore: s.TopUpBefore, Instruments: []string{},
		ResolutionRefs: s.ResolutionRefs, Note: s.Note}
	for _, i := range s.Investors {
		p.Investors = append(p.Investors, investorPayload{HolderID: idText(i.HolderID), Amount: i.Amount})
	}
	for _, id := range s.Instruments {
		p.Instruments = append(p.Instruments, id.String())
	}
	return json.Marshal(p)
}

// ReadScenario reads a scenario's version as it was saved.
func ReadScenario(doc storage.Document) (Scenario, error) {
	var p scenarioPayload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return Scenario{}, err
	}
	out := Scenario{Name: p.Name, Price: p.Price, PreMoney: p.PreMoney, PriceDecimals: p.PriceDecimals, TopUp: p.TopUp, TopUpBefore: p.TopUpBefore,
		ResolutionRefs: p.ResolutionRefs, Note: p.Note}
	var err error
	if out.ClassAccountID, err = uuid.Parse(p.ClassAccountID); err != nil {
		return out, err
	}
	for _, i := range p.Investors {
		id, err := uuid.Parse(i.HolderID)
		if err != nil {
			return out, err
		}
		out.Investors = append(out.Investors, ScenarioInvestor{HolderID: id, Amount: i.Amount})
	}
	for _, s := range p.Instruments {
		id, err := uuid.Parse(s)
		if err != nil {
			return out, err
		}
		out.Instruments = append(out.Instruments, id)
	}
	if out.On, err = time.Parse(time.DateOnly, p.On); err != nil {
		return out, err
	}
	out.Date, err = time.Parse(time.DateOnly, p.Date)
	return out, err
}

// CheckScenario is what is wrong with a scenario on its own, every problem at once (ENG-22): no name, no day, both or
// neither of a price and a pre-money valuation, a price that is not one above zero with at most 6 decimals, a valuation
// that is not an amount above zero, decimals outside 0 to 6, a class that is not an active share-capital account, an
// investor named twice or putting in nothing, a top-up that is not a whole number of shares, an instrument named twice,
// or text too long.
func CheckScenario(s Scenario, set ledger.Settings) ledger.Problems {
	var probs ledger.Problems
	add := func(field, msg, fix string) {
		probs = append(probs, ledger.Problem{Code: CodeRound, Field: field, Message: msg, Fix: fix})
	}
	switch n := utf8.RuneCountInString(strings.TrimSpace(s.Name)); {
	case n == 0:
		add("name", "the scenario has no name", "Name it, such as Series A at S$3.00")
	case n > MaxScenarioName:
		add("name", fmt.Sprintf("the name has %d characters; the most is %d", n, MaxScenarioName), "Shorten it")
	}
	if s.On.IsZero() {
		add("on", "the round's day is missing", "Enter the day the round would take effect")
	}
	switch {
	case (s.Price == "") == (s.PreMoney == ""):
		add("price", "the scenario names both a price and a pre-money valuation, or neither", "Enter a price per share or a pre-money valuation")
	case s.Price != "":
		if p, err := money.ParseUnitPrice(s.Price, set.Base, "price"); err != nil || p.Sign() <= 0 {
			add("price", fmt.Sprintf("%q is not a price per share above zero in %s", s.Price, set.Base), "Enter the price per share, such as 3.00")
		}
	default:
		if !plainAmount.MatchString(s.PreMoney) || strings.Trim(s.PreMoney, "0.") == "" {
			add("pre_money", fmt.Sprintf("%q is not a valuation above zero in %s", s.PreMoney, set.Base), "Enter the pre-money valuation, such as 3000000")
		}
		if s.PriceDecimals < 0 || s.PriceDecimals > MaxPriceDecimals {
			add("price_decimals", fmt.Sprintf("%d decimals is outside 0 to %d", s.PriceDecimals, MaxPriceDecimals), "Enter 0 to 6, such as 4")
		}
	}
	if a, ok := set.Accounts[s.ClassAccountID]; !ok || !a.ShareCapital || a.Group || !a.Active {
		add("class_account_id", "the class is not an active share-capital account", "Choose 3010 Ordinary or 3020 Preference share capital")
	}
	if len(s.Investors) == 0 {
		add("investors", "the round has no investor", "Add each investor and the money they put in")
	}
	if len(s.Investors) > MaxInvestors {
		add("investors", fmt.Sprintf("the round has %d investors; the most is %d", len(s.Investors), MaxInvestors), "Model fewer at once")
	}
	seen := map[uuid.UUID]bool{}
	for k, i := range s.Investors {
		field := fmt.Sprintf("investors[%d]", k)
		if i.HolderID == uuid.Nil {
			add(field+".holder_id", "names no investor", "Choose the contact who puts the money in")
		} else if seen[i.HolderID] {
			add(field+".holder_id", "names an investor already in the round", "Put each investor's money in one line")
		}
		seen[i.HolderID] = true
		if !plainAmount.MatchString(i.Amount) || strings.Trim(i.Amount, "0.") == "" {
			add(field+".amount", fmt.Sprintf("%q is not an amount above zero in %s", i.Amount, set.Base), "Enter the money they put in, such as 600000")
		}
	}
	if s.TopUp != "" && !wholeShares.MatchString(s.TopUp) {
		add("top_up", fmt.Sprintf("%q is not a whole number of shares above zero", s.TopUp), "Enter the shares added to the pool, or leave it out")
	}
	ids := map[uuid.UUID]bool{}
	for k, id := range s.Instruments {
		if ids[id] {
			add(fmt.Sprintf("instruments[%d]", k), "names an instrument already converting", "Name each SAFE or note once")
		}
		ids[id] = true
	}
	if n := utf8.RuneCountInString(s.ResolutionRefs); n > MaxResolutionRefs {
		add("resolution_refs", fmt.Sprintf("the references have %d characters; the most is %d", n, MaxResolutionRefs), "Shorten them")
	}
	if utf8.RuneCountInString(s.Note) > MaxNote {
		add("note", fmt.Sprintf("the note is longer than %d characters", MaxNote), "Shorten it")
	}
	return probs
}

// checkedScenario checks a scenario on its own and against the books: each investor a contact marked investor or
// shareholder, each instrument a recorded SAFE or note a priced round converts and no conversion stands for.
func checkedScenario(ctx context.Context, u storage.UnitOfWork, s Scenario, set ledger.Settings) error {
	probs := CheckScenario(s, set)
	add := func(field, msg, fix string) {
		probs = append(probs, ledger.Problem{Code: CodeRound, Field: field, Message: msg, Fix: fix})
	}
	for k, i := range s.Investors {
		if i.HolderID == uuid.Nil {
			continue
		}
		c, err := u.Contacts().Get(ctx, i.HolderID)
		switch {
		case errors.Is(err, ports.ErrNotFound):
			add(fmt.Sprintf("investors[%d].holder_id", k), "the contact does not exist in this organisation", "Choose a contact marked investor or shareholder")
		case err != nil:
			return err
		case !c.Investor && !c.Shareholder:
			add(fmt.Sprintf("investors[%d].holder_id", k), c.LegalName+" is not marked investor or shareholder", "Mark the contact an investor in Contacts first")
		}
	}
	converting, err := ConvertingAt(ctx, u)
	if err != nil {
		return err
	}
	for k, id := range s.Instruments {
		if !slices.ContainsFunc(converting, func(c Converting) bool { return c.ID == id }) {
			add(fmt.Sprintf("instruments[%d]", k), "is not a recorded SAFE or note a priced round converts, or a conversion of it stands",
				"Choose one of the SAFEs and notes not converted yet")
		}
	}
	if len(probs) > 0 {
		return probs
	}
	return nil
}

// ModelOf is a scenario's inputs as the model takes them.
func ModelOf(s Scenario, set ledger.Settings) (RoundModel, error) {
	m := RoundModel{On: s.On, PriceDecimals: s.PriceDecimals}
	if s.Price != "" {
		p, err := money.ParseUnitPrice(s.Price, set.Base, "price")
		if err != nil {
			return m, err
		}
		m.Price = &p
	} else {
		v, err := money.ParseField(s.PreMoney, set.Base, "pre_money")
		if err != nil {
			return m, err
		}
		m.PreMoney = &v
	}
	for _, i := range s.Investors {
		a, err := money.ParseField(i.Amount, set.Base, "amount")
		if err != nil {
			return m, err
		}
		m.Investors = append(m.Investors, RoundInvestor{HolderID: i.HolderID, Amount: a})
	}
	if s.TopUp != "" {
		m.TopUp = &TopUp{Before: s.TopUpBefore}
		if _, _, err := m.TopUp.Shares.SetString(s.TopUp); err != nil {
			return m, err
		}
	}
	return m, nil
}

// OutcomeIn is what a scenario gives against the books in u (design D8): the model over the cap table fully diluted at
// the end of the day before its round, with its instruments converting at its price. It saves nothing.
func OutcomeIn(ctx context.Context, u storage.UnitOfWork, s Scenario, set ledger.Settings) (RoundOutcome, error) {
	m, err := ModelOf(s, set)
	if err != nil {
		return RoundOutcome{}, err
	}
	before, err := TableIn(ctx, u, s.On.AddDate(0, 0, -1))
	if err != nil {
		return RoundOutcome{}, err
	}
	converting, err := ConvertingAt(ctx, u)
	if err != nil {
		return RoundOutcome{}, err
	}
	var safes []Converting
	for _, id := range s.Instruments {
		if i := slices.IndexFunc(converting, func(c Converting) bool { return c.ID == id }); i >= 0 {
			safes = append(safes, converting[i])
		}
	}
	enabled := map[string]bool{}
	for _, t := range Templates {
		if enabled[t.Key], err = Enabled(ctx, u, t.Key); err != nil {
			return RoundOutcome{}, err
		}
	}
	return ModelRound(m, before, safes, func(k string) bool { return enabled[k] })
}

// DraftScenarioIn saves a scenario in u: a draft, modelled on read until it is approved.
func DraftScenarioIn(ctx context.Context, u storage.UnitOfWork, s Scenario, set ledger.Settings, today time.Time) (storage.Document, error) {
	s.Date = today
	if err := checkedScenario(ctx, u, s, set); err != nil {
		return storage.Document{}, err
	}
	b, err := EncodeScenario(s)
	if err != nil {
		return storage.Document{}, err
	}
	return u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentRoundScenario, Payload: b})
}

// ReviseScenarioIn saves a new version of a scenario not approved yet, in u.
func ReviseScenarioIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, s Scenario, set ledger.Settings) (storage.Document, error) {
	doc, err := Drafted(ctx, u, storage.DocumentRoundScenario, id)
	if err != nil {
		return doc, err
	}
	was, err := ReadScenario(doc)
	if err != nil {
		return doc, err
	}
	s.Date = was.Date
	if err := checkedScenario(ctx, u, s, set); err != nil {
		return doc, err
	}
	b, err := EncodeScenario(s)
	if err != nil {
		return doc, err
	}
	return u.Documents().Revise(ctx, id, b)
}

// ReadyToApprove is what stops a scenario being asked for approval (FM-1): the resolutions its issues will name, and
// for a top-up a round on or before today, since a pool resolution is never dated after it.
func ReadyToApprove(s Scenario, today time.Time) error {
	var probs ledger.Problems
	if strings.TrimSpace(s.ResolutionRefs) == "" {
		probs = append(probs, ledger.Problem{Code: CodeRound, Field: "resolution_refs", Message: "the scenario names no resolution for its issues",
			Fix: "Name the shareholders' approval and the board resolution the round rests on"})
	}
	if s.TopUp != "" && s.On.After(today) {
		probs = append(probs, ledger.Problem{Code: CodeRound, Field: "on", Message: "a top-up's resolution is dated the round's day, which is after today",
			Fix: "Ask for approval on or after the round's day"})
	}
	if len(probs) > 0 {
		return probs
	}
	return nil
}

// ApproveScenarioIn approves a scenario in u (design D8): it is checked again and modelled, then each investor's issue
// for cash is drafted at the round's price with the scenario's resolutions, and a top-up drafts a pool resolution
// enlarging the pool in force on the round's day. The scenario then changes no more. The instruments' conversions are
// drafted from the round's issue once that is approved (DraftConversionsIn). It returns the drafts.
func ApproveScenarioIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, set ledger.Settings, today time.Time) ([]storage.Document, error) {
	doc, err := Drafted(ctx, u, storage.DocumentRoundScenario, id)
	if err != nil {
		return nil, err
	}
	s, err := ReadScenario(doc)
	if err != nil {
		return nil, err
	}
	if err := checkedScenario(ctx, u, s, set); err != nil {
		return nil, err
	}
	if err := ReadyToApprove(s, today); err != nil {
		return nil, err
	}
	out, err := OutcomeIn(ctx, u, s, set)
	if err != nil {
		return nil, err
	}
	var drafts []storage.Document
	note := "From the round scenario " + s.Name
	for _, issue := range out.Issues {
		d, _, err := DraftTransactionIn(ctx, u, Transaction{Kind: KindIssueCash, HolderID: issue.HolderID, ClassAccountID: s.ClassAccountID,
			Shares: issue.Shares.Text('f'), Price: out.Price.String(), ResolutionRefs: s.ResolutionRefs, Note: note, Date: today}, set)
		if err != nil {
			return nil, err
		}
		drafts = append(drafts, d)
	}
	if s.TopUp != "" {
		pool, err := PoolOn(ctx, u, s.On)
		if err != nil {
			return nil, err
		}
		var size apd.Decimal
		if _, err := exact.Add(&size, &pool.Size, &out.UnallocatedAfter); err != nil {
			return nil, err
		}
		if _, err := exact.Sub(&size, &size, &out.UnallocatedBefore); err != nil { // the size in force, with the top-up
			return nil, err
		}
		class := pool.ClassID
		if class == uuid.Nil {
			class = s.ClassAccountID
		}
		d, err := DraftPoolIn(ctx, u, PoolResolution{Size: text(&size), ClassAccountID: class, ResolutionRef: s.ResolutionRefs, ResolvedOn: s.On,
			Note: note}, set, today)
		if err != nil {
			return nil, err
		}
		drafts = append(drafts, d)
	}
	if err := u.Documents().SetStatus(ctx, id, storage.DocumentPosted); err != nil {
		return nil, err
	}
	return drafts, nil
}

// Scenarios are every round scenario, drafts and approved, oldest first.
func Scenarios(ctx context.Context, u storage.UnitOfWork) ([]storage.Document, error) {
	return all(ctx, u, storage.DocumentRoundScenario)
}
