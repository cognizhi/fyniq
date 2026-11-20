package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/consolidation"
	"github.com/cognizhi/fyniq/internal/domain/groupconsol"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the parent's consolidation (FR-ENT-05, FR-ENT-06, FR-RPT-12; p3-multi-entity design D4 to D8)
//
// The parent maps each entity's accounts to its own chart, enters its own translation rates and the elimination pairs, and runs a consolidation: it reads each linked entity's figures
// through the link (the only read of another organisation, audited in both trails), translates and eliminates them with the engine that has an oracle half, and freezes the run in its
// own books. Everything is the parent's own data; no AI read tool or MCP tool reads it.

var groupMonthPattern = regexp.MustCompile(`^[0-9]{4}-(0[1-9]|1[0-2])$`)

// ConsolidationCodes are the refusals of a run (ENG-23).
var ConsolidationCodes = []string{groupconsol.CodeUnmapped, groupconsol.CodeCounterparty, groupconsol.CodeNothing, consolidation.CodeInput, consolidation.CodeRate}

// GroupMapping is where an entity's account lands on the parent's chart.
type GroupMapping struct {
	ID                   string `json:"id" format:"uuid" doc:"The mapping's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6001"`
	EntityID             string `json:"entity_id,omitempty" format:"uuid" doc:"The entity's row in the group (its id in getEntityGroup); absent for the parent's own accounts." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5c01"`
	AccountCode          string `json:"account_code" doc:"The entity's account code." example:"2210"`
	GroupAccountID       string `json:"group_account_id" format:"uuid" doc:"The parent's account it maps to." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6002"`
	CounterpartyEntityID string `json:"counterparty_entity_id,omitempty" format:"uuid" doc:"For an intercompany account, the other entity of the group (its row's id)." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5c02"`
	CounterpartyParent   bool   `json:"counterparty_parent" doc:"For an intercompany account, the parent itself is the other side." example:"false"`
	Version              int    `json:"version" doc:"The mapping's version." example:"1"`
}

// GroupMappingInput maps an account, or maps it again.
type GroupMappingInput struct {
	EntityID             string `json:"entity_id,omitempty" format:"uuid" doc:"The entity's row in the group; leave it out for the parent's own accounts (only an intercompany account needs a mapping of its own)." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5c01"`
	AccountCode          string `json:"account_code" pattern:"^[0-9A-Za-z][0-9A-Za-z.-]{0,19}$" doc:"The account's code in the entity's chart." example:"2210"`
	GroupAccountID       string `json:"group_account_id" format:"uuid" doc:"The parent's account (not a group account) it maps to." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6002"`
	CounterpartyEntityID string `json:"counterparty_entity_id,omitempty" format:"uuid" doc:"For an intercompany account, the other entity of the group." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5c02"`
	CounterpartyParent   bool   `json:"counterparty_parent,omitempty" doc:"For an intercompany account, the parent itself is the other side." example:"false"`
}

// GroupMappingPage is a page of mappings.
type GroupMappingPage struct {
	Items      []GroupMapping `json:"items" nullable:"false" doc:"The mappings, the parent's own first, then each entity's by code."`
	NextCursor string         `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjEifQ"`
}

// GroupRate is the group's translation rates for a currency in a month.
type GroupRate struct {
	ID        string    `json:"id" format:"uuid" doc:"The entry's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c7001"`
	Currency  string    `json:"currency" doc:"The entity's currency." example:"MYR"`
	Month     string    `json:"month" doc:"The month, YYYY-MM." example:"2026-09"`
	Closing   string    `json:"closing" doc:"Group currency per one unit of the entity's currency at the month's end." example:"0.3056000000"`
	Average   string    `json:"average" doc:"The month's average rate, in the same terms." example:"0.3058000000"`
	Note      string    `json:"note" doc:"Where the rates came from, in the person's words." example:"MAS daily rates"`
	EnteredAt time.Time `json:"entered_at" doc:"When it was entered; a newer entry of the same month and currency corrects an older." example:"2026-10-02T09:00:00Z"`
}

// GroupRateInput enters a rate.
type GroupRateInput struct {
	Currency string `json:"currency" pattern:"^[A-Z]{3}$" doc:"The entity's currency." example:"MYR"`
	Month    string `json:"month" pattern:"^[0-9]{4}-(0[1-9]|1[0-2])$" doc:"The month, YYYY-MM." example:"2026-09"`
	Closing  string `json:"closing" pattern:"^[0-9]{1,10}(\\.[0-9]{1,10})?$" doc:"Group currency per one unit of the entity's currency at the month's end, up to 10 decimals; above nothing." example:"0.3056"`
	Average  string `json:"average" pattern:"^[0-9]{1,10}(\\.[0-9]{1,10})?$" doc:"The month's average, in the same terms." example:"0.3058"`
	Note     string `json:"note" minLength:"1" maxLength:"200" doc:"Where the rates came from." example:"MAS daily rates"`
}

// GroupRatePage is a page of rate entries.
type GroupRatePage struct {
	Items      []GroupRate `json:"items" nullable:"false" doc:"Every entry, newest month first."`
	NextCursor string      `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjEifQ"`
}

// GroupPair is two accounts of the parent's chart that mirror each other.
type GroupPair struct {
	A string `json:"a" format:"uuid" doc:"The account that normally holds a debit: due from, an intercompany expense, an investment." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6011"`
	B string `json:"b" format:"uuid" doc:"The account that normally holds a credit: due to, intercompany revenue, share capital." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6012"`
}

