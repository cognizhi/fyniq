// Package budget keeps the company's budgets (PRD-001 FR-PLN-01, C.1 #35; p1-planning design D6): a financial year's
// monthly amounts by income and expense account and dimension value, in versions (original, revised), each drafted,
// then approved and so recorded and locked. The year's budget is its latest recorded version. A budget posts nothing.
//
// Amounts are signed as the P&L shows them: income and costs both above zero for what is expected.
package budget

import (
	"context"
	"encoding/json"
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
	"github.com/cognizhi/fyniq/internal/domain/period"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The budget's codes.
const (
	CodeBudget   = "BUDGET-10" // a budget that does not fit: its year, a line, an account, a value, an amount
	CodeRecorded = "BUDGET-11" // a recorded budget changes no more
	CodeImport   = "BUDGET-12" // a row or a column of an imported sheet that does not fit
)

// The kinds of budget version.
const (
	KindOriginal = "original"
	KindRevised  = "revised"
)

// MaxLines is the most lines a budget keeps (FY301).
const MaxLines = 5000

// Error is a refusal in plain words (ENG-23).
type Error struct {
	Code, Message, Fix string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Fix) }

// Budget is one version of a financial year's budget. Its types carry the API's documentation as tags.
type Budget struct {
	Name    string       `json:"name" minLength:"1" maxLength:"80" doc:"The version's name." example:"FY2027 budget"`
	Kind    string       `json:"kind" enum:"original,revised" doc:"original: the year's first budget; revised: a later version that revises one." example:"original"`
	Year    string       `json:"financial_year" format:"date" doc:"The financial year's first day." example:"2027-01-01"`
	Revises string       `json:"revises,omitempty" format:"uuid" doc:"The budget this one revises, for a revised version." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c9d01"`
	Lines   []BudgetLine `json:"lines" maxItems:"5000" nullable:"false" doc:"The amounts by account and dimension values: one line per account and set of values."`
}

// BudgetLine is one account's and set of dimension values' amounts, month by month.
type BudgetLine struct {
	AccountID  string        `json:"account_id" format:"uuid" doc:"A postable income or expense account." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b20"`
	Dimensions []string      `json:"dimension_value_ids,omitempty" nullable:"false" doc:"The values of the organisation's dimensions the line is for, at most one per dimension; none for the whole company."`
	Amounts    []MonthAmount `json:"amounts" doc:"One amount for each month of the year, in order, signed as the P&L shows it."`
}

// MonthAmount is a budget line's amount for one month, a decimal string in the base currency.
type MonthAmount = string

var amountPattern = regexp.MustCompile(`^-?(0|[1-9][0-9]{0,14})(\.[0-9]+)?$`)

// Encode is a budget as a document version keeps it.
func Encode(b Budget) ([]byte, error) { return json.Marshal(b) }

// Read reads a budget's version as it was saved.
func Read(doc storage.Document) (Budget, error) {
	var b Budget
	err := json.Unmarshal(doc.Payload, &b)
	return b, err
}

// Chart is what a budget's lines are checked against: the accounts and the dimensions' values.
type Chart struct {
	Accounts   map[uuid.UUID]storage.Account
	Values     map[uuid.UUID]ValueOf // a dimension value's id → its dimension and whether both are in use
	ByCode     map[string]storage.Account
	Dimensions map[string]uuid.UUID               // a dimension's name, in lower case → its id
	Codes      map[uuid.UUID]map[string]uuid.UUID // a dimension → its values' codes, in lower case → their ids
}

// ValueOf is a dimension value's dimension, and whether the value and its dimension are in use.
type ValueOf struct {
	Dimension uuid.UUID
	Active    bool
}

// ChartIn reads the chart a budget is checked against, in u.
func ChartIn(ctx context.Context, u storage.UnitOfWork) (Chart, error) {
	c := Chart{Accounts: map[uuid.UUID]storage.Account{}, Values: map[uuid.UUID]ValueOf{}, ByCode: map[string]storage.Account{},
		Dimensions: map[string]uuid.UUID{}, Codes: map[uuid.UUID]map[string]uuid.UUID{}}
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return c, err
	}
	for _, a := range accounts {
		c.Accounts[a.ID] = a
		c.ByCode[a.Code] = a
	}
	dims, err := u.Dimensions().List(ctx)
	if err != nil {
		return c, err
	}
	active := map[uuid.UUID]bool{}
	for _, d := range dims {
		active[d.ID] = d.Active
		c.Dimensions[strings.ToLower(strings.TrimSpace(d.Name))] = d.ID
		c.Codes[d.ID] = map[string]uuid.UUID{}
	}
	vs, err := u.Dimensions().Values(ctx, nil)
	if err != nil {
		return c, err
	}
	for _, v := range vs {
		c.Values[v.ID] = ValueOf{Dimension: v.DimensionID, Active: active[v.DimensionID] && v.Active}
		if m := c.Codes[v.DimensionID]; m != nil {
			m[strings.ToLower(v.Code)] = v.ID
		}
	}
	return c, nil
}

