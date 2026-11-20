//go:build local

package postgres_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A third-party AI app's record keeps what an Administrator registered (AI-46; migration 001153): only its removal is noted, once, and nothing is deleted or changed by
// any role, the worker included; another organisation never sees it (RLS).
// req: AI-46
// asvs: V8.2.2, V4.1.1
func TestAThirdPartyAIAppsRecordKeepsWhatWasRegistered(t *testing.T) {
	b := newRunBooks(t, "Third party apps Pte Ltd")
	ctx := context.Background()
	client := "mcp-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:20]
	entry := uuid.New()
	if err := store(t, "fyniq_api").Do(signedIn(), b.org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		_, err = u.MCPThirdPartyApps().Register(ctx, storage.MCPThirdPartyApp{ClientID: client, Name: "Acme", Publisher: "Acme Pte Ltd", RedirectURIs: []string{"https://acme.example/cb"},
			Purpose: "Questions", ProcessingLocation: "United States", OutsideSingapore: true, InventoryEntry: entry, RegisteredBy: b.owner, RegisteredAt: b.now})
		return err
	}); err != nil {
		t.Fatalf("registering: %v", err)
	}
	// removed once, with who and when; a second removal finds nothing
	if err := store(t, "fyniq_api").Do(signedIn(), b.org, func(ctx context.Context, u storage.UnitOfWork) error {
		gone, err := u.MCPThirdPartyApps().Remove(ctx, client, b.owner, b.now)
		if err != nil || gone.RemovedAt == nil || gone.Name != "Acme" {
			t.Errorf("removing: %+v %v", gone, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	err := store(t, "fyniq_api").Do(signedIn(), b.org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.MCPThirdPartyApps().Remove(ctx, client, b.owner, b.now)
		return err
	})
	if !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("removing it twice: %v", err)
	}
	// the database's own fences, raw, as an attacker with the API's or the worker's role would try them
	for _, c := range []struct{ name, role, sql string }{
		{"delete", "fyniq_api", `DELETE FROM fyniq.mcp_third_party_app`},
		{"a rename", "fyniq_api", `UPDATE fyniq.mcp_third_party_app SET name = 'Fyniq'`},
		{"new redirects", "fyniq_api", `UPDATE fyniq.mcp_third_party_app SET redirect_uris = ARRAY['https://evil.example/cb']`},
		{"the worker", "fyniq_worker", `UPDATE fyniq.mcp_third_party_app SET removed_at = NULL`},
		{"the executor", "fyniq_executor", `INSERT INTO fyniq.mcp_third_party_app (client_id, name, publisher, redirect_uris, purpose, processing_location, outside_singapore, inventory_entry, registered_by, registered_at) VALUES ('mcp-aaaaaaaaaaaaaaaaaaaa','x','x',ARRAY['https://x.example/cb'],'x','x',false,gen_random_uuid(),gen_random_uuid(),now())`},
	} {
		err := asOrg(ctx, conn(t, c.role), b.org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.sql); return err })
		if err == nil {
			t.Errorf("%s was allowed", c.name)
		}
	}
}