// GroupPairs are the elimination pairs.
type GroupPairs struct {
	Pairs []GroupPair `json:"pairs" nullable:"false" maxItems:"20" doc:"At most 20 pairs."`
}

// GroupEntityAccount is an account of an entity, as the link shows it.
type GroupEntityAccount struct {
	Code                  string `json:"code" doc:"The entity's account code." example:"2210"`
	Name                  string `json:"name" doc:"Its name." example:"SST payable"`
	Type                  string `json:"type" enum:"asset,liability,equity,revenue,expense" doc:"Its type." example:"liability"`
	HasFigures            bool   `json:"has_figures" doc:"true when it has a movement; only such an account needs a mapping." example:"true"`
	Mapped                bool   `json:"mapped" doc:"true when it is mapped." example:"false"`
	SuggestedGroupAccount string `json:"suggested_group_account_id,omitempty" format:"uuid" doc:"The parent's account with the same code, when it has one." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6002"`
}

// GroupEntityAccountPage is a page of an entity's accounts.
type GroupEntityAccountPage struct {
	Items      []GroupEntityAccount `json:"items" nullable:"false" doc:"The entity's accounts by code."`
	NextCursor string               `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjEifQ"`
}

// ConsolidationInput asks for a run.
type ConsolidationInput struct {
	From string `json:"from" pattern:"^[0-9]{4}-(0[1-9]|1[0-2])$" doc:"The first month the run reports, YYYY-MM." example:"2026-07"`
	To   string `json:"to" pattern:"^[0-9]{4}-(0[1-9]|1[0-2])$" doc:"The last month, YYYY-MM; the balance sheet is at its end." example:"2026-09"`
}

// ConsolidationHeader is a run in a list.
type ConsolidationHeader struct {
	ID          string    `json:"id" format:"uuid" doc:"The run's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c8001"`
	From        string    `json:"from" doc:"The first month." example:"2026-07"`
	To          string    `json:"to" doc:"The last month." example:"2026-09"`
	Currency    string    `json:"currency" doc:"The group currency." example:"SGD"`
	Provisional bool      `json:"provisional" doc:"true when an entity's period in the run was not closed when it was read." example:"false"`
	CreatedAt   time.Time `json:"created_at" doc:"When it was made." example:"2026-10-02T09:00:00Z"`
}

// ConsolidationHeaderPage is a page of runs.
type ConsolidationHeaderPage struct {
	Items      []ConsolidationHeader `json:"items" nullable:"false" doc:"The runs, newest first."`
	NextCursor string                `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjEifQ"`
}

// ConsolidationPeriod is a period's state when the run read it.
type ConsolidationPeriod struct {
	Month  string `json:"month" doc:"The month." example:"2026-09"`
	Status string `json:"status" enum:"open,soft_locked,closed" doc:"Its state." example:"closed"`
}

// ConsolidationSource is an organisation as the run read it.
type ConsolidationSource struct {
	Key         string                `json:"key" doc:"parent, or the entity's row ID." example:"parent"`
	Label       string                `json:"label" doc:"What the group calls it." example:"Cognizhi Pte Ltd"`
	Currency    string                `json:"currency" doc:"Its base currency." example:"SGD"`
	Revision    int64                 `json:"ledger_revision" doc:"Its ledger revision when read." example:"1204"`
	LinkEnded   bool                  `json:"link_ended" doc:"true when the entity's link has ended since: the figures are the parent's record of what it was shown." example:"false"`
	Periods     []ConsolidationPeriod `json:"periods" nullable:"false" doc:"Its periods' state when read."`
	Translation string                `json:"translation_reserve" doc:"Its translation reserve in the group currency." example:"-412.33"`
}

// ConsolidationLine is a translated line.
type ConsolidationLine struct {
	Entity       string `json:"entity" doc:"The source's key." example:"parent"`
	Account      string `json:"account" doc:"The account's code on the parent's chart." example:"4000"`
	Counterparty string `json:"counterparty,omitempty" doc:"For an intercompany account, the other source's key." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5c01"`
	Section      string `json:"section" enum:"balance,equity,result,brought_forward" doc:"balance: an asset or liability at the closing rate; equity: at each month's closing rate; result: the run's months at the average rates; brought_forward: earlier months' result." example:"result"`
	Amount       string `json:"amount" doc:"In the group currency, a debit positive." example:"228400.00"`
}

// ConsolidationDifference is what two entities do not agree on.
type ConsolidationDifference struct {
	DebitEntity  string `json:"debit_entity" doc:"The source holding the debit side's account." example:"parent"`
	CreditEntity string `json:"credit_entity" doc:"The source holding the credit side's account." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5c01"`
	A            string `json:"a" doc:"The debit side's account code." example:"1350"`
	B            string `json:"b" doc:"The credit side's account code." example:"2350"`
	Debit        string `json:"debit" doc:"The debit side's amount." example:"18000.00"`
	Credit       string `json:"credit" doc:"The credit side's amount, a credit negative." example:"-17639.23"`
	Matched      string `json:"matched" doc:"What was eliminated from both: the smaller side." example:"17639.23"`
	Difference   string `json:"difference" doc:"The two sides' sum: what is not agreed, left in place and plugged nowhere. Positive: the debit side is larger." example:"360.77"`
}