// Budgetable says whether an account takes a budget: a postable, active income or expense account.
func Budgetable(a storage.Account) bool {
	return !a.Group && a.Active && (a.Type == storage.Revenue || a.Type == storage.Expense)
}

// YearOf is the financial year that starts on the budget's day, or false.
func YearOf(cal period.Calendar, start string) (period.Year, bool) {
	d, err := time.Parse(time.DateOnly, start)
	if err != nil {
		return period.Year{}, false
	}
	n := cal.Number(d)
	if n < 1 {
		return period.Year{}, false
	}
	y := cal.Year(n)
	return y, y.StartsOn.Equal(d)
}

// lineKey is a line's account and its values, sorted: one line each.
func lineKey(l BudgetLine) string {
	vs := slices.Clone(l.Dimensions)
	slices.Sort(vs)
	return l.AccountID + "|" + strings.Join(vs, ",")
}

// Check is everything wrong with a budget, every problem at once (ENG-22), each with its field (ENG-23).
func Check(b Budget, cur money.Currency, cal period.Calendar, chart Chart) ledger.Problems {
	var probs ledger.Problems
	add := func(field, msg, fix string) {
		probs = append(probs, ledger.Problem{Code: CodeBudget, Field: field, Message: msg, Fix: fix})
	}
	if n := utf8.RuneCountInString(strings.TrimSpace(b.Name)); n == 0 || n > 80 {
		add("name", "the name is blank or longer than 80 characters", "Name the version, such as FY2027 budget")
	}
	switch b.Kind {
	case KindOriginal:
		if b.Revises != "" {
			add("revises", "an original budget revises nothing", "Make it a revised version, or leave revises out")
		}
	case KindRevised:
		if _, err := uuid.Parse(b.Revises); err != nil {
			add("revises", "a revised budget names the budget it revises", "Name the recorded budget it revises")
		}
	default:
		add("kind", fmt.Sprintf("%q is not original or revised", b.Kind), "Choose original or revised")
	}
	y, ok := YearOf(cal, b.Year)
	if !ok {
		add("financial_year", fmt.Sprintf("%q is not the first day of a financial year", b.Year), "Name the financial year by its first day, such as 2027-01-01")
	}
	if len(b.Lines) > MaxLines {
		add("lines", fmt.Sprintf("it has %d lines; a budget keeps at most %d", len(b.Lines), MaxLines), "Budget fewer accounts or dimension values")
	}
	seen := map[string]int{}
	for i, l := range b.Lines {
		at := fmt.Sprintf("lines[%d]", i)
		id, err := uuid.Parse(l.AccountID)
		acc, known := chart.Accounts[id]
		switch {
		case err != nil || !known:
			add(at+".account_id", "the chart has no such account", "Name an account of the chart")
		case !Budgetable(acc):
			add(at+".account_id", fmt.Sprintf("%s %s is not a postable, active income or expense account", acc.Code, acc.Name),
				"Budget income and expense accounts only; the forecast plans cash")
		}
		dims := map[uuid.UUID]bool{}
		for j, v := range l.Dimensions {
			vid, err := uuid.Parse(v)
			of, known := chart.Values[vid]
			switch {
			case err != nil || !known:
				add(fmt.Sprintf("%s.dimension_value_ids[%d]", at, j), "the organisation has no such dimension value", "Name a value of one of its dimensions")
			case !of.Active:
				add(fmt.Sprintf("%s.dimension_value_ids[%d]", at, j), "the value or its dimension is out of use", "Name a value in use")
			case dims[of.Dimension]:
				add(fmt.Sprintf("%s.dimension_value_ids[%d]", at, j), "the line names two values of one dimension", "Name at most one value of each dimension")
			default:
				dims[of.Dimension] = true
			}
		}
		if j, dup := seen[lineKey(l)]; dup {
			add(at, fmt.Sprintf("it repeats line %d's account and values", j), "Keep one line per account and set of values")
		}
		seen[lineKey(l)] = i
		if ok && len(l.Amounts) != len(y.Months) {
			add(at+".amounts", fmt.Sprintf("it has %d amounts; the year has %d months", len(l.Amounts), len(y.Months)), "Give one amount for each month of the year")
		}
		for k, a := range l.Amounts {
			if !amountPattern.MatchString(a) {
				add(fmt.Sprintf("%s.amounts[%d]", at, k), fmt.Sprintf("%q is not an amount", a), "Enter an amount, such as 2500.00")
			} else if dot := strings.IndexByte(a, '.'); dot >= 0 && len(a)-dot-1 > cur.MinorUnits {
				add(fmt.Sprintf("%s.amounts[%d]", at, k), fmt.Sprintf("%s has more decimals than %s", a, cur.Code), fmt.Sprintf("Enter it with at most %d decimals", cur.MinorUnits))
			}
		}
	}
	if len(probs) > 0 {
		return probs
	}
	return nil
}

