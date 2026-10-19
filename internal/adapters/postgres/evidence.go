package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) Evidence() storage.EvidenceFiles { return evidenceFiles{u} }

type evidenceFiles struct{ u *unitOfWork }

// evidenceRow is every evidence query's row: they all select the same columns.
type evidenceRow = sqlcdb.GetEvidenceRow

func toEvidence(r evidenceRow) storage.Evidence {
	return storage.Evidence{ID: r.ID, NewEvidence: storage.NewEvidence{DocumentID: r.DocumentID, EvidenceType: r.EvidenceType,
		Note: str(r.Note), RelatesOn: nullDate(r.RelatesOn), FileName: r.FileName, MediaType: r.MediaType, Size: r.SizeBytes,
		SHA256: r.Sha256, OriginalSHA256: r.OriginalSha256, Source: storage.EvidenceSource(r.Source), DataClass: storage.DataClass(r.DataClass),
		StagingKey: r.StagingKey, UploadedBy: r.UploadedBy},
		UploadedAt: r.UploadedAt, State: storage.EvidenceState(r.State), RemovedAt: r.RemovedAt, LockedKey: str(r.LockedKey),
		RetainUntil: r.RetainUntil, FinalisedAt: r.FinalisedAt, Version: int(r.Version)}
}

func timeOrNil(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

// evidenceChanges are an evidence file's fields for its audit event: what FR-DOC-07 asks the trail to hold (the
// hashes, uploader, time, source and parent) and its state. The file name is the uploader's data, kept in
// the row; the trail carries the hashes that identify the file.
func evidenceChanges(a, b storage.Evidence) map[string][2]any {
	out := map[string][2]any{}
	diff := func(k string, x, y any) {
		if fmt.Sprint(x) != fmt.Sprint(y) {
			out[k] = [2]any{x, y}
		}
	}
	id := func(v uuid.UUID) any {
		if v == uuid.Nil {
			return nil
		}
		return v.String()
	}
	diff("document_id", id(a.DocumentID), id(b.DocumentID))
	diff("evidence_type", nilIfEmpty(a.EvidenceType), nilIfEmpty(b.EvidenceType))
	diff("relates_on", nilIfEmpty(dayOf(a.RelatesOn)), nilIfEmpty(dayOf(b.RelatesOn)))
	diff("media_type", nilIfEmpty(a.MediaType), nilIfEmpty(b.MediaType))
	if a.Size != b.Size {
		out["size_bytes"] = [2]any{nilIfZero(a.Size), nilIfZero(b.Size)}
	}
	diff("sha256", nilIfEmpty(a.SHA256), nilIfEmpty(b.SHA256))
	diff("original_sha256", nilIfEmpty(a.OriginalSHA256), nilIfEmpty(b.OriginalSHA256))
	diff("source", nilIfEmpty(string(a.Source)), nilIfEmpty(string(b.Source)))
	diff("data_class", nilIfEmpty(string(a.DataClass)), nilIfEmpty(string(b.DataClass)))
	diff("uploaded_by", nilIfEmpty(a.UploadedBy), nilIfEmpty(b.UploadedBy))
	if a.UploadedAt.IsZero() && !b.UploadedAt.IsZero() {
		out["uploaded_at"] = [2]any{nil, b.UploadedAt.UTC().Format(time.RFC3339)}
	}
	diff("state", nilIfEmpty(string(a.State)), nilIfEmpty(string(b.State)))
	diff("removed_at", timeOrNil(a.RemovedAt), timeOrNil(b.RemovedAt))
	diff("locked_key", nilIfEmpty(a.LockedKey), nilIfEmpty(b.LockedKey))
	diff("retain_until", timeOrNil(a.RetainUntil), timeOrNil(b.RetainUntil))
	return out
}

func nilIfZero(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}

func (r evidenceFiles) Attach(ctx context.Context, e storage.NewEvidence) (storage.Evidence, error) {
	if err := r.u.check(); err != nil {
		return storage.Evidence{}, err
	}
	row, err := r.u.q.InsertEvidence(ctx, sqlcdb.InsertEvidenceParams{DocumentID: e.DocumentID, EvidenceType: e.EvidenceType, Note: opt(e.Note),
		RelatesOn: pgDate(e.RelatesOn), FileName: e.FileName, MediaType: e.MediaType, SizeBytes: e.Size, Sha256: e.SHA256,
		OriginalSha256: e.OriginalSHA256, Source: string(e.Source), DataClass: string(e.DataClass), StagingKey: e.StagingKey,
		UploadedBy: e.UploadedBy})
	if err != nil {
		return storage.Evidence{}, mapError(err)
	}
	ev := toEvidence(evidenceRow(row))
	if err := r.u.event(ctx, "evidence.attached", "evidence", ev.ID, &ev.Version, evidenceChanges(storage.Evidence{}, ev)); err != nil {
		return storage.Evidence{}, err
	}
	return ev, nil
}

func (r evidenceFiles) Get(ctx context.Context, id uuid.UUID) (storage.Evidence, error) {
	if err := r.u.check(); err != nil {
		return storage.Evidence{}, err
	}
	row, err := r.u.q.GetEvidence(ctx, id)
	if err != nil {
		return storage.Evidence{}, mapError(err)
	}
	return toEvidence(row), nil
}

func (r evidenceFiles) OfDocument(ctx context.Context, documentID, after uuid.UUID, limit int) ([]storage.Evidence, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 10000 {
		return nil, &storage.Error{Code: storage.CodeInvalid, Message: fmt.Sprintf("a page of %d files", limit), Hint: "Ask for 1 to 10,000"}
	}
	rows, err := r.u.q.PageDocumentEvidence(ctx, sqlcdb.PageDocumentEvidenceParams{DocumentID: documentID, After: after, PageSize: int32(limit)})
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.Evidence, 0, len(rows))
	for _, row := range rows {
		out = append(out, toEvidence(evidenceRow(row)))
	}
	return out, nil
}

