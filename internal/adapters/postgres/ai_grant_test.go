//go:build local

package postgres_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The assistant's delegated read grants keep their rules whoever writes (PRD-001 §9.3 rev 0.277, AI-01, AI-36): a
// grant resolves, in the organisation its token names, to the person and their roles there now; it lives at most 5
// minutes, ends with its turn, once, and then resolves to nothing, as it does in another organisation, past its
// expiry, or for someone who is not a member. Only its hash is kept; nothing of it changes but its end; the worker
// issues none and the AI's role touches none (CMP-AI-01); another organisation sees none of it.
// req: AI-01
// req: ai/proposals The orchestrator holds no write credential
// req: QA-ENG-05
// asvs: V7.2.3, V7.4.1, V8.2.2
func TestAttackAIGrantsReadOnlyForTheirTurn(t *testing.T) {
	ctx := context.Background()
	a, b := books(t, "Grants A Pte Ltd"), books(t, "Grants B Pte Ltd")
	api := store(t, "fyniq_api")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	person, outsider := founder(t, api, a.Org, 1, now), founder(t, api, b.Org, 2, now)
	issue := func(org, user, turn uuid.UUID) (string, error) {
		token, hash, err := authn.NewAIToken(org)
		if err != nil {
			t.Fatal(err)
		}
		return token, api.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.AIGrants().Issue(ctx, storage.AIGrant{TokenHash: hash, UserID: user, Turn: turn, IssuedAt: now, ExpiresAt: now.Add(authn.MaxDelegated)})
			return err
		})
	}
	resolve := func(token string, at time.Time) (storage.AIGrantHolder, error) {
		org, hash, ok := authn.ParseAIToken(token)
		if !ok {
			t.Fatalf("not a token: %q", token)
		}
		return api.AIGrant(ctx, org, hash, at)
	}

	turn := uuid.New()
	token, err := issue(a.Org, person, turn)
	if err != nil {
		t.Fatal(err)
	}
	if g, err := resolve(token, now.Add(time.Minute)); err != nil || g.UserID != person || g.Turn != turn || len(g.Roles) != 1 || g.Roles[0] != "owner" {
		t.Fatalf("a live grant: %+v %v", g, err)
	}
	// Its organisation is in the token, not a secret: naming another one finds nothing.
	_, hash, _ := authn.ParseAIToken(token)
	if _, err := api.AIGrant(ctx, b.Org, hash, now); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("the grant in another organisation: %v", err)
	}
	if _, err := resolve(token, now.Add(authn.MaxDelegated)); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("an expired grant: %v", err)
	}
	if _, err := resolve(token, now.Add(-time.Second)); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("a grant before it was issued: %v", err)
	}
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error { return u.AIGrants().End(ctx, turn, now) }); err != nil {
		t.Fatal(err)
	}
	if _, err := resolve(token, now); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("an ended grant: %v", err)
	}
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error { return u.AIGrants().End(ctx, turn, now) }); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("ending twice: %v", err)
	}
	// Someone who is not a member of the organisation reads nothing there, and a turn has one grant.
	stranger, err := issue(a.Org, outsider, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolve(stranger, now); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("a grant for a non-member: %v", err)
	}
	if _, err := issue(a.Org, person, turn); err == nil {
		t.Error("a second grant for the turn")
	}
	if _, err := issue(a.Org, person, uuid.New()); err != nil {
		t.Fatal(err)
	}

	for _, x := range []struct {
		what, role, stmt string
		states           []string
	}{
		{"the owner moving a grant to another person", "fyniq_migrator", `UPDATE fyniq.ai_grant SET user_id = '` + outsider.String() + `'`, []string{"FY340"}},
		{"the owner lengthening a grant", "fyniq_migrator", `UPDATE fyniq.ai_grant SET expires_at = expires_at + interval '1 hour'`, []string{"FY340"}},
		{"the owner reopening a grant", "fyniq_migrator", `UPDATE fyniq.ai_grant SET ended_at = NULL WHERE ended_at IS NOT NULL`, []string{"FY340"}},
		{"the owner removing a grant", "fyniq_migrator", `DELETE FROM fyniq.ai_grant`, []string{"FY340"}},
		{"the owner emptying them", "fyniq_migrator", `TRUNCATE fyniq.ai_grant`, []string{"FY060", "0A000"}},
		{"a grant of an hour", "fyniq_api", `INSERT INTO fyniq.ai_grant (token_hash, user_id, turn_id, issued_at, expires_at)
		  VALUES (sha256('x'), '` + person.String() + `', gen_random_uuid(), now(), now() + interval '1 hour')`, []string{"23514"}},
		{"a token kept in the clear", "fyniq_api", `INSERT INTO fyniq.ai_grant (token_hash, user_id, turn_id, issued_at, expires_at)
		  VALUES ('fyai_secret', '` + person.String() + `', gen_random_uuid(), now(), now() + interval '1 minute')`, []string{"23514"}},
		{"the API changing who a grant reads as", "fyniq_api", `UPDATE fyniq.ai_grant SET user_id = '` + outsider.String() + `'`, []string{"42501"}},
		{"the worker issuing a grant", "fyniq_worker", `INSERT INTO fyniq.ai_grant (token_hash, user_id, turn_id, issued_at, expires_at)
		  VALUES (sha256('y'), '` + person.String() + `', gen_random_uuid(), now(), now() + interval '1 minute')`, []string{"42501"}},
		{"the worker resolving a grant", "fyniq_worker", `SELECT * FROM fyniq.ai_grant_for(gen_random_uuid(), sha256('x'), now())`, []string{"42501"}},
		{"the AI reading the grants", "fyniq_ai", `SELECT count(*) FROM fyniq.ai_grant`, []string{"42501"}},
		{"the AI resolving a grant", "fyniq_ai", `SELECT * FROM fyniq.ai_grant_for(gen_random_uuid(), sha256('x'), now())`, []string{"42501"}},
	} {
		err := asOrg(ctx, conn(t, x.role), a.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, x.stmt); return err })
		wantState(t, x.what, err, x.states...)
	}
	// Only hashes are kept, and another organisation sees none of them.
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		return u.AIGrants().End(ctx, turn, now)
	}); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("another organisation ending the grant: %v", err)
	}
	var kept []byte
	if err := asOrg(ctx, conn(t, "fyniq_api"), a.Org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT token_hash FROM fyniq.ai_grant WHERE turn_id = $1`, turn).Scan(&kept)
	}); err != nil || len(kept) != 32 || bytes.Contains([]byte(token), kept) {
		t.Errorf("the kept hash: %x %v", kept, err)
	}
}
