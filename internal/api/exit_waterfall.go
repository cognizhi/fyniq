package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/equity"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The exit waterfall sandbox (FR-EQ-29; p2-reporting-planning task 8.1 and design D14): what each class and holder would receive if the company were sold for an amount, under the
// preferences the register records, next to a plain split by shares. It reads the register and posts and saves nothing.

// WaterfallHolderView is a holder's part of a class's proceeds.
type WaterfallHolderView struct {
	HolderID   string `json:"holder_id" format:"uuid" doc:"The holder." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c11"`
	HolderName string `json:"holder_name" doc:"Their name." example:"Seedling Ventures Pte. Ltd."`
	Shares     string `json:"shares" doc:"Their shares of the class, as converted." example:"1500000"`
	Invested   Money  `json:"invested" doc:"The money invested in the class that is theirs: the class's issues shared by shares (ALLOC-PRORATA)."`
	Total      Money  `json:"total" doc:"What they receive."`
}

// WaterfallClassView is a class's proceeds.
type WaterfallClassView struct {
	ClassID       string                `json:"class_id" doc:"The class's ID, or account:<id> for the shares of an account with no recorded class." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d10"`
	Name          string                `json:"name" doc:"Its name." example:"Series Seed"`
	HasPreference bool                  `json:"has_preference" doc:"Whether it has a liquidation preference." example:"true"`
	Multiple      string                `json:"multiple,omitempty" doc:"The multiple of the money invested its preference pays first." example:"1"`
	Particip      bool                  `json:"participating,omitempty" doc:"Whether it shares the rest as converted after its preference, with no cap." example:"false"`
	Seniority     int                   `json:"seniority,omitempty" doc:"1 is paid first; classes of the same seniority share pari passu." example:"1"`
	Preference    Money                 `json:"preference" doc:"What it takes as its preference: zero for a class that converts or has none."`
	Residual      Money                 `json:"residual" doc:"What it takes of the rest, as converted."`
	Total         Money                 `json:"total" doc:"Its proceeds."`
	Converts      bool                  `json:"converts" doc:"True for a non-participating preference class that does better converting." example:"false"`
	Holders       []WaterfallHolderView `json:"holders" nullable:"false" doc:"Its holders' parts."`
}

// ExitWaterfall is the sandbox's answer.
type ExitWaterfall struct {
	Available   bool                 `json:"available" doc:"False until a class with a liquidation preference is recorded: nothing is modelled." example:"true"`
	Message     string               `json:"message,omitempty" doc:"Why it is not available." example:"No share class records a liquidation preference."`
	Currency    string               `json:"currency" doc:"The base currency every amount is in." example:"SGD"`
	On          string               `json:"on" format:"date" doc:"The day of the register it reads." example:"2027-03-12"`
	ExitValue   Money                `json:"exit_value" doc:"What the shareholders receive after debts and sale costs."`
	Classes     []WaterfallClassView `json:"classes" nullable:"false" doc:"Under the preferences, each class's proceeds, in the order of its name."`
	AsConverted []WaterfallClassView `json:"as_converted" nullable:"false" doc:"Every class converted, no preference: each share the same."`
	Notes       []string             `json:"notes" nullable:"false" doc:"What the register records that the waterfall does not model."`
	Modelled    []string             `json:"modelled" nullable:"false" doc:"The structures the waterfall models: the others are refused by name, never approximated."`
}

// TransformSchema implements huma.SchemaTransformer: an example for each note and each modelled structure (NFR-API-01).
func (ExitWaterfall) TransformSchema(_ huma.Registry, s *huma.Schema) *huma.Schema {
	itemExamples(s, map[string]any{"notes": "Series Seed has anti-dilution protection (broad based), and no adjustment of its conversion is recorded, so none is applied [lawyer to confirm].",
		"modelled": "An uncapped participating preference"})
	return s
}

type exitWaterfallInput struct {
	ExitValue string `query:"exit_value" required:"true" pattern:"^[0-9]+(\\.[0-9]+)?$" doc:"What the shareholders receive, in the base currency, as a plain decimal." example:"8000000.00"`
	On        string `query:"on" format:"date" doc:"The day of the register to read; left out, the organisation's today." example:"2027-03-12"`
}

type exitWaterfallOutput struct{ Body ExitWaterfall }

// CodeWaterfallRefused is the sandbox refusing a structure it does not model (EQ-WF-01).
const CodeWaterfallRefused = equity.CodeWaterfall

