//go:build local

package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/email"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

const mailFrom = "Fyniq <no-reply@fyniq.test>"

// fakeMail is an email provider whose answers a test sets: send gives the nth send's error (1-based), status
// what the provider says of a message; without them every send is accepted and the provider cannot say. name
// names it: "fake" when empty.
type fakeMail struct {
	mu     sync.Mutex
	name   string
	sent   []ports.OutboundEmail
	send   func(n int) error
	status func() (ports.EmailReceipt, error)
}

func (f *fakeMail) Name() string {
	if f.name == "" {
		return "fake"
	}
	return f.name
}

func (f *fakeMail) Send(_ context.Context, m ports.OutboundEmail) (ports.EmailReceipt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, m)
	if f.send != nil {
		if err := f.send(len(f.sent)); err != nil {
			return ports.EmailReceipt{}, err
		}
	}
	return ports.EmailReceipt{ProviderID: "<" + m.Key + "@" + f.Name() + ">"}, nil
}

func (f *fakeMail) Status(context.Context, ports.OutboundEmail, time.Time) (ports.EmailReceipt, error) {
	if f.status == nil {
		return ports.EmailReceipt{}, ports.ErrEmailUnknown
	}
	return f.status()
}

func (f *fakeMail) sends() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

// mailHarness is an outbox whose worker sends email through fs, in order.
func mailHarness(t *testing.T, fs ...*fakeMail) (*harness, outbox.Work) {
	t.Helper()
	kinds := map[string]outbox.Work{}
	h := newHarness(t, kinds)
	var providers []ports.Email
	for _, f := range fs {
		providers = append(providers, f)
	}
	kinds[email.Kind] = email.Work(providers, store(t, "fyniq_worker"), mailFrom, h.clock)
	return h, kinds[email.Kind]
}

