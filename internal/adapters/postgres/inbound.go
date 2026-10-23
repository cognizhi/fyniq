package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// RecordIdentityEmail implements storage.Sessions.
func (s *Store) RecordIdentityEmail(ctx context.Context, issuer, subject, email string, now time.Time) error {
	return mapError(s.q().RecordIdentityEmail(ctx, sqlcdb.RecordIdentityEmailParams{Issuer: issuer, Subject: subject, Email: strings.ToLower(email), Now: now}))
}

func (u *unitOfWork) InboundMessages() storage.InboundMessages { return inbounds{u} }

type inbounds struct{ u *unitOfWork }

// inboundRow is every message query's row: they all select the same columns.
type inboundRow = sqlcdb.InboundMessagesRow

func toInbound(r inboundRow) storage.InboundMessage {
	return storage.InboundMessage{ID: r.ID, AddressID: r.AddressID, Provider: r.Provider, ProviderID: r.ProviderID, Sender: r.Sender,
		Subject: str(r.Subject), ReceivedAt: r.ReceivedAt, Status: r.Status, SPF: str(r.Spf), DKIM: str(r.Dkim), DMARC: str(r.Dmarc),
		FetchedAt: r.FetchedAt, ExpiresAt: r.ExpiresAt, DecidedBy: str(r.DecidedBy), DecidedAt: r.DecidedAt, LastError: str(r.LastError)}
}

type inboundFileRow = sqlcdb.InboundFilesRow

func toInboundFile(r inboundFileRow) storage.InboundFile {
	return storage.InboundFile{ID: r.ID, MessageID: r.MessageID, Kind: r.Kind, FileName: r.FileName, MediaType: r.MediaType, Size: r.SizeBytes,
		SHA256: r.Sha256, OriginalSHA256: r.OriginalSha256, BlobKey: r.BlobKey, StoredAt: r.StoredAt, RemovedAt: r.RemovedAt}
}

// clipTo keeps s at n characters.
func clipTo(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

func (r inbounds) Receive(ctx context.Context, n storage.NewInboundMessage) (storage.InboundMessage, bool, error) {
	if err := r.u.check(); err != nil {
		return storage.InboundMessage{}, false, err
	}
	row, err := r.u.q.InsertInboundMessage(ctx, sqlcdb.InsertInboundMessageParams{AddressID: n.AddressID, Provider: n.Provider,
		ProviderID: n.ProviderID, Sender: strings.ToLower(n.Sender), Subject: opt(clipTo(n.Subject, 500)), ReceivedAt: n.ReceivedAt})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.InboundMessage{}, false, nil // the provider announced it before
	}
	if err != nil {
		return storage.InboundMessage{}, false, mapError(err)
	}
	m := toInbound(inboundRow(row))
	if err := r.u.event(ctx, "inbound_message.received", "inbound_message", m.ID, nil, map[string][2]any{
		"address_id": {nil, m.AddressID.String()}, "provider": {nil, m.Provider}, "sender": {nil, m.Sender}, "subject": {nil, nilIfEmpty(m.Subject)}}); err != nil {
		return storage.InboundMessage{}, false, err
	}
	return m, true, nil
}

func (r inbounds) Get(ctx context.Context, id uuid.UUID) (storage.InboundMessage, error) {
	if err := r.u.check(); err != nil {
		return storage.InboundMessage{}, err
	}
	row, err := r.u.q.GetInboundMessage(ctx, id)
	if err != nil {
		return storage.InboundMessage{}, mapError(err)
	}
	return toInbound(inboundRow(row)), nil
}

// moved records a message's status change in the audit trail.
func (r inbounds) moved(ctx context.Context, m storage.InboundMessage, from string) error {
	return r.u.event(ctx, "inbound_message."+m.Status, "inbound_message", m.ID, nil, map[string][2]any{
		"status": {from, m.Status}, "spf": {nil, nilIfEmpty(m.SPF)}, "dkim": {nil, nilIfEmpty(m.DKIM)}, "dmarc": {nil, nilIfEmpty(m.DMARC)},
		"last_error": {nil, nilIfEmpty(m.LastError)}})
}

func (r inbounds) Fetched(ctx context.Context, id uuid.UUID, f storage.InboundFetch, at time.Time) (storage.InboundMessage, error) {
	if err := r.u.check(); err != nil {
		return storage.InboundMessage{}, err
	}
	row, err := r.u.q.FetchedInboundMessage(ctx, sqlcdb.FetchedInboundMessageParams{ID: id, Status: f.Status, Spf: opt(clipTo(f.SPF, 40)),
		Dkim: opt(clipTo(f.DKIM, 40)), Dmarc: opt(clipTo(f.DMARC, 40)), FetchedAt: &at, ExpiresAt: f.ExpiresAt, LastError: opt(clip(f.LastError))})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.InboundMessage{}, &storage.Error{Code: storage.CodeVersion, Message: fmt.Sprintf("inbound message %s was fetched already", id)}
	}
	if err != nil {
		return storage.InboundMessage{}, mapError(err)
	}
	m := toInbound(inboundRow(row))
	return m, r.moved(ctx, m, storage.InboundReceived)
}

