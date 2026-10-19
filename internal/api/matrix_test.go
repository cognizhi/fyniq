//go:build local

package api

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/authz/authztest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// matrixCall makes one operation's request for a caller, with fresh records where it changes one, and
// reports whether the call changed nothing when it was refused.
type matrixCall func(e *env, token string) (r response, unchanged func() bool)

// gstYears keeps the matrix's GST registrations apart, a year each.
var gstYears int

// matrixGST returns the ID of a GST registration of e's organisation, recording one first if needed.
func matrixGST(e *env) string {
	l := e.call(http.MethodGet, "/v1/organisation/gst-registrations", e.as(authz.Controller), nil)
	if items, _ := l.Body["items"].([]any); len(items) > 0 {
		return items[0].(map[string]any)["id"].(string)
	}
	r := e.call(http.MethodPost, "/v1/organisation/gst-registrations", e.as(authz.Controller),
		map[string]any{"registered_from": "2090-01-01", "deregistered_from": "2090-12-31", "gst_number": "202312345K"})
	return r.str("id")
}

// matrixCalls has one request per operation; an operation without one fails the test.
var matrixCalls = map[string]matrixCall{
	"getOrganisation": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/organisation", tok, nil), nil
	},
	"listAccounts": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/accounts", tok, nil), nil
	},
	"getOrganisationProfile": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/organisation/profile", tok, nil), nil
	},
	"updateOrganisationProfile": func(e *env, tok string) (response, func() bool) {
		footer := "Matrix " + uuid.NewString()
		r := e.call(http.MethodPatch, "/v1/organisation/profile", tok, map[string]any{"document_footer": footer}, "If-Match", "*")
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/organisation/profile", e.as(authz.Controller), nil).Body["document_footer"] != footer
		}
	},
	"listGSTRegistrations": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/organisation/gst-registrations", tok, nil), nil
	},
	"getGSTStatus": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/organisation/gst-status?date=2026-10-20", tok, nil), nil
	},
	"getGSTRegistration": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/organisation/gst-registrations/"+matrixGST(e), tok, nil), nil
	},
	// Each allowed call records a registration a year after the last, so none overlaps.
	"createGSTRegistration": func(e *env, tok string) (response, func() bool) {
		gstYears++
		from := fmt.Sprintf("%d-01-01", 2100+gstYears)
		r := e.call(http.MethodPost, "/v1/organisation/gst-registrations", tok, map[string]any{"registered_from": from,
			"deregistered_from": fmt.Sprintf("%d-12-31", 2100+gstYears), "gst_number": "202312345K"})
		return r, func() bool {
			l := e.call(http.MethodGet, "/v1/organisation/gst-registrations?page_size=500", e.as(authz.Controller), nil)
			return !strings.Contains(l.Raw, `"registered_from":"`+from+`"`)
		}
	},
	"updateGSTRegistration": func(e *env, tok string) (response, func() bool) {
		id := matrixGST(e)
		number := fmt.Sprintf("M%d", gstYears+100000)
		gstYears++
		r := e.call(http.MethodPatch, "/v1/organisation/gst-registrations/"+id, tok, map[string]any{"gst_number": number}, "If-Match", "*")
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/organisation/gst-registrations/"+id, e.as(authz.Controller), nil).str("gst_number") != number
		}
	},
	"listSequences": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/organisation/sequences", tok, nil), nil
	},
	"getSequence": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/organisation/sequences/journal", tok, nil), nil
	},
	// Each allowed call moves the next number forward by one.
	"updateSequence": func(e *env, tok string) (response, func() bool) {
		cur := e.call(http.MethodGet, "/v1/organisation/sequences/journal", e.as(authz.Controller), nil)
		next := cur.Body["next_number"].(float64) + 1
		r := e.call(http.MethodPatch, "/v1/organisation/sequences/journal", tok, map[string]any{"next_number": next}, "If-Match", "*")
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/organisation/sequences/journal", e.as(authz.Controller), nil).Body["next_number"] != next
		}
	},
	// The chart loads only into an organisation with no account, so each call is made, with the same
	// principal, in an organisation of its own.
	"loadStandardChart": func(e *env, tok string) (response, func() bool) {
		ctx := context.Background()
		org, err := e.store.CreateOrganisation(ctx, storage.NewOrganisation{Name: "Matrix chart " + uuid.NewString()[:8], BaseCurrency: "SGD"})
		if err != nil {
			panic(err)
		}
		e.tokens.mu.Lock()
		p := e.tokens.m[tok]
		e.tokens.mu.Unlock()
		if p.Organisation != uuid.Nil { // the Super admin stays outside every organisation
			p.Organisation = org.ID
		}
		r := e.call(http.MethodPost, "/v1/accounts:load-standard-chart", e.principal(p), nil)
		return r, func() bool {
			var n int64
			_ = e.store.Do(ctx, org.ID, func(ctx context.Context, u storage.UnitOfWork) error {
				n, err = u.Accounts().Count(ctx)
				return err
			})
			return n == 0
		}
	},
	// Each allowed call adds an account with a code of its own.
	"createAccount": func(e *env, tok string) (response, func() bool) {
		gstYears++
		code := fmt.Sprintf("9%03d", gstYears)
		r := e.call(http.MethodPost, "/v1/accounts", tok, map[string]any{"code": code, "name": "Matrix " + code, "type": "expense"})
		return r, func() bool {
			l := e.call(http.MethodGet, "/v1/accounts?page_size=500", e.as(authz.Accountant), nil)
			return !strings.Contains(l.Raw, `"code":"`+code+`"`)
		}
	},
	"updateAccount": func(e *env, tok string) (response, func() bool) {
		name := "Rent " + uuid.NewString()[:8]
		r := e.call(http.MethodPatch, "/v1/accounts/"+e.books.Rent.ID.String(), tok, map[string]any{"name": name}, "If-Match", "*")
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/accounts/"+e.books.Rent.ID.String(), e.as(authz.Accountant), nil).str("name") != name
		}
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
	"bulkCreateJournals": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/journals:bulk", tok, map[string]any{"items": []any{e.draft("10.00")}}), nil
	},
	"postJournal": func(e *env, tok string) (response, func() bool) {
		id, tag := e.createDraft("10.00")
		r := e.call(http.MethodPost, "/v1/journals/"+id+"/post", tok, nil, "If-Match", tag)
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/journals/"+id, e.as(authz.Accountant), nil).str("status") == "draft"
		}
	},
	"getSetupStatus": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/setup", tok, nil), nil
	},
	// Setup runs once per installation, so the matrix sends a code that is never valid: allowed means
	// the API looked at the code (409), refused means 401 or 403.
	"completeSetup": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/setup", tok, map[string]any{"setup_code": "NEVER-A-VALID-SETUP-CODE-0",
			"display_name": "Matrix", "organisation": map[string]any{"name": "Matrix Pte Ltd", "base_currency": "SGD"}}), nil
	},
	// The Super admins' operations: no matrix principal is a Super admin in the database and no code
	// is valid, so allowed means the API looked past the policy (a refused code, or its Super admin
	// check), refused means the policy's 403.
	"createSuperAdminInvitation": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/super-admins/invitations", tok, nil), nil
	},
	"acceptSuperAdminInvitation": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/super-admins", tok, map[string]any{"invitation_code": "NEVER-A-VALID-INVITATION-0", "display_name": "Matrix"}), nil
	},
	"replaceSuperAdminRecoveryCodes": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/super-admins/recovery-codes", tok, nil), nil
	},
	"confirmSuperAdminRecovery": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/super-admins/01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01/recovery-confirmations", tok, nil), nil
	},
	"recoverSuperAdmin": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/super-admins/recovery", tok, map[string]any{"recovery_code": "NEVER-A-VALID-RECOVERY-CODE-0",
			"confirmation_code": "NEVER-A-VALID-CONFIRMATION-0"}), nil
	},
	"listSignInProviders": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/sign-in-providers", tok, nil), nil
	},
	"getSession": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/session", tok, nil), nil
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
	switch {
	case p.Public:
		return true, false
	case p.Capability == authz.Setup || p.Capability == authz.SuperAdmins || p.Capability == authz.Session: // any person; never the AI
		return role != authz.AI, false
	}
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
			beyondPolicy := r.Status == http.StatusForbidden && (r.problemCode() == CodeNotSuperAdmin || r.problemCode() == CodeStepUpRequired)
			switch {
			case want && policies[op].NoOrganisation && (r.Status == http.StatusUnauthorized || (r.Status == http.StatusForbidden && !beyondPolicy)):
				t.Errorf("%s × %s: allowed, the API answered %d %s", role, op, r.Status, r.problemCode())
			case want && !policies[op].NoOrganisation && (r.Status < 200 || r.Status > 299):
				t.Errorf("%s × %s: Appendix D allows it, the API answered %d %s", role, op, r.Status, r.problemCode())
			case !want && (r.Status != http.StatusForbidden || beyondPolicy):
				t.Errorf("%s × %s: Appendix D refuses it, the API answered %d %s", role, op, r.Status, r.problemCode())
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
