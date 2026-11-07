package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/equity"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// RoundCodes are the codes a round scenario answers besides the common ones.
var RoundCodes = []string{equity.CodeRound, equity.CodeInstrument, equity.CodeRecorded, storage.CodeRoundScenario}

// RoundInvestorDraft is an investor's money in a round scenario.
type RoundInvestorDraft struct {
	HolderID string `json:"holder_id" format:"uuid" doc:"The investor: a contact marked investor or shareholder; an existing holder taking up their pro-rata right too." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c11"`
	Amount   string `json:"amount" pattern:"^[0-9]{1,14}(\\.[0-9]{1,2})?$" doc:"The money they put in, in the base currency." example:"600000.00"`
}

// InstrumentRef is a recorded SAFE or note named by its ID.
type InstrumentRef string

// Schema implements huma.SchemaProvider: a UUID string, with an example.
func (InstrumentRef) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Format: "uuid", Description: "A recorded SAFE or note (listInstruments).", Examples: []any{"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6a01"}}
}

// RoundScenarioDraft is a round scenario's inputs (FR-EQ-21, design D8).
type RoundScenarioDraft struct {
	Name           string               `json:"name" maxLength:"200" doc:"What the scenario is called, to tell it from the others side by side." example:"Series A at S$3.00"`
	On             string               `json:"on" format:"date" doc:"The day the round would take effect: the cap table at the end of the day before it is the one it dilutes." example:"2027-03-12"`
	Price          string               `json:"price,omitempty" pattern:"^([0-9]{1,10}(\\.[0-9]{1,6})?)?$" doc:"The price per share, in the base currency; or leave it out and give pre_money." example:"3.00"`
	PreMoney       string               `json:"pre_money,omitempty" pattern:"^([0-9]{1,14}(\\.[0-9]{1,2})?)?$" doc:"A pre-money valuation, divided by the shares fully diluted before the round (with a top-up before the money) and cut down to price_decimals [accountant to confirm what the terms count]." example:"3000000"`
	PriceDecimals  int                  `json:"price_decimals" minimum:"0" maximum:"6" doc:"The decimals a price from pre_money is cut down to." example:"4"`
	ClassAccountID string               `json:"class_account_id" format:"uuid" doc:"The share-capital account of the class the round issues." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d30"`
	Investors      []RoundInvestorDraft `json:"investors" nullable:"false" maxItems:"50" doc:"Each investor's money; RND-SHARE allots each the whole shares it buys."`
	TopUp          string               `json:"top_up,omitempty" pattern:"^([0-9]{1,14})?$" doc:"Shares added to the option pool with the round; left out for none." example:"100000"`
	TopUpBefore    bool                 `json:"top_up_before,omitempty" doc:"The top-up is before the money: it counts in the shares the pre-money valuation is of, so the existing holders bear it." example:"true"`
	Instruments    []InstrumentRef      `json:"instruments" nullable:"false" maxItems:"50" doc:"The recorded SAFEs and notes converting at the round (listInstruments), each at its own terms over the shares issued before it (design D6)."`
	ResolutionRefs string               `json:"resolution_refs,omitempty" maxLength:"500" doc:"The resolutions the round's issues will name: needed to ask for approval." example:"Board resolution 5 Mar 2027; shareholders' resolution 5 Mar 2027"`
	Note           string               `json:"note,omitempty" maxLength:"1000" doc:"A note." example:"Term sheet of 1 Mar 2027"`
}

// RoundIssueView is an investor's allotment in the modelled round.
type RoundIssueView struct {
	HolderID   string `json:"holder_id" format:"uuid" doc:"The investor." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c11"`
	HolderName string `json:"holder_name" doc:"Their name." example:"Seedling Ventures Pte. Ltd."`
	Shares     string `json:"shares" doc:"The whole shares their money buys (RND-SHARE)." example:"200000"`
	Amount     Money  `json:"amount" doc:"What those shares cost: round(shares × price)."`
	Left       Money  `json:"left" doc:"What of their money buys no whole share."`
}

