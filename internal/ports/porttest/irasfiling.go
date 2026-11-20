package porttest

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/cognizhi/fyniq/internal/ports"
)

// IRASDriver is what only a stand-in can do: start an outage. A real adapter's sandbox run passes nil, and the steps marked stub_only are skipped.
type IRASDriver interface{ SetOutage(on bool) }

// IRASFilingContract runs contracts/connectors/iras-filing.contract.json against f. It is the one suite every adapter of ports.IRASFiler must pass.
func IRASFilingContract(t *testing.T, f ports.IRASFiler, drv IRASDriver, contractPath string) {
	t.Helper()
	raw, err := os.ReadFile(contractPath) // #nosec G304 -- a path the test names
	if err != nil {
		t.Fatal(err)
	}
	var c struct {
		Contract string `json:"contract"`
		Steps    []struct {
			Name         string          `json:"name"`
			Call         string          `json:"call"`
			Kind         string          `json:"kind"`
			Period       string          `json:"period"`
			Key          string          `json:"key"`
			Save         string          `json:"save"`
			ValidateOnly bool            `json:"validate_only"`
			StubOnly     bool            `json:"stub_only"`
			Expect       json.RawMessage `json:"expect"`
		} `json:"steps"`
	}
	if err := json.Unmarshal(raw, &c); err != nil || c.Contract != "iras-filing@1" || len(c.Steps) == 0 {
		t.Fatalf("the contract is %q, %d steps, %v", c.Contract, len(c.Steps), err)
	}
	saved := map[string]string{}
	for _, s := range c.Steps {
		if s.StubOnly && drv == nil {
			continue
		}
		var want struct {
			Accepted  *bool  `json:"accepted"`
			SameAs    string `json:"same_as"`
			Reference string `json:"reference"`
			Problems  string `json:"problems"`
		}
		if len(s.Expect) > 0 {
			_ = json.Unmarshal(s.Expect, &want)
		}
		switch s.Call {
		case "file":
			a, err := f.File(context.Background(), ports.IRASFiling{Kind: s.Kind, Period: s.Period, Key: s.Key, Payload: []byte("opaque"), ValidateOnly: s.ValidateOnly})
			if err != nil {
				t.Fatalf("%s: %v", s.Name, err)
			}
			if want.Accepted != nil && a.Accepted != *want.Accepted {
				t.Errorf("%s: accepted is %v", s.Name, a.Accepted)
			}
			if want.Reference == "empty" && a.Reference != "" {
				t.Errorf("%s: a reference %q", s.Name, a.Reference)
			}
			if want.Problems == "non-empty" && len(a.Problems) == 0 {
				t.Errorf("%s: no problems named", s.Name)
			}
			if want.SameAs != "" && a.Reference != saved[want.SameAs] {
				t.Errorf("%s: the reference is %q, not %q", s.Name, a.Reference, saved[want.SameAs])
			}
			if s.Save != "" {
				saved[s.Save] = a.Reference
			}
		case "outage":
			drv.SetOutage(true)
			if _, err := f.File(context.Background(), ports.IRASFiling{Kind: ports.FilingGSTF5, Period: "2027-03-31", Key: "k9", Payload: []byte("x")}); !errors.Is(err, ports.ErrIRASUnavailable) {
				t.Errorf("%s: %v", s.Name, err)
			}
			drv.SetOutage(false)
		default:
			t.Fatalf("%s: unknown call %q", s.Name, s.Call)
		}
	}
}
