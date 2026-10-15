package differential

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

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
		if _, err := time.Parse(time.DateOnly, r.Date); err != nil {
			return Scenario{}, &InvalidError{fmt.Sprintf("the reversal of %s has the date %q, which is not a calendar date", r.Document, r.Date)}
		}
	}
	return s, nil
}

// CheckResult validates a result against result.schema.json.
func CheckResult(data []byte) error {
	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return err
	}
	return resultSchema.Validate(v)
}