// ConsolidationTotal is a consolidated account in a section.
type ConsolidationTotal struct {
	Account string `json:"account" doc:"The account's code." example:"4000"`
	Section string `json:"section" enum:"balance,equity,result,brought_forward" doc:"The section." example:"result"`
	Amount  string `json:"amount" doc:"After the eliminations, in the group currency." example:"241610.40"`
}

// ConsolidationAccount names an account of the parent's chart the run uses.
type ConsolidationAccount struct {
	Code string `json:"code" doc:"The account's code." example:"4000"`
	Name string `json:"name" doc:"Its name." example:"Revenue"`
	Type string `json:"type" enum:"asset,liability,equity,revenue,expense" doc:"Its type." example:"revenue"`
}

// ConsolidationRate is a rate a run used.
type ConsolidationRate struct {
	Currency string `json:"currency" doc:"The entity's currency." example:"MYR"`
	Month    string `json:"month" doc:"The month." example:"2026-09"`
	Closing  string `json:"closing" doc:"The closing rate." example:"0.3056000000"`
	Average  string `json:"average" doc:"The average rate." example:"0.3058000000"`
	Note     string `json:"note" doc:"Its source." example:"MAS daily rates"`
}

// Consolidation is a frozen run: what was read, the translated lines, the eliminations, every difference and the consolidated figures.
type Consolidation struct {
	ConsolidationHeader
	Sources      []ConsolidationSource     `json:"sources" nullable:"false" doc:"The organisations in the run: the parent first."`
	Lines        []ConsolidationLine       `json:"lines" nullable:"false" doc:"Every translated line of every source, before the eliminations."`
	Eliminations []ConsolidationLine       `json:"eliminations" nullable:"false" doc:"What was taken off, by source."`
	Differences  []ConsolidationDifference `json:"differences" nullable:"false" doc:"Every pair the entities do not agree on; nothing is plugged."`
	Consolidated []ConsolidationTotal      `json:"consolidated" nullable:"false" doc:"The consolidated figures by account and section."`
	Reserve      string                    `json:"translation_reserve" doc:"The sum of the entities' translation reserves: the balancing figure of the translation." example:"-412.33"`
	Accounts     []ConsolidationAccount    `json:"accounts" nullable:"false" doc:"The accounts the run uses, by code."`
	Rates        []ConsolidationRate       `json:"rates" nullable:"false" doc:"The rates the run used."`
}

type (
	groupMappingsOutput struct{ Body GroupMappingPage }
	groupMappingInput   struct {
		KeyHeader
		PreferHeader
		Body GroupMappingInput
	}
	groupMappingOutput struct {
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     GroupMapping
	}
	groupRatesOutput struct{ Body GroupRatePage }
	groupRateInput   struct {
		KeyHeader
		PreferHeader
		Body GroupRateInput
	}
	groupRateOutput struct {
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     GroupRate
	}
	groupPairsOutput struct {
		ETag string `header:"ETag" doc:"The set-up's version, for If-Match."`
		Body GroupPairs
	}
	groupPairsInput struct {
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body GroupPairs
	}
	// GroupEntityPath names an entity's row in the group.
	GroupEntityPath struct {
		EntityID string `path:"entity_id" format:"uuid" doc:"The entity's row (its id in getEntityGroup)." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5c01"`
	}
	groupEntityAccountsInput struct {
		GroupEntityPath
		PageQuery
	}
	groupEntityAccountsOutput struct{ Body GroupEntityAccountPage }
	consolidationRunInput     struct {
		KeyHeader
		PreferHeader
		Body ConsolidationInput
	}
	consolidationOutput struct {
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     Consolidation
	}
	consolidationListOutput struct{ Body ConsolidationHeaderPage }
	// ConsolidationPath names a run.
	ConsolidationPath struct {
		RunID string `path:"run_id" format:"uuid" doc:"The run's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c8001"`
	}
	consolidationGetInput struct{ ConsolidationPath }
	consolidationGetOut   struct{ Body Consolidation }
)

func groupMonthOf(t time.Time) string { return t.Format("2006-01") }

func groupRateOf(r storage.GroupRate) GroupRate {
	return GroupRate{ID: r.ID.String(), Currency: r.Currency, Month: groupMonthOf(r.Month), Closing: string(r.Closing), Average: string(r.Average), Note: r.Note, EnteredAt: r.EnteredAt.UTC()}
}

func groupMappingOf(m storage.GroupMapping) GroupMapping {
	out := GroupMapping{ID: m.ID.String(), AccountCode: m.EntityAccountCode, GroupAccountID: m.GroupAccountID.String(), CounterpartyParent: m.CounterpartyParent, Version: m.Version}
	if m.EntityRow != uuid.Nil {
		out.EntityID = m.EntityRow.String()
	}
	if m.CounterpartyRow != uuid.Nil {
		out.CounterpartyEntityID = m.CounterpartyRow.String()
	}
	return out
}

func groupOptionalID(field, s string) (uuid.UUID, error) {
	if strings.TrimSpace(s) == "" {
		return uuid.Nil, nil
	}
	return parseID(field, s)
}

// parseMonth reads YYYY-MM as the month's first day.
func parseMonth(field, s string) (time.Time, error) {
	if !groupMonthPattern.MatchString(s) {
		return time.Time{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: field, Message: "is not a month as YYYY-MM", Fix: "Send the month as 2026-09"}})
	}
	t, err := time.Parse("2006-01", s)
	return t, err
}

