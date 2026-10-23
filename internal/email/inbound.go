package email

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/evidence"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ mail received at the email-in addresses (FR-INT-22)

// FetchKind is the outbox kind of fetching a received message, run by the worker class.
const FetchKind = "inbound.fetch"

// ExpireJob is the platform job that ends quarantined messages whose days have passed (outbox.PlatformSchedules).
const ExpireJob = "inbound.expire"

// QuarantineSetting is the organisation's days to keep quarantined mail; 0 keeps it until someone decides.
const QuarantineSetting = "email.quarantine_days"

// MaxFile is the largest attachment kept, as for any upload (FR-DOC-08).
const MaxFile = 25 << 20

type fetchPayload struct {
	MessageID uuid.UUID `json:"message_id"`
}

// Receive records a message the provider announced at an address and queues its fetch, in the unit of work u; false
// when the provider announced it before.
func Receive(ctx context.Context, d outbox.Dispatcher, u storage.UnitOfWork, org uuid.UUID, n storage.NewInboundMessage) (storage.InboundMessage, bool, error) {
	m, created, err := u.InboundMessages().Receive(ctx, n)
	if err != nil || !created {
		return m, created, err
	}
	b, err := json.Marshal(fetchPayload{MessageID: m.ID})
	if err != nil {
		return m, true, err
	}
	_, err = d.Enqueue(ctx, u, org, storage.NewOutboxItem{Class: storage.ClassWorker, Kind: FetchKind, Payload: b, BusinessKey: "inbound:" + m.ID.String()})
	return m, true, err
}

// Authenticated reports whether the receiving server found the sender's domain vouched for it: DMARC passed, or,
// where the domain publishes no DMARC policy, SPF or DKIM did. A sender who is not authenticated is never known.
func Authenticated(spf, dkim, dmarc string) bool {
	switch strings.ToLower(dmarc) {
	case "pass":
		return true
	case "", "none":
		return strings.EqualFold(spf, "pass") || strings.EqualFold(dkim, "pass")
	}
	return false
}

// Standing is what becomes of a message from sender at an address of member user (nil: the organisation's), on the
// owner's decisions of 23 Oct 2026: a sender rejected at that address is rejected; an authenticated sender accepted
// there before, or known, is accepted: at a member's address the member's verified sign-in email, at the
// organisation's its members' and its contacts' billing and remittance emails; anyone else is quarantined.
func Standing(sender string, user *uuid.UUID, authenticated bool, decisions []storage.CaptureSender, members map[uuid.UUID]string,
	contacts []string) string {
	sender = strings.ToLower(sender)
	for _, d := range decisions {
		if d.Address == sender && ((d.UserID == nil && user == nil) || (d.UserID != nil && user != nil && *d.UserID == *user)) {
			if d.Decision == storage.SenderRejected {
				return storage.InboundRejected
			}
			if authenticated {
				return storage.InboundAccepted
			}
		}
	}
	if !authenticated {
		return storage.InboundQuarantined
	}
	if user != nil {
		if members[*user] == sender {
			return storage.InboundAccepted
		}
		return storage.InboundQuarantined
	}
	for _, m := range members {
		if m == sender {
			return storage.InboundAccepted
		}
	}
	for _, c := range contacts {
		if c == sender {
			return storage.InboundAccepted
		}
	}
	return storage.InboundQuarantined
}

// BlobKey is where a message's file is kept in the staging store: outside the evidence sweeper's prefix, so the
// message's rejection or expiry deletes it.
func BlobKey(org, message, file uuid.UUID) string {
	return "inbound/" + org.String() + "/" + message.String() + "/" + file.String()
}

// Fetcher fetches each received message, decides its standing and keeps its files.
type Fetcher struct {
	Mail    ports.InboundMail
	Store   storage.Store
	Files   ports.BlobStore // the staging store
	Scanner ports.Scanner
	Clock   ports.Clock
}

// Work is the worker's handler of FetchKind:
//   - A message fetched before is left as it is.
//   - The provider's message is read; a failure is retried by the outbox and, on the last attempt, the message failed.
//   - A rejected sender's message keeps nothing. Otherwise its text body and each attachment, checked and scanned like
//     an upload (FR-DOC-08), are kept as untrusted files; a file the checks refuse is left out, and the message says
//     which. A quarantined message ends after the organisation's quarantine days, unless they are 0.
func (f Fetcher) Work() outbox.Work {
	return func(ctx context.Context, org uuid.UUID, row storage.OutboxRow) error {
		var p fetchPayload
		if err := json.Unmarshal(row.Payload, &p); err != nil || p.MessageID == uuid.Nil {
			return fmt.Errorf("inbound: outbox row %s is not a fetch", row.ID)
		}
		var m storage.InboundMessage
		var addr storage.CaptureAddress
		var decisions []storage.CaptureSender
		var members map[uuid.UUID]string
		var contacts []string
		var days int
		if err := f.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			var err error
			if m, err = u.InboundMessages().Get(ctx, p.MessageID); err != nil || m.Status != storage.InboundReceived {
				return err
			}
			if addr, err = u.CaptureAddresses().Get(ctx, m.AddressID); err != nil {
				return err
			}
			if decisions, err = u.CaptureAddresses().Decisions(ctx); err != nil {
				return err
			}
			if members, err = u.InboundMessages().MemberEmails(ctx); err != nil {
				return err
			}
			if contacts, err = u.InboundMessages().ContactEmails(ctx); err != nil {
				return err
			}
			days, err = quarantineDays(ctx, u, f.Clock.Now())
			return err
		}); err != nil {
			return err
		}
		if m.Status != storage.InboundReceived {
			return nil
		}
		fetched := func(fx storage.InboundFetch, files []storage.InboundFile) error {
			return f.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
				for _, file := range files {
					if _, err := u.InboundMessages().AddFile(ctx, file); err != nil {
						return err
					}
				}
				_, err := u.InboundMessages().Fetched(ctx, m.ID, fx, f.Clock.Now())
				return err
			})
		}
		mail, err := f.Mail.Fetch(ctx, m.ProviderID)
		if err != nil {
			if row.MaxAttempts > 0 && row.Attempts >= row.MaxAttempts {
				return fetched(storage.InboundFetch{Status: storage.InboundFailed, LastError: err.Error()}, nil)
			}
			return err
		}
		fx := storage.InboundFetch{SPF: mail.SPF, DKIM: mail.DKIM, DMARC: mail.DMARC}
		fx.Status = Standing(m.Sender, addr.UserID, Authenticated(mail.SPF, mail.DKIM, mail.DMARC), decisions, members, contacts)
		if fx.Status == storage.InboundRejected {
			return fetched(fx, nil)
		}
		if fx.Status == storage.InboundQuarantined && days > 0 {
			end := m.ReceivedAt.Add(time.Duration(days) * 24 * time.Hour)
			fx.ExpiresAt = &end
		}
		files, left, err := f.keep(ctx, org, m, mail)
		if err != nil {
			return err
		}
		if len(left) > 0 {
			fx.LastError = "not kept: " + strings.Join(left, "; ")
		}
		return fetched(fx, files)
	}
}

