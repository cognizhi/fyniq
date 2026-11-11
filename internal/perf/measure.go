package perf

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"mime/multipart"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports"
)

// The budgets the harness measures (PRD-001 §15).
var (
	Reads = Budget{ID: "NFR-PERF-01", What: "interactive reads (a journal, a list page, a search) by 100 people, each pausing 1 to 3 s between reads (NFR-SCALE-01)", Limit: 300 * time.Millisecond, Each: true, Gated: true}
	// Throughput is the same reads, 100 at once and back to back: where the stack saturates, reported and never gated
	// (the owner's decision of 11 Nov 2026, PRD-001 rev 0.252).
	Throughput = Budget{ID: "NFR-SCALE-01", What: "the same reads, 100 at once, back to back: throughput", Limit: 300 * time.Millisecond, Each: true}
	Posting    = Budget{ID: "NFR-PERF-03", What: "save and post a 50-line journal", Limit: 500 * time.Millisecond, Each: true, Gated: true}
	Imports    = Budget{ID: "NFR-PERF-04", What: "import a 10,000-line bank statement", Limit: time.Minute, Gated: true}
	Exports    = Budget{ID: "NFR-PERF-05", What: "export the general ledger of the grown months to CSV", Limit: 5 * time.Minute, Gated: true}
	Cold       = Budget{ID: "NFR-PERF-06", What: "the first request after the API starts from idle", Limit: 2300 * time.Millisecond}
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
	// Only names the budgets to measure, by requirement (NFR-PERF-02) or step (reads, reports, posting, import,
	// export); none measures every one.
	Only []string
	// Pause is the least and the most a reader waits between two reads, as a person reading a page does: 1 to 3 s
	// when zero (NFR-SCALE-01, rev 0.252).
	Pause [2]time.Duration
}

// wanted is whether the run measures a budget.
func (m Measurer) wanted(b Budget, step string) bool {
	return len(m.Only) == 0 || slices.Contains(m.Only, b.ID) || slices.Contains(m.Only, step)
}

// Run measures every budget against the grown ledger.
func (m Measurer) Run(ctx context.Context, g GrowReport) []Result {
	if m.Readers <= 0 {
		m.Readers = 100
	}
	if m.ReadsEach <= 0 {
		m.ReadsEach = 10
	}
	var out []Result
	if m.wanted(Reads, "reads") {
		m.say("perf: reads by %d people, each pausing between reads", m.Readers)
		out = append(out, m.reads(ctx, g, Reads))
	}
	if m.wanted(Throughput, "reads") {
		m.say("perf: the same reads, %d at once, back to back", m.Readers)
		out = append(out, m.reads(ctx, g, Throughput))
	}
	if m.wanted(ReportsBudget(m.Lines), "reports") {
		m.say("perf: reports")
		out = append(out, m.reports(ctx))
	}
	if m.wanted(Posting, "posting") {
		m.say("perf: posting")
		out = append(out, m.posting(ctx, g))
	}
	if m.wanted(Imports, "import") {
		m.say("perf: a %d-line bank import", StatementLines)
		out = append(out, m.importing(ctx, g))
	}
	if m.wanted(Exports, "export") {
		m.say("perf: the general ledger's export")
		out = append(out, m.exporting(ctx))
	}
	if !m.wanted(Cold, "cold") {
		return out
	}
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
// reads times the readers' reads: for Reads each pauses between reads as a person does, for Throughput none does.
func (m Measurer) reads(ctx context.Context, g GrowReport, b Budget) Result {
	least, most := m.Pause[0], m.Pause[1]
	if most <= 0 {
		least, most = time.Second, 3*time.Second
	}
	pause := func(r *rand.Rand) {
		if b.Gated {
			time.Sleep(least + time.Duration(r.Int64N(int64(most-least)+1)))
		}
	}
	// Each kind of read takes its turn, so a list or a search is measured as often as a journal is.
	kinds := []string{"list", "search"}
	if len(g.Sample) > 0 {
		kinds = append(kinds, "journal")
	}
	path := func(kind string, i int) string {
		switch kind {
		case "list":
			return "/v1/journals?page_size=50"
		case "search":
			return "/v1/search?q=" + url.QueryEscape("perf payment")
		}
		return "/v1/journals/" + g.Sample[i%len(g.Sample)].String()
	}
	var mu sync.Mutex
	var took []time.Duration
	per := map[string][]time.Duration{}
	var first error
	var wg sync.WaitGroup
	for w := range m.Readers {
		wg.Go(func() {
			r := rand.New(rand.NewPCG(uint64(w), 1)) // #nosec G404 G115 -- a reader's pauses, not a secret
			for i := range m.ReadsEach {
				pause(r)
				n := w*m.ReadsEach + i
				kind := kinds[n%len(kinds)]
				_, d, err := m.call(ctx, fmt.Sprintf("perf-reader-%d", w), http.MethodGet, path(kind, n/len(kinds)), nil, "", nil)
				mu.Lock()
				if err != nil && first == nil {
					first = err
				}
				if err == nil {
					took = append(took, d)
					per[kind] = append(per[kind], d)
				}
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	res := sampled(b, took, first)
	res.Note = each(per, kinds)
	return res
}

// each is the 95th percentile of each kind's samples, in words: where the time goes.
func each(per map[string][]time.Duration, kinds []string) string {
	var parts []string
	for _, k := range kinds {
		if len(per[k]) > 0 {
			parts = append(parts, fmt.Sprintf("%s p95 %s", k, ms(Percentile(per[k], 95))))
		}
	}
	return strings.Join(parts, ", ")
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
	kinds := []string{"pl", "bs", "tb"}
	var took []time.Duration
	per := map[string][]time.Duration{}
	var first error
	var cold []string
	for round := range 10 {
		for _, kind := range kinds {
			_, d, err := m.call(ctx, "perf", http.MethodGet, fmt.Sprintf("/v1/reports/statements/%s?from=%s&to=%s&compare=prior_period", kind, from, to), nil, "", nil)
			if err != nil {
				first = cmpErr(first, err)
				continue
			}
			took = append(took, d)
			per[kind] = append(per[kind], d)
			if round == 0 {
				cold = append(cold, fmt.Sprintf("%s %s", kind, ms(d)))
			}
		}
	}
	r := sampled(ReportsBudget(m.Lines), took, first)
	r.Note = each(per, kinds) + "; first " + strings.Join(cold, ", ")
	return r
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
		// If-Match is the draft's ETag sent back, as an integrator would: Caddy's compression names its encoding in it
		// ("1-draft-zstd"), which the API takes (FR-INT-09, rev 0.251).
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
