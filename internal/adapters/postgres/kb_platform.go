package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/domain/knowledge"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// LoadPlatformKB copies a release's knowledge base into the global tables as the role in url, which must be fyniq_migrator:
// the release is the only path that writes them (§11.4, AI-56; migration 000560). A release already loaded with the same
// version and digest is skipped; any other replaces the whole set in one call, so a reader sees one release or the next,
// never a mix.
func LoadPlatformKB(ctx context.Context, url string, rel knowledge.Release, log io.Writer) error {
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		return fmt.Errorf("connect as fyniq_migrator: %w", err)
	}
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()
	q := sqlcdb.New(conn)
	have, err := q.KBPlatformDigest(ctx)
	switch {
	case err == nil && have.Version == rel.Version && have.Digest == rel.Digest:
		return nil
	case err != nil && !errors.Is(err, pgx.ErrNoRows):
		return mapError(err)
	}
	type source struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	}
	type guide struct {
		File   string   `json:"file"`
		Key    string   `json:"key"`
		Title  string   `json:"title"`
		Roles  []string `json:"roles"`
		Status string   `json:"status"`
		Body   string   `json:"body"`
	}
	type page struct {
		Key        string   `json:"key"`
		Type       string   `json:"type"`
		Title      string   `json:"title"`
		Summary    string   `json:"summary"`
		Aliases    []string `json:"aliases"`
		Body       string   `json:"body"`
		SearchBody string   `json:"search_body"`
		Sources    []source `json:"sources"`
		DataClass  string   `json:"data_class"`
		Taint      string   `json:"taint"`
		Status     string   `json:"status"`
		CompiledBy string   `json:"compiled_by"`
		CompiledAt string   `json:"compiled_at"`
		Digest     string   `json:"digest"`
	}
	doc := struct {
		Version string  `json:"version"`
		Schema  string  `json:"schema"`
		Digest  string  `json:"digest"`
		Guides  []guide `json:"guides"`
		Pages   []page  `json:"pages"`
	}{Version: rel.Version, Schema: rel.Schema, Digest: rel.Digest}
	for _, g := range rel.Guide {
		doc.Guides = append(doc.Guides, guide{File: g.Path, Key: g.Key, Title: g.Title, Roles: g.Roles, Status: g.Status, Body: g.Body})
	}
	for _, p := range rel.Pages {
		pg := page{Key: p.Key, Type: p.Type, Title: p.Title, Summary: p.Summary, Aliases: nonNil(p.Aliases), Body: p.Body,
			SearchBody: knowledge.SearchText(p.Body), DataClass: p.DataClass, Taint: p.Taint, Status: p.Status,
			CompiledBy: p.CompiledBy, CompiledAt: p.CompiledAt, Digest: p.Digest}
		for _, s := range p.Sources {
			pg.Sources = append(pg.Sources, source{s.Path, s.SHA256})
		}
		doc.Pages = append(doc.Pages, pg)
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	if err := q.KBPlatformLoad(ctx, raw); err != nil {
		return fmt.Errorf("load the release's knowledge base: %w", mapError(err))
	}
	fmt.Fprintf(log, "loaded the knowledge base of release %s: %d pages, %d guide pages (%s)\n", rel.Version, len(rel.Pages), len(rel.Guide), rel.Digest[:12])
	return nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// KnowledgeBase implements storage.UnitOfWork: the platform pages through the reader filter.
func (u *unitOfWork) KnowledgeBase() storage.KnowledgeBase { return kbRepo{u} }

type kbRepo struct{ u *unitOfWork }

func (r kbRepo) PlatformInfo(ctx context.Context) (storage.KBPlatformInfo, bool, error) {
	if err := r.u.check(); err != nil {
		return storage.KBPlatformInfo{}, false, err
	}
	row, err := r.u.q.KBPlatformInfo(ctx)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return storage.KBPlatformInfo{}, false, nil
	case err != nil:
		return storage.KBPlatformInfo{}, false, mapError(err)
	}
	return storage.KBPlatformInfo{Version: row.Version, Schema: row.Schema, Digest: row.Digest, LoadedAt: row.LoadedAt}, true, nil
}

func (r kbRepo) PlatformIndex(ctx context.Context, rd storage.KBReader) ([]storage.KBEntry, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.KBPlatformIndex(ctx, sqlcdb.KBPlatformIndexParams{Roles: nonNil(rd.Roles), Classes: nonNil(rd.Classes)})
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.KBEntry, 0, len(rows))
	for _, x := range rows {
		out = append(out, storage.KBEntry{Key: x.Key, Type: x.Type, Title: x.Title, Summary: x.Summary, Status: x.Status, DataClass: x.DataClass})
	}
	return out, nil
}

func (r kbRepo) PlatformPage(ctx context.Context, key string, rd storage.KBReader) (storage.KBPlatformPage, bool, error) {
	if err := r.u.check(); err != nil {
		return storage.KBPlatformPage{}, false, err
	}
	x, err := r.u.q.KBPlatformPageGet(ctx, sqlcdb.KBPlatformPageGetParams{Key: key, Roles: nonNil(rd.Roles), Classes: nonNil(rd.Classes)})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return storage.KBPlatformPage{}, false, nil
	case err != nil:
		return storage.KBPlatformPage{}, false, mapError(err)
	}
	return storage.KBPlatformPage{
		KBEntry: storage.KBEntry{Key: x.Key, Type: x.Type, Title: x.Title, Summary: x.Summary, Status: x.Status, DataClass: x.DataClass},
		Aliases: x.Aliases, Body: x.Body, Sources: x.Sources, Taint: x.Taint, CompiledBy: x.CompiledBy, CompiledAt: x.CompiledAt,
		Digest: x.Digest, Release: x.Release,
	}, true, nil
}

func (r kbRepo) PlatformGuides(ctx context.Context, key string, rd storage.KBReader) ([]storage.KBGuideRef, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.KBPlatformPageGuides(ctx, sqlcdb.KBPlatformPageGuidesParams{Key: key, Roles: nonNil(rd.Roles), Classes: nonNil(rd.Classes)})
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.KBGuideRef, 0, len(rows))
	for _, x := range rows {
		out = append(out, storage.KBGuideRef{File: x.File, Key: x.Key, Title: x.Title, Draft: x.Draft})
	}
	return out, nil
}

func (r kbRepo) PlatformSearch(ctx context.Context, s storage.KBSearch) ([]storage.KBHit, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.KBPlatformSearch(ctx, sqlcdb.KBPlatformSearchParams{Terms: nonNil(s.Terms), Roles: nonNil(s.Reader.Roles),
		Classes: nonNil(s.Reader.Classes), Scope: s.Scope, MaxHits: int32(min(max(s.Limit, 1), 50))}) // #nosec G115 -- capped; the function refuses more than 50
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.KBHit, 0, len(rows))
	for _, x := range rows {
		out = append(out, storage.KBHit{Kind: x.Kind, Key: x.Key, Title: x.Title, Summary: x.Summary, Snippet: x.Snippet, Rank: x.Rank, Draft: x.Draft})
	}
	return out, nil
}
