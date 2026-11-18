//go:build local

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The organisation's knowledge base only goes forward, whoever writes (AI-50, AI-56, AI-57; p1-knowledge-base design D5): a
// note changes by its next version, a page's versions are numbered and kept as they were made, a publish makes the next
// snapshot out of the live one, the live pages are read only in the data classes the reader may read, and another
// organisation sees none of it. The database refuses a compiler's draft written by a person, a person's edit written as
// the compiler's, an overlay without its base, and any change to or removal of a version or a snapshot, even by the
// owner of the schema; the worker and the AI write none of it.
// asvs: V8.2.2, V16.3.3
func TestAttackTheOrganisationsKnowledgeBaseOnlyGoesForward(t *testing.T) {
	ctx := context.Background()
	a, b := books(t, "Knowledge A Pte Ltd"), books(t, "Knowledge B Pte Ltd")
	api := store(t, "fyniq_api")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	person := founder(t, api, a.Org, 1, now)
	var v1, v2 storage.KBVersion
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		k := u.OrganisationKnowledge()
		n, err := k.SaveNote(ctx, storage.KBNote{Key: "expense-policy", Kind: "policy", Title: "Expense policy", Body: "Claims within 30 days.", UpdatedBy: person, UpdatedAt: now}, 0)
		if err != nil || n.Version != 1 {
			t.Fatalf("the note: %+v %v", n, err)
		}
		n.Body = "Claims within 14 days."
		n.UpdatedAt = now.Add(time.Minute)
		if n, err = k.SaveNote(ctx, n, 1); err != nil || n.Version != 2 {
			t.Fatalf("the changed note: %+v %v", n, err)
		}
		if _, err := k.SaveNote(ctx, n, 1); storage.CodeOf(err) != storage.CodeVersion {
			t.Errorf("a note changed on a stale version: %v", err)
		}
		if v1, err = k.AddVersion(ctx, storage.KBVersion{Key: "org.expense-policy", Type: "policy", Title: "Expense policy", Summary: "How claims are made.",
			Aliases: []string{"claims rule"}, Body: "# Expense policy\n\n## Claims\n\nClaims are made within 14 days.\n", Origin: storage.KBOriginCompiled, AuthorKind: "compiler",
			Compiler: "claude-opus-5-5", PromptVersion: "kb-compile@1", CreatedAt: now}); err != nil || v1.Number != 1 {
			t.Fatalf("the draft: %+v %v", v1, err)
		}
		if v2, err = k.AddVersion(ctx, storage.KBVersion{Key: "org.expense-policy", Type: "policy", Title: "Expense policy", Summary: "How claims are made.",
			Body: "# Expense policy\n\n## Claims\n\nClaims are made within 21 days.\n", Origin: storage.KBOriginHumanEdited, HumanEdited: true, AuthorKind: "person",
			AuthorUser: person, CreatedAt: now.Add(time.Minute)}); err != nil || v2.Number != 2 {
			t.Fatalf("the edit: %+v %v", v2, err)
		}
		if _, err := k.AddVersion(ctx, storage.KBVersion{Key: "org.payroll-rules", Type: "policy", Title: "Pay rules", Summary: "How pay is decided.", Body: "# Pay\n\n## Rules\n\nPay is monthly.\n",
			DataClass: "payroll", Origin: storage.KBOriginHumanEdited, HumanEdited: true, AuthorKind: "person", AuthorUser: person, CreatedAt: now}); err != nil {
			t.Fatal(err)
		}
		if live, err := k.Published(ctx); err != nil || len(live) != 0 {
			t.Errorf("before a publish: %v %v", live, err)
		}
		if idx, err := k.LiveIndex(ctx, []string{"general"}); err != nil || len(idx) != 0 {
			t.Errorf("a draft is not live: %v %v", idx, err)
		}
		s1, err := k.Publish(ctx, map[string]int{"org.expense-policy": 1}, nil, &person, "first", now.Add(2*time.Minute))
		if err != nil || s1.Number != 1 {
			t.Fatalf("the first snapshot: %+v %v", s1, err)
		}
		s2, err := k.Publish(ctx, map[string]int{"org.expense-policy": 2, "org.payroll-rules": 1}, nil, &person, "second", now.Add(3*time.Minute))
		if err != nil || s2.Number != 2 {
			t.Fatalf("the second snapshot: %+v %v", s2, err)
		}
		live, _ := k.Published(ctx)
		if live["org.expense-policy"] != 2 || live["org.payroll-rules"] != 1 || len(live) != 2 {
			t.Errorf("the live pages: %v", live)
		}
		if snaps, err := k.Snapshots(ctx, 10); err != nil || len(snaps) != 2 || snaps[0].Number != 2 {
			t.Errorf("the snapshots: %+v %v", snaps, err)
		}
		// Only the data classes the reader may read.
		general, _ := k.LiveIndex(ctx, []string{"general"})
		all, _ := k.LiveIndex(ctx, []string{"general", "payroll"})
		if len(general) != 1 || general[0].Key != "org.expense-policy" || general[0].Number != 2 || len(all) != 2 {
			t.Errorf("the live index: %+v / %+v", general, all)
		}
		if _, ok, _ := k.LivePage(ctx, "org.payroll-rules", []string{"general"}); ok {
			t.Error("a payroll page opened to a reader without payroll")
		}
		if p, ok, err := k.LivePage(ctx, "org.expense-policy", []string{"general"}); err != nil || !ok || p.Number != 2 || p.Origin != storage.KBOriginHumanEdited {
			t.Errorf("the live page: %+v %v %v", p, ok, err)
		}
		hits, err := k.Search(ctx, []string{"claims", "rule"}, []string{"general"}, 5)
		if err != nil || len(hits) != 1 || hits[0].Key != "org.expense-policy" {
			t.Errorf("the search: %+v %v", hits, err)
		}
		if hits, _ := k.Search(ctx, []string{"monthly", "pay"}, []string{"general"}, 5); len(hits) != 0 {
			t.Errorf("a search found a payroll page: %+v", hits)
		}
		if vs, err := k.Versions(ctx, "org.expense-policy"); err != nil || len(vs) != 2 || vs[0].Number != 2 {
			t.Errorf("the versions: %+v %v", vs, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Another organisation sees nothing, however it asks.
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		k := u.OrganisationKnowledge()
		if idx, _ := k.LiveIndex(ctx, []string{"general", "payroll"}); len(idx) != 0 {
			t.Errorf("another organisation's index: %+v", idx)
		}
		if _, err := k.Note(ctx, "expense-policy"); err == nil {
			t.Error("another organisation read the note")
		}
		if hits, _ := k.Search(ctx, []string{"claims"}, []string{"general"}, 5); len(hits) != 0 {
			t.Errorf("another organisation's search: %+v", hits)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, x := range []struct {
		what, role, stmt string
		states           []string
	}{
		{"the owner rewriting a version", "fyniq_migrator", `UPDATE fyniq.kb_page_version SET body = 'Claims are made within 90 days.'`, []string{"FY561"}},
		{"the owner removing a version", "fyniq_migrator", `DELETE FROM fyniq.kb_page_version`, []string{"FY561", "23503"}},
		{"the owner rewriting a snapshot", "fyniq_migrator", `UPDATE fyniq.kb_snapshot SET reason = 'x'`, []string{"FY561"}},
		{"the owner removing a snapshot's pages", "fyniq_migrator", `DELETE FROM fyniq.kb_snapshot_page`, []string{"FY561"}},
		{"the owner emptying them", "fyniq_migrator", `TRUNCATE fyniq.kb_snapshot_page, fyniq.kb_snapshot, fyniq.kb_page_version`, []string{"FY060", "0A000"}},
		{"a note's version going back", "fyniq_migrator", `UPDATE fyniq.kb_note SET version = 1, body = 'x'`, []string{"FY561"}},
		{"a note's key changing", "fyniq_migrator", `UPDATE fyniq.kb_note SET key = 'other', version = version + 1`, []string{"FY561"}},
		{"a compiler's draft written by a person", "fyniq_migrator", `INSERT INTO fyniq.kb_page_version (key, number, type, title, summary, body, origin, human_edited, author_kind, author_user, created_at, search)
		   SELECT 'org.y', 1, 'concept', 't', 's', 'b', 'compiled', false, 'person', user_id, now(), ''::tsvector FROM fyniq.app_user LIMIT 1`, []string{"23514"}},
		{"a person's edit written as the compiler's", "fyniq_migrator", `INSERT INTO fyniq.kb_page_version (key, number, type, title, summary, body, origin, human_edited, author_kind, created_at, search)
		   VALUES ('org.y', 1, 'concept', 't', 's', 'b', 'human_edited', true, 'compiler', now(), ''::tsvector)`, []string{"23514"}},
		{"a compiled draft that says a person edited it", "fyniq_migrator", `INSERT INTO fyniq.kb_page_version (key, number, type, title, summary, body, origin, human_edited, author_kind, created_at, search)
		   VALUES ('org.y', 1, 'concept', 't', 's', 'b', 'compiled', true, 'compiler', now(), ''::tsvector)`, []string{"23514"}},
		{"an overlay without its base", "fyniq_migrator", `INSERT INTO fyniq.kb_page_version (key, number, type, title, summary, body, origin, human_edited, author_kind, created_at, search)
		   SELECT 'claims.make-a-claim', 1, 'concept', 't', 's', 'b', 'overlay', true, 'person', now(), ''::tsvector`, []string{"23514"}},
		{"a page of another's key that is no overlay", "fyniq_migrator", `INSERT INTO fyniq.kb_page_version (key, number, type, title, summary, body, origin, human_edited, author_kind, created_at, search)
		   VALUES ('claims.make-a-claim', 1, 'concept', 't', 's', 'b', 'rendered', false, 'system', now(), ''::tsvector)`, []string{"23514"}},
		{"an untrusted page", "fyniq_migrator", `INSERT INTO fyniq.kb_page_version (key, number, type, title, summary, body, taint, origin, human_edited, author_kind, created_at, search)
		   VALUES ('org.z', 1, 'concept', 't', 's', 'b', 'untrusted', 'rendered', false, 'system', now(), ''::tsvector)`, []string{"23514"}},
		{"a summary over 160 characters", "fyniq_migrator", `INSERT INTO fyniq.kb_page_version (key, number, type, title, summary, body, origin, human_edited, author_kind, created_at, search)
		   VALUES ('org.z', 1, 'concept', 't', repeat('s', 161), 'b', 'rendered', false, 'system', now(), ''::tsvector)`, []string{"23514"}},
		{"the API removing a snapshot", "fyniq_api", `DELETE FROM fyniq.kb_snapshot`, []string{"42501"}},
		{"the API changing a version", "fyniq_api", `UPDATE fyniq.kb_page_version SET body = 'x'`, []string{"42501"}},
		{"the worker writing a version", "fyniq_worker", `INSERT INTO fyniq.kb_page_version (key, number, type, title, summary, body, origin, human_edited, author_kind, created_at, search)
		   VALUES ('org.z', 1, 'concept', 't', 's', 'b', 'rendered', false, 'system', now(), ''::tsvector)`, []string{"42501"}},
		{"the executor writing a note", "fyniq_executor", `INSERT INTO fyniq.kb_note (key, kind, title, body, created_by, created_at, updated_by, updated_at)
		   VALUES ('x', 'note', 't', 'b', gen_random_uuid(), now(), gen_random_uuid(), now())`, []string{"42501"}},
		{"the AI reading the pages", "fyniq_ai", `SELECT count(*) FROM fyniq.kb_page_version`, []string{"42501"}},
		{"the AI reading what is live", "fyniq_ai", `SELECT count(*) FROM fyniq.kb_live_page`, []string{"42501"}},
		{"the AI writing a snapshot", "fyniq_ai", `INSERT INTO fyniq.kb_snapshot (number, created_at) VALUES (99, now())`, []string{"42501"}},
		{"the AI running the search", "fyniq_ai", `SELECT * FROM fyniq.kb_org_search(ARRAY['x'], ARRAY['general'], 5)`, []string{"42501"}},
	} {
		err := asOrg(ctx, conn(t, x.role), a.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, x.stmt); return err })
		wantState(t, x.what, err, x.states...)
	}
}

// A finding is open until a person resolves it, once, and stops its page's publication meanwhile (AI-52, AI-56; design D8).
// asvs: V8.2.2
func TestAFindingIsResolvedOnceByAPerson(t *testing.T) {
	ctx := context.Background()
	a := books(t, "Findings Pte Ltd")
	api := store(t, "fyniq_api")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	person := founder(t, api, a.Org, 1, now)
	var id uuid.UUID
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		k := u.OrganisationKnowledge()
		f, err := k.AddFinding(ctx, storage.KBFinding{Kind: "contradiction", PageKey: "org.expense-policy", PageVersion: 2, Detail: "says 14 days, the setting says 30", CreatedAt: now})
		if err != nil {
			return err
		}
		id = f.ID
		open, err := k.OpenFindings(ctx, "org.expense-policy")
		if err != nil || len(open) != 1 || open[0].Kind != "contradiction" || open[0].PageVersion != 2 {
			t.Errorf("the open findings: %+v %v", open, err)
		}
		if other, _ := k.OpenFindings(ctx, "org.other"); len(other) != 0 {
			t.Errorf("another page's findings: %+v", other)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		k := u.OrganisationKnowledge()
		if err := k.ResolveFinding(ctx, id, person, "The page is right: the setting changed", now.Add(time.Minute)); err != nil {
			return err
		}
		if open, _ := k.OpenFindings(ctx, "org.expense-policy"); len(open) != 0 {
			t.Errorf("a resolved finding is open: %+v", open)
		}
		if all, _ := k.Findings(ctx, "resolved", 10); len(all) != 1 || all[0].ResolvedBy == nil || *all[0].ResolvedBy != person {
			t.Errorf("the resolved findings: %+v", all)
		}
		if err := k.ResolveFinding(ctx, id, person, "again", now.Add(2*time.Minute)); err != ports.ErrNotFound {
			t.Errorf("resolved twice: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, x := range []struct {
		what, stmt string
		states     []string
	}{
		{"the owner reopening it", `UPDATE fyniq.kb_lint_finding SET status = 'open', resolved_at = NULL, resolved_by = NULL`, []string{"FY561"}},
		{"the owner rewriting what it found", `UPDATE fyniq.kb_lint_finding SET detail = 'nothing'`, []string{"FY561"}},
		{"the owner removing it", `DELETE FROM fyniq.kb_lint_finding`, []string{"FY561"}},
	} {
		err := asOrg(ctx, conn(t, "fyniq_migrator"), a.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, x.stmt); return err })
		wantState(t, x.what, err, x.states...)
	}
}
