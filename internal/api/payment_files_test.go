//go:build local

package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/paymentfile"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// standIn stands in for a bank's own file that is not built yet, so the setting's use can be tested before it is: one
// line a payment, from what release gives every writer.
type standIn struct{ name string }

func (s standIn) Name() string { return s.name }

func (s standIn) Write(f paymentfile.File) (paymentfile.Out, error) {
	var b bytes.Buffer
	for _, p := range f.Payments {
		fmt.Fprintf(&b, "%s|%s|%s|%s\r\n", p.AccountName, p.Number, p.Amount.String(), p.Reference)
	}
	return paymentfile.Out{Name: f.Batch + "-stand-in.txt", MediaType: "text/plain", Data: b.Bytes()}, nil
}

// approvedBatchFrom prepares a batch of one supplier's bill paid from acc and approves it as the engine does; with its
// ETag.
func (e *env) approvedBatchFrom(acc storage.MoneyAccount) (string, string) {
	e.t.Helper()
	vendor := e.vendor("Payee " + uuid.NewString()[:8])
	e.vendorAccount(vendor, "0123456789")
	r := e.call(http.MethodPost, "/v1/payment-batches", steppedUp(e, e.as(authz.Accountant)), batchBody(acc, "bill:"+e.billOf(vendor, "10.00", "0.90")))
	if r.Status != http.StatusCreated {
		e.t.Fatalf("preparing the batch = %d: %s", r.Status, r.Raw)
	}
	e.approveBatch(r.str("id"))
	g := e.call(http.MethodGet, "/v1/payment-batches/"+r.str("id"), e.as(authz.Accountant), nil)
	return r.str("id"), g.Header.Get("ETag")
}

// moneyAccountAt sets up an SGD bank account at institution, as Banking keeps it.
func (e *env) moneyAccountAt(institution string) storage.MoneyAccount {
	e.t.Helper()
	acc := e.chartMoneyAccount()
	var m storage.MoneyAccount
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		m, err = u.Banking().CreateMoneyAccount(ctx, storage.NewMoneyAccount{AccountID: acc.ID, Kind: storage.MoneyBank, Name: acc.Name,
			Institution: institution, Identifier: "072-" + acc.Code, Currency: "SGD", At: e.clock.Now()})
		return err
	})
	return m
}

// paymentFileChanges are the payment file's before and after in each audit event of a money account, oldest first.
func (e *env) paymentFileChanges(account string) []string {
	e.t.Helper()
	auditor := e.as(authz.Auditor)
	l := e.call(http.MethodGet, "/v1/audit/events?record_type=money_account&record_id="+account, auditor, nil)
	items, _ := l.Body["items"].([]any)
	if l.Status != http.StatusOK || len(items) == 0 {
		e.t.Fatalf("the money account's events: %d %s", l.Status, l.Raw)
	}
	var out []string
	for i := len(items) - 1; i >= 0; i-- {
		ev := e.call(http.MethodGet, "/v1/audit/events/"+items[i].(map[string]any)["id"].(string), auditor, nil)
		for _, c := range ev.Body["changes"].([]any) {
			if c := c.(map[string]any); c["field"] == "payment_file" {
				out = append(out, fmt.Sprintf("%s: %v → %v", ev.str("action"), c["before"], c["after"]))
			}
		}
	}
	return out
}

