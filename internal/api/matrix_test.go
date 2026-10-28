//go:build local

package api

import (
	"context"
	"errors"
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
	"github.com/cognizhi/fyniq/internal/ports"
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

// matrixDays keeps the matrix's exchange rates apart, a day each.
var matrixDays int

// matrixDay is a day no other matrix call uses.
func matrixDay() time.Time {
	matrixDays++
	return time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, matrixDays)
}

// matrixRate enters a USD rate for on in e's organisation.
func matrixRate(e *env, on time.Time) storage.FXRate {
	var r storage.FXRate
	if err := e.store.Do(storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "matrix-fixture"}}), e.books.Org,
		func(ctx context.Context, u storage.UnitOfWork) (err error) {
			r, err = u.FXRates().Enter(ctx, storage.NewFXRate{Kind: storage.FXRateDay, Currency: "USD", On: on, Rate: "1.3", Quote: "1.3", QuoteForm: "base-per-unit"})
			return err
		}); err != nil {
		panic(err)
	}
	return r
}

// matrixRates are e's organisation's current rates for on.
func matrixRates(e *env, on time.Time) []storage.FXRate {
	var rs []storage.FXRate
	if err := e.store.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		rs, err = u.FXRates().List(ctx, storage.FXRateFilter{From: on, To: on})
		return err
	}); err != nil {
		panic(err)
	}
	return rs
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

// matrixChecklistOrg makes an organisation of its own for tok's principal, with its October and November.
func matrixChecklistOrg(e *env, tok string) (porttest.Books, authn.Principal) {
	b := porttest.NewBooks(e.t, e.store, "Matrix checklist "+uuid.NewString()[:8])
	e.tokens.mu.Lock()
	p := e.tokens.m[tok]
	e.tokens.mu.Unlock()
	if p.Organisation != uuid.Nil {
		p.Organisation = b.Org
	}
	return b, p
}

// matrixAccountingOrg is the organisation the accounting framework and policies are tried in, with books from 1 October 2026.
var matrixAccountingOrg = map[*env]uuid.UUID{}

// matrixAccounting returns tok's principal moved into the organisation of the accounting framework and policies, made
// once per env, and a token of an Owner of it, who records the versions others confirm or approve.
func matrixAccounting(e *env, tok string) (authn.Principal, string) {
	org, ok := matrixAccountingOrg[e]
	if !ok {
		b, _ := matrixOpening(e, e.as(authz.Owner), false)
		org, matrixAccountingOrg[e] = b.Org, b.Org
	}
	e.tokens.mu.Lock()
	p := e.tokens.m[tok]
	e.tokens.mu.Unlock()
	if p.Organisation != uuid.Nil {
		p.Organisation = org
	}
	p.StepUpAt = e.clock.Now() // signing asks for step-up (rev 0.63)
	return p, e.principal(authn.Principal{Client: "web", Class: authn.ClassUser, Organisation: org, Roles: []authz.Role{authz.Owner}})
}

// matrixFrameworkVersion is the version of the framework in force in the accounting organisation, 0 when none is.
func matrixFrameworkVersion(e *env, owner string) float64 {
	f, _ := e.call(http.MethodGet, "/v1/organisation/accounting", owner, nil).Body["framework"].(map[string]any)
	v, _ := f["version"].(float64)
	return v
}

