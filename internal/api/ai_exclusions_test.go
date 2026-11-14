//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
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
