//go:build local

package postgres_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The proposal executor's one write on a contact is the narrow query of ct.update_contact (migration 000960): the executor changes six details
// of an active company contact and no other column of it, and never the contact of a person (an individual, a director, a shareholder, an investor,
// a linked member) or an inactive one, whatever SQL it sends; the API's all-column update stays the API's.
// req: AI-10
// req: ai/task-catalogue ct.update_contact Update contact details
// asvs: V8.2.2, V8.3.1
func TestAttackTheExecutorChangesSixDetailsOfACompanyContactAndNothingElse(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Executor contact Pte Ltd")
	var company, person, inactive storage.Contact
	api := store(t, "fyniq_api")
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if company, err = u.Contacts().Create(ctx, storage.ContactDetails{LegalName: "Northwind Analytics", Customer: true, BillingEmail: "old@northwind.example", RemittanceEmail: "pay@northwind.example"}); err != nil {
			return err
		}
		if person, err = u.Contacts().Create(ctx, storage.ContactDetails{LegalName: "Mei Lin Tan", Customer: true, Individual: true, Phone: "+65 6000 0001"}); err != nil {
			return err
		}
		if inactive, err = u.Contacts().Create(ctx, storage.ContactDetails{LegalName: "Dormant Pte Ltd", Customer: true}); err != nil {
			return err
		}
		inactive, err = u.Contacts().Update(ctx, inactive.ID, inactive.Version, inactive.ContactDetails, false)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	exec := store(t, "fyniq_executor")
	// The six details change, at the version read, and nothing else moves.
	var after storage.Contact
	if err := exec.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		after, err = u.Contacts().UpdateAIFields(ctx, company.ID, company.Version, storage.ContactAIFields{SetBillingEmail: true, BillingEmail: "accounts@northwind.example", SetPhone: true, Phone: "+65 6000 0000"})
		return err
	}); err != nil {
		t.Fatalf("the narrow update by the executor: %v", err)
	}
	if after.BillingEmail != "accounts@northwind.example" || after.Phone != "+65 6000 0000" || after.RemittanceEmail != "pay@northwind.example" || after.Version != company.Version+1 {
		t.Errorf("the contact after: %+v", after)
	}
	// At a version that moved, it is refused.
	err := exec.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Contacts().UpdateAIFields(ctx, company.ID, company.Version, storage.ContactAIFields{SetPhone: true, Phone: "+65 6000 9999"})
		return err
	})
	if storage.CodeOf(err) != storage.CodeVersion {
		t.Errorf("a stale update: %v", err)
	}
	// The contact of a person, and an inactive one, are never the executor's, by the database.
	for name, c := range map[string]storage.Contact{"a person's contact": person, "an inactive contact": inactive} {
		err := exec.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Contacts().UpdateAIFields(ctx, c.ID, c.Version, storage.ContactAIFields{SetPhone: true, Phone: "+65 6000 1111"})
			return err
		})
		if err == nil {
			t.Errorf("the executor changed %s", name)
		}
	}
	// Raw SQL: no other column is the executor's, whatever the row.
	for _, c := range []struct {
		what, stmt string
		id         uuid.UUID
		state      string
	}{
		{"the remittance email", `UPDATE fyniq.contact SET remittance_email = 'steal@evil.example' WHERE id = $1`, company.ID, "42501"},
		{"the legal name", `UPDATE fyniq.contact SET legal_name = 'Evil Ltd' WHERE id = $1`, company.ID, "42501"},
		{"the tax residence", `UPDATE fyniq.contact SET tax_residence = 'ZZ' WHERE id = $1`, company.ID, "42501"},
		{"the UEN", `UPDATE fyniq.contact SET uen_or_tax_id = '1' WHERE id = $1`, company.ID, "42501"},
		{"the Peppol ID", `UPDATE fyniq.contact SET peppol_id = '0195:x' WHERE id = $1`, company.ID, "42501"},
		{"the active flag", `UPDATE fyniq.contact SET is_active = false WHERE id = $1`, company.ID, "42501"},
		{"making it a director", `UPDATE fyniq.contact SET is_director = true WHERE id = $1`, company.ID, "42501"},
		{"linking a member", `UPDATE fyniq.contact SET member_user_id = gen_random_uuid() WHERE id = $1`, company.ID, "42501"},
		{"the discount terms", `UPDATE fyniq.contact SET discount_days = 5 WHERE id = $1`, company.ID, "42501"},
		{"a person's phone", `UPDATE fyniq.contact SET phone = '1' WHERE id = $1`, person.ID, "42501"},
		{"deleting a contact", `DELETE FROM fyniq.contact WHERE id = $1`, company.ID, "42501"},
		{"adding a contact", `INSERT INTO fyniq.contact (legal_name, is_customer) VALUES ('Ghost', true)`, uuid.Nil, "42501"},
	} {
		err := asOrg(ctx, conn(t, "fyniq_executor"), b.Org, func(tx pgx.Tx) error {
			if c.id == uuid.Nil {
				_, err := tx.Exec(ctx, c.stmt)
				return err
			}
			_, err := tx.Exec(ctx, c.stmt, c.id)
			return err
		})
		wantState(t, c.what, err, c.state)
	}
}