func registerExitWaterfall(a *API) {
	register(a, authz.GetExitWaterfall, huma.Operation{
		OperationID: "getExitWaterfall", Method: http.MethodGet, Path: "/v1/exit-waterfall", Tags: []string{"Equity"},
		Summary: "Model what an exit pays each class and holder",
		Description: "A sandbox (FR-EQ-29): for an exit value, what each share class and each holder would receive under the liquidation preferences the register records (a non-participating preference " +
			"of a multiple of the money invested, which converts when that pays the class more; an uncapped participating preference; seniority between classes, classes of one seniority sharing pari " +
			"passu), next to every class converted with no preference. The money invested in a class is its issues up to the day, shared among its holders by shares. A capped participating " +
			"preference and a cumulative dividend are refused by name (EQ-WF-01), never approximated, until a lawyer or accountant confirms them. Nothing is posted or saved. Read-only.",
	}, []string{CodeInvalid, CodeWaterfallRefused}, func(ctx context.Context, in *exitWaterfallInput) (*exitWaterfallOutput, error) {
		p, _ := authn.From(ctx)
		var out ExitWaterfall
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			o, err := u.Organisation(ctx)
			if err != nil {
				return err
			}
			base, err := money.Lookup(o.BaseCurrency)
			if err != nil {
				return err
			}
			exit, bad := moneyIn(Money{Amount: in.ExitValue, Currency: base.Code}, "exit_value")
			if bad != nil {
				return invalid([]FieldProblem{*bad})
			}
			day, err := orgToday(ctx, a, u)
			if err != nil {
				return err
			}
			if in.On != "" {
				if day, err = time.Parse(time.DateOnly, in.On); err != nil {
					return invalid([]FieldProblem{{Code: CodeInvalid, Field: "on", Message: "is not a date", Fix: "Send the day as YYYY-MM-DD"}})
				}
			}
			out = ExitWaterfall{Currency: base.Code, On: dateOf(day), ExitValue: moneyOut(exit), Classes: []WaterfallClassView{}, AsConverted: []WaterfallClassView{}, Notes: []string{},
				Modelled: []string{"A non-participating liquidation preference of a multiple of the money invested, converting when that pays more",
					"An uncapped participating preference", "Seniority between classes, and classes of one seniority pari passu"}}
			reg, err := equity.WaterfallFromRegister(ctx, u, day, base)
			if err != nil {
				return waterfallRefusal(err)
			}
			out.Notes = append(out.Notes, reg.Notes...)
			if !reg.HasPreference {
				out.Message = "No share class records a liquidation preference, so there is no waterfall to show. Record a preference class under Equity first."
				return nil
			}
			res, err := equity.ModelWaterfall(exit, reg.Classes)
			if err != nil {
				return waterfallRefusal(err)
			}
			v, err := a.capViewOf(ctx, u)
			if err != nil {
				return err
			}
			byID := map[string]equity.WaterfallClass{}
			for _, c := range reg.Classes {
				byID[c.ID] = c
			}
			view := func(paid []equity.WaterfallPaid) ([]WaterfallClassView, error) {
				var vs []WaterfallClassView
				for _, pd := range paid {
					src := byID[pd.ID]
					c := WaterfallClassView{ClassID: pd.ID, Name: pd.Name, HasPreference: src.Preference, Particip: src.Participating, Seniority: src.Seniority,
						Preference: moneyOut(pd.Preference), Residual: moneyOut(pd.Residual), Total: moneyOut(pd.Total), Converts: pd.Converts, Holders: []WaterfallHolderView{}}
					if src.Preference {
						c.Multiple = src.Multiple
					}
					for i, h := range pd.Holders {
						id, perr := uuid.Parse(h.ID)
						if perr != nil {
							return nil, perr
						}
						name, nerr := v.name(ctx, u, id)
						if nerr != nil {
							return nil, nerr
						}
						sh := WaterfallHolderView{HolderID: h.ID, HolderName: name, Total: moneyOut(h.Total), Invested: moneyOut(src.Holders[i].Invested)}
						sh.Shares = strconv.FormatInt(src.Holders[i].Shares, 10)
						c.Holders = append(c.Holders, sh)
					}
					vs = append(vs, c)
				}
				return vs, nil
			}
			if out.Classes, err = view(res.Classes); err != nil {
				return err
			}
			out.AsConverted, err = view(res.AsConverted)
			out.Available = true
			return err
		})
		if err != nil {
			return nil, err
		}
		return &exitWaterfallOutput{Body: out}, nil
	})
}

// waterfallRefusal turns the waterfall's refusal into a 422 with its code, naming the class.
func waterfallRefusal(err error) error {
	msg := err.Error()
	if rest, ok := strings.CutPrefix(msg, equity.CodeWaterfall+": "); ok {
		return problem(CodeWaterfallRefused, rest, "Record only the structures the waterfall models, or ask the company's lawyer or accountant to confirm the others")
	}
	var p *Problem
	if errors.As(err, &p) {
		return err
	}
	return err
}
