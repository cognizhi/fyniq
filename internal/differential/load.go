package differential

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	schemas "github.com/cognizhi/fyniq/test/differential"
)

// InvalidError is a scenario that is not fyniq-scenario@1 at all (test/differential/README.md,
// "Invalid scenario or rejected document"): the runner fails, as the oracle exits with status 2.
type InvalidError struct{ Reason string }

func (e *InvalidError) Error() string { return "invalid scenario: " + e.Reason }

func compile(name string, schema []byte) *jsonschema.Schema {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schema))
	if err != nil {
		panic(err)
	}
	c := jsonschema.NewCompiler()
	c.AssertFormat()
	if err := c.AddResource(name, doc); err != nil {
		panic(err)
	}
	s, err := c.Compile(name)
	if err != nil {
		panic(err)
	}
	return s
}

var (
	scenarioSchema = compile("scenario.schema.json", schemas.ScenarioSchema)
	resultSchema   = compile("result.schema.json", schemas.ResultSchema)
)

// noDuplicateKeys walks the JSON and refuses an object that repeats a key, which encoding/json
// would silently resolve to the last one.
func noDuplicateKeys(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var walk func() error
	walk = func() error {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		switch tok {
		case json.Delim('{'):
			seen := map[string]bool{}
			for dec.More() {
				key, err := dec.Token()
				if err != nil {
					return err
				}
				k := key.(string)
				if seen[k] {
					return fmt.Errorf("the key %q appears twice in one object", k)
				}
				seen[k] = true
				if err := walk(); err != nil {
					return err
				}
			}
			_, err = dec.Token()
			return err
		case json.Delim('['):
			for dec.More() {
				if err := walk(); err != nil {
					return err
				}
			}
			_, err = dec.Token()
			return err
		}
		return nil
	}
	if err := walk(); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return fmt.Errorf("there is more after the scenario")
	}
	return nil
}

// ParseScenario reads a scenario and refuses an invalid one: the schema, repeated keys, a base
// currency it does not declare, repeated account codes or document IDs, and dates that are not real.
func ParseScenario(data []byte) (Scenario, error) {
	if err := noDuplicateKeys(data); err != nil {
		return Scenario{}, &InvalidError{err.Error()}
	}
	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return Scenario{}, &InvalidError{err.Error()}
	}
	if err := scenarioSchema.Validate(v); err != nil {
		return Scenario{}, &InvalidError{err.Error()}
	}
	var s Scenario
	if err := json.Unmarshal(data, &s); err != nil {
		return Scenario{}, &InvalidError{err.Error()}
	}
	if _, ok := s.Currencies[s.BaseCurrency]; !ok {
		return Scenario{}, &InvalidError{fmt.Sprintf("the base currency %s is not among the scenario's currencies", s.BaseCurrency)}
	}
	codes := map[string]bool{}
	for _, a := range s.Accounts {
		if codes[a.Code] {
			return Scenario{}, &InvalidError{fmt.Sprintf("account %s appears twice", a.Code)}
		}
		codes[a.Code] = true
	}
	ids := map[string]bool{}
	for _, d := range s.Documents {
		if ids[d.ID] {
			return Scenario{}, &InvalidError{fmt.Sprintf("document %s appears twice", d.ID)}
		}
		ids[d.ID] = true
		if _, err := time.Parse(time.DateOnly, d.Date); err != nil {
			return Scenario{}, &InvalidError{fmt.Sprintf("document %s has the date %q, which is not a calendar date", d.ID, d.Date)}
		}
	}
	for _, r := range s.Reversals {
		what, date := "reversal", r.Date
		if r.Edit != nil {
			what, date = "edit", r.Edit.Date
		}
		if _, err := time.Parse(time.DateOnly, date); err != nil {
			return Scenario{}, &InvalidError{fmt.Sprintf("the %s of %s has the date %q, which is not a calendar date", what, r.Document, date)}
		}
	}
	if reason := realDates(v, ""); reason != "" {
		return Scenario{}, &InvalidError{reason}
	}
	if reason := p1Format(s); reason != "" {
		return Scenario{}, &InvalidError{reason}
	}
	if reason := dimensionFormat(s); reason != "" {
		return Scenario{}, &InvalidError{reason}
	}
	return s, nil
}