// Each money account says which file its payment batches give (FR-BNK-12, rev 0.157; p1-payment-files task 1.2, design
// D1, D2, D5). Set up, a DBS account gives the payment list, and says which files it may give and why not the others:
// Wise's file is for a Wise account alone, and DBS IDEAL's UFF is not available until DBS's specification is in hand;
// both are refused with their codes, and an Airwallex account is offered the payment list alone. With Wise's file
// standing in as built, a Wise account is set to it, audited with the old and new value; a batch released from it gives
// Wise's file, and keeps it, to the byte, when the account goes back to the payment list, which the next batch gives. A
// change of bank that would leave a file the bank cannot read is refused, and release refuses a file that is no longer
// written, releasing nothing.
// req: FR-BNK-12
// req: AUD-01
// req: QA-RPT-02
// req: banking/bank-transactions The file is a setting of the money account (rev 0.157)
// req: banking/bank-transactions Airwallex pays through its API (rev 0.157)
// asvs: V2.3.1, V16.2.1
func TestAMoneyAccountSaysWhichFileItsBatchesGive(t *testing.T) {
	e := newEnv(t)
	bookkeeper := e.as(authz.Bookkeeper)
	create := func(institution, file string) response {
		body := map[string]any{"account_id": e.chartMoneyAccount().ID.String(), "kind": "bank", "name": institution + " current", "institution": institution}
		if file != "" {
			body["payment_file"] = file
		}
		return e.call(http.MethodPost, "/v1/money-accounts", bookkeeper, body)
	}
	choices := func(r response) map[string]string {
		out := map[string]string{}
		for _, c := range r.Body["payment_files"].([]any) {
			c := c.(map[string]any)
			out[c["format"].(string)] = fmt.Sprint(c["offered"])
			if reason, _ := c["reason"].(string); reason != "" {
				out[c["format"].(string)] = reason
			}
		}
		return out
	}

	dbs := create("DBS Bank Ltd", "")
	if dbs.Status != http.StatusCreated || dbs.str("payment_file") != "payment-list" || dbs.str("provider") != "dbs" {
		t.Fatalf("a DBS account set up: %d %s", dbs.Status, dbs.Raw)
	}
	if c := choices(dbs); c["payment-list"] != "true" || !strings.Contains(c["wise-batch"], "only for a Wise account") ||
		!strings.Contains(c["dbs-uff"], "not available yet") || !strings.Contains(c["dbs-uff"], "DBS UFF Version 2.2") {
		t.Errorf("a DBS account's choices: %v", c)
	}
	if got := e.paymentFileChanges(dbs.str("id")); len(got) != 1 || got[0] != "money_account.opened: <nil> → payment-list" {
		t.Errorf("the account's opening event: %v", got)
	}
	e.problemIs(create("DBS", "wise-batch"), "createMoneyAccount", 422, paymentfile.CodeProvider)
	e.problemIs(create("DBS", "dbs-uff"), "createMoneyAccount", 422, paymentfile.CodeNotBuilt)
	e.problemIs(create("Wise", "wise-batch"), "createMoneyAccount", 422, paymentfile.CodeNotBuilt)
	refused := e.call(http.MethodPut, "/v1/money-accounts/"+dbs.str("id"), bookkeeper, map[string]any{"kind": "bank", "name": "DBS current",
		"institution": "DBS", "payment_file": "wise-batch"}, "If-Match", dbs.Header.Get("ETag"))
	e.problemIs(refused, "replaceMoneyAccount", 422, paymentfile.CodeProvider)
	if fields(refused)["body.payment_file"] != paymentfile.CodeProvider {
		t.Errorf("the refusal does not name the field: %s", refused.Raw)
	}
	if air := create("Airwallex", ""); air.str("provider") != "airwallex" || choices(air)["payment-list"] != "true" ||
		!strings.Contains(choices(air)["wise-batch"], "only for a Wise account") || !strings.Contains(choices(air)["dbs-uff"], "only for a DBS account") {
		t.Errorf("an Airwallex account: %s", air.Raw)
	}

	// Wise's file stands in as built.
	built := payFiles
	t.Cleanup(func() { payFiles = built })
	payFiles = paymentfile.Writers{storage.PaymentFileList: paymentfile.PaymentList{}, storage.PaymentFileWise: standIn{"wise-batch@0"}}
	wise := e.moneyAccountAt("Wise")
	path := "/v1/money-accounts/" + wise.ID.String()
	set := func(file, institution, tag string) response {
		body := map[string]any{"kind": "bank", "name": wise.Name, "institution": institution}
		if file != "" {
			body["payment_file"] = file
		}
		return e.call(http.MethodPut, path, bookkeeper, body, "If-Match", tag)
	}
	s := set("wise-batch", "Wise", `"1"`)
	if s.Status != http.StatusOK || s.str("payment_file") != "wise-batch" || choices(s)["wise-batch"] != "true" {
		t.Fatalf("setting a Wise account to Wise's file: %d %s", s.Status, s.Raw)
	}
	release := func(id, tag string) response {
		return e.call(http.MethodPost, "/v1/payment-batches/"+id+"/release", steppedUp(e, e.as(authz.PaymentReleaser)), nil, "If-Match", tag)
	}
	released := release(e.approvedBatchFrom(wise))
	if released.Status != http.StatusOK || released.str("release", "writer") != "wise-batch@0" || !strings.HasSuffix(released.str("release", "file_name"), "-stand-in.txt") {
		t.Fatalf("a batch released from the Wise account: %d %s", released.Status, released.Raw)
	}
	file := func(batch string) (string, string) {
		f := e.call(http.MethodGet, "/v1/payment-batches/"+batch+"/file", steppedUp(e, e.as(authz.PaymentReleaser)), nil)
		if f.Status != http.StatusOK {
			t.Fatalf("downloading the file = %d: %s", f.Status, f.Raw)
		}
		data, err := base64.StdEncoding.DecodeString(f.str("content_base64"))
		if err != nil {
			t.Fatal(err)
		}
		return string(data), f.str("sha256")
	}
	first, firstHash := file(released.str("id"))
	if !strings.Contains(first, "|0123456789|10.90|") || firstHash != released.str("release", "sha256") {
		t.Errorf("Wise's file as written: %q %s", first, firstHash)
	}

	back := set("payment-list", "Wise", s.Header.Get("ETag"))
	if back.Status != http.StatusOK || back.str("payment_file") != "payment-list" {
		t.Fatalf("back to the payment list: %d %s", back.Status, back.Raw)
	}
	if again, hash := file(released.str("id")); again != first || hash != firstHash {
		t.Errorf("the batch released before the change lost its file: %q %s", again, hash)
	}
	next := release(e.approvedBatchFrom(wise))
	if next.Status != http.StatusOK || next.str("release", "writer") != "payment-list@1" {
		t.Fatalf("the next batch: %d %s", next.Status, next.Raw)
	}
	if got := strings.Join(e.paymentFileChanges(wise.ID.String()), "; "); got != "money_account.opened: <nil> → payment-list; "+
		"money_account.changed: payment-list → wise-batch; money_account.changed: wise-batch → payment-list" {
		t.Errorf("the changes audited: %s", got)
	}

	// A change of bank never leaves a file the bank cannot read; left out, the file stays as it is.
	s = set("wise-batch", "Wise", back.Header.Get("ETag"))
	e.problemIs(set("", "DBS", s.Header.Get("ETag")), "replaceMoneyAccount", 422, paymentfile.CodeProvider)
	if kept := set("", "Wise Business", s.Header.Get("ETag")); kept.Status != http.StatusOK || kept.str("payment_file") != "wise-batch" {
		t.Fatalf("a change leaving the file out: %d %s", kept.Status, kept.Raw)
	}

	// Release reads the file when it writes it: one no longer written is refused, and nothing is released.
	id, tag := e.approvedBatchFrom(wise)
	payFiles = built
	e.problemIs(release(id, tag), "releasePaymentBatch", 422, paymentfile.CodeNotBuilt)
	if g := e.call(http.MethodGet, "/v1/payment-batches/"+id, e.as(authz.Accountant), nil); g.str("status") != "approved" || g.Body["release"] != nil {
		t.Errorf("a refused release left the batch: %s", g.Raw)
	}
}
