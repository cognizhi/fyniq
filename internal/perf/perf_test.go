package perf

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/bankfile"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/money"
)

// The 95th percentile is the nearest rank; a run passes only when every gated budget is met, and its report says each
// budget's limit, what was measured and whether it was met (QA-PERF-01).
// req: QA-PERF-01
func TestTheReportSaysWhatEachBudgetMeasured(t *testing.T) {
	var ds []time.Duration
	for i := 1; i <= 100; i++ {
		ds = append(ds, time.Duration(i)*time.Millisecond)
	}
	if Percentile(ds, 95) != 95*time.Millisecond || Percentile(ds, 50) != 50*time.Millisecond || Percentile(ds, 100) != 100*time.Millisecond ||
		Percentile(nil, 95) != 0 {
		t.Fatal("the percentiles")
	}
	r := Report{At: time.Date(2099, 3, 1, 2, 0, 0, 0, time.UTC), Version: "0.9.0", Lines: 1_000_000,
		Grown:   GrowReport{Journals: 20_000, Lines: 1_000_000, Took: 30 * time.Minute, LinesPerSecond: 555},
		Results: []Result{sampled(Reads, ds, nil), once(Imports, 61*time.Second, nil), {Budget: Cold, Note: "staging"}}}
	r.Settle()
	if r.Passed || !r.Results[0].Passed || r.Results[1].Passed {
		t.Fatalf("a missed gated budget fails the run: %+v", r)
	}
	md := r.Markdown()
	for _, want := range []string{"a gated budget missed", "| NFR-PERF-01 |", "p95 ≤ 300 ms", "p95 95 ms", "| NFR-PERF-04 |", "1m1s", "✖ missed",
		"| NFR-PERF-06 |", "reported (staging)", "1000000 lines in 20000 journals"} {
		if !strings.Contains(md, want) {
			t.Errorf("the report lacks %q:\n%s", want, md)
		}
	}
	dir := t.TempDir()
	if err := Write(dir, r); err != nil {
		t.Fatal(err)
	}
	var back Report
	b, _ := os.ReadFile(filepath.Join(dir, "report.json"))
	if err := json.Unmarshal(b, &back); err != nil || back.Passed || len(back.Results) != 3 {
		t.Errorf("report.json: %+v %v", back, err)
	}
	if ReportsBudget(1_000_000).Limit != 2*time.Second || ReportsBudget(10_000_000).Limit != 5*time.Second {
		t.Error("NFR-PERF-02's limits: 2 s at 1 M lines, 5 s at 10 M")
	}
}

// The import's statement is a valid OFX file Fyniq's own reader takes whole: 10,000 lines, each its own, in the
// account and currency it names (NFR-PERF-04).
// req: NFR-PERF-04
func TestTheStatementIsOneFyniqReads(t *testing.T) {
	sgd, err := money.Lookup("SGD")
	if err != nil {
		t.Fatal(err)
	}
	file := Statement("PERF-1", "SGD", StatementLines, time.Date(2099, 3, 1, 0, 0, 0, 0, time.UTC), 7)
	statements, err := bankfile.Parse("ofx", file, nil, sgd, bankfile.DefaultLimits)
	if err != nil || len(statements) != 1 {
		t.Fatalf("the statement: %v", err)
	}
	s := statements[0]
	if s.Account != "PERF-1" || s.Currency.Code != "SGD" || len(s.Lines) != StatementLines {
		t.Fatalf("read back: %s %s %d lines", s.Account, s.Currency.Code, len(s.Lines))
	}
	seen := map[string]bool{}
	for _, l := range s.Lines {
		if seen[l.Reference] {
			t.Fatalf("a line repeats: %s", l.Reference)
		}
		seen[l.Reference] = true
	}
	if len(file) > bankfile.DefaultLimits.MaxBytes {
		t.Errorf("the statement is %d bytes, over the import's limit", len(file))
	}
}

