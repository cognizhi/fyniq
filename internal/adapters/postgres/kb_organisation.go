package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/domain/knowledge"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// OrganisationKnowledge implements storage.UnitOfWork (migration 000563; p1-knowledge-base design D5).
func (u *unitOfWork) OrganisationKnowledge() storage.OrganisationKnowledge { return orgKB{u} }

type orgKB struct{ u *unitOfWork }

func noteOf(id uuid.UUID, key, kind, title, body, class string, version int32, by uuid.UUID, at time.Time, upBy uuid.UUID, upAt time.Time) storage.KBNote {
	return storage.KBNote{ID: id, Key: key, Kind: kind, Title: title, Body: body, DataClass: class, Version: int(version), CreatedBy: by, CreatedAt: at,
		UpdatedBy: upBy, UpdatedAt: upAt}
}

func (r orgKB) Note(ctx context.Context, key string) (storage.KBNote, error) {
	if err := r.u.check(); err != nil {
		return storage.KBNote{}, err
	}
	x, err := r.u.q.KBNoteByKey(ctx, key)
	if err != nil {
		return storage.KBNote{}, mapError(err)
	}
	return noteOf(x.ID, x.Key, x.Kind, x.Title, x.Body, x.DataClass, x.Version, x.CreatedBy, x.CreatedAt, x.UpdatedBy, x.UpdatedAt), nil
}

func (r orgKB) Notes(ctx context.Context) ([]storage.KBNote, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.KBNotes(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.KBNote, 0, len(rows))
	for _, x := range rows {
		out = append(out, noteOf(x.ID, x.Key, x.Kind, x.Title, x.Body, x.DataClass, x.Version, x.CreatedBy, x.CreatedAt, x.UpdatedBy, x.UpdatedAt))
	}
	return out, nil
}

func (r orgKB) SaveNote(ctx context.Context, n storage.KBNote, expected int) (storage.KBNote, error) {
	if err := r.u.check(); err != nil {
		return storage.KBNote{}, err
	}
	if n.DataClass == "" {
		n.DataClass = "general"
	}
	if expected == 0 {
		x, err := r.u.q.InsertKBNote(ctx, sqlcdb.InsertKBNoteParams{Key: n.Key, Kind: n.Kind, Title: n.Title, Body: n.Body, DataClass: n.DataClass,
			By: n.UpdatedBy, At: n.UpdatedAt})
		if err != nil {
			return storage.KBNote{}, mapError(err)
		}
		n.ID, n.Version, n.CreatedBy, n.CreatedAt = x.ID, int(x.Version), n.UpdatedBy, n.UpdatedAt
		return n, r.u.event(ctx, "kb_note.created", "kb_note", n.ID, &n.Version, map[string][2]any{"key": {nil, n.Key}, "kind": {nil, n.Kind},
			"title": {nil, n.Title}, "body": {nil, n.Body}})
	}
	old, err := r.Note(ctx, n.Key)
	if err != nil {
		return storage.KBNote{}, err
	}
	rows, err := r.u.q.UpdateKBNote(ctx, sqlcdb.UpdateKBNoteParams{Kind: n.Kind, Title: n.Title, Body: n.Body, DataClass: n.DataClass,
		By: n.UpdatedBy, At: n.UpdatedAt, Key: n.Key, Version: int32(expected)}) // #nosec G115 -- a version counter
	if err != nil {
		return storage.KBNote{}, mapError(err)
	}
	if rows == 0 {
		return storage.KBNote{}, &storage.Error{Code: storage.CodeVersion, Message: "the note changed since you read it",
			Hint: "Read it again and make the change on its current version"}
	}
	n.ID, n.Version, n.CreatedBy, n.CreatedAt = old.ID, expected+1, old.CreatedBy, old.CreatedAt
	return n, r.u.event(ctx, "kb_note.changed", "kb_note", n.ID, &n.Version, map[string][2]any{"title": {old.Title, n.Title}, "body": {old.Body, n.Body},
		"kind": {old.Kind, n.Kind}, "data_class": {old.DataClass, n.DataClass}})
}

