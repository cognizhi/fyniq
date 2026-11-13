//go:build local

package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/adapters/postgres/pgtest"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/domain/backup"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The restore drill, on its own database: a copy is made from it as a template while nobody is connected, the ledger's
// and the audit chain's verifications run on the copy as the worker's role, an anchor written after the copy's point in
// time is counted, not failed (where the nightly verification fails on it), the copy is removed, and the drill recorded
// in the installation's trail. A chain that disagrees with an anchor at or before the head fails the drill, which still
// removes its copy and records the failure (DR-06, NFR-DR-03, CTL-RES-02; p1-operations-dr design D2).
// req: DR-06
// req: NFR-DR-03
// req: CTL-RES-02
// asvs: V16.4.2
func TestTheRestoreDrill(t *testing.T) {
	ctx := context.Background()
	// Its own database: the template copy needs nobody connected to the source, and the drill verifies every organisation.
	own, err := pgtest.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(own.Stop)
	at := func(role, database string) string {
		u, err := url.Parse(own.URL(role))
		if err != nil {
			t.Fatal(err)
		}
		if database != "" {
			u.Path = "/" + database
		}
		return u.String()
	}
	connect := func(u string) *postgres.Store {
		s, err := postgres.Connect(u) // lazily: no connection until it is used
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	api, worker := connect(at("fyniq_api", "")), connect(at("fyniq_worker", ""))
	b := porttest.NewBooks(t, api, "Drill Pte Ltd")
	svc := journal.Service{Store: api}
	doc, err := svc.Create(ctx, b.Org, saleDraft(t, b, porttest.Day(10, 14), "250.00"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Post(ctx, b.Org, doc.ID); err != nil {
		t.Fatal(err)
	}
	frozen := clock.NewFrozen(time.Date(2099, 3, 1, 2, 0, 0, 0, time.UTC))
	blobs, err := local.NewDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	signer, err := local.LoadOrCreateEd25519(filepath.Join(t.TempDir(), "anchor.ed25519"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := audit.AnchorAll(ctx, worker, blobs, signer, frozen); err != nil {
		t.Fatal(err)
	}
	keys, err := blobs.List(ctx, audit.AnchorPrefix(b.Org))
	if err != nil || len(keys) != 1 {
		t.Fatalf("anchors: %v %v", keys, err)
	}
	head, err := strconv.ParseInt(strings.TrimSuffix(filepath.Base(keys[0]), ".json"), 10, 64)
	if err != nil || head < 2 {
		t.Fatalf("the head anchored: %q %v", keys[0], err)
	}
	writeJSON := func(key string, v any) {
		t.Helper()
		b, err := json.Marshal(v)
		if err == nil {
			err = blobs.Create(ctx, key, bytes.NewReader(b))
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	// An anchor of a later event, as one written after a copy's point in time is: the nightly verification fails on it.
	ahead, err := audit.Sign(ctx, signer, b.Org, head+5, []byte("a later head"), frozen.Now())
	if err != nil {
		t.Fatal(err)
	}
	writeJSON(audit.AnchorKey(b.Org, head+5), ahead)
	if _, err := audit.VerifyAll(ctx, worker, blobs, signer, nil); err == nil {
		t.Fatal("the nightly verification passed an anchor past the head")
	}
	api.Close()
	worker.Close()

	drill := backup.Drill{
		Clone: postgres.TemplateClone{AdminURL: at("fyniq", "postgres"), Source: "fyniq", Clock: frozen},
		Open: func(_ context.Context, c ports.CloneTarget) (storage.Store, func(), error) {
			s, err := postgres.Connect(at("fyniq_worker", c.Database))
			if err != nil {
				return nil, func() {}, err
			}
			return s, s.Close, nil
		},
		Store: connect(at("fyniq_worker", "")), Installation: installationAt(b.Org), Blobs: blobs, Signer: signer, Clock: frozen,
	}
	rep, err := drill.Run(ctx)
	if err != nil || !rep.OK() || rep.Organisations != 1 || rep.LedgerFailed != 0 || rep.ChainsFailed != 0 || rep.Ahead != 1 ||
		!rep.Removed || rep.Copy != "fyniq_drill_20990301_020000" || !rep.PointInTime.Equal(frozen.Now().Add(-time.Hour)) {
		t.Fatalf("the drill: %+v, %v", rep, err)
	}
	databases := func() []string {
		t.Helper()
		c, err := pgx.Connect(ctx, at("fyniq", "postgres"))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = c.Close(ctx) }()
		rows, err := c.Query(ctx, "SELECT datname FROM pg_database WHERE datname LIKE 'fyniq%' ORDER BY datname")
		if err != nil {
			t.Fatal(err)
		}
		names, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			t.Fatal(err)
		}
		return names
	}
	if got := databases(); len(got) != 1 || got[0] != "fyniq" {
		t.Errorf("the copy was not removed: %v", got)
	}
	drilled := func(status string) int {
		t.Helper()
		c, err := pgx.Connect(ctx, at("fyniq_readonly", ""))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = c.Close(ctx) }()
		var n int
		if err := asOrg(ctx, c, b.Org, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM fyniq.audit_event WHERE action = 'restore.drilled'
			  AND body::jsonb->'changes'->'status'->>1 = $1 AND body::jsonb->'changes'->'copy_removed'->>1 = 'true'`, status).Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := drilled("passed"); n != 1 {
		t.Errorf("the drill's record: %d", n)
	}

	// An anchor at or before the head that the chain disagrees with fails the drill; its copy goes all the same.
	forged, err := audit.Sign(ctx, signer, b.Org, 1, []byte("a rewritten chain"), frozen.Now())
	if err != nil {
		t.Fatal(err)
	}
	writeJSON(audit.AnchorKey(b.Org, 1), forged)
	frozen.Set(frozen.Now().Add(time.Minute))
	rep, err = drill.Run(ctx)
	if err == nil || rep.OK() || rep.ChainsFailed != 1 || !rep.Removed || !strings.Contains(rep.Problem, "anchor at event 1") {
		t.Fatalf("a drill on a rewritten chain: %+v, %v", rep, err)
	}
	if got := databases(); len(got) != 1 {
		t.Errorf("the failed drill's copy was not removed: %v", got)
	}
	if n := drilled("failed"); n != 1 {
		t.Errorf("the failed drill's record: %d", n)
	}
}
