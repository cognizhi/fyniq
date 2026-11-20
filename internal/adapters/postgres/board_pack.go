package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------ the board pack and the investor update (FR-RPT-06, FR-EQ-23, rev 0.540)

func (u *unitOfWork) BoardPacks() storage.BoardPacks           { return boardPacks{u} }
func (u *unitOfWork) InvestorUpdates() storage.InvestorUpdates { return investorUpdates{u} }

type boardPacks struct{ u *unitOfWork }

func toBoardPack(r sqlcdb.FyniqBoardPack) storage.BoardPack {
	p := storage.BoardPack{ID: r.ID, Month: r.Month, Version: int(r.Version), Status: r.Status, Highlights: r.Highlights, Commentary: r.Commentary, Asks: r.Asks,
		Figures: json.RawMessage(r.Figures), Supersedes: r.Supersedes, Edit: int(r.Edit), CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt, UpdatedBy: r.UpdatedBy,
		UpdatedAt: r.UpdatedAt, PublishedAt: r.PublishedAt}
	if r.LedgerRevision != nil {
		p.LedgerRevision = *r.LedgerRevision
	}
	if r.PublishedBy != nil {
		p.PublishedBy = *r.PublishedBy
	}
	return p
}

func (r boardPacks) Get(ctx context.Context, id uuid.UUID) (storage.BoardPack, error) {
	if err := r.u.check(); err != nil {
		return storage.BoardPack{}, err
	}
	row, err := r.u.q.GetBoardPack(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.BoardPack{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.BoardPack{}, mapError(err)
	}
	return toBoardPack(row), nil
}

func (r boardPacks) List(ctx context.Context, publishedOnly bool) ([]storage.BoardPack, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ListBoardPacks(ctx, publishedOnly)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.BoardPack, 0, len(rows))
	for _, row := range rows {
		out = append(out, toBoardPack(row))
	}
	return out, nil
}

func (r boardPacks) Latest(ctx context.Context, month time.Time) (storage.BoardPack, error) {
	if err := r.u.check(); err != nil {
		return storage.BoardPack{}, err
	}
	row, err := r.u.q.LatestBoardPack(ctx, month)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.BoardPack{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.BoardPack{}, mapError(err)
	}
	return toBoardPack(row), nil
}

func (r boardPacks) CreateDraft(ctx context.Context, month time.Time, by string, at time.Time) (storage.BoardPack, error) {
	latest, err := r.Latest(ctx, month)
	version, sup := 1, (*uuid.UUID)(nil)
	switch {
	case errors.Is(err, ports.ErrNotFound):
	case err != nil:
		return storage.BoardPack{}, err
	default:
		version, sup = latest.Version+1, &latest.ID
	}
	row, err := r.u.q.InsertBoardPackDraft(ctx, sqlcdb.InsertBoardPackDraftParams{Month: month, Version: int32(version), Supersedes: sup, By: by, At: at}) // #nosec G115 -- a month's few versions
	if err != nil {
		return storage.BoardPack{}, mapError(err)
	}
	out := toBoardPack(row)
	v := out.Version
	return out, r.u.event(ctx, "board_pack.drafted", "board_pack", out.ID, &v, map[string][2]any{"month": {nil, month.Format("2006-01")}, "version": {nil, out.Version}})
}

func (r boardPacks) EditDraft(ctx context.Context, id uuid.UUID, highlights, commentary, asks, by string, at time.Time, edit int) (storage.BoardPack, error) {
	if err := r.u.check(); err != nil {
		return storage.BoardPack{}, err
	}
	before, err := r.Get(ctx, id)
	if err != nil {
		return storage.BoardPack{}, err
	}
	row, err := r.u.q.EditBoardPackDraft(ctx, sqlcdb.EditBoardPackDraftParams{ID: id, Edit: int32(min(max(edit, 1), 1<<30)), // #nosec G115 -- bounded
		Highlights: highlights, Commentary: commentary, Asks: asks, By: by, At: at})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.BoardPack{}, &storage.Error{Code: storage.CodeVersion, Message: "the draft changed since it was read, or is published"}
	}
	if err != nil {
		return storage.BoardPack{}, mapError(err)
	}
	out := toBoardPack(row)
	v := out.Edit
	return out, r.u.event(ctx, "board_pack.edited", "board_pack", out.ID, &v, map[string][2]any{"highlights": {before.Highlights, out.Highlights},
		"commentary": {before.Commentary, out.Commentary}, "asks": {before.Asks, out.Asks}})
}

func (r boardPacks) Publish(ctx context.Context, id uuid.UUID, figures json.RawMessage, revision int64, by string, at time.Time, edit int) (storage.BoardPack, error) {
	if err := r.u.check(); err != nil {
		return storage.BoardPack{}, err
	}
	row, err := r.u.q.PublishBoardPack(ctx, sqlcdb.PublishBoardPackParams{ID: id, Edit: int32(min(max(edit, 1), 1<<30)), Figures: figures, Revision: &revision, By: &by, At: &at}) // #nosec G115 -- bounded
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.BoardPack{}, &storage.Error{Code: storage.CodeVersion, Message: "the draft changed since it was read, or is published"}
	}
	if err != nil {
		return storage.BoardPack{}, mapError(err)
	}
	out := toBoardPack(row)
	v := out.Edit
	return out, r.u.event(ctx, "board_pack.published", "board_pack", out.ID, &v, map[string][2]any{"status": {storage.PackDraft, storage.PackPublished},
		"ledger_revision": {nil, revision}, "version": {nil, out.Version}})
}

