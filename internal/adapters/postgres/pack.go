package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/packs"
)

// LoadShippedPacks validates the pack versions this release embeds (packs/) and loads them as the role in
// url, which must be fyniq_migrator: the release is the only path that writes pack contents (§11.4,
// ENG-30). Migrate runs it after the migrations.
func LoadShippedPacks(ctx context.Context, url string, log io.Writer) error {
	versions, err := pack.LoadAll(packs.FS)
	if err != nil {
		return fmt.Errorf("the statutory packs are refused: %w", err)
	}
	return LoadPacks(ctx, url, versions, log)
}

// LoadPacks copies versions into the global pack tables, each in its own transaction. A version already
// loaded with the same digest is skipped; with another digest it is refused (FY110), as a loaded version
// never changes: a correction is a new version.
func LoadPacks(ctx context.Context, url string, versions []pack.Version, log io.Writer) error {
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		return fmt.Errorf("connect as fyniq_migrator: %w", err)
	}
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()
	for _, v := range versions {
		loaded, err := loadPack(ctx, conn, v)
		if err != nil {
			return fmt.Errorf("load %s: %w", v.ID, err)
		}
		if loaded {
			fmt.Fprintf(log, "loaded statutory pack %s (%s)\n", v.ID, v.Digest[:12])
		}
	}
	return nil
}

func loadPack(ctx context.Context, conn *pgx.Conn, v pack.Version) (bool, error) {
	q := sqlcdb.New(conn)
	have, err := q.PackVersionDigest(ctx, v.ID)
	switch {
	case err == nil && have == v.Digest:
		return false, nil
	case err == nil:
		return false, &storage.Error{Code: storage.CodePack,
			Message: fmt.Sprintf("%s is already loaded with other contents (digest %s, now %s)", v.ID, have[:12], v.Digest[:12]),
			Hint:    "A loaded pack version never changes: publish the correction as a new version, <pack>@<year>.<n+1> (ENG-30, ENG-31)"}
	case !errors.Is(err, pgx.ErrNoRows):
		return false, mapError(err)
	}
	sources, err := json.Marshal(v.Sources)
	if err != nil {
		return false, err
	}
	type packRow struct {
		Table  string            `json:"table_name"`
		Pinned bool              `json:"pinned"`
		RowNo  int               `json:"row_no"`
		Cells  map[string]string `json:"cells"`
	}
	var rows []packRow
	for _, set := range []struct {
		tables map[string]pack.Table
		pinned bool
	}{{v.Tables, false}, {v.Pinned, true}} {
		names := make([]string, 0, len(set.tables))
		for n := range set.tables {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			t := set.tables[n]
			for i, row := range t.Rows {
				cells := make(map[string]string, len(row))
				for c, col := range t.Columns {
					cells[col] = row[c]
				}
				rows = append(rows, packRow{Table: n, Pinned: set.pinned, RowNo: i + 1, Cells: cells})
			}
		}
	}
	rowsJSON, err := json.Marshal(rows)
	if err != nil {
		return false, err
	}
	p := sqlcdb.InsertPackVersionParams{ID: v.ID, Pack: v.Pack, Title: v.Title, EffectiveFrom: v.EffectiveFrom,
		ReviewDate: v.ReviewDate, Verifier: v.Verifier, Conditions: v.Conditions, Sources: sources, Digest: v.Digest, Rows: rowsJSON}
	if v.EffectiveTo != nil {
		p.EffectiveTo = pgtype.Date{Time: *v.EffectiveTo, Valid: true}
	}
	if v.Supersedes != "" {
		p.Supersedes = &v.Supersedes
	}
	if v.TaxPointRule != "" {
		p.TaxPointRule = &v.TaxPointRule
	}
	// One statement: the version and its rows are stored together or not at all.
	if err := q.InsertPackVersion(ctx, p); err != nil {
		return false, mapError(err)
	}
	return true, nil
}

// Packs implements storage.UnitOfWork: the loaded versions through the read-only view, and the
// organisation's activations under its row-level security.
func (u *unitOfWork) Packs() storage.Packs { return packsRepo{u} }

type packsRepo struct{ u *unitOfWork }

func (r packsRepo) Versions(ctx context.Context) ([]storage.PackVersion, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.PackVersions(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.PackVersion, 0, len(rows))
	for _, row := range rows {
		v := storage.PackVersion{ID: row.ID, Pack: row.Pack, Digest: row.Digest, EffectiveFrom: row.EffectiveFrom,
			ReviewDate: row.ReviewDate, LoadedAt: row.LoadedAt}
		if row.EffectiveTo.Valid {
			to := row.EffectiveTo.Time
			v.EffectiveTo = &to
		}
		out = append(out, v)
	}
	return out, nil
}

func (r packsRepo) Activations(ctx context.Context) ([]storage.PackActivation, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ActivePackVersions(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.PackActivation, 0, len(rows))
	for _, row := range rows {
		out = append(out, storage.PackActivation{VersionID: row.VersionID, ProposedBy: row.ProposedBy, ApprovedBy: row.ApprovedBy,
			Approval: row.ApprovalID, ActivatedAt: row.ActivatedAt})
	}
	return out, nil
}