// RoundConversionView is an instrument's conversion in the modelled round.
type RoundConversionView struct {
	InstrumentID string `json:"instrument_id" format:"uuid" doc:"The SAFE or note." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6a01"`
	HolderName   string `json:"holder_name" doc:"Its holder." example:"Hatch Capital Pte. Ltd."`
	Price        string `json:"price" doc:"Its conversion price, the lower of its cap price and the discounted round price, cut down to its decimals." example:"2.40"`
	Shares       string `json:"shares" doc:"The whole shares it converts into (RND-SHARE)." example:"52083"`
	Converted    Money  `json:"converted" doc:"What converts: round(shares × price)."`
	Left         Money  `json:"left" doc:"What stays on 2700 until it is settled under its terms."`
	Review       string `json:"review,omitempty" doc:"What the accountant completes in its draft, when no enabled template covers it (⚠)." example:"the accountant completes it: it follows no template, a discount"`
}

// RoundHolderView is a holder before and after the modelled round.
type RoundHolderView struct {
	HolderID           string `json:"holder_id" format:"uuid" doc:"The holder." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c11"`
	HolderName         string `json:"holder_name" doc:"Their name." example:"Ah Kow"`
	IssuedBefore       string `json:"issued_before" doc:"Shares held the day before the round." example:"1000000"`
	IssuedAfter        string `json:"issued_after" doc:"Shares held after it." example:"1000000"`
	DilutedBefore      string `json:"fully_diluted_before" doc:"Shares and options fully diluted before." example:"1000000"`
	DilutedAfter       string `json:"fully_diluted_after" doc:"Fully diluted after." example:"1000000"`
	OwnershipBefore    string `json:"ownership_before,omitempty" doc:"Percentage of the shares issued before (RND-DISPLAY); left out when none is." example:"100.00"`
	OwnershipAfter     string `json:"ownership_after,omitempty" doc:"Percentage of the shares issued after." example:"79.87"`
	DilutedShareBefore string `json:"diluted_before,omitempty" doc:"Percentage fully diluted before." example:"100.00"`
	DilutedShareAfter  string `json:"diluted_after,omitempty" doc:"Percentage fully diluted after." example:"79.87"`
	Dilution           string `json:"dilution,omitempty" doc:"Percentage points fully diluted lost: before less after, exact, shown to 2 decimals; negative for a gain." example:"20.13"`
	ProRata            string `json:"pro_rata" doc:"What their pro-rata right entitles them to of the round's new shares: the new shares × their part fully diluted before, rounded down [accountant to confirm against the side letter]." example:"252083"`
}

// RoundOutcomeView is a scenario's outcome, computed on read (FR-EQ-21): never stored.
type RoundOutcomeView struct {
	Price             string                `json:"price" doc:"The price per share." example:"3.00"`
	Currency          string                `json:"currency" doc:"The base currency." example:"SGD"`
	PreMoney          Money                 `json:"pre_money" doc:"The price × the shares fully diluted before the round (with a top-up before the money)."`
	PostMoney         Money                 `json:"post_money" doc:"The price × the shares fully diluted after it."`
	Issues            []RoundIssueView      `json:"issues" nullable:"false" doc:"Each investor's allotment."`
	Conversions       []RoundConversionView `json:"conversions" nullable:"false" doc:"Each instrument's conversion."`
	Holders           []RoundHolderView     `json:"holders" nullable:"false" doc:"Each holder before and after, by name."`
	IssuedBefore      string                `json:"issued_before" example:"1000000"`
	IssuedAfter       string                `json:"issued_after" example:"1252083"`
	DilutedBefore     string                `json:"fully_diluted_before" example:"1000000"`
	DilutedAfter      string                `json:"fully_diluted_after" example:"1252083"`
	UnallocatedBefore string                `json:"unallocated_before" doc:"What the option pool leaves to grant before." example:"0"`
	UnallocatedAfter  string                `json:"unallocated_after" doc:"And after, with the top-up." example:"0"`
	NewShares         string                `json:"new_shares" doc:"Every share the round issues: the investors' and the conversions'." example:"252083"`
}

