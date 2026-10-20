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
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/authz/authztest"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
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

// matrixOpening makes an organisation of its own for tok's principal, with books from 1 October 2026 and, when draft is
// true, balanced opening balances saved as a draft.
func matrixOpening(e *env, tok string, draft bool) (porttest.Books, authn.Principal) {
	ctx := context.Background()
	b := porttest.NewBooks(e.t, e.store, "Matrix opening "+uuid.NewString()[:8])
	start, end := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	if err := e.store.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		pr := o.Profile
		pr.FYStartMonth, pr.BooksStart, pr.FirstYearEnd = 1, &start, &end
		_, err = u.UpdateOrganisation(ctx, o.Version, pr)
		return err
	}); err != nil {
		panic(err)
	}
	if draft {
		sgd := money.MustLookup("SGD")
		ten, err := money.Parse("10.00", sgd)
		if err != nil {
			panic(err)
		}
		minus := ten.Neg()
		if _, err := e.api.journals.SaveOpening(ctx, b.Org, "", []ledger.JournalLine{{AccountID: b.Bank.ID, Amount: ten}, {AccountID: b.Sales.ID, Amount: minus}}); err != nil {
			panic(err)
		}
	}
	e.tokens.mu.Lock()
	p := e.tokens.m[tok]
	e.tokens.mu.Unlock()
	if p.Organisation != uuid.Nil {
		p.Organisation = b.Org
	}
	return b, p
}

// matrixOpeningStatus is the status of an organisation's opening balances, "" when there are none.
func matrixOpeningStatus(e *env, org uuid.UUID) storage.DocumentStatus {
	var status storage.DocumentStatus
	_ = e.store.Do(context.Background(), org, func(ctx context.Context, u storage.UnitOfWork) error {
		d, err := u.Documents().OpeningBalance(ctx)
		status = d.Status
		return err
	})
	return status
}

// matrixContact returns the ID of a contact of e's organisation, adding one first if needed.
func matrixContact(e *env) string {
	l := e.call(http.MethodGet, "/v1/contacts", e.as(authz.Accountant), nil)
	if items, _ := l.Body["items"].([]any); len(items) > 0 {
		return items[0].(map[string]any)["id"].(string)
	}
	return e.call(http.MethodPost, "/v1/contacts", e.as(authz.Accountant), map[string]any{"legal_name": "Matrix contact", "customer": true}).str("id")
}

// matrixItem returns the ID of an item of e's organisation, adding one first if needed.
func matrixItem(e *env) string {
	l := e.call(http.MethodGet, "/v1/items", e.as(authz.Accountant), nil)
	if items, _ := l.Body["items"].([]any); len(items) > 0 {
		return items[0].(map[string]any)["id"].(string)
	}
	return e.call(http.MethodPost, "/v1/items", e.as(authz.Accountant), map[string]any{"code": "MATRIX", "name": "Matrix item", "type": "service"}).str("id")
}

// matrixMember returns the ID of a member of e's organisation, inviting and adding one first if needed. The
// organisation keeps one member at most, so role changes stay in the founder bootstrap (FR-ADM-01).
func matrixMember(e *env) string {
	l := e.call(http.MethodGet, "/v1/members", e.as(authz.Administrator), nil)
	if items, _ := l.Body["items"].([]any); len(items) > 0 {
		return items[0].(map[string]any)["id"].(string)
	}
	code := e.call(http.MethodPost, "/v1/members/invitations", e.as(authz.Administrator), map[string]any{"display_name": "Matrix member",
		"roles": []string{"employee"}}).str("code")
	return e.call(http.MethodPost, "/v1/members", e.principal(authn.Principal{Subject: "matrix-member", Client: "web"}),
		map[string]any{"invitation_code": code, "display_name": "Matrix member"}).str("member_id")
}

// matrixInvitation returns the ID of a new pending invitation of e's organisation.
func matrixInvitation(e *env) string {
	return e.call(http.MethodPost, "/v1/members/invitations", e.as(authz.Administrator), map[string]any{"display_name": "Matrix invitee",
		"roles": []string{"bookkeeper"}}).str("id")
}

// matrixEvidence attaches a receipt to a new draft journal as an Accountant and returns both IDs.
func matrixEvidence(e *env) (journalID, evidenceID string) {
	journalID, _ = e.createDraft("10.00")
	r := e.upload("/v1/journals/"+journalID+"/evidence", e.as(authz.Accountant), map[string]string{"evidence_type": "receipt"},
		"receipt.pdf", []byte("%PDF-1.7 matrix receipt"), "If-Match", "*")
	if r.Status != http.StatusCreated {
		e.t.Fatalf("addJournalEvidence = %d: %s", r.Status, r.Raw)
	}
	return journalID, r.str("id")
}

