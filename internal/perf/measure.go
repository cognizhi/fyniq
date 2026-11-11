package perf

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports"
)

// The budgets the harness measures (PRD-001 §15).
var (
	Reads   = Budget{ID: "NFR-PERF-01", What: "interactive reads (a journal, a list page, a search), 100 readers at once (NFR-SCALE-01)", Limit: 300 * time.Millisecond, Each: true, Gated: true}
	Posting = Budget{ID: "NFR-PERF-03", What: "save and post a 50-line journal", Limit: 500 * time.Millisecond, Each: true, Gated: true}
	Imports = Budget{ID: "NFR-PERF-04", What: "import a 10,000-line bank statement", Limit: time.Minute, Gated: true}
	Exports = Budget{ID: "NFR-PERF-05", What: "export the general ledger of the grown months to CSV", Limit: 5 * time.Minute, Gated: true}
	Cold    = Budget{ID: "NFR-PERF-06", What: "the first request after the API starts from idle", Limit: 2300 * time.Millisecond}
)

// ReportsBudget is NFR-PERF-02's: a standard report over twelve months with its comparative, within 2 s at up to 1 M
// ledger lines and 5 s at 10 M.
func ReportsBudget(lines int) Budget {
	limit := 2 * time.Second
	if lines > 1_000_000 {
		limit = 5 * time.Second
	}
	return Budget{ID: "NFR-PERF-02", What: "the P&L, balance sheet and trial balance over twelve months with the prior period", Limit: limit, Each: true, Gated: true}
}

// StatementLines is the size of NFR-PERF-04's import.
const StatementLines = 10_000

// Measurer times each budget through the running stack's API as an Owner and Accountant.
type Measurer struct {
	API string // the API's base, such as http://localhost:8080/api
	// Token is a bearer token for the organisation as client: an Owner and Accountant, who may read, post and import. Each
	// reader is a client of its own, as each of 100 people is, so the API's per-client limits count each apart.
	Token func(client string) (string, error)
	HTTP  *http.Client
	Clock ports.Clock
	// Readers is how many read at once (NFR-SCALE-01's 100 concurrent users), each ReadsEach times.
	Readers, ReadsEach int
	Lines              int // the ledger's size, which sets the reports' limit
	Log                io.Writer
}

// Run measures every budget against the grown ledger.
func (m Measurer) Run(ctx context.Context, g GrowReport) []Result {
	if m.Readers <= 0 {
		m.Readers = 100
	}
	if m.ReadsEach <= 0 {
		m.ReadsEach = 10
	}
	m.say("perf: reads, %d readers at once", m.Readers)
	out := []Result{m.reads(ctx, g)}
	m.say("perf: reports")
	out = append(out, m.reports(ctx))
	m.say("perf: posting")
	out = append(out, m.posting(ctx, g))
	m.say("perf: a %d-line bank import", StatementLines)
	out = append(out, m.importing(ctx, g))
	m.say("perf: the general ledger's export")
	out = append(out, m.exporting(ctx))
	return append(out, Result{Budget: Cold, Note: "measured on the real images in staging, not on the local stack [live-verify]"})
}

func (m Measurer) say(format string, args ...any) {
	if m.Log != nil {
		fmt.Fprintf(m.Log, format+"\n", args...)
	}
}

// answer is an API answer.
type answer struct {
	status int
	header http.Header
	body   []byte
}

func (m Measurer) call(ctx context.Context, client, method, path string, body io.Reader, contentType string, header map[string]string) (answer, time.Duration, error) {
	token, err := m.Token(client)
	if err != nil {
		return answer{}, 0, err
	}
	r, err := http.NewRequestWithContext(ctx, method, m.API+path, body)
	if err != nil {
		return answer{}, 0, err
	}
	r.Header.Set("Authorization", "Bearer "+token)
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	for k, v := range header {
		r.Header.Set(k, v)
	}
	start := m.Clock.Now()
	res, err := m.HTTP.Do(r)
	if err != nil {
		return answer{}, 0, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = res.Body.Close() }()
	b, err := io.ReadAll(res.Body)
	took := m.Clock.Now().Sub(start)
	if err != nil {
		return answer{}, took, fmt.Errorf("%s %s: %w", method, path, err)
	}
	a := answer{status: res.StatusCode, header: res.Header, body: b}
	if res.StatusCode/100 != 2 {
		return a, took, fmt.Errorf("%s %s: %d %s", method, path, res.StatusCode, bytes.TrimSpace(b[:min(len(b), 300)]))
	}
	return a, took, nil
}