// keep checks, scans and stores a message's text body and attachments, returning their records and why any was left
// out. Each file's ID comes from the message and the provider's ID of it, so a retry overwrites the same blob.
func (f Fetcher) keep(ctx context.Context, org uuid.UUID, m storage.InboundMessage, mail ports.InboundEmail) ([]storage.InboundFile, []string, error) {
	type part struct {
		kind, name, ref string
		data            []byte
		a               *ports.InboundAttachment
	}
	var parts []part
	if strings.TrimSpace(mail.Text) != "" {
		parts = append(parts, part{kind: storage.InboundBody, name: "message.txt", ref: "body", data: []byte(mail.Text)})
	}
	for i := range mail.Attachments {
		a := &mail.Attachments[i]
		parts = append(parts, part{kind: storage.InboundAttachment, name: a.FileName, ref: "attachment:" + a.ID, a: a})
	}
	var out []storage.InboundFile
	var left []string
	for _, pt := range parts {
		data := pt.data
		if pt.a != nil {
			b, err := f.Mail.Attachment(ctx, m.ProviderID, *pt.a, MaxFile)
			if errors.Is(err, ports.ErrTooLarge) {
				left = append(left, pt.name+": larger than 25 MB")
				continue
			}
			if err != nil {
				return nil, nil, err
			}
			data = b
		}
		file, problems, err := evidence.Check(ctx, f.Scanner, pt.name, data)
		if err != nil {
			return nil, nil, err
		}
		if len(problems) > 0 {
			left = append(left, pt.name+": "+problems[0].Message)
			continue
		}
		id := uuid.NewSHA1(m.ID, []byte(pt.ref))
		key := BlobKey(org, m.ID, id)
		if err := f.Files.Put(ctx, key, strings.NewReader(string(file.Data))); err != nil {
			return nil, nil, err
		}
		out = append(out, storage.InboundFile{ID: id, MessageID: m.ID, Kind: pt.kind, FileName: file.Name, MediaType: file.MediaType,
			Size: int64(len(file.Data)), SHA256: file.SHA256, OriginalSHA256: file.OriginalSHA256, BlobKey: key})
	}
	return out, left, nil
}

// quarantineDays is the organisation's setting on day now.
func quarantineDays(ctx context.Context, u storage.UnitOfWork, now time.Time) (int, error) {
	d, ok := settings.Lookup(QuarantineSetting)
	if !ok {
		return 0, fmt.Errorf("inbound: no setting %s", QuarantineSetting)
	}
	changes, err := u.Settings().Changes(ctx, QuarantineSetting)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(settings.On(d, changes, now).Value)
}

// Settle ends a quarantined message as accepted, rejected or expired, in u, and returns the keys of the files to
// delete once u commits: a rejected or expired message keeps none.
func Settle(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, status string) (storage.InboundMessage, []string, error) {
	m, err := u.InboundMessages().Decide(ctx, id, status)
	if err != nil || status == storage.InboundAccepted {
		return m, nil, err
	}
	removed, err := u.InboundMessages().RemoveFiles(ctx, id)
	keys := make([]string, 0, len(removed))
	for _, r := range removed {
		keys = append(keys, r.BlobKey)
	}
	return m, keys, err
}

// ExpireAll ends every organisation's quarantined messages whose days have passed at now, deleting their files.
func ExpireAll(ctx context.Context, store storage.Store, files ports.BlobStore, now time.Time) (int, error) {
	orgs, err := store.Organisations(ctx)
	if err != nil {
		return 0, err
	}
	var n int
	var errs []error
	for _, org := range orgs {
		var keys []string
		err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			due, err := u.InboundMessages().DueToExpire(ctx, now)
			if err != nil {
				return err
			}
			for _, m := range due {
				_, k, err := Settle(ctx, u, m.ID, storage.InboundExpired)
				if err != nil {
					return err
				}
				keys = append(keys, k...)
				n++
			}
			return nil
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("organisation %s: %w", org, err))
			continue
		}
		for _, k := range keys {
			if err := files.Delete(ctx, k); err != nil && !errors.Is(err, ports.ErrNotFound) {
				errs = append(errs, err)
			}
		}
	}
	return n, errors.Join(errs...)
}
