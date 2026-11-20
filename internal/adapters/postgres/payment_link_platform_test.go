//go:build local

package postgres_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The narrow function (FR-AR-13, rev 0.588) records a live payment link for the platform's job and nothing else: only for a posted invoice and an enabled connector of the organisation (not another's,
// not one that is installed but not enabled), an https URL, and one live link per invoice; the worker holds no right on the link table, so the function is the only way in, and the audit event must
// be in the same transaction.
// req: FR-AR-13
// asvs: V4.2.1
func TestTheNarrowFunctionRecordsALiveLinkOnlyForAPostedInvoiceAndAnEnabledConnector(t *testing.T) {
	ctx := context.Background()
	api, worker := store(t, "fyniq_api"), store(t, "fyniq_worker")
	books := porttest.NewBooks(t, api, "Link Pte Ltd")
	other := porttest.NewBooks(t, api, "Another link Pte Ltd")
	at := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	enabled := func(org uuid.UUID) uuid.UUID {
		id := pspConnector(t, org)
		if err := api.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			c, err := u.Connectors().Organisation(ctx)
			if err != nil {
				return err
			}
			var cur storage.InstalledConnector
			for _, x := range c {
				if x.ID == id {
					cur = x
				}
			}
			for _, step := range []struct{ s, state string }{{storage.StepConfigure, storage.ConnectorConfigured}, {storage.StepTest, storage.ConnectorTested}, {storage.StepEnable, storage.ConnectorEnabled}} {
				if step.s == storage.StepTest {
					cur.Test = &storage.ConnectorTest{Passed: true, Reason: "Read.", At: at}
				}
				if cur, err = u.Connectors().ChangeOrganisation(ctx, cur.ID, cur.Version, storage.ConnectorChange{Step: step.s, State: step.state, Configuration: map[string]any{}, Position: 1,
					Test: cur.Test, ManifestVersion: "1.0.0", ManifestHash: strings.Repeat("a", 64), SecretNames: []string{"api_key"}, At: at}); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	mine, theirs := enabled(books.Org), enabled(other.Org)
	var idle uuid.UUID
	if err := api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		c, err := u.Connectors().InstallOrganisation(ctx, storage.NewConnector{Connector: "hitpay", ManifestVersion: "1.0.0", ManifestHash: strings.Repeat("b", 64), SecretNames: []string{"api_key"}, Position: 2, At: at})
		idle = c.ID
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var posted, draft storage.Document
	if err := api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if posted, err = u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentInvoice, Payload: []byte(`{}`)}); err != nil {
			return err
		}
		if err = u.Documents().SetStatus(ctx, posted.ID, storage.DocumentPosted); err != nil {
			return err
		}
		draft, err = u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentInvoice, Payload: []byte(`{}`)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	link := func(org uuid.UUID, inv, conn uuid.UUID, request, url string) error {
		wctx := storage.WithAudit(ctx, storage.AuditContext{Actor: &storage.Actor{Type: "scheduler", ID: "payment_link"}})
		return worker.Do(wctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.PaymentLinks().CreateByFunction(ctx, storage.NewPaymentLink{ConnectorID: conn, InvoiceID: inv, ProviderRequestID: request, URL: url, Reference: "INV-1", Currency: "SGD", Amount: "100.00", At: at})
			return err
		})
	}
	for name, c := range map[string]struct {
		org      uuid.UUID
		inv, con uuid.UUID
		url      string
	}{
		"a draft invoice":           {books.Org, draft.ID, mine, "https://pay.example/1"},
		"a connector not enabled":   {books.Org, posted.ID, idle, "https://pay.example/2"},
		"another organisation's":    {books.Org, posted.ID, theirs, "https://pay.example/3"},
		"a document that is no one": {books.Org, uuid.New(), mine, "https://pay.example/4"},
		"a plain http URL":          {books.Org, posted.ID, mine, "http://pay.example/5"},
	} {
		if err := link(c.org, c.inv, c.con, "pr_"+uuid.NewString()[:6], c.url); err == nil {
			t.Errorf("%s was recorded", name)
		}
	}
	if err := link(books.Org, posted.ID, mine, "pr_ok", "https://pay.example/ok"); err != nil {
		t.Fatalf("the link: %v", err)
	}
	if err := link(books.Org, posted.ID, mine, "pr_second", "https://pay.example/second"); err == nil {
		t.Error("a second live link was recorded")
	}
	// Without the audit event the database refuses, whatever the function is given.
	c := conn(t, "fyniq_worker")
	if err := asOrg(ctx, c, books.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "SELECT fyniq.create_payment_link_by_platform($1, $2, 'pr_raw', 'https://pay.example/raw', 'INV-9', 'SGD', 10, NULL)", posted.ID, mine)
		return err
	}); err == nil {
		t.Error("a link with no audit event was recorded")
	}
	// The worker holds no right on the table.
	c = conn(t, "fyniq_worker")
	if err := asOrg(ctx, c, books.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "INSERT INTO fyniq.payment_link (connector_id, invoice_id, provider_request_id, url, reference, currency, amount, created_by, created_at) VALUES ($1, $2, 'x', 'https://x.example', 'r', 'SGD', 1, 'user:x', now())", mine, posted.ID)
		return err
	}); err == nil {
		t.Error("the worker inserted into the link table")
	}
}
