//go:build local

package api

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
)

// matrixComments are the authorisation matrix's rows for comments and mentions, on a contact.
var matrixComments = map[string]func(e *env, tok string) (response, func() bool){
	"listRecordComments": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/records/contact/"+e.supplier()+"/comments", tok, nil), nil
	},
	"addRecordComment": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/records/contact/"+e.supplier()+"/comments", tok, map[string]any{"body": "Please confirm the payment terms."}), nil
	},
	"listRecordMentionable": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/records/contact/"+e.supplier()+"/mentionable", tok, nil), nil
	},
}

// A team of four for the comment tests: two Owners, an Approver who is an Employee, and an Employee.
type commenters struct {
	c                        claimants
	ahkow, weiling, mei, nia string // their user IDs
}

func newCommenters(t *testing.T) commenters {
	e := newEnv(t)
	owner := e.as(authz.Owner)
	invite := func(name string, roles ...string) string {
		return e.call(http.MethodPost, "/v1/members/invitations", owner, map[string]any{"display_name": name, "roles": roles}).str("code")
	}
	join := func(name, code string) string {
		j := e.call(http.MethodPost, "/v1/members", e.principal(authn.Principal{Subject: name, Client: "web"}), map[string]any{"invitation_code": code, "display_name": name})
		if j.Status != 201 {
			t.Fatalf("%s joining: %d %s", name, j.Status, j.Raw)
		}
		return j.str("user_id")
	}
	codes := [4]string{invite("Ah Kow", "owner", "controller"), invite("Wei Ling", "owner", "controller"), invite("Mei", "approver", "employee"), invite("Nia", "employee")}
	m := commenters{c: claimants{e: e}}
	m.ahkow, m.weiling, m.mei, m.nia = join("Ah Kow", codes[0]), join("Wei Ling", codes[1]), join("Mei", codes[2]), join("Nia", codes[3])
	m.c.ahkow, m.c.weiling, m.c.mei = m.ahkow, m.weiling, m.mei
	return m
}

