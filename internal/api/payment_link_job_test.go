//go:build local

package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/connector"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/egress"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/paymentlinkjob"
	"github.com/cognizhi/fyniq/internal/pdftext"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// queuedLinkJobs are the payment-link jobs the outbox holds, drained from the queue; the other tasks are dropped.
func (e *env) queuedLinkJobs() []storage.OutboxRow {
	e.t.Helper()
	ctx := context.Background()
	var out []storage.OutboxRow
	for {
		task, err := e.queue.Next(ctx, "worker")
		if err != nil {
			return out
		}
		var m outbox.Message
		if err := json.Unmarshal(task.Payload, &m); err != nil {
			e.t.Fatal(err)
		}
		var row storage.OutboxRow
		if err := e.store.Do(ctx, m.OrganisationID, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			row, err = u.Outbox().Get(ctx, m.OutboxID)
			return err
		}); err != nil {
			e.t.Fatal(err)
		}
		if row.Kind == paymentlinkjob.Kind {
			out = append(out, row)
		}
	}
}

// FR-AR-13 (rev 0.588): with automatic payment links on, issuing an invoice queues a job and asks the provider for nothing; the worker's job then asks the provider once for the open balance (GST included), records the
// link through the narrow function (the worker holds no right on the table), and the invoice's email carries it. With the setting off nothing is queued; a second run asks for nothing, as the invoice has a live
// link; a provider that refuses leaves no link.
// req: FR-AR-13
// asvs: V2.3.1
func TestIssuingAnInvoiceQueuesALinkJobThatTheWorkerRunsOutsideTheRequest(t *testing.T) {
	e := newEnv(t)
	e.readyToEmail()
	f := &fakeLinkProvider{fakeLedgerProvider: fakeLedgerProvider{payouts: map[string][]string{}, balance: "0.00"}}
	b := fakeLinkConnector(t, f)
	cat, err := connector.NewCatalogue(b)
	if err != nil {
		t.Fatal(err)
	}
	rt := &connector.Runtime{Catalogue: cat, Secrets: e.api.deps.Connectors.Runtime.Secrets, Egress: &egress.Guard{Local: true}, Clock: e.clock}
	e.api.deps.Connectors = ConnectorDeps{Catalogue: cat, Secrets: e.api.deps.Connectors.Secrets, Runtime: rt}
	var c storage.InstalledConnector
	m := b.Manifest
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if c, err = u.Connectors().InstallOrganisation(ctx, storage.NewConnector{Connector: m.ID, ManifestVersion: m.Version, ManifestHash: m.Hash(),
			SecretNames: []string{"token"}, Consent: m.Consent(), Position: 1, At: e.clock.Now()}); err != nil {
			return err
		}
		for _, step := range []struct{ s, state string }{{storage.StepConfigure, storage.ConnectorConfigured}, {storage.StepTest, storage.ConnectorTested},
			{storage.StepEnable, storage.ConnectorEnabled}} {
			if step.s == storage.StepTest {
				c.Test = &storage.ConnectorTest{Passed: true, Reason: "Read.", At: e.clock.Now()}
			}
			if c, err = u.Connectors().ChangeOrganisation(ctx, c.ID, c.Version, storage.ConnectorChange{Step: step.s, State: step.state,
				Configuration: map[string]any{}, Position: 1, Test: c.Test, ManifestVersion: m.Version, ManifestHash: m.Hash(), SecretNames: []string{"token"},
				Consent: m.Consent(), At: e.clock.Now()}); err != nil {
				return err
			}
		}
		return nil
	})
	clearing := e.fedAccount("SGD", c)
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.PSP().SaveAccount(ctx, storage.NewPSPAccount{MoneyAccountID: clearing.ID, ConnectorID: c.ID, FeeAccountID: e.books.Rent.ID, At: e.clock.Now()}, 0)
		return err
	})
	worker, err := postgres.Open(context.Background(), db.URL("fyniq_worker"))
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close()
	run := func(row storage.OutboxRow) error {
		return paymentlinkjob.Maker{Store: worker, Runtime: rt, Clock: e.clock}.Work()(context.Background(), e.books.Org, row)
	}
	// Off: nothing is queued.
	e.issuedInvoice("1", "100.00")
	if jobs := e.queuedLinkJobs(); len(jobs) != 0 {
		t.Fatalf("with the setting off: %d jobs", len(jobs))
	}
	// On: the invoice is issued, the provider is asked nothing, and one job waits.
	const key = "/v1/organisation/settings/invoices.payment_link_on_issue"
	acc := e.as(authz.Administrator)
	if r := e.call(http.MethodPatch, key, acc, map[string]any{"value": "true"}, "If-Match", e.call(http.MethodGet, key, acc, nil).Header.Get("ETag")); r.Status >= 300 {
		t.Fatalf("switching it on: %d %s", r.Status, r.Raw)
	}
	id, tag := e.issuedInvoice("1", "100.00")
	jobs := e.queuedLinkJobs()
	if len(jobs) != 1 || len(f.requests) != 0 {
		t.Fatalf("after issuing: %d jobs, the provider asked %d times", len(jobs), len(f.requests))
	}
	if err := run(jobs[0]); err != nil {
		t.Fatalf("the job: %v", err)
	}
	if len(f.requests) != 1 || f.requests[0].Amount != "109.00" || !strings.HasPrefix(f.requests[0].Reference, "INV-") {
		t.Fatalf("the provider was asked %+v", f.requests)
	}
	link := e.call(http.MethodGet, "/v1/invoices/"+id+"/payment-link", e.as(authz.Accountant), nil)
	if link.Status != 200 || link.str("status") != "live" || !strings.HasPrefix(link.str("url"), "https://") || link.str("amount", "amount") != "109.00" {
		t.Fatalf("the link: %d %s", link.Status, link.Raw)
	}
	// A second run asks for nothing: the invoice has a live link already.
	if err := run(jobs[0]); err != nil || len(f.requests) != 1 {
		t.Errorf("a second run: %v, asked %d times", err, len(f.requests))
	}
	// The invoice's email carries the link.
	if r := e.call(http.MethodPost, "/v1/invoices/"+id+"/email", e.as(authz.Accountant), nil, "If-Match", tag, "Idempotency-Key", uuid.NewString()); r.Status != 200 {
		t.Fatalf("emailInvoice: %d %s", r.Status, r.Raw)
	}
	found := false
	for _, text := range e.queuedEmailTexts() {
		found = found || strings.Contains(text, "You can pay this invoice online: "+link.str("url"))
	}
	if !found {
		t.Error("the email does not carry the link")
	}
	// The invoice's PDF, as downloaded, says where to pay (FR-AR-13, rev 0.597); with the PDF setting off it does not; an invoice without a link has no such line.
	pdfText := func(invoice string) string {
		doc := e.call(http.MethodGet, "/v1/invoices/"+invoice+"/document", e.as(authz.Accountant), nil)
		raw, err := base64.StdEncoding.DecodeString(doc.str("pdf"))
		if doc.Status != 200 || err != nil {
			t.Fatalf("the document: %d %s", doc.Status, doc.Raw)
		}
		text, err := pdftext.Text(raw)
		if err != nil {
			t.Fatal(err)
		}
		return strings.Join(strings.Fields(text), "")
	}
	compact := func(x string) string { return strings.Join(strings.Fields(x), "") }
	if text := pdfText(id); !strings.Contains(text, compact("Pay online: "+link.str("url"))) {
		t.Errorf("the PDF does not carry the link %s", link.str("url"))
	}
	const pdfKey = "/v1/organisation/settings/invoices.payment_link_in_pdf"
	if r := e.call(http.MethodPatch, pdfKey, acc, map[string]any{"value": "false"}, "If-Match", e.call(http.MethodGet, pdfKey, acc, nil).Header.Get("ETag")); r.Status >= 300 {
		t.Fatalf("switching the PDF link off: %d %s", r.Status, r.Raw)
	}
	if text := pdfText(id); strings.Contains(text, compact(link.str("url"))) {
		t.Error("the PDF carries the link with the setting off")
	}
	// A provider that refuses for good leaves no link and no error.
	f.refuse = true
	other, _ := e.issuedInvoice("1", "70.00")
	jobs = e.queuedLinkJobs()
	if len(jobs) != 1 {
		t.Fatalf("the second invoice's job: %d", len(jobs))
	}
	if err := run(jobs[0]); err != nil {
		t.Errorf("a refusing provider: %v", err)
	}
	if r := e.call(http.MethodGet, "/v1/invoices/"+other+"/payment-link", e.as(authz.Accountant), nil); r.Status != http.StatusNotFound {
		t.Errorf("a link for the refused request: %d %s", r.Status, r.Raw)
	}
}

