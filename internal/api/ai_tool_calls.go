package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/aitool"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The AI's read tools through the API (PRD-001 AI-01, AI-P1, AI-68, AI-04; p1-ai-assistant design D4, D5, D6): the AI
// service names a registry tool and the input the model gave it, and the API runs it as the turn's person by calling
// the tool's public read operation in process with the turn's own delegated token. Every rule that operation has
// applies unchanged: the person's roles, the masking, the AI's read-only gate (aiReadable). What comes back is cut to
// the tool's limits, and every figure in it is a typed fact.

// AIToolCall is a read tool's call the AI service asks for: the tool and the input the model gave it.
type AIToolCall struct {
	Tool  AIToolName     `json:"tool" doc:"The read tool the model called, by its registry name." example:"records.search"`
	Input map[string]any `json:"input" doc:"The input the model gave it, checked against the tool's input schema."`
}

// AIToolResult is what a tool read, within its limits (AI-04): its figures as facts (AI-68), and the rest of what the
// operation answered, each figure in it replaced by {"fact": its ID}.
type AIToolResult struct {
	Tool     string            `json:"tool" example:"records.search"`
	Version  int               `json:"version" doc:"The tool's version in the registry, which the turn records." example:"1"`
	Facts    []aitool.Fact     `json:"facts" nullable:"false" doc:"Every figure the tool read (AI-68)."`
	Content  any               `json:"content,omitempty" doc:"What the operation answered, cut to the tool's limits, its figures replaced by their facts; absent when the facts are all."`
	Omitted  []aitool.Omission `json:"omitted" nullable:"false" doc:"What the limits left out: of each list, how many it shows of how many there are."`
	Revision int64             `json:"ledger_revision" doc:"The ledger revision it was read at (ENG-46)." example:"1204"`
}

type (
	aiToolCallInput  struct{ Body AIToolCall }
	aiToolCallOutput struct{ Body AIToolResult }
)

// The codes of a tool call's refusal.
const (
	CodeToolInput   = "AI-TOOL-01" // the input is not what the tool takes
	CodeToolElse    = "AI-TOOL-02" // the tool runs in the AI service, or is not built yet
	CodeToolRefused = "AI-TOOL-03" // the operation refused it: the person may not read it, or it is not there
	CodeToolTimeout = "AI-TOOL-04" // it took longer than the tool's limit
)

// recordPaths are where records.get reads each kind of record.
var recordPaths = map[string]string{
	"invoice": "/v1/invoices/", "credit_note": "/v1/invoices/", "bill": "/v1/bills/", "vendor_credit": "/v1/bills/",
	"journal": "/v1/journals/", "expense_claim": "/v1/expense-claims/", "quote": "/v1/quotes/", "sales_order": "/v1/quotes/",
	"purchase_order": "/v1/purchase-orders/", "contact": "/v1/contacts/", "account": "/v1/accounts/", "item": "/v1/items/",
}

// recordFields are the fields of each kind of record the AI reads (design D6, AI-04): what a question about the books
// needs, leaving out people's contact details, addresses and tax IDs, the people who prepared and posted it, its emails,
// its approvals and its free-text notes. The exclusion policy (AI-02, task 4.1) applies on top.
var recordFields = map[string][]string{
	"invoice": {"id", "kind", "number", "reference", "date", "due_date", "status", "currency", "fx_rate", "customer", "lines", "net", "gst",
		"gst_subtotals", "total", "deposits", "credits", "balance", "open", "receivable", "likely_paid", "posted_at", "checks"},
	"bill": {"id", "kind", "number", "supplier_ref", "date", "due_date", "status", "currency", "fx_rate", "supplier", "lines", "input_tax", "total",
		"withholding", "early_payment_discount", "payable", "payments", "planned_payment_on", "open", "posted_at", "checks"},
	"journal":       {"id", "number", "date", "memo", "status", "lines", "reverse_on", "posted_at", "checks"},
	"expense_claim": {"id", "number", "title", "date", "status", "stage", "claimant", "lines", "total", "posted_at", "checks"},
	"quote": {"id", "kind", "number", "reference", "date", "valid_until", "status", "currency", "fx_rate", "customer", "lines", "net", "gst",
		"total", "invoices", "checks"},
	"purchase_order": {"id", "number", "reference", "date", "expected_on", "status", "currency", "supplier", "lines", "net", "gst", "total",
		"total_base", "bills", "checks"},
	"contact": {"id", "legal_name", "customer", "vendor", "individual", "director", "shareholder", "investor", "active", "country", "currency",
		"payment_terms_days", "customer_group_id", "default_account_id", "default_gst_code"},
	"account": {"id", "code", "name", "type", "group", "report_group", "role", "function", "currency", "active", "control", "money_account",
		"cash_flow_line", "parent_id", "guidance"},
	"item": {"id", "code", "name", "description", "type", "unit", "gst_code", "prices", "revenue_account_id", "active"},
}

// documentKinds are the kinds read through one operation with another: a read of the one must be of it.
var documentKinds = []string{"invoice", "credit_note", "bill", "vendor_credit", "quote", "sales_order"}