func registerConsolidation(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	people := []string{CodeSessionRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld}
	signed := func(ctx context.Context, what string) (authn.Principal, error) { return signedIn(ctx, what) }

	register(a, authz.ListGroupMappings, huma.Operation{
		OperationID: "listGroupMappings", Method: http.MethodGet, Path: "/v1/group/mappings", Tags: []string{"Groups"},
		Summary:     "List the account mappings of the group",
		Description: "Where each entity's account lands on this organisation's chart (FR-ENT-05), the parent's own accounts first, then each entity's by code, a page at a time. Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *PageQuery) (*groupMappingsOutput, error) {
		p, _ := authn.From(ctx)
		var all []GroupMapping
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			ms, err := u.Consolidations().Mappings(ctx)
			for _, m := range ms {
				all = append(all, groupMappingOf(m))
			}
			return err
		})
		if err != nil {
			return nil, err
		}
		items, next, err := pageOf(all, func(m GroupMapping) string { return m.ID }, *in)
		if items == nil {
			items = []GroupMapping{}
		}
		return &groupMappingsOutput{Body: GroupMappingPage{Items: items, NextCursor: next}}, err
	})

	register(a, authz.MapGroupAccount, huma.Operation{
		OperationID: "mapGroupAccount", Method: http.MethodPost, Path: "/v1/group/mappings", Tags: []string{"Groups"}, DefaultStatus: http.StatusOK,
		Summary: "Map an account of an entity to this organisation's chart",
		Description: "Maps an entity's account code to an account of this organisation's chart, or maps it again as the account's next version (FR-ENT-05). An intercompany account names the other entity (or this " +
			"organisation) as its counterparty. The entity must be one of this group's rows, and the account a leaf of this chart. Side effects: the mapping and its audit event. Idempotent with Idempotency-Key; " +
			"Prefer: return=preview checks it and saves nothing.",
	}, append(append([]string{CodeNotFound}, people...), append([]string{storage.CodeGroup}, locks...)...), func(ctx context.Context, in *groupMappingInput) (*groupMappingOutput, error) {
		p, err := signed(ctx, "Mapping an account")
		if err != nil {
			return nil, err
		}
		entity, err := groupOptionalID("body.entity_id", in.Body.EntityID)
		if err != nil {
			return nil, err
		}
		account, err := parseID("group_account_id", in.Body.GroupAccountID)
		if err != nil {
			return nil, err
		}
		cp, err := groupOptionalID("body.counterparty_entity_id", in.Body.CounterpartyEntityID)
		if err != nil {
			return nil, err
		}
		if cp != uuid.Nil && in.Body.CounterpartyParent {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.counterparty_parent", Message: "cannot be set with a counterparty entity", Fix: "Name one counterparty"}})
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (GroupMapping, error) {
			rows, err := u.EntityGroups().Entities(ctx)
			if err != nil {
				return GroupMapping{}, err
			}
			known := map[uuid.UUID]bool{}
			for _, r := range rows {
				known[r.ID] = r.State != "invited"
			}
			if (entity != uuid.Nil && !known[entity]) || (cp != uuid.Nil && !known[cp]) {
				return GroupMapping{}, problem(CodeNotFound, "No entity of this group has that ID.", "Read the group to find its entities' IDs")
			}
			acc, err := u.Accounts().Get(ctx, account)
			if errors.Is(err, ports.ErrNotFound) || (err == nil && acc.Group) {
				return GroupMapping{}, problem(CodeNotFound, "No account of this chart with that ID takes figures.", "Map to a leaf account of the chart")
			}
			if err != nil {
				return GroupMapping{}, err
			}
			m, err := u.Consolidations().Map(ctx, storage.NewGroupMapping{EntityRow: entity, EntityAccountCode: strings.TrimSpace(in.Body.AccountCode), GroupAccountID: account,
				CounterpartyRow: cp, CounterpartyParent: in.Body.CounterpartyParent, By: p.UserID, At: a.deps.Clock.Now()})
			return groupMappingOf(m), err
		})
		if err != nil {
			return nil, err
		}
		return &groupMappingOutput{Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})

	register(a, authz.ListGroupRates, huma.Operation{
		OperationID: "listGroupRates", Method: http.MethodGet, Path: "/v1/group/rates", Tags: []string{"Groups"},
		Summary: "List the group's translation rates",
		Description: "The closing and average rate of each month and currency this organisation entered for translating its entities (FR-ENT-05), newest month first; a newer entry of a month and currency " +
			"corrects an older, and every entry is kept. Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *PageQuery) (*groupRatesOutput, error) {
		p, _ := authn.From(ctx)
		var all []GroupRate
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			rs, err := u.Consolidations().Rates(ctx)
			for _, r := range rs {
				all = append(all, groupRateOf(r))
			}
			return err
		})
		if err != nil {
			return nil, err
		}
		items, next, err := pageOf(all, func(r GroupRate) string { return r.ID }, *in)
		if items == nil {
			items = []GroupRate{}
		}
		return &groupRatesOutput{Body: GroupRatePage{Items: items, NextCursor: next}}, err
	})

	register(a, authz.AddGroupRate, huma.Operation{
		OperationID: "addGroupRate", Method: http.MethodPost, Path: "/v1/group/rates", Tags: []string{"Groups"}, DefaultStatus: http.StatusCreated,
		Summary: "Enter the closing and average rate of a month",
		Description: "Enters this group's own translation rates for an entity's currency in a month: group currency per one unit of the currency, up to 10 decimals, above nothing, with where they came from " +
			"(FR-ENT-05). Never the entity's own FX rates. A newer entry corrects an older; nothing is overwritten. Side effects: the entry and its audit event. Idempotent with Idempotency-Key; " +
			"Prefer: return=preview checks it and saves nothing.",
	}, append(append([]string{}, people...), locks...), func(ctx context.Context, in *groupRateInput) (*groupRateOutput, error) {
		p, err := signed(ctx, "Entering a translation rate")
		if err != nil {
			return nil, err
		}
		month, err := parseMonth("body.month", in.Body.Month)
		if err != nil {
			return nil, err
		}
		note := strings.TrimSpace(in.Body.Note)
		if n := utf8.RuneCountInString(note); n < 1 || n > 200 {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.note", Message: "must be 1 to 200 characters", Fix: "Say where the rates came from"}})
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (GroupRate, error) {
			org, err := u.Organisation(ctx)
			if err != nil {
				return GroupRate{}, err
			}
			group, err := money.Lookup(org.BaseCurrency)
			if err != nil {
				return GroupRate{}, err
			}
			cur, err := money.Lookup(in.Body.Currency)
			if err != nil || cur == group {
				return GroupRate{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.currency", Message: "is not a currency Fyniq knows other than the group's own", Fix: "Enter the rates of an entity's currency, which differs from " + group.Code}})
			}
			for field, v := range map[string]string{"body.closing": in.Body.Closing, "body.average": in.Body.Average} {
				if _, err := money.ParseRate(v, cur, group, note); err != nil {
					return GroupRate{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: field, Message: "is not a rate: up to 10 decimals, above nothing", Fix: "Enter group currency per one unit of " + cur.Code}})
				}
			}
			r, err := u.Consolidations().AddRate(ctx, storage.NewGroupRate{Currency: cur.Code, Month: month, Closing: storage.Decimal(in.Body.Closing), Average: storage.Decimal(in.Body.Average),
				Note: note, By: p.UserID, At: a.deps.Clock.Now()})
			return groupRateOf(r), err
		})
		if err != nil {
			return nil, err
		}
		return &groupRateOutput{Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})

	register(a, authz.GetGroupPairs, huma.Operation{
		OperationID: "getGroupPairs", Method: http.MethodGet, Path: "/v1/group/pairs", Tags: []string{"Groups"},
		Summary: "Read the elimination pairs",
		Description: "The pairs of accounts of this organisation's chart that mirror each other, which a consolidation eliminates entity by entity (FR-ENT-06): due from and due to, intercompany revenue and expense, " +
			"the parent's investment and an entity's share capital. The ETag is the set's version. Read-only; no side effects.",
	}, locks, func(ctx context.Context, in *struct{}) (*groupPairsOutput, error) {
		p, _ := authn.From(ctx)
		out := &groupPairsOutput{Body: GroupPairs{Pairs: []GroupPair{}}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			ps, v, err := u.Consolidations().Pairs(ctx)
			for _, x := range ps {
				out.Body.Pairs = append(out.Body.Pairs, GroupPair{A: x.A.String(), B: x.B.String()})
			}
			out.ETag = etag(v)
			return err
		})
		return out, err
	})

	register(a, authz.SetGroupPairs, huma.Operation{
		OperationID: "setGroupPairs", Method: http.MethodPut, Path: "/v1/group/pairs", Tags: []string{"Groups"},
		Summary: "Set the elimination pairs",
		Description: "Replaces the pairs of mirror accounts over the version you read (If-Match; \"0\" when none was ever set): at most 20, each account a leaf of this chart and used once as a debit side " +
			"or a credit side (FR-ENT-06). Side effects: the new version and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append(append([]string{CodeNotFound, CodeIfMatchRequired, CodeStale, storage.CodeVersion}, people...), locks...), func(ctx context.Context, in *groupPairsInput) (*groupPairsOutput, error) {
		p, err := signed(ctx, "Setting the elimination pairs")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		pairs := make([]storage.GroupPair, len(in.Body.Pairs))
		seen := map[uuid.UUID]bool{}
		for i, x := range in.Body.Pairs {
			ida, err := parseID("body.pairs.a", x.A)
			if err != nil {
				return nil, err
			}
			idb, err := parseID("body.pairs.b", x.B)
			if err != nil {
				return nil, err
			}
			if ida == idb || seen[ida] || seen[idb] {
				return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.pairs", Message: "an account is in one pair only, and a pair names two accounts", Fix: "Give each account one place"}})
			}
			seen[ida], seen[idb] = true, true
			pairs[i] = storage.GroupPair{A: ida, B: idb}
		}
		newVersion := 0
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (GroupPairs, error) {
			_, version, err := u.Consolidations().Pairs(ctx)
			if err != nil {
				return GroupPairs{}, err
			}
			if err := checkIfMatch(in.IfMatch, etag(version)); err != nil {
				return GroupPairs{}, err
			}
			for _, x := range pairs {
				for _, id := range []uuid.UUID{x.A, x.B} {
					acc, err := u.Accounts().Get(ctx, id)
					if errors.Is(err, ports.ErrNotFound) || (err == nil && acc.Group) {
						return GroupPairs{}, problem(CodeNotFound, "A pair names an account that takes no figures in this chart.", "Use leaf accounts of this chart")
					}
					if err != nil {
						return GroupPairs{}, err
					}
				}
			}
			if err := u.Consolidations().SetPairs(ctx, pairs, version, p.UserID, a.deps.Clock.Now()); err != nil {
				return GroupPairs{}, err
			}
			newVersion = version + 1
			out := GroupPairs{Pairs: []GroupPair{}}
			for _, x := range pairs {
				out.Pairs = append(out.Pairs, GroupPair{A: x.A.String(), B: x.B.String()})
			}
			return out, nil
		})
		if err != nil {
			return nil, err
		}
		return &groupPairsOutput{ETag: etag(newVersion), Body: w.Body}, nil
	})

	register(a, authz.ListGroupEntityAccounts, huma.Operation{
		OperationID: "listGroupEntityAccounts", Method: http.MethodGet, Path: "/v1/group/entities/{entity_id}/accounts", Tags: []string{"Groups"},
		Summary: "List an entity's accounts, to map them",
		Description: "Reads the accounts of a linked entity through the link, with whether each has figures and is mapped, and the account of this chart with the same code as a suggestion (FR-ENT-05). It is a read of " +
			"another organisation under its consent, so it is in both organisations' audit trails. Refused (STORAGE-GROUP-03) when no live link with that entity exists. Read-only apart from the audit event.",
	}, append([]string{CodeNotFound, CodeCursor, storage.CodeGroupLink}, locks...), func(ctx context.Context, in *groupEntityAccountsInput) (*groupEntityAccountsOutput, error) {
		p, err := signed(ctx, "Reading an entity's accounts")
		if err != nil {
			return nil, err
		}
		row, err := parseID("entity_id", in.EntityID)
		if err != nil {
			return nil, err
		}
		var all []GroupEntityAccount
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			rows, err := u.EntityGroups().Entities(ctx)
			if err != nil {
				return err
			}
			var entity storage.GroupEntity
			for _, r := range rows {
				if r.ID == row {
					entity = r
				}
			}
			if entity.ID == uuid.Nil || entity.State != "active" {
				return problem(CodeNotFound, "No active entity of this group has that ID.", "Read the group to find its entities")
			}
			through := a.deps.Clock.Now().UTC()
			fig, err := u.EntityGroups().Figures(ctx, entity.EntityOrg, time.Date(through.Year(), through.Month(), 1, 0, 0, 0, 0, time.UTC), p.UserID)
			if err != nil {
				return err
			}
			chart, err := u.Accounts().List(ctx)
			if err != nil {
				return err
			}
			byCode := map[string]storage.Account{}
			for _, c := range chart {
				if !c.Group {
					byCode[c.Code] = c
				}
			}
			maps, err := u.Consolidations().Mappings(ctx)
			if err != nil {
				return err
			}
			mapped := map[string]bool{}
			for _, m := range maps {
				if m.EntityRow == row {
					mapped[m.EntityAccountCode] = true
				}
			}
			seen := map[string]*GroupEntityAccount{}
			for _, l := range fig.Lines {
				x, ok := seen[l.Code]
				if !ok {
					x = &GroupEntityAccount{Code: l.Code, Name: l.Name, Type: l.Type, Mapped: mapped[l.Code]}
					if s, ok := byCode[l.Code]; ok {
						x.SuggestedGroupAccount = s.ID.String()
					}
					seen[l.Code] = x
					all = append(all, GroupEntityAccount{})
				}
				if v := strings.TrimLeft(string(l.Net), "-0."); v != "" {
					x.HasFigures = true
				}
			}
			all = all[:0]
			codes := make([]string, 0, len(seen))
			for c := range seen {
				codes = append(codes, c)
			}
			sort.Strings(codes)
			for _, c := range codes {
				all = append(all, *seen[c])
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		items, next, err := pageOf(all, func(x GroupEntityAccount) string { return x.Code }, in.PageQuery)
		if items == nil {
			items = []GroupEntityAccount{}
		}
		return &groupEntityAccountsOutput{Body: GroupEntityAccountPage{Items: items, NextCursor: next}}, err
	})

	register(a, authz.RunConsolidation, huma.Operation{
		OperationID: "runConsolidation", Method: http.MethodPost, Path: "/v1/group/consolidations", Tags: []string{"Groups"}, DefaultStatus: http.StatusCreated,
		Summary: "Run a consolidation of the group",
		Description: "Reads each linked entity's figures through its link (audited in both organisations' trails), translates them into this organisation's currency with the group's own rates (RND-TRANSLATE; " +
			"assets and liabilities at the closing rate of the last month, revenue and expense at each month's average, equity at each month's closing rate, the translation reserve as the balancing figure), " +
			"eliminates the mirror accounts of the pairs, and freezes the run (FR-ENT-06, FR-RPT-12). The smaller side of a pair is eliminated and the rest stays, listed as a difference; nothing is plugged and " +
			"nothing is posted to any ledger. An account with figures and no mapping, a counterparty that is no entity of the run and a month with no rate refuse the run and name what to fix. A run that read a period " +
			"not yet closed is provisional. Side effects: the run and audit events. Idempotent with Idempotency-Key; Prefer: return=preview does everything but keep the run.",
	}, append(append(append([]string{CodeNotFound}, people...), ConsolidationCodes...), append([]string{storage.CodeGroupLink}, locks...)...), func(ctx context.Context, in *consolidationRunInput) (*consolidationOutput, error) {
		p, err := signed(ctx, "Running a consolidation")
		if err != nil {
			return nil, err
		}
		from, err := parseMonth("body.from", in.Body.From)
		if err != nil {
			return nil, err
		}
		to, err := parseMonth("body.to", in.Body.To)
		if err != nil {
			return nil, err
		}
		if from.After(to) || to.After(a.deps.Clock.Now().UTC().AddDate(0, 1, 0)) {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.from", Message: "the first month is after the last, or the last is in the future", Fix: "Give the run's first and last month, first not after last"}})
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (Consolidation, error) {
			return a.runConsolidation(ctx, u, p, from, to)
		})
		if err != nil {
			return nil, err
		}
		return &consolidationOutput{Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})

	register(a, authz.ListConsolidations, huma.Operation{
		OperationID: "listConsolidations", Method: http.MethodGet, Path: "/v1/group/consolidations", Tags: []string{"Groups"},
		Summary:     "List the consolidation runs",
		Description: "The frozen runs of this group, newest first, a page at a time, without their figures (FR-RPT-12). Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *PageQuery) (*consolidationListOutput, error) {
		p, _ := authn.From(ctx)
		var all []ConsolidationHeader
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			rs, err := u.Consolidations().Runs(ctx)
			for _, r := range rs {
				all = append(all, headerOf(r))
			}
			return err
		})
		if err != nil {
			return nil, err
		}
		items, next, err := pageOf(all, func(h ConsolidationHeader) string { return h.ID }, *in)
		if items == nil {
			items = []ConsolidationHeader{}
		}
		return &consolidationListOutput{Body: ConsolidationHeaderPage{Items: items, NextCursor: next}}, err
	})

	register(a, authz.GetConsolidation, huma.Operation{
		OperationID: "getConsolidation", Method: http.MethodGet, Path: "/v1/group/consolidations/{run_id}", Tags: []string{"Groups"},
		Summary: "Read a consolidation run",
		Description: "A frozen run with what it read, every translated line, the eliminations, every difference and the consolidated figures (FR-RPT-12). The figures are the parent's record of what it was shown " +
			"while each link stood; a run whose entity's link has ended says so. Read-only; no side effects.",
	}, append([]string{CodeNotFound}, locks...), func(ctx context.Context, in *consolidationGetInput) (*consolidationGetOut, error) {
		p, _ := authn.From(ctx)
		id, err := parseID("run_id", in.RunID)
		if err != nil {
			return nil, err
		}
		var out Consolidation
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			r, err := u.Consolidations().Run(ctx, id)
			if errors.Is(err, ports.ErrNotFound) {
				return problem(CodeNotFound, "No consolidation run with that ID exists.", "List the runs to find their IDs")
			}
			if err != nil {
				return err
			}
			out, err = a.consolidationOf(ctx, u, r)
			return err
		})
		return &consolidationGetOut{Body: out}, err
	})
}

