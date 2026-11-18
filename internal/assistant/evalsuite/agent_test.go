package evalsuite_test

import (
	"os"
	"testing"
	"testing/fstest"

	"github.com/cognizhi/fyniq/internal/assistant/evalsuite"
)

// The repository's agent suites load, and an agent suite that could not judge soundly, or could not be planted, is
// refused as it loads: a plant made as a role the harness does not have, a recorded plant made by the Owner who signs
// it, a "$<id>" naming no earlier plant (a case's plants see the suite's), a model that is neither the suite's nor
// unavailable, and an unavailable model that makes a proposal (p1-company-secretary task 7.1).
// req: QA-AI-07
func TestTheAgentSuitesAreWellFormed(t *testing.T) {
	if _, err := evalsuite.LoadAgents(os.DirFS("../../../evals")); err != nil {
		t.Fatal(err)
	}
	suite := func(plants, cases string) fstest.MapFS {
		return fstest.MapFS{"a/agent.json": {Data: []byte(`{"id":"a","agent":"company_secretary","version":1,"kind":"agent","description":"x",` +
			`"company":{"seed":1,"journals":10},"task_types":["sec.create_task"],"plant":[` + plants + `],"cases":[` + cases + `]}`)}}
	}
	fund := `{"id":"fund","path":"/v1/contacts","body":{"legal_name":"Fund","investor":true}}`
	rights := func(as, ref string) string {
		return `{"id":"rights","path":"/v1/investor-rights","as":"` + as + `","record":true,"body":{"holder_id":"` + ref + `"}}`
	}
	runCase := func(extra string) string {
		return `{"id":"c","description":"x","today":"2027-07-01",` + extra + `"expect":{"findings":[{"key":"calendar:x:*","status":"due"}]},"solution":{"text":"File it."}}`
	}
	entry := `{"id":"entry","path":"/v1/controller-entries","as":"company_secretary","record":true,"body":{"holder_id":"$fund"}}`
	if _, err := evalsuite.LoadAgents(suite(fund+","+rights("accountant", "$fund"), runCase(`"plant":[`+entry+`],`))); err != nil {
		t.Errorf("a case's plant naming the suite's: %v", err)
	}
	if _, err := evalsuite.LoadAgents(suite(fund, runCase(`"model":"unavailable",`))); err != nil {
		t.Errorf("a case whose model is unavailable: %v", err)
	}
	for name, fsys := range map[string]fstest.MapFS{
		"a plant made as a role the harness does not have": suite(fund+","+rights("auditor", "$fund"), runCase(``)),
		"a recorded plant made by the Owner":               suite(fund+","+rights("owner", "$fund"), runCase(``)),
		"a recorded plant made by no named role":           suite(fund+","+rights("", "$fund"), runCase(``)),
		"a reference to no earlier plant":                  suite(rights("accountant", "$fund")+","+fund, runCase(``)),
		"a case's plant naming another case's":             suite(fund, runCase(`"plant":[`+rights("accountant", "$other")+`],`)),
		"two plants with one ID":                           suite(fund+","+fund, runCase(``)),
		"a model that is not unavailable":                  suite(fund, runCase(`"model":"gpt-x",`)),
		"an unavailable model that proposes": suite(fund, `{"id":"c","description":"x","today":"2027-07-01","model":"unavailable",`+
			`"expect":{"findings":[{"key":"k","status":"due"}],"proposals":["sec.create_task"]},`+
			`"solution":{"text":"File it.","proposals":[{"task_type":"sec.create_task","payload":{"title":"x"}}]}}`),
	} {
		if _, err := evalsuite.LoadAgents(fsys); err == nil {
			t.Errorf("%s: loaded", name)
		} else {
			t.Logf("%s: %v", name, err)
		}
	}
}

// A wanted key ending in "*" matches a finding and its tasks by prefix, as a record's ID in a key is the run's own; a
// case whose model is unavailable passes only when the run says its AI step did not run, in its report and in Fyniq's
// summary.
// req: QA-AI-07
// req: AI-64
// req: G4-110
func TestTheAgentJudge(t *testing.T) {
	if !evalsuite.SameKey("calendar:register_nominee_central:01a1:2027-06-24", "calendar:register_nominee_central:*") ||
		evalsuite.SameKey("calendar:register_nominee_entry:01a1:2027-06-24", "calendar:register_nominee_central:*") ||
		evalsuite.SameKey("calendar:gst_f5:2026-12-31x", "calendar:gst_f5:2026-12-31") {
		t.Error("a key ending in * matches by its prefix, and any other key whole")
	}
	nominee := evalsuite.AgentCase{ID: "n", Expect: evalsuite.AgentExpect{
		Findings: []evalsuite.WantFinding{{Key: "calendar:register_nominee_central:*", Status: "due", Window: "overdue"}},
		Tasks:    map[string]int{"calendar:register_nominee_central:*": 1}}}
	ran := evalsuite.AgentOutcome{Status: "done", Findings: []evalsuite.WantFinding{{Key: "calendar:register_nominee_central:01a1:2027-06-24", Status: "due", Window: "overdue"}},
		Tasks: map[string]int{"calendar:register_nominee_central:01a1:2027-06-24": 1}}
	if v := evalsuite.JudgeAgent(nominee, ran); !v.Pass {
		t.Errorf("the nominee filing found and its task open: %s", v.Why)
	}
	untasked := ran
	untasked.Tasks = map[string]int{}
	if evalsuite.JudgeAgent(nominee, untasked).Pass {
		t.Error("the nominee filing found with no task open passes")
	}

	down := evalsuite.AgentCase{ID: "u", Model: "unavailable", Expect: evalsuite.AgentExpect{Findings: []evalsuite.WantFinding{{Key: "calendar:financial_year:-", Status: "not_assessed"}}}}
	missed := evalsuite.AgentOutcome{Status: "done", Findings: []evalsuite.WantFinding{{Key: "calendar:financial_year:-", Status: "not_assessed"}},
		ModelMissed: "the AI step could not run: the provider did not answer", Summary: "1 item is not assessed. The AI could not add its explanation this time."}
	if v := evalsuite.JudgeAgent(down, missed); !v.Pass {
		t.Errorf("a run that says its AI step did not run: %s", v.Why)
	}
	silent := missed
	silent.ModelMissed = ""
	if evalsuite.JudgeAgent(down, silent).Pass {
		t.Error("a run whose report does not say its AI step did not run passes")
	}
	unsaid := missed
	unsaid.Summary = "1 item is not assessed."
	if evalsuite.JudgeAgent(down, unsaid).Pass {
		t.Error("a run whose summary does not say the AI could not explain passes")
	}
}