func versionOf(x sqlcdb.KBPageVersionsRow) storage.KBVersion {
	v := storage.KBVersion{ID: x.ID, Key: x.Key, Number: int(x.Number), Type: x.Type, Title: x.Title, Summary: x.Summary, Aliases: x.Aliases, Body: x.Body,
		Sources: x.Sources, DataClass: x.DataClass, Origin: x.Origin, HumanEdited: x.HumanEdited, Compiler: x.Compiler, PromptVersion: x.PromptVersion,
		AuthorKind: x.AuthorKind, CreatedAt: x.CreatedAt}
	if x.BaseKey != nil {
		v.BaseKey = *x.BaseKey
	}
	if x.BaseDigest != nil {
		v.BaseDigest = *x.BaseDigest
	}
	if x.Restores != nil {
		v.Restores = int(*x.Restores)
	}
	if x.AuthorUser != nil {
		v.AuthorUser = *x.AuthorUser
	}
	return v
}

func (r orgKB) AddVersion(ctx context.Context, v storage.KBVersion) (storage.KBVersion, error) {
	if err := r.u.check(); err != nil {
		return storage.KBVersion{}, err
	}
	if v.DataClass == "" {
		v.DataClass = "general"
	}
	sources := []byte(v.Sources)
	if len(sources) == 0 {
		sources = []byte(`[]`)
	}
	p := sqlcdb.InsertKBPageVersionParams{Key: v.Key, Type: v.Type, Title: v.Title, Summary: v.Summary, Aliases: nonNil(v.Aliases), Body: v.Body, Sources: sources,
		DataClass: v.DataClass, Origin: v.Origin, HumanEdited: v.HumanEdited, Compiler: v.Compiler, PromptVersion: v.PromptVersion, AuthorKind: v.AuthorKind,
		At: v.CreatedAt, SearchBody: knowledge.SearchText(v.Body)}
	if v.BaseKey != "" {
		p.BaseKey, p.BaseDigest = &v.BaseKey, &v.BaseDigest
	}
	if v.Restores > 0 {
		n := int32(v.Restores) // #nosec G115 -- a version number
		p.Restores = &n
	}
	if v.AuthorUser != uuid.Nil {
		p.AuthorUser = &v.AuthorUser
	}
	x, err := r.u.q.InsertKBPageVersion(ctx, p)
	if err != nil {
		return storage.KBVersion{}, mapError(err)
	}
	v.ID, v.Number = x.ID, int(x.Number)
	changes := map[string][2]any{"key": {nil, v.Key}, "number": {nil, v.Number}, "origin": {nil, v.Origin}, "title": {nil, v.Title}, "body": {nil, v.Body}}
	return v, r.u.event(ctx, "kb_page.version_added", "kb_page_version", v.ID, &v.Number, changes)
}

func (r orgKB) Versions(ctx context.Context, key string) ([]storage.KBVersion, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.KBPageVersions(ctx, key)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.KBVersion, 0, len(rows))
	for _, x := range rows {
		out = append(out, versionOf(x))
	}
	return out, nil
}

func (r orgKB) Latest(ctx context.Context) ([]storage.KBVersion, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.KBLatestPageVersions(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.KBVersion, 0, len(rows))
	for _, x := range rows {
		out = append(out, versionOf(sqlcdb.KBPageVersionsRow(x)))
	}
	return out, nil
}

