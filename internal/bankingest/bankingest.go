// Package bankingest imports the banks' emailed reports (PRD-001 FR-BNK-17; p1-bank-import-reconcile task 7.2, design
// D10): a message accepted at the bank-ingest address is imported by the worker through the money account whose email
// source names its sender, each attachment as a statement with the continuity and duplicate checks. The worker holds no
// posting right, so the lines wait in the queue for a person's Reconcile now (FR-BNK-14; the owner's decision of 26 Oct
// 2026, rev 0.143). A message with no attachment is read as an incoming-funds alert (FR-BNK-16, UC-BNK-02; task 7.3):
// a pending signal pointing to the invoice it most likely pays, posting nothing.
package bankingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/bankfile"
	"github.com/cognizhi/fyniq/internal/domain/banking"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Kind is the outbox kind of importing an accepted report, run by the worker class.
const Kind = "bank.ingest"

// Actor is the principal that imports the banks' emailed reports.
var Actor = storage.Actor{Type: "service", ID: "bank.ingest"}

// The audit actions of a report that imports nothing, so a person can see why in the trail.
const (
	ActionUnmatched = "bank_report.unmatched" // no money account's email source names the sender
	ActionRefused   = "bank_report.refused"   // the file does not read, or is not for the account, or the alert does not read
)

type payload struct {
	MessageID uuid.UUID `json:"message_id"`
}

// Queue queues the import of an accepted message at a bank-ingest address, in u; any other message is left.
func Queue(ctx context.Context, d outbox.Dispatcher, u storage.UnitOfWork, org uuid.UUID, m storage.InboundMessage, addr storage.CaptureAddress) error {
	if addr.Kind != storage.CaptureBank {
		return nil
	}
	b, err := json.Marshal(payload{MessageID: m.ID})
	if err != nil {
		return err
	}
	_, err = d.Enqueue(ctx, u, org, storage.NewOutboxItem{Class: storage.ClassWorker, Kind: Kind, Payload: b, BusinessKey: "bank-report:" + m.ID.String()})
	return err
}

// Accepted is email.Fetcher's hook for a message it accepted: it queues the import of one at a bank-ingest address.
func Accepted(d outbox.Dispatcher) func(ctx context.Context, u storage.UnitOfWork, org uuid.UUID, m storage.InboundMessage, addr storage.CaptureAddress, at time.Time) error {
	return func(ctx context.Context, u storage.UnitOfWork, org uuid.UUID, m storage.InboundMessage, addr storage.CaptureAddress, _ time.Time) error {
		return Queue(ctx, d, u, org, m, addr)
	}
}

// Importer imports accepted reports from the staging store, where the fetch kept their files.
type Importer struct {
	Store storage.Store
	Files ports.BlobStore
	Clock ports.Clock
}

// source is the money account a sender reports for on a day, and how its reports read.
type source struct {
	account storage.MoneyAccount
	src     storage.MoneyAccountSource
	mapping *bankfile.Mapping
}

// sourceOf finds the money account whose email source in force on day, or set for a later day, names sender: the first
// by name when more than one does.
func sourceOf(ctx context.Context, u storage.UnitOfWork, sender string, day time.Time) (*source, error) {
	sender = strings.ToLower(sender)
	accounts, err := u.Banking().MoneyAccounts(ctx)
	if err != nil {
		return nil, err
	}
	for _, a := range accounts {
		if a.ClosedOn != nil && a.ClosedOn.Before(day) {
			continue
		}
		sources, err := u.Banking().Sources(ctx, a.ID) // earliest first
		if err != nil {
			return nil, err
		}
		from := 0
		for i, s := range sources {
			if !s.FromDate.After(day) {
				from = i
			}
		}
		for _, s := range sources[min(from, len(sources)):] {
			if s.Kind != storage.MoneySourceEmail || !slices.Contains(s.Senders, sender) {
				continue
			}
			out := &source{account: a, src: s}
			if s.MappingID != nil {
				all, err := u.Banking().Mappings(ctx)
				if err != nil {
					return nil, err
				}
				for _, m := range all {
					if m.ID == *s.MappingID {
						x, err := bankfile.ReadMapping(m.Spec)
						if err != nil {
							return nil, err
						}
						out.mapping = &x
					}
				}
			}
			return out, nil
		}
	}
	return nil, nil
}