// queueMail queues a test message in org and waits for the worker to run its send.
func queueMail(t *testing.T, h *harness, org uuid.UUID) storage.EmailMessage {
	t.Helper()
	var m storage.EmailMessage
	if err := h.store.Do(context.Background(), org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		m, err = email.Queue(ctx, h.disp, u, org, "test", "accounts@example.com", "Fyniq test message", "Hello.\n")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	h.push.Wait()
	return m
}

func emailMessage(t *testing.T, org, id uuid.UUID) storage.EmailMessage {
	t.Helper()
	var m storage.EmailMessage
	if err := store(t, "fyniq_api").Do(context.Background(), org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		m, err = u.EmailMessages().Get(ctx, id)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return m
}

// actions lists the audit actions about object id.
func actions(t *testing.T, org, id uuid.UUID) []string {
	t.Helper()
	var out []string
	for _, s := range events(t, org) {
		var ev audit.Event
		if err := json.Unmarshal([]byte(s.Body), &ev); err != nil {
			t.Fatal(err)
		}
		if ev.Object.ID == id {
			out = append(out, ev.Action)
		}
	}
	return out
}

// A queued message is sent once by the worker, from the platform's sender, under its own key; the record
// shows the provider's acceptance, and a second run of the same job sends nothing. Queueing and the outcome
// are audited.
// req: FR-INT-21
// req: FR-NTF-05
func TestAnEmailIsSentOnceThroughTheOutbox(t *testing.T) {
	b := books(t, "Email once Pte Ltd")
	f := &fakeMail{}
	h, work := mailHarness(t, f)
	m := queueMail(t, h, b.Org)

	got := emailMessage(t, b.Org, m.ID)
	if got.Status != storage.EmailAccepted || got.Provider != "fake" || got.ProviderID != "<"+m.ID.String()+"@fake>" || got.Attempts != 1 ||
		got.AcceptedAt == nil || !got.AcceptedAt.Equal(h.clock.Now()) {
		t.Fatalf("after the worker ran: %+v", got)
	}
	if f.sends() != 1 {
		t.Fatalf("%d sends; want 1", f.sends())
	}
	s := f.sent[0]
	if s.Key != m.ID.String() || s.From != mailFrom || len(s.To) != 1 || s.To[0] != "accounts@example.com" || s.Subject != "Fyniq test message" ||
		s.Text != "Hello.\n" {
		t.Fatalf("the send: %+v", s)
	}
	// The same job again, as a duplicate delivery would run it: the message has its outcome, so nothing is sent.
	payload := fmt.Sprintf(`{"message_id":%q,"to":"accounts@example.com","subject":"Fyniq test message","text":"Hello.\n"}`, m.ID)
	if err := work(context.Background(), b.Org, storage.OutboxRow{Payload: []byte(payload), MaxAttempts: 5, Attempts: 1}); err != nil {
		t.Fatal(err)
	}
	if f.sends() != 1 {
		t.Fatalf("a second run sent it again (%d sends)", f.sends())
	}
	if got := actions(t, b.Org, m.ID); strings.Join(got, ",") != "email_message.queued,email_message.accepted" {
		t.Fatalf("the message's audit trail: %v", got)
	}
}

// A send that may have handed the message over (the connection broke after the data) is reconciled before
// any retry: the provider's own answer decides, and when it cannot say, the message is unknown and never
// sent again on a guess. Only a provider that says it never took the message gets it again (G4-073).
// req: FR-INT-21
func TestAnEmailThatMayHaveBeenTakenIsNeverSentAgainOnAGuess(t *testing.T) {
	broken := func(n int) error {
		if n == 1 {
			return errors.New("finishing the message: connection reset by peer")
		}
		return nil
	}
	for name, c := range map[string]struct {
		status     func() (ports.EmailReceipt, error)
		want       string
		sends      int
		providerID string
	}{
		"the provider cannot say": {nil, storage.EmailUnknown, 1, ""},
		"the provider took it": {func() (ports.EmailReceipt, error) { return ports.EmailReceipt{ProviderID: "pm-42"}, nil },
			storage.EmailAccepted, 1, "pm-42"},
		"the provider never took it": {func() (ports.EmailReceipt, error) { return ports.EmailReceipt{}, ports.ErrEmailRefused },
			storage.EmailAccepted, 2, ""},
	} {
		t.Run(name, func(t *testing.T) {
			b := books(t, "Email "+name+" Pte Ltd")
			f := &fakeMail{send: broken, status: c.status}
			h, _ := mailHarness(t, f)
			m := queueMail(t, h, b.Org)
			got := emailMessage(t, b.Org, m.ID)
			if got.Status != c.want || f.sends() != c.sends {
				t.Fatalf("%s after %d sends; want %s after %d (%+v)", got.Status, f.sends(), c.want, c.sends, got)
			}
			if c.providerID != "" && got.ProviderID != c.providerID {
				t.Fatalf("the provider's reference is %q; want %q", got.ProviderID, c.providerID)
			}
			if c.want == storage.EmailUnknown && !strings.Contains(got.LastError, "not sent again") {
				t.Fatalf("an unknown message says %q", got.LastError)
			}
		})
	}
}

// A provider's refusal is retried by the outbox; on the job's last attempt the message is failed with the
// provider's answer, so the record says it was never sent.
// req: FR-INT-21
// req: FR-NTF-05
func TestARefusedEmailIsRetriedThenFailed(t *testing.T) {
	b := books(t, "Email refused Pte Ltd")
	f := &fakeMail{send: func(int) error { return fmt.Errorf("%w: 554 5.7.1 relay denied", ports.ErrEmailRefused) }}
	h, _ := mailHarness(t, f)
	m := queueMail(t, h, b.Org)
	// The local push stops after 4 deliveries; the outbox's sweep delivers the row's last attempt.
	var row uuid.UUID
	if err := asOrg(context.Background(), conn(t, "fyniq_worker"), b.Org, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), "SELECT id FROM fyniq.outbox WHERE business_key = $1", "email:"+m.ID.String()).Scan(&row)
	}); err != nil {
		t.Fatal(err)
	}
	token, _ := h.tokens.Token("worker")
	h.deliver(t, "worker", token, b.Org, row)
	got := emailMessage(t, b.Org, m.ID)
	if got.Status != storage.EmailFailed || got.Attempts != 5 || f.sends() != 5 || !strings.Contains(got.LastError, "554 5.7.1 relay denied") {
		t.Fatalf("after %d sends: %+v; want failed after 5", f.sends(), got)
	}
	if got := actions(t, b.Org, m.ID); strings.Join(got, ",") != "email_message.queued,email_message.failed" {
		t.Fatalf("the message's audit trail: %v", got)
	}
}

