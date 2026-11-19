//go:build local

package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/banktransaction"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// disputeOn records a dispute's balance transactions on connector c (its withdrawal and its fee) and imports their two lines
// into money account m, posted to disputes account d when post is true.
func (e *env) disputeOn(t *testing.T, m storage.MoneyAccount, c storage.InstalledConnector, disputes storage.Account, id, gross, fee string, post bool) {
	t.Helper()
	worker, err := postgres.Open(context.Background(), db.URL("fyniq_worker"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(worker.Close)
	at := e.clock.Now()
	txn := "txn_" + id
	sgd := money.MustLookup("SGD")
	g, _ := money.Parse(gross, sgd)
	f, _ := money.Parse(fee, sgd)
	n, _ := g.Sub(f)
	if err := worker.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, _, err := u.PSP().RecordTxn(ctx, storage.NewPSPTxn{ConnectorID: c.ID, TxnID: txn, Type: "adjustment", Category: storage.PSPCatDispute, SourceKind: "dispute", SourceID: id,
			Currency: "SGD", Gross: storage.Decimal(gross), Fee: storage.Decimal(fee), Net: storage.Decimal(n.String()), ProviderAt: at, SeenAt: at})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var lines []storage.BankLine
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		_, lines, err = u.Banking().Import(ctx, storage.NewBankStatement{MoneyAccountID: m.ID, Source: storage.MoneySourceConnector, ConnectorID: &c.ID, Format: storage.FormatAPI,
			ParserVersion: "api@1", PeriodFrom: porttest.Day(10, 12), PeriodTo: porttest.Day(10, 12), Currency: "SGD", At: at,
			Lines: []storage.NewBankLine{
				{Position: 1, BookingDate: porttest.Day(10, 12), Amount: storage.Decimal(gross), Reference: txn, Description: "dispute " + id, Fingerprint: strings.Repeat(id[len(id)-1:], 64)},
				{Position: 2, BookingDate: porttest.Day(10, 12), Amount: storage.Decimal("-" + fee), Reference: txn + ":fee", Description: "fee " + id, Fingerprint: strings.Repeat(id[len(id)-2:len(id)-1], 64)}}})
		return err
	})
	if !post {
		return
	}
	svc := journal.BankTransactions{Store: e.store, Clock: e.clock}
	for _, l := range lines {
		if _, err := svc.Categorise(context.Background(), e.books.Org, journal.Categorisation{Basis: storage.BasisExact, At: at, Draft: banktransaction.Draft{LineID: l.ID,
			Automatic: true, Parts: []banktransaction.Part{{AccountID: disputes.ID, GSTCode: "NA"}}}}); err != nil {
			t.Fatal(err)
		}
	}
}