// context of a write: the calendar, the chart and the base currency.
func setting(ctx context.Context, u storage.UnitOfWork) (period.Calendar, Chart, money.Currency, error) {
	o, err := u.Organisation(ctx)
	if err != nil {
		return period.Calendar{}, Chart{}, money.Currency{}, err
	}
	cur, err := money.Lookup(o.BaseCurrency)
	if err != nil {
		return period.Calendar{}, Chart{}, cur, err
	}
	cal, err := period.CalendarOf(o.Profile)
	if err != nil {
		return cal, Chart{}, cur, err
	}
	chart, err := ChartIn(ctx, u)
	return cal, chart, cur, err
}

// checkIn is Check in u, with a revised budget's original recorded for the same year.
func checkIn(ctx context.Context, u storage.UnitOfWork, b Budget) error {
	cal, chart, cur, err := setting(ctx, u)
	if err != nil {
		return err
	}
	probs := Check(b, cur, cal, chart)
	if b.Kind == KindRevised {
		if id, err := uuid.Parse(b.Revises); err == nil {
			doc, err := u.Documents().Get(ctx, id)
			was, rerr := Read(doc)
			switch {
			case err != nil || doc.Type != storage.DocumentBudget || rerr != nil:
				probs = append(probs, ledger.Problem{Code: CodeBudget, Field: "revises", Message: "it names no budget", Fix: "Name the recorded budget it revises"})
			case doc.Status != storage.DocumentPosted || was.Year != b.Year:
				probs = append(probs, ledger.Problem{Code: CodeBudget, Field: "revises", Message: "it names a budget not recorded, or of another year",
					Fix: "Name the year's recorded budget it revises"})
			}
		}
	}
	if len(probs) > 0 {
		return probs
	}
	return nil
}

func tidy(b Budget) Budget {
	b.Name = strings.TrimSpace(b.Name)
	if b.Lines == nil {
		b.Lines = []BudgetLine{}
	}
	for i := range b.Lines {
		if b.Lines[i].Dimensions == nil {
			b.Lines[i].Dimensions = []string{}
		}
	}
	return b
}

// canonical writes every amount at the currency's minor units, as Check allows them: 10000 is 10000.00.
func canonical(b Budget, cur money.Currency) Budget {
	c := apd.BaseContext.WithPrecision(60)
	for i := range b.Lines {
		for k, a := range b.Lines[i].Amounts {
			x, _, err := apd.NewFromString(a)
			if err != nil {
				continue
			}
			var q apd.Decimal
			if _, err := c.Quantize(&q, x, -int32(cur.MinorUnits)); err == nil {
				q.Negative = q.Negative && !q.IsZero()
				b.Lines[i].Amounts[k] = q.Text('f')
			}
		}
	}
	return b
}

// checkedIn is the budget tidied, checked in u and written at the currency's minor units.
func checkedIn(ctx context.Context, u storage.UnitOfWork, b Budget) (Budget, error) {
	b = tidy(b)
	if err := checkIn(ctx, u, b); err != nil {
		return b, err
	}
	o, err := u.Organisation(ctx)
	if err != nil {
		return b, err
	}
	cur, err := money.Lookup(o.BaseCurrency)
	if err != nil {
		return b, err
	}
	return canonical(b, cur), nil
}

// DraftIn saves a budget as a draft in u.
func DraftIn(ctx context.Context, u storage.UnitOfWork, b Budget) (storage.Document, error) {
	b, err := checkedIn(ctx, u, b)
	if err != nil {
		return storage.Document{}, err
	}
	raw, err := Encode(b)
	if err != nil {
		return storage.Document{}, err
	}
	return u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentBudget, Payload: raw})
}