func (r evidenceFiles) Remove(ctx context.Context, id uuid.UUID, version int) (storage.Evidence, error) {
	if err := r.u.check(); err != nil {
		return storage.Evidence{}, err
	}
	cur, err := r.u.q.LockEvidence(ctx, id)
	if err != nil {
		return storage.Evidence{}, mapError(err)
	}
	before := toEvidence(evidenceRow(cur))
	if before.Version != version {
		return storage.Evidence{}, &storage.Error{Code: storage.CodeVersion,
			Message: fmt.Sprintf("the file changed since it was read: it is at version %d, not %d", before.Version, version)}
	}
	row, err := r.u.q.RemoveEvidence(ctx, sqlcdb.RemoveEvidenceParams{ID: id, RemovedAt: ptr(r.u.store.clock.Now())})
	if err != nil {
		return storage.Evidence{}, mapError(err)
	}
	after := toEvidence(evidenceRow(row))
	changes := evidenceChanges(before, after)
	changes["version"] = [2]any{before.Version, after.Version}
	if err := r.u.event(ctx, "evidence.removed", "evidence", id, &after.Version, changes); err != nil {
		return storage.Evidence{}, err
	}
	return after, nil
}

func (r evidenceFiles) Finalise(ctx context.Context, id uuid.UUID, lockedKey string, retainUntil time.Time) (storage.Evidence, error) {
	if err := r.u.check(); err != nil {
		return storage.Evidence{}, err
	}
	cur, err := r.u.q.LockEvidence(ctx, id)
	if err != nil {
		return storage.Evidence{}, mapError(err)
	}
	before := toEvidence(evidenceRow(cur))
	row, err := r.u.q.FinaliseEvidence(ctx, sqlcdb.FinaliseEvidenceParams{ID: id, LockedKey: &lockedKey, RetainUntil: &retainUntil,
		FinalisedAt: ptr(r.u.store.clock.Now())})
	if err != nil {
		return storage.Evidence{}, mapError(err)
	}
	after := toEvidence(evidenceRow(row))
	changes := evidenceChanges(before, after)
	changes["version"] = [2]any{before.Version, after.Version}
	if err := r.u.event(ctx, "evidence.finalised", "evidence", id, &after.Version, changes); err != nil {
		return storage.Evidence{}, err
	}
	return after, nil
}

func ptr[T any](v T) *T { return &v }

func (r evidenceFiles) Unfinalised(ctx context.Context, limit int) ([]storage.Evidence, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 10000 {
		return nil, &storage.Error{Code: storage.CodeInvalid, Message: fmt.Sprintf("a page of %d files", limit), Hint: "Ask for 1 to 10,000"}
	}
	rows, err := r.u.q.UnfinalisedEvidence(ctx, int32(limit))
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.Evidence, 0, len(rows))
	for _, row := range rows {
		out = append(out, toEvidence(evidenceRow(row)))
	}
	return out, nil
}

func (r evidenceFiles) Final(ctx context.Context, after uuid.UUID, limit int) ([]storage.Evidence, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 10000 {
		return nil, &storage.Error{Code: storage.CodeInvalid, Message: fmt.Sprintf("a page of %d files", limit), Hint: "Ask for 1 to 10,000"}
	}
	rows, err := r.u.q.PageFinalEvidence(ctx, sqlcdb.PageFinalEvidenceParams{After: after, PageSize: int32(limit)})
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.Evidence, 0, len(rows))
	for _, row := range rows {
		out = append(out, toEvidence(evidenceRow(row)))
	}
	return out, nil
}

func keySet(keys []string, err error) (map[string]bool, error) {
	if err != nil {
		return nil, mapError(err)
	}
	out := make(map[string]bool, len(keys))
	for _, k := range keys {
		out[k] = true
	}
	return out, nil
}

func (r evidenceFiles) LiveStagingKeys(ctx context.Context, keys []string) (map[string]bool, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	return keySet(r.u.q.LiveStagingKeys(ctx, keys))
}

func (r evidenceFiles) Known(ctx context.Context, lockedKeys []string) (map[string]bool, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	return keySet(r.u.q.KnownLockedKeys(ctx, lockedKeys))
}