// dimensionFormat names the first break of the dimensions' format rules (README, "Invalid scenario or rejected
// document" and "Dimensions"), or returns "": two dimensions of one name or two values of one code, an account that
// requires a dimension not declared, or a journal's line naming a dimension or a code not declared.
func dimensionFormat(s Scenario) string {
	codes := map[string]map[string]bool{}
	for _, d := range s.Dimensions {
		if codes[d.Name] != nil {
			return fmt.Sprintf("dimension %s appears twice", d.Name)
		}
		codes[d.Name] = map[string]bool{}
		for _, v := range d.Values {
			if codes[d.Name][v.Code] {
				return fmt.Sprintf("dimension %s has the value %s twice", d.Name, v.Code)
			}
			codes[d.Name][v.Code] = true
		}
	}
	for _, a := range s.Accounts {
		for _, name := range a.Requires {
			if codes[name] == nil {
				return fmt.Sprintf("account %s requires %s, which is not among the dimensions", a.Code, name)
			}
		}
	}
	lines := func(doc string, ls []Line) string {
		for i, l := range ls {
			for name, code := range l.Dimensions {
				switch {
				case codes[name] == nil:
					return fmt.Sprintf("%s line %d names the dimension %s, which is not among the dimensions", doc, i+1, name)
				case !codes[name][code]:
					return fmt.Sprintf("%s line %d names %s %s, which is not among its values", doc, i+1, name, code)
				}
			}
		}
		return ""
	}
	for _, d := range s.Documents {
		if reason := lines(d.ID, d.Lines); reason != "" {
			return reason
		}
	}
	for _, r := range s.Reversals {
		if r.Edit != nil {
			if reason := lines(r.Document+"/edit", r.Edit.Lines); reason != "" {
				return reason
			}
		}
	}
	return ""
}

// dateKeys are the keys whose string values are dates in fyniq-scenario@1 (scenario.schema.json's "date").
var dateKeys = map[string]bool{"date": true, "as_of": true, "books_start": true, "from": true, "registered_from": true,
	"registered_to": true, "spread_from": true, "depreciate_from": true, "incurred": true, "reverse_on": true}

// realDates names the first date anywhere in the scenario that is not a calendar date, or returns "".
func realDates(v any, key string) string {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			if reason := realDates(x[k], k); reason != "" {
				return reason
			}
		}
	case []any:
		for _, y := range x {
			if reason := realDates(y, key); reason != "" {
				return reason
			}
		}
	case string:
		if _, err := time.Parse(time.DateOnly, x); dateKeys[key] && err != nil {
			return fmt.Sprintf("%s %q is not a calendar date", key, x)
		}
	}
	return ""
}