// FR-AR-13 (rev 0.597): the link is asked for whichever way an invoice is issued: an invoice the scheduler issues from an approved recurring invoice and one a second person's approval issues each
// queue the job when the setting is on, a billing provider's own invoice (already collected) queues none, and a preview posts nothing and queues nothing.
// req: FR-AR-13
// req: FR-AR-07
// asvs: V2.3.1
func TestEveryPathThatIssuesAnInvoiceQueuesItsLinkJob(t *testing.T) {
	f := newFounders(t)
	e := f.e
	weiling := f.as(f.weiling, authz.Accountant, authz.Controller)
	ahkow := f.as(f.ahkow, authz.Accountant, authz.Controller)
	const key = "/v1/organisation/settings/invoices.payment_link_on_issue"
	acc := e.as(authz.Administrator)
	if r := e.call(http.MethodPatch, key, acc, map[string]any{"value": "true"}, "If-Match", e.call(http.MethodGet, key, acc, nil).Header.Get("ETag")); r.Status >= 300 {
		t.Fatalf("switching it on: %d %s", r.Status, r.Raw)
	}
	e.queuedLinkJobs() // start empty
	// The scheduler issues the approved recurring invoice and the worker's hook queues one job.
	body := e.recurringInvoice("Retainer", "800.00", monthly(1, "2026-10-01"))
	body["mode"] = "issue_and_send"
	c := e.call(http.MethodPost, "/v1/recurring-invoices", weiling, body)
	asked := e.call(http.MethodPost, "/v1/recurring-invoices/"+c.str("id")+"/approval", weiling, nil, "If-Match", c.Header.Get("ETag"))
	if a := f.decide(ahkow, asked.str("id"), "approve", hashOf(asked)); a.Status != 200 {
		t.Fatalf("approving it: %d %s", a.Status, a.Raw)
	}
	hook := paymentlinkjob.OnIssue(outbox.Dispatcher{Store: e.store, Queue: e.queue, Clock: e.clock})
	if run := e.makeInvoiceDraftsAs(time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC), hook); run.Issued != 1 {
		t.Fatalf("the scheduler: %+v", run)
	}
	if jobs := e.queuedLinkJobs(); len(jobs) != 1 {
		t.Fatalf("after the scheduler issued an invoice: %d link jobs", len(jobs))
	}
	// An invoice a person issues through the service, never through the API's own command, queues too (the approval path calls the same service); the provider's own invoice does not.
	id, _ := e.createInvoice("1", "50.00")
	svc := journal.Invoices{Store: e.store, AfterIssued: hook}
	if _, err := svc.Post(context.Background(), e.books.Org, uuid.MustParse(id)); err != nil {
		t.Fatal(err)
	}
	if jobs := e.queuedLinkJobs(); len(jobs) != 1 {
		t.Fatalf("after the service issued an invoice: %d link jobs", len(jobs))
	}
	provided, _ := e.createInvoice("1", "60.00")
	total, err := money.Parse("60.00", money.MustLookup("SGD"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PostProvided(context.Background(), e.books.Org, uuid.MustParse(provided), total); err != nil {
		t.Fatalf("the provider's invoice: %v", err)
	}
	if jobs := e.queuedLinkJobs(); len(jobs) != 0 {
		t.Fatalf("a billing provider's own invoice queued %d link jobs", len(jobs))
	}
}
