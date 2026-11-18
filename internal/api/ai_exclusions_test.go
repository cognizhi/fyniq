//go:build local

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/assistant"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The AI exclusion policy runs on every read an AI principal makes (AI-02, AI-20; p1-ai-assistant design D8): an
// individual's NRIC and home address, and a bank account number or NRIC written into a memo, never reach the
// assistant, through a public operation or a read tool, while the person reading the same records sees them; the
// Owner's own exclusions take a field out too; pay goes to the AI only with AI payroll access on and a payroll right of
// the person's own.
// req: AI-02
// req: AI-20
// asvs: V14.2.1, V8.2.1
func TestWhatNeverReachesTheAI(t *testing.T) {
	e := newEnv(t)
	f := foundersIn(e)
	acc := e.as(authz.Accountant)
	contact := e.call(http.MethodPost, "/v1/contacts", acc, map[string]any{"legal_name": "Tan Ah Kow", "individual": true, "customer": true,
		"uen_or_tax_id": "S1234567D", "address": "12 Jalan Kayu, Singapore 799000"})
	if contact.Status != http.StatusCreated {
		t.Fatalf("createContact: %d %s", contact.Status, contact.Raw)
	}
	d := e.draft("100.00")
	d["memo"] = "Refund to 012-345678-9 for S1234567D"
	journal := e.call(http.MethodPost, "/v1/journals", acc, d)
	if journal.Status != http.StatusCreated {
		t.Fatalf("createJournal: %d %s", journal.Status, journal.Raw)
	}
	var token string
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		token, err = e.api.issueAIGrant(ctx, u, e.books.Org, uuid.MustParse(f.ahkow), uuid.New())
		return err
	})
	contactPath, journalPath := "/v1/contacts/"+contact.str("id"), "/v1/journals/"+journal.str("id")

	// The person sees it all; the assistant, nothing that never reaches a model.
	if r := e.call(http.MethodGet, contactPath, acc, nil); !strings.Contains(r.Raw, "S1234567D") || !strings.Contains(r.Raw, "Jalan Kayu") {
		t.Fatalf("the person's read: %s", r.Raw)
	}
	for _, path := range []string{contactPath, journalPath} {
		r := e.call(http.MethodGet, path, token, nil)
		if r.Status != http.StatusOK || strings.Contains(r.Raw, "S1234567D") || strings.Contains(r.Raw, "Jalan Kayu") || strings.Contains(r.Raw, "345678") {
			t.Errorf("the AI's read of %s: %d %s", path, r.Status, r.Raw)
		}
	}
	if r := e.call(http.MethodGet, contactPath, token, nil); r.str("uen_or_tax_id") != "S****567D" || r.str("legal_name") != "Tan Ah Kow" {
		t.Errorf("the masked NRIC: %s", r.Raw)
	}
	if r := e.call(http.MethodGet, journalPath, token, nil); r.str("memo") != "Refund to ****6789 for S****567D" {
		t.Errorf("the memo: %s", r.Raw)
	}
	service, _ := e.services.Token(ServiceAI)
	g := e.call(http.MethodPost, "/internal/v1/tool-calls", token, map[string]any{"tool": "records.get", "input": map[string]any{"kind": "journal",
		"id": journal.str("id")}}, ServiceTokenHeader, service)
	if g.Status != http.StatusOK || strings.Contains(g.Raw, "345678") || strings.Contains(g.Raw, "S1234567D") {
		t.Errorf("records.get: %d %s", g.Status, g.Raw)
	}

	// The Owner's own exclusion: the memo goes too.
	y, m, day := e.clock.Now().Date()
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Settings().Record(ctx, settingAIExclusions, "", "memo", time.Date(y, m, day, 0, 0, 0, 0, time.UTC), 1)
		return err
	})
	if r := e.call(http.MethodGet, journalPath, token, nil); r.Status != http.StatusOK || r.Body["memo"] != nil {
		t.Errorf("the Owner's exclusion: %s", r.Raw)
	}
	if r := e.call(http.MethodGet, journalPath, acc, nil); r.str("memo") == "" {
		t.Errorf("the person's memo: %s", r.Raw)
	}

	// Pay: only with AI payroll access on, and only for a person who may see payroll.
	policy := func(roles ...authz.Role) bool {
		p, err := e.api.aiExclusionPolicy(context.Background(), authn.Principal{Organisation: e.books.Org, Roles: roles, Class: authn.ClassAgent})
		if err != nil {
			t.Fatal(err)
		}
		return p.Payroll
	}
	if policy(authz.Owner) {
		t.Error("pay with AI payroll access off")
	}
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Settings().Record(ctx, settingAIPayroll, "false", "true", time.Date(y, m, day, 0, 0, 0, 0, time.UTC), 1)
		return err
	})
	if !policy(authz.Owner) || policy(authz.Bookkeeper) {
		t.Error("pay with AI payroll access on: an Owner reads it, a Bookkeeper does not")
	}
}