// matrixPolicy is the version and approver of the leases policy in force in the accounting organisation.
func matrixPolicy(e *env, owner string) (float64, any) {
	for _, it := range e.call(http.MethodGet, "/v1/organisation/accounting", owner, nil).Body["policies"].([]any) {
		if a := it.(map[string]any); a["area"] == "leases" && a["policy"] != nil {
			pol := a["policy"].(map[string]any)
			return pol["version"].(float64), pol["approved_by"]
		}
	}
	return 0, nil
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

// matrixRecurring adds an active recurring journal, as an Accountant, and returns its ID.
func matrixRecurring(e *env) string {
	return e.call(http.MethodPost, "/v1/recurring-journals", e.as(authz.Accountant),
		e.recurring("Matrix "+uuid.NewString()[:8], "10.00", monthly(1, "2026-11-01"))).str("id")
}

// matrixRecurringBill adds an active recurring bill, as an Accountant, and returns its ID.
func matrixRecurringBill(e *env) string {
	return e.call(http.MethodPost, "/v1/recurring-bills", e.as(authz.Accountant),
		e.recurringBill("Matrix "+uuid.NewString()[:8], "10.00", monthly(1, "2026-11-01"))).str("id")
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

// matrixBillEvidence attaches a file to a new draft bill, as an Accountant, and returns both IDs.
func matrixBillEvidence(e *env) (billID, evidenceID string) {
	billID, _ = e.createBill("10.00", "0.90")
	r := e.upload("/v1/bills/"+billID+"/evidence", e.as(authz.Accountant), map[string]string{"evidence_type": "invoice"},
		"invoice.pdf", []byte("%PDF-1.7 matrix invoice"), "If-Match", "*")
	if r.Status != http.StatusCreated {
		e.t.Fatalf("addBillEvidence = %d: %s", r.Status, r.Raw)
	}
	return billID, r.str("id")
}

// matrixInvoiceEvidence attaches a file to a new draft invoice, as an Accountant, and returns both IDs.
func matrixInvoiceEvidence(e *env) (invoiceID, evidenceID string) {
	invoiceID, _ = e.createInvoice("1", "10.00")
	r := e.upload("/v1/invoices/"+invoiceID+"/evidence", e.as(authz.Accountant), map[string]string{"evidence_type": "contract"},
		"order.pdf", []byte("%PDF-1.7 matrix order"), "If-Match", "*")
	if r.Status != http.StatusCreated {
		e.t.Fatalf("addInvoiceEvidence = %d: %s", r.Status, r.Raw)
	}
	return invoiceID, r.str("id")
}

// matrixCalls has one request per operation; an operation without one fails the test.
var matrixCalls = map[string]matrixCall{
	"addInvoiceEvidence": func(e *env, tok string) (response, func() bool) {
		id, _ := e.createInvoice("1", "10.00")
		r := e.upload("/v1/invoices/"+id+"/evidence", tok, map[string]string{"evidence_type": "contract"}, "order.pdf",
			[]byte("%PDF-1.7 matrix order"), "If-Match", "*")
		return r, func() bool {
			l := e.call(http.MethodGet, "/v1/invoices/"+id+"/evidence", e.as(authz.Accountant), nil)
			items, _ := l.Body["items"].([]any)
			return l.Status == http.StatusOK && len(items) == 0
		}
	},
	"listInvoiceEvidence": func(e *env, tok string) (response, func() bool) {
		id, _ := matrixInvoiceEvidence(e)
		return e.call(http.MethodGet, "/v1/invoices/"+id+"/evidence", tok, nil), nil
	},
	"getInvoiceEvidence": func(e *env, tok string) (response, func() bool) {
		id, ev := matrixInvoiceEvidence(e)
		return e.call(http.MethodGet, "/v1/invoices/"+id+"/evidence/"+ev, tok, nil), nil
	},
	"removeInvoiceEvidence": func(e *env, tok string) (response, func() bool) {
		id, ev := matrixInvoiceEvidence(e)
		r := e.call(http.MethodDelete, "/v1/invoices/"+id+"/evidence/"+ev, tok, nil, "If-Match", "*")
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/invoices/"+id+"/evidence/"+ev, e.as(authz.Accountant), nil).str("state") == "attached"
		}
	},
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
	"addBillEvidence": func(e *env, tok string) (response, func() bool) {
		id, _ := e.createBill("10.00", "0.90")
		r := e.upload("/v1/bills/"+id+"/evidence", tok, map[string]string{"evidence_type": "invoice"}, "invoice.pdf",
			[]byte("%PDF-1.7 matrix invoice"), "If-Match", "*")
		return r, func() bool {
			l := e.call(http.MethodGet, "/v1/bills/"+id+"/evidence", e.as(authz.Accountant), nil)
			items, _ := l.Body["items"].([]any)
			return l.Status == http.StatusOK && len(items) == 0
		}
	},
	"listBillEvidence": func(e *env, tok string) (response, func() bool) {
		id, _ := matrixBillEvidence(e)
		return e.call(http.MethodGet, "/v1/bills/"+id+"/evidence", tok, nil), nil
	},
	"getBillEvidence": func(e *env, tok string) (response, func() bool) {
		id, ev := matrixBillEvidence(e)
		return e.call(http.MethodGet, "/v1/bills/"+id+"/evidence/"+ev, tok, nil), nil
	},
	"removeBillEvidence": func(e *env, tok string) (response, func() bool) {
		id, ev := matrixBillEvidence(e)
		r := e.call(http.MethodDelete, "/v1/bills/"+id+"/evidence/"+ev, tok, nil, "If-Match", "*")
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/bills/"+id+"/evidence/"+ev, e.as(authz.Accountant), nil).str("state") == "attached"
		}
	},
	"listPurchaseOrders": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/purchase-orders", tok, nil), nil
	},
	"getPurchaseOrder": func(e *env, tok string) (response, func() bool) {
		id, _ := e.createOrder("1", "10")
		return e.call(http.MethodGet, "/v1/purchase-orders/"+id, tok, nil), nil
	},
	"createPurchaseOrder": func(e *env, tok string) (response, func() bool) {
		d := e.orderDraft("1", "10")
		ref := "M-" + uuid.NewString()[:8]
		d["reference"] = ref
		r := e.call(http.MethodPost, "/v1/purchase-orders", tok, d)
		return r, func() bool {
			l := e.call(http.MethodGet, "/v1/purchase-orders?page_size=500", e.as(authz.Accountant), nil)
			return !strings.Contains(l.Raw, `"reference":"`+ref+`"`)
		}
	},
	"replacePurchaseOrder": func(e *env, tok string) (response, func() bool) {
		id, _ := e.createOrder("1", "10")
		r := e.call(http.MethodPut, "/v1/purchase-orders/"+id, tok, e.orderDraft("2", "10"), "If-Match", "*")
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/purchase-orders/"+id, e.as(authz.Accountant), nil).Body["version"] == float64(1)
		}
	},
	"approvePurchaseOrder": func(e *env, tok string) (response, func() bool) {
		id, _ := e.createOrder("1", "10")
		r := e.call(http.MethodPost, "/v1/purchase-orders/"+id+"/approve", tok, nil, "If-Match", "*")
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/purchase-orders/"+id, e.as(authz.Accountant), nil).str("status") == "draft"
		}
	},
	"requestPurchaseOrderApproval": func(e *env, tok string) (response, func() bool) {
		was := requestsOf(e, "purchase_order")
		id, tag := e.createOrder("10", "250")
		person := steppedUp(e, tok)
		r := e.call(http.MethodPost, "/v1/purchase-orders/"+id+"/approval", person, nil, "If-Match", tag)
		if r.Status == http.StatusAccepted {
			e.call(http.MethodPost, "/v1/approvals/"+r.str("id")+"/withdraw", person, nil, "If-Match", `"1"`)
		}
		return r, func() bool { return requestsOf(e, "purchase_order") == was }
	},
	"closePurchaseOrder": func(e *env, tok string) (response, func() bool) {
		id, _ := e.approvedOrder()
		r := e.call(http.MethodPost, "/v1/purchase-orders/"+id+"/close", tok, map[string]any{"reason": "Matrix"}, "If-Match", "*")
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/purchase-orders/"+id, e.as(authz.Accountant), nil).str("status") == "approved"
		}
	},
	"billPurchaseOrder": func(e *env, tok string) (response, func() bool) {
		id, _ := e.approvedOrder()
		r := e.call(http.MethodPost, "/v1/purchase-orders/"+id+"/bill", tok, nil, "If-Match", "*")
		return r, func() bool {
			return len(e.call(http.MethodGet, "/v1/purchase-orders/"+id, e.as(authz.Accountant), nil).Body["bills"].([]any)) == 0
		}
	},
	"sendTestEmail": func(e *env, tok string) (response, func() bool) {
		matrixProfileEmail(e)
		drainWork(e.t, e)
		r := e.call(http.MethodPost, "/v1/organisation/email/test-messages", tok, nil)
		return r, func() bool { return nextWork(e.t, e) == "" }
	},
	"getEmailSettings": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/organisation/email", tok, nil), nil
	},
	"listConnectorCatalogue": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/connector-catalogue", tok, nil), nil
	},
	"listInstallationConnectors": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/installation/connectors", tok, nil), nil
	},
	"getInstallationConnector": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/installation/connectors/01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f70", tok, nil), nil
	},
	"installInstallationConnector": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/installation/connectors", tok, map[string]any{"connector": "example-email"}), nil
	},
	"configureInstallationConnector": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPut, "/v1/installation/connectors/01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f70/configuration", tok,
			map[string]any{"configuration": map[string]any{}}, "If-Match", "*"), nil
	},
	"setInstallationConnectorSecret": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPut, "/v1/installation/connectors/01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f70/secrets/send_token", tok,
			map[string]any{"value": "matrix-value"}, "If-Match", "*"), nil
	},
	"stepInstallationConnector": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/installation/connectors/01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f70/steps", tok,
			map[string]any{"step": "pause"}, "If-Match", "*"), nil
	},
	"testInstallationConnector": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/installation/connectors/01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f70/test", tok, nil, "If-Match", "*"), nil
	},
	"listInstallationChanges": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/installation/changes", tok, nil), nil
	},
	"approveInstallationChange": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/installation/changes/01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f71/approve", tok,
			map[string]any{"envelope_hash": strings.Repeat("0", 64)}), nil
	},
	"rejectInstallationChange": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/installation/changes/01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f71/reject", tok,
			map[string]any{"envelope_hash": strings.Repeat("0", 64)}), nil
	},
	"listOrganisationConnectors": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/organisation/connectors", tok, nil), nil
	},
	"getOrganisationConnector": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/organisation/connectors/"+matrixOrgConnector(e), tok, nil), nil
	},
	"installOrganisationConnector": func(e *env, tok string) (response, func() bool) {
		return askConnector(e, tok, http.MethodPost, "/v1/organisation/connectors", map[string]any{"connector": "example-org"})
	},
	"configureOrganisationConnector": func(e *env, tok string) (response, func() bool) {
		return askConnector(e, tok, http.MethodPut, "/v1/organisation/connectors/"+matrixOrgConnector(e)+"/configuration",
			map[string]any{"configuration": map[string]any{"address": "smtp.example.com:587"}}, "If-Match", "*")
	},
	"setOrganisationConnectorSecret": func(e *env, tok string) (response, func() bool) {
		return askConnector(e, tok, http.MethodPut, "/v1/organisation/connectors/"+matrixOrgConnector(e)+"/secrets/key",
			map[string]any{"value": "matrix-value"}, "If-Match", "*")
	},
	"stepOrganisationConnector": func(e *env, tok string) (response, func() bool) {
		return askConnector(e, tok, http.MethodPost, "/v1/organisation/connectors/"+matrixOrgConnector(e)+"/steps",
			map[string]any{"step": "uninstall"}, "If-Match", "*")
	},
	"testOrganisationConnector": func(e *env, tok string) (response, func() bool) {
		matrixProfileEmail(e)
		return e.call(http.MethodPost, "/v1/organisation/connectors/"+matrixOrgConnector(e)+"/test", tok, nil, "If-Match", "*"), nil
	},
	"setEmailSender": func(e *env, tok string) (response, func() bool) {
		reminders := func() any {
			return e.call(http.MethodGet, "/v1/organisation/email", e.as(authz.Auditor), nil).Body["senders"].([]any)[2].(map[string]any)["etag"]
		}
		before := reminders()
		r := e.call(http.MethodPut, "/v1/organisation/email/senders/reminders", tok, map[string]any{"address": "reminders@mail.example.com"}, "If-Match", "*")
		return r, func() bool { return reminders() == before }
	},
	"listEmailSuppressions": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/organisation/email/suppressions", tok, nil), nil
	},
	"releaseEmailSuppression": func(e *env, tok string) (response, func() bool) {
		id := matrixSuppression(e)
		r := e.call(http.MethodPost, "/v1/organisation/email/suppressions/"+id+"/release", tok, nil)
		return r, func() bool {
			for _, x := range e.call(http.MethodGet, "/v1/organisation/email/suppressions", e.as(authz.Auditor), nil).Body["items"].([]any) {
				if x.(map[string]any)["id"] == id {
					return true
				}
			}
			return false
		}
	},
	"getCaptureAddresses": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/organisation/email/capture-addresses", tok, nil), nil
	},
	"setOrganisationCaptureAddress": func(e *env, tok string) (response, func() bool) {
		tag := func() any {
			return e.call(http.MethodGet, "/v1/organisation/email/capture-addresses", e.as(authz.Auditor), nil).Body["organisation_etag"]
		}
		before := tag()
		r := e.call(http.MethodPut, "/v1/organisation/email/capture-addresses/organisation", tok, map[string]any{"name": "m-" + uuid.NewString()[:8]}, "If-Match", "*")
		return r, func() bool { return tag() == before }
	},
	"setMyCaptureAddress": func(e *env, tok string) (response, func() bool) {
		// An address of one's own needs a person: the role's caller, as a member of the organisation.
		e.tokens.mu.Lock()
		p := e.tokens.m[tok]
		e.tokens.mu.Unlock()
		p.UserID = matrixMemberUser(e)
		person := e.singleFactor(p)
		mine := func() any {
			own := e.principal(authn.Principal{UserID: p.UserID, Client: "web", Class: authn.ClassUser, Organisation: e.books.Org, Roles: []authz.Role{authz.Employee}})
			return e.call(http.MethodGet, "/v1/organisation/email/capture-addresses", own, nil).Body["mine_etag"]
		}
		before := mine()
		r := e.call(http.MethodPut, "/v1/organisation/email/capture-addresses/mine", person, map[string]any{"name": "m-" + uuid.NewString()[:8]}, "If-Match", "*")
		return r, func() bool { return mine() == before }
	},
	"listInboundMessages": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/organisation/email/inbound-messages", tok, nil), nil
	},
	"listMyInboundMessages": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/organisation/email/inbound-messages/mine", tok, nil), nil
	},
	"decideCaptureSender": func(e *env, tok string) (response, func() bool) {
		// A decision shows only in what it does to mail that arrives; nothing is quarantined here to settle.
		return e.call(http.MethodPost, "/v1/organisation/email/capture-senders", tok, map[string]any{"sender": "m-" + uuid.NewString()[:8] + "@example.com",
			"decision": "accepted"}), nil
	},
	"decideMyCaptureSender": func(e *env, tok string) (response, func() bool) {
		e.tokens.mu.Lock()
		p := e.tokens.m[tok]
		e.tokens.mu.Unlock()
		p.UserID = matrixMemberUser(e)
		return e.call(http.MethodPost, "/v1/organisation/email/capture-senders/mine", e.singleFactor(p), map[string]any{"sender": "m-" + uuid.NewString()[:8] + "@example.com",
			"decision": "accepted"}), nil
	},
	"listNotifications": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/notifications", asMatrixMember(e, tok), nil), nil
	},
	"readNotification": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/notifications/"+matrixNotification(e)+"/read", asMatrixMember(e, tok), nil), nil
	},
	"unreadNotification": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/notifications/"+matrixNotification(e)+"/unread", asMatrixMember(e, tok), nil), nil
	},
	"archiveNotification": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/notifications/"+matrixNotification(e)+"/archive", asMatrixMember(e, tok), nil), nil
	},
	"readAllNotifications": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/notifications/read-all", asMatrixMember(e, tok), nil), nil
	},
	"getNotificationPreferences": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/notification-preferences", asMatrixMember(e, tok), nil), nil
	},
	"setNotificationPreference": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPut, "/v1/notification-preferences/approval.decided", asMatrixMember(e, tok), map[string]any{"email": "daily"}, "If-Match", "*"), nil
	},
	"setQuietHours": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPut, "/v1/notification-quiet-hours", asMatrixMember(e, tok), map[string]any{"from": "22:00", "to": "07:00"}, "If-Match", "*"), nil
	},
	"sendTestNotification": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/notifications/test-messages", asMatrixMember(e, tok), nil), nil
	},
	"listEmailMessages": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/organisation/email/messages", tok, nil), nil
	},
	"listBanners": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/banners", tok, nil), nil
	},
	"dismissBanner": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/banners/"+matrixBanner(e)+"/dismiss", asMatrixMember(e, tok), nil), nil
	},
	"listAnnouncements": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/announcements", tok, nil), nil
	},
	"publishAnnouncement": func(e *env, tok string) (response, func() bool) {
		before := matrixAnnouncements(e)
		return e.call(http.MethodPost, "/v1/announcements", tok, map[string]any{"message": "Matrix", "severity": "info",
				"ends_at": e.clock.Now().Add(time.Hour).UTC().Format(time.RFC3339)}),
			func() bool { return matrixAnnouncements(e) == before }
	},
	"withdrawAnnouncement": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/announcements/"+matrixBanner(e)+"/withdraw", tok, nil), nil
	},
	"listInstallationAnnouncements": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/installation/announcements", tok, nil), nil
	},
	"publishInstallationAnnouncement": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/installation/announcements", tok, map[string]any{"message": "Matrix", "severity": "info",
			"ends_at": e.clock.Now().Add(time.Hour).UTC().Format(time.RFC3339)}), nil
	},
	"withdrawInstallationAnnouncement": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/installation/announcements/01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f60/withdraw", tok, nil), nil
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
	"getAccounting": func(e *env, tok string) (response, func() bool) {
		p, _ := matrixAccounting(e, tok)
		return e.call(http.MethodGet, "/v1/organisation/accounting", e.principal(p), nil), nil
	},
	"recordAccountingFramework": func(e *env, tok string) (response, func() bool) {
		p, owner := matrixAccounting(e, tok)
		before := matrixFrameworkVersion(e, owner)
		r := e.call(http.MethodPost, "/v1/organisation/accounting/framework", e.principal(p),
			map[string]any{"framework": "sfrs_se", "edition": "Matrix", "from_on": "2026-10-01"})
		return r, func() bool { return matrixFrameworkVersion(e, owner) == before }
	},
	"confirmAccountingFramework": func(e *env, tok string) (response, func() bool) {
		p, owner := matrixAccounting(e, tok)
		v := e.call(http.MethodPost, "/v1/organisation/accounting/framework", owner,
			map[string]any{"framework": "sfrs_se", "edition": "Matrix", "from_on": "2026-10-01"}).Body["version"].(float64)
		r := e.call(http.MethodPost, fmt.Sprintf("/v1/organisation/accounting/framework/%d/confirm", int(v)), e.principal(p), nil)
		return r, func() bool {
			f, _ := e.call(http.MethodGet, "/v1/organisation/accounting", owner, nil).Body["framework"].(map[string]any)
			return f["confirmed_by"] == nil
		}
	},
	"recordAccountingPolicy": func(e *env, tok string) (response, func() bool) {
		p, owner := matrixAccounting(e, tok)
		before, _ := matrixPolicy(e, owner)
		r := e.call(http.MethodPost, "/v1/organisation/accounting/policies/leases", e.principal(p),
			map[string]any{"wording": "Matrix", "from_on": "2026-10-01"})
		return r, func() bool { v, _ := matrixPolicy(e, owner); return v == before }
	},
	"approveAccountingPolicy": func(e *env, tok string) (response, func() bool) {
		p, owner := matrixAccounting(e, tok)
		v := e.call(http.MethodPost, "/v1/organisation/accounting/policies/leases", owner,
			map[string]any{"wording": "Matrix", "from_on": "2026-10-01"}).Body["version"].(float64)
		r := e.call(http.MethodPost, fmt.Sprintf("/v1/organisation/accounting/policies/leases/%d/approve", int(v)), e.principal(p), nil)
		return r, func() bool { _, by := matrixPolicy(e, owner); return by == nil }
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
	// Changing posted opening balances asks for approval, so each call asks in an organisation of its own whose opening
	// balances are posted, as a person with a Fyniq user, stepped up now.
	"requestOpeningBalancesEdit": func(e *env, tok string) (response, func() bool) {
		ctx := context.Background()
		b, p := matrixOpening(e, tok, true)
		if _, err := e.api.journals.PostOpening(storage.WithAudit(ctx, storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "matrix-poster"}}), b.Org); err != nil {
			panic(err)
		}
		if p.Class == authn.ClassUser {
			p.UserID, p.StepUpAt = uuid.MustParse(matrixApprover(e)), e.clock.Now()
		}
		line := func(account uuid.UUID, amount string) map[string]any {
			return map[string]any{"account_id": account.String(), "amount": map[string]any{"amount": amount, "currency": "SGD"}}
		}
		r := e.call(http.MethodPost, "/v1/opening-balances/edit-approval", e.principal(p), map[string]any{
			"lines": []any{line(b.Bank.ID, "20.00"), line(b.Sales.ID, "-20.00")}, "reason": "Matrix"}, "If-Match", "*")
		return r, func() bool {
			var n int
			_ = e.store.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
				rs, err := u.Approvals().List(ctx, "")
				n = len(rs)
				return err
			})
			return n == 0
		}
	},
	"getCloseChecklistTemplate": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/close-checklist-template", tok, nil), nil
	},
	"getPeriodChecklist": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/periods/"+e.books.Nov.ID.String()+"/checklist", tok, nil), nil
	},
	// Each change to the template and each mark is made in an organisation of its own, so a refused call is seen to change
	// nothing.
	"replaceCloseChecklistTemplate": func(e *env, tok string) (response, func() bool) {
		b, p := matrixChecklistOrg(e, tok)
		r := e.call(http.MethodPut, "/v1/close-checklist-template", e.principal(p), map[string]any{"tasks": []any{map[string]any{"title": "Matrix task", "due_days": 3}}},
			"If-Match", "*")
		return r, func() bool {
			var none bool
			_ = e.store.Do(context.Background(), b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
				_, err := u.Closes().ChecklistTemplate(ctx)
				none = errors.Is(err, ports.ErrNotFound)
				return nil
			})
			return none
		}
	},
	"markChecklistTask": func(e *env, tok string) (response, func() bool) {
		b, p := matrixChecklistOrg(e, tok)
		r := e.call(http.MethodPost, "/v1/periods/"+b.Oct.ID.String()+"/checklist/documents", e.principal(p), map[string]any{"state": "done"})
		return r, func() bool {
			var n int
			_ = e.store.Do(context.Background(), b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
				es, err := u.Closes().ChecklistEntries(ctx, b.Oct.ID)
				n = len(es)
				return err
			})
			return n == 0
		}
	},
	// Exchange rates (rev 0.70): each write uses a day of its own, so a refused call is seen to have entered nothing.
	"listFxRates": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/fx/rates", tok, nil), nil
	},
	"getFxRate": func(e *env, tok string) (response, func() bool) {
		r := matrixRate(e, matrixDay())
		return e.call(http.MethodGet, "/v1/fx/rates/"+r.ID.String(), tok, nil), nil
	},
	"listFeedRates": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/fx/feed?from=2026-10-01&to=2026-10-31", tok, nil), nil
	},
	"lookupFxRate": func(e *env, tok string) (response, func() bool) {
		on := matrixDay()
		matrixRate(e, on)
		return e.call(http.MethodGet, "/v1/fx/rate?currency=USD&date="+on.Format(time.DateOnly), tok, nil), nil
	},
	"createFxRate": func(e *env, tok string) (response, func() bool) {
		on := matrixDay()
		return e.call(http.MethodPost, "/v1/fx/rates", tok, map[string]any{"kind": "day", "currency": "USD", "on": on.Format(time.DateOnly), "quote": "1.3"}),
			func() bool { return len(matrixRates(e, on)) == 0 }
	},
	"bulkCreateFxRates": func(e *env, tok string) (response, func() bool) {
		on := matrixDay()
		return e.call(http.MethodPost, "/v1/fx/rates:bulk", tok, map[string]any{"items": []map[string]any{
				{"kind": "day", "currency": "USD", "on": on.Format(time.DateOnly), "quote": "1.3"}}}),
			func() bool { return len(matrixRates(e, on)) == 0 }
	},
	"withdrawFxRate": func(e *env, tok string) (response, func() bool) {
		on := matrixDay()
		r := matrixRate(e, on)
		return e.call(http.MethodPost, "/v1/fx/rates/"+r.ID.String()+"/withdraw", tok, map[string]any{"reason": "Matrix"}, "If-Match", `"1"`),
			func() bool { return len(matrixRates(e, on)) == 1 }
	},
	// The revaluation (rev 0.73): each run is of a September of its own, ended by the matrix's clock, with no foreign-currency
	// balance, so it records the revaluation and posts nothing; a refused call leaves no revaluation document.
	"listForeignMoneyAccounts": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/fx/money-accounts", tok, nil), nil
	},
	"getFxRevaluation": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/periods/"+e.books.Oct.ID.String()+"/fx-revaluation", tok, nil), nil
	},
	"postFxRevaluation": func(e *env, tok string) (response, func() bool) {
		b := porttest.NewBooks(e.t, e.store, "Matrix revaluation "+uuid.NewString()[:8])
		var sep storage.Period
		if err := e.store.Do(context.Background(), b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			sep, err = u.Periods().Create(ctx, storage.NewPeriod{StartsOn: porttest.Day(9, 1), EndsOn: porttest.Day(9, 30)})
			return err
		}); err != nil {
			panic(err)
		}
		e.tokens.mu.Lock()
		p := e.tokens.m[tok]
		e.tokens.mu.Unlock()
		if p.Organisation != uuid.Nil {
			p.Organisation = b.Org
		}
		runs := func() int {
			var n int
			_ = e.store.Do(context.Background(), b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
				docs, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: "fx_revaluation", Limit: 10})
				n = len(docs)
				return err
			})
			return n
		}
		return e.call(http.MethodPost, "/v1/periods/"+sep.ID.String()+"/fx-revaluation", e.principal(p), nil, "If-Match", "*"),
			func() bool { return runs() == 0 }
	},
	// Year end needs the financial years, so each call reads it in an organisation of its own whose books start on 1 October
	// 2026.
	"getYearEnd": func(e *env, tok string) (response, func() bool) {
		b, p := matrixOpening(e, tok, false)
		return e.call(http.MethodGet, "/v1/periods/"+b.Oct.ID.String()+"/year-end", e.principal(p), nil), nil
	},
	"getPeriodClose": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/periods/"+e.books.Oct.ID.String()+"/close", tok, nil), nil
	},
	"runCloseChecks": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/periods/"+e.books.Nov.ID.String()+"/close-checks", tok, nil, "If-Match", "*"), nil
	},
	"listPeriodVariances": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/periods/"+e.books.Nov.ID.String()+"/variances", tok, nil), nil
	},
	// Each call explains November's sales in an organisation of its own, whose totals are known: S$5,000 in October and
	// S$8,000 in November, income so credit.
	"explainVariance": func(e *env, tok string) (response, func() bool) {
		ctx := context.Background()
		b := porttest.NewBooks(e.t, e.store, "Matrix flux "+uuid.NewString()[:8])
		if err := e.store.Do(storage.WithAudit(ctx, storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "matrix-fixture"}}), b.Org,
			func(ctx context.Context, u storage.UnitOfWork) error {
				if _, err := u.Ledger().Append(ctx, porttest.Sale(uuid.NewString(), porttest.Day(10, 5), b, "5000.00")); err != nil {
					return err
				}
				_, err := u.Ledger().Append(ctx, porttest.Sale(uuid.NewString(), porttest.Day(11, 5), b, "8000.00"))
				return err
			}); err != nil {
			panic(err)
		}
		e.tokens.mu.Lock()
		p := e.tokens.m[tok]
		e.tokens.mu.Unlock()
		if p.Organisation != uuid.Nil {
			p.Organisation = b.Org
		}
		r := e.call(http.MethodPost, "/v1/periods/"+b.Nov.ID.String()+"/variances/"+b.Sales.ID.String()+"/explanation", e.principal(p),
			map[string]any{"current": map[string]any{"amount": "-8000.00", "currency": "SGD"}, "previous": map[string]any{"amount": "-5000.00", "currency": "SGD"},
				"explanation": "A new customer started in November."})
		return r, func() bool {
			var n int
			_ = e.store.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
				xs, err := u.Closes().VarianceExplanations(ctx, b.Nov.ID)
				n = len(xs)
				return err
			})
			return n == 0
		}
	},
	// A close cannot be undone here, so each call closes October of an organisation of its own, on checks another person
	// ran, as a person stepped up now.
	"closePeriod": func(e *env, tok string) (response, func() bool) {
		ctx := context.Background()
		b := porttest.NewBooks(e.t, e.store, "Matrix close "+uuid.NewString()[:8])
		checklistDone(e.t, e.store, b.Org, b.Oct.ID)
		c, err := e.api.closing.RunChecks(storage.WithAudit(ctx, storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "matrix-preparer"}}), b.Org, b.Oct.ID)
		if err != nil {
			panic(err)
		}
		e.tokens.mu.Lock()
		p := e.tokens.m[tok]
		e.tokens.mu.Unlock()
		if p.Organisation != uuid.Nil {
			p.Organisation = b.Org
		}
		if p.Class == authn.ClassUser {
			p.UserID, p.StepUpAt = uuid.New(), e.clock.Now()
		}
		r := e.call(http.MethodPost, "/v1/periods/"+b.Oct.ID.String()+"/close", e.principal(p), map[string]any{"checks_id": c.ID.String(),
			"statement": "Reviewed."}, "If-Match", "*")
		return r, func() bool {
			var open bool
			_ = e.store.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
				x, err := u.Periods().Get(ctx, b.Oct.ID)
				open = x.Status == storage.PeriodOpen
				return err
			})
			return open
		}
	},
	// Reopening needs a closed period, and closing one here would stop the other rows posting, so each call asks in an
	// organisation of its own whose October is closed, as a person with a Fyniq user, stepped up now.
	"requestPeriodReopen": func(e *env, tok string) (response, func() bool) {
		ctx := context.Background()
		b := porttest.NewBooks(e.t, e.store, "Matrix reopen "+uuid.NewString()[:8])
		as := func(who string) context.Context {
			return storage.WithAudit(ctx, storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: who}})
		}
		checklistDone(e.t, e.store, b.Org, b.Oct.ID)
		c, err := e.api.closing.RunChecks(as("matrix-preparer"), b.Org, b.Oct.ID)
		if err == nil {
			_, _, err = e.api.closing.Close(as("matrix-attester"), b.Org, b.Oct.ID, c.ID, "Reviewed.")
		}
		if err != nil {
			panic(err)
		}
		e.tokens.mu.Lock()
		p := e.tokens.m[tok]
		e.tokens.mu.Unlock()
		if p.Organisation != uuid.Nil {
			p.Organisation = b.Org
		}
		if p.Class == authn.ClassUser {
			p.UserID, p.StepUpAt = uuid.MustParse(matrixApprover(e)), e.clock.Now()
		}
		r := e.call(http.MethodPost, "/v1/periods/"+b.Oct.ID.String()+"/reopen", e.principal(p), map[string]any{"reason": "Matrix"}, "If-Match", "*")
		return r, func() bool {
			var n int
			_ = e.store.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
				rs, err := u.Approvals().List(ctx, "")
				n = len(rs)
				return err
			})
			return n == 0
		}
	},
	"getGeneralLedger": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/reports/general-ledger?account_id="+e.books.Bank.ID.String()+"&from=2026-10-01&to=2026-11-30", tok, nil), nil
	},
	"getSchedules": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/reports/schedules?as_at=2026-10-31", tok, nil), nil
	},
	"getFinancialStatement": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/reports/statements/pl?from=2026-10&to=2026-11", tok, nil), nil
	},
	"listJournals": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/journals", tok, nil), nil
	},
	"listWaitingReversals": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/journals/waiting-reversals", tok, nil), nil
	},
	"listRecurringJournals": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/recurring-journals", tok, nil), nil
	},
	"getRecurringJournal": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/recurring-journals/"+matrixRecurring(e), tok, nil), nil
	},
	"createRecurringJournal": func(e *env, tok string) (response, func() bool) {
		name := "Matrix " + uuid.NewString()[:8]
		r := e.call(http.MethodPost, "/v1/recurring-journals", tok, e.recurring(name, "10.00", monthly(1, "2026-11-01")))
		return r, func() bool {
			l := e.call(http.MethodGet, "/v1/recurring-journals?page_size=500", e.as(authz.Accountant), nil)
			return !strings.Contains(l.Raw, `"name":"`+name+`"`)
		}
	},
	"replaceRecurringJournal": func(e *env, tok string) (response, func() bool) {
		id, name := matrixRecurring(e), "Matrix "+uuid.NewString()[:8]
		r := e.call(http.MethodPut, "/v1/recurring-journals/"+id, tok, e.recurring(name, "10.00", monthly(1, "2026-11-01")), "If-Match", "*")
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/recurring-journals/"+id, e.as(authz.Accountant), nil).str("name") != name
		}
	},
	"pauseRecurringJournal": func(e *env, tok string) (response, func() bool) {
		id := matrixRecurring(e)
		r := e.call(http.MethodPost, "/v1/recurring-journals/"+id+"/pause", tok, nil, "If-Match", "*")
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/recurring-journals/"+id, e.as(authz.Accountant), nil).str("status") == "active"
		}
	},
	"resumeRecurringJournal": func(e *env, tok string) (response, func() bool) {
		id := matrixRecurring(e)
		e.call(http.MethodPost, "/v1/recurring-journals/"+id+"/pause", e.as(authz.Accountant), nil, "If-Match", "*")
		r := e.call(http.MethodPost, "/v1/recurring-journals/"+id+"/resume", tok, nil, "If-Match", "*")
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/recurring-journals/"+id, e.as(authz.Accountant), nil).str("status") == "paused"
		}
	},
	"listRecurringBills": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/recurring-bills", tok, nil), nil
	},
	"getRecurringBill": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/recurring-bills/"+matrixRecurringBill(e), tok, nil), nil
	},
	"createRecurringBill": func(e *env, tok string) (response, func() bool) {
		name := "Matrix " + uuid.NewString()[:8]
		r := e.call(http.MethodPost, "/v1/recurring-bills", tok, e.recurringBill(name, "10.00", monthly(1, "2026-11-01")))
		return r, func() bool {
			l := e.call(http.MethodGet, "/v1/recurring-bills?page_size=500", e.as(authz.Accountant), nil)
			return !strings.Contains(l.Raw, `"name":"`+name+`"`)
		}
	},
	"replaceRecurringBill": func(e *env, tok string) (response, func() bool) {
		id, name := matrixRecurringBill(e), "Matrix "+uuid.NewString()[:8]
		r := e.call(http.MethodPut, "/v1/recurring-bills/"+id, tok, e.recurringBill(name, "10.00", monthly(1, "2026-11-01")), "If-Match", "*")
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/recurring-bills/"+id, e.as(authz.Accountant), nil).str("name") != name
		}
	},
	"pauseRecurringBill": func(e *env, tok string) (response, func() bool) {
		id := matrixRecurringBill(e)
		r := e.call(http.MethodPost, "/v1/recurring-bills/"+id+"/pause", tok, nil, "If-Match", "*")
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/recurring-bills/"+id, e.as(authz.Accountant), nil).str("status") == "active"
		}
	},
	"resumeRecurringBill": func(e *env, tok string) (response, func() bool) {
		id := matrixRecurringBill(e)
		e.call(http.MethodPost, "/v1/recurring-bills/"+id+"/pause", e.as(authz.Accountant), nil, "If-Match", "*")
		r := e.call(http.MethodPost, "/v1/recurring-bills/"+id+"/resume", tok, nil, "If-Match", "*")
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/recurring-bills/"+id, e.as(authz.Accountant), nil).str("status") == "paused"
		}
	},
	"listRecurringInvoices": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/recurring-invoices", tok, nil), nil
	},
	"getRecurringInvoice": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/recurring-invoices/"+matrixRecurringInvoice(e), tok, nil), nil
	},
	"createRecurringInvoice": func(e *env, tok string) (response, func() bool) {
		name := "Matrix " + uuid.NewString()[:8]
		r := e.call(http.MethodPost, "/v1/recurring-invoices", tok, e.recurringInvoice(name, "10.00", monthly(1, "2026-11-01")))
		return r, func() bool {
			l := e.call(http.MethodGet, "/v1/recurring-invoices?page_size=500", e.as(authz.Accountant), nil)
			return !strings.Contains(l.Raw, `"name":"`+name+`"`)
		}
	},
	"replaceRecurringInvoice": func(e *env, tok string) (response, func() bool) {
		id, name := matrixRecurringInvoice(e), "Matrix "+uuid.NewString()[:8]
		r := e.call(http.MethodPut, "/v1/recurring-invoices/"+id, tok, e.recurringInvoice(name, "10.00", monthly(1, "2026-11-01")), "If-Match", "*")
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/recurring-invoices/"+id, e.as(authz.Accountant), nil).str("name") != name
		}
	},
	"pauseRecurringInvoice": func(e *env, tok string) (response, func() bool) {
		id := matrixRecurringInvoice(e)
		r := e.call(http.MethodPost, "/v1/recurring-invoices/"+id+"/pause", tok, nil, "If-Match", "*")
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/recurring-invoices/"+id, e.as(authz.Accountant), nil).str("status") == "active"
		}
	},
	"resumeRecurringInvoice": func(e *env, tok string) (response, func() bool) {
		id := matrixRecurringInvoice(e)
		e.call(http.MethodPost, "/v1/recurring-invoices/"+id+"/pause", e.as(authz.Accountant), nil, "If-Match", "*")
		r := e.call(http.MethodPost, "/v1/recurring-invoices/"+id+"/resume", tok, nil, "If-Match", "*")
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/recurring-invoices/"+id, e.as(authz.Accountant), nil).str("status") == "paused"
		}
	},
	"requestRecurringInvoiceApproval": func(e *env, tok string) (response, func() bool) {
		was := requestsOf(e, "recurring_invoice")
		body := e.recurringInvoice("Matrix "+uuid.NewString()[:8], "10.00", monthly(1, "2026-11-01"))
		body["mode"] = "issue_and_send"
		c := e.call(http.MethodPost, "/v1/recurring-invoices", e.as(authz.Accountant), body)
		person := steppedUp(e, tok)
		r := e.call(http.MethodPost, "/v1/recurring-invoices/"+c.str("id")+"/approval", person, nil, "If-Match", c.Header.Get("ETag"))
		if r.Status == http.StatusAccepted {
			e.call(http.MethodPost, "/v1/approvals/"+r.str("id")+"/withdraw", person, nil, "If-Match", `"1"`)
		}
		return r, func() bool { return requestsOf(e, "recurring_invoice") == was }
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
	// Asking for approval to post or to void a journal: an allowed role reaches the request, which the row withdraws.
	"requestJournalPost": func(e *env, tok string) (response, func() bool) {
		was := requestsOf(e, "journal")
		id, tag := e.createDraft("2500.00")
		person := steppedUp(e, tok)
		r := e.call(http.MethodPost, "/v1/journals/"+id+"/post-approval", person, nil, "If-Match", tag)
		if r.Status == http.StatusAccepted {
			e.call(http.MethodPost, "/v1/approvals/"+r.str("id")+"/withdraw", person, nil, "If-Match", `"1"`)
		}
		return r, func() bool { return requestsOf(e, "journal") == was }
	},
	"requestJournalReversal": func(e *env, tok string) (response, func() bool) {
		was := requestsOf(e, "journal")
		id, tag := e.postedJournal("10.00")
		person := steppedUp(e, tok)
		r := e.call(http.MethodPost, "/v1/journals/"+id+"/reverse-approval", person, map[string]any{"date": "2026-10-28", "reason": "Matrix test"}, "If-Match", tag)
		if r.Status == http.StatusAccepted {
			e.call(http.MethodPost, "/v1/approvals/"+r.str("id")+"/withdraw", person, nil, "If-Match", `"1"`)
		}
		return r, func() bool { return requestsOf(e, "journal") == was }
	},
	// An edit in Founder mode is asked for (requestJournalEdit), so the row edits in an organisation in Standard mode.
	"editJournal": func(e *env, tok string) (response, func() bool) {
		s := inStandardMode(e)
		e.tokens.mu.Lock()
		p := e.tokens.m[tok]
		e.tokens.mu.Unlock()
		if p.Organisation == e.books.Org {
			p.Organisation = s.books.Org
			tok = s.principal(p)
		}
		id, tag := s.postedJournal("10.00")
		r := s.call(http.MethodPost, "/v1/journals/"+id+"/edit", tok, s.edit("12.00", "2026-10-21", "Matrix test"), "If-Match", tag)
		return r, func() bool {
			return s.call(http.MethodGet, "/v1/journals/"+id, s.as(authz.Accountant), nil).Header.Get("ETag") == tag
		}
	},
	"requestJournalEdit": func(e *env, tok string) (response, func() bool) {
		was := requestsOf(e, "journal")
		id, tag := e.postedJournal("10.00")
		person := steppedUp(e, tok)
		r := e.call(http.MethodPost, "/v1/journals/"+id+"/edit-approval", person, e.edit("12.00", "2026-10-21", "Matrix test"), "If-Match", tag)
		if r.Status == http.StatusAccepted {
			e.call(http.MethodPost, "/v1/approvals/"+r.str("id")+"/withdraw", person, nil, "If-Match", `"1"`)
		}
		return r, func() bool { return requestsOf(e, "journal") == was }
	},
	// Bills (FR-AP-02): read under V, prepared under C, posted and voided under P; asking for approval is posting.
	"getPurchaseGSTCodes": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/purchases/gst-codes?on=2026-10-14", tok, nil), nil
	},
	"getSalesGSTCodes": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/sales/gst-codes?on=2026-10-14", tok, nil), nil
	},
	"listBills": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/bills", tok, nil), nil
	},
	"getBill": func(e *env, tok string) (response, func() bool) {
		id, _ := e.createBill("10.00", "0.90")
		return e.call(http.MethodGet, "/v1/bills/"+id, tok, nil), nil
	},
	"createBill": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/bills", tok, e.billDraft("10.00", "0.90")), nil
	},
	"replaceBill": func(e *env, tok string) (response, func() bool) {
		id, _ := e.createBill("10.00", "0.90")
		r := e.call(http.MethodPut, "/v1/bills/"+id, tok, e.billDraft("11.00", "0.99"), "If-Match", "*")
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/bills/"+id, e.as(authz.Accountant), nil).Body["version"] == float64(1)
		}
	},
	"postBill": func(e *env, tok string) (response, func() bool) {
		id, tag := e.createBill("10.00", "0.90")
		r := e.call(http.MethodPost, "/v1/bills/"+id+"/post", tok, nil, "If-Match", tag)
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/bills/"+id, e.as(authz.Accountant), nil).str("status") == "draft"
		}
	},
	"requestBillPost": func(e *env, tok string) (response, func() bool) {
		was := requestsOf(e, "bill")
		id, tag := e.createBill("2500.00", "225.00")
		person := steppedUp(e, tok)
		r := e.call(http.MethodPost, "/v1/bills/"+id+"/post-approval", person, nil, "If-Match", tag)
		if r.Status == http.StatusAccepted {
			e.call(http.MethodPost, "/v1/approvals/"+r.str("id")+"/withdraw", person, nil, "If-Match", `"1"`)
		}
		return r, func() bool { return requestsOf(e, "bill") == was }
	},
	"requestBillVoid": func(e *env, tok string) (response, func() bool) {
		was := requestsOf(e, "bill")
		id, tag := e.postedBill("10.00", "0.90")
		person := steppedUp(e, tok)
		r := e.call(http.MethodPost, "/v1/bills/"+id+"/void-approval", person, map[string]any{"date": "2026-10-28", "reason": "Matrix test"}, "If-Match", tag)
		if r.Status == http.StatusAccepted {
			e.call(http.MethodPost, "/v1/approvals/"+r.str("id")+"/withdraw", person, nil, "If-Match", `"1"`)
		}
		return r, func() bool { return requestsOf(e, "bill") == was }
	},
	"requestBillEdit": func(e *env, tok string) (response, func() bool) {
		was := requestsOf(e, "bill")
		id, tag := e.postedBill("10.00", "0.90")
		person := steppedUp(e, tok)
		r := e.call(http.MethodPost, "/v1/bills/"+id+"/edit-approval", person, e.billEdit("12.00", "1.08", "Matrix test"), "If-Match", tag)
		if r.Status == http.StatusAccepted {
			e.call(http.MethodPost, "/v1/approvals/"+r.str("id")+"/withdraw", person, nil, "If-Match", `"1"`)
		}
		return r, func() bool { return requestsOf(e, "bill") == was }
	},
	// An edit in Founder mode is asked for (requestBillEdit), so the row edits in an organisation in Standard mode.
	"editBill": func(e *env, tok string) (response, func() bool) {
		s := inStandardMode(e)
		e.tokens.mu.Lock()
		p := e.tokens.m[tok]
		e.tokens.mu.Unlock()
		if p.Organisation == e.books.Org {
			p.Organisation = s.books.Org
			tok = s.principal(p)
		}
		id, tag := s.postedBill("10.00", "0.90")
		r := s.call(http.MethodPost, "/v1/bills/"+id+"/edit", tok, s.billEdit("12.00", "1.08", "Matrix test"), "If-Match", tag)
		return r, func() bool {
			return s.call(http.MethodGet, "/v1/bills/"+id, s.as(authz.Accountant), nil).Header.Get("ETag") == tag
		}
	},
	// A void in Founder mode is asked for (requestBillVoid), so the row voids in an organisation in Standard mode.
	"voidBill": func(e *env, tok string) (response, func() bool) {
		s := inStandardMode(e)
		e.tokens.mu.Lock()
		p := e.tokens.m[tok]
		e.tokens.mu.Unlock()
		if p.Organisation == e.books.Org {
			p.Organisation = s.books.Org
			tok = s.principal(p)
		}
		id, tag := s.postedBill("10.00", "0.90")
		r := s.call(http.MethodPost, "/v1/bills/"+id+"/void", tok, map[string]any{"date": "2026-10-28", "reason": "Matrix test"}, "If-Match", tag)
		return r, func() bool {
			return s.call(http.MethodGet, "/v1/bills/"+id, s.as(authz.Accountant), nil).str("status") == "posted"
		}
	},
	// Invoices (FR-AR-03): read under V, prepared under C, issued, voided and edited under P; asking for approval is issuing.
	// Customer receipts (FR-AR-18): read under V, recorded and voided under P; asking to void is posting.
	"listCustomerReceipts": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/customer-receipts", tok, nil), nil
	},
	"getCustomerReceipt": func(e *env, tok string) (response, func() bool) {
		id, _ := e.recordedReceipt()
		return e.call(http.MethodGet, "/v1/customer-receipts/"+id, tok, nil), nil
	},
	"recordCustomerReceipt": func(e *env, tok string) (response, func() bool) {
		inv, _ := e.issuedInvoice("1", "10.00")
		r := e.call(http.MethodPost, "/v1/customer-receipts", tok, e.receiptOf(inv, "10.00"))
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/invoices/"+inv, e.as(authz.Accountant), nil).str("states", "settlement") == "open"
		}
	},
	"requestCustomerReceiptVoid": func(e *env, tok string) (response, func() bool) {
		was := requestsOf(e, "customer_receipt")
		id, tag := e.recordedReceipt()
		person := steppedUp(e, tok)
		r := e.call(http.MethodPost, "/v1/customer-receipts/"+id+"/void-approval", person, map[string]any{"date": "2026-10-28", "reason": "Matrix test"}, "If-Match", tag)
		if r.Status == http.StatusAccepted {
			e.call(http.MethodPost, "/v1/approvals/"+r.str("id")+"/withdraw", person, nil, "If-Match", `"1"`)
		}
		return r, func() bool { return requestsOf(e, "customer_receipt") == was }
	},
	// A void in Founder mode is asked for, so the row voids in an organisation in Standard mode.
	"voidCustomerReceipt": func(e *env, tok string) (response, func() bool) {
		s := inStandardMode(e)
		e.tokens.mu.Lock()
		p := e.tokens.m[tok]
		e.tokens.mu.Unlock()
		if p.Organisation == e.books.Org {
			p.Organisation = s.books.Org
			tok = s.principal(p)
		}
		id, tag := s.recordedReceipt()
		r := s.call(http.MethodPost, "/v1/customer-receipts/"+id+"/void", tok, map[string]any{"date": "2026-10-28", "reason": "Matrix test"}, "If-Match", tag)
		return r, func() bool {
			return s.call(http.MethodGet, "/v1/customer-receipts/"+id, s.as(authz.Accountant), nil).str("status") == "posted"
		}
	},
	// Write-offs (FR-AR-17): read under V, drafted under C, asked for and voided under P; posting is the approval's.
	"listWriteOffs": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/write-offs", tok, nil), nil
	},
	"getWriteOff": func(e *env, tok string) (response, func() bool) {
		id, _ := e.draftWriteOff()
		return e.call(http.MethodGet, "/v1/write-offs/"+id, tok, nil), nil
	},
	"createWriteOff": func(e *env, tok string) (response, func() bool) {
		inv, _ := e.issuedInvoice("1", "10.00")
		return e.call(http.MethodPost, "/v1/write-offs", tok, e.writeOffDraft(inv, "10.00")), nil
	},
	"replaceWriteOff": func(e *env, tok string) (response, func() bool) {
		id, _ := e.draftWriteOff()
		g := e.call(http.MethodGet, "/v1/write-offs/"+id, e.as(authz.Accountant), nil)
		r := e.call(http.MethodPut, "/v1/write-offs/"+id, tok, e.writeOffDraft(g.str("invoice", "id"), "5.00"), "If-Match", "*")
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/write-offs/"+id, e.as(authz.Accountant), nil).Body["version"] == float64(1)
		}
	},
	"requestWriteOffPost": func(e *env, tok string) (response, func() bool) {
		was := requestsOf(e, "write_off")
		id, tag := e.draftWriteOff()
		person := steppedUp(e, tok)
		r := e.call(http.MethodPost, "/v1/write-offs/"+id+"/post-approval", person, nil, "If-Match", tag)
		if r.Status == http.StatusAccepted {
			e.call(http.MethodPost, "/v1/approvals/"+r.str("id")+"/withdraw", person, nil, "If-Match", `"1"`)
		}
		return r, func() bool { return requestsOf(e, "write_off") == was }
	},
	"requestWriteOffVoid": func(e *env, tok string) (response, func() bool) {
		was := requestsOf(e, "write_off")
		id, tag := e.postedWriteOff()
		person := steppedUp(e, tok)
		r := e.call(http.MethodPost, "/v1/write-offs/"+id+"/void-approval", person, map[string]any{"date": "2026-10-31", "reason": "Matrix test"}, "If-Match", tag)
		if r.Status == http.StatusAccepted {
			e.call(http.MethodPost, "/v1/approvals/"+r.str("id")+"/withdraw", person, nil, "If-Match", `"1"`)
		}
		return r, func() bool { return requestsOf(e, "write_off") == was }
	},
	// A void in Founder mode is asked for, so the row voids in an organisation in Standard mode.
	"voidWriteOff": func(e *env, tok string) (response, func() bool) {
		s := inStandardMode(e)
		e.tokens.mu.Lock()
		p := e.tokens.m[tok]
		e.tokens.mu.Unlock()
		if p.Organisation == e.books.Org {
			p.Organisation = s.books.Org
			tok = s.principal(p)
		}
		id, tag := s.postedWriteOff()
		r := s.call(http.MethodPost, "/v1/write-offs/"+id+"/void", tok, map[string]any{"date": "2026-10-31", "reason": "Matrix test"}, "If-Match", tag)
		return r, func() bool {
			return s.call(http.MethodGet, "/v1/write-offs/"+id, s.as(authz.Accountant), nil).str("status") == "posted"
		}
	},
	// Customer credits applied to invoices (FR-AR-04): read under V, applied and voided under P; asking to void is posting.
	"getCustomerCreditApplication": func(e *env, tok string) (response, func() bool) {
		id, _ := e.appliedCustomerCredit()
		return e.call(http.MethodGet, "/v1/customer-credit-applications/"+id, tok, nil), nil
	},
	"applyCustomerCredit": func(e *env, tok string) (response, func() bool) {
		inv, _ := e.issuedInvoice("1", "10.00")
		total := e.call(http.MethodGet, "/v1/invoices/"+inv, e.as(authz.Accountant), nil).str("total", "amount")
		r := e.call(http.MethodPost, "/v1/customer-credit-applications", tok, applyingCredit(e.issuedCreditNote("1", "10.00"), inv, total))
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/invoices/"+inv, e.as(authz.Accountant), nil).str("states", "settlement") == "open"
		}
	},
	"requestCustomerCreditApplicationVoid": func(e *env, tok string) (response, func() bool) {
		was := requestsOf(e, "customer_credit_application")
		id, tag := e.appliedCustomerCredit()
		person := steppedUp(e, tok)
		r := e.call(http.MethodPost, "/v1/customer-credit-applications/"+id+"/void-approval", person, map[string]any{"date": "2026-10-28", "reason": "Matrix test"},
			"If-Match", tag)
		if r.Status == http.StatusAccepted {
			e.call(http.MethodPost, "/v1/approvals/"+r.str("id")+"/withdraw", person, nil, "If-Match", `"1"`)
		}
		return r, func() bool { return requestsOf(e, "customer_credit_application") == was }
	},
	// A void in Founder mode is asked for, so the row voids in an organisation in Standard mode.
	"voidCustomerCreditApplication": func(e *env, tok string) (response, func() bool) {
		s := inStandardMode(e)
		e.tokens.mu.Lock()
		p := e.tokens.m[tok]
		e.tokens.mu.Unlock()
		if p.Organisation == e.books.Org {
			p.Organisation = s.books.Org
			tok = s.principal(p)
		}
		id, tag := s.appliedCustomerCredit()
		r := s.call(http.MethodPost, "/v1/customer-credit-applications/"+id+"/void", tok, map[string]any{"date": "2026-10-28", "reason": "Matrix test"}, "If-Match", tag)
		return r, func() bool {
			return s.call(http.MethodGet, "/v1/customer-credit-applications/"+id, s.as(authz.Accountant), nil).str("status") == "posted"
		}
	},
	// Refunds to customers (FR-AR-04): read under V, recorded and voided under P; asking to void is posting.
	"getCustomerRefund": func(e *env, tok string) (response, func() bool) {
		id, _ := e.refundedCustomerCredit()
		return e.call(http.MethodGet, "/v1/customer-refunds/"+id, tok, nil), nil
	},
	"recordCustomerRefund": func(e *env, tok string) (response, func() bool) {
		cn := e.issuedCreditNote("1", "10.00")
		total := e.call(http.MethodGet, "/v1/invoices/"+cn, e.as(authz.Accountant), nil).str("total", "amount")
		r := e.call(http.MethodPost, "/v1/customer-refunds", tok, e.refundingCredit(cn, total))
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/invoices/"+cn, e.as(authz.Accountant), nil).str("states", "settlement") == "open"
		}
	},
	"requestCustomerRefundVoid": func(e *env, tok string) (response, func() bool) {
		was := requestsOf(e, "customer_refund")
		id, tag := e.refundedCustomerCredit()
		person := steppedUp(e, tok)
		r := e.call(http.MethodPost, "/v1/customer-refunds/"+id+"/void-approval", person, map[string]any{"date": "2026-10-28", "reason": "Matrix test"}, "If-Match", tag)
		if r.Status == http.StatusAccepted {
			e.call(http.MethodPost, "/v1/approvals/"+r.str("id")+"/withdraw", person, nil, "If-Match", `"1"`)
		}
		return r, func() bool { return requestsOf(e, "customer_refund") == was }
	},
	// A void in Founder mode is asked for, so the row voids in an organisation in Standard mode.
	"voidCustomerRefund": func(e *env, tok string) (response, func() bool) {
		s := inStandardMode(e)
		e.tokens.mu.Lock()
		p := e.tokens.m[tok]
		e.tokens.mu.Unlock()
		if p.Organisation == e.books.Org {
			p.Organisation = s.books.Org
			tok = s.principal(p)
		}
		id, tag := s.refundedCustomerCredit()
		r := s.call(http.MethodPost, "/v1/customer-refunds/"+id+"/void", tok, map[string]any{"date": "2026-10-28", "reason": "Matrix test"}, "If-Match", tag)
		return r, func() bool {
			return s.call(http.MethodGet, "/v1/customer-refunds/"+id, s.as(authz.Accountant), nil).str("status") == "posted"
		}
	},
	// Customer deposits (FR-AR-09): read under V, recorded and voided under P; asking to void is posting.
	"listCustomerDeposits": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/customer-deposits", tok, nil), nil
	},
	"getCustomerDeposit": func(e *env, tok string) (response, func() bool) {
		id, _ := e.recordedDeposit()
		return e.call(http.MethodGet, "/v1/customer-deposits/"+id, tok, nil), nil
	},
	"recordCustomerDeposit": func(e *env, tok string) (response, func() bool) {
		was := len(e.call(http.MethodGet, "/v1/customer-deposits?page_size=100", e.as(authz.Accountant), nil).Body["items"].([]any))
		r := e.call(http.MethodPost, "/v1/customer-deposits", tok, e.depositDraft("security", "10.00", ""))
		return r, func() bool {
			return len(e.call(http.MethodGet, "/v1/customer-deposits?page_size=100", e.as(authz.Accountant), nil).Body["items"].([]any)) == was
		}
	},
	"requestBankRuleAutoApply": func(e *env, tok string) (response, func() bool) {
		was := requestsOf(e, "bank_rule")
		rule := e.bankRule("Matrix")
		person := steppedUp(e, tok)
		r := e.call(http.MethodPost, "/v1/bank-rules/"+rule.ID.String()+"/auto-apply-approval", person, nil, "If-Match", ruleETag(rule))
		if r.Status == http.StatusAccepted {
			e.call(http.MethodPost, "/v1/approvals/"+r.str("id")+"/withdraw", person, nil, "If-Match", `"1"`)
		}
		return r, func() bool { return requestsOf(e, "bank_rule") == was }
	},
	"requestCustomerDepositVoid": func(e *env, tok string) (response, func() bool) {
		was := requestsOf(e, "customer_deposit")
		id, tag := e.recordedDeposit()
		person := steppedUp(e, tok)
		r := e.call(http.MethodPost, "/v1/customer-deposits/"+id+"/void-approval", person, map[string]any{"date": "2026-10-28", "reason": "Matrix test"}, "If-Match", tag)
		if r.Status == http.StatusAccepted {
			e.call(http.MethodPost, "/v1/approvals/"+r.str("id")+"/withdraw", person, nil, "If-Match", `"1"`)
		}
		return r, func() bool { return requestsOf(e, "customer_deposit") == was }
	},
	// A void in Founder mode is asked for, so the row voids in an organisation in Standard mode.
	"voidCustomerDeposit": func(e *env, tok string) (response, func() bool) {
		s := inStandardMode(e)
		e.tokens.mu.Lock()
		p := e.tokens.m[tok]
		e.tokens.mu.Unlock()
		if p.Organisation == e.books.Org {
			p.Organisation = s.books.Org
			tok = s.principal(p)
		}
		id, tag := s.recordedDeposit()
		r := s.call(http.MethodPost, "/v1/customer-deposits/"+id+"/void", tok, map[string]any{"date": "2026-10-28", "reason": "Matrix test"}, "If-Match", tag)
		return r, func() bool {
			return s.call(http.MethodGet, "/v1/customer-deposits/"+id, s.as(authz.Accountant), nil).str("status") == "posted"
		}
	},
	// Quotes and sales orders (FR-AR-01): read under V, drafted and invoiced under C, sent, answered and cancelled under P.
	"listQuotes": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/quotes", tok, nil), nil
	},
	"getSalesPipeline": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/quotes/pipeline?as_at=2026-10-31", tok, nil), nil
	},
	"getQuote": func(e *env, tok string) (response, func() bool) {
		id, _ := e.sentQuote()
		return e.call(http.MethodGet, "/v1/quotes/"+id, tok, nil), nil
	},
	"createQuote": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/quotes", tok, e.quoteDraft("quote", "1", "10.00")), nil
	},
	"replaceQuote": func(e *env, tok string) (response, func() bool) {
		c := e.call(http.MethodPost, "/v1/quotes", e.as(authz.Accountant), e.quoteDraft("quote", "1", "10.00"))
		r := e.call(http.MethodPut, "/v1/quotes/"+c.str("id"), tok, e.quoteDraft("quote", "2", "10.00"), "If-Match", c.Header.Get("ETag"))
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/quotes/"+c.str("id"), e.as(authz.Accountant), nil).Body["version"] == float64(1)
		}
	},
	"sendQuote": func(e *env, tok string) (response, func() bool) {
		c := e.call(http.MethodPost, "/v1/quotes", e.as(authz.Accountant), e.quoteDraft("quote", "1", "10.00"))
		r := e.call(http.MethodPost, "/v1/quotes/"+c.str("id")+"/send", tok, nil, "If-Match", c.Header.Get("ETag"))
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/quotes/"+c.str("id"), e.as(authz.Accountant), nil).str("status") == "draft"
		}
	},
	"acceptQuote": func(e *env, tok string) (response, func() bool) {
		id, tag := e.sentQuote()
		r := e.call(http.MethodPost, "/v1/quotes/"+id+"/accept", tok, map[string]any{"date": "2026-10-20"}, "If-Match", tag)
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/quotes/"+id, e.as(authz.Accountant), nil).str("status") == "sent"
		}
	},
	"declineQuote": func(e *env, tok string) (response, func() bool) {
		id, tag := e.sentQuote()
		r := e.call(http.MethodPost, "/v1/quotes/"+id+"/decline", tok, map[string]any{"date": "2026-10-20"}, "If-Match", tag)
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/quotes/"+id, e.as(authz.Accountant), nil).str("status") == "sent"
		}
	},
	"invoiceQuote": func(e *env, tok string) (response, func() bool) {
		id, tag := e.sentQuote()
		a := e.call(http.MethodPost, "/v1/quotes/"+id+"/accept", e.as(authz.Accountant), map[string]any{"date": "2026-10-20"}, "If-Match", tag)
		r := e.call(http.MethodPost, "/v1/quotes/"+id+"/invoice", tok, nil, "If-Match", a.Header.Get("ETag"))
		return r, func() bool {
			return len(e.call(http.MethodGet, "/v1/quotes/"+id, e.as(authz.Accountant), nil).Body["invoices"].([]any)) == 0
		}
	},
	"cancelQuote": func(e *env, tok string) (response, func() bool) {
		id, tag := e.sentQuote()
		r := e.call(http.MethodPost, "/v1/quotes/"+id+"/cancel", tok, map[string]any{"reason": "Matrix test"}, "If-Match", tag)
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/quotes/"+id, e.as(authz.Accountant), nil).str("status") == "sent"
		}
	},
	"getReceivables": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/receivables?as_at=2026-10-31", tok, nil), nil
	},
	"listInvoices": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/invoices", tok, nil), nil
	},
	"getInvoice": func(e *env, tok string) (response, func() bool) {
		id, _ := e.createInvoice("1", "10.00")
		return e.call(http.MethodGet, "/v1/invoices/"+id, tok, nil), nil
	},
	"getInvoiceDocument": func(e *env, tok string) (response, func() bool) {
		id, _ := e.createInvoice("1", "10.00")
		return e.call(http.MethodGet, "/v1/invoices/"+id+"/document", tok, nil), nil
	},
	"emailInvoice": func(e *env, tok string) (response, func() bool) {
		id, tag := e.issuedInvoice("1", "10.00")
		r := e.call(http.MethodPost, "/v1/invoices/"+id+"/email", tok, map[string]any{"to": "ap@acme.example"}, "If-Match", tag)
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/invoices/"+id, e.as(authz.Accountant), nil).str("states", "delivery") == "not_sent"
		}
	},
	"emailQuote": func(e *env, tok string) (response, func() bool) {
		id, tag := e.sentQuote()
		r := e.call(http.MethodPost, "/v1/quotes/"+id+"/email", tok, map[string]any{"to": "ap@acme.example"}, "If-Match", tag)
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/quotes/"+id, e.as(authz.Accountant), nil).str("delivery") == "not_sent"
		}
	},
	// The online link (FR-AR-06, FR-AR-16): public, so every caller opens it; revoking is P.
	"getPublicDocument": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/public/document", tok, nil, "Fyniq-Link", e.sharedLink("document")), nil
	},
	"getPublicDocumentPDF": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/public/document/pdf", tok, nil, "Fyniq-Link", e.sharedLink("pdf")), nil
	},
	"acceptQuoteByLink": func(e *env, tok string) (response, func() bool) {
		_, link := e.emailedQuoteLink()
		return e.call(http.MethodPost, "/v1/public/document/accept", tok, map[string]any{"name": "Jane Tan"}, "Fyniq-Link", link), nil
	},
	"declineQuoteByLink": func(e *env, tok string) (response, func() bool) {
		_, link := e.emailedQuoteLink()
		return e.call(http.MethodPost, "/v1/public/document/decline", tok, map[string]any{"comment": "No"}, "Fyniq-Link", link), nil
	},
	"revokeDocumentLink": func(e *env, tok string) (response, func() bool) {
		id, _ := e.emailedInvoiceLink("1", "10.00")
		links := e.call(http.MethodGet, "/v1/invoices/"+id, e.as(authz.Accountant), nil).Body["links"].([]any)
		link := links[0].(map[string]any)["id"].(string)
		r := e.call(http.MethodPost, "/v1/document-links/"+link+"/revoke", tok, nil)
		return r, func() bool {
			l := e.call(http.MethodGet, "/v1/invoices/"+id, e.as(authz.Accountant), nil).Body["links"].([]any)
			return l[0].(map[string]any)["status"] == "live"
		}
	},
	"createInvoice": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/invoices", tok, e.invoiceDraft("1", "10.00")), nil
	},
	"replaceInvoice": func(e *env, tok string) (response, func() bool) {
		id, _ := e.createInvoice("1", "10.00")
		r := e.call(http.MethodPut, "/v1/invoices/"+id, tok, e.invoiceDraft("1", "11.00"), "If-Match", "*")
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/invoices/"+id, e.as(authz.Accountant), nil).Body["version"] == float64(1)
		}
	},
	"issueInvoice": func(e *env, tok string) (response, func() bool) {
		id, tag := e.createInvoice("1", "10.00")
		r := e.call(http.MethodPost, "/v1/invoices/"+id+"/issue", tok, nil, "If-Match", tag)
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/invoices/"+id, e.as(authz.Accountant), nil).str("status") == "draft"
		}
	},
	"requestInvoiceIssue": func(e *env, tok string) (response, func() bool) {
		was := requestsOf(e, "invoice")
		id, tag := e.createInvoice("1", "2500.00")
		person := steppedUp(e, tok)
		r := e.call(http.MethodPost, "/v1/invoices/"+id+"/issue-approval", person, nil, "If-Match", tag)
		if r.Status == http.StatusAccepted {
			e.call(http.MethodPost, "/v1/approvals/"+r.str("id")+"/withdraw", person, nil, "If-Match", `"1"`)
		}
		return r, func() bool { return requestsOf(e, "invoice") == was }
	},
	"requestInvoiceVoid": func(e *env, tok string) (response, func() bool) {
		was := requestsOf(e, "invoice")
		id, tag := e.issuedInvoice("1", "10.00")
		person := steppedUp(e, tok)
		r := e.call(http.MethodPost, "/v1/invoices/"+id+"/void-approval", person, map[string]any{"date": "2026-10-28", "reason": "Matrix test"}, "If-Match", tag)
		if r.Status == http.StatusAccepted {
			e.call(http.MethodPost, "/v1/approvals/"+r.str("id")+"/withdraw", person, nil, "If-Match", `"1"`)
		}
		return r, func() bool { return requestsOf(e, "invoice") == was }
	},
	"requestInvoiceEdit": func(e *env, tok string) (response, func() bool) {
		was := requestsOf(e, "invoice")
		id, tag := e.issuedInvoice("1", "10.00")
		person := steppedUp(e, tok)
		r := e.call(http.MethodPost, "/v1/invoices/"+id+"/edit-approval", person, e.invoiceEdit("1", "12.00", "Matrix test"), "If-Match", tag)
		if r.Status == http.StatusAccepted {
			e.call(http.MethodPost, "/v1/approvals/"+r.str("id")+"/withdraw", person, nil, "If-Match", `"1"`)
		}
		return r, func() bool { return requestsOf(e, "invoice") == was }
	},
	// An edit or a void in Founder mode is asked for, so the rows edit and void in an organisation in Standard mode.
	"editInvoice": func(e *env, tok string) (response, func() bool) {
		s := inStandardMode(e)
		e.tokens.mu.Lock()
		p := e.tokens.m[tok]
		e.tokens.mu.Unlock()
		if p.Organisation == e.books.Org {
			p.Organisation = s.books.Org
			tok = s.principal(p)
		}
		id, tag := s.issuedInvoice("1", "10.00")
		r := s.call(http.MethodPost, "/v1/invoices/"+id+"/edit", tok, s.invoiceEdit("1", "12.00", "Matrix test"), "If-Match", tag)
		return r, func() bool {
			return s.call(http.MethodGet, "/v1/invoices/"+id, s.as(authz.Accountant), nil).Header.Get("ETag") == tag
		}
	},
	"voidInvoice": func(e *env, tok string) (response, func() bool) {
		s := inStandardMode(e)
		e.tokens.mu.Lock()
		p := e.tokens.m[tok]
		e.tokens.mu.Unlock()
		if p.Organisation == e.books.Org {
			p.Organisation = s.books.Org
			tok = s.principal(p)
		}
		id, tag := s.issuedInvoice("1", "10.00")
		r := s.call(http.MethodPost, "/v1/invoices/"+id+"/void", tok, map[string]any{"date": "2026-10-28", "reason": "Matrix test"}, "If-Match", tag)
		return r, func() bool {
			return s.call(http.MethodGet, "/v1/invoices/"+id, s.as(authz.Accountant), nil).str("status") == "posted"
		}
	},
	// Payments of bills recorded by hand (FR-AP-02): read under V, recorded and voided under P; asking to void is posting.
	// Supplier credits applied to bills (FR-AP-03): read under V, applied and voided under P; asking to void is posting.
	"getCreditApplication": func(e *env, tok string) (response, func() bool) {
		id, _ := e.appliedCredit()
		return e.call(http.MethodGet, "/v1/credit-applications/"+id, tok, nil), nil
	},
	"applyCredit": func(e *env, tok string) (response, func() bool) {
		b, _ := e.postedBill("10.00", "0.90")
		c := e.postedCredit("10.00", "0.90")
		r := e.call(http.MethodPost, "/v1/credit-applications", tok, map[string]any{"credit_id": c, "bill_id": b, "date": "2026-10-20",
			"amount": map[string]any{"amount": "10.90", "currency": "SGD"}})
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/bills/"+b, e.as(authz.Accountant), nil).str("states", "payment") == "unpaid"
		}
	},
	"requestCreditApplicationVoid": func(e *env, tok string) (response, func() bool) {
		was := requestsOf(e, "credit_application")
		id, tag := e.appliedCredit()
		person := steppedUp(e, tok)
		r := e.call(http.MethodPost, "/v1/credit-applications/"+id+"/void-approval", person, map[string]any{"date": "2026-10-28", "reason": "Matrix test"}, "If-Match", tag)
		if r.Status == http.StatusAccepted {
			e.call(http.MethodPost, "/v1/approvals/"+r.str("id")+"/withdraw", person, nil, "If-Match", `"1"`)
		}
		return r, func() bool { return requestsOf(e, "credit_application") == was }
	},
	// A void in Founder mode is asked for (requestCreditApplicationVoid), so the row voids in an organisation in Standard mode.
	"voidCreditApplication": func(e *env, tok string) (response, func() bool) {
		s := inStandardMode(e)
		e.tokens.mu.Lock()
		p := e.tokens.m[tok]
		e.tokens.mu.Unlock()
		if p.Organisation == e.books.Org {
			p.Organisation = s.books.Org
			tok = s.principal(p)
		}
		id, tag := s.appliedCredit()
		r := s.call(http.MethodPost, "/v1/credit-applications/"+id+"/void", tok, map[string]any{"date": "2026-10-28", "reason": "Matrix test"}, "If-Match", tag)
		return r, func() bool {
			return s.call(http.MethodGet, "/v1/credit-applications/"+id, s.as(authz.Accountant), nil).str("status") == "posted"
		}
	},
	// Refunds from suppliers (FR-AP-03): read under V, recorded and voided under P; asking to void is posting.
	"getSupplierRefund": func(e *env, tok string) (response, func() bool) {
		id, _ := e.refundedCredit()
		return e.call(http.MethodGet, "/v1/supplier-refunds/"+id, tok, nil), nil
	},
	"recordSupplierRefund": func(e *env, tok string) (response, func() bool) {
		c := e.postedCredit("10.00", "0.90")
		r := e.call(http.MethodPost, "/v1/supplier-refunds", tok, map[string]any{"credit_id": c, "date": "2026-10-22", "money_account_id": e.moneyAccount(),
			"amount": map[string]any{"amount": "10.90", "currency": "SGD"}})
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/bills/"+c, e.as(authz.Accountant), nil).str("open", "amount") == "10.90"
		}
	},
	"requestSupplierRefundVoid": func(e *env, tok string) (response, func() bool) {
		was := requestsOf(e, "supplier_refund")
		id, tag := e.refundedCredit()
		person := steppedUp(e, tok)
		r := e.call(http.MethodPost, "/v1/supplier-refunds/"+id+"/void-approval", person, map[string]any{"date": "2026-10-28", "reason": "Matrix test"}, "If-Match", tag)
		if r.Status == http.StatusAccepted {
			e.call(http.MethodPost, "/v1/approvals/"+r.str("id")+"/withdraw", person, nil, "If-Match", `"1"`)
		}
		return r, func() bool { return requestsOf(e, "supplier_refund") == was }
	},
	// A void in Founder mode is asked for (requestSupplierRefundVoid), so the row voids in an organisation in Standard mode.
	"voidSupplierRefund": func(e *env, tok string) (response, func() bool) {
		s := inStandardMode(e)
		e.tokens.mu.Lock()
		p := e.tokens.m[tok]
		e.tokens.mu.Unlock()
		if p.Organisation == e.books.Org {
			p.Organisation = s.books.Org
			tok = s.principal(p)
		}
		id, tag := s.refundedCredit()
		r := s.call(http.MethodPost, "/v1/supplier-refunds/"+id+"/void", tok, map[string]any{"date": "2026-10-28", "reason": "Matrix test"}, "If-Match", tag)
		return r, func() bool {
			return s.call(http.MethodGet, "/v1/supplier-refunds/"+id, s.as(authz.Accountant), nil).str("status") == "posted"
		}
	},
	"planBillPayment": func(e *env, tok string) (response, func() bool) {
		id, _ := e.postedBill("10.00", "0.90")
		r := e.call(http.MethodPost, "/v1/bills/"+id+"/payment-plan", tok, map[string]any{"planned_on": "2026-11-20"}, "If-Match", "*")
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/bills/"+id, e.as(authz.Accountant), nil).str("planned_payment_on") == ""
		}
	},
	"listBillReminders": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/bill-reminders", tok, nil), nil
	},
	// Reminders to customers (FR-AR-08): listed under V, sent or skipped under P on "Invoices, credit notes"; customer
	// groups are contact master data.
	"listSalesReminders": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/sales-reminders", tok, nil), nil
	},
	"sendSalesReminder": func(e *env, tok string) (response, func() bool) {
		id := matrixOverdueInvoice(e)
		r := e.call(http.MethodPost, "/v1/sales-reminders/"+id+"/send", tok, map[string]any{"step": 7})
		return r, func() bool { return e.remindersDue()[id] != nil }
	},
	"skipSalesReminder": func(e *env, tok string) (response, func() bool) {
		id := matrixOverdueInvoice(e)
		r := e.call(http.MethodPost, "/v1/sales-reminders/"+id+"/skip", tok, map[string]any{"step": 7})
		return r, func() bool { return e.remindersDue()[id] != nil }
	},
	// Statements of account (FR-AR-08): read under V, made under C and emailed under P on "Invoices, credit notes".
	"getCustomerStatement": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/contacts/"+e.customer()+"/statement?from=2026-10-01&to=2026-10-15", tok, nil), nil
	},
	"listStatements": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/statements", tok, nil), nil
	},
	"getStatement": func(e *env, tok string) (response, func() bool) {
		id, _ := matrixStatement(e)
		return e.call(http.MethodGet, "/v1/statements/"+id, tok, nil), nil
	},
	"createStatement": func(e *env, tok string) (response, func() bool) {
		e.readyToEmail()
		e.invoiceDue("2026-10-01", "2026-10-31")
		count := func() int {
			items, _ := e.call(http.MethodGet, "/v1/statements?page_size=500", e.as(authz.Accountant), nil).Body["items"].([]any)
			return len(items)
		}
		was := count()
		r := e.call(http.MethodPost, "/v1/statements", tok, map[string]any{"customer_id": e.customer(), "from": "2026-10-01", "to": "2026-10-15"})
		return r, func() bool { return count() == was }
	},
	"emailStatement": func(e *env, tok string) (response, func() bool) {
		id, tag := matrixStatement(e)
		r := e.call(http.MethodPost, "/v1/statements/"+id+"/email", tok, map[string]any{"to": "ap@acme.example"}, "If-Match", tag)
		return r, func() bool {
			sends, _ := e.call(http.MethodGet, "/v1/statements/"+id, e.as(authz.Accountant), nil).Body["sends"].([]any)
			return len(sends) == 0
		}
	},
	"listCustomerGroups": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/customer-groups", tok, nil), nil
	},
	"getCustomerGroup": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/customer-groups/"+matrixCustomerGroup(e), tok, nil), nil
	},
	"createCustomerGroup": func(e *env, tok string) (response, func() bool) {
		name := "Matrix " + uuid.NewString()[:8]
		r := e.call(http.MethodPost, "/v1/customer-groups", tok, map[string]any{"name": name, "reminder_days": []int{0}})
		return r, func() bool {
			return !strings.Contains(e.call(http.MethodGet, "/v1/customer-groups", e.as(authz.Accountant), nil).Raw, `"name":"`+name+`"`)
		}
	},
	"replaceCustomerGroup": func(e *env, tok string) (response, func() bool) {
		id, name := matrixCustomerGroup(e), "Matrix "+uuid.NewString()[:8]
		r := e.call(http.MethodPut, "/v1/customer-groups/"+id, tok, map[string]any{"name": name, "reminder_days": []int{1}}, "If-Match", "*")
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/customer-groups/"+id, e.as(authz.Accountant), nil).str("name") != name
		}
	},
	"previewDocumentTemplate": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/document-template-previews", tok, map[string]any{"kind": "email", "document_type": "quote",
			"content": map[string]any{"schema": "fyniq.template/1", "subject": "Quotation {{document.number}}", "body": "Dear {{customer.name}}"}}), nil
	},
	"exportDocumentTemplates": func(e *env, tok string) (response, func() bool) {
		id, _ := e.createTemplate()
		return e.call(http.MethodGet, "/v1/document-template-bundle?template_id="+id, tok, nil), nil
	},
	"importDocumentTemplates": func(e *env, tok string) (response, func() bool) {
		id, _ := e.createTemplate()
		bundle := e.call(http.MethodGet, "/v1/document-template-bundle?template_id="+id, e.as(authz.Accountant), nil).Body
		name := "Imported " + uuid.NewString()[:8]
		bundle["templates"].([]any)[0].(map[string]any)["name"] = name
		r := e.call(http.MethodPost, "/v1/document-template-bundles", tok, bundle)
		return r, func() bool {
			return !strings.Contains(e.call(http.MethodGet, "/v1/document-templates?page_size=500", e.as(authz.Accountant), nil).Raw, name)
		}
	},
	"listDocumentTemplateDefaults": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/document-template-defaults", tok, nil), nil
	},
	"getAppliedDocumentTemplate": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/document-template-resolution?kind=document&document_type=invoice", tok, nil), nil
	},
	"setDocumentTemplateDefault": func(e *env, tok string) (response, func() bool) {
		id, tag := e.createTemplate()
		e.call(http.MethodPost, "/v1/document-templates/"+id+"/publish", e.as(authz.Owner), nil, "If-Match", tag)
		r := e.call(http.MethodPut, "/v1/document-template-defaults/email/invoice", tok, map[string]any{"template_id": id}, "If-Match", "*")
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/document-template-resolution?kind=email&document_type=invoice", e.as(authz.Accountant), nil).str("template", "id") != id
		}
	},
	"listContactDocumentTemplates": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/contacts/"+e.supplier()+"/document-templates", tok, nil), nil
	},
	"setContactDocumentTemplate": func(e *env, tok string) (response, func() bool) {
		r := e.call(http.MethodPut, "/v1/contacts/"+e.supplier()+"/document-templates/email/remittance", tok, map[string]any{}, "If-Match", "*")
		return r, nil
	},
	"listDocumentTemplates": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/document-templates", tok, nil), nil
	},
	"getDocumentTemplateCatalogue": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/document-template-catalogue/credit_note", tok, nil), nil
	},
	"getDocumentTemplate": func(e *env, tok string) (response, func() bool) {
		id, _ := e.createTemplate()
		return e.call(http.MethodGet, "/v1/document-templates/"+id, tok, nil), nil
	},
	"listDocumentTemplateVersions": func(e *env, tok string) (response, func() bool) {
		id, _ := e.createTemplate()
		return e.call(http.MethodGet, "/v1/document-templates/"+id+"/versions", tok, nil), nil
	},
	"getDocumentTemplateVersion": func(e *env, tok string) (response, func() bool) {
		id, tag := e.createTemplate()
		if p := e.call(http.MethodPost, "/v1/document-templates/"+id+"/publish", e.as(authz.Owner), nil, "If-Match", tag); p.Status != http.StatusOK {
			e.t.Fatalf("publishDocumentTemplate = %d: %s", p.Status, p.Raw)
		}
		return e.call(http.MethodGet, "/v1/document-templates/"+id+"/versions/1", tok, nil), nil
	},
	"createDocumentTemplate": func(e *env, tok string) (response, func() bool) {
		name := "Matrix " + uuid.NewString()[:8]
		r := e.call(http.MethodPost, "/v1/document-templates", tok, map[string]any{"kind": "document", "document_type": "quote", "name": name})
		return r, func() bool {
			return !strings.Contains(e.call(http.MethodGet, "/v1/document-templates?page_size=500", e.as(authz.Accountant), nil).Raw, name)
		}
	},
	"replaceDocumentTemplate": func(e *env, tok string) (response, func() bool) {
		id, _ := e.createTemplate()
		r := e.call(http.MethodPut, "/v1/document-templates/"+id, tok, map[string]any{"name": "Renamed", "content": map[string]any{
			"schema": "fyniq.template/1", "subject": "Invoice {{document.number}}", "body": "Dear {{customer.name}}"}}, "If-Match", "*")
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/document-templates/"+id, e.as(authz.Accountant), nil).str("name") == "Invoice email"
		}
	},
	"publishDocumentTemplate": func(e *env, tok string) (response, func() bool) {
		id, _ := e.createTemplate()
		r := e.call(http.MethodPost, "/v1/document-templates/"+id+"/publish", tok, nil, "If-Match", "*")
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/document-templates/"+id, e.as(authz.Accountant), nil).Body["published_version"] == nil
		}
	},
	"requestDocumentTemplatePublication": func(e *env, tok string) (response, func() bool) {
		was := requestsOf(e, "document_template")
		id, tag := e.createTemplate()
		person := steppedUp(e, tok)
		r := e.call(http.MethodPost, "/v1/document-templates/"+id+"/approval", person, nil, "If-Match", tag)
		if r.Status == http.StatusAccepted {
			e.call(http.MethodPost, "/v1/approvals/"+r.str("id")+"/withdraw", person, nil, "If-Match", `"1"`)
		}
		return r, func() bool { return requestsOf(e, "document_template") == was }
	},
	"listWithholdingDecisions": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/contacts/"+e.foreignSupplier()+"/withholding-decisions", tok, nil), nil
	},
	"recordWithholdingDecision": func(e *env, tok string) (response, func() bool) {
		id := e.foreignSupplier()
		agent := "Matrix " + uuid.NewString()[:8]
		r := e.call(http.MethodPost, "/v1/contacts/"+id+"/withholding-decisions", tok, map[string]any{"nature_of_payment": "interest",
			"outcome": "not_applicable", "tax_agent": agent}, "If-Match", "*")
		return r, func() bool {
			return !strings.Contains(e.call(http.MethodGet, "/v1/contacts/"+id+"/withholding-decisions", e.as(authz.Accountant), nil).Raw, agent)
		}
	},
	"getPayables": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/payables?as_at=2026-10-31", tok, nil), nil
	},
	"listBillPayments": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/bill-payments", tok, nil), nil
	},
	"getBillPayment": func(e *env, tok string) (response, func() bool) {
		id, _ := e.recordedPayment()
		return e.call(http.MethodGet, "/v1/bill-payments/"+id, tok, nil), nil
	},
	"recordBillPayment": func(e *env, tok string) (response, func() bool) {
		b, _ := e.postedBill("10.00", "0.90")
		r := e.call(http.MethodPost, "/v1/bill-payments", tok, e.payment(b, "10.90"))
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/bills/"+b, e.as(authz.Accountant), nil).str("states", "payment") == "unpaid"
		}
	},
	"requestBillPaymentVoid": func(e *env, tok string) (response, func() bool) {
		was := requestsOf(e, "bill_payment")
		id, tag := e.recordedPayment()
		person := steppedUp(e, tok)
		r := e.call(http.MethodPost, "/v1/bill-payments/"+id+"/void-approval", person, map[string]any{"date": "2026-10-28", "reason": "Matrix test"}, "If-Match", tag)
		if r.Status == http.StatusAccepted {
			e.call(http.MethodPost, "/v1/approvals/"+r.str("id")+"/withdraw", person, nil, "If-Match", `"1"`)
		}
		return r, func() bool { return requestsOf(e, "bill_payment") == was }
	},
	// A void in Founder mode is asked for (requestBillPaymentVoid), so the row voids in an organisation in Standard mode.
	"voidBillPayment": func(e *env, tok string) (response, func() bool) {
		s := inStandardMode(e)
		e.tokens.mu.Lock()
		p := e.tokens.m[tok]
		e.tokens.mu.Unlock()
		if p.Organisation == e.books.Org {
			p.Organisation = s.books.Org
			tok = s.principal(p)
		}
		id, tag := s.recordedPayment()
		r := s.call(http.MethodPost, "/v1/bill-payments/"+id+"/void", tok, map[string]any{"date": "2026-10-28", "reason": "Matrix test"}, "If-Match", tag)
		return r, func() bool {
			return s.call(http.MethodGet, "/v1/bill-payments/"+id, s.as(authz.Accountant), nil).str("status") == "posted"
		}
	},
	// A void in Founder mode is asked for (requestJournalReversal), so the row reverses in an organisation in Standard mode.
	"reverseJournal": func(e *env, tok string) (response, func() bool) {
		s := inStandardMode(e)
		e.tokens.mu.Lock()
		p := e.tokens.m[tok]
		e.tokens.mu.Unlock()
		if p.Organisation == e.books.Org {
			p.Organisation = s.books.Org
			tok = s.principal(p)
		}
		id, tag := s.postedJournal("10.00")
		r := s.call(http.MethodPost, "/v1/journals/"+id+"/reverse", tok, map[string]any{"date": "2026-10-28", "reason": "Matrix test"}, "If-Match", tag)
		return r, func() bool {
			return s.call(http.MethodGet, "/v1/journals/"+id, s.as(authz.Accountant), nil).str("status") == "posted"
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
			cell := authztest.ParseCell(rows[string(c)][authz.Column(role)])
			if (c == authz.ExpenseClaims || c == authz.PaymentBatches) && cell.Qualifier != "" {
				continue // an Employee's own claims are reached from the claim, not the approvals (design D10); nor is "C (salaries)"
			}
			if ok, _ := authztest.Allows(cell, p.Need); ok {
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
	if allowed && p.Unqualified && authztest.ParseCell(row[col]).Qualifier != "" { // such as Reports' "V sales" (rev 0.52)
		return false, false
	}
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
	for _, rows := range []map[string]func(e *env, tok string) (response, func() bool){matrixBanking, matrixBankLines, matrixBankRules, matrixBankIngest, matrixBankSignals, matrixClaims, matrixReimbursements, matrixMileage, matrixCategories, matrixClaimEvidence, matrixAdvances, matrixCards, matrixAssets, matrixDisposals, matrixPayee, matrixClaimantBank, matrixPaymentBatches, matrixPaymentBatchRelease} {
		for op, call := range rows {
			matrixCalls[op] = call
		}
	}
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
		for _, op := range slices.Sorted(maps.Keys(policies)) {
			// A session per operation: one would pass the rate limit of a minute (FR-INT-16) on the frozen clock.
			tok := e.principal(p)
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

// matrixMemberUser is the user of the matrix's member of e's organisation.
func matrixMemberUser(e *env) uuid.UUID {
	id := matrixMember(e)
	for _, x := range e.call(http.MethodGet, "/v1/members", e.as(authz.Administrator), nil).Body["items"].([]any) {
		if m := x.(map[string]any); m["id"] == id {
			return uuid.MustParse(m["user_id"].(string))
		}
	}
	e.t.Fatalf("the matrix member %s is not listed", id)
	return uuid.Nil
}