// RoundScenarioView is a round scenario with its outcome.
type RoundScenarioView struct {
	RoundScenarioDraft
	ID         string            `json:"id" format:"uuid" doc:"The scenario." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c7a01"`
	Status     string            `json:"status" enum:"draft,approved" doc:"draft: changed freely; approved: its issues are drafted, and it changes no more." example:"draft"`
	Version    int               `json:"version" doc:"The version of its inputs; with its status it makes the ETag." example:"1"`
	Outcome    *RoundOutcomeView `json:"outcome,omitempty" doc:"What it gives against today's books; left out when it cannot be worked out, with problems saying why."`
	Problems   []FieldProblem    `json:"problems" nullable:"false" doc:"Why the outcome cannot be worked out, such as an instrument converted since."`
	ApprovalID string            `json:"approval_id,omitempty" format:"uuid" doc:"The approval request waiting, if one is." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c7a11"`
	ApprovedBy *Person           `json:"approved_by,omitempty" doc:"Who approved it."`
	ApprovedAt *time.Time        `json:"approved_at,omitempty" doc:"When." example:"2027-03-06T09:30:00Z"`
}

// RoundScenarioPage is a page of scenarios, the latest first: side by side.
type RoundScenarioPage struct {
	Items      []RoundScenarioView `json:"items" nullable:"false"`
	NextCursor string              `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjIwMjcifQ"`
}