// CTL-AI-06 on every change (design D8): an NRIC, a bank account number and a home address planted in the books, and an
// NRIC and an account number typed into an earlier question, reach no request the assistant's model receives, end to
// end: the person asks through the API, the real AI service runs the read tools as them, and every request the model
// got is scanned. The reads did happen: the model read the contact's name and its masked NRIC.
// req: CTL-AI-06
// req: AI-02
// req: AI-20
// asvs: V14.2.1
func TestPlantedExclusionsReachNoModelRequest(t *testing.T) {
	e := newEnv(t)
	f := foundersIn(e)
	aiSwitchedOn(e)
	sg := routable(e, "Exclusions", "SG", []string{"gpt-6"})
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.AIRoutes().Save(ctx, storage.AIRoute{Agent: "chat", Version: 1, Classes: []string{"business-figures", "personal-data"}, MadeBy: "user:test",
			MadeAt: e.clock.Now(), Steps: []storage.AIRouteStep{{ConnectionID: sg, Model: "gpt-6"}}})
		return err
	})
	acc := e.as(authz.Accountant)
	contact := e.call(http.MethodPost, "/v1/contacts", acc, map[string]any{"legal_name": "Tan Ah Kow", "individual": true, "customer": true,
		"uen_or_tax_id": "S1234567D", "address": "12 Jalan Kayu, Singapore 799000"})
	d := e.draft("100.00")
	d["memo"] = "Refund to 012-345678-9 for S1234567D"
	journal := e.call(http.MethodPost, "/v1/journals", acc, d)
	if contact.Status != http.StatusCreated || journal.Status != http.StatusCreated {
		t.Fatalf("the planted records: %d %s %d %s", contact.Status, contact.Raw, journal.Status, journal.Raw)
	}
	words := func(s string) ports.ModelResponse {
		b, _ := json.Marshal(assistant.Structured{Segments: []assistant.Segment{{Text: s}}})
		return ports.ModelResponse{Parts: []ports.ModelPart{{Kind: ports.PartText, Text: string(b)}}, StopReason: ports.StopEnd}
	}
	model := &scripted{answers: []ports.ModelResponse{
		{Parts: []ports.ModelPart{
			{Kind: ports.PartToolCall, CallID: "c1", Tool: "records_get", Input: json.RawMessage(`{"kind":"contact","id":"` + contact.str("id") + `"}`)},
			{Kind: ports.PartToolCall, CallID: "c2", Tool: "records_get", Input: json.RawMessage(`{"kind":"journal","id":"` + journal.str("id") + `"}`)},
			{Kind: ports.PartToolCall, CallID: "c3", Tool: "records_search", Input: json.RawMessage(`{"q":"Tan","kind":"contacts"}`)}},
			StopReason: ports.StopToolCall},
		words("Tan Ah Kow is a customer, and a journal records a refund to them."),
		words("The journal's memo names the refund."),
	}}
	e.api.deps.AIProviders = func(context.Context, storage.UnitOfWork, storage.AIConnection) (ports.ModelProvider, error) {
		return model, nil
	}
	withAIService(e)
	ahkow := f.as(f.ahkow)
	c := e.call(http.MethodPost, "/v1/assistant/conversations", ahkow, map[string]any{}).str("id")
	for _, q := range []string{"Is S1234567D, who pays from 012-345678-9, our customer?", "And what did we refund them?"} {
		kinds, data := events(e.call(http.MethodPost, "/v1/assistant/conversations/"+c+"/turns", ahkow, map[string]any{"question": q}).Raw)
		if kinds[len(kinds)-1] != "answer" || data[len(data)-1]["status"] != "answered" {
			t.Fatalf("%q: %v %v", q, kinds, data)
		}
	}
	if len(model.requests) != 3 {
		t.Fatalf("%d model requests", len(model.requests))
	}
	sent, _ := json.Marshal(model.requests)
	for _, planted := range []string{"S1234567D", "1234567D", "012-345678-9", "345678", "Jalan Kayu", "799000"} {
		if strings.Contains(string(sent), planted) {
			t.Errorf("a model request carries %q", planted)
		}
	}
	if !strings.Contains(string(sent), "Tan Ah Kow") || !strings.Contains(string(sent), "S****567D") || !strings.Contains(string(sent), "****6789") {
		t.Errorf("the reads did not reach the model as they should, masked: %s", sent)
	}
}