// Work is the worker's handler of Kind:
//   - A message that is not accepted, or not at a bank-ingest address, imports nothing.
//   - A sender no money account's email source names imports nothing, recorded in the audit trail (ActionUnmatched).
//   - Each attachment is imported as statements of that account, in its own unit of work, through the source's format
//     and mapping, with the continuity and duplicate checks: a second run drops every line it added the first time. A
//     file that does not read is recorded (ActionRefused) and the others still import; a store failure is retried.
//   - A message with no attachment is an incoming-funds alert: its text is recorded as a pending signal on the account
//     (banking.RecordAlert), once per message; text that does not read as one is recorded (ActionRefused).
func (g Importer) Work() outbox.Work {
	return func(ctx context.Context, org uuid.UUID, row storage.OutboxRow) error {
		var p payload
		if err := json.Unmarshal(row.Payload, &p); err != nil || p.MessageID == uuid.Nil {
			return fmt.Errorf("bankingest: outbox row %s is not a report", row.ID)
		}
		ctx = storage.WithAudit(ctx, storage.AuditContext{Actor: &Actor, Reason: "The bank's emailed report (FR-BNK-17)"})
		var (
			m     storage.InboundMessage
			src   *source
			files []storage.InboundFile
			body  *storage.InboundFile
		)
		if err := g.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			var err error
			if m, err = u.InboundMessages().Get(ctx, p.MessageID); err != nil || m.Status != storage.InboundAccepted {
				return err
			}
			addr, err := u.CaptureAddresses().Get(ctx, m.AddressID)
			if err != nil || addr.Kind != storage.CaptureBank {
				return err // not a report: src stays nil, and nothing is imported
			}
			if src, err = sourceOf(ctx, u, m.Sender, m.ReceivedAt); err != nil {
				return err
			}
			if src == nil {
				return u.Audit().Record(ctx, storage.AuditRecord{Action: ActionUnmatched, ObjectType: "inbound_message", ObjectID: m.ID,
					Changes: map[string][2]any{"sender": {nil, m.Sender}}})
			}
			all, err := u.InboundMessages().Files(ctx, m.ID)
			for _, f := range all {
				switch {
				case f.RemovedAt != nil:
				case f.Kind == storage.InboundAttachment:
					files = append(files, f)
				case f.Kind == storage.InboundBody:
					body = &f
				}
			}
			return err
		}); err != nil {
			return err
		}
		if src == nil {
			return nil
		}
		if len(files) == 0 {
			return g.alert(ctx, org, m, src, body)
		}
		for _, f := range files {
			data, err := g.read(ctx, f.BlobKey)
			if err != nil {
				return err
			}
			err = g.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
				_, err := banking.Import(ctx, u, banking.ImportRequest{Format: src.src.ReportFormat, Data: data, Account: &src.account.ID,
					Mapping: src.mapping, Source: storage.MoneySourceEmail, FileRef: f.BlobKey, At: g.Clock.Now()})
				return err
			})
			var refusal *banking.Error
			if errors.As(err, &refusal) {
				err = g.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
					return u.Audit().Record(ctx, storage.AuditRecord{Action: ActionRefused, ObjectType: "inbound_message", ObjectID: m.ID,
						Changes: map[string][2]any{"file": {nil, f.FileName}, "code": {nil, refusal.Code}, "message": {nil, refusal.Message}}})
				})
			}
			if err != nil {
				return err
			}
		}
		return nil
	}
}

// alert records a message's text as an incoming-funds alert on src's account, unless the message's signal is recorded
// already; text that does not read as one, or an alert in another currency, is recorded in the audit trail instead.
func (g Importer) alert(ctx context.Context, org uuid.UUID, m storage.InboundMessage, src *source, body *storage.InboundFile) error {
	var text []byte
	if body != nil {
		var err error
		if text, err = g.read(ctx, body.BlobKey); err != nil {
			return err
		}
	}
	a, ok := banking.ParseAlert(string(text))
	return g.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		refuse := func(message string) error {
			return u.Audit().Record(ctx, storage.AuditRecord{Action: ActionRefused, ObjectType: "inbound_message", ObjectID: m.ID,
				Changes: map[string][2]any{"file": {nil, "message.txt"}, "code": {nil, banking.CodeAlert}, "message": {nil, message}}})
		}
		if !ok {
			return refuse("the message has no statement file, and its text is not an incoming-funds alert")
		}
		signals, err := u.BankMatching().Signals(ctx)
		if err != nil {
			return err
		}
		for _, s := range signals {
			if s.InboundMessageID != nil && *s.InboundMessageID == m.ID {
				return nil // recorded by an earlier run
			}
		}
		_, err = banking.RecordAlert(ctx, u, src.account, a, &m.ID, m.ReceivedAt)
		var refusal *banking.Error
		if errors.As(err, &refusal) {
			return refuse(refusal.Message)
		}
		return err
	})
}

func (g Importer) read(ctx context.Context, key string) ([]byte, error) {
	r, err := g.Files.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(io.LimitReader(r, int64(bankfile.DefaultLimits.MaxBytes)+1))
}
