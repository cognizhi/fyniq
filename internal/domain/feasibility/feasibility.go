// Package feasibility keeps feasibility studies (FR-PLN-06, FR-PLN-08; p1-planning design D8): one idea's inputs as a
// document, checked in plain words (ENG-23), drafted, changed and kept, never recorded and never posting. Its figures are
// the calculator's (internal/domain/planning), worked out on every read with sg-cit's headline rate at the study's date.
package feasibility

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/gst"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/domain/planning"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// CodeStudy is a study the calculator cannot take as it is: a field out of its limits, each named (ENG-23).
const CodeStudy = "STUDY-01"

// StudyPlan is a feasibility study as its document keeps it and the API takes it (design D8). Rates, the working
// capital and the yearly changes are fractions: 0.18 for 18%.
type StudyPlan struct {
	Name               string `json:"name" minLength:"1" maxLength:"80" doc:"The idea's name." example:"Copilot for SMEs (new product line)"`
	Years              int    `json:"years" minimum:"1" maximum:"10" doc:"The evaluation period, 1 to 10 years." example:"5"`
	DiscountRate       string `json:"discount_rate" doc:"The discount rate, a fraction from 0 to 1." example:"0.18"`
	MaxPayback         string `json:"max_payback" doc:"The longest acceptable payback, in years, at most 50." example:"3"`
	Investment         string `json:"investment" doc:"Spent at the start, in the base currency." example:"150000.00"`
	Life               int    `json:"life" minimum:"1" maximum:"50" doc:"The years it is depreciated over, straight line to its salvage value." example:"5"`
	Salvage            string `json:"salvage" doc:"What it fetches at the end of the period, at most the investment." example:"0.00"`
	WorkingCapital     string `json:"working_capital" doc:"Working capital kept, a fraction of each year's revenue." example:"0.1"`
	Units              string `json:"units" doc:"Units sold in the first year, at most 1 decimal." example:"40"`
	UnitGrowth         string `json:"unit_growth" doc:"Their yearly change, a fraction above -1." example:"0.5"`
	Price              string `json:"price" doc:"The price per unit in the first year." example:"3600.00"`
	PriceGrowth        string `json:"price_growth" doc:"Its yearly change, a fraction above -1." example:"0.03"`
	VariableCost       string `json:"variable_cost" doc:"The variable cost per unit in the first year." example:"700.00"`
	VariableCostGrowth string `json:"variable_cost_growth" doc:"Its yearly change, a fraction above -1." example:"0.02"`
	Fixed              string `json:"fixed" doc:"The fixed costs of the first year." example:"180000.00"`
	FixedGrowth        string `json:"fixed_growth" doc:"Their yearly change, a fraction above -1." example:"0.08"`
}

// stored is a study as its document version keeps it: the plan and the study's date, its first version's, which sets
// its tax rate.
type stored struct {
	StudyPlan
	Date string `json:"date"`
}

// Read reads a study's version as it was saved, and the study's date.
func Read(doc storage.Document) (StudyPlan, time.Time, error) {
	var x stored
	if err := json.Unmarshal(doc.Payload, &x); err != nil {
		return StudyPlan{}, time.Time{}, err
	}
	day, err := time.Parse(time.DateOnly, x.Date)
	return x.StudyPlan, day, err
}

// Check is everything wrong with a study, each named with its field (ENG-21, ENG-23).
func Check(p StudyPlan, base money.Currency) ledger.Problems {
	var probs ledger.Problems
	add := func(field, msg, fix string) {
		probs = append(probs, ledger.Problem{Code: CodeStudy, Field: field, Message: msg, Fix: fix})
	}
	if n := len([]rune(strings.TrimSpace(p.Name))); n < 1 || n > 80 {
		add("name", "is empty or longer than 80 characters", "Name the idea in 1 to 80 characters")
	}
	if p.Years < planning.MinStudyYears || p.Years > planning.MaxStudyYears {
		add("years", fmt.Sprintf("is %d", p.Years), "Evaluate it over 1 to 10 years")
	}
	if p.Life < 1 || p.Life > 50 {
		add("life", fmt.Sprintf("is %d years", p.Life), "Depreciate it over 1 to 50 years")
	}
	read := func(field, s string, places int) *apd.Decimal {
		x, _, err := apd.NewFromString(strings.TrimSpace(s))
		if err != nil || x.Form != apd.Finite || -int(x.Exponent) > places || x.NumDigits()+int64(x.Exponent) > 13 {
			add(field, fmt.Sprintf("%q is not a decimal with at most %d decimals", s, places), "Enter a decimal, such as 0.18 or 1500.00")
			return nil
		}
		return x
	}
	between := func(field, s string, places int, lo, hi string, loOpen bool, fix string) *apd.Decimal {
		x := read(field, s, places)
		if x == nil {
			return nil
		}
		l, _, _ := apd.NewFromString(lo)
		h, _, _ := apd.NewFromString(hi)
		if c := x.Cmp(l); c < 0 || (loOpen && c == 0) || x.Cmp(h) > 0 {
			add(field, fmt.Sprintf("%s is outside its limits", s), fix)
			return nil
		}
		return x
	}
	between("discount_rate", p.DiscountRate, 4, "0", "1", false, "Enter the rate as a fraction from 0 to 1, such as 0.18 for 18%")
	between("max_payback", p.MaxPayback, 1, "0", "50", false, "Enter the longest acceptable payback in years, at most 50")
	amount := fmt.Sprintf("Enter an amount of zero or more with at most %d decimals", base.MinorUnits)
	investment := between("investment", p.Investment, base.MinorUnits, "0", "999999999999", false, amount)
	salvage := between("salvage", p.Salvage, base.MinorUnits, "0", "999999999999", false, amount)
	if investment != nil && salvage != nil && salvage.Cmp(investment) > 0 {
		add("salvage", "is more than the investment", "Enter a salvage value no more than the investment")
	}
	between("working_capital", p.WorkingCapital, 4, "0", "1", false, "Enter the working capital as a fraction of revenue, such as 0.1")
	between("units", p.Units, 1, "0", "999999999", false, "Enter the first year's units, at most 1 decimal")
	between("price", p.Price, base.MinorUnits, "0", "999999999999", false, amount)
	between("variable_cost", p.VariableCost, base.MinorUnits, "0", "999999999999", false, amount)
	between("fixed", p.Fixed, base.MinorUnits, "0", "999999999999", false, amount)
	change := "Enter the yearly change as a fraction above -1 and below 10, such as 0.05 for 5%"
	for _, g := range [][2]string{{"unit_growth", p.UnitGrowth}, {"price_growth", p.PriceGrowth}, {"variable_cost_growth", p.VariableCostGrowth},
		{"fixed_growth", p.FixedGrowth}} {
		between(g[0], g[1], 4, "-1", "9.9999", true, change)
	}
	return probs
}