// matrixCalls has one request per operation; an operation without one fails the test.
var matrixCalls = map[string]matrixCall{
	"addJournalEvidence": func(e *env, tok string) (response, func() bool) {
		id, _ := e.createDraft("10.00")
		r := e.upload("/v1/journals/"+id+"/evidence", tok, map[string]string{"evidence_type": "receipt"}, "receipt.pdf",
			[]byte("%PDF-1.7 matrix receipt"), "If-Match", "*")
		return r, func() bool {
			l := e.call(http.MethodGet, "/v1/journals/"+id+"/evidence", e.as(authz.Accountant), nil)
			items, _ := l.Body["items"].([]any)
			return l.Status == http.StatusOK && len(items) == 0
		}
	},
	"listJournalEvidence": func(e *env, tok string) (response, func() bool) {
		id, _ := matrixEvidence(e)
		return e.call(http.MethodGet, "/v1/journals/"+id+"/evidence", tok, nil), nil
	},
	"getJournalEvidence": func(e *env, tok string) (response, func() bool) {
		id, ev := matrixEvidence(e)
		return e.call(http.MethodGet, "/v1/journals/"+id+"/evidence/"+ev, tok, nil), nil
	},
	"removeJournalEvidence": func(e *env, tok string) (response, func() bool) {
		id, ev := matrixEvidence(e)
		r := e.call(http.MethodDelete, "/v1/journals/"+id+"/evidence/"+ev, tok, nil, "If-Match", "*")
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/journals/"+id+"/evidence/"+ev, e.as(authz.Accountant), nil).str("state") == "attached"
		}
	},
	"sendTestEmail": func(e *env, tok string) (response, func() bool) {
		matrixProfileEmail(e)
		drainWork(e.t, e)
		r := e.call(http.MethodPost, "/v1/organisation/email/test-messages", tok, nil)
		return r, func() bool { return nextWork(e.t, e) == "" }
	},
	"getEmailMessage": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/organisation/email/messages/"+matrixEmail(e), tok, nil), nil
	},
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
	"listContacts": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/contacts", tok, nil), nil
	},
	"getContact": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/contacts/"+matrixContact(e), tok, nil), nil
	},
	"createContact": func(e *env, tok string) (response, func() bool) {
		name := "Matrix " + uuid.NewString()[:8]
		r := e.call(http.MethodPost, "/v1/contacts", tok, map[string]any{"legal_name": name, "customer": true})
		return r, func() bool {
			l := e.call(http.MethodGet, "/v1/contacts?page_size=500", e.as(authz.Accountant), nil)
			return !strings.Contains(l.Raw, `"legal_name":"`+name+`"`)
		}
	},
	"updateContact": func(e *env, tok string) (response, func() bool) {
		id, phone := matrixContact(e), "+65 "+uuid.NewString()[:8]
		r := e.call(http.MethodPatch, "/v1/contacts/"+id, tok, map[string]any{"phone": phone}, "If-Match", "*")
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/contacts/"+id, e.as(authz.Accountant), nil).str("phone") != phone
		}
	},
	"listSettings": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/organisation/settings", tok, nil), nil
	},
	"getSetting": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/organisation/settings/features.payroll", tok, nil), nil
	},
	"updateSetting": func(e *env, tok string) (response, func() bool) {
		path := "/v1/organisation/settings/sales.payment_terms_days"
		version := func() any { return e.call(http.MethodGet, path, e.as(authz.Controller), nil).Body["version"] }
		was := version()
		r := e.call(http.MethodPatch, path, tok, map[string]any{"value": "31"}, "If-Match", "*")
		return r, func() bool { return version() == was }
	},
	"listMembers": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/members", tok, nil), nil
	},
	"listMemberInvitations": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/members/invitations", tok, nil), nil
	},
	"createMemberInvitation": func(e *env, tok string) (response, func() bool) {
		name := "Matrix " + uuid.NewString()[:8]
		r := e.call(http.MethodPost, "/v1/members/invitations", tok, map[string]any{"display_name": name, "roles": []string{"bookkeeper"}})
		return r, func() bool {
			return !strings.Contains(e.call(http.MethodGet, "/v1/members/invitations?page_size=500", e.as(authz.Administrator), nil).Raw, name)
		}
	},
	"withdrawMemberInvitation": func(e *env, tok string) (response, func() bool) {
		id := matrixInvitation(e)
		r := e.call(http.MethodDelete, "/v1/members/invitations/"+id, tok, nil)
		return r, func() bool {
			return strings.Contains(e.call(http.MethodGet, "/v1/members/invitations?page_size=500", e.as(authz.Administrator), nil).Raw, id)
		}
	},
	"acceptMemberInvitation": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/members", tok, map[string]any{"invitation_code": "NEVER-A-VALID-INVITATION-0", "display_name": "Matrix"}), nil
	},
	"assignRole": func(e *env, tok string) (response, func() bool) {
		id := matrixMember(e)
		r := e.call(http.MethodPost, "/v1/members/"+id+"/roles", tok, map[string]any{"role": "auditor"})
		held := func() bool {
			return strings.Contains(e.call(http.MethodGet, "/v1/members", e.as(authz.Administrator), nil).Raw, `"auditor"`)
		}
		if held() { // allowed: end it again, so the next principal can be given it
			e.call(http.MethodDelete, "/v1/members/"+id+"/roles/auditor", e.as(authz.Administrator), nil)
		}
		return r, func() bool { return !held() }
	},
	"revokeRole": func(e *env, tok string) (response, func() bool) {
		id := matrixMember(e)
		e.call(http.MethodPost, "/v1/members/"+id+"/roles", e.as(authz.Administrator), map[string]any{"role": "tax_agent"})
		r := e.call(http.MethodDelete, "/v1/members/"+id+"/roles/tax_agent", tok, nil)
		held := strings.Contains(e.call(http.MethodGet, "/v1/members", e.as(authz.Administrator), nil).Raw, `"tax_agent"`)
		if held { // refused: end it, so the next principal can try again
			e.call(http.MethodDelete, "/v1/members/"+id+"/roles/tax_agent", e.as(authz.Administrator), nil)
		}
		return r, func() bool { return held }
	},
	// Each approval's own policy decides what a person sees and signs (rev 0.36), so the matrix asks for a request
	// that does not exist: allowed means the API looked past the operation's policy (404, or step-up for a signature);
	// approvals_test.go signs real ones.
	"withdrawApproval": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/approvals/"+uuid.NewString()+"/withdraw", steppedUp(e, tok), nil, "If-Match", `"1"`), nil
	},
	"listApprovalPolicies": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/approval-policies", tok, nil), nil
	},
	"getApprovalPolicy": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/approval-policies/organisation_setting", tok, nil), nil
	},
	// A policy change always waits for approval: an allowed role, as a person who passed step-up, gets the request.
	"changeApprovalPolicy": func(e *env, tok string) (response, func() bool) {
		was := policyRequests(e)
		r := e.call(http.MethodPatch, "/v1/approval-policies/membership", steppedUp(e, tok), map[string]any{"steps": []map[string]any{{"quorum": 1}}},
			"If-Match", `"0"`)
		return r, func() bool { return policyRequests(e) == was }
	},
	"listStatutoryPacks": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/statutory-packs", tok, nil), nil
	},
	"getStatutoryPack": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/statutory-packs/sg-gst@2026.1", tok, nil), nil
	},
	// Asking to activate always waits for approval; an allowed role's request is withdrawn again, so the next role
	// can ask too.
	"activateStatutoryPack": func(e *env, tok string) (response, func() bool) {
		was := packRequests(e)
		person := steppedUp(e, tok)
		r := e.call(http.MethodPost, "/v1/statutory-packs/sg-gst@2026.1/activate", person, nil, "If-Match", `"0"`)
		if r.Status == http.StatusAccepted {
			e.call(http.MethodPost, "/v1/approvals/"+r.str("id")+"/withdraw", person, nil, "If-Match", `"1"`)
		}
		return r, func() bool { return packRequests(e) == was }
	},
	// The passkey ceremonies are a person's own, in a browser session: the matrix's principals have none, so an allowed
	// role is refused past its policy (API-AUTHZ-04) and the AI by it. TestPasskeys runs them with a real session.
	"listPasskeys": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/session/passkeys", tok, nil), nil
	},
	"startPasskeyRegistration": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/session/passkeys/challenge", tok, nil), nil
	},
	"registerPasskey": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/session/passkeys", tok, map[string]any{"challenge_id": uuid.NewString(), "name": "Laptop",
			"credential": map[string]any{"id": "AAAA"}}), nil
	},
	"revokePasskey": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodDelete, "/v1/session/passkeys/"+uuid.NewString(), tok, nil), nil
	},
	"startStepUp": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/session/step-up/challenge", tok, nil), nil
	},
	"stepUp": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/session/step-up", tok, map[string]any{"challenge_id": uuid.NewString(),
			"credential": map[string]any{"id": "AAAA"}}), nil
	},
	// A person's devices are their browser sessions, which the matrix's principals lack: an allowed role is refused past
	// its policy (API-AUTHZ-04). TestYourDevices runs them with real sessions.
	"listDevices": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/session/devices", tok, nil), nil
	},
	"signOutDevice": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodDelete, "/v1/session/devices/00000000000000000000000000000000", tok, nil), nil
	},
	"signOutOtherDevices": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/session/devices/sign-out-others", tok, nil), nil
	},
	"getTeamMode": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/team-mode", tok, nil), nil
	},
	// Asking to switch always waits for approval; an allowed role's request is withdrawn again, so the next role finds
	// none pending.
	"switchTeamMode": func(e *env, tok string) (response, func() bool) {
		was := requestsOf(e, "team_mode")
		person := steppedUp(e, tok)
		r := e.call(http.MethodPatch, "/v1/team-mode", person, map[string]any{"mode": "standard"}, "If-Match", `"0"`)
		if r.Status == http.StatusAccepted {
			e.call(http.MethodPost, "/v1/approvals/"+r.str("id")+"/withdraw", person, nil, "If-Match", `"1"`)
		}
		return r, func() bool { return requestsOf(e, "team_mode") == was }
	},
	// A passkey challenge needs a browser session, which the matrix's principals lack: an allowed role is refused past its
	// policy (API-AUTHZ-04). TestPasskeySignedApprovals runs it with real sessions.
	"challengeApproval": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/approvals/"+uuid.NewString()+"/challenge", steppedUp(e, tok), nil, "If-Match", `"1"`), nil
	},
	"listReviewLog": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/review-log", tok, nil), nil
	},
	// Reviewing needs entries the caller did not act on; an unknown one is refused past the policy (404).
	"reviewLogEntries": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/review-log/reviews", steppedUp(e, tok), map[string]any{"entry_ids": []string{uuid.NewString()}}), nil
	},
	"listApprovals": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/approvals", tok, nil), nil
	},
	"getApproval": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/approvals/"+uuid.NewString(), tok, nil), nil
	},
	"approveApproval": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/approvals/"+uuid.NewString()+"/approve", tok, map[string]any{"envelope_hash": strings.Repeat("0", 64)},
			"If-Match", "*"), nil
	},
	"rejectApproval": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/approvals/"+uuid.NewString()+"/reject", tok, map[string]any{"envelope_hash": strings.Repeat("0", 64),
			"reason": "Matrix"}, "If-Match", "*"), nil
	},
	"listItems": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/items", tok, nil), nil
	},
	"getItem": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/items/"+matrixItem(e), tok, nil), nil
	},
	"createItem": func(e *env, tok string) (response, func() bool) {
		code := "M-" + uuid.NewString()[:8]
		r := e.call(http.MethodPost, "/v1/items", tok, map[string]any{"code": code, "name": "Matrix item", "type": "product"})
		return r, func() bool {
			l := e.call(http.MethodGet, "/v1/items?page_size=500", e.as(authz.Accountant), nil)
			return !strings.Contains(l.Raw, `"code":"`+code+`"`)
		}
	},
	"updateItem": func(e *env, tok string) (response, func() bool) {
		id, unit := matrixItem(e), uuid.NewString()[:8]
		r := e.call(http.MethodPatch, "/v1/items/"+id, tok, map[string]any{"unit": unit}, "If-Match", "*")
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/items/"+id, e.as(authz.Accountant), nil).str("unit") != unit
		}
	},
	"getAccount": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/accounts/"+e.books.Bank.ID.String(), tok, nil), nil
	},
	"listPeriods": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/periods", tok, nil), nil
	},
	"getPeriod": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/periods/"+e.books.Oct.ID.String(), tok, nil), nil
	},
	// Each call opens the first year of an organisation of its own, whose years are set.
	"openFinancialYear": func(e *env, tok string) (response, func() bool) {
		ctx := context.Background()
		org, err := e.store.CreateOrganisation(ctx, storage.NewOrganisation{Name: "Matrix year " + uuid.NewString()[:8], BaseCurrency: "SGD"})
		if err != nil {
			panic(err)
		}
		start, end := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
		if err := e.store.Do(ctx, org.ID, func(ctx context.Context, u storage.UnitOfWork) error {
			o, err := u.Organisation(ctx)
			if err != nil {
				return err
			}
			p := o.Profile
			p.FYStartMonth, p.BooksStart, p.FirstYearEnd = 1, &start, &end
			_, err = u.UpdateOrganisation(ctx, o.Version, p)
			return err
		}); err != nil {
			panic(err)
		}
		e.tokens.mu.Lock()
		p := e.tokens.m[tok]
		e.tokens.mu.Unlock()
		if p.Organisation != uuid.Nil {
			p.Organisation = org.ID
		}
		r := e.call(http.MethodPost, "/v1/periods:open-year", e.principal(p), map[string]any{"starts_on": "2026-10-01"})
		return r, func() bool {
			var n int
			_ = e.store.Do(ctx, org.ID, func(ctx context.Context, u storage.UnitOfWork) error {
				ps, err := u.Periods().List(ctx)
				n = len(ps)
				return err
			})
			return n == 0
		}
	},
	"getOpeningBalances": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/opening-balances", tok, nil), nil
	},
	// The opening balances are one per organisation and post once, so each call is made, with the same principal, in an
	// organisation of its own, whose books start on 1 October 2026.
	"replaceOpeningBalances": func(e *env, tok string) (response, func() bool) {
		b, p := matrixOpening(e, tok, false)
		r := e.call(http.MethodPut, "/v1/opening-balances", e.principal(p), map[string]any{"lines": []any{
			map[string]any{"account_id": b.Bank.ID.String(), "amount": map[string]any{"amount": "10.00", "currency": "SGD"}}}}, "If-Match", "*")
		return r, func() bool { return matrixOpeningStatus(e, b.Org) == "" }
	},
	"postOpeningBalances": func(e *env, tok string) (response, func() bool) {
		b, p := matrixOpening(e, tok, true)
		r := e.call(http.MethodPost, "/v1/opening-balances/post", e.principal(p), nil, "If-Match", "*")
		return r, func() bool { return matrixOpeningStatus(e, b.Org) == storage.DocumentDraft }
	},
	"listJournals": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/journals", tok, nil), nil
	},
	"replaceJournal": func(e *env, tok string) (response, func() bool) {
		id, _ := e.createDraft("10.00")
		r := e.call(http.MethodPut, "/v1/journals/"+id, tok, e.draft("11.00"), "If-Match", "*")
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/journals/"+id, e.as(authz.Accountant), nil).Body["version"] == float64(1)
		}
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
	case p.Capability == authz.Setup || p.Capability == authz.SuperAdmins || p.Capability == authz.Session || p.Capability == authz.Joining: // any person; never the AI
		return role != authz.AI, false
	case p.OwnerMay && role == authz.Owner: // FR-ADM-01's founder bootstrap (rev 0.35): the matrix's organisation has no second member
		return true, false
	case p.Capability == authz.Approvals: // a person with the right on a row approvals cover (rev 0.36); never the AI
		if role == authz.AI || authz.Column(role) == "" {
			return false, false
		}
		for _, c := range authz.ApprovalRows {
			if ok, _ := authztest.Allows(authztest.ParseCell(rows[string(c)][authz.Column(role)]), p.Need); ok {
				return true, false
			}
		}
		return false, false
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
	allowed, masked = authztest.Allows(authztest.ParseCell(row[col]), p.Need)
	if allowed && p.Parent != "" { // and the record it is under must be visible, unmasked
		parent, ok := rows[string(p.Parent)]
		if !ok {
			t.Fatalf("Appendix D has no row %q", p.Parent)
		}
		if sees, partly := authztest.Allows(authztest.ParseCell(parent[col]), authz.View); !sees || partly {
			return false, false
		}
	}
	return allowed, masked
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
			beyondPolicy := r.Status == http.StatusForbidden && (r.problemCode() == CodeNotSuperAdmin || r.problemCode() == CodeStepUpRequired ||
				r.problemCode() == CodeSessionRequired)
			// The installation's operations and the approvals decide past their policy: by a code, or by the request's own policy.
			lenient := policies[op].NoOrganisation || policies[op].Capability == authz.Approvals
			switch {
			case want && lenient && (r.Status == http.StatusUnauthorized || (r.Status == http.StatusForbidden && !beyondPolicy)):
				t.Errorf("%s × %s: allowed, the API answered %d %s", role, op, r.Status, r.problemCode())
			case want && !lenient && (r.Status < 200 || r.Status > 299):
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