type (
	RoundScenarioPath struct {
		ScenarioID string `path:"scenario_id" format:"uuid" doc:"The round scenario." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c7a01"`
	}
	scenarioListInput  struct{ PageQuery }
	scenarioListOutput struct{ Body RoundScenarioPage }
	scenarioDraftInput struct {
		KeyHeader
		PreferHeader
		Body RoundScenarioDraft
	}
	scenarioReviseInput struct {
		RoundScenarioPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body RoundScenarioDraft
	}
	scenarioAskInput struct {
		RoundScenarioPath
		KeyHeader
		IfMatchHeader
		PreferHeader
	}
	scenarioReadOutput struct {
		ETag string `header:"ETag" doc:"Its version and status, for If-Match."`
		Body RoundScenarioView
	}
	scenarioWriteOutput struct {
		Status   int
		ETag     string `header:"ETag" doc:"Its version and status, for If-Match."`
		Location string `header:"Location" doc:"Its URL."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     RoundScenarioView
	}
)

// scenarioIn reads a scenario's fields, or what of them is wrong.
func scenarioIn(d RoundScenarioDraft) (equity.Scenario, error) {
	s := equity.Scenario{Name: strings.TrimSpace(d.Name), Price: strings.TrimSpace(d.Price), PreMoney: strings.TrimSpace(d.PreMoney),
		PriceDecimals: d.PriceDecimals, TopUp: strings.TrimSpace(d.TopUp), TopUpBefore: d.TopUpBefore,
		ResolutionRefs: strings.TrimSpace(d.ResolutionRefs), Note: strings.TrimSpace(d.Note)}
	var fields []FieldProblem
	var err error
	if s.On, err = time.Parse(time.DateOnly, d.On); err != nil {
		fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.on", Message: "is not a date", Fix: "Send the day as YYYY-MM-DD"})
	}
	if s.ClassAccountID, err = uuid.Parse(d.ClassAccountID); err != nil {
		fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.class_account_id", Message: "is not an ID", Fix: "Send the share-capital account's ID"})
	}
	for k, i := range d.Investors {
		id, err := uuid.Parse(i.HolderID)
		if err != nil {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: fmt.Sprintf("body.investors[%d].holder_id", k), Message: "is not an ID", Fix: "Send the investor's contact ID"})
		}
		s.Investors = append(s.Investors, equity.ScenarioInvestor{HolderID: id, Amount: strings.TrimSpace(i.Amount)})
	}
	for k, x := range d.Instruments {
		id, err := uuid.Parse(string(x))
		if err != nil {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: fmt.Sprintf("body.instruments[%d]", k), Message: "is not an ID", Fix: "Send the instrument's ID"})
		}
		s.Instruments = append(s.Instruments, id)
	}
	if len(fields) > 0 {
		return s, invalid(fields)
	}
	return s, nil
}

func scenarioDraftOf(s equity.Scenario) RoundScenarioDraft {
	d := RoundScenarioDraft{Name: s.Name, On: s.On.Format(time.DateOnly), Price: s.Price, PreMoney: s.PreMoney, PriceDecimals: s.PriceDecimals,
		ClassAccountID: s.ClassAccountID.String(), Investors: []RoundInvestorDraft{}, TopUp: s.TopUp, TopUpBefore: s.TopUpBefore, Instruments: []InstrumentRef{},
		ResolutionRefs: s.ResolutionRefs, Note: s.Note}
	for _, i := range s.Investors {
		d.Investors = append(d.Investors, RoundInvestorDraft{HolderID: i.HolderID.String(), Amount: i.Amount})
	}
	for _, id := range s.Instruments {
		d.Instruments = append(d.Instruments, InstrumentRef(id.String()))
	}
	return d
}

// outcomeView is a scenario's outcome as the API shows it, names resolved.
func (a *API) outcomeView(ctx context.Context, u storage.UnitOfWork, o equity.RoundOutcome, safes map[uuid.UUID]uuid.UUID) (RoundOutcomeView, error) {
	names := map[uuid.UUID]string{}
	name := func(id uuid.UUID) (string, error) {
		if n, ok := names[id]; ok {
			return n, nil
		}
		c, err := u.Contacts().Get(ctx, id)
		if err != nil {
			return "", err
		}
		names[id] = c.LegalName
		return c.LegalName, nil
	}
	out := RoundOutcomeView{Price: o.Price.String(), Currency: o.Price.Currency().Code, PreMoney: moneyOut(o.PreMoney), PostMoney: moneyOut(o.PostMoney),
		Issues: []RoundIssueView{}, Conversions: []RoundConversionView{}, Holders: []RoundHolderView{}, IssuedBefore: whole(o.IssuedBefore),
		IssuedAfter: whole(o.IssuedAfter), DilutedBefore: whole(o.DilutedBefore), DilutedAfter: whole(o.DilutedAfter),
		UnallocatedBefore: whole(o.UnallocatedBefore), UnallocatedAfter: whole(o.UnallocatedAfter), NewShares: whole(o.NewShares)}
	for _, x := range o.Issues {
		n, err := name(x.HolderID)
		if err != nil {
			return out, err
		}
		out.Issues = append(out.Issues, RoundIssueView{HolderID: x.HolderID.String(), HolderName: n, Shares: whole(x.Shares), Amount: moneyOut(x.Amount),
			Left: moneyOut(x.Left)})
	}
	for _, c := range o.Conversions {
		n, err := name(safes[c.ID])
		if err != nil {
			return out, err
		}
		v := RoundConversionView{InstrumentID: c.ID.String(), HolderName: n, Price: c.Price.String(), Shares: whole(c.Shares),
			Converted: moneyOut(c.Converted), Left: moneyOut(c.Left)}
		if len(c.Uncovered) > 0 {
			v.Review = equity.UncoveredText(equity.Conversion{Uncovered: c.Uncovered})
		}
		out.Conversions = append(out.Conversions, v)
	}
	for _, h := range o.Holders {
		n, err := name(h.HolderID)
		if err != nil {
			return out, err
		}
		out.Holders = append(out.Holders, RoundHolderView{HolderID: h.HolderID.String(), HolderName: n, IssuedBefore: whole(h.IssuedBefore),
			IssuedAfter: whole(h.IssuedAfter), DilutedBefore: whole(h.DilutedBefore), DilutedAfter: whole(h.DilutedAfter), OwnershipBefore: h.OwnershipBefore,
			OwnershipAfter: h.OwnershipAfter, DilutedShareBefore: h.DilutedShareBefore, DilutedShareAfter: h.DilutedShareAfter, Dilution: h.Drop,
			ProRata: whole(h.ProRata)})
	}
	slices.SortStableFunc(out.Holders, func(x, y RoundHolderView) int {
		return strings.Compare(strings.ToLower(x.HolderName), strings.ToLower(y.HolderName))
	})
	return out, nil
}

func (a *API) scenarioOf(ctx context.Context, u storage.UnitOfWork, doc storage.Document, set ledger.Settings, asked map[string]string) (RoundScenarioView, error) {
	s, err := equity.ReadScenario(doc)
	if err != nil {
		return RoundScenarioView{}, err
	}
	out := RoundScenarioView{RoundScenarioDraft: scenarioDraftOf(s), ID: doc.ID.String(), Status: "draft", Version: doc.Version, Problems: []FieldProblem{},
		ApprovalID: asked[doc.ID.String()]}
	if doc.Status == storage.DocumentPosted {
		out.Status = "approved"
	}
	if doc.Status == storage.DocumentPosted {
		out.ApprovedBy, out.ApprovedAt = personOf(doc.PostedBy), doc.PostedAt
		if err := a.name(ctx, u, out.ApprovedBy); err != nil {
			return out, err
		}
	}
	o, err := equity.OutcomeIn(ctx, u, s, set)
	var e *equity.Error
	switch {
	case err == nil:
		instruments, err := equity.Instruments(ctx, u)
		if err != nil {
			return out, err
		}
		holders := map[uuid.UUID]uuid.UUID{}
		for _, d := range instruments {
			if i, err := equity.ReadInstrument(d); err == nil {
				holders[d.ID] = i.HolderID
			}
		}
		view, err := a.outcomeView(ctx, u, o, holders)
		if err != nil {
			return out, err
		}
		out.Outcome = &view
	case errors.As(err, &e):
		out.Problems = append(out.Problems, FieldProblem{Code: e.Code, Field: "body", Message: e.Message, Fix: e.Fix})
	default:
		return out, err
	}
	return out, nil
}

func (a *API) shownScenario(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (RoundScenarioView, error) {
	doc, err := equityRecordOfType(ctx, u, storage.DocumentRoundScenario, id)
	if err != nil {
		return RoundScenarioView{}, err
	}
	set, err := journal.LoadSettings(ctx, u)
	if err != nil {
		return RoundScenarioView{}, err
	}
	asked, err := a.waiting(ctx, u, approval.ObjectRoundScenario)
	if err != nil {
		return RoundScenarioView{}, err
	}
	return a.scenarioOf(ctx, u, doc, set, asked)
}

func scenarioAnswer(w written[RoundScenarioView]) *scenarioWriteOutput {
	if w.Previewed {
		return &scenarioWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}
	}
	return &scenarioWriteOutput{Status: w.Status, ETag: etag(w.Body.Version, "draft"), Location: "/v1/round-scenarios/" + w.Body.ID,
		Replayed: replayedHeader(w.Replayed), Body: w.Body}
}

// scenarioDependencies are a scenario's envelope values: its status.
func scenarioDependencies(ctx context.Context, u storage.UnitOfWork, key string) (int, map[string]string, error) {
	id, err := uuid.Parse(key)
	if err != nil {
		return 0, nil, err
	}
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return 0, nil, err
	}
	return doc.Version, map[string]string{"status": string(doc.Status)}, nil
}

func registerRoundScenarios(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid}, locks...)

	register(a, authz.ListRoundScenarios, huma.Operation{
		OperationID: "listRoundScenarios", Method: http.MethodGet, Path: "/v1/round-scenarios", Tags: []string{"Equity"},
		Summary: "List the round scenarios, side by side",
		Description: "Every round scenario, drafts and approved, the latest first, each with its outcome against today's books (FR-EQ-21, " +
			"design D8): the price per share, the pre-money and post-money valuations, each investor's shares, each instrument's " +
			"conversion, and each holder before and after with the dilution in percentage points. Outcomes are computed on read and " +
			"never stored. Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *scenarioListInput) (*scenarioListOutput, error) {
		p, _ := authn.From(ctx)
		out := scenarioListOutput{Body: RoundScenarioPage{Items: []RoundScenarioView{}}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			set, err := journal.LoadSettings(ctx, u)
			if err != nil {
				return err
			}
			asked, err := a.waiting(ctx, u, approval.ObjectRoundScenario)
			if err != nil {
				return err
			}
			docs, err := equity.Scenarios(ctx, u)
			if err != nil {
				return err
			}
			all := make([]RoundScenarioView, 0, len(docs))
			for _, d := range slices.Backward(docs) {
				x, err := a.scenarioOf(ctx, u, d, set, asked)
				if err != nil {
					return err
				}
				all = append(all, x)
			}
			out.Body.Items, out.Body.NextCursor, err = pageOf(all, func(x RoundScenarioView) string { return x.ID }, in.PageQuery)
			if out.Body.Items == nil {
				out.Body.Items = []RoundScenarioView{}
			}
			return err
		})
		return &out, err
	})

	register(a, authz.CreateRoundScenario, huma.Operation{
		OperationID: "createRoundScenario", Method: http.MethodPost, Path: "/v1/round-scenarios", Tags: []string{"Equity"},
		DefaultStatus: http.StatusCreated, Summary: "Model a round",
		Description: "Saves a round scenario and answers with its outcome (FR-EQ-21, design D8): a price per share or a pre-money " +
			"valuation, each investor's money (an existing holder's pro-rata take-up included), a pool top-up before or after the money, " +
			"and the SAFEs and notes converting. With Prefer: return=preview it is a sandbox: the outcome comes back and nothing is " +
			"saved. Nothing ever posts: approving the scenario with requestRoundScenarioApproval drafts the round's issues, which then " +
			"follow FR-EQ-17. Side effects: the draft, audit events. Every problem comes back together. Idempotent with Idempotency-Key.",
	}, append(append([]string{CodeNotFound}, writes...), RoundCodes...), func(ctx context.Context, in *scenarioDraftInput) (*scenarioWriteOutput, error) {
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (RoundScenarioView, error) {
			set, err := journal.LoadSettings(ctx, u)
			if err != nil {
				return RoundScenarioView{}, err
			}
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return RoundScenarioView{}, err
			}
			s, err := scenarioIn(in.Body)
			if err != nil {
				return RoundScenarioView{}, err
			}
			doc, err := equity.DraftScenarioIn(ctx, u, s, set, today)
			if err != nil {
				return RoundScenarioView{}, equityProblems(err)
			}
			return a.shownScenario(ctx, u, doc.ID)
		})
		if err != nil {
			return nil, err
		}
		return scenarioAnswer(w), nil
	})

	register(a, authz.GetRoundScenario, huma.Operation{
		OperationID: "getRoundScenario", Method: http.MethodGet, Path: "/v1/round-scenarios/{scenario_id}", Tags: []string{"Equity"},
		Summary:     "Get a round scenario",
		Description: "Returns a round scenario with its ETag and its outcome against today's books (FR-EQ-21). Read-only; no side effects.",
	}, append([]string{CodeNotFound}, locks...), func(ctx context.Context, in *RoundScenarioPath) (*scenarioReadOutput, error) {
		id, err := parseID("scenario_id", in.ScenarioID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out scenarioReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := equityRecordOfType(ctx, u, storage.DocumentRoundScenario, id)
			if err != nil {
				return err
			}
			out.ETag = recordETag(doc)
			out.Body, err = a.shownScenario(ctx, u, id)
			return err
		})
		return &out, err
	})

	register(a, authz.ReplaceRoundScenario, huma.Operation{
		OperationID: "replaceRoundScenario", Method: http.MethodPut, Path: "/v1/round-scenarios/{scenario_id}", Tags: []string{"Equity"},
		Summary: "Change a round scenario",
		Description: "Saves a new version of a scenario not approved yet, and answers with its outcome (FR-EQ-21). An approved one changes " +
			"no more (EQUITY-05): model another. Side effects: the new version, audit events. Needs If-Match with its ETag. Idempotent " +
			"with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired}, writes...), RoundCodes...), func(ctx context.Context, in *scenarioReviseInput) (*scenarioWriteOutput, error) {
		id, err := parseID("scenario_id", in.ScenarioID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (RoundScenarioView, error) {
			doc, err := equity.Drafted(ctx, u, storage.DocumentRoundScenario, id)
			if err != nil {
				return RoundScenarioView{}, err
			}
			if err := checkIfMatch(in.IfMatch, recordETag(doc)); err != nil {
				return RoundScenarioView{}, err
			}
			set, err := journal.LoadSettings(ctx, u)
			if err != nil {
				return RoundScenarioView{}, err
			}
			s, err := scenarioIn(in.Body)
			if err != nil {
				return RoundScenarioView{}, err
			}
			if _, err := equity.ReviseScenarioIn(ctx, u, id, s, set); err != nil {
				return RoundScenarioView{}, equityProblems(err)
			}
			return a.shownScenario(ctx, u, id)
		})
		if err != nil {
			return nil, err
		}
		return scenarioAnswer(w), nil
	})

	register(a, authz.RequestRoundScenarioApproval, huma.Operation{
		OperationID: "requestRoundScenarioApproval", Method: http.MethodPost, Path: "/v1/round-scenarios/{scenario_id}/approval", Tags: []string{"Equity"},
		DefaultStatus: http.StatusAccepted, Summary: "Ask for approval of a round scenario",
		Description: "Asks another person to approve a scenario at the version read (FR-EQ-21, FM-1). Approving it checks and models it " +
			"again, then drafts an issue for cash per investor at the round's price with the scenario's resolutions, and for a top-up " +
			"a pool resolution; each draft then follows FR-EQ-17. The instruments' conversions are drafted from the round's issue " +
			"once it is approved (draftInstrumentConversions). It needs the resolutions named, and a top-up a round on or before " +
			"today. The answer is 202 with an approval request, which a person who holds A on \"Cap table, holders, investors, equity " +
			"transactions\" approves, stepped up, never the one who asked. Needs a person signed in who passed step-up, and If-Match " +
			"with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing.",
	}, append(append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeSessionRequired, CodeStepUpRequired}, writes...), RoundCodes...),
		func(ctx context.Context, in *scenarioAskInput) (*equityAskOutput, error) {
			id, err := parseID("scenario_id", in.ScenarioID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
				doc, err := equity.Drafted(ctx, u, storage.DocumentRoundScenario, id)
				if err != nil {
					return Approval{}, err
				}
				if err := checkIfMatch(in.IfMatch, recordETag(doc)); err != nil {
					return Approval{}, err
				}
				s, err := equity.ReadScenario(doc)
				if err != nil {
					return Approval{}, err
				}
				today, err := orgToday(ctx, a, u)
				if err != nil {
					return Approval{}, err
				}
				if err := equity.ReadyToApprove(s, today); err != nil {
					return Approval{}, equityProblems(err)
				}
				set, err := journal.LoadSettings(ctx, u)
				if err != nil {
					return Approval{}, err
				}
				o, err := equity.OutcomeIn(ctx, u, s, set)
				if err != nil {
					return Approval{}, err
				}
				diff := []ApprovalDiff{{Field: "status", Before: "draft", After: "approved"}, {Field: "price", After: o.Price.String() + " " + o.Price.Currency().Code},
					{Field: "new_shares", After: whole(o.NewShares)}, {Field: "post_money", After: o.PostMoney.String() + " " + o.PostMoney.Currency().Code},
					{Field: "drafts", After: fmt.Sprintf("%d issues for cash%s", len(o.Issues), map[bool]string{true: " and a pool resolution", false: ""}[s.TopUp != ""])}}
				return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectRoundScenario, ObjectKey: doc.ID.String(),
					Operation: approval.OpRoundScenarioApprove, Summary: fmt.Sprintf("Approve the round %s: %s new shares at %s", s.Name, whole(o.NewShares), o.Price),
					Execute: map[string]any{"scenario_id": doc.ID.String(), "version": strconv.Itoa(doc.Version)}, Diff: diff})
			})
			if err != nil {
				return nil, err
			}
			return &equityAskOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})
}