func headerOf(r storage.ConsolidationRun) ConsolidationHeader {
	return ConsolidationHeader{ID: r.ID.String(), From: groupMonthOf(r.From), To: groupMonthOf(r.To), Currency: r.Currency, Provisional: r.Provisional, CreatedAt: r.CreatedAt.UTC()}
}

// runConsolidation reads, translates, eliminates and keeps a run in the parent's unit of work.
func (a *API) runConsolidation(ctx context.Context, u storage.UnitOfWork, p authn.Principal, from, to time.Time) (Consolidation, error) {
	org, err := u.Organisation(ctx)
	if err != nil {
		return Consolidation{}, err
	}
	group, err := money.Lookup(org.BaseCurrency)
	if err != nil {
		return Consolidation{}, err
	}
	chartRows, err := u.Accounts().List(ctx)
	if err != nil {
		return Consolidation{}, err
	}
	var chart []groupconsol.Account
	typeOf := map[uuid.UUID]storage.Account{}
	for _, c := range chartRows {
		typeOf[c.ID] = c
		if !c.Group {
			chart = append(chart, groupconsol.Account{ID: c.ID, Code: c.Code, Name: c.Name, Type: string(c.Type)})
		}
	}
	fromKey, toKey := groupMonthOf(from), groupMonthOf(to)
	// The parent's own figures, through the balance service, month by month.
	periods, err := u.Periods().List(ctx)
	if err != nil {
		return Consolidation{}, err
	}
	own := groupconsol.Source{Key: groupconsol.ParentKey, Label: org.Name, Currency: group}
	for _, per := range periods {
		month := time.Date(per.StartsOn.Year(), per.StartsOn.Month(), 1, 0, 0, 0, 0, time.UTC)
		if groupMonthOf(month) > toKey {
			continue
		}
		own.Periods = append(own.Periods, storage.EntityPeriod{Month: month, Status: string(per.Status)})
		tb, err := balance.Trial(ctx, u, group, per.ID)
		if err != nil {
			return Consolidation{}, err
		}
		for _, r := range tb.Rows {
			acc := typeOf[r.AccountID]
			own.Lines = append(own.Lines, storage.EntityFigureLine{Code: acc.Code, Name: acc.Name, Type: string(acc.Type), Currency: group.Code, Month: month, Net: storage.Decimal(r.Base.String())})
		}
	}
	sources := []groupconsol.Source{own}
	rows, err := u.EntityGroups().Entities(ctx)
	if err != nil {
		return Consolidation{}, err
	}
	for _, r := range rows {
		if r.State != "active" {
			continue
		}
		fig, err := u.EntityGroups().Figures(ctx, r.EntityOrg, to, p.UserID)
		if err != nil {
			return Consolidation{}, err
		}
		cur, err := money.Lookup(r.EntityCurrency)
		if err != nil {
			return Consolidation{}, err
		}
		sources = append(sources, groupconsol.Source{Key: r.ID.String(), Label: r.Label, Row: r.ID, Currency: cur, Revision: fig.Revision, Periods: fig.Periods, Lines: fig.Lines})
	}
	maps, err := u.Consolidations().Mappings(ctx)
	if err != nil {
		return Consolidation{}, err
	}
	rates, err := u.Consolidations().Rates(ctx)
	if err != nil {
		return Consolidation{}, err
	}
	pairs, _, err := u.Consolidations().Pairs(ctx)
	if err != nil {
		return Consolidation{}, err
	}
	prepared, err := groupconsol.Prepare(group, fromKey, toKey, sources, groupconsol.Setup{Chart: chart, Mappings: maps, Rates: rates, Pairs: pairs})
	if err != nil {
		return Consolidation{}, err
	}
	if len(prepared.Problems) > 0 {
		fields := make([]FieldProblem, len(prepared.Problems))
		for i, pr := range prepared.Problems {
			fields[i] = FieldProblem{Code: pr.Code, Field: strings.TrimSpace(pr.Entity + " " + pr.Account), Message: pr.Message, Fix: pr.Fix}
		}
		first := prepared.Problems[0]
		return Consolidation{}, problem(first.Code, "The consolidation cannot be run: "+strings.TrimSpace(first.Entity+" "+first.Account)+" "+first.Message+".", first.Fix, fields...)
	}
	res, err := consolidation.Work(prepared.Input)
	if err != nil {
		var ce *consolidation.Error
		if errors.As(err, &ce) {
			return Consolidation{}, problem(ce.Code, "The consolidation cannot be run: "+ce.Message+".", ce.Fix)
		}
		return Consolidation{}, err
	}
	inputs, result, err := groupconsol.Freeze(prepared, res)
	if err != nil {
		return Consolidation{}, err
	}
	run, err := u.Consolidations().SaveRun(ctx, storage.NewConsolidationRun{From: from, To: to, Currency: group.Code, Provisional: prepared.Provisional, Inputs: inputs, Result: result, By: p.UserID, At: a.deps.Clock.Now()})
	if err != nil {
		return Consolidation{}, err
	}
	return a.consolidationOf(ctx, u, run)
}