// Drafted reads a budget not recorded yet, its row locked: not found for any other document.
func Drafted(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (storage.Document, error) {
	doc, err := u.Documents().Lock(ctx, id)
	if err != nil {
		return doc, err
	}
	if doc.Type != storage.DocumentBudget {
		return doc, ports.ErrNotFound
	}
	if doc.Status != storage.DocumentDraft {
		return doc, &Error{CodeRecorded, "the budget is approved and locked, so it changes no more", "Copy it into a revised version"}
	}
	return doc, nil
}

// ReviseIn saves a new version of a budget not recorded yet, in u.
func ReviseIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, b Budget) (storage.Document, error) {
	if _, err := Drafted(ctx, u, id); err != nil {
		return storage.Document{}, err
	}
	b, err := checkedIn(ctx, u, b)
	if err != nil {
		return storage.Document{}, err
	}
	raw, err := Encode(b)
	if err != nil {
		return storage.Document{}, err
	}
	return u.Documents().Revise(ctx, id, raw)
}

// ReadyIn checks a drafted budget again before it is asked for approval or approved.
func ReadyIn(ctx context.Context, u storage.UnitOfWork, doc storage.Document) (Budget, error) {
	b, err := Read(doc)
	if err != nil {
		return b, err
	}
	return b, checkIn(ctx, u, b)
}

// RecordIn records a drafted budget in u, as its approval does: checked again, then locked. It posts nothing.
func RecordIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (storage.Document, error) {
	doc, err := Drafted(ctx, u, id)
	if err != nil {
		return doc, err
	}
	if _, err := ReadyIn(ctx, u, doc); err != nil {
		return doc, err
	}
	if err := u.Documents().SetStatus(ctx, id, storage.DocumentPosted); err != nil {
		return doc, err
	}
	return u.Documents().Get(ctx, id)
}

// Recorded are every recorded budget, the latest recorded first.
func Recorded(ctx context.Context, u storage.UnitOfWork) ([]storage.Document, error) {
	var out []storage.Document
	f := storage.DocumentFilter{Type: storage.DocumentBudget, Status: storage.DocumentPosted, Limit: 200}
	for {
		page, err := u.Documents().Page(ctx, f)
		if err != nil {
			return nil, err
		}
		out = append(out, page...)
		if len(page) < f.Limit {
			break
		}
		f.AfterDate, f.AfterID = page[len(page)-1].Date, &page[len(page)-1].ID
	}
	at := func(d storage.Document) time.Time {
		if d.PostedAt != nil {
			return *d.PostedAt
		}
		return time.Time{}
	}
	slices.SortStableFunc(out, func(a, b storage.Document) int { return at(b).Compare(at(a)) })
	return out, nil
}

// OfYear is the year's budget: its latest recorded version, or false.
func OfYear(ctx context.Context, u storage.UnitOfWork, start time.Time) (storage.Document, Budget, bool, error) {
	docs, err := Recorded(ctx, u)
	if err != nil {
		return storage.Document{}, Budget{}, false, err
	}
	for _, doc := range docs {
		b, err := Read(doc)
		if err != nil {
			return doc, b, false, err
		}
		if b.Year == start.Format(time.DateOnly) {
			return doc, b, true, nil
		}
	}
	return storage.Document{}, Budget{}, false, nil
}

// Copy is a budget's lines for another year, each amount × (1 + growth), rounded once (RND-PLAN); months in order, so the
// two years need the same number of months.
func Copy(from Budget, growth string, months int, cur money.Currency) ([]BudgetLine, error) {
	g, _, err := apd.NewFromString(growth)
	if err != nil || g.Form != apd.Finite {
		return nil, &Error{CodeBudget, fmt.Sprintf("the growth %q is not a decimal", growth), "Enter the growth as a fraction, such as 0.05 for 5%"}
	}
	if g.Cmp(apd.New(-1, 0)) < 0 || g.Cmp(apd.New(10, 0)) > 0 {
		return nil, &Error{CodeBudget, fmt.Sprintf("the growth %s is outside -1 to 10", growth), "Enter the growth as a fraction from -1 to 10"}
	}
	factor := new(apd.Decimal)
	c := apd.BaseContext.WithPrecision(60)
	if _, err := c.Add(factor, apd.New(1, 0), g); err != nil {
		return nil, err
	}
	var out []BudgetLine
	for _, l := range from.Lines {
		if len(l.Amounts) != months {
			return nil, &Error{CodeBudget, fmt.Sprintf("the budget's year has %d months and the year it is copied to %d", len(l.Amounts), months),
				"Copy it to a year of as many months, or enter the budget"}
		}
		nl := BudgetLine{AccountID: l.AccountID, Dimensions: slices.Clone(l.Dimensions), Amounts: make([]MonthAmount, months)}
		for k, a := range l.Amounts {
			x, _, err := apd.NewFromString(a)
			if err != nil {
				return nil, err
			}
			if _, err := c.Mul(x, x, factor); err != nil {
				return nil, err
			}
			nl.Amounts[k] = rounding.Plan(x, cur).Text('f')
		}
		out = append(out, nl)
	}
	return out, nil
}