func (r orgKB) Published(ctx context.Context) (map[string]int, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.KBLiveVersions(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make(map[string]int, len(rows))
	for _, x := range rows {
		out[x.Key] = int(x.Version)
	}
	return out, nil
}

func (r orgKB) Publish(ctx context.Context, versions map[string]int, approval, by *uuid.UUID, reason string, now time.Time) (storage.KBSnapshot, error) {
	if err := r.u.check(); err != nil {
		return storage.KBSnapshot{}, err
	}
	live, err := r.Published(ctx)
	if err != nil {
		return storage.KBSnapshot{}, err
	}
	next := map[string]int{}
	for k, v := range live {
		next[k] = v
	}
	for k, v := range versions {
		next[k] = v
	}
	made, err := r.u.q.InsertKBSnapshot(ctx, sqlcdb.InsertKBSnapshotParams{ApprovalID: approval, CreatedBy: by, Reason: reason, At: now})
	if err != nil {
		return storage.KBSnapshot{}, mapError(err)
	}
	n := made.Number
	for k, v := range next {
		if err := r.u.q.InsertKBSnapshotPage(ctx, sqlcdb.InsertKBSnapshotPageParams{SnapshotNumber: n, Key: k, Version: int32(v)}); err != nil { // #nosec G115 -- a version number
			return storage.KBSnapshot{}, mapError(err)
		}
	}
	number := int(n)
	changes := map[string][2]any{"snapshot": {nil, number}, "pages": {nil, len(next)}}
	for k, v := range versions {
		changes["page:"+k] = [2]any{live[k], v}
	}
	s := storage.KBSnapshot{ID: made.ID, Number: number, ApprovalID: approval, CreatedBy: by, Reason: reason, CreatedAt: now}
	// An audit event per snapshot: the snapshot is the object, and its changes list each page that moved.
	return s, r.u.event(ctx, "kb_snapshot.published", "kb_snapshot", made.ID, &number, changes)
}

func (r orgKB) Snapshots(ctx context.Context, limit int) ([]storage.KBSnapshot, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.KBSnapshots(ctx, int32(min(max(limit, 1), 200))) // #nosec G115 -- capped
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.KBSnapshot, 0, len(rows))
	for _, x := range rows {
		out = append(out, storage.KBSnapshot{ID: x.ID, Number: int(x.Number), ApprovalID: x.ApprovalID, CreatedBy: x.CreatedBy, Reason: x.Reason, CreatedAt: x.CreatedAt})
	}
	return out, nil
}

func (r orgKB) LiveIndex(ctx context.Context, classes []string) ([]storage.KBLivePage, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.KBLiveIndex(ctx, nonNil(classes))
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.KBLivePage, 0, len(rows))
	for _, x := range rows {
		p := storage.KBLivePage{KBEntry: storage.KBEntry{Key: x.Key, Type: x.Type, Title: x.Title, Summary: x.Summary, Status: "published", DataClass: x.DataClass},
			Origin: x.Origin, Number: int(x.Number), Snapshot: int(x.SnapshotNumber)}
		if x.BaseKey != nil {
			p.BaseKey = *x.BaseKey
		}
		out = append(out, p)
	}
	return out, nil
}

func (r orgKB) LivePage(ctx context.Context, key string, classes []string) (storage.KBLivePage, bool, error) {
	if err := r.u.check(); err != nil {
		return storage.KBLivePage{}, false, err
	}
	x, err := r.u.q.KBLivePage(ctx, sqlcdb.KBLivePageParams{Key: key, Classes: nonNil(classes)})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return storage.KBLivePage{}, false, nil
	case err != nil:
		return storage.KBLivePage{}, false, mapError(err)
	}
	p := storage.KBLivePage{KBEntry: storage.KBEntry{Key: x.Key, Type: x.Type, Title: x.Title, Summary: x.Summary, Status: "published", DataClass: x.DataClass},
		Aliases: x.Aliases, Body: x.Body, Sources: json.RawMessage(x.Sources), Origin: x.Origin, Number: int(x.Number), Snapshot: int(x.SnapshotNumber)}
	if x.BaseKey != nil {
		p.BaseKey = *x.BaseKey
	}
	if x.BaseDigest != nil {
		p.BaseDigest = *x.BaseDigest
	}
	return p, true, nil
}