// FR-UX-13: a member comments on a record they may see, and mentions members who may see it too. A bill is seen by those whose roles view
// the Bills row: an Employee does not, so the record is not found for them, they are never offered as a name to mention, and a mention of
// them is refused in the same words as a mention of someone who is no member at all, so nothing of who may see it is learnt. A mention
// tells the member mentioned, and only them; a member narrowed to departments sees a bill of their own department only, and so is
// offered, mentioned and told only for those.
// req: FR-UX-13
// req: SEC-34
// asvs: V8.2.1, V8.3.1, V8.2.2
func TestMembersCommentOnRecordsAndMentionOnlyWhoMaySeeThem(t *testing.T) {
	m := newCommenters(t)
	e := m.c.e
	accountant := e.as(authz.Accountant)
	ahkow := m.c.as(m.ahkow, authz.Owner, authz.Controller)
	weiling := m.c.as(m.weiling, authz.Owner, authz.Controller)
	mei := m.c.as(m.mei, authz.Approver, authz.Employee)
	nia := m.c.as(m.nia, authz.Employee)
	dept := e.call(http.MethodPost, "/v1/dimensions", accountant, map[string]any{"kind": "department", "name": "Department"})
	eng := e.call(http.MethodPost, "/v1/dimension-values", accountant, map[string]any{"dimension_id": dept.str("id"), "code": "ENG", "name": "Engineering"}).str("id")
	ops := e.call(http.MethodPost, "/v1/dimension-values", accountant, map[string]any{"dimension_id": dept.str("id"), "code": "OPS", "name": "Operations"}).str("id")
	bill := func(net, value string) string {
		d := e.billDraft(net, "0.00")
		d["lines"].([]map[string]any)[0]["dimension_value_ids"] = []string{value}
		r := e.call(http.MethodPost, "/v1/bills", accountant, d)
		if r.Status != http.StatusCreated {
			t.Fatalf("a bill: %d %s", r.Status, r.Raw)
		}
		return r.str("id")
	}
	engBill, opsBill := bill("100.00", eng), bill("200.00", ops)
	names := func(r response) []string {
		var out []string
		for _, it := range r.Body["items"].([]any) {
			out = append(out, it.(map[string]any)["name"].(string))
		}
		return out
	}
	at := func(id, what string) string { return "/v1/records/bill/" + id + "/" + what }

	// Who may be mentioned: Wei Ling and Mei, who view bills; not Nia, an Employee; not the caller.
	if r := e.call(http.MethodGet, at(engBill, "mentionable"), ahkow, nil); r.Status != 200 || fmt.Sprint(names(r)) != "[Mei Wei Ling]" {
		t.Fatalf("the members who may be mentioned: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodGet, at(engBill, "mentionable?q=wei"), ahkow, nil); fmt.Sprint(names(r)) != "[Wei Ling]" {
		t.Errorf("a search by the start of a name: %s", r.Raw)
	}
	// An Employee finds nothing of it.
	e.problemIs(e.call(http.MethodGet, at(engBill, "comments"), nia, nil), "listRecordComments", http.StatusNotFound, CodeNotFound)
	e.problemIs(e.call(http.MethodGet, at(engBill, "mentionable"), nia, nil), "listRecordMentionable", http.StatusNotFound, CodeNotFound)
	e.problemIs(e.call(http.MethodPost, at(engBill, "comments"), nia, map[string]any{"body": "Hello"}), "addRecordComment", http.StatusNotFound, CodeNotFound)
	// A mention of Nia is refused as a mention of nobody is.
	noOne := e.call(http.MethodPost, at(engBill, "comments"), ahkow, map[string]any{"body": "@Ghost look", "mention_ids": []string{"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e99"}})
	cannot := e.call(http.MethodPost, at(engBill, "comments"), ahkow, map[string]any{"body": "@Nia look", "mention_ids": []string{m.nia}})
	if noOne.Status != 422 || cannot.Status != 422 || !strings.Contains(cannot.Raw, "is not a member who may see this record") ||
		strings.ReplaceAll(noOne.Raw, "01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e99", "") == "" || !strings.Contains(noOne.Raw, "is not a member who may see this record") {
		t.Fatalf("a mention of a member who may not see it: %d %s / %d %s", cannot.Status, cannot.Raw, noOne.Status, noOne.Raw)
	}
	for what, body := range map[string]map[string]any{"empty": {"body": "   "}, "twice": {"body": "x", "mention_ids": []string{m.mei, m.mei}}} {
		if r := e.call(http.MethodPost, at(engBill, "comments"), ahkow, body); r.Status != 422 {
			t.Errorf("%s: %d %s", what, r.Status, r.Raw)
		}
	}

	// Ah Kow comments and mentions Wei Ling and Mei: kept on the bill, and they are told.
	drainWork(t, e)
	c := e.call(http.MethodPost, at(engBill, "comments"), ahkow, map[string]any{"body": "@Wei Ling @Mei the GST on this line looks high.", "mention_ids": []string{m.weiling, m.mei}})
	if c.Status != 201 || c.str("author", "name") != "Ah Kow" || len(c.Body["mentions"].([]any)) != 2 {
		t.Fatalf("commenting: %d %s", c.Status, c.Raw)
	}
	fanOutAll(t, e)
	for who, tok := range map[string]string{"Wei Ling": weiling, "Mei": mei} {
		n := e.call(http.MethodGet, "/v1/notifications", tok, nil)
		items, _ := n.Body["items"].([]any)
		if n.Status != 200 || len(items) != 1 || items[0].(map[string]any)["type"] != "comment.mention" || items[0].(map[string]any)["title"] != "Ah Kow mentioned you on a draft bill" {
			t.Errorf("%s's notification: %s", who, n.Raw)
		}
	}
	if n := e.call(http.MethodGet, "/v1/notifications", ahkow, nil); len(n.Body["items"].([]any)) != 0 {
		t.Errorf("the writer was told: %s", n.Raw)
	}
	if n := e.call(http.MethodGet, "/v1/notifications", nia, nil); len(n.Body["items"].([]any)) != 0 {
		t.Errorf("an Employee was told: %s", n.Raw)
	}
	thread := e.call(http.MethodGet, at(engBill, "comments"), weiling, nil)
	if thread.Status != 200 || len(thread.Body["items"].([]any)) != 1 {
		t.Fatalf("the thread: %d %s", thread.Status, thread.Raw)
	}
	// No other bill's thread has it.
	if r := e.call(http.MethodGet, at(opsBill, "comments"), weiling, nil); len(r.Body["items"].([]any)) != 0 {
		t.Errorf("another bill's thread: %s", r.Raw)
	}
	// A record of another type under the right ID is not found.
	e.problemIs(e.call(http.MethodGet, "/v1/records/invoice/"+engBill+"/comments", weiling, nil), "listRecordComments", http.StatusNotFound, CodeNotFound)
	if r := e.call(http.MethodGet, "/v1/records/payslip/"+engBill+"/comments", weiling, nil); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("a kind that takes no comments: %d %s", r.Status, r.Raw)
	}

	// Narrow Mei to Engineering: she sees Engineering's bill and not Operations'; so she is offered, mentioned and told for the first alone.
	e.narrowExisting(m.mei, eng)
	if r := e.call(http.MethodGet, at(opsBill, "mentionable"), ahkow, nil); fmt.Sprint(names(r)) != "[Wei Ling]" {
		t.Errorf("who may be mentioned on another department's bill: %s", r.Raw)
	}
	if r := e.call(http.MethodGet, at(engBill, "mentionable"), ahkow, nil); fmt.Sprint(names(r)) != "[Mei Wei Ling]" {
		t.Errorf("who may be mentioned on their department's bill: %s", r.Raw)
	}
	if r := e.call(http.MethodPost, at(opsBill, "comments"), ahkow, map[string]any{"body": "@Mei look", "mention_ids": []string{m.mei}}); r.Status != 422 {
		t.Errorf("a mention of a member narrowed away from the record: %d %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodGet, at(opsBill, "comments"), mei, nil), "listRecordComments", http.StatusNotFound, CodeNotFound)
	if r := e.call(http.MethodGet, at(engBill, "comments"), mei, nil); r.Status != 200 || len(r.Body["items"].([]any)) != 1 {
		t.Errorf("their department's thread: %d %s", r.Status, r.Raw)
	}
	// The Auditor and every other role: no one who may not see a contact reads its thread.
	if r := e.call(http.MethodGet, "/v1/records/contact/"+e.supplier()+"/comments", nia, nil); r.Status != http.StatusNotFound && r.Status != http.StatusForbidden {
		t.Errorf("an Employee reads a contact's thread: %d %s", r.Status, r.Raw)
	}
}