func (m Measurer) postJSON(ctx context.Context, path string, v any, header map[string]string) (answer, time.Duration, error) {
	const client = "perf"
	b, err := json.Marshal(v)
	if err != nil {
		return answer{}, 0, err
	}
	if header == nil {
		header = map[string]string{}
	}
	header["Idempotency-Key"] = uuid.NewString()
	return m.call(ctx, client, http.MethodPost, path, bytes.NewReader(b), "application/json", header)
}

// reads times a journal, a page of journals and a search, by many readers at once.
func (m Measurer) reads(ctx context.Context, g GrowReport) Result {
	paths := []string{"/v1/journals?page_size=50", "/v1/search?q=" + url.QueryEscape("perf payment")}
	for _, id := range g.Sample {
		paths = append(paths, "/v1/journals/"+id.String())
	}
	var mu sync.Mutex
	var took []time.Duration
	var first error
	var wg sync.WaitGroup
	for w := range m.Readers {
		wg.Go(func() {
			for i := range m.ReadsEach {
				_, d, err := m.call(ctx, fmt.Sprintf("perf-reader-%d", w), http.MethodGet, paths[(w*m.ReadsEach+i)%len(paths)], nil, "", nil)
				mu.Lock()
				if err != nil && first == nil {
					first = err
				}
				if err == nil {
					took = append(took, d)
				}
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	return sampled(Reads, took, first)
}

// lastMonths are the twelve months that end with the last whole month, as YYYY-MM.
func (m Measurer) lastMonths() (from, to string) {
	now := m.Clock.Now().UTC()
	last := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -1, 0)
	return last.AddDate(0, -11, 0).Format("2006-01"), last.Format("2006-01")
}

// reports times the standard reports over twelve months with the prior twelve beside them.
func (m Measurer) reports(ctx context.Context) Result {
	from, to := m.lastMonths()
	var took []time.Duration
	var first error
	for range 10 {
		for _, kind := range []string{"pl", "bs", "tb"} {
			_, d, err := m.call(ctx, "perf", http.MethodGet, fmt.Sprintf("/v1/reports/statements/%s?from=%s&to=%s&compare=prior_period", kind, from, to), nil, "", nil)
			if err != nil {
				first = cmpErr(first, err)
				continue
			}
			took = append(took, d)
		}
	}
	return sampled(ReportsBudget(m.Lines), took, first)
}

func cmpErr(first, err error) error {
	if first != nil {
		return first
	}
	return err
}

type moneyAmount struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

// posting times saving and posting 50-line journals, as the web app does: the draft, then its posting under its ETag.
func (m Measurer) posting(ctx context.Context, g GrowReport) Result {
	if len(g.Expenses) == 0 {
		return sampled(Posting, nil, errors.New("no accounts to post to: the ledger was not grown"))
	}
	_, to := m.lastMonths()
	var took []time.Duration
	var first error
	for i := range 20 {
		type line struct {
			AccountID uuid.UUID   `json:"account_id"`
			Amount    moneyAmount `json:"amount"`
		}
		var lines []line
		var total int64
		for k := range 49 {
			cents := int64(100 + (i*49+k)%9_900)
			total += cents
			lines = append(lines, line{AccountID: g.Expenses[k%len(g.Expenses)], Amount: moneyAmount{decimal(cents), g.Currency}})
		}
		lines = append(lines, line{AccountID: g.Money, Amount: moneyAmount{decimal(-total), g.Currency}})
		draft, d1, err := m.postJSON(ctx, "/v1/journals", map[string]any{"date": to + "-15", "memo": fmt.Sprintf("perf posting %d", i), "lines": lines}, nil)
		if err != nil {
			first = cmpErr(first, err)
			continue
		}
		var created struct {
			ID      string `json:"id"`
			Version int    `json:"version"`
			Status  string `json:"status"`
		}
		if err := json.Unmarshal(draft.body, &created); err != nil || created.ID == "" {
			first = cmpErr(first, fmt.Errorf("the draft's answer names no journal: %s", draft.body))
			continue
		}
		// If-Match is the draft's ETag sent back, as an integrator would: Caddy's compression may have made it weak, which
		// the API takes (FR-INT-09, rev 0.250).
		_, d2, err := m.postJSON(ctx, "/v1/journals/"+created.ID+"/post", map[string]any{},
			map[string]string{"If-Match": draft.header.Get("ETag")})
		if err != nil {
			first = cmpErr(first, err)
			continue
		}
		took = append(took, d1+d2)
	}
	return sampled(Posting, took, first)
}

// importing times importing a 10,000-line OFX statement into a money account of its own.
func (m Measurer) importing(ctx context.Context, g GrowReport) Result {
	start := m.Clock.Now()
	// The money account of the grown ledger's money chart account (one per chart account), made the first time. Its
	// identifier comes from the chart account, so each run names the same one: a read shows identifiers masked.
	identifier := "PERF-" + strings.ToUpper(strings.ReplaceAll(g.Money.String(), "-", "")[:12])
	list, _, err := m.call(ctx, "perf", http.MethodGet, "/v1/money-accounts", nil, "", nil)
	if err != nil {
		return once(Imports, 0, err)
	}
	var accounts struct {
		Items []struct {
			ID        string `json:"id"`
			AccountID string `json:"account_id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(list.body, &accounts); err != nil {
		return once(Imports, 0, err)
	}
	var created struct{ ID string }
	for _, a := range accounts.Items {
		if a.AccountID == g.Money.String() {
			created.ID = a.ID
		}
	}
	if created.ID == "" {
		acct, _, err := m.postJSON(ctx, "/v1/money-accounts", map[string]any{"kind": "bank", "name": "Perf operating account", "account_id": g.Money,
			"identifier": identifier}, nil)
		if err != nil {
			return once(Imports, 0, fmt.Errorf("the statement's money account: %w", err))
		}
		if err := json.Unmarshal(acct.body, &created); err != nil || created.ID == "" {
			return once(Imports, 0, fmt.Errorf("the money account's answer names no account: %s", acct.body))
		}
	}
	file := Statement(identifier, g.Currency, StatementLines, start.UTC().AddDate(0, 0, -1), uint64(start.UnixNano())) // #nosec G115 -- a seed
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", identifier+".ofx")
	if err == nil {
		_, err = part.Write(file)
	}
	for k, v := range map[string]string{"format": "ofx", "money_account_id": created.ID} {
		if err == nil {
			err = w.WriteField(k, v)
		}
	}
	if err == nil {
		err = w.Close()
	}
	if err != nil {
		return once(Imports, 0, err)
	}
	_, took, err := m.call(ctx, "perf", http.MethodPost, "/v1/bank-imports", &body, w.FormDataContentType(), map[string]string{"Idempotency-Key": uuid.NewString()})
	r := once(Imports, took, err)
	r.Note = fmt.Sprintf("%d KB", len(file)/1024)
	return r
}

// exporting times the general ledger's export over the grown months, from the request until the file is ready.
func (m Measurer) exporting(ctx context.Context) Result {
	now := m.Clock.Now().UTC()
	this := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	from, to := this.AddDate(0, -Months, 0), this.AddDate(0, 0, -1)
	start := m.Clock.Now()
	a, _, err := m.postJSON(ctx, "/v1/report-exports", map[string]any{"report": "general_ledger", "format": "csv",
		"from": from.Format(time.DateOnly), "to": to.Format(time.DateOnly)}, nil)
	if err != nil {
		return once(Exports, 0, err)
	}
	var x struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Rows   int64  `json:"rows"`
		Why    string `json:"why"`
	}
	if err := json.Unmarshal(a.body, &x); err != nil || x.ID == "" {
		return once(Exports, 0, fmt.Errorf("the export's answer names no export: %s", a.body))
	}
	deadline := start.Add(Exports.Limit + time.Minute)
	for x.Status == "pending" && m.Clock.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return once(Exports, 0, ctx.Err())
		case <-time.After(2 * time.Second):
		}
		b, _, err := m.call(ctx, "perf", http.MethodGet, "/v1/report-exports/"+x.ID, nil, "", nil)
		if err != nil {
			return once(Exports, 0, err)
		}
		if err := json.Unmarshal(b.body, &x); err != nil {
			return once(Exports, 0, err)
		}
	}
	took := m.Clock.Now().Sub(start)
	if x.Status != "ready" {
		return once(Exports, took, fmt.Errorf("the export ended %s: %s", x.Status, x.Why))
	}
	r := once(Exports, took, nil)
	r.Note = fmt.Sprintf("%d rows", x.Rows)
	return r
}
