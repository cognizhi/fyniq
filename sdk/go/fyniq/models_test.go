package fyniq

import (
	"encoding/json"
	"strings"
	"testing"
)

// The typed models are the contract's own (FR-INT-17, rev 0.611): a body built from one marshals to the contract's JSON, leaving out what is optional and unset; an answer decodes
// into one, money staying a decimal string; and a schema named like a hand-written type of the client has Model after its name.
// req: FR-INT-17
func TestTheModelsBuildBodiesAndReadAnswers(t *testing.T) {
	body, err := json.Marshal(IntegrationTaskCreate{ExternalRef: "OPS-1", Title: "Chase the letter"})
	if err != nil || string(body) != `{"external_ref":"OPS-1","title":"Chase the letter"}` {
		t.Fatalf("a body: %s %v", body, err)
	}
	var task IntegrationTask
	answer := &Response{Body: []byte(`{"id":"0193","external_ref":"OPS-1","title":"Chase","status":"open","client_id":"jira","created_at":"2026-11-21T00:00:00Z","updated_at":"2026-11-21T00:00:00Z","version":3}`)}
	if err := answer.Decode(&task); err != nil || task.Status != "open" || task.Version != 3 || task.DueOn != nil {
		t.Fatalf("an answer: %+v %v", task, err)
	}
	var problem ProblemModel
	if err := json.Unmarshal([]byte(`{"code":"X-01","title":"t","status":400}`), &problem); err != nil {
		t.Fatalf("a schema named like the client's Problem: %v", err)
	}
}

// A number is never a float: a contract number reads into json.Number, so no amount is rounded on the way in.
// req: FR-INT-17
// req: ENG-10
func TestNoModelHoldsAFloat(t *testing.T) {
	src, err := readFile("models_gen.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"float32", "float64"} {
		if strings.Contains(src, bad) {
			t.Errorf("the models hold a %s: money and numbers stay decimal strings or json.Number", bad)
		}
	}
}