// The database keeps the record honest: an outcome never changes, the API only queues, the worker only
// records attempts and outcomes, and nobody deletes a message.
// req: FR-INT-21
func TestAnEmailsOutcomeIsFinalAndOnlyTheWorkerRecordsIt(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Email record Pte Ltd")
	h, _ := mailHarness(t, &fakeMail{})
	m := queueMail(t, h, b.Org)
	if got := emailMessage(t, b.Org, m.ID); got.Status != storage.EmailAccepted {
		t.Fatalf("the message is %s", got.Status)
	}
	err := store(t, "fyniq_worker").Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.EmailMessages().Finish(ctx, m.ID, storage.EmailOutcome{Status: storage.EmailFailed, Provider: "fake", LastError: "late"})
		return err
	})
	if storage.CodeOf(err) != storage.CodeVersion {
		t.Fatalf("a second outcome: %v; want %s", err, storage.CodeVersion)
	}
	for _, c := range []struct{ role, what, stmt string }{
		{"fyniq_worker", "change an outcome", "UPDATE fyniq.email_message SET status = 'failed' WHERE id = $1"},
		{"fyniq_migrator", "change an outcome as the owner", "UPDATE fyniq.email_message SET last_error = 'edited' WHERE id = $1"},
		{"fyniq_api", "record an outcome as the API", "UPDATE fyniq.email_message SET status = 'accepted' WHERE id = $1"},
		{"fyniq_worker", "queue a message as the worker", "INSERT INTO fyniq.email_message (purpose, recipient, subject) SELECT 'test', 'x@example.com', 's' WHERE $1::uuid IS NOT NULL"},
		{"fyniq_api", "delete a message", "DELETE FROM fyniq.email_message WHERE id = $1"},
		{"fyniq_worker", "delete a message as the worker", "DELETE FROM fyniq.email_message WHERE id = $1"},
	} {
		err := asOrg(ctx, conn(t, c.role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.stmt, m.ID); return err })
		wantState(t, c.what, err, "FY130", "42501")
	}
}

// The failover sends only what the primary refused: a refusal moves the send on at once, and the record names the
// provider that took it; a send the primary may have taken is reconciled with the primary and never handed to the
// failover (FR-INT-21, PRD-001 rev 0.102).
// req: FR-INT-21
func TestTheFailoverSendsOnlyWhatThePrimaryRefused(t *testing.T) {
	t.Run("refused, then taken by the failover", func(t *testing.T) {
		b := books(t, "Email failover Pte Ltd")
		primary := &fakeMail{name: "zeptomail", send: func(int) error { return fmt.Errorf("%w: zeptomail answered 401 TM_4001 invalid key", ports.ErrEmailRefused) }}
		failover := &fakeMail{name: "resend"}
		h, _ := mailHarness(t, primary, failover)
		m := queueMail(t, h, b.Org)
		got := emailMessage(t, b.Org, m.ID)
		if got.Status != storage.EmailAccepted || got.Provider != "resend" || got.ProviderID != "<"+m.ID.String()+"@resend>" || got.Attempts != 2 {
			t.Fatalf("after the primary refused: %+v", got)
		}
		if primary.sends() != 1 || failover.sends() != 1 || failover.sent[0].Key != m.ID.String() {
			t.Fatalf("%d sends to the primary and %d to the failover; want one each, under the message's key", primary.sends(), failover.sends())
		}
	})
	t.Run("may have been taken by the primary", func(t *testing.T) {
		b := books(t, "Email no failover Pte Ltd")
		primary := &fakeMail{name: "zeptomail", send: func(int) error { return errors.New("zeptomail: no answer, so the message may have been taken") }}
		failover := &fakeMail{name: "resend"}
		h, _ := mailHarness(t, primary, failover)
		m := queueMail(t, h, b.Org)
		got := emailMessage(t, b.Org, m.ID)
		if got.Status != storage.EmailUnknown || got.Provider != "zeptomail" || failover.sends() != 0 || primary.sends() != 1 {
			t.Fatalf("%+v after %d sends to the primary and %d to the failover; want unknown, the failover never asked", got, primary.sends(), failover.sends())
		}
	})
	t.Run("refused by both, then failed", func(t *testing.T) {
		b := books(t, "Email both refused Pte Ltd")
		no := func(int) error { return fmt.Errorf("%w: 422 recipient rejected", ports.ErrEmailRefused) }
		primary, failover := &fakeMail{name: "zeptomail", send: no}, &fakeMail{name: "resend", send: no}
		h, _ := mailHarness(t, primary, failover)
		m := queueMail(t, h, b.Org)
		var row uuid.UUID
		if err := asOrg(context.Background(), conn(t, "fyniq_worker"), b.Org, func(tx pgx.Tx) error {
			return tx.QueryRow(context.Background(), "SELECT id FROM fyniq.outbox WHERE business_key = $1", "email:"+m.ID.String()).Scan(&row)
		}); err != nil {
			t.Fatal(err)
		}
		token, _ := h.tokens.Token("worker")
		h.deliver(t, "worker", token, b.Org, row)
		got := emailMessage(t, b.Org, m.ID)
		if got.Status != storage.EmailFailed || primary.sends() != 5 || failover.sends() != 5 ||
			!strings.Contains(got.LastError, "zeptomail: ") || !strings.Contains(got.LastError, "resend: ") {
			t.Fatalf("after %d and %d sends: %+v; want failed, naming both", primary.sends(), failover.sends(), got)
		}
	})
}

