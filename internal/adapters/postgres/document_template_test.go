//go:build local

package postgres_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// FR-TPL-04 (rev 0.95): a template's draft is saved over at the next version; publishing makes the next numbered
// version, which never changes, and moves the template's version in force with it; a template keeps its kind and type
// and is never deleted (FY176). Every change is audited on the template, and another organisation sees none of it.
// req: FR-TPL-04
func TestTemplateVersionsNeverChange(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "accountant"}})
	s := store(t, "fyniq_api")
	b := books(t, "Templates Pte Ltd")
	other := books(t, "Others Pte Ltd")
	draft := []byte(`{"schema":"fyniq.template/1","subject":"Invoice {{document.number}}","body":"Dear {{customer.name}}"}`)
	sum := func(b []byte) []byte { h := sha256.Sum256(b); return h[:] }
	var tpl storage.DocumentTemplate
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		tpl, err = u.DocumentTemplates().Create(ctx, storage.NewDocumentTemplate{Kind: "email", DocumentType: "invoice", Name: "Invoice email", Draft: draft})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if tpl.Version != 1 || tpl.Published != 0 || tpl.SavedBy != "user:accountant" {
		t.Fatalf("a new template: %+v", tpl)
	}
	publish := func(version int, content []byte, from int) (storage.DocumentTemplate, storage.TemplateVersion, error) {
		var out storage.DocumentTemplate
		var v storage.TemplateVersion
		err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			out, v, err = u.DocumentTemplates().Publish(ctx, tpl.ID, version, storage.NewTemplateVersion{Content: content, Hash: sum(content), Note: "First", RepublishedFrom: from})
			return err
		})
		return out, v, err
	}
	tpl, v1, err := publish(1, draft, 0)
	if err != nil {
		t.Fatal(err)
	}
	if tpl.Version != 2 || tpl.Published != 1 || v1.Number != 1 || v1.Note != "First" || !bytes.Equal(v1.Hash, sum(draft)) {
		t.Fatalf("published: %+v %+v", tpl, v1)
	}
	if _, _, err := publish(1, draft, 0); !storageCode(err, storage.CodeVersion) {
		t.Errorf("a publication from a stale version: %v", err)
	}
	changed := []byte(`{"schema":"fyniq.template/1","subject":"Invoice {{document.number}} is due","body":"Dear {{customer.name}}"}`)
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if _, err := u.DocumentTemplates().SaveDraft(ctx, tpl.ID, 1, "Invoice email", changed); !storageCode(err, storage.CodeVersion) {
			t.Errorf("a stale save: %v", err)
		}
		tpl, err = u.DocumentTemplates().SaveDraft(ctx, tpl.ID, tpl.Version, "Invoice email", changed)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if tpl.Version != 3 || tpl.Published != 1 {
		t.Fatalf("a saved draft: %+v", tpl)
	}
	if tpl, _, err = publish(3, changed, 0); err != nil {
		t.Fatal(err)
	}
	tpl, v3, err := publish(tpl.Version, draft, 1) // the rollback
	if err != nil {
		t.Fatal(err)
	}
	if v3.Number != 3 || v3.RepublishedFrom != 1 || tpl.Published != 3 {
		t.Errorf("the rollback: %+v", v3)
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		vs, err := u.DocumentTemplates().Versions(ctx, tpl.ID, 0, 2)
		if err != nil || len(vs) != 2 || vs[0].Number != 3 || vs[1].Number != 2 {
			t.Errorf("the latest versions: %v %v", vs, err)
		}
		if vs, _ = u.DocumentTemplates().Versions(ctx, tpl.ID, 2, 10); len(vs) != 1 || vs[0].Number != 1 {
			t.Errorf("the versions before 2: %v", vs)
		}
		if _, err := u.DocumentTemplates().Version(ctx, tpl.ID, 4); !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("version 4: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := store(t, "fyniq_readonly").Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		events, err := u.Audit().Events(ctx, 0, 1000)
		n := 0
		for _, e := range events {
			if bytes.Contains([]byte(e.Body), []byte(`"document_template.`)) {
				n++
			}
		}
		if n != 5 { // made, published, saved, published, rolled back
			t.Errorf("%d template events, want 5", n)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Do(ctx, other.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		if _, err := u.DocumentTemplates().Get(ctx, tpl.ID); !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("another organisation reads the template: %v", err)
		}
		ts, err := u.DocumentTemplates().Page(ctx, [16]byte{}, 10, storage.TemplateFilter{})
		if err != nil || len(ts) != 0 {
			t.Errorf("another organisation lists %d templates (%v)", len(ts), err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	id := tpl.ID.String()
	for _, c := range []struct{ role, stmt, state string }{
		{"fyniq_api", "UPDATE fyniq.document_template_version SET note = 'changed'", "42501"},
		{"fyniq_api", "DELETE FROM fyniq.document_template_version", "42501"},
		{"fyniq_api", "DELETE FROM fyniq.document_template", "42501"},
		{"fyniq_migrator", "UPDATE fyniq.document_template_version SET note = 'changed'", "FY176"},
		{"fyniq_migrator", "DELETE FROM fyniq.document_template_version", "FY176"},
		{"fyniq_migrator", "DELETE FROM fyniq.document_template", "FY176"},
		{"fyniq_migrator", "UPDATE fyniq.document_template SET kind = 'document', version = version + 1", "FY176"},
		{"fyniq_api", "UPDATE fyniq.document_template SET version = version + 2", "FY176"},
		{"fyniq_api", "UPDATE fyniq.document_template SET published = 4, version = version + 1", "FY176"},
		{"fyniq_api", "UPDATE fyniq.document_template SET published = 2, version = version + 1", "FY176"},
		{"fyniq_api", "INSERT INTO fyniq.document_template_version (template_id, number, content, content_hash, published_by, published_at) " +
			"VALUES ('" + id + "', 5, '{}', sha256('x'), 'user:x', now())", "FY176"},
		{"fyniq_api", "INSERT INTO fyniq.document_template_version (template_id, number, content, content_hash, published_by, published_at) " +
			"VALUES ('" + id + "', 3, '{}', sha256('x'), 'user:x', now())", "FY176"},
		{"fyniq_api", "INSERT INTO fyniq.document_template_version (template_id, number, content, content_hash, republished_from, published_by, published_at) " +
			"VALUES ('" + id + "', 4, '{}', sha256('x'), 4, 'user:x', now())", "23514"},
		{"fyniq_api", "INSERT INTO fyniq.document_template (kind, document_type, name, draft, saved_by, saved_at) " +
			"VALUES ('letter', 'invoice', 'x', '{}', 'user:x', now())", "23514"},
		{"fyniq_api", "INSERT INTO fyniq.document_template (kind, document_type, name, draft, saved_by, saved_at) " +
			"VALUES ('email', 'payslip', 'x', '{}', 'user:x', now())", "23514"},
		{"fyniq_api", "INSERT INTO fyniq.document_template (kind, document_type, name, draft, saved_by, saved_at) " +
			"VALUES ('email', 'invoice', 'x', '[]', 'user:x', now())", "23514"},
		{"fyniq_api", "INSERT INTO fyniq.document_template (kind, document_type, name, draft, saved_by, saved_at) " +
			"VALUES ('email', 'invoice', 'x', '{}', 'user:x', now())", "FY061"}, // no audit event in its transaction
		{"fyniq_worker", "UPDATE fyniq.document_template SET name = 'x', version = version + 1", "42501"},
		{"fyniq_readonly", "INSERT INTO fyniq.document_template (kind, document_type, name, draft, saved_by, saved_at) " +
			"VALUES ('email', 'invoice', 'x', '{}', 'user:x', now())", "42501"},
		{"fyniq_migrator", "TRUNCATE fyniq.document_template_version CASCADE", "FY002"},
	} {
		err := asOrg(ctx, conn(t, c.role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.stmt); return err })
		wantState(t, c.role+": "+c.stmt, err, c.state)
	}
}

// FR-TPL-05 (rev 0.99): a default or a contact's choice of template is append-only, the latest in force, and names only a
// published template of its own kind and type (FY177, FY178); a contact's choice is audited on the contact.
// req: FR-TPL-05
func TestTemplateChoicesAreAppendOnly(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "owner"}})
	s := store(t, "fyniq_api")
	b := books(t, "Choices Pte Ltd")
	content := []byte(`{"schema":"fyniq.template/1","subject":"Invoice {{document.number}}","body":"Dear {{customer.name}}"}`)
	sum := sha256.Sum256(content)
	var published, draft storage.DocumentTemplate
	var customer storage.Contact
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if published, err = u.DocumentTemplates().Create(ctx, storage.NewDocumentTemplate{Kind: "email", DocumentType: "invoice", Name: "Invoice email", Draft: content}); err != nil {
			return err
		}
		if published, _, err = u.DocumentTemplates().Publish(ctx, published.ID, 1, storage.NewTemplateVersion{Content: content, Hash: sum[:]}); err != nil {
			return err
		}
		if draft, err = u.DocumentTemplates().Create(ctx, storage.NewDocumentTemplate{Kind: "email", DocumentType: "invoice", Name: "Draft email", Draft: content}); err != nil {
			return err
		}
		customer, err = u.Contacts().Create(ctx, storage.ContactDetails{LegalName: "Acme Trading Pte Ltd", Customer: true})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		if _, err := u.DocumentTemplates().SetDefault(ctx, "email", "invoice", &published.ID); err != nil {
			return err
		}
		if _, err := u.DocumentTemplates().SetDefault(ctx, "email", "quote", nil); err != nil {
			return err
		}
		_, err := u.DocumentTemplates().SetContactTemplate(ctx, customer.ID, "email", "invoice", &published.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		ds, err := u.DocumentTemplates().Defaults(ctx)
		if err != nil || len(ds) != 2 || ds[0].DocumentType != "invoice" || *ds[0].TemplateID != published.ID || ds[1].TemplateID != nil {
			t.Errorf("the defaults: %+v %v", ds, err)
		}
		cs, err := u.DocumentTemplates().ContactTemplates(ctx, customer.ID)
		if err != nil || len(cs) != 1 || *cs[0].ContactID != customer.ID || cs[0].SetBy != "user:owner" {
			t.Errorf("the contact's choices: %+v %v", cs, err)
		}
		// A draft, or a template of another type, is refused by the database too.
		if _, err := u.DocumentTemplates().SetDefault(ctx, "email", "invoice", &draft.ID); !storageCode(err, storage.CodeTemplate) {
			t.Errorf("a draft as the default: %v", err)
		}
		return nil
	}); err == nil {
		t.Error("the refused choice committed")
	}
	for _, c := range []struct{ role, stmt, state string }{
		{"fyniq_api", "UPDATE fyniq.document_template_default SET template_id = NULL", "42501"},
		{"fyniq_migrator", "UPDATE fyniq.document_template_default SET template_id = NULL", "FY177"},
		{"fyniq_migrator", "DELETE FROM fyniq.contact_document_template", "FY177"},
		{"fyniq_api", "INSERT INTO fyniq.document_template_default (kind, document_type, template_id, set_by, set_at) VALUES ('document', 'invoice', '" +
			published.ID.String() + "', 'user:x', now())", "FY178"},
		{"fyniq_api", "INSERT INTO fyniq.contact_document_template (contact_id, kind, document_type, template_id, set_by, set_at) VALUES ('" +
			customer.ID.String() + "', 'email', 'quote', '" + published.ID.String() + "', 'user:x', now())", "FY178"},
		{"fyniq_api", "INSERT INTO fyniq.document_template_default (kind, document_type, set_by, set_at) VALUES ('email', 'invoice', 'user:x', now())", "FY061"},
		{"fyniq_worker", "INSERT INTO fyniq.document_template_default (kind, document_type, set_by, set_at) VALUES ('email', 'invoice', 'user:x', now())", "42501"},
	} {
		err := asOrg(ctx, conn(t, c.role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.stmt); return err })
		wantState(t, c.role+": "+c.stmt, err, c.state)
	}
}