type turnTokenKey struct{}

// turnToken is the delegated read token the AI service sent, as its Authorization header.
func turnToken(ctx context.Context) string {
	t, _ := ctx.Value(turnTokenKey{}).(string)
	return t
}

func registerToolCalls(a *API) {
	internalOp(a, huma.Operation{
		OperationID: "callAITool", Method: http.MethodPost, Path: internalBase + "/tool-calls", Tags: []string{"Assistant"},
		Summary: "Run a read tool for a turn",
		Description: "Runs one tool of the registry (contracts/ai/tools) as the turn's person (AI-01, AI-P1): through its public read " +
			"operation with the turn's delegated token, so the person's roles and the masking apply and nothing is written. The result " +
			"is cut to the tool's limits (rows, time and a token budget; AI-04), what was left out counted, and every figure in it is a " +
			"typed fact (AI-68). calc runs in the AI service itself.",
	}, []string{CodeToolInput, CodeToolElse, CodeToolRefused, CodeToolTimeout}, func(ctx context.Context, in *aiToolCallInput) (*aiToolCallOutput, error) {
		tool, ok := aitool.Default().Get(string(in.Body.Tool))
		if !ok {
			return nil, problem(CodeToolInput, string(in.Body.Tool)+" is not a tool of the registry.", "Name one of the registry's read tools")
		}
		if err := checkToolInput(tool, in.Body.Input); err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(ctx, time.Duration(tool.Limits.TimeoutMS)*time.Millisecond)
		defer cancel()
		out, err := a.runTool(ctx, tool, in.Body.Input)
		if ctx.Err() == context.DeadlineExceeded {
			return nil, problem(CodeToolTimeout, fmt.Sprintf("%s took longer than its %d ms.", tool.Name, tool.Limits.TimeoutMS),
				"Ask for less: fewer months, one dimension member, or a narrower search")
		}
		if err != nil {
			return nil, err
		}
		out.Tool, out.Version = tool.Name, tool.Version
		return &aiToolCallOutput{Body: out}, nil
	})
}

// checkToolInput refuses an input the tool's schema does not take, naming the field.
func checkToolInput(tool aitool.Tool, input map[string]any) error {
	var s huma.Schema
	if err := json.Unmarshal(tool.InputSchema, &s); err != nil {
		return err
	}
	s.PrecomputeMessages()
	res := &huma.ValidateResult{}
	if input == nil {
		input = map[string]any{}
	}
	huma.Validate(huma.NewMapRegistry("#/components/schemas/", huma.DefaultSchemaNamer), &s, huma.NewPathBuffer([]byte("input"), 0),
		huma.ModeWriteToServer, input, res)
	if len(res.Errors) == 0 {
		return nil
	}
	var words []string
	for _, e := range res.Errors {
		words = append(words, e.Error())
	}
	return problem(CodeToolInput, tool.Name+" does not take this input: "+strings.Join(words, "; ")+".",
		"Call it with the input its definition describes")
}