// p1Format checks the rules of the P1 sources, the statements and the KPIs that a JSON Schema cannot state (README,
// "Invalid scenario or rejected document"), and names the first one broken, or returns "".
func p1Format(s Scenario) string {
	p1, opening := map[string]bool{}, ""
	for _, d := range s.Documents {
		switch d.Type {
		case ledger.SourceManualJournal:
		case OpeningBalances: // README, "Opening balances"
			switch {
			case opening != "":
				return fmt.Sprintf("the scenario has two opening balances, %s and %s; the organisation has one set", opening, d.ID)
			case s.Settings == nil || s.Settings.BooksStart == "" || s.AsOf == "":
				return "opening balances need settings with books_start, and as_of"
			case d.Date != s.Settings.BooksStart:
				return fmt.Sprintf("the opening balances %s are dated %s, not the books' start %s", d.ID, d.Date, s.Settings.BooksStart)
			case s.Statements:
				return "the statements of a scenario with opening balances are not in the format yet"
			}
			opening = d.ID
		default:
			p1[d.ID] = true
		}
		if d.ReverseOn != "" && s.AsOf == "" {
			return fmt.Sprintf("journal %s reverses by itself, which needs as_of", d.ID)
		}
	}
	if len(p1) > 0 && (s.Settings == nil || s.AsOf == "") {
		return "a scenario with a P1 document needs settings and as_of"
	}
	for _, r := range s.Reversals {
		date := r.Date
		if x := r.Edit; x != nil {
			date = x.Date
			switch {
			case p1[r.Document]:
				return fmt.Sprintf("the edit names %s, a P1 document, which the format cannot edit yet", r.Document)
			case r.Document == opening && (x.Date != s.Settings.BooksStart || x.ReverseOn != ""):
				return fmt.Sprintf("the edit of the opening balances %s is dated %s or reverses by itself; they post at the books' start", r.Document, x.Date)
			case x.ReverseOn != "" && s.AsOf == "":
				return fmt.Sprintf("the edit of %s reverses by itself, which needs as_of", r.Document)
			}
		} else {
			switch {
			case p1[r.Document]:
				return fmt.Sprintf("the reversal names %s, a P1 document, which the format cannot void yet", r.Document)
			case r.Document == opening:
				return fmt.Sprintf("the reversal names the opening balances %s, which change only by an edit", r.Document)
			}
		}
		if s.AsOf != "" && date > s.AsOf {
			return fmt.Sprintf("as_of %s is before the reversal or edit of %s, dated %s", s.AsOf, r.Document, date)
		}
	}
	if s.AsOf != "" {
		for _, d := range s.Documents {
			if d.Date > s.AsOf {
				return fmt.Sprintf("as_of %s is before document %s's date, %s", s.AsOf, d.ID, d.Date)
			}
		}
	}
	codes := map[string]Account{}
	for _, a := range s.Accounts {
		codes[a.Code] = a
	}
	if st := s.Settings; st != nil {
		for k, code := range st.Accounts {
			if _, ok := codes[code]; !ok {
				return fmt.Sprintf("settings.accounts.%s names account %s, which is not in accounts", k, code)
			}
		}
		for code, cur := range st.MoneyAccounts {
			if _, ok := codes[code]; !ok {
				return fmt.Sprintf("settings.money_accounts names account %s, which is not in accounts", code)
			}
			if _, ok := s.Currencies[cur]; !ok {
				return fmt.Sprintf("settings.money_accounts gives account %s the currency %s, which is not in currencies", code, cur)
			}
		}
	}
	types := map[string]string{"retained-earnings": "equity", "depreciation": "expense", "income-tax-expense": "expense"}
	seen := map[string]bool{}
	for _, a := range s.Accounts {
		want, ok := types[a.Role]
		switch {
		case !ok:
		case seen[a.Role]:
			return fmt.Sprintf("two accounts have the role %s", a.Role)
		case a.Type != want:
			return fmt.Sprintf("account %s has the role %s but is %s, not %s", a.Code, a.Role, a.Type, want)
		}
		seen[a.Role] = true
	}
	// The month-end revaluation (README, "The revalued accounts and the period-end rates").
	flagged := false
	for _, a := range s.Accounts {
		if !a.Revalue {
			continue
		}
		flagged = true
		switch {
		case a.Type != "asset" && a.Type != "liability":
			return fmt.Sprintf("account %s is marked revalue but is %s: only an asset or a liability may be (COA-10)", a.Code, a.Type)
		case s.Settings != nil && (s.Settings.Accounts["prepayments"] == a.Code || s.Settings.Accounts["deferred_revenue"] == a.Code):
			return fmt.Sprintf("account %s is marked revalue but holds prepayments or deferred revenue (COA-10)", a.Code)
		}
	}
	if flagged {
		var code string
		if s.Settings != nil {
			code = s.Settings.Accounts["unrealised_fx"]
		}
		a, ok := codes[code]
		switch {
		case s.AsOf == "" || code == "":
			return "a scenario with an account marked revalue needs as_of and settings.accounts.unrealised_fx"
		case !ok || !a.Active || a.Group || a.Role != "" || a.Type != "revenue" && a.Type != "expense":
			return fmt.Sprintf("settings.accounts.unrealised_fx names %s, which is not an active revenue or expense account without a role", code)
		case s.Statements:
			return "the statements of a scenario with an account marked revalue are not in the format yet"
		}
	}
	if reason := checkPeriodEndRates(s); reason != "" {
		return reason
	}
	if s.Statements && !seen["retained-earnings"] {
		return "statements need an account with the role retained-earnings"
	}
	if len(s.KPIs) > 0 && !s.Statements {
		return "kpis come only with statements"
	}
	return ""
}

// CheckResult validates a result against result.schema.json.
func CheckResult(data []byte) error {
	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return err
	}
	return resultSchema.Validate(v)
}
