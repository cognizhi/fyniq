//go:build local

package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// entriesOf are the IDs of the entries a source document posted, in revision order.
func (e *env) entriesOf(source string) []string {
	e.t.Helper()
	var out []string
	if err := e.store.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		es, err := u.Ledger().OfSource(ctx, uuid.MustParse(source))
		for _, x := range es {
			out = append(out, x.ID.String())
		}
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
	return out
}

// A ledger line traces to its entry and its source (FR-UX-04, the Trace panel): an issued invoice's entry gives its
// lines with their accounts, and its source the invoice, its number, status, date, customer and page; a journal's has
// no party. A view of part of the reports is refused, as the general ledger's is; an unknown entry is not found.
// req: FR-UX-04
// req: FR-GL-11
func TestALedgerEntryTracesToItsSource(t *testing.T) {
	e := newEnv(t)
	accountant := e.as(authz.Accountant)
	invoice, _ := e.issuedInvoice("2", "500.00")
	entries := e.entriesOf(invoice)
	if len(entries) != 1 {
		t.Fatalf("the invoice's entries: %v", entries)
	}
	r := e.call(http.MethodGet, "/v1/ledger/entries/"+entries[0], accountant, nil)
	if r.Status != http.StatusOK {
		t.Fatalf("getLedgerEntry = %d: %s", r.Status, r.Raw)
	}
	src := r.Body["source"].(map[string]any)
	inv := e.call(http.MethodGet, "/v1/invoices/"+invoice, accountant, nil)
	if src["type"] != "invoice" || src["id"] != invoice || src["document_type"] != "invoice" || src["number"] != inv.str("number") ||
		src["status"] == nil || src["date"] != inv.str("date") || src["party"] != "Acme Analytics Pte Ltd" || src["version"] != float64(1) || src["link"] != "/sales/invoices/"+invoice {
		t.Errorf("the invoice as the source: %v", src)
	}
	lines := r.Body["lines"].([]any)
	debits := 0
	for _, x := range lines {
		l := x.(map[string]any)
		if l["code"] == nil || l["name"] == nil || l["account_id"] == nil || l["base"].(map[string]any)["currency"] != "SGD" {
			t.Errorf("a line: %v", l)
		}
		if a := l["base"].(map[string]any)["amount"].(string); a != "" && a[0] != '-' {
			debits++
		}
	}
	if len(lines) < 2 || debits == 0 || r.Body["ledger_revision"] == nil || r.Body["posting_date"] != inv.str("date") {
		t.Errorf("the entry: %s", r.Raw)
	}

	journal, _ := e.postedJournal("80.00")
	j := e.call(http.MethodGet, "/v1/ledger/entries/"+e.entriesOf(journal)[0], accountant, nil)
	if js := j.Body["source"].(map[string]any); j.Status != http.StatusOK || js["type"] != "manual_journal" || js["document_type"] != "journal" ||
		js["party"] != nil || js["status"] != "posted" || js["link"] != "/accounting/journals/"+journal {
		t.Errorf("a journal's entry: %d %s", j.Status, j.Raw)
	}

	e.problemIs(e.call(http.MethodGet, "/v1/ledger/entries/"+entries[0], e.as(authz.ARSpecialist), nil), "getLedgerEntry", 403, CodeForbidden)
	e.problemIs(e.call(http.MethodGet, "/v1/ledger/entries/"+uuid.NewString(), accountant, nil), "getLedgerEntry", 404, CodeNotFound)
	e.problemIs(e.call(http.MethodGet, "/v1/ledger/entries/nope", accountant, nil), "getLedgerEntry", 422, CodeInvalid)
}

// The general ledger as at a revision gives the lines and the opening balance the ledger held then (FR-RPT-08), so a
// statement's figure traces to the lines it was read from (FR-UX-04): a journal posted after it is left out; a
// revision later than the ledger's is refused.
// req: FR-RPT-08
// req: FR-UX-04
func TestTheGeneralLedgerAsAtARevision(t *testing.T) {
	e := newEnv(t)
	accountant := e.as(authz.Accountant)
	bank := e.books.Bank.ID.String()
	e.postedJournal("30.00")
	report := func(query string) response {
		t.Helper()
		r := e.call(http.MethodGet, "/v1/reports/general-ledger?account_id="+bank+"&from=2026-10-01&to=2026-10-31"+query, accountant, nil)
		if r.Status != http.StatusOK {
			t.Fatalf("getGeneralLedger%s = %d: %s", query, r.Status, r.Raw)
		}
		return r
	}
	then := report("")
	revision := int64(then.Body["ledger_revision"].(float64))
	e.postedJournal("45.00")
	now, at := report(""), report("&as_at_revision="+strconv.FormatInt(revision, 10))
	lines := func(r response) int { return len(r.Body["accounts"].([]any)[0].(map[string]any)["lines"].([]any)) }
	closing := func(r response) string {
		return r.Body["accounts"].([]any)[0].(map[string]any)["closing"].(map[string]any)["amount"].(string)
	}
	if lines(now) != lines(then)+1 || lines(at) != lines(then) || closing(at) != closing(then) || at.Body["ledger_revision"] != float64(revision) {
		t.Errorf("as at %d: %d lines, closing %s; then %d, %s; now %d", revision, lines(at), closing(at), lines(then), closing(then), lines(now))
	}
	if r := e.call(http.MethodGet, "/v1/reports/general-ledger?account_id="+bank+"&from=2026-10-01&to=2026-10-31&as_at_revision=999999",
		accountant, nil); r.Status != 422 || !strings.Contains(r.Raw, `"code":"`+CodeLaterRevision+`"`) {
		t.Errorf("a later revision: %d %s", r.Status, r.Raw)
	}
}