func (r inbounds) AddFile(ctx context.Context, f storage.InboundFile) (storage.InboundFile, error) {
	if err := r.u.check(); err != nil {
		return storage.InboundFile{}, err
	}
	row, err := r.u.q.InsertInboundFile(ctx, sqlcdb.InsertInboundFileParams{MessageID: f.MessageID, Kind: f.Kind, FileName: clipTo(f.FileName, 255),
		MediaType: f.MediaType, SizeBytes: f.Size, Sha256: f.SHA256, OriginalSha256: f.OriginalSHA256, BlobKey: f.BlobKey, StoredAt: r.u.store.clock.Now()})
	if err != nil {
		return storage.InboundFile{}, mapError(err)
	}
	out := toInboundFile(inboundFileRow(row))
	if err := r.u.event(ctx, "inbound_message.file_stored", "inbound_message", out.MessageID, nil, map[string][2]any{
		"file_id": {nil, out.ID.String()}, "kind": {nil, out.Kind}, "file_name": {nil, out.FileName}, "media_type": {nil, out.MediaType},
		"size_bytes": {nil, out.Size}, "sha256": {nil, out.SHA256}}); err != nil {
		return storage.InboundFile{}, err
	}
	return out, nil
}

func (r inbounds) Files(ctx context.Context, message uuid.UUID) ([]storage.InboundFile, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.InboundFiles(ctx, message)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.InboundFile, 0, len(rows))
	for _, x := range rows {
		out = append(out, toInboundFile(x))
	}
	return out, nil
}

func (r inbounds) Decide(ctx context.Context, id uuid.UUID, status string) (storage.InboundMessage, error) {
	if err := r.u.check(); err != nil {
		return storage.InboundMessage{}, err
	}
	var row inboundRow
	var err error
	if status == storage.InboundExpired {
		var x sqlcdb.ExpireInboundMessageRow
		x, err = r.u.q.ExpireInboundMessage(ctx, id)
		row = inboundRow(x)
	} else {
		var x sqlcdb.DecideInboundMessageRow
		x, err = r.u.q.DecideInboundMessage(ctx, sqlcdb.DecideInboundMessageParams{ID: id, Status: status, DecidedBy: opt(r.u.actor(ctx)),
			DecidedAt: ptr(r.u.store.clock.Now())})
		row = inboundRow(x)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		if _, gerr := r.u.q.GetInboundMessage(ctx, id); gerr != nil {
			return storage.InboundMessage{}, mapError(gerr)
		}
		return storage.InboundMessage{}, &storage.Error{Code: storage.CodeVersion, Message: fmt.Sprintf("inbound message %s is not in quarantine", id)}
	}
	if err != nil {
		return storage.InboundMessage{}, mapError(err)
	}
	m := toInbound(row)
	return m, r.moved(ctx, m, storage.InboundQuarantined)
}

func (r inbounds) RemoveFiles(ctx context.Context, message uuid.UUID) ([]storage.InboundFile, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.RemoveInboundFiles(ctx, sqlcdb.RemoveInboundFilesParams{MessageID: message, RemovedAt: ptr(r.u.store.clock.Now())})
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.InboundFile, 0, len(rows))
	for _, x := range rows {
		f := toInboundFile(inboundFileRow(x))
		out = append(out, f)
		if err := r.u.event(ctx, "inbound_message.file_removed", "inbound_message", message, nil, map[string][2]any{
			"file_id": {nil, f.ID.String()}, "file_name": {nil, f.FileName}}); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (r inbounds) List(ctx context.Context, f storage.InboundFilter) ([]storage.InboundMessage, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	var before *uuid.UUID
	if f.Before != uuid.Nil {
		before = &f.Before
	}
	limit := f.Limit
	if limit <= 0 || limit > math.MaxInt32 {
		limit = 100
	}
	rows, err := r.u.q.InboundMessages(ctx, sqlcdb.InboundMessagesParams{Status: f.Status, AddressID: f.Address, Before: before,
		PageSize: int32(limit)}) // 1 to MaxInt32, checked above
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.InboundMessage, 0, len(rows))
	for _, x := range rows {
		out = append(out, toInbound(x))
	}
	return out, nil
}

func (r inbounds) QuarantinedFrom(ctx context.Context, sender string, address uuid.UUID) ([]storage.InboundMessage, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.QuarantinedInboundFrom(ctx, sqlcdb.QuarantinedInboundFromParams{Sender: strings.ToLower(sender), AddressID: address})
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.InboundMessage, 0, len(rows))
	for _, x := range rows {
		out = append(out, toInbound(inboundRow(x)))
	}
	return out, nil
}

func (r inbounds) DueToExpire(ctx context.Context, now time.Time) ([]storage.InboundMessage, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.InboundDueToExpire(ctx, &now)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.InboundMessage, 0, len(rows))
	for _, x := range rows {
		out = append(out, toInbound(inboundRow(x)))
	}
	return out, nil
}

func (r inbounds) MemberEmails(ctx context.Context) (map[uuid.UUID]string, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.MemberEmails(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make(map[uuid.UUID]string, len(rows))
	for _, x := range rows {
		out[x.UserID] = x.Email
	}
	return out, nil
}

func (r inbounds) ContactEmails(ctx context.Context) ([]string, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ContactEmails(ctx)
	return rows, mapError(err)
}