// consolidationOf reads a frozen run's documents back as the API's answer, with each source's link state and the names of the accounts it uses.
func (a *API) consolidationOf(ctx context.Context, u storage.UnitOfWork, r storage.ConsolidationRun) (Consolidation, error) {
	var in groupconsol.Inputs
	var res groupconsol.Result
	if err := json.Unmarshal(r.Inputs, &in); err != nil {
		return Consolidation{}, err
	}
	if err := json.Unmarshal(r.Result, &res); err != nil {
		return Consolidation{}, err
	}
	out := Consolidation{ConsolidationHeader: headerOf(r), Sources: []ConsolidationSource{}, Lines: []ConsolidationLine{}, Eliminations: []ConsolidationLine{}, Differences: []ConsolidationDifference{},
		Consolidated: []ConsolidationTotal{}, Reserve: res.Reserve, Accounts: []ConsolidationAccount{}, Rates: []ConsolidationRate{}}
	rows, err := u.EntityGroups().Entities(ctx)
	if err != nil {
		return out, err
	}
	ended := map[string]bool{}
	for _, e := range rows {
		ended[e.ID.String()] = e.State == "ended"
	}
	reserve := map[string]string{}
	for _, e := range res.Entities {
		reserve[e.Key] = e.Reserve
		for _, l := range e.Lines {
			out.Lines = append(out.Lines, ConsolidationLine{Entity: e.Key, Account: l.Account, Counterparty: l.Counterparty, Section: l.Section, Amount: l.Amount})
		}
	}
	for _, s := range in.Sources {
		src := ConsolidationSource{Key: s.Key, Label: s.Label, Currency: s.Currency, Revision: s.Revision, LinkEnded: ended[s.Key], Periods: []ConsolidationPeriod{}, Translation: reserve[s.Key]}
		for _, p := range s.Periods {
			src.Periods = append(src.Periods, ConsolidationPeriod{Month: p.Month, Status: p.Status})
		}
		out.Sources = append(out.Sources, src)
	}
	for _, e := range res.Eliminations {
		out.Eliminations = append(out.Eliminations, ConsolidationLine{Entity: e.Entity, Account: e.Account, Counterparty: e.Counterparty, Section: e.Section, Amount: e.Amount})
	}
	for _, d := range res.Differences {
		out.Differences = append(out.Differences, ConsolidationDifference{DebitEntity: d.DebitEntity, CreditEntity: d.CreditEntity, A: d.A, B: d.B, Debit: d.Debit, Credit: d.Credit, Matched: d.Matched, Difference: d.Difference})
	}
	for _, t := range res.Consolidated {
		out.Consolidated = append(out.Consolidated, ConsolidationTotal{Account: t.Account, Section: t.Section, Amount: t.Amount})
	}
	for _, x := range in.Rates {
		out.Rates = append(out.Rates, ConsolidationRate{Currency: x.Currency, Month: x.Month, Closing: x.Closing, Average: x.Average, Note: x.Note})
	}
	chart, err := u.Accounts().List(ctx)
	if err != nil {
		return out, err
	}
	used := map[string]bool{}
	for _, l := range out.Lines {
		used[l.Account] = true
	}
	for _, c := range chart {
		if used[c.Code] {
			out.Accounts = append(out.Accounts, ConsolidationAccount{Code: c.Code, Name: c.Name, Type: string(c.Type)})
		}
	}
	return out, nil
}