// The measurer calls the API as the web app does, as an Owner: reads by many at once, the reports with the prior
// period, each journal saved and then posted under its ETag, the statement imported as multipart into a money account
// of its own, and the general ledger's export followed until it is ready (NFR-PERF-01 to NFR-PERF-05).
// req: NFR-PERF-01
// req: NFR-PERF-02
// req: NFR-PERF-03
// req: NFR-PERF-04
// req: NFR-PERF-05
// req: NFR-SCALE-01
func TestTheMeasurerCallsTheAPIAsTheWebAppDoes(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]int{}
	var problems []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		key := r.Method + " " + r.URL.Path
		seen[key]++
		bad := func(why string) { problems = append(problems, key+": "+why) }
		if r.Header.Get("Authorization") != "Bearer owner-token" {
			bad("no bearer token")
		}
		if r.Method == http.MethodPost && r.Header.Get("Idempotency-Key") == "" {
			bad("no Idempotency-Key")
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/report-exports/x-1": // ready at its first poll
			_, _ = io.WriteString(w, `{"id":"x-1","status":"ready","rows":1234}`)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/reports/statements/"):
			if r.URL.Query().Get("compare") != "prior_period" || r.URL.Query().Get("from") == "" {
				bad("not twelve months with the prior period")
			}
			_, _ = io.WriteString(w, `{}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/money-accounts":
			_, _ = io.WriteString(w, `{"items":[{"id":"other","account_id":"`+uuid.NewString()+`","identifier":"X"}]}`)
		case r.Method == http.MethodGet:
			_, _ = io.WriteString(w, `{"items":[]}`)
		case r.URL.Path == "/api/v1/journals":
			var body struct {
				Lines []struct {
					Amount struct{ Amount, Currency string } `json:"amount"`
				} `json:"lines"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if len(body.Lines) != 50 || body.Lines[0].Amount.Currency != "SGD" {
				bad("not a 50-line journal in SGD")
			}
			w.Header().Set("ETag", `W/"1-draft"`) // weakened by a compressing proxy
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":"`+uuid.NewString()+`","version":1,"status":"draft"}`)
		case strings.HasSuffix(r.URL.Path, "/post"):
			if r.Header.Get("If-Match") != `"1-draft"` {
				bad("posted without the draft's version and status")
			}
			_, _ = io.WriteString(w, `{}`)
		case r.URL.Path == "/api/v1/money-accounts":
			var body struct {
				AccountID string `json:"account_id"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.AccountID == "" {
				bad("a money account with no chart account")
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":"ma-1"}`)
		case r.URL.Path == "/api/v1/bank-imports":
			_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
			form, err := multipart.NewReader(r.Body, params["boundary"]).ReadForm(32 << 20)
			if err != nil || form.Value["format"][0] != "ofx" || form.Value["money_account_id"][0] != "ma-1" || len(form.File["file"]) != 1 {
				bad("not the statement as multipart")
			}
			_, _ = io.WriteString(w, `{"statements":[]}`)
		case r.URL.Path == "/api/v1/report-exports":
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"id":"x-1","status":"pending"}`)
		default:
			bad("unexpected")
			http.Error(w, "unexpected", http.StatusBadRequest)
		}
	}))
	defer srv.Close()
	g := GrowReport{Sample: []uuid.UUID{uuid.New(), uuid.New()}, Expenses: []uuid.UUID{uuid.New(), uuid.New()}, Money: uuid.New(), Currency: "SGD"}
	clients := map[string]bool{}
	token := func(client string) (string, error) {
		mu.Lock()
		clients[client] = true
		mu.Unlock()
		return "owner-token", nil
	}
	m := Measurer{API: srv.URL + "/api", Token: token, HTTP: srv.Client(), Clock: clock.System{}, Readers: 8, ReadsEach: 3, Lines: 1000}
	results := m.Run(context.Background(), g)
	if len(problems) > 0 {
		t.Fatalf("the calls: %s", strings.Join(problems, "; "))
	}
	byID := map[string]Result{}
	for _, r := range results {
		byID[r.ID] = r
	}
	if r := byID["NFR-PERF-01"]; r.Samples != 24 || r.Error != "" || !clients["perf-reader-7"] || !clients["perf"] || len(clients) != 9 {
		t.Errorf("the reads, each reader a client of its own: %+v %v", r, clients)
	}
	if r := byID["NFR-PERF-02"]; r.Samples != 30 || r.Error != "" {
		t.Errorf("the reports: %+v", r)
	}
	if r := byID["NFR-PERF-03"]; r.Samples != 20 || r.Error != "" || seen["POST /api/v1/journals"] != 20 {
		t.Errorf("the postings: %+v", r)
	}
	if r := byID["NFR-PERF-04"]; r.Error != "" || !r.Passed {
		t.Errorf("the import: %+v", r)
	}
	if r := byID["NFR-PERF-05"]; r.Error != "" || r.Note != "1234 rows" || seen["GET /api/v1/report-exports/x-1"] == 0 {
		t.Errorf("the export: %+v (polled %d)", r, seen["GET /api/v1/report-exports/x-1"])
	}
	if r := byID["NFR-PERF-06"]; r.Gated || r.Samples != 0 {
		t.Errorf("the cold start is reported from staging: %+v", r)
	}
}