type investorUpdates struct{ u *unitOfWork }

func toInvestorUpdate(r sqlcdb.FyniqInvestorUpdate) storage.InvestorUpdate {
	u := storage.InvestorUpdate{ID: r.ID, PackID: r.PackID, Kind: r.Kind, Title: r.Title, Highlights: r.Highlights, Asks: r.Asks, Recipients: r.Recipients, Status: r.Status,
		Edit: int(r.Edit), CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt, UpdatedBy: r.UpdatedBy, UpdatedAt: r.UpdatedAt, SentAt: r.SentAt}
	if r.SentBy != nil {
		u.SentBy = *r.SentBy
	}
	return u
}

func (r investorUpdates) Get(ctx context.Context, id uuid.UUID) (storage.InvestorUpdate, error) {
	if err := r.u.check(); err != nil {
		return storage.InvestorUpdate{}, err
	}
	row, err := r.u.q.GetInvestorUpdate(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.InvestorUpdate{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.InvestorUpdate{}, mapError(err)
	}
	return toInvestorUpdate(row), nil
}

func (r investorUpdates) List(ctx context.Context, sentOnly bool) ([]storage.InvestorUpdate, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ListInvestorUpdates(ctx, sentOnly)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.InvestorUpdate, 0, len(rows))
	for _, row := range rows {
		out = append(out, toInvestorUpdate(row))
	}
	return out, nil
}

func (r investorUpdates) CreateDraft(ctx context.Context, packID uuid.UUID, kind, title, by string, at time.Time) (storage.InvestorUpdate, error) {
	if err := r.u.check(); err != nil {
		return storage.InvestorUpdate{}, err
	}
	row, err := r.u.q.InsertInvestorUpdate(ctx, sqlcdb.InsertInvestorUpdateParams{PackID: packID, Kind: kind, Title: title, By: by, At: at})
	if err != nil {
		return storage.InvestorUpdate{}, mapError(err)
	}
	out := toInvestorUpdate(row)
	v := out.Edit
	return out, r.u.event(ctx, "investor_update.drafted", "investor_update", out.ID, &v, map[string][2]any{"pack_id": {nil, packID.String()}, "kind": {nil, kind}, "title": {nil, title}})
}

func (r investorUpdates) EditDraft(ctx context.Context, id uuid.UUID, title, highlights, asks, by string, at time.Time, edit int) (storage.InvestorUpdate, error) {
	before, err := r.Get(ctx, id)
	if err != nil {
		return storage.InvestorUpdate{}, err
	}
	row, err := r.u.q.EditInvestorUpdate(ctx, sqlcdb.EditInvestorUpdateParams{ID: id, Edit: int32(min(max(edit, 1), 1<<30)), Title: title, Highlights: highlights, Asks: asks, By: by, At: at}) // #nosec G115 -- bounded
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.InvestorUpdate{}, &storage.Error{Code: storage.CodeVersion, Message: "the draft changed since it was read, or is sent"}
	}
	if err != nil {
		return storage.InvestorUpdate{}, mapError(err)
	}
	out := toInvestorUpdate(row)
	v := out.Edit
	return out, r.u.event(ctx, "investor_update.edited", "investor_update", out.ID, &v, map[string][2]any{"title": {before.Title, out.Title},
		"highlights": {before.Highlights, out.Highlights}, "asks": {before.Asks, out.Asks}})
}

func (r investorUpdates) Send(ctx context.Context, id uuid.UUID, recipients []uuid.UUID, by string, at time.Time, edit int) (storage.InvestorUpdate, error) {
	if err := r.u.check(); err != nil {
		return storage.InvestorUpdate{}, err
	}
	row, err := r.u.q.SendInvestorUpdate(ctx, sqlcdb.SendInvestorUpdateParams{ID: id, Edit: int32(min(max(edit, 1), 1<<30)), Recipients: recipients, By: &by, At: &at}) // #nosec G115 -- bounded
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.InvestorUpdate{}, &storage.Error{Code: storage.CodeVersion, Message: "the draft changed since it was read, or is sent"}
	}
	if err != nil {
		return storage.InvestorUpdate{}, mapError(err)
	}
	out := toInvestorUpdate(row)
	v := out.Edit
	rs := make([]string, len(recipients))
	for i, x := range recipients {
		rs[i] = x.String()
	}
	return out, r.u.event(ctx, "investor_update.sent", "investor_update", out.ID, &v, map[string][2]any{"status": {storage.UpdateDraft, storage.UpdateSent}, "recipients": {nil, rs}})
}

func (r investorUpdates) MarkRead(ctx context.Context, id, user uuid.UUID, at time.Time) (bool, error) {
	if err := r.u.check(); err != nil {
		return false, err
	}
	rid, err := r.u.q.InsertInvestorUpdateRead(ctx, sqlcdb.InsertInvestorUpdateReadParams{UpdateID: id, UserID: user, At: at})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil // opened already
	}
	if err != nil {
		return false, mapError(err)
	}
	return true, r.u.event(ctx, "investor_update_read.recorded", "investor_update_read", rid, nil, map[string][2]any{"update_id": {nil, id.String()}, "user_id": {nil, user.String()}})
}

func (r investorUpdates) Reads(ctx context.Context, id uuid.UUID) ([]storage.InvestorUpdateRead, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.InvestorUpdateReads(ctx, id)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.InvestorUpdateRead, 0, len(rows))
	for _, row := range rows {
		out = append(out, storage.InvestorUpdateRead{UpdateID: row.UpdateID, UserID: row.UserID, ReadAt: row.ReadAt})
	}
	return out, nil
}
