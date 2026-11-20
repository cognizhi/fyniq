package differential

import (
	"bytes"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	schemas "github.com/cognizhi/fyniq/test/differential"

	"github.com/cognizhi/fyniq/internal/domain/allocation"
	"github.com/cognizhi/fyniq/internal/domain/money"
)

type allocationCase struct {
	Name       string   `json:"name"`
	Currency   string   `json:"currency"`
	MinorUnits int      `json:"minor_units"`
	Amount     string   `json:"amount"`
	Key        string   `json:"key"`
	Targets    []string `json:"targets"`
	Unplaced   string   `json:"unplaced"`
	Want       struct {
		Parts       []string `json:"parts"`
		Unallocated string   `json:"unallocated"`
	} `json:"want"`
}

// work splits a case as the engine does: its key turns the targets into weights, and the split is ALLOC-PRORATA's.
func (c allocationCase) work(t *testing.T) ([]string, string) {
	t.Helper()
	cur, err := money.Lookup(c.Currency)
	if err != nil {
		t.Fatal(err)
	}
	if cur.MinorUnits != c.MinorUnits {
		t.Fatalf("%s: %s has %d minor units, the case says %d", c.Name, c.Currency, cur.MinorUnits, c.MinorUnits)
	}
	amount, err := money.Parse(c.Amount, cur)
	if err != nil {
		t.Fatalf("%s: %v", c.Name, err)
	}
	var units []int64
	var unplaced int64
	switch c.Key {
	case "percent":
		units, unplaced, err = allocation.PercentUnits(c.Targets)
	case "quantity":
		if units, err = allocation.QuantityUnits(c.Targets); err == nil && c.Unplaced != "" {
			var u []int64
			if u, err = allocation.QuantityUnits([]string{c.Unplaced}); err == nil {
				unplaced = u[0]
			}
		}
	case "weights":
		for _, s := range c.Targets {
			n, perr := strconv.ParseInt(s, 10, 64)
			if perr != nil {
				t.Fatalf("%s: weight %q", c.Name, s)
			}
			units = append(units, n)
		}
		if c.Unplaced != "" {
			if unplaced, err = strconv.ParseInt(c.Unplaced, 10, 64); err != nil {
				t.Fatalf("%s: unplaced %q", c.Name, c.Unplaced)
			}
		}
	default:
		t.Fatalf("%s: no key %q", c.Name, c.Key)
	}
	if err != nil {
		t.Fatalf("%s: %v", c.Name, err)
	}
	res, err := allocation.Split(amount, units, unplaced)
	if err != nil {
		t.Fatalf("%s: %v", c.Name, err)
	}
	parts := make([]string, len(res.Parts))
	for i, p := range res.Parts {
		parts[i] = p.String()
	}
	return parts, res.Unallocated.String()
}

// The hand-worked allocation cases and the 240 the oracle generated (FR-DIM-03, ALLOC-PRORATA) conform to their schema and the engine reproduces each: every target's part and the part left
// unallocated, to the cent. The oracle's own module reproduces the same files (oracle/tests/test_allocation.py), which is how the two implementations are held to one rule (QA-ENG-02).
// req: QA-ENG-02, FR-DIM-03
func TestTheAllocationCasesConformAndTheEngineReproducesEach(t *testing.T) {
	schema := compile("allocation.schema.json", schemas.AllocationSchema)
	for file, least := range map[string]int{"hand.json": 15, "generated.json": 240} {
		raw, err := os.ReadFile("../../test/differential/allocations/" + file)
		if err != nil {
			t.Fatal(err)
		}
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		for i, d := range doc.([]any) {
			if err := schema.Validate(d); err != nil {
				t.Errorf("%s case %d: %v", file, i, err)
			}
		}
		var list []allocationCase
		if err := json.Unmarshal(raw, &list); err != nil {
			t.Fatal(err)
		}
		if len(list) < least {
			t.Fatalf("%s: only %d cases", file, len(list))
		}
		for _, c := range list {
			parts, unallocated := c.work(t)
			if strings.Join(parts, " ") != strings.Join(c.Want.Parts, " ") || unallocated != c.Want.Unallocated {
				t.Errorf("%s: %s: engine %v + %s unallocated, reference %v + %s", file, c.Name, parts, unallocated, c.Want.Parts, c.Want.Unallocated)
			}
		}
	}
}
