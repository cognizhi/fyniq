//go:build local

package api

import (
	"maps"
	"net/http"
	"slices"
	"testing"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/authz/authztest"
)

// matrixCall makes one operation's request for a caller, with fresh records where it changes one, and
// reports whether the call changed nothing when it was refused.
type matrixCall func(e *env, token string) (r response, unchanged func() bool)

// matrixCalls has one request per operation; an operation without one fails the test.
var matrixCalls = map[string]matrixCall{
	"getOrganisation": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/organisation", tok, nil), nil
	},
	"listAccounts": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/accounts", tok, nil), nil
	},
	"getAccount": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/accounts/"+e.books.Bank.ID.String(), tok, nil), nil
	},
	"getJournal": func(e *env, tok string) (response, func() bool) {
		id, _ := e.createDraft("10.00")
		return e.call(http.MethodGet, "/v1/journals/"+id, tok, nil), nil
	},
	"createJournal": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/journals", tok, e.draft("10.00")), nil
	},
	"postJournal": func(e *env, tok string) (response, func() bool) {
		id, tag := e.createDraft("10.00")
		r := e.call(http.MethodPost, "/v1/journals/"+id+"/post", tok, nil, "If-Match", tag)
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/journals/"+id, e.as(authz.Accountant), nil).str("status") == "draft"
		}
	},
	"reverseJournal": func(e *env, tok string) (response, func() bool) {
		id, tag := e.postedJournal("10.00")
		r := e.call(http.MethodPost, "/v1/journals/"+id+"/reverse", tok, map[string]any{"date": "2026-10-28", "reason": "Matrix test"}, "If-Match", tag)
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/journals/"+id, e.as(authz.Accountant), nil).str("status") == "posted"
		}
	},
}

// expected reads PRD-001 Appendix D for a role and a policy, independently of package authz's own
// decision code: the cell of the role's column, or membership for the organisation itself. The AI
// acts for a user who holds every role, so only the AI column limits it.
func expected(t *testing.T, rows map[string]map[string]string, role authz.Role, p authz.Policy) (allowed, masked bool) {
	col := authz.Column(role)
	if col == "" {
		return false, false // the HR officer (Nemura only) and the Super admin hold no organisation right
	}
	if p.Capability == authz.Membership {
		return true, false
	}
	row, ok := rows[string(p.Capability)]
	if !ok {
		t.Fatalf("Appendix D has no row %q", p.Capability)
	}
	return authztest.Allows(authztest.ParseCell(row[col]), p.Need)
}

// The authorisation matrix: every role of §7.1, the AI and the Super admin × every operation,
// allowed or refused as Appendix D says, and a refusal changes nothing (§17.3 exit item 2, API half).
// req: QA-SEC-03
// req: SEC-02
// asvs: V8.2.1, V8.3.1
func TestAuthorisationMatrix(t *testing.T) {
	e := newEnv(t)
	_, rows := authztest.AppendixD(t)
	policies := e.api.Policies()
	for op := range policies {
		if matrixCalls[op] == nil {
			t.Errorf("operation %s has no row in the authorisation matrix test", op)
		}
	}
	every := make([]authz.Role, 0, len(authz.Roles))
	for r := range authz.Roles {
		every = append(every, r)
	}
	slices.Sort(every)
	var allowed, refused int
	for _, role := range authz.Principals() {
		p := authn.Principal{Client: "web", Class: authn.ClassUser, Organisation: e.books.Org, Roles: []authz.Role{role}}
		switch role {
		case authz.AI:
			p.Class, p.Roles = authn.ClassAgent, every
		case authz.SuperAdmin:
			p.Organisation = [16]byte{} // outside every organisation (FR-ADM-07)
		}
		tok := e.principal(p)
		for _, op := range slices.Sorted(maps.Keys(policies)) {
			want, _ := expected(t, rows, role, policies[op])
			r, unchanged := matrixCalls[op](e, tok)
			switch {
			case want && (r.Status < 200 || r.Status > 299):
				t.Errorf("%s × %s: Appendix D allows it, the API answered %d %s", role, op, r.Status, r.problemCode())
			case !want && r.Status != http.StatusForbidden:
				t.Errorf("%s × %s: Appendix D refuses it, the API answered %d", role, op, r.Status)
			case !want && unchanged != nil && !unchanged():
				t.Errorf("%s × %s: refused, but the record changed", role, op)
			}
			if want {
				allowed++
			} else {
				refused++
			}
		}
	}
	t.Logf("%d roles × %d operations: %d allowed, %d refused", len(authz.Principals()), len(policies), allowed, refused)
	if allowed == 0 || refused == 0 {
		t.Fatal("a matrix with one outcome proves nothing")
	}
}