// runTool runs a tool through its operation.
func (a *API) runTool(ctx context.Context, tool aitool.Tool, in map[string]any) (AIToolResult, error) {
	str := func(k string) string { s, _ := in[k].(string); return s }
	var revision int64
	if err := a.deps.Store.Do(ctx, authnOrg(ctx), func(ctx context.Context, u storage.UnitOfWork) (err error) {
		revision, err = u.Ledger().Revision(ctx)
		return err
	}); err != nil {
		return AIToolResult{}, err
	}
	today := a.deps.Clock.Now().UTC().Format(time.DateOnly)
	subject := aitool.Subject{Revision: revision, AsAt: today}
	var body any
	switch tool.Name {
	case "metrics.query":
		var out MetricsResult
		if err := a.read(ctx, http.MethodPost, "/v1/metrics:query", in, &out); err != nil {
			return AIToolResult{}, err
		}
		facts := make([]aitool.Fact, 0, len(out.Facts))
		for _, f := range out.Facts {
			facts = append(facts, aitool.Fact{ID: f.ID, Measure: f.Measure, Name: f.Name, Value: f.Value, Words: f.Words, Undefined: f.Undefined,
				Unit: f.Unit, Currency: f.Currency, From: f.From, To: f.To, Dimension: f.Dimension, Member: f.Member, MemberName: f.MemberName,
				Sign: f.Sign, Revision: f.Revision, Source: f.Source, Lineage: f.Lineage})
		}
		res := AIToolResult{Facts: facts, Omitted: []aitool.Omission{}, Revision: out.Revision}
		if len(facts) > tool.Limits.Rows {
			res.Facts, res.Omitted = facts[:tool.Limits.Rows], []aitool.Omission{{Path: "facts", Shown: tool.Limits.Rows, Of: len(facts)}}
		}
		for aitool.Tokens(res.Facts) > tool.Limits.ResultTokens && len(res.Facts) > 1 {
			res.Facts = res.Facts[:len(res.Facts)/2]
			res.Omitted = []aitool.Omission{{Path: "facts", Shown: len(res.Facts), Of: len(facts)}}
		}
		return res, nil
	case "records.search":
		q := url.Values{"q": {str("q")}}
		if k := str("kind"); k != "" {
			q.Set("group", k)
		}
		subject.Measure, subject.Source, subject.Lineage = "search", "a search of the records", "/v1/search?"+q.Encode()
		if err := a.read(ctx, http.MethodGet, subject.Lineage, nil, &body); err != nil {
			return AIToolResult{}, err
		}
	case "records.get":
		kind, id := str("kind"), str("id")
		subject.Measure, subject.Kind, subject.Member, subject.Lineage = kind, kind, id, recordPaths[kind]+id
		subject.Source = "the " + strings.ReplaceAll(kind, "_", " ")
		if err := a.read(ctx, http.MethodGet, subject.Lineage, nil, &body); err != nil {
			return AIToolResult{}, err
		}
		record, _ := body.(map[string]any)
		if got, _ := record["kind"].(string); slices.Contains(documentKinds, kind) && got != "" && got != kind {
			return AIToolResult{}, problem(CodeToolRefused, "No "+strings.ReplaceAll(kind, "_", " ")+" has that ID: it is a "+
				strings.ReplaceAll(got, "_", " ")+".", "Read it as a "+strings.ReplaceAll(got, "_", " "))
		}
		kept := map[string]any{}
		for _, f := range recordFields[fieldsOf(kind)] {
			if v, ok := record[f]; ok {
				kept[f] = v
			}
		}
		body = kept
	case "checks.list":
		subject.Measure, subject.Source, subject.Lineage = "checks", "the checks of the books", "/v1/checks"
		if err := a.read(ctx, http.MethodGet, subject.Lineage, nil, &body); err != nil {
			return AIToolResult{}, err
		}
		if all, ok := body.(map[string]any); ok {
			checks, _ := all["checks"].([]any)
			kept := []any{}
			for _, c := range checks {
				m, _ := c.(map[string]any)
				if (str("area") == "" || strings.EqualFold(fmt.Sprint(m["area"]), str("area"))) && (str("status") == "" || m["status"] == str("status")) {
					delete(m, "items") // one check's items are checks.explain's
					kept = append(kept, m)
				}
			}
			all["checks"] = kept
		}
	case "checks.explain":
		subject.Measure, subject.Source, subject.Lineage = "check", "a check of the books", "/v1/checks/"+url.PathEscape(str("key"))
		if err := a.read(ctx, http.MethodGet, subject.Lineage, nil, &body); err != nil {
			return AIToolResult{}, err
		}
	case "calc":
		return AIToolResult{}, problem(CodeToolElse, "calc runs in the AI service, not the API.", "Run calc in the AI service (AI-P6)")
	default:
		return AIToolResult{}, problem(CodeToolElse, tool.Name+" is not built yet (p1-ai-assistant task 3.2).", "Use another read tool")
	}
	content, facts, omitted := aitool.Fit(body, tool.Limits, subject)
	if facts == nil {
		facts = []aitool.Fact{}
	}
	if omitted == nil {
		omitted = []aitool.Omission{}
	}
	return AIToolResult{Facts: facts, Content: content, Omitted: omitted, Revision: revision}, nil
}

// fieldsOf is the kind whose fields a kind of record keeps: a credit note's are an invoice's, a vendor credit's a bill's,
// a sales order's a quote's.
func fieldsOf(kind string) string {
	switch kind {
	case "credit_note":
		return "invoice"
	case "vendor_credit":
		return "bill"
	case "sales_order":
		return "quote"
	}
	return kind
}

// read calls the public operation at path in process as the turn's person, with the turn's delegated token, and decodes
// its answer into out, keeping numbers' exact text when out is an any. A refusal comes back as the operation's own
// problem under CodeToolRefused, so the model reads why.
func (a *API) read(ctx context.Context, method, path string, body any, out any) error {
	var r *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	} else {
		r = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, path, r)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", turnToken(ctx))
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.RemoteAddr = "127.0.0.1:0" // in process
	w := &capture{header: http.Header{}}
	a.Adapter().ServeHTTP(w, req)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if w.status >= 300 {
		var p Problem
		if json.Unmarshal(w.body.Bytes(), &p) != nil || p.Code == "" {
			return problem(CodeToolRefused, fmt.Sprintf("The read was refused (%d).", w.status), "Read something else")
		}
		detail := p.Detail
		if detail == "" {
			detail = p.Title + "."
		}
		return problem(CodeToolRefused, detail+" ("+p.Code+")", p.Fix)
	}
	if v, ok := out.(*any); ok {
		*v, err = aitool.Decode(w.body.Bytes())
		return err
	}
	return json.Unmarshal(w.body.Bytes(), out)
}

// capture is an in-process response.
type capture struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (c *capture) Header() http.Header { return c.header }
func (c *capture) WriteHeader(status int) {
	if c.status == 0 {
		c.status = status
	}
}
func (c *capture) Write(b []byte) (int, error) {
	if c.status == 0 {
		c.status = http.StatusOK
	}
	return c.body.Write(b)
}
