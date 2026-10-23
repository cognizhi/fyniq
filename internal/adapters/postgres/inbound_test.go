//go:build local

package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/email"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// fakeInbound is a receiving provider with one message and its attachments by ID.
type fakeInbound struct {
	mail  ports.InboundEmail
	files map[string][]byte
	err   error
}

func (f *fakeInbound) Name() string { return "resend" }

func (f *fakeInbound) Fetch(context.Context, string) (ports.InboundEmail, error) {
	return f.mail, f.err
}

func (f *fakeInbound) Attachment(_ context.Context, _ string, a ports.InboundAttachment, limit int64) ([]byte, error) {
	b := f.files[a.ID]
	if int64(len(b)) > limit {
		return nil, ports.ErrTooLarge
	}
	return b, nil
}

// inboundAt makes an email-in address in org, for member when not nil, records a message from sender at it as the
// webhook does, and returns the message.
func inboundAt(t *testing.T, org uuid.UUID, member *uuid.UUID, sender string) storage.InboundMessage {
	t.Helper()
	var m storage.InboundMessage
	if err := store(t, "fyniq_api").Do(context.Background(), org, func(ctx context.Context, u storage.UnitOfWork) error {
		live, err := u.CaptureAddresses().Live(ctx)
		if err != nil {
			return err
		}
		var addr storage.CaptureAddress
		for _, a := range live {
			if (a.UserID == nil) == (member == nil) {
				addr = a
			}
		}
		if addr.ID == uuid.Nil {
			if addr, err = u.CaptureAddresses().Create(ctx, "in-"+uuid.NewString()[:8], member); err != nil {
				return err
			}
		}
		m, _, err = u.InboundMessages().Receive(ctx, storage.NewInboundMessage{AddressID: addr.ID, Provider: "resend", ProviderID: uuid.NewString(),
			Sender: sender, Subject: "Invoice INV-0042", ReceivedAt: clock.System{}.Now()})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return m
}

// fetch runs the worker's fetch of m with provider p, keeping files in dir.
func fetch(t *testing.T, org uuid.UUID, m storage.InboundMessage, p ports.InboundMail, dir ports.BlobStore, attempts int) error {
	t.Helper()
	f := email.Fetcher{Mail: p, Store: store(t, "fyniq_worker"), Files: dir, Scanner: local.EICARScanner{}, Clock: clock.System{}}
	payload, _ := json.Marshal(map[string]any{"message_id": m.ID})
	return f.Work()(context.Background(), org, storage.OutboxRow{Payload: payload, Attempts: attempts, MaxAttempts: 5})
}

func inbound(t *testing.T, org, id uuid.UUID) (storage.InboundMessage, []storage.InboundFile) {
	t.Helper()
	var m storage.InboundMessage
	var files []storage.InboundFile
	if err := store(t, "fyniq_api").Do(context.Background(), org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		if m, err = u.InboundMessages().Get(ctx, id); err != nil {
			return err
		}
		files, err = u.InboundMessages().Files(ctx, id)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return m, files
}

// A member's verified sign-in email is kept on their identity, and is theirs among the organisation's members' emails;
// a contact's billing and remittance emails are the organisation's contacts' emails.
// req: FR-INT-22
func TestMembersAndContactsAreKnownSenders(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Inbound known Pte Ltd")
	api := store(t, "fyniq_api")
	member := founder(t, api, b.Org, 51, clock.System{}.Now())
	var issuer, subject string
	if err := asOrg(ctx, conn(t, "fyniq_migrator"), b.Org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT issuer, subject FROM fyniq.user_identity WHERE user_id = $1", member).Scan(&issuer, &subject)
	}); err != nil {
		t.Fatal(err)
	}
	if err := api.RecordIdentityEmail(ctx, issuer, subject, "Mei@Example.com", clock.System{}.Now()); err != nil {
		t.Fatal(err)
	}
	if err := api.RecordIdentityEmail(ctx, "urn:nobody", "nobody", "x@example.com", clock.System{}.Now()); err != nil {
		t.Fatalf("an identity without a user: %v", err)
	}
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		if _, err := u.Contacts().Create(ctx, storage.ContactDetails{LegalName: "Supplier Pte Ltd", Vendor: true, BillingEmail: "AR@Supplier.example",
			RemittanceEmail: "pay@supplier.example", Country: "SG", Currency: "SGD"}); err != nil {
			return err
		}
		emails, err := u.InboundMessages().MemberEmails(ctx)
		if err != nil || emails[member] != "mei@example.com" {
			t.Errorf("the members' emails: %v, %v", emails, err)
		}
		contacts, err := u.InboundMessages().ContactEmails(ctx)
		if err != nil || strings.Join(contacts, ",") != "ar@supplier.example,pay@supplier.example" {
			t.Errorf("the contacts' emails: %v, %v", contacts, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// The worker fetches a message and decides it: a contact's authenticated mail is accepted with its text and its
// attachments checked, scanned and kept; one the checks refuse is left out, saying why; a stranger's is quarantined for
// the organisation's days; a sender the domain does not vouch for is quarantined even when known; a fetch that keeps
// failing ends failed. A message fetched before is left as it is.
// req: FR-INT-22
func TestTheWorkerFetchesDecidesAndKeepsReceivedMail(t *testing.T) {
	b := books(t, "Inbound fetch Pte Ltd")
	if err := store(t, "fyniq_api").Do(context.Background(), b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Contacts().Create(ctx, storage.ContactDetails{LegalName: "Supplier Pte Ltd", Vendor: true, BillingEmail: "ar@supplier.example", Country: "SG", Currency: "SGD"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	dir, err := local.NewDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pdf := []byte("%PDF-1.7\n% an invoice\n")
	provider := &fakeInbound{mail: ports.InboundEmail{From: "ar@supplier.example", Subject: "Invoice", Text: "Please find our invoice.", SPF: "pass",
		DKIM: "pass", DMARC: "pass", Attachments: []ports.InboundAttachment{{ID: "a1", FileName: "INV-0042.pdf", ContentType: "application/pdf", Size: int64(len(pdf))},
			{ID: "a2", FileName: "virus.pdf", Size: 68}}},
		files: map[string][]byte{"a1": pdf, "a2": []byte(`X5O!P%@AP[4\PZX54(P^)7CC)7}$EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*`)}}

	m := inboundAt(t, b.Org, nil, "AR@Supplier.example")
	if err := fetch(t, b.Org, m, provider, dir, 1); err != nil {
		t.Fatal(err)
	}
	got, files := inbound(t, b.Org, m.ID)
	if got.Status != storage.InboundAccepted || got.DMARC != "pass" || got.ExpiresAt != nil || len(files) != 2 || files[0].Kind != storage.InboundBody ||
		files[1].FileName != "INV-0042.pdf" || files[1].MediaType != "application/pdf" || !strings.Contains(got.LastError, "virus.pdf") {
		t.Fatalf("a contact's message: %+v, files %+v", got, files)
	}
	if !strings.HasPrefix(files[1].BlobKey, "inbound/"+b.Org.String()+"/"+m.ID.String()+"/") {
		t.Fatalf("the blob key %s", files[1].BlobKey)
	}
	if err := fetch(t, b.Org, m, provider, dir, 1); err != nil {
		t.Fatalf("fetching it again: %v", err)
	}
	if again, f2 := inbound(t, b.Org, m.ID); again.Status != storage.InboundAccepted || len(f2) != 2 {
		t.Fatalf("a second fetch changed it: %+v", again)
	}

	provider.mail.From, provider.mail.Attachments = "someone@stranger.example", nil
	stranger := inboundAt(t, b.Org, nil, "someone@stranger.example")
	if err := fetch(t, b.Org, stranger, provider, dir, 1); err != nil {
		t.Fatal(err)
	}
	if got, _ := inbound(t, b.Org, stranger.ID); got.Status != storage.InboundQuarantined || got.ExpiresAt == nil ||
		!got.ExpiresAt.Equal(stranger.ReceivedAt.Add(30*24*time.Hour)) {
		t.Fatalf("a stranger's message: %+v", got)
	}

	provider.mail.DMARC = "fail"
	spoofed := inboundAt(t, b.Org, nil, "ar@supplier.example")
	if err := fetch(t, b.Org, spoofed, provider, dir, 1); err != nil {
		t.Fatal(err)
	}
	if got, _ := inbound(t, b.Org, spoofed.ID); got.Status != storage.InboundQuarantined {
		t.Fatalf("a contact's address failing DMARC: %s", got.Status)
	}

	provider.err = errors.New("resend receiving answered 503")
	broken := inboundAt(t, b.Org, nil, "ar@supplier.example")
	if err := fetch(t, b.Org, broken, provider, dir, 1); err == nil {
		t.Fatal("a failed fetch before the last attempt was not retried")
	}
	if err := fetch(t, b.Org, broken, provider, dir, 5); err != nil {
		t.Fatal(err)
	}
	if got, _ := inbound(t, b.Org, broken.ID); got.Status != storage.InboundFailed || !strings.Contains(got.LastError, "503") {
		t.Fatalf("a fetch that kept failing: %+v", got)
	}
}

// A quarantined message whose days have passed expires and its files go; nothing else changes. A message's status only
// moves forward and what it was received as never changes (FY185); a file is only ever removed, once (FY186); only the
// API receives and decides, only the worker fetches.
// req: FR-INT-22
func TestQuarantinedMailExpiresAndKeepsItsHistory(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Inbound expiry Pte Ltd")
	dir, err := local.NewDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	provider := &fakeInbound{mail: ports.InboundEmail{From: "x@stranger.example", Text: "Hello", DMARC: "pass"}}
	m := inboundAt(t, b.Org, nil, "x@stranger.example")
	if err := fetch(t, b.Org, m, provider, dir, 1); err != nil {
		t.Fatal(err)
	}
	_, files := inbound(t, b.Org, m.ID)
	if len(files) != 1 {
		t.Fatalf("%d files kept", len(files))
	}
	if n, err := email.ExpireAll(ctx, store(t, "fyniq_worker"), dir, m.ReceivedAt.Add(29*24*time.Hour)); err != nil || n != 0 {
		t.Fatalf("before its days passed: %d, %v", n, err)
	}
	if _, err := email.ExpireAll(ctx, store(t, "fyniq_worker"), dir, m.ReceivedAt.Add(31*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, files := inbound(t, b.Org, m.ID)
	if got.Status != storage.InboundExpired || files[0].RemovedAt == nil {
		t.Fatalf("after its days: %+v, %+v", got, files)
	}
	if _, err := dir.Get(ctx, files[0].BlobKey); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("the expired message's file is still kept: %v", err)
	}
	for _, x := range []struct{ role, what, stmt, state string }{
		{"fyniq_migrator", "change an expired message's status", "UPDATE fyniq.inbound_message SET status = 'accepted' WHERE id = $1", "FY185"},
		{"fyniq_migrator", "change who sent it", "UPDATE fyniq.inbound_message SET sender = 'y@example.com' WHERE id = $1", "FY185"},
		{"fyniq_migrator", "delete a message", "DELETE FROM fyniq.inbound_message WHERE id = $1", "FY185"},
		{"fyniq_migrator", "restore a removed file", "UPDATE fyniq.inbound_file SET removed_at = NULL WHERE message_id = $1", "FY186"},
		{"fyniq_migrator", "delete a file", "DELETE FROM fyniq.inbound_file WHERE message_id = $1", "FY186"},
		{"fyniq_worker", "receive as the worker", "INSERT INTO fyniq.inbound_message (address_id, provider, provider_id, sender, received_at) SELECT address_id, 'resend', 'x', 'a@b.c', now() FROM fyniq.inbound_message WHERE id = $1", "42501"},
		{"fyniq_api", "record a fetch as the API", "UPDATE fyniq.inbound_message SET spf = 'pass' WHERE id = $1", "42501"},
		{"fyniq_api", "keep a file as the API", "INSERT INTO fyniq.inbound_file (message_id, kind, file_name, media_type, size_bytes, sha256, original_sha256, blob_key, stored_at) SELECT $1, 'body', 'x', 'text/plain', 1, repeat('0', 64), repeat('0', 64), 'inbound/x', now()", "42501"},
	} {
		err := asOrg(ctx, conn(t, x.role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, x.stmt, m.ID); return err })
		wantState(t, x.what, err, x.state, x.state)
	}
}