// FR-PSP-04 (the owner's decision of 19 Nov 2026): a lost dispute's money waits in disputes clearing until a person asks for the
// draft journal that writes it off (Dr the dispute loss account, Cr disputes clearing), once per dispute, only when every line of
// the dispute has posted; up to the limit (S$100 by default) nothing more is needed, above it the person confirms that the invoice's
// revenue stays as invoiced and says why; the journal is only a draft, posted under Journals. A parked event is marked dealt with
// by a person, with a reason, once.
// req: banking/payment-providers A lost dispute posts nothing
// req: banking/payment-providers Unknown event type parked
func TestALostDisputeIsWrittenOffByADraftJournal(t *testing.T) {
	e := newEnv(t)
	admin, acct, bk := e.as(authz.Administrator), e.as(authz.Accountant), e.as(authz.Bookkeeper)
	m, c := e.providerAccount()
	var disputes, loss storage.Account
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if disputes, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "1055", Name: "Payment disputes clearing", Type: storage.Asset}); err != nil {
			return err
		}
		loss, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "5045", Name: "Dispute losses", Type: storage.Expense})
		return err
	})
	path := "/v1/payment-providers/accounts/" + m.ID.String()
	setup := func(withLoss bool) response {
		b := map[string]any{"fee_account_id": e.books.Rent.ID.String(), "disputes_account_id": disputes.ID.String()}
		if withLoss {
			b["dispute_loss_account_id"] = loss.ID.String()
		}
		hdr := []string{}
		if v := e.pspVersion(m.ID); v > 0 {
			hdr = []string{"If-Match", `"` + strconv.Itoa(v) + `"`}
		}
		return e.call(http.MethodPut, path, admin, b, hdr...)
	}
	if r := setup(false); r.Status != http.StatusOK || r.str("dispute_loss_account_id") != "" {
		t.Fatalf("setup: %d %s", r.Status, r.Raw)
	}
	body := func(extra map[string]any) map[string]any {
		b := map[string]any{"money_account_id": m.ID.String()}
		for k, v := range extra {
			b[k] = v
		}
		return b
	}
	e.disputeOn(t, m, c, disputes, "dp_001", "-50.00", "15.00", true)  // holds 65.00
	e.disputeOn(t, m, c, disputes, "dp_002", "-150.00", "15.00", true) // holds 165.00, above the limit
	e.disputeOn(t, m, c, disputes, "dp_003", "-20.00", "15.00", false) // lines not posted

	if r := e.call(http.MethodPost, "/v1/payment-providers/disputes/dp_001/loss", acct, body(nil)); r.Status != http.StatusConflict || r.problemCode() != CodePSPDisputeState {
		t.Fatalf("with no loss account set: %d %s", r.Status, r.Raw)
	}
	if r := setup(true); r.Status != http.StatusOK || r.str("dispute_loss_account_name") != "5045 Dispute losses" {
		t.Fatalf("setup with a loss account: %d %s", r.Status, r.Raw)
	}
	// An account that is not an expense is refused as the loss account.
	if r := e.call(http.MethodPut, path, admin, map[string]any{"fee_account_id": e.books.Rent.ID.String(), "dispute_loss_account_id": e.books.Sales.ID.String()},
		"If-Match", `"`+strconv.Itoa(e.pspVersion(m.ID))+`"`); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("a revenue account as the loss account: %d %s", r.Status, r.Raw)
	}

	list := e.call(http.MethodGet, "/v1/payment-providers/disputes", acct, nil)
	byID := map[string]map[string]any{}
	for _, x := range list.Body["items"].([]any) {
		it := x.(map[string]any)
		byID[it["dispute_id"].(string)] = it
	}
	if len(byID) != 3 || byID["dp_001"]["status"] != "open" || byID["dp_001"]["held"].(map[string]any)["amount"] != "65.00" || byID["dp_001"]["over_limit"] != false ||
		byID["dp_002"]["over_limit"] != true || byID["dp_003"]["all_posted"] != false {
		t.Fatalf("the disputes: %s", list.Raw)
	}

	r := e.call(http.MethodPost, "/v1/payment-providers/disputes/dp_001/loss", acct, body(nil))
	if r.Status != http.StatusCreated || r.str("amount", "amount") != "65.00" || r.Body["over_limit"] != false {
		t.Fatalf("the draft: %d %s", r.Status, r.Raw)
	}
	j := e.call(http.MethodGet, "/v1/journals/"+r.str("document_id"), acct, nil)
	if j.Status != http.StatusOK || j.str("status") != "draft" {
		t.Fatalf("the journal: %d %s", j.Status, j.Raw)
	}
	lines, _ := j.Body["lines"].([]any)
	if len(lines) != 2 || lines[0].(map[string]any)["account_id"] != loss.ID.String() || lines[1].(map[string]any)["account_id"] != disputes.ID.String() {
		t.Errorf("its lines: %s", j.Raw)
	}
	if bal := e.balanceOf(t, loss.ID); bal != "0.00" {
		t.Errorf("a draft posted: the loss account holds %s", bal)
	}
	if r := e.call(http.MethodPost, "/v1/payment-providers/disputes/dp_001/loss", acct, body(nil)); r.Status != http.StatusConflict || r.problemCode() != "STORAGE-DUPLICATE-01" && r.problemCode() != "STORAGE-DUPLICATE-01 " && !strings.Contains(r.Raw, "already") {
		t.Errorf("a second draft: %d %s", r.Status, r.Raw)
	}
	// Above the limit: a reason and the acknowledgement; the lines of a dispute must all have posted.
	if r := e.call(http.MethodPost, "/v1/payment-providers/disputes/dp_002/loss", acct, body(nil)); r.Status != http.StatusUnprocessableEntity || r.problemCode() != CodePSPDisputeLimit {
		t.Errorf("above the limit with no reason: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/payment-providers/disputes/dp_002/loss", acct, body(map[string]any{"accept_overstated_revenue": true})); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("above the limit with no reason, accepted: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/payment-providers/disputes/dp_002/loss", acct, body(map[string]any{"accept_overstated_revenue": true, "reason": "One-off charge: no invoice"})); r.Status != http.StatusCreated ||
		r.Body["over_limit"] != true {
		t.Errorf("above the limit, confirmed: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/payment-providers/disputes/dp_003/loss", acct, body(nil)); r.Status != http.StatusConflict || r.problemCode() != CodePSPDisputeState {
		t.Errorf("a dispute with unposted lines: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/payment-providers/disputes/dp_999/loss", acct, body(nil)); r.Status != http.StatusNotFound {
		t.Errorf("an unknown dispute: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/payment-providers/disputes/dp_001/loss", e.as(authz.Auditor), body(nil)); r.Status != http.StatusForbidden {
		t.Errorf("an auditor prepared a journal: %d", r.Status)
	}
	after := e.call(http.MethodGet, "/v1/payment-providers/disputes", acct, nil)
	for _, x := range after.Body["items"].([]any) {
		it := x.(map[string]any)
		if it["dispute_id"] == "dp_001" && (it["status"] != "written_off" || it["loss_document_id"] == nil) {
			t.Errorf("dp_001 after: %v", it)
		}
	}

	// A parked event is resolved by a person, once, with a reason.
	var ev storage.PSPEvent
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		var isNew bool
		if ev, isNew, err = u.PSPEvents().Receive(ctx, storage.NewPSPEvent{ID: uuid.New(), ConnectorID: c.ID, Provider: "stripe", EventID: "evt_park", EventType: "radar.thing",
			ParserVersion: "stripe@1", Payload: []byte("sealed"), PayloadSHA256: strings.Repeat("b", 64), ReceivedAt: e.clock.Now()}); err != nil || !isNew {
			return err
		}
		_, err = u.PSPEvents().Settle(ctx, ev.ID, storage.PSPParked, "unknown type", e.clock.Now())
		return err
	})
	evPath := "/v1/payment-providers/events/" + ev.ID.String() + "/resolution"
	if r := e.call(http.MethodPost, evPath, bk, map[string]any{"reason": ""}); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("a resolution with no reason: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, evPath, bk, map[string]any{"reason": "A fraud warning for a refunded charge: nothing to post"}); r.Status != http.StatusOK || r.str("status") != "processed" {
		t.Errorf("the resolution: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, evPath, bk, map[string]any{"reason": "Again"}); r.Status != http.StatusConflict || r.problemCode() != CodePSPEventState {
		t.Errorf("a second resolution: %d %s", r.Status, r.Raw)
	}
}