// TaxRate is sg-cit's headline rate for the year of assessment of income earned on day (design D8): the organisation's
// active version in force then, else this release's (ENG-31).
func TaxRate(ctx context.Context, u storage.UnitOfWork, day time.Time) (*apd.Decimal, string, error) {
	v, ok, err := gst.PackOn(ctx, u, "sg-cit", day)
	if err != nil {
		return nil, "", err
	}
	if !ok {
		return nil, "", &planning.Error{Code: planning.CodeStatutory, Field: "packs",
			Message: fmt.Sprintf("no sg-cit version is in force on %s", day.Format("2 Jan 2006")), Fix: "Activate the sg-cit pack"}
	}
	r, err := pack.CITRate(v, day.Year()+1)
	if err != nil {
		return nil, "", &planning.Error{Code: planning.CodeStatutory, Field: "packs",
			Message: fmt.Sprintf("%s has no rate for YA %d", v.ID, day.Year()+1), Fix: "Activate an sg-cit version for that year of assessment"}
	}
	return r, v.ID, nil
}

// Study is the calculator's study of a plan, with the tax rate.
func Study(p StudyPlan, base money.Currency, taxRate *apd.Decimal) planning.Study {
	d := func(s string) *apd.Decimal {
		x, _, err := apd.NewFromString(strings.TrimSpace(s))
		if err != nil {
			return apd.New(0, 0)
		}
		return x
	}
	return planning.Study{Currency: base, Years: p.Years, Rate: d(p.DiscountRate), MaxPayback: d(p.MaxPayback), Investment: d(p.Investment),
		Life: p.Life, Salvage: d(p.Salvage), WorkingCap: d(p.WorkingCapital), Units: d(p.Units), UnitGrowth: d(p.UnitGrowth),
		Price: d(p.Price), PriceGrowth: d(p.PriceGrowth), VarCost: d(p.VariableCost), VarCostGrowth: d(p.VariableCostGrowth),
		Fixed: d(p.Fixed), FixedGrowth: d(p.FixedGrowth), TaxRate: taxRate}
}

// tidy trims a plan's name and figures.
func tidy(p StudyPlan) StudyPlan {
	for _, s := range []*string{&p.Name, &p.DiscountRate, &p.MaxPayback, &p.Investment, &p.Salvage, &p.WorkingCapital, &p.Units,
		&p.UnitGrowth, &p.Price, &p.PriceGrowth, &p.VariableCost, &p.VariableCostGrowth, &p.Fixed, &p.FixedGrowth} {
		*s = strings.TrimSpace(*s)
	}
	return p
}

// DraftIn saves a study in u, dated today: its tax rate is the one in force then.
func DraftIn(ctx context.Context, u storage.UnitOfWork, p StudyPlan, base money.Currency, today time.Time) (storage.Document, error) {
	p = tidy(p)
	if probs := Check(p, base); probs != nil {
		return storage.Document{}, probs
	}
	b, err := json.Marshal(stored{p, today.Format(time.DateOnly)})
	if err != nil {
		return storage.Document{}, err
	}
	return u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentFeasibilityStudy, Payload: b})
}

// Get reads a study by ID: not found for any other document.
func Get(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, lock bool) (storage.Document, error) {
	get := u.Documents().Get
	if lock {
		get = u.Documents().Lock
	}
	doc, err := get(ctx, id)
	if err != nil {
		return doc, err
	}
	if doc.Type != storage.DocumentFeasibilityStudy {
		return doc, ports.ErrNotFound
	}
	return doc, nil
}

// ReviseIn saves a new version of a study in u; its date, and so its tax rate, stays its first.
func ReviseIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, p StudyPlan, base money.Currency) (storage.Document, error) {
	doc, err := Get(ctx, u, id, true)
	if err != nil {
		return storage.Document{}, err
	}
	_, day, err := Read(doc)
	if err != nil {
		return storage.Document{}, err
	}
	p = tidy(p)
	if probs := Check(p, base); probs != nil {
		return storage.Document{}, probs
	}
	b, err := json.Marshal(stored{p, day.Format(time.DateOnly)})
	if err != nil {
		return storage.Document{}, err
	}
	return u.Documents().Revise(ctx, id, b)
}
