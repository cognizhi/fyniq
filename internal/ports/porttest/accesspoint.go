package porttest

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"testing"

	"github.com/cognizhi/fyniq/internal/ports"
)

// AccessPointDriver is what only a stand-in can do: receive a document as a supplier's access point would, and start
// an outage. A real adapter's sandbox run passes nil, and the steps marked stub_only are skipped.
type AccessPointDriver interface {
	Receive(from string, payload []byte)
	SetOutage(on bool)
}

type apStep struct {
	Name       string          `json:"name"`
	Call       string          `json:"call"`
	ID         string          `json:"id"`
	Key        string          `json:"key"`
	To         string          `json:"to"`
	Payload    string          `json:"payload"`
	Save       string          `json:"save"`
	Of         string          `json:"of"`
	From       string          `json:"from"`
	Cursor     string          `json:"cursor"`
	SaveCursor bool            `json:"save_cursor"`
	StubOnly   bool            `json:"stub_only"`
	Expect     json.RawMessage `json:"expect"`
}

type apExpect struct {
	Registered *bool    `json:"registered"`
	Accepts    []string `json:"accepts"`
	SameAs     string   `json:"same_as"`
	State      string   `json:"state"`
	Ack        string   `json:"ack"`
	Reasons    string   `json:"reasons"`
	Count      *int     `json:"count"`
	From       string   `json:"from"`
}

// AccessPointContract runs contracts/connectors/peppol-access-point.contract.json against ap: valid is a document the network
// accepts and invalid one it refuses. It is the one suite every adapter of ports.AccessPoint must pass.
func AccessPointContract(t *testing.T, ap ports.AccessPoint, drv AccessPointDriver, contractPath string, valid, invalid []byte) {
	t.Helper()
	raw, err := os.ReadFile(contractPath) // #nosec G304 -- a path the test names
	if err != nil {
		t.Fatal(err)
	}
	var c struct {
		Contract string   `json:"contract"`
		Steps    []apStep `json:"steps"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	if c.Contract != "peppol-access-point@1" || len(c.Steps) == 0 {
		t.Fatalf("the contract is %q with %d steps", c.Contract, len(c.Steps))
	}
	ctx := context.Background()
	saved := map[string]string{}
	cursor := ""
	for _, s := range c.Steps {
		if s.StubOnly && drv == nil {
			continue
		}
		var want apExpect
		if len(s.Expect) > 0 {
			if err := json.Unmarshal(s.Expect, &want); err != nil {
				t.Fatalf("%s: %v", s.Name, err)
			}
		}
		payload := valid
		if s.Payload == "invalid" {
			payload = invalid
		}
		switch s.Call {
		case "lookup":
			p, err := ap.Lookup(ctx, s.ID)
			if err != nil {
				t.Fatalf("%s: %v", s.Name, err)
			}
			if want.Registered != nil && p.Registered != *want.Registered {
				t.Errorf("%s: registered is %v", s.Name, p.Registered)
			}
			for _, a := range want.Accepts {
				if !slices.Contains(p.Accepts, a) {
					t.Errorf("%s: it does not accept %s", s.Name, a)
				}
			}
		case "send":
			id, err := ap.Send(ctx, ports.AccessPointSend{To: s.To, DocumentUUID: "0a1b2c3d-1111-4222-8333-444455556666", Version: 1, Kind: "invoice", Payload: payload, Key: s.Key})
			if err != nil || id == "" {
				t.Fatalf("%s: %q, %v", s.Name, id, err)
			}
			if want.SameAs != "" && id != saved[want.SameAs] {
				t.Errorf("%s: a second send gave %s, not %s", s.Name, id, saved[want.SameAs])
			}
			if s.Save != "" {
				saved[s.Save] = id
			}
		case "status":
			st, err := ap.Status(ctx, saved[s.Of])
			if err != nil {
				t.Fatalf("%s: %v", s.Name, err)
			}
			if st.State != want.State {
				t.Errorf("%s: the state is %s, not %s", s.Name, st.State, want.State)
			}
			if want.Ack == "non-empty" && st.IRASAck == "" {
				t.Errorf("%s: no acknowledgement ID", s.Name)
			}
			if want.Reasons == "non-empty" && len(st.Reasons) == 0 {
				t.Errorf("%s: no reasons", s.Name)
			}
		case "receive":
			drv.Receive(s.From, valid)
		case "poll":
			from := s.Cursor
			if from == "saved" {
				from = cursor
			}
			docs, next, err := ap.Poll(ctx, from)
			if err != nil {
				t.Fatalf("%s: %v", s.Name, err)
			}
			if want.Count != nil && len(docs) != *want.Count {
				t.Errorf("%s: %d documents, want %d", s.Name, len(docs), *want.Count)
			}
			if want.From != "" && (len(docs) == 0 || docs[0].From != want.From) {
				t.Errorf("%s: wrong sender", s.Name)
			}
			if s.SaveCursor {
				cursor = next
			}
		case "outage":
			drv.SetOutage(true)
			if _, err := ap.Lookup(ctx, "0195:SGUEN201912345A"); !errors.Is(err, ports.ErrAccessPointUnavailable) {
				t.Errorf("%s: lookup gave %v", s.Name, err)
			}
			if _, err := ap.Send(ctx, ports.AccessPointSend{Key: "k3", Payload: valid}); !errors.Is(err, ports.ErrAccessPointUnavailable) {
				t.Errorf("%s: send gave %v", s.Name, err)
			}
			if _, err := ap.Status(ctx, saved["tx1"]); !errors.Is(err, ports.ErrAccessPointUnavailable) {
				t.Errorf("%s: status gave %v", s.Name, err)
			}
			if _, _, err := ap.Poll(ctx, ""); !errors.Is(err, ports.ErrAccessPointUnavailable) {
				t.Errorf("%s: poll gave %v", s.Name, err)
			}
			drv.SetOutage(false)
			if _, err := ap.Lookup(ctx, "0195:SGUEN201912345A"); err != nil {
				t.Errorf("%s: after the outage: %v", s.Name, err)
			}
		default:
			t.Fatalf("%s: unknown call %q", s.Name, s.Call)
		}
	}
}
