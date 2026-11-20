//go:build local

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/authz"
	capdom "github.com/cognizhi/fyniq/internal/domain/capture"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The capture eval suites (QA-AI-02, AI-18, CTL-AI-05; p1-document-capture task 4.2, design D10). Each case of evals/<suite>/capture.json is a synthetic
// document, captured and read through the API and the real reading path (masking, screening, the strict parse, the platform's checks) by the model the run
// sets, and its stored reading is judged field by field: what the document prints exactly, null for what it does not, and none of the words an injected
// instruction or a removed number would put in a field.
//
// First the harness checks itself: a model that answers each case's own right reading passes it, and the same reading with a figure changed, a field
// invented or an injected instruction repeated fails it (a reading the pipeline stopped counts as failed). Then every model recorded in
// evals/cassettes/capture reads each case from its recordings, offline; a case with no recording is reported, and with FYNIQ_EVALS_RELEASE=1 it fails, as
// does a suite no model has recorded. FYNIQ_AI_LIVE=1 with FYNIQ_EVALS_MODEL records a model's readings on the synthetic documents only (DEV-08) [live-verify:
// it needs a developer's key]. With FYNIQ_EVALS_OUT the report is written there as capture.json, which `make evals` folds into its report.
// req: QA-AI-02
// req: AI-18
// req: CTL-AI-05
// asvs: V8.2.2
func TestTheCaptureSuites(t *testing.T) {
	suites, err := capdom.LoadEvals(os.DirFS("../../evals"))
	if err != nil {
		t.Fatal(err)
	}
	cassettes := "../../evals/cassettes/capture"
	live := os.Getenv(local.LiveEnv) == "1"
	release := os.Getenv("FYNIQ_EVALS_RELEASE") == "1"
	models, err := assistantModels(cassettes)
	if err != nil {
		t.Fatal(err)
	}
	if live {
		if models = []string{os.Getenv("FYNIQ_EVALS_MODEL")}; models[0] == "" {
			t.Fatalf("%s=1 records one model: name it with FYNIQ_EVALS_MODEL", local.LiveEnv)
		}
	}
	rep := evalReport{Mode: "replay", Release: release, Suites: []evalSuiteReport{}}
	if live {
		rep.Mode = "live"
	}
	for _, s := range suites {
		sr := evalSuiteReport{ID: s.ID, Kind: "capture", Version: s.Version, Cases: len(s.Cases), Harness: "sound", Runs: []evalRun{}}
		t.Run(s.ID, func(t *testing.T) {
			var weak []string
			right, _ := captureRun(t, s, "harness-right", func(c capdom.EvalCase) ports.ModelProvider { return &readingModel{reading: capdom.ReadingOf(c.Expect)} })
			for _, c := range s.Cases {
				if v := right[c.ID]; !v.Pass {
					weak = append(weak, c.ID+": its own right reading fails: "+v.Why)
				}
			}
			wrong, _ := captureRun(t, s, "harness-wrong", func(c capdom.EvalCase) ports.ModelProvider { return &readingModel{reading: wrongReading(c)} })
			for _, c := range s.Cases {
				if v := wrong[c.ID]; v.Pass && !c.Expect.Screened { // a screened document never reaches the model, so its answer cannot be wrong
					weak = append(weak, c.ID+": a wrong reading passes")
				}
			}
			if len(weak) > 0 {
				sr.Harness = "not sound: " + strings.Join(weak, "; ")
				t.Errorf("%s does not judge soundly:\n%s", s.ID, strings.Join(weak, "\n"))
			}
			for _, m := range models {
				var inner ports.ModelProvider = local.Replay{Dir: cassettes}
				if live {
					if inner, err = local.Models(cassettes, local.LiveAgent()); err != nil {
						t.Fatal(err)
					}
				}
				run := evalRun{Model: m, Failed: map[string]string{}}
				got, missing := captureRun(t, s, m, func(capdom.EvalCase) ports.ModelProvider { return &unrecorded{inner: inner} })
				for _, c := range s.Cases {
					switch v := got[c.ID]; {
					case missing[c.ID]:
						run.Unrecorded = append(run.Unrecorded, c.ID)
					case v.Pass:
						run.Passed++
					default:
						run.Failed[c.ID] = v.Why
					}
				}
				switch {
				case len(run.Failed) > 0:
					run.Result = "fail"
				case len(run.Unrecorded) > 0:
					run.Result = "incomplete"
				default:
					run.Result = "pass"
				}
				sr.Runs = append(sr.Runs, run)
				if run.Result == "fail" || release && run.Result == "incomplete" {
					t.Errorf("%s on %s: %s, %d of %d right: %v %v", s.ID, m, run.Result, run.Passed, len(s.Cases), run.Failed, run.Unrecorded)
				}
			}
			if len(sr.Runs) == 0 {
				sr.Note = "no model's readings are recorded for it: record them with FYNIQ_AI_LIVE=1 FYNIQ_EVALS_MODEL=<model id>, on the synthetic documents only"
				if release {
					t.Errorf("%s: a release needs every case recorded and passed, and no model's readings are recorded", s.ID)
				}
			}
		})
		rep.Suites = append(rep.Suites, sr)
	}
	if dir := os.Getenv("FYNIQ_EVALS_OUT"); dir != "" {
		raw, err := json.MarshalIndent(rep, "", "  ")
		if err == nil {
			err = os.MkdirAll(dir, 0o750)
		}
		if err == nil {
			err = os.WriteFile(filepath.Join(dir, "capture.json"), append(raw, '\n'), 0o600)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
}

// wrongReading is a case's right reading with what a careless model gets wrong: a total changed, a field the document does not show invented, and, for a
// document that carries an instruction or a removed number, those words repeated in a line's description.
func wrongReading(c capdom.EvalCase) capdom.Reading {
	r := capdom.ReadingOf(c.Expect)
	bad := "999.99"
	r.Total.Value = &bad
	uen := "201234567K"
	if _, ok := c.Expect.Fields["supplier_uen"]; !ok {
		r.SupplierUEN.Value = &uen
	}
	if len(c.Expect.Absent) > 0 && len(r.Lines) > 0 {
		words := strings.Join(c.Expect.Absent, " ")
		r.Lines[0].Description.Value = &words
	}
	return r
}

// readingModel answers every request with one reading.
type readingModel struct{ reading capdom.Reading }

func (readingModel) Capabilities() ports.ModelCapabilities { return readsFilesCaps() }
func (m *readingModel) Call(_ context.Context, r ports.ModelRequest) (ports.ModelResponse, error) {
	b, _ := json.Marshal(m.reading)
	return ports.ModelResponse{Model: r.Model, Parts: []ports.ModelPart{{Kind: ports.PartText, Text: string(b)}}, StopReason: ports.StopEnd, Usage: ports.ModelUsage{Input: 100, Output: 20}}, nil
}
func (m *readingModel) Stream(ctx context.Context, r ports.ModelRequest, _ func(ports.ModelEvent) error) (ports.ModelResponse, error) {
	return m.Call(ctx, r)
}
func (readingModel) ListModels(context.Context, string) (ports.ModelPage, error) {
	return ports.ModelPage{}, nil
}

func readsFilesCaps() ports.ModelCapabilities {
	c := full
	c.PDF, c.Images = true, true
	return c
}

// swapProvider is the provider each case sets.
type swapProvider struct{ cur ports.ModelProvider }

func (s *swapProvider) Capabilities() ports.ModelCapabilities { return readsFilesCaps() }
func (s *swapProvider) Call(ctx context.Context, r ports.ModelRequest) (ports.ModelResponse, error) {
	return s.cur.Call(ctx, r)
}
func (s *swapProvider) Stream(ctx context.Context, r ports.ModelRequest, on func(ports.ModelEvent) error) (ports.ModelResponse, error) {
	return s.cur.Stream(ctx, r, on)
}
func (s *swapProvider) ListModels(ctx context.Context, p string) (ports.ModelPage, error) {
	return s.cur.ListModels(ctx, p)
}

// captureRun captures and reads every case of the suite in an organisation of its own, as the person who enters bills, by the provider of for each, and
// judges the readings. missing says which cases a replay had no recording for.
func captureRun(t *testing.T, s capdom.EvalSuite, model string, of func(capdom.EvalCase) ports.ModelProvider) (map[string]capdom.Verdict, map[string]bool) {
	t.Helper()
	e := newEnv(t)
	aiSwitchedOn(e)
	staging, err := local.NewDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e.api.deps.Staging = staging
	sg := routable(e, "Capture evals "+model, "SG", []string{model})
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.AIRoutes().Save(ctx, storage.AIRoute{Agent: "document_capture", Version: 1, Classes: []string{"business-figures", "personal-data"}, MadeBy: "user:evals",
			MadeAt: e.clock.Now(), Steps: []storage.AIRouteStep{{ConnectionID: sg, Model: model}}})
		return err
	})
	swap := &swapProvider{}
	e.api.deps.AIProviders = func(context.Context, storage.UnitOfWork, storage.AIConnection) (ports.ModelProvider, error) {
		return swap, nil
	}
	acc := e.as(authz.Accountant)
	verdicts, missing := map[string]capdom.Verdict{}, map[string]bool{}
	for _, c := range s.Cases {
		p := of(c)
		swap.cur = p
		e.clock.Advance(10 * time.Minute) // within the model calls' limits
		up := e.capture(acc, c.FileName, []byte(c.Text))
		if up.Status != http.StatusCreated {
			t.Fatalf("%s: capturing: %d %s", c.ID, up.Status, up.Raw)
		}
		r := e.call(http.MethodPost, "/v1/captures/"+up.str("id")+"/read", acc, nil, "If-Match", up.Header.Get("ETag"))
		if u, ok := p.(*unrecorded); ok && u.missing {
			missing[c.ID] = true
		}
		if r.Status != http.StatusOK {
			// A reading the pipeline stopped or could not parse is no right reading, except that screening stopping a document made to be stopped is right.
			verdicts[c.ID] = capdom.Verdict{Pass: c.Expect.Screened && strings.HasPrefix(r.str("code"), "AI-SCREEN"), Why: "no reading was kept: " + r.str("code") + " " + r.str("detail")}
			continue
		}
		raw, err := json.Marshal(r.Body["reading"])
		if err != nil {
			t.Fatal(err)
		}
		var got capdom.Reading
		if err := json.Unmarshal(raw, &got); err != nil {
			verdicts[c.ID] = capdom.Verdict{Why: "the reading cannot be read: " + err.Error()}
			continue
		}
		verdicts[c.ID] = c.Judge(got)
	}
	return verdicts, missing
}
