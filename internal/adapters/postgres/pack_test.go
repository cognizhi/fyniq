//go:build local

package postgres_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/packs"
)

// req: ENG-30
// req: FR-ORG-05
// req: platform/release-and-versioning Statutory parameter pack versions
// The migrator loads the packs the release ships; every service reads them through the views; loading
// again is a no-op, and loading a version again with other contents is refused.
func TestTheMigratorLoadsTheShippedPacksOnce(t *testing.T) {
	ctx := context.Background()
	versions, err := pack.LoadAll(packs.FS)
	if err != nil {
		t.Fatal(err)
	}
	var id, digest string
	var rates, pinned int
	c := conn(t, "fyniq_api")
	if err := c.QueryRow(ctx, "SELECT id, digest FROM fyniq.statutory_pack_versions WHERE id = 'sg-gst@2026.1'").Scan(&id, &digest); err != nil {
		t.Fatal(err)
	}
	if digest != versions[0].Digest {
		t.Errorf("loaded digest %s; the shipped version's is %s", digest, versions[0].Digest)
	}
	if err := c.QueryRow(ctx, `SELECT count(*) FILTER (WHERE table_name = 'rates' AND NOT pinned), count(*) FILTER (WHERE pinned)
  FROM fyniq.statutory_pack_rows WHERE version_id = 'sg-gst@2026.1'`).Scan(&rates, &pinned); err != nil {
		t.Fatal(err)
	}
	if rates != len(versions[0].Tables["rates"].Rows) || pinned == 0 {
		t.Errorf("loaded %d rates and %d pinned cases", rates, pinned)
	}
	var rate string
	if err := c.QueryRow(ctx, `SELECT cells->>'rate' FROM fyniq.statutory_pack_rows
  WHERE version_id = 'sg-gst@2026.1' AND table_name = 'rates' AND NOT pinned AND cells->>'effective_from' = '2024-01-01'`).Scan(&rate); err != nil || rate != "0.09" {
		t.Errorf("the rate from 1 Jan 2024 reads %q (%v); want the decimal string 0.09", rate, err)
	}

	// The storage port reads the same, and an organisation has activated nothing.
	b := books(t, "Packs ENG-30 Pte Ltd")
	err = store(t, "fyniq_api").Do(ctx, b.Org, func(ctx context.Context, uow storage.UnitOfWork) error {
		vs, err := uow.Packs().Versions(ctx)
		if err != nil {
			return err
		}
		if len(vs) == 0 || vs[0].ID != "sg-gst@2026.1" || vs[0].Digest != versions[0].Digest || vs[0].EffectiveFrom.Format("2006-01-02") != "1994-04-01" {
			t.Errorf("the port reads %+v", vs)
		}
		as, err := uow.Packs().Activations(ctx)
		if err == nil && len(as) != 0 {
			t.Errorf("activations before the approval engine: %+v", as)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := postgres.LoadShippedPacks(ctx, db.URL("fyniq_migrator"), io.Discard); err != nil {
		t.Fatalf("loading the same packs again: %v", err)
	}
	changed := versions[0]
	changed.Digest = "0000000000000000000000000000000000000000000000000000000000000000"
	err = postgres.LoadPacks(ctx, db.URL("fyniq_migrator"), []pack.Version{changed}, io.Discard)
	var se *storage.Error
	if !errors.As(err, &se) || se.Code != storage.CodePack {
		t.Fatalf("loading sg-gst@2026.1 with other contents: got %v; want %s", err, storage.CodePack)
	}
}

// req: ENG-30
// req: data/data-model Global tables
// req: QA-ENG-05
// asvs: V8.2.2
// asvs: V13.2.2
// No application role can write pack contents, or read them but through the views; even the migrator,
// which owns them, cannot change or remove a loaded version.
func TestAttackServiceRolesCannotWritePackContents(t *testing.T) {
	ctx := context.Background()
	for _, role := range serviceRoles {
		c := conn(t, role)
		for name, stmt := range map[string]string{
			"insert a version":        `INSERT INTO fyniq.statutory_pack_version (id, pack, title, effective_from, review_date, verifier, conditions, sources, digest) VALUES ('sg-gst@2026.9', 'sg-gst', 'x', '2026-01-01', '2027-01-01', 'x', 'x', '[{}]', repeat('a', 64))`,
			"update a rate":           `UPDATE fyniq.statutory_pack_row SET cells = '{"rate": "0.07"}' WHERE version_id = 'sg-gst@2026.1'`,
			"delete a version":        `DELETE FROM fyniq.statutory_pack_version WHERE id = 'sg-gst@2026.1'`,
			"read the table":          `SELECT count(*) FROM fyniq.statutory_pack_row`,
			"update through the view": `UPDATE fyniq.statutory_pack_rows SET cells = '{"rate": "0.07"}' WHERE version_id = 'sg-gst@2026.1'`,
			"insert through the view": `INSERT INTO fyniq.statutory_pack_versions (id, pack, title, effective_from, review_date, verifier, conditions, sources, digest) VALUES ('sg-gst@2026.9', 'sg-gst', 'x', '2026-01-01', '2027-01-01', 'x', 'x', '[{}]', repeat('a', 64))`,
			"truncate":                `TRUNCATE fyniq.statutory_pack_row`,
		} {
			_, err := c.Exec(ctx, stmt)
			wantState(t, role+": "+name, err, "42501")
		}
	}
	m := conn(t, "fyniq_migrator")
	_, err := m.Exec(ctx, `UPDATE fyniq.statutory_pack_row SET cells = '{"rate": "0.07"}' WHERE version_id = 'sg-gst@2026.1' AND table_name = 'rates'`)
	wantState(t, "the migrator changes a loaded rate", err, "FY110")
	_, err = m.Exec(ctx, `DELETE FROM fyniq.statutory_pack_row WHERE version_id = 'sg-gst@2026.1'`)
	wantState(t, "the migrator removes a loaded version's rows", err, "FY110")
	_, err = m.Exec(ctx, `INSERT INTO fyniq.statutory_pack_row (version_id, table_name, pinned, row_no, cells)
VALUES ('sg-gst@2026.1', 'rates', false, 99, '{"rate": "0.10 [verify]"}')`)
	wantState(t, "a [verify] value", err, "23514")
	_, err = m.Exec(ctx, `INSERT INTO fyniq.statutory_pack_version (id, pack, title, effective_from, review_date, verifier, conditions, sources, digest)
VALUES ('sg-gst@2026.9', 'sg-gst', 'x', '2026-01-01', '2027-01-01', 'x', 'x', '[]', repeat('a', 64))`)
	wantState(t, "a version citing no source", err, "23514")
}

// req: ENG-32
// req: SOD-10
// req: QA-ENG-05
// asvs: V8.2.2
// An organisation's activation is its own audited tenant row. Only the API inserts one, as the record of the
// approved request to activate the version (rev 0.41; TestAttackPackActivationIsTheApprovedRequest), and the
// database refuses one approved by the person who proposed it.
func TestAttackActivationNeedsTwoPeopleAndAnAuditEvent(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Attack ENG-32 Pte Ltd")
	alice, bob := uuid.New(), uuid.New()
	insert := func(role string, proposer, approver uuid.UUID) error {
		return asOrg(ctx, conn(t, role), b.Org, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO fyniq.statutory_pack_activation (version_id, proposed_by, approved_by, approval_id)
VALUES ('sg-gst@2026.1', $1, $2, $3)`, proposer, approver, uuid.New())
			return err
		})
	}
	for _, role := range serviceRoles {
		want := "42501"
		if role == "fyniq_api" {
			want = "FY107" // the API may insert, only as the record of an approved request
		}
		wantState(t, role+" activates a pack version without an approved request", insert(role, alice, bob), want)
	}
	// As the owner, so only the table's own rules can refuse.
	wantState(t, "approved by its proposer", insert("fyniq_migrator", alice, alice), "FY107", "23514")
	wantState(t, "an activation without an approved request", insert("fyniq_migrator", alice, bob), "FY107")
	if n := count(t, b.Org, "statutory_pack_activation"); n != 0 {
		t.Errorf("%d activations were stored; want none", n)
	}
}