func (r orgKB) Search(ctx context.Context, terms, classes []string, limit int) ([]storage.KBHit, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.KBOrgSearch(ctx, sqlcdb.KBOrgSearchParams{Terms: nonNil(terms), Classes: nonNil(classes), MaxHits: int32(min(max(limit, 1), 50))}) // #nosec G115 -- capped
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.KBHit, 0, len(rows))
	for _, x := range rows {
		out = append(out, storage.KBHit{Kind: "page", Key: x.Key, Title: x.Title, Summary: x.Summary, Snippet: x.Snippet, Rank: x.Rank})
	}
	return out, nil
}

func findingOf(id uuid.UUID, kind string, key *string, version *int32, detail, status string, at time.Time, by *uuid.UUID, resolvedAt *time.Time, resolution string) storage.KBFinding {
	f := storage.KBFinding{ID: id, Kind: kind, Detail: detail, Status: status, CreatedAt: at, ResolvedBy: by, ResolvedAt: resolvedAt, Resolution: resolution}
	if key != nil {
		f.PageKey = *key
	}
	if version != nil {
		f.PageVersion = int(*version)
	}
	return f
}

func (r orgKB) AddFinding(ctx context.Context, f storage.KBFinding) (storage.KBFinding, error) {
	if err := r.u.check(); err != nil {
		return storage.KBFinding{}, err
	}
	p := sqlcdb.InsertKBFindingParams{Kind: f.Kind, Detail: f.Detail, At: f.CreatedAt}
	if f.PageKey != "" {
		p.PageKey = &f.PageKey
	}
	if f.PageVersion > 0 {
		n := int32(f.PageVersion) // #nosec G115 -- a version number
		p.PageVersion = &n
	}
	id, err := r.u.q.InsertKBFinding(ctx, p)
	if err != nil {
		return storage.KBFinding{}, mapError(err)
	}
	f.ID, f.Status = id, "open"
	return f, r.u.event(ctx, "kb_finding.raised", "kb_lint_finding", id, nil, map[string][2]any{"kind": {nil, f.Kind}, "page": {nil, f.PageKey}, "detail": {nil, f.Detail}})
}

func (r orgKB) Findings(ctx context.Context, status string, limit int) ([]storage.KBFinding, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.KBFindings(ctx, sqlcdb.KBFindingsParams{Status: status, MaxRows: int32(min(max(limit, 1), 500))}) // #nosec G115 -- capped
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.KBFinding, 0, len(rows))
	for _, x := range rows {
		out = append(out, findingOf(x.ID, x.Kind, x.PageKey, x.PageVersion, x.Detail, x.Status, x.CreatedAt, x.ResolvedBy, x.ResolvedAt, x.Resolution))
	}
	return out, nil
}

func (r orgKB) OpenFindings(ctx context.Context, key string) ([]storage.KBFinding, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.KBOpenFindingsOf(ctx, key)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.KBFinding, 0, len(rows))
	for _, x := range rows {
		out = append(out, findingOf(x.ID, x.Kind, x.PageKey, x.PageVersion, x.Detail, x.Status, x.CreatedAt, x.ResolvedBy, x.ResolvedAt, x.Resolution))
	}
	return out, nil
}

func (r orgKB) ResolveFinding(ctx context.Context, id, by uuid.UUID, resolution string, at time.Time) error {
	if err := r.u.check(); err != nil {
		return err
	}
	n, err := r.u.q.ResolveKBFinding(ctx, sqlcdb.ResolveKBFindingParams{By: &by, At: &at, Resolution: resolution, ID: id})
	if err != nil {
		return mapError(err)
	}
	if n == 0 {
		return ports.ErrNotFound
	}
	return r.u.event(ctx, "kb_finding.resolved", "kb_lint_finding", id, nil, map[string][2]any{"status": {"open", "resolved"}, "resolution": {nil, resolution}})
}