// A message is sent from the organisation's sender of its purpose (a sign-in alert from security's), or the
// platform's while it has none; a choice of no address returns to the platform's. Each choice is audited.
// req: FR-INT-21
func TestAnEmailIsSentFromTheOrganisationsSenderOfItsPurpose(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Email sender Pte Ltd")
	f := &fakeMail{}
	h, _ := mailHarness(t, f)
	set := func(purpose, address, name string) storage.EmailSender {
		var c storage.EmailSender
		if err := store(t, "fyniq_api").Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			var err error
			c, err = u.EmailSenders().Set(ctx, purpose, address, name)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return c
	}
	send := func(purpose string) ports.OutboundEmail {
		n := f.sends()
		if err := h.store.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := email.Queue(ctx, h.disp, u, b.Org, purpose, "founder@example.com", "Hello", "Hello.\n")
			return err
		}); err != nil {
			t.Fatal(err)
		}
		h.push.Wait()
		if f.sends() != n+1 {
			t.Fatalf("%d sends; want %d", f.sends(), n+1)
		}
		return f.sent[n]
	}
	if got := send("test").From; got != mailFrom {
		t.Fatalf("with no sender chosen, from %q; want the platform's", got)
	}
	c := set(storage.SenderNotifications, "accounts@example.com", "Email Sender Pte Ltd")
	set(storage.SenderSecurity, "security@example.com", "")
	if got, want := send("test").From, `"Email Sender Pte Ltd" <accounts@example.com>`; got != want {
		t.Fatalf("a notification from %q; want %q", got, want)
	}
	if got, want := send("sign_in_alert").From, "<security@example.com>"; got != want {
		t.Fatalf("a sign-in alert from %q; want security's, %q", got, want)
	}
	set(storage.SenderNotifications, "", "")
	if got := send("test").From; got != mailFrom {
		t.Fatalf("after returning to the platform's sender, from %q", got)
	}
	if got := actions(t, b.Org, c.ID); strings.Join(got, ",") != "email_sender.set" {
		t.Fatalf("the choice's audit trail: %v", got)
	}
}

// A sender choice never changes or goes (FY179): the API records one, the worker only reads them.
// req: FR-INT-21
func TestASenderChoiceIsAppendOnly(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Email sender record Pte Ltd")
	var c storage.EmailSender
	if err := store(t, "fyniq_api").Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		c, err = u.EmailSenders().Set(ctx, storage.SenderInvoices, "billing@example.com", "Billing")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, x := range []struct{ role, what, stmt string }{
		{"fyniq_migrator", "change a choice as the owner", "UPDATE fyniq.email_sender SET address = 'x@example.com' WHERE id = $1"},
		{"fyniq_migrator", "delete a choice as the owner", "DELETE FROM fyniq.email_sender WHERE id = $1"},
		{"fyniq_api", "change a choice", "UPDATE fyniq.email_sender SET address = 'x@example.com' WHERE id = $1"},
		{"fyniq_worker", "choose a sender as the worker", "INSERT INTO fyniq.email_sender (purpose, set_by, set_at) SELECT 'invoices', 'user:x', now() WHERE $1::uuid IS NOT NULL"},
	} {
		err := asOrg(ctx, conn(t, x.role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, x.stmt, c.ID); return err })
		wantState(t, x.what, err, "FY179", "42501")
	}
}
