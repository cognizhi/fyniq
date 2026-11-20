//go:build local

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/connector"
	"github.com/cognizhi/fyniq/internal/domain/invoicenow"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/wiring"
)

// recordingEmail is a provider that notes a message handed to it.
type recordingEmail struct{ sent int }

func (r *recordingEmail) Name() string { return "recording" }
func (r *recordingEmail) Send(context.Context, ports.OutboundEmail) (ports.EmailReceipt, error) {
	r.sent++
	return ports.EmailReceipt{ProviderID: "r"}, nil
}
func (r *recordingEmail) Status(context.Context, ports.OutboundEmail, time.Time) (ports.EmailReceipt, error) {
	return ports.EmailReceipt{ProviderID: "r"}, nil
}

// Every adapter the connector runtime builds for a sandbox organisation is refused, its email goes to a sink, the InvoiceNow access point is never
// called, and an organisation that is not a sandbox one gets what it always did (FR-INT-18). The flag is read in the work's own unit of work, and
// the refusal comes before anything is looked up, so a connector that was installed before cannot send either.
// req: FR-INT-18
// asvs: V8.2.1, V4.2.1
func TestAttackASandboxOrganisationGetsNoOutboundAdapter(t *testing.T) {
	ctx := context.Background()
	real := books(t, "Sandbox runtime Pte Ltd").Org
	st := store(t, "fyniq_worker")
	creator := founder(t, store(t, "fyniq_api"), real, 71, clock.System{}.Now().UTC().Truncate(time.Microsecond))
	sandbox := placeSandbox(t, creator)
	now := clock.System{}.Now()
	rt := &connector.Runtime{}
	id := uuid.New()
	refused := func(name string, org uuid.UUID, run func(u storage.UnitOfWork) error) error {
		t.Helper()
		return st.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error { return run(u) })
	}
	for name, run := range map[string]func(u storage.UnitOfWork) error{
		"bank data": func(u storage.UnitOfWork) error {
			_, _, err := rt.BankData(ctx, u, id, connector.SyncSchedule)
			return err
		},
		"a payment provider": func(u storage.UnitOfWork) error {
			_, _, err := rt.PaymentProvider(ctx, u, id, connector.ProviderWork)
			return err
		},
		"payment links": func(u storage.UnitOfWork) error {
			_, _, err := rt.PaymentLinks(ctx, u, id, connector.LinkRequest)
			return err
		},
		"chat": func(u storage.UnitOfWork) error {
			_, _, err := rt.Messaging(ctx, u, id, connector.ChatRule)
			return err
		},
		"inbound email": func(u storage.UnitOfWork) error {
			_, _, err := rt.InboundEmail(ctx, u, connector.OutboxEmail)
			return err
		},
		"an AI model": func(u storage.UnitOfWork) error {
			_, err := rt.Model(ctx, u, storage.AIConnection{Connector: "openai", Host: "api.openai.com"})
			return err
		},
	} {
		if err := refused(name, sandbox, run); !errors.Is(err, connector.ErrSandbox) {
			t.Errorf("%s for a sandbox organisation: %v", name, err)
		}
		if name == "an AI model" { // a real organisation's model needs a catalogue: the point here is the sandbox's refusal
			continue
		}
		if err := refused(name, real, run); errors.Is(err, connector.ErrSandbox) {
			t.Errorf("%s for a real organisation is refused as a sandbox", name)
		}
	}
	// Outbound email: a sink for the sandbox, whatever providers the deployment holds; the deployment's for a real organisation.
	rec := &recordingEmail{}
	kit := &wiring.Kit{Store: st, Mail: wiring.MailConfig{Providers: []ports.Email{rec}}}
	providers := kit.EmailProviders(rt)
	set, err := providers(ctx, sandbox)
	if err != nil || len(set.Enabled) != 1 || set.Enabled[0].Name() != "sandbox" {
		t.Fatalf("a sandbox organisation's email providers: %+v %v", set, err)
	}
	if _, err := set.Enabled[0].Send(ctx, ports.OutboundEmail{To: []string{"someone@example.com"}}); err != nil || rec.sent != 0 {
		t.Fatalf("a sandbox organisation's email was sent: %v (%d sent)", err, rec.sent)
	}
	if set, err = providers(ctx, real); err != nil || len(set.Enabled) != 1 || set.Enabled[0].Name() != "recording" {
		t.Fatalf("a real organisation's email providers: %+v %v", set, err)
	}
	if err := st.Do(ctx, sandbox, func(ctx context.Context, u storage.UnitOfWork) error {
		m, err := rt.OutboundEmail(ctx, u, connector.OutboxEmail)
		if err != nil || len(m.Enabled) != 1 || m.Enabled[0].Name() != "sandbox" {
			t.Errorf("the runtime's outbound email: %+v %v", m, err)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// InvoiceNow: nothing is sent, refreshed or polled for a sandbox organisation, and a nil access point shows none is touched.
	if err := st.Do(ctx, sandbox, func(ctx context.Context, u storage.UnitOfWork) error {
		sub := storage.InvoiceSubmission{ID: uuid.New(), Status: "sent", TransmissionID: "t-1"}
		if got, err := invoicenow.Refresh(ctx, u, nil, sub, now); err != nil || got.Status != "sent" {
			t.Errorf("a refresh: %+v %v", got, err)
		}
		if got, err := invoicenow.Receive(ctx, u, nil, now); err != nil || len(got) != 0 {
			t.Errorf("a poll: %+v %v", got, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
